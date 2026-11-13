package dev.agentpush.poc

import dev.agentpush.poc.ui.markdownImageFallbacks
import org.junit.Assert.*
import org.junit.Test

class MarkdownImageFallbackTest {
    @Test fun referenceImagesResolveDefinitionsWithoutTouchingCode() {
        val source = "![chart][demo]\n\n[demo]: /Users/demo/reference.png\n\n`![chart][demo]`"
        val result = markdownImageFallbacks(source, emptySet()) { "Unavailable: $it" }
        assertTrue(result.startsWith("Unavailable: reference.png"))
        assertTrue(result.contains("`![chart][demo]`"))
    }
    @Test fun failedUrlsMatchRendererUnescaping() {
        for ((source, url) in listOf(
            "![demo](https://example.invalid/chart\\(1\\).png)" to "https://example.invalid/chart(1).png",
            "![demo](https://example.invalid/chart.png?a=1&amp;b=2)" to "https://example.invalid/chart.png?a=1&amp;b=2",
            "![chart][demo]\n\n[demo]: https://example.invalid/chart.png" to "https://example.invalid/chart.png",
        )) {
            val result = markdownImageFallbacks(source, setOf(url)) { "Unavailable: $it" }
            assertTrue("Source: $source; result: $result", result.startsWith("Unavailable:"))
        }
    }
    @Test fun networkReferenceImageBecomesRenderableInlineImage() {
        val source = "![chart][demo]\n\n[demo]: https://example.invalid/chart.png"
        assertTrue(markdownImageFallbacks(source, emptySet()) { "Unavailable: $it" }
            .startsWith("![chart](https://example.invalid/chart.png)"))
    }
    @Test fun localImageBecomesFilenameWithoutLeakingPath() {
        val result = markdownImageFallbacks("Before ![chart](</Users/demo/chart 1.png>) after", emptySet()) { "Unavailable: $it" }
        assertTrue(result.contains("chart 1.png"))
        assertFalse(result.contains("/Users/demo"))
        assertFalse(result.contains("!["))
        assertTrue(result.contains("Before")); assertTrue(result.contains("after"))
    }
    @Test fun failedNetworkImageCollapsesButSuccessfulLinkStays() {
        val failed = "https://example.invalid/demo-broken.png"
        val source = "![bad]($failed) ![good](https://example.invalid/demo-good.png)"
        val result = markdownImageFallbacks(source, setOf(failed)) { "Unavailable: $it" }
        assertTrue(result.contains("Unavailable: demo-broken.png"))
        assertTrue(result.contains("![good](https://example.invalid/demo-good.png)"))
    }
    @Test fun codeExamplesAreNotRewritten() {
        val source = "```md\n![demo](/tmp/demo.png)\n```\n`![demo](/tmp/demo.png)`\n\n    ![demo](/tmp/demo.png)"
        assertEquals(source, markdownImageFallbacks(source, emptySet()) { "Unavailable: $it" })
    }
}
