package dev.agentpush.poc

import dev.agentpush.poc.ui.screens.TimelineItem
import dev.agentpush.poc.ui.screens.buildSessionTimeline
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

class SessionTimelineMergeTest {
    private val id = "demo-merge-session"

    @Test fun timestampOnlyAnchorAndNewNotificationsSurviveLaterNativeItems() {
        val receipt = event(100, 0, "demo anchor output")
        val newer = event(101, 4, "demo new notification not yet in transcript")
        val items = listOf(item("user", 1, SessionItemType.USER, "demo next question"),
            item("answer", 3, SessionItemType.ASSISTANT, "demo next answer"))
        val timeline = buildSessionTimeline(session(listOf(receipt, newer), listOf(reply(10, 2))), items)
        assertEquals(listOf("event-100", "item-demo-user", "reply-10", "item-demo-answer", "event-101"), timeline.map { it.key })
    }

    @Test fun fallbackRepliesAreInterleavedAndDuplicatePreviewsNeverCrossUserBoundaries() {
        val timeline = buildSessionTimeline(session(listOf(
            event(100, 0, "demo repeated output", "done"),
            event(101, 2, "demo repeated output", "message"),
            event(102, 3, "demo repeated output", "done"),
        ), listOf(reply(10, 1))), emptyList())
        assertEquals(listOf("event-100", "reply-10", "event-102"), timeline.map { it.key })
    }

    @Test fun nativeOutputRepresentsNotificationButOldRoundCannotSwallowNewEvent() {
        val items = listOf(item("old-answer", 0, SessionItemType.ASSISTANT, "demo same output"),
            item("new-user", 2, SessionItemType.USER, "demo another question"))
        val timeline = buildSessionTimeline(session(listOf(
            event(100, 1, "demo   same output"),
            event(101, 3, "demo same output"),
        )), items)
        assertEquals(listOf("item-demo-old-answer", "item-demo-new-user", "event-101"), timeline.map { it.key })
    }

    @Test fun legacyDuplicateIdsKeepDistinctBodiesAndKeysWhenRowsArePrepended() {
        val first = item("duplicate", 1, SessionItemType.ASSISTANT, "demo first body").copy(itemKey = "")
        val second = first.copy(text = "demo second body")
        val old = buildSessionTimeline(session(), listOf(first, second))
        val withEarlier = buildSessionTimeline(session(), listOf(item("earlier", 0, SessionItemType.USER, "demo earlier"), first, second))
        assertEquals(2, old.size)
        assertNotEquals(old[0].key, old[1].key)
        assertEquals(old.map { it.key }, withEarlier.drop(1).map { it.key })
        assertEquals(listOf(first.text, second.text), old.map { (it as TimelineItem.Item).value.text })
    }

    @Test fun oldRepeatedPromptCannotConsumeANewerPhoneReply() {
        val items = listOf(item("old-user", 0, SessionItemType.USER, "demo phone question"),
            item("answer", 3, SessionItemType.ASSISTANT, "demo new answer"))
        val timeline = buildSessionTimeline(session(replies = listOf(reply(10, 2))), items)
        assertEquals(listOf("item-demo-old-user", "reply-10", "item-demo-answer"), timeline.map { it.key })
    }

    @Test fun samePromptWithinTenSecondsUsesOnePhoneReplyRow() {
        val prompt = item("current-user", 1, SessionItemType.USER, "demo phone question").copy(ts = "2026-11-21T12:01:07Z")
        val sent = reply(10, 1).copy(ts = "2026-11-21T12:01:03Z")
        val timeline = buildSessionTimeline(session(replies = listOf(sent)), listOf(prompt))
        assertEquals(listOf("reply-10"), timeline.map { it.key })
        val oldSent = sent.copy(ts = "2026-11-21T12:00:03Z")
        assertEquals(2, buildSessionTimeline(session(replies = listOf(oldSent)), listOf(prompt)).size)
    }

    private fun timestamp(minute: Int) = "2026-11-21T12:${minute.toString().padStart(2, '0')}:00Z"
    private fun event(number: Long, minute: Int, text: String, kind: String = "done") = AgentEvent(
        number, timestamp(minute), "codex", kind, id, "/tmp/demo-merge", "demo", "demo", text, true,
    )
    private fun item(key: String, minute: Int, type: SessionItemType, text: String) = SessionItem(
        id = "demo-$key", itemKey = "demo-$key", ts = timestamp(minute), type = type, text = text,
    )
    private fun reply(number: Long, minute: Int) = AgentReply(
        id = number, eventId = 100, sessionId = id, ts = timestamp(minute),
        text = "demo phone question", status = ReplyStatus.INJECTED,
    )
    private fun session(events: List<AgentEvent> = emptyList(), replies: List<AgentReply> = emptyList()) = AgentUiState(
        settings = AppSettings("https://relay.example.org", "demo-token"),
        sessionMeta = mapOf(id to SessionMeta(sessionId = id, source = "codex", title = "demo merge")),
        events = events.associateBy { it.id }, replies = replies.associateBy { it.id },
    ).session(id)!!
}
