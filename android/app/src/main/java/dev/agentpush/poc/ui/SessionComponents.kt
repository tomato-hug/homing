package dev.agentpush.poc.ui

import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.CallSplit
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.HourglassTop
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.Shield
import androidx.compose.material.icons.outlined.SmartToy
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.agentpush.poc.AgentBrand
import dev.agentpush.poc.SessionStatus
import dev.agentpush.poc.SessionSummary
import dev.agentpush.poc.SessionTagKind
import dev.agentpush.poc.SessionTagValue
import dev.agentpush.poc.sessionTags
import dev.agentpush.poc.ui.theme.HomingMono

@Composable
fun AgentGlyph(
    brand: AgentBrand,
    size: Dp = 20.dp,
    modifier: Modifier = Modifier,
    tint: Color = MaterialTheme.colorScheme.onSurface,
) {
    val icon = when (brand) {
        AgentBrand.CODEX -> Icons.Outlined.Terminal
        AgentBrand.CLAUDE -> Icons.Outlined.AutoAwesome
        AgentBrand.WORKBUDDY -> Icons.Outlined.SmartToy
        AgentBrand.UNKNOWN -> Icons.Outlined.Person
    }
    Icon(
        imageVector = icon,
        contentDescription = brand.displayName,
        modifier = modifier.size(size),
        tint = tint,
    )
}

