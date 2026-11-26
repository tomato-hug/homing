package dev.agentpush.poc

import android.app.Application
import android.provider.Settings
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.getValue
import androidx.compose.ui.graphics.PathFillType
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.test.core.app.ApplicationProvider
import dev.agentpush.poc.ui.WorkspaceDrawerShell
import dev.agentpush.poc.ui.agentLogoDrawable
import dev.agentpush.poc.ui.agentLogoSpec
import dev.agentpush.poc.ui.hasAgentLogo
import dev.agentpush.poc.ui.linkifyFilePaths
import dev.agentpush.poc.ui.looksLikeFilePath
import dev.agentpush.poc.ui.decodeFileLink
import dev.agentpush.poc.ui.highlightLines
import dev.agentpush.poc.ui.theme.AgentInboxTheme
import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class WorkspaceDrawerAndBootstrapTest {
    @get:Rule
    val compose = createComposeRule()

    private lateinit var app: Application
    private lateinit var store: AgentStore
    private lateinit var viewModel: AgentViewModel

    @Before
    fun setUp() {
        app = ApplicationProvider.getApplicationContext()
        Settings.Global.putFloat(
            app.contentResolver,
            Settings.Global.ANIMATOR_DURATION_SCALE,
            0f,
        )
        app.getSharedPreferences("homing_workspace_prefs", 0).edit().clear().commit()
        store = AgentStore.get(app)
        viewModel = AgentViewModel(app)
    }

    @Test
    fun notifyPayloadParsesZeroRttPreviewAndPrePopulatesSession() {
        val raw = JSONObject()
            .put("type", "notify")
            .put("level", "P1")
            .put("source", "claude")
            .put("session_id", "demo-session-1")
            .put("event_id", 701L)
            .put("title", "修复移动端重连体验")
            .put("summary", "已完成修复并通过验证")
            .put("preview_text", "## 修复结果\n- 零延迟打开会话正文\n- 支持多 Agent 抽屉")
            .put("preview_truncated", false)
            .put("at", "2026-11-25T12:30:00.000Z")
            .put("kind", "reply")

        val payload = NotifyPayload.fromJson(raw)
        assertNotNull(payload)
        assertEquals("## 修复结果\n- 零延迟打开会话正文\n- 支持多 Agent 抽屉", payload!!.previewText)

        store.replaceStateForTest(
            AgentUiState(
                settings = AppSettings("http://127.0.0.1:18787", "demo-token"),
                activeComputerKey = "demo-host-1",
            ),
        )
        store.ingestNotificationPreview("demo-host-1", payload)

        val opened = store.state.value.phoneSession("demo-session-1")
        assertNotNull("session should be immediately available in 0-RTT before RPC", opened)
        assertEquals(1, opened!!.events.size)
        assertTrue(opened.events.first().text.contains("零延迟打开会话正文"))
    }

    @Test
    fun workspaceShellBootsIntoConversationAndFiltersAgentsInLeftDrawer() = runBlocking {
        val sample = AgentUiState(
            settings = AppSettings("http://127.0.0.1:18787", "demo-token"),
            connection = ConnectionState.CONNECTED,
            activeComputerKey = "demo-host-1",
            sessionMeta = mapOf(
                "demo-claude-1" to SessionMeta(
                    computerId = "demo-host-1",
                    sessionId = "demo-claude-1",
                    source = "claude",
                    title = "批准部署脚本",
                    titleSource = "agent",
                    project = "homing",
                    status = "needs_permission",
                    lastTs = "2026-11-25T12:10:00.000Z",
                    lastPreview = "请求执行 deploy.sh",
                    unread = 1,
                ),
                "demo-codex-2" to SessionMeta(
                    computerId = "demo-host-1",
                    sessionId = "demo-codex-2",
                    source = "codex",
                    title = "重构协议单元测试",
                    titleSource = "agent",
                    project = "homing-core",
                    status = "completed",
                    lastTs = "2026-11-25T12:20:00.000Z",
                    lastPreview = "29 个包全部通过",
                    unread = 0,
                ),
            ),
            events = mapOf(
                101L to AgentEvent(
                    id = 101L,
                    ts = "2026-11-25T12:10:00.000Z",
                    source = "claude",
                    kind = "permission",
                    sessionId = "demo-claude-1",
                    cwd = "/tmp/homing",
                    project = "homing",
                    title = "批准部署脚本",
                    text = "请求执行 deploy.sh",
                    read = false,
                ),
                102L to AgentEvent(
                    id = 102L,
                    ts = "2026-11-25T12:20:00.000Z",
                    source = "codex",
                    kind = "reply",
                    sessionId = "demo-codex-2",
                    cwd = "/tmp/homing-core",
                    project = "homing-core",
                    title = "重构协议单元测试",
                    text = "29 个包全部通过",
                    read = true,
                ),
            ),
        )
        store.replaceStateForTest(sample)

        compose.setContent {
            AgentInboxTheme {
                WorkspaceDrawerShell(
                    viewModel = viewModel,
                    state = sample,
                    hasConfiguredTarget = true,
                    snackbarHostState = SnackbarHostState(),
                    requestedSessionId = "demo-codex-2",
                    onRequestedSessionConsumed = {},
                    onNewSession = {},
                    onAddComputer = {},
                    onOpenRooms = {},
                    onOpenMe = {},
                    onOpenSettings = {},
                    onOpenBrowser = { _, _ -> },
                )
            }
        }
        compose.waitForIdle()

        // 1. Boots directly into requested conversation with cross-agent urgent jump pill
        compose.onNodeWithText("重构协议单元测试").assertExists()
        compose.onNodeWithTag("urgent-jump-pill").assertExists()
        compose.onNodeWithText("1 待审 ›").assertExists()

        // 2. Opens Left Drawer via top-left hamburger button
        compose.onNodeWithTag("open-workspace-drawer").performClick()
        compose.waitForIdle()

        compose.onNodeWithTag("workspace-drawer").assertExists()
        compose.onNodeWithTag("drawer-urgent-section").assertExists()
        compose.onNodeWithText("待你处理 (1)").assertExists()
        compose.onNodeWithText("允许").assertExists()

        // 3. Filters sessions by Agent capsule ("Codex 1")
        compose.onNodeWithText("Codex 1").performClick()
        compose.waitForIdle()
        compose.onNodeWithText("待你处理 (1)").assertDoesNotExist()
    }

    @Test
    fun allSupportedAgentsHaveValidBrandLogosAndProperFillRules() {
        val supportedIds = listOf(
            "claude", "claude-code", "codex", "workbuddy", "kimi", "opencode",
            "zcode", "cursor", "windsurf", "trae", "zed", "antigravity",
            "gemini-cli", "qwen-code", "copilot-cli", "dsh", "codebuddy", "kiro",
            "openclaw", "goose", "roo-code", "cline", "amp", "openhands",
            "kilo", "junie", "minimax-design", "warp", "aider", "droid",
        )
        for (id in supportedIds) {
            assertTrue("Expected a brand logo for agent '$id'", hasAgentLogo(id))
        }
        // Official full-color rasters (gradients / masks preserved) for the agents
        // whose hand-written single-color vectors previously rendered wrong.
        for (id in listOf("codex", "claude", "workbuddy", "kimi", "dsh", "gemini-cli", "cursor", "trae", "antigravity")) {
            assertNotNull("Expected official raster logo for '$id'", agentLogoDrawable(id))
        }
        assertEquals(agentLogoDrawable("claude"), agentLogoDrawable("claude-code"))
        // Vector fallback: Codex's ">_" glyph is a cut-out and must use EvenOdd.
        assertTrue(agentLogoSpec("codex")!!.parts.all { it.evenOdd })
        assertTrue(agentLogoSpec("cursor")!!.parts.first().evenOdd)
        assertTrue(PathFillType.NonZero != PathFillType.EvenOdd)
    }

    @Test
    fun closeResetKeepsRunningOnlyAndQuickClearAndRestoreLastTenRecordsWork() = runBlocking {
        val server = MockWebServer()
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val path = request.path.orEmpty()
                return when {
                    path.startsWith("/api/sessions/demo-finished-history/items") -> {
                        // Return 15 items; client must cap/restore the latest 10 items
                        val arr = JSONArray()
                        for (i in 1..15) {
                            arr.put(
                                JSONObject()
                                    .put("id", "item-$i")
                                    .put("item_key", "key-$i")
                                    .put("ts", "2026-11-25T10:${String.format("%02d", i)}:00.000Z")
                                    .put("type", if (i % 2 == 1) "user" else "assistant")
                                    .put("role", if (i % 2 == 1) "user" else "assistant")
                                    .put("text", "历史对话消息 #$i"),
                            )
                        }
                        MockResponse().setResponseCode(200).setBody(
                            JSONObject()
                                .put("session_id", "demo-finished-history")
                                .put("source", "claude")
                                .put("items", arr)
                                .toString(),
                        )
                    }
                    path.startsWith("/api/sessions/demo-finished-history") -> {
                        MockResponse().setResponseCode(200).setBody(
                            JSONObject()
                                .put("session_id", "demo-finished-history")
                                .put("source", "claude")
                                .put("title", "昨天的重构方案")
                                .put("status", "completed")
                                .toString(),
                        )
                    }
                    path == "/api/agents/workbuddy/wake" -> {
                        MockResponse().setResponseCode(200).setBody(
                            JSONObject()
                                .put("ok", true)
                                .put("source", "workbuddy")
                                .put("display_name", "WorkBuddy")
                                .put("wake_mode", "app")
                                .put("process_running", true)
                                .put("active_online", true)
                                .put("message", "已在电脑后台打开 WorkBuddy")
                                .toString(),
                        )
                    }
                    else -> MockResponse().setResponseCode(404).setBody("""{"error":"not found"}""")
                }
            }
        }
        server.start()
        try {
            val baseUrl = server.url("/").toString().trimEnd('/')
            AgentApi.bindTransport(null, null)
            val directory = AgentDirectory(
                agents = mapOf(
                    "codex" to AgentProfile(
                        source = "codex",
                        displayName = "Codex",
                        available = true,
                        installed = true,
                        processRunning = true,
                        activeOnline = true,
                        canWake = true,
                        wakeMode = "cli",
                    ),
                    "claude" to AgentProfile(
                        source = "claude",
                        displayName = "Claude",
                        available = true,
                        installed = true,
                        processRunning = false,
                        activeOnline = false,
                        canWake = true,
                        wakeMode = "cli",
                    ),
                    "workbuddy" to AgentProfile(
                        source = "workbuddy",
                        displayName = "WorkBuddy",
                        available = false,
                        installed = true,
                        processRunning = false,
                        activeOnline = false,
                        canWake = true,
                        wakeMode = "app",
                    ),
                ),
                available = true,
            )

            val initial = AgentUiState(
                settings = AppSettings(baseUrl, "demo-token"),
                connection = ConnectionState.CONNECTED,
                activeComputerKey = "demo-host-1",
                agentDirectory = directory,
                sessionMeta = mapOf(
                    "demo-running-1" to SessionMeta(
                        computerId = "demo-host-1",
                        sessionId = "demo-running-1",
                        source = "codex",
                        title = "后台正在编译内核",
                        titleSource = "agent",
                        project = "homing",
                        status = "running",
                        running = true,
                        lastTs = "2026-11-25T11:00:00.000Z",
                    ),
                    "demo-finished-history" to SessionMeta(
                        computerId = "demo-host-1",
                        sessionId = "demo-finished-history",
                        source = "claude",
                        title = "昨天的重构方案",
                        titleSource = "agent",
                        project = "homing",
                        status = "completed",
                        running = false,
                        lastTs = "2026-11-25T10:15:00.000Z",
                    ),
                    "demo-codex-done" to SessionMeta(
                        computerId = "demo-host-1",
                        sessionId = "demo-codex-done",
                        source = "codex",
                        title = "已完成的脚本优化",
                        titleSource = "agent",
                        project = "homing",
                        status = "completed",
                        running = false,
                        lastTs = "2026-11-25T10:30:00.000Z",
                    ),
                ),
                events = mapOf(
                    201L to AgentEvent(
                        id = 201L,
                        ts = "2026-11-25T11:00:00.000Z",
                        source = "codex",
                        kind = "reply",
                        sessionId = "demo-running-1",
                        cwd = "/tmp/homing",
                        project = "homing",
                        title = "后台正在编译内核",
                        text = "正在执行构建…",
                        read = false,
                    ),
                    202L to AgentEvent(
                        id = 202L,
                        ts = "2026-11-25T10:15:00.000Z",
                        source = "claude",
                        kind = "reply",
                        sessionId = "demo-finished-history",
                        cwd = "/tmp/homing",
                        project = "homing",
                        title = "昨天的重构方案",
                        text = "已完成",
                        read = true,
                    ),
                    203L to AgentEvent(
                        id = 203L,
                        ts = "2026-11-25T10:30:00.000Z",
                        source = "codex",
                        kind = "reply",
                        sessionId = "demo-codex-done",
                        cwd = "/tmp/homing",
                        project = "homing",
                        title = "已完成的脚本优化",
                        text = "优化完毕",
                        read = true,
                    ),
                ),
            )
            store.replaceStateForTest(initial)

            // Simulate closing Homing and reopening: only running session survives!
            store.markAppClosedForWorkspaceReset()
            store.onAppOpened(requestedSessionId = null)

            compose.setContent {
                val liveState by viewModel.state.collectAsStateWithLifecycle()
                AgentInboxTheme {
                    WorkspaceDrawerShell(
                        viewModel = viewModel,
                        state = liveState,
                        hasConfiguredTarget = true,
                        snackbarHostState = SnackbarHostState(),
                        requestedSessionId = null,
                        onRequestedSessionConsumed = {},
                        onNewSession = {},
                        onAddComputer = {},
                        onOpenRooms = {},
                        onOpenMe = {},
                        onOpenSettings = {},
                        onOpenBrowser = { _, _ -> },
                    )
                }
            }
            compose.waitForIdle()
            fun inDrawer(text: String) = compose.onAllNodes(
                hasText(text) and hasAnyAncestor(hasTestTag("drawer-session-list")),
            )

            // Running session is kept on reopen; finished historical sessions are cleared!
            compose.onAllNodesWithText("后台正在编译内核").onFirst().assertExists()
            compose.onNodeWithTag("header-quick-clear").assertExists()
            // Top horizontal sibling-session switcher bar is removed per mobile UX design.
            compose.onNodeWithTag("project-session-switcher").assertDoesNotExist()

            // Open Drawer: inactive WorkBuddy & Claude are NOT in active capsules; Codex IS active
            compose.onNodeWithTag("open-workspace-drawer").performClick()
            compose.waitForIdle()
            compose.onNodeWithText("Codex 1").assertExists()
            compose.onNodeWithText("WorkBuddy").assertDoesNotExist()
            inDrawer("昨天的重构方案").assertCountEquals(0)
            inDrawer("已完成的脚本优化").assertCountEquals(0)

            // Restore 1 historical session -> loads latest 10 items (#6..#15)
            val restoredCount = store.restoreHistoricalSessions(listOf("demo-finished-history"), itemLimit = 10)
            assertEquals(1, restoredCount)
            val restoredItems = store.state.value.sessionItems["demo-finished-history"]?.items.orEmpty()
            assertEquals("Should restore exactly the latest 10 items", 10, restoredItems.size)
            assertEquals("历史对话消息 #6", restoredItems.first().text)
            assertEquals("历史对话消息 #15", restoredItems.last().text)
            // Restoring history must not mark Claude as woken.
            assertFalse(store.state.value.isAgentActiveOnMac("claude"))

            compose.waitForIdle()
            // Both sessions share /tmp/homing -> merged into one project entry that expands.
            inDrawer("homing").onFirst().assertExists()
            inDrawer("昨天的重构方案").onFirst().assertExists()

            // Quick-clear in Drawer clears restored finished session while keeping running session!
            compose.onNodeWithTag("drawer-quick-clear").performClick()
            compose.waitForIdle()
            inDrawer("昨天的重构方案").assertCountEquals(0)
            compose.onAllNodesWithText("后台正在编译内核").onFirst().assertExists()

            // Undo restores it right back!
            assertTrue(store.undoWorkspaceCleanup())
            compose.waitForIdle()
            inDrawer("昨天的重构方案").onFirst().assertExists()

            // Remote Wake brings WorkBuddy online
            val wakeRes = store.wakeAgent("workbuddy")
            assertTrue(wakeRes.ok)
            assertTrue(store.state.value.agentDirectory.profile("workbuddy").activeOnline)
            assertFalse(store.state.value.mobileWorkspace.wokenAgentIds.isEmpty())
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun projectGroupingMergesSessionsAndFilePathsBecomeViewerLinks() {
        fun summary(id: String, cwd: String, ts: String, running: Boolean = false) = SessionSummary(
            sessionId = id, source = "codex", brand = AgentBrand.CODEX, title = id, titleSource = "agent",
            workspace = "", project = cwd.substringAfterLast('/'), branch = "", model = "", cwd = cwd,
            status = if (running) SessionStatus.RUNNING else SessionStatus.COMPLETED,
            latestEvent = null, events = emptyList(), replies = emptyList(), unreadCount = 0,
            lastPreview = "", lastTs = ts, running = running,
        )
        val groups = groupSessionsByProject(
            listOf(
                summary("demo-a1", "/tmp/demo-alpha", "2026-11-25T10:00:00Z"),
                summary("demo-a2", "/tmp/demo-alpha/", "2026-11-25T11:00:00Z", running = true),
                summary("demo-b1", "/tmp/demo-beta", "2026-11-25T12:00:00Z"),
            ),
        )
        assertEquals(2, groups.size)
        val alpha = groups.first { it.name == "demo-alpha" }
        assertEquals(2, alpha.sessions.size)
        assertEquals("demo-a2", alpha.primary.sessionId) // running first
        assertEquals("demo-alpha", groups.first().name) // running project sorts first

        assertTrue(looksLikeFilePath("internal/api/fs_read.go:42"))
        assertTrue(looksLikeFilePath("README.md"))
        assertFalse(looksLikeFilePath("v1.2.3"))
        assertFalse(looksLikeFilePath("foo.bar()"))
        assertFalse(looksLikeFilePath("https://example.com/a.js"))
        val linked = linkifyFilePaths(
            "改动了 `android/app/Foo.kt` 和 /tmp/demo-alpha/src/main.go:12\n```\nkeep `src/a.kt` inside fence\n```\n[docs](https://example.com/x.md)",
        )
        assertTrue(linked.contains("[`android/app/Foo.kt`](homing-file:"))
        assertTrue(linked.contains("[/tmp/demo-alpha/src/main.go:12](homing-file:"))
        assertTrue(linked.contains("keep `src/a.kt` inside fence"))
        assertTrue(linked.contains("[docs](https://example.com/x.md)"))
        val encoded = Regex("homing-file:[^)]+").find(linked)!!.value
        assertEquals("android/app/Foo.kt", decodeFileLink(encoded))

        val highlighted = highlightLines("fun main() {\n  // hi\n  val s = \"x\"\n}", "kotlin")
        assertEquals(4, highlighted.size)
        assertTrue(highlighted[1].spanStyles.isNotEmpty())
    }

    @Test
    fun sponsorAndCreditsSheetsRenderWeChatAlipayQrAndOpenSourceCredits() {
        for (channel in dev.agentpush.poc.ui.SponsorChannel.entries) {
            assertNotNull(
                "Bundled QR bitmap missing for ${channel.id}",
                dev.agentpush.poc.ui.loadSponsorBitmap(app, channel.assetPath),
            )
        }
        var openSponsor = false
        var openCredits = false
        compose.setContent {
            AgentInboxTheme {
                dev.agentpush.poc.ui.AboutAndSponsorCard(
                    onOpenSponsor = { openSponsor = true },
                    onOpenCredits = { openCredits = true },
                )
                dev.agentpush.poc.ui.SponsorBottomSheet(onDismiss = {})
            }
        }
        compose.waitForIdle()
        compose.onNodeWithTag("settings-open-sponsor").performClick()
        assertTrue(openSponsor)
        compose.onNodeWithTag("settings-open-credits").performClick()
        assertTrue(openCredits)

        compose.onNodeWithTag("sponsor-bottom-sheet").assertExists()
        compose.onNodeWithText("保存微信赞赏码到相册").assertExists()
        compose.onNodeWithTag("sponsor-tab-alipay").performClick()
        compose.waitForIdle()
        compose.onNodeWithText("保存支付宝收款码到相册").assertExists()
    }

    @Test
    fun multiRomKeepAliveDetectsAllMajorVendorsAndBuildsSafeIntents() {
        assertEquals(
            RomKeepAliveProfile.HYPEROS_MIUI,
            KeepAliveHelper.detectRomProfile("Xiaomi", "Redmi", "OS2.0.4.0"),
        )
        assertEquals(
            RomKeepAliveProfile.HYPEROS_MIUI,
            KeepAliveHelper.detectRomProfile("Generic", "Android", "15") { key ->
                if (key == "ro.mi.os.version.name") "OS2.0" else ""
            },
        )
        assertEquals(
            RomKeepAliveProfile.HARMONY_EMUI,
            KeepAliveHelper.detectRomProfile("HUAWEI", "HUAWEI", "HarmonyOS 4.2"),
        )
        assertEquals(
            RomKeepAliveProfile.MAGICOS,
            KeepAliveHelper.detectRomProfile("HONOR", "HONOR", "MagicOS 9.0"),
        )
        assertEquals(
            RomKeepAliveProfile.ORIGINOS_FUNTOUCH,
            KeepAliveHelper.detectRomProfile("vivo", "iQOO", "OriginOS 5"),
        )
        assertEquals(
            RomKeepAliveProfile.COLOROS_OXYGEN,
            KeepAliveHelper.detectRomProfile("OnePlus", "OnePlus", "PJZ110_15.0"),
        )
        assertEquals(
            RomKeepAliveProfile.ONE_UI,
            KeepAliveHelper.detectRomProfile("samsung", "samsung", "UP1A.231005"),
        )
        assertEquals(
            RomKeepAliveProfile.FLYME,
            KeepAliveHelper.detectRomProfile("Meizu", "meizu", "Flyme 11"),
        )
        assertEquals(
            RomKeepAliveProfile.ROG_ZUI,
            KeepAliveHelper.detectRomProfile("asus", "ASUS", "WW_Phone"),
        )
        assertEquals(
            RomKeepAliveProfile.STOCK_ANDROID,
            KeepAliveHelper.detectRomProfile("Google", "google", "AP4A.250105"),
        )

        for (profile in RomKeepAliveProfile.entries) {
            assertTrue("Steps empty for ${profile.id}", profile.steps.isNotEmpty())
            assertTrue("Button label blank for ${profile.id}", profile.autoStartButtonLabel.isNotBlank())
            val intents = KeepAliveHelper.oemIntentsFor("dev.homing.app", profile)
            assertTrue("OEM intents empty for ${profile.id}", intents.isNotEmpty())
        }

        // Boot receiver must ignore unrelated intents and never crash without paired target.
        val receiver = KeepAliveBootReceiver()
        receiver.onReceive(app, android.content.Intent("dev.homing.UNRELATED"))
        receiver.onReceive(app, android.content.Intent(android.content.Intent.ACTION_BOOT_COMPLETED))
        receiver.onReceive(app, android.content.Intent(android.content.Intent.ACTION_MY_PACKAGE_REPLACED))
    }

    @Test
    fun workBuddyIdlePromptNoiseAndOldDiskSessionsAreFilteredAndAwayMonitorWorks() = runBlocking {
        // 1. Verify idle prompt noise & WorkBuddy/CodeBuddy brand normalization helpers
        assertTrue(isIdlePromptNoiseEvent("needs_input", "CodeBuddy is waiting for your input"))
        assertTrue(isIdlePromptNoiseEvent("needs_input", "Claude is waiting for your input."))
        assertFalse(isIdlePromptNoiseEvent("needs_input", "请确认打印双面还是单面？"))
        assertEquals(
            "WorkBuddy asks: 请确认打印参数",
            normalizeHookSourceBrand("workbuddy", "CodeBuddy asks: 请确认打印参数"),
        )

        val stateWithOldDiskAndWbNoise = AgentUiState(
            settings = AppSettings("http://127.0.0.1:18787", "demo-token"),
            connection = ConnectionState.CONNECTED,
            activeComputerKey = "demo-host-1",
            away = false,
            agentDirectory = AgentDirectory(
                agents = mapOf(
                    "codex" to AgentProfile(
                        source = "codex",
                        displayName = "Codex",
                        available = true,
                        installed = true,
                        processRunning = true,
                        activeOnline = true,
                        canWake = true,
                    ),
                    "workbuddy" to AgentProfile(
                        source = "workbuddy",
                        displayName = "WorkBuddy",
                        available = true,
                        installed = true,
                        processRunning = true,
                        activeOnline = true,
                        canWake = true,
                    ),
                ),
                available = true,
            ),
            sessionMeta = mapOf(
                "demo-disk-codex-old" to SessionMeta(
                    computerId = "demo-host-1",
                    sessionId = "demo-disk-codex-old",
                    source = "codex",
                    title = "8小时前的磁盘旧项目",
                    titleSource = "agent",
                    project = "legacy-proj",
                    status = "completed",
                    lastTs = "2026-11-26T01:00:00.000Z",
                ),
                "demo-wb-finished" to SessionMeta(
                    computerId = "demo-host-1",
                    sessionId = "demo-wb-finished",
                    source = "workbuddy",
                    title = "演示文档打印任务",
                    titleSource = "agent",
                    project = "demo-doc",
                    status = "needs_input",
                    lastTs = "2026-11-26T13:39:00.000Z",
                    lastPreview = "CodeBuddy is waiting for your input",
                ),
            ),
            events = mapOf(
                301L to AgentEvent(
                    id = 301L,
                    ts = "2026-11-26T13:38:00.000Z",
                    source = "workbuddy",
                    kind = "reply",
                    sessionId = "demo-wb-finished",
                    cwd = "/tmp/demo-doc",
                    project = "demo-doc",
                    title = "WorkBuddy 回复",
                    text = "任务已完成，队列已清空。",
                    read = false,
                ),
                302L to AgentEvent(
                    id = 302L,
                    ts = "2026-11-26T13:39:00.000Z",
                    source = "workbuddy",
                    kind = "needs_input",
                    sessionId = "demo-wb-finished",
                    cwd = "/tmp/demo-doc",
                    project = "demo-doc",
                    title = "WorkBuddy 等待输入",
                    text = "CodeBuddy is waiting for your input",
                    read = false,
                ),
            ),
        )

        // WorkBuddy session must resolve to COMPLETED (not WAITING_INPUT / urgent)
        val sessions = stateWithOldDiskAndWbNoise.sessions()
        val wb = sessions.first { it.sessionId == "demo-wb-finished" }
        assertEquals(SessionStatus.COMPLETED, wb.status)
        assertEquals(null, wb.pendingEvent)

        // Disk-only old Codex session (zero events) must NOT appear in visible workspace
        val diskOld = sessions.first { it.sessionId == "demo-disk-codex-old" }
        assertFalse(dev.agentpush.poc.ui.isSessionVisibleInWorkspace(diskOld, stateWithOldDiskAndWbNoise, sessions))

        // On app open with away=false, desk-side completed sessions stay out of mobile workspace
        store.replaceStateForTest(stateWithOldDiskAndWbNoise)
        store.markAppClosedForWorkspaceReset()
        store.onAppOpened(requestedSessionId = null)

        // New event arriving while away=false must NOT auto-surface into liveSessionIds
        store.ingestEvent(
            AgentEvent(
                id = 303L,
                ts = "2026-11-26T14:00:00.000Z",
                source = "codex",
                kind = "reply",
                sessionId = "demo-desk-codex",
                cwd = "/tmp/desk-proj",
                project = "desk-proj",
                title = "Codex 回复",
                text = "在电脑前刚跑完的一轮",
                read = false,
            ),
        )
        assertFalse("demo-desk-codex" in store.state.value.mobileWorkspace.liveSessionIds)

        // Turning ON away monitoring allows new away-time events to surface immediately!
        store.applyAwayUpdate(
            JSONObject()
                .put("enabled", true)
                .put(
                    "current",
                    JSONObject()
                        .put("id", "away-1")
                        .put("started_at", "2026-11-26T14:05:00.000Z")
                        .put("started_after_event_id", 303L),
                ),
        )
        assertTrue(store.state.value.away)
        store.ingestEvent(
            AgentEvent(
                id = 304L,
                ts = "2026-11-26T14:06:00.000Z",
                source = "codex",
                kind = "reply",
                sessionId = "demo-away-codex",
                cwd = "/tmp/away-proj",
                project = "away-proj",
                title = "离席期间完成的任务",
                text = "已处理完毕",
                read = false,
            ),
        )
        assertTrue("demo-away-codex" in store.state.value.mobileWorkspace.liveSessionIds)
    }
}
