package dev.agentpush.poc

import android.content.Context

data class AppSettings(
    val baseUrl: String,
    val token: String,
    val previewMentions: Boolean = true,
) {
    companion object {
        const val DEFAULT_BASE_URL = ""
        private const val PREFS_NAME = "agent_push_settings"
        private const val KEY_BASE_URL = "base_url"
        private const val KEY_TOKEN = "token"
        private const val KEY_PREVIEW_MENTIONS = "preview_mentions"

        fun load(context: Context): AppSettings {
            val preferences = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            return AppSettings(
                baseUrl = preferences.getString(KEY_BASE_URL, DEFAULT_BASE_URL)
                    ?: DEFAULT_BASE_URL,
                token = preferences.getString(KEY_TOKEN, "").orEmpty(),
                previewMentions = preferences.getBoolean(KEY_PREVIEW_MENTIONS, true),
            )
        }

        fun save(
            context: Context,
            baseUrl: String,
            token: String,
            previewMentions: Boolean = true,
        ): AppSettings {
            val normalizedBaseUrl = normalizeBaseUrl(baseUrl)
            val normalizedToken = token.trim()
            context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .edit()
                .putString(KEY_BASE_URL, normalizedBaseUrl)
                .putString(KEY_TOKEN, normalizedToken)
                .putBoolean(KEY_PREVIEW_MENTIONS, previewMentions)
                .apply()
            return AppSettings(normalizedBaseUrl, normalizedToken, previewMentions)
        }

        fun normalizeBaseUrl(value: String): String {
            var url = value.trim().ifBlank { DEFAULT_BASE_URL }
            if (url.isBlank()) return ""
            if (!url.startsWith("http://", ignoreCase = true) &&
                !url.startsWith("https://", ignoreCase = true)
            ) {
                url = "http://$url"
            }
            return url.trimEnd('/')
        }
    }
}
