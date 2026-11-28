package dev.agentpush.poc

import android.util.Log
import dev.agentpush.poc.protocol.DeviceIdentity
import dev.agentpush.poc.protocol.HomingCrypto
import dev.agentpush.poc.protocol.HomingCryptoAndroid
import dev.agentpush.poc.protocol.HomingEnvelope
import dev.agentpush.poc.protocol.HomingKeyPair
import dev.agentpush.poc.protocol.PairedComputer
import dev.agentpush.poc.protocol.ProtocolUrls
import kotlinx.coroutines.CancellableContinuation
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withTimeoutOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.time.Instant
import java.util.Base64
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

data class CloudRpcResult(
    val requestId: String,
    val status: Int,
    val body: Any?,
)

class CloudRpcException(message: String) : Exception(message)

private data class PendingCloudRpc(
    val serverUrl: String,
    val continuation: CancellableContinuation<CloudRpcResult>,
)

private data class PendingCloudUpload(
    val serverUrl: String,
    val computerId: String,
    val continuation: CancellableContinuation<CloudRpcResult>,
)

private data class PendingCloudDownload(
    val serverUrl: String,
    val computerId: String,
    val continuation: CancellableContinuation<ByteArray>,
    val output: ByteArrayOutputStream = ByteArrayOutputStream(),
    var nextSeq: Int = 0,
)

/**
 * Receives end-to-end encrypted notification events over protocol v1.
 *
 * One socket is kept per relay server. A socket may carry events from several
 * paired computers because they all address the same phone identity.
 */
