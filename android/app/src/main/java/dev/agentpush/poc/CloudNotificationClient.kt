package dev.agentpush.poc

import android.util.Log
import dev.agentpush.poc.protocol.DeviceIdentity
import dev.agentpush.poc.protocol.HomingCrypto
import dev.agentpush.poc.protocol.HomingCryptoAndroid
import dev.agentpush.poc.protocol.HomingEnvelope
import dev.agentpush.poc.protocol.PairedComputer
import dev.agentpush.poc.protocol.ProtocolUrls
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import org.json.JSONObject
import java.time.Instant
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

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
        RECONNECTING,
        STOPPED,
    }

    interface Listener {
        /**
         * Return true only after the notification was handed to Android. The
         * queue acknowledgement is sent after this returns true.
         */
        fun onNotification(computer: PairedComputer, payload: NotifyPayload): Boolean

        fun onStateChanged(server: String, state: State, detail: String?) = Unit
    }

    private val lock = Any()
    private val sessions = linkedMapOf<String, ServerSession>()
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

    fun stop() {
        val current = synchronized(lock) {
            running = false
            val copy = sessions.values.toList()
            sessions.clear()
            copy
        }
        current.forEach(ServerSession::stop)
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
            val url = runCatching {
                ProtocolUrls.normalizeWebSocketUrl(computer.server)
            }.getOrNull() ?: return@forEach
            grouped.getOrPut(url) { mutableListOf() }.add(computer)
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
            }
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
            }
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

        private fun connect() {
            val currentGeneration: Int
            synchronized(stateLock) {
                if (!started || stopped) return
                reconnectTask?.cancel(false)
                reconnectTask = null
                currentGeneration = generation.incrementAndGet()
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

                    override fun onClosed(
                        webSocket: WebSocket,
                        code: Int,
                        reason: String,
                    ) {
                        connectionLost(
                            currentGeneration,
                            "连接关闭 $code${reason.takeIf { it.isNotBlank() }?.let { ": $it" }.orEmpty()}",
                        )
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
                    connected(currentGeneration)
                    sendPushRegistration()
                }
                "msg" -> handleEnvelope(webSocket, currentGeneration, message)
                "ping" -> webSocket.send("""{"t":"pong"}""")
                "pong" -> Unit
                "error" -> {
                    val detail = message.optString("message").ifBlank {
                        message.optString("code")
                    }
                    listener.onStateChanged(serverUrl, State.RECONNECTING, detail)
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
                backoffMillis = INITIAL_RECONNECT_MILLIS
                reconnectTask?.cancel(false)
                reconnectTask = null
                cancelHeartbeatLocked()
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
            if (payload.optString("type") != "event") {
                acknowledge(webSocket, envelope.id)
                return
            }
            val name = payload.optString("name")
            if (name != "notify") {
                acknowledge(webSocket, envelope.id)
                return
            }
            val data = payload.optJSONObject("data")
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

        private fun acknowledge(webSocket: WebSocket, id: String) {
            webSocket.send(JSONObject().put("t", "ack").put("id", id).toString())
        }

        private fun connectionLost(currentGeneration: Int, detail: String) {
            val delay: Long
            synchronized(stateLock) {
                if (currentGeneration != generation.get() || stopped) return
                generation.incrementAndGet()
                socket = null
                cancelHeartbeatLocked()
                backoffMillis = (backoffMillis * 2).coerceAtMost(MAX_RECONNECT_MILLIS)
                delay = backoffMillis / 2
                reconnectTask?.cancel(false)
                reconnectTask = scheduler.schedule(
                    { connect() },
                    delay,
                    TimeUnit.MILLISECONDS,
                )
            }
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

    companion object {
        private const val TAG = "HomingCloudWS"
        private const val INITIAL_RECONNECT_MILLIS = 1_000L
        private const val MAX_RECONNECT_MILLIS = 60_000L
        private const val HEARTBEAT_SECONDS = 25L

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
