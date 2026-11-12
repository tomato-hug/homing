package dev.agentpush.poc.ui

import android.content.Context
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.graphics.drawscope.withTransform
import androidx.compose.ui.graphics.vector.PathParser
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.Dp
import dev.agentpush.poc.R
import kotlinx.coroutines.isActive
import org.json.JSONObject
import kotlin.math.abs

/**
 * 灰鸽 v2 的八个场景（res/raw/pigeon_v2.json，与控制台 console/src/lib/pigeon_v2.json 同一份）。
 *
 * 触发约定（docs/homing-v2/pigeon_v2.json 的 trigger 字段）：
 * - RECV 收信：收到新的待处理动态；启动页的入场
 * - SEND 寄出：回复 / 审批被电脑确认收到
 * - TAP 轻点：点品牌标、下拉刷新松手
 * - WAKE 唤醒：重新连上电脑
 * - WAIT 等你处理：有待审批或在等回复时循环
 * - SLEEP 离线：和电脑断开时循环
 * - PAIR 配对成功：添加电脑完成
 * - ERROR 出错：发送失败、连接被拒
 */
enum class PigeonScene(val key: String) {
    RECV("recv"),
    SEND("send"),
    TAP("tap"),
    WAKE("wake"),
    WAIT("wait"),
    SLEEP("sleep"),
    PAIR("pair"),
    ERROR("error"),
}

/** 裁切：BIRD 只画 120×120 的鸽子图标框；STAGE 画整个 240×130 舞台（飞入飞出看得见）。 */
enum class PigeonCrop { BIRD, STAGE }

enum class PigeonNeck(val color: Color) {
    NORMAL(Color(0xFF2DD4BF)),
    WAITING(Color(0xFFFBBF24)),
    ERROR(Color(0xFFF87171)),
    OFFLINE(Color(0xFF64748B)),
}

private data class V2Key(
    val t: Float,
    val tx: Float?,
    val ty: Float?,
    val rot: Float?,
    val sx: Float?,
    val sy: Float?,
    val opacity: Float?,
    val fill: Color?,
)

private data class V2Track(val easing: FloatArray, val keys: List<V2Key>)

private data class V2Scene(
    val period: Float,
    /** 一次性播放的时长；之后回到静止（或 hold 时停在最后一帧） */
    val play: Float,
    val visibleExtras: Set<String>,
    val hidden: Set<String>,
    val tracks: Map<String, V2Track>,
)

internal data class V2Frame(
    val tx: Float = 0f,
    val ty: Float = 0f,
    val rot: Float = 0f,
    val sx: Float = 1f,
    val sy: Float = 1f,
    val opacity: Float? = null,
    val fill: Color? = null,
)

private object PigeonV2Store {
    @Volatile
    private var cached: Map<String, V2Scene>? = null

    fun scenes(context: Context): Map<String, V2Scene> {
        cached?.let { return it }
        return synchronized(this) {
            cached ?: parse(context.applicationContext).also { cached = it }
        }
    }

    private fun parse(context: Context): Map<String, V2Scene> {
        val text = context.resources.openRawResource(R.raw.pigeon_v2)
            .bufferedReader(Charsets.UTF_8)
            .use { it.readText() }
        val scenes = JSONObject(text).getJSONObject("scenes")
        val out = linkedMapOf<String, V2Scene>()
        for (name in scenes.keys()) {
            val scene = scenes.getJSONObject(name)
            val tracksJson = scene.getJSONObject("tracks")
            val tracks = linkedMapOf<String, V2Track>()
            for (part in tracksJson.keys()) {
                val track = tracksJson.getJSONObject(part)
                val easingJson = track.optJSONArray("easing")
                val easing = FloatArray(4) { index ->
                    easingJson?.optDouble(index, if (index < 2) 0.0 else 1.0)?.toFloat()
                        ?: if (index < 2) 0f else 1f
                }
                val keysJson = track.getJSONArray("keys")
                val keys = (0 until keysJson.length()).map { index ->
                    val key = keysJson.getJSONObject(index)
                    fun num(field: String): Float? =
                        if (key.has(field)) key.optDouble(field).toFloat() else null
                    V2Key(
                        t = key.optDouble("t", 0.0).toFloat(),
                        tx = num("tx"),
                        ty = num("ty"),
                        rot = num("rot"),
                        sx = num("sx"),
                        sy = num("sy"),
                        opacity = num("opacity"),
                        fill = key.optString("fill").takeIf { it.isNotBlank() }?.let(::parseColor),
                    )
                }
                tracks[part] = V2Track(easing, keys)
            }
            out[name] = V2Scene(
                period = scene.optDouble("loop_period", 2.0).toFloat(),
                play = scene.optDouble("play", scene.optDouble("loop_period", 2.0)).toFloat(),
                visibleExtras = scene.optJSONArray("visible_extras").toStringSet(),
                hidden = scene.optJSONArray("hidden").toStringSet(),
                tracks = tracks,
            )
        }
        return out
    }

