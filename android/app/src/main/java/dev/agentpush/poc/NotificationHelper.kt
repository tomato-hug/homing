package dev.agentpush.poc

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.RemoteInput
import android.content.Context
import android.content.Intent
import android.graphics.Color
import android.graphics.drawable.Icon
import android.os.Build
import java.time.Instant

object NotificationHelper {
    const val ACTION_REPLY = "dev.agentpush.poc.action.REPLY"
    const val ACTION_READ = "dev.agentpush.poc.action.READ"
    const val ACTION_FORWARD = "dev.agentpush.poc.action.FORWARD"
    const val EXTRA_SESSION_ID = "session_id"
    const val EXTRA_EVENT_ID = "event_id"
    const val EXTRA_NOTIFICATION_ID = "notification_id"
    const val EXTRA_TITLE = "event_title"
    const val EXTRA_UNREAD_IDS = "unread_ids"
    const val EXTRA_DISPATCH_ID = "dispatch_id"
    const val REMOTE_INPUT_KEY = "reply_text"

    const val SERVICE_CHANNEL_ID = "agent_connection"
    const val REPLY_CHANNEL_ID = "agent_replies"
    const val P0_CHANNEL_ID = "homing_p0"
    const val P1_CHANNEL_ID = "homing_p1"
    const val P2_CHANNEL_ID = "homing_p2"
    const val WAITING_CHANNEL_ID = "homing_waiting"
    const val COLLAB_CHANNEL_ID = "homing_collab"
    const val DONE_CHANNEL_ID = "homing_done"
    const val ERROR_CHANNEL_ID = "homing_error"
    const val SERVICE_NOTIFICATION_ID = 1

    fun createChannels(context: Context) {
        val manager = context.getSystemService(NotificationManager::class.java)
        val replyChannel = NotificationChannel(
            REPLY_CHANNEL_ID,
            "Homing 提醒",
            NotificationManager.IMPORTANCE_HIGH,
        ).apply {
            description = "Homing 按会话合并的 agent 回复、授权与等待输入提醒"
            enableVibration(true)
            vibrationPattern = longArrayOf(0, 160, 90, 160)
            lockscreenVisibility = Notification.VISIBILITY_PRIVATE
        }
        val serviceChannel = NotificationChannel(
            SERVICE_CHANNEL_ID,
            "Homing 连接",
            NotificationManager.IMPORTANCE_LOW,
        ).apply {
            description = "保持 Homing 与 Mac 中继服务的实时连接"
            setShowBadge(false)
        }
        val p0Channel = NotificationChannel(
            P0_CHANNEL_ID,
            "Homing 需要处理",
            NotificationManager.IMPORTANCE_HIGH,
        ).apply {
            description = "需要授权或执行失败的提醒"
            enableVibration(true)
            vibrationPattern = longArrayOf(0, 180, 100, 180)
            lockscreenVisibility = Notification.VISIBILITY_PRIVATE
        }
        val p1Channel = NotificationChannel(
            P1_CHANNEL_ID,
            "Homing 等待回复",
            NotificationManager.IMPORTANCE_DEFAULT,
        ).apply {
            description = "agent 正在等待你输入"
            enableVibration(true)
            lockscreenVisibility = Notification.VISIBILITY_PRIVATE
        }
        val p2Channel = NotificationChannel(
            P2_CHANNEL_ID,
            "Homing 已完成",
            NotificationManager.IMPORTANCE_LOW,
        ).apply {
            description = "任务完成摘要，不打断当前操作"
            setShowBadge(true)
            lockscreenVisibility = Notification.VISIBILITY_PRIVATE
        }
        val waitingChannel = NotificationChannel(
            WAITING_CHANNEL_ID,
            "Homing · 等你處理",
            NotificationManager.IMPORTANCE_HIGH,
        ).apply {
            description = "授權、提問與需要你決定的任務"
            enableVibration(true)
            vibrationPattern = longArrayOf(0, 180, 100, 180)
            lockscreenVisibility = Notification.VISIBILITY_PRIVATE
        }
        val collabChannel = NotificationChannel(
            COLLAB_CHANNEL_ID,
            "Homing · 協作結果",
            NotificationManager.IMPORTANCE_DEFAULT,
        ).apply {
            description = "跨 Agent 子任務完成後的結論"
            enableVibration(true)
            lockscreenVisibility = Notification.VISIBILITY_PRIVATE
        }
        val doneChannel = NotificationChannel(
            DONE_CHANNEL_ID,
            "Homing · 已完成",
            NotificationManager.IMPORTANCE_LOW,
        ).apply {
            description = "任務完成摘要，不打断当前操作"
            setShowBadge(true)
            lockscreenVisibility = Notification.VISIBILITY_PRIVATE
        }
        val errorChannel = NotificationChannel(
            ERROR_CHANNEL_ID,
            "Homing · 運行異常",
            NotificationManager.IMPORTANCE_HIGH,
        ).apply {
            description = "Agent 執行失敗或中斷"
            enableVibration(true)
            vibrationPattern = longArrayOf(0, 220, 120, 220)
            lockscreenVisibility = Notification.VISIBILITY_PRIVATE
        }
        manager.createNotificationChannels(
            listOf(
                replyChannel,
                serviceChannel,
                p0Channel,
                p1Channel,
                p2Channel,
                waitingChannel,
                collabChannel,
                doneChannel,
                errorChannel,
            ),
        )
    }

