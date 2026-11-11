package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.QuestionAnswer
import androidx.compose.material.icons.outlined.Shield
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.agentpush.poc.AgentEvent
import dev.agentpush.poc.ApprovalOption
import dev.agentpush.poc.SessionStatus
import dev.agentpush.poc.SessionSummary
import dev.agentpush.poc.ui.theme.HomingMono
import dev.agentpush.poc.ui.theme.HomingPalette

/**
 * A compact, always-visible action surface for the request that is currently blocking a session.
 *
 * The full event stays in the timeline. This dock only keeps the next action within thumb reach,
 * matching the "pending request above composer" pattern used by T3 Code.
 */
@Composable
internal fun SessionPendingDock(
    session: SessionSummary,
    event: AgentEvent,
    expanded: Boolean,
    onToggleExpanded: () -> Unit,
    onDecision: (eventId: Long, decision: String, optionId: String?) -> Unit,
    onReply: () -> Unit,
) {
    val permission = event.isPermission || session.status == SessionStatus.NEEDS_PERMISSION
    val options = if (permission) {
        event.approvalOptions.ifEmpty {
            listOf(
                ApprovalOption("allow_once", "允许一次", "allow"),
                ApprovalOption("reject_once", "拒绝", "deny"),
            )
        }
    } else {
        emptyList()
    }
    val accent = if (permission) HomingPalette.Warning else HomingPalette.Brand
    val title = if (permission) "需要授权" else "等你回复"
    val detail = event.text.ifBlank { event.title }.ifBlank { "Agent 正在等待你继续。" }

    Surface(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 10.dp, vertical = 6.dp)
            .testTag("session-pending-dock"),
        color = HomingPalette.Card,
        shape = RoundedCornerShape(16.dp),
        border = BorderStroke(1.dp, accent.copy(alpha = 0.48f)),
        shadowElevation = 4.dp,
    ) {
        Column(modifier = Modifier.padding(horizontal = 12.dp, vertical = 9.dp)) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(9.dp),
            ) {
                Surface(
                    color = accent.copy(alpha = 0.14f),
                    shape = RoundedCornerShape(10.dp),
                ) {
                    Icon(
                        imageVector = if (permission) {
                            Icons.Outlined.Shield
                        } else {
                            Icons.Outlined.QuestionAnswer
                        },
                        contentDescription = null,
                        modifier = Modifier
                            .padding(7.dp)
                            .size(17.dp),
                        tint = accent,
                    )
                }
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = title,
                        style = MaterialTheme.typography.labelLarge.copy(
                            fontWeight = FontWeight.SemiBold,
                        ),
                        color = accent,
                    )
                    Text(
                        text = detail.replace(Regex("\\s+"), " ").trim(),
                        modifier = Modifier.padding(top = 1.dp),
                        style = MaterialTheme.typography.bodySmall,
                        color = HomingPalette.Body,
                        maxLines = if (expanded) 5 else 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                TextButton(
                    onClick = onToggleExpanded,
                    modifier = Modifier.height(38.dp),
                ) {
                    Text(if (expanded) "收起" else "详情", style = MaterialTheme.typography.labelSmall)
                    Spacer(Modifier.width(2.dp))
                    Icon(
                        imageVector = if (expanded) {
                            Icons.Outlined.ExpandLess
                        } else {
                            Icons.Outlined.ExpandMore
                        },
                        contentDescription = null,
                        modifier = Modifier.size(16.dp),
                    )
                }
            }

            when {
                permission && options.size <= 2 -> {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = 7.dp),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        options.forEach { option ->
                            val allow = option.decision != "deny"
                            if (allow) {
                                Button(
                                    onClick = { onDecision(event.id, option.decision, option.id) },
                                    modifier = Modifier.weight(1f),
                                    colors = ButtonDefaults.buttonColors(
                                        containerColor = accent,
                                        contentColor = HomingPalette.Background,
                                    ),
                                    shape = RoundedCornerShape(10.dp),
                                ) {
                                    Text(option.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                }
                            } else {
                                OutlinedButton(
                                    onClick = { onDecision(event.id, option.decision, option.id) },
                                    modifier = Modifier.weight(1f),
                                    shape = RoundedCornerShape(10.dp),
                                    colors = ButtonDefaults.outlinedButtonColors(
                                        contentColor = HomingPalette.Error,
                                    ),
                                ) {
                                    Text(option.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                }
                            }
                        }
                    }
                }
                permission -> {
                    OutlinedButton(
                        onClick = onToggleExpanded,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = 7.dp),
                        shape = RoundedCornerShape(10.dp),
                        colors = ButtonDefaults.outlinedButtonColors(contentColor = accent),
                    ) {
                        Text("查看 ${options.size} 个授权选项")
                    }
                }
                else -> {
                    Button(
                        onClick = onReply,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = 7.dp),
                        shape = RoundedCornerShape(10.dp),
                        colors = ButtonDefaults.buttonColors(
                            containerColor = accent,
                            contentColor = HomingPalette.Background,
                        ),
                    ) {
                        Icon(
                            Icons.Outlined.Edit,
                            contentDescription = null,
                            modifier = Modifier.size(16.dp),
                        )
                        Spacer(Modifier.width(6.dp))
                        Text("输入回复")
                    }
                }
            }

            if (expanded) {
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(top = 8.dp)
                        .background(
                            color = HomingPalette.Background.copy(alpha = 0.58f),
                            shape = RoundedCornerShape(10.dp),
                        )
                        .padding(horizontal = 10.dp, vertical = 8.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    if (permission) {
                        val command = event.text.ifBlank { event.title }
                        if (command.isNotBlank()) {
                            Text(
                                text = command,
                                style = MaterialTheme.typography.bodySmall.copy(
                                    fontFamily = HomingMono,
                                    fontSize = 11.sp,
                                ),
                                color = HomingPalette.Body,
                            )
                        }
                        if (options.size > 2) {
                            options.forEach { option ->
                                val allow = option.decision != "deny"
                                TextButton(
                                    onClick = { onDecision(event.id, option.decision, option.id) },
                                    modifier = Modifier.fillMaxWidth(),
                                ) {
                                    Text(
                                        text = option.label,
                                        modifier = Modifier.weight(1f),
                                        color = if (allow) accent else HomingPalette.Error,
                                        maxLines = 1,
                                        overflow = TextOverflow.Ellipsis,
                                    )
                                    Text(
                                        text = if (allow) "允许" else "拒绝",
                                        style = MaterialTheme.typography.labelSmall,
                                        color = if (allow) accent else HomingPalette.Error,
                                    )
                                }
                            }
                        }
                    } else {
                        Text(
                            text = detail,
                            style = MaterialTheme.typography.bodySmall,
                            color = HomingPalette.Body,
                        )
                    }
                }
            }
        }
    }
}

