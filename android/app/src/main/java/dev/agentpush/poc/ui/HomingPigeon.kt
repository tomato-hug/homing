package dev.agentpush.poc.ui

import android.content.Context
import android.provider.Settings
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.graphics.drawscope.withTransform
import androidx.compose.ui.graphics.vector.PathParser
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import dev.agentpush.poc.R
import kotlinx.coroutines.isActive
import org.json.JSONArray
import org.json.JSONObject
import kotlin.math.abs

enum class PigeonState {
    Idle,
    Flap,
    ArriveThenIdle,
    BobOnce,
    Static,
}

private data class PigeonKey(
    val at: Float,
    val tx: Float = 0f,
    val ty: Float = 0f,
    val rot: Float = 0f,
    val sx: Float = 1f,
    val sy: Float = 1f,
    val alpha: Float = 1f,
    val color: Color? = null,
)

private data class PigeonTrack(
    val layer: String,
    val period: Float,
    val delay: Float,
    val loop: Boolean,
    val keys: List<PigeonKey>,
) {
    fun valueAt(timeSeconds: Float): PigeonKey {
        if (keys.isEmpty()) return PigeonKey(0f)
        val local = (timeSeconds - delay).coerceAtLeast(0f).let {
            if (loop && period > 0f) it % period else it
        }
        if (local <= keys.first().at) return keys.first()
        if (local >= keys.last().at) return keys.last()
        for (index in 0 until keys.lastIndex) {
            val first = keys[index]
            val second = keys[index + 1]
            if (local in first.at..second.at) {
                val span = (second.at - first.at).coerceAtLeast(0.0001f)
                val fraction = ((local - first.at) / span).coerceIn(0f, 1f)
                return PigeonKey(
                    at = local,
                    tx = lerp(first.tx, second.tx, fraction),
                    ty = lerp(first.ty, second.ty, fraction),
                    rot = lerp(first.rot, second.rot, fraction),
                    sx = lerp(first.sx, second.sx, fraction),
                    sy = lerp(first.sy, second.sy, fraction),
                    alpha = lerp(first.alpha, second.alpha, fraction),
                    color = lerpColor(first.color, second.color, fraction),
                )
            }
        }
        return keys.last()
    }
}

private data class PigeonSpec(
    val body: Path,
    val head: Path,
    val neck: Path,
    val wing: Path,
    val eye: Path,
    val bodyColor: Color,
    val headColor: Color,
    val neckColor: Color,
    val wingColor: Color,
    val eyeColor: Color,
    val tileRadius: Float,
    val tileColor: Color,
    val states: Map<String, List<PigeonTrack>>,
)

private object PigeonSpecStore {
    @Volatile
    private var cached: PigeonSpec? = null

    fun get(context: Context): PigeonSpec {
        cached?.let { return it }
        return synchronized(this) {
            cached ?: parse(context.applicationContext).also { cached = it }
        }
    }

    private fun parse(context: Context): PigeonSpec {
        val text = context.resources.openRawResource(R.raw.pigeon)
            .bufferedReader(Charsets.UTF_8)
            .use { it.readText() }
        val root = JSONObject(text)
        val layers = root.getJSONObject("layers")
        val statesJson = root.getJSONObject("states")
        val states = linkedMapOf<String, List<PigeonTrack>>()
        val keys = statesJson.keys()
        while (keys.hasNext()) {
            val name = keys.next()
            val array = statesJson.optJSONArray(name) ?: continue
            states[name] = buildList {
                for (index in 0 until array.length()) {
                    val track = array.optJSONObject(index) ?: continue
                    val layer = track.optString("layer")
                    if (layer.isBlank()) continue
                    add(parseTrack(track, layer))
                }
            }
        }
        return PigeonSpec(
            body = path(layers.getJSONObject("body").getString("d")),
            head = path(layers.getJSONObject("head").getString("d")),
            neck = path(layers.getJSONObject("neck").getString("d")),
            wing = path(layers.getJSONObject("wing").getString("d")),
            eye = Path().apply {
                val ellipse = layers.getJSONObject("eye").getJSONObject("ellipse")
                val cx = ellipse.getDouble("cx").toFloat()
                val cy = ellipse.getDouble("cy").toFloat()
                val radius = ellipse.getDouble("r").toFloat()
                addOval(
                    androidx.compose.ui.geometry.Rect(
                        cx - radius,
                        cy - radius,
                        cx + radius,
                        cy + radius,
                    ),
                )
            },
            bodyColor = color(layers.getJSONObject("body").getString("fill")),
            headColor = color(layers.getJSONObject("head").getString("fill")),
            neckColor = color(layers.getJSONObject("neck").getString("fill")),
            wingColor = color(layers.getJSONObject("wing").getString("fill")),
            eyeColor = color(layers.getJSONObject("eye").getString("fill")),
            tileRadius = root.getJSONObject("tile").optDouble("rx", 27.0).toFloat(),
            tileColor = color(root.getJSONObject("tile").optString("fill", "#0F172A")),
            states = states,
        )
    }

