package dev.agentpush.poc

import android.app.Application
import android.content.Intent
import android.os.Handler
import android.os.Looper
import dev.agentpush.poc.media.PendingAttachment
import java.io.File
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.plus
import kotlinx.coroutines.withContext
import org.json.JSONObject

data class SettingsUiState(
    val testing: Boolean = false,
    val saving: Boolean = false,
    val testSucceeded: Boolean? = null,
    val message: String? = null,
)

class AgentViewModel(application: Application) : AndroidViewModel(application) {
    private val store = AgentStore.get(application)
    private val _settingsUi = MutableStateFlow(SettingsUiState())
    private val _snackbar = MutableStateFlow<String?>(null)
    private val _pendingNavigationSession = MutableStateFlow<String?>(null)
    private val _sessionSearchHits = MutableStateFlow<List<SessionSearchHit>>(emptyList())
    private val _sessionSearchNotice = MutableStateFlow("")
    private var pendingLaunchRequestId: String? = null
    private var sessionLive: SessionLiveStream? = null
    private var liveSessionId: String? = null
    private val mainHandler = Handler(Looper.getMainLooper())
    private val safeScope = viewModelScope + CrashReporter.coroutineExceptionHandler

    val state: StateFlow<AgentUiState> = store.state
    val settingsUi: StateFlow<SettingsUiState> = _settingsUi.asStateFlow()
    val snackbar: StateFlow<String?> = _snackbar.asStateFlow()
    val pendingNavigationSession: StateFlow<String?> =
        _pendingNavigationSession.asStateFlow()
    val sessionSearchHits: StateFlow<List<SessionSearchHit>> =
        _sessionSearchHits.asStateFlow()
    val sessionSearchNotice: StateFlow<String> =
        _sessionSearchNotice.asStateFlow()

    init {
        safeScope.launch {
            val hasTarget = store.state.value.settings.token.isNotBlank() ||
                dev.agentpush.poc.protocol.PairedComputerStore(application).load().isNotEmpty()
            if (hasTarget) {
                runCatching { store.refreshAll() }
                    .onFailure { showMessage(it.message ?: "同步失败") }
            }
        }
        safeScope.launch {
            store.state.collect { state ->
                val requestId = pendingLaunchRequestId ?: return@collect
                val matched = state.sessionMeta.values.firstOrNull {
                    it.requestId == requestId && it.sessionId.isNotBlank()
                } ?: return@collect
                pendingLaunchRequestId = null
                _pendingNavigationSession.value = matched.sessionId
                showMessage("${matched.brand.displayName} 会话已就绪")
            }
        }
        safeScope.launch {
            // The event bridge is live-only. Catch up after a connection returns,
            // including native transcript items for the conversation left open.
            // Keep the last outage in the key: StateFlow can conflate a fast
            // disconnect/reconnect before the main thread observes either.
            store.state.map { Triple(it.activeComputerKey, it.connection,
                it.connectionDiagnostics.lastDisconnectedAt) }
                .distinctUntilChanged()
                .drop(1) // The initial refresh above already covers the first state.
                .collect { (computer, connection, _) ->
                    if (connection != ConnectionState.CONNECTED) return@collect
                    val sessionId = liveSessionId
                    runCatching { store.refreshAll() }
                    if (sessionId != null && liveSessionId == sessionId &&
                        store.state.value.activeComputerKey == computer) {
                        runCatching { store.refreshSessionDetails(sessionId, force = true) }
                    }
                }
        }
        safeScope.launch {
            withContext(Dispatchers.IO) {
                runCatching { CrashReporter.uploadPending(store.state.value.settings) }
            }
        }
    }

    fun refresh() {
        safeScope.launch {
            runCatching { store.refreshAll() }
                .onFailure { showMessage("刷新失败：${it.message}") }
        }
    }

    fun reconnect() {
        startPushService(reconnect = true)
        refresh()
    }

    fun activateComputer(deviceId: String) {
        store.activateComputer(deviceId)
    }

