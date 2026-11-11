package dev.agentpush.poc

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.json.JSONObject

class SessionPresentationTest {
    private val uuid = "01a1bad2-568a-7b33-ba69-9344a0470cad"

    @Test
    fun nativeTitleWinsOverPromptAndProject() {
        val resolved = resolveSessionTitle(
            serverTitle = "修复 SSE 重连",
            titleSource = "agent",
            firstQuestion = "帮我看看连接",
            eventTitle = "Codex 回复",
            project = "agent-push-poc",
            sessionId = uuid,
            source = "codex",
        )

        assertEquals("修复 SSE 重连", resolved.title)
        assertEquals("agent", resolved.source)
    }

    @Test
    fun homingTitleWinsOverPromptAndProject() {
        val resolved = resolveSessionTitle(
            serverTitle = "手机端自訂名稱",
            titleSource = "homing",
            firstQuestion = "幫我看看連線",
            eventTitle = "Codex 回覆",
            project = "agent-push-poc",
            sessionId = uuid,
            source = "codex",
        )

        assertEquals("手机端自訂名稱", resolved.title)
        assertEquals("homing", resolved.source)
        assertEquals("用户命名", titleSourceLabel("homing"))
    }

    @Test
    fun agentTitleWrapperIsCleanedWithoutTouchingCustomTitle() {
        val native = resolveSessionTitle(
            serverTitle = "标题：创建鹈鹕骑自行车SVG动画",
            titleSource = "agent",
            firstQuestion = "请做一个动画",
            eventTitle = "Codex 回复",
            project = "demo-project",
            sessionId = uuid,
            source = "codex",
        )
        val custom = resolveSessionTitle(
            serverTitle = "标题：这是我自己取的名字",
            titleSource = "homing",
            firstQuestion = "请做一个动画",
            eventTitle = "Codex 回复",
            project = "demo-project",
            sessionId = uuid,
            source = "codex",
        )

        assertEquals("创建鹈鹕骑自行车SVG动画", native.title)
        assertEquals("标题：这是我自己取的名字", custom.title)
    }

    @Test
    fun firstPromptBeatsProjectFallback() {
        val resolved = resolveSessionTitle(
            serverTitle = "agent-push-poc",
            titleSource = "project",
            firstQuestion = "# 修复手机端未读\n请保留中文排版",
            eventTitle = "Claude Code 回复",
            project = "agent-push-poc",
            sessionId = uuid,
            source = "claude",
        )

        assertEquals("修复手机端未读", resolved.title)
        assertEquals("first_prompt", resolved.source)
    }

    @Test
    fun uuidIsNeverUsedAsTitle() {
        val resolved = resolveSessionTitle(
            serverTitle = uuid,
            titleSource = "agent",
            firstQuestion = null,
            eventTitle = null,
            project = "项目名",
            sessionId = uuid,
            source = "codex",
        )

        assertFalse(resolved.title.contains(uuid))
        assertEquals("项目名", resolved.title)
        assertEquals("project", resolved.source)
    }

    @Test
    fun fallbackTitleIsAlwaysNonEmpty() {
        val codex = resolveSessionTitle(
            serverTitle = null,
            titleSource = null,
            firstQuestion = null,
            eventTitle = null,
            project = "",
            sessionId = "x",
            source = "codex",
        )
        val unknown = resolveSessionTitle(
            serverTitle = null,
            titleSource = null,
            firstQuestion = null,
            eventTitle = null,
            project = "",
            sessionId = "",
            source = "",
        )

        assertEquals("Codex 会话", codex.title)
        assertEquals("未命名会话", unknown.title)
        assertEquals("fallback", codex.source)
    }

    @Test
    fun tagsFollowProjectBranchModelWorkspacePriority() {
        val summary = session(
            cwd = "/Users/me/Projects/agent-push-poc",
            project = "agent-push-poc",
            workspace = "Projects",
            branch = "codex/session-ui",
            model = "gpt-5.6",
        )

        val tags = sessionTags(summary)

        assertEquals(
            listOf(
                SessionTagKind.PROJECT,
                SessionTagKind.BRANCH,
                SessionTagKind.MODEL,
                SessionTagKind.WORKSPACE,
            ),
            tags.map { it.kind },
        )
        assertEquals("codex/session-ui", tags[1].text)
    }