    private fun parseTrack(json: JSONObject, layer: String): PigeonTrack {
        val keysArray = json.optJSONArray("keys") ?: JSONArray()
        val keys = buildList {
            for (index in 0 until keysArray.length()) {
                val entry = keysArray.optJSONArray(index) ?: continue
                val values = entry.optJSONObject(1) ?: continue
                add(
                    PigeonKey(
                        at = entry.optDouble(0, 0.0).toFloat(),
                        tx = values.optDouble("tx", 0.0).toFloat(),
                        ty = values.optDouble("ty", 0.0).toFloat(),
                        rot = values.optDouble("rot", 0.0).toFloat(),
                        sx = values.optDouble("sx", 1.0).toFloat(),
                        sy = values.optDouble("sy", 1.0).toFloat(),
                        alpha = values.optDouble("alpha", 1.0).toFloat(),
                        color = values.optString("color").takeIf { it.isNotBlank() }?.let(::color),
                    ),
                )
            }
        }
        return PigeonTrack(
            layer = layer,
            period = json.optDouble("period_s", 0.0).toFloat(),
            delay = json.optDouble("delay_s", 0.0).toFloat(),
            loop = json.optBoolean("loop", false),
            keys = keys,
        )
    }

    private fun path(data: String): Path = PathParser().parsePathString(data).toPath()

    private fun color(value: String): Color = Color(android.graphics.Color.parseColor(value))
}