@Composable
fun SessionTagStrip(
    session: SessionSummary,
    modifier: Modifier = Modifier,
    maxVisible: Int = 3,
) {
    val tags = sessionTags(session)
    if (tags.isEmpty()) return
    var expanded by remember(session.sessionId) { mutableStateOf(false) }
    val textStyle = MaterialTheme.typography.labelMedium.copy(fontSize = 12.sp)
    val textMeasurer = rememberTextMeasurer()
    val density = LocalDensity.current
    val gapPx = with(density) { 5.dp.roundToPx() }
    val maxTagPx = with(density) { 126.dp.roundToPx() }
    val iconPx = with(density) { 15.dp.roundToPx() }
    val horizontalPaddingPx = with(density) { 12.dp.roundToPx() }

    BoxWithConstraints(modifier = modifier) {
        val availablePx = with(density) { maxWidth.roundToPx() }
        val overflowCandidates = tags.drop(maxVisible)
        var reservePx = 0
        if (overflowCandidates.isNotEmpty()) {
            reservePx = textMeasurer.measure(
                AnnotatedString("+${overflowCandidates.size}"),
                textStyle,
            ).size.width + horizontalPaddingPx + gapPx
        }

        val visible = mutableListOf<SessionTagValue>()
        var usedPx = reservePx
        for (tag in tags) {
            if (visible.size >= maxVisible) break
            val textWidth = textMeasurer.measure(
                AnnotatedString(tag.text),
                textStyle,
            ).size.width.coerceAtMost(maxTagPx)
            val extra = if (tag.kind == SessionTagKind.BRANCH) iconPx else 0
            val nextWidth = textWidth + horizontalPaddingPx + extra + gapPx
            if (visible.isNotEmpty() && usedPx + nextWidth > availablePx) break
            visible += tag
            usedPx += nextWidth
        }

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(5.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            visible.forEach { tag ->
                SessionTagChip(tag)
            }
            if (visible.size < tags.size) {
                Box {
                    Surface(
                        onClick = { expanded = true },
                        color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f),
                        shape = RoundedCornerShape(5.dp),
                    ) {
                        Text(
                            text = "+${tags.size - visible.size}",
                            modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp),
                            style = textStyle,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    DropdownMenu(
                        expanded = expanded,
                        onDismissRequest = { expanded = false },
                    ) {
                        tags.drop(visible.size).forEach { tag ->
                            DropdownMenuItem(
                                text = {
                                    Text(
                                        text = when (tag.kind) {
                                            SessionTagKind.WORKSPACE -> "工作区 · ${tag.text}"
                                            SessionTagKind.PROJECT -> "项目 · ${tag.text}"
                                            SessionTagKind.BRANCH -> "分支 · ${tag.text}"
                                            SessionTagKind.MODEL -> "模型 · ${tag.text}"
                                        },
                                        maxLines = 2,
                                    )
                                },
                                onClick = { expanded = false },
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun SessionTagChip(tag: SessionTagValue) {
    val container: Color
    val content: Color
    val border: BorderStroke?
    when (tag.kind) {
        SessionTagKind.PROJECT -> {
            container = MaterialTheme.colorScheme.primaryContainer
            content = MaterialTheme.colorScheme.onPrimaryContainer
            border = null
        }
        SessionTagKind.BRANCH -> {
            container = Color.Transparent
            content = MaterialTheme.colorScheme.onSurfaceVariant
            border = BorderStroke(1.dp, MaterialTheme.colorScheme.outline)
        }
        else -> {
            container = Color.Transparent
            content = MaterialTheme.colorScheme.onSurfaceVariant
            border = null
        }
    }
    Surface(
        color = container,
        contentColor = content,
        shape = RoundedCornerShape(5.dp),
        border = border,
    ) {
        Row(
            modifier = Modifier.padding(
                horizontal = if (tag.kind == SessionTagKind.MODEL) 0.dp else 6.dp,
                vertical = 2.dp,
            ),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(3.dp),
        ) {
            if (tag.kind == SessionTagKind.BRANCH) {
                Icon(
                    imageVector = Icons.Outlined.CallSplit,
                    contentDescription = null,
                    modifier = Modifier.size(12.dp),
                    tint = content,
                )
            }
            Text(
                text = tag.text,
                modifier = Modifier.widthIn(max = 118.dp),
                style = MaterialTheme.typography.labelMedium.copy(
                    fontSize = 12.sp,
                    fontFamily = if (tag.kind == SessionTagKind.BRANCH) {
                        HomingMono
                    } else {
                        FontFamily.Default
                    },
                ),
                color = content,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                softWrap = false,
            )
        }
    }
}

@Composable
fun SessionStatusBadge(
    status: SessionStatus,
    modifier: Modifier = Modifier,
    showText: Boolean = true,
) {
    val color = statusDisplayColor(status)
    val label = status.label
    Row(
        modifier = modifier,
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        when (status) {
            SessionStatus.RUNNING -> RunningDot(color)
            SessionStatus.WAITING_INPUT -> Icon(
                Icons.Outlined.HourglassTop,
                contentDescription = label,
                tint = color,
                modifier = Modifier.size(14.dp),
            )
            SessionStatus.NEEDS_PERMISSION -> Icon(
                Icons.Outlined.Shield,
                contentDescription = label,
                tint = color,
                modifier = Modifier.size(14.dp),
            )
            SessionStatus.ERROR -> Icon(
                Icons.Outlined.Close,
                contentDescription = label,
                tint = color,
                modifier = Modifier.size(14.dp),
            )
            SessionStatus.COMPLETED -> Icon(
                Icons.Outlined.Check,
                contentDescription = label,
                tint = color,
                modifier = Modifier.size(14.dp),
            )
            SessionStatus.IDLE -> Box(
                modifier = Modifier
                    .size(6.dp)
                    .background(color.copy(alpha = 0.7f), CircleShape),
            )
        }
        if (showText) {
            Text(
                text = label,
                style = MaterialTheme.typography.labelSmall,
                color = color,
                maxLines = 1,
            )
        }
    }
}

@Composable
private fun RunningDot(color: Color) {
    val transition = rememberInfiniteTransition(label = "running")
    val alpha by transition.animateFloat(
        initialValue = 0.35f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(
            animation = tween(850),
            repeatMode = RepeatMode.Reverse,
        ),
        label = "runningAlpha",
    )
    Box(
        modifier = Modifier
            .size(7.dp)
            .alpha(alpha)
            .background(color, CircleShape),
    )
}

@Composable
fun statusDisplayColor(status: SessionStatus): Color {
    val dark = MaterialTheme.colorScheme.background.luminance() < 0.5f
    return when (status) {
        SessionStatus.RUNNING -> if (dark) Color(0xFF6FD99B) else Color(0xFF189A58)
        SessionStatus.WAITING_INPUT,
        SessionStatus.NEEDS_PERMISSION,
        -> if (dark) Color(0xFFF2B544) else Color(0xFFB45309)
        SessionStatus.ERROR -> MaterialTheme.colorScheme.error
        SessionStatus.COMPLETED -> MaterialTheme.colorScheme.secondary
        SessionStatus.IDLE -> MaterialTheme.colorScheme.onSurfaceVariant
    }
}
