package dev.agentpush.poc

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

class QuestionDecisionTest {

    @Test
    fun parsesStructuredQuestionSessionItemAndRetainsQuestionsAfterToolResult() {
        val pendingJson = JSONObject(
            """
            {
              "ts": "2026-01-01T00:00:02Z",
              "id": "workbuddy-tool-call-q1",
              "seq": 4,
              "type": "question",
              "role": "assistant",
              "title": "部署目标",
              "text": "[部署目标] 请选择目标环境？\n1. Staging — 测试环境\n2. Production — 生产环境",
              "tool": { "name": "AskUserQuestion", "status": "running" },
              "questions": [
                {
                  "header": "部署目标",
                  "question": "请选择目标环境？",
                  "multi_select": false,
                  "options": [
                    { "label": "Staging", "description": "测试环境" },
                    { "label": "Production", "description": "生产环境" }
                  ]
                }
              ]
            }
            """.trimIndent(),
        )

        val item = SessionItem.fromJson(pendingJson)
        assertNotNull(item)
        item!!
        assertEquals(SessionItemType.QUESTION, item.type)
        assertEquals("部署目标", item.title)
        assertEquals(1, item.questions.size)
        assertEquals(2, item.questions[0].options.size)
        assertEquals("Staging", item.questions[0].options[0].label)

        val answeredJson = JSONObject(
            """
            {
              "ts": "2026-01-01T00:00:05Z",
              "id": "workbuddy-tool-call-q1",
              "seq": 4,
              "type": "tool_result",
              "role": "tool",
              "tool": { "name": "AskUserQuestion", "output_tail": "Staging", "status": "done" }
            }
            """.trimIndent(),
        )
        val answered = SessionItem.fromJson(answeredJson)!!
        val initial = appendSessionItem(SessionItemsState(), item)
        val updated = appendSessionItem(initial, answered)

        assertEquals(1, updated.items.size)
        val finalItem = updated.items[0]
        assertEquals(SessionItemType.QUESTION, finalItem.type)
        assertEquals("done", finalItem.tool.status)
        assertEquals(1, finalItem.questions.size)
        assertEquals("Staging", finalItem.tool.outputTail)
        assertTrue(latestPendingQuestionItems(updated.items).isEmpty())
    }

    @Test
    fun parsesAndSerializesAgentEventWithQuestions() {
        val raw = JSONObject(
            """
            {
              "id": 91,
              "ts": "2026-01-01T00:00:03Z",
              "source": "workbuddy",
              "kind": "needs_input",
              "session_id": "demo-wb-q",
              "cwd": "/tmp/demo",
              "project": "demo",
              "title": "WorkBuddy · 待确认：风格",
              "text": "[风格] 请选择主题色？\n1. 深海蓝",
              "read": false,
              "questions": [
                {
                  "header": "风格",
                  "question": "请选择主题色？",
                  "multi_select": true,
                  "options": [
                    { "label": "深海蓝", "description": "沉稳" },
                    { "label": "琥珀橙", "description": "醒目" }
                  ]
                }
              ]
            }
            """.trimIndent(),
        )

        val event = AgentEvent.fromJson(raw)
        assertEquals(1, event.questions.size)
        assertTrue(event.questions[0].multiSelect)
        assertEquals("深海蓝", event.questions[0].options[0].label)

        val roundTrip = AgentEvent.fromJson(event.toJson())
        assertEquals(event.questions, roundTrip.questions)
        assertTrue(roundTrip.isPending)
    }
}
