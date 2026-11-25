package dev.agentpush.poc

import android.app.Application
import androidx.lifecycle.ViewModelStore
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.After
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.shadows.ShadowLooper
import java.util.concurrent.atomic.AtomicBoolean

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class)
@OptIn(ExperimentalCoroutinesApi::class)
class ReconnectedSessionTest {
    private val mainDispatcher = StandardTestDispatcher()
    @Before fun bindMainDispatcher() {
        resetTestAgentStore()
        Dispatchers.setMain(mainDispatcher)
    }
    @After fun resetMainDispatcher() {
        resetTestAgentStore()
        Dispatchers.resetMain()
    }

    @Test fun reconnectLoadsMessagesMissedWhileTheConversationStayedOpen() = checkReconnect(false)
    @Test fun quickReconnectStillCatchesUpWhenTheIntermediateStateIsConflated() = checkReconnect(true)

    private fun checkReconnect(quick: Boolean): Unit = runBlocking {
        val calls = java.util.concurrent.CopyOnWriteArrayList<String>()
        val afterReconnect = AtomicBoolean(false)
        val session = "demo-reconnect-session"
        val scope = "demo-reconnect-host"
        val server = MockWebServer()
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val text = if (afterReconnect.get()) "demo-after-reconnect" else "demo-before-reconnect"
                calls.add(request.requestUrl!!.encodedPath + ":" + text)
                val body = when (request.requestUrl!!.encodedPath) {
                    "/api/events" -> "[]"
                    "/api/sessions/$session" -> """{"session_id":"$session","source":"codex","status":"done","running":false}"""
                    "/api/sessions/$session/items" -> """{"history_version":1,"source":"codex","session_id":"$session","anchor_event_id":17,"items":[{"id":"$text","item_key":"$text","type":"message","role":"assistant","text":"$text"}],"reason":null,"history_status":"available","before_cursor":"demo-before"}"""
                    else -> "{}"
                }
                return MockResponse().setBody(body)
            }
        }
        server.start()
        val app = ApplicationProvider.getApplicationContext<Application>()
        LocalCache(app).clear()
        val store = AgentStore.get(app)
        AgentApi.bindTransport(null)
        store.replaceStateForTest(AgentUiState(
            settings = AppSettings(server.url("/").toString().trimEnd('/'), "demo-token"),
            connection = ConnectionState.CONNECTED,
            activeComputerKey = scope,
            sessionMeta = mapOf(session to SessionMeta(sessionId = session, source = "codex", status = "done")),
            phoneConversations = PhoneConversationState(receipts = mapOf(
                phoneConversationKey(scope, "codex", session) to PhoneReceipt(17, "2026-11-24T12:00:00Z", 1L),
            )),
        ))
        val model = AgentViewModel(app)
        val lifecycle = ViewModelStore().also { it.put("demo", model) }
        suspend fun awaitText(expected: String): Boolean {
            repeat(250) {
                mainDispatcher.scheduler.runCurrent()
                ShadowLooper.idleMainLooper()
                if (store.state.value.sessionItems[session]?.items?.any { it.text == expected } == true) return true
                delay(20)
            }
            return false
        }
        try {
            model.openSession(session)
            assertTrue("initial conversation did not load", awaitText("demo-before-reconnect"))
            afterReconnect.set(true)
            if (quick) {
                // Network callbacks can return before the UI observes the
                // intermediate disconnected state.
                withContext(Dispatchers.Default) {
                    store.onConnectionLost("demo brief network outage")
                    store.onConnectionEstablished()
                }
            } else {
                store.onConnectionLost("demo network outage")
                mainDispatcher.scheduler.runCurrent()
                ShadowLooper.idleMainLooper()
                delay(30)
                store.onConnectionEstablished()
            }
            val refreshed = awaitText("demo-after-reconnect")
            assertTrue("reconnect did not refresh: calls=$calls; state=${store.state.value.connection}; error=${store.state.value.error}; items=${store.state.value.sessionItems[session]?.items?.map { it.text }}", refreshed)
        } finally {
            lifecycle.clear()
            mainDispatcher.scheduler.runCurrent()
            AgentApi.bindTransport(null)
            server.shutdown()
        }
    }
}
