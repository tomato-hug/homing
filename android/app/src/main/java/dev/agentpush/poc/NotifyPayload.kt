package dev.agentpush.poc

import org.json.JSONArray
import org.json.JSONObject

data class NotifySession(
    val source: String,
    val sessionId: String,
    val title: String,
) {
    companion object {
        fun fromJson(json: JSONObject): NotifySession? {
            val sessionId = json.optString("session_id").trim()
            if (sessionId.isBlank()) return null
            return NotifySession(
                source = json.optString("source").trim(),
                sessionId = sessionId,
                title = json.optString("title").trim(),
            )
        }
    }
}

data class NotifyPayload(
    val type: String,
    val level: String,
    val source: String,
    val sessionId: String,
    val eventId: Long,
    val title: String,
    val summary: String,
    val at: String,
    val kind: String,
    val dispatchId: String,
    val temp: Boolean,
    val merged: Boolean,
    val count: Int,
    val sessions: List<NotifySession>,
) {
    val brand: AgentBrand
        get() = AgentBrand.fromWire(source)

    val targetSessionId: String?
        get() = sessionId.takeIf {
            it.isNotBlank() && (!merged || count <= 1)
        }

    val notificationKey: String
        get() = sessionId.ifBlank { "notify:$at:$title:$count" }

    companion object {
        fun fromJson(json: JSONObject): NotifyPayload? {
            if (json.optString("type").ifBlank { "notify" } != "notify") return null
            val level = json.optString("level").uppercase().takeIf {
                it == "P0" || it == "P1" || it == "P2"
            } ?: return null
            val source = json.optString("source").trim()
            val rawTitle = json.optString("title").trim()
            val summary = json.optString("summary").trim()
            if (rawTitle.isBlank() && summary.isBlank()) return null
            val title = rawTitle.ifBlank { AgentBrand.fromWire(source).displayName }
            val sessions = buildList {
                val array = json.optJSONArray("sessions") ?: JSONArray()
                for (index in 0 until array.length()) {
                    array.optJSONObject(index)?.let(NotifySession::fromJson)?.let(::add)
                }
            }
            return NotifyPayload(
                type = "notify",
                level = level,
                source = source,
                sessionId = json.optString("session_id").trim(),
                eventId = json.optLong("event_id", 0L),
                title = title,
                summary = summary,
                at = json.optString("at").trim(),
                kind = json.optString("kind").trim(),
                dispatchId = json.optString("dispatch_id").trim(),
                temp = json.optBoolean("temp", false),
                merged = json.optBoolean("merged", false),
                count = json.optInt("count", 1).coerceAtLeast(1),
                sessions = sessions,
            )
        }
    }
}
