package dev.agentpush.poc

import org.json.JSONArray
import org.json.JSONObject
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import kotlin.math.max

enum class AgentBrand(
    val wire: String,
    val displayName: String,
    val initial: String,
) {
    CLAUDE("claude", "Claude", "C"),
    CODEX("codex", "Codex", "C"),
    WORKBUDDY("workbuddy", "WorkBuddy", "W"),
    UNKNOWN("unknown", "Agent", "A");

    companion object {
        fun fromWire(value: String): AgentBrand {
            return entries.firstOrNull { it.wire == value.lowercase() } ?: UNKNOWN
        }
    }
}

enum class ReplyStatus(val wire: String, val label: String) {
    QUEUED("queued", "排队中"),
    HELD("held", "已交给钩子"),
    INJECTED("injected", "已注入"),
    FAILED("failed", "失败");

    companion object {
        fun fromWire(value: String?): ReplyStatus {
            return entries.firstOrNull { it.wire == value?.lowercase() } ?: QUEUED
        }
    }
}

enum class SessionStatus(val wire: String, val label: String) {
    RUNNING("running", "运行中"),
    WAITING_INPUT("waiting_input", "等待输入"),
    NEEDS_PERMISSION("needs_permission", "需要授权"),
    ERROR("error", "出错"),
    COMPLETED("completed", "完成"),
    IDLE("idle", "待命");

    companion object {
        fun fromWire(value: String?): SessionStatus? {
            val normalized = value?.lowercase()?.replace('-', '_')?.replace(' ', '_') ?: return null
            return when (normalized) {
                "running", "busy", "working", "in_progress" -> RUNNING
                "waiting_input", "needs_input", "waiting" -> WAITING_INPUT
                "needs_permission", "permission", "permission_prompt" -> NEEDS_PERMISSION
                "error", "failed", "failure" -> ERROR
                "completed", "complete", "done", "reply" -> COMPLETED
                "idle", "ready" -> IDLE
                else -> entries.firstOrNull { it.wire == normalized }
            }
        }

        fun fromKind(kind: String?): SessionStatus? {
            return when (kind) {
                "permission" -> NEEDS_PERMISSION
                "needs_input" -> WAITING_INPUT
                "error" -> ERROR
                "reply" -> COMPLETED
                else -> null
            }
        }
    }
}

enum class ConnectionState {
    CONNECTED,
    RECONNECTING,
    DISCONNECTED,
}

data class ConnectionDiagnostics(
    val lastConnectedAt: Long? = null,
    val lastDisconnectedAt: Long? = null,
    val lastDisconnectReason: String? = null,
)

data class Attachment(
    val id: String,
    val name: String,
    val mime: String,
    val size: Long,
    val kind: String,
    val url: String,
) {
    val isImage: Boolean
        get() = kind == "image" || mime.startsWith("image/")

    val isHtml: Boolean
        get() = kind == "html" || mime.contains("html")

    val isPdf: Boolean
        get() = kind == "pdf" || mime == "application/pdf"

    fun sizeLabel(): String {
        if (size <= 0) return ""
        return when {
            size < 1024 -> "$size B"
            size < 1024 * 1024 -> "${size / 1024} KB"
            else -> String.format("%.1f MB", size / 1024.0 / 1024.0)
        }
    }

    companion object {
        fun fromJson(json: JSONObject): Attachment {
            val name = json.stringOrBlank("name").ifBlank { "附件" }
            val mime = json.stringOrBlank("mime")
            val declaredKind = json.stringOrBlank("kind")
            val kind = declaredKind.ifBlank {
                when {
                    mime.startsWith("image/") -> "image"
                    mime.contains("html") -> "html"
                    mime == "application/pdf" -> "pdf"
                    name.endsWith(".html", true) || name.endsWith(".htm", true) -> "html"
                    name.endsWith(".pdf", true) -> "pdf"
                    else -> "file"
                }
            }
            return Attachment(
                id = json.stringOrBlank("id").ifBlank { "$name-$mime" },
                name = name,
                mime = mime.ifBlank { "application/octet-stream" },
                size = json.optLong("size", 0L),
                kind = kind,
                url = json.stringOrBlank("url"),
            )
        }

        fun listFromJson(array: JSONArray?): List<Attachment> {
            if (array == null) return emptyList()
            return buildList {
                for (index in 0 until array.length()) {
                    array.optJSONObject(index)?.let { add(fromJson(it)) }
                }
            }
        }
    }
}

