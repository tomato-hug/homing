package dev.agentpush.poc.ui

import android.content.pm.ActivityInfo
import android.webkit.WebChromeClient
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Code
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.FitScreen
import androidx.compose.material.icons.outlined.OpenInFull
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.ScreenRotation
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.platform.UriHandler
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import coil3.compose.AsyncImagePainter
import coil3.compose.rememberAsyncImagePainter
import com.mikepenz.markdown.model.ImageTransformer
import com.mikepenz.markdown.model.ImageData
import com.mikepenz.markdown.compose.Markdown
import com.mikepenz.markdown.compose.components.markdownComponents
import com.mikepenz.markdown.compose.elements.MarkdownCodeFence
import com.mikepenz.markdown.compose.elements.MarkdownImage
import com.mikepenz.markdown.m3.markdownColor
import com.mikepenz.markdown.m3.markdownTypography
import dev.agentpush.poc.AgentApi
import dev.agentpush.poc.AppSettings
import dev.agentpush.poc.Attachment
import dev.agentpush.poc.R
import dev.agentpush.poc.ui.theme.HomingMono
import java.net.URLDecoder

@Composable
fun RichMarkdown(
    source: String,
    modifier: Modifier = Modifier,
    onCopyCode: (String) -> Unit,
    onOpenUrl: (String) -> Unit,
) {
    val activeSettings = LocalAppSettings.current
    val currentUriHandler = LocalUriHandler.current
    val viewer = LocalDocumentViewer.current
    val linkHandler = remember(currentUriHandler, viewer) {
        object : UriHandler {
            override fun openUri(uri: String) {
                val filePath = decodeFileLink(uri)
                    ?: uri.removePrefix("file://").takeIf {
                        uri.startsWith("file://") || (uri.startsWith("/") && looksLikeFilePath(uri))
                    }
                when {
                    filePath != null && viewer != null -> viewer.openPath(filePath)
                    uri.startsWith("http://") || uri.startsWith("https://") -> onOpenUrl(uri)
                    else -> runCatching { currentUriHandler.openUri(uri) }
                }
            }
        }
    }
    var previewHtml by remember { mutableStateOf<String?>(null) }
    var previewImageUrl by remember { mutableStateOf<String?>(null) }
    var localCodeDoc by remember { mutableStateOf<CodeViewerDoc?>(null) }
    val renderLimit = 80_000
    val truncated = source.length > renderLimit
    val failedImages = remember(source) { mutableStateMapOf<String, Boolean>() }
    val failedLinks = failedImages.keys.toSet()
    val unavailableImage = stringResource(R.string.image_unavailable)
    val renderedSource = remember(source, failedLinks, unavailableImage, viewer != null) {
        val base = markdownImageFallbacks(source.take(renderLimit), failedLinks) { String.format(unavailableImage, it) }
        if (viewer != null) linkifyFilePaths(base) else base
    }
    val images = remember(source) {
        object : ImageTransformer {
            @Composable
            override fun transform(link: String): ImageData {
                val url = link.trim('<', '>')
                val painter = rememberAsyncImagePainter(url, onState = { state ->
                    if (state is AsyncImagePainter.State.Error) failedImages[url] = true
                })
                return ImageData(painter)
            }
        }
    }

    SelectionContainer {
        Column(modifier = modifier.fillMaxWidth()) {
            CompositionLocalProvider(LocalUriHandler provides linkHandler) {
                Markdown(
                    content = renderedSource,
                    modifier = Modifier.fillMaxWidth(),
                    colors = markdownColor(),
                    typography = markdownTypography(
                        h1 = MaterialTheme.typography.headlineMedium,
                        h2 = MaterialTheme.typography.headlineSmall,
                        h3 = MaterialTheme.typography.titleLarge,
                        h4 = MaterialTheme.typography.titleMedium,
                        h5 = MaterialTheme.typography.titleSmall,
                        h6 = MaterialTheme.typography.labelLarge,
                    ),
                    imageTransformer = images,
                    components = markdownComponents(
                        codeFence = { model ->
                            MarkdownCodeFence(
                                content = model.content,
                                node = model.node,
                                style = model.typography.code,
                            ) { code, language, style ->
                                RichCodeBlock(
                                    code = code,
                                    language = language,
                                    style = style,
                                    onCopy = { onCopyCode(code) },
                                    onPreview = if (looksLikeHtml(code, language)) {
                                        { previewHtml = code }
                                    } else {
                                        null
                                    },
                                    onExpand = {
                                        val label = language?.ifBlank { null } ?: "代码"
                                        if (viewer != null) {
                                            viewer.openText(label, code, language.orEmpty())
                                        } else {
                                            localCodeDoc = CodeViewerDoc(label, code, language.orEmpty())
                                        }
                                    },
                                )
                            }
                        },
                        image = { model ->
                            val rawUrl = remember(model.node, model.content) {
                                extractMarkdownImageUrl(model.content, model.node)
                            }
                            Box(
                                modifier = Modifier
                                    .clip(RoundedCornerShape(10.dp))
                                    .clickable(enabled = !rawUrl.isNullOrBlank()) {
                                        if (!rawUrl.isNullOrBlank()) {
                                            previewImageUrl = rawUrl
                                        }
                                    },
                            ) {
                                MarkdownImage(content = model.content, node = model.node)
                            }
                        },
                    ),
                )
            }
            if (truncated) {
                Text(
                    text = "内容过长，仅渲染前 ${renderLimit / 1000} 千字符。",
                    modifier = Modifier.padding(top = 8.dp),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }

    previewHtml?.let { html ->
        HtmlPreviewDialog(html = html, onDismiss = { previewHtml = null })
    }
    previewImageUrl?.let { imgUrl ->
        FullScreenUrlImageDialog(
            url = imgUrl,
            settings = activeSettings,
            onDismiss = { previewImageUrl = null },
        )
    }
    localCodeDoc?.let { doc ->
        CodeViewerDialog(doc = doc, onDismiss = { localCodeDoc = null }, onOpenUrl = onOpenUrl)
    }
}

private val INLINE_MD_IMAGE_DEST = Regex("!\\[[^\\]]*\\]\\(([^)\\s]+)(?:\\s+[\"'][^\"']*[\"'])?\\)")

private fun extractMarkdownImageUrl(
    content: String,
    node: org.intellij.markdown.ast.ASTNode,
): String? {
    val slice = runCatching {
        content.substring(
            node.startOffset.coerceIn(0, content.length),
            node.endOffset.coerceIn(0, content.length),
        )
    }.getOrNull() ?: return null
    return INLINE_MD_IMAGE_DEST.find(slice)?.groupValues?.getOrNull(1)?.trim('<', '>')
}

@Composable
private fun RichCodeBlock(
    code: String,
    language: String?,
    style: androidx.compose.ui.text.TextStyle,
    onCopy: () -> Unit,
    onPreview: (() -> Unit)?,
    onExpand: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(
                MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.7f),
                RoundedCornerShape(10.dp),
            ),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .background(MaterialTheme.colorScheme.surfaceVariant)
                .padding(start = 12.dp, end = 2.dp, top = 3.dp, bottom = 3.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(
                imageVector = Icons.Outlined.Code,
                contentDescription = null,
                modifier = Modifier.size(14.dp),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(
                text = language?.ifBlank { "代码" } ?: "代码",
                modifier = Modifier
                    .weight(1f)
                    .padding(start = 6.dp),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (onPreview != null) {
                IconButton(onClick = onPreview, modifier = Modifier.size(34.dp)) {
                    Icon(
                        imageVector = Icons.Outlined.PlayArrow,
                        contentDescription = "预览 HTML",
                        modifier = Modifier.size(18.dp),
                    )
                }
            }
            IconButton(onClick = onExpand, modifier = Modifier.size(34.dp)) {
                Icon(
                    imageVector = Icons.Outlined.OpenInFull,
                    contentDescription = "全屏查看代码",
                    modifier = Modifier.size(16.dp),
                )
            }
            IconButton(onClick = onCopy, modifier = Modifier.size(34.dp)) {
                Icon(
                    imageVector = Icons.Outlined.ContentCopy,
                    contentDescription = "复制代码",
                    modifier = Modifier.size(17.dp),
                )
            }
        }
        Text(
            text = code,
            modifier = Modifier
                .fillMaxWidth()
                .horizontalScroll(rememberScrollState())
                .padding(12.dp),
            style = style.copy(
                fontFamily = HomingMono,
                fontSize = 13.sp,
                lineHeight = 20.sp,
            ),
            softWrap = false,
        )
    }
}

private fun looksLikeHtml(code: String, language: String?): Boolean {
    val lang = language?.lowercase().orEmpty()
    if (lang in setOf("html", "htm", "xhtml", "svg")) return true
    val trimmed = code.trimStart().lowercase()
    return trimmed.startsWith("<!doctype html") ||
        trimmed.startsWith("<html") ||
        (trimmed.startsWith("<") && trimmed.contains("</") && trimmed.contains(">"))
}

fun rewriteMarkdownImageUrls(
    source: String,
    attachments: List<Attachment>,
    settings: AppSettings,
): String {
    val images = attachments.filter { it.isImage }
    if (images.isEmpty() || !source.contains("![")) return source
    return rewriteMarkdownDestinations(source, imagesOnly = true) { rawUrl ->
        val decoded = runCatching {
            URLDecoder.decode(rawUrl.replace("+", "%2B"), Charsets.UTF_8.name())
        }.getOrDefault(rawUrl)
        val image = images.firstOrNull { attachment ->
            decoded == attachment.name || decoded.endsWith("/${attachment.name}")
        }
        if (image != null) {
            AgentApi.attachmentUrl(settings, image)
        } else {
            null
        }
    }
}

@Composable
internal fun HtmlPreviewDialog(
    html: String,
    title: String = "HTML 预览",
    onDismiss: () -> Unit,
) {
    val context = LocalContext.current
    val activity = remember(context) { context.findHostActivity() }
    val initialOrientation = remember(activity) {
        activity?.requestedOrientation ?: ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
    }
    var landscape by remember { mutableStateOf(false) }
    var fitToScreen by remember(html) { mutableStateOf(true) }
    var progress by remember(html) { mutableIntStateOf(0) }
    var webViewRef by remember { mutableStateOf<WebView?>(null) }

    DisposableEffect(activity) {
        onDispose {
            activity?.requestedOrientation = initialOrientation
            webViewRef?.apply {
                stopLoading()
                webChromeClient = null
                webViewClient = WebViewClient()
                destroy()
            }
            webViewRef = null
        }
    }

    Dialog(
        onDismissRequest = onDismiss,
        properties = DialogProperties(
            usePlatformDefaultWidth = false,
            decorFitsSystemWindows = false,
        ),
    ) {
        Surface(
            modifier = Modifier.fillMaxSize(),
            color = MaterialTheme.colorScheme.background,
        ) {
            Column(
                modifier = Modifier
                    .fillMaxSize()
                    .statusBarsPadding()
                    .navigationBarsPadding(),
            ) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(start = 16.dp, end = 6.dp, top = 4.dp, bottom = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        text = title,
                        modifier = Modifier.weight(1f),
                        style = MaterialTheme.typography.titleMedium,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    TextButton(
                        onClick = {
                            val next = !fitToScreen
                            fitToScreen = next
                            webViewRef?.let { applyMobileFitScript(it, next) }
                        },
                        contentPadding = PaddingValues(horizontal = 8.dp, vertical = 2.dp),
                    ) {
                        Icon(
                            imageVector = Icons.Outlined.FitScreen,
                            contentDescription = null,
                            modifier = Modifier.size(16.dp),
                        )
                        Text(
                            text = if (fitToScreen) " 适应" else " 1:1",
                            style = MaterialTheme.typography.labelMedium,
                        )
                    }
                    if (activity != null) {
                        IconButton(
                            onClick = {
                                val next = !landscape
                                landscape = next
                                activity.requestedOrientation = if (next) {
                                    ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
                                } else {
                                    ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
                                }
                            },
                        ) {
                            Icon(
                                imageVector = Icons.Outlined.ScreenRotation,
                                contentDescription = if (landscape) "切回竖屏" else "横屏查看",
                                tint = if (landscape) {
                                    MaterialTheme.colorScheme.primary
                                } else {
                                    MaterialTheme.colorScheme.onSurface
                                },
                            )
                        }
                    }
                    IconButton(onClick = onDismiss) {
                        Icon(Icons.Outlined.Close, contentDescription = "关闭")
                    }
                }
                if (progress in 1..99) {
                    LinearProgressIndicator(
                        progress = { progress / 100f },
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
                val preparedHtml = remember(html) {
                    prepareHtmlForMobilePreview(html, fitToScreen = true)
                }
                AndroidView(
                    modifier = Modifier
                        .fillMaxWidth()
                        .weight(1f),
                    factory = { viewContext ->
                        WebView(viewContext).apply {
                            setBackgroundColor(0xFFFFFFFF.toInt())
                            settings.javaScriptEnabled = true
                            settings.domStorageEnabled = true
                            settings.allowFileAccess = false
                            settings.allowContentAccess = false
                            settings.useWideViewPort = true
                            settings.loadWithOverviewMode = true
                            settings.setSupportZoom(true)
                            settings.builtInZoomControls = true
                            settings.displayZoomControls = false
                            webChromeClient = object : WebChromeClient() {
                                override fun onProgressChanged(view: WebView?, newProgress: Int) {
                                    progress = newProgress
                                }
                            }
                            webViewClient = object : WebViewClient() {
                                override fun onPageFinished(view: WebView?, url: String?) {
                                    super.onPageFinished(view, url)
                                    view?.let { applyMobileFitScript(it, fitToScreen) }
                                }
                            }
                            webViewRef = this
                            loadDataWithBaseURL(
                                "https://agent-inbox.local/",
                                preparedHtml,
                                "text/html",
                                "UTF-8",
                                null,
                            )
                        }
                    },
                )
            }
        }
    }
}
