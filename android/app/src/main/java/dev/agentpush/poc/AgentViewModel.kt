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

    init {
        safeScope.launch {
            runCatching { store.refreshAll() }
                .onFailure { showMessage(it.message ?: "同步失败") }
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

    fun openSession(sessionId: String) {
        CrashReporter.setLastSession(sessionId)
        NotificationHelper.cancelSession(getApplication(), sessionId)
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
        sessionLive = SessionLiveStream(
            sessionId = sessionId,
            settingsProvider = { store.state.value.settings },
            onFrame = { eventName, data ->
                val json = runCatching { JSONObject(data) }.getOrNull()
                if (json != null) {
                    when (eventName) {
                        "session" -> store.applySessionUpdate(json)
                        "item" -> store.applyItem(json)
                        "turn" -> {
                            store.applyTurn(json)
                            if (
                                json.optString("state") in setOf(
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

    fun markSessionRead(sessionId: String) {
        NotificationHelper.cancelSession(getApplication(), sessionId)
        safeScope.launch {
            runCatching { store.markSessionRead(sessionId) }
                .onFailure { showMessage("标记已读失败：${it.message}") }
        }
    }

    fun setAway(enabled: Boolean) {
        safeScope.launch {
            runCatching { store.setAway(enabled) }
                .onFailure { showMessage("离开模式切换失败：${it.message}") }
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

    fun sendReply(
        session: SessionSummary,
        text: String,
        eventId: Long = session.latestEvent?.id ?: 0L,
        files: List<String> = emptyList(),
        model: String? = null,
        effort: String? = null,
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
            )
            if (reply.status == ReplyStatus.FAILED) {
                showMessage("发送失败：${reply.message ?: "请稍后重试"}")
            }
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

    fun decideApproval(
        eventId: Long,
        decision: String,
        onFallbackText: (() -> Unit)? = null,
    ) {
        safeScope.launch {
            val result = store.decideApproval(eventId, decision)
            if (!result.ok) {
                if (onFallbackText != null) {
                    onFallbackText()
                } else {
                    showMessage("授权操作失败：${result.message ?: "请稍后重试"}")
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

    fun createSession(
        source: String,
        cwd: String,
        text: String,
        localFiles: List<PendingAttachment>,
        model: String? = null,
        effort: String? = null,
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