data class AgentEvent(
    val id: Long,
    val ts: String,
    val source: String,
    val kind: String,
    val sessionId: String,
    val cwd: String,
    val project: String,
    val title: String,
    val text: String,
    val read: Boolean,
    val attachments: List<Attachment> = emptyList(),
    val localhostLinks: List<Int> = emptyList(),
    val sessionMeta: SessionMeta? = null,
) {
    val brand: AgentBrand
        get() = AgentBrand.fromWire(source)

    val isPermission: Boolean
        get() = kind == "permission"

    val isPending: Boolean
        get() = kind == "permission" || kind == "needs_input"

    val sessionKey: String
        get() = if (sessionId.isNotBlank()) {
            sessionId
        } else {
            "local:${source}:${cwd.ifBlank { project }}"
        }

    fun shortSummary(limit: Int = 180): String {
        return text
            .replace(Regex("```[\\s\\S]*?```"), " [代码] ")
            .replace(Regex("[#>*_`\\[\\]]"), "")
            .replace(Regex("\\s+"), " ")
            .trim()
            .take(limit)
    }

    fun toJson(): JSONObject {
        return JSONObject()
            .put("id", id)
            .put("ts", ts)
            .put("source", source)
            .put("kind", kind)
            .put("session_id", sessionId)
            .put("cwd", cwd)
            .put("project", project)
            .put("title", title)
            .put("text", text)
            .put("read", read)
            .put("session_meta", sessionMeta?.toJson())
            .put(
                "attachments",
                JSONArray().apply {
                    attachments.forEach { attachment ->
                        put(
                            JSONObject()
                                .put("id", attachment.id)
                                .put("name", attachment.name)
                                .put("mime", attachment.mime)
                                .put("size", attachment.size)
                                .put("kind", attachment.kind)
                                .put("url", attachment.url),
                        )
                    }
                },
            )
    }

    companion object {
        fun fromJson(json: JSONObject): AgentEvent {
            val cwd = json.stringOrBlank("cwd")
            val links = json.optJSONArray("localhost_links")
            val source = json.stringOrBlank("source")
            val sessionId = json.stringOrBlank("session_id")
            val project = json.stringOrBlank("project").ifBlank { projectFromCwd(cwd) }
            val nestedMeta = json.optJSONObject("session_meta")
            return AgentEvent(
                id = json.optLong("id", 0L),
                ts = json.stringOrBlank("ts").ifBlank { Instant.now().toString() },
                source = source,
                kind = json.stringOrBlank("kind").ifBlank { "reply" },
                sessionId = sessionId,
                cwd = cwd,
                project = project,
                title = json.stringOrBlank("title"),
                text = json.stringOrBlank("text"),
                read = json.optBoolean("read", false),
                attachments = Attachment.listFromJson(json.optJSONArray("attachments")),
                sessionMeta = nestedMeta?.let {
                    SessionMeta.fromJson(
                        json = it,
                        fallbackSessionId = sessionId,
                        fallbackSource = source,
                        fallbackCwd = cwd,
                        fallbackProject = project,
                    )
                },
                localhostLinks = buildList {
                    if (links != null) {
                        for (index in 0 until links.length()) {
                            links.optInt(index).takeIf { it > 0 }?.let(::add)
                        }
                    }
                },
            )
        }
    }
}

data class AgentReply(
    val id: Long,
    val eventId: Long,
    val sessionId: String,
    val text: String,
    val ts: String,
    val status: ReplyStatus,
    val message: String? = null,
    val isLocal: Boolean = false,
    val files: List<String> = emptyList(),
    val model: String = "",
    val effort: String = "",
)

data class ReplyReceipt(
    val ok: Boolean,
    val replyId: Long,
    val status: ReplyStatus,
    val message: String? = null,
)

data class AgentCapabilities(
    val reply: Boolean = true,
    val newSession: Boolean = true,
    val switchModel: Boolean = false,
    val effort: Boolean = false,
    val stop: Boolean = false,
    val approve: Boolean = true,
    val live: Boolean = true,
) {
    companion object {
        fun fromJson(json: JSONObject?): AgentCapabilities {
            if (json == null) return AgentCapabilities()
            return AgentCapabilities(
                reply = json.optBoolean("reply", true),
                newSession = json.optBoolean("new_session", true),
                switchModel = json.optBoolean("switch_model", false),
                effort = json.optBoolean("effort", false),
                stop = json.optBoolean("stop", false),
                approve = json.optBoolean("approve", true),
                live = json.optBoolean("live", true),
            )
        }
    }
}

