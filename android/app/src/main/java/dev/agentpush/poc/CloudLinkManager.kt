package dev.agentpush.poc

import android.content.Context
import android.util.Log
import dev.agentpush.poc.protocol.DeviceIdentityStore
import dev.agentpush.poc.protocol.PairedComputer
import dev.agentpush.poc.protocol.PairedComputerStore
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withTimeoutOrNull
import org.json.JSONObject
import java.util.concurrent.ConcurrentHashMap

class CloudLinkManager private constructor(context: Context) : CloudLink {
    private val appContext = context.applicationContext
    private val computers = PairedComputerStore(appContext)
    private val lock = Any()
    private val serverStates = ConcurrentHashMap<String, CloudNotificationClient.State>()
    private val serverDetails = ConcurrentHashMap<String, String>()
    private val _connected = MutableStateFlow(false)
    private val _statusDetail = MutableStateFlow<String?>(null)
    private val _events = MutableSharedFlow<TransportEvent>(
        extraBufferCapacity = 128,
        onBufferOverflow = BufferOverflow.DROP_OLDEST,
    )
    private var client: CloudNotificationClient? = null
    private var starting = false

    @Volatile
    private var notificationHandler:
        ((PairedComputer, NotifyPayload) -> Boolean)? = null

    override val connected: StateFlow<Boolean> = _connected.asStateFlow()
    override val statusDetail: StateFlow<String?> = _statusDetail.asStateFlow()

    fun setNotificationHandler(
        handler: (PairedComputer, NotifyPayload) -> Boolean,
    ) {
        notificationHandler = handler
    }

    override fun ensureStarted() {
        if (computers.load().isEmpty()) return
        synchronized(lock) {
            if (client != null || starting) return
            starting = true
        }
        Thread({
            try {
                val identity = DeviceIdentityStore(appContext).loadOrCreate()
                val created = CloudNotificationClient(
                    identity = identity,
                    computersProvider = {
                        val all = computers.load()
                        if (isLocalLanNetworkAvailable(appContext)) {
                            all
                        } else {
                            // On cellular data (no Wi-Fi/Ethernet), skip RFC1918 LAN WebSocket
                            // candidates so OkHttp never spins 10s timeouts against 192.168.x.x.
                            all.map { peer ->
                                if (peer.server.isNotBlank() && !isPrivateLanAddressUrl(peer.server)) {
                                    peer.copy(lanServers = emptyList())
                                } else {
                                    peer
                                }
                            }
                        }
                    },
                    listener = object : CloudNotificationClient.Listener {
                        override fun onNotification(
                            computer: PairedComputer,
                            payload: NotifyPayload,
                        ): Boolean {
                            return notificationHandler?.invoke(computer, payload) ?: true
                        }

                        override fun onEvent(
                            computer: PairedComputer,
                            name: String,
                            data: JSONObject,
                        ): Boolean {
                            _events.tryEmit(
                                TransportEvent(
                                    name = name,
                                    data = data,
                                    computerId = computer.deviceId,
                                ),
                            )
                            return true
                        }

                        override fun onStateChanged(
                            server: String,
                            state: CloudNotificationClient.State,
                            detail: String?,
                        ) {
                            if (state == CloudNotificationClient.State.STOPPED) {
                                serverStates.remove(server)
                                serverDetails.remove(server)
                            } else {
                                serverStates[server] = state
                                if (state == CloudNotificationClient.State.CONNECTED && detail.isNullOrBlank()) {
                                    serverDetails.remove(server)
                                } else if (!detail.isNullOrBlank()) {
                                    serverDetails[server] = detail
                                }
                            }
                            val anyConnected = serverStates.values.any {
                                it == CloudNotificationClient.State.CONNECTED
                            }
                            _connected.value = anyConnected
                            _statusDetail.value = resolveStatusDetail(anyConnected)
                            Log.i(
                                TAG,
                                "Cloud transport $server: $state" +
                                    detail?.let { " · $it" }.orEmpty(),
                            )
                        }
                    },
                    pushTokenProvider = { HomingFirebase.cachedToken(appContext) },
                )
                synchronized(lock) {
                    client = created
                }
                created.start()
                HomingFirebase.requestToken(appContext) {
                    created.updatePushRegistration()
                }
            } catch (error: Throwable) {
                CrashReporter.record(error, origin = "cloud-link-manager")
                Log.e(TAG, "Unable to start cloud transport", error)
            } finally {
                synchronized(lock) {
                    starting = false
                }
            }
        }, "homing-cloud-link").apply {
            isDaemon = true
            start()
        }
    }

