package dev.agentpush.poc.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.agentpush.poc.AgentBrand
import dev.agentpush.poc.ConnectionState
import dev.agentpush.poc.ui.theme.HomingPalette

val CodexGreen = Color(0xFF4ADE80)
val ClaudeOrange = Color(0xFFFB923C)
val WorkBuddyBlue = Color(0xFF60A5FA)

fun AgentBrand.accentColor(): Color {
    return when (this) {
        AgentBrand.CODEX -> CodexGreen
        AgentBrand.CLAUDE -> ClaudeOrange
        AgentBrand.WORKBUDDY -> WorkBuddyBlue
        AgentBrand.UNKNOWN -> HomingPalette.Muted
    }
}

@Composable
fun AgentAvatar(
    brand: AgentBrand,
    size: Dp = 46.dp,
    modifier: Modifier = Modifier,
) {
    val accent = brand.accentColor()
    Box(
        modifier = modifier
            .size(size)
            .clip(CircleShape)
            .background(accent.copy(alpha = 0.16f)),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = brand.initial,
            color = accent,
            fontSize = (size.value * 0.38f).sp,
            fontWeight = FontWeight.Bold,
        )
    }
}

@Composable
fun ConnectionPill(
    connection: ConnectionState,
    server: String,
    modifier: Modifier = Modifier,
) {
    val (label, color) = when (connection) {
        ConnectionState.CONNECTED -> "已连接" to CodexGreen
        ConnectionState.RECONNECTING -> "重连中" to ClaudeOrange
        ConnectionState.DISCONNECTED -> "未连接" to MaterialTheme.colorScheme.error
    }
    val host = serverHostLabel(server)
    Surface(
        modifier = modifier,
        color = color.copy(alpha = 0.12f),
        shape = RoundedCornerShape(50),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 10.dp, vertical = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Box(
                modifier = Modifier
                    .size(7.dp)
                    .clip(CircleShape)
                    .background(color),
            )
            Text(
                text = "$label · $host",
                color = MaterialTheme.colorScheme.onSurface,
                style = MaterialTheme.typography.labelMedium,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@Composable
fun PendingTag(text: String, modifier: Modifier = Modifier) {
    Surface(
        modifier = modifier,
        color = ClaudeOrange.copy(alpha = 0.16f),
        shape = RoundedCornerShape(7.dp),
    ) {
        Text(
            text = text,
            modifier = Modifier.padding(horizontal = 7.dp, vertical = 3.dp),
            color = ClaudeOrange,
            style = MaterialTheme.typography.labelMedium,
            fontWeight = FontWeight.Bold,
        )
    }
}

@Composable
fun UnreadBadge(count: Int, modifier: Modifier = Modifier) {
    if (count <= 0) {
        return
    }
    Surface(
        modifier = modifier,
        color = MaterialTheme.colorScheme.primary,
        shape = CircleShape,
    ) {
        Box(
            modifier = Modifier.size(if (count > 9) 24.dp else 20.dp),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                text = if (count > 99) "99+" else count.toString(),
                color = MaterialTheme.colorScheme.onPrimary,
                fontSize = if (count > 9) 10.sp else 11.sp,
                fontWeight = FontWeight.Bold,
            )
        }
    }
}

@Composable
fun OfflineBanner(
    connection: ConnectionState,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
    reason: String? = null,
) {
    if (connection == ConnectionState.CONNECTED) {
        return
    }
    Surface(
        onClick = onRetry,
        modifier = modifier.fillMaxWidth(),
        color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.72f),
        shape = RoundedCornerShape(16.dp),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 14.dp, vertical = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Icon(
                imageVector = Icons.Outlined.CloudOff,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(20.dp),
            )
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = if (connection == ConnectionState.RECONNECTING) {
                        "正在重新连接 Mac"
                    } else {
                        "暂时无法连接 Mac"
                    },
                    style = MaterialTheme.typography.labelLarge,
                )
                Text(
                    text = "已有内容仍可查看，连接恢复后会自动同步。",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                if (!reason.isNullOrBlank()) {
                    Text(
                        text = "最近断开：$reason",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
            Text(
                text = "重试",
                modifier = Modifier.padding(6.dp),
                color = MaterialTheme.colorScheme.primary,
                style = MaterialTheme.typography.labelLarge,
            )
        }
    }
}

@Composable
fun SectionCard(
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    Surface(
        modifier = modifier.fillMaxWidth(),
        color = MaterialTheme.colorScheme.surface,
        shape = RoundedCornerShape(12.dp),
        tonalElevation = 1.dp,
    ) {
        Column(modifier = Modifier.padding(18.dp)) {
            content()
        }
    }
}

@Composable
fun HomingCard(
    modifier: Modifier = Modifier,
    borderColor: Color = MaterialTheme.colorScheme.outlineVariant,
    content: @Composable () -> Unit,
) {
    Surface(
        modifier = modifier.fillMaxWidth(),
        color = HomingPalette.Card,
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(1.dp, borderColor),
        tonalElevation = 0.dp,
        content = content,
    )
}

@Composable
fun MacStatusPill(
    connection: ConnectionState,
    modifier: Modifier = Modifier,
) {
    val dotColor = when (connection) {
        ConnectionState.CONNECTED -> HomingPalette.Brand
        ConnectionState.RECONNECTING -> HomingPalette.Warning
        ConnectionState.DISCONNECTED -> HomingPalette.Error
    }
    Surface(
        modifier = modifier,
        color = HomingPalette.Raised.copy(alpha = 0.72f),
        shape = RoundedCornerShape(50),
        border = BorderStroke(1.dp, HomingPalette.Border.copy(alpha = 0.55f)),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 10.dp, vertical = 7.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Box(
                modifier = Modifier
                    .size(7.dp)
                    .clip(CircleShape)
                    .background(dotColor),
            )
            Text(
                text = "我的 Mac",
                style = MaterialTheme.typography.labelMedium,
                color = HomingPalette.Body,
                maxLines = 1,
            )
        }
    }
}

fun serverHostLabel(server: String): String {
    return runCatching {
        val uri = java.net.URI(server)
        val host = uri.host ?: server.removePrefix("http://").removePrefix("https://")
        if (uri.port > 0) "$host:${uri.port}" else host
    }.getOrDefault(server.removePrefix("http://").removePrefix("https://"))
}

@Composable
fun VerticalGap(height: Dp) {
    Spacer(modifier = Modifier.height(height))
}