    private fun org.json.JSONArray?.toStringSet(): Set<String> {
        if (this == null) return emptySet()
        return (0 until length()).map { optString(it) }.filter { it.isNotBlank() }.toSet()
    }

    private fun parseColor(value: String): Color = Color(android.graphics.Color.parseColor(value))
}

internal fun cubicBezier(x1: Float, y1: Float, x2: Float, y2: Float, x: Float): Float {
    if (x <= 0f) return 0f
    if (x >= 1f) return 1f
    var t = x
    repeat(6) {
        val inv = 1f - t
        val cx = 3f * x1 * t * inv * inv + 3f * x2 * t * t * inv + t * t * t - x
        val dx = 3f * x1 * inv * inv + 6f * (x2 - x1) * t * inv + 3f * (1f - x2) * t * t
        if (abs(dx) < 1e-6f) return@repeat
        t = (t - cx / dx).coerceIn(0f, 1f)
    }
    val inv = 1f - t
    return 3f * y1 * t * inv * inv + 3f * y2 * t * t * inv + t * t * t
}

private fun V2Track.sample(time: Float): V2Frame {
    if (keys.isEmpty()) return V2Frame()
    fun at(key: V2Key) = V2Frame(
        tx = key.tx ?: 0f,
        ty = key.ty ?: 0f,
        rot = key.rot ?: 0f,
        sx = key.sx ?: 1f,
        sy = key.sy ?: 1f,
        opacity = key.opacity,
        fill = key.fill,
    )
    if (time <= keys.first().t) return at(keys.first())
    if (time >= keys.last().t) return at(keys.last())
    for (index in 0 until keys.lastIndex) {
        val a = keys[index]
        val b = keys[index + 1]
        if (time < a.t || time > b.t) continue
        val span = (b.t - a.t).coerceAtLeast(0.0001f)
        val f = cubicBezier(easing[0], easing[1], easing[2], easing[3], (time - a.t) / span)
        fun mix(p: Float?, q: Float?, fallback: Float): Float {
            val from = p ?: fallback
            val to = q ?: fallback
            return from + (to - from) * f
        }
        val opacity = if (a.opacity == null && b.opacity == null) null else mix(a.opacity, b.opacity, 1f)
        val fill = when {
            a.fill != null && b.fill != null -> Color(
                red = a.fill.red + (b.fill.red - a.fill.red) * f,
                green = a.fill.green + (b.fill.green - a.fill.green) * f,
                blue = a.fill.blue + (b.fill.blue - a.fill.blue) * f,
            )
            else -> a.fill ?: b.fill
        }
        return V2Frame(
            tx = mix(a.tx, b.tx, 0f),
            ty = mix(a.ty, b.ty, 0f),
            rot = mix(a.rot, b.rot, 0f),
            sx = mix(a.sx, b.sx, 1f),
            sy = mix(a.sy, b.sy, 1f),
            opacity = opacity,
            fill = fill,
        )
    }
    return at(keys.last())
}

// ---- 几何（与控制台 pigeon.ts 一致） ----------------------------------------

private val BODY = "M62 48 C56 56 44 60 30 66 L12 74 L31 79 C40 89 58 94 74 88 C92 80 96 60 86 50 C82 44 70 42 62 48 Z"
private val HEAD = "M66 38 C65 28 77 24 84 29 C86 31 87 34 87 37 L96 41 L88 43 C86 47 80 50 73 50 C68 49 66 44 66 38 Z"
private val NECK = "M63 44 C70 50 80 51 86 49 L87 55 C79 57 68 55 61 50 Z"
private val WING = "M36 71 C50 61 66 61 76 68 C64 80 48 82 36 71 Z"

private object PigeonPaths {
    val body: Path = PathParser().parsePathString(BODY).toPath()
    val head: Path = PathParser().parsePathString(HEAD).toPath()
    val neck: Path = PathParser().parsePathString(NECK).toPath()
    val wing: Path = PathParser().parsePathString(WING).toPath()
}