data class AgentModel(
    val id: String,
    val label: String,
    val provider: String,
    val isDefault: Boolean,
    val efforts: List<String>,
) {
    val displayName: String
        get() = label.ifBlank { id }

    companion object {
        fun fromJson(json: JSONObject): AgentModel? {
            val id = json.stringOrBlank("id")
                .ifBlank { json.stringOrBlank("model") }
                .ifBlank { json.stringOrBlank("name") }
            if (id.isBlank()) return null
            val efforts = json.optJSONArray("efforts")
            return AgentModel(
                id = id,
                label = json.stringOrBlank("label")
                    .ifBlank { json.stringOrBlank("name") }
                    .ifBlank { id },
                provider = json.stringOrBlank("provider")
                    .ifBlank { id.substringBefore('/') }
                    .ifBlank { "默认" },
                isDefault = json.optBoolean(
                    "is_default",
                    json.optBoolean("default", false),
                ),
                efforts = buildList {
                    if (efforts != null) {
                        for (index in 0 until efforts.length()) {
                            efforts.optString(index)
                                .takeIf { it.isNotBlank() }
                                ?.let(::add)
                        }
                    }
                }.distinct(),
            )
        }
    }
}

data class AgentProfile(
    val source: String,
    val capabilities: AgentCapabilities = AgentCapabilities(),
    val models: List<AgentModel> = emptyList(),
    val defaultModel: String = "",
) {
    val brand: AgentBrand
        get() = AgentBrand.fromWire(source)

    val defaultModelOption: AgentModel?
        get() = models.firstOrNull { it.isDefault }
            ?: models.firstOrNull { it.id == defaultModel }
            ?: models.firstOrNull()
}

data class AgentDirectory(
    val agents: Map<String, AgentProfile> = emptyMap(),
    val available: Boolean = false,
) {
    fun profile(source: String): AgentProfile {
        return agents[source.lowercase()]
            ?: AgentProfile(source = source.lowercase())
    }
}

data class ContextUsage(
    val used: Long = 0L,
    val limit: Long = 0L,
) {
    val fraction: Float
        get() = if (limit > 0) (used.toFloat() / limit.toFloat()).coerceIn(0f, 1f) else 0f

    val percent: Int
        get() = (fraction * 100).toInt()

    val usedLabel: String
        get() = formatTokenCount(used)

    val limitLabel: String
        get() = formatTokenCount(limit)
}

fun formatTokenCount(value: Long): String {
    return when {
        value <= 0 -> "0"
        value < 1_000 -> value.toString()
        value < 1_000_000 -> String.format("%.1fk", value / 1_000.0)
        else -> String.format("%.2fm", value / 1_000_000.0)
    }
}

