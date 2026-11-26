package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Computer
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Shield
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.agentpush.poc.ApprovalOption
import dev.agentpush.poc.HomeAttention
import dev.agentpush.poc.SessionMeta
import dev.agentpush.poc.elapsedSince
import dev.agentpush.poc.relativeTime
import dev.agentpush.poc.ui.HomingCard
import dev.agentpush.poc.ui.theme.HomingMono
import dev.agentpush.poc.ui.theme.HomingPalette

@Composable
internal fun HomeRunningStrip(
    sessions: List<SessionMeta>,
    now: Long,
    showComputer: Boolean,
    onOpenSession: (String, String) -> Unit,
    onSelectSession: (dev.agentpush.poc.SessionMeta) -> Unit = {},
) {
    LazyRow(
        contentPadding = PaddingValues(horizontal = 1.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(sessions, key = { dev.agentpush.poc.phoneConversationKey(it.computerId, it.source, it.sessionId) }) { session ->
            Surface(
                onClick = { onSelectSession(session); onOpenSession(session.sessionId, session.computerId) },
                shape = RoundedCornerShape(50),
                color = HomingPalette.Brand.copy(alpha = 0.09f),
                border = BorderStroke(1.dp, HomingPalette.Brand.copy(alpha = 0.24f)),
            ) {
                Row(
                    modifier = Modifier
                        .height(40.dp)
                        .padding(horizontal = 13.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(7.dp),
                ) {
                    Box(
                        modifier = Modifier
                            .size(7.dp)
                            .clip(CircleShape)
                            .background(HomingPalette.Brand),
                    )
                    Text(
                        text = listOf(
                            if (showComputer) session.computerName else "",
                            session.brand.displayName,
                            session.project,
                        ).filter { it.isNotBlank() }.joinToString(" · "),
                        style = MaterialTheme.typography.labelMedium,
                        color = HomingPalette.Body,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        text = elapsedSince(session.runningSince.ifBlank { session.lastTs }, now)
                            .ifBlank { "运行中" },
                        style = MaterialTheme.typography.labelSmall.copy(
                            fontFamily = HomingMono,
                            fontSize = 11.sp,
                        ),
                        color = HomingPalette.Brand,
                        maxLines = 1,
                    )
                }
            }
        }
    }
}

@Composable
internal fun HomeSectionLabel(text: String, count: Int) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = text,
            style = MaterialTheme.typography.titleMedium.copy(fontSize = 16.sp),
            color = HomingPalette.Text,
        )
        Spacer(Modifier.width(7.dp))
        Surface(color = HomingPalette.Raised, shape = CircleShape) {
            Text(
                text = count.toString(),
                modifier = Modifier.padding(horizontal = 7.dp, vertical = 1.dp),
                style = MaterialTheme.typography.labelSmall,
                color = HomingPalette.Muted,
            )
        }
    }
}

