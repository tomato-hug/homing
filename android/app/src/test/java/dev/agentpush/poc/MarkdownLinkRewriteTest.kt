package dev.agentpush.poc

import dev.agentpush.poc.ui.absolutizeMarkdown
import dev.agentpush.poc.ui.rewriteMarkdownImageUrls
import org.junit.Assert.assertEquals
import org.junit.Test

class MarkdownLinkRewriteTest {
    private val settings = AppSettings("http://127.0.0.1:18820", "demo-phone-token")

    @Test fun serverLinksResolveWithoutExtraSlashAndReferencesResolve() {
        val source = "[file](/f/demo/file.png)\n\n![chart][demo]\n\n[demo]: </f/demo/chart.png>"
        assertEquals("[file](http://127.0.0.1:18820/f/demo/file.png)\n\n![chart][demo]\n\n[demo]: <http://127.0.0.1:18820/f/demo/chart.png>", absolutizeMarkdown(source, settings))
    }

    @Test fun computerLocalPathsAndCodeRemainUnchanged() {
        val source = "![local](/tmp/demo.png)\n\n`![code](/f/demo/code.png)`\n\n```markdown\n![code](/tmp/demo-code.png)\n[file](/f/demo/file.png)\n```\n\n    ![code](/f/demo/indented.png)"
        assertEquals(source, absolutizeMarkdown(source, settings))
    }

    @Test fun attachmentRewriteLeavesMatchingCodeExamplesUnchanged() {
        val source = "![chart](/tmp/demo.png)\n\n`![chart](/tmp/demo.png)`\n\n```markdown\n![chart](/tmp/demo.png)\n```"
        val attachment = Attachment(id = "demo-image", name = "demo.png", mime = "image/png", size = 1, kind = "image", url = "/f/demo/image")
        assertEquals("![chart](http://127.0.0.1:18820/f/demo/image)\n\n`![chart](/tmp/demo.png)`\n\n```markdown\n![chart](/tmp/demo.png)\n```", rewriteMarkdownImageUrls(source, listOf(attachment), settings))
    }
}
