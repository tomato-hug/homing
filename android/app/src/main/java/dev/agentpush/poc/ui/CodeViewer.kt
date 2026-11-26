package dev.agentpush.poc.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.WrapText
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Code
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.KeyboardArrowDown
import androidx.compose.material.icons.outlined.KeyboardArrowUp
import androidx.compose.material.icons.outlined.Language
import androidx.compose.material.icons.outlined.Remove
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import dev.agentpush.poc.Attachment
import dev.agentpush.poc.ui.theme.HomingMono
import kotlinx.coroutines.launch
import java.net.URLDecoder
import java.net.URLEncoder

/** A text document shown in the built-in code / text viewer. */
data class CodeViewerDoc(
    val title: String,
    val content: String,
    val language: String = "",
    val subtitle: String = "",
    val line: Int = 0,
    val truncated: Boolean = false,
)

/** Result of reading a project file from the computer. */
data class RemoteTextFile(
    val path: String,
    val name: String,
    val content: String,
    val language: String = "",
    val line: Int = 0,
    val truncated: Boolean = false,
    val size: Long = 0L,
)

/** Opens documents in the built-in viewer from anywhere inside a conversation. */
interface DocumentViewerHost {
    fun openText(title: String, content: String, language: String = "", subtitle: String = "")
    fun openPath(path: String)
    fun openAttachment(attachment: Attachment)
}

val LocalDocumentViewer = staticCompositionLocalOf<DocumentViewerHost?> { null }

internal const val HOMING_FILE_SCHEME = "homing-file:"

private val TEXT_EXTENSIONS = setOf(
    "kt", "kts", "java", "go", "rs", "py", "js", "jsx", "ts", "tsx", "mjs", "cjs", "json", "jsonc",
    "yaml", "yml", "toml", "md", "markdown", "txt", "html", "htm", "css", "scss", "less", "sh", "bash",
    "zsh", "fish", "swift", "m", "mm", "c", "h", "cpp", "hpp", "cc", "cs", "rb", "php", "sql", "xml",
    "gradle", "properties", "ini", "conf", "cfg", "lock", "vue", "svelte", "dart", "lua", "r", "scala",
    "proto", "graphql", "gql", "csv", "tsv", "log", "mod", "sum", "plist", "env", "example", "sample",
    "patch", "diff", "tf", "hcl", "dockerfile", "makefile", "cmake", "mk", "ex", "exs", "erl", "hs",
    "clj", "zig", "nim", "jl", "pl", "ps1", "bat", "svg",
)

private val TEXT_BARE_NAMES = setOf(
    "makefile", "dockerfile", "readme", "license", "agents.md", "claude.md", "gemfile", "procfile",
)

internal fun languageForName(name: String): String {
    val lower = name.trim().lowercase().substringAfterLast('/')
    if (lower == "dockerfile") return "dockerfile"
    if (lower == "makefile") return "makefile"
    return lower.substringAfterLast('.', "")
}

internal fun isTextLikeAttachment(attachment: Attachment): Boolean {
    if (attachment.isImage || attachment.isPdf) return false
    val mime = attachment.mime.lowercase()
    if (mime.startsWith("text/") && !mime.contains("html")) return true
    if (mime.contains("json") || mime.contains("xml") || mime.contains("yaml") ||
        mime.contains("javascript") || mime.contains("x-sh") || mime.contains("toml")
    ) {
        return true
    }
    val name = attachment.name.lowercase()
    if (name.substringAfterLast('/') in TEXT_BARE_NAMES) return true
    return languageForName(name) in TEXT_EXTENSIONS && !attachment.isHtml
}

private val lineSuffix = Regex(":\\d+(:\\d+)?$")

