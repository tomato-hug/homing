package dev.agentpush.poc.protocol

import android.os.Handler
import android.os.Looper
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import org.json.JSONObject
import java.time.Instant
import java.util.Timer
import java.util.TimerTask
import java.util.concurrent.TimeUnit

class ProtocolPairingClient(
    private val identity: DeviceIdentity,
    private val words: List<String>,
    private val httpClient: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(0, TimeUnit.MILLISECONDS)
        .pingInterval(25, TimeUnit.SECONDS)
        .build(),
    private val crypto: HomingCrypto = HomingCryptoAndroid.instance,
) {
    interface Listener {
        fun onConnecting()
        fun onWaiting(hostId: String, sas: List<String>, fingerprint: String)
        fun onAccepted(computer: PairedComputer)
        fun onError(message: String)
    }

    private val mainHandler = Handler(Looper.getMainLooper())
    private var socket: WebSocket? = null
    private var timeout: Timer? = null
    private var heartbeat: Timer? = null
    private var finished = false

    fun start(
        server: String,
        token: String?,
        code6: String?,
        expectedHostId: String?,
        expectedFingerprint: String?,
        listener: Listener,
    ) {
        close()
        finished = false
        val wsUrl = runCatching { ProtocolUrls.normalizeWebSocketUrl(server) }
            .getOrElse {
                listener.onError(it.message ?: "服务器地址无效")
                return
            }
        if (token.isNullOrBlank() && code6.isNullOrBlank()) {
            listener.onError("需要扫码或输入 6 位配对码")
            return
        }
        listener.onConnecting()
        val request = Request.Builder().url(wsUrl).build()
        socket = httpClient.newWebSocket(request, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                startHeartbeat(webSocket)
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                val message = runCatching { JSONObject(text) }.getOrNull()
                if (message == null) {
                    fail(listener, "服务器返回了无法解析的消息")
                    return
                }
                when (message.optString("t")) {
                    "challenge" -> sendHello(webSocket, message.optString("nonce"))
                    "hello.ok" -> sendPairJoin(
                        webSocket,
                        token,
                        code6,
                        listener,
                    )
                    "pair.waiting" -> handleWaiting(
                        message,
                        expectedHostId,
                        expectedFingerprint,
                        listener,
                    )
                    "pair.accepted" -> handleAccepted(
                        message,
                        server,
                        expectedHostId,
                        expectedFingerprint,
                        listener,
                    )
                    "pair.rejected" -> fail(listener, "电脑拒绝了配对请求")
                    "error" -> fail(listener, friendlyError(message.optString("code")))
                    "msg", "pong", "ping" -> Unit
                }
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                if (!finished) {
                    fail(listener, "连接失败：${t.message ?: t.javaClass.simpleName}")
                }
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                if (!finished) {
                    fail(listener, "连接已断开${if (reason.isBlank()) "" else "：$reason"}")
                }
            }
        })
        timeout = Timer("homing-pair-timeout", true).also {
            it.schedule(object : TimerTask() {
                override fun run() {
                    fail(listener, "配对超时，请重新生成二维码或配对码")
                }
            }, 300_000)
        }
    }

    fun close() {
        finished = true
        timeout?.cancel()
        timeout = null
        heartbeat?.cancel()
        heartbeat = null
        socket?.close(1000, "client closed")
        socket = null
    }

    fun sendEncrypted(
        computer: PairedComputer,
        payload: JSONObject,
    ): Boolean {
        val recipientPublicKey = runCatching {
            crypto.fromBase64(computer.publicKeyBase64)
        }.getOrNull() ?: return false
        val sender = HomingKeyPair(identity.publicKey, identity.secretKey)
        val envelope = crypto.createEnvelope(
            from = identity.deviceId,
            to = computer.deviceId,
            payloadJson = payload.toString(),
            sender = sender,
            recipientPublicKey = recipientPublicKey,
        )
        return socket?.send(envelope.toJson().toString()) == true
    }

    private fun sendHello(webSocket: WebSocket, nonce: String) {
        if (nonce.isBlank()) {
            return
        }
        val ts = Instant.now().toString()
        val signature = crypto.helloSignature(identity.secretKey, nonce, ts)
        webSocket.send(JSONObject()
            .put("t", "hello")
            .put("device_id", identity.deviceId)
            .put("pub", identity.publicKeyBase64)
            .put("ts", ts)
            .put("sig", crypto.toBase64(signature))
            .toString())
    }

    private fun sendPairJoin(
        webSocket: WebSocket,
        token: String?,
        code6: String?,
        listener: Listener,
    ) {
        val message = JSONObject()
            .put("t", "pair.join")
            .put("pub", identity.publicKeyBase64)
            .put("name", android.os.Build.MODEL.ifBlank { "Android 手机" })
            .put("platform", "android")
        if (!token.isNullOrBlank()) message.put("token", token)
        else message.put("code6", code6?.filter { it in '0'..'9' })
        webSocket.send(message.toString())
        mainHandler.post { listener.onConnecting() }
    }

    private fun handleWaiting(
        message: JSONObject,
        expectedHostId: String?,
        expectedFingerprint: String?,
        listener: Listener,
    ) {
        val hostId = message.optString("host_id")
        val hostPublicKey = runCatching {
            crypto.fromBase64(message.optString("host_pub"))
        }.getOrNull()
        if (hostPublicKey == null) {
            fail(listener, "电脑公钥无效")
            return
        }
        val fingerprint = crypto.fingerprint(hostPublicKey)
        val verificationError = PairingHostVerifier.check(
            hostId = hostId,
            expectedHostId = expectedHostId,
            actualFingerprint = fingerprint,
            expectedFingerprint = expectedFingerprint,
        )
        if (verificationError != null) {
            fail(listener, verificationError)
            return
        }
        val sas = crypto.sasWords(hostPublicKey, identity.publicKey, words)
        mainHandler.post {
            listener.onWaiting(hostId, sas, fingerprint)
        }
    }

    private fun handleAccepted(
        message: JSONObject,
        server: String,
        expectedHostId: String?,
        expectedFingerprint: String?,
        listener: Listener,
    ) {
        val hostId = message.optString("host_id")
        val hostPublicKey = runCatching {
            crypto.fromBase64(message.optString("host_pub"))
        }.getOrNull()
        if (hostPublicKey == null) {
            fail(listener, "电脑公钥无效")
            return
        }
        val fingerprint = crypto.fingerprint(hostPublicKey)
        val verificationError = PairingHostVerifier.check(
            hostId = hostId,
            expectedHostId = expectedHostId,
            actualFingerprint = fingerprint,
            expectedFingerprint = expectedFingerprint,
        )
        if (verificationError != null) {
            fail(listener, verificationError)
            return
        }
        val now = System.currentTimeMillis()
        val lanServers = (message.optJSONArray("lan_servers")
            ?: message.optJSONArray("lan_urls"))
            ?.let { array ->
                buildList {
                    for (index in 0 until array.length()) {
                        val candidate = array.optString(index).trim()
                        if (candidate.isBlank()) continue
                        runCatching { ProtocolUrls.normalizeWebSocketUrl(candidate) }
                            .getOrNull()
                            ?.let(::add)
                    }
                }.distinct().take(8)
            }
            .orEmpty()
        val computer = PairedComputer(
            deviceId = hostId,
            name = "我的 Mac",
            server = server.trim(),
            publicKeyBase64 = crypto.toBase64(hostPublicKey),
            fingerprint = fingerprint,
            pairedAt = now,
            lastSeen = now,
            lanServers = lanServers,
        )
        finished = true
        timeout?.cancel()
        heartbeat?.cancel()
        socket?.close(1000, "paired")
        socket = null
        mainHandler.post { listener.onAccepted(computer) }
    }

    private fun startHeartbeat(webSocket: WebSocket) {
        heartbeat?.cancel()
        heartbeat = Timer("homing-pair-heartbeat", true).also {
            it.scheduleAtFixedRate(object : TimerTask() {
                override fun run() {
                    webSocket.send("""{"t":"ping"}""")
                }
            }, 25_000, 25_000)
        }
    }

    private fun fail(listener: Listener, message: String) {
        if (finished) return
        finished = true
        timeout?.cancel()
        heartbeat?.cancel()
        socket?.cancel()
        socket = null
        mainHandler.post { listener.onError(message) }
    }

    private fun friendlyError(code: String): String = when (code) {
        "invalid_code" -> "配对码不正确"
        "too_many_attempts" -> "配对码尝试次数过多，请在电脑上重新生成"
        "invalid_pair_token", "pair_expired" -> "配对码已过期，请重新生成"
        "host_offline" -> "电脑当前不在线"
        "not_authenticated", "invalid_signature" -> "设备身份认证失败"
        "device_key_changed" -> "本机身份密钥与服务器记录冲突"
        "self_pair" -> "不能和本机自己配对"
        "not_paired" -> "两台设备尚未配对"
        "pair_not_pending" -> "配对请求已失效"
        else -> "配对失败：${code.ifBlank { "unknown_error" }}"
    }
}

object PairingHostVerifier {
    fun check(
        hostId: String,
        expectedHostId: String?,
        actualFingerprint: String,
        expectedFingerprint: String?,
    ): String? {
        if (!expectedHostId.isNullOrBlank() && expectedHostId != hostId) {
            return "二维码中的电脑 ID 与服务器返回的不一致"
        }
        if (!expectedFingerprint.isNullOrBlank() &&
            !actualFingerprint.equals(expectedFingerprint.trim(), ignoreCase = true)
        ) {
            return "电脑指纹不一致，已中止配对"
        }
        return null
    }
}

private fun HomingEnvelope.toJson(): JSONObject = JSONObject()
    .put("v", v)
    .put("t", t)
    .put("id", id)
    .put("from", from)
    .put("to", to)
    .put("ts", ts)
    .put("nonce", nonce)
    .put("ct", ct)