    fun activeComputerId(): String = store.activeComputerId()
    fun activatePhoneSession(meta: SessionMeta) = store.activatePhoneSession(meta)

    fun clearPhoneInbox() = store.clearPhoneInbox()
    fun hideSessionOnPhone(session: SessionSummary) = store.hideSessionOnPhone(session)
    fun hideSessionsOnPhone(sessions: List<SessionSummary>) = store.hideSessionsOnPhone(sessions)
    fun undoPhoneCleanup() = store.undoPhoneCleanup()
    fun restorePhoneSessions() = store.restorePhoneSessions()

    fun quickClearWorkspaceSessions(candidates: List<SessionSummary>): Int {
        return store.quickClearWorkspaceSessions(candidates)
    }

    suspend fun readProjectFile(path: String, cwd: String): dev.agentpush.poc.ui.RemoteTextFile =
        AgentApi.readProjectFile(store.state.value.settings, path, cwd)

    suspend fun readAttachmentText(attachment: Attachment): String {
        val bytes = AgentApi.downloadAttachmentBytes(store.state.value.settings, attachment)
        val limited = if (bytes.size > 1_048_576) bytes.copyOf(1_048_576) else bytes
        if (limited.take(8192).any { it.toInt() == 0 }) {
            throw java.io.IOException("二进制文件无法以文本方式查看")
        }
        return String(limited, Charsets.UTF_8)
    }

    fun dismissWorkspaceSession(session: SessionSummary): Boolean {
        return store.dismissWorkspaceSession(session)
    }

    fun undoWorkspaceCleanup(): Boolean {
        return store.undoWorkspaceCleanup()
    }

    fun wakeAgent(
        agentId: String,
        onResult: (Result<WakeAgentResult>) -> Unit = {},
    ) {
        safeScope.launch {
            val result = runCatching { store.wakeAgent(agentId) }
            result.onSuccess { res ->
                if (res.message.isNotBlank()) showMessage(res.message)
            }.onFailure { err ->
                showMessage("唤醒失败：${err.message ?: err.javaClass.simpleName}")
            }
            onResult(result)
        }
    }

    fun restoreHistoricalSessions(
        sessionIds: List<String>,
        itemLimit: Int = 10,
        onCompleted: (String?) -> Unit = {},
    ) {
        val clean = sessionIds.map { it.trim() }.filter { it.isNotBlank() }.distinct()
        if (clean.isEmpty()) return
        val firstId = clean.first()
        safeScope.launch {
            runCatching {
                store.restoreHistoricalSessions(clean, itemLimit = itemLimit)
            }.onSuccess { count ->
                _pendingNavigationSession.value = firstId
                onCompleted(firstId)
                showMessage(
                    if (count == 1) {
                        "已恢复会话及最近 $itemLimit 条记录"
                    } else {
                        "已恢复 $count 个会话（各载入最近 $itemLimit 条记录）"
                    },
                )
            }.onFailure { err ->
                showMessage("恢复会话失败：${err.message ?: err.javaClass.simpleName}")
                onCompleted(null)
            }
        }
    }
    fun loadEarlierContext(sessionId: String) {
        safeScope.launch { store.loadEarlierContext(sessionId) }
    }

    fun openSession(sessionId: String) {
        CrashReporter.setLastSession(sessionId)
        NotificationHelper.cancelSession(getApplication(), sessionId)
        store.preparePhoneContext(sessionId)
        startSessionStream(sessionId)
        safeScope.launch {
            store.markSessionRead(sessionId)
            runCatching { store.refreshSessionDetails(sessionId, force = true) }
                .onFailure { showMessage("会话详情暂不可用：${it.message}") }
            runCatching { store.refreshReplies(sessionId) }
                .onFailure { showMessage("回复记录暂不可用") }
        }
    }

    fun closeSessionStream() {
        sessionLive?.stop()
        sessionLive = null
        liveSessionId = null
    }