    @Test
    fun groupingUsesCwdNotProjectName() {
        val first = session(cwd = "/tmp/one", project = "same", lastTs = "2026-11-08T10:00:00Z")
        val second = session(cwd = "/tmp/two", project = "same", lastTs = "2026-11-08T11:00:00Z")

        val projectGroups = groupSessions(listOf(first, second), SessionGrouping.PROJECT)
        val agentGroups = groupSessions(listOf(first, second), SessionGrouping.AGENT)
        val timeGroups = groupSessions(listOf(first, second), SessionGrouping.TIME)

        assertEquals(2, projectGroups.size)
        assertEquals("/tmp/two", projectGroups.first().key)
        assertEquals(1, agentGroups.size)
        assertEquals("codex", agentGroups.first().sessions.first().brand.wire)
        assertEquals(1, timeGroups.size)
        assertEquals(2, timeGroups.first().sessions.size)
    }

    @Test
    fun quickActionsMatchStatus() {
        assertEquals(SessionQuickAction.APPROVE, SessionStatus.NEEDS_PERMISSION.quickAction())
        assertEquals(SessionQuickAction.REPLY, SessionStatus.WAITING_INPUT.quickAction())
        assertEquals(SessionQuickAction.VIEW, SessionStatus.RUNNING.quickAction())
        assertEquals(SessionQuickAction.VIEW, SessionStatus.COMPLETED.quickAction())
    }

    @Test
    fun agentGroupingSplitsDynamicSources() {
        val kimi = session(
            cwd = "/tmp/kimi",
            lastTs = "2026-11-08T10:00:00Z",
            source = "kimi",
        )
        val dsh = session(
            cwd = "/tmp/dsh",
            lastTs = "2026-11-08T11:00:00Z",
            source = "deepseek-harness",
        )

        val groups = groupSessions(listOf(kimi, dsh), SessionGrouping.AGENT)

        assertEquals(2, groups.size)
        assertEquals(
            setOf("agent:kimi", "agent:deepseek-harness"),
            groups.map { it.key }.toSet(),
        )
    }

    @Test
    fun identifierDetectionHandlesUuidAndSessionId() {
        assertTrue(isBareSessionIdentifier(uuid))
        assertTrue(isBareSessionIdentifier("custom-thread", "custom-thread"))
        assertFalse(isBareSessionIdentifier("修复会话标题", "custom-thread"))
    }

    @Test
    fun eventParsesNestedSessionMeta() {
        val event = AgentEvent.fromJson(
            JSONObject(
                """
                {
                  "id": 88,
                  "ts": "2026-11-08T14:20:12.589Z",
                  "source": "codex",
                  "kind": "reply",
                  "session_id": "$uuid",
                  "cwd": "/Users/me/Projects/agent-push-poc",
                  "project": "agent-push-poc",
                  "title": "Codex 回复",
                  "text": "收到",
                  "read": false,
                  "session_meta": {
                    "title": "修复会话卡片",
                    "title_source": "agent",
                    "workspace": "Projects",
                    "project": "agent-push-poc",
                    "branch": "codex/session-ui",
                    "model": "gpt-5.6"
                  }
                }
                """.trimIndent(),
            ),
        )

        assertEquals("修复会话卡片", event.sessionMeta?.title)
        assertEquals("agent", event.sessionMeta?.titleSource)
        assertEquals(uuid, event.sessionMeta?.sessionId)
        assertEquals("codex", event.sessionMeta?.source)
        assertEquals("codex/session-ui", event.sessionMeta?.branch)
    }

    @Test
    fun sessionEventParsesRequestId() {
        val meta = SessionMeta.fromJson(
            JSONObject(
                """
                {
                  "session_id": "claude-request-1",
                  "source": "claude",
                  "title": "从手机新建的任务",
                  "title_source": "first_prompt",
                  "workspace": "Projects",
                  "project": "agent-push-poc",
                  "branch": null,
                  "cwd": "/Users/me/Projects/agent-push-poc",
                  "model": null,
                  "status": "running",
                  "unread": 0,
                  "last_ts": "2026-11-08T14:30:00Z",
                  "request_id": "request-42"
                }
                """.trimIndent(),
            ),
        )

        assertEquals("request-42", meta.requestId)
        assertEquals("Claude", meta.brand.displayName)
        assertEquals(SessionStatus.RUNNING, SessionStatus.fromWire(meta.status))
        assertEquals("", meta.branch)
        assertEquals("", meta.model)
    }

    @Test
    fun sessionOnlyReplyUsesSessionIdAndSource() {
        val payload = buildReplyPayload(
            eventId = null,
            sessionId = "session-without-events",
            source = "claude",
            text = "继续",
            files = listOf("/tmp/note.txt"),
        )

        assertFalse(payload.has("event_id"))
        assertEquals("session-without-events", payload.getString("session_id"))
        assertEquals("claude", payload.getString("source"))
        assertEquals("/tmp/note.txt", payload.getJSONArray("files").getString(0))
    }

