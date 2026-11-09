package dev.agentpush.poc

import android.app.Notification
import android.app.Person
import androidx.annotation.RequiresApi

@RequiresApi(28)
internal object NotificationMessagingApi28 {
    fun build(
        conversationTitle: String,
        meName: String,
        agentName: String,
        messages: List<NotificationLine>,
    ): Notification.MessagingStyle {
        val me = Person.Builder()
            .setName(meName)
            .setImportant(true)
            .build()
        val agent = Person.Builder()
            .setName(agentName)
            .setImportant(true)
            .build()
        return Notification.MessagingStyle(me)
            .setConversationTitle(conversationTitle)
            .setGroupConversation(true)
            .apply {
                messages.forEach { message ->
                    addMessage(
                        message.text,
                        message.timestamp,
                        if (message.outgoing) me else agent,
                    )
                }
            }
    }
}