    private fun startSessionStream(sessionId: String) {
        if (liveSessionId == sessionId && sessionLive != null) return
        closeSessionStream()
        liveSessionId = sessionId
        val computerScope = store.state.value.activeComputerKey
        sessionLive = SessionLiveStream(
            sessionId = sessionId,
            events = store.sessionEvents(sessionId),
            onFrame = { eventName, data ->
                val json = runCatching { JSONObject(data) }.getOrNull()
                if (json != null && store.state.value.activeComputerKey == computerScope && liveSessionId == sessionId) {
                    when (eventName) {
                        "session" -> store.applySessionUpdate(json)
                        "item" -> store.applyItem(json)
                        "turn" -> {
                            store.applyTurn(json)
                            if (
                                json.optString("state") in setOf(
                                    "idle",
                                    "finished",
                                    "failed",
                                    "complete",
                                    "completed",
                                )
                            ) {
                                safeScope.launch {
                                    runCatching {
                                        store.refreshSessionDetails(sessionId, force = true)
                                    }
                                }
                            }
                        }
                        "reply_status" -> store.applyReplyStatus(json)
                        "dispatch" -> store.applyDispatchUpdate(json)
                        "message" -> AgentEvent.fromJson(json).let { event ->
                            if (event.id > 0) store.ingestEvent(event)
                        }
                    }
                }
            },
        ).also { it.start() }
    }

    fun consumePendingNavigation() {
        _pendingNavigationSession.value = null
    }

    fun refreshTranscript(sessionId: String) {
        safeScope.launch {
            runCatching { store.refreshSessionDetails(sessionId, force = true) }
                .onFailure { showMessage("会话详情加载失败：${it.message}") }
        }
    }

    fun setReasoningVisible(sessionId: String, visible: Boolean) {
        safeScope.launch {
            runCatching {
                store.setSessionReasoning(sessionId, enabled = visible)
            }.onFailure {
                showMessage(
                    if (visible) "思考过程加载失败：${it.message}" else "无法收起思考过程",
                )
            }
        }
    }

    fun markSessionRead(sessionId: String) {
        NotificationHelper.cancelSession(getApplication(), sessionId)
        safeScope.launch {
            runCatching { store.markSessionRead(sessionId) }
                .onFailure { showMessage("标记已读失败：${it.message}") }
        }
    }

    fun searchSessionContent(query: String) {
        val clean = query.trim()
        if (clean.isBlank()) {
            _sessionSearchHits.value = emptyList()
            _sessionSearchNotice.value = ""
            return
        }
        _sessionSearchNotice.value = ""
        safeScope.launch {
            runCatching {
                AgentApi.searchSessions(
                    settings = store.state.value.settings,
                    query = clean,
                    scope = "content",
                    limit = 50,
                )
            }.onSuccess {
                _sessionSearchHits.value = it
            }.onFailure {
                _sessionSearchHits.value = emptyList()
                _sessionSearchNotice.value = if (it is AgentApiException && it.statusCode == 409) {
                    it.message?.takeIf(String::isNotBlank) ?: "正文索引已关闭，可在设置中重新开启"
                } else {
                    "正文搜索失败：${it.message}"
                }
            }
        }
    }

    fun clearSessionSearch() {
        _sessionSearchHits.value = emptyList()
        _sessionSearchNotice.value = ""
    }

    fun updateDraft(sessionId: String, text: String) {
        store.updateDraft(sessionId, text)
    }

    fun clearDraft(sessionId: String) {
        store.clearDraft(sessionId)
    }

    fun clearLocalCache() {
        safeScope.launch {
            runCatching { store.clearLocalCache() }
                .onSuccess { showMessage("已清除本地快照、会话缓存和草稿") }
                .onFailure { showMessage("清除本地缓存失败：${it.message}") }
        }
    }

    fun markAllRead() {
        safeScope.launch {
            store.state.value.sessions().forEach { session ->
                NotificationHelper.cancelSession(getApplication(), session.sessionId)
            }
            runCatching { store.markAllRead() }
                .onFailure { showMessage("全部已读失败：${it.message}") }
        }
    }

