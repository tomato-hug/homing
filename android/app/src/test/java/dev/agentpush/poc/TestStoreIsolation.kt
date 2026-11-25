package dev.agentpush.poc

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.cancel

/** ViewModel tests own their store, including its background collectors. */
internal fun resetTestAgentStore() {
    val singleton = AgentStore::class.java.getDeclaredField("instance").apply { isAccessible = true }
    (singleton.get(null) as? AgentStore)?.let { previous ->
        val scope = AgentStore::class.java.getDeclaredField("backgroundScope").apply { isAccessible = true }
        (scope.get(previous) as CoroutineScope).cancel()
    }
    singleton.set(null, null)
    AgentApi.bindTransport(null)
}
