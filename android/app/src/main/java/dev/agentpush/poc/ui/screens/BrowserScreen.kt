package dev.agentpush.poc.ui.screens

import android.app.DownloadManager
import android.content.ClipData
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.net.Uri
import android.os.Environment
import android.webkit.CookieManager
import android.webkit.URLUtil
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.ArrowForward
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.OpenInNew
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import java.io.FilterInputStream
import java.net.HttpURLConnection
import java.net.URL

@Composable
fun BrowserScreen(
    url: String,
    title: String,
    serverBase: String,
    proxyCapability: String?,
    authToken: String,
    onBack: () -> Unit,
) {
    val context = LocalContext.current
    val clipboard = LocalClipboardManager.current
    var address by remember(url) { mutableStateOf(url) }
    var pageTitle by remember(url) { mutableStateOf(title.ifBlank { "网页" }) }
    var progress by remember { mutableIntStateOf(0) }
    var canGoBack by remember { mutableStateOf(false) }
    var canGoForward by remember { mutableStateOf(false) }
    var menuOpen by remember { mutableStateOf(false) }
    var webViewRef by remember { mutableStateOf<WebView?>(null) }
    var localhostUrl by remember { mutableStateOf<String?>(null) }

    fun loadAddress(input: String) {
        var value = input.trim()
        if (value.isBlank()) return
        if (!value.startsWith("http://") && !value.startsWith("https://")) {
            value = "https://$value"
        }
        address = value
        webViewRef?.loadUrl(value)
    }

    Scaffold(
        containerColor = MaterialTheme.colorScheme.background,
        topBar = {
            Surface(color = MaterialTheme.colorScheme.surface, tonalElevation = 2.dp) {
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .statusBarsPadding(),
                ) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(start = 4.dp, end = 4.dp, top = 3.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        IconButton(onClick = onBack) {
                            Icon(Icons.AutoMirrored.Outlined.ArrowBack, "关闭")
                        }
                        Text(
                            text = pageTitle,
                            modifier = Modifier.weight(1f),
                            style = MaterialTheme.typography.titleMedium,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        IconButton(
                            onClick = {
                                webViewRef?.reload()
                            },
                        ) {
                            Icon(Icons.Outlined.Refresh, "刷新")
                        }
                        Box {
                            IconButton(onClick = { menuOpen = true }) {
                                Icon(Icons.Outlined.MoreVert, "更多")
                            }
                            DropdownMenu(
                                expanded = menuOpen,
                                onDismissRequest = { menuOpen = false },
                            ) {
                                DropdownMenuItem(
                                    text = { Text("分享链接") },
                                    leadingIcon = {
                                        Icon(Icons.Outlined.Share, contentDescription = null)
                                    },
                                    onClick = {
                                        menuOpen = false
                                        val current = webViewRef?.url ?: address
                                        context.startActivity(
                                            Intent.createChooser(
                                                Intent(Intent.ACTION_SEND).apply {
                                                    type = "text/plain"
                                                    putExtra(Intent.EXTRA_TEXT, current)
                                                },
                                                "分享链接",
                                            ),
                                        )
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text("复制链接") },
                                    leadingIcon = {
                                        Icon(Icons.Outlined.ContentCopy, contentDescription = null)
                                    },
                                    onClick = {
                                        menuOpen = false
                                        clipboard.setText(
                                            AnnotatedString(webViewRef?.url ?: address),
                                        )
                                        Toast.makeText(context, "链接已复制", Toast.LENGTH_SHORT).show()
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text("系统浏览器打开") },
                                    leadingIcon = {
                                        Icon(Icons.Outlined.OpenInNew, contentDescription = null)
                                    },
                                    onClick = {
                                        menuOpen = false
                                        val current = webViewRef?.url ?: address
                                        runCatching {
                                            context.startActivity(
                                                Intent(Intent.ACTION_VIEW, Uri.parse(current)),
                                            )
                                        }
                                    },
                                )
                            }
                        }
                    }
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 8.dp, vertical = 3.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(2.dp),
                    ) {
                        IconButton(
                            onClick = { webViewRef?.goBack() },
                            enabled = canGoBack,
                        ) {
                            Icon(Icons.AutoMirrored.Outlined.ArrowBack, "后退")
                        }
                        IconButton(
                            onClick = { webViewRef?.goForward() },
                            enabled = canGoForward,
                        ) {
                            Icon(Icons.AutoMirrored.Outlined.ArrowForward, "前进")
                        }
                        OutlinedTextField(
                            value = address,
                            onValueChange = { address = it },
                            modifier = Modifier.weight(1f),
                            singleLine = true,
                            textStyle = MaterialTheme.typography.bodySmall,
                            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Go),
                            keyboardActions = KeyboardActions(onGo = { loadAddress(address) }),
                        )
                        IconButton(onClick = { loadAddress(address) }) {
                            Icon(Icons.Outlined.OpenInNew, "打开")
                        }
                    }
                    if (progress in 1..99) {
                        LinearProgressIndicator(
                            progress = { progress / 100f },
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                }
            }
        },
    ) { padding ->
        AndroidView(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding),
            factory = { viewContext ->
                WebView(viewContext).apply {
                    setBackgroundColor(0xFF111418.toInt())
                    settings.javaScriptEnabled = true
                    settings.domStorageEnabled = true
                    settings.allowFileAccess = false
                    settings.allowContentAccess = false
                    settings.loadsImagesAutomatically = true
                    settings.builtInZoomControls = true
                    settings.displayZoomControls = false
                    settings.useWideViewPort = true
                    settings.loadWithOverviewMode = true
                    CookieManager.getInstance().setAcceptCookie(true)
                    webChromeClient = object : WebChromeClient() {
                        override fun onProgressChanged(view: WebView?, newProgress: Int) {
                            progress = newProgress
                        }

                        override fun onReceivedTitle(view: WebView?, title: String?) {
                            if (!title.isNullOrBlank()) pageTitle = title
                        }
                    }
                    webViewClient = object : WebViewClient() {
                        override fun onPageStarted(view: WebView?, url: String?, favicon: Bitmap?) {
                            super.onPageStarted(view, url, favicon)
                            if (!url.isNullOrBlank()) address = url
                            canGoBack = view?.canGoBack() == true
                            canGoForward = view?.canGoForward() == true
                        }

                        override fun onPageFinished(view: WebView?, url: String?) {
                            super.onPageFinished(view, url)
                            if (!url.isNullOrBlank()) address = url
                            canGoBack = view?.canGoBack() == true
                            canGoForward = view?.canGoForward() == true
                        }

                        override fun shouldOverrideUrlLoading(
                            view: WebView?,
                            request: WebResourceRequest?,
                        ): Boolean {
                            val target = request?.url?.toString() ?: return false
                            val host = request.url.host?.lowercase().orEmpty()
                            if (host == "localhost" || host == "127.0.0.1" || host == "::1") {
                                localhostUrl = target
                                return true
                            }
                            if (target.startsWith("http://") || target.startsWith("https://")) {
                                return false
                            }
                            runCatching {
                                viewContext.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(target)))
                            }
                            return true
                        }

                        override fun shouldInterceptRequest(
                            view: WebView?,
                            request: WebResourceRequest?,
                        ): WebResourceResponse? {
                            val target = request?.url?.toString().orEmpty()
                            if (!target.startsWith("${serverBase.trimEnd('/')}/p/")) {
                                return super.shouldInterceptRequest(view, request)
                            }
                            return authenticatedProxyResponse(target, authToken)
                        }
                    }
                    setDownloadListener { downloadUrl, userAgent, contentDisposition, mime, _ ->
                        runCatching {
                            val fileName = URLUtil.guessFileName(
                                downloadUrl,
                                contentDisposition,
                                mime,
                            )
                            val request = DownloadManager.Request(Uri.parse(downloadUrl)).apply {
                                setMimeType(mime)
                                addRequestHeader("User-Agent", userAgent)
                                CookieManager.getInstance().getCookie(downloadUrl)?.let {
                                    addRequestHeader("Cookie", it)
                                }
                                setNotificationVisibility(
                                    DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED,
                                )
                                setDestinationInExternalPublicDir(
                                    Environment.DIRECTORY_DOWNLOADS,
                                    fileName,
                                )
                            }
                            viewContext.getSystemService(DownloadManager::class.java)
                                .enqueue(request)
                            Toast.makeText(
                                viewContext,
                                "已开始下载 $fileName",
                                Toast.LENGTH_SHORT,
                            ).show()
                        }.onFailure {
                            Toast.makeText(
                                viewContext,
                                "下载失败：${it.message}",
                                Toast.LENGTH_SHORT,
                            ).show()
                        }
                    }
                    webViewRef = this
                    loadUrl(url)
                }
            },
            update = { webViewRef = it },
        )
    }

    localhostUrl?.let { target ->
        val uri = Uri.parse(target)
        val port = uri.port
        AlertDialog(
            onDismissRequest = { localhostUrl = null },
            title = { Text("网页链接指向 Mac 本机") },
            text = {
                Text(
                    "localhost / 127.0.0.1 在手机上指向手机自己，无法直接打开。" +
                        if (port > 0) {
                            "\n\n若中继已开启端口代理，可尝试通过中继访问 :$port。"
                        } else {
                            ""
                        },
                )
            },
            confirmButton = {
                if (port > 0 && serverBase.isNotBlank()) {
                    TextButton(
                        onClick = {
                            val path = uri.encodedPath?.trimStart('/').orEmpty()
                            val query = uri.encodedQuery?.let { "?$it" }.orEmpty()
                            val proxy = buildString {
                                append(serverBase.trimEnd('/'))
                                append("/p/$port/")
                                append(path)
                                append(query)
                                if (!proxyCapability.isNullOrBlank()) {
                                    append(if (query.isBlank()) "?" else "&")
                                    append("cap=")
                                    append(proxyCapability)
                                }
                            }
                            localhostUrl = null
                            webViewRef?.loadUrl(proxy)
                        },
                    ) {
                        Text("通过中继打开")
                    }
                }
            },
            dismissButton = {
                TextButton(onClick = { localhostUrl = null }) { Text("取消") }
            },
        )
    }
}

