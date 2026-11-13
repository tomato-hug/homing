package dev.agentpush.poc.ui

import org.intellij.markdown.MarkdownElementTypes
import org.intellij.markdown.IElementType
import org.intellij.markdown.ast.ASTNode
import org.intellij.markdown.flavours.commonmark.CommonMarkFlavourDescriptor
import org.intellij.markdown.parser.MarkdownParser
import java.net.URLDecoder
import java.util.Locale
import com.mikepenz.markdown.utils.getUnescapedTextInNode

// Parse image nodes so examples inside code fences, inline code and indented
// code remain byte-for-byte unchanged. This never opens local filesystem paths.
internal fun markdownImageFallbacks(source: String, failed: Set<String>, label: (String) -> String): String {
    if (!source.contains("![")) return source
    val tree = MarkdownParser(CommonMarkFlavourDescriptor()).buildMarkdownTreeFromString(source)
    val replacements = mutableListOf<Pair<IntRange, String>>()
    fun find(node: ASTNode, type: IElementType): ASTNode? {
        if (node.type == type) return node
        return node.children.firstNotNullOfOrNull { find(it, type) }
    }
    fun normalizeLabel(label: String) = label.removePrefix("[").removeSuffix("]").trim().replace(Regex("\\s+"), " ").lowercase(Locale.ROOT)
    val definitions = mutableMapOf<String, String>()
    fun collectDefinitions(node: ASTNode) {
        if (node.type == MarkdownElementTypes.LINK_DEFINITION) {
            val key = find(node, MarkdownElementTypes.LINK_LABEL)?.getUnescapedTextInNode(source)
            val link = find(node, MarkdownElementTypes.LINK_DESTINATION)?.getUnescapedTextInNode(source)
            if (key != null && link != null) definitions.putIfAbsent(normalizeLabel(key), link.trim('<', '>'))
        } else node.children.forEach { collectDefinitions(it) }
    }
    collectDefinitions(tree)
    fun visit(node: ASTNode) {
        if (node.type == MarkdownElementTypes.IMAGE) {
            val target = find(node, MarkdownElementTypes.LINK_DESTINATION)
            val alt = find(node, MarkdownElementTypes.LINK_TEXT)?.getUnescapedTextInNode(source)?.removePrefix("[")?.removeSuffix("]")
                ?: source.substring(node.startOffset, node.endOffset).substringAfter("![").substringBefore(']')
            val link = target?.getUnescapedTextInNode(source)?.trim('<', '>') ?: run {
                val explicit = find(node, MarkdownElementTypes.LINK_LABEL)?.getUnescapedTextInNode(source).orEmpty()
                definitions[normalizeLabel(explicit).ifBlank { normalizeLabel(alt) }]
            } ?: return
            val remote = link.startsWith("https://", true) || link.startsWith("http://", true) || link.startsWith("data:image/", true)
            if (!remote || link in failed) {
                val decoded = runCatching { URLDecoder.decode(link.replace("+", "%2B"), "UTF-8") }.getOrDefault(link)
                val name = decoded.substringBefore('?').substringBefore('#').trimEnd('/').substringAfterLast('/').substringAfterLast('\\').ifBlank { "image" }
                val safeName = name.replace(Regex("[\\\\`*_{}\\[\\]()<>#!|]")) { "\\${it.value}" }
                replacements += (node.startOffset until node.endOffset) to label(safeName)
            } else if (target == null || source.substring(target.startOffset, target.endOffset).startsWith('<')) {
                // The renderer expects an inline destination even for references.
                val safeAlt = alt.replace(Regex("[\\\\\\[\\]]")) { "\\${it.value}" }
                val inlineUrl = link.replace("\\", "%5C").replace("<", "%3C").replace(">", "%3E").replace(" ", "%20")
                    .replace("(", "\\(").replace(")", "\\)")
                replacements += (node.startOffset until node.endOffset) to "![$safeAlt]($inlineUrl)"
            }
        } else node.children.forEach { visit(it) }
    }
    visit(tree)
    val out = StringBuilder(source)
    replacements.asReversed().forEach { (range, replacement) -> out.replace(range.first, range.last + 1, replacement) }
    return out.toString()
}