class CloudNotificationClient(
    private val identity: DeviceIdentity,
    private val computersProvider: () -> List<PairedComputer>,
    private val listener: Listener,
    private val pushTokenProvider: () -> String? = { null },
    private val crypto: HomingCrypto = HomingCryptoAndroid.instance,
    private val httpClient: OkHttpClient = defaultHttpClient(),
    private val scheduler: ScheduledExecutorService = Executors.newSingleThreadScheduledExecutor {
        Thread(it, "homing-cloud-ws").apply { isDaemon = true }
    },
) {
    enum class State {
        CONNECTING,
        CONNECTED,
        AUTH_REQUIRED,
        RECONNECTING,
        STOPPED,
    }

    interface Listener {
        /**
         * Return true only after the notification was handed to Android. The
         * queue acknowledgement is sent after this returns true.
         */
        fun onNotification(computer: PairedComputer, payload: NotifyPayload): Boolean

        /**
         * Receives a decrypted SSE-shaped event. Return true once it was
         * handled so the relay can remove it from the offline queue.
         */
        fun onEvent(
            computer: PairedComputer,
            name: String,
            data: JSONObject,
        ): Boolean = true

        fun onStateChanged(server: String, state: State, detail: String?) = Unit
    }

    private val lock = Any()
    private val sessions = linkedMapOf<String, ServerSession>()
    private val rpcLock = Any()
    private val pendingRpc = mutableMapOf<String, PendingCloudRpc>()
    private val fileLock = Any()
    private val pendingUploads = mutableMapOf<String, PendingCloudUpload>()
    private val pendingDownloads = mutableMapOf<String, PendingCloudDownload>()
    private val seenEnvelopes = object : LinkedHashMap<String, Long>() {
        override fun removeEldestEntry(
            eldest: MutableMap.MutableEntry<String, Long>?,
        ): Boolean = size > MAX_SEEN_ENVELOPES
    }
    private var running = false

    fun start() {
        val started = mutableListOf<ServerSession>()
        synchronized(lock) {
            if (running) return
            running = true
            syncComputersLocked(started)
        }
        started.forEach(ServerSession::start)
    }

    /** Picks up a computer added or removed while the service is running. */
    fun refreshComputers() {
        val started = mutableListOf<ServerSession>()
        val stopped = mutableListOf<ServerSession>()
        synchronized(lock) {
            if (!running) return
            syncComputersLocked(started, stopped)
        }
        stopped.forEach(ServerSession::stop)
        started.forEach(ServerSession::start)
    }

    fun reconnectNow() {
        val current = synchronized(lock) {
            if (!running) return
            sessions.values.toList()
        }
        current.forEach(ServerSession::reconnectNow)
    }

    fun updatePushRegistration() {
        val current = synchronized(lock) {
            if (!running) return
            sessions.values.toList()
        }
        current.forEach(ServerSession::sendPushRegistration)
    }

    suspend fun requestRPC(
        computer: PairedComputer,
        method: String,
        path: String,
        body: JSONObject? = null,
        timeoutMillis: Long = DEFAULT_RPC_TIMEOUT_MILLIS,
    ): CloudRpcResult {
        require(path.startsWith("/api/")) { "云端 RPC 只能访问 /api/ 路径" }
        val normalizedMethod = method.trim().uppercase().ifBlank { "GET" }
        if (normalizedMethod !in ALLOWED_RPC_METHODS) {
            throw CloudRpcException("云端请求方法不受支持")
        }
        val candidates = serverSessions(computer)
        if (candidates.isEmpty()) {
            throw CloudRpcException("这台电脑还没有可用连接")
        }
        val requestId = crypto.ulid()
        val payload = JSONObject()
            .put("type", "rpc")
            .put("rid", requestId)
            .put("method", normalizedMethod)
            .put("path", path)
            .put("accept_encoding", "gzip")
        if (body != null) payload.put("body", body)

        val startedAt = System.currentTimeMillis()
        var lastError: Throwable? = null
        val ordered = candidates.sortedByDescending { it.second.isConnected() }
        val attempts = if (canReplayTransportRequest(normalizedMethod)) ordered else ordered.take(1)
        for (candidate in attempts) {
            val remaining = timeoutMillis - (System.currentTimeMillis() - startedAt)
            if (remaining <= 0) break
            try {
                return requestRPCOnSession(
                    serverUrl = candidate.first,
                    session = candidate.second,
                    computer = computer,
                    requestId = requestId,
                    payload = payload,
                    timeoutMillis = remaining,
                )
            } catch (error: CancellationException) {
                throw error
            } catch (error: Exception) {
                lastError = error
            }
        }
        throw lastError ?: CloudRpcException("云端请求超时")
    }

    private suspend fun requestRPCOnSession(
        serverUrl: String,
        session: ServerSession,
        computer: PairedComputer,
        requestId: String,
        payload: JSONObject,
        timeoutMillis: Long,
    ): CloudRpcResult {
        val result = withTimeoutOrNull(timeoutMillis.coerceAtLeast(1L)) {
            suspendCancellableCoroutine<CloudRpcResult> { continuation ->
                synchronized(rpcLock) {
                    pendingRpc[requestId] = PendingCloudRpc(serverUrl, continuation)
                }
                continuation.invokeOnCancellation {
                    synchronized(rpcLock) { pendingRpc.remove(requestId) }
                }
                if (!session.sendEncrypted(computer, payload)) {
                    failRPCRequest(requestId, "云端连接未就绪")
                }
            }
        }
        return result ?: throw CloudRpcException("云端请求超时")
    }

    suspend fun uploadFile(
        computer: PairedComputer,
        path: String,
        bytes: ByteArray,
        contentType: String = "application/octet-stream",
        timeoutMillis: Long = DEFAULT_FILE_TIMEOUT_MILLIS,
    ): CloudRpcResult {
        require(path.startsWith("/api/upload")) { "云端文件上传只能写入 /api/upload" }
        if (bytes.size > MAX_FILE_BYTES) {
            throw CloudRpcException("单文件上限 20 MB")
        }
        val candidates = serverSessions(computer)
        val (serverUrl, session) = candidates.firstOrNull { it.second.isConnected() }
            ?: candidates.firstOrNull()
            ?: throw CloudRpcException("這台電腦還沒有可用連接")
        val fid = crypto.ulid()
        val result = withTimeoutOrNull(timeoutMillis.coerceAtLeast(1L)) {
            suspendCancellableCoroutine<CloudRpcResult> { continuation ->
                val pending = PendingCloudUpload(
                    serverUrl = serverUrl,
                    computerId = computer.deviceId,
                    continuation = continuation,
                )
                synchronized(fileLock) { pendingUploads[fid] = pending }
                continuation.invokeOnCancellation {
                    synchronized(fileLock) { pendingUploads.remove(fid, pending) }
                }

                fun sendChunks() {
                    var seq = 0
                    if (bytes.isEmpty()) {
                        val payload = JSONObject()
                            .put("type", "file.chunk")
                            .put("fid", fid)
                            .put("seq", 0)
                            .put("last", true)
                            .put("data", "")
                            .put("path", path)
                            .put("content_type", contentType)
                        if (!session.sendEncrypted(computer, payload)) {
                            failFileTransfer(fid, "雲端連接未就緒", upload = true)
                        }
                        return
                    }
                    var offset = 0
                    while (offset < bytes.size) {
                        val end = (offset + FILE_CHUNK_BYTES).coerceAtMost(bytes.size)
                        val payload = JSONObject()
                            .put("type", "file.chunk")
                            .put("fid", fid)
                            .put("seq", seq)
                            .put("last", end == bytes.size)
                            .put(
                                "data",
                                Base64.getEncoder().encodeToString(
                                    bytes.copyOfRange(offset, end),
                                ),
                            )
                        if (seq == 0) {
                            payload.put("path", path)
                            payload.put("content_type", contentType)
                        }
                        if (!session.sendEncrypted(computer, payload)) {
                            failFileTransfer(fid, "雲端連接未就緒", upload = true)
                            return
                        }
                        offset = end
                        seq += 1
                    }
                }
                sendChunks()
            }
        }
        return result ?: throw CloudRpcException("雲端文件上傳超時")
    }

    suspend fun downloadFile(
        computer: PairedComputer,
        path: String,
        timeoutMillis: Long = DEFAULT_FILE_TIMEOUT_MILLIS,
    ): ByteArray {
        if ((!path.startsWith("/f/") && !path.startsWith("/api/fs/raw?")) || path.contains("..")) {
            throw CloudRpcException("云端附件下载只允许能力链接或项目媒体预览")
        }
        val candidates = serverSessions(computer)
        val (serverUrl, session) = candidates.firstOrNull { it.second.isConnected() }
            ?: candidates.firstOrNull()
            ?: throw CloudRpcException("這台電腦還沒有可用連接")
        val fid = crypto.ulid()
        val result = withTimeoutOrNull(timeoutMillis.coerceAtLeast(1L)) {
            suspendCancellableCoroutine<ByteArray> { continuation ->
                val pending = PendingCloudDownload(
                    serverUrl = serverUrl,
                    computerId = computer.deviceId,
                    continuation = continuation,
                )
                synchronized(fileLock) { pendingDownloads[fid] = pending }
                continuation.invokeOnCancellation {
                    synchronized(fileLock) { pendingDownloads.remove(fid, pending) }
                }
                val payload = JSONObject()
                    .put("type", "file.request")
                    .put("fid", fid)
                    .put("path", path)
                if (!session.sendEncrypted(computer, payload)) {
                    failFileTransfer(fid, "雲端連接未就緒", upload = false)
                }
            }
        }
        return result ?: throw CloudRpcException("雲端附件下載超時")
    }

    fun stop() {
        val current = synchronized(lock) {
            running = false
            val copy = sessions.values.toList()
            sessions.clear()
            copy
        }
        current.forEach(ServerSession::stop)
        failPendingRPC(null, "云端连接已停止")
        failPendingFiles(null, "云端连接已停止")
        scheduler.shutdownNow()
    }

    private fun syncComputersLocked(
        started: MutableList<ServerSession>,
        stopped: MutableList<ServerSession> = mutableListOf(),
    ) {
        val computers = runCatching(computersProvider).getOrDefault(emptyList())
        val grouped = LinkedHashMap<String, MutableList<PairedComputer>>()
        computers.forEach { computer ->
            if (computer.deviceId.isBlank()) return@forEach
            computer.serverCandidates().forEach { raw ->
                val url = runCatching {
                    ProtocolUrls.normalizeWebSocketUrl(raw)
                }.getOrNull() ?: return@forEach
                grouped.getOrPut(url) { mutableListOf() }.add(computer)
            }
        }

        val removed = sessions.keys.filterNot(grouped::containsKey)
        removed.forEach { url ->
            sessions.remove(url)?.let(stopped::add)
        }
        grouped.forEach { (url, list) ->
            val next = list.associateBy { it.deviceId }
            val session = sessions[url]
            if (session == null) {
                sessions[url] = ServerSession(url, next).also(started::add)
            } else {
                session.updateComputers(next)
            }
        }
    }

    private inner class ServerSession(
        private val serverUrl: String,
        initialComputers: Map<String, PairedComputer>,
    ) {
        private val stateLock = Any()
        private val generation = AtomicInteger(0)
        private var computers = initialComputers
        private var started = false
        private var stopped = false
        private var socket: WebSocket? = null
        private var reconnectTask: ScheduledFuture<*>? = null
        private var heartbeatTask: ScheduledFuture<*>? = null
        private var backoffMillis = INITIAL_RECONNECT_MILLIS
        private var connectedNow = false
        private var connectedAtMillis = 0L

        fun start() {
            val shouldConnect = synchronized(stateLock) {
                if (started || stopped) return
                started = true
                true
            }
            if (shouldConnect) connect()
        }

        fun updateComputers(next: Map<String, PairedComputer>) {
            synchronized(stateLock) {
                computers = next
            }
        }

        fun carries(deviceId: String): Boolean {
            return synchronized(stateLock) { computers.containsKey(deviceId) }
        }

        fun isConnected(): Boolean {
            return synchronized(stateLock) { connectedNow }
        }

        fun reconnectNow() {
            synchronized(stateLock) {
                if (!started || stopped) return
                reconnectTask?.cancel(false)
                reconnectTask = null
                generation.incrementAndGet()
                socket?.cancel()
                socket = null
                cancelHeartbeatLocked()
                backoffMillis = INITIAL_RECONNECT_MILLIS
                connectedNow = false
            }
            failPendingRPC(serverUrl, "云端连接正在重连")
            failPendingFiles(serverUrl, "云端连接正在重连")
            listener.onStateChanged(serverUrl, State.RECONNECTING, "网络恢复，立即重连")
            connect()
        }

        fun stop() {
            val socketToClose: WebSocket?
            synchronized(stateLock) {
                stopped = true
                generation.incrementAndGet()
                reconnectTask?.cancel(false)
                reconnectTask = null
                cancelHeartbeatLocked()
                socketToClose = socket
                socket = null
                connectedNow = false
            }
            failPendingRPC(serverUrl, "云端连接已停止")
            failPendingFiles(serverUrl, "云端连接已停止")
            socketToClose?.close(1000, "client stopped")
            listener.onStateChanged(serverUrl, State.STOPPED, null)
        }

        fun sendPushRegistration() {
            val token = pushTokenProvider()?.trim().orEmpty()
            if (token.isBlank()) return
            val active = synchronized(stateLock) { socket }
            active?.send(JSONObject()
                .put("t", "push.register")
                .put("provider", "fcm")
                .put("token", token)
                .toString())
        }

        fun sendEncrypted(computer: PairedComputer, payload: JSONObject): Boolean {
            val active = synchronized(stateLock) { socket } ?: return false
            return runCatching {
                val recipientPublicKey = crypto.fromBase64(computer.publicKeyBase64)
                val envelope = crypto.createEnvelope(
                    from = identity.deviceId,
                    to = computer.deviceId,
                    payloadJson = payload.toString(),
                    sender = HomingKeyPair(identity.publicKey, identity.secretKey),
                    recipientPublicKey = recipientPublicKey,
                )
                active.send(envelope.toRelayJson().toString())
            }.getOrElse {
                Log.w(TAG, "Unable to encrypt RPC request", it)
                false
            }
        }

        private fun connect() {
            val currentGeneration: Int
            synchronized(stateLock) {
                if (!started || stopped) return
                reconnectTask?.cancel(false)
                reconnectTask = null
                currentGeneration = generation.incrementAndGet()
                connectedNow = false
            }
            listener.onStateChanged(
                serverUrl,
                if (backoffMillis == INITIAL_RECONNECT_MILLIS) {
                    State.CONNECTING
                } else {
                    State.RECONNECTING
                },
                null,
            )
            val request = Request.Builder().url(serverUrl).build()
            val webSocket = httpClient.newWebSocket(
                request,
                object : WebSocketListener() {
                    override fun onOpen(webSocket: WebSocket, response: Response) = Unit

                    override fun onMessage(webSocket: WebSocket, text: String) {
                        if (!isCurrent(currentGeneration)) return
                        handleMessage(webSocket, currentGeneration, text)
                    }

                    override fun onFailure(
                        webSocket: WebSocket,
                        t: Throwable,
                        response: Response?,
                    ) {
                        connectionLost(
                            currentGeneration,
                            "${t.javaClass.simpleName}: ${t.message.orEmpty()}".trim(),
                        )
                    }

                    override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                        webSocket.close(code, reason)
                        if (isRelayAuthCloseReason(reason)) {
                            val authCode = if (reason.contains("suspended", ignoreCase = true)) {
                                "account_suspended"
                            } else {
                                "account_required"
                            }
                            handleAuthBlocked(null, currentGeneration, authCode, formatAuthError(authCode, reason))
                        } else {
                            connectionLost(currentGeneration, "连接关闭 $code")
                        }
                    }

                    override fun onClosed(
                        webSocket: WebSocket,
                        code: Int,
                        reason: String,
                    ) {
                        if (isRelayAuthCloseReason(reason)) {
                            val authCode = if (reason.contains("suspended", ignoreCase = true)) {
                                "account_suspended"
                            } else {
                                "account_required"
                            }
                            handleAuthBlocked(null, currentGeneration, authCode, formatAuthError(authCode, reason))
                        } else {
                            connectionLost(
                                currentGeneration,
                                "连接关闭 $code${reason.takeIf { it.isNotBlank() }?.let { ": $it" }.orEmpty()}",
                            )
                        }
                    }
                },
            )
            synchronized(stateLock) {
                if (currentGeneration != generation.get() || stopped) {
                    webSocket.cancel()
                } else {
                    socket = webSocket
                }
            }
        }

        private fun handleMessage(
            webSocket: WebSocket,
            currentGeneration: Int,
            text: String,
        ) {
            val message = runCatching { JSONObject(text) }.getOrElse {
                Log.w(TAG, "Relay sent invalid JSON", it)
                return
            }
            when (message.optString("t")) {
                "challenge" -> sendHello(webSocket, currentGeneration, message.optString("nonce"))
                "hello.ok" -> {
                    if (message.has("bound") && !message.optBoolean("bound", true)) {
                        handleAuthBlocked(
                            webSocket,
                            currentGeneration,
                            "account_required",
                            formatAuthError("account_required", ""),
                        )
                    } else {
                        connected(currentGeneration)
                        sendPushRegistration()
                    }
                }
                "msg" -> handleEnvelope(webSocket, currentGeneration, message)
                "ping" -> webSocket.send("""{"t":"pong"}""")
                "pong" -> Unit
                "error" -> {
                    val code = message.optString("code")
                    val detail = message.optString("message").ifBlank { code }
                    if (code in RELAY_POLICY_ERROR_CODES) {
                        // Quota / size limits reject one request but keep the
                        // session valid. Reconnecting would only resend the same
                        // traffic and hammer the relay, so fail the waiting
                        // requests with the relay's explanation instead.
                        failPendingRPC(serverUrl, detail)
                        failPendingFiles(serverUrl, detail)
                        listener.onStateChanged(serverUrl, State.CONNECTED, detail)
                    } else if (code in RELAY_AUTH_ERROR_CODES) {
                        handleAuthBlocked(
                            webSocket,
                            currentGeneration,
                            code,
                            formatAuthError(code, detail),
                        )
                    } else {
                        // An error frame does not close the socket. Start an actual
                        // reconnect and fail uncertain requests instead of leaving
                        // the UI disconnected while this session stays open.
                        connectionLost(currentGeneration, detail)
                        webSocket.cancel()
                    }
                }
            }
        }

        private fun sendHello(
            webSocket: WebSocket,
            currentGeneration: Int,
            nonce: String,
        ) {
            if (nonce.isBlank()) return
            val ts = Instant.now().toString()
            val signature = runCatching {
                crypto.helloSignature(identity.secretKey, nonce, ts)
            }.getOrElse {
                Log.w(TAG, "Unable to sign protocol hello", it)
                webSocket.close(1011, "hello signature failed")
                return
            }
            val sent = webSocket.send(JSONObject()
                .put("t", "hello")
                .put("device_id", identity.deviceId)
                .put("pub", identity.publicKeyBase64)
                .put("ts", ts)
                .put("sig", crypto.toBase64(signature))
                .toString())
            if (!sent) {
                connectionLost(currentGeneration, "hello 发送失败")
            }
        }

        private fun connected(currentGeneration: Int) {
            synchronized(stateLock) {
                if (currentGeneration != generation.get() || stopped) return
                reconnectTask?.cancel(false)
                reconnectTask = null
                cancelHeartbeatLocked()
                connectedNow = true
                connectedAtMillis = System.currentTimeMillis()
                heartbeatTask = scheduler.scheduleWithFixedDelay(
                    {
                        if (isCurrent(currentGeneration)) {
                            synchronized(stateLock) { socket }
                                ?.send("""{"t":"ping"}""")
                        }
                    },
                    HEARTBEAT_SECONDS,
                    HEARTBEAT_SECONDS,
                    TimeUnit.SECONDS,
                )
            }
            listener.onStateChanged(serverUrl, State.CONNECTED, null)
        }

        private fun handleEnvelope(
            webSocket: WebSocket,
            currentGeneration: Int,
            message: JSONObject,
        ) {
            val envelope = HomingEnvelope(
                v = message.optInt("v", 0),
                t = message.optString("t"),
                id = message.optString("id"),
                from = message.optString("from"),
                to = message.optString("to"),
                ts = message.optString("ts"),
                nonce = message.optString("nonce"),
                ct = message.optString("ct"),
            )
            if (envelope.v != 1 || envelope.t != "msg" ||
                envelope.to != identity.deviceId || envelope.id.isBlank()
            ) {
                return
            }
            if (!claimEnvelope(envelope.from, envelope.id)) {
                acknowledge(webSocket, envelope.id)
                return
            }
            val computer = synchronized(stateLock) { computers[envelope.from] } ?: return
            val senderPublicKey = runCatching {
                crypto.fromBase64(computer.publicKeyBase64)
            }.getOrNull() ?: return
            val plaintext = runCatching {
                crypto.openEnvelope(envelope, senderPublicKey, identity.secretKey)
            }.getOrElse {
                Log.w(TAG, "Unable to decrypt envelope from ${computer.deviceId}", it)
                return
            }
            val payload = runCatching { JSONObject(plaintext) }.getOrNull() ?: return
            if (payload.optString("type") == "rpc_result") {
                completeRPC(payload)
                acknowledge(webSocket, envelope.id)
                return
            }
            when (payload.optString("type")) {
                "file.result" -> {
                    completeFileTransfer(payload, computer)
                    acknowledge(webSocket, envelope.id)
                    return
                }
                "file.chunk" -> {
                    acceptDownloadChunk(payload, computer)
                    acknowledge(webSocket, envelope.id)
                    return
                }
            }
            if (payload.optString("type") != "event") {
                acknowledge(webSocket, envelope.id)
                return
            }
            val name = payload.optString("name")
            if (name.isBlank()) {
                acknowledge(webSocket, envelope.id)
                return
            }
            val data = payload.optJSONObject("data")
            if (name != "notify") {
                val delivered = data != null && runCatching {
                    listener.onEvent(computer, name, data)
                }.getOrElse {
                    Log.e(TAG, "Event handler failed for $name", it)
                    false
                }
                if (delivered) {
                    acknowledge(webSocket, envelope.id)
                } else {
                    webSocket.close(1011, "event failed")
                    connectionLost(currentGeneration, "本地事件处理失败")
                }
                return
            }
            val notify = data?.let(NotifyPayload::fromJson)
            if (notify == null) {
                acknowledge(webSocket, envelope.id)
                return
            }
            val delivered = runCatching {
                listener.onNotification(computer, notify)
            }.getOrElse {
                Log.e(TAG, "Notification handler failed", it)
                false
            }
            if (delivered) {
                acknowledge(webSocket, envelope.id)
            } else {
                webSocket.close(1011, "notification failed")
                connectionLost(currentGeneration, "本地通知处理失败")
            }
        }

        private fun completeRPC(payload: JSONObject) {
            val requestId = payload.optString("rid")
            if (requestId.isBlank()) return
            val pending = synchronized(rpcLock) {
                pendingRpc[requestId]
                    ?.takeIf { it.serverUrl == serverUrl }
                    ?.let { pendingRpc.remove(requestId) }
            } ?: return
            val body = try {
                RpcBodyCodec.decode(payload)
            } catch (error: Exception) {
                pending.continuation.resumeWithException(CloudRpcException("雲端回應解碼失敗"))
                return
            }
            pending.continuation.resume(
                CloudRpcResult(
                    requestId = requestId,
                    status = payload.optInt("status", 0),
                    body = body,
                ),
            )
        }

        private fun acknowledge(webSocket: WebSocket, id: String) {
            webSocket.send(JSONObject().put("t", "ack").put("id", id).toString())
        }

        private fun handleAuthBlocked(
            webSocket: WebSocket?,
            currentGeneration: Int,
            code: String,
            detail: String,
        ) {
            synchronized(stateLock) {
                if (currentGeneration != generation.get() || stopped) return
                generation.incrementAndGet()
                socket = null
                connectedNow = false
                connectedAtMillis = 0L
                cancelHeartbeatLocked()
                backoffMillis = MAX_RECONNECT_MILLIS
                reconnectTask?.cancel(false)
                reconnectTask = scheduler.schedule(
                    { connect() },
                    MAX_RECONNECT_MILLIS,
                    TimeUnit.MILLISECONDS,
                )
            }
            webSocket?.close(1000, code)
            failPendingRPC(serverUrl, detail)
            failPendingFiles(serverUrl, detail)
            listener.onStateChanged(serverUrl, State.AUTH_REQUIRED, detail)
        }

        private fun connectionLost(currentGeneration: Int, detail: String) {
            val delay: Long
            val now = System.currentTimeMillis()
            synchronized(stateLock) {
                if (currentGeneration != generation.get() || stopped) return
                generation.incrementAndGet()
                socket = null
                connectedNow = false
                if (connectedAtMillis > 0L && (now - connectedAtMillis) >= STABLE_SESSION_MILLIS) {
                    backoffMillis = INITIAL_RECONNECT_MILLIS
                }
                connectedAtMillis = 0L
                cancelHeartbeatLocked()
                delay = backoffMillis
                backoffMillis = (backoffMillis * 2).coerceAtMost(MAX_RECONNECT_MILLIS)
                reconnectTask?.cancel(false)
                reconnectTask = scheduler.schedule(
                    { connect() },
                    delay,
                    TimeUnit.MILLISECONDS,
                )
            }
            failPendingRPC(serverUrl, "云端连接已断开")
            failPendingFiles(serverUrl, "云端连接已断开")
            listener.onStateChanged(serverUrl, State.RECONNECTING, detail)
        }

        private fun cancelHeartbeatLocked() {
            heartbeatTask?.cancel(false)
            heartbeatTask = null
        }

        private fun isCurrent(value: Int): Boolean {
            return synchronized(stateLock) {
                !stopped && value == generation.get()
            }
        }
    }

    private fun serverSessions(
        computer: PairedComputer,
    ): List<Pair<String, ServerSession>> {
        val seen = mutableSetOf<String>()
        return buildList {
            computer.serverCandidates().forEach { raw ->
                val url = runCatching {
                    ProtocolUrls.normalizeWebSocketUrl(raw)
                }.getOrNull() ?: return@forEach
                if (!seen.add(url)) return@forEach
                val session = synchronized(lock) { sessions[url] } ?: return@forEach
                if (session.carries(computer.deviceId)) add(url to session)
            }
        }
    }

    private fun claimEnvelope(from: String, id: String): Boolean {
        val now = System.currentTimeMillis()
        synchronized(seenEnvelopes) {
            seenEnvelopes.entries.removeAll { now - it.value > SEEN_ENVELOPE_TTL_MILLIS }
            val key = "$from:$id"
            if (seenEnvelopes.containsKey(key)) return false
            seenEnvelopes[key] = now
            return true
        }
    }

    private fun acceptDownloadChunk(payload: JSONObject, computer: PairedComputer) {
        val fid = payload.optString("fid")
        if (fid.isBlank()) return
        val pending = synchronized(fileLock) {
            pendingDownloads[fid]?.takeIf {
                it.computerId == computer.deviceId
            }
        } ?: return
        val seq = payload.optInt("seq", -1)
        if (seq != pending.nextSeq) {
            failFileTransfer(fid, "附件分塊序號不連續", upload = false)
            return
        }
        val data = runCatching {
            Base64.getDecoder().decode(payload.optString("data"))
        }.getOrElse {
            failFileTransfer(fid, "附件分塊格式錯誤", upload = false)
            return
        }
        if (pending.output.size() + data.size > MAX_FILE_BYTES) {
            failFileTransfer(fid, "单文件上限 20 MB", upload = false)
            return
        }
        pending.output.write(data)
        pending.nextSeq += 1
        if (!payload.optBoolean("last", false)) return
        val completed = synchronized(fileLock) {
            pendingDownloads.remove(fid, pending)
        }
        if (completed) {
            pending.continuation.resume(pending.output.toByteArray())
        }
    }

    private fun completeFileTransfer(payload: JSONObject, computer: PairedComputer) {
        val fid = payload.optString("fid")
        if (fid.isBlank()) return
        val upload = synchronized(fileLock) {
            pendingUploads[fid]?.takeIf {
                it.computerId == computer.deviceId
            }
        }
        if (upload != null) {
            val removed = synchronized(fileLock) { pendingUploads.remove(fid, upload) }
            if (removed) {
                upload.continuation.resume(
                    CloudRpcResult(
                        requestId = fid,
                        status = payload.optInt("status", 0),
                        body = payload.opt("body")?.takeUnless { it == JSONObject.NULL },
                    ),
                )
            }
            return
        }
        val download = synchronized(fileLock) {
            pendingDownloads[fid]?.takeIf {
                it.computerId == computer.deviceId
            }
        } ?: return
        val removed = synchronized(fileLock) { pendingDownloads.remove(fid, download) }
        if (!removed) return
        val body = payload.optJSONObject("body")
        val message = body?.optString("error")
            ?.takeIf { it.isNotBlank() }
            ?: "雲端附件下載失敗"
        download.continuation.resumeWithException(CloudRpcException(message))
    }

    private fun failFileTransfer(fid: String, message: String, upload: Boolean) {
        if (upload) {
            val pending = synchronized(fileLock) { pendingUploads.remove(fid) } ?: return
            pending.continuation.resumeWithException(CloudRpcException(message))
        } else {
            val pending = synchronized(fileLock) { pendingDownloads.remove(fid) } ?: return
            pending.continuation.resumeWithException(CloudRpcException(message))
        }
    }

    private fun failPendingFiles(serverUrl: String?, message: String) {
        val uploads = synchronized(fileLock) {
            val ids = pendingUploads.entries
                .filter { serverUrl == null || it.value.serverUrl == serverUrl }
                .map { it.key }
            ids.mapNotNull { pendingUploads.remove(it) }
        }
        uploads.forEach {
            it.continuation.resumeWithException(CloudRpcException(message))
        }
        val downloads = synchronized(fileLock) {
            val ids = pendingDownloads.entries
                .filter { serverUrl == null || it.value.serverUrl == serverUrl }
                .map { it.key }
            ids.mapNotNull { pendingDownloads.remove(it) }
        }
        downloads.forEach {
            it.continuation.resumeWithException(CloudRpcException(message))
        }
    }

    private fun failPendingRPC(serverUrl: String?, message: String) {
        val failed = synchronized(rpcLock) {
            val ids = pendingRpc.entries
                .filter { serverUrl == null || it.value.serverUrl == serverUrl }
                .map { it.key }
            ids.mapNotNull { pendingRpc.remove(it) }
        }
        failed.forEach {
            it.continuation.resumeWithException(CloudRpcException(message))
        }
    }

    private fun failRPCRequest(requestId: String, message: String) {
        val pending = synchronized(rpcLock) {
            pendingRpc.remove(requestId)
        } ?: return
        pending.continuation.resumeWithException(CloudRpcException(message))
    }

    companion object {
        private const val TAG = "HomingCloudWS"
        private val RELAY_POLICY_ERROR_CODES = setOf(
            "daily_quota_exceeded",
            "weekly_time_exceeded",
            "payload_too_large_for_tier",
        )
        private val RELAY_AUTH_ERROR_CODES = setOf(
            "account_required",
            "account_suspended",
            "invalid_relay_key",
            "not_paired",
            "host_quota_exceeded",
            "device_quota_exceeded",
        )
        private const val INITIAL_RECONNECT_MILLIS = 1_000L
        private const val MAX_RECONNECT_MILLIS = 60_000L
        private const val STABLE_SESSION_MILLIS = 15_000L

        private fun isRelayAuthCloseReason(reason: String): Boolean {
            val lower = reason.lowercase()
            return "account required" in lower ||
                "account suspended" in lower ||
                "invalid relay key" in lower
        }

        private fun formatAuthError(code: String, rawDetail: String): String =
            when (code) {
                "account_required" -> "中继账号未绑定：请先在电脑端「云中转设置」填写 Relay Key 并保存；若电脑已绑定请重新扫码配对"
                "not_paired" -> "设备配对已失效：请在电脑端打开配对二维码重新扫码"
                else -> rawDetail.ifBlank { code }
            }
        private const val HEARTBEAT_SECONDS = 25L
        private const val DEFAULT_RPC_TIMEOUT_MILLIS = 20_000L
        private const val DEFAULT_FILE_TIMEOUT_MILLIS = 90_000L
        private const val FILE_CHUNK_BYTES = 48 * 1024
        private const val MAX_FILE_BYTES = 20 * 1024 * 1024
        private const val MAX_SEEN_ENVELOPES = 2_000
        private const val SEEN_ENVELOPE_TTL_MILLIS = 10 * 60 * 1_000L
        private val ALLOWED_RPC_METHODS = setOf("GET", "POST", "PATCH", "PUT", "DELETE")

        private fun defaultHttpClient(): OkHttpClient {
            return OkHttpClient.Builder()
                .connectTimeout(10, TimeUnit.SECONDS)
                .readTimeout(0, TimeUnit.MILLISECONDS)
                .pingInterval(25, TimeUnit.SECONDS)
                .retryOnConnectionFailure(true)
                .build()
        }
    }
}

private fun HomingEnvelope.toRelayJson(): JSONObject = JSONObject()
    .put("v", v)
    .put("t", t)
    .put("id", id)
    .put("from", from)
    .put("to", to)
    .put("ts", ts)
    .put("nonce", nonce)
    .put("ct", ct)
