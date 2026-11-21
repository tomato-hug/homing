package dev.agentpush.poc

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import org.json.JSONTokener
import java.io.File
import java.io.FilterOutputStream
import java.io.IOException
import java.io.OutputStream
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder
import java.time.Instant

class AgentApiException(
    val statusCode: Int,
    message: String,
) : IOException(message)

data class UploadResult(
    val ok: Boolean,
    val path: String,
    val name: String,
    val size: Long,
    val mime: String,
)

data class NewSessionResult(
    val ok: Boolean,
    val sessionId: String?,
    val status: String,
    val error: String? = null,
    val warning: String? = null,
    val requestId: String? = null,
    val cwd: String = "",
)

data class DispatchResult(
    val ok: Boolean,
    val dispatch: AgentDispatch? = null,
    val error: String? = null,
)

data class SessionItemsResult(
    val items: List<SessionItem> = emptyList(),
    val turns: List<SessionTurn> = emptyList(),
    val reason: String? = null,
    val available: Boolean = true,
    val history: SessionHistoryState? = null,
    val roundsReturned: Int = 0,
)

data class CommandResult(
    val ok: Boolean,
    val message: String? = null,
)

object AgentApi {
    @Volatile
    private var transportRouter: TransportRouter? = null

    @Volatile
    private var activeSettingsProvider: (() -> AppSettings)? = null

    internal fun bindTransport(
        router: TransportRouter?,
        activeSettings: (() -> AppSettings)? = null,
    ) {
        transportRouter = router
        activeSettingsProvider = activeSettings
    }

    suspend fun probe(settings: AppSettings): String = withContext(Dispatchers.IO) {
        val events = fetchEventsBlocking(settings, since = 0L)
        val sessions = runCatching { fetchSessions(settings, limit = 20) }
            .getOrDefault(emptyList())
        val projects = runCatching { fetchProjectsBlocking(settings) }.getOrDefault(emptyList())
        "连接正常，已读取 ${sessions.size} 个会话、${events.size} 条事件、${projects.size} 个项目"
    }

    suspend fun fetchEvents(settings: AppSettings, since: Long = 0L): List<AgentEvent> =
        withContext(Dispatchers.IO) {
            fetchEventsBlocking(settings, since)
        }

    suspend fun fetchSessions(
        settings: AppSettings,
        source: String = "",
        status: String = "",
        limit: Int = 500,
    ): List<SessionMeta> =
        withContext(Dispatchers.IO) {
            val query = buildList {
                add("limit=${limit.coerceIn(1, 500)}")
                source.takeIf { it.isNotBlank() }?.let { add("source=$it") }
                status.takeIf { it.isNotBlank() }?.let { add("status=$it") }
            }.joinToString("&")
            val body = requestText(settings, "/api/sessions?$query", "GET")
            val root = JSONTokener(body).nextValue()
            val array = when (root) {
                is JSONArray -> root
                is JSONObject -> root.optJSONArray("sessions")
                    ?: root.optJSONArray("items")
                    ?: JSONArray()
                else -> JSONArray()
            }
            buildList {
                for (index in 0 until array.length()) {
                    val objectAtIndex = array.optJSONObject(index) ?: continue
                    val meta = SessionMeta.fromJson(objectAtIndex)
                    if (meta.sessionId.isNotBlank()) add(meta)
                }
            }
        }

    suspend fun fetchSession(settings: AppSettings, sessionId: String): SessionMeta? =
        withContext(Dispatchers.IO) {
            val encodedSession = URLEncoder.encode(sessionId, Charsets.UTF_8.name())
            val body = try {
                requestText(settings, "/api/sessions/$encodedSession", "GET")
            } catch (error: AgentApiException) {
                if (error.statusCode == 404) return@withContext null
                throw error
            }
            val json = runCatching { JSONObject(body) }.getOrNull() ?: return@withContext null
            val session = json.optJSONObject("session") ?: json
            SessionMeta.fromJson(session, fallbackSessionId = sessionId)
        }

    suspend fun fetchInbox(settings: AppSettings): InboxFeed = withContext(Dispatchers.IO) {
        val body = try {
            requestText(settings, "/api/inbox", "GET")
        } catch (error: AgentApiException) {
            if (error.statusCode in setOf(404, 405, 501)) {
                return@withContext InboxFeed(available = false)
            }
            throw error
        }
        val root = runCatching { JSONObject(body) }.getOrNull()
            ?: return@withContext InboxFeed(available = false)
        val waiting = parseInboxEntries(root.optJSONArray("waiting"))
        val running = parseInboxEntries(root.optJSONArray("running"))
        val recentDone = parseInboxEntries(root.optJSONArray("recent_done"))
        val counts = root.optJSONObject("counts")
        InboxFeed(
            waiting = waiting,
            running = running,
            recentDone = recentDone,
            waitingCount = counts?.optInt("waiting", waiting.size) ?: waiting.size,
            runningCount = counts?.optInt("running", running.size) ?: running.size,
            generatedAt = root.stringOrBlank("generated_at"),
            available = true,
        )
    }

    suspend fun fetchHome(settings: AppSettings): HomeFeed? = withContext(Dispatchers.IO) {
        val body = try {
            requestText(settings, "/api/home", "GET")
        } catch (error: AgentApiException) {
            if (error.statusCode in setOf(404, 405, 501)) return@withContext null
            throw error
        }
        runCatching { HomeFeed.fromJson(JSONObject(body)) }.getOrNull()
    }

    suspend fun clearAwayPeriod(settings: AppSettings, id: String): Boolean =
        withContext(Dispatchers.IO) {
            if (id.isBlank()) return@withContext false
            val encoded = URLEncoder.encode(id, Charsets.UTF_8.name())
            val body = try {
                requestText(settings, "/api/away/periods/$encoded/clear", "POST", "{}")
            } catch (error: AgentApiException) {
                if (error.statusCode == 404) return@withContext false
                throw error
            }
            runCatching { JSONObject(body).optBoolean("ok", true) }.getOrDefault(true)
        }

