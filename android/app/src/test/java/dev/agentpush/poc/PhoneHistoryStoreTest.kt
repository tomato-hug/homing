package dev.agentpush.poc

import android.app.Application
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.json.JSONArray
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.time.Instant
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.CopyOnWriteArrayList

/** Real Store/Room paths, with synthetic demo notifications and a local HTTP fixture. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class)
class PhoneHistoryStoreTest {
    private lateinit var app: Application
    private lateinit var store: AgentStore
    private lateinit var server: MockWebServer
    private lateinit var settings: AppSettings
    private lateinit var sessionId: String
    private val itemResponses = ConcurrentLinkedQueue<MockResponse>()
    private val requests = CopyOnWriteArrayList<RecordedRequest>()
    private var receivedEvents = JSONArray()

    @Before
    fun setup() = runBlocking {
        app = ApplicationProvider.getApplicationContext()
        LocalCache(app).clear()
        server = MockWebServer()
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                requests += request
                val path = request.requestUrl!!.encodedPath
                return when {
                    path == "/api/events" -> jsonResponse(receivedEvents.toString())
                    path.endsWith("/items") -> itemResponses.poll()
                        ?: jsonResponse("""{"error":"demo unexpected history request"}""", 503)
                    path.startsWith("/api/sessions/") -> jsonResponse(JSONObject()
                        .put("session_id", path.substringAfterLast('/')).put("source", SOURCE)
                        .put("status", "done").put("running", false).toString())
                    else -> jsonResponse("{}")
                }
            }
        }
        server.start()
        settings = AppSettings(server.url("/").toString().trimEnd('/'), "demo-store-history-token")
        store = AgentStore.get(app)
        AgentApi.bindTransport(null)
    }

    @After
    fun cleanup() = runBlocking {
        store.clearLocalCache()
        AgentApi.bindTransport(null)
        server.shutdown()
    }

    @Test
    fun notificationOnlyReceiptFetchesItsBodyAndKeepsItOnLegacyHost() = runBlocking {
        seed("notification-only")
        val event = store.state.value.events.getValue(ANCHOR)
        store.replaceStateForTest(store.state.value.copy(events = emptyMap()))
        receivedEvents = JSONArray().put(event.copy(id = ANCHOR - 1, text = "demo earlier excluded").toJson())
            .put(event.toJson()).put(event.copy(id = ANCHOR + 1, source = "demo-other-source").toJson())
        itemResponses += jsonResponse("""{"items":[{"id":"demo-legacy","text":"demo older transcript","type":"user"}]}""")

        store.refreshSessionDetails(sessionId, force = true)

        assertEquals(listOf(ANCHOR), store.state.value.phoneSession(sessionId)!!.events.map { it.id })
        assertEquals(event.text, store.state.value.phoneSession(sessionId)!!.events.single().text)
        assertTrue(details().items.isEmpty())
        assertEquals("unsupported", details().history.status)
        val request = requests.single { it.requestUrl!!.encodedPath == "/api/events" }
        assertEquals("99", request.requestUrl!!.queryParameter("since"))
    }

    @Test
    fun initialRefreshUsesReceiptAnchorAndDropsLegacyPreReceiptPrompt() = runBlocking {
        seed("initial", legacyItems = listOf(SessionItem(id = "demo-legacy-prompt",
            ts = "2026-11-21T09:00:00Z", type = SessionItemType.USER, text = "demo old private prompt")))
        enqueuePage(listOf(row(30)))

        store.refreshSessionDetails(sessionId, force = true)

        val current = details()
        assertEquals(listOf("demo-key-30"), current.items.map { it.itemKey })
        assertFalse(current.items.any { it.text == "demo old private prompt" || it.type == SessionItemType.USER })
        assertEquals(ANCHOR, current.history.anchorEventId)
        assertEquals(contextKey(), current.contextKey)
        val url = historyRequests().single().requestUrl!!
        assertEquals(SOURCE, url.queryParameter("source"))
        assertEquals("100", url.queryParameter("anchor_event_id"))
        assertNull(url.queryParameter("before"))
        assertNull(url.queryParameter("turns"))
        assertNull(url.queryParameter("limit"))
    }

    @Test
    fun previousTenRoundsArePrependedOnceAndSurviveRefresh() = runBlocking {
        seed("pages")
        enqueuePage(listOf(row(30), row(31)), cursor = "demo-anchor-cursor")
        store.refreshSessionDetails(sessionId, force = true)

        enqueuePage((1..30).map(::row), cursor = "demo-older-cursor", rounds = 10)
        store.loadEarlierContext(sessionId)

        val expanded = details()
        assertEquals(31, expanded.items.size)
        assertEquals("demo-key-1", expanded.items.first().itemKey)
        assertEquals("demo-key-31", expanded.items.last().itemKey)
        assertEquals(1, expanded.items.count { it.itemKey == "demo-key-30" })
        assertEquals(10, expanded.history.loadedRounds)
        assertEquals("demo-older-cursor", expanded.history.beforeCursor)
        assertFalse(expanded.history.loading)
        val beforeRequest = historyRequests()[1].requestUrl!!
        assertEquals("demo-anchor-cursor", beforeRequest.queryParameter("before"))
        assertEquals("10", beforeRequest.queryParameter("turns"))

        enqueuePage(listOf(row(30), row(31), row(32)), cursor = "demo-new-initial-cursor")
        store.refreshSessionDetails(sessionId, force = true)

        val refreshed = details()
        assertEquals(32, refreshed.items.size)
        assertEquals("demo-key-1", refreshed.items.first().itemKey)
        assertEquals("demo-key-32", refreshed.items.last().itemKey)
        assertEquals(refreshed.items.size, refreshed.items.map { it.itemKey }.toSet().size)
        assertEquals(10, refreshed.history.loadedRounds)
        assertEquals("demo-older-cursor", refreshed.history.beforeCursor)
        assertEquals(1, refreshed.history.revision)
        val cached = LocalCache(app).load().items.getValue(contextKey())
        assertEquals(refreshed.items.map { it.itemKey }, cached.map { it.itemKey })
    }

    @Test
    fun expiredPageKeepsVisibleItemsAndRetryBoundary() = runBlocking {
        seed("expired")
        enqueuePage(listOf(row(30), row(31)), cursor = "demo-retry-boundary")
        store.refreshSessionDetails(sessionId, force = true)
        val before = details()
        itemResponses += jsonResponse("""{"code":"history_cursor_expired","error":"demo cursor has expired"}""", 409)

        store.loadEarlierContext(sessionId)

        val after = details()
        assertEquals(before.items, after.items)
        assertEquals("demo-retry-boundary", after.history.beforeCursor)
        assertEquals(0, after.history.loadedRounds)
        assertFalse(after.history.loading)
        assertFalse(after.history.error.isNullOrBlank())
        assertTrue(after.available)
        assertEquals(before.items.map { it.itemKey }, LocalCache(app).load().items.getValue(contextKey()).map { it.itemKey })
    }

    @Test
    fun managedSseRejectsMissingEarlierEqualOrWrongSourceTimestampedRows() = runBlocking {
        seed("sse")
        enqueuePage(listOf(row(30)))
        store.refreshSessionDetails(sessionId, force = true)
        val anchorRows = details().items

        store.applyItem(frame(row(1).apply { remove("ts") }))
        store.applyItem(frame(row(2)))
        store.applyItem(frame(row(30).put("text", "demo replay must not overwrite anchor")))
        store.applyItem(frame(row(31), source = "demo-wrong-source"))
        assertEquals(anchorRows, details().items)

        store.applyItem(frame(row(31)))
        assertEquals(listOf("demo-key-30", "demo-key-31"), details().items.map { it.itemKey })
        assertEquals("demo assistant 30", details().items.first().text)
    }

    @Test
    fun scopedCacheReloadRetainsMoreThanThreeHundredRowsAndStableIdentities() = runBlocking {
        val key = phoneConversationKey(COMPUTER, SOURCE, "demo-cache-long-history")
        val otherKey = phoneConversationKey("demo-other-computer", SOURCE, "demo-cache-long-history")
        val rows = (1..450).map { index -> SessionItem(
            id = "codex-i${index % 100}", itemKey = "demo-durable-key-$index",
            ts = BASE.plusSeconds(index.toLong()).toString(), type = SessionItemType.ASSISTANT,
            role = "assistant", text = "demo durable item $index") }
        val cache = LocalCache(app)
        cache.saveItems(key, rows)
        cache.saveItems(otherKey, listOf(rows[0].copy(itemKey = "demo-other-key", text = "demo other computer")))
        PhoneConversationPreferences(app).save(PhoneConversationState(
            receipts = mapOf(key to PhoneReceipt(ANCHOR, ANCHOR_TS, 1)),
            histories = mapOf(key to SessionHistoryState(managed = true, anchorEventId = ANCHOR,
                beforeCursor = "demo-durable-before", loadedRounds = 150, revision = 15))))

        // New wrapper instances read persisted Room/preferences data, not the Store's memory cache.
        val reloaded = LocalCache(app).load()
        val restored = reloaded.items.getValue(key)
        assertEquals(450, restored.size)
        assertEquals("demo-durable-key-1", restored.first().itemKey)
        assertEquals("demo-durable-key-450", restored.last().itemKey)
        assertEquals(450, restored.map { it.itemKey }.toSet().size)
        assertEquals("demo other computer", reloaded.items.getValue(otherKey).single().text)
        val history = PhoneConversationPreferences(app).load().histories.getValue(key)
        assertEquals("demo-durable-before", history.beforeCursor)
        assertEquals(150, history.loadedRounds)
    }

    @Test
    fun draftsRoundTripSeparatelyForComputerSourceAndSession() = runBlocking {
        val sharedSession = "demo-draft-same-session"
        val first = phoneConversationKey(COMPUTER, "codex", sharedSession)
        val second = phoneConversationKey(COMPUTER, "hermes", sharedSession)
        val third = phoneConversationKey("demo-other-computer", "codex", sharedSession)
        val cache = LocalCache(app)
        cache.saveDraft(first, "demo codex draft")
        cache.saveDraft(second, "demo hermes draft")
        cache.saveDraft(third, "demo other computer draft")

        val drafts = LocalCache(app).load().drafts
        assertEquals("demo codex draft", drafts[first])
        assertEquals("demo hermes draft", drafts[second])
        assertEquals("demo other computer draft", drafts[third])
        assertNull(drafts[sharedSession])
        cache.saveDraft(second, "")
        val afterClear = LocalCache(app).load().drafts
        assertNull(afterClear[second])
        assertEquals("demo codex draft", afterClear[first])
        assertEquals("demo other computer draft", afterClear[third])
    }

    @Test
    fun rapidScopeSwitchDoesNotCancelAnotherConversationsDraftSave() = runBlocking {
        seed("draft-debounce")
        val original = store.state.value
        val firstKey = contextKey()
        store.updateDraft(sessionId, "demo first computer codex draft")

        store.replaceStateForTest(original.copy(sessionMeta = mapOf(sessionId to original.sessionMeta.getValue(sessionId)
            .copy(source = "hermes")), sessionDrafts = emptyMap()))
        val secondKey = phoneConversationKey(COMPUTER, "hermes", sessionId)
        store.updateDraft(sessionId, "demo first computer hermes draft")

        store.replaceStateForTest(original.copy(activeComputerKey = "demo-other-computer",
            sessionMeta = mapOf(sessionId to original.sessionMeta.getValue(sessionId).copy(computerId = "demo-other-computer")),
            sessionDrafts = emptyMap()))
        val thirdKey = phoneConversationKey("demo-other-computer", SOURCE, sessionId)
        store.updateDraft(sessionId, "demo other computer codex draft")

        // Wait on an observable disk write; no arbitrary sleep and no process-global reset.
        withTimeout(5_000) {
            while (!LocalCache(app).load().drafts.keys.containsAll(listOf(firstKey, secondKey, thirdKey))) delay(20)
        }
        val drafts = LocalCache(app).load().drafts
        assertEquals("demo first computer codex draft", drafts[firstKey])
        assertEquals("demo first computer hermes draft", drafts[secondKey])
        assertEquals("demo other computer codex draft", drafts[thirdKey])
    }

    private fun seed(suffix: String, legacyItems: List<SessionItem> = emptyList()) {
        sessionId = "demo-history-store-$suffix"
        val key = contextKey()
        val receipt = AgentEvent(id = ANCHOR, ts = ANCHOR_TS, source = SOURCE, sessionId = sessionId,
            kind = "done", cwd = "/tmp/demo-history-store", project = "demo-history-store",
            title = "Demo received response", text = "demo assistant 30", read = false)
        store.replaceStateForTest(AgentUiState(settings = settings, activeComputerKey = COMPUTER,
            phoneConversations = PhoneConversationState(receipts = mapOf(key to PhoneReceipt(ANCHOR, ANCHOR_TS, 1))),
            sessionMeta = mapOf(sessionId to SessionMeta(sessionId = sessionId, source = SOURCE, computerId = COMPUTER,
                title = "Demo history store", status = "done", running = false, lastTs = ANCHOR_TS)),
            events = mapOf(ANCHOR to receipt),
            sessionItems = mapOf(sessionId to if (legacyItems.isEmpty()) SessionItemsState(contextKey = key,
                history = SessionHistoryState(managed = true, anchorEventId = ANCHOR), available = false)
                else SessionItemsState(items = legacyItems))))
    }

    private fun details(): SessionItemsState = store.state.value.sessionItems.getValue(sessionId)
    private fun contextKey(): String = phoneConversationKey(COMPUTER, SOURCE, sessionId)
    private fun historyRequests(): List<RecordedRequest> = requests.filter { it.requestUrl!!.encodedPath.endsWith("/items") }

    private fun enqueuePage(items: List<JSONObject>, cursor: String = "demo-before", rounds: Int = 0) {
        itemResponses += jsonResponse(JSONObject().put("history_version", 1).put("anchor_event_id", ANCHOR)
            .put("source", SOURCE).put("session_id", sessionId).put("anchor_status", "matched")
            .put("items", JSONArray(items)).put("turns", JSONArray()).put("count", items.size)
            .put("before_cursor", cursor).put("history_status", "available").put("has_more_before", true)
            .put("rounds_returned", rounds).toString())
    }

    private fun row(index: Int): JSONObject {
        val role = when (index % 3) { 1 -> "user"; 2 -> "tool"; else -> "assistant" }
        return JSONObject().put("id", "codex-i$index").put("item_key", "demo-key-$index")
            .put("seq", index).put("ts", BASE.plusSeconds(index.toLong()).toString())
            .put("type", if (role == "tool") "tool_call" else "message")
            .put("role", role).put("text", "demo $role $index").apply {
                if (role == "tool") put("tool", JSONObject().put("name", "demo-tool")
                    .put("status", "done").put("output_tail", "demo complete tool output $index"))
            }
    }

    private fun frame(item: JSONObject, source: String = SOURCE): JSONObject = JSONObject()
        .put("session_id", sessionId).put("source", source).put("item", item)

    private fun jsonResponse(body: String, status: Int = 200): MockResponse = MockResponse()
        .setResponseCode(status).setHeader("Content-Type", "application/json").setBody(body)

    private companion object {
        const val SOURCE = "codex"
        const val COMPUTER = "demo-history-computer"
        const val ANCHOR = 100L
        const val ANCHOR_TS = "2026-11-21T10:00:30Z"
        val BASE: Instant = Instant.parse("2026-11-21T10:00:00Z")
    }
}
