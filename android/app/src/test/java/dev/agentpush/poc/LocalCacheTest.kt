package dev.agentpush.poc

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class LocalCacheTest {
    @Test
    fun snapshotAndDraftRoundTrip() = runBlocking {
        val cache = LocalCache(ApplicationProvider.getApplicationContext<Context>())
        cache.clear()
        val meta = SessionMeta(
            sessionId = "demo-session-1",
            source = "codex",
            title = "演示会话",
            cwd = "/tmp/demo",
            project = "demo",
            lastTs = "2026-11-10T12:00:00.000Z",
            read = false,
        )
        cache.saveSnapshot(
            sessions = listOf(meta),
            inbox = InboxFeed(
                waiting = listOf(
                    InboxEntry(
                        kind = InboxEntryKind.APPROVAL,
                        sessionId = meta.sessionId,
                        title = meta.title,
                        summary = "请求授权",
                        sessionMeta = meta,
                    ),
                ),
                available = true,
            ),
            home = HomeFeed(
                away = HomeAway(enabled = true, current = AwayPeriod(id = "demo-away")),
                attention = listOf(
                    HomeAttention(
                        session = meta,
                        reason = "needs_reply",
                        eventId = 9L,
                        headline = "等你回覆",
                        summary = "要用方案 A 还是方案 B？",
                    ),
                ),
                needsYouCount = 1,
                available = true,
            ),
            projects = listOf(
                ProjectInfo(
                    cwd = "/tmp/demo",
                    project = "demo",
                    count = 1,
                    lastTs = meta.lastTs,
                    sources = listOf("codex"),
                ),
            ),
            away = true,
            drafts = mapOf(meta.sessionId to "尚未发送的草稿"),
            savedAt = 1234L,
        )

        val loaded = cache.load()

        assertEquals(1, loaded.sessions.size)
        assertEquals("演示会话", loaded.sessions.single().title)
        assertEquals(1234L, loaded.savedAt)
        assertTrue(loaded.away)
        assertEquals("尚未发送的草稿", loaded.drafts[meta.sessionId])
        assertNotNull(loaded.inbox)
        assertEquals(1, loaded.inbox?.waiting?.size)
        assertEquals(InboxEntryKind.APPROVAL, loaded.inbox?.waiting?.single()?.kind)
        assertTrue(loaded.home?.away?.enabled == true)
        assertEquals("demo-away", loaded.home?.away?.current?.id)
        assertEquals(1, loaded.home?.attention?.size)
        assertEquals("等你回覆", loaded.home?.attention?.single()?.headline)
        assertEquals("demo", loaded.projects.single().project)
    }

    @Test
    fun outboxKeepsOrderAndCanBeRemoved() = runBlocking {
        val cache = LocalCache(ApplicationProvider.getApplicationContext<Context>())
        cache.clear()

        val first = cache.queueReply(
            OutboxReply(
                localReplyId = -1L,
                sessionId = "demo-session-1",
                eventId = 9L,
                source = "codex",
                text = "第一条",
                files = listOf("/tmp/demo.png"),
                model = "demo-model",
                effort = "high",
                permissionMode = "default",
            ),
        )
        val second = cache.queueReply(
            OutboxReply(
                localReplyId = -2L,
                sessionId = "demo-session-2",
                eventId = null,
                source = "claude",
                text = "第二条",
                permissionMode = "plan",
            ),
        )

        val queued = cache.outbox()
        assertEquals(listOf(-1L, -2L), queued.map { it.localReplyId })
        assertEquals(listOf("/tmp/demo.png"), queued.first().files)
        assertEquals("plan", queued.last().permissionMode)

        cache.deleteOutbox(first)
        assertEquals(listOf(-2L), cache.outbox().map { it.localReplyId })
        cache.deleteOutbox(second)
        cache.clear()
        assertTrue(cache.outbox().isEmpty())
    }

    @Test
    fun sessionItemsRoundTripKeepsRecentTranscript() = runBlocking {
        val cache = LocalCache(ApplicationProvider.getApplicationContext<Context>())
        cache.clear()
        val items = (1..305).map { index ->
            SessionItem(
                id = "demo-item-$index",
                ts = "2026-11-10T12:00:${index.toString().padStart(2, '0')}.000Z",
                type = SessionItemType.ASSISTANT,
                role = "assistant",
                text = "演示条目 $index",
            )
        }

        cache.saveItems("demo-session-items", items)
        val restored = cache.load().items["demo-session-items"].orEmpty()

        assertEquals(LocalCache.MAX_ITEMS_PER_SESSION, restored.size)
        assertEquals("demo-item-6", restored.first().id)
        assertEquals("演示条目 305", restored.last().text)
    }
}
