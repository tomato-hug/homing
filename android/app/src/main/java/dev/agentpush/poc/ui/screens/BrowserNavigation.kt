package dev.agentpush.poc.ui.screens

import android.webkit.WebResourceResponse
import dev.agentpush.poc.AgentApi
import dev.agentpush.poc.ui.guessWebMime
import dev.agentpush.poc.ui.prepareHtmlForMobilePreview
import kotlinx.coroutines.runBlocking
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import java.io.ByteArrayInputStream
import java.io.FilterInputStream
import java.net.HttpURLConnection

/** Only web URLs cross into WebView, including initial links and the address field. */
internal fun browserHttpUrl(input: String, allowBareHost: Boolean = false): HttpUrl? {
    val value = input.trim()
    if (value.isEmpty() || value.any { it.isWhitespace() || it.code < 32 || it == '\\' }) return null
    val hasScheme = Regex("^[A-Za-z][A-Za-z0-9+.-]*:").containsMatchIn(value)
    val hostWithPort = Regex("^[^/:?#]+:[0-9]+(?:[/#?].*)?$").matches(value)
    val candidate = if (allowBareHost && (!hasScheme || hostWithPort)) "https://$value" else value
    val parsed = candidate.toHttpUrlOrNull() ?: return null
    return parsed.takeIf { it.username.isEmpty() && it.password.isEmpty() }
}

internal fun isBrowserLocalUrl(url: HttpUrl): Boolean {
    val host = url.host.trimEnd('.')
    return host == "localhost" || host.endsWith(".localhost") || host == "::1" ||
        host == "0.0.0.0" || Regex("127(?:\\.[0-9]{1,3}){3}").matches(host)
}

/** Match the parsed origin and the port-proxy route, never a textual URL prefix. */
internal fun isAuthenticatedProxyUrl(target: String, serverBase: String): Boolean {
    val url = browserHttpUrl(target) ?: return false
    val base = browserHttpUrl(serverBase) ?: return false
    if (url.scheme != base.scheme || url.host != base.host || url.port != base.port) return false
    val prefix = base.encodedPath.trimEnd('/') + "/p/"
    if (!url.encodedPath.startsWith(prefix)) return false
    val port = url.encodedPath.removePrefix(prefix).substringBefore('/').toIntOrNull() ?: return false
    return port in 1..65535 && url.pathSegments.none { it == "." || it == ".." || '/' in it || '\\' in it }
}

internal fun browserProxyUrl(target: String, serverBase: String, capability: String?): String? {
    val local = browserHttpUrl(target)?.takeIf(::isBrowserLocalUrl) ?: return null
    // The relay forwards only HTTP, even if the original URL used another scheme.
    if (local.scheme != "http") return null
    val base = browserHttpUrl(serverBase) ?: return null
    return base.newBuilder()
        .encodedPath(base.encodedPath.trimEnd('/') + "/p/${local.port}" + local.encodedPath)
        .encodedQuery(local.encodedQuery)
        .encodedFragment(local.encodedFragment)
        .apply { if (!capability.isNullOrBlank()) addQueryParameter("cap", capability) }
        .build().toString()
}

private fun proxyFailure(code: Int, message: String) = WebResourceResponse(
    "text/plain", "UTF-8", code, "Proxy unavailable", mapOf("Cache-Control" to "no-store"),
    ByteArrayInputStream(message.toByteArray(Charsets.UTF_8)),
)