private val PIVOT = mapOf(
    "b-bird" to Offset(55f, 60f),
    "b-squash" to Offset(58f, 97f),
    "b-head" to Offset(72f, 46f),
    "b-eye" to Offset(78f, 34f),
    "b-wing" to Offset(72f, 68f),
    "b-legs" to Offset(60f, 86f),
    "b-neck" to Offset(74f, 50f),
    "b-beakletter" to Offset(99f, 43f),
    "x-letter" to Offset(156f, 61f),
    "x-badge" to Offset(202f, 28f),
    "x-dot" to Offset(176f, 34f),
    "x-bang" to Offset(168f, 30f),
    "x-z1" to Offset(150f, 48f),
    "x-z2" to Offset(160f, 38f),
    "x-wind" to Offset(44f, 62f),
)

private val EXTRAS = setOf(
    "b-legs", "b-beakletter", "b-eyes", "x-ground", "x-letter", "x-badge", "x-check",
    "x-ring", "x-tile", "x-dot", "x-z1", "x-z2", "x-bang", "x-wind", "x-pc", "x-phone", "x-arc",
)

private val Slate = Color(0xFF64748B)
private val SlateLight = Color(0xFF94A3B8)
private val Ink = Color(0xFF0F172A)
private val Paper = Color(0xFFF8FAFC)
private val Teal = Color(0xFF2DD4BF)
private val TealInk = Color(0xFF042F2E)
private val Amber = Color(0xFFFBBF24)
private val Red = Color(0xFFF87171)
private val Leg = Color(0xFFF59E0B)

@Composable
fun HomingPigeonScene(
    scene: PigeonScene?,
    size: Dp,
    modifier: Modifier = Modifier,
    loop: Boolean = false,
    hold: Boolean = false,
    crop: PigeonCrop = PigeonCrop.BIRD,
    neck: PigeonNeck = PigeonNeck.NORMAL,
    bodyColor: Color = Color(0xFFE2E8F0),
    playKey: Any? = Unit,
    contentDescription: String? = null,
    onFinished: (() -> Unit)? = null,
) {
    val context = LocalContext.current
    val scenes = remember(context) { PigeonV2Store.scenes(context) }
    val reduced = remember(context) { isReducedMotion(context) }
    val spec = scene?.let { scenes[it.key] }
    var time by remember(scene, playKey) { mutableFloatStateOf(0f) }
    var finished by remember(scene, playKey) { mutableStateOf(scene == null) }
    val latestFinished by rememberUpdatedState(onFinished)

    LaunchedEffect(scene, playKey, loop, hold) {
        time = 0f
        finished = spec == null
        if (spec == null) return@LaunchedEffect
        if (reduced) {
            // 减少动态效果：停在场景中段的定格，一次性场景随后回到静止
            time = if (loop) 0f else spec.play
            if (!loop && !hold) {
                kotlinx.coroutines.delay(900)
                finished = true
                latestFinished?.invoke()
            }
            return@LaunchedEffect
        }
        var start = -1L
        while (isActive) {
            val frame = withFrameNanos { it }
            if (start < 0) start = frame
            val elapsed = (frame - start) / 1_000_000_000f
            if (!loop && elapsed >= spec.play) {
                if (hold) time = spec.play else finished = true
                latestFinished?.invoke()
                break
            }
            time = if (loop) elapsed % spec.period else elapsed
        }
    }

    val active = if (finished) null else spec
    val semantics = if (contentDescription == null) Modifier else Modifier.semantics {
        this.contentDescription = contentDescription
    }
    val aspect = if (crop == PigeonCrop.STAGE) 240f / 130f else 1f
    Canvas(
        modifier = modifier
            .size(width = size * aspect, height = size)
            .then(semantics),
    ) {
        val viewport = if (crop == PigeonCrop.STAGE) Rect(0f, 0f, 240f, 130f) else Rect(56f, 11f, 176f, 131f)
        val factor = this.size.height / viewport.height
        scale(factor, factor, pivot = Offset.Zero) {
            translate(-viewport.left, -viewport.top) {
                drawPigeonScene(active, time, neck, bodyColor)
            }
        }
    }
}

