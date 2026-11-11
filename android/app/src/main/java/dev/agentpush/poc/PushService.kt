package dev.agentpush.poc

import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.net.ConnectivityManager
import android.net.Network
import android.os.IBinder
import android.os.PowerManager
import android.os.SystemClock
import android.util.Log
import dev.agentpush.poc.protocol.PairedComputerStore
import org.json.JSONObject
import java.io.BufferedReader
import java.io.EOFException
import java.io.IOException
import java.io.InputStreamReader
import java.net.ConnectException
import java.net.HttpURLConnection
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import javax.net.ssl.SSLException

class PushService : Service() {
    private var worker: Thread? = null
    private val notifiedSessions = mutableSetOf<String>()

    @Volatile
    private var running = false

    @Volatile
    private var activeConnection: HttpURLConnection? = null

    @Volatile
    private var reconnectRequested = false

    @Volatile
    private var foregroundReady = false

    @Volatile
    private var forcedDisconnectReason: String? = null

    private var wakeLock: PowerManager.WakeLock? = null
    private var connectivityManager: ConnectivityManager? = null
    private var networkCallbackRegisteredAt = 0L

    private val store by lazy { AgentStore.get(this) }

    private val networkCallback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) {
            val callbackAge = SystemClock.elapsedRealtime() - networkCallbackRegisteredAt
            if (!running || callbackAge < NETWORK_CALLBACK_GRACE_MILLIS) {
                return
            }
            Log.i(TAG, "Network available, reconnecting now")
            forceReconnect(null)
            ensureCloudClient()
            store.invalidateTransportRoute()
            CloudLinkManager.get(this@PushService).reconnectNow()
        }

        override fun onLost(network: Network) {
            if (!running) {
                return
            }
            val reason = "网络已断开（ConnectivityManager）"
            Log.w(TAG, "Network lost, closing SSE connection")
            store.onConnectionLost(reason)
            store.invalidateTransportRoute()
            updateServiceNotification("等待网络 · $reason")
            forceReconnect(reason)
        }
    }

    override fun onCreate() {
        super.onCreate()
        NotificationHelper.createChannels(this)
        wakeLock = getSystemService(PowerManager::class.java)
            .newWakeLock(
                PowerManager.PARTIAL_WAKE_LOCK,
                "$packageName:agent-push-reconnect",
            )
            .apply { setReferenceCounted(false) }
        connectivityManager = getSystemService(ConnectivityManager::class.java)
        networkCallbackRegisteredAt = SystemClock.elapsedRealtime()
        runCatching {
            connectivityManager?.registerDefaultNetworkCallback(networkCallback)
        }.onFailure {
            Log.w(TAG, "Unable to register network callback", it)
        }
        runCatching {
            startForeground(
                NotificationHelper.SERVICE_NOTIFICATION_ID,
                NotificationHelper.buildServiceNotification(this, "正在连接 Mac"),
            )
        }.onSuccess {
            foregroundReady = true
        }.onFailure { error ->
            foregroundReady = false
            CrashReporter.record(error, origin = "push-service-start-foreground")
            Log.e(TAG, "startForeground failed", error)
            stopSelf()
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (!foregroundReady) {
            return START_NOT_STICKY
        }
        running = true
        store.markConnecting()
        ensureCloudClient()
        if (intent?.action == ACTION_RECONNECT) {
            reconnectRequested = true
            activeConnection?.disconnect()
            worker?.interrupt()
            store.invalidateTransportRoute()
            CloudLinkManager.get(this).reconnectNow()
        }
        updateServiceNotification(
            if (intent?.action == ACTION_RECONNECT) "正在重新连接 Mac" else "正在连接 Mac",
        )

        if (worker?.isAlive != true) {
            worker = Thread({ connectionLoop() }, "agent-push-sse").apply { start() }
        }
        return START_STICKY
    }

    override fun onTaskRemoved(rootIntent: Intent?) {
        Log.i(TAG, "Task removed; foreground service remains active")
        super.onTaskRemoved(rootIntent)
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onDestroy() {
        running = false
        reconnectRequested = false
        activeConnection?.disconnect()
        activeConnection = null
        worker?.interrupt()
        worker = null
        runCatching { connectivityManager?.unregisterNetworkCallback(networkCallback) }
        releaseWakeLock()
        if (store.state.value.connection == ConnectionState.CONNECTED) {
            store.onConnectionLost("服务已停止")
        }
        store.setConnectionState(ConnectionState.DISCONNECTED)
        stopForeground(STOP_FOREGROUND_REMOVE)
        super.onDestroy()
    }

    private fun connectionLoop() {
        var delayMillis = 1_000L
        while (running) {
            val settings = AppSettings.load(this)
            Thread.interrupted()
            if (!running) {
                break
            }
            reconnectRequested = false
            forcedDisconnectReason = null
            acquireWakeLock()
            try {
                if (store.state.value.transportKind == TransportKind.CLOUD) {
                    val cloud = CloudLinkManager.get(this)
                    cloud.ensureStarted()
                    if (cloud.connected.value) {
                        store.onConnectionEstablished()
                    } else {
                        store.markConnecting()
                    }
                    Thread.sleep(CLOUD_IDLE_POLL_MILLIS)
                    continue
                }
                if (settings.token.isBlank()) {
                    throw IOException("Token 不能为空")
                }
                readEventStream(settings)
            } catch (error: Exception) {
                if (!running) {
                    break
                }
                val reason = disconnectReason(error)
                Log.w(TAG, "SSE disconnected: $reason", error)
                store.onConnectionLost(reason)
                updateServiceNotification("重连中 · ${reason.take(90)}")
            } finally {
                releaseWakeLock()
            }

            if (!running) {
                break
            }
            if (reconnectRequested) {
                reconnectRequested = false
                delayMillis = 1_000L
                continue
            }
            try {
                Thread.sleep(delayMillis)
            } catch (_: InterruptedException) {
                if (!running) {
                    break
                }
                delayMillis = 1_000L
                continue
            }
            delayMillis = (delayMillis * 2).coerceAtMost(30_000L)
        }
    }

    private fun readEventStream(settings: AppSettings) {
        val connection = AgentApi.openStreamConnection(settings)
        activeConnection = connection

        try {
            val code = connection.responseCode
            if (code !in 200..299) {
                val detail = connection.errorStream
                    ?.bufferedReader(Charsets.UTF_8)
                    ?.use { it.readText() }
                    .orEmpty()
                throw AgentApiException(
                    code,
                    "HTTP $code ${detail.take(160)}".trim(),
                )
            }

            store.onConnectionEstablished()
            releaseWakeLock()
            updateServiceNotification("已连接 Mac")

            val reader = BufferedReader(InputStreamReader(connection.inputStream, Charsets.UTF_8))
            val dataLines = mutableListOf<String>()
            var eventName = ""

            fun dispatchSseEvent() {
                if (dataLines.isEmpty()) {
                    eventName = ""
                    return
                }
                val data = dataLines.joinToString("\n")
                val currentEventName = eventName
                dataLines.clear()
                eventName = ""
                handleFrame(currentEventName, data)
            }

            reader.use {
                while (running) {
                    val line = it.readLine() ?: break
                    when {
                        line.isEmpty() -> dispatchSseEvent()
                        line.startsWith(":") -> Unit
                        line.startsWith("event:") -> {
                            eventName = line.substringAfter("event:").trim()
                        }
                        line.startsWith("data:") -> {
                            dataLines.add(line.substringAfter("data:").removePrefix(" "))
                        }
                    }
                }
                dispatchSseEvent()
            }
            if (running) {
                throw EOFException("中继关闭了 SSE 连接")
            }
        } finally {
            if (activeConnection === connection) {
                activeConnection = null
            }
            connection.disconnect()
        }
    }

    private fun forceReconnect(reason: String?) {
        reconnectRequested = true
        if (reason != null) {
            forcedDisconnectReason = reason
        }
        activeConnection?.disconnect()
        worker?.interrupt()
    }

    private fun ensureCloudClient() {
        if (PairedComputerStore(this).load().isEmpty()) return
        CloudLinkManager.get(this).ensureStarted()
    }

    private fun disconnectReason(error: Throwable): String {
        forcedDisconnectReason?.let { forced ->
            forcedDisconnectReason = null
            return forced
        }
        val type = when (error) {
            is SocketTimeoutException -> "SocketTimeoutException"
            is UnknownHostException -> "UnknownHostException"
            is ConnectException -> "ConnectException"
            is AgentApiException -> "AgentApiException"
            is EOFException -> "EOFException"
            is SSLException -> "SSLException"
            is IOException -> "IOException"
            else -> error.javaClass.simpleName
        }
        val message = error.message
            ?.replace(Regex("\\s+"), " ")
            ?.trim()
            ?.take(120)
        return if (message.isNullOrBlank()) type else "$type: $message"
    }

    private fun acquireWakeLock() {
        val lock = wakeLock ?: return
        runCatching {
            if (!lock.isHeld) {
                lock.acquire(WAKE_LOCK_TIMEOUT_MILLIS)
                Log.d(TAG, "Partial wake lock acquired for reconnect")
            }
        }.onFailure {
            Log.w(TAG, "Unable to acquire wake lock", it)
        }
    }

    private fun releaseWakeLock() {
        val lock = wakeLock ?: return
        runCatching {
            if (lock.isHeld) {
                lock.release()
                Log.d(TAG, "Partial wake lock released")
            }
        }
    }

    private fun handleFrame(eventName: String, data: String) {
        val json = runCatching { JSONObject(data) }.getOrNull() ?: return
        if (eventName == "notify") {
            NotifyPayload.fromJson(json)?.let { payload ->
                runCatching { NotificationHelper.notifyAgentAlert(this, payload) }
                    .onFailure { Log.w(TAG, "Unable to post notify event", it) }
            }
            return
        }
        if (eventName in setOf("session", "item", "turn", "dispatch")) {
            store.applyLiveEvent(eventName, json)
            return
        }
        if (eventName == "reply_status") {
            store.applyLiveEvent(eventName, json)
            val sessionId = json.optString("session_id")
            if (sessionId in notifiedSessions) {
                store.session(sessionId)?.let { session ->
                    if (session.muted || session.archived) return
                    val status = ReplyStatus.fromWire(json.optString("status"))
                    NotificationHelper.notifySession(
                        this,
                        session,
                        "回复状态：${status.label}",
                    )
                }
            }
            return
        }
        if (eventName.isNotEmpty() && eventName != "message") {
            return
        }

        val event = AgentEvent.fromJson(json)
        if (event.id <= 0) {
            return
        }
        val isNew = store.ingestEvent(event)
        if (!isNew || !shouldNotify(event.id)) {
            return
        }
        val session = store.session(event.sessionKey) ?: return
        if (session.muted || session.archived) return
        if (!ReminderPreferences.load(this).allows(event.kind)) return
        notifiedSessions += session.sessionId
        runCatching {
            NotificationHelper.notifySession(this, session)
        }.onFailure {
            Log.w(TAG, "Unable to post notification", it)
        }
    }

    private fun shouldNotify(eventId: Long): Boolean {
        val preferences = getSharedPreferences(NOTIFIED_PREFS, MODE_PRIVATE)
        val notified = preferences.getStringSet(KEY_NOTIFIED_IDS, emptySet())
            ?.toMutableSet()
            ?: mutableSetOf()
        if (!notified.add(eventId.toString())) {
            return false
        }
        val trimmed = notified
            .sortedBy { it.toLongOrNull() ?: 0L }
            .takeLast(500)
            .toSet()
        preferences.edit().putStringSet(KEY_NOTIFIED_IDS, trimmed).apply()
        return true
    }

    private fun updateServiceNotification(status: String) {
        val manager = getSystemService(NotificationManager::class.java)
        manager.notify(
            NotificationHelper.SERVICE_NOTIFICATION_ID,
            NotificationHelper.buildServiceNotification(this, status),
        )
    }

    companion object {
        const val ACTION_RECONNECT = "dev.agentpush.poc.action.RECONNECT"
        private const val TAG = "AgentPushService"
        private const val NOTIFIED_PREFS = "agent_push_notified"
        private const val KEY_NOTIFIED_IDS = "notified_ids"
        private const val WAKE_LOCK_TIMEOUT_MILLIS = 90_000L
        private const val CLOUD_IDLE_POLL_MILLIS = 2_000L
        private const val NETWORK_CALLBACK_GRACE_MILLIS = 1_500L

        fun requestReconnect(context: Context) {
            val intent = Intent(context, PushService::class.java).apply {
                action = ACTION_RECONNECT
            }
            runCatching { context.startForegroundService(intent) }
                .onFailure { Log.w(TAG, "Unable to request reconnect", it) }
        }
    }
}
