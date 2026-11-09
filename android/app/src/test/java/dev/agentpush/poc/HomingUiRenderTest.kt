package dev.agentpush.poc

import android.app.Application
import android.provider.Settings
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performScrollToIndex
import androidx.test.core.app.ApplicationProvider
import dev.agentpush.poc.ui.screens.HomingInboxScreen
import dev.agentpush.poc.ui.screens.HomingSplashScreen
import dev.agentpush.poc.ui.screens.MeScreen
import dev.agentpush.poc.ui.screens.ProjectsScreen
import dev.agentpush.poc.ui.theme.AgentInboxTheme
import org.junit.Before
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
class HomingUiRenderTest {
    @get:Rule
    val compose = createComposeRule()

    private lateinit var app: Application
    private lateinit var viewModel: AgentViewModel

    @Before
    fun setUp() {
        app = ApplicationProvider.getApplicationContext()
        Settings.Global.putFloat(
            app.contentResolver,
            Settings.Global.ANIMATOR_DURATION_SCALE,
            0f,
        )
        app.getSharedPreferences("homing_session_flags", 0).edit().clear().commit()
        viewModel = AgentViewModel(app)
    }

    @Test
    fun splashRendersBrandAndConnectionCopy() {
        compose.setContent {
            AgentInboxTheme {
                HomingSplashScreen(
                    connection = ConnectionState.CONNECTED,
                    onFinished = {},
                )
            }
        }
        compose.onNodeWithText("Homing").assertExists()
        compose.onNodeWithText("每个 agent 的回复，都能飞回你手里。").assertExists()
        compose.onNodeWithText("正在连接 我的 Mac…").assertExists()
    }

    @Test
    fun inboxRendersPrioritiesRunningAndCompleted() {
        installState(sampleState())
        compose.setContent {
            AgentInboxTheme {
                HomingInboxScreen(
                    viewModel = viewModel,
                    onOpenSession = {},
                    onNewSession = {},
                    onOpenAllSessions = {},
                )
            }
        }
        compose.onNodeWithText("3 件事等你").assertExists()
        compose.onNodeWithText("另有 1 个在运行").assertExists()
        compose.onNodeWithText("需要授权的标题").assertExists()
        compose.onNodeWithText("等你回复").assertExists()
        compose.onNodeWithTag("homing-inbox").performScrollToIndex(7)
        compose.onNodeWithText("刚完成").assertExists()
        compose.onNodeWithTag("homing-inbox").performScrollToIndex(8)
        compose.onNodeWithText("改了 2 个文件", substring = true).assertExists()
        compose.onNodeWithTag("new-session-fab").assertExists()
    }

    @Test
    fun inboxUsesServerFeedCountsAndSections() {
        installState(
            sampleState().copy(
                inbox = InboxFeed(
                    waiting = listOf(
                        InboxEntry(
                            kind = InboxEntryKind.APPROVAL,
                            sessionId = "session-permission",
                            read = false,
                        ),
                    ),
                    running = emptyList(),
                    recentDone = listOf(
                        InboxEntry(
                            kind = InboxEntryKind.DONE,
                            sessionId = "session-completed",
                            read = false,
                        ),
                    ),
                    waitingCount = 9,
                    runningCount = 2,
                    available = true,
                ),
            ),
        )
        compose.setContent {
            AgentInboxTheme {
                HomingInboxScreen(
                    viewModel = viewModel,
                    onOpenSession = {},
                    onNewSession = {},
                    onOpenAllSessions = {},
                )
            }
        }

        compose.onNodeWithText("9 件事等你").assertExists()
        compose.onNodeWithText("另有 2 个在运行").assertExists()
        compose.onNodeWithText("需要授权的标题").assertExists()
        compose.onNodeWithTag("homing-inbox").performScrollToIndex(4)
        compose.onNodeWithText("刚完成").assertExists()
    }

    @Test
    fun projectsGroupAndCollapseTemporarySessions() {
        installState(sampleState())
        compose.setContent {
            AgentInboxTheme {
                ProjectsScreen(viewModel = viewModel, onOpenSession = {})
            }
        }
        compose.onNodeWithText("按项目").assertExists()
        compose.onNodeWithText("按时间").assertExists()
        compose.onNodeWithText("project-a").assertExists()
        compose.onNodeWithTag("projects-list").performScrollToIndex(1)
        compose.onNodeWithText("临时会话 1 个").assertExists()
    }

    @Test
    fun meRendersAgentsRemindersAndVersion() {
        installState(sampleState())
        compose.setContent {
            AgentInboxTheme {
                MeScreen(viewModel = viewModel, onOpenSettings = {})
            }
        }
        compose.onNodeWithText("我的").assertExists()
        compose.onNodeWithText("重新配对").assertExists()
        compose.onNodeWithText("Codex").assertExists()
        compose.onNodeWithText("Homing 0.7.0").performScrollTo().assertExists()
        compose.onNodeWithText("OpenCode").performScrollTo().assertExists()
        compose.onNodeWithText("等你回复").performScrollTo().assertExists()
    }

    private fun installState(state: AgentUiState) {
        AgentStore.get(app).replaceStateForTest(state)
    }

