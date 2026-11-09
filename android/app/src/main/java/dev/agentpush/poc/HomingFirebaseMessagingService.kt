package dev.agentpush.poc

import android.util.Log
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage

/**
 * Receives content-free wake-ups. The notification body is fetched over the
 * encrypted protocol connection, never from the FCM payload.
 */
class HomingFirebaseMessagingService : FirebaseMessagingService() {
    override fun onNewToken(token: String) {
        HomingFirebase.saveToken(this, token)
        PushService.requestReconnect(this)
    }

    override fun onMessageReceived(message: RemoteMessage) {
        if (message.data["type"] != "wake") {
            Log.w(TAG, "Ignoring FCM message without type=wake")
            return
        }
        PushService.requestReconnect(this)
    }

    companion object {
        private const val TAG = "HomingFCM"
    }
}
