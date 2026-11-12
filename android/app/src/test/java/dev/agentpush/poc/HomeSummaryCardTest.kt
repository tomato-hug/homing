package dev.agentpush.poc

import android.app.Application
import android.provider.Settings
import androidx.compose.ui.test.assertHeightIsAtLeast
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ApplicationProvider
import dev.agentpush.poc.ui.HomingPigeonScene
import dev.agentpush.poc.ui.PigeonCrop
import dev.agentpush.poc.ui.PigeonScene
import dev.agentpush.poc.ui.cubicBezier
import dev.agentpush.poc.ui.screens.HomeSummaryCard
import dev.agentpush.poc.ui.theme.AgentInboxTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class HomeSummaryCardTest {
    @get:Rule
    val compose = createComposeRule()

    @Before
    fun setUp() {
        val app: Application = ApplicationProvider.getApplicationContext()
        Settings.Global.putFloat(app.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 0f)
    }

    @Test
    fun summaryShowsConnectionHeadlineAndLargeTiles() {
        var attentionTaps = 0
        var openAll = 0
        compose.setContent {
            AgentInboxTheme {
                HomeSummaryCard(
                    connection = ConnectionState.CONNECTED,
                    transportKind = TransportKind.LAN,
                    computerLabel = "我的 Mac",
                    headline = "3 件新动态",
                    subtitle = "另有 2 个在运行",
                    attentionCount = 3,
                    runningCount = 2,
                    recentCount = 12,
                    onAttention = { attentionTaps += 1 },
                    onOpenAll = { openAll += 1 },
                )
            }
        }
        compose.onNodeWithText("已连接").assertExists()
        compose.onNodeWithText("3 件新动态").assertExists()
        compose.onNodeWithText("待处理").assertExists()
        compose.onNodeWithTag("summary-attention").assertHeightIsAtLeast(48.dp).performClick()
        compose.onNodeWithTag("summary-all").assertHeightIsAtLeast(48.dp).performClick()
        assertEquals(1, attentionTaps)
        assertEquals(1, openAll)
    }

    @Test
    fun everyPigeonSceneRendersWithoutCrashing() {
        compose.setContent {
            AgentInboxTheme {
                androidx.compose.foundation.layout.Column {
                    PigeonScene.entries.forEach { scene ->
                        HomingPigeonScene(scene = scene, size = 40.dp, crop = PigeonCrop.STAGE, loop = true)
                    }
                    HomingPigeonScene(scene = null, size = 32.dp)
                }
            }
        }
        compose.waitForIdle()
    }

    @Test
    fun cubicBezierHitsEndpointsAndIsMonotonic() {
        assertEquals(0f, cubicBezier(0.45f, 0f, 0.55f, 1f, 0f))
        assertEquals(1f, cubicBezier(0.45f, 0f, 0.55f, 1f, 1f))
        var previous = 0f
        for (step in 1..20) {
            val value = cubicBezier(0.2f, 0.7f, 0.3f, 1f, step / 20f)
            assertTrue(value >= previous - 1e-4f)
            previous = value
        }
    }
}
