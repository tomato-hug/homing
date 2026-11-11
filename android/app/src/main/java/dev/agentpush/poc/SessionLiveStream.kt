package dev.agentpush.poc

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.launch

class SessionLiveStream(
    private val sessionId: String,
    private val events: Flow<TransportEvent>,
    private val onFrame: (eventName: String, data: String) -> Unit,
    private val onConnection: (connected: Boolean, reason: String?) -> Unit = { _, _ -> },
) {
    private val scope = CoroutineScope(
        SupervisorJob() + Dispatchers.IO + CrashReporter.coroutineExceptionHandler,
    )
    private var job: Job? = null

    fun start() {
        if (job != null) return
        job = scope.launch {
            try {
                events.collect { event ->
                    onConnection(true, null)
                    onFrame(event.name, event.data.toString())
                }
                onConnection(false, "会话事件流已结束")
            } catch (error: Exception) {
                onConnection(
                    false,
                    error.message
                        ?.replace(Regex("\\s+"), " ")
                        ?.trim()
                        ?.take(120)
                        ?: error.javaClass.simpleName,
                )
            }
        }
    }

    fun stop() {
        job?.cancel()
        job = null
        scope.cancel()
    }

    override fun toString(): String = "SessionLiveStream($sessionId)"
}