data class SessionMeta(
    val sessionId: String,
    val source: String = "",
    val title: String = "",
    val titleSource: String = "",
    val workspace: String = "",
    val project: String = "",
    val branch: String = "",
    val cwd: String = "",
    val model: String = "",
    val status: String = "",
    val unread: Int = -1,
    val lastTs: String = "",
    val lastPreview: String = "",
    val eventCount: Int = 0,
    val requestId: String = "",
    val effort: String = "",
    val permissionMode: String = "",
    val contextUsage: ContextUsage? = null,
    val running: Boolean? = null,
    val runningSince: String = "",
    val read: Boolean? = null,
    val pinned: Boolean? = null,
    val muted: Boolean? = null,
    val archived: Boolean? = null,
    val isTemp: Boolean? = null,
    val isEmpty: Boolean? = null,
) {
    val brand: AgentBrand
        get() = AgentBrand.fromWire(source)

    fun toJson(): JSONObject {
        return JSONObject()
            .put("session_id", sessionId)
            .put("source", source)
            .put("title", title)
            .put("title_source", titleSource)
            .put("workspace", workspace)
            .put("project", project)
            .put("branch", branch)
            .put("cwd", cwd)
            .put("model", model)
            .put("status", status)
            .put("unread", unread)
            .put("last_ts", lastTs)
            .put("last_preview", lastPreview)
            .put("event_count", eventCount)
            .put("request_id", requestId)
            .put("effort", effort)
            .put("permission_mode", permissionMode)
            .put("running", running)
            .put("running_since", runningSince)
            .put("read", read)
            .put("pinned", pinned)
            .put("muted", muted)
            .put("archived", archived)
            .put("is_temp", isTemp)
            .put("is_empty", isEmpty)
            .put(
                "context_usage",
                contextUsage?.let {
                    JSONObject()
                        .put("used", it.used)
                        .put("limit", it.limit)
                },
            )
    }

    companion object {
        fun fromJson(
            json: JSONObject,
            fallbackSessionId: String = "",
            fallbackSource: String = "",
            fallbackCwd: String = "",
            fallbackProject: String = "",
        ): SessionMeta {
            val cwd = json.stringOrBlank("cwd").ifBlank { fallbackCwd }
            val context = json.optJSONObject("context_usage")
            return SessionMeta(
                sessionId = json.stringOrBlank("session_id")
                    .ifBlank { json.stringOrBlank("id") }
                    .ifBlank { fallbackSessionId },
                source = json.stringOrBlank("source").ifBlank { fallbackSource },
                title = json.stringOrBlank("title"),
                titleSource = json.stringOrBlank("title_source"),
                workspace = json.stringOrBlank("workspace"),
                project = json.stringOrBlank("project")
                    .ifBlank { fallbackProject }
                    .ifBlank { projectFromCwd(cwd) },
                branch = json.stringOrBlank("branch"),
                cwd = cwd,
                model = json.stringOrBlank("model"),
                status = json.stringOrBlank("status"),
                unread = json.optInt("unread", -1),
                lastTs = json.stringOrBlank("last_ts"),
                lastPreview = json.stringOrBlank("last_preview"),
                eventCount = json.optInt("event_count", 0),
                requestId = json.stringOrBlank("request_id"),
                effort = json.stringOrBlank("effort"),
                permissionMode = json.stringOrBlank("permission_mode"),
                contextUsage = context?.let {
                    ContextUsage(
                        used = it.optLong("used", 0L),
                        limit = it.optLong("limit", 0L),
                    )
                },
                running = if (json.has("running") && !json.isNull("running")) {
                    json.optBoolean("running", false)
                } else {
                    null
                },
                runningSince = json.stringOrBlank("running_since"),
                read = json.booleanOrNull("read"),
                pinned = json.booleanOrNull("pinned"),
                muted = json.booleanOrNull("muted"),
                archived = json.booleanOrNull("archived"),
                isTemp = json.booleanOrNull("is_temp"),
                isEmpty = json.booleanOrNull("is_empty"),
            )
        }
    }
}

data class ProjectInfo(
    val cwd: String,
    val project: String,
    val count: Int,
    val lastTs: String,
    val sources: List<String>,
) {
    companion object {
        fun fromJson(json: JSONObject): ProjectInfo {
            val sources = json.optJSONArray("sources")
            return ProjectInfo(
                cwd = json.stringOrBlank("cwd"),
                project = json.stringOrBlank("project")
                    .ifBlank { projectFromCwd(json.stringOrBlank("cwd")) },
                count = json.optInt("count", 0),
                lastTs = json.stringOrBlank("last_ts"),
                sources = buildList {
                    if (sources != null) {
                        for (index in 0 until sources.length()) {
                            sources.optString(index).takeIf { it.isNotBlank() }?.let(::add)
                        }
                    }
                },
            )
        }
    }
}

data class TranscriptMessage(
    val role: String,
    val text: String,
    val ts: String,
    val attachments: List<Attachment> = emptyList(),
) {
    val isUser: Boolean
        get() = role == "user"

    val isAssistant: Boolean
        get() = role == "assistant"

    companion object {
        fun fromJson(json: JSONObject): TranscriptMessage {
            return TranscriptMessage(
                role = json.stringOrBlank("role").ifBlank { "assistant" },
                text = json.stringOrBlank("text"),
                ts = json.stringOrBlank("ts"),
                attachments = Attachment.listFromJson(json.optJSONArray("attachments")),
            )
        }
    }
}

data class TranscriptPage(
    val messages: List<TranscriptMessage> = emptyList(),
    val reason: String? = null,
) {
    val firstUserQuestion: String?
        get() = messages.firstOrNull { it.isUser && it.text.isNotBlank() }?.text
}

