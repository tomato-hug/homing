package dev.agentpush.poc

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import dev.agentpush.poc.ui.screens.HomeRunningStrip
import dev.agentpush.poc.ui.theme.AgentInboxTheme
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class, qualifiers = "w800dp-h600dp")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class PhoneHomeIdentityTest {
    @get:Rule val compose = createComposeRule()

    @Test
    fun runningCardsWithSameNativeIdSelectTheirExactComputerAndSource() {
        val first = SessionMeta(sessionId = "demo-shared-id", computerId = "demo-computer-a", source = "codex", project = "A")
        val second = first.copy(computerId = "demo-computer-b", project = "B")
        val third = first.copy(source = "claude", project = "C")
        var selected: SessionMeta? = null
        compose.setContent {
            AgentInboxTheme {
                HomeRunningStrip(listOf(first, second, third), now = 0, showComputer = false,
                    onSelectSession = { selected = it }, onOpenSession = { _, _ -> })
            }
        }
        listOf(first, second, third).forEach { session ->
            compose.onNodeWithText("${session.brand.displayName} · ${session.project}").performScrollTo().performClick()
            assertEquals(session, selected)
        }
    }
}
