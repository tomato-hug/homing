package dev.agentpush.poc

import android.app.RemoteInput
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log
import kotlinx.coroutines.runBlocking
import java.util.concurrent.Executors

class ReplyActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val action = intent.action ?: return
        if (action != NotificationHelper.ACTION_REPLY &&
            action != NotificationHelper.ACTION_READ &&
            action != NotificationHelper.ACTION_FORWARD &&
            action != NotificationHelper.ACTION_APPROVE &&
            action != NotificationHelper.ACTION_DENY
        ) {
            return
        }

        val pendingResult = goAsync()
        val applicationContext = context.applicationContext
        val store = AgentStore.get(applicationContext)
        val sessionId = intent.getStringExtra(NotificationHelper.EXTRA_SESSION_ID).orEmpty()
        val notificationId = intent.getIntExtra(
            NotificationHelper.EXTRA_NOTIFICATION_ID,
            NotificationHelper.notificationId(sessionId),
        )
        val title = intent.getStringExtra(NotificationHelper.EXTRA_TITLE)
            ?: "Homing 回复"

        val eventId = intent.getLongExtra(
            NotificationHelper.EXTRA_EVENT_ID,
            store.session(sessionId)?.replyEventId ?: 0L,
        )
        val unreadIds = intent.getLongArrayExtra(NotificationHelper.EXTRA_UNREAD_IDS)
            ?.toList()
            .orEmpty()

        executor.execute {
            try {
                runBlocking {
                    if (action == NotificationHelper.ACTION_APPROVE ||
                        action == NotificationHelper.ACTION_DENY
                    ) {
                        val decision = if (action == NotificationHelper.ACTION_APPROVE) "allow" else "deny"
                        val ok = if (eventId > 0) {
                            runCatching {
                                AgentApi.decideApproval(
                                    store.state.value.settings,
                                    eventId,
                                    decision,
                                ).ok
                            }.getOrDefault(false)
                        } else {
                            false
                        }
                        if (ok) {
                            runCatching { store.refreshAll() }
                        }
                        NotificationHelper.updateReplyFallback(
                            applicationContext,
                            notificationId,
                            sessionId,
                            title,
                            ok,
                            if (ok) {
                                if (decision == "allow") "已允许" else "已拒绝"
                            } else {
                                "处理授权失败，请打开应用查看"
                            },
                        )
                    } else if (action == NotificationHelper.ACTION_FORWARD) {
                        val dispatchId = intent.getStringExtra(
                            NotificationHelper.EXTRA_DISPATCH_ID,
                        ).orEmpty()
                        if (dispatchId.isBlank()) return@runBlocking
                        val result = store.forwardDispatch(dispatchId)
                        NotificationHelper.updateReplyFallback(
                            applicationContext,
                            notificationId,
                            sessionId,
                            title,
                            result.ok,
                            result.message ?: if (result.ok) "已转交给父 Agent" else "转交失败",
                        )
                    } else if (action == NotificationHelper.ACTION_READ) {
                        val ids = unreadIds.ifEmpty {
                            store.session(sessionId)?.events
                                ?.filter { !it.read }
                                ?.map { it.id }
                                .orEmpty()
                        }
                        if (ids.isNotEmpty()) {
                            store.markEventsRead(ids, sessionId)
                        }
                        NotificationHelper.cancelSession(applicationContext, sessionId)
                    } else {
                        val remoteInput = RemoteInput.getResultsFromIntent(intent)
                        val replyText = remoteInput
                            ?.getCharSequence(NotificationHelper.REMOTE_INPUT_KEY)
                            ?.toString()
                            ?.trim()
                            .orEmpty()
                        if (replyText.isEmpty()) {
                            NotificationHelper.updateReplyFallback(
                                applicationContext,
                                notificationId,
                                sessionId,
                                title,
                                false,
                                "回复内容为空",
                            )
                            return@runBlocking
                        }

                        val result = store.sendReply(
                            sessionId = sessionId,
                            eventId = eventId.takeIf { it > 0 },
                            source = store.session(sessionId)?.brand?.wire.orEmpty(),
                            text = replyText,
                        )
                        val session = store.session(sessionId)
                        val statusLine = if (result.status == ReplyStatus.FAILED) {
                            "发送失败：${result.message ?: "请重试"}"
                        } else {
                            "已发送 · ${result.status.label}"
                        }
                        if (session != null) {
                            NotificationHelper.notifySession(
                                applicationContext,
                                session,
                                statusLine,
                            )
                        } else {
                            NotificationHelper.updateReplyFallback(
                                applicationContext,
                                notificationId,
                                sessionId,
                                title,
                                result.status != ReplyStatus.FAILED,
                                result.message ?: result.status.label,
                            )
                        }
                    }
                }
            } catch (error: Exception) {
                Log.e("AgentReplyReceiver", "Notification action failed", error)
            } finally {
                pendingResult.finish()
            }
        }
    }

    companion object {
        private val executor = Executors.newSingleThreadExecutor()
    }
}
