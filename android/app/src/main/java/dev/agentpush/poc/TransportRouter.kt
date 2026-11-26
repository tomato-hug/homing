package dev.agentpush.poc

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.channelFlow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import org.json.JSONObject
import java.io.IOException

class TransportRouter(
    private val lan: HomingTransport,
    private val cloud: HomingTransport,
    private val probeLan: suspend () -> Boolean,
    private val cloudAvailable: () -> Boolean,
    private val clock: () -> Long = System::currentTimeMillis,
    private val canUseLan: () -> Boolean = { true },
) {
    private val routeMutex = Mutex()
    private val failureLock = Any()
    private val _kind = MutableStateFlow(TransportKind.LAN)
    private var consecutiveLanFailures = 0
    private var routeInitialized = false
    private var lastProbeAt = 0L
    private var lanProbeBackoffMillis = INITIAL_LAN_PROBE_BACKOFF_MILLIS
    private var nextLanProbeAllowedAt = 0L

    @Volatile
    private var routeDirty = false

    val kind: StateFlow<TransportKind> = _kind.asStateFlow()

    suspend fun ensureRoute(force: Boolean = false) {
        routeMutex.withLock {
            val now = clock()
            val lanPermitted = runCatching { canUseLan() }.getOrDefault(true)
            if (!lanPermitted) {
                _kind.value = if (cloudAvailable()) TransportKind.CLOUD else TransportKind.LAN
                routeInitialized = true
                lastProbeAt = now
                routeDirty = false
                return
            }
            if (!force && !routeDirty && routeInitialized) {
                if (_kind.value == TransportKind.CLOUD && now < nextLanProbeAllowedAt) {
                    return
                }
                if (_kind.value == TransportKind.LAN && now - lastProbeAt < ROUTE_CACHE_MILLIS) {
                    return
                }
            }
            val lanAvailable = probeLanSafely()
            _kind.value = when {
                lanAvailable -> TransportKind.LAN
                cloudAvailable() -> TransportKind.CLOUD
                else -> TransportKind.LAN
            }
            routeInitialized = true
            lastProbeAt = now
            routeDirty = false
            if (lanAvailable) {
                clearLanFailures()
                lanProbeBackoffMillis = INITIAL_LAN_PROBE_BACKOFF_MILLIS
                nextLanProbeAllowedAt = 0L
            } else {
                nextLanProbeAllowedAt = now + lanProbeBackoffMillis
                lanProbeBackoffMillis = nextLanBackoff(lanProbeBackoffMillis)
            }
        }
    }

    suspend fun request(
        method: String,
        path: String,
        body: JSONObject? = null,
    ): TransportResponse {
        ensureRoute()
        val route = _kind.value
        if (route == TransportKind.CLOUD) {
            return requestCloud(method, path, body)
        }
        return try {
            lan.request(method, path, body).also { clearLanFailures() }
        } catch (error: Exception) {
            if (error !is IOException || error is AgentApiException) throw error
            if (recordLanFailure() && canReplayTransportRequest(method)) {
                requestCloud(method, path, body)
            } else {
                throw error
            }
        }
    }

    @OptIn(ExperimentalCoroutinesApi::class)
    fun events(sessionId: String? = null): Flow<TransportEvent> = channelFlow {
        ensureRoute()
        val monitor = launch {
            while (isActive) {
                val onLan = _kind.value == TransportKind.LAN
                delay(if (onLan) ROUTE_CACHE_MILLIS else CLOUD_MONITOR_INTERVAL_MILLIS)
                ensureRoute(force = onLan)
            }
        }
        try {
            kind.flatMapLatest { current ->
                when (current) {
                    TransportKind.LAN -> lan.events(sessionId)
                    TransportKind.CLOUD -> cloud.events(sessionId)
                }
            }.collect { send(it) }
        } finally {
            monitor.cancel()
        }
    }

    suspend fun upload(
        path: String,
        bytes: ByteArray,
        name: String,
    ): JSONObject {
        ensureRoute()
        val current = _kind.value
        if (current == TransportKind.CLOUD) {
            if (bytes.size > CLOUD_FILE_LIMIT_BYTES) {
                throw CloudRpcException("单文件上限 20 MB")
            }
            return cloud.upload(path, bytes, name)
        }
        return lan.upload(path, bytes, name)
    }

    suspend fun download(url: String): ByteArray {
        ensureRoute()
        return when (_kind.value) {
            TransportKind.LAN -> lan.download(url)
            TransportKind.CLOUD -> cloud.download(url)
        }
    }

    fun invalidate() {
        routeDirty = true
        lanProbeBackoffMillis = INITIAL_LAN_PROBE_BACKOFF_MILLIS
        nextLanProbeAllowedAt = 0L
    }

    private suspend fun requestCloud(
        method: String,
        path: String,
        body: JSONObject?,
    ): TransportResponse {
        return try {
            cloud.request(method, path, body)
        } catch (error: Exception) {
            if (error is CancellationException ||
                error !is IOException && error !is CloudRpcException ||
                error is AgentApiException
            ) throw error
            val lanPermitted = runCatching { canUseLan() }.getOrDefault(true)
            if (lanPermitted && probeLanSafely()) {
                _kind.value = TransportKind.LAN
                routeInitialized = true
                lastProbeAt = clock()
                lanProbeBackoffMillis = INITIAL_LAN_PROBE_BACKOFF_MILLIS
                nextLanProbeAllowedAt = 0L
                clearLanFailures()
                // A lost response does not prove a mutation failed to execute.
                if (canReplayTransportRequest(method)) lan.request(method, path, body) else throw error
            } else {
                throw error
            }
        }
    }

    private suspend fun probeLanSafely(): Boolean = try {
        probeLan()
    } catch (error: CancellationException) {
        throw error
    } catch (_: Exception) {
        false
    }

    private fun recordLanFailure(): Boolean {
        synchronized(failureLock) {
            consecutiveLanFailures += 1
            if (consecutiveLanFailures >= LAN_FAILURES_BEFORE_SWITCH && cloudAvailable()) {
                _kind.value = TransportKind.CLOUD
                nextLanProbeAllowedAt = clock() + lanProbeBackoffMillis
                lanProbeBackoffMillis = nextLanBackoff(lanProbeBackoffMillis)
                return true
            }
            return false
        }
    }

    private fun clearLanFailures() {
        synchronized(failureLock) {
            consecutiveLanFailures = 0
        }
    }

    private fun nextLanBackoff(current: Long): Long {
        return when {
            current < 10_000L -> 10_000L
            current < 30_000L -> 30_000L
            current < 60_000L -> 60_000L
            else -> MAX_LAN_PROBE_BACKOFF_MILLIS
        }
    }

    companion object {
        private const val ROUTE_CACHE_MILLIS = 3_000L
        private const val CLOUD_MONITOR_INTERVAL_MILLIS = 30_000L
        private const val INITIAL_LAN_PROBE_BACKOFF_MILLIS = 3_000L
        private const val MAX_LAN_PROBE_BACKOFF_MILLIS = 5 * 60_000L
        private const val LAN_FAILURES_BEFORE_SWITCH = 2
        private const val CLOUD_FILE_LIMIT_BYTES = 20 * 1024 * 1024
    }
}
