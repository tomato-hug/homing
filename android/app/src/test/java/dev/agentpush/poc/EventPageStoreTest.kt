package dev.agentpush.poc

import android.app.Application
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import dev.agentpush.poc.protocol.PairedComputer
import dev.agentpush.poc.protocol.PairedComputerStore
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.cancel
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.After
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.json.JSONObject
import java.util.Collections

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class)
class EventPageStoreTest {
    private var createdStore: AgentStore? = null
    private fun freshStore(app: Application): AgentStore = AgentStore::class.java
        .getDeclaredConstructor(Context::class.java).apply { isAccessible = true }
        .newInstance(app).also { createdStore = it }

    @After fun unbind() {
        createdStore?.let { store ->
            (AgentStore::class.java.getDeclaredField("backgroundScope").apply { isAccessible = true }
                .get(store) as CoroutineScope).cancel()
        }
        AgentApi.bindTransport(null)
    }

    @Test fun interruptedCatchupResumesWithoutSkippingNewerLiveEvent() = runBlocking {
        val cursors = Collections.synchronizedList(mutableListOf<Long>())
        var failSecond = true
        val server = MockWebServer()
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                if (request.requestUrl!!.encodedPath != "/api/events") return MockResponse().setBody("{}")
                val since = request.requestUrl!!.queryParameter("since")!!.toLong()
                cursors.add(since)
                if (since == 2L && failSecond) return MockResponse().setResponseCode(503)
                val id = if (since == 0L) 2 else 4
                return MockResponse().setBody("""{"events":[{"id":$id,"source":"codex","session_id":"demo-page-store","text":"demo event $id"}],"next_since":$id,"has_more":${since == 0L}}""")
            }
        }
        server.start()
        try {
            val store = freshStore(ApplicationProvider.getApplicationContext<Application>())
            AgentApi.bindTransport(null)
            store.replaceStateForTest(AgentUiState(
                settings = AppSettings(server.url("/").toString().trimEnd('/'), "demo-token"),
                activeComputerKey = "demo-page-retry-host",
                events = mapOf(100L to AgentEvent.fromJson(JSONObject("""{"id":100,"source":"codex","session_id":"demo-live"}"""))),
            ))
            assertNotNull(runCatching { store.refreshAll() }.exceptionOrNull())
            assertTrue(store.state.value.events.keys.containsAll(listOf(2L, 100L)))
            failSecond = false
            store.refreshAll()
            assertEquals(listOf(0L, 2L, 2L), cursors.toList())
            assertTrue(store.state.value.events.keys.containsAll(listOf(2L, 4L, 100L)))
        } finally { server.shutdown() }
    }

    @Test fun preselectedComputerStillActivatesItsOwnState() {
        val app = ApplicationProvider.getApplicationContext<Application>()
        val paired = PairedComputerStore(app)
        val store = freshStore(app)
        store.replaceStateForTest(AgentUiState(settings = AppSettings("http://127.0.0.1:1", "demo-token"), activeComputerKey = "demo-old-host"))
        paired.save(PairedComputer("demo-new-host", "Demo host", "wss://relay.example.test/v1/ws", "demo-public", "demo-fingerprint", 1, 1))
        paired.select("demo-new-host") // The selection UI saves this before calling AgentStore.
        store.activateComputer("demo-new-host")
        assertEquals("demo-new-host", store.state.value.activeComputerKey)
        paired.remove("demo-new-host")
    }
}
