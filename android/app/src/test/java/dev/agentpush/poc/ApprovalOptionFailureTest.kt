package dev.agentpush.poc

import android.app.Application
import androidx.lifecycle.ViewModelStore
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.Dispatchers
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
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
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
class ApprovalOptionFailureTest {
    private val mainDispatcher = StandardTestDispatcher()
    @Before fun bindMainDispatcher() {
        resetTestAgentStore()
        Dispatchers.setMain(mainDispatcher)
    }
    @After fun resetMainDispatcher() {
        resetTestAgentStore()
        Dispatchers.resetMain()
    }

    @Test fun expiredStructuredApprovalDoesNotSendAnOrdinaryReply() = checkFailure(404)
    @Test fun unavailableStructuredApprovalDoesNotSendAnOrdinaryReply() = checkFailure(503)

    private fun checkFailure(status: Int): Unit = runBlocking {
        val fallbackSent = AtomicBoolean(false)
        val server = MockWebServer()
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse =
                if (request.requestUrl!!.encodedPath == "/api/approvals/17") {
                    MockResponse().setResponseCode(status)
                        .setBody("""{"ok":false,"error":"not_pending","message":"demo approval is no longer pending"}""")
                } else MockResponse().setBody("{}")
        }
        server.start()
        val app = ApplicationProvider.getApplicationContext<Application>()
        val store = AgentStore.get(app)
        AgentApi.bindTransport(null)
        store.replaceStateForTest(AgentUiState(settings = AppSettings(
            server.url("/").toString().trimEnd('/'), "demo-token",
        )))
        val model = AgentViewModel(app)
        val lifecycle = ViewModelStore().also { it.put("demo-approval", model) }
        try {
            model.decideApproval(17, "allow", { fallbackSent.set(true) }, "allow_once")
            for (attempt in 0 until 250) {
                mainDispatcher.scheduler.runCurrent()
                ShadowLooper.idleMainLooper()
                if (fallbackSent.get() || model.snackbar.value != null) break
                delay(20)
            }
            assertFalse("failed approval was converted into an ordinary chat reply", fallbackSent.get())
            assertNotNull("the failed approval must remain visible to the user", model.snackbar.value)
        } finally {
            lifecycle.clear()
            mainDispatcher.scheduler.runCurrent()
            AgentApi.bindTransport(null)
            server.shutdown()
        }
    }
}
