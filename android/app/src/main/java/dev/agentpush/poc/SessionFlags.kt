package dev.agentpush.poc

import android.content.Context
import org.json.JSONObject

data class LocalSessionFlags(
    val read: Boolean = false,
    val pinned: Boolean = false,
    val muted: Boolean = false,
    val archived: Boolean = false,
    val readAt: Long = 0L,
) {
    fun toJson(): JSONObject = JSONObject()
        .put("read", read)
        .put("pinned", pinned)
        .put("muted", muted)
        .put("archived", archived)
        .put("read_at", readAt)

    companion object {
        fun fromJson(json: JSONObject): LocalSessionFlags = LocalSessionFlags(
            read = json.optBoolean("read", false),
            pinned = json.optBoolean("pinned", false),
            muted = json.optBoolean("muted", false),
            archived = json.optBoolean("archived", false),
            readAt = json.optLong("read_at", 0L),
        )
    }
}

class SessionFlagsStore(context: Context) {
    private val preferences = context.applicationContext.getSharedPreferences(
        PREFS_NAME,
        Context.MODE_PRIVATE,
    )

    fun load(): Map<String, LocalSessionFlags> = synchronized(this) {
        val raw = preferences.getString(KEY_FLAGS, null) ?: return emptyMap()
        val root = runCatching { JSONObject(raw) }.getOrNull() ?: return emptyMap()
        val result = linkedMapOf<String, LocalSessionFlags>()
        val keys = root.keys()
        while (keys.hasNext()) {
            val sessionId = keys.next()
            root.optJSONObject(sessionId)?.let { json ->
                result[sessionId] = LocalSessionFlags.fromJson(json)
            }
        }
        result
    }

    fun update(
        sessionId: String,
        transform: (LocalSessionFlags) -> LocalSessionFlags,
    ): Map<String, LocalSessionFlags> = synchronized(this) {
        val current = load().toMutableMap()
        current[sessionId] = transform(
            current[sessionId] ?: LocalSessionFlags(),
        )
        saveLocked(current)
        current
    }

    fun updateAll(
        transform: (LocalSessionFlags) -> LocalSessionFlags,
    ): Map<String, LocalSessionFlags> = synchronized(this) {
        val current = load().mapValues { (_, flags) -> transform(flags) }.toMutableMap()
        saveLocked(current)
        current
    }

    fun updateMany(
        sessionIds: Collection<String>,
        transform: (LocalSessionFlags) -> LocalSessionFlags,
    ): Map<String, LocalSessionFlags> = synchronized(this) {
        val current = load().toMutableMap()
        for (sessionId in sessionIds) {
            if (sessionId.isBlank()) continue
            current[sessionId] = transform(current[sessionId] ?: LocalSessionFlags())
        }
        saveLocked(current)
        current
    }

    private fun saveLocked(value: Map<String, LocalSessionFlags>) {
        val root = JSONObject()
        for ((sessionId, flags) in value) {
            root.put(sessionId, flags.toJson())
        }
        preferences.edit().putString(KEY_FLAGS, root.toString()).apply()
    }

    companion object {
        private const val PREFS_NAME = "homing_session_flags"
        private const val KEY_FLAGS = "flags"
    }
}
