package dev.agentpush.poc

import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** Android's JSONObject.optString converts JSON null to the literal "null". */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class AndroidHistoryJsonTest {
    @Test fun nullHistoryReasonDoesNotBecomeVisibleErrorText() = runBlocking {
        AgentApi.bindTransport(null)
        val server = MockWebServer()
        server.enqueue(MockResponse().setBody("""{"history_version":1,"source":"codex","session_id":"demo-json-history","anchor_event_id":17,"items":[],"reason":null,"history_reason":null,"history_status":"available","before_cursor":"demo-before"}"""))
        server.start()
        try {
            val result = AgentApi.fetchSessionItems(
                AppSettings(server.url("/").toString().trimEnd('/'), "demo-token"),
                "demo-json-history", source = "codex", anchorEventId = 17,
            )
            assertNull(result.reason)
            assertNull(result.history?.reason)
        } finally { AgentApi.bindTransport(null); server.shutdown() }
    }
}
