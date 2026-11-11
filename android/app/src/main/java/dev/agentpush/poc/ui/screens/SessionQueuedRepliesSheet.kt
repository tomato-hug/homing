package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import dev.agentpush.poc.AgentReply
import dev.agentpush.poc.formatClock
import dev.agentpush.poc.ui.theme.HomingMono
import dev.agentpush.poc.ui.theme.HomingPalette

/**
 * Queue control for replies the relay has accepted but the agent has not consumed.
 *
 * Editing is implemented as "withdraw, then put the text back in the composer": the server has
 * no edit-in-place operation, and resending must create a new reply after the old one is gone.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun SessionQueuedRepliesSheet(
    open: Boolean,
    replies: List<AgentReply>,
    onDismiss: () -> Unit,
    onEdit: (AgentReply) -> Unit,
    onCancel: (AgentReply) -> Unit,
    onCancelAll: () -> Unit,
) {
    if (!open) return
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        containerColor = HomingPalette.Background,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(bottom = 16.dp),
        ) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 18.dp, end = 8.dp, bottom = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "排队中的回复",
                        style = MaterialTheme.typography.titleMedium,
                        color = HomingPalette.Text,
                    )
                    Text(
                        text = "${replies.size} 条等待 Agent 读取",
                        style = MaterialTheme.typography.labelSmall,
                        color = HomingPalette.Muted,
                    )
                }
                if (replies.isNotEmpty()) {
                    TextButton(onClick = onCancelAll) {
                        Text("全部撤回", color = HomingPalette.Error)
                    }
                }
                IconButton(onClick = onDismiss) {
                    Icon(Icons.Outlined.Close, contentDescription = "关闭排队回复")
                }
            }
            if (replies.isEmpty()) {
                Text(
                    text = "没有排队中的回复。",
                    modifier = Modifier.padding(horizontal = 18.dp, vertical = 20.dp),
                    style = MaterialTheme.typography.bodySmall,
                    color = HomingPalette.Muted,
                )
            } else {
                LazyColumn(
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(max = 480.dp)
                        .testTag("queued-replies-list"),
                    contentPadding = PaddingValues(horizontal = 14.dp, vertical = 4.dp),
                    verticalArrangement = Arrangement.spacedBy(7.dp),
                ) {
                    items(replies, key = { it.id }) { reply ->
                        Surface(
                            color = HomingPalette.Card,
                            shape = RoundedCornerShape(12.dp),
                        ) {
                            Column(
                                modifier = Modifier.padding(horizontal = 12.dp, vertical = 10.dp),
                            ) {
                                Row(
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                                ) {
                                    Icon(
                                        Icons.Outlined.Schedule,
                                        contentDescription = null,
                                        modifier = Modifier.size(15.dp),
                                        tint = HomingPalette.Warning,
                                    )
                                    Text(
                                        text = formatClock(reply.ts).ifBlank { "刚刚" },
                                        style = MaterialTheme.typography.labelSmall.copy(
                                            fontFamily = HomingMono,
                                        ),
                                        color = HomingPalette.Muted,
                                    )
                                    if (reply.model.isNotBlank()) {
                                        Text(
                                            text = reply.model,
                                            style = MaterialTheme.typography.labelSmall.copy(
                                                fontFamily = HomingMono,
                                            ),
                                            color = HomingPalette.Muted,
                                            maxLines = 1,
                                            overflow = TextOverflow.Ellipsis,
                                        )
                                    }
                                    if (reply.files.isNotEmpty()) {
                                        Text(
                                            text = "${reply.files.size} 个附件",
                                            style = MaterialTheme.typography.labelSmall,
                                            color = HomingPalette.Muted,
                                        )
                                    }
                                }
                                Text(
                                    text = reply.text.ifBlank { "仅附件" },
                                    modifier = Modifier.padding(top = 6.dp),
                                    style = MaterialTheme.typography.bodySmall,
                                    color = HomingPalette.Body,
                                    maxLines = 4,
                                    overflow = TextOverflow.Ellipsis,
                                )
                                Row(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .padding(top = 5.dp),
                                    horizontalArrangement = Arrangement.End,
                                    verticalAlignment = Alignment.CenterVertically,
                                ) {
                                    TextButton(
                                        onClick = { onEdit(reply) },
                                        modifier = Modifier.height(36.dp),
                                    ) {
                                        Icon(
                                            Icons.Outlined.Edit,
                                            contentDescription = null,
                                            modifier = Modifier.size(15.dp),
                                        )
                                        Text("编辑")
                                    }
                                    TextButton(
                                        onClick = { onCancel(reply) },
                                        modifier = Modifier.height(36.dp),
                                    ) {
                                        Text("撤回", color = HomingPalette.Error)
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}
