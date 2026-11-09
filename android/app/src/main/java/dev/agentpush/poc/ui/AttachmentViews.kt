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
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.core.content.ContextCompat
import coil3.compose.SubcomposeAsyncImage
import coil3.compose.SubcomposeAsyncImageContent
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
    Column(
        modifier = modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        images.chunked(3).forEachIndexed { rowIndex, row ->
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                row.forEachIndexed { columnIndex, attachment ->
                    val index = rowIndex * 3 + columnIndex
                    RemoteThumbnail(
                        url = AgentApi.attachmentUrl(settings, attachment),
                        description = attachment.name,
                        onClick = { onOpen(index) },
                        modifier = Modifier.weight(1f),
                    )
                }
                repeat(3 - row.size) {
                    Box(modifier = Modifier.weight(1f))
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
    val request = coil3.request.ImageRequest.Builder(LocalContext.current)
        .data(url)
        .memoryCacheKey("$url#$retry")
        .build()
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
            error = {
                Box(contentAlignment = Alignment.Center) {
                    Column(horizontalAlignment = Alignment.CenterHorizontally) {
                        Icon(Icons.Outlined.Refresh, contentDescription = "重试")
                        Surface(
                            onClick = { retry += 1 },
                            color = MaterialTheme.colorScheme.surfaceVariant,
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
        )
    }
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
                modifier = Modifier.fillMaxSize(),
            ) { page ->
                ZoomableRemoteImage(
                    url = AgentApi.attachmentUrl(settings, images[page]),
                    description = images[page].name,
                )
            }
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .align(Alignment.TopCenter)
                    .padding(horizontal = 8.dp, vertical = 30.dp),
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
            Text(
                text = images.getOrNull(pagerState.currentPage)?.name.orEmpty(),
                modifier = Modifier
                    .align(Alignment.BottomCenter)
                    .padding(horizontal = 24.dp, vertical = 38.dp),
                color = Color.White.copy(alpha = 0.85f),
                style = MaterialTheme.typography.bodySmall,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
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
private fun ZoomableRemoteImage(url: String, description: String) {
    var scale by remember(url) { mutableFloatStateOf(1f) }
    var offsetX by remember(url) { mutableFloatStateOf(0f) }
    var offsetY by remember(url) { mutableFloatStateOf(0f) }
    var retry by remember(url) { mutableIntStateOf(0) }

    fun clampOffset() {
        val maxX = 420f * (scale - 1f)
        val maxY = 760f * (scale - 1f)
        offsetX = offsetX.coerceIn(-maxX.coerceAtLeast(0f), maxX.coerceAtLeast(0f))
        offsetY = offsetY.coerceIn(-maxY.coerceAtLeast(0f), maxY.coerceAtLeast(0f))
    }

    val request = coil3.request.ImageRequest.Builder(LocalContext.current)
        .data(url)
        .memoryCacheKey("$url#$retry")
        .build()
    SubcomposeAsyncImage(
        model = request,
        contentDescription = description,
        contentScale = ContentScale.Fit,
        modifier = Modifier
            .fillMaxSize()
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
                        if (event.changes.size > 1 || scale > 1.01f) {
                            val zoom = event.calculateZoom()
                            val pan = event.calculatePan()
                            scale = (scale * zoom).coerceIn(1f, 6f)
                            offsetX += pan.x
                            offsetY += pan.y
                            clampOffset()
                            event.changes.forEach { change ->
                                if (change.positionChanged()) change.consume()
                            }
                        }
                    } while (event.changes.any { it.pressed })
                }
            }
            .pointerInput(url) {
                detectTapGestures(
                    onDoubleTap = { position ->
                        if (scale > 1.2f) {
                            scale = 1f
                            offsetX = 0f
                            offsetY = 0f
                        } else {
                            scale = 2.6f
                            offsetX = (size.width / 2f - position.x) * 1.6f
                            offsetY = (size.height / 2f - position.y) * 1.6f
                        }
                    },
                )
            },
        loading = {
            Box(contentAlignment = Alignment.Center) {
                CircularProgressIndicator(color = Color.White)
            }
        },
        error = {
            Box(
                modifier = Modifier.fillMaxSize(),
                contentAlignment = Alignment.Center,
            ) {
                Column(horizontalAlignment = Alignment.CenterHorizontally) {
                    Icon(Icons.Outlined.Image, contentDescription = null, tint = Color.White)
                    Text("图片加载失败", color = Color.White)
                    Surface(
                        onClick = { retry += 1 },
                        shape = CircleShape,
                        color = Color.White.copy(alpha = 0.18f),
                        modifier = Modifier.padding(top = 8.dp),
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