    suspend fun patchSession(
        settings: AppSettings,
        sessionId: String,
        fields: Map<String, Boolean> = emptyMap(),
        title: String? = null,
    ): SessionMeta? = withContext(Dispatchers.IO) {
        if (sessionId.isBlank() || (fields.isEmpty() && title == null)) return@withContext null
        val encodedSession = URLEncoder.encode(sessionId, Charsets.UTF_8.name())
        val payload = JSONObject()
        fields.forEach { (key, value) -> payload.put(key, value) }
        title?.let { payload.put("title", it) }
        val body = try {
            requestText(
                settings,
                "/api/sessions/$encodedSession",
                "PATCH",
                payload.toString(),
            )
        } catch (error: AgentApiException) {
            if (error.statusCode in setOf(404, 405, 501)) {
                return@withContext null
            }
            throw error
        }
        val json = runCatching { JSONObject(body) }.getOrNull()
            ?: return@withContext null
        val sessionJson = json.optJSONObject("session")
        val meta = SessionMeta.fromJson(
            sessionJson ?: json,
            fallbackSessionId = sessionId,
        )
        val state = json.optJSONObject("state")
        meta.copy(
            read = state?.booleanOrNull("read") ?: meta.read,
            pinned = state?.booleanOrNull("pinned") ?: meta.pinned,
            muted = state?.booleanOrNull("muted") ?: meta.muted,
            archived = state?.booleanOrNull("archived") ?: meta.archived,
            remindOnDone = state?.booleanOrNull("remind_on_done") ?: meta.remindOnDone,
        )
    }

    suspend fun cancelReply(settings: AppSettings, replyId: Long): ReplyReceipt =
        withContext(Dispatchers.IO) {
            if (replyId <= 0) {
                return@withContext ReplyReceipt(
                    ok = false,
                    replyId = replyId,
                    status = ReplyStatus.FAILED,
                    message = "回复 id 无效",
                )
            }
            val body = try {
                requestText(
                    settings,
                    "/api/replies/$replyId/cancel",
                    "POST",
                    "{}",
                )
            } catch (error: AgentApiException) {
                if (error.statusCode in setOf(404, 405, 409, 501)) {
                    return@withContext ReplyReceipt(
                        ok = false,
                        replyId = replyId,
                        status = ReplyStatus.QUEUED,
                        message = error.message,
                    )
                }
                throw error
            }
            val json = runCatching { JSONObject(body) }.getOrElse { JSONObject() }
            ReplyReceipt(
                ok = json.optBoolean("ok", false),
                replyId = json.optLong("reply_id", replyId),
                status = ReplyStatus.fromWire(json.optString("status").ifBlank { "cancelled" }),
                message = json.optString("message").takeIf { it.isNotBlank() },
            )
        }

    suspend fun fetchAgentDirectory(settings: AppSettings): AgentDirectory =
        withContext(Dispatchers.IO) {
            val body = try {
                requestText(settings, "/api/agents", "GET")
            } catch (error: AgentApiException) {
                if (error.statusCode == 404 || error.statusCode == 501) {
                    return@withContext AgentDirectory()
                }
                throw error
            }
            parseAgentDirectory(body)
        }

    suspend fun fetchAgentCatalog(settings: AppSettings): AgentCatalog =
        withContext(Dispatchers.IO) {
            val body = try {
                requestText(settings, "/api/agent-catalog", "GET")
            } catch (error: AgentApiException) {
                if (error.statusCode == 404 || error.statusCode == 501) {
                    return@withContext AgentCatalog()
                }
                throw error
            }
            runCatching { JSONObject(body) }
                .getOrNull()
                ?.let(AgentCatalog::fromJson)
                ?: AgentCatalog()
        }

    suspend fun setAgentConnected(
        settings: AppSettings,
        agentId: String,
        connected: Boolean,
    ): AgentActionResult = withContext(Dispatchers.IO) {
        val id = URLEncoder.encode(agentId.trim(), Charsets.UTF_8.name())
        val path = if (connected) {
            "/api/agent-catalog/$id/quick"
        } else {
            "/api/agents/$id/disconnect"
        }
        val body = requestText(settings, path, "POST", "{}")
        val json = runCatching { JSONObject(body) }.getOrNull()
            ?: throw IOException("Mac 返回了无法识别的接入结果")
        val ok = json.optBoolean("ok", true)
        val message = json.optString("note")
            .ifBlank { json.optString("message") }
            .ifBlank { json.optString("error") }
        if (!ok) {
            throw IOException(message.ifBlank { "接入操作失败" })
        }
        AgentActionResult(ok = true, message = message)
    }

    suspend fun searchSessions(
        settings: AppSettings,
        query: String,
        scope: String = "content",
        limit: Int = 50,
    ): List<SessionSearchHit> = withContext(Dispatchers.IO) {
        val clean = query.trim()
        if (clean.isBlank()) return@withContext emptyList()
        val path = "/api/search?q=${URLEncoder.encode(clean, Charsets.UTF_8.name())}" +
            "&scope=${URLEncoder.encode(scope, Charsets.UTF_8.name())}" +
            "&limit=${limit.coerceIn(1, 200)}"
        val body = try {
            requestText(settings, path, "GET")
        } catch (error: AgentApiException) {
            if (error.statusCode in setOf(404, 405, 501)) {
                return@withContext emptyList()
            }
            throw error
        }
        runCatching { JSONObject(body) }.getOrNull()
            ?.let(SessionSearchHit::listFromJson)
            .orEmpty()
    }

