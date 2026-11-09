package dev.agentpush.poc

import android.content.Context

data class AppSettings(
    val baseUrl: String,
    val token: String,
) {
    companion object {
        const val DEFAULT_BASE_URL = "http://192.168.1.3:18787"
        private const val PREFS_NAME = "agent_push_settings"
        private const val KEY_BASE_URL = "base_url"
        private const val KEY_TOKEN = "token"

        fun load(context: Context): AppSettings {
            val preferences = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            return AppSettings(
                baseUrl = preferences.getString(KEY_BASE_URL, DEFAULT_BASE_URL)
                    ?: DEFAULT_BASE_URL,
                token = preferences.getString(KEY_TOKEN, "").orEmpty(),
            )
        }

        fun save(context: Context, baseUrl: String, token: String): AppSettings {
            val normalizedBaseUrl = normalizeBaseUrl(baseUrl)
            val normalizedToken = token.trim()
            context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .edit()
                .putString(KEY_BASE_URL, normalizedBaseUrl)
                .putString(KEY_TOKEN, normalizedToken)
                .apply()
            return AppSettings(normalizedBaseUrl, normalizedToken)
        }

        fun normalizeBaseUrl(value: String): String {
            var url = value.trim().ifBlank { DEFAULT_BASE_URL }
            if (!url.startsWith("http://", ignoreCase = true) &&
                !url.startsWith("https://", ignoreCase = true)
            ) {
                url = "http://$url"
            }
            return url.trimEnd('/')
        }
    }
}
