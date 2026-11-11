package dev.agentpush.poc

import android.app.Application
import android.provider.Settings
import androidx.test.core.app.ApplicationProvider
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.time.Instant

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class)
class ReplyReconciliationTest {
    private lateinit var app: Application

    @Before
    fun setUp() {
        app = ApplicationProvider.getApplicationContext()
        Settings.Global.putFloat(
            app.contentResolver,
            Settings.Global.ANIMATOR_DURATION_SCALE,
            0f,
        )
    }

    @Test
    fun staleQueuedReceiptDoesNotDowngradeDeliveredReply() {
        val delivered = reply(id = 20, status = ReplyStatus.INJECTED, message = "已交给队列")
        val stale = reply(id = 20, status = ReplyStatus.QUEUED, message = "排队中")

        val merged = reconcileIncomingReply(delivered, stale)

        assertEquals(ReplyStatus.INJECTED, merged.status)
        assertEquals("已交给队列", merged.message)
    }

    @Test
    fun deliveredStatusStillUpdatesQueuedReply() {
        val queued = reply(id = 21, status = ReplyStatus.QUEUED)
        val delivered = reply(id = 21, status = ReplyStatus.INJECTED, message = "已注入")

        val merged = reconcileIncomingReply(queued, delivered)

        assertEquals(ReplyStatus.INJECTED, merged.status)
        assertEquals("已注入", merged.message)
    }

    @Test
    fun sentWithModelWarningIsNotTreatedAsQueued() {
        assertEquals(
            ReplyStatus.INJECTED,
            ReplyStatus.fromWire("switch_model_unsupported"),
        )
    }

    @Test
    fun statusEventMovesOptimisticReplyToServerId() {
        val store = AgentStore.get(app)
        val local = reply(id = -7, status = ReplyStatus.QUEUED, isLocal = true)
        store.replaceStateForTest(
            AgentUiState(
                settings = AppSettings("http://127.0.0.1:1", ""),
                replies = mapOf(local.id to local),
            ),
        )

        store.applyReplyStatus(
            JSONObject()
                .put("reply_id", 42)
                .put("event_id", local.eventId)
                .put("session_id", local.sessionId)
                .put("status", "injected")
                .put("message", "已注入"),
        )

        val updated = store.state.value.replies[42]
        assertNotNull(updated)
        assertEquals(ReplyStatus.INJECTED, updated?.status)
        assertFalse(store.state.value.replies.containsKey(local.id))
    }

    private fun reply(
        id: Long,
        status: ReplyStatus,
        message: String? = null,
        isLocal: Boolean = false,
    ): AgentReply {
        return AgentReply(
            id = id,
            eventId = 9,
            sessionId = "demo-session",
            text = "demo reply",
            ts = Instant.parse("2026-11-11T00:00:00Z").toString(),
            status = status,
            message = message,
            isLocal = isLocal,
        )
    }
}
