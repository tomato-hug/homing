package dev.agentpush.poc

import android.net.Uri
import android.view.View
import android.view.ViewGroup
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextReplacement
import androidx.compose.ui.test.performImeAction
import dev.agentpush.poc.ui.screens.BrowserScreen
import dev.agentpush.poc.ui.screens.authenticatedProxyResponse
import dev.agentpush.poc.ui.screens.browserProxyUrl
import dev.agentpush.poc.ui.screens.isAuthenticatedProxyUrl
import dev.agentpush.poc.ui.theme.AgentInboxTheme
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.util.concurrent.TimeUnit

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class BrowserScreenTest {
    @get:Rule val compose = createAndroidComposeRule<ComponentActivity>()

    private fun show(url: String = "https://example.org/demo", base: String = "https://relay.example.org", onBack: () -> Unit = {}): WebView {
        compose.setContent {
            AgentInboxTheme {
                BrowserScreen(url, "演示网页", base, null, "demo-browser-token", onBack)
            }
        }
        compose.waitForIdle()
        return findWebView(compose.activity.window.decorView)!!
    }

    @Test fun initialScriptUrlIsNeverLoaded() {
        val view = show("javascript:alert('demo')")
        assertNull(shadowOf(view).lastLoadedUrl)
        compose.onNodeWithText("无法打开此链接").assertExists()
    }

    @Test fun systemBackUsesPageHistoryThenReturnsToConversation() {
        var closed = 0
        val view = show(onBack = { closed++ })
        val shadow = shadowOf(view)
        compose.runOnIdle {
            shadow.setCanGoBack(true)
            compose.activity.onBackPressedDispatcher.onBackPressed()
        }
        assertEquals(1, shadow.goBackInvocations)
        assertEquals(0, closed)
        compose.runOnIdle {
            shadow.setCanGoBack(false)
            compose.activity.onBackPressedDispatcher.onBackPressed()
        }
        assertEquals(1, closed)
    }

    @Test fun closeReturnsToConversationEvenWithPageHistory() {
        var closed = 0
        val view = show(onBack = { closed++ })
        compose.runOnIdle { shadowOf(view).setCanGoBack(true) }
        compose.onNodeWithContentDescription("返回会话").performClick()
        assertEquals(1, closed)
        assertEquals(0, shadowOf(view).goBackInvocations)
    }

    @Test fun untrustedSchemesCannotLaunchOtherActivities() {
        val view = show()
        val app = shadowOf(compose.activity.application)
        compose.runOnIdle {
            assertTrue(view.webViewClient.shouldOverrideUrlLoading(view, Request("intent://demo/#Intent;scheme=demo;end")))
        }
        assertNull(app.nextStartedActivity)
    }

    @Test fun addressBarBlocksScriptsAndOpensBareHostsOverHttps() {
        val view = show()
        compose.onNodeWithTag("browser-address").performTextReplacement("javascript:alert('demo')")
        compose.onNodeWithTag("browser-address").performImeAction()
        assertEquals("https://example.org/demo", shadowOf(view).lastLoadedUrl)
        compose.onNodeWithText("无法打开此链接").assertExists()
        compose.onNodeWithTag("browser-address").performTextReplacement("example.net/demo")
        compose.onNodeWithTag("browser-address").performImeAction()
        assertEquals("https://example.net/demo", shadowOf(view).lastLoadedUrl)
        compose.onNodeWithText("无法打开此链接").assertDoesNotExist()
    }

    @Test fun initialLocalhostRequiresProxyChoiceAndPreservesUrlParts() {
        val view = show("http://[::1]:3000/demo?q=hello%20world#section")
        assertNull(shadowOf(view).lastLoadedUrl)
        compose.onNodeWithText("网页链接指向 Mac 本机").assertExists()
        compose.onNodeWithText("尝试直连代理").performClick()
        assertEquals("https://relay.example.org/p/3000/demo?q=hello%20world#section", shadowOf(view).lastLoadedUrl)
        assertEquals(
            "https://relay.example.org/p/3000/demo?q=hello%20world&cap=demo%2Bcap#section",
            browserProxyUrl("http://localhost:3000/demo?q=hello%20world#section", "https://relay.example.org", "demo+cap"),
        )
        assertNull(browserProxyUrl("https://localhost:3000/demo", "https://relay.example.org", null))
    }

    @Test fun mainFrameHttpFailureShowsRetryAndSubresourceFailureDoesNot() {
        val view = show()
        val response = WebResourceResponse("text/plain", "UTF-8", 503, "Unavailable", emptyMap(), null)
        compose.runOnIdle { view.webViewClient.onReceivedHttpError(view, Request("https://example.org/image", mainFrame = false), response) }
        compose.onNodeWithText("重试").assertDoesNotExist()
        compose.runOnIdle { view.webViewClient.onReceivedHttpError(view, Request("https://example.org/demo"), response) }
        compose.onNodeWithText("重试").performClick()
        assertEquals("https://example.org/demo", shadowOf(view).lastLoadedUrl)
    }

    @Test fun proxyRedirectNeverSendsBearerToNonProxyPath() {
        MockWebServer().use { server ->
            server.enqueue(MockResponse().setResponseCode(302).addHeader("Location", "/api/demo-private"))
            server.enqueue(MockResponse().setBody("must not be fetched"))
            val view = show(base = server.url("/").toString())
            val response = view.webViewClient.shouldInterceptRequest(view, Request(server.url("/p/3000/demo").toString()))
            assertEquals("Bearer demo-browser-token", server.takeRequest(2, TimeUnit.SECONDS)?.getHeader("Authorization"))
            assertNull("Proxy redirects must not carry the relay credential outside /p", server.takeRequest(200, TimeUnit.MILLISECONDS))
            assertEquals(502, response?.statusCode)
            response?.data?.close()
        }
    }

    @Test fun proxyPostIsNotSilentlyRewrittenToGet() {
        MockWebServer().use { server ->
            server.enqueue(MockResponse().setBody("unexpected GET"))
            val view = show(base = server.url("/").toString())
            val response = view.webViewClient.shouldInterceptRequest(view, Request(server.url("/p/3000/submit").toString(), method = "POST"))
            assertNull(server.takeRequest(200, TimeUnit.MILLISECONDS))
            assertEquals(405, response?.statusCode)
            response?.data?.close()
        }
    }

    @Test fun proxyAuthIsBoundToCanonicalOriginAndPortRoute() {
        val base = "https://relay.example.org"
        assertTrue(isAuthenticatedProxyUrl("https://RELAY.example.org:443/p/3000/demo", base))
        for (url in listOf(
            "https://relay.example.org.attacker.invalid/p/3000/demo",
            "https://relay.example.org@attacker.invalid/p/3000/demo",
            "https://relay.example.org:8443/p/3000/demo",
            "http://relay.example.org/p/3000/demo",
            "https://relay.example.org/p/3000/../../api/demo-private",
            "https://relay.example.org/p/3000/%2e%2e/%2e%2e/api/demo-private",
            "https://relay.example.org/p/3000/%2f..%2fapi/demo-private",
            "https://relay.example.org/p/0/demo",
            "https://relay.example.org/p/65536/demo",
        )) {
            assertFalse(url, isAuthenticatedProxyUrl(url, base))
            assertNull(authenticatedProxyResponse(url, base, "demo-browser-token", "GET"))
        }
    }

    @Test fun externalRedirectReceivesNoRequestWhileValidProxyReturnsItsBody() {
        MockWebServer().use { relay ->
            MockWebServer().use { external ->
                relay.enqueue(MockResponse().setResponseCode(302).addHeader("Location", external.url("/demo")))
                external.enqueue(MockResponse().setBody("must not be fetched"))
                val base = relay.url("/").toString()
                val redirected = authenticatedProxyResponse(relay.url("/p/3000/demo").toString(), base, "demo-browser-token", "GET")
                assertEquals("Bearer demo-browser-token", relay.takeRequest(2, TimeUnit.SECONDS)?.getHeader("Authorization"))
                assertNull(external.takeRequest(200, TimeUnit.MILLISECONDS))
                assertEquals(502, redirected?.statusCode)
                redirected?.data?.close()
                relay.enqueue(MockResponse().setHeader("Content-Type", "text/html; charset=UTF-8").setBody("<h1>demo browser</h1>"))
                val response = authenticatedProxyResponse(relay.url("/p/3000/demo").toString(), base, "demo-browser-token", "GET")
                assertEquals(200, response?.statusCode)
                assertEquals("<h1>demo browser</h1>", response?.data?.bufferedReader()?.use { it.readText() })
                assertEquals("Bearer demo-browser-token", relay.takeRequest(2, TimeUnit.SECONDS)?.getHeader("Authorization"))
            }
        }
    }

    private fun findWebView(view: View): WebView? {
        if (view is WebView) return view
        if (view is ViewGroup) for (index in 0 until view.childCount) findWebView(view.getChildAt(index))?.let { return it }
        return null
    }

    private class Request(private val value: String, private val method: String = "GET", private val mainFrame: Boolean = true) : WebResourceRequest {
        override fun getUrl(): Uri = Uri.parse(value)
        override fun isForMainFrame() = mainFrame
        override fun isRedirect() = false
        override fun hasGesture() = true
        override fun getMethod() = method
        override fun getRequestHeaders(): Map<String, String> = emptyMap()
    }
}
