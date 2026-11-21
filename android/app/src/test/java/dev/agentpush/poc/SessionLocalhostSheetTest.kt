package dev.agentpush.poc

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import dev.agentpush.poc.ui.screens.SessionSheets
import dev.agentpush.poc.ui.theme.AgentInboxTheme
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class SessionLocalhostSheetTest {
    @get:Rule val compose = createComposeRule()

    @Test fun directHttpProxyPreservesQueryFragmentAndCapability() {
        var opened = ""
        show("http://localhost:3000/demo?q=hello%20world#section") { opened = it }
        compose.onNodeWithText("云端中转不转发电脑网页", substring = true).assertExists()
        compose.onNodeWithText("尝试直连代理").performClick()
        assertEquals("https://relay.example.org/p/3000/demo?q=hello%20world&cap=demo%2Bcap#section", opened)
    }

    @Test fun httpsLocalServiceCannotBeOpenedThroughHttpOnlyProxy() {
        show("https://localhost:3000/demo") {}
        compose.onNodeWithText("尝试直连代理").assertDoesNotExist()
        compose.onNodeWithText("此地址暂不支持端口代理", substring = true).assertExists()
    }

    private fun show(url: String, onOpen: (String) -> Unit) {
        compose.setContent { AgentInboxTheme {
            SessionSheets(
                session = null, modelPickerOpen = false, agentProfile = AgentDirectory().profile("codex"),
                effectiveModel = "", effectiveEffort = "", onDismissModelPicker = {}, onApplyModel = { _, _ -> },
                imageViewer = null, settings = AppSettings("https://relay.example.org", "demo-token"),
                onDismissImageViewer = {}, localhostPrompt = url, proxyCapability = "demo+cap",
                onDismissLocalhostPrompt = {}, onOpenBrowser = { target, _ -> onOpen(target) },
            )
        } }
    }
}
