package dev.agentpush.poc.ui

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.calculatePan
import androidx.compose.foundation.gestures.calculateZoom
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material.icons.outlined.InsertDriveFile
import androidx.compose.material.icons.outlined.Language
import androidx.compose.material.icons.outlined.PictureAsPdf
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.pointer.positionChanged
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.core.content.ContextCompat
import coil3.compose.SubcomposeAsyncImage
import coil3.compose.SubcomposeAsyncImageContent
import coil3.request.CachePolicy
import coil3.request.ImageRequest
import dev.agentpush.poc.AgentApi
import dev.agentpush.poc.AppSettings
import dev.agentpush.poc.Attachment
import dev.agentpush.poc.media.AttachmentFiles
import kotlinx.coroutines.launch
import kotlin.math.abs

@Composable
fun InlineImageGrid(
    attachments: List<Attachment>,
    settings: AppSettings,
    onOpen: (Int) -> Unit,
    modifier: Modifier = Modifier,
) {
    val images = attachments.filter { it.isImage }
    if (images.isEmpty()) return
    if (images.size == 1) {
        val single = images.first()
        SingleInlineImagePreview(
            url = AgentApi.attachmentUrl(settings, single),
            attachment = single,
            onClick = { onOpen(0) },
            modifier = modifier,
        )
        return
    }
    val columns = if (images.size == 2) 2 else 3
    Column(
        modifier = modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        images.chunked(columns).forEachIndexed { rowIndex, row ->
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                row.forEachIndexed { columnIndex, attachment ->
                    val index = rowIndex * columns + columnIndex
                    RemoteThumbnail(
                        url = AgentApi.attachmentUrl(settings, attachment),
                        description = attachment.name,
                        onClick = { onOpen(index) },
                        modifier = Modifier.weight(1f),
                    )
                }
                repeat(columns - row.size) {
                    Box(modifier = Modifier.weight(1f))
                }
            }
        }
    }
}