    private fun parseInboxEntries(array: JSONArray?): List<InboxEntry> {
        if (array == null) return emptyList()
        return buildList {
            for (index in 0 until array.length()) {
                array.optJSONObject(index)?.let(InboxEntry::fromJson)?.let(::add)
            }
        }
    }

    suspend fun fetchSessionItems(
        settings: AppSettings,
        sessionId: String,
        limit: Int = 300,
        before: String? = null,
        includeReasoning: Boolean = false,
        source: String = "",
        anchorEventId: Long? = null,
    ): SessionItemsResult = withContext(Dispatchers.IO) {
        val encodedSession = URLEncoder.encode(sessionId, Charsets.UTF_8.name())
        val query = buildList {
            if (anchorEventId != null) {
                add("source=${URLEncoder.encode(source, Charsets.UTF_8.name())}")
                add("anchor_event_id=$anchorEventId")
                if (!before.isNullOrBlank()) add("turns=10")
            } else {
                add("limit=${limit.coerceIn(1, 2000)}")
            }
            before?.takeIf { it.isNotBlank() }?.let {
                add("before=${URLEncoder.encode(it, Charsets.UTF_8.name())}")
            }
            if (includeReasoning) add("include_reasoning=1")
        }.joinToString("&")
        val body = try {
            requestText(
                settings,
                "/api/sessions/$encodedSession/items?$query",
                "GET",
            )
        } catch (error: AgentApiException) {
            if (anchorEventId == null && (error.statusCode == 404 || error.statusCode == 501)) {
                return@withContext SessionItemsResult(available = false)
            }
            throw error
        }
        val root = JSONTokener(body).nextValue()
        if (anchorEventId != null && ((root as? JSONObject)?.optInt("history_version") != 1 ||
                root.optLong("anchor_event_id") != anchorEventId)) {
            return@withContext SessionItemsResult(
                available = false,
                reason = "电脑端尚未支持按收到消息载入上下文，请更新电脑端。",
                history = SessionHistoryState(managed = true, anchorEventId = anchorEventId,
                    status = "unsupported", reason = "server_upgrade_required"),
            )
        }
        val array = when (root) {
            is JSONArray -> root
            is JSONObject -> root.optJSONArray("items") ?: JSONArray()
            else -> JSONArray()
        }
        if (anchorEventId != null && root is JSONObject) {
            if (root.stringOrBlank("source") != source || root.stringOrBlank("session_id") != sessionId ||
                (0 until array.length()).any { array.optJSONObject(it)?.stringOrBlank("item_key").isNullOrBlank() }) {
                throw AgentApiException(502, "会话上下文响应身份不一致，已保留当前内容。")
            }
        }
        val turnsArray = (root as? JSONObject)?.optJSONArray("turns")
        val items = buildList {
            for (index in 0 until array.length()) {
                val item = array.optJSONObject(index)
                    ?.let { SessionItem.fromJson(it, fallbackId = "$sessionId-$index") }
                if (item != null) add(item)
            }
        }
        val turns = buildList {
            if (turnsArray != null) {
                for (index in 0 until turnsArray.length()) {
                    turnsArray.optJSONObject(index)
                        ?.let(SessionTurn::fromJson)
                        ?.let(::add)
                }
            }
        }
        SessionItemsResult(
            items = normalizeSessionItems(items),
            turns = turns,
            reason = (root as? JSONObject)?.optString("reason")
                ?.takeIf { it.isNotBlank() },
            available = true,
            history = if (anchorEventId != null && root is JSONObject) SessionHistoryState(
                managed = true, anchorEventId = anchorEventId,
                beforeCursor = root.stringOrBlank("before_cursor").takeIf { it.isNotBlank() },
                status = root.stringOrBlank("history_status").ifBlank { "unknown" },
                reason = root.stringOrBlank("history_reason").takeIf { it.isNotBlank() },
            ) else null,
            roundsReturned = (root as? JSONObject)?.optInt("rounds_returned", 0) ?: 0,
        )
    }

    suspend fun fetchSessionCommands(
        settings: AppSettings,
        sessionId: String,
    ): List<SessionCommand> = withContext(Dispatchers.IO) {
        if (sessionId.isBlank()) return@withContext emptyList()
        val encodedSession = URLEncoder.encode(sessionId, Charsets.UTF_8.name())
        val body = try {
            requestText(settings, "/api/sessions/$encodedSession/commands", "GET")
        } catch (error: AgentApiException) {
            if (error.statusCode in setOf(404, 405, 501)) return@withContext emptyList()
            throw error
        }
        val array = runCatching { JSONObject(body).optJSONArray("commands") }.getOrNull()
            ?: return@withContext emptyList()
        buildList {
            for (index in 0 until array.length()) {
                array.optJSONObject(index)
                    ?.let(SessionCommand::fromJson)
                    ?.let(::add)
            }
        }
    }

    suspend fun completeFiles(
        settings: AppSettings,
        cwd: String,
        prefix: String,
    ): List<CompletedPath> = withContext(Dispatchers.IO) {
        if (cwd.isBlank()) return@withContext emptyList()
        val query = "cwd=${URLEncoder.encode(cwd, Charsets.UTF_8.name())}" +
            "&prefix=${URLEncoder.encode(prefix, Charsets.UTF_8.name())}"
        val body = try {
            requestText(settings, "/api/fs/complete?$query", "GET")
        } catch (error: AgentApiException) {
            if (error.statusCode in setOf(400, 403, 404, 405, 501)) {
                return@withContext emptyList()
            }
            throw error
        }
        val array = runCatching { JSONObject(body).optJSONArray("items") }.getOrNull()
            ?: return@withContext emptyList()
        buildList {
            for (index in 0 until array.length()) {
                array.optJSONObject(index)
                    ?.let(CompletedPath::fromJson)
                    ?.let(::add)
            }
        }
    }