/** True when [raw] reads like a path to a source / text file rather than prose or code. */
internal fun looksLikeFilePath(raw: String): Boolean {
    val text = raw.trim()
    if (text.length < 3 || text.length > 260) return false
    if (text.any { it.isWhitespace() }) return false
    if (text.contains("://") || text.startsWith("@") || text.startsWith("-")) return false
    if (text.any { it in "()=,;{}<>|\"'*?$" }) return false
    val path = text.replace(lineSuffix, "")
    if (path.endsWith("/")) return false
    if (Regex("^[0-9.]+$").matches(path)) return false
    val base = path.substringAfterLast('/').lowercase()
    val ext = base.substringAfterLast('.', "")
    val knownExt = ext.isNotBlank() && ext in TEXT_EXTENSIONS
    if (!path.contains('/')) {
        return knownExt || base in TEXT_BARE_NAMES
    }
    if (path.startsWith("//")) return false
    return knownExt || base in TEXT_BARE_NAMES || (ext.isNotBlank() && ext.length <= 10 && ext.all { it.isLetterOrDigit() })
}

internal fun encodeFileLink(path: String): String =
    HOMING_FILE_SCHEME + URLEncoder.encode(path, Charsets.UTF_8.name()).replace("+", "%20")

internal fun decodeFileLink(uri: String): String? {
    if (!uri.startsWith(HOMING_FILE_SCHEME)) return null
    return runCatching {
        URLDecoder.decode(uri.removePrefix(HOMING_FILE_SCHEME), Charsets.UTF_8.name())
    }.getOrNull()
}

private val inlineCode = Regex("(?<![`\\[])`([^`\\n]{2,260})`(?!`)")
private val barePath = Regex("(?<![\\w/\\[(.\\-~])((?:~|\\.{1,2})?/(?:[\\w.\\-@]+/)+[\\w.\\-@]+\\.[A-Za-z0-9]{1,10}(?::\\d+(?::\\d+)?)?)")

/**
 * Turns file paths mentioned in an agent reply into tappable links handled by
 * the in-app viewer. Fenced code blocks and existing markdown links are left
 * untouched.
 */
internal fun linkifyFilePaths(markdown: String): String {
    if (!markdown.contains('/') && !markdown.contains('`')) return markdown
    val out = StringBuilder(markdown.length + 64)
    var inFence = false
    markdown.lineSequence().forEachIndexed { index, line ->
        if (index > 0) out.append('\n')
        val trimmed = line.trimStart()
        if (trimmed.startsWith("```") || trimmed.startsWith("~~~")) {
            inFence = !inFence
            out.append(line)
            return@forEachIndexed
        }
        if (inFence || trimmed.startsWith("    ") || trimmed.startsWith("\t")) {
            out.append(line)
            return@forEachIndexed
        }
        out.append(linkifyLine(line))
    }
    return out.toString()
}

private fun linkifyLine(line: String): String {
    val result = StringBuilder()
    var cursor = 0
    for (match in inlineCode.findAll(line)) {
        val start = match.range.first
        val end = match.range.last + 1
        val before = line.substring(cursor, start)
        result.append(linkifyBarePaths(before))
        val code = match.groupValues[1]
        val alreadyLinked = start > 0 && line[start - 1] == '[' ||
            (end + 1 < line.length && line[end] == ']' && line[end + 1] == '(')
        if (!alreadyLinked && looksLikeFilePath(code)) {
            result.append("[`").append(code).append("`](").append(encodeFileLink(code.trim())).append(')')
        } else {
            result.append(match.value)
        }
        cursor = end
    }
    result.append(linkifyBarePaths(line.substring(cursor)))
    return result.toString()
}

private fun linkifyBarePaths(segment: String): String {
    if (!segment.contains('/')) return segment
    // Leave markdown link / image syntax and URLs alone.
    if (segment.contains("](") || segment.contains("://")) return segment
    return barePath.replace(segment) { match ->
        val path = match.groupValues[1]
        if (looksLikeFilePath(path)) "[$path](${encodeFileLink(path)})" else match.value
    }
}

// ----------------------------------------------------------------- highlight

