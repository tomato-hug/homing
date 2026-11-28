package dev.agentpush.poc.ui

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import android.webkit.WebView
import dev.agentpush.poc.AgentApi
import java.net.URLDecoder

private val VIEWPORT_META_REGEX = Regex(
    """<meta\b[^>]*name\s*=\s*["']?viewport["']?[^>]*>""",
    RegexOption.IGNORE_CASE,
)
private val HEAD_OPEN_REGEX = Regex("""<head\b[^>]*>""", RegexOption.IGNORE_CASE)
private val BODY_OPEN_REGEX = Regex("""<body\b[^>]*>""", RegexOption.IGNORE_CASE)

private const val DEFAULT_MOBILE_VIEWPORT =
    """<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=5.0, user-scalable=yes">"""

/**
 * Prepares LLM-generated HTML or bare SVG snippets for crisp mobile preview:
 * wraps bare SVG fragments in a responsive HTML shell and ensures a viewport meta
 * tag is present so WebView scaling and zoom work reliably.
 */
internal fun prepareHtmlForMobilePreview(
    rawHtml: String,
    @Suppress("UNUSED_PARAMETER") fitToScreen: Boolean = true,
): String {
    val trimmed = rawHtml.trimStart()
    val lower = trimmed.lowercase()
    val isBareSvg = lower.startsWith("<svg") ||
        (lower.startsWith("<?xml") && "<svg" in lower && "<html" !in lower)
    if (isBareSvg) {
        return buildString {
            append("<!doctype html><html><head><meta charset=\"utf-8\">")
            append(DEFAULT_MOBILE_VIEWPORT)
            append("<style>html,body{margin:0;padding:12px;min-height:100%;display:flex;align-items:center;justify-content:center;background:#f8fafc;box-sizing:border-box}svg{max-width:100%;height:auto}</style>")
            append("</head><body>")
            append(rawHtml)
            append("</body></html>")
        }
    }
    if (VIEWPORT_META_REGEX.containsMatchIn(rawHtml)) {
        return rawHtml
    }
    HEAD_OPEN_REGEX.find(rawHtml)?.let { match ->
        return buildString(rawHtml.length + DEFAULT_MOBILE_VIEWPORT.length + 1) {
            append(rawHtml, 0, match.range.last + 1)
            append('\n')
            append(DEFAULT_MOBILE_VIEWPORT)
            append(rawHtml, match.range.last + 1, rawHtml.length)
        }
    }
    BODY_OPEN_REGEX.find(rawHtml)?.let { match ->
        return buildString(rawHtml.length + DEFAULT_MOBILE_VIEWPORT.length + 16) {
            append(rawHtml, 0, match.range.first)
            append("<head><meta charset=\"utf-8\">")
            append(DEFAULT_MOBILE_VIEWPORT)
            append("</head>")
            append(rawHtml, match.range.first, rawHtml.length)
        }
    }
    return "<head><meta charset=\"utf-8\">$DEFAULT_MOBILE_VIEWPORT</head>\n$rawHtml"
}

/**
 * JavaScript executed after HTML/SVG page load to prevent off-screen clipping from
 * desktop flex centering (`height: 100vh`) or fixed wide canvases, while scaling to
 * fit mobile viewports when [fitToScreen] is enabled.
 */
internal fun mobileFitJavascript(fitToScreen: Boolean): String {
    val fitFlag = if (fitToScreen) "true" else "false"
    return """
        (function() {
          try {
            var fit = $fitFlag;
            var styleId = '__homing_fit_style';
            var existing = document.getElementById(styleId);
            if (!fit) {
              if (existing) existing.remove();
              var metaOff = document.querySelector('meta[name="viewport"]');
              if (!metaOff) {
                metaOff = document.createElement('meta');
                metaOff.name = 'viewport';
                (document.head || document.documentElement).appendChild(metaOff);
              }
              metaOff.setAttribute('content', 'width=device-width, initial-scale=1.0, minimum-scale=0.25, maximum-scale=5.0, user-scalable=yes');
              return;
            }
            var minLeft = 0;
            var minTop = 0;
            var maxRight = document.documentElement ? document.documentElement.scrollWidth : window.innerWidth;
            var nodes = document.querySelectorAll('body > *, svg, canvas');
            var count = Math.min(nodes.length, 48);
            for (var i = 0; i < count; i++) {
              var rect = nodes[i].getBoundingClientRect();
              if (!rect || (rect.width === 0 && rect.height === 0)) continue;
              if (rect.left < minLeft) minLeft = rect.left;
              if (rect.top < minTop) minTop = rect.top;
              if (rect.right > maxRight) maxRight = rect.right;
            }
            if ((minLeft < -2 || minTop < -2) && !existing) {
              var style = document.createElement('style');
              style.id = styleId;
              style.textContent = 'html,body{height:auto !important;min-height:100vh !important;overflow:visible !important;} body{justify-content:flex-start !important;align-items:flex-start !important;}';
              (document.head || document.documentElement).appendChild(style);
              minLeft = 0;
              maxRight = Math.max(maxRight, document.documentElement ? document.documentElement.scrollWidth : window.innerWidth);
            }
            var screenW = Math.max(window.innerWidth || document.documentElement.clientWidth || 360, 320);
            var spanW = Math.ceil(maxRight - Math.min(0, minLeft));
            var meta = document.querySelector('meta[name="viewport"]');
            if (!meta) {
              meta = document.createElement('meta');
              meta.name = 'viewport';
              (document.head || document.documentElement).appendChild(meta);
            }
            if (spanW > screenW + 8) {
              var targetW = Math.min(Math.max(spanW + 24, screenW), 2400);
              var scale = Math.max(0.15, Math.min(1.0, screenW / targetW));
              meta.setAttribute('content', 'width=' + targetW + ', initial-scale=' + scale.toFixed(4) + ', minimum-scale=0.1, maximum-scale=5.0, user-scalable=yes');
            } else {
              meta.setAttribute('content', 'width=device-width, initial-scale=1.0, minimum-scale=0.25, maximum-scale=5.0, user-scalable=yes');
            }
          } catch (_) {}
        })();
    """.trimIndent()
}