    suspend fun fetchProjects(settings: AppSettings): List<ProjectInfo> =
        withContext(Dispatchers.IO) {
            fetchProjectsBlocking(settings)
        }

    suspend fun fetchTranscript(
        settings: AppSettings,
        sessionId: String,
        limit: Int = 200,
    ): TranscriptPage = withContext(Dispatchers.IO) {
        val encodedSession = URLEncoder.encode(sessionId, Charsets.UTF_8.name())
        val body = try {
            requestText(
                settings,
                "/api/sessions/$encodedSession/transcript?limit=$limit",
                "GET",
            )
        } catch (error: AgentApiException) {
            if (error.statusCode == 404) return@withContext TranscriptPage()
            throw error
        }
        val root = JSONTokener(body).nextValue()
        val array = when (root) {
            is JSONArray -> root
            is JSONObject -> root.optJSONArray("messages")
                ?: root.optJSONArray("transcript")
                ?: JSONArray()
            else -> JSONArray()
        }
        val reason = (root as? JSONObject)?.optString("reason")
            ?.takeIf { it.isNotBlank() }
        TranscriptPage(
            messages = buildList {
                for (index in 0 until array.length()) {
                    val objectAtIndex = array.optJSONObject(index) ?: continue
                    val message = TranscriptMessage.fromJson(objectAtIndex)
                    if (message.text.isNotBlank() || message.attachments.isNotEmpty()) {
                        add(message)
                    }
                }
            },
            reason = reason,
        )
    }

    suspend fun fetchReplies(
        settings: AppSettings,
        sessionId: String,
    ): List<AgentReply> = withContext(Dispatchers.IO) {
        val encodedSession = URLEncoder.encode(sessionId, Charsets.UTF_8.name())
        val body = try {
            requestText(settings, "/api/replies?session_id=$encodedSession", "GET")
        } catch (error: AgentApiException) {
            if (error.statusCode == 404) {
                return@withContext emptyList()
            }
            throw error
        }

        val root = JSONTokener(body).nextValue()
        val array = when (root) {
            is JSONArray -> root
            is JSONObject -> root.optJSONArray("replies")
                ?: root.optJSONArray("items")
                ?: JSONArray()
            else -> JSONArray()
        }
        buildList {
            for (index in 0 until array.length()) {
                array.optJSONObject(index)?.let { add(parseReply(it)) }
            }
        }
    }

    suspend fun markRead(settings: AppSettings, ids: Collection<Long>) {
        if (ids.isEmpty()) return
        withContext(Dispatchers.IO) {
            val payload = JSONObject().put("ids", JSONArray(ids))
            requestText(settings, "/api/read", "POST", payload.toString())
        }
    }

    suspend fun reply(
        settings: AppSettings,
        eventId: Long?,
        sessionId: String,
        source: String,
        text: String,
        files: List<String> = emptyList(),
        model: String? = null,
        effort: String? = null,
        permissionMode: String? = null,
    ): ReplyReceipt = withContext(Dispatchers.IO) {
        val payload = buildReplyPayload(
            eventId,
            sessionId,
            source,
            text,
            files,
            model,
            effort,
            permissionMode,
        )
        val body = requestText(settings, "/api/reply", "POST", payload.toString())
        val json = runCatching { JSONObject(body) }.getOrElse { JSONObject() }
        ReplyReceipt(
            ok = json.optBoolean("ok", true),
            replyId = json.optLong("reply_id", 0L),
            status = ReplyStatus.fromWire(json.optString("status").ifBlank { "queued" }),
            message = json.optString("message").takeIf { it.isNotBlank() },
        )
    }

    suspend fun dispatch(
        settings: AppSettings,
        parentSessionId: String,
        parentSource: String,
        parentEventId: Long?,
        targetAgent: String,
        text: String,
        cwd: String,
    ): DispatchResult = withContext(Dispatchers.IO) {
        val payload = JSONObject()
            .put("parent_session_id", parentSessionId)
            .put("parent_source", parentSource)
            .put("target_agent", targetAgent)
            .put("text", text)
            .put("cwd", cwd)
            .put("mode", "parallel")
        if (parentEventId != null && parentEventId > 0) {
            payload.put("parent_event_id", parentEventId)
        }
        val body = requestText(
            settings = settings,
            path = "/api/dispatch",
            method = "POST",
            body = payload.toString(),
            readTimeoutMs = 35_000,
        )
        val json = runCatching { JSONObject(body) }.getOrElse { JSONObject() }
        val nested = json.optJSONObject("dispatch") ?: json
        DispatchResult(
            ok = json.optBoolean("ok", true),
            dispatch = AgentDispatch.fromJson(nested),
            error = json.stringOrBlank("error").takeIf { it.isNotBlank() },
        )
    }

    suspend fun compose(
        settings: AppSettings,
        sessionId: String,
        source: String,
        text: String,
        files: List<String> = emptyList(),
        model: String? = null,
        effort: String? = null,
        permissionMode: String? = null,
        workspace: String = "auto",
        returnMode: String = "card",
        mentionOverrides: List<ComposeMentionOverride> = emptyList(),
        dryRun: Boolean = false,
    ): ComposeResult = withContext(Dispatchers.IO) {
        val payload = JSONObject()
            .put("session_id", sessionId)
            .put("source", source)
            .put("text", text)
            .put(
                "files",
                JSONArray().apply {
                    files.filter { it.isNotBlank() }.forEach { put(it) }
                },
            )
            .put(
                "collab",
                JSONObject()
                    .put("context", JSONArray().put("recent").put("files"))
                    .put("workspace", workspace)
                    .put("return", returnMode),
            )
            .put("dry_run", dryRun)
        if (mentionOverrides.isNotEmpty()) {
            payload.put(
                "mentions",
                JSONArray().apply {
                    mentionOverrides.forEach { put(it.toJson()) }
                },
            )
        }
        model?.takeIf { it.isNotBlank() }?.let { payload.put("model", it) }
        effort?.takeIf { it.isNotBlank() }?.let { payload.put("effort", it) }
        permissionMode?.takeIf { it.isNotBlank() }
            ?.let { payload.put("permission_mode", it) }
        val body = requestText(
            settings = settings,
            path = "/api/compose",
            method = "POST",
            body = payload.toString(),
            readTimeoutMs = 35_000,
        )
        val json = runCatching { JSONObject(body) }.getOrElse {
            throw AgentApiException(0, "协作响应不是 JSON")
        }
        ComposeResult.fromJson(json)
    }

