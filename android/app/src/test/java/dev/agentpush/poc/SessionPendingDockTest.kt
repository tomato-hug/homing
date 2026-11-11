package dev.agentpush.poc

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import dev.agentpush.poc.ui.screens.SessionPendingDock
import dev.agentpush.poc.ui.theme.AgentInboxTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class SessionPendingDockTest {
    @get:Rule
    val compose = createComposeRule()

    @Test
    fun permissionCanBeApprovedWithoutOpeningTimelineCard() {
        val event = pendingEvent(
            id = 41L,
            kind = "permission",
            text = "是否允许执行 ./gradlew test？",
        )
        var decision: Triple<Long, String, String?>? = null
        compose.setContent {
            AgentInboxTheme {
                SessionPendingDock(
                    session = session(
                        event = event,
                        status = SessionStatus.NEEDS_PERMISSION,
                    ),
                    event = event,
                    expanded = false,
                    onToggleExpanded = {},
                    onDecision = { id, value, optionId ->
                        decision = Triple(id, value, optionId)
                    },
                    onReply = {},
                )
            }
        }

        compose.onNodeWithText("允许一次").performClick()
        compose.waitForIdle()

        assertEquals(Triple(41L, "allow", "allow_once"), decision)
    }

    @Test
    fun waitingForInputFocusesComposerThroughReplyAction() {
        val event = pendingEvent(
            id = 42L,
            kind = "needs_input",
            text = "要保留旧的构建产物吗？",
        )
        var replyRequested = false
        compose.setContent {
            AgentInboxTheme {
                SessionPendingDock(
                    session = session(
                        event = event,
                        status = SessionStatus.WAITING_INPUT,
                    ),
                    event = event,
                    expanded = false,
                    onToggleExpanded = {},
                    onDecision = { _, _, _ -> },
                    onReply = { replyRequested = true },
                )
            }
        }

        compose.onNodeWithText("输入回复").performClick()
        compose.waitForIdle()

        assertTrue(replyRequested)
    }

    private fun session(event: AgentEvent, status: SessionStatus): SessionSummary {
        return SessionSummary(
            sessionId = "demo-session-1",
            source = "codex",
            brand = AgentBrand.CODEX,
            title = "等待中的测试会话",
            titleSource = "agent",
            workspace = "agent-push-poc",
            project = "agent-push-poc",
            branch = "main",
            model = "gpt-test",
            cwd = "/tmp/homing-demo",
            status = status,
            latestEvent = event,
            events = listOf(event),
            replies = emptyList(),
            unreadCount = 1,
            lastPreview = event.text,
            lastTs = event.ts,
            capabilities = AgentCapabilities(approve = true),
        )
    }

    private fun pendingEvent(id: Long, kind: String, text: String): AgentEvent {
        return AgentEvent(
            id = id,
            ts = "2026-11-11T08:00:00Z",
            source = "codex",
            kind = kind,
            sessionId = "demo-session-1",
            cwd = "/tmp/homing-demo",
            project = "agent-push-poc",
            title = "等待中的测试会话",
            text = text,
            read = false,
        )
    }
}