@Composable
fun HomingPigeon(
    state: PigeonState,
    size: Dp,
    modifier: Modifier = Modifier,
    showTile: Boolean = false,
    showEye: Boolean = true,
    showWing: Boolean = true,
    animationKey: Any? = Unit,
    contentDescription: String? = null,
) {
    val context = LocalContext.current
    val spec = remember(context) { PigeonSpecStore.get(context) }
    val reducedMotion = remember(context) { isReducedMotion(context) }
    val effective = if (reducedMotion) PigeonState.Static else state
    var time by remember(effective, animationKey) { mutableFloatStateOf(0f) }

    LaunchedEffect(effective, animationKey) {
        time = 0f
        if (effective == PigeonState.Static) return@LaunchedEffect
        var start = 0L
        var initialized = false
        while (isActive) {
            androidx.compose.runtime.withFrameNanos { frame ->
                if (!initialized) {
                    start = frame
                    initialized = true
                }
                time = ((frame - start) / 1_000_000_000f).coerceAtLeast(0f)
            }
            if ((effective == PigeonState.BobOnce && time >= 0.8f)) {
                time = 0.8f
                break
            }
        }
    }

    val semanticsModifier = if (contentDescription == null) {
        Modifier
    } else {
        Modifier.semantics { this.contentDescription = contentDescription }
    }

    Canvas(
        modifier = modifier
            .size(size)
            .then(semanticsModifier),
    ) {
        val canvasScale = this.size.minDimension / 120f
        scale(canvasScale, canvasScale, pivot = Offset.Zero) {
            if (showTile) {
                drawRoundRect(
                    color = spec.tileColor,
                    size = Size(120f, 120f),
                    cornerRadius = CornerRadius(spec.tileRadius),
                )
            }
            val frame = pigeonFrame(spec, effective, time)
            translate(4f, 6f) {
                val bird = frame["bird"] ?: PigeonKey(0f)
                withTransform({
                    translate(bird.tx, bird.ty)
                    rotate(bird.rot, Offset.Zero)
                    scale(bird.sx, bird.sy, Offset.Zero)
                }) {
                    val squash = frame["squash"] ?: PigeonKey(0f)
                    withTransform({
                        scale(squash.sx, squash.sy, Offset(52f, 90f))
                    }) {
                        drawLayer(spec.body, spec.bodyColor, bird.alpha)
                        val head = frame["head"] ?: PigeonKey(0f)
                        withTransform({ translate(head.tx, head.ty) }) {
                            drawLayer(spec.head, spec.headColor, bird.alpha)
                            if (showEye) {
                                val eye = frame["eye"] ?: PigeonKey(0f)
                                withTransform({
                                    scale(eye.sx, eye.sy, Offset(78f, 34f))
                                }) {
                                    drawLayer(spec.eye, spec.eyeColor, bird.alpha)
                                }
                            }
                        }
                        val neck = frame["neck"] ?: PigeonKey(0f)
                        drawLayer(
                            spec.neck,
                            neck.color ?: spec.neckColor,
                            bird.alpha,
                        )
                        if (showWing) {
                            val wing = frame["wing"] ?: PigeonKey(0f)
                            withTransform({
                                rotate(wing.rot, Offset(72f, 68f))
                                scale(wing.sx, wing.sy, Offset(72f, 68f))
                            }) {
                                drawLayer(spec.wing, spec.wingColor, bird.alpha)
                            }
                        }
                    }
                }
            }
        }
    }
}

private fun DrawScope.drawLayer(path: Path, color: Color, alpha: Float) {
    drawPath(path, color.copy(alpha = color.alpha * alpha.coerceIn(0f, 1f)))
}

private fun pigeonFrame(
    spec: PigeonSpec,
    state: PigeonState,
    time: Float,
): Map<String, PigeonKey> {
    val result = mutableMapOf<String, PigeonKey>()
    val stateName = when (state) {
        PigeonState.Idle -> "idle"
        PigeonState.Flap -> "flap"
        PigeonState.ArriveThenIdle -> "arrive_then_idle"
        PigeonState.BobOnce -> "bob_once"
        PigeonState.Static -> "idle"
    }
    val idleAfter = 1.6f
    val activeName = if (state == PigeonState.ArriveThenIdle && time > idleAfter) {
        "idle"
    } else {
        stateName
    }
    val activeTime = if (activeName == "idle" && state == PigeonState.ArriveThenIdle) {
        time - idleAfter
    } else {
        time
    }
    val tracks = spec.states[activeName].orEmpty()
    for (track in tracks) {
        result[track.layer] = track.valueAt(activeTime)
    }
    if (state == PigeonState.ArriveThenIdle && time <= idleAfter) {
        val idleTracks = spec.states["idle"].orEmpty()
        for (track in idleTracks) {
            if (track.layer !in result) {
                result[track.layer] = track.valueAt(0f)
            }
        }
    }
    return result
}

fun isReducedMotion(context: Context): Boolean {
    val scale = runCatching {
        Settings.Global.getFloat(
            context.contentResolver,
            Settings.Global.ANIMATOR_DURATION_SCALE,
            1f,
        )
    }.getOrDefault(1f)
    return abs(scale) < 0.01f
}

private fun lerp(start: Float, end: Float, fraction: Float): Float {
    return start + (end - start) * fraction
}

private fun lerpColor(start: Color?, end: Color?, fraction: Float): Color? {
    if (start == null && end == null) return null
    val from = start ?: end!!
    val to = end ?: start!!
    return Color(
        red = lerp(from.red, to.red, fraction),
        green = lerp(from.green, to.green, fraction),
        blue = lerp(from.blue, to.blue, fraction),
        alpha = lerp(from.alpha, to.alpha, fraction),
    )
}