private fun authenticatedProxyResponse(
    target: String,
    token: String,
): WebResourceResponse? {
    if (token.isBlank()) return null
    return runCatching {
        val connection = URL(target).openConnection() as HttpURLConnection
        connection.connectTimeout = 10_000
        connection.readTimeout = 60_000
        connection.instanceFollowRedirects = true
        connection.setRequestProperty("Authorization", "Bearer $token")
        connection.setRequestProperty("Accept", "*/*")
        val code = connection.responseCode
        val mime = connection.contentType?.substringBefore(';') ?: "application/octet-stream"
        val charset = connection.contentType
            ?.split(';')
            ?.firstOrNull { it.trim().startsWith("charset=", ignoreCase = true) }
            ?.substringAfter('=')
            ?.trim()
        val headers = buildMap {
            connection.headerFields.forEach { (key, values) ->
                if (key != null && values != null && key.lowercase() !in setOf(
                        "connection",
                        "transfer-encoding",
                        "content-length",
                    )
                ) {
                    put(key, values.joinToString(","))
                }
            }
        }
        val rawStream = if (code in 200..299) connection.inputStream else connection.errorStream
            ?: connection.inputStream
        val stream = object : FilterInputStream(rawStream) {
            override fun close() {
                runCatching { super.close() }
                connection.disconnect()
            }
        }
        WebResourceResponse(
            mime,
            charset,
            code,
            connection.responseMessage ?: "HTTP $code",
            headers,
            stream,
        )
    }.getOrNull()
}
