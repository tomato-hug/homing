package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.Bolt
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.CleaningServices
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Menu
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material.icons.outlined.Stop
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.agentpush.poc.AgentProfile
import dev.agentpush.poc.ContextUsage
import dev.agentpush.poc.SessionItemsState
import dev.agentpush.poc.SessionStatus
import dev.agentpush.poc.SessionSummary
import dev.agentpush.poc.R
import dev.agentpush.poc.defaultAgentDisplayName
import dev.agentpush.poc.titleSourceLabel

import dev.agentpush.poc.ui.AgentGlyph
import dev.agentpush.poc.ui.ClaudeOrange
import dev.agentpush.poc.ui.CodexGreen
import dev.agentpush.poc.ui.SessionStatusBadge
import dev.agentpush.poc.ui.theme.HomingMono
import dev.agentpush.poc.ui.theme.HomingPalette
import kotlinx.coroutines.delay

@Composable
internal fun SessionHeader(
    session: SessionSummary?,
    infoExpanded: Boolean,
    transcriptOnlyAgent: Boolean,
    profile: AgentProfile,
    itemsState: SessionItemsState?,
    onBack: () -> Unit,
    onToggleInfo: () -> Unit,
    onOpenSearch: () -> Unit,
    onRefreshTranscript: () -> Unit,
    onToggleOnlyAgent: () -> Unit,
    onToggleReasoning: () -> Unit,
    onCopySessionId: () -> Unit,
    onCopy: (String, String) -> Unit,
    onRename: (String) -> Unit,
    onExport: () -> Unit,
    onStop: () -> Unit,
    onTogglePinned: () -> Unit,
    onToggleMuted: () -> Unit,
    onToggleArchived: () -> Unit,
    onToggleRemindOnDone: () -> Unit,
    navigationAsDrawer: Boolean = false,
    otherUrgentCount: Int = 0,
    onJumpUrgent: (() -> Unit)? = null,
    returnToSessionTitle: String? = null,
    onReturnPreviousSession: (() -> Unit)? = null,
    onQuickClear: (() -> Unit)? = null,
    projectSiblings: List<SessionSummary> = emptyList(),
    projectHistoryIds: Set<String> = emptySet(),
    onOpenSibling: (String) -> Unit = {},
    awayEnabled: Boolean = false,
    onToggleAway: (() -> Unit)? = null,
) {
    var menuExpanded by remember { mutableStateOf(false) }
    var renameOpen by remember { mutableStateOf(false) }
    var titleDetailsOpen by remember { mutableStateOf(false) }
    var renameText by remember { mutableStateOf("") }
    Surface(
        color = MaterialTheme.colorScheme.background.copy(alpha = 0.98f),
        tonalElevation = 0.dp,
        border = BorderStroke(0.dp, Color.Transparent),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .statusBarsPadding(),
        ) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 4.dp, end = 8.dp, top = 4.dp, bottom = 6.dp),
                verticalAlignment = Alignment.Top,
            ) {
                IconButton(
                    onClick = onBack,
                    modifier = if (navigationAsDrawer) {
                        Modifier.testTag("open-workspace-drawer")
                    } else {
                        Modifier
                    },
                ) {
                    Icon(
                        imageVector = if (navigationAsDrawer) {
                            Icons.Outlined.Menu
                        } else {
                            Icons.AutoMirrored.Outlined.ArrowBack
                        },
                        contentDescription = if (navigationAsDrawer) "打开会话抽屉" else "返回",
                    )
                }
                if (session != null) {
                    AgentGlyph(
                        brand = session.brand,
                        source = session.source,
                        size = 24.dp,
                        modifier = Modifier.padding(top = 10.dp),
                    )
                    Spacer(Modifier.width(8.dp))
                    Column(
                        modifier = Modifier.weight(1f),
                    ) {
                        Text(
                            text = session.title,
                            style = TextStyle(
                                fontSize = 17.sp,
                                lineHeight = 22.sp,
                                fontWeight = FontWeight.Bold,
                                color = MaterialTheme.colorScheme.onSurface,
                            ),
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.combinedClickable(
                                onClick = {
                                    titleDetailsOpen = true
                                },
                                onLongClick = onToggleInfo,
                            ).testTag("session-title"),
                        )
                        Spacer(Modifier.height(4.dp))
                        Text(
                            text = buildString {
                                append(session.agentLabel.ifBlank { defaultAgentDisplayName(session.source) })
                                append(" · ")
                                append(session.project.ifBlank { "未命名项目" })
                                session.branch.takeIf {
                                    it.isNotBlank() && !it.equals("HEAD", true)
                                }?.let {
                                    append(" · ")
                                    append(it)
                                }
                            },
                            style = TextStyle(
                                fontSize = 11.5.sp,
                                lineHeight = 16.sp,
                                color = HomingPalette.Muted,
                            ),
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        if (!returnToSessionTitle.isNullOrBlank() && onReturnPreviousSession != null) {
                            Spacer(Modifier.height(4.dp))
                            Surface(
                                onClick = onReturnPreviousSession,
                                color = HomingPalette.Raised,
                                shape = RoundedCornerShape(6.dp),
                                modifier = Modifier.testTag("return-previous-session-pill"),
                            ) {
                                Text(
                                    text = "↩ 返回：$returnToSessionTitle",
                                    modifier = Modifier.padding(horizontal = 7.dp, vertical = 2.dp),
                                    style = TextStyle(
                                        fontSize = 11.sp,
                                        color = HomingPalette.BrandSoft,
                                    ),
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                    }
                    Column(
                        horizontalAlignment = Alignment.End,
                        verticalArrangement = Arrangement.spacedBy(2.dp),
                    ) {
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(4.dp),
                        ) {
                            if (otherUrgentCount > 0 && onJumpUrgent != null) {
                                Surface(
                                    onClick = onJumpUrgent,
                                    color = HomingPalette.Warning.copy(alpha = 0.16f),
                                    border = BorderStroke(1.dp, HomingPalette.Warning.copy(alpha = 0.65f)),
                                    shape = RoundedCornerShape(999.dp),
                                    modifier = Modifier.testTag("urgent-jump-pill"),
                                ) {
                                    Row(
                                        modifier = Modifier.padding(horizontal = 8.dp, vertical = 4.dp),
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(2.dp),
                                    ) {
                                        Icon(
                                            Icons.Outlined.Bolt,
                                            contentDescription = null,
                                            modifier = Modifier.size(13.dp),
                                            tint = HomingPalette.Warning,
                                        )
                                        Text(
                                            text = "$otherUrgentCount 待审 ›",
                                            style = TextStyle(
                                                fontSize = 11.5.sp,
                                                fontWeight = FontWeight.SemiBold,
                                                color = HomingPalette.Warning,
                                            ),
                                        )
                                    }
                                }
                            }
                            HeaderRunState(session = session, itemsState = itemsState)
                            if (onToggleAway != null) {
                                Surface(
                                    onClick = onToggleAway,
                                    color = if (awayEnabled) {
                                        HomingPalette.Brand.copy(alpha = 0.15f)
                                    } else {
                                        HomingPalette.Card
                                    },
                                    border = BorderStroke(
                                        1.dp,
                                        if (awayEnabled) {
                                            HomingPalette.Brand.copy(alpha = 0.6f)
                                        } else {
                                            HomingPalette.NavBorder
                                        },
                                    ),
                                    shape = RoundedCornerShape(999.dp),
                                    modifier = Modifier.testTag("header-away-toggle"),
                                ) {
                                    Row(
                                        modifier = Modifier.padding(horizontal = 7.dp, vertical = 3.dp),
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(4.dp),
                                    ) {
                                        Box(
                                            modifier = Modifier
                                                .size(6.dp)
                                                .background(
                                                    if (awayEnabled) HomingPalette.Added else HomingPalette.Muted,
                                                    CircleShape,
                                                ),
                                        )
                                        Text(
                                            text = if (awayEnabled) "监听中" else "未监听",
                                            style = TextStyle(
                                                fontSize = 10.5.sp,
                                                fontWeight = FontWeight.SemiBold,
                                                color = if (awayEnabled) HomingPalette.Brand else HomingPalette.Muted,
                                            ),
                                        )
                                    }
                                }
                            }
                            if (onQuickClear != null) {
                                IconButton(
                                    onClick = onQuickClear,
                                    modifier = Modifier
                                        .size(34.dp)
                                        .testTag("header-quick-clear"),
                                ) {
                                    Icon(
                                        Icons.Outlined.CleaningServices,
                                        contentDescription = "快速清理会话",
                                        modifier = Modifier.size(18.dp),
                                        tint = HomingPalette.Muted,
                                    )
                                }
                            }
                            IconButton(
                                onClick = { menuExpanded = true },
                                modifier = Modifier.size(36.dp),
                            ) {
                                Icon(
                                    Icons.Outlined.MoreVert,
                                    contentDescription = "更多",
                                    modifier = Modifier.size(21.dp),
                                )
                            }
                            DropdownMenu(
                                expanded = menuExpanded,
                                onDismissRequest = { menuExpanded = false },
                            ) {
                                if (onToggleAway != null) {
                                    DropdownMenuItem(
                                        text = { Text(if (awayEnabled) "关闭离席监听" else "开启离席监听") },
                                        onClick = {
                                            menuExpanded = false
                                            onToggleAway()
                                        },
                                    )
                                }
                                DropdownMenuItem(
                                    text = { Text("搜索这个会话") },
                                    leadingIcon = {
                                        Icon(Icons.Outlined.Search, contentDescription = null)
                                    },
                                    onClick = {
                                        menuExpanded = false
                                        onOpenSearch()
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text("重新加载完整对话") },
                                    leadingIcon = {
                                        Icon(Icons.Outlined.Refresh, contentDescription = null)
                                    },
                                    onClick = {
                                        menuExpanded = false
                                        onRefreshTranscript()
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text(if (infoExpanded) "收起元数据" else "查看元数据") },
                                    onClick = {
                                        menuExpanded = false
                                        onToggleInfo()
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text("复制 session_id") },
                                    leadingIcon = {
                                        Icon(Icons.Outlined.ContentCopy, contentDescription = null)
                                    },
                                    onClick = {
                                        menuExpanded = false
                                        onCopySessionId()
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text("重命名") },
                                    onClick = {
                                        menuExpanded = false
                                        renameText = session.title
                                        renameOpen = true
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text("导出 Markdown") },
                                    leadingIcon = {
                                        Icon(Icons.Outlined.Share, contentDescription = null)
                                    },
                                    onClick = {
                                        menuExpanded = false
                                        onExport()
                                    },
                                )
                                DropdownMenuItem(
                                    text = {
                                        Text(
                                            if (transcriptOnlyAgent) {
                                                "显示全部消息"
                                            } else {
                                                "只看 Agent 回复"
                                            },
                                        )
                                    },
                                    leadingIcon = {
                                        Icon(
                                            if (transcriptOnlyAgent) {
                                                Icons.Outlined.Schedule
                                            } else {
                                                Icons.Outlined.CheckCircle
                                            },
                                            contentDescription = null,
                                        )
                                    },
                                    onClick = {
                                        menuExpanded = false
                                        onToggleOnlyAgent()
                                    },
                                )
                                DropdownMenuItem(
                                    text = {
                                        Text(
                                            if (itemsState?.includeReasoning == true) {
                                                "隐藏思考过程"
                                            } else {
                                                "查看思考过程"
                                            },
                                        )
                                    },
                                    leadingIcon = {
                                        Icon(
                                            if (itemsState?.includeReasoning == true) {
                                                Icons.Outlined.VisibilityOff
                                            } else {
                                                Icons.Outlined.Visibility
                                            },
                                            contentDescription = null,
                                        )
                                    },
                                    onClick = {
                                        menuExpanded = false
                                        onToggleReasoning()
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text(if (session.pinned) "取消置顶" else "置顶会话") },
                                    onClick = {
                                        menuExpanded = false
                                        onTogglePinned()
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text(if (session.muted) "恢复通知" else "静音会话") },
                                    onClick = {
                                        menuExpanded = false
                                        onToggleMuted()
                                    },
                                )
                                DropdownMenuItem(
                                    text = {
                                        Text(
                                            if (session.remindOnDone) {
                                                "取消完成提醒"
                                            } else {
                                                "完成时提醒我"
                                            },
                                        )
                                    },
                                    leadingIcon = {
                                        Icon(Icons.Outlined.Schedule, contentDescription = null)
                                    },
                                    onClick = {
                                        menuExpanded = false
                                        onToggleRemindOnDone()
                                    },
                                )
                                DropdownMenuItem(
                                    text = {
                                        Text(if (session.archived) "恢复归档" else "归档会话")
                                    },
                                    onClick = {
                                        menuExpanded = false
                                        onToggleArchived()
                                    },
                                )
                            }
                        }
                        if (session.running && session.capabilities.stop) {
                            TextButton(
                                onClick = onStop,
                                modifier = Modifier.height(32.dp),
                                contentPadding = PaddingValues(horizontal = 7.dp),
                            ) {
                                Icon(
                                    Icons.Outlined.Stop,
                                    contentDescription = null,
                                    modifier = Modifier.size(14.dp),
                                )
                                Spacer(Modifier.width(2.dp))
                                Text("停止", style = MaterialTheme.typography.labelSmall)
                            }
                        }
                    }
                } else {
                    Text("会话", style = MaterialTheme.typography.titleLarge)
                }
            }
            val contextUsage = session?.contextUsage?.takeIf { it.used > 0 }
            if (contextUsage != null) {
                ContextUsageBar(
                    usage = contextUsage,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 16.dp, vertical = 2.dp),
                )
            }
            if (infoExpanded && session != null) {
                Surface(
                    color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.45f),
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 14.dp, vertical = 5.dp),
                    shape = RoundedCornerShape(10.dp),
                    border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
                ) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        MetadataRow(
                            label = "来源",
                            value = "${session.brand.displayName} · ${session.brand.wire}",
                            onCopy = onCopy,
                        )
                        MetadataRow(
                            label = "完整目录",
                            value = session.cwd.ifBlank { "未提供" },
                            onCopy = onCopy,
                        )
                        MetadataRow(
                            label = "Session ID",
                            value = session.sessionId.ifBlank { "未提供" },
                            onCopy = onCopy,
                            monospace = true,
                        )
                        MetadataRow(
                            label = "标题来源",
                            value = titleSourceLabel(session.titleSource) +
                                if (session.titleSource.isNotBlank()) {
                                    " · ${session.titleSource}"
                                } else {
                                    ""
                                },
                            onCopy = onCopy,
                        )
                        MetadataRow(
                            label = "模型",
                            value = buildString {
                                append(session.model.ifBlank { "未提供" })
                                if (session.effort.isNotBlank()) {
                                    append(" · ")
                                    append(session.effort)
                                }
                            },
                            onCopy = onCopy,
                        )
                        MetadataRow(
                            label = "Git 分支",
                            value = session.branch.ifBlank { "未提供" },
                            onCopy = onCopy,
                            monospace = true,
                        )
                        if (session.permissionMode.isNotBlank()) {
                            MetadataRow(
                                label = "权限模式",
                                value = session.permissionMode,
                                onCopy = onCopy,
                                monospace = true,
                            )
                        }
                        session.contextUsage?.takeIf { it.used > 0 }?.let { usage ->
                            MetadataRow(
                                label = "上下文",
                                value = if (usage.limit > 0) {
                                    "${usage.usedLabel} / ${usage.limitLabel}（${usage.percent}%）"
                                } else {
                                    "已用 ${usage.usedLabel} tokens"
                                },
                                onCopy = onCopy,
                                monospace = true,
                            )
                        }
                        MetadataRow(
                            label = "能力",
                            value = buildString {
                                append("回复")
                                append(if (profile.capabilities.reply) "✓" else "×")
                                append(" 新建")
                                append(if (profile.capabilities.newSession) "✓" else "×")
                                append(" 切模型")
                                append(if (profile.capabilities.switchModel) "✓" else "×")
                                append(" 停止")
                                append(if (profile.capabilities.stop) "✓" else "×")
                            },
                            onCopy = { _, _ -> },
                            copyable = false,
                        )
                    }
                }
            }
        }
    }
    if (titleDetailsOpen && session != null) {
        AlertDialog(
            onDismissRequest = { titleDetailsOpen = false },
            text = { Text(session.title, modifier = Modifier.testTag("session-title-details")) },
            confirmButton = { TextButton(onClick = { titleDetailsOpen = false }) { Text(stringResource(R.string.close)) } },
            dismissButton = { TextButton(onClick = {
                titleDetailsOpen = false
                renameText = session.title
                renameOpen = true
            }) { Text(stringResource(R.string.rename_session)) } },
        )
    }
    if (renameOpen && session != null) {
        AlertDialog(
            onDismissRequest = { renameOpen = false },
            title = { Text("重命名会话") },
            text = {
                OutlinedTextField(
                    value = renameText,
                    onValueChange = { renameText = it.take(120) },
                    label = { Text("标题") },
                    supportingText = { Text("只修改 Homing 的显示名，不改 Agent 文件") },
                    singleLine = true,
                )
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        onRename(renameText.trim())
                        renameOpen = false
                    },
                    enabled = renameText.isNotBlank(),
                ) {
                    Text("保存")
                }
            },
            dismissButton = {
                TextButton(onClick = { renameOpen = false }) {
                    Text("取消")
                }
            },
        )
    }
}

