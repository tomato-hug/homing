package dev.agentpush.poc

import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class RoomChatStateTest {
    @Test fun receiptReconcilesOnlyMatchingStableId() {
        val pending = RoomPendingMessage("demo-id-1", "demo text", "claude", RoomDelivery.Unknown)
        val unrelated = RoomMessage.fromJson(JSONObject("""{"client_id":"demo-id-2","from":"user","text":"demo text"}"""))
        val acknowledged = RoomMessage.fromJson(JSONObject("""{"client_id":"demo-id-1","from":"user","text":"demo text"}"""))
        assertFalse(pending.isDeliveredBy(listOf(unrelated)))
        assertTrue(pending.isDeliveredBy(listOf(acknowledged)))
    }
    @Test fun networkFailureIsUnknownAndClientRejectionIsDefinitive() {
        assertEquals(RoomDelivery.Unknown, roomDeliveryForError(java.io.IOException("demo disconnected")))
        assertEquals(RoomDelivery.Unknown, roomDeliveryForError(AgentApiException(500,"demo error")))
        assertEquals(RoomDelivery.Rejected, roomDeliveryForError(AgentApiException(409,"demo rejected")))
    }
    @Test fun mentionsRecognizeChineseAndEnglishPunctuationWithoutMatchingEmail() {
        assertEquals(listOf("codex"), parseRoomMentions("@codex，帮我看"))
        assertEquals(listOf("claude"), parseRoomMentions("请@claude：核对"))
        assertTrue(parseRoomMentions("demo@example.invalid").isEmpty())
    }
    @Test fun outgoingMessageSendsCorrelationAndMentionAtomically() = kotlinx.coroutines.runBlocking {
        val server=okhttp3.mockwebserver.MockWebServer()
        server.enqueue(okhttp3.mockwebserver.MockResponse().setBody("""{"room":{"id":"room-demo"}}""")); server.start()
        try {
            AgentApi.bindTransport(null)
            RoomApi.say(AppSettings(server.url("/").toString().trimEnd('/'),"demo-token"),"room-demo","@claude demo","demo-id-1","claude")
            val body=JSONObject(server.takeRequest(3,java.util.concurrent.TimeUnit.SECONDS)!!.body.readUtf8())
            assertEquals("demo-id-1",body.getString("client_id")); assertEquals("claude",body.getString("mention"))
        } finally { server.shutdown() }
    }
}

class RoomActionTest {
    @Test fun actionAndResultAreParsed() {
        val room=Room.fromJson(JSONObject("""{"id":"room-demo","actions":[{"id":"action-1","text":"demo task","status":"completed","agent":"codex","dispatch_id":"demo-dispatch","session_id":"demo-session","result":"demo result"}]}"""))
        assertEquals("demo result",room.actions.single().result)
        assertEquals("demo-session",room.actions.single().sessionId)
    }
    @Test fun actionDispatchRequiresConfirmedPayload() = kotlinx.coroutines.runBlocking {
        val server=okhttp3.mockwebserver.MockWebServer()
        server.enqueue(okhttp3.mockwebserver.MockResponse().setBody("""{"room":{"id":"room-demo"}}"""));server.start()
        try {
            AgentApi.bindTransport(null)
            RoomApi.dispatchAction(AppSettings(server.url("/").toString().trimEnd('/'),"demo-token"),"room-demo","action-1","codex","demo task")
            val req=server.takeRequest(3,java.util.concurrent.TimeUnit.SECONDS)!!
            assertEquals("/api/rooms/room-demo/actions/action-1/dispatch",req.path)
            val body=JSONObject(req.body.readUtf8());assertTrue(body.getBoolean("confirmed"));assertEquals("codex",body.getString("agent"))
        } finally {server.shutdown()}
    }
}
