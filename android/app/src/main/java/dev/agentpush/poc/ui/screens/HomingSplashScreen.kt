package dev.agentpush.poc.ui.screens

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.agentpush.poc.ConnectionState
import dev.agentpush.poc.ui.HomingPigeon
import dev.agentpush.poc.ui.PigeonState
import dev.agentpush.poc.ui.isReducedMotion
import dev.agentpush.poc.ui.theme.HomingMono
import dev.agentpush.poc.ui.theme.HomingPalette
import kotlinx.coroutines.delay

@Composable
fun HomingSplashScreen(
    connection: ConnectionState,
    onFinished: () -> Unit,
) {
    val context = LocalContext.current
    val reducedMotion = remember(context) { isReducedMotion(context) }
    val liveConnection by rememberUpdatedState(connection)
    var titleVisible by remember { mutableStateOf(reducedMotion) }
    var subtitleVisible by remember { mutableStateOf(reducedMotion) }
    val titleAlpha by animateFloatAsState(
        targetValue = if (titleVisible) 1f else 0f,
        animationSpec = tween(420),
        label = "splashTitle",
    )
    val subtitleAlpha by animateFloatAsState(
        targetValue = if (subtitleVisible) 1f else 0f,
        animationSpec = tween(420),
        label = "splashSubtitle",
    )

    LaunchedEffect(Unit) {
        if (!reducedMotion) {
            delay(1_100)
            titleVisible = true
            delay(200)
            subtitleVisible = true
        }
        val startedAt = System.currentTimeMillis()
        delay(2_200)
        while (
            liveConnection != ConnectionState.CONNECTED &&
            System.currentTimeMillis() - startedAt < 6_000
        ) {
            delay(200)
        }
        onFinished()
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .statusBarsPadding()
            .navigationBarsPadding()
            .padding(horizontal = 28.dp, vertical = 34.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Spacer(Modifier.weight(1f))
        HomingPigeon(
            state = PigeonState.ArriveThenIdle,
            size = 148.dp,
            showTile = false,
            contentDescription = "Homing 鸽子",
        )
        Spacer(Modifier.weight(0.34f))
        Column(
            modifier = Modifier.alpha(titleAlpha),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            androidx.compose.material3.Text(
                text = "Homing",
                color = HomingPalette.Text,
                fontSize = 44.sp,
                lineHeight = 48.sp,
                fontWeight = FontWeight.SemiBold,
                letterSpacing = (-0.8).sp,
            )
            androidx.compose.material3.Text(
                text = "每个 agent 的回复，都能飞回你手里。",
                modifier = Modifier
                    .padding(top = 10.dp)
                    .alpha(subtitleAlpha),
                color = HomingPalette.Muted,
                fontSize = 15.sp,
                lineHeight = 23.sp,
                textAlign = TextAlign.Center,
            )
        }
        Spacer(Modifier.weight(0.7f))
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            HomingPigeon(
                state = PigeonState.Flap,
                size = 26.dp,
                showTile = false,
                contentDescription = null,
            )
            Column {
                androidx.compose.material3.Text(
                    text = "正在连接 我的 Mac…",
                    color = HomingPalette.Body,
                    fontSize = 13.sp,
                    lineHeight = 18.sp,
                )
                androidx.compose.material3.Text(
                    text = "局域网 · 中继端口 18787",
                    color = HomingPalette.Muted,
                    fontFamily = HomingMono,
                    fontSize = 10.5.sp,
                    lineHeight = 15.sp,
                )
            }
        }
    }
}
