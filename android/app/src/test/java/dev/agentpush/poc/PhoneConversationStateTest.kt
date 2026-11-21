package dev.agentpush.poc

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class)
class PhoneConversationStateTest {
    private val oldTime = "2026-11-21T10:00:00Z"
    private val newTime = "2026-11-21T10:00:01Z"
    private val key = "[\"demo-computer-a\",\"codex\",\"demo-shared-session\"]"

    @Before
    fun clearPreferences() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        context.getSharedPreferences("homing_phone_conversations_v1", Context.MODE_PRIVATE)
            .edit().clear().commit()
    }

    @Test
    fun clearHidesInboxButKeepsSessionListAndNewEventReappears() {
        val old = session()
        val state = PhoneConversationState().clearInbox(mapOf(key to PhoneActivity(100, oldTime)))

        assertFalse(state.isInboxVisible(old))
        assertFalse(state.isSessionHidden(old))
        assertTrue(state.isInboxVisible(session(eventId = 101)))
        assertTrue(state.isInboxVisible(session(timestamp = newTime)))
        assertTrue(state.undoCleanup().isInboxVisible(old))
    }

    @Test
    fun hideIsScopedByComputerSourceAndSessionEvenWhenComputerIsFallback() {
        val state = PhoneConversationState().hide(mapOf(key to PhoneActivity(100, oldTime)))

        assertTrue(state.isSessionHidden(session()))
        assertTrue(state.isSessionHidden(session(computer = ""), "demo-computer-a"))
        assertFalse(state.isSessionHidden(session(computer = "demo-computer-b")))
        assertFalse(state.isSessionHidden(session(source = "claude")))
        assertFalse(state.isSessionHidden(session(id = "demo-other-session")))
        assertFalse(state.isSessionHidden(session(eventId = 101)))
    }

    @Test
    fun batchHideCanUndoAllRowsAndRestoreDoesNotResetInboxCutoff() {
        val otherKey = "[\"demo-computer-a\",\"codex\",\"demo-other-session\"]"
        val initial = PhoneConversationState().clearInbox(mapOf(key to PhoneActivity(100, oldTime)))
        val hidden = initial.hide(mapOf(key to PhoneActivity(100, oldTime), otherKey to PhoneActivity(100, oldTime)))

        assertTrue(hidden.isSessionHidden(session()))
        assertTrue(hidden.isSessionHidden(session(id = "demo-other-session")))
        assertEquals(initial.hiddenSessions, hidden.undoCleanup().hiddenSessions)
        assertEquals(initial.clearedInbox, hidden.undoCleanup().clearedInbox)
        val restored = hidden.restoreSessions()
        assertFalse(restored.hasHiddenSessions)
        assertFalse(restored.isInboxVisible(session()))
        assertTrue(restored.undoCleanup().isSessionHidden(session()))
    }

    @Test
    fun receiptAnchorRemainsAtFirstReceivedEventAcrossRefreshes() {
        val first = PhoneConversationState().receive(key, 100, oldTime, now = 1000)
        val updated = first.receive(key, 101, newTime, now = 2000)
        assertEquals(PhoneReceipt(100, oldTime, 1000), updated.receipts[key])
        assertTrue(PhoneConversationState().receive(key, 0, oldTime).receipts.isEmpty())
    }

    @Test
    fun compositeKeyEscapesDelimitersInsteadOfColliding() {
        assertEquals(key, phoneConversationKey("demo-computer-a", "codex", "demo-shared-session"))
        assertNotEquals(phoneConversationKey("demo:a", "b", "c"), phoneConversationKey("demo", "a:b", "c"))
        assertNotEquals(phoneConversationKey("demo\"", "b", "c"), phoneConversationKey("demo", "\"b", "c"))
    }

    @Test
    fun homeDeduplicatesWithinSessionButKeepsOtherComputersAndSources() {
        val a = meta()
        val b = meta(computer = "demo-computer-b")
        val c = meta(source = "claude")
        val state = PhoneConversationState().clearInbox(mapOf(key to PhoneActivity(100, oldTime)))
        val visible = state.visibleHome(HomeFeed(
            attention = listOf(
                HomeAttention(session = a, eventId = 100, at = oldTime, reason = "done"),
                HomeAttention(session = a, eventId = 101, at = newTime, reason = "done", unseenCount = 2),
                HomeAttention(session = b, eventId = 100, at = oldTime, reason = "done"),
                HomeAttention(session = c, eventId = 100, at = oldTime, reason = "done"),
            ),
            running = listOf(a, b), resume = listOf(a, b, c),
        ))
        assertEquals(3, visible.attention.size)
        assertEquals(101L, visible.attention.first { it.session.computerId == a.computerId && it.session.source == "codex" }.eventId)
        assertEquals(3, visible.needsYouCount)
        assertTrue(visible.running.isEmpty())
        assertTrue(visible.resume.isEmpty())
    }

    @Test
    fun preferencesRestoreCleanupReceiptsAndHistoryWithoutPersistingTransientLoading() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val history = SessionHistoryState(managed = true, anchorEventId = 100,
            beforeCursor = "demo-history-cursor", status = "available", loadedRounds = 10, revision = 2,
            loading = true, error = "demo transient error")
        val state = PhoneConversationState().receive(key, 100, oldTime, now = 1000)
            .clearInbox(mapOf(key to PhoneActivity(100, oldTime)))
            .hide(mapOf(key to PhoneActivity(100, oldTime)))
            .copy(histories = mapOf(key to history))
        PhoneConversationPreferences(context).save(state)
        val restored = PhoneConversationPreferences(context).load()
        assertEquals(state.receipts, restored.receipts)
        assertEquals(state.clearedInbox, restored.clearedInbox)
        assertEquals(state.hiddenSessions, restored.hiddenSessions)
        assertEquals(history.copy(loading = false, error = null), restored.histories[key])
        assertFalse(restored.isInboxVisible(session()))
        assertTrue(restored.isInboxVisible(session(eventId = 101)))
    }

    @Test
    fun historyMergeUpdatesStableRowsAndRetainsEarlierPagesInOrder() {
        val older = item("demo-old", "demo-key-old", "old")
        val current = item("demo-current", "demo-key-current", "partial").copy(partial = true)
        val final = current.copy(id = "demo-current-final", text = "final", partial = false)
        val appended = mergePhoneItems(listOf(older, current), listOf(final), prepend = false)
        assertEquals(listOf("old", "final"), appended.map { it.text })
        val earlier = item("demo-earlier", "demo-key-earlier", "earlier")
        assertEquals(listOf("earlier", "old", "final"),
            mergePhoneItems(appended, listOf(earlier, older), prepend = true).map { it.text })
    }

    @Test
    fun authoritativeRowsReplaceMatchingLiveRowsWithoutLosingDifferentFileChanges() {
        val live = SessionItem(id = "demo-live-file", ts = oldTime, type = SessionItemType.FILE_CHANGE,
            files = listOf(ItemFileChange("demo-a.kt", added = 1)))
        val unrelated = SessionItem(id = "demo-server-file", itemKey = "demo-key-file-b", ts = oldTime,
            type = SessionItemType.FILE_CHANGE, files = listOf(ItemFileChange("demo-b.kt", added = 2)))
        val retained = mergePhoneItems(listOf(live), listOf(unrelated), prepend = false)
        assertEquals(listOf("demo-a.kt", "demo-b.kt"), retained.flatMap { it.files }.map { it.path })

        val confirmed = live.copy(id = "demo-confirmed-file", itemKey = "demo-key-file-a")
        val reconciled = mergePhoneItems(retained, listOf(confirmed), prepend = false)
        assertEquals(2, reconciled.size)
        assertEquals(setOf("demo-key-file-a", "demo-key-file-b"), reconciled.map { it.itemKey }.toSet())
    }

    private fun meta(computer: String = "demo-computer-a", source: String = "codex", id: String = "demo-shared-session") =
        SessionMeta(sessionId = id, source = source, title = "Demo state test", computerId = computer, lastTs = oldTime)

    private fun session(computer: String = "demo-computer-a", source: String = "codex", id: String = "demo-shared-session",
                        eventId: Long = 100, timestamp: String = oldTime): SessionSummary {
        val event = AgentEvent(id = eventId, ts = timestamp, source = source, kind = "done", sessionId = id,
            cwd = "/demo/state-test", project = "demo", title = "Demo state test", text = "demo result", read = false)
        return AgentUiState(settings = AppSettings("http://127.0.0.1:1", "demo-test-token"),
            sessionMeta = mapOf(id to meta(computer, source, id).copy(lastTs = timestamp)),
            events = mapOf(eventId to event)).sessions().single()
    }

    private fun item(id: String, key: String, text: String) =
        SessionItem(id = id, itemKey = key, ts = oldTime, type = SessionItemType.ASSISTANT, text = text)
}