private fun DrawScope.drawPigeonScene(
    scene: V2Scene?,
    time: Float,
    neckState: PigeonNeck,
    bodyColor: Color,
) {
    val frames = scene?.tracks?.mapValues { it.value.sample(time) }.orEmpty()
    fun opacity(part: String): Float {
        if (scene?.hidden?.contains(part) == true) return 0f
        val base = if (part !in EXTRAS || scene?.visibleExtras?.contains(part) == true) 1f else 0f
        return (frames[part]?.opacity ?: base).coerceIn(0f, 1f)
    }
    fun part(name: String, block: DrawScope.(Float) -> Unit) {
        val alpha = opacity(name)
        if (alpha <= 0.001f) return
        val frame = frames[name]
        if (frame == null) {
            block(alpha)
            return
        }
        val pivot = PIVOT[name] ?: Offset.Zero
        withTransform({
            translate(frame.tx, frame.ty)
            rotate(frame.rot, pivot)
            scale(frame.sx, frame.sy, pivot)
        }) { block(alpha) }
    }
    val isPair = scene != null && scene.visibleExtras.contains("x-phone")

    // 背景
    part("x-tile") { a ->
        drawRoundRect(Ink.copy(alpha = a), Offset(56f, 11f), Size(120f, 120f), CornerRadius(27f))
    }
    part("x-ground") { a ->
        drawLine(Color(0xFF334155).copy(alpha = a), Offset(30f, 114f), Offset(210f, 114f), 1.5f, StrokeCap.Round)
    }
    part("x-arc") { a ->
        val arc = Path().apply {
            moveTo(34f, 64f)
            quadraticTo(120f, -24f, 208f, 58f)
        }
        drawPath(arc, Color(0xFF475569).copy(alpha = a), style = Stroke(1.4f, pathEffect = PathEffect.dashPathEffect(floatArrayOf(3f, 4f))))
    }
    part("x-pc") { a ->
        drawRoundRect(Color(0xFF111827).copy(alpha = a), Offset(12f, 74f), Size(44f, 28f), CornerRadius(3f))
        drawRoundRect(Slate.copy(alpha = a), Offset(12f, 74f), Size(44f, 28f), CornerRadius(3f), style = Stroke(1.4f))
        drawLine(Slate.copy(alpha = a), Offset(6f, 106f), Offset(62f, 106f), 2f, StrokeCap.Round)
    }
    part("x-phone") { a ->
        drawRoundRect(Color(0xFF111827).copy(alpha = a), Offset(196f, 68f), Size(24f, 44f), CornerRadius(5f))
        drawRoundRect(Slate.copy(alpha = a), Offset(196f, 68f), Size(24f, 44f), CornerRadius(5f), style = Stroke(1.4f))
    }

    // 鸟（v1 图标坐标，放在舞台的 (60,17)）
    translate(60f, 17f) {
        part("b-bird") { birdAlpha ->
            part("b-squash") { _ ->
                part("b-legs") { a ->
                    val c = Leg.copy(alpha = a * birdAlpha)
                    drawLine(c, Offset(56f, 87f), Offset(54f, 99f), 2.4f, StrokeCap.Round)
                    drawLine(c, Offset(66f, 87f), Offset(68f, 99f), 2.4f, StrokeCap.Round)
                }
                drawPath(PigeonPaths.body, bodyColor.copy(alpha = birdAlpha))
                part("b-head") { _ ->
                    drawPath(PigeonPaths.head, bodyColor.copy(alpha = birdAlpha))
                    part("b-eye") { _ ->
                        part("b-eyeo") { a ->
                            drawCircle(Ink.copy(alpha = a * birdAlpha), 2.4f, Offset(78f, 34f))
                        }
                        part("b-eyes") { a ->
                            val lid = Path().apply {
                                moveTo(75.2f, 33.6f)
                                quadraticTo(78f, 36.4f, 80.8f, 33.6f)
                            }
                            drawPath(lid, Ink.copy(alpha = a * birdAlpha), style = Stroke(1.6f, cap = StrokeCap.Round))
                        }
                    }
                    part("b-beakletter") { a ->
                        drawRoundRect(Paper.copy(alpha = a * birdAlpha), Offset(92f, 38f), Size(14f, 10f), CornerRadius(1.5f))
                        val flap = Path().apply {
                            moveTo(92.5f, 38.5f)
                            lineTo(99f, 44f)
                            lineTo(105.5f, 38.5f)
                        }
                        drawPath(flap, SlateLight.copy(alpha = a * birdAlpha), style = Stroke(0.8f))
                    }
                }
                part("b-neck") { _ ->
                    val fill = frames["b-neck"]?.fill ?: neckState.color
                    drawPath(PigeonPaths.neck, fill.copy(alpha = birdAlpha))
                }
                part("b-wing") { _ ->
                    drawPath(PigeonPaths.wing, SlateLight.copy(alpha = birdAlpha))
                }
            }
        }
    }

    // 前景
    part("x-letter") { a ->
        drawRoundRect(Paper.copy(alpha = a), Offset(148f, 56f), Size(16f, 11f), CornerRadius(1.6f))
        val flap = Path().apply {
            moveTo(148.5f, 56.5f)
            lineTo(156f, 62.5f)
            lineTo(163.5f, 56.5f)
        }
        drawPath(flap, SlateLight.copy(alpha = a), style = Stroke(0.9f))
    }
    part("x-badge") { a ->
        drawCircle(Teal.copy(alpha = a), 7f, Offset(202f, 28f))
        drawCircle(TealInk.copy(alpha = a), 2.4f, Offset(202f, 28f))
    }
    val checkCenter = if (isPair) Offset(222f, 44f) else Offset(190f, 40f)
    drawCheck(frames["x-check"], opacity("x-check"), checkCenter)
    val ringCenter = if (isPair) Offset(208f, 60f) else Offset(115f, 77f)
    val ringRadius = if (isPair) 24f else 46f
    val ringAlpha = opacity("x-ring")
    if (ringAlpha > 0.001f) {
        val frame = frames["x-ring"] ?: V2Frame()
        withTransform({
            translate(frame.tx, frame.ty)
            scale(frame.sx, frame.sy, ringCenter)
        }) {
            drawCircle(Teal.copy(alpha = ringAlpha), ringRadius, ringCenter, style = Stroke(2f))
        }
    }
    part("x-dot") { a -> drawCircle(Amber.copy(alpha = a), 5.5f, Offset(176f, 34f)) }
    part("x-z1") { a -> drawZ(Offset(146f, 44f), 7f, SlateLight.copy(alpha = a)) }
    part("x-z2") { a -> drawZ(Offset(155f, 32f), 10f, SlateLight.copy(alpha = a)) }
    part("x-bang") { a ->
        drawRoundRect(Red.copy(alpha = a), Offset(165.5f, 20f), Size(5f, 14f), CornerRadius(2.5f))
        drawCircle(Red.copy(alpha = a), 2.8f, Offset(168f, 39f))
    }
    part("x-wind") { a ->
        val c = SlateLight.copy(alpha = a)
        drawLine(c, Offset(30f, 52f), Offset(56f, 52f), 2f, StrokeCap.Round)
        drawLine(c, Offset(22f, 64f), Offset(56f, 64f), 2f, StrokeCap.Round)
        drawLine(c, Offset(32f, 76f), Offset(52f, 76f), 2f, StrokeCap.Round)
    }
}