data class SessionSummary(
    val sessionId: String,
    val brand: AgentBrand,
    val title: String,
    val titleSource: String,
    val workspace: String,
    val project: String,
    val branch: String,
    val model: String,
    val cwd: String,
    val status: SessionStatus,
    val latestEvent: AgentEvent?,
    val events: List<AgentEvent>,
    val replies: List<AgentReply>,
    val unreadCount: Int,
    val lastPreview: String,
    val lastTs: String,
    val transcript: TranscriptPage? = null,
    val requestId: String = "",
    val effort: String = "",
    val permissionMode: String = "",
    val contextUsage: ContextUsage? = null,
    val running: Boolean = false,
    val runningSince: String = "",
    val capabilities: AgentCapabilities = AgentCapabilities(),
    val read: Boolean = false,
    val pinned: Boolean = false,
    val muted: Boolean = false,
    val archived: Boolean = false,
    val isTemp: Boolean = false,
    val isEmpty: Boolean = false,
) {
    val pendingEvent: AgentEvent?
        get() = latestEvent?.takeIf { event ->
            event.isPending && replies.none { reply ->
                reply.eventId == event.id && reply.status != ReplyStatus.FAILED
            }
        }

    val pending: Boolean
        get() = status == SessionStatus.NEEDS_PERMISSION ||
            status == SessionStatus.WAITING_INPUT ||
            pendingEvent != null

    val pendingLabel: String?
        get() = when (status) {
            SessionStatus.NEEDS_PERMISSION -> "需要授权"
            SessionStatus.WAITING_INPUT -> "等待输入"
            else -> when (pendingEvent?.kind) {
                "permission" -> "需要授权"
                "needs_input" -> "等待输入"
                else -> null
            }
        }

    val latestReplyText: String
        get() = lastPreview.ifBlank {
            latestEvent?.text?.ifBlank { latestEvent.title }.orEmpty()
        }

    val replyEventId: Long?
        get() = latestEvent?.id?.takeIf { it > 0 }

    val hasRealEvent: Boolean
        get() = events.any { it.id > 0 }

    val canReply: Boolean
        get() = sessionId.isNotBlank()
}

enum class InboxEntryKind(val wire: String) {
    APPROVAL("approval"),
    NEEDS_REPLY("needs_reply"),
    ERROR("error"),
    RUNNING("running"),
    DONE("done");

    companion object {
        fun fromWire(value: String): InboxEntryKind? {
            return entries.firstOrNull { it.wire == value.trim().lowercase() }
        }
    }
}

data class InboxEntry(
    val kind: InboxEntryKind? = null,
    val sessionId: String,
    val eventId: String = "",
    val title: String = "",
    val summary: String = "",
    val at: String = "",
    val pinned: Boolean? = null,
    val read: Boolean? = null,
    val sessionMeta: SessionMeta? = null,
) {
    companion object {
        fun fromJson(json: JSONObject): InboxEntry? {
            val sessionJson = json.optJSONObject("session")
            val meta = sessionJson?.let {
                SessionMeta.fromJson(it)
            } ?: SessionMeta.fromJson(json)
            val sessionId = meta.sessionId
                .ifBlank { json.stringOrBlank("session_id") }
                .ifBlank { json.stringOrBlank("id") }
            if (sessionId.isBlank()) return null
            val title = json.stringOrBlank("title").ifBlank { meta.title }
            val summary = json.stringOrBlank("summary").ifBlank { meta.lastPreview }
            val at = json.stringOrBlank("at").ifBlank { meta.lastTs }
            val pinned = json.booleanOrNull("pinned") ?: meta.pinned
            val read = json.booleanOrNull("read") ?: meta.read
            return InboxEntry(
                kind = InboxEntryKind.fromWire(json.stringOrBlank("kind")),
                sessionId = sessionId,
                eventId = json.stringOrBlank("event_id"),
                title = title,
                summary = summary,
                at = at,
                pinned = pinned,
                read = read,
                sessionMeta = meta.copy(
                    sessionId = sessionId,
                    title = meta.title.ifBlank { title },
                    lastPreview = meta.lastPreview.ifBlank { summary },
                    lastTs = meta.lastTs.ifBlank { at },
                    pinned = pinned,
                    read = read,
                ),
            )
        }
    }
}

