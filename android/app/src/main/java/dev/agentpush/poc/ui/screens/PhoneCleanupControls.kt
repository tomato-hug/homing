package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ClearAll
import androidx.compose.material.icons.outlined.Undo
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import dev.agentpush.poc.ui.theme.HomingPalette

@Composable
internal fun PhoneCleanupControls(
    canUndo: Boolean,
    hasHiddenSessions: Boolean,
    onUndo: () -> Unit,
    onRestore: () -> Unit,
    inboxHasContent: Boolean? = null,
    onClearInbox: () -> Unit = {},
) {
    if (inboxHasContent == null && !canUndo && !hasHiddenSessions) return
    Column(modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            if (inboxHasContent != null) {
                TextButton(
                    onClick = onClearInbox,
                    enabled = inboxHasContent,
                    modifier = Modifier.testTag("phone-clear-inbox").semantics {
                        contentDescription = "清空手机收件箱，仅清理手机显示"
                    },
                ) {
                    Icon(Icons.Outlined.ClearAll, contentDescription = null, modifier = Modifier.size(18.dp))
                    Text("清空收件箱", modifier = Modifier.padding(start = 6.dp))
                }
            }
            if (canUndo) {
                TextButton(onClick = onUndo, modifier = Modifier.testTag("phone-cleanup-undo")) {
                    Icon(Icons.Outlined.Undo, contentDescription = null, modifier = Modifier.size(18.dp))
                    Text("撤销清理", modifier = Modifier.padding(start = 6.dp))
                }
            }
        }
        if (hasHiddenSessions) {
            TextButton(onClick = onRestore, modifier = Modifier.testTag("phone-restore-sessions")) {
                Text("恢复手机上移除的会话")
            }
        }
        Text(
            text = "仅清理手机显示，新消息仍会出现",
            modifier = Modifier.padding(start = 12.dp, bottom = 6.dp),
            style = MaterialTheme.typography.labelSmall,
            color = HomingPalette.Muted,
        )
    }
}
