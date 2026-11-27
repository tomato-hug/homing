package dev.agentpush.poc

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import android.provider.Settings
import android.view.View
import androidx.compose.material3.SnackbarHostState
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import dev.agentpush.poc.ui.WorkspaceDrawerShell
import dev.agentpush.poc.ui.theme.AgentInboxTheme
import java.io.File
import java.time.Instant
import kotlinx.coroutines.runBlocking
import org.junit.Assume
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import org.robolectric.shadows.ShadowLooper

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class, qualifiers = "w412dp-h892dp-xxhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class ShowcaseScreenshotTest {
    @get:Rule
    val compose = createComposeRule()

    @Test
    fun captureSyntheticShowcaseScreenshots(): Unit = runBlocking {
        val outDirPath = System.getProperty("homing.captureScreenshots")
            ?: System.getenv("HOMING_CAPTURE_SCREENSHOTS")
        Assume.assumeTrue("Set HOMING_CAPTURE_SCREENSHOTS=<dir> to render showcase PNGs", !outDirPath.isNullOrBlank())

        val app: Application = ApplicationProvider.getApplicationContext()
        Settings.Global.putFloat(app.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 0f)
        app.getSharedPreferences("homing_workspace_prefs", 0).edit().clear().commit()
        app.getSharedPreferences("homing_session_flags", 0).edit().clear().commit()

        val store = AgentStore.get(app)
        val viewModel = AgentViewModel(app)

        val now = Instant.now()
        val codexTs = now.minusSeconds(20).toString()
        val claudeTs = now.minusSeconds(45).toString()
        val wbTs = now.minusSeconds(120).toString()

        val state = AgentUiState(
            settings = AppSettings("", ""),
            connection = ConnectionState.CONNECTED,
            transportKind = TransportKind.CLOUD,
            away = true,
            activeComputerKey = "demo-mac",
            mobileInbox = MobileInboxMode(
                enabled = true,
                sinceMillis = now.minusSeconds(600).toEpochMilli(),
            ),
            mobileWorkspace = MobileWorkspaceState(
                baselineReady = true,
                baselineMaxEventId = 100L,
                liveSessionIds = setOf("demo-codex-1", "demo-claude-2", "demo-workbuddy-3"),
            ),
            phoneConversations = PhoneConversationState(
                receipts = mapOf(
                    phoneConversationKey("demo-mac", "codex", "demo-codex-1") to PhoneReceipt(
                        eventId = 102L,
                        timestamp = now.minusSeconds(180).toString(),
                        receivedAt = now.minusSeconds(180).toEpochMilli(),
                    ),
                    phoneConversationKey("demo-mac", "claude", "demo-claude-2") to PhoneReceipt(
                        eventId = 101L,
                        timestamp = now.minusSeconds(240).toString(),
                        receivedAt = now.minusSeconds(240).toEpochMilli(),
                    ),
                ),
            ),
            agentDirectory = AgentDirectory(
                agents = mapOf(
                    "codex" to AgentProfile(
                        source = "codex",
                        displayName = "Codex",
                        available = true,
                        installed = true,
                        processRunning = true,
                        activeOnline = true,
                        models = listOf(
                            AgentModel(
                                id = "gpt-5.3-codex",
                                label = "gpt-5.3-codex",
                                provider = "openai",
                                isDefault = true,
                                efforts = listOf("low", "medium", "high"),
                            ),
                        ),
                        defaultModel = "gpt-5.3-codex",
                        capabilities = AgentCapabilities(reply = true, newSession = true, switchModel = true, stop = true),
                    ),
                    "claude" to AgentProfile(
                        source = "claude",
                        displayName = "Claude Code",
                        available = true,
                        installed = true,
                        processRunning = true,
                        activeOnline = true,
                        capabilities = AgentCapabilities(reply = true, newSession = true, switchModel = true, stop = true),
                    ),
                    "workbuddy" to AgentProfile(
                        source = "workbuddy",
                        displayName = "WorkBuddy",
                        available = true,
                        installed = true,
                        processRunning = true,
                        activeOnline = true,
                        capabilities = AgentCapabilities(reply = true, stop = true),
                    ),
                ),
                available = true,
            ),
            sessionMeta = mapOf(
                "demo-codex-1" to SessionMeta(
                    computerId = "demo-mac",
                    computerName = "Studio Mac",
                    sessionId = "demo-codex-1",
                    source = "codex",
                    title = "Split-Horizon 双栈直连优化",
                    titleSource = "agent",
                    project = "demo-relay-core",
                    branch = "main",
                    cwd = "/workspace/demo-relay-core",
                    model = "gpt-5.3-codex",
                    effort = "high",
                    status = "completed",
                    lastTs = codexTs,
                    lastPreview = "已完成 TUN Fake-IP 直连穿透与 1-RTT 重连同步",
                    contextUsage = ContextUsage(used = 64200, limit = 258400),
                    unread = 0,
                ),
                "demo-claude-2" to SessionMeta(
                    computerId = "demo-mac",
                    computerName = "Studio Mac",
                    sessionId = "demo-claude-2",
                    source = "claude",
                    title = "审核服务端并发测试命令",
                    titleSource = "agent",
                    project = "demo-api-gateway",
                    branch = "feat/quota",
                    cwd = "/workspace/demo-api-gateway",
                    model = "claude-sonnet-4-5",
                    status = "needs_permission",
                    lastTs = claudeTs,
                    lastPreview = "允许执行 go test -race ./internal/server/ ?",
                    unread = 1,
                ),
                "demo-workbuddy-3" to SessionMeta(
                    computerId = "demo-mac",
                    computerName = "Studio Mac",
                    sessionId = "demo-workbuddy-3",
                    source = "workbuddy",
                    title = "生成移动端暗色主题样式校验",
                    titleSource = "agent",
                    project = "demo-mobile-ui",
                    branch = "main",
                    cwd = "/workspace/demo-mobile-ui",
                    status = "completed",
                    lastTs = wbTs,
                    lastPreview = "3 项深色对比度对比已通过校验",
                    unread = 0,
                ),
            ),
            events = mapOf(
                101L to AgentEvent(
                    id = 101L,
                    ts = claudeTs,
                    source = "claude",
                    kind = "permission",
                    sessionId = "demo-claude-2",
                    cwd = "/workspace/demo-api-gateway",
                    project = "demo-api-gateway",
                    title = "请求权限确认",
                    text = "允许执行 go test -race ./internal/server/ ?",
                    read = false,
                ),
                102L to AgentEvent(
                    id = 102L,
                    ts = codexTs,
                    source = "codex",
                    kind = "reply",
                    sessionId = "demo-codex-1",
                    cwd = "/workspace/demo-relay-core",
                    project = "demo-relay-core",
                    title = "Split-Horizon 双栈直连优化",
                    text = "已完成 **Split-Horizon 双栈直连** 验证：\n\n- 开发机模型面保留本机代理直通海外 API\n- 控制面自动穿透 `198.18.0.0/15` TUN Fake-IP 直连国内公网中继\n- 手机纯 5G 零代理断网重连已在 **1-RTT** 内自动补齐状态",
                    read = true,
                ),
            ),
            sessionItems = mapOf(
                "demo-codex-1" to SessionItemsState(
                    items = listOf(
                        SessionItem(
                            id = "demo-item-user",
                            ts = now.minusSeconds(55).toString(),
                            type = SessionItemType.USER,
                            role = "user",
                            text = "确认下电脑开着 TUN 代理时，中继连接器有没有绕过 Fake-IP 直连？",
                        ),
                        SessionItem(
                            id = "demo-item-files",
                            ts = now.minusSeconds(30).toString(),
                            type = SessionItemType.FILE_CHANGE,
                            files = listOf(
                                ItemFileChange("internal/cloud/direct_dial.go", added = 42, removed = 6),
                                ItemFileChange("internal/cloud/connector.go", added = 18, removed = 4),
                            ),
                        ),
                        SessionItem(
                            id = "demo-item-assistant",
                            ts = codexTs,
                            type = SessionItemType.ASSISTANT,
                            role = "assistant",
                            text = "已完成 **Split-Horizon 双栈直连** 验证：\n\n- 开发机模型面保留本机代理直通海外 API\n- 控制面自动穿透 `198.18.0.0/15` TUN Fake-IP 直连国内公网中继\n- 手机纯 5G 零代理断网重连已在 **1-RTT** 内自动补齐状态",
                        ),
                    ),
                    available = true,
                    running = false,
                ),
            ),
            sessionApiAvailable = true,
        )

        store.replaceStateForTest(state)

        lateinit var rootView: View
        compose.setContent {
            rootView = LocalView.current
            AgentInboxTheme {
                WorkspaceDrawerShell(
                    viewModel = viewModel,
                    state = state,
                    hasConfiguredTarget = true,
                    snackbarHostState = SnackbarHostState(),
                    requestedSessionId = "demo-codex-1",
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
        ShadowLooper.idleMainLooper()

        val outDir = File(outDirPath)
        outDir.mkdirs()

        fun renderViewToPng(view: View, file: File) {
            compose.runOnIdle {
                val target = view.rootView ?: view
                val w = target.width.coerceAtLeast(1)
                val h = target.height.coerceAtLeast(1)
                val bmp = Bitmap.createBitmap(w, h, Bitmap.Config.ARGB_8888)
                val canvas = Canvas(bmp)
                target.draw(canvas)
                file.outputStream().use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
                bmp.recycle()
            }
        }

        renderViewToPng(rootView, File(outDir, "mobile-session.png"))

        compose.onNodeWithTag("open-workspace-drawer").performClick()
        compose.waitForIdle()
        ShadowLooper.idleMainLooper()

        renderViewToPng(rootView, File(outDir, "mobile-drawer.png"))
    }
}
