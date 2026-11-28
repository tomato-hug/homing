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
    OUTBOX("outbox", "待发送"),
    QUEUED("queued", "排队中"),
    HELD("held", "已交给钩子"),
    INJECTED("injected", "已注入"),
    CANCELLED("cancelled", "已撤回"),
    FAILED("failed", "失败");

    companion object {
        fun fromWire(value: String?): ReplyStatus {
            return when (value?.lowercase()) {
                "switch_model_unsupported", "sent", "delivered" -> INJECTED
                else -> entries.firstOrNull { it.wire == value?.lowercase() } ?: QUEUED
            }
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

private val CODEBUDDY_WORD_REGEX = Regex("\\bCodeBuddy(\\s+Code)?\\b", RegexOption.IGNORE_CASE)

internal fun isIdlePromptNoiseEvent(kind: String, text: String): Boolean {
    val cleanKind = kind.trim().lowercase()
    if (cleanKind !in setOf("needs_input", "waiting_input", "needs_reply", "waiting")) {
        return false
    }
    val cleaned = text.trim().lowercase().trimEnd('.', '!', '。', '！', ' ')
    if (cleaned.isEmpty()) return false
    if (cleaned == "waiting for your input") return true
    val suffix = " is waiting for your input"
    if (cleaned.endsWith(suffix)) {
        val prefix = cleaned.removeSuffix(suffix).trim()
        if (prefix.isNotEmpty() && prefix.split(Regex("\\s+")).size <= 4) {
            return true
        }
    }
    return false
}

internal fun normalizeHookSourceBrand(source: String, text: String): String {
    if (!source.trim().equals("workbuddy", ignoreCase = true) || text.isBlank()) {
        return text
    }
    return CODEBUDDY_WORD_REGEX.replace(text, "WorkBuddy")
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
    val approvalOptions: List<ApprovalOption> = emptyList(),
    val questions: List<ItemQuestion> = emptyList(),
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
                "approval_options",
                JSONArray().apply { approvalOptions.forEach { put(it.toJson()) } },
            )
            .apply {
                if (questions.isNotEmpty()) {
                    put("questions", itemQuestionsToJson(questions))
                }
            }
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
            val approvalOptions = buildList {
                fun addAll(array: JSONArray?) {
                    if (array == null) return
                    for (index in 0 until array.length()) {
                        array.optJSONObject(index)
                            ?.let(ApprovalOption::fromJson)
                            ?.let(::add)
                    }
                }
                addAll(json.optJSONArray("approval_options"))
                addAll(nestedMeta?.optJSONObject("acp")?.optJSONArray("options"))
            }
            val parsedKind = json.stringOrBlank("kind").ifBlank { "reply" }
            val rawTitle = json.stringOrBlank("title")
            val rawText = json.stringOrBlank("text")
            val shouldNormalizeBrand = parsedKind in setOf("needs_input", "permission", "needs_reply", "approval")
            return AgentEvent(
                id = json.optLong("id", 0L),
                ts = json.stringOrBlank("ts").ifBlank { Instant.now().toString() },
                source = source,
                kind = parsedKind,
                sessionId = sessionId,
                cwd = cwd,
                project = project,
                title = if (shouldNormalizeBrand) normalizeHookSourceBrand(source, rawTitle) else rawTitle,
                text = if (shouldNormalizeBrand) normalizeHookSourceBrand(source, rawText) else rawText,
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
                approvalOptions = approvalOptions,
                questions = parseItemQuestions(json.optJSONArray("questions")),
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
    val permissionMode: String = "",
)

private val nonQueueDowngradeStatuses = setOf(
    ReplyStatus.HELD,
    ReplyStatus.INJECTED,
    ReplyStatus.CANCELLED,
    ReplyStatus.FAILED,
)

/**
 * HTTP receipts and SSE frames can arrive out of order. A stale "queued"
 * snapshot must never turn an already delivered reply back into a cancellable
 * queue item.
 */
internal fun reconcileIncomingReply(
    existing: AgentReply?,
    incoming: AgentReply,
): AgentReply {
    if (existing == null) return incoming
    if (
        incoming.status != ReplyStatus.QUEUED ||
        existing.status !in nonQueueDowngradeStatuses
    ) {
        return incoming
    }
    return incoming.copy(
        status = existing.status,
        message = existing.message ?: incoming.message,
    )
}

data class ReplyReceipt(
    val ok: Boolean,
    val replyId: Long,
    val status: ReplyStatus,
    val message: String? = null,
)

enum class DispatchStatus(val wire: String, val label: String) {
    STARTING("starting", "启动中"),
    WAITING_SESSION("waiting_session", "等待会话"),
    STARTED("started", "已启动"),
    RUNNING("running", "运行中"),
    NEEDS_YOU("needs_you", "等你处理"),
    COMPLETED("completed", "完成"),
    FAILED("failed", "派发失败"),
    CANCELLED("cancelled", "已取消");

    companion object {
        fun fromWire(value: String?): DispatchStatus {
            return entries.firstOrNull { it.wire == value?.lowercase() } ?: STARTING
        }
    }
}

data class DispatchOutcome(
    val text: String = "",
    val filesChanged: List<String> = emptyList(),
    val finishedAt: String = "",
    val branch: String = "",
    val diffStat: String = "",
) {
    companion object {
        fun fromJson(json: JSONObject?): DispatchOutcome? {
            if (json == null) return null
            val files = json.optJSONArray("files_changed")
            return DispatchOutcome(
                text = json.stringOrBlank("text"),
                filesChanged = buildList {
                    if (files != null) {
                        for (index in 0 until files.length()) {
                            files.optString(index)
                                .takeIf { it.isNotBlank() }
                                ?.let(::add)
                        }
                    }
                },
                finishedAt = json.stringOrBlank("finished_at"),
                branch = json.stringOrBlank("branch"),
                diffStat = json.stringOrBlank("diff_stat"),
            )
        }
    }
}

data class ComposeMentionPlan(
    val agent: String = "",
    val preset: String = "ask",
    val text: String = "",
    val target: String = "new",
    val threadId: String = "",
    val workspace: String = "",
    val context: List<String> = emptyList(),
    val dispatchable: Boolean = false,
    val reason: String = "",
) {
    val brand: AgentBrand
        get() = AgentBrand.fromWire(agent)

    val presetLabel: String
        get() = when (preset) {
            "do" -> "动手"
            "review" -> "审查"
            "handoff" -> "交棒"
            else -> "咨询"
        }

    companion object {
        fun fromJson(json: JSONObject): ComposeMentionPlan {
            return ComposeMentionPlan(
                agent = json.stringOrBlank("agent").lowercase(),
                preset = json.stringOrBlank("preset").ifBlank { "ask" },
                text = json.stringOrBlank("text"),
                target = json.stringOrBlank("target").ifBlank { "new" },
                threadId = json.stringOrBlank("thread_id"),
                workspace = json.stringOrBlank("workspace"),
                context = json.optJSONArray("context")?.let { array ->
                    (0 until array.length()).mapNotNull { index ->
                        array.optString(index).takeIf { it.isNotBlank() }
                    }
                }.orEmpty(),
                dispatchable = json.optBoolean("dispatchable", false),
                reason = json.stringOrBlank("reason"),
            )
        }
    }
}

data class ComposeMentionOverride(
    val agent: String,
    val target: String = "auto",
    val context: List<String> = listOf("recent", "files"),
    val workspace: String = "auto",
) {
    fun toJson(): JSONObject = JSONObject()
        .put("agent", agent)
        .put("target", target)
        .put(
            "context",
            JSONArray().apply { context.forEach { put(it) } },
        )
        .put("workspace", workspace)
}

data class ComposePlan(
    val message: String = "",
    val mentions: List<ComposeMentionPlan> = emptyList(),
)

data class ComposeReply(
    val replyId: Long = 0L,
    val status: String = "",
    val message: String = "",
)

data class ComposeResult(
    val plan: ComposePlan = ComposePlan(),
    val reply: ComposeReply? = null,
    val dispatches: List<AgentDispatch> = emptyList(),
    val dryRun: Boolean = false,
) {
    companion object {
        fun fromJson(json: JSONObject): ComposeResult {
            val planJson = json.optJSONObject("plan")
            val mentionArray = planJson?.optJSONArray("mentions")
            val dispatchArray = json.optJSONArray("dispatches")
            val replyJson = json.optJSONObject("reply")
            return ComposeResult(
                plan = ComposePlan(
                    message = planJson?.stringOrBlank("message").orEmpty(),
                    mentions = buildList {
                        if (mentionArray != null) {
                            for (index in 0 until mentionArray.length()) {
                                mentionArray.optJSONObject(index)?.let {
                                    add(ComposeMentionPlan.fromJson(it))
                                }
                            }
                        }
                    },
                ),
                reply = replyJson?.let {
                    ComposeReply(
                        replyId = it.optLong("reply_id", 0L),
                        status = it.stringOrBlank("status"),
                        message = it.stringOrBlank("message"),
                    )
                },
                dispatches = buildList {
                    if (dispatchArray != null) {
                        for (index in 0 until dispatchArray.length()) {
                            dispatchArray.optJSONObject(index)?.let {
                                val dispatch = AgentDispatch.fromJson(it)
                                if (dispatch.id.isNotBlank()) add(dispatch)
                            }
                        }
                    }
                },
                dryRun = json.optBoolean("dry_run", false),
            )
        }
    }
}

data class AgentDispatch(
    val id: String,
    val parentSessionId: String = "",
    val parentSource: String = "",
    val parentEventId: Long? = null,
    val targetAgent: String = "",
    val targetSessionId: String = "",
    val requestId: String = "",
    val text: String = "",
    val cwd: String = "",
    val mode: String = "parallel",
    val preset: String = "ask",
    val workspace: String = "",
    val thread: String = "",
    val branch: String = "",
    val repoCwd: String = "",
    val worktreeDiscardedAt: String = "",
    val status: DispatchStatus = DispatchStatus.STARTING,
    val message: String = "",
    val result: DispatchOutcome? = null,
    val createdAt: String = "",
    val updatedAt: String = "",
) {
    val targetBrand: AgentBrand
        get() = AgentBrand.fromWire(targetAgent)

    companion object {
        fun fromJson(json: JSONObject): AgentDispatch {
            val parentEventId = if (json.has("parent_event_id") && !json.isNull("parent_event_id")) {
                json.optLong("parent_event_id", 0L).takeIf { it > 0 }
            } else {
                null
            }
            return AgentDispatch(
                id = json.stringOrBlank("dispatch_id")
                    .ifBlank { json.stringOrBlank("id") },
                parentSessionId = json.stringOrBlank("parent_session_id"),
                parentSource = json.stringOrBlank("parent_source"),
                parentEventId = parentEventId,
                targetAgent = json.stringOrBlank("target_agent").lowercase(),
                targetSessionId = json.stringOrBlank("target_session_id"),
                requestId = json.stringOrBlank("request_id"),
                text = json.stringOrBlank("text"),
                cwd = json.stringOrBlank("cwd"),
                mode = json.stringOrBlank("mode").ifBlank { "parallel" },
                preset = json.stringOrBlank("preset").ifBlank { "ask" },
                workspace = json.stringOrBlank("workspace"),
                thread = json.stringOrBlank("thread"),
                branch = json.stringOrBlank("branch"),
                repoCwd = json.stringOrBlank("repo_cwd"),
                worktreeDiscardedAt = json.stringOrBlank("worktree_discarded_at"),
                status = DispatchStatus.fromWire(json.stringOrBlank("status")),
                message = json.stringOrBlank("message"),
                result = DispatchOutcome.fromJson(json.optJSONObject("result")),
                createdAt = json.stringOrBlank("created_at"),
                updatedAt = json.stringOrBlank("updated_at"),
            )
        }
    }
}

data class AgentCapabilities(
    val reply: Boolean = true,
    val newSession: Boolean = false,
    // null 表示旧中继没有声明 readiness。WorkBuddy 的旧 ACP 会复用当前活跃
    // 会话，不能仅凭 new_session=true 就允许手机新建，故对旧响应按不可新建处理。
    val newSessionReady: Boolean? = null,
    val dispatch: Boolean = false,
    val switchModel: Boolean = false,
    val effort: Boolean = false,
    val stop: Boolean = false,
    val approve: Boolean = true,
    val live: Boolean = true,
    val permissionModes: List<String> = emptyList(),
) {
    companion object {
        fun fromJson(json: JSONObject?): AgentCapabilities {
            if (json == null) return AgentCapabilities()
            val permissionModes = json.optJSONArray("permission_modes")
            return AgentCapabilities(
                reply = json.optBoolean("reply", true),
                newSession = json.optBoolean("new_session", false),
                newSessionReady = if (json.has("new_session_ready")) {
                    json.optBoolean("new_session_ready", false)
                } else {
                    null
                },
                dispatch = json.optBoolean("dispatch", false),
                switchModel = json.optBoolean("switch_model", false),
                effort = json.optBoolean("effort", false),
                stop = json.optBoolean("stop", false),
                approve = json.optBoolean("approve", true),
                live = json.optBoolean("live", true),
                permissionModes = buildList {
                    if (permissionModes != null) {
                        for (index in 0 until permissionModes.length()) {
                            permissionModes.optString(index)
                                .takeIf { it.isNotBlank() }
                                ?.let(::add)
                        }
                    }
                }.distinct(),
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
    val displayName: String = "",
    val icon: String = "",
    val available: Boolean = false,
    val installed: Boolean = false,
    val processRunning: Boolean = false,
    val activeOnline: Boolean = false,
    val canWake: Boolean = false,
    val wakeMode: String = "",
    val capabilities: AgentCapabilities = AgentCapabilities(),
    val models: List<AgentModel> = emptyList(),
    val defaultModel: String = "",
) {
    val brand: AgentBrand
        get() = AgentBrand.fromWire(source)

    val isOnlineForNewSession: Boolean
        get() = available && supportsNewSession

    val supportsNewSession: Boolean
        get() = capabilities.newSession && (
            capabilities.newSessionReady
                ?: (source != AgentBrand.WORKBUDDY.wire)
            )

    val label: String
        get() = displayName.ifBlank { defaultAgentDisplayName(source) }

    val initial: String
        get() = label.trim().firstOrNull()?.uppercase() ?: brand.initial

    val defaultModelOption: AgentModel?
        get() = models.firstOrNull { it.isDefault }
            ?: models.firstOrNull { it.id == defaultModel }
            ?: models.firstOrNull()
}

fun defaultAgentDisplayName(source: String): String {
    return when (source.trim().lowercase()) {
        "claude", "claude-code" -> "Claude"
        "codex" -> "Codex"
        "workbuddy" -> "WorkBuddy"
        "kimi" -> "Kimi"
        "opencode" -> "OpenCode"
        "zcode" -> "ZCode"
        "cursor" -> "Cursor"
        "windsurf" -> "Windsurf"
        "trae" -> "Trae"
        "zed" -> "Zed"
        "antigravity" -> "Antigravity"
        "gemini", "gemini-cli" -> "Gemini CLI"
        "qwen", "qwen-code" -> "Qwen Code"
        "copilot-cli", "github-copilot" -> "Copilot CLI"
        "dsh", "deepseek", "deepseek-harness" -> "DSH"
        "codebuddy" -> "CodeBuddy"
        "kiro" -> "Kiro"
        "openclaw" -> "OpenClaw"
        "goose" -> "Goose"
        "roo", "roo-code" -> "Roo Code"
        "cline" -> "Cline"
        "amp" -> "Amp"
        "openhands" -> "OpenHands"
        "kilo", "kilocode" -> "Kilo Code"
        "junie" -> "Junie"
        "minimax", "minimax-design" -> "MiniMax"
        "warp" -> "Warp"
        "aider" -> "Aider"
        "droid" -> "Droid"
        "crush" -> "Crush"
        "pi" -> "Pi"
        "auggie" -> "Auggie"
        "" -> "Agent"
        else -> source.trim().replace('-', ' ').replace('_', ' ')
            .split(' ')
            .filter { it.isNotBlank() }
            .joinToString(" ") { word ->
                word.replaceFirstChar { ch -> ch.uppercase() }
            }
            .ifBlank { "Agent" }
    }
}

data class MobileWorkspaceUndo(
    val clearedAtEpochMs: Long,
    val baselineMaxEventId: Long,
    val baselineReady: Boolean,
    val restoredSessionIds: Set<String>,
    val liveSessionIds: Set<String>,
    val dismissedSessions: Map<String, PhoneActivity>,
)

data class MobileWorkspaceState(
    val clearedAtEpochMs: Long = 0L,
    val baselineMaxEventId: Long = 0L,
    val baselineReady: Boolean = true,
    val restoredSessionIds: Set<String> = emptySet(),
    val liveSessionIds: Set<String> = emptySet(),
    val dismissedSessions: Map<String, PhoneActivity> = emptyMap(),
    val wokenAgentIds: Set<String> = emptySet(),
    val undo: MobileWorkspaceUndo? = null,
) {
    val canUndo: Boolean
        get() = undo != null
}

/**
 * "Online on the Mac" means the agent process is running, the Mac reports it as
 * active (recently used or woken), or the user explicitly woke it from this phone.
 * Merely being installed does not count.
 */
fun AgentUiState.isAgentActiveOnMac(source: String): Boolean {
    val key = source.trim().lowercase()
    if (key.isBlank()) return false
    if (key in mobileWorkspace.wokenAgentIds) return true
    val profile = agentDirectory.agents[key] ?: return false
    return profile.processRunning || profile.activeOnline
}

/** Groups sessions by the project directory they belong to. */
data class ProjectSessionGroup(
    val key: String,
    val name: String,
    val cwd: String,
    val sessions: List<SessionSummary>,
) {
    val primary: SessionSummary
        get() = sessions.first()
    val urgentCount: Int
        get() = sessions.count { it.isUrgentForPhone() }
    val runningCount: Int
        get() = sessions.count { it.running }
    val lastTs: String
        get() = sessions.maxOfOrNull { it.lastTs.ifBlank { "" } }.orEmpty()
    val sources: List<String>
        get() = sessions.map { it.source.lowercase() }.filter { it.isNotBlank() }.distinct()
}

internal fun SessionSummary.isUrgentForPhone(): Boolean {
    if (archived || isEmpty) return false
    return status == SessionStatus.NEEDS_PERMISSION ||
        status == SessionStatus.WAITING_INPUT ||
        status == SessionStatus.ERROR ||
        pendingEvent != null
}

internal fun SessionSummary.projectGroupKey(): String {
    val dir = cwd.trim().trimEnd('/')
    if (dir.isNotBlank()) return dir
    val name = project.trim()
    return if (name.isNotBlank()) "project:$name" else "project:未命名项目"
}

internal fun groupSessionsByProject(sessions: List<SessionSummary>): List<ProjectSessionGroup> {
    return sessions
        .groupBy { it.projectGroupKey() }
        .map { (key, list) ->
            val ordered = list.sortedWith(
                compareByDescending<SessionSummary> { it.isUrgentForPhone() }
                    .thenByDescending { it.running }
                    .thenByDescending { it.lastTs },
            )
            val head = ordered.first()
            ProjectSessionGroup(
                key = key,
                name = head.project.ifBlank { projectFromCwd(head.cwd) }.ifBlank { "未命名项目" },
                cwd = head.cwd,
                sessions = ordered,
            )
        }
        .sortedWith(
            compareByDescending<ProjectSessionGroup> { it.urgentCount > 0 }
                .thenByDescending { it.runningCount > 0 }
                .thenByDescending { it.lastTs },
        )
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

data class SessionSearchHit(
    val sessionId: String,
    val source: String,
    val title: String,
    val snippet: String,
    val itemId: String = "",
) {
    companion object {
        fun listFromJson(root: JSONObject): List<SessionSearchHit> {
            val array = root.optJSONArray("hits") ?: return emptyList()
            return buildList {
                for (index in 0 until array.length()) {
                    val hit = array.optJSONObject(index) ?: continue
                    val session = hit.optJSONObject("session") ?: continue
                    val snippetJson = hit.optJSONObject("snippet")
                    val sessionId = session.stringOrBlank("session_id")
                    if (sessionId.isBlank()) continue
                    val snippet = if (snippetJson != null) {
                        snippetJson.stringOrBlank("before") +
                            snippetJson.stringOrBlank("match") +
                            snippetJson.stringOrBlank("after")
                    } else {
                        hit.stringOrBlank("snippet")
                    }
                    add(
                        SessionSearchHit(
                            sessionId = sessionId,
                            source = session.stringOrBlank("source"),
                            title = session.stringOrBlank("title"),
                            snippet = snippet.trim(),
                            itemId = hit.stringOrBlank("item_id"),
                        ),
                    )
                }
            }
        }
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

data class SessionCommand(
    val name: String,
    val description: String = "",
    val argumentHint: String = "",
) {
    companion object {
        fun fromJson(json: JSONObject): SessionCommand? {
            val name = json.stringOrBlank("name").removePrefix("/")
            if (name.isBlank()) return null
            return SessionCommand(
                name = name,
                description = json.stringOrBlank("description"),
                argumentHint = json.stringOrBlank("argument_hint"),
            )
        }
    }
}

data class CompletedPath(
    val path: String,
    val type: String = "file",
) {
    companion object {
        fun fromJson(json: JSONObject): CompletedPath? {
            val path = json.stringOrBlank("path")
            if (path.isBlank()) return null
            return CompletedPath(
                path = path,
                type = json.stringOrBlank("type").ifBlank { "file" },
            )
        }
    }
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
    val computerId: String = "",
    val computerName: String = "",
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
    val remindOnDone: Boolean = false,
    val isTemp: Boolean? = null,
    val isEmpty: Boolean? = null,
    val seenEventId: Long = 0L,
    val seenAt: String = "",
    val collab: SessionCollab? = null,
) {
    val brand: AgentBrand
        get() = AgentBrand.fromWire(source)

    fun toJson(): JSONObject {
        return JSONObject()
            .put("session_id", sessionId)
            .put("computer_id", computerId)
            .put("computer_name", computerName)
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
            .put("remind_on_done", remindOnDone)
            .put("is_temp", isTemp)
            .put("is_empty", isEmpty)
            .put("seen_event_id", seenEventId)
            .put("seen_at", seenAt.ifBlank { JSONObject.NULL })
            .put("collab", collab?.toJson())
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
                computerId = json.stringOrBlank("computer_id"),
                computerName = json.stringOrBlank("computer_name"),
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
                remindOnDone = json.optBoolean("remind_on_done", false),
                isTemp = json.booleanOrNull("is_temp"),
                isEmpty = json.booleanOrNull("is_empty"),
                seenEventId = json.optLong("seen_event_id", 0L),
                seenAt = json.stringOrBlank("seen_at"),
                collab = SessionCollab.fromJson(json.optJSONObject("collab")),
            )
        }
    }
}

data class HomeComputerStatus(
    val computerId: String,
    val name: String = "",
    val available: Boolean = false,
    val error: String = "",
    val attentionCount: Int = 0,
    val runningCount: Int = 0,
    val resumeCount: Int = 0,
    val earlierCount: Int = 0,
) {
    fun toJson(): JSONObject = JSONObject()
        .put("computer_id", computerId)
        .put("name", name)
        .put("available", available)
        .put("error", error)
        .put("attention_count", attentionCount)
        .put("running_count", runningCount)
        .put("resume_count", resumeCount)
        .put("earlier_count", earlierCount)

    companion object {
        fun fromJson(json: JSONObject): HomeComputerStatus? {
            val computerId = json.stringOrBlank("computer_id")
            if (computerId.isBlank()) return null
            return HomeComputerStatus(
                computerId = computerId,
                name = json.stringOrBlank("name"),
                available = json.optBoolean("available", false),
                error = json.stringOrBlank("error"),
                attentionCount = json.optInt("attention_count", 0),
                runningCount = json.optInt("running_count", 0),
                resumeCount = json.optInt("resume_count", 0),
                earlierCount = json.optInt("earlier_count", 0),
            )
        }
    }
}

data class SessionCollab(
    val role: String = "",
    val parentSessionId: String = "",
    val parentSource: String = "",
    val parentDispatchId: String = "",
    val children: List<SessionCollabChild> = emptyList(),
) {
    val isParent: Boolean
        get() = role == "parent" && children.isNotEmpty()

    val isChild: Boolean
        get() = role == "child" && parentSessionId.isNotBlank()

    fun toJson(): JSONObject {
        val childArray = JSONArray()
        children.forEach { child ->
            childArray.put(
                JSONObject()
                    .put("dispatch_id", child.dispatchId)
                    .put("agent", child.agent)
                    .put("status", child.status)
                    .put("preset", child.preset),
            )
        }
        return JSONObject()
            .put("role", role)
            .put(
                "parent",
                if (isChild) {
                    JSONObject()
                        .put("session_id", parentSessionId)
                        .put("source", parentSource)
                        .put("dispatch_id", parentDispatchId)
                } else {
                    JSONObject.NULL
                },
            )
            .put("children", childArray)
    }

    companion object {
        fun fromJson(json: JSONObject?): SessionCollab? {
            if (json == null) return null
            val parent = json.optJSONObject("parent")
            val childrenJson = json.optJSONArray("children")
            val children = buildList {
                if (childrenJson != null) {
                    for (index in 0 until childrenJson.length()) {
                        val child = childrenJson.optJSONObject(index) ?: continue
                        add(
                            SessionCollabChild(
                                dispatchId = child.stringOrBlank("dispatch_id"),
                                agent = child.stringOrBlank("agent"),
                                status = child.stringOrBlank("status"),
                                preset = child.stringOrBlank("preset"),
                            ),
                        )
                    }
                }
            }
            val role = json.stringOrBlank("role")
            if (role.isBlank() && parent == null && children.isEmpty()) return null
            return SessionCollab(
                role = role,
                parentSessionId = parent?.stringOrBlank("session_id").orEmpty(),
                parentSource = parent?.stringOrBlank("source").orEmpty(),
                parentDispatchId = parent?.stringOrBlank("dispatch_id").orEmpty(),
                children = children,
            )
        }
    }
}

data class SessionCollabChild(
    val dispatchId: String,
    val agent: String,
    val status: String,
    val preset: String,
)

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
    val source: String = "",
    val brand: AgentBrand,
    val agentLabel: String = "",
    val agentInitial: String = "",
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
    val remindOnDone: Boolean = false,
    val isTemp: Boolean = false,
    val isEmpty: Boolean = false,
    val seenAt: String = "",
    val collab: SessionCollab? = null,
    val computerId: String = "",
    val computerName: String = "",
    val pendingSuperseded: Boolean = false,
) {
    val pendingEvent: AgentEvent?
        get() = if (pendingSuperseded) {
            null
        } else {
            latestEvent?.takeIf { event ->
                event.isPending && replies.none { reply ->
                    reply.eventId == event.id &&
                        reply.status != ReplyStatus.FAILED &&
                        reply.status != ReplyStatus.CANCELLED
                }
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

data class AwayPeriod(
    val id: String,
    val startedAt: String = "",
    val endedAt: String = "",
    val source: String = "manual",
    val deviceId: String = "",
    val clearedAt: String = "",
    val startedAfterEventId: Long? = null,
) {
    val active: Boolean
        get() = endedAt.isBlank()

    fun toJson(): JSONObject {
        return JSONObject()
            .put("id", id)
            .put("started_at", startedAt)
            .put("ended_at", endedAt.ifBlank { JSONObject.NULL })
            .put("source", source)
            .put("device_id", deviceId)
            .put("cleared_at", clearedAt.ifBlank { JSONObject.NULL })
            .put("started_after_event_id", startedAfterEventId ?: JSONObject.NULL)
    }

    companion object {
        fun fromJson(json: JSONObject?): AwayPeriod? {
            if (json == null) return null
            val id = json.stringOrBlank("id")
            if (id.isBlank()) return null
            val afterEventId = if (json.has("started_after_event_id") && !json.isNull("started_after_event_id")) {
                json.optLong("started_after_event_id", -1L).takeIf { it >= 0L }
            } else {
                null
            }
            return AwayPeriod(
                id = id,
                startedAt = json.stringOrBlank("started_at"),
                endedAt = json.stringOrBlank("ended_at"),
                source = json.stringOrBlank("source").ifBlank { "manual" },
                deviceId = json.stringOrBlank("device_id"),
                clearedAt = json.stringOrBlank("cleared_at"),
                startedAfterEventId = afterEventId,
            )
        }
    }
}

data class HomeAway(
    val enabled: Boolean = false,
    val current: AwayPeriod? = null,
    val period: AwayPeriod? = null,
) {
    fun toJson(): JSONObject {
        return JSONObject()
            .put("enabled", enabled)
            .put("current", current?.toJson() ?: JSONObject.NULL)
            .put("period", period?.toJson() ?: JSONObject.NULL)
    }
}

data class ApprovalOption(
    val id: String,
    val label: String,
    val decision: String,
) {
    fun toJson(): JSONObject {
        return JSONObject()
            .put("id", id)
            .put("label", label)
            .put("decision", decision)
    }

    companion object {
        fun fromJson(json: JSONObject): ApprovalOption? {
            val id = json.stringOrBlank("id")
                .ifBlank { json.stringOrBlank("option_id") }
            if (id.isBlank()) return null
            val kind = json.stringOrBlank("kind").lowercase()
            val decision = json.stringOrBlank("decision").ifBlank {
                if (kind.contains("reject") || kind.contains("deny")) "deny" else "allow"
            }
            return ApprovalOption(
                id = id,
                label = json.stringOrBlank("label")
                    .ifBlank { json.stringOrBlank("name") }
                    .ifBlank { id },
                decision = decision,
            )
        }
    }
}

data class HomeApproval(
    val command: String = "",
    val options: List<ApprovalOption> = emptyList(),
) {
    fun toJson(): JSONObject {
        return JSONObject()
            .put("command", command)
            .put("options", JSONArray().apply { options.forEach { put(it.toJson()) } })
    }

    companion object {
        fun fromJson(json: JSONObject?): HomeApproval? {
            if (json == null) return null
            val options = json.optJSONArray("options")
            return HomeApproval(
                command = json.stringOrBlank("command"),
                options = buildList {
                    if (options != null) {
                        for (index in 0 until options.length()) {
                            options.optJSONObject(index)
                                ?.let(ApprovalOption::fromJson)
                                ?.let(::add)
                        }
                    }
                },
            )
        }
    }
}

data class HomeCollab(
    val children: Int = 0,
    val running: Int = 0,
    val needsYou: Int = 0,
) {
    fun toJson(): JSONObject {
        return JSONObject()
            .put("children", children)
            .put("running", running)
            .put("needs_you", needsYou)
    }
}

data class HomeAttention(
    val session: SessionMeta,
    val reason: String,
    val sinceAway: Boolean = false,
    val eventId: Long = 0L,
    val unseenCount: Int = 0,
    val headline: String = "",
    val summary: String = "",
    val at: String = "",
    val actions: List<String> = emptyList(),
    val approval: HomeApproval? = null,
    val collab: HomeCollab? = null,
) {
    val sessionId: String
        get() = session.sessionId

    fun toJson(): JSONObject {
        return JSONObject()
            .put("session", session.toJson())
            .put("reason", reason)
            .put("since_away", sinceAway)
            .put("event_id", eventId)
            .put("unseen_count", unseenCount)
            .put("headline", headline)
            .put("summary", summary)
            .put("at", at)
            .put("actions", JSONArray(actions))
            .put("approval", approval?.toJson() ?: JSONObject.NULL)
            .put("collab", collab?.toJson() ?: JSONObject.NULL)
    }

    companion object {
        fun fromJson(json: JSONObject): HomeAttention? {
            val sessionJson = json.optJSONObject("session") ?: return null
            val session = SessionMeta.fromJson(sessionJson)
            if (session.sessionId.isBlank()) return null
            val actions = json.optJSONArray("actions")
            val collab = json.optJSONObject("collab")
            return HomeAttention(
                session = session,
                reason = json.stringOrBlank("reason"),
                sinceAway = json.optBoolean("since_away", false),
                eventId = json.optLong("event_id", 0L),
                unseenCount = json.optInt("unseen_count", 0),
                headline = json.stringOrBlank("headline"),
                summary = json.stringOrBlank("summary"),
                at = json.stringOrBlank("at"),
                actions = buildList {
                    if (actions != null) {
                        for (index in 0 until actions.length()) {
                            actions.optString(index)
                                .takeIf { it.isNotBlank() }
                                ?.let(::add)
                        }
                    }
                },
                approval = HomeApproval.fromJson(json.optJSONObject("approval")),
                collab = collab?.let {
                    HomeCollab(
                        children = it.optInt("children", 0),
                        running = it.optInt("running", 0),
                        needsYou = it.optInt("needs_you", 0),
                    )
                },
            )
        }
    }
}

data class HomeFeed(
    val away: HomeAway = HomeAway(),
    val attention: List<HomeAttention> = emptyList(),
    val running: List<SessionMeta> = emptyList(),
    val resume: List<SessionMeta> = emptyList(),
    val computers: List<HomeComputerStatus> = emptyList(),
    val earlierCount: Int = 0,
    val earlierNextCursor: String = "",
    val needsYouCount: Int = 0,
    val awayNewCount: Int = 0,
    val runningCount: Int = 0,
    val newActivityScope: String = "away",
    val generatedAt: String = "",
    val available: Boolean = false,
) {
    val attentionSessions: List<SessionMeta>
        get() = attention.map { it.session }

    fun toJson(): JSONObject {
        return JSONObject()
            .put("away", away.toJson())
            .put("attention", JSONArray().apply {
                attention.forEach { put(it.toJson()) }
            })
            .put("running", JSONArray().apply {
                running.forEach { put(it.toJson()) }
            })
            .put("resume", JSONArray().apply {
                resume.forEach { put(it.toJson()) }
            })
            .put("computers", JSONArray().apply {
                computers.forEach { put(it.toJson()) }
            })
            .put(
                "earlier",
                JSONObject()
                    .put("count", earlierCount)
                    .put("next_cursor", earlierNextCursor),
            )
            .put(
                "counts",
                JSONObject()
                    .put("needs_you", needsYouCount)
                    .put("away_new", awayNewCount)
                    .put("running", runningCount),
            )
            .put("settings", JSONObject().put("new_activity", newActivityScope))
            .put("generated_at", generatedAt)
    }

    companion object {
        fun fromJson(json: JSONObject): HomeFeed {
            fun sessions(key: String): List<SessionMeta> {
                val array = json.optJSONArray(key) ?: return emptyList()
                return buildList {
                    for (index in 0 until array.length()) {
                        val item = array.optJSONObject(index) ?: continue
                        add(SessionMeta.fromJson(item))
                    }
                }
            }
            val awayJson = json.optJSONObject("away")
            val attentionJson = json.optJSONArray("attention")
            val computersJson = json.optJSONArray("computers")
            val earlier = json.optJSONObject("earlier")
            val counts = json.optJSONObject("counts")
            val settings = json.optJSONObject("settings")
            return HomeFeed(
                away = HomeAway(
                    enabled = awayJson?.optBoolean("enabled", false) ?: false,
                    current = AwayPeriod.fromJson(awayJson?.optJSONObject("current")),
                    period = AwayPeriod.fromJson(awayJson?.optJSONObject("period")),
                ),
                attention = buildList {
                    if (attentionJson != null) {
                        for (index in 0 until attentionJson.length()) {
                            val item = attentionJson.optJSONObject(index) ?: continue
                            HomeAttention.fromJson(item)?.let(::add)
                        }
                    }
                },
                running = sessions("running"),
                resume = sessions("resume"),
                computers = buildList {
                    if (computersJson != null) {
                        for (index in 0 until computersJson.length()) {
                            val item = computersJson.optJSONObject(index) ?: continue
                            HomeComputerStatus.fromJson(item)?.let(::add)
                        }
                    }
                },
                earlierCount = earlier?.optInt("count", 0) ?: 0,
                earlierNextCursor = earlier?.stringOrBlank("next_cursor").orEmpty(),
                needsYouCount = counts?.optInt("needs_you", 0) ?: 0,
                awayNewCount = counts?.optInt("away_new", 0) ?: 0,
                runningCount = counts?.optInt("running", 0) ?: 0,
                newActivityScope = settings?.stringOrBlank("new_activity")
                    ?.ifBlank { "away" }
                    ?: "away",
                generatedAt = json.stringOrBlank("generated_at"),
                available = true,
            )
        }
    }
}

data class AgentUiState(
    val settings: AppSettings,
    val connection: ConnectionState = ConnectionState.DISCONNECTED,
    val transportKind: TransportKind = TransportKind.LAN,
    val connectionDiagnostics: ConnectionDiagnostics = ConnectionDiagnostics(),
    val events: Map<Long, AgentEvent> = emptyMap(),
    val replies: Map<Long, AgentReply> = emptyMap(),
    val sessionMeta: Map<String, SessionMeta> = emptyMap(),
    val agentDirectory: AgentDirectory = AgentDirectory(),
    val agentCatalog: AgentCatalog = AgentCatalog(),
    val dispatches: Map<String, AgentDispatch> = emptyMap(),
    val sessionItems: Map<String, SessionItemsState> = emptyMap(),
    val sessionApiAvailable: Boolean = true,
    val transcriptCache: Map<String, TranscriptPage> = emptyMap(),
    val projects: List<ProjectInfo> = emptyList(),
    val away: Boolean = false,
    val isRefreshing: Boolean = false,
    val error: String? = null,
    val lastUpdatedAt: Long? = null,
    val localSessionFlags: Map<String, LocalSessionFlags> = emptyMap(),
    val mobileInbox: MobileInboxMode = MobileInboxMode(),
    val phoneConversations: PhoneConversationState = PhoneConversationState(),
    val mobileWorkspace: MobileWorkspaceState = MobileWorkspaceState(),
    val activeComputerKey: String = "",
    val inbox: InboxFeed? = null,
    val home: HomeFeed? = null,
    val sessionDrafts: Map<String, String> = emptyMap(),
    val settledApprovalIds: Set<Long> = emptySet(),
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
            val meta = sessionMeta[key]
            val sessionEvents = grouped[key].orEmpty().filter {
                (meta == null || it.source == meta.source) &&
                    !isIdlePromptNoiseEvent(it.kind, it.text)
            }.sortedBy { it.id }
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
            val wireStatus = SessionStatus.fromWire(meta?.status)
            val itemsState = sessionItems[key]
            val latestSupersededByItems = latest?.isPending == true &&
                itemsState?.items.orEmpty().any { item ->
                    item.type == SessionItemType.ASSISTANT &&
                        !item.partial &&
                        item.text.isNotBlank() &&
                        epochMillis(item.ts) > epochMillis(latest.ts) + 500L
                }
            val latestApprovalSettled = latest != null && (
                latest.id in settledApprovalIds ||
                    (latest.isPermission && latest.read && wireStatus != SessionStatus.NEEDS_PERMISSION) ||
                    latestSupersededByItems
                )
            val status = when {
                latestApprovalSettled -> SessionStatus.COMPLETED
                wireStatus == SessionStatus.WAITING_INPUT &&
                    isIdlePromptNoiseEvent("needs_input", meta?.lastPreview.orEmpty()) ->
                    SessionStatus.fromKind(latest?.kind) ?: SessionStatus.COMPLETED
                wireStatus == SessionStatus.WAITING_INPUT &&
                    sessionEvents.isNotEmpty() &&
                    latest?.kind != "needs_input" ->
                    SessionStatus.fromKind(latest?.kind) ?: SessionStatus.COMPLETED
                else -> wireStatus
                    ?: SessionStatus.fromKind(latest?.kind)
                    ?: SessionStatus.IDLE
            }
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
                source = source,
                brand = brand,
                agentLabel = profile.label,
                agentInitial = profile.initial,
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
                remindOnDone = meta?.remindOnDone ?: false,
                isTemp = meta?.isTemp ?: false,
                isEmpty = meta?.isEmpty ?: false,
                seenAt = meta?.seenAt.orEmpty(),
                collab = meta?.collab,
                computerId = meta?.computerId.orEmpty(),
                computerName = meta?.computerName.orEmpty(),
                pendingSuperseded = latestApprovalSettled,
            )
        }.sortedWith(
            compareByDescending<SessionSummary> { it.pinned }
                .thenByDescending { it.lastTs.ifBlank { Instant.EPOCH.toString() } },
        )
    }

    fun phoneSession(sessionId: String): SessionSummary? {
        val session = session(sessionId) ?: return null
        val key = phoneConversationKey(session.computerId.ifBlank { activeComputerKey }, session.source, sessionId)
        val receipt = phoneConversations.receipts[key]
        val events = session.events.filter { event ->
            event.source == session.source && receipt != null && event.id >= receipt.eventId
        }
        val replies = session.replies.filter { reply ->
            reply.isLocal || (receipt != null && epochMillis(reply.ts) >= epochMillis(receipt.timestamp))
        }
        // Full legacy transcript caches predate phone receipt boundaries. They must never
        // bypass the user's explicit request to load previous context.
        return session.copy(events = events, latestEvent = events.lastOrNull(), replies = replies, transcript = null)
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
    val sourceKind = titleSource?.trim()?.lowercase().orEmpty()
    val rawServer = serverTitle?.trim().orEmpty()
    val server = if (sourceKind == "agent") {
        cleanAgentTitle(rawServer)
    } else {
        rawServer
    }
    val nativeSources = setOf("", "custom", "homing", "agent", "first_prompt")
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

private val markdownHeading = Regex("^#{1,6}\\s+")

private val nativeTitlePrefix = Regex(
    "(?i)^(?:会话标题|會話標題|對話標題|标题|標題|title|session title|conversation title)\\s*[：:]\\s*",
)

private fun cleanAgentTitle(value: String): String {
    var title = value.trim()
    while (true) {
        val withoutHeading = markdownHeading.replace(title, "").trim()
        if (withoutHeading == title) break
        title = withoutHeading
    }
    while (true) {
        val withoutPrefix = nativeTitlePrefix.replace(title, "").trim()
        if (withoutPrefix == title) break
        title = withoutPrefix
    }
    return title
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
        "custom", "homing" -> "用户命名"
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