internal fun applyMobileFitScript(webView: WebView, fitToScreen: Boolean) {
    webView.evaluateJavascript(mobileFitJavascript(fitToScreen), null)
}

/**
 * Infers `(mimeType, charset)` from extension and magic bytes for both local
 * cloud-tunnel `/f/` attachments and `/api/fs/raw` responses.
 */
internal fun guessWebMime(urlOrPath: String, bytes: ByteArray): Pair<String, String?> {
    val cleanBase = urlOrPath.substringBefore('#').substringBefore('?')
    val queryPath = extractQueryParam(urlOrPath, "path").orEmpty()
    val candidateName = (if (cleanBase.endsWith("/api/fs/raw") && queryPath.isNotBlank()) {
        queryPath
    } else {
        cleanBase
    }).lowercase()
    when {
        candidateName.endsWith(".html") || candidateName.endsWith(".htm") -> return "text/html" to "UTF-8"
        candidateName.endsWith(".svg") -> return "image/svg+xml" to "UTF-8"
        candidateName.endsWith(".css") -> return "text/css" to "UTF-8"
        candidateName.endsWith(".js") || candidateName.endsWith(".mjs") -> return "application/javascript" to "UTF-8"
        candidateName.endsWith(".json") -> return "application/json" to "UTF-8"
        candidateName.endsWith(".png") -> return "image/png" to null
        candidateName.endsWith(".jpg") || candidateName.endsWith(".jpeg") -> return "image/jpeg" to null
        candidateName.endsWith(".gif") -> return "image/gif" to null
        candidateName.endsWith(".webp") -> return "image/webp" to null
        candidateName.endsWith(".bmp") -> return "image/bmp" to null
        candidateName.endsWith(".ico") -> return "image/x-icon" to null
        candidateName.endsWith(".avif") -> return "image/avif" to null
        candidateName.endsWith(".pdf") -> return "application/pdf" to null
        candidateName.endsWith(".txt") || candidateName.endsWith(".md") -> return "text/plain" to "UTF-8"
    }
    if (bytes.size >= 4) {
        if (bytes[0] == 0x89.toByte() && bytes[1] == 0x50.toByte()) return "image/png" to null
        if (bytes[0] == 0xFF.toByte() && bytes[1] == 0xD8.toByte()) return "image/jpeg" to null
        if (bytes[0] == 0x47.toByte() && bytes[1] == 0x49.toByte()) return "image/gif" to null
        if (bytes[0] == 0x52.toByte() && bytes[1] == 0x49.toByte()) return "image/webp" to null
        if (bytes[0] == '%'.code.toByte() && bytes[1] == 'P'.code.toByte() && bytes[2] == 'D'.code.toByte()) {
            return "application/pdf" to null
        }
        val head = String(bytes, 0, minOf(bytes.size, 512), Charsets.UTF_8).trimStart().lowercase()
        if (head.startsWith("<!doctype html") || head.startsWith("<html") || "<body" in head) {
            return "text/html" to "UTF-8"
        }
        if (head.startsWith("<svg") || (head.startsWith("<?xml") && "<svg" in head)) {
            return "image/svg+xml" to "UTF-8"
        }
    }
    return "application/octet-stream" to null
}

private fun extractQueryParam(url: String, key: String): String? {
    val query = url.substringAfter('?', "").substringBefore('#')
    if (query.isBlank()) return null
    for (pair in query.split('&')) {
        val eq = pair.indexOf('=')
        if (eq <= 0) continue
        if (pair.substring(0, eq) == key) {
            val raw = pair.substring(eq + 1)
            return runCatching { URLDecoder.decode(raw, Charsets.UTF_8.name()) }.getOrDefault(raw)
        }
    }
    return null
}

internal fun isCloudAttachmentBrowserUrl(url: String): Boolean {
    return url.startsWith(AgentApi.CLOUD_ATTACHMENT_ORIGIN) ||
        AgentApi.shouldRouteViaCloudTransport(url)
}

internal fun friendlyBrowserAddress(url: String): String {
    if (!url.startsWith(AgentApi.CLOUD_ATTACHMENT_ORIGIN)) return url
    val queryPath = extractQueryParam(url, "path").orEmpty()
    val rawName = if (queryPath.isNotBlank()) {
        queryPath.substringAfterLast('/')
    } else {
        url.substringBefore('#').substringBefore('?').substringAfterLast('/')
    }
    val decoded = runCatching { URLDecoder.decode(rawName.replace("+", "%2B"), Charsets.UTF_8.name()) }
        .getOrDefault(rawName)
        .ifBlank { "预览文件" }
    return "电脑文件 · $decoded"
}

internal fun Context.findHostActivity(): Activity? {
    var current: Context? = this
    while (current is ContextWrapper) {
        if (current is Activity) return current
        current = current.baseContext
    }
    return null
}
