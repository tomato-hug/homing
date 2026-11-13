package dev.agentpush.poc

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.onStart
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

    @Test
    fun lostLanMutationReceiptNeverReplaysOnCloud() = runBlocking {
        for (method in listOf("POST", "PUT", "PATCH", "DELETE")) {
            val lan = FakeTransport(TransportKind.LAN).apply { failRequests = true }
            val cloud = FakeTransport(TransportKind.CLOUD)
            val router = TransportRouter(lan, cloud, { true }, { true })

            repeat(2) {
                val error = runCatching { router.request(method, "/api/demo") }.exceptionOrNull()
                assertTrue("$method must report the uncertain result", error is IOException)
            }

            assertEquals(2, lan.requestCount)
            assertEquals("$method must not be replayed", 0, cloud.requestCount)
            assertEquals(TransportKind.CLOUD, router.kind.value)
            router.request(method, "/api/demo")
            assertEquals("a new explicit request can use cloud", 1, cloud.requestCount)
        }
    }

    @Test
    fun lostCloudMutationReceiptNeverReplaysOnLan() = runBlocking {
        val lan = FakeTransport(TransportKind.LAN)
        val cloud = FakeTransport(TransportKind.CLOUD).apply { failRequests = true }
        var lanAvailable = false
        val router = TransportRouter(lan, cloud, { lanAvailable }, { true })
        router.ensureRoute()
        lanAvailable = true

        val error = runCatching { router.request("POST", "/api/demo") }.exceptionOrNull()

        assertTrue(error is IOException)
        assertEquals(0, lan.requestCount)
        assertEquals(1, cloud.requestCount)
    }

    @Test
    fun cancellationAndApplicationErrorsDoNotSwitchRoutes() = runBlocking {
        for (error in listOf(CancellationException("demo cancellation"),
            AgentApiException(403, "demo forbidden"), IllegalArgumentException("demo input"))) {
            val lan = FakeTransport(TransportKind.LAN).apply { requestError = error }
            val cloud = FakeTransport(TransportKind.CLOUD)
            val router = TransportRouter(lan, cloud, { true }, { true })
            repeat(2) {
                assertTrue(runCatching { router.request("GET", "/api/demo") }.exceptionOrNull() === error)
            }
            assertEquals(0, cloud.requestCount)
            assertEquals(TransportKind.LAN, router.kind.value)
        }
    }

    @Test
    fun cancelledCloudRequestDoesNotRetryOnLan() = runBlocking {
        val cancelled = CancellationException("demo cancellation")
        val lan = FakeTransport(TransportKind.LAN)
        val cloud = FakeTransport(TransportKind.CLOUD).apply { requestError = cancelled }
        var lanAvailable = false
        val router = TransportRouter(lan, cloud, { lanAvailable }, { true })
        router.ensureRoute()
        lanAvailable = true

        assertTrue(runCatching { router.request("GET", "/api/demo") }.exceptionOrNull() === cancelled)
        assertEquals(0, lan.requestCount)
        assertEquals(TransportKind.CLOUD, router.kind.value)
    }

    @Test
    fun eventStreamSwitchesToCloudWithoutAnotherRequest() = runBlocking {
        val lanStarted = CompletableDeferred<Unit>()
        val lan = FakeTransport(TransportKind.LAN).apply { onEventsStarted = { lanStarted.complete(Unit) } }
        val cloud = FakeTransport(TransportKind.CLOUD)
        var lanAvailable = true
        val router = TransportRouter(lan, cloud, { lanAvailable }, { true })
        cloud.eventsFlow.tryEmit(TransportEvent("item", JSONObject().put("session_id", "demo-session")))
        val result = async { withTimeout(5_000) { router.events().first() } }
        lanStarted.await()
        lanAvailable = false

        assertEquals("demo-session", result.await().data.getString("session_id"))
        assertEquals(TransportKind.CLOUD, router.kind.value)
        assertEquals(0, cloud.requestCount)
    }

    private class FakeTransport(
        override val kind: TransportKind,
    ) : HomingTransport {
        val eventsFlow = MutableSharedFlow<TransportEvent>(
            replay = 1,
            extraBufferCapacity = 8,
        )
        var failRequests = false
        var requestError: Exception? = null
        var onEventsStarted: () -> Unit = {}
        var requestCount = 0

        override suspend fun request(
            method: String,
            path: String,
            body: JSONObject?,
        ): TransportResponse {
            requestCount += 1
            requestError?.let { throw it }
            if (failRequests) throw IOException("demo network failure")
            return TransportResponse(
                status = 200,
                body = JSONObject().put("ok", true),
                raw = """{"ok":true}""",
            )
        }

        override fun events(sessionId: String?): Flow<TransportEvent> = eventsFlow.onStart { onEventsStarted() }

        override suspend fun upload(
            path: String,
            bytes: ByteArray,
            name: String,
        ): JSONObject = JSONObject().put("ok", true)

        override suspend fun download(url: String): ByteArray = ByteArray(0)
    }
}