    suspend fun fetchDispatches(
        settings: AppSettings,
        parentSessionId: String = "",
        limit: Int = 200,
    ): List<AgentDispatch> = withContext(Dispatchers.IO) {
        val query = buildList {
            add("limit=${limit.coerceIn(1, 500)}")
            parentSessionId.takeIf { it.isNotBlank() }?.let {
                add("parent_session_id=${URLEncoder.encode(it, Charsets.UTF_8.name())}")
            }
        }.joinToString("&")
        val body = requestText(settings, "/api/dispatches?$query", "GET")
        val root = runCatching { JSONObject(body) }.getOrElse { JSONObject() }
        val array = root.optJSONArray("dispatches") ?: JSONArray()
        buildList {
            for (index in 0 until array.length()) {
                array.optJSONObject(index)?.let { item ->
                    val dispatch = AgentDispatch.fromJson(item)
                    if (dispatch.id.isNotBlank()) add(dispatch)
                }
            }
        }
    }

    suspend fun forwardDispatch(
        settings: AppSettings,
        dispatchId: String,
        text: String? = null,
    ): CommandResult = withContext(Dispatchers.IO) {
        val payload = JSONObject()
        text?.takeIf { it.isNotBlank() }?.let { payload.put("text", it) }
        val encoded = URLEncoder.encode(dispatchId, Charsets.UTF_8.name())
        val body = requestText(
            settings = settings,
            path = "/api/dispatches/$encoded/forward",
            method = "POST",
            body = payload.toString(),
            readTimeoutMs = 35_000,
        )
        val json = runCatching { JSONObject(body) }.getOrElse { JSONObject() }
        CommandResult(
            ok = json.optBoolean("ok", true),
            message = json.stringOrBlank("error").takeIf { it.isNotBlank() },
        )
    }

    suspend fun replyDispatch(
        settings: AppSettings,
        dispatchId: String,
        text: String,
    ): DispatchResult = withContext(Dispatchers.IO) {
        val encoded = URLEncoder.encode(dispatchId, Charsets.UTF_8.name())
        val body = requestText(
            settings = settings,
            path = "/api/dispatches/$encoded/reply",
            method = "POST",
            body = JSONObject().put("text", text).toString(),
            readTimeoutMs = 35_000,
        )
        val json = runCatching { JSONObject(body) }.getOrElse { JSONObject() }
        val nested = json.optJSONObject("dispatch") ?: json
        DispatchResult(
            ok = json.optBoolean("ok", true),
            dispatch = AgentDispatch.fromJson(nested),
            error = json.stringOrBlank("error").takeIf { it.isNotBlank() },
        )
    }

    suspend fun cancelDispatch(
        settings: AppSettings,
        dispatchId: String,
    ): DispatchResult = withContext(Dispatchers.IO) {
        val encoded = URLEncoder.encode(dispatchId, Charsets.UTF_8.name())
        val body = requestText(
            settings = settings,
            path = "/api/dispatches/$encoded/cancel",
            method = "POST",
            body = "{}",
        )
        val json = runCatching { JSONObject(body) }.getOrElse { JSONObject() }
        val nested = json.optJSONObject("dispatch") ?: json
        DispatchResult(
            ok = json.optBoolean("ok", true),
            dispatch = AgentDispatch.fromJson(nested),
            error = json.stringOrBlank("error").takeIf { it.isNotBlank() },
        )
    }

    suspend fun discardDispatchWorktree(
        settings: AppSettings,
        dispatchId: String,
    ): DispatchResult = withContext(Dispatchers.IO) {
        val encoded = URLEncoder.encode(dispatchId, Charsets.UTF_8.name())
        val body = requestText(
            settings = settings,
            path = "/api/dispatches/$encoded/discard",
            method = "POST",
            body = JSONObject().put("confirm", true).toString(),
        )
        val json = runCatching { JSONObject(body) }.getOrElse { JSONObject() }
        val nested = json.optJSONObject("dispatch") ?: json
        DispatchResult(
            ok = json.optBoolean("ok", true),
            dispatch = AgentDispatch.fromJson(nested),
            error = json.stringOrBlank("error").takeIf { it.isNotBlank() },
        )
    }

    suspend fun upload(
        settings: AppSettings,
        eventId: Long,
        file: File,
        name: String,
        mime: String,
        onProgress: (Float) -> Unit = {},
    ): UploadResult = uploadTo(
        settings = settings,
        query = "event_id=$eventId&name=${URLEncoder.encode(name, Charsets.UTF_8.name())}",
        file = file,
        name = name,
        mime = mime,
        onProgress = onProgress,
    )

    suspend fun uploadForCwd(
        settings: AppSettings,
        cwd: String,
        file: File,
        name: String,
        mime: String,
        onProgress: (Float) -> Unit = {},
    ): UploadResult = uploadTo(
        settings = settings,
        query = "cwd=${URLEncoder.encode(cwd, Charsets.UTF_8.name())}" +
            "&name=${URLEncoder.encode(name, Charsets.UTF_8.name())}",
        file = file,
        name = name,
        mime = mime,
        onProgress = onProgress,
    )

