package dev.agentpush.poc

import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.json.JSONArray
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.util.concurrent.TimeUnit

/** Synthetic HTTP fixtures only. No host transcripts, real notifications or replies. */
class PhoneHistoryApiTest {
    private lateinit var server: MockWebServer
    private lateinit var settings: AppSettings

    @Before
    fun startServer() {
        AgentApi.bindTransport(null)
        server = MockWebServer()
        server.start()
        settings = AppSettings(server.url("/").toString().trimEnd('/'), "demo-history-token")
    }

    @After
    fun stopServer() {
        AgentApi.bindTransport(null)
        server.shutdown()
    }

    @Test
    fun numericReaderIdCannotClaimAnUnrelatedNotification() {
        val item = SessionItem.fromJson(JSONObject("""{"id":"100","type":"assistant","text":"demo reader row"}"""))!!
        assertEquals(null, item.eventId)
    }

    @Test
    fun legacyResponseCannotSupplyPreReceiptItems() = runBlocking {
        for (body in listOf(
            """{"items":[{"id":"demo-old-user","type":"user","text":"demo pre-receipt private prompt"}]}""",
            """[{"id":"demo-old-user","type":"user","text":"demo pre-receipt private prompt"}]""",
        )) {
            enqueue(body)
            val result = AgentApi.fetchSessionItems(settings, SESSION, source = SOURCE, anchorEventId = ANCHOR)
            assertFalse(result.available)
            assertTrue(result.items.isEmpty())
            assertEquals("server_upgrade_required", result.history?.reason)
        }
    }

    @Test
    fun initialRequestCarriesExactSourceAndAnchorWithoutLegacyLimit() = runBlocking {
        enqueue(envelope(source = "demo source+one").put("reason", JSONObject.NULL).toString())
        val result = AgentApi.fetchSessionItems(settings, SESSION, source = "demo source+one", anchorEventId = ANCHOR)
        assertNull(result.reason)
        val request = server.takeRequest(2, TimeUnit.SECONDS)
        assertNotNull(request)
        val url = request!!.requestUrl!!
        assertEquals("/api/sessions/$SESSION/items", url.encodedPath)
        assertEquals("demo source+one", url.queryParameter("source"))
        assertEquals("17", url.queryParameter("anchor_event_id"))
        assertNull(url.queryParameter("limit"))
        assertNull(url.queryParameter("before"))
        assertNull(url.queryParameter("turns"))
        assertTrue(result.available)
        assertEquals("demo-stable-answer", result.items.single().itemKey)
    }

    @Test
    fun explicitHistoryPreservesOpaqueCursorAndRequestsTenRounds() = runBlocking {
        val cursor = "demo/opaque+cursor= with space"
        enqueue(envelope().put("rounds_returned", 10).put("before_cursor", "demo-next-cursor").toString())
        val result = AgentApi.fetchSessionItems(settings, SESSION, before = cursor,
            includeReasoning = true, source = SOURCE, anchorEventId = ANCHOR)
        val url = server.takeRequest(2, TimeUnit.SECONDS)!!.requestUrl!!
        assertEquals(cursor, url.queryParameter("before"))
        assertEquals("10", url.queryParameter("turns"))
        assertEquals("17", url.queryParameter("anchor_event_id"))
        assertEquals(SOURCE, url.queryParameter("source"))
        assertEquals("1", url.queryParameter("include_reasoning"))
        assertNull(url.queryParameter("limit"))
        assertEquals(10, result.roundsReturned)
        assertEquals("demo-next-cursor", result.history?.beforeCursor)
    }

    @Test
    fun expiredHistoryIsAnErrorRatherThanAnEmptySuccessfulPage() = runBlocking {
        enqueue("""{"code":"history_cursor_expired","error":"demo cursor expired"}""", 409)
        val error = runCatching {
            AgentApi.fetchSessionItems(settings, SESSION, before = "demo-cursor", source = SOURCE, anchorEventId = ANCHOR)
        }.exceptionOrNull()
        assertTrue(error is AgentApiException)
        assertEquals(409, (error as AgentApiException).statusCode)
        assertEquals("demo cursor expired", error.message)
    }

    @Test
    fun matchingAnchorDoesNotAuthorizeAnotherSourceOrSession() = runBlocking {
        for (body in listOf(
            envelope(source = "demo-other-source"),
            envelope(session = "demo-other-session"),
        )) {
            enqueue(body.toString())
            val response = runCatching {
                AgentApi.fetchSessionItems(settings, SESSION, source = SOURCE, anchorEventId = ANCHOR)
            }
            assertTrue("A mismatched conversation must not supply transcript items",
                response.isFailure || response.getOrNull()?.available == false)
            assertTrue(response.getOrNull()?.items.isNullOrEmpty())
        }
    }