    @Test
    fun replyWithEventKeepsLegacyEventIdPayload() {
        val payload = buildReplyPayload(
            eventId = 42L,
            sessionId = "session",
            source = "codex",
            text = "继续",
            files = emptyList(),
        )

        assertEquals(42L, payload.getLong("event_id"))
        assertFalse(payload.has("session_id"))
    }

    @Test
    fun unreadComesFromPhoneEventsNotServerMeta() {
        val event = event(
            id = 7,
            sessionId = "session-1",
            read = false,
            text = "新回复",
        )
        val meta = SessionMeta(
            sessionId = "session-1",
            source = "codex",
            title = "会话标题",
            titleSource = "agent",
            project = "agent-push-poc",
            cwd = "/Users/me/Projects/agent-push-poc",
            status = "done",
            unread = 99,
        )
        val state = AgentUiState(
            settings = AppSettings("http://127.0.0.1:18787", "token"),
            events = mapOf(event.id to event),
            sessionMeta = mapOf(meta.sessionId to meta),
        )

        val session = state.sessions().single()

        assertEquals(1, session.unreadCount)
        assertEquals("新回复", session.lastPreview)
    }

    @Test
    fun sessionsApiIsPrimaryButEventsRemainFallback() {
        val event = event(
            id = 8,
            sessionId = "event-only",
            read = true,
            text = "只有事件",
        )
        val shared = AgentUiState(
            settings = AppSettings("http://127.0.0.1:18787", "token"),
            events = mapOf(event.id to event),
        )

        assertEquals(0, shared.copy(sessionApiAvailable = true).sessions().size)
        assertEquals(1, shared.copy(sessionApiAvailable = false).sessions().size)
    }

    @Test
    fun acpPermissionOptionsSurviveEventRoundTrip() {
        val raw = JSONObject(
            """
            {
              "id": 41,
              "ts": "2026-11-08T10:00:00Z",
              "source": "hermes",
              "kind": "permission",
              "session_id": "demo-session-acp",
              "title": "请求授权",
              "text": "写入文件",
              "session_meta": {
                "acp": {
                  "options": [
                    {"id": "allow_once", "label": "仅此次", "kind": "allow_once"},
                    {"id": "allow_always", "label": "总是允许", "kind": "allow_always"},
                    {"id": "reject_once", "label": "拒绝", "kind": "reject_once"}
                  ]
                }
              }
            }
            """.trimIndent(),
        )

        val event = AgentEvent.fromJson(raw)

        assertEquals(listOf("仅此次", "总是允许", "拒绝"), event.approvalOptions.map { it.label })
        assertEquals(listOf("allow", "allow", "deny"), event.approvalOptions.map { it.decision })
        val restored = AgentEvent.fromJson(event.toJson())
        assertEquals(3, restored.approvalOptions.size)
        assertEquals("allow_always", restored.approvalOptions[1].id)
    }

    private fun session(
        cwd: String,
        project: String = "agent-push-poc",
        workspace: String = "Projects",
        branch: String = "main",
        model: String = "gpt-5.6",
        lastTs: String = "2026-11-08T10:00:00Z",
        source: String = "codex",
    ): SessionSummary {
        return SessionSummary(
            sessionId = "$cwd-$lastTs",
            source = source,
            brand = AgentBrand.fromWire(source),
            agentLabel = if (source == "codex") "Codex" else source,
            agentInitial = source.firstOrNull()?.uppercase().orEmpty(),
            title = "会话标题",
            titleSource = "agent",
            workspace = workspace,
            project = project,
            branch = branch,
            model = model,
            cwd = cwd,
            status = SessionStatus.COMPLETED,
            latestEvent = null,
            events = emptyList(),
            replies = emptyList(),
            unreadCount = 0,
            lastPreview = "最后一句摘要",
            lastTs = lastTs,
        )
    }

    private fun event(
        id: Long,
        sessionId: String,
        read: Boolean,
        text: String,
    ): AgentEvent {
        return AgentEvent(
            id = id,
            ts = "2026-11-08T10:00:00Z",
            source = "codex",
            kind = "reply",
            sessionId = sessionId,
            cwd = "/Users/me/Projects/agent-push-poc",
            project = "agent-push-poc",
            title = "Codex 回复",
            text = text,
            read = read,
        )
    }
}
