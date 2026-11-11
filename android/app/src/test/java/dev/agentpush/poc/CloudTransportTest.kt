package dev.agentpush.poc

import dev.agentpush.poc.protocol.PairedComputer
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.take
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.runBlocking
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Test

class CloudTransportTest {
    @Test
    fun eventsFollowTheActiveComputerWithoutDroppingLegacyFrames(): Unit = runBlocking {
        val link = FakeCloudLink()
        val transport = CloudTransport(link)
        val events = mutableListOf<TransportEvent>()
        val collection = async(start = CoroutineStart.UNDISPATCHED) {
            transport.events().take(3).toList(events)
        }

        link.active = computer("computer-a")
        link.events.emit(event("a-1", "computer-a", "demo-session"))
        link.events.emit(event("b-1", "computer-b", "demo-session"))
        link.events.emit(event("a-2", "computer-a", "demo-session"))
        link.events.emit(event("legacy", "", "demo-session"))

        assertEquals(listOf("a-1", "a-2", "legacy"), collection.await().map { it.id })
        assertEquals(listOf("a-1", "a-2", "legacy"), events.map { it.id })
    }

    @Test
    fun switchingActiveComputerFiltersTheSameSessionIdBySourceComputer(): Unit = runBlocking {
        val link = FakeCloudLink()
        val transport = CloudTransport(link)

        link.active = computer("computer-a")
        link.events.emit(event("a-session", "computer-a", "shared-session"))

        link.active = computer("computer-b")
        val next = async(start = CoroutineStart.UNDISPATCHED) {
            transport.events("shared-session").take(1).toList()
        }
        link.events.emit(event("a-session-again", "computer-a", "shared-session"))
        link.events.emit(event("b-session", "computer-b", "shared-session"))

        assertEquals("b-session", next.await().single().id)
    }

    private fun computer(id: String): PairedComputer = PairedComputer(
        deviceId = id,
        name = id,
        server = "wss://relay.example/v1/ws",
        publicKeyBase64 = "demo-public-key",
        fingerprint = "demo-fingerprint",
        pairedAt = 1L,
        lastSeen = 1L,
    )

    private fun event(id: String, computerId: String, sessionId: String): TransportEvent =
        TransportEvent(
            id = id,
            name = "item",
            data = JSONObject()
                .put("session_id", sessionId)
                .put("item", JSONObject().put("id", id)),
            computerId = computerId,
        )
}

private class FakeCloudLink : CloudLink {
    override val connected: StateFlow<Boolean> = MutableStateFlow(true)
    val events = MutableSharedFlow<TransportEvent>(extraBufferCapacity = 16)
    var active: PairedComputer? = null

    override fun ensureStarted() = Unit

    override fun preferredComputer(): PairedComputer? = active

    override suspend fun awaitConnected(timeoutMillis: Long): Boolean = true

    override fun events(): Flow<TransportEvent> = events

    override suspend fun request(
        computer: PairedComputer,
        method: String,
        path: String,
        body: JSONObject?,
        timeoutMillis: Long,
    ): CloudRpcResult = error("not used")

    override suspend fun uploadFile(
        computer: PairedComputer,
        path: String,
        bytes: ByteArray,
        contentType: String,
    ): CloudRpcResult = error("not used")

    override suspend fun downloadFile(computer: PairedComputer, path: String): ByteArray =
        error("not used")
}