    fun channelIdForLevel(level: String): String = when (level.uppercase()) {
        "P0" -> P0_CHANNEL_ID
        "P2" -> P2_CHANNEL_ID
        else -> P1_CHANNEL_ID
    }

    fun channelIdForPayload(payload: NotifyPayload): String {
        return when (payload.kind.lowercase()) {
            "permission", "needs_input", "question", "awaiting_input", "approval" ->
                WAITING_CHANNEL_ID
            "collab_result" -> COLLAB_CHANNEL_ID
            "error", "failed" -> ERROR_CHANNEL_ID
            "done", "completed", "turn_complete", "reply", "stop" -> DONE_CHANNEL_ID
            else -> channelIdForLevel(payload.level)
        }
    }

    fun notifyAgentAlert(context: Context, payload: NotifyPayload) {
        val notificationId = notificationId(payload.notificationKey)
        val brand = payload.brand
        val title = if (payload.level == "P2" && payload.merged) {
            "Homing · ${payload.title}"
        } else if (brand == AgentBrand.UNKNOWN) {
            payload.title
        } else {
            "${brand.displayName} · ${payload.title}"
        }
        val body = payload.summary.ifBlank { payload.title }
        val openIntent = Intent(context, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP
            payload.targetSessionId?.let { putExtra(EXTRA_SESSION_ID, it) }
        }
        val openPendingIntent = PendingIntent.getActivity(
            context,
            notificationId,
            openIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val builder = Notification.Builder(
            context,
            channelIdForPayload(payload),
        )
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(title)
            .setContentText(body)
            .setStyle(Notification.BigTextStyle().bigText(body))
            .setSubText("Homing")
            .setContentIntent(openPendingIntent)
            .setAutoCancel(true)
            .setCategory(Notification.CATEGORY_MESSAGE)
            .setVisibility(Notification.VISIBILITY_PRIVATE)
            .setColor(notificationColor(brand))
            .setShowWhen(true)
            .setWhen(parseMillis(payload.at))
        if (payload.level != "P2") {
            builder.setPriority(Notification.PRIORITY_HIGH)
        }
        if (payload.kind == "collab_result" && payload.dispatchId.isNotBlank()) {
            val forwardIntent = Intent(context, ReplyActionReceiver::class.java).apply {
                action = ACTION_FORWARD
                putExtra(EXTRA_SESSION_ID, payload.sessionId)
                putExtra(EXTRA_DISPATCH_ID, payload.dispatchId)
                putExtra(EXTRA_NOTIFICATION_ID, notificationId)
                putExtra(EXTRA_TITLE, title)
            }
            val forwardPendingIntent = PendingIntent.getBroadcast(
                context,
                notificationId xor 0x20000000,
                forwardIntent,
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
            )
            builder.addAction(
                Notification.Action.Builder(
                    Icon.createWithResource(context, android.R.drawable.ic_menu_share),
                    "轉交",
                    forwardPendingIntent,
                ).build(),
            )
        }
        context.getSystemService(NotificationManager::class.java)
            .notify(notificationId, builder.build())
    }

    fun buildServiceNotification(context: Context, status: String): Notification {
        val openIntent = Intent(context, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP
        }
        val openPendingIntent = PendingIntent.getActivity(
            context,
            1,
            openIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        return Notification.Builder(context, SERVICE_CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle("Homing")
            .setContentText(status)
            .setContentIntent(openPendingIntent)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setCategory(Notification.CATEGORY_SERVICE)
            .setColor(Color.rgb(45, 212, 191))
            .build()
    }

    fun notifySession(
        context: Context,
        session: SessionSummary,
        statusLine: String? = null,
    ) {
        val manager = context.getSystemService(NotificationManager::class.java)
        val notificationId = notificationId(session.sessionId)
        val title = "${session.brand.displayName} · ${session.project}"
        val messages = buildList<NotificationLine> {
            session.events.forEach { event ->
                val label = when (event.kind) {
                    "permission" -> "【需要审批】"
                    "needs_input" -> "【等待输入】"
                    else -> ""
                }
                add(
                    NotificationLine(
                        text = "$label${event.text.ifBlank { event.title }}" +
                            if (event.attachments.isNotEmpty()) {
                                "  [附件 ${event.attachments.size} 个]"
                            } else {
                                ""
                            },
                        timestamp = parseMillis(event.ts),
                        sender = session.brand.displayName,
                        outgoing = false,
                    ),
                )
            }
            session.replies.forEach { reply ->
                add(
                    NotificationLine(
                        text = reply.text,
                        timestamp = parseMillis(reply.ts),
                        sender = "我",
                        outgoing = true,
                    ),
                )
            }
        }.sortedBy { it.timestamp }.takeLast(6)

        val displayMessages = if (statusLine == null) {
            messages
        } else {
            messages + NotificationLine(
                text = statusLine,
                timestamp = System.currentTimeMillis(),
                sender = "我",
                outgoing = true,
            )
        }
        val style: Notification.Style = if (Build.VERSION.SDK_INT >= 28) {
            NotificationMessagingApi28.build(
                conversationTitle = title,
                meName = "我",
                agentName = session.brand.displayName,
                messages = displayMessages,
            )
        } else {
            @Suppress("DEPRECATION")
            Notification.MessagingStyle("我")
                .setConversationTitle(title)
                .apply {
                    displayMessages.forEach { message ->
                        addMessage(message.text, message.timestamp, message.sender)
                    }
                }
        }

        val openIntent = Intent(context, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP
            putExtra(EXTRA_SESSION_ID, session.sessionId)
        }
        val openPendingIntent = PendingIntent.getActivity(
            context,
            notificationId,
            openIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )

        val replyIntent = Intent(context, ReplyActionReceiver::class.java).apply {
            action = ACTION_REPLY
            putExtra(EXTRA_SESSION_ID, session.sessionId)
            putExtra(
                EXTRA_EVENT_ID,
                session.latestEvent?.id ?: session.events.lastOrNull()?.id ?: 0L,
            )
            putExtra(EXTRA_TITLE, title)
            putExtra(EXTRA_NOTIFICATION_ID, notificationId)
        }
        val replyPendingIntent = PendingIntent.getBroadcast(
            context,
            notificationId,
            replyIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_MUTABLE,
        )
        val remoteInput = RemoteInput.Builder(REMOTE_INPUT_KEY)
            .setLabel("输入回复")
            .build()
        val replyAction = Notification.Action.Builder(
            Icon.createWithResource(context, android.R.drawable.ic_menu_send),
            "回复",
            replyPendingIntent,
        )
            .addRemoteInput(remoteInput)
            .setAllowGeneratedReplies(true)
            .build()

        val unreadIds = session.events.filter { !it.read }.map { it.id }.toLongArray()
        val readIntent = Intent(context, ReplyActionReceiver::class.java).apply {
            action = ACTION_READ
            putExtra(EXTRA_SESSION_ID, session.sessionId)
            putExtra(EXTRA_NOTIFICATION_ID, notificationId)
            putExtra(EXTRA_UNREAD_IDS, unreadIds)
        }
        val readPendingIntent = PendingIntent.getBroadcast(
            context,
            notificationId xor 0x40000000,
            readIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val readAction = Notification.Action.Builder(
            Icon.createWithResource(context, android.R.drawable.checkbox_on_background),
            "已读",
            readPendingIntent,
        ).build()

        val latest = displayMessages.lastOrNull()
        val builder = Notification.Builder(context, REPLY_CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(title)
            .setContentText(latest?.text ?: session.title)
            .setSubText(statusLine ?: session.brand.displayName)
            .setStyle(style)
            .setContentIntent(openPendingIntent)
            .setAutoCancel(true)
            .setCategory(Notification.CATEGORY_MESSAGE)
            .setColor(notificationColor(session.brand))
            .setShowWhen(true)
            .setWhen(latest?.timestamp ?: System.currentTimeMillis())
            .addAction(replyAction)
            .addAction(readAction)

        manager.notify(notificationId, builder.build())
    }

    fun updateReplyFallback(
        context: Context,
        notificationId: Int,
        sessionId: String,
        title: String,
        success: Boolean,
        message: String?,
    ) {
        val manager = context.getSystemService(NotificationManager::class.java)
        val status = if (success) {
            "已发送${message?.let { " · $it" }.orEmpty()}"
        } else {
            "发送失败：${message.orEmpty()}"
        }
        val openIntent = Intent(context, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP
            putExtra(EXTRA_SESSION_ID, sessionId)
        }
        val openPendingIntent = PendingIntent.getActivity(
            context,
            notificationId,
            openIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val notification = Notification.Builder(context, REPLY_CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(title)
            .setContentText(status)
            .setStyle(Notification.BigTextStyle().bigText(status))
            .setContentIntent(openPendingIntent)
            .setAutoCancel(true)
            .setCategory(Notification.CATEGORY_MESSAGE)
            .setColor(Color.rgb(45, 212, 191))
            .build()
        manager.notify(notificationId, notification)
    }

    fun cancelSession(context: Context, sessionId: String) {
        context.getSystemService(NotificationManager::class.java)
            .cancel(notificationId(sessionId))
    }

    fun notificationId(sessionId: String): Int {
        return (sessionId.hashCode() and 0x7fffffff) + 1_000
    }

    private fun parseMillis(timestamp: String): Long {
        return runCatching { Instant.parse(timestamp).toEpochMilli() }
            .getOrDefault(System.currentTimeMillis())
    }

    private fun notificationColor(brand: AgentBrand): Int {
        return when (brand) {
            AgentBrand.CODEX -> Color.rgb(34, 181, 115)
            AgentBrand.CLAUDE -> Color.rgb(232, 137, 37)
            AgentBrand.WORKBUDDY -> Color.rgb(59, 130, 246)
            AgentBrand.UNKNOWN -> Color.rgb(37, 99, 235)
        }
    }
}

internal data class NotificationLine(
    val text: String,
    val timestamp: Long,
    val sender: String,
    val outgoing: Boolean,
)