    private suspend fun uploadTo(
        settings: AppSettings,
        query: String,
        file: File,
        name: String,
        mime: String,
        onProgress: (Float) -> Unit,
    ): UploadResult = withContext(Dispatchers.IO) {
        val router = transportRouter
        if (router != null) {
            router.ensureRoute()
            if (router.kind.value == TransportKind.CLOUD) {
                if (file.length() > CLOUD_FILE_LIMIT_BYTES) {
                    throw CloudRpcException("單文件上限 20 MB")
                }
                val bytes = file.readBytes()
                val json = router.upload("/api/upload?$query", bytes, name)
                onProgress(1f)
                return@withContext UploadResult(
                    ok = json.optBoolean("ok", true),
                    path = json.optString("path"),
                    name = json.optString("name", name),
                    size = json.optLong("size", bytes.size.toLong()),
                    mime = json.optString("mime", mime),
                )
            }
        }
        val connection = URL(
            "${settings.baseUrl.trimEnd('/')}/api/upload?$query",
        ).openConnection() as HttpURLConnection
        try {
            connection.requestMethod = "POST"
            connection.connectTimeout = 10_000
            connection.readTimeout = 180_000
            connection.useCaches = false
            connection.doOutput = true
            connection.setRequestProperty("Authorization", "Bearer ${settings.token}")
            connection.setRequestProperty("Accept", "application/json")
            connection.setRequestProperty(
                "Content-Type",
                mime.ifBlank { "application/octet-stream" },
            )
            connection.setFixedLengthStreamingMode(file.length())

            val total = file.length().coerceAtLeast(1L)
            var copied = 0L
            val progressOut = object : FilterOutputStream(connection.outputStream) {
                override fun write(b: Int) {
                    out.write(b)
                    copied += 1
                    onProgress((copied.toFloat() / total).coerceIn(0f, 1f))
                }

                override fun write(buffer: ByteArray, offset: Int, length: Int) {
                    out.write(buffer, offset, length)
                    copied += length
                    onProgress((copied.toFloat() / total).coerceIn(0f, 1f))
                }

                override fun close() {
                    flush()
                    out.close()
                }
            }
            file.inputStream().use { input ->
                progressOut.use { output -> input.copyTo(output, DEFAULT_BUFFER_SIZE * 4) }
            }
            onProgress(1f)

            val code = connection.responseCode
            val stream = if (code in 200..299) connection.inputStream else connection.errorStream
            val responseBody = stream
                ?.bufferedReader(Charsets.UTF_8)
                ?.use { it.readText() }
                .orEmpty()
            if (code !in 200..299) {
                val message = runCatching {
                    JSONObject(responseBody).optString("error")
                }.getOrNull()?.takeIf { it.isNotBlank() }
                    ?: responseBody.take(240).ifBlank { "HTTP $code" }
                throw AgentApiException(code, message)
            }
            val json = JSONObject(responseBody)
            UploadResult(
                ok = json.optBoolean("ok", true),
                path = json.optString("path"),
                name = json.optString("name", name),
                size = json.optLong("size", file.length()),
                mime = json.optString("mime", mime),
            )
        } finally {
            connection.disconnect()
        }
    }

    suspend fun createSession(
        settings: AppSettings,
        source: String,
        cwd: String,
        text: String,
        files: List<String> = emptyList(),
        model: String? = null,
        effort: String? = null,
        permissionMode: String? = null,
    ): NewSessionResult = withContext(Dispatchers.IO) {
        val payload = JSONObject()
            .put("source", source)
            .put("cwd", cwd)
            .put("text", text)
            .put("files", JSONArray(files))
        model?.takeIf { it.isNotBlank() }?.let { payload.put("model", it) }
        effort?.takeIf { it.isNotBlank() }?.let { payload.put("effort", it) }
        permissionMode?.takeIf { it.isNotBlank() }
            ?.let { payload.put("permission_mode", it) }
        try {
            val body = requestText(settings, "/api/sessions/new", "POST", payload.toString())
            val json = JSONObject(body)
            NewSessionResult(
                ok = json.optBoolean("ok", true),
                sessionId = json.stringOrBlank("session_id").takeIf { it.isNotBlank() },
                status = json.stringOrBlank("status").ifBlank { "started" },
                error = json.stringOrBlank("error").takeIf { it.isNotBlank() },
                warning = json.stringOrBlank("warning").takeIf { it.isNotBlank() },
                requestId = json.stringOrBlank("request_id").takeIf { it.isNotBlank() },
                cwd = json.stringOrBlank("cwd").ifBlank { cwd },
            )
        } catch (error: AgentApiException) {
            NewSessionResult(
                ok = false,
                sessionId = null,
                status = "failed",
                error = error.message ?: "启动失败",
                cwd = cwd,
            )
        }
    }

    suspend fun getAway(settings: AppSettings): Boolean = withContext(Dispatchers.IO) {
        val body = try {
            requestText(settings, "/api/away", "GET")
        } catch (error: AgentApiException) {
            if (error.statusCode == 404) return@withContext false
            throw error
        }
        val json = runCatching { JSONObject(body) }.getOrElse { JSONObject() }
        json.optBoolean("enabled", false)
    }

    suspend fun setAway(settings: AppSettings, enabled: Boolean): Boolean =
        withContext(Dispatchers.IO) {
            val payload = JSONObject().put("enabled", enabled)
            val body = requestText(settings, "/api/away", "POST", payload.toString())
            val json = runCatching { JSONObject(body) }.getOrElse { JSONObject() }
            json.optBoolean("enabled", enabled)
        }

