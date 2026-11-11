package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Inbox
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.agentpush.poc.MobileInboxMode
import dev.agentpush.poc.ui.theme.HomingPalette
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

@Composable
internal fun MobileInboxModeCard(
    mode: MobileInboxMode,
    residentCount: Int = mode.residentSessionIds.size,
    onToggle: (Boolean) -> Unit,
    modifier: Modifier = Modifier,
) {
    val enabled = mode.enabled
    Surface(
        modifier = modifier
            .fillMaxWidth()
            .testTag("mobile-inbox-mode"),
        color = if (enabled) {
            HomingPalette.Brand.copy(alpha = 0.08f)
        } else {
            HomingPalette.Card
        },
        shape = RoundedCornerShape(13.dp),
        border = BorderStroke(
            1.dp,
            if (enabled) HomingPalette.Brand.copy(alpha = 0.34f) else HomingPalette.NavBorder,
        ),
    ) {
        Row(
            modifier = Modifier.padding(start = 13.dp, end = 9.dp, top = 11.dp, bottom = 11.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(11.dp),
        ) {
            Box(
                modifier = Modifier
                    .size(34.dp)
                    .clip(CircleShape)
                    .background(
                        if (enabled) {
                            HomingPalette.Brand.copy(alpha = 0.16f)
                        } else {
                            HomingPalette.Raised
                        },
                    ),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = if (enabled) {
                        Icons.Outlined.NotificationsActive
                    } else {
                        Icons.Outlined.Inbox
                    },
                    contentDescription = null,
                    modifier = Modifier.size(19.dp),
                    tint = if (enabled) HomingPalette.Brand else HomingPalette.Muted,
                )
            }
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = if (enabled) "收件箱已开启" else "收件箱是空的",
                    style = MaterialTheme.typography.bodyMedium.copy(
                        fontSize = 14.sp,
                        fontWeight = FontWeight.Medium,
                    ),
                    color = HomingPalette.Text,
                )
                Text(
                    text = if (enabled) {
                        buildString {
                            append(formatInboxStart(mode.sinceMillis))
                            append(" 起只收新动态")
                            if (residentCount > 0) append(" · $residentCount 个常驻")
                        }
                    } else {
                        "开启后，新回复和授权才会进入手机"
                    },
                    modifier = Modifier.padding(top = 2.dp),
                    style = MaterialTheme.typography.labelSmall.copy(fontSize = 11.sp),
                    color = HomingPalette.Muted,
                    maxLines = 2,
                )
            }
            if (enabled) {
                TextButton(
                    onClick = { onToggle(false) },
                    modifier = Modifier.testTag("toggle-mobile-inbox"),
                ) {
                    Text("关闭", color = HomingPalette.Muted)
                }
            } else {
                Button(
                    onClick = { onToggle(true) },
                    modifier = Modifier.testTag("toggle-mobile-inbox"),
                    shape = RoundedCornerShape(9.dp),
                    colors = ButtonDefaults.buttonColors(
                        containerColor = HomingPalette.Brand,
                        contentColor = HomingPalette.Background,
                    ),
                    contentPadding = androidx.compose.foundation.layout.PaddingValues(
                        horizontal = 13.dp,
                        vertical = 8.dp,
                    ),
                ) {
                    Text("开启", fontSize = 12.sp, fontWeight = FontWeight.SemiBold)
                }
            }
        }
    }
}

internal fun formatInboxStart(millis: Long): String {
    if (millis <= 0L) return "刚刚"
    return Instant.ofEpochMilli(millis)
        .atZone(ZoneId.systemDefault())
        .format(DateTimeFormatter.ofPattern("HH:mm"))
}
