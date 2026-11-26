package dev.agentpush.poc

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.os.UserManager
import android.util.Log
import androidx.core.content.ContextCompat
import dev.agentpush.poc.protocol.PairedComputerStore

/**
 * Resumes Homing's E2E relay / LAN foreground push service automatically after
 * device reboot or app upgrade when a paired computer or token is configured.
 */
class KeepAliveBootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent?) {
        val action = intent?.action.orEmpty()
        if (
            action != Intent.ACTION_BOOT_COMPLETED &&
            action != Intent.ACTION_MY_PACKAGE_REPLACED &&
            action != Intent.ACTION_LOCKED_BOOT_COMPLETED
        ) {
            return
        }
        val userManager = context.getSystemService(UserManager::class.java)
        if (userManager != null && !userManager.isUserUnlocked) {
            return
        }
        val hasTarget = runCatching {
            PairedComputerStore(context).load().isNotEmpty() ||
                AppSettings.load(context).token.isNotBlank()
        }.getOrDefault(false)
        if (!hasTarget) {
            return
        }
        runCatching {
            ContextCompat.startForegroundService(
                context,
                Intent(context, PushService::class.java),
            )
        }.onFailure { err ->
            Log.w("KeepAliveBootReceiver", "Deferred PushService start on $action", err)
        }
    }
}