@Composable
private fun SingleInlineImagePreview(
    url: String,
    attachment: Attachment,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var retry by remember(url) { mutableIntStateOf(0) }
    val context = LocalContext.current
    val request = remember(url, retry) {
        ImageRequest.Builder(context)
            .data(url)
            .memoryCacheKey("${AgentApi.mediaCacheKey(url)}#$retry")
            .apply {
                if (retry > 0) {
                    memoryCachePolicy(CachePolicy.WRITE_ONLY)
                    diskCachePolicy(CachePolicy.WRITE_ONLY)
                }
            }
            .build()
    }
    Surface(
        onClick = onClick,
        modifier = modifier.fillMaxWidth(),
        shape = RoundedCornerShape(12.dp),
        color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.55f),
    ) {
        Column(modifier = Modifier.fillMaxWidth()) {
            SubcomposeAsyncImage(
                model = request,
                contentDescription = attachment.name,
                contentScale = ContentScale.Fit,
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(min = 160.dp, max = 300.dp),
                loading = {
                    Box(
                        modifier = Modifier
                            .fillMaxWidth()
                            .heightIn(min = 160.dp),
                        contentAlignment = Alignment.Center,
                    ) {
                        CircularProgressIndicator(modifier = Modifier.size(24.dp), strokeWidth = 2.dp)
                    }
                },
                error = { errState ->
                    val reason = errState.result.throwable.message?.take(90).orEmpty()
                    Box(
                        modifier = Modifier
                            .fillMaxWidth()
                            .heightIn(min = 140.dp)
                            .padding(14.dp),
                        contentAlignment = Alignment.Center,
                    ) {
                        Column(
                            horizontalAlignment = Alignment.CenterHorizontally,
                            verticalArrangement = Arrangement.spacedBy(6.dp),
                        ) {
                            Icon(
                                Icons.Outlined.Image,
                                contentDescription = null,
                                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                            Text(
                                text = if (reason.isNotBlank()) "图片加载失败：$reason" else "图片加载失败",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                maxLines = 2,
                                overflow = TextOverflow.Ellipsis,
                            )
                            Surface(
                                onClick = { retry += 1 },
                                color = MaterialTheme.colorScheme.surface,
                                shape = CircleShape,
                            ) {
                                Row(
                                    modifier = Modifier.padding(horizontal = 12.dp, vertical = 5.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(4.dp),
                                ) {
                                    Icon(Icons.Outlined.Refresh, contentDescription = null, modifier = Modifier.size(14.dp))
                                    Text("重试", style = MaterialTheme.typography.labelSmall)
                                }
                            }
                        }
                    }
                },
                success = { SubcomposeAsyncImageContent() },
            )
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 10.dp, vertical = 7.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                Icon(
                    Icons.Outlined.Image,
                    contentDescription = null,
                    modifier = Modifier.size(15.dp),
                    tint = MaterialTheme.colorScheme.primary,
                )
                Text(
                    text = attachment.name,
                    modifier = Modifier.weight(1f),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.MiddleEllipsis,
                )
                val sizeLabel = attachment.sizeLabel()
                if (sizeLabel.isNotBlank()) {
                    Text(
                        text = sizeLabel,
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

@Composable
private fun RemoteThumbnail(
    url: String,
    description: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var retry by remember(url) { mutableIntStateOf(0) }
    val context = LocalContext.current
    val request = remember(url, retry) {
        ImageRequest.Builder(context)
            .data(url)
            .memoryCacheKey("${AgentApi.mediaCacheKey(url)}#$retry")
            .apply {
                if (retry > 0) {
                    memoryCachePolicy(CachePolicy.WRITE_ONLY)
                    diskCachePolicy(CachePolicy.WRITE_ONLY)
                }
            }
            .build()
    }
    Surface(
        onClick = onClick,
        modifier = modifier.aspectRatio(1f),
        shape = RoundedCornerShape(10.dp),
        color = MaterialTheme.colorScheme.surfaceVariant,
    ) {
        SubcomposeAsyncImage(
            model = request,
            contentDescription = description,
            contentScale = ContentScale.Crop,
            modifier = Modifier.fillMaxSize(),
            loading = {
                Box(contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(modifier = Modifier.size(22.dp), strokeWidth = 2.dp)
                }
            },
            error = { errState ->
                val hint = errState.result.throwable.message?.take(28).orEmpty()
                Box(contentAlignment = Alignment.Center, modifier = Modifier.padding(6.dp)) {
                    Column(
                        horizontalAlignment = Alignment.CenterHorizontally,
                        verticalArrangement = Arrangement.spacedBy(3.dp),
                    ) {
                        Icon(Icons.Outlined.Refresh, contentDescription = "重试", modifier = Modifier.size(18.dp))
                        if (hint.isNotBlank()) {
                            Text(
                                text = hint,
                                style = MaterialTheme.typography.labelSmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                        }
                        Surface(
                            onClick = { retry += 1 },
                            color = MaterialTheme.colorScheme.surface,
                            shape = CircleShape,
                        ) {
                            Text(
                                "重试",
                                modifier = Modifier.padding(horizontal = 8.dp, vertical = 3.dp),
                                style = MaterialTheme.typography.labelSmall,
                            )
                        }
                    }
                }
            },
            success = { SubcomposeAsyncImageContent() },
        )
    }
}

@Composable
fun FullScreenUrlImageDialog(
    url: String,
    title: String = "",
    settings: AppSettings,
    onDismiss: () -> Unit,
) {
    val name = title.ifBlank {
        url.substringBefore('?').substringAfterLast('/').ifBlank { "image.png" }
    }
    val mime = when (name.substringAfterLast('.', "").lowercase()) {
        "svg" -> "image/svg+xml"
        "png" -> "image/png"
        "gif" -> "image/gif"
        "webp" -> "image/webp"
        else -> "image/jpeg"
    }
    FullScreenImageViewer(
        attachments = listOf(
            Attachment(
                id = "url-${url.hashCode()}",
                name = name,
                mime = mime,
                size = 0L,
                kind = "image",
                url = url,
            ),
        ),
        initialIndex = 0,
        settings = settings,
        onDismiss = onDismiss,
    )
}

@Composable
fun FullScreenImageViewer(
    attachments: List<Attachment>,
    initialIndex: Int,
    settings: AppSettings,
    onDismiss: () -> Unit,
) {
    val images = remember(attachments) { attachments.filter { it.isImage } }
    if (images.isEmpty()) return
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val pagerState = rememberPagerState(
        initialPage = initialIndex.coerceIn(0, images.lastIndex),
        pageCount = { images.size },
    )
    var operationsBusy by remember { mutableStateOf(false) }

    fun saveCurrent() {
        val attachment = images.getOrNull(pagerState.currentPage) ?: return
        if (Build.VERSION.SDK_INT < 29 &&
            ContextCompat.checkSelfPermission(
                context,
                Manifest.permission.WRITE_EXTERNAL_STORAGE,
            ) != PackageManager.PERMISSION_GRANTED
        ) {
            Toast.makeText(context, "请先允许存储权限后再保存", Toast.LENGTH_SHORT).show()
            return
        }
        operationsBusy = true
        scope.launch {
            runCatching {
                val file = AttachmentFiles.download(context, settings, attachment)
                AttachmentFiles.saveImage(context, file, attachment.name)
            }.onSuccess {
                Toast.makeText(context, "已保存到相册", Toast.LENGTH_SHORT).show()
            }.onFailure {
                Toast.makeText(context, "保存失败：${it.message}", Toast.LENGTH_SHORT).show()
            }
            operationsBusy = false
        }
    }

    val storagePermission = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { granted ->
        if (granted) saveCurrent()
    }

    var isCurrentPageZoomed by remember { mutableStateOf(false) }
    var chromeVisible by remember { mutableStateOf(true) }

    Dialog(
        onDismissRequest = onDismiss,
        properties = DialogProperties(
            usePlatformDefaultWidth = false,
            decorFitsSystemWindows = false,
        ),
    ) {
        Box(modifier = Modifier.fillMaxSize().background(Color.Black)) {
            HorizontalPager(
                state = pagerState,
                userScrollEnabled = !isCurrentPageZoomed,
                modifier = Modifier.fillMaxSize(),
            ) { page ->
                ZoomableRemoteImage(
                    url = AgentApi.attachmentUrl(settings, images[page]),
                    description = images[page].name,
                    onZoomChanged = { zoomed ->
                        if (pagerState.currentPage == page) {
                            isCurrentPageZoomed = zoomed
                        }
                    },
                    onToggleChrome = { chromeVisible = !chromeVisible },
                )
            }
            if (chromeVisible) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .align(Alignment.TopCenter)
                        .background(Color.Black.copy(alpha = 0.45f))
                        .statusBarsPadding()
                        .padding(horizontal = 8.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                IconButton(onClick = onDismiss) {
                    Icon(Icons.Outlined.Close, contentDescription = "关闭", tint = Color.White)
                }
                Text(
                    text = "${pagerState.currentPage + 1}/${images.size}",
                    modifier = Modifier.weight(1f),
                    color = Color.White,
                    style = MaterialTheme.typography.titleMedium,
                )
                if (operationsBusy) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(22.dp),
                        strokeWidth = 2.dp,
                        color = Color.White,
                    )
                } else {
                    IconButton(onClick = {
                        val permission = if (Build.VERSION.SDK_INT < 29) {
                            Manifest.permission.WRITE_EXTERNAL_STORAGE
                        } else {
                            null
                        }
                        if (permission != null &&
                            ContextCompat.checkSelfPermission(context, permission) !=
                            PackageManager.PERMISSION_GRANTED
                        ) {
                            storagePermission.launch(permission)
                        } else {
                            saveCurrent()
                        }
                    }) {
                        Icon(Icons.Outlined.Download, "保存到相册", tint = Color.White)
                    }
                    IconButton(onClick = {
                        val attachment = images.getOrNull(pagerState.currentPage) ?: return@IconButton
                        operationsBusy = true
                        scope.launch {
                            runCatching {
                                val file = AttachmentFiles.download(context, settings, attachment)
                                AttachmentFiles.share(context, file, attachment.mime)
                            }.onFailure {
                                Toast.makeText(
                                    context,
                                    "分享失败：${it.message}",
                                    Toast.LENGTH_SHORT,
                                ).show()
                            }
                            operationsBusy = false
                        }
                    }) {
                        Icon(Icons.Outlined.Share, "分享", tint = Color.White)
                    }
                }
                }
            }
            if (chromeVisible) {
                Text(
                    text = images.getOrNull(pagerState.currentPage)?.name.orEmpty(),
                    modifier = Modifier
                        .align(Alignment.BottomCenter)
                        .background(Color.Black.copy(alpha = 0.45f), RoundedCornerShape(999.dp))
                        .navigationBarsPadding()
                        .padding(horizontal = 24.dp, vertical = 16.dp),
                    color = Color.White.copy(alpha = 0.9f),
                    style = MaterialTheme.typography.bodySmall,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

@Composable
fun AttachmentCardList(
    attachments: List<Attachment>,
    settings: AppSettings,
    onOpenHtml: (Attachment) -> Unit,
    onOpenImage: (Int) -> Unit,
    modifier: Modifier = Modifier,
) {
    if (attachments.isEmpty()) return
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val viewer = LocalDocumentViewer.current
    val images = attachments.filter { it.isImage }
    Column(
        modifier = modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(7.dp),
    ) {
        InlineImageGrid(
            attachments = attachments,
            settings = settings,
            onOpen = onOpenImage,
        )
        attachments.filterNot { it.isImage }.forEach { attachment ->
            var busy by remember(attachment.id) { mutableStateOf(false) }
            var menu by remember(attachment.id) { mutableStateOf(false) }
            Surface(
                onClick = {
                    when {
                        attachment.isHtml -> onOpenHtml(attachment)
                        viewer != null && isTextLikeAttachment(attachment) -> viewer.openAttachment(attachment)
                        else -> {
                            busy = true
                            scope.launch {
                                runCatching {
                                    val file = AttachmentFiles.download(
                                        context,
                                        settings,
                                        attachment,
                                    )
                                    AttachmentFiles.open(context, file, attachment.mime)
                                }.onFailure {
                                    Toast.makeText(
                                        context,
                                        "打开失败：${it.message}",
                                        Toast.LENGTH_SHORT,
                                    ).show()
                                }
                                busy = false
                            }
                        }
                    }
                },
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(12.dp),
                color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.55f),
            ) {
                Row(
                    modifier = Modifier.padding(horizontal = 12.dp, vertical = 10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    Icon(
                        imageVector = when {
                            attachment.isHtml -> Icons.Outlined.Language
                            attachment.isPdf -> Icons.Outlined.PictureAsPdf
                            else -> Icons.Outlined.InsertDriveFile
                        },
                        contentDescription = null,
                        tint = MaterialTheme.colorScheme.primary,
                    )
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = attachment.name,
                            style = MaterialTheme.typography.bodyMedium,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        Text(
                            text = when {
                                attachment.isHtml -> "HTML · 应用内打开"
                                attachment.isPdf -> "PDF · ${attachment.sizeLabel()}"
                                else -> "${attachment.sizeLabel().ifBlank { "文件" }} · 点击用系统应用打开"
                            },
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    if (busy) {
                        CircularProgressIndicator(
                            modifier = Modifier.size(18.dp),
                            strokeWidth = 2.dp,
                        )
                    } else {
                        Box {
                            TextButton(onClick = { menu = true }) {
                                Text("更多")
                            }
                            DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                                DropdownMenuItem(
                                    text = { Text("分享文件") },
                                    onClick = {
                                        menu = false
                                        scope.launch {
                                            runCatching {
                                                val file = AttachmentFiles.download(
                                                    context,
                                                    settings,
                                                    attachment,
                                                )
                                                AttachmentFiles.share(
                                                    context,
                                                    file,
                                                    attachment.mime,
                                                )
                                            }.onFailure {
                                                Toast.makeText(
                                                    context,
                                                    "分享失败：${it.message}",
                                                    Toast.LENGTH_SHORT,
                                                ).show()
                                            }
                                        }
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text("复制链接") },
                                    onClick = {
                                        menu = false
                                        val clipboard = context.getSystemService(
                                            android.content.ClipboardManager::class.java,
                                        )
                                        clipboard.setPrimaryClip(
                                            android.content.ClipData.newPlainText(
                                                "附件链接",
                                                AgentApi.attachmentUrl(settings, attachment),
                                            ),
                                        )
                                        Toast.makeText(context, "链接已复制", Toast.LENGTH_SHORT).show()
                                    },
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun ZoomableRemoteImage(
    url: String,
    description: String,
    onZoomChanged: (Boolean) -> Unit = {},
    onToggleChrome: () -> Unit = {},
) {
    var scale by remember(url) { mutableFloatStateOf(1f) }
    var offsetX by remember(url) { mutableFloatStateOf(0f) }
    var offsetY by remember(url) { mutableFloatStateOf(0f) }
    var retry by remember(url) { mutableIntStateOf(0) }
    var containerSize by remember(url) { mutableStateOf(IntSize.Zero) }

    fun clampOffset(nextScale: Float = scale) {
        val maxX = ((containerSize.width.takeIf { it > 0 } ?: 1080) * (nextScale - 1f)) / 2f
        val maxY = ((containerSize.height.takeIf { it > 0 } ?: 1920) * (nextScale - 1f)) / 2f
        offsetX = offsetX.coerceIn(-maxX.coerceAtLeast(0f), maxX.coerceAtLeast(0f))
        offsetY = offsetY.coerceIn(-maxY.coerceAtLeast(0f), maxY.coerceAtLeast(0f))
    }

    val context = LocalContext.current
    val request = remember(url, retry) {
        ImageRequest.Builder(context)
            .data(url)
            .memoryCacheKey("${AgentApi.mediaCacheKey(url)}#$retry")
            .apply {
                if (retry > 0) {
                    memoryCachePolicy(CachePolicy.WRITE_ONLY)
                    diskCachePolicy(CachePolicy.WRITE_ONLY)
                }
            }
            .build()
    }
    SubcomposeAsyncImage(
        model = request,
        contentDescription = description,
        contentScale = ContentScale.Fit,
        modifier = Modifier
            .fillMaxSize()
            .onSizeChanged { containerSize = it }
            .graphicsLayer {
                scaleX = scale
                scaleY = scale
                translationX = offsetX
                translationY = offsetY
            }
            .pointerInput(url) {
                awaitEachGesture {
                    awaitFirstDown(requireUnconsumed = false)
                    do {
                        val event = awaitPointerEvent()
                        if (event.changes.size > 1 || scale > 1.02f) {
                            val zoom = event.calculateZoom()
                            val pan = event.calculatePan()
                            val nextScale = (scale * zoom).coerceIn(1f, 6f)
                            scale = nextScale
                            if (nextScale <= 1.02f) {
                                scale = 1f
                                offsetX = 0f
                                offsetY = 0f
                            } else {
                                offsetX += pan.x
                                offsetY += pan.y
                                clampOffset(nextScale)
                            }
                            onZoomChanged(scale > 1.02f)
                            event.changes.forEach { change ->
                                if (change.positionChanged()) change.consume()
                            }
                        }
                    } while (event.changes.any { it.pressed })
                }
            }
            .pointerInput(url) {
                detectTapGestures(
                    onTap = { onToggleChrome() },
                    onDoubleTap = { position ->
                        if (scale > 1.2f) {
                            scale = 1f
                            offsetX = 0f
                            offsetY = 0f
                            onZoomChanged(false)
                        } else {
                            scale = 2.6f
                            offsetX = (size.width / 2f - position.x) * 1.6f
                            offsetY = (size.height / 2f - position.y) * 1.6f
                            clampOffset(2.6f)
                            onZoomChanged(true)
                        }
                    },
                )
            },
        loading = {
            Box(contentAlignment = Alignment.Center) {
                CircularProgressIndicator(color = Color.White)
            }
        },
        error = { errState ->
            val reason = errState.result.throwable.message?.take(120).orEmpty()
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(24.dp),
                contentAlignment = Alignment.Center,
            ) {
                Column(
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Icon(Icons.Outlined.Image, contentDescription = null, tint = Color.White)
                    Text("图片加载失败", color = Color.White, style = MaterialTheme.typography.titleSmall)
                    if (reason.isNotBlank()) {
                        Text(
                            text = reason,
                            color = Color.White.copy(alpha = 0.72f),
                            style = MaterialTheme.typography.bodySmall,
                            maxLines = 3,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                    Surface(
                        onClick = { retry += 1 },
                        shape = CircleShape,
                        color = Color.White.copy(alpha = 0.18f),
                        modifier = Modifier.padding(top = 4.dp),
                    ) {
                        Text(
                            text = "重试",
                            modifier = Modifier.padding(horizontal = 16.dp, vertical = 7.dp),
                            color = Color.White,
                        )
                    }
                }
            }
        },
        success = { SubcomposeAsyncImageContent() },
    )
}