    fun openStreamConnection(settings: AppSettings): HttpURLConnection {
        return openStreamConnection(settings, sessionId = null)
    }

    fun openStreamConnection(
        settings: AppSettings,
        sessionId: String?,
    ): HttpURLConnection {
        if (settings.token.isBlank()) {
            throw IOException("Token 不能为空")
        }
        val query = sessionId?.takeIf { it.isNotBlank() }?.let {
            "?session_id=${URLEncoder.encode(it, Charsets.UTF_8.name())}"
        }.orEmpty()
        val connection = URL("${settings.baseUrl.trimEnd('/')}/api/stream$query")
            .openConnection() as HttpURLConnection
        connection.requestMethod = "GET"
        connection.connectTimeout = 10_000
        connection.readTimeout = 60_000
        connection.useCaches = false
        connection.setRequestProperty("Authorization", "Bearer ${settings.token}")
        connection.setRequestProperty("Accept", "text/event-stream")
        connection.setRequestProperty("Cache-Control", "no-cache")
        return connection
    }

    suspend fun stopSession(
        settings: AppSettings,
        sessionId: String,
    ): CommandResult = withContext(Dispatchers.IO) {
        val encodedSession = URLEncoder.encode(sessionId, Charsets.UTF_8.name())
        try {
            val body = requestText(
                settings,
                "/api/sessions/$encodedSession/stop",
                "POST",
                "{}",
            )
            val json = runCatching { JSONObject(body) }.getOrElse { JSONObject() }
            CommandResult(
                ok = json.optBoolean("ok", true),
                message = json.stringOrBlank("message").takeIf { it.isNotBlank() },
            )
        } catch (error: Exception) {
            CommandResult(ok = false, message = error.message ?: error.javaClass.simpleName)
        }
    }

    suspend fun decideApproval(
        settings: AppSettings,
        eventId: Long,
        decision: String,
        optionId: String? = null,
    ): CommandResult = withContext(Dispatchers.IO) {
        val payload = JSONObject().put("decision", decision)
        optionId?.takeIf { it.isNotBlank() }?.let { payload.put("option_id", it) }
        try {
            val body = requestText(
                settings,
                "/api/approvals/$eventId",
                "POST",
                payload.toString(),
            )
            val json = runCatching { JSONObject(body) }.getOrElse { JSONObject() }
            CommandResult(
                ok = json.optBoolean("ok", true),
                message = json.stringOrBlank("message").takeIf { it.isNotBlank() },
            )
        } catch (error: Exception) {
            CommandResult(ok = false, message = error.message ?: error.javaClass.simpleName)
        }
    }

    suspend fun reportClientLog(
        settings: AppSettings,
        payload: JSONObject,
    ): Boolean = withContext(Dispatchers.IO) {
        try {
            requestText(
                settings,
                "/api/client-log",
                "POST",
                payload.toString(),
            )
            true
        } catch (error: AgentApiException) {
            if (error.statusCode == 404 || error.statusCode == 501) {
                true
            } else {
                throw error
            }
        }
    }

    fun attachmentUrl(settings: AppSettings, attachment: Attachment): String {
        if (attachment.url.startsWith("http://") || attachment.url.startsWith("https://")) {
            return attachment.url
        }
        return "${settings.baseUrl.trimEnd('/')}/${attachment.url.trimStart('/')}"
    }

    fun proxyUrl(
        settings: AppSettings,
        port: Int,
        path: String = "/",
        capability: String? = null,
    ): String {
        val base = "${settings.baseUrl.trimEnd('/')}/p/$port/${path.trimStart('/')}"
        if (capability.isNullOrBlank()) return base
        val separator = if (base.contains('?')) "&" else "?"
        return "$base${separator}cap=$capability"
    }

    private suspend fun fetchEventsBlocking(
        settings: AppSettings,
        since: Long,
    ): List<AgentEvent> {
        val body = requestText(settings, "/api/events?since=$since", "GET")
        val array = JSONArray(body)
        return buildList {
            for (index in 0 until array.length()) {
                val objectAtIndex = array.optJSONObject(index) ?: continue
                val event = AgentEvent.fromJson(objectAtIndex)
                if (event.id > 0) add(event)
            }
        }
    }

    private suspend fun fetchProjectsBlocking(settings: AppSettings): List<ProjectInfo> {
        val body = try {
            requestText(settings, "/api/projects", "GET")
        } catch (error: AgentApiException) {
            if (error.statusCode == 404) return emptyList()
            throw error
        }
        val root = JSONTokener(body).nextValue()
        val array = when (root) {
            is JSONArray -> root
            is JSONObject -> root.optJSONArray("projects") ?: JSONArray()
            else -> JSONArray()
        }
        return buildList {
            for (index in 0 until array.length()) {
                val objectAtIndex = array.optJSONObject(index) ?: continue
                val project = ProjectInfo.fromJson(objectAtIndex)
                if (project.cwd.isNotBlank()) add(project)
            }
        }
    }

    private fun parseReply(json: JSONObject): AgentReply {
        val status = when {
            json.has("status") -> ReplyStatus.fromWire(json.stringOrBlank("status"))
            json.optBoolean("injected", false) -> ReplyStatus.INJECTED
            else -> ReplyStatus.QUEUED
        }
        val files = json.optJSONArray("files")
        return AgentReply(
            id = json.optLong("id", json.optLong("reply_id", 0L)),
            eventId = json.optLong("event_id", 0L),
            sessionId = json.stringOrBlank("session_id"),
            text = json.stringOrBlank("text"),
            ts = json.stringOrBlank("ts").ifBlank { Instant.now().toString() },
            status = status,
            message = json.stringOrBlank("message").takeIf { it.isNotBlank() },
            isLocal = false,
            files = buildList {
                if (files != null) {
                    for (index in 0 until files.length()) {
                        files.optString(index).takeIf { it.isNotBlank() }?.let(::add)
                    }
                }
            },
        )
    }

