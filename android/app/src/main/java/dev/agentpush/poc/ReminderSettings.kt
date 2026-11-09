package dev.agentpush.poc

import android.content.Context

data class ReminderSettings(
    val waitingInput: Boolean = true,
    val permission: Boolean = true,
    val error: Boolean = true,
    val completed: Boolean = true,
) {
    fun allows(kind: String): Boolean {
        return when (kind) {
            "permission" -> permission
            "needs_input" -> waitingInput
            "error" -> error
            "reply" -> completed
            else -> true
        }
    }
}

object ReminderPreferences {
    private const val PREFS = "homing_reminders"

    fun load(context: Context): ReminderSettings {
        val preferences = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        return ReminderSettings(
            waitingInput = preferences.getBoolean("waiting_input", true),
            permission = preferences.getBoolean("permission", true),
            error = preferences.getBoolean("error", true),
            completed = preferences.getBoolean("completed", true),
        )
    }

    fun save(context: Context, settings: ReminderSettings) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .edit()
            .putBoolean("waiting_input", settings.waitingInput)
            .putBoolean("permission", settings.permission)
            .putBoolean("error", settings.error)
            .putBoolean("completed", settings.completed)
            .apply()
    }
}
