package dev.agentpush.poc

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

class MobileInboxModeTest {
    private val now = Instant.parse("2026-11-11T03:00:00Z")

    @Test
    fun disabledInboxHidesAttentionAndNonResidentSessions() {
        val mode = MobileInboxMode(enabled = false)
        val old = now.minusSeconds(600).toString()
        val meta = meta("old-session", old)
        val home = HomeFeed(
            attention = listOf(attention(meta, old)),
            running = listOf(meta.copy(running = true)),
            resume = listOf(meta),
            earlierCount = 12,
            needsYouCount = 1,
            runningCount = 1,
            available = true,
        )

        val visible = mode.visibleHome(home)

        assertTrue(visible.attention.isEmpty())
        assertTrue(visible.running.isEmpty())
        assertTrue(visible.resume.isEmpty())
        assertEquals(0, visible.needsYouCount)
        assertEquals(0, visible.runningCount)
        assertEquals(0, visible.earlierCount)
    }

    @Test
    fun enabledInboxOnlyAddsActivityAfterTheCutoff() {
        val mode = MobileInboxMode(
            enabled = true,
            sinceMillis = now.minusSeconds(60).toEpochMilli(),
        )
        val old = meta("old-session", now.minusSeconds(600).toString())
        val fresh = meta("fresh-session", now.minusSeconds(5).toString())
        val home = HomeFeed(
            attention = listOf(
                attention(old, old.lastTs),
                attention(fresh, fresh.lastTs),
            ),
            running = listOf(
                old.copy(running = true),
                fresh.copy(running = true),
            ),
            resume = listOf(old, fresh),
            needsYouCount = 2,
            runningCount = 2,
            available = true,
        )

        val visible = mode.visibleHome(home)

        assertEquals(listOf("fresh-session"), visible.attention.map { it.sessionId })
        assertEquals(listOf("fresh-session"), visible.running.map { it.sessionId })
        assertEquals(listOf("fresh-session"), visible.resume.map { it.sessionId })
        assertEquals(1, visible.needsYouCount)
        assertEquals(1, visible.runningCount)
    }

    @Test
    fun residentSessionStaysVisibleButDoesNotCreateOldAttention() {
        val mode = MobileInboxMode(
            enabled = false,
            residentSessionIds = setOf("resident-session"),
        )
        val old = now.minusSeconds(3_600).toString()
        val resident = meta("resident-session", old)

        assertTrue(mode.isSessionVisible(resident))
        assertTrue(mode.visibleHome(HomeFeed(resume = listOf(resident))).resume.isNotEmpty())
        assertFalse(
            mode.copy(enabled = true, sinceMillis = now.minusSeconds(60).toEpochMilli())
                .isAttentionVisible(attention(resident, old)),
        )
    }

    private fun meta(id: String, lastTs: String): SessionMeta {
        return SessionMeta(
            sessionId = id,
            source = "codex",
            title = id,
            cwd = "/tmp/$id",
            lastTs = lastTs,
        )
    }

    private fun attention(session: SessionMeta, at: String): HomeAttention {
        return HomeAttention(
            session = session,
            reason = "done",
            at = at,
            eventId = 1,
        )
    }
}