    /** 会议室接口（/api/rooms）共用的请求入口，解析在 Rooms.kt。走同一套局域网 / 云端传输。 */
    internal suspend fun roomRequest(
        settings: AppSettings,
        path: String,
        method: String,
        body: String? = null,
    ): String = withContext(Dispatchers.IO) {
        requestText(
            settings = settings,
            path = path,
            method = method,
            body = body,
            readTimeoutMs = 30_000,
        )
    }

    private suspend fun requestText(
        settings: AppSettings,
        path: String,
        method: String,
        body: String? = null,
        readTimeoutMs: Int = 20_000,
    ): String {
        val router = transportRouter
        val activeSettings = activeSettingsProvider?.invoke()
        if (router != null && (activeSettings == null || activeSettings == settings)) {
            val parsedBody = body?.let {
                runCatching { JSONTokener(it).nextValue() as? JSONObject }.getOrNull()
            }
            val response = router.request(method, path, parsedBody)
            if (response.status !in 200..299) {
                val message = (response.body as? JSONObject)
                    ?.optString("error")
                    ?.takeIf { it.isNotBlank() }
                    ?: response.raw.take(240)
                        .ifBlank { "HTTP ${response.status}" }
                throw AgentApiException(response.status, message)
            }
            return response.raw
        }
        if (settings.token.isBlank()) {
            throw IOException("Token 不能为空")
        }
        val connection = URL("${settings.baseUrl.trimEnd('/')}$path")
            .openConnection() as HttpURLConnection
        try {
            connection.requestMethod = method
            connection.connectTimeout = 6_000
            connection.readTimeout = readTimeoutMs
            connection.useCaches = false
            connection.setRequestProperty("Authorization", "Bearer ${settings.token}")
            connection.setRequestProperty("Accept", "application/json")

            if (body != null) {
                val bytes = body.toByteArray(Charsets.UTF_8)
                connection.doOutput = true
                connection.setRequestProperty("Content-Type", "application/json; charset=utf-8")
                connection.setFixedLengthStreamingMode(bytes.size)
                connection.outputStream.use { it.write(bytes) }
            }

            val code = connection.responseCode
            val stream = if (code in 200..299) connection.inputStream else connection.errorStream
            val responseBody = stream
                ?.bufferedReader(Charsets.UTF_8)
                ?.use { it.readText() }
                .orEmpty()
            if (code !in 200..299) {
                val message = runCatching {
                    JSONObject(responseBody).optString("error")
                }.getOrNull()?.takeIf { it.isNotBlank() }
                    ?: responseBody.take(240).ifBlank { "HTTP $code" }
                throw AgentApiException(code, message)
            }
            return responseBody
        } finally {
            connection.disconnect()
        }
    }

    private const val DEFAULT_BUFFER_SIZE = 8 * 1024
    private const val CLOUD_FILE_LIMIT_BYTES = 20 * 1024 * 1024
}

internal fun parseAgentDirectory(body: String): AgentDirectory {
    val root = runCatching { JSONObject(body) }.getOrNull()
        ?: return AgentDirectory()
    val agents = linkedMapOf<String, AgentProfile>()

    fun addAgent(sourceHint: String, json: JSONObject) {
        val source = json.stringOrBlank("source")
            .ifBlank { json.stringOrBlank("id") }
            .ifBlank { sourceHint }
            .lowercase()
        if (source.isBlank()) return
        val modelsArray = json.optJSONArray("models")
        val models = buildList {
            if (modelsArray != null) {
                for (index in 0 until modelsArray.length()) {
                    modelsArray.optJSONObject(index)
                        ?.let(AgentModel::fromJson)
                        ?.let(::add)
                }
            }
        }.distinctBy { it.id }
        val defaultModel = json.stringOrBlank("default_model")
            .ifBlank { models.firstOrNull { it.isDefault }?.id.orEmpty() }
        agents[source] = AgentProfile(
            source = source,
            displayName = json.stringOrBlank("display_name")
                .ifBlank { json.stringOrBlank("name") },
            icon = json.stringOrBlank("icon"),
            available = json.optBoolean("available", false),
            installed = json.optBoolean("installed", false),
            capabilities = AgentCapabilities.fromJson(
                json.optJSONObject("capabilities"),
            ),
            models = models.map { model ->
                if (model.isDefault || model.id == defaultModel) {
                    model.copy(isDefault = true)
                } else {
                    model
                }
            },
            defaultModel = defaultModel,
        )
    }

    val array = root.optJSONArray("agents")
    if (array != null) {
        for (index in 0 until array.length()) {
            array.optJSONObject(index)?.let { addAgent("", it) }
        }
    } else {
        val map = root.optJSONObject("agents") ?: root
        val keys = map.keys()
        while (keys.hasNext()) {
            val key = keys.next()
            map.optJSONObject(key)?.let { addAgent(key, it) }
        }
    }
    return AgentDirectory(agents = agents, available = agents.isNotEmpty())
}

internal fun buildReplyPayload(
    eventId: Long?,
    sessionId: String,
    source: String,
    text: String,
    files: List<String>,
    model: String? = null,
    effort: String? = null,
    permissionMode: String? = null,
): JSONObject {
    return JSONObject().apply {
        if (eventId != null && eventId > 0) {
            put("event_id", eventId)
        } else {
            put("session_id", sessionId)
            put("source", source)
        }
        put("text", text)
        put("files", JSONArray(files))
        model?.takeIf { it.isNotBlank() }?.let { put("model", it) }
        effort?.takeIf { it.isNotBlank() }?.let { put("effort", it) }
        permissionMode?.takeIf { it.isNotBlank() }?.let { put("permission_mode", it) }
    }
}