private fun DrawScope.drawCheck(frame: V2Frame?, alpha: Float, center: Offset) {
    if (alpha <= 0.001f) return
    val f = frame ?: V2Frame()
    withTransform({
        translate(f.tx, f.ty)
        scale(f.sx, f.sy, center)
    }) {
        drawCircle(Teal.copy(alpha = alpha), 9f, center)
        val mark = Path().apply {
            moveTo(center.x - 4.5f, center.y)
            lineTo(center.x - 1.5f, center.y + 3f)
            lineTo(center.x + 4f, center.y - 3f)
        }
        drawPath(mark, TealInk.copy(alpha = alpha), style = Stroke(2.2f, cap = StrokeCap.Round, join = StrokeJoin.Round))
    }
}

private fun DrawScope.drawZ(topLeft: Offset, size: Float, color: Color) {
    val z = Path().apply {
        moveTo(topLeft.x, topLeft.y)
        lineTo(topLeft.x + size, topLeft.y)
        lineTo(topLeft.x, topLeft.y + size)
        lineTo(topLeft.x + size, topLeft.y + size)
    }
    drawPath(z, color, style = Stroke(size * 0.2f, cap = StrokeCap.Round, join = StrokeJoin.Round))
}

/** 无动画、单帧的鸽子（截图测试、减少动态效果时也走这里）。 */
@Composable
fun HomingPigeonStill(
    size: Dp,
    modifier: Modifier = Modifier,
    neck: PigeonNeck = PigeonNeck.NORMAL,
) {
    HomingPigeonScene(scene = null, size = size, modifier = modifier, neck = neck)
}