private val codeKeywords = setOf(
    "if", "else", "for", "while", "do", "return", "func", "fun", "fn", "val", "var", "let", "const",
    "class", "struct", "interface", "trait", "impl", "import", "package", "def", "from", "as", "try",
    "catch", "finally", "throw", "throws", "raise", "except", "new", "public", "private", "protected",
    "internal", "static", "final", "override", "open", "abstract", "sealed", "data", "object",
    "companion", "suspend", "async", "await", "yield", "true", "false", "null", "nil", "None", "True",
    "False", "self", "this", "super", "type", "typealias", "enum", "pub", "use", "mod", "match", "case",
    "switch", "default", "break", "continue", "in", "is", "not", "and", "or", "go", "defer", "select",
    "chan", "map", "range", "export", "function", "extends", "implements", "lambda", "with", "when",
    "where", "mut", "elif", "pass", "void", "int", "long", "bool", "boolean", "string", "float",
    "double", "char", "byte", "unsigned", "echo", "then", "fi", "esac", "done", "local", "readonly",
    "goto", "lateinit", "init", "constructor", "get", "set", "of", "typeof", "instanceof", "delete",
)

private val hashCommentLangs = setOf(
    "py", "sh", "bash", "zsh", "fish", "yaml", "yml", "toml", "rb", "r", "pl", "conf", "ini", "cfg",
    "properties", "dockerfile", "makefile", "mk", "cmake", "ex", "exs", "tf", "hcl", "env", "gitignore",
    "python", "shell", "ruby",
)

private val dashCommentLangs = setOf("sql", "lua", "hs")

private val hlDefault = Color(0xFFE6EDF3)
private val hlKeyword = Color(0xFFFF7B72)
private val hlString = Color(0xFFA5D6FF)
private val hlComment = Color(0xFF8B949E)
private val hlNumber = Color(0xFF79C0FF)
private val hlType = Color(0xFFFFA657)
private val hlMatch = Color(0x66F2CC60)

internal fun normalizeLanguage(raw: String): String = when (val lang = raw.trim().lowercase()) {
    "kotlin" -> "kt"
    "golang" -> "go"
    "python", "python3" -> "py"
    "javascript", "node" -> "js"
    "typescript" -> "ts"
    "shell", "console", "terminal", "sh", "bash", "zsh" -> "sh"
    "yml" -> "yaml"
    "markdown" -> "md"
    "rust" -> "rs"
    "ruby" -> "rb"
    "csharp", "c#" -> "cs"
    "c++" -> "cpp"
    else -> lang
}

/** Highlights all lines in one pass, tracking multi-line block comments. */
internal fun highlightLines(content: String, language: String): List<AnnotatedString> {
    val lang = normalizeLanguage(language)
    val lines = content.split('\n')
    val out = ArrayList<AnnotatedString>(lines.size)
    var inBlock = false
    val plain = lang in setOf("txt", "log", "csv", "tsv", "md", "")
    for (line in lines) {
        if (plain) {
            out.add(AnnotatedString(line.trimEnd('\r')))
            continue
        }
        val (annotated, stillBlock) = highlightLine(line.trimEnd('\r'), lang, inBlock)
        inBlock = stillBlock
        out.add(annotated)
    }
    return out
}

