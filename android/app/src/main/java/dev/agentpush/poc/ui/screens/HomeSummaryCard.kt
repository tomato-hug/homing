package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowForward
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.agentpush.poc.ConnectionState
import dev.agentpush.poc.TransportKind
import dev.agentpush.poc.ui.theme.HomingPalette

/**
 * 首页概览卡：把以前挤在顶栏里的连接胶囊、单独一行的大标题和副标题合成一张卡，
 * 下面三个等宽的数字块（待处理 / 运行中 / 最近会话）都是 64dp 高的整块点击区。
 */
@Composable
internal fun HomeSummaryCard(
    connection: ConnectionState,
    transportKind: TransportKind,
    computerLabel: String,
    headline: String,
    subtitle: String,
    attentionCount: Int,
    runningCount: Int,
    recentCount: Int,
    onAttention: () -> Unit,
    onOpenAll: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val (statusText, statusColor) = when (connection) {
        ConnectionState.CONNECTED -> "已连接" to HomingPalette.Brand
        ConnectionState.RECONNECTING -> "正在重连" to HomingPalette.Warning
        ConnectionState.DISCONNECTED -> "离线" to HomingPalette.Error
    }
    Surface(
        modifier = modifier
            .fillMaxWidth()
            .testTag("home-summary"),
        color = HomingPalette.Card,
        shape = RoundedCornerShape(18.dp),
        border = BorderStroke(1.dp, HomingPalette.Border.copy(alpha = 0.6f)),
    ) {
        Column(modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 14.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Box(
                    modifier = Modifier
                        .size(8.dp)
                        .clip(CircleShape)
                        .background(statusColor),
                )
                Text(
                    text = "$computerLabel · ${transportKind.label}",
                    modifier = Modifier
                        .weight(1f)
                        .padding(start = 8.dp),
                    style = MaterialTheme.typography.bodyMedium.copy(fontSize = 14.sp),
                    color = HomingPalette.Body,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = statusText,
                    style = MaterialTheme.typography.labelLarge.copy(fontWeight = FontWeight.Medium),
                    color = statusColor,
                    maxLines = 1,
                )
            }
            Text(
                text = headline,
                modifier = Modifier.padding(top = 12.dp),
                style = MaterialTheme.typography.displaySmall.copy(
                    fontSize = 28.sp,
                    lineHeight = 34.sp,
                    fontWeight = FontWeight.SemiBold,
                ),
                color = HomingPalette.Text,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                text = subtitle,
                modifier = Modifier.padding(top = 2.dp),
                style = MaterialTheme.typography.bodyMedium.copy(fontSize = 14.sp),
                color = HomingPalette.Muted,
                maxLines = 2,
            )
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = 14.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                SummaryTile(
                    label = "待处理",
                    value = attentionCount,
                    accent = if (attentionCount > 0) HomingPalette.Warning else HomingPalette.Text,
                    onClick = onAttention,
                    tag = "summary-attention",
                    modifier = Modifier.weight(1f),
                )
                SummaryTile(
                    label = "运行中",
                    value = runningCount,
                    accent = if (runningCount > 0) HomingPalette.Brand else HomingPalette.Text,
                    onClick = onOpenAll,
                    tag = "summary-running",
                    modifier = Modifier.weight(1f),
                )
                SummaryTile(
                    label = "全部会话",
                    value = recentCount,
                    accent = HomingPalette.Text,
                    onClick = onOpenAll,
                    tag = "summary-all",
                    showArrow = true,
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

@Composable
private fun SummaryTile(
    label: String,
    value: Int,
    accent: Color,
    onClick: () -> Unit,
    tag: String,
    modifier: Modifier = Modifier,
    showArrow: Boolean = false,
) {
    Surface(
        onClick = onClick,
        modifier = modifier
            .heightIn(min = 64.dp)
            .testTag(tag),
        color = HomingPalette.Raised.copy(alpha = 0.7f),
        shape = RoundedCornerShape(12.dp),
    ) {
        Column(
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 10.dp),
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = value.toString(),
                    modifier = Modifier.weight(1f),
                    style = MaterialTheme.typography.titleLarge.copy(
                        fontSize = 22.sp,
                        lineHeight = 26.sp,
                        fontWeight = FontWeight.SemiBold,
                    ),
                    color = accent,
                    maxLines = 1,
                )
                if (showArrow) {
                    Icon(
                        Icons.AutoMirrored.Outlined.ArrowForward,
                        contentDescription = null,
                        tint = HomingPalette.Muted,
                        modifier = Modifier.size(18.dp),
                    )
                }
            }
            Text(
                text = label,
                style = MaterialTheme.typography.labelMedium.copy(fontSize = 12.5.sp),
                color = HomingPalette.Muted,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}
