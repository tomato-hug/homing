package dev.agentpush.poc

import dev.agentpush.poc.ui.MAX_LINE_RENDER_CHARS
import dev.agentpush.poc.ui.absolutizeMarkdown
import dev.agentpush.poc.ui.friendlyBrowserAddress
import dev.agentpush.poc.ui.guessWebMime
import dev.agentpush.poc.ui.isRasterImagePath
import dev.agentpush.poc.ui.prepareHtmlForMobilePreview
import dev.agentpush.poc.ui.trimRenderLine
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class WebPreviewSupportTest {
    @Test
    fun prepareHtmlWrapsBareSvgAndInjectsMissingViewport() {
        val wrapped = prepareHtmlForMobilePreview("""<svg viewBox="0 0 800 500"><circle cx="40" cy="40" r="20"/></svg>""")
        assertTrue(wrapped.contains("<!doctype html>"))
        assertTrue(wrapped.contains("""name="viewport""""))
        assertTrue(wrapped.contains("<svg viewBox=\"0 0 800 500\">"))

        val htmlNoMeta = "<html><head><title>Test</title></head><body><h1>Hi</h1></body></html>"
        val injected = prepareHtmlForMobilePreview(htmlNoMeta)
        assertTrue(injected.contains("""name="viewport""""))
        assertTrue(injected.contains("<title>Test</title>"))

        val htmlWithMeta = """<html><head><meta name="viewport" content="width=device-width"></head><body>Ok</body></html>"""
        assertEquals(htmlWithMeta, prepareHtmlForMobilePreview(htmlWithMeta))
    }

    @Test
    fun guessWebMimeAndFriendlyCloudAddresses() {
        assertEquals("text/html" to "UTF-8", guessWebMime("https://homing.local/f/abc/pelican.html", byteArrayOf()))
        assertEquals("image/svg+xml" to "UTF-8", guessWebMime("https://homing.local/f/abc/scene.svg", byteArrayOf()))
        assertEquals("image/png" to null, guessWebMime("https://homing.local/api/fs/raw?path=preview.png", byteArrayOf()))
        assertEquals(
            "image/svg+xml" to "UTF-8",
            guessWebMime("https://homing.local/f/abc/noext", "<svg xmlns='http://www.w3.org/2000/svg'/>".toByteArray()),
        )
        assertEquals(
            "电脑文件 · pelican bicycle.html",
            friendlyBrowserAddress("https://homing.local/f/2dfd7c00112233445566778899aabbcc/pelican%20bicycle.html"),
        )
        assertEquals(
            "电脑文件 · chart.png",
            friendlyBrowserAddress("https://homing.local/api/fs/raw?path=%2Ftmp%2Fchart.png&cwd=%2Ftmp"),
        )
    }

    @Test
    fun absolutizeMarkdownRoutesImagesOverCloudOriginWhenBaseBlank() {
        val settings = AppSettings(baseUrl = "", token = "")
        val out = absolutizeMarkdown(
            source = "Look ![shot](out/scene.png) and ![att](/f/0123456789abcdef0123456789abcdef/a.png)",
            settings = settings,
            cwd = "/Users/demo/proj",
        )
        assertTrue(out.contains("https://homing.local/api/fs/raw?path=out%2Fscene.png&cwd=%2FUsers%2Fdemo%2Fproj"))
        assertTrue(out.contains("https://homing.local/f/0123456789abcdef0123456789abcdef/a.png"))
    }

    @Test
    fun trimRenderLineCapsHugeLinesAndDetectsRasterImages() {
        val huge = "x".repeat(MAX_LINE_RENDER_CHARS + 500)
        val trimmed = trimRenderLine(huge)
        assertTrue(trimmed.length < huge.length)
        assertTrue(trimmed.contains("复制全文"))
        assertTrue(isRasterImagePath("/tmp/demo/out.PNG"))
        assertFalse(isRasterImagePath("/tmp/demo/page.html"))
        assertFalse(isRasterImagePath("/tmp/demo/vector.svg"))
    }

    @Test
    fun settledAndSupersededApprovalsClearStalePendingBanner() {
        val permEvent = AgentEvent(
            id = 42L,
            ts = "2026-11-28T06:00:00Z",
            source = "workbuddy",
            kind = "permission",
            sessionId = "wb-1",
            cwd = "/tmp/demo",
            project = "demo",
            title = "WorkBuddy 请求权限",
            text = "needs your permission to use AskUserQuestion",
            read = false,
        )
        val state = AgentUiState(
            settings = AppSettings(baseUrl = "", token = ""),
            events = mapOf(42L to permEvent),
            sessionMeta = mapOf(
                "wb-1" to SessionMeta(
                    sessionId = "wb-1",
                    source = "workbuddy",
                    status = "needs_permission",
                    lastTs = "2026-11-28T06:05:00Z",
                ),
            ),
            settledApprovalIds = setOf(42L),
        )
        val session = state.session("wb-1")!!
        assertEquals(SessionStatus.COMPLETED, session.status)
        assertNull(session.pendingEvent)

        val supersededState = state.copy(
            settledApprovalIds = emptySet(),
            sessionItems = mapOf(
                "wb-1" to SessionItemsState(
                    available = true,
                    items = listOf(
                        SessionItem(
                            id = "msg-2",
                            type = SessionItemType.ASSISTANT,
                            ts = "2026-11-28T06:05:00Z",
                            text = "已生成完成，请预览。",
                        ),
                    ),
                ),
            ),
        )
        val supersededSession = supersededState.session("wb-1")!!
        assertEquals(SessionStatus.COMPLETED, supersededSession.status)
        assertNull(supersededSession.pendingEvent)
    }
}
