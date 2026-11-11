package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.CallSplit
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import dev.agentpush.poc.AgentBrand
import dev.agentpush.poc.AgentDispatch
import dev.agentpush.poc.DispatchStatus
import dev.agentpush.poc.SessionStatus
import dev.agentpush.poc.ui.AgentAvatar
import dev.agentpush.poc.ui.ClaudeOrange
import dev.agentpush.poc.ui.CodexGreen
import dev.agentpush.poc.ui.accentColor

@Composable
internal fun CrossAgentDispatchPanel(
    dispatches: List<AgentDispatch>,
    targetStatuses: Map<String, SessionStatus?>,
    onOpenSession: (String) -> Unit,
    onForward: (AgentDispatch, String?) -> Unit,
    onCancel: (AgentDispatch) -> Unit,
    onDiscard: (AgentDispatch) -> Unit,
    onReply: (AgentDispatch) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Icon(
                Icons.AutoMirrored.Outlined.CallSplit,
                contentDescription = null,
                modifier = Modifier.size(16.dp),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(
                text = "跨 Agent 子任务 · ${dispatches.size}",
                style = MaterialTheme.typography.labelLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        for (dispatch in dispatches) {
            CrossAgentDispatchCard(
                dispatch = dispatch,
                targetStatus = targetStatuses[dispatch.id],
                onOpenSession = onOpenSession,
                onForward = onForward,
                onCancel = onCancel,
                onDiscard = onDiscard,
                onReply = onReply,
            )
        }
    }
}

@Composable
internal fun InboundDispatchPanel(
    dispatches: List<AgentDispatch>,
    onOpenSession: (String) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        for (dispatch in dispatches) {
            val parentBrand = AgentBrand.fromWire(dispatch.parentSource)
            val accent = parentBrand.accentColor()
            Surface(
                modifier = Modifier
                    .fillMaxWidth()
                    .testTag("inbound-dispatch-${dispatch.id}"),
                color = accent.copy(alpha = 0.07f),
                shape = RoundedCornerShape(14.dp),
                border = BorderStroke(1.dp, accent.copy(alpha = 0.22f)),
            ) {
                Row(
                    modifier = Modifier.padding(11.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    Icon(
                        Icons.AutoMirrored.Outlined.CallSplit,
                        contentDescription = null,
                        modifier = Modifier.size(18.dp),
                        tint = accent,
                    )
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = "来自 ${parentBrand.displayName} 的跨 Agent 任务",
                            style = MaterialTheme.typography.labelLarge,
                            color = MaterialTheme.colorScheme.onSurface,
                        )
                        Text(
                            text = dispatch.text.replace(Regex("\\s+"), " "),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 2,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                    TextButton(
                        onClick = { onOpenSession(dispatch.parentSessionId) },
                        enabled = dispatch.parentSessionId.isNotBlank(),
                    ) {
                        Text("来源会话")
                    }
                }
            }
        }
    }
}

@Composable
private fun CrossAgentDispatchCard(
    dispatch: AgentDispatch,
    targetStatus: SessionStatus?,
    onOpenSession: (String) -> Unit,
    onForward: (AgentDispatch, String?) -> Unit,
    onCancel: (AgentDispatch) -> Unit,
    onDiscard: (AgentDispatch) -> Unit,
    onReply: (AgentDispatch) -> Unit,
) {
    val brand = dispatch.targetBrand
    val accent = brand.accentColor()
    val confirmDiscard = remember(dispatch.id) { mutableStateOf(false) }
    var forwardDraft by remember(dispatch.id) { mutableStateOf<String?>(null) }
    val clipboard = LocalClipboardManager.current
    val (statusLabel, statusColor) = when {
        dispatch.status == DispatchStatus.FAILED ->
            "派发失败" to MaterialTheme.colorScheme.error
        dispatch.status == DispatchStatus.WAITING_SESSION ->
            "等待会话绑定" to ClaudeOrange
        dispatch.status == DispatchStatus.NEEDS_YOU ->
            "等你处理" to ClaudeOrange
        dispatch.status == DispatchStatus.COMPLETED ->
            "完成" to CodexGreen
        dispatch.status == DispatchStatus.STARTING ->
            "启动中" to MaterialTheme.colorScheme.onSurfaceVariant
        dispatch.status == DispatchStatus.CANCELLED ->
            "已取消" to MaterialTheme.colorScheme.onSurfaceVariant
        targetStatus == SessionStatus.NEEDS_PERMISSION ->
            "子会话等待授权" to ClaudeOrange
        targetStatus == SessionStatus.WAITING_INPUT ->
            "子会话等待输入" to ClaudeOrange
        targetStatus == SessionStatus.RUNNING ->
            "子会话运行中" to CodexGreen
        targetStatus == SessionStatus.ERROR ->
            "子会话出错" to MaterialTheme.colorScheme.error
        targetStatus == SessionStatus.COMPLETED ->
            "子会话已完成" to CodexGreen
        else ->
            "已启动" to CodexGreen
    }
    Surface(
        modifier = Modifier
            .fillMaxWidth()
            .testTag("cross-agent-dispatch-${dispatch.id}"),
        color = accent.copy(alpha = 0.07f),
        shape = RoundedCornerShape(16.dp),
        border = BorderStroke(1.dp, accent.copy(alpha = 0.24f)),
    ) {
        Column(modifier = Modifier.padding(12.dp)) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(9.dp),
            ) {
                AgentAvatar(brand, source = dispatch.parentSource, size = 34.dp)
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "派给 ${brand.displayName}",
                        style = MaterialTheme.typography.titleSmall,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Text(
                        text = "@${dispatch.targetAgent}",
                        style = MaterialTheme.typography.labelSmall,
                        color = accent,
                    )
                }
                Surface(
                    color = statusColor.copy(alpha = 0.12f),
                    shape = RoundedCornerShape(50),
                ) {
                    Text(
                        text = statusLabel,
                        modifier = Modifier.padding(horizontal = 8.dp, vertical = 4.dp),
                        style = MaterialTheme.typography.labelSmall,
                        color = statusColor,
                    )
                }
            }
            Spacer(Modifier.height(9.dp))
            Text(
                text = dispatch.text,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
                maxLines = 5,
                overflow = TextOverflow.Ellipsis,
            )
            if (dispatch.message.isNotBlank()) {
                Spacer(Modifier.height(5.dp))
                Text(
                    text = dispatch.message,
                    style = MaterialTheme.typography.labelSmall,
                    color = if (dispatch.status == DispatchStatus.FAILED) {
                        MaterialTheme.colorScheme.error
                    } else {
                        MaterialTheme.colorScheme.onSurfaceVariant
                    },
                    maxLines = 3,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            val outcome = dispatch.result
            if (dispatch.status == DispatchStatus.COMPLETED &&
                outcome != null &&
                outcome.text.isNotBlank()
            ) {
                Spacer(Modifier.height(8.dp))
                Surface(
                    color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.45f),
                    shape = RoundedCornerShape(10.dp),
                ) {
                    Text(
                        text = outcome.text,
                        modifier = Modifier.padding(9.dp),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = 6,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                val detail = buildList {
                    if (outcome.filesChanged.isNotEmpty()) {
                        add("改了 ${outcome.filesChanged.size} 个文件")
                    }
                    if (outcome.branch.isNotBlank()) {
                        add("分支 ${outcome.branch}")
                    }
                }.joinToString(" · ")
                if (detail.isNotBlank()) {
                    Text(
                        text = detail,
                        modifier = Modifier.padding(top = 5.dp),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                if (outcome.diffStat.isNotBlank()) {
                    Spacer(Modifier.height(6.dp))
                    Surface(
                        color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.35f),
                        shape = RoundedCornerShape(8.dp),
                    ) {
                        Text(
                            text = outcome.diffStat,
                            modifier = Modifier.padding(8.dp),
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = 4.dp),
                horizontalArrangement = Arrangement.End,
            ) {
                if (dispatch.status == DispatchStatus.COMPLETED) {
                    TextButton(onClick = { onReply(dispatch) }) {
                        Text("追问")
                    }
                    TextButton(
                        onClick = {
                            val branch = dispatch.result?.branch.orEmpty()
                            forwardDraft = buildString {
                                append("【@${dispatch.targetAgent} 的结论】\n")
                                append(dispatch.result?.text.orEmpty())
                                if (branch.isNotBlank()) {
                                    append("\n\n请检查并合并分支 ")
                                    append(branch)
                                    append("。")
                                }
                            }
                        },
                    ) {
                        Text("转交")
                    }
                } else if (dispatch.status != DispatchStatus.CANCELLED &&
                    dispatch.status != DispatchStatus.FAILED
                ) {
                    TextButton(onClick = { onCancel(dispatch) }) {
                        Text("取消")
                    }
                }
                if (dispatch.workspace == "worktree" &&
                    dispatch.branch.isNotBlank() &&
                    dispatch.worktreeDiscardedAt.isBlank() &&
                    (dispatch.status == DispatchStatus.COMPLETED ||
                        dispatch.status == DispatchStatus.CANCELLED ||
                        dispatch.status == DispatchStatus.FAILED)
                ) {
                    TextButton(onClick = { confirmDiscard.value = true }) {
                        Text("丢弃")
                    }
                }
                if (dispatch.targetSessionId.isNotBlank()) {
                    TextButton(onClick = { onOpenSession(dispatch.targetSessionId) }) {
                        Text("打开 ${brand.displayName} 会话")
                    }
                }
                val resultBranch = dispatch.result?.branch.orEmpty()
                if (resultBranch.isNotBlank()) {
                    TextButton(
                        onClick = { clipboard.setText(AnnotatedString(resultBranch)) },
                    ) {
                        Text("复制分支")
                    }
                }
            }
        }
    }
    if (confirmDiscard.value) {
        AlertDialog(
            onDismissRequest = { confirmDiscard.value = false },
            title = { Text("丢弃协作 worktree？") },
            text = {
                Text(
                    "将删除 ${dispatch.branch} 和未合并的改动；主工作区不受影响。" +
                        "这一步无法撤销。",
                )
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        confirmDiscard.value = false
                        onDiscard(dispatch)
                    },
                ) {
                    Text("丢弃")
                }
            },
            dismissButton = {
                TextButton(onClick = { confirmDiscard.value = false }) {
                    Text("取消")
                }
            },
        )
    }
    forwardDraft?.let { draft ->
        AlertDialog(
            onDismissRequest = { forwardDraft = null },
            title = { Text("转交给父 Agent") },
            text = {
                OutlinedTextField(
                    value = draft,
                    onValueChange = { forwardDraft = it },
                    label = { Text("可编辑的回覆文本") },
                    minLines = 4,
                    maxLines = 10,
                )
            },
            confirmButton = {
                TextButton(
                    enabled = draft.isNotBlank(),
                    onClick = {
                        onForward(dispatch, draft.trim())
                        forwardDraft = null
                    },
                ) {
                    Text("送出")
                }
            },
            dismissButton = {
                TextButton(onClick = { forwardDraft = null }) {
                    Text("取消")
                }
            },
        )
    }
}
