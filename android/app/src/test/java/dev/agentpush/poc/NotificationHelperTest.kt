package dev.agentpush.poc

import android.app.NotificationManager
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28])
class NotificationHelperTest {
    @Test
    fun notifyPayloadUsesPriorityChannelAndOpensSession() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        NotificationHelper.createChannels(context)
        val manager = context.getSystemService(NotificationManager::class.java)
        val shadowManager = Shadows.shadowOf(manager)

        val expected = listOf(
            Triple("P0", "permission", NotificationHelper.WAITING_CHANNEL_ID),
            Triple("P1", "needs_input", NotificationHelper.WAITING_CHANNEL_ID),
            Triple("P1", "collab_result", NotificationHelper.COLLAB_CHANNEL_ID),
            Triple("P2", "done", NotificationHelper.DONE_CHANNEL_ID),
            Triple("P0", "error", NotificationHelper.ERROR_CHANNEL_ID),
        )
        expected.forEach { (level, kind, channelId) ->
            val sessionId = "demo-$level-$kind"
            val payload = NotifyPayload.fromJson(JSONObject()
                .put("type", "notify")
                .put("level", level)
                .put("kind", kind)
                .put("source", "codex")
                .put("session_id", sessionId)
                .put("title", "示例通知")
                .put("summary", "示例摘要")
                .put("at", "2026-11-09T08:00:00.000Z"))
            requireNotNull(payload)
            NotificationHelper.notifyAgentAlert(context, payload)

            val id = NotificationHelper.notificationId(payload.notificationKey)
            val notification = shadowManager.getNotification(id)
            requireNotNull(notification)
            assertEquals(channelId, notification.channelId)
            val savedIntent = Shadows.shadowOf(notification.contentIntent).savedIntent
            assertEquals(
                sessionId,
                savedIntent.getStringExtra(NotificationHelper.EXTRA_SESSION_ID),
            )
        }
        assertEquals(
            NotificationManager.IMPORTANCE_HIGH,
            manager.getNotificationChannel(NotificationHelper.P0_CHANNEL_ID).importance,
        )
        assertEquals(
            NotificationManager.IMPORTANCE_HIGH,
            manager.getNotificationChannel(NotificationHelper.WAITING_CHANNEL_ID).importance,
        )
        assertEquals(
            NotificationManager.IMPORTANCE_LOW,
            manager.getNotificationChannel(NotificationHelper.DONE_CHANNEL_ID).importance,
        )
    }
}