    private fun sampleState(): AgentUiState {
        val now = Instant.now()
        val runningEvent = event(
            id = 1,
            source = "codex",
            kind = "reply",
            sessionId = "session-running",
            cwd = "/Users/me/project-a",
            title = "运行中的标题",
            text = "正在读取仓库结构",
            secondsAgo = 80,
        )
        val permissionEvent = event(
            id = 2,
            source = "claude",
            kind = "permission",
            sessionId = "session-permission",
            cwd = "/Users/me/project-a",
            title = "需要授权的标题",
            text = "rm -rf build && ./gradlew assembleDebug",
            secondsAgo = 60,
        )
        val waitingEvent = event(
            id = 3,
            source = "workbuddy",
            kind = "needs_input",
            sessionId = "session-waiting",
            cwd = "/Users/me/project-a",
            title = "等待回复的标题",
            text = "要保留旧的构建产物吗？",
            secondsAgo = 45,
        )
        val errorEvent = event(
            id = 4,
            source = "codex",
            kind = "error",
            sessionId = "session-error",
            cwd = "/Users/me/project-a",
            title = "出错的标题",
            text = "Gradle daemon 退出，退出码 1",
            secondsAgo = 30,
        )
        val completedEvent = event(
            id = 5,
            source = "claude",
            kind = "reply",
            sessionId = "session-completed",
            cwd = "/Users/me/project-a",
            title = "刚完成的标题",
            text = "已经更新了两个文件并通过测试",
            secondsAgo = 20,
        )
        val temporaryEvent = event(
            id = 6,
            source = "codex",
            kind = "reply",
            sessionId = "session-temp",
            cwd = "/tmp/homing-test",
            title = "临时测试会话",
            text = "测试完成",
            secondsAgo = 10,
        )
        val meta = listOf(
            sessionMeta(
                id = "session-running",
                source = "codex",
                title = "运行中的标题",
                cwd = "/Users/me/project-a",
                status = "running",
                running = true,
                runningSince = now.minusSeconds(80).toString(),
                lastTs = runningEvent.ts,
                preview = "正在读取仓库结构",
            ),
            sessionMeta(
                id = "session-permission",
                source = "claude",
                title = "需要授权的标题",
                cwd = "/Users/me/project-a",
                status = "needs_permission",
                lastTs = permissionEvent.ts,
                preview = "rm -rf build",
            ),
            sessionMeta(
                id = "session-waiting",
                source = "workbuddy",
                title = "等待回复的标题",
                cwd = "/Users/me/project-a",
                status = "waiting_input",
                lastTs = waitingEvent.ts,
                preview = "要保留旧的构建产物吗？",
            ),
            sessionMeta(
                id = "session-error",
                source = "codex",
                title = "出错的标题",
                cwd = "/Users/me/project-a",
                status = "error",
                lastTs = errorEvent.ts,
                preview = "Gradle daemon 退出",
            ),
            sessionMeta(
                id = "session-completed",
                source = "claude",
                title = "刚完成的标题",
                cwd = "/Users/me/project-a",
                status = "completed",
                lastTs = completedEvent.ts,
                preview = "已经更新了两个文件并通过测试",
            ),
            sessionMeta(
                id = "session-temp",
                source = "codex",
                title = "临时测试会话",
                cwd = "/tmp/homing-test",
                status = "completed",
                lastTs = temporaryEvent.ts,
                preview = "测试完成",
            ),
        ).associateBy { it.sessionId }
        return AgentUiState(
            settings = AppSettings("http://127.0.0.1:1", "test"),
            connection = ConnectionState.CONNECTED,
            events = listOf(
                runningEvent,
                permissionEvent,
                waitingEvent,
                errorEvent,
                completedEvent,
                temporaryEvent,
            ).associateBy { it.id },
            sessionMeta = meta,
            agentDirectory = AgentDirectory(
                agents = mapOf(
                    "codex" to AgentProfile("codex"),
                    "claude" to AgentProfile("claude"),
                    "workbuddy" to AgentProfile("workbuddy"),
                ),
                available = true,
            ),
            sessionItems = mapOf(
                "session-completed" to SessionItemsState(
                    items = listOf(
                        SessionItem(
                            id = "files-1",
                            ts = completedEvent.ts,
                            type = SessionItemType.FILE_CHANGE,
                            files = listOf(
                                ItemFileChange("README.md", added = 2, removed = 0),
                                ItemFileChange("Main.kt", added = 5, removed = 3),
                            ),
                        ),
                    ),
                    available = true,
                ),
            ),
            sessionApiAvailable = true,
        )
    }

    private fun event(
        id: Long,
        source: String,
        kind: String,
        sessionId: String,
        cwd: String,
        title: String,
        text: String,
        secondsAgo: Long,
    ): AgentEvent {
        return AgentEvent(
            id = id,
            ts = Instant.now().minusSeconds(secondsAgo).toString(),
            source = source,
            kind = kind,
            sessionId = sessionId,
            cwd = cwd,
            project = "project-a",
            title = title,
            text = text,
            read = false,
        )
    }

    private fun sessionMeta(
        id: String,
        source: String,
        title: String,
        cwd: String,
        status: String,
        running: Boolean = false,
        runningSince: String = "",
        lastTs: String,
        preview: String,
    ): SessionMeta {
        return SessionMeta(
            sessionId = id,
            source = source,
            title = title,
            titleSource = "custom",
            workspace = "me",
            project = "project-a",
            branch = "main",
            cwd = cwd,
            model = "gpt-test",
            status = status,
            lastTs = lastTs,
            lastPreview = preview,
            running = running,
            runningSince = runningSince,
        )
    }
}