    @Test
    fun historyVersionWithoutStableItemKeysFailsClosed() = runBlocking {
        val body = envelope()
        body.getJSONArray("items").getJSONObject(0).remove("item_key")
        enqueue(body.toString())
        val response = runCatching {
            AgentApi.fetchSessionItems(settings, SESSION, source = SOURCE, anchorEventId = ANCHOR)
        }
        assertTrue("Version 1 cannot fall back to reader-local IDs for history identity",
            response.isFailure || response.getOrNull()?.available == false)
        assertTrue(response.getOrNull()?.items.isNullOrEmpty())
    }

    @Test
    fun distinctStableItemsSurviveReusedReaderIds() = runBlocking {
        val body = envelope().put("items", JSONArray()
            .put(item("demo-earlier-user", "codex-i1", "user", "demo earlier prompt"))
            .put(item("demo-later-answer", "codex-i1", "assistant", "demo later answer")))
        enqueue(body.toString())
        val result = AgentApi.fetchSessionItems(settings, SESSION, source = SOURCE, anchorEventId = ANCHOR)
        assertTrue(result.available)
        assertEquals(listOf("demo-earlier-user", "demo-later-answer"), result.items.map { it.itemKey })
        assertEquals(listOf("demo earlier prompt", "demo later answer"), result.items.map { it.text })
    }

    @Test
    fun refreshKeepsExplicitEarlierPagesAndUpsertsOverlappingAnchor() {
        fun row(key: String, text: String) = SessionItem(id = "demo-reader-$key",
            itemKey = key, ts = "2026-11-21T10:00:00Z", type = SessionItemType.ASSISTANT, text = text)
        val anchor = row("demo-anchor", "demo anchor")
        val later = row("demo-later", "demo later")
        val earlier = row("demo-earlier", "demo earlier")
        val loaded = mergePhoneItems(listOf(anchor, later), listOf(earlier, anchor), prepend = true)
        val refreshed = mergePhoneItems(loaded,
            listOf(anchor.copy(text = "demo corrected anchor"), later, row("demo-new", "demo newest")), prepend = false)
        assertEquals(listOf("demo-earlier", "demo-anchor", "demo-later", "demo-new"), refreshed.map { it.itemKey })
        assertEquals("demo corrected anchor", refreshed[1].text)
    }

    @Test
    fun confirmingAnOlderLiveRowDoesNotMoveItAfterNewerLiveContent() {
        val first = SessionItem(id = "demo-live-first", ts = "2026-11-21T10:00:00Z",
            type = SessionItemType.ASSISTANT, text = "demo first response")
        val second = first.copy(id = "demo-live-second", ts = "2026-11-21T10:00:01Z", text = "demo second response")
        val confirmedFirst = first.copy(id = "demo-native-first", itemKey = "demo-key-first")
        val merged = mergePhoneItems(listOf(first, second), listOf(confirmedFirst), prepend = false)
        assertEquals(listOf("demo first response", "demo second response"), merged.map { it.text })
        assertEquals("demo-key-first", merged.first().itemKey)
    }

    @Test
    fun liveReaderIdReuseCannotEraseAnAuthoritativeEarlierItem() {
        val earlier = SessionItem(id = "codex-i1", itemKey = "demo-earlier-stable",
            ts = "2026-11-21T10:00:00Z", type = SessionItemType.ASSISTANT, text = "demo earlier response")
        val existing = SessionItemsState(items = listOf(earlier), contextKey = "demo-context",
            history = SessionHistoryState(managed = true, anchorEventId = ANCHOR))
        val incoming = SessionItem(id = "codex-i1", ts = "2026-11-21T11:00:00Z",
            type = SessionItemType.ASSISTANT, text = "demo newer response after parser renumbering")
        val result = appendSessionItem(existing, incoming)
        assertTrue("Unkeyed replay must not overwrite a different authoritative row",
            result.items.any { it.itemKey == "demo-earlier-stable" && it.text == "demo earlier response" })
    }

    private fun enqueue(body: String, status: Int = 200) {
        server.enqueue(MockResponse().setResponseCode(status)
            .setHeader("Content-Type", "application/json").setBody(body))
    }

    private fun envelope(source: String = SOURCE, session: String = SESSION): JSONObject = JSONObject()
        .put("history_version", 1).put("anchor_event_id", ANCHOR)
        .put("session_id", session).put("source", source)
        .put("anchor_status", "matched").put("before_cursor", "demo-before")
        .put("history_status", "available").put("has_more_before", true)
        .put("rounds_returned", 0).put("turns", JSONArray())
        .put("items", JSONArray().put(item("demo-stable-answer", "codex-i1", "assistant", "demo answer")))

    private fun item(key: String, id: String, role: String, text: String): JSONObject = JSONObject()
        .put("item_key", key).put("id", id).put("type", "message").put("role", role)
        .put("ts", "2026-11-21T10:00:00Z").put("text", text)

    private companion object {
        const val SESSION = "demo-history-session"
        const val SOURCE = "demo-history-source"
        const val ANCHOR = 17L
    }
}
