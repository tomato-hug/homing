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

        val expected = mapOf(
            "P0" to NotificationHelper.P0_CHANNEL_ID,
            "P1" to NotificationHelper.P1_CHANNEL_ID,
            "P2" to NotificationHelper.P2_CHANNEL_ID,
        )
        expected.forEach { (level, channelId) ->
            val sessionId = "demo-$level"
            val payload = NotifyPayload.fromJson(JSONObject()
                .put("type", "notify")
                .put("level", level)
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
            NotificationManager.IMPORTANCE_DEFAULT,
            manager.getNotificationChannel(NotificationHelper.P1_CHANNEL_ID).importance,
        )
        assertEquals(
            NotificationManager.IMPORTANCE_LOW,
            manager.getNotificationChannel(NotificationHelper.P2_CHANNEL_ID).importance,
        )
    }
}
