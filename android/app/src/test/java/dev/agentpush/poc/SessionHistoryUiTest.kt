package dev.agentpush.poc

import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertTextContains
import androidx.compose.ui.test.junit4.StateRestorationTester
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToIndex
import androidx.compose.ui.test.performTextInput
import dev.agentpush.poc.ui.screens.SessionDetailActions
import dev.agentpush.poc.ui.screens.SessionDetailContent
import dev.agentpush.poc.ui.screens.TimelineItem
import dev.agentpush.poc.ui.screens.foldAssistantFragments
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
class SessionHistoryUiTest {
    @get:Rule val compose = createComposeRule()
    private val sessionId = "demo-history-session"
    private val stamp = "2026-11-21T12:00:00Z"

    @Test fun earlierContextLoadsOnlyAfterExplicitTapAndDisablesWhileLoading() {
        var calls = 0
        var state by mutableStateOf(state())
        compose.setContent { AgentInboxTheme {
            SessionDetailContent(state, sessionId, SnackbarHostState(), {}, { _, _ -> },
                SessionDetailActions(loadEarlierContext = { assertEquals(sessionId, it); calls++ }))
        } }
        compose.onNodeWithTag("session-timeline").performScrollToIndex(0)
        assertEquals(0, calls)
        compose.onNodeWithText("载入前 10 轮对话").performClick()
        assertEquals(1, calls)
        compose.runOnIdle {
            val old = state.sessionItems.getValue(sessionId)
            state = state.copy(sessionItems = mapOf(sessionId to old.copy(history = old.history.copy(loading = true))))
        }
        compose.onNodeWithTag("load-earlier-context").assertIsNotEnabled()
    }

    @Test fun unavailableHistoryExplainsReasonAndDoesNotOfferFallbackFullTranscript() {
        val history = SessionHistoryState(managed = true, anchorEventId = 100, status = "unavailable", reason = "此 Agent 暂不支持按轮载入历史")
        val old = AgentEvent(1, "2026-11-20T12:00:00Z", "codex", "done", sessionId, "/tmp/demo-history", "demo", "demo", "demo old hidden event", true)
        val current = old.copy(id = 100, ts = stamp, text = "demo phone receipt")
        val state = state(emptyList(), history).copy(
            events = mapOf(1L to old, 100L to current),
            transcriptCache = mapOf(sessionId to TranscriptPage(listOf(
                TranscriptMessage(role = "user", text = "demo hidden legacy transcript", ts = old.ts),
            ))),
        )
        compose.setContent { AgentInboxTheme {
            SessionDetailContent(state, sessionId, SnackbarHostState(), {}, { _, _ -> })
        } }
        compose.onNodeWithTag("session-timeline").performScrollToIndex(0)
        compose.onNodeWithText("此 Agent 暂不支持按轮载入历史").assertExists()
        compose.onNodeWithTag("load-earlier-context").assertIsNotEnabled()
        assertEquals(listOf(100L), state.phoneSession(sessionId)?.events?.map { it.id })
        compose.onNodeWithTag("session-timeline").performScrollToIndex(1)
        // Markdown parses asynchronously; wait for rendered content, not an arbitrary sleep.
        compose.waitUntil(timeoutMillis = 3_000) {
            compose.onAllNodesWithText("demo phone receipt").fetchSemanticsNodes().isNotEmpty()
        }
        compose.onNodeWithText("demo phone receipt").assertExists()
        compose.onNodeWithText("demo old hidden event").assertDoesNotExist()
        compose.onNodeWithText("demo hidden legacy transcript").assertDoesNotExist()
    }

    @Test fun conversationWithoutReceiptExplainsWhenContextBecomesAvailable() {
        val state = state(emptyList(), SessionHistoryState(managed = true))
        compose.setContent { AgentInboxTheme {
            SessionDetailContent(state, sessionId, SnackbarHostState(), {}, { _, _ -> })
        } }
        compose.onNodeWithText("收到新消息后可载入更早上下文").assertExists()
        compose.onNodeWithTag("load-earlier-context").assertIsNotEnabled()
    }

    @Test fun prependingHistoryKeepsTheVisibleMessageAndDoesNotSignalNewContent() {
        var state by mutableStateOf(state((0..30).map(::item)))
        compose.setContent { AgentInboxTheme {
            SessionDetailContent(state, sessionId, SnackbarHostState(), {}, { _, _ -> })
        } }
        compose.mainClock.advanceTimeBy(500)
        compose.onNodeWithTag("session-timeline").performScrollToIndex(15)
        compose.onNodeWithText("demo 行 14").assertIsDisplayed()
        compose.runOnIdle {
            val old = state.sessionItems.getValue(sessionId)
            state = state.copy(sessionItems = mapOf(sessionId to old.copy(
                items = (-20..-1).map(::item) + old.items,
                history = old.history.copy(loadedRounds = 10, revision = 1),
            )))
        }
        compose.mainClock.advanceTimeBy(500)
        compose.onNodeWithText("demo 行 14").assertIsDisplayed()
        compose.onNodeWithText("有新内容 ↓").assertDoesNotExist()
    }

