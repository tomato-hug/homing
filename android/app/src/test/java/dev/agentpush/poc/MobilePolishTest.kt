package dev.agentpush.poc

import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.activity.ComponentActivity
import androidx.compose.material3.Surface
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.test.performTextReplacement
import androidx.compose.ui.test.performClick
import android.graphics.Bitmap
import android.graphics.Canvas
import java.io.File
import dev.agentpush.poc.ui.ModelPickerSheet
import dev.agentpush.poc.ui.RichMarkdown
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.MockResponse
import dev.agentpush.poc.ui.theme.AgentInboxTheme
import dev.agentpush.poc.ui.screens.SessionComposer
import dev.agentpush.poc.ui.screens.SessionHeader
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.test.performSemanticsAction
import androidx.compose.ui.text.TextLayoutResult
import org.junit.Assert.assertTrue
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class, qualifiers = "zh-rCN-w390dp-h844dp")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class MobilePolishTest {
    @get:Rule val compose = createAndroidComposeRule<ComponentActivity>()

    @Test
    fun shortMarkdownHeadingDoesNotDominatePhoneReadingArea() {
        compose.setContent { AgentInboxTheme {
            RichMarkdown("# demo-phone 真機驗收\n\n這是合成資料，不是真實工作會話。", modifier = Modifier.testTag("phone-markdown"), onCopyCode = {}, onOpenUrl = {})
        } }
        val height = compose.onNodeWithTag("phone-markdown").fetchSemanticsNode().size.height / compose.density.density
        assertTrue("short heading and paragraph should leave room for conversation: $height dp", height <= 120f)
    }

    @Test
    fun unavailableMarkdownImageShowsFilenameInsteadOfEmptyImage() {
        assertUnavailableImage(reference = false)
    }
    @Test
    fun unavailableReferenceImageAlsoShowsFilename() {
        assertUnavailableImage(reference = true)
    }
    private fun assertUnavailableImage(reference: Boolean) {
        val server = MockWebServer()
        server.enqueue(MockResponse().setResponseCode(404))
        server.start()
        try {
            compose.setContent { AgentInboxTheme {
                val url = server.url("/demo-missing.png")
                RichMarkdown(if (reference) "![demo][image]\n\n[image]: $url" else "![demo]($url)", onCopyCode = {}, onOpenUrl = {})
            } }
            compose.waitUntil(5_000) {
                compose.onAllNodesWithText("图片暂时无法显示", substring = true).fetchSemanticsNodes().isNotEmpty()
            }
            compose.onNodeWithText("图片暂时无法显示", substring = true).assertExists()
            compose.onNodeWithText("demo-missing.png", substring = true).assertExists()
            assertEquals(1, server.requestCount)
        } finally { server.shutdown() }
    }

    @Test
    fun composerStartsCompactAndExplainsReadonlyModelOnDemand() {
        compose.setContent { AgentInboxTheme {
            Surface {
            SessionComposer(
                text = "", onTextChange = {}, pending = emptyList(), modelLabel = "demo/long-model-name",
                effortLabel = "", showEffortPill = false, permissionLabel = "", permissionModes = emptyList(),
                onPermissionChange = {}, modelOverride = false, canSwitchModel = false,
                modelDisabledReason = "Demo model is read only", onModelClick = {}, dispatchTargets = emptyList(),
                mentions = emptyList(), mentionOptions = emptyMap(), onMentionPresetChange = { _, _ -> },
                onMentionOptionsChange = { _, _ -> }, commands = emptyList(), cwd = "", completeFiles = { _, _ -> emptyList() },
                enabled = true, canSend = false, queuedCount = 0, onCancelQueued = {}, onRemove = {}, onRetryUpload = {},
                onPhotoPicker = {}, onCamera = {}, onFilePicker = {}, onSpeech = {}, running = false, runningSince = "",
                canStop = false, onStop = {}, onSend = {},
            )
            }
        } }
        val height = compose.onNodeWithTag("session-input").fetchSemanticsNode().size.height / compose.density.density
        assertTrue("empty composer should fit one line, got $height dp", height <= 64f)
        saveScreenshot("session-composer", "composer")
        compose.onNodeWithText("Demo model is read only").assertDoesNotExist()
        compose.onNodeWithTag("model-info").performClick()
        compose.onNodeWithText("Demo model is read only").assertExists()
    }

    @Test
    fun longTitleUsesOneLineAndOpensFullTitle() {
        val title = "示例：一个需要完整保留、可以点击展开但不挤占会话正文空间的很长标题"
        val session = SessionSummary("demo-session", brand = AgentBrand.CODEX, title = title, titleSource = "custom",
            workspace = "", project = "demo", branch = "", model = "", cwd = "", status = SessionStatus.COMPLETED,
            latestEvent = null, events = emptyList(), replies = emptyList(), unreadCount = 0, lastPreview = "", lastTs = "")
        compose.setContent { AgentInboxTheme {
            SessionHeader(session, false, false, AgentProfile("codex"), null, {}, {}, {}, {}, {}, {}, {}, { _, _ -> }, {}, {}, {}, {}, {}, {}, {})
        } }
        val layouts = mutableListOf<TextLayoutResult>()
        compose.onNodeWithTag("session-title").performSemanticsAction(SemanticsActions.GetTextLayoutResult) { it(layouts) }
        assertEquals(1, layouts.single().lineCount)
        compose.onNodeWithTag("session-title").performClick()
        compose.onNodeWithTag("session-title-details").assertExists()
    }

    @Test
    fun refreshedSmallModelListDoesNotKeepAnInvisibleFilter() {
        val count = androidx.compose.runtime.mutableStateOf(13)
        compose.setContent { AgentInboxTheme {
            val models = (0 until count.value).map { AgentModel("demo/$it", "Demo Model $it", "demo", it == 0, emptyList()) }
            ModelPickerSheet(AgentProfile("codex", models = models), "demo/0", "", {}, { _, _ -> })
        } }
        compose.onNodeWithTag("model-search").performTextInput("no-such-model")
        compose.runOnIdle { count.value = 2 }
        compose.onNodeWithTag("model-search").assertDoesNotExist()
        compose.onNodeWithText("Demo Model 0").assertExists()
    }

    @Test
    fun modelSearchFindsFriendlyNameAndIdAndKeepsSelection() {
        val models = (0..12).map { AgentModel("demo/model-$it", if (it == 12) "Atlas Friendly" else "Demo Model $it", "demo", it == 0, emptyList()) }
        var applied = ""
        compose.setContent {
            AgentInboxTheme {
                ModelPickerSheet(AgentProfile("codex", models = models), models[0].id, "", {}, { model, _ -> applied = model })
            }
        }
        compose.onNodeWithTag("model-search").performTextInput("FRIENDLY")
        compose.onNodeWithText("Atlas Friendly").assertExists().performClick()
        compose.onNodeWithText("Demo Model 0").assertDoesNotExist()
        compose.onNodeWithTag("model-search").performTextReplacement("no-such-model")
        compose.onNodeWithText("没有匹配的模型").assertExists()
        compose.onNodeWithTag("model-search").performTextReplacement("model-12")
        compose.onNodeWithText("Atlas Friendly").assertExists()
        compose.onNodeWithText("用于下一条消息").performClick()
        assertEquals("demo/model-12", applied)
    }

    private fun saveScreenshot(tag: String, name: String) {
        val directory = File("build/reports/mobile-polish").apply { mkdirs() }
        val bounds = compose.onNodeWithTag(tag).fetchSemanticsNode().boundsInRoot
        val view = compose.activity.window.decorView
        val bitmap = Bitmap.createBitmap(bounds.width.toInt(), bounds.height.toInt(), Bitmap.Config.ARGB_8888)
        compose.runOnIdle {
            val canvas = Canvas(bitmap)
            canvas.translate(-bounds.left, -bounds.top)
            view.draw(canvas)
        }
        File(directory, "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }
}
