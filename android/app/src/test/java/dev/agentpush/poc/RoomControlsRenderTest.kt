package dev.agentpush.poc

import androidx.activity.ComponentActivity
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import dev.agentpush.poc.ui.screens.RoomControls
import dev.agentpush.poc.ui.theme.AgentInboxTheme
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class, qualifiers = "zh-rCN-w354dp-h792dp")
class RoomControlsRenderTest {
    @get:Rule val compose = createAndroidComposeRule<ComponentActivity>()
    private fun room(status: String) = Room.fromJson(JSONObject("""{
        "id":"room-demo","status":"$status","round":1,"rounds":2,"speaking":"codex",
        "participants":[{"agent":"codex","name":"Codex","status":"speaking"},{"agent":"claude","name":"Claude","status":"waiting"}]
    }"""))

    @Test fun callingWhilePausedSelectsNextParticipantWithoutResuming() {
        var called = ""
        val actions = mutableListOf<String>()
        compose.setContent { AgentInboxTheme {
            RoomControls(room("paused"), "", false, {}, {}, { actions += it }, onCall = { called = it })
        } }
        compose.onNodeWithTag("room-call").performClick()
        compose.onNodeWithText("Claude").performClick()
        assertEquals("claude", called)
        assertTrue(actions.isEmpty())
        compose.onNodeWithText("继续").assertExists()
    }

    @Test fun recoveredClosingRoomCanResumeMinutes() {
        var action = ""
        val closing = room("paused").copy(endRequested = true)
        compose.setContent { AgentInboxTheme { RoomControls(closing, "", false, {}, {}, { action = it }) } }
        compose.onNodeWithText("继续整理纪要").performClick()
        assertEquals("resume", action)
        compose.onNodeWithTag("room-input").assertDoesNotExist()
        compose.onNodeWithTag("room-call").assertDoesNotExist()
    }

    @Test fun summarizingDoesNotOfferAnotherTurnOrInput() {
        compose.setContent { AgentInboxTheme { RoomControls(room("summarizing"), "demo draft", false, {}, {}, {}) } }
        compose.onNodeWithTag("room-call").assertDoesNotExist()
        compose.onNodeWithTag("room-input").assertDoesNotExist()
        compose.onNodeWithTag("room-end").assertDoesNotExist()
    }
}