    @Test fun restoringConversationKeepsDraftAndReadingPosition() {
        val restore = StateRestorationTester(compose)
        restore.setContent { AgentInboxTheme {
            SessionDetailContent(state((0..30).map(::item)), sessionId, SnackbarHostState(), {}, { _, _ -> })
        } }
        compose.mainClock.advanceTimeBy(500)
        compose.onNodeWithTag("session-input").performTextInput("demo unfinished draft")
        compose.onNodeWithTag("session-timeline").performScrollToIndex(15)
        compose.onNodeWithText("demo 行 14").assertIsDisplayed()
        restore.emulateSavedInstanceStateRestore()
        compose.mainClock.advanceTimeBy(500)
        compose.onNodeWithTag("session-input").assertTextContains("demo unfinished draft")
        compose.onNodeWithText("demo 行 14").assertIsDisplayed()
    }

    @Test fun serverItemIdentitySurvivesPrependingAndReaderIdChanges() {
        val original = item(1)
        assertEquals(TimelineItem.Item(original, 1).key, TimelineItem.Item(original.copy(id = "demo-new-reader-id"), 40).key)
        val fallback = original.copy(itemKey = "")
        assertEquals(TimelineItem.Item(fallback, 1).key, TimelineItem.Item(fallback, 40).key)
        assertEquals(
            TimelineItem.ProcessGroup(listOf(original), 1).key,
            TimelineItem.ProcessGroup(listOf(original.copy(id = "demo-new-reader-id"), item(2)), 40).key,
        )
    }

    @Test fun assistantFragmentsFoldOnlyInsideTheSameNotificationAndKeepUserToolBoundaries() {
        val first = item(1).copy(type = SessionItemType.ASSISTANT, text = "demo first fragment", eventId = 100)
        val second = item(2).copy(type = SessionItemType.ASSISTANT, text = "demo next fragment", eventId = 100)
        val user = item(3)
        val tool = item(4).copy(type = SessionItemType.TOOL_CALL, eventId = 100)
        val otherRound = second.copy(id = "demo-next-round", itemKey = "demo-other-round", eventId = 101)
        val folded = listOf(
            TimelineItem.Item(first, 0), TimelineItem.Item(second, 1),
            TimelineItem.Item(user, 2), TimelineItem.Item(first, 3),
            TimelineItem.Item(tool, 4), TimelineItem.Item(second, 5), TimelineItem.Item(otherRound, 6),
        ).foldAssistantFragments()
        assertEquals(6, folded.size)
        val combined = folded.first() as TimelineItem.Item
        assertEquals("demo first fragment\n\ndemo next fragment", combined.value.text)
        assertEquals(TimelineItem.Item(first, 0).key, combined.key)
        assertEquals(listOf(SessionItemType.ASSISTANT, SessionItemType.USER, SessionItemType.ASSISTANT,
            SessionItemType.TOOL_CALL, SessionItemType.ASSISTANT, SessionItemType.ASSISTANT), folded.map { (it as TimelineItem.Item).value.type })
    }

    private fun item(index: Int) = SessionItem(id = "demo-row-$index", itemKey = "demo-stable-$index", ts = stamp,
        type = SessionItemType.USER, text = "demo 行 $index")

    private fun state(
        items: List<SessionItem> = listOf(item(0)),
        history: SessionHistoryState = SessionHistoryState(managed = true, anchorEventId = 100, beforeCursor = "demo-before-cursor", status = "available"),
    ): AgentUiState {
        val computer = "demo-computer"
        val key = phoneConversationKey(computer, "codex", sessionId)
        return AgentUiState(
            settings = AppSettings("https://relay.example.org", "demo-token"),
            activeComputerKey = computer,
            phoneConversations = PhoneConversationState(receipts = mapOf(key to PhoneReceipt(100, stamp, 1))),
            sessionMeta = mapOf(sessionId to SessionMeta(sessionId = sessionId, source = "codex", computerId = computer,
                title = "demo history", cwd = "/tmp/demo-history", lastTs = stamp, status = "done")),
            sessionItems = mapOf(sessionId to SessionItemsState(items = items, history = history)),
        )
    }
}
