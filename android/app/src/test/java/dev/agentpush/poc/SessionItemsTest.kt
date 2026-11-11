package dev.agentpush.poc

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SessionItemsTest {
    @Test
    fun nullContextLimitIsParsedAsUsedOnly() {
        val meta = SessionMeta.fromJson(
            JSONObject(
                """
                {
                  "session_id": "s1",
                  "source": "claude",
                  "context_usage": {"used": 1234, "limit": null},
                  "model": null
                }
                """.trimIndent(),
            ),
        )

        val usage = meta.contextUsage
        assertNotNull(usage)
        assertEquals(1234L, usage!!.used)
        assertEquals(0L, usage.limit)
        assertEquals("", meta.model)
    }

    @Test
    fun parsesAgentDirectoryWithCapabilitiesAndModels() {
        val directory = parseAgentDirectory(
            """
            {
              "agents": [
                {
                  "source": "codex",
                  "display_name": "Codex CLI",
                  "icon": "codex",
                  "available": true,
                  "installed": true,
                  "capabilities": {
                    "reply": true,
                    "new_session": true,
                    "switch_model": true,
                    "effort": true,
                    "stop": true,
                    "approve": true,
                    "live": true
                  },
                  "models": [
                    {
                      "id": "gpt-5.6",
                      "label": "GPT-5.6",
                      "provider": "openai",
                      "is_default": true,
                      "efforts": ["low", "high"]
                    }
                  ],
                  "default_model": "gpt-5.6"
                }
              ]
            }
            """.trimIndent(),
        )

        val profile = directory.profile("codex")
        assertTrue(directory.available)
        assertTrue(profile.capabilities.switchModel)
        assertTrue(profile.capabilities.stop)
        assertEquals("Codex CLI", profile.label)
        assertTrue(profile.available)
        assertTrue(profile.installed)
        assertEquals("gpt-5.6", profile.defaultModel)
        assertEquals("GPT-5.6", profile.defaultModelOption?.displayName)
        assertEquals(listOf("low", "high"), profile.models.single().efforts)
    }

    @Test
    fun missingAgentsEndpointLeavesCapabilitiesConservative() {
        val profile = AgentDirectory().profile("claude")

        assertFalse(profile.capabilities.newSession)
        assertFalse(profile.capabilities.switchModel)
        assertFalse(profile.capabilities.stop)
        assertFalse(profile.capabilities.effort)
        assertTrue(profile.capabilities.reply)
    }

    @Test
    fun missingNewSessionCapabilityFailsClosed() {
        val directory = parseAgentDirectory(
            """
            {
              "agents": [
                {
                  "source": "opencode",
                  "available": true,
                  "installed": true,
                  "capabilities": {
                    "reply": false,
                    "sessions": true
                  }
                }
              ]
            }
            """.trimIndent(),
        )

        val profile = directory.profile("opencode")
        assertTrue(profile.available)
        assertFalse(profile.capabilities.newSession)
        assertFalse(profile.supportsNewSession)
        assertFalse(profile.isOnlineForNewSession)
    }

    @Test
    fun legacyWorkBuddyCapabilityDoesNotEnableUnsafeNewSessions() {
        val directory = parseAgentDirectory(
            """
            {
              "agents": [
                {
                  "source": "workbuddy",
                  "available": true,
                  "installed": true,
                  "capabilities": {
                    "reply": true,
                    "new_session": true
                  }
                }
              ]
            }
            """.trimIndent(),
        )

        val profile = directory.profile("workbuddy")
        assertTrue(profile.capabilities.newSession)
        assertFalse(profile.supportsNewSession)
        assertFalse(profile.isOnlineForNewSession)
    }

    @Test
    fun explicitNewSessionReadinessEnablesAgent() {
        val directory = parseAgentDirectory(
            """
            {
              "agents": [
                {
                  "source": "workbuddy",
                  "available": true,
                  "installed": true,
                  "capabilities": {
                    "reply": true,
                    "new_session": true,
                    "new_session_ready": true
                  }
                }
              ]
            }
            """.trimIndent(),
        )

        val profile = directory.profile("workbuddy")
        assertTrue(profile.supportsNewSession)
        assertTrue(profile.isOnlineForNewSession)
    }

    @Test
    fun parsesCurrentAgentsShapeWithDefaultAlias() {
        val directory = parseAgentDirectory(
            """
            {
              "agents": [
                {
                  "source": "codex",
                  "models": [
                    {"id": "workbuddy/deepseek-v4.1-flash", "provider": "workbuddy", "default": true},
                    {"id": "gpt-6-astra", "provider": "openai", "default": false}
                  ]
                }
              ]
            }
            """.trimIndent(),
        )

        val profile = directory.profile("codex")
        assertEquals(2, profile.models.size)
        assertEquals("workbuddy/deepseek-v4.1-flash", profile.defaultModel)
        assertTrue(profile.models.first().isDefault)
        assertFalse(profile.capabilities.switchModel)
    }

    @Test
    fun parsesConnectedAgentCatalogWithoutShowingDisconnectedRows() {
        val catalog = AgentCatalog.fromJson(
            JSONObject(
                """
                {
                  "agents": [
                    {
                      "id": "codex",
                      "name": "Codex",
                      "vendor": "OpenAI",
                      "icon": "codex",
                      "detected": true,
                      "connected": true,
                      "available": true,
                      "can_connect": true,
                      "hook_installed": true
                    },
                    {
                      "id": "opencode",
                      "name": "OpenCode",
                      "detected": true,
                      "connected": false,
                      "available": true,
                      "can_connect": true
                    }
                  ],
                  "summary": {
                    "connected": 1,
                    "detected": 1
                  }
                }
                """.trimIndent(),
            ),
        )

        assertTrue(catalog.available)
        assertEquals(listOf("codex"), catalog.connected.map { it.id })
        assertEquals(listOf("opencode"), catalog.connectable.map { it.id })
        assertEquals(1, catalog.connectedCount)
        assertEquals("codex", catalog.connected.single().logoSource)
    }

    @Test
    fun parsesContentSearchSnippets() {
        val hits = SessionSearchHit.listFromJson(
            JSONObject(
                """
                {
                  "hits": [
                    {
                      "session": {
                        "session_id": "demo-session-search",
                        "source": "kimi",
                        "title": "示例会话"
                      },
                      "snippet": {
                        "before": "前文 ",
                        "match": "登入流程",
                        "after": " 後文"
                      },
                      "item_id": "item-7"
                    }
                  ]
                }
                """.trimIndent(),
            ),
        )

        assertEquals(1, hits.size)
        assertEquals("kimi", hits.single().source)
        assertEquals("前文 登入流程 後文", hits.single().snippet)
        assertEquals("item-7", hits.single().itemId)
    }

    @Test
    fun sessionMetaParsesContextAndRunningFields() {
        val meta = SessionMeta.fromJson(
            JSONObject(
                """
                {
                  "session_id": "session-1",
                  "source": "codex",
                  "model": "gpt-5.6",
                  "effort": "high",
                  "permission_mode": "on-request",
                  "running": true,
                  "running_since": "2026-11-08T15:00:00Z",
                  "context_usage": {"used": 64000, "limit": 128000}
                }
                """.trimIndent(),
            ),
        )

        assertEquals("high", meta.effort)
        assertEquals("on-request", meta.permissionMode)
        assertEquals(true, meta.running)
        assertEquals(50, meta.contextUsage?.percent)
    }

    @Test
    fun contextWithoutLimitKeepsOnlyUsedTokens() {
        val meta = SessionMeta.fromJson(
            JSONObject(
                """
                {
                  "session_id": "claude-session",
                  "source": "claude",
                  "context_usage": {"used": 175179, "limit": null}
                }
                """.trimIndent(),
            ),
        )

        assertEquals(175179L, meta.contextUsage?.used)
        assertEquals(0L, meta.contextUsage?.limit)
        assertEquals("175.2k", meta.contextUsage?.usedLabel)
    }

    @Test
    fun toolCommandAndResultMergeIntoSingleCard() {
        val call = SessionItem.fromJson(
            JSONObject(
                """
                {
                  "id": "codex-1",
                  "ts": "2026-11-08T15:00:00Z",
                  "role": "assistant",
                  "type": "command",
                  "tool": "exec_command",
                  "command": "echo sse-item-test",
                  "status": "completed"
                }
                """.trimIndent(),
            ),
        )!!
        val result = SessionItem.fromJson(
            JSONObject(
                """
                {
                  "id": "codex-2",
                  "ts": "2026-11-08T15:00:01Z",
                  "role": "tool",
                  "type": "tool_result",
                  "tool": "exec_command",
                  "exit_code": 0,
                  "text": "sse-item-test"
                }
                """.trimIndent(),
            ),
        )!!

        val merged = normalizeSessionItems(listOf(call, result))

        assertEquals(1, merged.size)
        assertEquals(SessionItemType.TOOL_CALL, merged.single().type)
        assertEquals("exec_command", merged.single().tool.name)
        assertEquals("echo sse-item-test", merged.single().tool.inputSummary)
        assertEquals("sse-item-test", merged.single().tool.outputTail)
        assertEquals(0, merged.single().tool.exitCode)
    }

    @Test
    fun parsesStructuredToolCallObjectFromCurrentRelay() {
        val item = SessionItem.fromJson(
            JSONObject(
                """
                {
                  "id": "codex-tool-call_00_iLEb8wImtWFB3Gek1y4I4289",
                  "ts": "2026-11-08T15:20:15.974Z",
                  "type": "tool_call",
                  "tool": {
                    "name": "exec_command",
                    "input_summary": "ls -la",
                    "output_tail": "total 0",
                    "exit_code": 0,
                    "status": "done"
                  }
                }
                """.trimIndent(),
            ),
        )!!

        assertEquals(SessionItemType.TOOL_CALL, item.type)
        assertEquals("exec_command", item.tool.name)
        assertEquals("ls -la", item.tool.inputSummary)
        assertEquals("total 0", item.tool.outputTail)
        assertEquals("done", item.tool.status)
        assertEquals(0, item.tool.exitCode)
        assertFalse(item.isFailed)
    }

    @Test
    fun parsesApprovalWithEventIdAndStatus() {
        val item = SessionItem.fromJson(
            JSONObject(
                """
                {
                  "id": "approval-9",
                  "ts": "2026-11-08T15:20:15.974Z",
                  "type": "approval",
                  "event_id": 9,
                  "status": "pending",
                  "text": "允许运行 ls -la？"
                }
                """.trimIndent(),
            ),
        )!!

        assertEquals(SessionItemType.APPROVAL, item.type)
        assertEquals(9L, item.eventId)
        assertEquals("pending", item.tool.status)
    }

    @Test
    fun truncatedLiveItemDoesNotReplaceFullTranscript() {
        val full = SessionItem(
            id = "item-full",
            ts = "2026-11-08T15:00:00Z",
            type = SessionItemType.TOOL_CALL,
            text = "完整內容".repeat(200),
            tool = ItemTool(
                name = "exec_command",
                inputSummary = "cat demo.txt",
                outputTail = "完整輸出".repeat(400),
                exitCode = 0,
                status = "done",
            ),
            files = listOf(
                ItemFileChange(
                    path = "demo.txt",
                    added = 2,
                    removed = 0,
                    diff = "+完整 diff".repeat(200),
                ),
            ),
        )
        val truncated = full.copy(
            text = "完整內容".repeat(10),
            tool = full.tool.copy(outputTail = "完整輸出".repeat(10), status = "running"),
            truncated = true,
        )

        val state = appendSessionItem(SessionItemsState(items = listOf(full)), truncated)
        val merged = state.items.single()

        assertEquals(full.text, merged.text)
        assertEquals(full.tool.outputTail, merged.tool.outputTail)
        assertEquals("running", merged.tool.status)
        assertEquals(full.files.single().diff, merged.files.single().diff)
        assertFalse(merged.truncated)
        assertTrue(state.running)

        val onlyLive = appendSessionItem(SessionItemsState(), truncated)
        assertTrue(onlyLive.items.single().truncated)
    }

    @Test
    fun parsesReasoningFileChangeAndPlan() {
        val reasoning = SessionItem.fromJson(
            JSONObject("""{"type":"thinking","role":"assistant","text":"先检查文件"}"""),
        )!!
        val files = SessionItem.fromJson(
            JSONObject(
                """
                {
                  "type": "file_change",
                  "files": [
                    {"path": "app/src/Main.kt", "added": 5, "removed": 2, "diff": "+a\n-b"}
                  ]
                }
                """.trimIndent(),
            ),
        )!!
        val plan = SessionItem.fromJson(
            JSONObject(
                """
                {
                  "type": "plan",
                  "plan": [
                    {"step": "解析接口", "status": "completed"},
                    {"step": "构建 APK", "status": "pending"}
                  ]
                }
                """.trimIndent(),
            ),
        )!!

        assertEquals(SessionItemType.REASONING, reasoning.type)
        assertEquals(SessionItemType.FILE_CHANGE, files.type)
        assertEquals("app/src/Main.kt", files.files.single().path)
        assertEquals(5, files.files.single().added)
        assertEquals(2, plan.plan.size)
        assertEquals("completed", plan.plan.first().status)
    }

    @Test
    fun noiseToolContextIsHidden() {
        val item = SessionItem.fromJson(
            JSONObject(
                """
                {
                  "role": "user",
                  "type": "text",
                  "text": "<tools>\nRemoved deferred tool namespaces\n</tools>"
                }
                """.trimIndent(),
            ),
        )!!

        assertTrue(item.isNoise())
        assertTrue(appendSessionItem(SessionItemsState(), item).items.isEmpty())
    }

    @Test
    fun turnEventsToggleRunningAndKeepStartedAt() {
        val started = applyTurnToItems(
            SessionItemsState(),
            SessionTurn(state = "started", ts = "2026-11-08T15:00:00Z"),
        )
        assertTrue(started.running)
        assertEquals("2026-11-08T15:00:00Z", started.runningSince)

        val finished = applyTurnToItems(
            started,
            SessionTurn(state = "finished", ts = "2026-11-08T15:00:10Z"),
        )
        assertFalse(finished.running)
        assertEquals("", finished.runningSince)
    }

    @Test
    fun itemSseAppendsAndDeduplicatesById() {
        val first = SessionItem(
            id = "item-1",
            ts = "2026-11-08T15:00:00Z",
            type = SessionItemType.ASSISTANT,
            text = "第一段",
        )
        val updated = first.copy(text = "更新后的内容")

        val once = appendSessionItem(SessionItemsState(), first)
        val twice = appendSessionItem(once, updated)

        assertEquals(1, twice.items.size)
        assertEquals("更新后的内容", twice.items.single().text)
        assertTrue(twice.running)
    }

    @Test
    fun toolCallSseUpdatesInPlaceWhenResultArrives() {
        val running = SessionItem(
            id = "codex-tool-call-1",
            ts = "2026-11-08T15:00:00Z",
            type = SessionItemType.TOOL_CALL,
            tool = ItemTool(
                name = "exec_command",
                inputSummary = "echo test",
                status = "running",
            ),
        )
        val done = running.copy(
            tool = running.tool.copy(
                outputTail = "test",
                exitCode = 0,
                status = "done",
            ),
        )

        val afterStart = appendSessionItem(SessionItemsState(), running)
        val afterResult = appendSessionItem(afterStart, done)

        assertEquals(1, afterResult.items.size)
        assertEquals("codex-tool-call-1", afterResult.items.single().id)
        assertEquals("done", afterResult.items.single().tool.status)
        assertEquals("test", afterResult.items.single().tool.outputTail)
        assertEquals(0, afterResult.items.single().tool.exitCode)
    }

    @Test
    fun replyPayloadCarriesOptionalModelAndEffort() {
        val payload = buildReplyPayload(
            eventId = 9L,
            sessionId = "session-1",
            source = "codex",
            text = "继续",
            files = emptyList(),
            model = "gpt-5.6",
            effort = "high",
        )

        assertEquals("gpt-5.6", payload.getString("model"))
        assertEquals("high", payload.getString("effort"))
    }

    @Test
    fun replyPayloadOmitsBlankModelAndEffort() {
        val payload = buildReplyPayload(
            eventId = null,
            sessionId = "session-1",
            source = "claude",
            text = "继续",
            files = emptyList(),
            model = "",
            effort = null,
        )

        assertFalse(payload.has("model"))
        assertFalse(payload.has("effort"))
        assertNotNull(payload.getString("text"))
    }

    @Test
    fun replyPayloadCarriesPermissionMode() {
        val payload = buildReplyPayload(
            eventId = null,
            sessionId = "session-1",
            source = "claude",
            text = "继续",
            files = emptyList(),
            permissionMode = "plan",
        )

        assertEquals("plan", payload.getString("permission_mode"))
    }
}
