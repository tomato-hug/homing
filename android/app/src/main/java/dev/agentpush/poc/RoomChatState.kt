package dev.agentpush.poc

import org.json.JSONObject

/** A failed write may have reached the server. Only a matching receipt proves delivery. */
enum class RoomDelivery { Sending, Unknown, Rejected }
data class RoomPendingMessage(val clientId: String, val text: String, val mention: String, val delivery: RoomDelivery) {
    fun isDeliveredBy(messages: List<RoomMessage>) = messages.any { it.clientId == clientId && it.from == "user" }
    fun toJson() = JSONObject().put("id", clientId).put("text", text).put("mention", mention).put("delivery", delivery.name).toString()
    companion object {
        fun fromJson(value: String): RoomPendingMessage? = runCatching {
            val json = JSONObject(value)
            val delivery = RoomDelivery.valueOf(json.getString("delivery"))
            RoomPendingMessage(json.getString("id"), json.getString("text"), json.optString("mention"), if (delivery == RoomDelivery.Sending) RoomDelivery.Unknown else delivery)
        }.getOrNull()
    }
}
fun roomDeliveryForError(error: Throwable): RoomDelivery =
    if (error is AgentApiException && error.statusCode in 400..499 && error.statusCode != 408 && error.statusCode != 429) RoomDelivery.Rejected else RoomDelivery.Unknown

/** Only an exact receipt clears the pending marker and its unchanged draft. */
fun completeRoomDelivery(prefs: android.content.SharedPreferences, key: String, currentDraft: String, deliveredText: String): Boolean {
    val remaining = if (currentDraft.trim() == deliveredText) "" else currentDraft
    return prefs.edit().remove("pending_" + key).putString("draft_" + key, remaining).commit()
}

/** Mentions are structured on send; punctuation must not swallow @agent. */
fun parseRoomMentions(text: String): List<String> =
    Regex("(?<![A-Za-z0-9_])@([A-Za-z0-9_.-]+)(?=\\s|\\p{P}|$)")
        .findAll(text).map { it.groupValues[1].lowercase() }.distinct().toList()
