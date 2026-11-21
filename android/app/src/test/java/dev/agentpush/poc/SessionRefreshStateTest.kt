package dev.agentpush.poc

import android.app.Application
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class)
class SessionRefreshStateTest {
    @After
    fun resetTransport() {
        AgentApi.bindTransport(null)
    }

    @Test
    fun completedSessionWithoutTurnHistoryClearsCachedRunningState() {
        val store = refresh(""""status":"done","running":false""")

        val items = store.state.value.sessionItems.getValue("demo-refresh-session")
        assertFalse(items.running)
        assertEquals("", items.runningSince)
        assertEquals(SessionStatus.COMPLETED, store.session("demo-refresh-session")?.status)
        assertEquals("", store.session("demo-refresh-session")?.runningSince)
    }

    @Test
    fun completedMetadataStillClearsRunningWhenItemsAreUnavailable() {
        val store = refresh(""""status":"done","running":false""", itemsAvailable = false)

        val items = store.state.value.sessionItems.getValue("demo-refresh-session")
        assertFalse(items.available)
        assertFalse(items.running)
        assertEquals("", items.runningSince)
        assertEquals(SessionStatus.COMPLETED, store.session("demo-refresh-session")?.status)
    }

    @Test
    fun missingRunningMetadataPreservesCachedLiveState() {
        val store = refresh(""""status":"running"""")

        assertTrue(store.state.value.sessionItems.getValue("demo-refresh-session").running)
    }

    @Test
    fun newerTurnHistoryCanStartAnotherTurnAfterCompletedMetadata() {
        val store = refresh(
            """"status":"done","running":false""",
            turns = """[{"state":"started","ts":"2026-11-21T11:00:00Z"}]""",
        )

        val items = store.state.value.sessionItems.getValue("demo-refresh-session")
        assertTrue(items.running)
        assertEquals("2026-11-21T11:00:00Z", items.runningSince)
    }

    @Test
    fun idleLiveTurnClearsRunningWithoutClaimingCompletionAndCanResume() {
        val store = refresh(""""status":"running","running":true""")

        store.applyTurn(JSONObject("""{"session_id":"demo-refresh-session","state":"idle","ts":"2026-11-21T11:01:00Z"}"""))

        assertFalse(store.state.value.sessionItems.getValue("demo-refresh-session").running)
        assertEquals("", store.state.value.sessionItems.getValue("demo-refresh-session").runningSince)
        assertEquals(SessionStatus.IDLE, store.session("demo-refresh-session")?.status)

        store.applyTurn(JSONObject("""{"session_id":"demo-refresh-session","state":"running","ts":"2026-11-21T11:02:00Z"}"""))
        assertTrue(store.state.value.sessionItems.getValue("demo-refresh-session").running)
        assertEquals(SessionStatus.RUNNING, store.session("demo-refresh-session")?.status)
    }

    @Test
    fun idleLiveTurnPreservesKnownCompletedStatus() {
        val store = refresh(""""status":"done","running":false""")
        store.applyTurn(JSONObject("""{"session_id":"demo-refresh-session","state":"idle","ts":"2026-11-21T11:01:00Z"}"""))
        assertEquals(SessionStatus.COMPLETED, store.session("demo-refresh-session")?.status)
    }

    @Test
    fun runningLiveTurnHistoryIsRecognizedOnRefresh() {
        val store = refresh(
            """"status":"running","running":true""",
            turns = """[{"state":"running","ts":"2026-11-21T11:00:00Z"}]""",
        )
        assertTrue(store.state.value.sessionItems.getValue("demo-refresh-session").running)
    }

    private fun refresh(
        metadata: String,
        turns: String = "[]",
        itemsAvailable: Boolean = true,
    ): AgentStore {
        val server = MockWebServer()
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val path = request.requestUrl!!.encodedPath
                val body = when (path) {
                    "/api/sessions/demo-refresh-session" ->
                        """{"session_id":"demo-refresh-session","source":"hermes",$metadata}"""
                    "/api/sessions/demo-refresh-session/items" -> {
                        if (!itemsAvailable) return MockResponse().setResponseCode(503)
                        """{"history_version":1,"anchor_event_id":99,"source":"hermes","session_id":"demo-refresh-session","items":[{"id":"demo-final","item_key":"demo-final-key","type":"message","role":"assistant","text":"demo completed"}],"turns":$turns}"""
                    }
                    else -> "{}"
                }
                return MockResponse().setHeader("Content-Type", "application/json").setBody(body)
            }
        }
        server.start()
        try {
            val app = ApplicationProvider.getApplicationContext<Application>()
            val store = AgentStore.get(app)
            AgentApi.bindTransport(null)
            store.replaceStateForTest(
                AgentUiState(
                    settings = AppSettings(server.url("/").toString().trimEnd('/'), "demo-token"),
                    phoneConversations = PhoneConversationState(receipts = mapOf(
                        phoneConversationKey("", "hermes", "demo-refresh-session") to
                            PhoneReceipt(99, "2026-11-21T09:00:00Z", 1L),
                    )),
                    sessionMeta = mapOf(
                        "demo-refresh-session" to SessionMeta(
                            sessionId = "demo-refresh-session",
                            source = "hermes",
                            status = "running",
                            running = true,
                            runningSince = "2026-11-21T10:00:00Z",
                        ),
                    ),
                    sessionItems = mapOf(
                        "demo-refresh-session" to SessionItemsState(
                            running = true,
                            runningSince = "2026-11-21T10:00:00Z",
                        ),
                    ),
                ),
            )
            runBlocking { store.refreshSessionDetails("demo-refresh-session", force = true) }
            return store
        } finally {
            server.shutdown()
        }
    }
}