    fun approve(session: SessionSummary) {
        safeScope.launch {
            val permissionEventId = session.pendingEvent
                ?.takeIf { it.isPermission }
                ?.id
            if (
                permissionEventId != null &&
                session.capabilities.approve
            ) {
                val result = store.decideApproval(
                    permissionEventId,
                    "allow",
                    session.sessionId,
                )
                if (result.ok) {
                    showMessage("已允许")
                    return@launch
                }
            }
            val reply = store.sendReply(
                sessionId = session.sessionId,
                eventId = session.replyEventId,
                source = session.brand.wire,
                text = "同意",
            )
            if (reply.status == ReplyStatus.FAILED) {
                showMessage("批准失败：${reply.message ?: "请稍后重试"}")
            } else {
                showMessage("已发送「同意」")
            }
        }
    }

    fun deny(session: SessionSummary) {
        safeScope.launch {
            val permissionEventId = session.pendingEvent
                ?.takeIf { it.isPermission }
                ?.id
            if (
                permissionEventId != null &&
                session.capabilities.approve
            ) {
                val result = store.decideApproval(
                    permissionEventId,
                    "deny",
                    session.sessionId,
                )
                if (result.ok) {
                    showMessage("已拒绝")
                    return@launch
                }
            }
            val reply = store.sendReply(
                sessionId = session.sessionId,
                eventId = session.replyEventId,
                source = session.brand.wire,
                text = "拒绝",
            )
            if (reply.status == ReplyStatus.FAILED) {
                showMessage("拒绝失败：${reply.message ?: "请稍后重试"}")
            } else {
                showMessage("已发送「拒绝」")
            }
        }
    }

    fun setPinned(sessionId: String, pinned: Boolean) {
        safeScope.launch { store.setSessionPinned(sessionId, pinned) }
    }

    fun setMuted(sessionId: String, muted: Boolean) {
        safeScope.launch { store.setSessionMuted(sessionId, muted) }
    }

    fun setArchived(sessionId: String, archived: Boolean) {
        safeScope.launch { store.setSessionArchived(sessionId, archived) }
    }

    fun setInboxMode(enabled: Boolean) {
        toggleAwayMonitoring(enabled)
    }

    fun toggleAwayMonitoring(enabled: Boolean) {
        store.setInboxMode(enabled)
        safeScope.launch {
            runCatching { store.setAway(enabled) }
                .onSuccess { actual ->
                    showMessage(
                        if (actual) {
                            "已开启离席监听：仅接收开启后的新消息"
                        } else {
                            "已关闭离席监听：在电脑前工作不打扰手机"
                        },
                    )
                }
                .onFailure {
                    showMessage("切换离席监听失败：${it.message}")
                }
        }
    }

    fun setSessionResident(sessionId: String, resident: Boolean) {
        store.setSessionResident(sessionId, resident)
        showMessage(if (resident) "已设为常驻" else "已取消常驻")
    }

    fun setAgentConnected(
        agentId: String,
        connected: Boolean,
        onResult: (Result<Unit>) -> Unit = {},
    ) {
        safeScope.launch {
            val result = runCatching {
                store.setAgentConnected(agentId, connected)
            }.map { action ->
                if (action.message.isNotBlank()) showMessage(action.message)
                Unit
            }
            result.onFailure {
                showMessage(
                    (if (connected) "接入失败：" else "关闭接入失败：") +
                        (it.message ?: it.javaClass.simpleName),
                )
            }
            onResult(result)
        }
    }

    fun setRemindOnDone(sessionId: String, enabled: Boolean) {
        safeScope.launch {
            store.setSessionRemindOnDone(sessionId, enabled)
            showMessage(if (enabled) "完成时会单独提醒你" else "已取消完成提醒")
        }
    }

    fun renameSession(sessionId: String, title: String) {
        safeScope.launch {
            if (store.setSessionTitle(sessionId, title)) {
                showMessage("标题已更新")
            } else {
                showMessage("重命名失败，请确认服务端支持自定义标题")
            }
        }
    }

    fun cancelQueuedReplies(sessionId: String) {
        safeScope.launch {
            val count = store.cancelQueuedReplies(sessionId)
            showMessage(
                if (count > 0) {
                    "已撤回 $count 条排队回复"
                } else {
                    "没有可撤回的排队回复，可能已经开始执行"
                },
            )
        }
    }

