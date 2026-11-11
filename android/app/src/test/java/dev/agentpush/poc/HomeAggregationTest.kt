package dev.agentpush.poc

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class HomeAggregationTest {
    private val computerA = "computer-a"
    private val computerB = "computer-b"

    @Test
    fun mergesAttentionAndKeepsTheOwningComputer() {
        val feedA = feed(
            attention = listOf(
                attention("session-a", "done", "2026-11-10T10:00:00Z"),
            ),
            resume = listOf(session("resume-a", "2026-11-10T09:00:00Z")),
            running = listOf(session("running-a", "2026-11-10T11:00:00Z")),
            awayNew = 1,
            runningCount = 1,
        )
        val feedB = feed(
            attention = listOf(
                attention("session-b", "approval", "2026-11-10T09:30:00Z"),
            ),
            resume = listOf(session("resume-b", "2026-11-10T10:30:00Z")),
            needsYou = 1,
            earlier = 3,
        )

        val merged = mergeComputerHomes(
            listOf(
                ComputerHomeResult(computerA, "書房 Mac", feedA),
                ComputerHomeResult(computerB, "客廳 Mac", feedB),
            ),
            activeComputerId = computerA,
        )

        assertEquals(2, merged.attention.size)
        assertEquals("approval", merged.attention.first().reason)
        assertEquals("客廳 Mac", merged.attention.first().session.computerName)
        assertEquals(computerA, merged.attention.last().session.computerId)
        assertEquals(1, merged.needsYouCount)
        assertEquals(1, merged.awayNewCount)
        assertEquals(1, merged.runningCount)
        assertEquals(3, merged.earlierCount)
        assertEquals(2, merged.resume.size)
        assertEquals("resume-b", merged.resume.first().sessionId)
        assertEquals(2, merged.computers.size)
        assertEquals(computerA, merged.computers.first().computerId)
        assertTrue(merged.available)
    }

    @Test
    fun keepsOfflineComputersVisibleWithoutInventingSessions() {
        val merged = mergeComputerHomes(
            listOf(
                ComputerHomeResult(computerA, "書房 Mac", feed()),
                ComputerHomeResult(
                    computerId = computerB,
                    computerName = "客廳 Mac",
                    error = "雲端通道逾時",
                ),
            ),
            activeComputerId = computerA,
        )

        assertTrue(merged.available)
        assertEquals(2, merged.computers.size)
        assertFalse(merged.computers[1].available)
        assertEquals("雲端通道逾時", merged.computers[1].error)
        assertTrue(merged.attention.isEmpty())
    }

    @Test
    fun homeFeedJsonRoundTripsComputerLabels() {
        val merged = mergeComputerHomes(
            listOf(
                ComputerHomeResult(
                    computerId = computerA,
                    computerName = "書房 Mac",
                    feed = feed(
                        attention = listOf(
                            attention("session-a", "approval", "2026-11-10T10:00:00Z"),
                        ),
                    ),
                ),
            ),
            activeComputerId = computerA,
        )

        val restored = HomeFeed.fromJson(merged.toJson())

        assertEquals("書房 Mac", restored.attention.first().session.computerName)
        assertEquals(computerA, restored.computers.first().computerId)
        assertTrue(restored.computers.first().available)
    }

    @Test
    fun identicalSessionIdsOnDifferentComputersStayIndependent() {
        val shared = "shared-session-id"
        val merged = mergeComputerHomes(
            listOf(
                ComputerHomeResult(
                    computerId = computerA,
                    computerName = "書房 Mac",
                    feed = feed(
                        attention = listOf(attention(shared, "approval", "2026-11-10T10:00:00Z")),
                        resume = listOf(session(shared, "2026-11-10T09:00:00Z")),
                    ),
                ),
                ComputerHomeResult(
                    computerId = computerB,
                    computerName = "客廳 Mac",
                    feed = feed(
                        attention = listOf(attention(shared, "needs_reply", "2026-11-10T11:00:00Z")),
                        resume = listOf(session(shared, "2026-11-10T10:30:00Z")),
                    ),
                ),
            ),
            activeComputerId = computerA,
        )

        assertEquals(2, merged.attention.size)
        assertEquals(setOf(computerA, computerB), merged.attention.map { it.session.computerId }.toSet())
        assertEquals(2, merged.resume.size)
        assertEquals(setOf(computerA, computerB), merged.resume.map { it.computerId }.toSet())
    }

    @Test
    fun singleComputerKeepsTheOriginalFeedShape() {
        val merged = mergeComputerHomes(
            listOf(
                ComputerHomeResult(
                    computerId = computerA,
                    computerName = "書房 Mac",
                    feed = feed(
                        attention = listOf(attention("session-a", "done", "2026-11-10T10:00:00Z")),
                        resume = listOf(session("resume-a", "2026-11-10T09:00:00Z")),
                    ),
                ),
            ),
            activeComputerId = computerA,
        )

        assertEquals(1, merged.attention.size)
        assertEquals(1, merged.resume.size)
        assertEquals(1, merged.computers.size)
        assertEquals(computerA, merged.attention.single().session.computerId)
    }

    private fun feed(
        attention: List<HomeAttention> = emptyList(),
        resume: List<SessionMeta> = emptyList(),
        running: List<SessionMeta> = emptyList(),
        needsYou: Int = 0,
        awayNew: Int = 0,
        runningCount: Int = 0,
        earlier: Int = 0,
    ): HomeFeed = HomeFeed(
        attention = attention,
        resume = resume,
        running = running,
        earlierCount = earlier,
        needsYouCount = needsYou,
        awayNewCount = awayNew,
        runningCount = runningCount,
        generatedAt = "2026-11-10T12:00:00Z",
        available = true,
    )

    private fun attention(
        sessionId: String,
        reason: String,
        at: String,
    ): HomeAttention = HomeAttention(
        session = session(sessionId, at),
        reason = reason,
        eventId = 1,
        at = at,
        headline = reason,
    )

    private fun session(sessionId: String, lastTs: String): SessionMeta = SessionMeta(
        sessionId = sessionId,
        source = "codex",
        title = sessionId,
        lastTs = lastTs,
        project = "demo",
    )
}
