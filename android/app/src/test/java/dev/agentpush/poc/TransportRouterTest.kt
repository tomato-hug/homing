package dev.agentpush.poc

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.IOException

class TransportRouterTest {
    @Test
    fun usesCloudWhenLanProbeFails() = runBlocking {
        val lan = FakeTransport(TransportKind.LAN)
        val cloud = FakeTransport(TransportKind.CLOUD)
        val router = TransportRouter(
            lan = lan,
            cloud = cloud,
            probeLan = { false },
            cloudAvailable = { true },
        )

        router.ensureRoute()

        assertEquals(TransportKind.CLOUD, router.kind.value)
    }

    @Test
    fun switchesAfterTwoConsecutiveLanFailures() = runBlocking {
        val lan = FakeTransport(TransportKind.LAN).apply {
            failRequests = true
        }
        val cloud = FakeTransport(TransportKind.CLOUD)
        val router = TransportRouter(
            lan = lan,
            cloud = cloud,
            probeLan = { true },
            cloudAvailable = { true },
        )

        val firstError = runCatching {
            router.request("GET", "/api/agents")
        }.exceptionOrNull()
        assertTrue(firstError is IOException)
        assertEquals(TransportKind.LAN, router.kind.value)

        val response = router.request("GET", "/api/agents")

        assertEquals(200, response.status)
        assertEquals(TransportKind.CLOUD, router.kind.value)
        assertEquals(1, cloud.requestCount)
    }

    @Test
    fun eventsFollowSelectedTransport() = runBlocking {
        val lan = FakeTransport(TransportKind.LAN)
        val cloud = FakeTransport(TransportKind.CLOUD)
        val router = TransportRouter(
            lan = lan,
            cloud = cloud,
            probeLan = { false },
            cloudAvailable = { true },
        )
        cloud.eventsFlow.tryEmit(
            TransportEvent("item", JSONObject().put("session_id", "demo-session")),
        )

        val event = withTimeout(1_000) {
            router.events("demo-session").first()
        }

        assertEquals("item", event.name)
        assertEquals("demo-session", event.data.getString("session_id"))
    }

    private class FakeTransport(
        override val kind: TransportKind,
    ) : HomingTransport {
        val eventsFlow = MutableSharedFlow<TransportEvent>(
            replay = 1,
            extraBufferCapacity = 8,
        )
        var failRequests = false
        var requestCount = 0

        override suspend fun request(
            method: String,
            path: String,
            body: JSONObject?,
        ): TransportResponse {
            requestCount += 1
            if (failRequests) throw IOException("demo network failure")
            return TransportResponse(
                status = 200,
                body = JSONObject().put("ok", true),
                raw = """{"ok":true}""",
            )
        }

        override fun events(sessionId: String?): Flow<TransportEvent> = eventsFlow

        override suspend fun upload(
            path: String,
            bytes: ByteArray,
            name: String,
        ): JSONObject = JSONObject().put("ok", true)

        override suspend fun download(url: String): ByteArray = ByteArray(0)
    }
}