    fun cancelQueuedReply(reply: AgentReply, onResult: (Boolean) -> Unit = {}) {
        safeScope.launch {
            val cancelled = store.cancelQueuedReply(reply)
            showMessage(
                if (cancelled) {
                    "已撤回这条排队回复"
                } else {
                    "撤回失败，可能已经开始执行"
                },
            )
            onResult(cancelled)
        }
    }

    fun sendReply(
        session: SessionSummary,
        text: String,
        eventId: Long = session.latestEvent?.id ?: 0L,
        files: List<String> = emptyList(),
        model: String? = null,
        effort: String? = null,
        permissionMode: String? = null,
    ) {
        if (text.isBlank() && files.isEmpty()) {
            return
        }
        safeScope.launch {
            val sendText = text.trim().ifBlank {
                if (files.isNotEmpty()) "（发送了附件）" else ""
            }
            if (sendText.isBlank()) return@launch
            val reply = store.sendReply(
                sessionId = session.sessionId,
                eventId = eventId.takeIf { it > 0 },
                source = session.brand.wire,
                text = sendText,
                files = files,
                model = model,
                effort = effort,
                permissionMode = permissionMode,
            )
            if (reply.status == ReplyStatus.FAILED) {
                showMessage("发送失败：${reply.message ?: "请稍后重试"}")
            }
        }
    }

    fun sendReplyTo(
        sessionId: String,
        source: String,
        eventId: Long,
        text: String,
    ) {
        if (sessionId.isBlank() || text.isBlank()) return
        safeScope.launch {
            val reply = store.sendReply(
                sessionId = sessionId,
                eventId = eventId.takeIf { it > 0 },
                source = source,
                text = text.trim(),
            )
            if (reply.status == ReplyStatus.FAILED) {
                showMessage("发送失败：${reply.message ?: "请稍后重试"}")
            }
        }
    }

    fun dispatchAgents(
        session: SessionSummary,
        eventId: Long,
        mentions: List<AgentMention>,
    ) {
        if (mentions.isEmpty()) return
        if (session.cwd.isBlank()) {
            showMessage("这个会话没有工作目录，无法派生子任务")
            return
        }
        safeScope.launch {
            var succeeded = 0
            val failures = mutableListOf<String>()
            val waiting = mutableListOf<String>()
            for (mention in mentions) {
                if (mention.text.isBlank()) {
                    failures += "@${mention.agentId} 缺少任务"
                    continue
                }
                runCatching {
                    store.dispatch(
                        parentSessionId = session.sessionId,
                        parentSource = session.brand.wire,
                        parentEventId = eventId.takeIf { it > 0 },
                        targetAgent = mention.agentId,
                        text = mention.text,
                        cwd = session.cwd,
                    )
                }.onSuccess { dispatch ->
                    succeeded++
                    if (dispatch.status == DispatchStatus.WAITING_SESSION) {
                        waiting += AgentBrand.fromWire(dispatch.targetAgent).displayName
                    }
                }.onFailure { error ->
                    failures += "@${mention.agentId}：${error.message ?: "派发失败"}"
                    runCatching { store.refreshDispatches(session.sessionId) }
                }
            }
            when {
                failures.isNotEmpty() && succeeded == 0 ->
                    showMessage("跨 Agent 派发失败：${failures.first()}")
                failures.isNotEmpty() ->
                    showMessage("已派发 $succeeded 个任务；${failures.first()}")
                waiting.isNotEmpty() ->
                    showMessage("${waiting.distinct().joinToString("、")} 已启动，等待会话回传")
                else ->
                    showMessage("已派发 $succeeded 个跨 Agent 任务")
            }
        }
    }

