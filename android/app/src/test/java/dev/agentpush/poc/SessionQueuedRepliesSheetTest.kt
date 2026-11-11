package dev.agentpush.poc

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import dev.agentpush.poc.ui.screens.SessionQueuedRepliesSheet
import dev.agentpush.poc.ui.theme.AgentInboxTheme
import org.junit.Assert.assertEquals
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
class SessionQueuedRepliesSheetTest {
    @get:Rule
    val compose = createComposeRule()

    @Test
    fun queuedReplyCanBeEditedOrWithdrawnIndividually() {
        val reply = AgentReply(
            id = 42,
            eventId = 7,
            sessionId = "session-demo",
            text = "修复登录按钮并补测试",
            ts = Instant.now().toString(),
            status = ReplyStatus.QUEUED,
            model = "gpt-test",
            files = listOf("/tmp/demo.png"),
        )
        var edited: AgentReply? = null
        var cancelled: AgentReply? = null

        compose.setContent {
            AgentInboxTheme {
                SessionQueuedRepliesSheet(
                    open = true,
                    replies = listOf(reply),
                    onDismiss = {},
                    onEdit = { edited = it },
                    onCancel = { cancelled = it },
                    onCancelAll = {},
                )
            }
        }
        compose.waitForIdle()

        compose.onNodeWithText("排队中的回复").assertExists()
        compose.onNodeWithText("修复登录按钮并补测试").assertExists()
        compose.onNodeWithText("编辑").performClick()
        compose.runOnIdle { assertEquals(reply, edited) }

        compose.onNodeWithText("撤回").performClick()
        compose.runOnIdle { assertEquals(reply, cancelled) }
    }
}
