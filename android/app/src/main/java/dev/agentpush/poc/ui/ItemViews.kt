package dev.agentpush.poc.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Build
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.HourglassEmpty
import androidx.compose.material.icons.outlined.OpenInFull
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.Remove
import androidx.compose.material.icons.outlined.RadioButtonUnchecked
import androidx.compose.material.icons.outlined.Security
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import dev.agentpush.poc.AgentBrand
import dev.agentpush.poc.AppSettings
import dev.agentpush.poc.ApprovalOption
import dev.agentpush.poc.ItemFileChange
import dev.agentpush.poc.SessionItem
import dev.agentpush.poc.SessionItemType
import dev.agentpush.poc.formatClock
import dev.agentpush.poc.ui.theme.HomingMono

@OptIn(ExperimentalFoundationApi::class)
@Composable
fun UserItemBubble(
    item: SessionItem,
    onLongClick: () -> Unit,
) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.End,
    ) {
        Surface(
            modifier = Modifier
                .widthIn(max = 348.dp)
                .combinedClickable(onClick = {}, onLongClick = onLongClick),
            color = MaterialTheme.colorScheme.primary,
            shape = RoundedCornerShape(
                topStart = 18.dp,
                topEnd = 5.dp,
                bottomEnd = 18.dp,
                bottomStart = 18.dp,
            ),
        ) {
            Column(modifier = Modifier.padding(horizontal = 14.dp, vertical = 11.dp)) {
                SelectionContainer {
                    Text(
                        text = item.text,
                        style = MaterialTheme.typography.bodyLarge.copy(
                            fontSize = 16.sp,
                            lineHeight = 24.sp,
                        ),
                        color = MaterialTheme.colorScheme.onPrimary,
                    )
                }
                val model = item.model.trim()
                if (model.isNotBlank()) {
                    Spacer(Modifier.height(6.dp))
                    Surface(
                        color = MaterialTheme.colorScheme.onPrimary.copy(alpha = 0.14f),
                        shape = RoundedCornerShape(50),
                    ) {
                        Text(
                            text = "本次 · $model",
                            modifier = Modifier.padding(horizontal = 7.dp, vertical = 3.dp),
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onPrimary.copy(alpha = 0.9f),
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                }
            }
        }
    }
}

@Composable
fun AssistantItemCard(
    item: SessionItem,
    brand: AgentBrand,
    settings: AppSettings,
    onOpenUrl: (String) -> Unit,
    onCopyCode: (String) -> Unit,
) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        color = MaterialTheme.colorScheme.surface,
        shape = RoundedCornerShape(16.dp),
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
        tonalElevation = 1.dp,
    ) {
        Column(modifier = Modifier.padding(14.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = brand.displayName,
                    style = MaterialTheme.typography.labelMedium,
                    color = brand.accentColor(),
                )
                Spacer(Modifier.weight(1f))
                Text(
                    text = formatClock(item.ts),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Spacer(Modifier.height(6.dp))
            RichMarkdown(
                source = absolutizeMarkdown(item.text.ifBlank { "（无文字内容）" }, settings),
                onCopyCode = onCopyCode,
                onOpenUrl = onOpenUrl,
            )
        }
    }
}