data class InboxFeed(
    val waiting: List<InboxEntry> = emptyList(),
    val running: List<InboxEntry> = emptyList(),
    val recentDone: List<InboxEntry> = emptyList(),
    val waitingCount: Int = waiting.size,
    val runningCount: Int = running.size,
    val generatedAt: String = "",
    val available: Boolean = false,
) {
    val sessions: List<SessionMeta>
        get() = (waiting + running + recentDone)
            .mapNotNull { it.sessionMeta }
            .distinctBy { it.sessionId }

    fun withSessionRead(sessionId: String): InboxFeed {
        val nextWaiting = waiting.filterNot { it.sessionId == sessionId }
        return copy(
            waiting = nextWaiting,
            running = running.map {
                if (it.sessionId == sessionId) it.copy(read = true) else it
            },
            recentDone = recentDone.map {
                if (it.sessionId == sessionId) it.copy(read = true) else it
            },
            waitingCount = nextWaiting.size,
        )
    }

    fun withoutWaiting(sessionId: String): InboxFeed {
        val nextWaiting = waiting.filterNot { it.sessionId == sessionId }
        return copy(waiting = nextWaiting, waitingCount = nextWaiting.size)
    }

    fun withoutSession(sessionId: String): InboxFeed {
        val nextWaiting = waiting.filterNot { it.sessionId == sessionId }
        val nextRunning = running.filterNot { it.sessionId == sessionId }
        return copy(
            waiting = nextWaiting,
            running = nextRunning,
            recentDone = recentDone.filterNot { it.sessionId == sessionId },
            waitingCount = nextWaiting.size,
            runningCount = nextRunning.size,
        )
    }
}

data class AgentUiState(
    val settings: AppSettings,
    val connection: ConnectionState = ConnectionState.DISCONNECTED,
    val connectionDiagnostics: ConnectionDiagnostics = ConnectionDiagnostics(),
    val events: Map<Long, AgentEvent> = emptyMap(),
    val replies: Map<Long, AgentReply> = emptyMap(),
    val sessionMeta: Map<String, SessionMeta> = emptyMap(),
    val agentDirectory: AgentDirectory = AgentDirectory(),
    val sessionItems: Map<String, SessionItemsState> = emptyMap(),
    val sessionApiAvailable: Boolean = true,
    val transcriptCache: Map<String, TranscriptPage> = emptyMap(),
    val projects: List<ProjectInfo> = emptyList(),
    val away: Boolean = false,
    val isRefreshing: Boolean = false,
    val error: String? = null,
    val lastUpdatedAt: Long? = null,
    val localSessionFlags: Map<String, LocalSessionFlags> = emptyMap(),
    val inbox: InboxFeed? = null,
) {
    val allUnreadCount: Int
        get() = events.values.count { !it.read }

    fun sessions(): List<SessionSummary> {
        val grouped = events.values.groupBy { it.sessionKey }
        val keys = if (sessionApiAvailable) {
            sessionMeta.keys.toList()
        } else {
            (grouped.keys + sessionMeta.keys).distinct()
        }
        return keys.distinct().mapNotNull { key ->
            val sessionEvents = grouped[key].orEmpty().sortedBy { it.id }
            val meta = sessionMeta[key]
            val latest = sessionEvents.lastOrNull()
            if (latest == null && meta == null) return@mapNotNull null

            val source = meta?.source?.takeIf { it.isNotBlank() }
                ?: latest?.source.orEmpty()
            val brand = AgentBrand.fromWire(source)
            val cwd = meta?.cwd?.takeIf { it.isNotBlank() }
                ?: latest?.cwd.orEmpty()
            val project = meta?.project?.takeIf { it.isNotBlank() }
                ?: latest?.project?.takeIf { it.isNotBlank() }
                ?: projectFromCwd(cwd)
            val workspace = meta?.workspace?.takeIf { it.isNotBlank() }
                ?: workspaceFromCwd(cwd)
            val transcript = transcriptCache[key]
            val resolvedTitle = resolveSessionTitle(
                serverTitle = meta?.title,
                titleSource = meta?.titleSource,
                firstQuestion = transcript?.firstUserQuestion,
                eventTitle = latest?.title,
                project = project,
                sessionId = key,
                source = source,
            )
            val status = SessionStatus.fromWire(meta?.status)
                ?: SessionStatus.fromKind(latest?.kind)
                ?: SessionStatus.IDLE
            val itemsState = sessionItems[key]
            val running = (
                itemsState?.running == true ||
                    meta?.running == true ||
                    status == SessionStatus.RUNNING
                ) &&
                status != SessionStatus.NEEDS_PERMISSION &&
                status != SessionStatus.WAITING_INPUT &&
                status != SessionStatus.ERROR
            val effectiveStatus = when {
                status == SessionStatus.NEEDS_PERMISSION ||
                    status == SessionStatus.ERROR -> status
                running -> SessionStatus.RUNNING
                else -> status
            }
            val cachedFlags = localSessionFlags[key] ?: LocalSessionFlags()
            val read = meta?.read ?: cachedFlags.read
            val pinned = meta?.pinned ?: cachedFlags.pinned
            val muted = meta?.muted ?: cachedFlags.muted
            val archived = meta?.archived ?: cachedFlags.archived
            val localUnread = sessionEvents.count { !it.read }
            val unread = if (read) {
                0
            } else if (localUnread > 0) {
                localUnread
            } else {
                meta?.unread?.coerceAtLeast(0) ?: 0
            }
            val lastTs = meta?.lastTs?.takeIf { it.isNotBlank() }
                ?: latest?.ts.orEmpty()
            val preview = meta?.lastPreview?.takeIf { it.isNotBlank() }
                ?: latest?.shortSummary(220).orEmpty()
            val sessionReplies = replies.values
                .filter { it.sessionId == key }
                .sortedBy { it.ts }

            val profile = agentDirectory.profile(source)
            SessionSummary(
                sessionId = key,
                brand = brand,
                title = resolvedTitle.title,
                titleSource = resolvedTitle.source,
                workspace = workspace,
                project = project,
                branch = meta?.branch.orEmpty(),
                model = meta?.model?.takeIf { it.isNotBlank() }
                    ?: profile.defaultModel,
                cwd = cwd,
                status = effectiveStatus,
                latestEvent = latest,
                events = sessionEvents,
                replies = sessionReplies,
                unreadCount = unread,
                lastPreview = preview,
                lastTs = lastTs,
                transcript = transcript,
                requestId = meta?.requestId.orEmpty(),
                effort = meta?.effort.orEmpty(),
                permissionMode = meta?.permissionMode.orEmpty(),
                contextUsage = meta?.contextUsage,
                running = running,
                runningSince = meta?.runningSince
                    ?.takeIf { it.isNotBlank() }
                    ?: itemsState?.runningSince.orEmpty(),
                capabilities = profile.capabilities,
                read = read,
                pinned = pinned,
                muted = muted,
                archived = archived,
                isTemp = meta?.isTemp ?: false,
                isEmpty = meta?.isEmpty ?: false,
            )
        }.sortedWith(
            compareByDescending<SessionSummary> { it.pinned }
                .thenByDescending { it.lastTs.ifBlank { Instant.EPOCH.toString() } },
        )
    }

    fun session(sessionId: String): SessionSummary? {
        return sessions().firstOrNull { it.sessionId == sessionId }
    }
}