    fun compose(
        session: SessionSummary,
        text: String,
        plan: ComposerDispatchPlan,
        files: List<String>,
        model: String?,
        effort: String?,
        permissionMode: String?,
        mentionOverrides: List<ComposeMentionOverride>,
        onResult: (Boolean) -> Unit,
    ) {
        if (text.isBlank() || !plan.hasMentions) {
            onResult(false)
            return
        }
        if (session.cwd.isBlank()) {
            showMessage("这个会话没有工作目录，无法派生子任务")
            onResult(false)
            return
        }
        safeScope.launch {
            val result = runCatching {
                store.compose(
                    sessionId = session.sessionId,
                    source = session.brand.wire,
                    text = text,
                    parentText = plan.message,
                    files = files,
                    model = model,
                    effort = effort,
                    permissionMode = permissionMode,
                    mentionOverrides = mentionOverrides,
                )
            }
            result.onSuccess { composed ->
                val count = composed.dispatches.size
                val waiting = composed.dispatches
                    .filter { it.status == DispatchStatus.WAITING_SESSION }
                    .map { it.targetBrand.displayName }
                    .distinct()
                when {
                    count == 0 -> showMessage("已发送，但没有建立协作任务")
                    waiting.isNotEmpty() ->
                        showMessage("已派发 $count 个任务；${waiting.joinToString("、")} 等待会话回传")
                    else -> showMessage("已派发 $count 个跨 Agent 任务")
                }
                onResult(true)
            }.onFailure { error ->
                showMessage("跨 Agent 派发失败：${error.message ?: "请稍后重试"}")
                onResult(false)
            }
        }
    }

    fun previewCompose(
        session: SessionSummary,
        text: String,
        files: List<String>,
        model: String?,
        effort: String?,
        permissionMode: String?,
        mentionOverrides: List<ComposeMentionOverride>,
        onResult: (ComposeResult?) -> Unit,
    ) {
        if (text.isBlank()) {
            onResult(null)
            return
        }
        if (session.cwd.isBlank()) {
            showMessage("这个会话没有工作目录，无法派生子任务")
            onResult(null)
            return
        }
        safeScope.launch {
            runCatching {
                store.previewCompose(
                    sessionId = session.sessionId,
                    source = session.brand.wire,
                    text = text,
                    files = files,
                    model = model,
                    effort = effort,
                    permissionMode = permissionMode,
                    mentionOverrides = mentionOverrides,
                )
            }.onSuccess { onResult(it) }
                .onFailure {
                    showMessage("无法生成发送预览：${it.message ?: "请稍后重试"}")
                    onResult(null)
                }
        }
    }

    fun setPreviewMentions(enabled: Boolean) {
        store.setPreviewMentions(enabled)
    }

    fun forwardDispatch(dispatch: AgentDispatch, text: String? = null) {
        safeScope.launch {
            runCatching { store.forwardDispatch(dispatch.id, text) }
                .onSuccess { showMessage("结论已转交给父 Agent") }
                .onFailure { showMessage("转交失败：${it.message ?: "请稍后重试"}") }
        }
    }

    fun cancelDispatch(dispatch: AgentDispatch) {
        safeScope.launch {
            runCatching { store.cancelDispatch(dispatch.id) }
                .onSuccess {
                    showMessage(
                        if (it.message.contains("不是 Homing 启动")) {
                            "已标记取消；该会话不是 Homing 启动的进程"
                        } else {
                            "已取消协作任务"
                        },
                    )
                }
                .onFailure { showMessage("取消失败：${it.message ?: "请稍后重试"}") }
        }
    }

    fun discardDispatchWorktree(dispatch: AgentDispatch) {
        safeScope.launch {
            runCatching { store.discardDispatchWorktree(dispatch.id) }
                .onSuccess { showMessage("已丢弃协作 worktree 与分支") }
                .onFailure { showMessage("丢弃 worktree 失败：${it.message ?: "请稍后重试"}") }
        }
    }

    fun retryReply(reply: AgentReply) {
        safeScope.launch {
            val retried = store.retryReply(reply)
            if (retried.status == ReplyStatus.FAILED) {
                showMessage("重试失败：${retried.message ?: "请稍后重试"}")
            }
        }
    }

