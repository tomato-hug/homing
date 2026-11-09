package dev.agentpush.poc

import android.content.Context
import android.util.Log
import com.google.firebase.FirebaseApp
import com.google.firebase.FirebaseOptions
import com.google.firebase.messaging.FirebaseMessaging

/**
 * Optional FCM support. A build without Firebase credentials still works over
 * the LAN; the credentials are supplied by environment variables so no
 * google-services.json or API key is committed.
 */
object HomingFirebase {
    private const val TAG = "HomingFirebase"
    private const val PREFS = "homing_firebase"
    private const val KEY_TOKEN = "fcm_token"

    fun isConfigured(): Boolean {
        return BuildConfig.FIREBASE_PROJECT_ID.isNotBlank() &&
            BuildConfig.FIREBASE_APPLICATION_ID.isNotBlank() &&
            BuildConfig.FIREBASE_API_KEY.isNotBlank() &&
            BuildConfig.FIREBASE_SENDER_ID.isNotBlank()
    }

    fun initialize(context: Context): Boolean {
        if (!isConfigured()) return false
        val appContext = context.applicationContext
        if (FirebaseApp.getApps(appContext).isEmpty()) {
            val options = FirebaseOptions.Builder()
                .setProjectId(BuildConfig.FIREBASE_PROJECT_ID)
                .setApplicationId(BuildConfig.FIREBASE_APPLICATION_ID)
                .setApiKey(BuildConfig.FIREBASE_API_KEY)
                .setGcmSenderId(BuildConfig.FIREBASE_SENDER_ID)
                .build()
            runCatching { FirebaseApp.initializeApp(appContext, options) }
                .onFailure {
                    Log.w(TAG, "Unable to initialize Firebase", it)
                    return false
                }
        }
        requestToken(appContext) {}
        return true
    }

    fun requestToken(context: Context, onToken: (String) -> Unit) {
        if (!isConfigured()) return
        cachedToken(context)?.takeIf { it.isNotBlank() }?.let(onToken)
        runCatching {
            FirebaseMessaging.getInstance().token.addOnSuccessListener { token ->
                if (token.isBlank()) return@addOnSuccessListener
                saveToken(context, token)
                onToken(token)
            }
        }.onFailure { Log.w(TAG, "FCM token request failed", it) }
    }

    fun cachedToken(context: Context): String? {
        return context.applicationContext
            .getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .getString(KEY_TOKEN, null)
    }

    fun saveToken(context: Context, token: String) {
        if (token.isBlank()) return
        context.applicationContext
            .getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .edit()
            .putString(KEY_TOKEN, token)
            .apply()
    }
}