private val genericEventTitle = Regex(
    "^(Codex|Claude( Code)?|WorkBuddy|Agent)\\s*(回复|通知|消息|请求权限|等待输入|需要审批)?$",
    RegexOption.IGNORE_CASE,
)

fun inferSessionTitle(
    serverTitle: String?,
    titleSource: String?,
    firstQuestion: String?,
    eventTitle: String?,
    project: String,
    sessionId: String = "",
    source: String = "",
): String = resolveSessionTitle(
    serverTitle = serverTitle,
    titleSource = titleSource,
    firstQuestion = firstQuestion,
    eventTitle = eventTitle,
    project = project,
    sessionId = sessionId,
    source = source,
).title

data class ResolvedSessionTitle(
    val title: String,
    val source: String,
)

fun resolveSessionTitle(
    serverTitle: String?,
    titleSource: String?,
    firstQuestion: String?,
    eventTitle: String?,
    project: String,
    sessionId: String = "",
    source: String = "",
): ResolvedSessionTitle {
    val server = serverTitle?.trim().orEmpty()
    val sourceKind = titleSource?.trim()?.lowercase().orEmpty()
    val nativeSources = setOf("", "custom", "agent", "first_prompt")
    if (sourceKind in nativeSources && isUsableSessionTitle(server, sessionId)) {
        return ResolvedSessionTitle(
            title = server.take(80),
            source = sourceKind.ifBlank { "agent" },
        )
    }
    val question = firstQuestion
        ?.lineSequence()
        ?.map { it.replace(Regex("^[#>*\\-\\s]+"), "").trim() }
        ?.firstOrNull { it.isNotBlank() && !it.startsWith("<") }
        ?.take(80)
        .orEmpty()
    if (isUsableSessionTitle(question, sessionId)) {
        return ResolvedSessionTitle(question, "first_prompt")
    }
    val event = eventTitle?.trim().orEmpty()
    if (isUsableSessionTitle(event, sessionId)) {
        return ResolvedSessionTitle(event.take(80), "agent")
    }
    val projectTitle = project.trim()
    if (isUsableSessionTitle(projectTitle, sessionId)) {
        return ResolvedSessionTitle(projectTitle.take(80), "project")
    }
    val sourceLabel = AgentBrand.fromWire(source).displayName
    return ResolvedSessionTitle(
        title = if (sourceLabel == AgentBrand.UNKNOWN.displayName) {
            "未命名会话"
        } else {
            "$sourceLabel 会话"
        },
        source = "fallback",
    )
}