@Composable
fun ReasoningItemCard(item: SessionItem, durationSeconds: Long? = null) {
    var expanded by remember(item.id) { mutableStateOf(false) }
    Surface(
        modifier = Modifier.fillMaxWidth(),
        color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.42f),
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.7f)),
    ) {
        Column(modifier = Modifier.padding(horizontal = 12.dp, vertical = 8.dp)) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(7.dp))
                    .combinedClickable(
                        onClick = { expanded = !expanded },
                        onLongClick = { expanded = !expanded },
                    ),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(
                    Icons.Outlined.HourglassEmpty,
                    contentDescription = null,
                    modifier = Modifier.size(16.dp),
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(
                    text = durationSeconds?.let { "思考 ${reasoningDurationLabel(it)}" } ?: "思考",
                    modifier = Modifier
                        .weight(1f)
                        .padding(start = 7.dp),
                    style = MaterialTheme.typography.labelLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(
                    text = "${item.text.length} 字",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.outline,
                )
                Icon(
                    imageVector = if (expanded) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore,
                    contentDescription = if (expanded) "收起思考过程" else "展开思考过程",
                    modifier = Modifier
                        .padding(start = 4.dp)
                        .size(18.dp),
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (expanded) {
                Spacer(Modifier.height(8.dp))
                SelectionContainer {
                    Text(
                        text = item.text,
                        style = MaterialTheme.typography.bodyMedium.copy(
                            fontSize = 14.sp,
                            lineHeight = 21.sp,
                        ),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

private fun reasoningDurationLabel(seconds: Long): String {
    return when {
        seconds < 60 -> "${seconds}s"
        seconds < 3_600 -> "${seconds / 60}m ${seconds % 60}s"
        else -> "${seconds / 3_600}h ${(seconds % 3_600) / 60}m"
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
fun ToolCallItemCard(
    item: SessionItem,
    onCopy: () -> Unit,
) {
    var expanded by remember(item.id) { mutableStateOf(false) }
    var outputDialog by remember(item.id) { mutableStateOf(false) }
    val failed = item.isFailed
    val accent = if (failed) MaterialTheme.colorScheme.error else CodexGreen
    val hasOutput = item.tool.outputTail.isNotBlank()
    Surface(
        modifier = Modifier
            .fillMaxWidth()
            .combinedClickable(
                onClick = { if (hasOutput) expanded = !expanded },
                onLongClick = onCopy,
            ),
        color = if (failed) {
            MaterialTheme.colorScheme.errorContainer.copy(alpha = 0.35f)
        } else {
            MaterialTheme.colorScheme.surface
        },
        shape = RoundedCornerShape(13.dp),
        border = BorderStroke(
            1.dp,
            if (failed) {
                MaterialTheme.colorScheme.error.copy(alpha = 0.5f)
            } else {
                MaterialTheme.colorScheme.outlineVariant
            },
        ),
    ) {
        Column(modifier = Modifier.padding(horizontal = 12.dp, vertical = 10.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Box(
                    modifier = Modifier
                        .size(27.dp)
                        .clip(RoundedCornerShape(8.dp))
                        .background(accent.copy(alpha = 0.13f)),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        imageVector = Icons.Outlined.Terminal,
                        contentDescription = null,
                        tint = accent,
                        modifier = Modifier.size(16.dp),
                    )
                }
                Column(
                    modifier = Modifier
                        .weight(1f)
                        .padding(start = 8.dp),
                ) {
                    Text(
                        text = item.tool.name.ifBlank { "工具调用" },
                        style = MaterialTheme.typography.labelLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        text = toolStatusLabel(item),
                        style = MaterialTheme.typography.labelSmall,
                        color = if (failed) MaterialTheme.colorScheme.error
                        else MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                if (hasOutput) {
                    IconButton(
                        onClick = { outputDialog = true },
                        modifier = Modifier.size(36.dp),
                    ) {
                        Icon(
                            imageVector = Icons.Outlined.OpenInFull,
                            contentDescription = "全屏查看工具输出",
                            modifier = Modifier.size(18.dp),
                            tint = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    Icon(
                        imageVector = if (expanded) Icons.Outlined.ExpandLess
                        else Icons.Outlined.ExpandMore,
                        contentDescription = if (expanded) "收起输出" else "展开输出",
                        modifier = Modifier.size(19.dp),
                        tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            val summary = item.tool.inputSummary.ifBlank { item.text }
            if (summary.isNotBlank()) {
                SelectionContainer {
                    Text(
                        text = summary,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = 7.dp),
                        style = MaterialTheme.typography.bodySmall.copy(
                            fontFamily = HomingMono,
                            fontSize = 12.sp,
                            lineHeight = 18.sp,
                        ),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = if (expanded) 12 else 3,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
            if (expanded && hasOutput) {
                Spacer(Modifier.height(8.dp))
                Surface(
                    color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.55f),
                    shape = RoundedCornerShape(8.dp),
                ) {
                    Text(
                        text = item.tool.outputTail,
                        modifier = Modifier
                            .fillMaxWidth()
                            .horizontalScroll(rememberScrollState())
                            .padding(9.dp),
                        style = MaterialTheme.typography.bodySmall.copy(
                            fontFamily = HomingMono,
                            fontSize = 12.sp,
                            lineHeight = 18.sp,
                        ),
                        softWrap = false,
                    )
                }
            }
            if (item.tool.exitCode != null) {
                Text(
                    text = "退出码 ${item.tool.exitCode}",
                    modifier = Modifier.padding(top = 6.dp),
                    style = MaterialTheme.typography.labelSmall,
                    color = if (failed) MaterialTheme.colorScheme.error
                    else MaterialTheme.colorScheme.outline,
                )
            }
            if (item.truncated) {
                Text(
                    text = "即時預覽已截斷，回合結束後會載入完整輸出",
                    modifier = Modifier.padding(top = 6.dp),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.outline,
                )
            }
        }
    }
    if (outputDialog) {
        FullScreenCodeDialog(
            title = "工具输出 · ${item.tool.name.ifBlank { "工具调用" }}",
            tabs = listOf("" to item.tool.outputTail),
            onDismiss = { outputDialog = false },
        )
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
fun FileChangeItemCard(
    item: SessionItem,
    onCopy: () -> Unit,
) {
    var expanded by remember(item.id) { mutableStateOf(false) }
    var diffDialog by remember(item.id) { mutableStateOf(false) }
    Surface(
        modifier = Modifier
            .fillMaxWidth()
            .combinedClickable(
                onClick = { expanded = !expanded },
                onLongClick = onCopy,
            ),
        color = MaterialTheme.colorScheme.surface,
        shape = RoundedCornerShape(13.dp),
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
    ) {
        Column(modifier = Modifier.padding(horizontal = 12.dp, vertical = 10.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(
                    Icons.Outlined.Build,
                    contentDescription = null,
                    modifier = Modifier.size(17.dp),
                    tint = MaterialTheme.colorScheme.primary,
                )
                Text(
                    text = "修改文件" + if (item.files.isNotEmpty()) " · ${item.files.size}" else "",
                    modifier = Modifier
                        .weight(1f)
                        .padding(start = 7.dp),
                    style = MaterialTheme.typography.labelLarge,
                )
                if (item.files.any { it.diff.isNotBlank() } || item.text.isNotBlank()) {
                    IconButton(
                        onClick = { diffDialog = true },
                        modifier = Modifier.size(36.dp),
                    ) {
                        Icon(
                            imageVector = Icons.Outlined.OpenInFull,
                            contentDescription = "全屏查看 diff",
                            modifier = Modifier.size(18.dp),
                            tint = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
                Icon(
                    imageVector = if (expanded) Icons.Outlined.ExpandLess
                    else Icons.Outlined.ExpandMore,
                    contentDescription = if (expanded) "收起 diff" else "展开 diff",
                    modifier = Modifier.size(19.dp),
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            item.files.forEach { file ->
                FileChangeRow(file)
            }
            if (expanded) {
                val diff = item.files.firstOrNull { it.diff.isNotBlank() }?.diff
                    ?: item.text
                if (diff.isNotBlank()) {
                    Spacer(Modifier.height(7.dp))
                    DiffBlock(diff)
                }
            } else if (item.files.isEmpty() && item.text.isNotBlank()) {
                Text(
                    text = item.text,
                    modifier = Modifier.padding(top = 5.dp),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            if (item.truncated) {
                Text(
                    text = "即時預覽已截斷，回合結束後會載入完整 diff",
                    modifier = Modifier.padding(top = 6.dp),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.outline,
                )
            }
        }
    }
    if (diffDialog) {
        val tabs = item.files
            .filter { it.diff.isNotBlank() }
            .map { it.path to it.diff }
            .ifEmpty { listOf("diff" to item.text) }
        FullScreenCodeDialog(
            title = "文件改动",
            tabs = tabs,
            onDismiss = { diffDialog = false },
        )
    }
}

@Composable
private fun FileChangeRow(file: ItemFileChange) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = file.path,
            modifier = Modifier.weight(1f),
            style = MaterialTheme.typography.bodySmall.copy(
                fontFamily = HomingMono,
                fontSize = 12.sp,
            ),
            maxLines = 1,
            overflow = TextOverflow.MiddleEllipsis,
        )
        if (file.added > 0) {
            Text(
                text = "+${file.added}",
                modifier = Modifier.padding(start = 7.dp),
                style = MaterialTheme.typography.labelSmall,
                color = CodexGreen,
            )
        }
        if (file.removed > 0) {
            Text(
                text = "-${file.removed}",
                modifier = Modifier.padding(start = 5.dp),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.error,
            )
        }
    }
}

@Composable
private fun DiffBlock(diff: String) {
    Surface(
        color = Color(0xFF101418),
        shape = RoundedCornerShape(8.dp),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .horizontalScroll(rememberScrollState())
                .padding(9.dp),
        ) {
            diff.lineSequence().take(160).forEach { line ->
                Text(
                    text = line.ifBlank { " " },
                    style = MaterialTheme.typography.bodySmall.copy(
                        fontFamily = HomingMono,
                        fontSize = 11.5.sp,
                        lineHeight = 17.sp,
                    ),
                    color = when {
                        line.startsWith("+") -> Color(0xFF7EE2A8)
                        line.startsWith("-") -> Color(0xFFFF9A9A)
                        line.startsWith("@@") -> Color(0xFF80C7FF)
                        else -> Color(0xFFD6DEE8)
                    },
                    softWrap = false,
                    maxLines = 1,
                )
            }
        }
    }
}

@Composable
private fun FullScreenCodeDialog(
    title: String,
    tabs: List<Pair<String, String>>,
    onDismiss: () -> Unit,
) {
    val available = tabs.ifEmpty { listOf("" to "") }
    var selectedTab by remember(title) { mutableIntStateOf(0) }
    var fontScale by remember(title) { mutableFloatStateOf(1f) }
    val selected = available[selectedTab.coerceIn(available.indices)]
    val vertical = rememberScrollState()
    val horizontal = rememberScrollState()

    Dialog(
        onDismissRequest = onDismiss,
        properties = DialogProperties(usePlatformDefaultWidth = false),
    ) {
        Surface(
            modifier = Modifier.fillMaxSize(),
            color = Color(0xFF0D1117),
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
                        .padding(start = 16.dp, end = 6.dp, top = 6.dp, bottom = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = title,
                            style = MaterialTheme.typography.titleMedium,
                            color = Color(0xFFE6EDF3),
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        if (selected.first.isNotBlank()) {
                            Text(
                                text = selected.first,
                                style = MaterialTheme.typography.labelSmall.copy(
                                    fontFamily = HomingMono,
                                    fontSize = 11.sp,
                                ),
                                color = Color(0xFF8B949E),
                                maxLines = 1,
                                overflow = TextOverflow.MiddleEllipsis,
                            )
                        }
                    }
                    IconButton(
                        onClick = { fontScale = (fontScale - 0.1f).coerceAtLeast(0.7f) },
                    ) {
                        Icon(
                            Icons.Outlined.Remove,
                            contentDescription = "缩小字号",
                            tint = Color(0xFFB1BAC4),
                        )
                    }
                    Text(
                        text = "${(fontScale * 100).toInt()}%",
                        style = MaterialTheme.typography.labelSmall.copy(
                            fontFamily = HomingMono,
                        ),
                        color = Color(0xFF8B949E),
                    )
                    IconButton(
                        onClick = { fontScale = (fontScale + 0.1f).coerceAtMost(1.8f) },
                    ) {
                        Icon(
                            Icons.Outlined.Add,
                            contentDescription = "放大字号",
                            tint = Color(0xFFB1BAC4),
                        )
                    }
                    IconButton(onClick = onDismiss) {
                        Icon(
                            Icons.Outlined.Close,
                            contentDescription = "关闭",
                            tint = Color(0xFFE6EDF3),
                        )
                    }
                }
                if (available.size > 1) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .horizontalScroll(rememberScrollState())
                            .padding(horizontal = 12.dp, vertical = 4.dp),
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        available.forEachIndexed { index, tab ->
                            Surface(
                                onClick = { selectedTab = index },
                                shape = RoundedCornerShape(8.dp),
                                color = if (index == selectedTab) {
                                    Color(0xFF1F6FEB)
                                } else {
                                    Color(0xFF161B22)
                                },
                            ) {
                                Text(
                                    text = tab.first.ifBlank { "diff ${index + 1}" },
                                    modifier = Modifier.padding(
                                        horizontal = 10.dp,
                                        vertical = 7.dp,
                                    ),
                                    style = MaterialTheme.typography.labelSmall.copy(
                                        fontFamily = HomingMono,
                                    ),
                                    color = if (index == selectedTab) {
                                        Color.White
                                    } else {
                                        Color(0xFFB1BAC4)
                                    },
                                    maxLines = 1,
                                )
                            }
                        }
                    }
                }
                SelectionContainer(
                    modifier = Modifier
                        .fillMaxWidth()
                        .weight(1f)
                        .verticalScroll(vertical)
                        .padding(horizontal = 14.dp, vertical = 10.dp),
                ) {
                    Column(modifier = Modifier.horizontalScroll(horizontal)) {
                        selected.second.lineSequence().forEach { line ->
                            Text(
                                text = line.ifBlank { " " },
                                style = MaterialTheme.typography.bodySmall.copy(
                                    fontFamily = HomingMono,
                                    fontSize = (12 * fontScale).sp,
                                    lineHeight = (18 * fontScale).sp,
                                ),
                                color = when {
                                    line.startsWith("+") -> Color(0xFF7EE2A8)
                                    line.startsWith("-") -> Color(0xFFFF9A9A)
                                    line.startsWith("@@") -> Color(0xFF80C7FF)
                                    else -> Color(0xFFD6DEE8)
                                },
                                softWrap = false,
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
fun PlanItemCard(item: SessionItem) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.35f),
        shape = RoundedCornerShape(13.dp),
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
    ) {
        Column(modifier = Modifier.padding(horizontal = 12.dp, vertical = 10.dp)) {
            Text(
                text = "执行计划",
                style = MaterialTheme.typography.labelLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            item.plan.forEach { step ->
                val done = step.status.lowercase() in setOf(
                    "completed",
                    "complete",
                    "done",
                    "finished",
                )
                val active = step.status.lowercase() in setOf("in_progress", "running", "active")
                Row(
                    modifier = Modifier.padding(top = 7.dp),
                    verticalAlignment = Alignment.Top,
                ) {
                    Icon(
                        imageVector = when {
                            done -> Icons.Outlined.CheckCircle
                            active -> Icons.Outlined.PlayArrow
                            else -> Icons.Outlined.RadioButtonUnchecked
                        },
                        contentDescription = null,
                        modifier = Modifier
                            .padding(top = 1.dp)
                            .size(16.dp),
                        tint = when {
                            done -> CodexGreen
                            active -> ClaudeOrange
                            else -> MaterialTheme.colorScheme.outline
                        },
                    )
                    Text(
                        text = step.step,
                        modifier = Modifier
                            .weight(1f)
                            .padding(start = 7.dp),
                        style = MaterialTheme.typography.bodyMedium.copy(
                            fontSize = 14.sp,
                            lineHeight = 21.sp,
                        ),
                        color = if (done) {
                            MaterialTheme.colorScheme.onSurfaceVariant
                        } else {
                            MaterialTheme.colorScheme.onSurface
                        },
                    )
                }
            }
            if (item.plan.isEmpty() && item.text.isNotBlank()) {
                SelectionContainer {
                    Text(
                        text = item.text,
                        modifier = Modifier.padding(top = 6.dp),
                        style = MaterialTheme.typography.bodyMedium,
                    )
                }
            }
        }
    }
}

@Composable
fun ApprovalItemCard(
    item: SessionItem,
    canApprove: Boolean,
    approvalOptions: List<ApprovalOption> = emptyList(),
    onDecision: (Long?, String, String?) -> Unit,
) {
    val status = item.tool.status.lowercase()
    val pending = status.isBlank() || status == "pending"
    val options = approvalOptions.ifEmpty {
        listOf(
            ApprovalOption("allow_once", "允许", "allow"),
            ApprovalOption("reject_once", "拒绝", "deny"),
        )
    }
    Surface(
        modifier = Modifier.fillMaxWidth(),
        color = ClaudeOrange.copy(alpha = 0.1f),
        shape = RoundedCornerShape(14.dp),
        border = BorderStroke(1.dp, ClaudeOrange.copy(alpha = 0.48f)),
    ) {
        Column(modifier = Modifier.padding(13.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(
                    Icons.Outlined.Security,
                    contentDescription = null,
                    modifier = Modifier.size(19.dp),
                    tint = ClaudeOrange,
                )
                Text(
                    text = "需要授权",
                    modifier = Modifier
                        .weight(1f)
                        .padding(start = 7.dp),
                    style = MaterialTheme.typography.titleSmall,
                    color = ClaudeOrange,
                )
                Text(
                    text = formatClock(item.ts),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (item.text.isNotBlank()) {
                SelectionContainer {
                    Text(
                        text = item.text,
                        modifier = Modifier.padding(top = 8.dp),
                        style = MaterialTheme.typography.bodyMedium.copy(
                            fontSize = 15.sp,
                            lineHeight = 22.sp,
                        ),
                    )
                }
            }
            if (canApprove && pending) {
                val buttons: @Composable (Modifier) -> Unit = { modifier ->
                    options.forEach { option ->
                        if (option.decision == "deny") {
                            OutlinedButton(
                                onClick = {
                                    onDecision(
                                        item.eventId,
                                        option.decision,
                                        option.id.takeIf { it.isNotBlank() },
                                    )
                                },
                                enabled = item.eventId != null,
                                modifier = modifier,
                                shape = RoundedCornerShape(10.dp),
                            ) {
                                Text(
                                    option.label,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                        } else {
                            Button(
                                onClick = {
                                    onDecision(
                                        item.eventId,
                                        option.decision,
                                        option.id.takeIf { it.isNotBlank() },
                                    )
                                },
                                enabled = item.eventId != null,
                                modifier = modifier,
                                shape = RoundedCornerShape(10.dp),
                            ) {
                                Text(
                                    option.label,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                    }
                }
                if (options.size <= 2) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = 11.dp),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        buttons(Modifier.weight(1f))
                    }
                } else {
                    Column(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = 11.dp),
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        buttons(Modifier.fillMaxWidth())
                    }
                }
            } else {
                Text(
                    text = when {
                        !canApprove ->
                            "当前 Agent 未启用应用内授权，请在输入框回复“同意”或“拒绝”。"
                        status == "allowed" -> "已允许"
                        status == "denied" -> "已拒绝"
                        status == "timeout" -> "授权请求已超时"
                        else -> "这个授权请求已处理"
                    },
                    modifier = Modifier.padding(top = 8.dp),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (canApprove && item.eventId == null) {
                Text(
                    text = "该条目没有可审批的事件 ID，只能通过文字回复授权。",
                    modifier = Modifier.padding(top = 6.dp),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
fun ErrorItemCard(item: SessionItem) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        color = MaterialTheme.colorScheme.errorContainer.copy(alpha = 0.55f),
        shape = RoundedCornerShape(13.dp),
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.error.copy(alpha = 0.45f)),
    ) {
        Row(
            modifier = Modifier.padding(12.dp),
            verticalAlignment = Alignment.Top,
        ) {
            Icon(
                Icons.Outlined.ErrorOutline,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.error,
                modifier = Modifier.size(20.dp),
            )
            SelectionContainer {
                Text(
                    text = item.text.ifBlank { "Agent 报告了一个错误" },
                    modifier = Modifier
                        .weight(1f)
                        .padding(start = 8.dp),
                    style = MaterialTheme.typography.bodyMedium.copy(
                        fontSize = 14.sp,
                        lineHeight = 21.sp,
                    ),
                    color = MaterialTheme.colorScheme.onErrorContainer,
                )
            }
        }
    }
}

@Composable
fun SystemItemRow(item: SessionItem) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 3.dp),
        horizontalArrangement = Arrangement.Center,
    ) {
        Text(
            text = item.text,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.outline,
        )
    }
}

@Composable
fun ItemCopyButton(
    text: String,
    onCopy: () -> Unit,
) {
    if (text.isBlank()) return
    TextButton(onClick = onCopy) {
        Icon(
            Icons.Outlined.ContentCopy,
            contentDescription = null,
            modifier = Modifier.size(15.dp),
        )
        Text("复制", modifier = Modifier.padding(start = 4.dp))
    }
}

private fun toolStatusLabel(item: SessionItem): String {
    return when {
        item.tool.status.equals("error", true) -> "失败"
        item.tool.status.equals("failed", true) -> "失败"
        item.tool.status.equals("pending", true) -> "执行中"
        item.tool.status.equals("running", true) -> "执行中"
        item.tool.exitCode != null && item.tool.exitCode != 0 ->
            "失败 · 退出码 ${item.tool.exitCode}"
        item.tool.exitCode == 0 -> "完成 · 退出码 0"
        item.tool.outputTail.isNotBlank() -> "完成"
        else -> "已调用"
    }
}

fun absolutizeMarkdown(source: String, settings: AppSettings): String {
    val base = settings.baseUrl.trimEnd('/')
    if (base.isBlank()) return source
    return Regex("(!?\\[[^\\]]*]\\((?!https?://|data:|content:)/)([^)]+)")
        .replace(source) { match ->
            "${match.groupValues[1]}$base/${match.groupValues[2].trimStart('/')}"
        }
}
