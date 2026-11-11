package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.ChevronRight
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.agentpush.poc.SessionMeta
import dev.agentpush.poc.relativeTime
import dev.agentpush.poc.ui.theme.HomingMono
import dev.agentpush.poc.ui.theme.HomingPalette
import dev.agentpush.poc.ui.AgentAvatar

@Composable
internal fun ResumeZone(
    sessions: List<SessionMeta>,
    now: Long,
    showComputer: Boolean,
    onOpenSession: (String, String) -> Unit,
    onNewSession: () -> Unit,
    onOpenAll: () -> Unit,
) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        color = Color.Transparent,
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(1.dp, HomingPalette.Border.copy(alpha = 0.65f)),
    ) {
        Column {
            Row(
                modifier = Modifier.padding(start = 14.dp, end = 6.dp, top = 8.dp, bottom = 2.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = "继续工作",
                    style = MaterialTheme.typography.titleMedium.copy(fontSize = 16.sp),
                    color = HomingPalette.Text,
                )
                Spacer(Modifier.weight(1f))
                TextButton(onClick = onNewSession) {
                    Icon(Icons.Outlined.Add, contentDescription = null, modifier = Modifier.size(16.dp))
                    Spacer(Modifier.width(4.dp))
                    Text("新会话", color = HomingPalette.BrandSoft)
                }
            }
            if (sessions.isEmpty()) {
                Text(
                    text = "没有需要继续的会话",
                    modifier = Modifier.padding(horizontal = 14.dp, vertical = 10.dp),
                    style = MaterialTheme.typography.bodySmall,
                    color = HomingPalette.Muted,
                )
            }
            sessions.forEach { session ->
                Surface(
                    onClick = { onOpenSession(session.sessionId, session.computerId) },
                    color = Color.Transparent,
                ) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 12.dp, vertical = 10.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        AgentAvatar(
                            brand = session.brand,
                            source = session.source,
                            size = 30.dp,
                        )
                        Column(
                            modifier = Modifier.weight(1f),
                        ) {
                            Text(
                                text = session.title.ifBlank { session.sessionId },
                                style = MaterialTheme.typography.bodyMedium,
                                color = HomingPalette.Text,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                            if (showComputer && session.computerName.isNotBlank()) {
                                Text(
                                    text = session.computerName,
                                    modifier = Modifier.padding(top = 1.dp),
                                    style = MaterialTheme.typography.labelSmall.copy(fontSize = 10.5.sp),
                                    color = HomingPalette.BrandSoft,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                        Text(
                            text = relativeTime(session.lastTs),
                            style = MaterialTheme.typography.labelSmall.copy(fontFamily = HomingMono),
                            color = HomingPalette.Muted,
                        )
                        Icon(
                            Icons.Outlined.ChevronRight,
                            contentDescription = null,
                            modifier = Modifier.size(16.dp),
                            tint = HomingPalette.Muted,
                        )
                    }
                }
            }
            TextButton(
                onClick = onOpenAll,
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text("全部会话", color = HomingPalette.Muted)
            }
        }
    }
}
