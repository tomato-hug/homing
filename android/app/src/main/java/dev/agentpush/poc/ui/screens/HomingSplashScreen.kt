package dev.agentpush.poc.ui.screens

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBars
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
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.layout.boundsInRoot
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.agentpush.poc.ConnectionState
import dev.agentpush.poc.TransportKind
import dev.agentpush.poc.ui.HomingPigeonScene
import dev.agentpush.poc.ui.PigeonScene
import dev.agentpush.poc.ui.isReducedMotion
import dev.agentpush.poc.ui.theme.HomingMono
import dev.agentpush.poc.ui.theme.HomingPalette
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** 启动页大鸽子的尺寸，以及它最后要落到的顶栏鸽子（InboxTopBar）的尺寸和位置。 */
private val SplashPigeonSize = 148.dp
private val TopBarPigeonSize = 32.dp
// InboxTopBar：左内边距 16dp，鸽子 32dp → 中心 x = 32dp；
// 上内边距 6dp，行高由 48dp 的按钮撑开 → 中心 y = 状态栏 + 6 + 24 = 状态栏 + 30dp。
private val TopBarPigeonCenterX = 32.dp
private val TopBarPigeonCenterYBelowStatusBar = 30.dp

/**
 * 启动页。叠在主界面上方（见 AgentApp）：
 * 1. 进场：大鸽子播「归巢 → 待机」，标题和副标题依次淡入（SPEC v2 §3）；
 * 2. 等连上电脑（最少 2.2 秒，最多 6 秒）；
 * 3. 退场：大鸽子缩小并飞到收件箱顶栏鸽子的位置，文字先淡出、背景随后淡出，
 *    露出下面已经加载好的收件箱，整段约 0.65 秒。
 * 系统关闭动画时跳过进场和飞行，只做淡出。
 */
@Composable
fun HomingSplashScreen(
    connection: ConnectionState,
    transportKind: TransportKind = TransportKind.LAN,
    onFinished: () -> Unit,
) {
    val context = LocalContext.current
    val density = LocalDensity.current
    val reducedMotion = remember(context) { isReducedMotion(context) }
    val liveConnection by rememberUpdatedState(connection)
    val statusBarTop = WindowInsets.statusBars.getTop(density)

    var titleVisible by remember { mutableStateOf(reducedMotion) }
    var subtitleVisible by remember { mutableStateOf(reducedMotion) }
    var pigeonCenter by remember { mutableStateOf(Offset.Unspecified) }
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
    // 退场进度：0 = 还在启动页，1 = 鸽子已落到顶栏。
    val flight = remember { Animatable(0f) }
    val textFade = remember { Animatable(1f) }
    val backdrop = remember { Animatable(1f) }

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
        if (reducedMotion) {
            backdrop.animateTo(0f, tween(180))
        } else {
            launch { textFade.animateTo(0f, tween(180)) }
            launch {
                delay(260)
                backdrop.animateTo(0f, tween(380))
            }
            flight.animateTo(1f, tween(620, easing = FastOutSlowInEasing))
        }
        onFinished()
    }

    // 目标中心（根坐标，像素）
    val target = with(density) {
        Offset(
            TopBarPigeonCenterX.toPx(),
            statusBarTop + TopBarPigeonCenterYBelowStatusBar.toPx(),
        )
    }
    val endScale = TopBarPigeonSize / SplashPigeonSize

    Box(
        modifier = Modifier
            .fillMaxSize()
            .testTag("homing-splash")
            // 启动页盖在主界面上时，挡住下面的点击。
            .clickable(
                interactionSource = remember { MutableInteractionSource() },
                indication = null,
                onClick = {},
            ),
    ) {
        Box(
            modifier = Modifier
                .fillMaxSize()
                .alpha(backdrop.value)
                .background(HomingPalette.Background),
        )
        Column(
            modifier = Modifier
                .fillMaxSize()
                .statusBarsPadding()
                .navigationBarsPadding()
                .padding(horizontal = 28.dp, vertical = 34.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Spacer(Modifier.weight(1f))
            // pigeon_v2「收信」：衔着信从左上飞进来、刹车、落地、把信递出去，
            // 然后整只鸽子飞到收件箱顶栏。
            HomingPigeonScene(
                scene = PigeonScene.RECV,
                size = SplashPigeonSize,
                contentDescription = "Homing 鸽子",
                modifier = Modifier
                    .onGloballyPositioned { coordinates ->
                        if (flight.value == 0f) {
                            pigeonCenter = coordinates.boundsInRoot().center
                        }
                    }
                    .graphicsLayer {
                        val progress = flight.value
                        if (progress > 0f && pigeonCenter != Offset.Unspecified) {
                            translationX = (target.x - pigeonCenter.x) * progress
                            translationY = (target.y - pigeonCenter.y) * progress
                            val scale = 1f + (endScale - 1f) * progress
                            scaleX = scale
                            scaleY = scale
                        }
                        // 最后 15% 交给顶栏里的真鸽子，避免两只叠在一起。
                        alpha = if (progress > 0.85f) (1f - progress) / 0.15f else 1f
                    },
            )
            Spacer(Modifier.weight(0.34f))
            Column(
                modifier = Modifier.alpha(titleAlpha * textFade.value),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                androidx.compose.material3.Text(
                    text = "Homing",
                    color = HomingPalette.Text,
                    fontSize = 44.sp,
                    lineHeight = 48.sp,
                    fontWeight = FontWeight.SemiBold,
                    letterSpacing = (-0.8).sp,
                    maxLines = 1,
                    softWrap = false,
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
                modifier = Modifier.alpha(textFade.value),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                // 连接状态的小鸽子：连上时「唤醒」，还在连就「离线」打盹
                HomingPigeonScene(
                    scene = if (connection == ConnectionState.CONNECTED) PigeonScene.WAKE else PigeonScene.SLEEP,
                    size = 26.dp,
                    loop = connection != ConnectionState.CONNECTED,
                    contentDescription = null,
                )
                Column {
                    androidx.compose.material3.Text(
                        text = if (connection == ConnectionState.CONNECTED) {
                            "已连接 我的 Mac"
                        } else {
                            "正在连接 我的 Mac…"
                        },
                        color = HomingPalette.Body,
                        fontSize = 13.sp,
                        lineHeight = 18.sp,
                    )
                    androidx.compose.material3.Text(
                        text = "${transportKind.label} · Homing",
                        color = HomingPalette.Muted,
                        fontFamily = HomingMono,
                        fontSize = 10.5.sp,
                        lineHeight = 15.sp,
                    )
                }
            }
        }
    }
}