internal fun cloudAttachmentResponse(
    target: String,
    isMainFrame: Boolean,
    fitToScreen: Boolean = true,
): WebResourceResponse? {
    if (!AgentApi.shouldRouteViaCloudTransport(target) && !target.startsWith(AgentApi.CLOUD_ATTACHMENT_ORIGIN)) {
        return null
    }
    return runCatching {
        val bytes = runBlocking { AgentApi.downloadUrlViaCloudTransport(target) }
        val (mime, charset) = guessWebMime(target, bytes)
        if (mime == "text/html") {
            val prepared = prepareHtmlForMobilePreview(
                String(bytes, Charsets.UTF_8),
                fitToScreen = fitToScreen,
            )
            WebResourceResponse(
                "text/html",
                "UTF-8",
                200,
                "OK",
                mapOf("Cache-Control" to "no-cache"),
                ByteArrayInputStream(prepared.toByteArray(Charsets.UTF_8)),
            )
        } else {
            WebResourceResponse(
                mime,
                charset,
                200,
                "OK",
                mapOf("Cache-Control" to "private, max-age=60"),
                ByteArrayInputStream(bytes),
            )
        }
    }.getOrElse { err ->
        if (!isMainFrame) {
            return proxyFailure(502, err.message ?: "无法通过云端中继读取文件")
        }
        val escaped = (err.message ?: "无法连接电脑通道")
            .replace("&", "&amp;")
            .replace("<", "&lt;")
            .replace(">", "&gt;")
        val html = """
            <!doctype html><html><head><meta charset="utf-8">
            <meta name="viewport" content="width=device-width,initial-scale=1">
            <style>body{font-family:sans-serif;background:#111418;color:#e6edf3;padding:28px 20px;line-height:1.6}
            .card{background:#1b2028;border:1px solid #30363d;border-radius:12px;padding:18px}
            h3{margin:0 0 8px;font-size:17px}p{margin:0;color:#9da7b3;font-size:14px}</style>
            </head><body><div class="card"><h3>电脑文件暂时无法读取</h3>
            <p>$escaped</p><p style="margin-top:8px">请确认电脑端 Homing 处于在线状态后，点击右上角刷新重试。</p>
            </div></body></html>
        """.trimIndent()
        WebResourceResponse(
            "text/html",
            "UTF-8",
            200,
            "OK",
            mapOf("Cache-Control" to "no-store"),
            ByteArrayInputStream(html.toByteArray(Charsets.UTF_8)),
        )
    }
}

internal fun authenticatedProxyResponse(
    target: String,
    serverBase: String,
    token: String,
    method: String,
): WebResourceResponse? {
    if (!isAuthenticatedProxyUrl(target, serverBase) || token.isBlank()) return null
    // WebResourceRequest does not expose request bodies. Replaying POST as GET loses data.
    if (method != "GET" && method != "HEAD") {
        return proxyFailure(405, "此端口代理暂不支持提交表单，请在电脑上操作。")
    }
    val connection = browserHttpUrl(target)!!.toUrl().openConnection() as HttpURLConnection
    return try {
        connection.connectTimeout = 10_000
        connection.readTimeout = 30_000
        connection.requestMethod = method
        connection.instanceFollowRedirects = false
        connection.setRequestProperty("Authorization", "Bearer $token")
        connection.setRequestProperty("Accept", "*/*")
        val code = connection.responseCode
        // WebResourceResponse cannot represent redirects. Never follow one with a relay token,
        // or load the final content under the original origin. Fail closed with a visible error.
        if (code in 300..399) {
            connection.disconnect()
            proxyFailure(502, "此端口代理暂不支持网页跳转，请在电脑上打开目标页面。")
        } else {
            val mime = connection.contentType?.substringBefore(';') ?: "application/octet-stream"
            val charset = connection.contentType?.split(';')
                ?.firstOrNull { it.trim().startsWith("charset=", ignoreCase = true) }
                ?.substringAfter('=')?.trim()
            val headers = connection.headerFields.entries
                .filter { (key, _) -> key != null && key.lowercase() !in setOf("connection", "transfer-encoding", "content-length") }
                .associate { (key, values) -> key!! to values.joinToString(",") }
            val rawStream = if (code in 200..299) connection.inputStream else connection.errorStream
            val stream = object : FilterInputStream(rawStream ?: ByteArrayInputStream(byteArrayOf())) {
                override fun close() {
                    try { super.close() } finally { connection.disconnect() }
                }
            }
            WebResourceResponse(mime, charset, code, connection.responseMessage?.takeIf { it.isNotBlank() } ?: "HTTP $code", headers, stream)
        }
    } catch (_: Exception) {
        connection.disconnect()
        proxyFailure(502, "无法连接电脑上的网页，请检查连接和端口代理设置后重试。")
    }
}
