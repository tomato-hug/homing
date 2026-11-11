package dev.agentpush.poc

import org.json.JSONArray
import org.json.JSONObject

/**
 * 会议室（/api/rooms）的数据模型与请求。后端见 internal/relayapi/rooms.go：
 * 几个 Agent 轮流发言的只读讨论，结束时生成会议纪要。
 */
data class RoomParticipant(
    val agent: String,
    val name: String,
    val role: String,
    /** waiting | speaking | needs_you | failed */
    val status: String,
    val sessionId: String,
) {
    val label: String get() = if (role.isBlank()) name else "$name（$role）"

    companion object {
        fun fromJson(json: JSONObject) = RoomParticipant(
            agent = json.optString("agent"),
            name = json.optString("name").ifBlank { json.optString("agent") },
            role = json.optString("role"),
            status = json.optString("status", "waiting"),
            sessionId = json.optString("session_id"),
        )
    }
}

data class RoomMessage(
    val seq: Int,
    val from: String,
    val name: String,
    /** statement | user | system | minutes */
    val kind: String,
    val round: Int,
    val text: String,
    val at: String,
) {
    companion object {
        fun fromJson(json: JSONObject) = RoomMessage(
            seq = json.optInt("seq"),
            from = json.optString("from"),
            name = json.optString("name"),
            kind = json.optString("kind"),
            round = json.optInt("round"),
            text = json.optString("text"),
            at = json.optString("at"),
        )
    }
}

data class Room(
    val id: String,
    val title: String,
    val topic: String,
    val cwd: String,
    val rounds: Int,
    val round: Int,
    /** running | paused | summarizing | ended */
    val status: String,
    val speaking: String,
    val participants: List<RoomParticipant>,
    val messages: List<RoomMessage>,
    val minutes: String,
    val note: String,
    val messageCount: Int,
    val preview: String,
    val updatedAt: String,
) {
    val ended: Boolean get() = status == "ended"
    val speakingParticipant: RoomParticipant?
        get() = participants.firstOrNull { it.agent == speaking && speaking.isNotBlank() }

    /** 列表和房间页顶部用的一句话状态。 */
    val statusLabel: String
        get() = when (status) {
            "running" -> if (round > 0) "进行中 · 第 $round / $rounds 轮" else "准备开始"
            "paused" -> "已暂停"
            "summarizing" -> "正在整理纪要"
            "ended" -> "已结束"
            else -> status
        }

    companion object {
        fun fromJson(json: JSONObject): Room {
            val participants = json.optJSONArray("participants").objects().map(RoomParticipant::fromJson)
            val messages = json.optJSONArray("messages").objects().map(RoomMessage::fromJson)
            return Room(
                id = json.optString("id"),
                title = json.optString("title"),
                topic = json.optString("topic"),
                cwd = json.optString("cwd"),
                rounds = json.optInt("rounds"),
                round = json.optInt("round"),
                status = json.optString("status"),
                speaking = json.optString("speaking"),
                participants = participants,
                messages = messages,
                minutes = json.optString("minutes"),
                note = json.optString("note"),
                messageCount = json.optInt("message_count", messages.size),
                preview = json.optString("preview"),
                updatedAt = json.optString("updated_at"),
            )
        }
    }
}

data class RoomAgentChoice(val id: String, val name: String)

data class RoomList(val rooms: List<Room>, val agents: List<RoomAgentChoice>)

data class RoomSeat(val agent: String, val role: String)

private fun JSONArray?.objects(): List<JSONObject> {
    if (this == null) return emptyList()
    return (0 until length()).mapNotNull { optJSONObject(it) }
}

object RoomApi {
    suspend fun list(settings: AppSettings): RoomList {
        val json = JSONObject(AgentApi.roomRequest(settings, "/api/rooms", "GET"))
        return RoomList(
            rooms = json.optJSONArray("rooms").objects().map(Room::fromJson),
            agents = json.optJSONArray("agents").objects().map {
                RoomAgentChoice(it.optString("id"), it.optString("name").ifBlank { it.optString("id") })
            },
        )
    }

    suspend fun get(settings: AppSettings, id: String): Room {
        val json = JSONObject(AgentApi.roomRequest(settings, "/api/rooms/${encodePath(id)}", "GET"))
        return Room.fromJson(json.optJSONObject("room") ?: JSONObject())
    }

    suspend fun create(
        settings: AppSettings,
        topic: String,
        cwd: String,
        rounds: Int,
        seats: List<RoomSeat>,
    ): Room {
        val participants = JSONArray()
        seats.forEach { participants.put(JSONObject().put("agent", it.agent).put("role", it.role)) }
        val body = JSONObject()
            .put("topic", topic)
            .put("cwd", cwd)
            .put("rounds", rounds)
            .put("mode", "round_robin")
            .put("participants", participants)
        val json = JSONObject(AgentApi.roomRequest(settings, "/api/rooms", "POST", body.toString()))
        return Room.fromJson(json.optJSONObject("room") ?: JSONObject())
    }

    suspend fun say(settings: AppSettings, id: String, text: String): Room {
        val body = JSONObject().put("text", text).toString()
        val json = JSONObject(
            AgentApi.roomRequest(settings, "/api/rooms/${encodePath(id)}/messages", "POST", body),
        )
        return Room.fromJson(json.optJSONObject("room") ?: JSONObject())
    }

    /** action：pause | resume | end | extend */
    suspend fun control(settings: AppSettings, id: String, action: String): Room {
        val body = JSONObject().put("action", action).toString()
        val json = JSONObject(
            AgentApi.roomRequest(settings, "/api/rooms/${encodePath(id)}/control", "POST", body),
        )
        return Room.fromJson(json.optJSONObject("room") ?: JSONObject())
    }

    private fun encodePath(value: String): String =
        java.net.URLEncoder.encode(value, "UTF-8").replace("+", "%20")
}
