package dev.agentpush.poc

import dev.agentpush.poc.ui.screens.newSessionAvailabilityLabel
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RoomsTest {
    private fun sampleRoomJson(status: String, speaking: String = "") = JSONObject()
        .put("id", "room-demo")
        .put("title", "示例会议")
        .put("topic", "示例议题")
        .put("cwd", "/tmp/demo")
        .put("rounds", 2)
        .put("round", 1)
        .put("status", status)
        .put("speaking", speaking)
        .put(
            "participants",
            JSONArray()
                .put(JSONObject().put("agent", "codex").put("name", "Codex").put("role", "架构").put("status", "speaking"))
                .put(JSONObject().put("agent", "claude").put("name", "Claude Code").put("status", "waiting")),
        )
        .put(
            "messages",
            JSONArray().put(
                JSONObject().put("seq", 1).put("from", "claude").put("name", "Claude Code")
                    .put("kind", "statement").put("round", 1).put("text", "示例发言"),
            ),
        )

    @Test
    fun pointNameDoesNotAddATurnOrChooseTheCurrentSpeaker() {
        val room = Room.fromJson(sampleRoomJson("running", "codex").put("next_speaker", "claude"))
        assertEquals("claude", room.nextSpeaker)
        assertEquals(listOf("claude"), room.callableParticipants.map { it.agent })
        assertTrue(Room.fromJson(sampleRoomJson("ended")).callableParticipants.isEmpty())
        assertTrue(Room.fromJson(sampleRoomJson("running").put("end_requested", true)).callableParticipants.isEmpty())
    }

    @Test
    fun sendsNamedParticipantOverActualRoomControlEndpoint() = kotlinx.coroutines.runBlocking {
        val server = okhttp3.mockwebserver.MockWebServer()
        server.enqueue(okhttp3.mockwebserver.MockResponse().setBody(JSONObject().put("room", sampleRoomJson("paused").put("next_speaker", "claude")).toString()))
        server.start()
        try {
            AgentApi.bindTransport(null)
            val room = RoomApi.control(AppSettings(server.url("/").toString().trimEnd('/'), "demo-room-token"), "room-demo", "call", "claude")
            val request = server.takeRequest(3, java.util.concurrent.TimeUnit.SECONDS)!!
            assertEquals("POST", request.method)
            assertEquals("/api/rooms/room-demo/control", request.path)
            val body = JSONObject(request.body.readUtf8())
            assertEquals("call", body.getString("action"))
            assertEquals("claude", body.getString("agent"))
            assertEquals("paused", room.status)
            assertEquals("claude", room.nextSpeaker)
        } finally { server.shutdown() }
    }

    @Test
    fun parsesRoomAndDescribesState() {
        val room = Room.fromJson(sampleRoomJson("running", speaking = "codex"))
        assertEquals("room-demo", room.id)
        assertEquals(2, room.participants.size)
        assertEquals("Codex（架构）", room.participants[0].label)
        assertEquals("Codex", room.speakingParticipant?.name)
        assertEquals("进行中 · 第 1 / 2 轮", room.statusLabel)
        assertEquals(1, room.messages.size)
        assertEquals(1, room.messageCount)

        val ended = Room.fromJson(sampleRoomJson("ended"))
        assertTrue(ended.ended)
        assertEquals("已结束", ended.statusLabel)
        assertEquals(null, ended.speakingParticipant)
    }

    @Test
    fun newSessionLabelsDescribeLaunchNotDesktopState() {
        assertEquals("可后台启动", newSessionAvailabilityLabel("codex", true))
        assertEquals("可后台启动", newSessionAvailabilityLabel("claude", true))
        assertEquals("桌面端已打开", newSessionAvailabilityLabel("workbuddy", true))
        assertEquals("需先打开 WorkBuddy", newSessionAvailabilityLabel("workbuddy", false))
        assertEquals("未就绪", newSessionAvailabilityLabel("kimi", false))
    }
}