    fun refreshComputers() {
        ensureStarted()
        client?.refreshComputers()
    }

    fun reconnectNow() {
        ensureStarted()
        client?.reconnectNow()
    }

    override fun preferredComputer(): PairedComputer? {
        return computers.active()
            ?.takeIf { it.deviceId.isNotBlank() && it.server.isNotBlank() }
    }

    fun selectComputer(deviceId: String) {
        computers.select(deviceId)
    }

    override suspend fun awaitConnected(timeoutMillis: Long): Boolean {
        ensureStarted()
        return withTimeoutOrNull(timeoutMillis) {
            connected.filter { it }.first()
            true
        } ?: false
    }

    override suspend fun request(
        computer: PairedComputer,
        method: String,
        path: String,
        body: JSONObject?,
        timeoutMillis: Long,
    ): CloudRpcResult {
        ensureStarted()
        val active = synchronized(lock) { client }
            ?: throw CloudRpcException("云端连接尚未建立")
        return active.requestRPC(
            computer = computer,
            method = method,
            path = path,
            body = body,
            timeoutMillis = timeoutMillis,
        )
    }

    override suspend fun uploadFile(
        computer: PairedComputer,
        path: String,
        bytes: ByteArray,
        contentType: String,
    ): CloudRpcResult {
        ensureStarted()
        val active = synchronized(lock) { client }
            ?: throw CloudRpcException("云端连接尚未建立")
        return active.uploadFile(
            computer = computer,
            path = path,
            bytes = bytes,
            contentType = contentType,
        )
    }

    override suspend fun downloadFile(
        computer: PairedComputer,
        path: String,
    ): ByteArray {
        ensureStarted()
        val active = synchronized(lock) { client }
            ?: throw CloudRpcException("云端连接尚未建立")
        return active.downloadFile(computer, path)
    }

    override fun events(): Flow<TransportEvent> = _events.asSharedFlow()

    fun stop() {
        val active = synchronized(lock) {
            val current = client
            client = null
            current
        }
        active?.stop()
        serverStates.clear()
        serverDetails.clear()
        _connected.value = false
        _statusDetail.value = null
    }

    private fun resolveStatusDetail(anyConnected: Boolean): String? {
        serverStates.entries.firstOrNull { it.value == CloudNotificationClient.State.AUTH_REQUIRED }
            ?.let { entry -> serverDetails[entry.key]?.takeIf { it.isNotBlank() } }
            ?.let { return it }
        if (anyConnected) {
            return serverStates.entries.firstOrNull { it.value == CloudNotificationClient.State.CONNECTED }
                ?.let { entry -> serverDetails[entry.key]?.takeIf { it.isNotBlank() } }
        }
        return serverDetails.values.firstOrNull { it.isNotBlank() }
    }

    companion object {
        private const val TAG = "HomingCloudLink"

        @Volatile
        private var instance: CloudLinkManager? = null

        fun get(context: Context): CloudLinkManager {
            return instance ?: synchronized(this) {
                instance ?: CloudLinkManager(context).also { instance = it }
            }
        }
    }
}

internal fun isPrivateLanAddressUrl(rawUrl: String): Boolean {
    val host = runCatching { java.net.URI(rawUrl.trim()).host.orEmpty().lowercase() }
        .getOrDefault("")
        .trim('[', ']')
    if (host.isBlank()) return false
    if (host == "localhost" || host == "127.0.0.1" || host == "::1" || host.endsWith(".local")) {
        return true
    }
    if (host.startsWith("192.168.") || host.startsWith("10.") || host.startsWith("169.254.")) {
        return true
    }
    if (host.startsWith("172.")) {
        val second = host.removePrefix("172.").substringBefore('.').toIntOrNull()
        if (second != null && second in 16..31) return true
    }
    return false
}
