package dev.agentpush.poc

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class RelayV2ModelTest {
    @Test
    fun remoteGovernanceFlagsOverrideLocalCache() {
        val meta = SessionMeta(
            sessionId = "session-1",
            source = "codex",
            title = "标题",
            project = "project",
            cwd = "/Users/me/project",
            read = true,
            pinned = true,
            muted = true,
            archived = false,
        )
        val state = AgentUiState(
            settings = AppSettings("http://127.0.0.1:18787", "token"),
            sessionMeta = mapOf(meta.sessionId to meta),
            localSessionFlags = mapOf(
                meta.sessionId to LocalSessionFlags(
                    read = false,
                    pinned = false,
                    muted = false,
                    archived = true,
                ),
            ),
        )

        val session = state.sessions().single()

        assertTrue(session.read)
        assertTrue(session.pinned)
        assertTrue(session.muted)
        assertFalse(session.archived)
    }

    @Test
    fun missingRemoteFlagsFallBackToLocalCache() {
        val meta = SessionMeta(
            sessionId = "session-1",
            source = "claude",
            title = "标题",
            project = "project",
            cwd = "/Users/me/project",
        )
        val state = AgentUiState(
            settings = AppSettings("http://127.0.0.1:18787", "token"),
            sessionMeta = mapOf(meta.sessionId to meta),
            localSessionFlags = mapOf(
                meta.sessionId to LocalSessionFlags(
                    read = true,
                    pinned = true,
                    muted = true,
                    archived = true,
                ),
            ),
        )

        val session = state.sessions().single()

        assertTrue(session.read)
        assertTrue(session.pinned)
        assertTrue(session.muted)
        assertTrue(session.archived)
    }

    @Test
    fun inboxEntryParsesGovernanceAndSessionFallbacks() {
        val entry = InboxEntry.fromJson(
            JSONObject(
                """
                {
                  "kind": "approval",
                  "event_id": "42",
                  "title": "Fallback title",
                  "summary": "Fallback summary",
                  "at": "2026-11-09T10:00:00Z",
                  "pinned": true,
                  "read": false,
                  "session": {
                    "session_id": "session-1",
                    "source": "codex",
                    "project": "project",
                    "cwd": "/Users/me/project",
                    "read": false,
                    "pinned": false,
                    "is_temp": false,
                    "is_empty": false
                  }
                }
                """.trimIndent(),
            ),
        )

        requireNotNull(entry)
        assertEquals(InboxEntryKind.APPROVAL, entry.kind)
        assertEquals("session-1", entry.sessionId)
        assertEquals("42", entry.eventId)
        assertEquals("Fallback title", entry.title)
        assertEquals("Fallback summary", entry.summary)
        assertTrue(entry.pinned == true)
        assertFalse(entry.read == true)
        assertEquals("codex", entry.sessionMeta?.source)
        assertEquals("Fallback title", entry.sessionMeta?.title)
        assertEquals("Fallback summary", entry.sessionMeta?.lastPreview)
    }

    @Test
    fun inboxReadRemovesWaitingAndRefreshesCount() {
        val waiting = InboxEntry(
            kind = InboxEntryKind.NEEDS_REPLY,
            sessionId = "session-1",
            read = false,
        )
        val feed = InboxFeed(
            waiting = listOf(waiting),
            running = emptyList(),
            recentDone = listOf(
                InboxEntry(
                    kind = InboxEntryKind.DONE,
                    sessionId = "session-1",
                    read = false,
                ),
            ),
            waitingCount = 1,
            runningCount = 0,
            available = true,
        )

        val updated = feed.withSessionRead("session-1")

        assertTrue(updated.waiting.isEmpty())
        assertEquals(0, updated.waitingCount)
        assertEquals(true, updated.recentDone.single().read)
    }

    @Test
    fun tempAndEmptyFlagsParseAndRemainNullableWhenMissing() {
        val withFlags = SessionMeta.fromJson(
            JSONObject(
                """
                {
                  "session_id": "session-1",
                  "is_temp": true,
                  "is_empty": false
                }
                """.trimIndent(),
            ),
        )
        val withoutFlags = SessionMeta.fromJson(
            JSONObject("""{"session_id":"session-2"}"""),
        )

        assertEquals(true, withFlags.isTemp)
        assertEquals(false, withFlags.isEmpty)
        assertNull(withoutFlags.isTemp)
        assertNull(withoutFlags.isEmpty)
    }
}