private fun highlightLine(line: String, lang: String, startInBlock: Boolean): Pair<AnnotatedString, Boolean> {
    var inBlock = startInBlock
    val hashComments = lang in hashCommentLangs
    val dashComments = lang in dashCommentLangs
    val slashComments = !hashComments && lang != "md"
    val markupComments = lang in setOf("html", "htm", "xml", "svg", "vue", "svelte", "plist")
    val text = buildAnnotatedString {
        var i = 0
        val n = line.length
        while (i < n) {
            if (inBlock) {
                val endToken = if (markupComments) "-->" else "*/"
                val end = line.indexOf(endToken, i)
                val stop = if (end < 0) n else end + endToken.length
                withStyle(SpanStyle(color = hlComment)) { append(line.substring(i, stop)) }
                i = stop
                if (end >= 0) inBlock = false
                continue
            }
            val c = line[i]
            when {
                markupComments && line.startsWith("<!--", i) -> {
                    inBlock = true
                }
                slashComments && line.startsWith("/*", i) -> {
                    inBlock = true
                }
                (slashComments && line.startsWith("//", i)) ||
                    (hashComments && c == '#') ||
                    (dashComments && line.startsWith("--", i)) -> {
                    withStyle(SpanStyle(color = hlComment)) { append(line.substring(i)) }
                    i = n
                }
                c == '"' || c == '\'' || c == '`' -> {
                    var j = i + 1
                    while (j < n) {
                        if (line[j] == '\\') {
                            j += 2
                            continue
                        }
                        if (line[j] == c) break
                        j++
                    }
                    val stop = (j + 1).coerceAtMost(n)
                    withStyle(SpanStyle(color = hlString)) { append(line.substring(i, stop)) }
                    i = stop
                }
                c.isDigit() && (i == 0 || !line[i - 1].isLetterOrDigit() && line[i - 1] != '_') -> {
                    var j = i + 1
                    while (j < n && (line[j].isLetterOrDigit() || line[j] == '.' || line[j] == '_')) j++
                    withStyle(SpanStyle(color = hlNumber)) { append(line.substring(i, j)) }
                    i = j
                }
                c.isLetter() || c == '_' -> {
                    var j = i + 1
                    while (j < n && (line[j].isLetterOrDigit() || line[j] == '_')) j++
                    val word = line.substring(i, j)
                    val color = when {
                        word in codeKeywords -> hlKeyword
                        word.first().isUpperCase() && word.length > 1 -> hlType
                        else -> hlDefault
                    }
                    withStyle(SpanStyle(color = color)) { append(word) }
                    i = j
                }
                else -> {
                    withStyle(SpanStyle(color = hlDefault)) { append(c) }
                    i++
                }
            }
        }
    }
    return text to inBlock
}

private fun withSearchMatches(text: AnnotatedString, query: String): AnnotatedString {
    if (query.isBlank()) return text
    val raw = text.text
    var index = raw.indexOf(query, ignoreCase = true)
    if (index < 0) return text
    return buildAnnotatedString {
        append(text)
        while (index >= 0) {
            addStyle(SpanStyle(background = hlMatch), index, index + query.length)
            index = raw.indexOf(query, index + query.length, ignoreCase = true)
        }
    }
}

// ----------------------------------------------------------------- viewer UI

private const val MAX_VIEWER_LINES = 20_000