    fun stopSession(sessionId: String) {
        safeScope.launch {
            val result = store.stopSession(sessionId)
            showMessage(
                if (result.ok) {
                    "已请求停止这个回合"
                } else {
                    "无法停止：${result.message ?: "中继未开启该能力"}"
                },
            )
        }
    }

    suspend fun loadSessionCommands(sessionId: String): List<SessionCommand> {
        if (sessionId.isBlank()) return emptyList()
        return runCatching {
            AgentApi.fetchSessionCommands(store.state.value.settings, sessionId)
        }.getOrDefault(emptyList())
    }

    suspend fun completeFiles(cwd: String, prefix: String): List<CompletedPath> {
        if (cwd.isBlank()) return emptyList()
        return runCatching {
            AgentApi.completeFiles(store.state.value.settings, cwd, prefix)
        }.getOrDefault(emptyList())
    }

    fun decideApproval(
        eventId: Long,
        decision: String,
        onFallbackText: (() -> Unit)? = null,
        optionId: String? = null,
    ) {
        safeScope.launch {
            val result = store.decideApproval(
                eventId = eventId,
                decision = decision,
                optionId = optionId,
            )
            if (!result.ok) {
                if (result.errorCode == "not_pending" || result.message == "not_pending") {
                    showMessage(result.message ?: "该授权请求已在电脑端处理或已过期，已自动清除")
                    val sessionId = store.state.value.events[eventId]?.sessionId
                    runCatching { store.refreshAll() }
                    if (!sessionId.isNullOrBlank()) {
                        runCatching { store.refreshSessionDetails(sessionId, force = true) }
                        runCatching { store.refreshReplies(sessionId) }
                    }
                } else if (onFallbackText != null && optionId.isNullOrBlank()) {
                    onFallbackText()
                } else {
                    showMessage("授权操作失败：${result.message ?: "请稍后重试"}")
                    val sessionId = store.state.value.events[eventId]?.sessionId
                    runCatching { store.refreshAll() }
                    if (!sessionId.isNullOrBlank()) {
                        runCatching { store.refreshSessionDetails(sessionId, force = true) }
                        runCatching { store.refreshReplies(sessionId) }
                    }
                }
            } else {
                showMessage(if (decision == "allow") "已允许" else "已拒绝")
            }
        }
    }

    fun uploadAttachment(
        eventId: Long?,
        cwd: String? = null,
        file: File,
        name: String,
        mime: String,
        onProgress: (Float) -> Unit,
        onResult: (Result<UploadResult>) -> Unit,
    ) {
        safeScope.launch {
            val result = runCatching {
                if (eventId != null && eventId > 0) {
                    AgentApi.upload(
                        settings = store.state.value.settings,
                        eventId = eventId,
                        file = file,
                        name = name,
                        mime = mime,
                        onProgress = { progress ->
                            mainHandler.post { onProgress(progress) }
                        },
                    )
                } else {
                    AgentApi.uploadForCwd(
                        settings = store.state.value.settings,
                        cwd = cwd.orEmpty(),
                        file = file,
                        name = name,
                        mime = mime,
                        onProgress = { progress ->
                            mainHandler.post { onProgress(progress) }
                        },
                    )
                }
            }
            result.onFailure {
                showMessage("上传失败：${it.message ?: it.javaClass.simpleName}")
            }
            onResult(result)
        }
    }

    fun refreshProjects(onResult: (Result<List<ProjectInfo>>) -> Unit = {}) {
        safeScope.launch {
            val result = runCatching { store.refreshProjects() }
            result.onFailure { showMessage("项目列表加载失败：${it.message}") }
            onResult(result.map { store.state.value.projects })
        }
    }

    suspend fun refreshAgentDirectory(): Result<AgentDirectory> {
        return runCatching { store.refreshAgentDirectory() }
    }