@Composable
private fun HeaderTag(text: String, emphasized: Boolean = false) {
    Surface(
        color = if (emphasized) {
            MaterialTheme.colorScheme.primary.copy(alpha = 0.1f)
        } else {
            MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.55f)
        },
        shape = RoundedCornerShape(5.dp),
    ) {
        Text(
            text = text,
            modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp),
            style = MaterialTheme.typography.labelSmall.copy(fontSize = 11.sp),
            color = if (emphasized) {
                MaterialTheme.colorScheme.primary
            } else {
                MaterialTheme.colorScheme.onSurfaceVariant
            },
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

@Composable
private fun HeaderRunState(
    session: SessionSummary,
    itemsState: SessionItemsState?,
) {
    if (session.running) {
        var now by remember { mutableStateOf(System.currentTimeMillis()) }
        LaunchedEffect(session.runningSince) {
            while (true) {
                now = System.currentTimeMillis()
                delay(1_000)
            }
        }
        Surface(
            color = CodexGreen.copy(alpha = 0.11f),
            shape = RoundedCornerShape(50),
        ) {
            Row(
                modifier = Modifier.padding(horizontal = 7.dp, vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(5.dp),
            ) {
                CircularProgressIndicator(
                    modifier = Modifier.size(11.dp),
                    strokeWidth = 2.dp,
                    color = CodexGreen,
                )
                Text(
                    text = "运行中" +
                        elapsedLabel(session.runningSince, now).let {
                            if (it.isBlank()) "" else " $it"
                        },
                    style = MaterialTheme.typography.labelSmall,
                    color = CodexGreen,
                    maxLines = 1,
                )
            }
        }
        return
    }
    val liveTurn = itemsState?.turns?.lastOrNull()
    if (liveTurn?.state == "failed") {
        SessionStatusBadge(status = SessionStatus.ERROR)
        return
    }
    SessionStatusBadge(status = session.status)
}

@Composable
private fun ContextUsageBar(
    usage: ContextUsage,
    modifier: Modifier = Modifier,
) {
    val color = when {
        usage.fraction >= 0.85f -> MaterialTheme.colorScheme.error
        usage.fraction >= 0.65f -> ClaudeOrange
        else -> CodexGreen
    }
    Column(
        modifier = if (usage.limit > 0) {
            modifier.testTag("context-usage-summary")
        } else {
            modifier.testTag("context-usage-pill")
        },
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(bottom = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = if (usage.limit > 0) {
                    "上下文 ${usage.usedLabel} / ${usage.limitLabel}"
                } else {
                    "上下文已用 ${usage.usedLabel} tokens"
                },
                style = MaterialTheme.typography.labelSmall.copy(
                    fontFamily = HomingMono,
                    fontSize = 10.sp,
                ),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
            )
            Spacer(Modifier.weight(1f))
            if (usage.limit > 0) {
                Text(
                    text = "${usage.percent}%",
                    style = MaterialTheme.typography.labelSmall.copy(
                        fontFamily = HomingMono,
                        fontSize = 10.sp,
                    ),
                    color = color,
                )
            }
        }
        if (usage.limit > 0) {
            LinearProgressIndicator(
                progress = { usage.fraction },
                modifier = Modifier
                    .fillMaxWidth()
                    .height(3.dp)
                    .testTag("context-usage-bar"),
                color = color,
                trackColor = MaterialTheme.colorScheme.surfaceVariant,
            )
        }
    }
}

internal fun elapsedLabel(since: String, now: Long): String {
    if (since.isBlank()) return ""
    val start = runCatching { java.time.Instant.parse(since).toEpochMilli() }.getOrNull()
        ?: return ""
    val seconds = ((now - start) / 1000L).coerceAtLeast(0L)
    return when {
        seconds < 60 -> "${seconds}s"
        seconds < 3600 -> "${seconds / 60}m ${seconds % 60}s"
        else -> "${seconds / 3600}h ${(seconds % 3600) / 60}m"
    }
}

@Composable
private fun MetadataRow(
    label: String,
    value: String,
    onCopy: (String, String) -> Unit,
    monospace: Boolean = false,
    copyable: Boolean = true,
) {
    val canCopy = copyable && value.isNotBlank() && value != "未提供"
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 2.dp),
        verticalAlignment = Alignment.Top,
    ) {
        Text(
            text = label,
            modifier = Modifier
                .width(74.dp)
                .padding(top = 2.dp),
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            text = value,
            modifier = Modifier
                .weight(1f)
                .padding(top = 1.dp),
            style = MaterialTheme.typography.bodySmall.copy(
                fontFamily = if (monospace) HomingMono else FontFamily.Default,
            ),
            color = MaterialTheme.colorScheme.onSurface,
        )
        IconButton(
            onClick = { onCopy(label, value) },
            enabled = canCopy,
            modifier = Modifier.size(26.dp),
        ) {
            Icon(
                imageVector = Icons.Outlined.ContentCopy,
                contentDescription = "复制$label",
                modifier = Modifier.size(14.dp),
                tint = if (canCopy) {
                    MaterialTheme.colorScheme.onSurfaceVariant
                } else {
                    MaterialTheme.colorScheme.outline
                },
            )
        }
    }
}
