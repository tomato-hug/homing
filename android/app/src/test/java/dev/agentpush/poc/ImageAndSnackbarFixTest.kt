package dev.agentpush.poc

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import dev.agentpush.poc.ui.LocalRegisterSnackbarHost
import dev.agentpush.poc.ui.linkifyFilePaths
import dev.agentpush.poc.ui.looksLikeFilePath
import dev.agentpush.poc.ui.screens.extractFallbackImageAttachments
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

@RunWith(RobolectricTestRunner::class)
class ImageAndSnackbarFixTest {

    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun homingMediaUrlAndCacheKeyNormalizeAcrossRoutes() {
        val capUrlCloud = "https://homing.local/f/0123456789abcdef0123456789abcdef/pelican.png"
        val capUrlLan = "http://192.168.1.25:18787/f/0123456789abcdef0123456789abcdef/pelican.png"
        val rawUrlCloud = "https://homing.local/api/fs/raw?path=pelican.png&cwd=%2Ftmp%2Fdemo"

        assertTrue(AgentApi.isHomingMediaUrl(capUrlCloud))
        assertTrue(AgentApi.isHomingMediaUrl(rawUrlCloud))
        assertTrue(AgentApi.isHomingMediaUrl("/f/abc/pic.png"))
        assertFalse(AgentApi.isHomingMediaUrl("https://example.com/image.png"))

        assertEquals(AgentApi.mediaCacheKey(capUrlCloud), AgentApi.mediaCacheKey(capUrlLan))
        assertEquals(
            "/api/fs/raw?path=pelican.png&cwd=%2Ftmp%2Fdemo",
            AgentApi.mediaCacheKey(rawUrlCloud),
        )
    }

    @Test
    fun guessImageMimeDetectsMagicBytesAndExtensions() {
        assertEquals("image/png", guessImageMime("https://homing.local/f/x/a", byteArrayOf(0x89.toByte(), 0x50, 0x4E, 0x47)))
        assertEquals("image/jpeg", guessImageMime("https://homing.local/f/x/a", byteArrayOf(0xFF.toByte(), 0xD8.toByte(), 0xFF.toByte(), 0xE0.toByte())))
        assertEquals("image/gif", guessImageMime("https://homing.local/f/x/a", byteArrayOf(0x47, 0x49, 0x46, 0x38)))
        assertEquals("image/webp", guessImageMime("https://homing.local/f/x/a", byteArrayOf(0x52, 0x49, 0x46, 0x46)))
        assertEquals("image/svg+xml", guessImageMime("https://homing.local/f/x/diagram", "<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>".toByteArray()))
        assertEquals("image/png", guessImageMime("https://homing.local/f/x/out.png", byteArrayOf(1, 2)))
    }

    @Test
    fun linkifyAndFallbackExtractionRecognizeUnicodeAndBareImagePaths() {
        val bareName = "生成完成：`pelican.png`"
        assertTrue(looksLikeFilePath("pelican.png"))
        assertTrue(linkifyFilePaths(bareName).contains("homing-file:pelican.png"))

        val unicodePath = "已保存到 /Users/demo/WorkBuddy/一只白鹈鹕_2026-11-28T15-53-25.png"
        val linked = linkifyFilePaths(unicodePath)
        assertTrue(linked.contains("homing-file:%2FUsers%2Fdemo%2FWorkBuddy%2F"))

        val attachments = extractFallbackImageAttachments(
            text = "输出图片：`output/pelican.png` 和 `/Users/demo/WorkBuddy/一只白鹈鹕.png`",
            cwd = "/Users/demo/WorkBuddy",
            existing = emptyList(),
            settings = AppSettings(baseUrl = "", token = ""),
        )
        assertEquals(2, attachments.size)
        assertTrue(attachments.all { it.isImage })
        assertTrue(attachments[0].url.startsWith("https://homing.local/api/fs/raw?"))
    }

    @Test
    fun nestedScaffoldSuppressesOuterSnackbarHostDuplicate() {
        composeRule.setContent {
            val snackbarHostState = remember { SnackbarHostState() }
            var nestedHosts by remember { mutableIntStateOf(0) }
            val register: () -> () -> Unit = remember {
                {
                    nestedHosts += 1
                    var disposed = false
                    {
                        if (!disposed) {
                            disposed = true
                            nestedHosts = (nestedHosts - 1).coerceAtLeast(0)
                        }
                    }
                }
            }
            CompositionLocalProvider(LocalRegisterSnackbarHost provides register) {
                Scaffold(
                    snackbarHost = {
                        if (nestedHosts == 0) {
                            SnackbarHost(snackbarHostState)
                        }
                    },
                ) { padding ->
                    Box(modifier = Modifier.padding(padding)) {
                        InnerScreenWithOwnSnackbar(snackbarHostState)
                    }
                }
            }
            LaunchedEffect(Unit) {
                snackbarHostState.showSnackbar("已复制回复")
            }
        }

        composeRule.waitForIdle()
        composeRule.onAllNodesWithText("已复制回复").assertCountEquals(1)
    }

    @Composable
    private fun InnerScreenWithOwnSnackbar(snackbarHostState: SnackbarHostState) {
        val register = LocalRegisterSnackbarHost.current
        DisposableEffect(register) {
            val unregister = register?.invoke()
            onDispose { unregister?.invoke() }
        }
        Scaffold(
            snackbarHost = { SnackbarHost(snackbarHostState) },
        ) { innerPadding ->
            Box(modifier = Modifier.padding(innerPadding))
        }
    }
}
