package dev.agentpush.poc

import androidx.activity.ComponentActivity
import android.content.Context
import android.content.SharedPreferences
import androidx.test.core.app.ApplicationProvider
import androidx.compose.foundation.layout.Box
import androidx.compose.material3.Text
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.performTextReplacement
import dev.agentpush.poc.ui.screens.RoomControls
import dev.agentpush.poc.ui.screens.RoomActionDialog
import dev.agentpush.poc.ui.screens.RoomActionList
import dev.agentpush.poc.ui.theme.AgentInboxTheme
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
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

    @Test fun mentionPickerAddsAddressWithoutSending() {
        var selected = ""
        val actions = mutableListOf<String>()
        compose.setContent { AgentInboxTheme {
            RoomControls(room("paused"), "", false, {}, {}, { actions += it }, onMention = { selected = it })
        } }
        compose.onNodeWithTag("room-mention").performClick()
        compose.onNodeWithText("Claude").performClick()
        assertEquals("claude", selected)
        assertTrue(actions.isEmpty())
    }

    @Test fun unresolvedMessageBlocksNewSend() {
        compose.setContent { AgentInboxTheme {
            RoomControls(room("paused"), "demo draft", false, {}, {}, {}, pending = RoomPendingMessage("demo-id", "demo draft", "", RoomDelivery.Unknown))
        } }
        compose.onNodeWithText("发送结果待确认").assertExists()
        compose.onNodeWithTag("room-send").assertIsNotEnabled()
    }

    @Test fun clearingUnknownDeliveryRequiresAcknowledgement() {
        var dismissed = false
        compose.setContent { AgentInboxTheme {
            RoomControls(room("paused"), "demo draft", false, {}, {}, {},
                pending = RoomPendingMessage("demo-id", "demo draft", "", RoomDelivery.Unknown), onDismissPending = { dismissed = true })
        } }
        compose.onNodeWithText("确认后重新编辑").performClick()
        assertFalse(dismissed)
        compose.onNodeWithText("可能已经送达，重新发送可能产生重复。请先查看会议记录。").assertExists()
        compose.onNodeWithText("我已检查").performClick()
        assertTrue(dismissed)
    }

    @Test fun closedRoomKeepsUnconfirmedDraftAccessible() {
        compose.setContent { AgentInboxTheme {
            RoomControls(room("ended"), "demo retained draft", false, {}, {}, {},
                pending = RoomPendingMessage("demo-id", "demo retained draft", "", RoomDelivery.Unknown))
        } }
        compose.onNodeWithText("发送结果待确认").assertExists()
        compose.onNodeWithText("复制草稿").assertExists()
    }

    @Test fun clearingDeliveryAndDraftUsesOneSynchronousCommit() {
        val base = ApplicationProvider.getApplicationContext<Context>().getSharedPreferences("demo-room-state", Context.MODE_PRIVATE)
        base.edit().clear().putString("draft_demo", "demo text").putString("pending_demo", "demo pending").commit()
        var commits = 0; var applies = 0
        val tracked = object : SharedPreferences by base {
            override fun edit(): SharedPreferences.Editor {
                val delegate = base.edit()
                return object : SharedPreferences.Editor by delegate {
                    override fun commit(): Boolean { commits++; return delegate.commit() }
                    override fun apply() { applies++; delegate.apply() }
                    override fun remove(key: String?): SharedPreferences.Editor { delegate.remove(key); return this }
                    override fun putString(key: String?, value: String?): SharedPreferences.Editor { delegate.putString(key, value); return this }
                }
            }
        }
        assertTrue(completeRoomDelivery(tracked, "demo", "demo text", "demo text"))
        assertEquals(1, commits); assertEquals(0, applies)
        assertFalse(base.contains("pending_demo")); assertEquals("", base.getString("draft_demo", "missing"))
    }

    @Test fun actionDispatchRequiresAnExplicitConfirmation() {
        val action = RoomAction("action-1", "demo task", "", "suggested", "", "", "", "", "demo-base")
        var dispatched = ""
        compose.setContent { AgentInboxTheme { Box {
            Text("会议")
            RoomActionDialog(action, room("paused").participants, {}, { agent, text -> dispatched = "$agent:$text" })
        } } }
        compose.onNodeWithText("Codex").performClick()
        assertEquals("", dispatched)
        compose.onNodeWithText("确认派发").performClick()
        assertEquals("codex:demo task", dispatched)
    }

    @Test fun suggestedActionCanBeEditedBeforeReview() {
        val action = RoomAction("action-1", "demo task", "", "suggested", "", "", "", "")
        var reviewed = ""
        compose.setContent { AgentInboxTheme { RoomActionList(listOf(action), { reviewed = it.text }, {}) } }
        compose.onNodeWithTag("room-action-edit-action-1").performTextReplacement("demo revised")
        compose.onNodeWithText("派发行动项").performClick()
        assertEquals("demo revised", reviewed)
    }

    @Test fun completedActionShowsResultInRoom() {
        val action = RoomAction("action-1", "demo task", "codex", "completed", "demo-dispatch", "demo-session", "demo result", "")
        compose.setContent { AgentInboxTheme { RoomActionList(listOf(action), {}, {}) } }
        compose.onNodeWithText("demo result").assertExists()
        compose.onNodeWithText("查看任务会话").assertExists()
    }

    @Test fun summarizingDoesNotOfferAnotherTurnOrInput() {
        compose.setContent { AgentInboxTheme { RoomControls(room("summarizing"), "demo draft", false, {}, {}, {}) } }
        compose.onNodeWithTag("room-call").assertDoesNotExist()
        compose.onNodeWithTag("room-input").assertDoesNotExist()
        compose.onNodeWithTag("room-end").assertDoesNotExist()
    }
}
