package dev.agentpush.poc.ui.screens

import android.app.DownloadManager
import android.content.Intent
import android.content.pm.ActivityInfo
import android.graphics.Bitmap
import android.net.Uri
import android.os.Environment
import android.webkit.CookieManager
import android.webkit.URLUtil
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebResourceError
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Toast
import androidx.activity.compose.BackHandler
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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.ArrowForward
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material.icons.outlined.FitScreen
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.OpenInNew
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.ScreenRotation
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
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import dev.agentpush.poc.AgentApi
import dev.agentpush.poc.media.AttachmentFiles
import dev.agentpush.poc.ui.applyMobileFitScript
import dev.agentpush.poc.ui.findHostActivity
import dev.agentpush.poc.ui.friendlyBrowserAddress
import dev.agentpush.poc.ui.guessWebMime
import dev.agentpush.poc.ui.isCloudAttachmentBrowserUrl
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File

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
    val focusManager = LocalFocusManager.current
    val scope = rememberCoroutineScope()
    val activity = remember(context) { context.findHostActivity() }
    val initialOrientation = remember(activity) {
        activity?.requestedOrientation ?: ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
    }
    var landscape by remember { mutableStateOf(false) }
    var fitToScreen by remember(url) { mutableStateOf(true) }
    var activeUrl by remember(url) { mutableStateOf(if (url == "about:blank") "" else url) }
    var address by remember(url) {
        mutableStateOf(
            when {
                url == "about:blank" -> ""
                isCloudAttachmentBrowserUrl(url) -> friendlyBrowserAddress(url)
                else -> url
            },
        )
    }
    var pageTitle by remember(url) { mutableStateOf(title.ifBlank { "网页" }) }
    var progress by remember { mutableIntStateOf(0) }
    var canGoBack by remember { mutableStateOf(false) }
    var canGoForward by remember { mutableStateOf(false) }
    var menuOpen by remember { mutableStateOf(false) }
    var webViewRef by remember { mutableStateOf<WebView?>(null) }
    var localhostUrl by remember { mutableStateOf<String?>(null) }
    var pageError by remember { mutableStateOf<String?>(null) }
    var failedUrl by remember { mutableStateOf<String?>(null) }

    fun syncDisplayedUrl(rawUrl: String) {
        activeUrl = rawUrl
        address = if (isCloudAttachmentBrowserUrl(rawUrl)) {
            friendlyBrowserAddress(rawUrl)
        } else {
            rawUrl
        }
    }

    fun rejectLink() {
        pageError = "无法打开此链接"
        failedUrl = null
        progress = 100
    }

    fun loadAddress(input: String, allowBareHost: Boolean = false) {
        val rawInput = if (input.startsWith("电脑文件 · ") && activeUrl.isNotBlank()) activeUrl else input
        val target = browserHttpUrl(rawInput, allowBareHost)
        if (target == null) {
            rejectLink()
            return
        }
        if (isBrowserLocalUrl(target) && !isAuthenticatedProxyUrl(target.toString(), serverBase)) {
            localhostUrl = target.toString()
            return
        }
        pageError = null
        failedUrl = null
        syncDisplayedUrl(target.toString())
        progress = 1
        focusManager.clearFocus()
        webViewRef?.loadUrl(target.toString())
    }

    fun reloadPage() {
        val target = failedUrl ?: webViewRef?.url ?: activeUrl.ifBlank { url }
        loadAddress(target)
    }

    fun shareOrOpenCloudFile(targetUrl: String, share: Boolean) {
        scope.launch {
            runCatching {
                val bytes = withContext(Dispatchers.IO) { AgentApi.downloadUrlViaCloudTransport(targetUrl) }
                val (mime, _) = guessWebMime(targetUrl, bytes)
                val name = friendlyBrowserAddress(targetUrl).removePrefix("电脑文件 · ")
                    .replace(Regex("[^\\p{L}\\p{N}._-]+"), "_")
                    .ifBlank { "preview.html" }
                val file = withContext(Dispatchers.IO) {
                    val dir = File(context.cacheDir, "attachments").apply { mkdirs() }
                    File(dir, "browser-$name").apply { writeBytes(bytes) }
                }
                if (share) {
                    AttachmentFiles.share(context, file, mime)
                } else {
                    AttachmentFiles.open(context, file, mime)
                }
            }.onFailure { err ->
                Toast.makeText(context, "打开文件失败：${err.message}", Toast.LENGTH_SHORT).show()
            }
        }
    }

    BackHandler {
        val webView = webViewRef
        if (webView?.canGoBack() == true) {
            pageError = null
            webView.goBack()
        } else {
            onBack()
        }
    }

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
                            Icon(Icons.AutoMirrored.Outlined.ArrowBack, "返回会话")
                        }
                        Text(
                            text = pageTitle,
                            modifier = Modifier.weight(1f),
                            style = MaterialTheme.typography.titleMedium,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        if (isCloudAttachmentBrowserUrl(activeUrl.ifBlank { url })) {
                            TextButton(
                                onClick = {
                                    val next = !fitToScreen
                                    fitToScreen = next
                                    webViewRef?.let { applyMobileFitScript(it, next) }
                                },
                                contentPadding = PaddingValues(horizontal = 6.dp, vertical = 2.dp),
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
                        IconButton(
                            onClick = {
                                reloadPage()
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
                                val currentTarget = webViewRef?.url?.takeIf { it.isNotBlank() }
                                    ?: activeUrl.ifBlank { address }
                                val isCloudDoc = isCloudAttachmentBrowserUrl(currentTarget)
                                DropdownMenuItem(
                                    text = { Text(if (isCloudDoc) "导出并分享文件" else "分享链接") },
                                    leadingIcon = {
                                        Icon(Icons.Outlined.Share, contentDescription = null)
                                    },
                                    onClick = {
                                        menuOpen = false
                                        if (isCloudDoc) {
                                            shareOrOpenCloudFile(currentTarget, share = true)
                                            return@DropdownMenuItem
                                        }
                                        runCatching { context.startActivity(
                                            Intent.createChooser(
                                                Intent(Intent.ACTION_SEND).apply {
                                                    type = "text/plain"
                                                    putExtra(Intent.EXTRA_TEXT, currentTarget)
                                                },
                                                "分享链接",
                                            ),
                                        ) }.onFailure {
                                            Toast.makeText(context, "暂时无法分享链接", Toast.LENGTH_SHORT).show()
                                        }
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
                                            AnnotatedString(currentTarget),
                                        )
                                        Toast.makeText(context, "链接已复制", Toast.LENGTH_SHORT).show()
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text(if (isCloudDoc) "用其他应用打开" else "系统浏览器打开") },
                                    leadingIcon = {
                                        Icon(
                                            if (isCloudDoc) Icons.Outlined.Download else Icons.Outlined.OpenInNew,
                                            contentDescription = null,
                                        )
                                    },
                                    onClick = {
                                        menuOpen = false
                                        if (isCloudDoc) {
                                            shareOrOpenCloudFile(currentTarget, share = false)
                                            return@DropdownMenuItem
                                        }
                                        val current = browserHttpUrl(currentTarget)?.toString()
                                            ?: return@DropdownMenuItem
                                        runCatching {
                                            context.startActivity(
                                                Intent(Intent.ACTION_VIEW, Uri.parse(current)),
                                            )
                                        }.onFailure { Toast.makeText(context, "没有可用的系统浏览器", Toast.LENGTH_SHORT).show() }
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
                            onClick = { pageError = null; webViewRef?.goBack() },
                            enabled = canGoBack,
                        ) {
                            Icon(Icons.AutoMirrored.Outlined.ArrowBack, "后退")
                        }
                        IconButton(
                            onClick = { pageError = null; webViewRef?.goForward() },
                            enabled = canGoForward,
                        ) {
                            Icon(Icons.AutoMirrored.Outlined.ArrowForward, "前进")
                        }
                        OutlinedTextField(
                            value = address,
                            onValueChange = { address = it },
                            modifier = Modifier.weight(1f).testTag("browser-address"),
                            placeholder = {
                                Text("输入网址，回车打开…", style = MaterialTheme.typography.bodySmall)
                            },
                            singleLine = true,
                            textStyle = MaterialTheme.typography.bodySmall,
                            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Go),
                            keyboardActions = KeyboardActions(onGo = { loadAddress(address, allowBareHost = true) }),
                        )
                        IconButton(onClick = { loadAddress(address, allowBareHost = true) }) {
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
        Box(Modifier.fillMaxSize().padding(padding).navigationBarsPadding()) {
            AndroidView(
                modifier = Modifier.fillMaxSize(),
                factory = { viewContext ->
                    WebView(viewContext).apply {
                        setBackgroundColor(0xFF111418.toInt())
                        settings.javaScriptEnabled = true
                        settings.domStorageEnabled = true
                        settings.allowFileAccess = false
                        settings.allowContentAccess = false
                        settings.javaScriptCanOpenWindowsAutomatically = false
                        // User-initiated target=_blank links stay in this WebView; no popup windows.
                        settings.setSupportMultipleWindows(false)
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
                                if (!url.isNullOrBlank()) syncDisplayedUrl(url)
                                pageError = null
                                failedUrl = null
                                progress = 1
                                canGoBack = view?.canGoBack() == true
                                canGoForward = view?.canGoForward() == true
                            }

                            override fun onPageFinished(view: WebView?, url: String?) {
                                super.onPageFinished(view, url)
                                if (!url.isNullOrBlank()) {
                                    syncDisplayedUrl(url)
                                    if (isCloudAttachmentBrowserUrl(url) && view != null) {
                                        applyMobileFitScript(view, fitToScreen)
                                    }
                                }
                                canGoBack = view?.canGoBack() == true
                                canGoForward = view?.canGoForward() == true
                                progress = 100
                            }

                            override fun doUpdateVisitedHistory(view: WebView?, url: String?, isReload: Boolean) {
                                if (!url.isNullOrBlank()) syncDisplayedUrl(url)
                                canGoBack = view?.canGoBack() == true
                                canGoForward = view?.canGoForward() == true
                            }

                            override fun onReceivedError(view: WebView?, request: WebResourceRequest?, error: WebResourceError?) {
                                if (request?.isForMainFrame == true) {
                                    failedUrl = request.url.toString()
                                    pageError = "网页加载失败，请检查网络连接后重试。"
                                    progress = 100
                                }
                            }

                            override fun onReceivedHttpError(view: WebView?, request: WebResourceRequest?, errorResponse: WebResourceResponse?) {
                                if (request?.isForMainFrame == true) {
                                    failedUrl = request.url.toString()
                                    pageError = if (isAuthenticatedProxyUrl(failedUrl.orEmpty(), serverBase)) {
                                        "电脑网页暂时无法打开（HTTP ${errorResponse?.statusCode}）。请检查直连地址、端口代理及白名单；此代理暂不支持网页跳转和表单提交。"
                                    } else {
                                        "网页暂时无法打开（HTTP ${errorResponse?.statusCode}）。"
                                    }
                                    progress = 100
                                }
                            }

                            override fun shouldOverrideUrlLoading(
                                view: WebView?,
                                request: WebResourceRequest?,
                            ): Boolean {
                                val target = request?.url?.toString() ?: return false
                                val parsed = browserHttpUrl(target)
                                if (parsed == null) {
                                    if (request.isForMainFrame) Toast.makeText(viewContext, "仅支持 HTTP 和 HTTPS 网页链接", Toast.LENGTH_SHORT).show()
                                    return true
                                }
                                if (isBrowserLocalUrl(parsed) && !isAuthenticatedProxyUrl(target, serverBase)) {
                                    if (request.isForMainFrame) localhostUrl = target
                                    return true
                                }
                                return false
                            }

                            override fun shouldInterceptRequest(
                                view: WebView?,
                                request: WebResourceRequest?,
                            ): WebResourceResponse? {
                                val target = request?.url?.toString().orEmpty()
                                cloudAttachmentResponse(
                                    target = target,
                                    isMainFrame = request?.isForMainFrame == true,
                                    fitToScreen = fitToScreen,
                                )?.let { return it }
                                return authenticatedProxyResponse(target, serverBase, authToken, request?.method.orEmpty())
                            }
                        }
                        setDownloadListener { downloadUrl, userAgent, contentDisposition, mime, _ ->
                            if (isCloudAttachmentBrowserUrl(downloadUrl)) {
                                shareOrOpenCloudFile(downloadUrl, share = false)
                                return@setDownloadListener
                            }
                            runCatching {
                                require(browserHttpUrl(downloadUrl) != null) { "仅支持 HTTP 和 HTTPS 下载链接" }
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
                        if (url.isNotBlank() && url != "about:blank") {
                            loadAddress(url)
                        } else {
                            progress = 100
                        }
                    }
                },
                update = { webViewRef = it },
            )
            pageError?.let { error ->
                Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
                    Column(
                        modifier = Modifier.fillMaxSize().padding(24.dp),
                        verticalArrangement = Arrangement.Center,
                        horizontalAlignment = Alignment.CenterHorizontally,
                    ) {
                        Text(error, style = MaterialTheme.typography.bodyLarge)
                        if (failedUrl != null) TextButton(onClick = ::reloadPage) { Text("重试") }
                        TextButton(onClick = onBack) { Text("返回会话") }
                    }
                }
            }
        }
    }

    localhostUrl?.let { target ->
        val local = browserHttpUrl(target)!!
        val port = local.port
        val proxy = browserProxyUrl(target, serverBase, proxyCapability)
        AlertDialog(
            onDismissRequest = { localhostUrl = null },
            title = { Text("网页链接指向 Mac 本机") },
            text = {
                Text(
                    "localhost / 127.0.0.1 在手机上指向手机自己，无法直接打开。" +
                        if (proxy != null) {
                            "\n\n仅当手机能直连这台电脑，且已开启 :$port 端口代理时可尝试访问。云端中转不转发电脑网页；部分网页跳转或表单暂不支持。"
                        } else {
                            "\n\n此地址暂不支持端口代理，请在电脑上打开。"
                        },
                )
            },
            confirmButton = {
                if (proxy != null) {
                    TextButton(
                        onClick = {
                            localhostUrl = null
                            loadAddress(proxy)
                        },
                    ) {
                        Text("尝试直连代理")
                    }
                }
            },
            dismissButton = {
                TextButton(onClick = { localhostUrl = null }) { Text("取消") }
            },
        )
    }
}
