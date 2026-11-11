package dev.agentpush.poc

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class NotifyPayloadTest {
    @Test
    fun parsesP0AndP1WithSessionTarget() {
        val p0 = NotifyPayload.fromJson(JSONObject()
            .put("type", "notify")
            .put("level", "P0")
            .put("source", "codex")
            .put("session_id", "demo-session-p0")
            .put("title", "示例：需要授权")
            .put("summary", "示例摘要")
            .put("at", "2026-11-09T08:00:00.000Z"))
        requireNotNull(p0)
        assertEquals("P0", p0.level)
        assertEquals(AgentBrand.CODEX, p0.brand)
        assertEquals("demo-session-p0", p0.targetSessionId)

        val p1 = NotifyPayload.fromJson(JSONObject()
            .put("level", "p1")
            .put("source", "workbuddy")
            .put("session_id", "demo-session-p1")
            .put("title", "示例：等待输入")
            .put("summary", "示例问题"))
        requireNotNull(p1)
        assertEquals("P1", p1.level)
        assertEquals(AgentBrand.WORKBUDDY, p1.brand)
        assertEquals("demo-session-p1", p1.targetSessionId)
    }

    @Test
    fun mergedP2DoesNotOpenAnArbitrarySession() {
        val payload = NotifyPayload.fromJson(JSONObject()
            .put("level", "P2")
            .put("title", "3 个会话已完成")
            .put("summary", "demo-a · demo-b")
            .put("merged", true)
            .put("count", 3)
            .put("sessions", org.json.JSONArray()
                .put(JSONObject()
                    .put("source", "codex")
                    .put("session_id", "demo-a")
                    .put("title", "示例 A"))
                .put(JSONObject()
                    .put("source", "claude")
                    .put("session_id", "demo-b")
                    .put("title", "示例 B"))))
        requireNotNull(payload)
        assertTrue(payload.merged)
        assertEquals(3, payload.count)
        assertEquals(2, payload.sessions.size)
        assertNull(payload.targetSessionId)
    }

    @Test
    fun parsesReasonAndDispatchIdForCollaborationAction() {
        val payload = NotifyPayload.fromJson(JSONObject()
            .put("level", "P1")
            .put("kind", "collab_result")
            .put("source", "claude")
            .put("session_id", "demo-session-parent")
            .put("dispatch_id", "dsp_demo-1")
            .put("title", "協作完成")
            .put("summary", "示例結論"))
        requireNotNull(payload)
        assertEquals("collab_result", payload.kind)
        assertEquals("dsp_demo-1", payload.dispatchId)
    }

    @Test
    fun rejectsUnknownLevelAndEmptyBody() {
        assertNull(NotifyPayload.fromJson(JSONObject()
            .put("level", "P9")
            .put("title", "示例")))
        assertNull(NotifyPayload.fromJson(JSONObject()
            .put("level", "P1")))
        assertNull(NotifyPayload.fromJson(JSONObject()
            .put("type", "other")
            .put("level", "P1")
            .put("title", "示例")))
    }
}