private val fullUuidPattern = Regex(
    "^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$",
)

fun isBareSessionIdentifier(value: String, sessionId: String = ""): Boolean {
    val normalized = value.trim()
    if (normalized.isEmpty()) return false
    if (sessionId.isNotBlank() && normalized.equals(sessionId.trim(), ignoreCase = true)) {
        return true
    }
    return fullUuidPattern.matches(normalized)
}

fun isUsableSessionTitle(value: String?, sessionId: String = ""): Boolean {
    val normalized = value?.trim().orEmpty()
    return normalized.isNotEmpty() &&
        !genericEventTitle.matches(normalized) &&
        !isBareSessionIdentifier(normalized, sessionId)
}

fun titleSourceLabel(value: String): String {
    return when (value.trim().lowercase()) {
        "custom" -> "用户命名"
        "agent" -> "Agent 标题"
        "first_prompt" -> "首条提问"
        "project" -> "项目名"
        "fallback" -> "兜底标题"
        "event" -> "事件标题"
        "" -> "客户端推导"
        else -> value
    }
}

fun workspaceFromCwd(cwd: String): String {
    if (cwd.isBlank()) return ""
    val parts = cwd.trimEnd('/', '\\').split('/', '\\').filter { it.isNotBlank() }
    return parts.dropLast(1).lastOrNull().orEmpty()
}

fun capabilityTokenIn(url: String): String? {
    return Regex("/f/([0-9a-fA-F]{32})/").find(url)?.groupValues?.getOrNull(1)
}

fun projectFromCwd(cwd: String): String {
    if (cwd.isBlank()) {
        return "未命名项目"
    }
    return cwd.trimEnd('/', '\\')
        .split('/', '\\')
        .lastOrNull { it.isNotBlank() }
        ?: "未命名项目"
}

fun relativeTime(ts: String, now: Instant = Instant.now()): String {
    val instant = runCatching { Instant.parse(ts) }.getOrNull() ?: return ""
    val seconds = max(0, now.epochSecond - instant.epochSecond)
    return when {
        seconds < 60 -> "刚刚"
        seconds < 3_600 -> "${seconds / 60} 分钟前"
        seconds < 86_400 -> "${seconds / 3_600} 小时前"
        seconds < 172_800 -> "昨天"
        seconds < 604_800 -> "${seconds / 86_400} 天前"
        else -> instant.atZone(ZoneId.systemDefault())
            .format(DateTimeFormatter.ofPattern("MM-dd"))
    }
}

fun elapsedSince(ts: String, now: Long = System.currentTimeMillis()): String {
    if (ts.isBlank()) return ""
    val start = runCatching { Instant.parse(ts).toEpochMilli() }.getOrNull() ?: return ""
    val seconds = ((now - start) / 1_000L).coerceAtLeast(0L)
    return when {
        seconds < 60 -> "${seconds}s"
        seconds < 3_600 -> "${seconds / 60}m ${seconds % 60}s"
        else -> "${seconds / 3_600}h ${(seconds % 3_600) / 60}m"
    }
}

fun formatClock(ts: String): String {
    val instant = runCatching { Instant.parse(ts) }.getOrNull() ?: return ""
    return instant.atZone(ZoneId.systemDefault())
        .format(DateTimeFormatter.ofPattern("MM-dd HH:mm"))
}

fun formatDiagnosticTime(epochMillis: Long?): String {
    if (epochMillis == null) {
        return "尚无记录"
    }
    return Instant.ofEpochMilli(epochMillis)
        .atZone(ZoneId.systemDefault())
        .format(DateTimeFormatter.ofPattern("MM-dd HH:mm:ss"))
}

internal fun JSONObject.stringOrBlank(key: String): String {
    val value = opt(key)
    return if (value == null || value == JSONObject.NULL) "" else value.toString()
}

internal fun JSONObject.booleanOrNull(key: String): Boolean? {
    if (!has(key) || isNull(key)) return null
    return optBoolean(key)
}