@Composable
internal fun HomeAttentionCard(
    item: HomeAttention,
    draft: String,
    showComputer: Boolean,
    onDraftChange: (String) -> Unit,
    onOpen: () -> Unit,
    onApprovalOption: (Long, String, String) -> Unit,
    onSend: () -> Unit,
    onMarkSeen: () -> Unit,
) {
    val accent = attentionColor(item.reason)
    HomingCard(borderColor = accent.copy(alpha = 0.38f)) {
        Column(modifier = Modifier.padding(horizontal = 14.dp, vertical = 13.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Surface(color = accent.copy(alpha = 0.15f), shape = CircleShape) {
                    Icon(
                        imageVector = attentionIcon(item.reason),
                        contentDescription = null,
                        modifier = Modifier
                            .padding(6.dp)
                            .size(16.dp),
                        tint = accent,
                    )
                }
                Spacer(Modifier.width(9.dp))
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = attentionLabel(item.reason),
                        style = MaterialTheme.typography.labelMedium,
                        color = accent,
                    )
                    Text(
                        text = item.session.title.ifBlank { item.headline },
                        style = MaterialTheme.typography.titleSmall,
                        color = HomingPalette.Text,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                Text(
                    text = relativeTime(item.at),
                    style = MaterialTheme.typography.labelSmall.copy(fontFamily = HomingMono),
                    color = HomingPalette.Muted,
                )
            }
            if (showComputer && item.session.computerName.isNotBlank()) {
                ComputerScopeLabel(
                    name = item.session.computerName,
                    modifier = Modifier.padding(top = 7.dp),
                )
            }
            Text(
                text = listOf(
                    item.session.brand.displayName,
                    item.session.project,
                    item.session.branch,
                ).filter { it.isNotBlank() }.joinToString(" · "),
                modifier = Modifier.padding(top = 7.dp),
                style = MaterialTheme.typography.labelSmall.copy(fontFamily = HomingMono),
                color = HomingPalette.Muted,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (item.headline.isNotBlank()) {
                Text(
                    text = item.headline,
                    modifier = Modifier.padding(top = 8.dp),
                    style = MaterialTheme.typography.bodyMedium,
                    color = HomingPalette.Text,
                )
            }
            if (item.summary.isNotBlank()) {
                Text(
                    text = item.summary,
                    modifier = Modifier.padding(top = 3.dp),
                    style = MaterialTheme.typography.bodySmall,
                    color = HomingPalette.Body,
                    maxLines = 3,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            val approval = item.approval
            if (approval != null && item.reason == "approval") {
                Surface(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(top = 9.dp),
                    color = HomingPalette.Background.copy(alpha = 0.65f),
                    shape = RoundedCornerShape(8.dp),
                    border = BorderStroke(1.dp, HomingPalette.Border.copy(alpha = 0.35f)),
                ) {
                    Text(
                        text = approval.command,
                        modifier = Modifier.padding(horizontal = 10.dp, vertical = 8.dp),
                        style = MaterialTheme.typography.bodySmall.copy(fontFamily = HomingMono),
                        color = HomingPalette.Body,
                    )
                }
            }
            item.collab?.let { collab ->
                Text(
                    text = buildString {
                        append("跨 Agent")
                        if (collab.children > 0) append(" · ${collab.children} 个子任务")
                        if (collab.running > 0) append(" · ${collab.running} 个运行中")
                        if (collab.needsYou > 0) append(" · ${collab.needsYou} 个等你")
                    },
                    modifier = Modifier.padding(top = 8.dp),
                    style = MaterialTheme.typography.labelSmall,
                    color = HomingPalette.BrandSoft,
                )
            }
            when {
                item.reason == "approval" -> {
                    val options = approval?.options.orEmpty().ifEmpty {
                        listOf(
                            ApprovalOption("reject_once", "拒绝", "deny"),
                            ApprovalOption("allow_once", "允许", "allow"),
                        )
                    }
                    if (options.size <= 2) {
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(top = 10.dp),
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                        ) {
                            for (option in options) {
                                ApprovalOptionButton(
                                    option = option,
                                    modifier = Modifier.weight(1f),
                                    onClick = {
                                        onApprovalOption(item.eventId, option.decision, option.id)
                                    },
                                )
                            }
                        }
                    } else {
                        Column(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(top = 10.dp),
                            verticalArrangement = Arrangement.spacedBy(7.dp),
                        ) {
                            for (option in options) {
                                ApprovalOptionButton(
                                    option = option,
                                    modifier = Modifier.fillMaxWidth(),
                                    onClick = {
                                        onApprovalOption(item.eventId, option.decision, option.id)
                                    },
                                )
                            }
                        }
                    }
                }
                item.reason == "needs_reply" -> {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = 9.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        OutlinedTextField(
                            value = draft,
                            onValueChange = onDraftChange,
                            modifier = Modifier.weight(1f),
                            placeholder = { Text("输入回复…", style = MaterialTheme.typography.bodySmall) },
                            textStyle = MaterialTheme.typography.bodySmall,
                            singleLine = true,
                            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
                            keyboardActions = KeyboardActions(onSend = { onSend() }),
                            shape = RoundedCornerShape(9.dp),
                        )
                        IconButton(onClick = onSend, enabled = draft.isNotBlank()) {
                            Icon(
                                Icons.AutoMirrored.Outlined.Send,
                                contentDescription = "发送",
                                tint = if (draft.isNotBlank()) HomingPalette.Brand else HomingPalette.Muted,
                            )
                        }
                    }
                }
            }
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                TextButton(onClick = onOpen) {
                    Text("打开会话", color = HomingPalette.BrandSoft)
                }
                Spacer(Modifier.weight(1f))
                TextButton(onClick = onMarkSeen) {
                    Text("标为已看", color = HomingPalette.Muted)
                }
            }
        }
    }
}

@Composable
internal fun ComputerScopeLabel(
    name: String,
    modifier: Modifier = Modifier,
) {
    Surface(
        modifier = modifier,
        color = HomingPalette.Brand.copy(alpha = 0.11f),
        shape = RoundedCornerShape(50),
        border = BorderStroke(1.dp, HomingPalette.Brand.copy(alpha = 0.2f)),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 8.dp, vertical = 3.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Icon(
                Icons.Outlined.Computer,
                contentDescription = null,
                modifier = Modifier.size(12.dp),
                tint = HomingPalette.BrandSoft,
            )
            Text(
                text = name,
                style = MaterialTheme.typography.labelSmall.copy(fontSize = 10.5.sp),
                color = HomingPalette.BrandSoft,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@Composable
private fun ApprovalOptionButton(
    option: ApprovalOption,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
) {
    val allow = option.decision != "deny"
    if (allow) {
        Button(
            onClick = onClick,
            modifier = modifier,
            colors = ButtonDefaults.buttonColors(
                containerColor = if (option.id == "allow_once") {
                    HomingPalette.Warning
                } else {
                    HomingPalette.Brand
                },
                contentColor = HomingPalette.Background,
            ),
        ) {
            Text(option.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    } else {
        OutlinedButton(
            onClick = onClick,
            modifier = modifier,
            colors = ButtonDefaults.outlinedButtonColors(contentColor = HomingPalette.Error),
        ) {
            Text(option.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

private fun attentionColor(reason: String): Color {
    return when (reason) {
        "approval" -> HomingPalette.Warning
        "needs_reply" -> HomingPalette.Brand
        "error" -> HomingPalette.Error
        "collab_result" -> Color(0xFFC4B5FD)
        "done" -> HomingPalette.Added
        else -> HomingPalette.Muted
    }
}

private fun attentionIcon(reason: String) = when (reason) {
    "approval" -> Icons.Outlined.Shield
    "error" -> Icons.Outlined.ErrorOutline
    "done", "collab_result" -> Icons.Outlined.Check
    else -> Icons.Outlined.Search
}

private fun attentionLabel(reason: String) = when (reason) {
    "approval" -> "请求授权"
    "needs_reply" -> "等你回复"
    "error" -> "运行出错"
    "collab_result" -> "协作结果"
    "done" -> "已完成"
    else -> "有新进展"
}
