package dev.agentpush.poc

import dev.agentpush.poc.ui.screens.TimelineItem
import dev.agentpush.poc.ui.screens.searchSessionTimeline
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class SessionSearchTest {
    @Test
    fun findsTextInsideToolAndFileChangesWithReadableRole() {
        val timeline = listOf(
            TimelineItem.Item(
                SessionItem(
                    id = "tool-1",
                    ts = "2026-11-11T08:00:00Z",
                    type = SessionItemType.TOOL_CALL,
                    text = "",
                    tool = ItemTool(
                        name = "shell",
                        inputSummary = "rg --files src",
                        outputTail = "src/Main.kt",
                    ),
                ),
                order = 0,
            ),
            TimelineItem.Item(
                SessionItem(
                    id = "file-1",
                    ts = "2026-11-11T08:01:00Z",
                    type = SessionItemType.FILE_CHANGE,
                    files = listOf(
                        ItemFileChange(
                            path = "src/Main.kt",
                            added = 3,
                            removed = 1,
                            diff = "@@ -1 +1 @@",
                        ),
                    ),
                ),
                order = 1,
            ),
            TimelineItem.Message(
                TranscriptMessage(
                    role = "assistant",
                    text = "已经修正 Main.kt 的入口判断。",
                    ts = "2026-11-11T08:02:00Z",
                ),
                order = 2,
            ),
        )

        val results = searchSessionTimeline(timeline, "main.kt")

        assertEquals(3, results.size)
        assertEquals("工具", results[0].role)
        assertEquals(0, results[0].timelineIndex)
        assertTrue(results[0].snippet.contains("Main.kt", ignoreCase = true))
        assertEquals("文件", results[1].role)
        assertEquals(1, results[1].timelineIndex)
        assertEquals("Agent", results[2].role)
        assertEquals(2, results[2].timelineIndex)
    }

    @Test
    fun blankQueryAndLimitAreRespected() {
        val timeline = (0..4).map { index ->
            TimelineItem.Message(
                TranscriptMessage(
                    role = "assistant",
                    text = "重复关键词 $index",
                    ts = "2026-11-11T08:0$index:00Z",
                ),
                order = index,
            )
        }

        assertTrue(searchSessionTimeline(timeline, "  ").isEmpty())
        assertEquals(2, searchSessionTimeline(timeline, "重复关键词", limit = 2).size)
    }
}

