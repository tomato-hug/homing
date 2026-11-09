package dev.agentpush.poc

import android.app.Application
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToIndex
import androidx.test.core.app.ApplicationProvider
import dev.agentpush.poc.ui.screens.LastCrashCard
import dev.agentpush.poc.ui.screens.SessionDetailContent
import dev.agentpush.poc.ui.theme.AgentInboxTheme
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.time.Instant

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class SessionDetailRenderTest {
    @get:Rule
    val compose = createComposeRule()

    @Test
    fun rendersEveryRealRelaySessionWithoutCrash() {
        val state = loadFixtureState()
        val sessions = state.sessions()
        assertEquals(9, sessions.size)
        var currentSessionId by mutableStateOf(sessions.first().sessionId)

        compose.setContent {
            AgentInboxTheme {
                SessionDetailContent(
                    state = state,
                    sessionId = currentSessionId,
                    snackbarHostState = SnackbarHostState(),
                    onBack = {},
                    onOpenBrowser = { _, _ -> },
                )
            }
        }

        for (session in sessions) {
            compose.runOnIdle { currentSessionId = session.sessionId }
            compose.waitForIdle()
            compose.onNodeWithTag("session-title").assertExists()
            compose.onNodeWithTag("session-composer").assertExists()
            compose.onNodeWithTag("model-pill").assertExists()

            val usage = session.contextUsage
            if (usage != null && usage.used > 0) {
                if (usage.limit > 0) {
                    compose.onNodeWithTag("context-usage-bar").assertExists()
                } else {
                    compose.onNodeWithTag("context-usage-pill").assertExists()
                }
            }

            val itemCount = state.sessionItems[session.sessionId]?.items?.size ?: 0
            if (itemCount > 1) {
                compose.onNodeWithTag("session-timeline")
                    .performScrollToIndex(itemCount - 1)
                compose.waitForIdle()
            }
        }
    }

    @Test
    fun rendersSessionScreenBeforeSessionDataArrives() {
        val empty = AgentUiState(
            settings = AppSettings("http://127.0.0.1:1", "test"),
        )
        compose.setContent {
            AgentInboxTheme {
                SessionDetailContent(
                    state = empty,
                    sessionId = "not-loaded-yet",
                    snackbarHostState = SnackbarHostState(),
                    onBack = {},
                    onOpenBrowser = { _, _ -> },
                )
            }
        }
        compose.waitForIdle()
        compose.onNodeWithText("会话").assertExists()
    }

    @Test
    fun duplicateSessionItemIdsRenderWithoutLazyKeyCollision() {
        val base = loadFixtureState().sessions().first()
        val duplicate = SessionItem(
            id = "duplicate-id",
            ts = "2026-11-08T12:00:00Z",
            type = SessionItemType.ASSISTANT,
            text = "第一条重复 id 的回复",
        )
        val state = stateWithSingleSession(
            base = base,
            items = listOf(
                duplicate,
                duplicate.copy(text = "第二条重复 id 的回复"),
            ),
            contextUsage = ContextUsage(used = 1234L, limit = 0L),
        )
        renderSingle(state, base.sessionId)
        compose.waitUntil(5_000) {
            compose.onAllNodesWithText("第二条重复 id 的回复", substring = true)
                .fetchSemanticsNodes()
                .isNotEmpty()
        }
        compose.onNodeWithText("第二条重复 id 的回复", substring = true).assertExists()
        compose.onNodeWithTag("context-usage-pill").assertExists()
    }

    @Test
    fun nullAndMissingItemFieldsRenderSafely() {
        val base = loadFixtureState().sessions().first()
        val items = listOf(
            SessionItem.fromJson(
                JSONObject()
                    .put("id", "user-null-model")
                    .put("ts", "2026-11-08T12:00:00Z")
                    .put("type", "user")
                    .put("text", "用户消息，model 为 null")
                    .put("model", JSONObject.NULL),
            )!!,
            SessionItem.fromJson(
                JSONObject()
                    .put("id", "tool-missing-object")
                    .put("ts", "2026-11-08T12:00:01Z")
                    .put("type", "tool_call")
                    .put("text", "没有 tool 对象"),
            )!!,
            SessionItem.fromJson(
                JSONObject()
                    .put("id", "approval-null-model")
                    .put("ts", "2026-11-08T12:00:02Z")
                    .put("type", "approval")
                    .put("text", "是否允许执行？")
                    .put("model", JSONObject.NULL)
                    .put("files", JSONArray()),
            )!!,
            SessionItem(
                id = "long-markdown",
                ts = "2026-11-08T12:00:03Z",
                type = SessionItemType.ASSISTANT,
                text = buildLongMarkdown(),
            ),
        )
        val state = stateWithSingleSession(
            base = base,
            items = items,
            contextUsage = ContextUsage(used = 9876L, limit = 0L),
        )
        renderSingle(state, base.sessionId)
        compose.onNodeWithText("用户消息，model 为 null", substring = true).assertExists()
        compose.onNodeWithTag("session-timeline").performScrollToIndex(items.lastIndex)
        compose.waitForIdle()
        compose.onNodeWithText("内容过长", substring = true).assertExists()
    }

    @Test
    fun sessionRouteTokensRoundTripSpecialIds() {
        val ids = listOf(
            "plain-session",
            "a/b:c d",
            "会话/../x",
            "uuid:1?query=2&x=3",
            "emoji-🧪-session",
        )
        for (id in ids) {
            val token = dev.agentpush.poc.ui.encodeToken(id)
            assertTrue("token 不得含路径分隔符: $token", !token.contains('/'))
            assertEquals(id, dev.agentpush.poc.ui.decodeToken(token))
        }
    }

    @Test
    fun lastCrashCardShowsAndExpandsPersistedReport() {
        val app = ApplicationProvider.getApplicationContext<Application>()
        CrashReporter.install(app)
        CrashReporter.clearLastReport()
        try {
            CrashReporter.setLastSession("session/crash-card")
            CrashReporter.record(IllegalStateException("detail render crash"), "unittest")
            compose.setContent {
                AgentInboxTheme {
                    LastCrashCard()
                }
            }
            compose.waitForIdle()
            compose.onNodeWithText("上次崩溃", substring = true).assertExists()
            compose.onNodeWithText("展开").performClick()
            compose.waitForIdle()
            compose.onNodeWithText("IllegalStateException", substring = true).assertExists()
            compose.onNodeWithText("复制").assertExists()
            compose.onNodeWithText("分享").assertExists()
        } finally {
            CrashReporter.clearLastReport()
        }
    }

    private fun renderSingle(state: AgentUiState, sessionId: String) {
        compose.setContent {
            AgentInboxTheme {
                SessionDetailContent(
                    state = state,
                    sessionId = sessionId,
                    snackbarHostState = SnackbarHostState(),
                    onBack = {},
                    onOpenBrowser = { _, _ -> },
                )
            }
        }
        compose.waitForIdle()
        compose.onNodeWithTag("session-title").assertExists()
    }

    private fun stateWithSingleSession(
        base: SessionSummary,
        items: List<SessionItem>,
        contextUsage: ContextUsage,
    ): AgentUiState {
        val meta = SessionMeta(
            sessionId = base.sessionId,
            source = base.brand.wire,
            title = base.title,
            titleSource = base.titleSource,
            workspace = base.workspace,
            project = base.project,
            branch = base.branch,
            cwd = base.cwd,
            model = base.model,
            status = base.status.wire,
            lastTs = base.lastTs,
            lastPreview = base.lastPreview,
            contextUsage = contextUsage,
            running = false,
        )
        return AgentUiState(
            settings = AppSettings("http://127.0.0.1:1", "test"),
            sessionMeta = mapOf(base.sessionId to meta),
            sessionItems = mapOf(
                base.sessionId to SessionItemsState(
                    items = items,
                    available = true,
                    running = false,
                ),
            ),
            agentDirectory = AgentDirectory(),
            sessionApiAvailable = true,
        )
    }

    private fun buildLongMarkdown(): String {
        return buildString {
            appendLine("# 长文档")
            appendLine()
            appendLine("| 列一 | 列二 |")
            appendLine("| --- | --- |")
            appendLine("| A | B |")
            appendLine()
            appendLine("- [x] 已完成")
            appendLine("- [ ] 未完成")
            appendLine()
            appendLine("> 引用 & <tag> \\\\ \"特殊字符\" 🧪")
            appendLine()
            appendLine("```kotlin")
            appendLine("val value = \"<>&\\\"\"")
            appendLine("```")
            appendLine()
            while (length < 90_000) {
                append("这是一段用于验证超长 Markdown 不会让详情页崩溃的文本。")
                appendLine()
            }
        }
    }

    private fun loadFixtureState(): AgentUiState {
        val text = javaClass.classLoader!!
            .getResourceAsStream("fixtures/relay-sessions.json")!!
            .bufferedReader(Charsets.UTF_8)
            .use { it.readText() }
        val root = JSONObject(text)
        val agents = parseAgentDirectory(root.getJSONObject("agents").toString())
        val sessionArray = root.getJSONArray("sessions")
        val meta = LinkedHashMap<String, SessionMeta>()
        val items = LinkedHashMap<String, SessionItemsState>()
        val transcripts = LinkedHashMap<String, TranscriptPage>()
        val events = LinkedHashMap<Long, AgentEvent>()

        for (index in 0 until sessionArray.length()) {
            val entry = sessionArray.getJSONObject(index)
            val sessionJson = entry.getJSONObject("session")
            val parsed = SessionMeta.fromJson(sessionJson)
            assertTrue(parsed.sessionId.isNotBlank())
            meta[parsed.sessionId] = parsed

            val itemArray = entry.getJSONObject("items").optJSONArray("items") ?: JSONArray()
            val parsedItems = buildList {
                for (itemIndex in 0 until itemArray.length()) {
                    itemArray.optJSONObject(itemIndex)?.let { itemJson ->
                        SessionItem.fromJson(itemJson, fallbackId = "${parsed.sessionId}-$itemIndex")
                            ?.let(::add)
                    }
                }
            }
            items[parsed.sessionId] = SessionItemsState(
                items = normalizeSessionItems(parsedItems),
                turns = emptyList(),
                available = true,
                loadedAt = System.currentTimeMillis(),
                running = parsed.running == true,
                runningSince = parsed.runningSince,
            )

            val messageArray = entry.getJSONObject("transcript").optJSONArray("messages")
                ?: JSONArray()
            transcripts[parsed.sessionId] = TranscriptPage(
                messages = buildList {
                    for (messageIndex in 0 until messageArray.length()) {
                        messageArray.optJSONObject(messageIndex)?.let {
                            add(TranscriptMessage.fromJson(it))
                        }
                    }
                },
            )

            val eventArray = entry.optJSONArray("events") ?: JSONArray()
            for (eventIndex in 0 until eventArray.length()) {
                eventArray.optJSONObject(eventIndex)?.let { eventJson ->
                    val event = AgentEvent.fromJson(eventJson)
                    if (event.id > 0) events[event.id] = event
                }
            }
        }

        assertNotNull(agents)
        assertTrue(events.isNotEmpty())
        return AgentUiState(
            settings = AppSettings("http://127.0.0.1:1", "test"),
            connection = ConnectionState.CONNECTED,
            events = events,
            sessionMeta = meta,
            agentDirectory = agents,
            sessionItems = items,
            sessionApiAvailable = true,
            transcriptCache = transcripts,
            lastUpdatedAt = Instant.now().toEpochMilli(),
        )
    }
}