@Composable
fun CodeViewerDialog(
    doc: CodeViewerDoc,
    onDismiss: () -> Unit,
    onOpenUrl: (String) -> Unit = {},
) {
    val clipboard = LocalClipboardManager.current
    val lang = remember(doc) { normalizeLanguage(doc.language.ifBlank { languageForName(doc.title) }) }
    val isMarkdown = lang == "md"
    val isHtml = lang in setOf("html", "htm", "svg")
    val allLines = remember(doc) { highlightLines(doc.content, lang) }
    val lines = remember(allLines) { allLines.take(MAX_VIEWER_LINES) }
    var wrap by remember(doc) { mutableStateOf(lang in setOf("md", "txt", "log", "")) }
    var preview by remember(doc) { mutableStateOf(isMarkdown) }
    var htmlPreview by remember(doc) { mutableStateOf(false) }
    var fontScale by remember { mutableFloatStateOf(1f) }
    var searchOpen by remember(doc) { mutableStateOf(false) }
    var query by remember(doc) { mutableStateOf("") }
    var matchCursor by remember(doc) { mutableIntStateOf(0) }
    var copied by remember(doc) { mutableStateOf(false) }
    val listState = rememberLazyListState()
    val scope = rememberCoroutineScope()
    val matches = remember(lines, query) {
        if (query.isBlank()) emptyList() else lines.indices.filter { lines[it].text.contains(query, ignoreCase = true) }
    }
    val fontSize = (12.5f * fontScale).sp
    val lineHeight = (19f * fontScale).sp
    val codeStyle = TextStyle(fontFamily = HomingMono, fontSize = fontSize, lineHeight = lineHeight, color = hlDefault)
    val gutterDigits = lines.size.toString().length.coerceAtLeast(2)
    val measurer = rememberTextMeasurer()
    val density = LocalDensity.current
    val charWidthPx = remember(fontScale) {
        measurer.measure(AnnotatedString("M"), codeStyle).size.width.coerceAtLeast(1)
    }
    val longest = remember(lines) { lines.maxOfOrNull { it.length } ?: 0 }
    val contentWidth = with(density) {
        ((longest + gutterDigits + 4) * charWidthPx).toDp().coerceIn(200.dp, 12_000.dp)
    }

    LaunchedEffect(doc, preview) {
        if (!preview && doc.line > 1) {
            listState.scrollToItem((doc.line - 4).coerceAtLeast(0))
        }
    }
    LaunchedEffect(matches, matchCursor) {
        if (matches.isNotEmpty()) {
            listState.animateScrollToItem((matches[matchCursor.coerceIn(matches.indices)] - 3).coerceAtLeast(0))
        }
    }

    Dialog(
        onDismissRequest = onDismiss,
        properties = DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false),
    ) {
        Surface(modifier = Modifier.fillMaxSize().testTag("code-viewer"), color = Color(0xFF0D1117)) {
            Column(modifier = Modifier.fillMaxSize().statusBarsPadding().navigationBarsPadding()) {
                Row(
                    modifier = Modifier.fillMaxWidth().padding(start = 4.dp, end = 4.dp, top = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    IconButton(onClick = onDismiss) {
                        Icon(Icons.Outlined.Close, contentDescription = "关闭", tint = hlDefault)
                    }
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = doc.title.ifBlank { "代码" },
                            style = MaterialTheme.typography.titleSmall,
                            color = hlDefault,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        Text(
                            text = buildString {
                                append(lang.ifBlank { "text" }.uppercase())
                                append(" · ")
                                append(allLines.size).append(" 行")
                                if (doc.truncated || allLines.size > MAX_VIEWER_LINES) append(" · 已截断")
                                if (doc.subtitle.isNotBlank()) append(" · ").append(doc.subtitle)
                            },
                            style = MaterialTheme.typography.labelSmall.copy(fontFamily = HomingMono, fontSize = 10.5.sp),
                            color = hlComment,
                            maxLines = 1,
                            overflow = TextOverflow.MiddleEllipsis,
                        )
                    }
                    if (isMarkdown) {
                        IconButton(onClick = { preview = !preview }, modifier = Modifier.testTag("code-viewer-preview")) {
                            Icon(
                                if (preview) Icons.Outlined.Code else Icons.Outlined.Visibility,
                                contentDescription = if (preview) "查看源码" else "预览",
                                tint = hlDefault,
                            )
                        }
                    }
                    if (isHtml) {
                        IconButton(onClick = { htmlPreview = true }) {
                            Icon(Icons.Outlined.Language, contentDescription = "预览网页", tint = hlDefault)
                        }
                    }
                    if (!preview) {
                        IconButton(onClick = { searchOpen = !searchOpen }, modifier = Modifier.testTag("code-viewer-search")) {
                            Icon(Icons.Outlined.Search, contentDescription = "搜索", tint = hlDefault)
                        }
                        IconButton(onClick = { wrap = !wrap }) {
                            Icon(
                                Icons.AutoMirrored.Outlined.WrapText,
                                contentDescription = if (wrap) "取消自动换行" else "自动换行",
                                tint = if (wrap) Color(0xFF58A6FF) else hlDefault,
                            )
                        }
                    }
                    IconButton(onClick = {
                        clipboard.setText(AnnotatedString(doc.content))
                        copied = true
                    }) {
                        Icon(
                            Icons.Outlined.ContentCopy,
                            contentDescription = "复制全部",
                            tint = if (copied) Color(0xFF7EE2A8) else hlDefault,
                        )
                    }
                }
                if (searchOpen && !preview) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 12.dp, vertical = 4.dp)
                            .background(Color(0xFF161B22), RoundedCornerShape(8.dp))
                            .padding(horizontal = 10.dp, vertical = 6.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        Icon(Icons.Outlined.Search, contentDescription = null, tint = hlComment, modifier = Modifier.size(16.dp))
                        Box(modifier = Modifier.weight(1f)) {
                            if (query.isEmpty()) {
                                Text("在文件中查找…", style = TextStyle(fontSize = 13.sp, color = hlComment))
                            }
                            BasicTextField(
                                value = query,
                                onValueChange = {
                                    query = it
                                    matchCursor = 0
                                },
                                singleLine = true,
                                textStyle = TextStyle(fontSize = 13.sp, color = hlDefault, fontFamily = HomingMono),
                                cursorBrush = SolidColor(Color(0xFF58A6FF)),
                                modifier = Modifier.fillMaxWidth().testTag("code-viewer-search-input"),
                            )
                        }
                        Text(
                            text = if (matches.isEmpty()) "0/0" else "${matchCursor + 1}/${matches.size}",
                            style = TextStyle(fontSize = 11.sp, color = hlComment, fontFamily = HomingMono),
                        )
                        IconButton(
                            onClick = { if (matches.isNotEmpty()) matchCursor = (matchCursor - 1 + matches.size) % matches.size },
                            modifier = Modifier.size(28.dp),
                        ) { Icon(Icons.Outlined.KeyboardArrowUp, contentDescription = "上一个", tint = hlDefault) }
                        IconButton(
                            onClick = { if (matches.isNotEmpty()) matchCursor = (matchCursor + 1) % matches.size },
                            modifier = Modifier.size(28.dp),
                        ) { Icon(Icons.Outlined.KeyboardArrowDown, contentDescription = "下一个", tint = hlDefault) }
                    }
                }
                Row(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.End,
                ) {
                    IconButton(onClick = { fontScale = (fontScale - 0.1f).coerceAtLeast(0.7f) }, modifier = Modifier.size(30.dp)) {
                        Icon(Icons.Outlined.Remove, contentDescription = "缩小字号", tint = hlComment, modifier = Modifier.size(16.dp))
                    }
                    Text("${(fontScale * 100).toInt()}%", style = TextStyle(fontSize = 10.5.sp, color = hlComment, fontFamily = HomingMono))
                    IconButton(onClick = { fontScale = (fontScale + 0.1f).coerceAtMost(1.8f) }, modifier = Modifier.size(30.dp)) {
                        Icon(Icons.Outlined.Add, contentDescription = "放大字号", tint = hlComment, modifier = Modifier.size(16.dp))
                    }
                }
                if (preview && isMarkdown) {
                    Column(
                        modifier = Modifier
                            .fillMaxSize()
                            .verticalScroll(rememberScrollState())
                            .background(MaterialTheme.colorScheme.background)
                            .padding(16.dp),
                    ) {
                        RichMarkdown(
                            source = doc.content,
                            onCopyCode = { clipboard.setText(AnnotatedString(it)) },
                            onOpenUrl = onOpenUrl,
                        )
                    }
                } else {
                    val bodyModifier = if (wrap) {
                        Modifier.fillMaxSize()
                    } else {
                        Modifier.fillMaxSize().horizontalScroll(rememberScrollState())
                    }
                    SelectionContainer(modifier = bodyModifier) {
                        LazyColumn(
                            state = listState,
                            modifier = if (wrap) Modifier.fillMaxSize() else Modifier.width(contentWidth).fillMaxSize(),
                            contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 32.dp),
                        ) {
                            itemsIndexed(lines, key = { index, _ -> index }) { index, line ->
                                val isTarget = doc.line > 0 && index == doc.line - 1
                                val isMatch = matches.isNotEmpty() && matches.getOrNull(matchCursor) == index
                                Row(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .background(
                                            when {
                                                isMatch -> Color(0x33F2CC60)
                                                isTarget -> Color(0x331F6FEB)
                                                else -> Color.Transparent
                                            },
                                        ),
                                ) {
                                    Text(
                                        text = (index + 1).toString(),
                                        modifier = Modifier
                                            .width(with(density) { ((gutterDigits + 1) * charWidthPx).toDp() })
                                            .padding(end = 4.dp),
                                        style = codeStyle.copy(color = Color(0xFF6E7681)),
                                        textAlign = TextAlign.End,
                                        maxLines = 1,
                                    )
                                    Text(
                                        text = withSearchMatches(line, query),
                                        modifier = Modifier.padding(start = 8.dp, end = 12.dp),
                                        style = codeStyle,
                                        softWrap = wrap,
                                    )
                                }
                            }
                            if (allLines.size > MAX_VIEWER_LINES) {
                                item {
                                    Text(
                                        "仅显示前 $MAX_VIEWER_LINES 行",
                                        modifier = Modifier.padding(12.dp),
                                        style = codeStyle.copy(color = hlComment),
                                    )
                                }
                            }
                        }
                    }
                }
            }
        }
    }
    if (htmlPreview) {
        HtmlPreviewDialog(html = doc.content, onDismiss = { htmlPreview = false })
    }
    LaunchedEffect(copied) {
        if (copied) {
            kotlinx.coroutines.delay(1500)
            copied = false
        }
    }
    @Suppress("UNUSED_VARIABLE") val unused = scope
}