    fun createSession(
        source: String,
        cwd: String,
        text: String,
        localFiles: List<PendingAttachment>,
        model: String? = null,
        effort: String? = null,
        permissionMode: String? = null,
        onUploadProgress: (index: Int, total: Int, name: String, progress: Float) -> Unit =
            { _, _, _, _ -> },
        onResult: (NewSessionResult) -> Unit,
    ) {
        safeScope.launch {
            val result = runCatching {
                val uploadedPaths = mutableListOf<String>()
                localFiles.forEachIndexed { index, attachment ->
                    mainHandler.post {
                        onUploadProgress(index, localFiles.size, attachment.name, 0f)
                    }
                    val uploaded = AgentApi.uploadForCwd(
                        settings = store.state.value.settings,
                        cwd = cwd,
                        file = attachment.file,
                        name = attachment.name,
                        mime = attachment.mime,
                        onProgress = { progress ->
                            mainHandler.post {
                                onUploadProgress(
                                    index,
                                    localFiles.size,
                                    attachment.name,
                                    progress,
                                )
                            }
                        },
                    )
                    uploadedPaths += uploaded.path
                }
                AgentApi.createSession(
                    settings = store.state.value.settings,
                    source = source,
                    cwd = cwd,
                    text = text,
                    files = uploadedPaths,
                    model = model,
                    effort = effort,
                    permissionMode = permissionMode,
                )
            }.getOrElse { error ->
                NewSessionResult(
                    ok = false,
                    sessionId = null,
                    status = "failed",
                    error = error.message ?: error.javaClass.simpleName,
                    cwd = cwd,
                )
            }
            if (result.ok && result.sessionId != null) {
                store.mergeCreatedSession(result.sessionId, source, result.cwd.ifBlank { cwd })
                refresh()
            }
            if (result.ok && result.sessionId == null && !result.requestId.isNullOrBlank()) {
                pendingLaunchRequestId = result.requestId
                showMessage("会话正在启动，就绪后会自动打开")
            }
            onResult(result)
        }
    }

    fun testConnection(baseUrl: String, token: String) {
        if (baseUrl.isBlank()) {
            _settingsUi.update {
                it.copy(testSucceeded = false, message = "服务器地址不能为空")
            }
            return
        }
        if (token.isBlank()) {
            _settingsUi.update {
                it.copy(testSucceeded = false, message = "Token 不能为空")
            }
            return
        }
        _settingsUi.update {
            it.copy(testing = true, testSucceeded = null, message = null)
        }
        safeScope.launch {
            runCatching { store.testConnection(baseUrl, token) }
                .onSuccess { message ->
                    _settingsUi.update {
                        it.copy(testing = false, testSucceeded = true, message = message)
                    }
                }
                .onFailure { error ->
                    _settingsUi.update {
                        it.copy(
                            testing = false,
                            testSucceeded = false,
                            message = "连接失败：${error.message ?: error.javaClass.simpleName}",
                        )
                    }
                }
        }
    }

    fun saveAndReconnect(baseUrl: String, token: String) {
        if (baseUrl.isBlank()) {
            _settingsUi.update {
                it.copy(testSucceeded = false, message = "服务器地址不能为空")
            }
            return
        }
        if (token.isBlank()) {
            _settingsUi.update {
                it.copy(testSucceeded = false, message = "Token 不能为空")
            }
            return
        }
        _settingsUi.update { it.copy(saving = true, message = null) }
        safeScope.launch {
            store.saveSettings(baseUrl, token)
            startPushService(reconnect = true)
            _settingsUi.update {
                it.copy(
                    saving = false,
                    testSucceeded = true,
                    message = "已保存，正在重新连接",
                )
            }
            refresh()
        }
    }

    fun startPushService(reconnect: Boolean = false) {
        val application = getApplication<Application>()
        val intent = Intent(application, PushService::class.java).apply {
            if (reconnect) {
                action = PushService.ACTION_RECONNECT
            }
        }
        runCatching { application.startForegroundService(intent) }
            .onSuccess { store.markConnecting() }
            .onFailure { error ->
                CrashReporter.record(error, origin = "foreground-service")
                showMessage("后台服务启动失败：${error.javaClass.simpleName}")
            }
    }

    fun showMessage(message: String) {
        _snackbar.value = message
    }

    fun consumeSnackbar() {
        _snackbar.value = null
    }
}
