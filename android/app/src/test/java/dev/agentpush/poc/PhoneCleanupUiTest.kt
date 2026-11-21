package dev.agentpush.poc

import android.app.Application
import android.provider.Settings
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.test.core.app.ApplicationProvider
import dev.agentpush.poc.ui.screens.HomingInboxScreen
import dev.agentpush.poc.ui.screens.ProjectsScreen
import dev.agentpush.poc.ui.theme.AgentInboxTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
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
class PhoneCleanupUiTest {
    @get:Rule val compose = createComposeRule()
    private lateinit var viewModel: AgentViewModel
    private lateinit var original: AgentUiState

    @Before
    fun setUp() {
        val app: Application = ApplicationProvider.getApplicationContext()
        Settings.Global.putFloat(app.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 0f)
        viewModel = AgentViewModel(app)
        val now = Instant.now()
        val event = AgentEvent(
            id = 101, ts = now.toString(), source = "codex", kind = "done",
            sessionId = "demo-cleanup-session", cwd = "/demo/cleanup-workspace",
            project = "demo-cleanup", title = "Demo cleanup conversation",
            text = "demo completed result", read = false,
        )
        original = AgentUiState(
            settings = AppSettings("http://127.0.0.1:1", "demo-test-token"),
            connection = ConnectionState.CONNECTED,
            events = mapOf(event.id to event),
            sessionMeta = mapOf(event.sessionId to SessionMeta(
                sessionId = event.sessionId, source = "codex", title = event.title,
                cwd = event.cwd, project = event.project, status = "completed",
                lastTs = event.ts, lastPreview = event.text,
            )),
            mobileInbox = MobileInboxMode(enabled = true, sinceMillis = now.minusSeconds(60).toEpochMilli()),
            sessionApiAvailable = true,
        )
        AgentStore.get(app).replaceStateForTest(original)
    }

    @Test
    fun clearingInboxHidesRowsAndUndoRestoresWithoutMarkingReadOrArchiving() {
        compose.setContent {
            AgentInboxTheme {
                HomingInboxScreen(viewModel, onOpenSession = {}, onNewSession = {}, onOpenAllSessions = {})
            }
        }
        compose.onNodeWithTag("phone-clear-inbox").performClick()
        compose.onNodeWithText("Demo cleanup conversation").assertDoesNotExist()
        compose.onNodeWithTag("phone-clear-inbox").assertIsNotEnabled()
        assertRemoteSessionUnchanged()
        compose.onNodeWithTag("phone-cleanup-undo").performClick()
        compose.onNodeWithTag("homing-inbox").performScrollToNode(hasText("Demo cleanup conversation"))
        compose.onNodeWithText("Demo cleanup conversation").assertExists()
        assertRemoteSessionUnchanged()
    }

    @Test
    fun quickRemoveHidesOnlyPhoneRowAndRestoreMakesItVisibleAgain() {
        compose.setContent {
            AgentInboxTheme { ProjectsScreen(viewModel, onOpenSession = {}, onNewSession = {}) }
        }
        compose.onNodeWithTag("projects-list")
            .performScrollToNode(hasTestTag("session-row-demo-cleanup-session"))
        compose.onNodeWithTag("phone-hide-session-demo-cleanup-session").performClick()
        compose.onNodeWithTag("session-row-demo-cleanup-session").assertDoesNotExist()
        assertRemoteSessionUnchanged()
        compose.onNodeWithTag("phone-restore-sessions").performClick()
        compose.onNodeWithTag("projects-list")
            .performScrollToNode(hasTestTag("session-row-demo-cleanup-session"))
        compose.onNodeWithTag("session-row-demo-cleanup-session").assertExists()
        assertRemoteSessionUnchanged()
    }

    private fun assertRemoteSessionUnchanged() {
        val state = viewModel.state.value
        assertEquals(original.events, state.events)
        assertEquals(original.sessionMeta, state.sessionMeta)
        assertFalse(state.sessions().single().archived)
    }
}