/**
 * Hosts the viewer for a conversation: provides [LocalDocumentViewer] to its
 * content, loads remote files / attachments, and shows loading or errors.
 */
@Composable
fun DocumentViewerScope(
    loadPath: suspend (String) -> RemoteTextFile,
    loadAttachment: suspend (Attachment) -> String,
    onOpenUrl: (String) -> Unit,
    content: @Composable () -> Unit,
) {
    var doc by remember { mutableStateOf<CodeViewerDoc?>(null) }
    var loading by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val host = remember(loadPath, loadAttachment) {
        object : DocumentViewerHost {
            override fun openText(title: String, content: String, language: String, subtitle: String) {
                doc = CodeViewerDoc(title = title, content = content, language = language, subtitle = subtitle)
            }

            override fun openPath(path: String) {
                loading = path.substringAfterLast('/')
                error = null
                scope.launch {
                    runCatching { loadPath(path) }
                        .onSuccess { file ->
                            doc = CodeViewerDoc(
                                title = file.name.ifBlank { path.substringAfterLast('/') },
                                content = file.content,
                                language = file.language.ifBlank { languageForName(file.name) },
                                subtitle = file.path,
                                line = file.line,
                                truncated = file.truncated,
                            )
                        }
                        .onFailure { error = it.message ?: "无法打开文件" }
                    loading = null
                }
            }

            override fun openAttachment(attachment: Attachment) {
                loading = attachment.name
                error = null
                scope.launch {
                    runCatching { loadAttachment(attachment) }
                        .onSuccess { text ->
                            doc = CodeViewerDoc(
                                title = attachment.name,
                                content = text,
                                language = languageForName(attachment.name),
                                subtitle = attachment.sizeLabel(),
                            )
                        }
                        .onFailure { error = it.message ?: "无法打开附件" }
                    loading = null
                }
            }
        }
    }
    CompositionLocalProvider(LocalDocumentViewer provides host) {
        content()
    }
    doc?.let { current ->
        CodeViewerDialog(doc = current, onDismiss = { doc = null }, onOpenUrl = onOpenUrl)
    }
    loading?.let { name ->
        Dialog(onDismissRequest = { loading = null }) {
            Surface(shape = RoundedCornerShape(14.dp), color = MaterialTheme.colorScheme.surface) {
                Row(
                    modifier = Modifier.padding(horizontal = 20.dp, vertical = 16.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp)
                    Text("正在从电脑读取 $name…", style = MaterialTheme.typography.bodyMedium)
                }
            }
        }
    }
    error?.let { message ->
        Dialog(onDismissRequest = { error = null }) {
            Surface(shape = RoundedCornerShape(14.dp), color = MaterialTheme.colorScheme.surface) {
                Column(modifier = Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Text("无法打开", style = MaterialTheme.typography.titleMedium)
                    Text(message, style = MaterialTheme.typography.bodyMedium)
                    androidx.compose.material3.TextButton(
                        onClick = { error = null },
                        modifier = Modifier.align(Alignment.End),
                    ) { Text("知道了") }
                }
            }
        }
    }
}
