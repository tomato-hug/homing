package dev.agentpush.poc

import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import java.util.concurrent.TimeUnit

class EventPageApiTest {
    private lateinit var server: MockWebServer
    private lateinit var settings: AppSettings

    @Before fun start() {
        AgentApi.bindTransport(null)
        server = MockWebServer().also { it.start() }
        settings = AppSettings(server.url("/").toString().trimEnd('/'), "demo-page-token")
    }
    @After fun stop() { AgentApi.bindTransport(null); server.shutdown() }

    @Test fun boundedPageCarriesCursorAndContinuation() = runBlocking {
        server.enqueue(MockResponse().setBody("""{"events":[{"id":12,"source":"codex","session_id":"demo-page","text":"demo full text"}],"next_since":12,"has_more":true}"""))
        val page = AgentApi.fetchEventPage(settings, since = 9)
        val url = server.takeRequest(2, TimeUnit.SECONDS)!!.requestUrl!!
        assertEquals("9", url.queryParameter("since"))
        assertEquals("32", url.queryParameter("limit"))
        assertEquals(12L, page.nextSince)
        assertTrue(page.hasMore)
        assertEquals("demo full text", page.events.single().text)
    }

    @Test fun legacyArrayCompletesWithoutRetryLoop() = runBlocking {
        server.enqueue(MockResponse().setBody("""[{"id":14,"source":"codex","session_id":"demo-page"}]"""))
        val page = AgentApi.fetchEventPage(settings, since = 9)
        assertEquals(14L, page.nextSince)
        assertFalse(page.hasMore)
    }

    @Test fun invalidContinuationCannotSkipEventsOrSpin() = runBlocking {
        for (body in listOf(
            """{"events":[],"next_since":9,"has_more":true}""",
            """{"events":[{"id":12}],"next_since":20,"has_more":true}""",
        )) {
            server.enqueue(MockResponse().setBody(body))
            assertNotNull(runCatching { AgentApi.fetchEventPage(settings, since = 9) }.exceptionOrNull())
        }
    }
}
