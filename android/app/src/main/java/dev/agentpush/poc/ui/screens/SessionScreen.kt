package dev.agentpush.poc.ui.screens

import android.Manifest
import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.PackageManager
import android.speech.RecognizerIntent
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.CallSplit
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.outlined.AttachFile
import androidx.compose.material.icons.outlined.AlternateEmail
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.Mic
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material.icons.outlined.Stop
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TextFieldDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.content.ContextCompat
import androidx.core.content.FileProvider
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.agentpush.poc.AgentApi
import dev.agentpush.poc.AgentBrand
import dev.agentpush.poc.AgentDispatch
import dev.agentpush.poc.AgentEvent
import dev.agentpush.poc.AgentMention
import dev.agentpush.poc.AgentMentionOptions
import dev.agentpush.poc.AgentProfile
import dev.agentpush.poc.AgentReply
import dev.agentpush.poc.AgentUiState
import dev.agentpush.poc.AgentViewModel
import dev.agentpush.poc.AppSettings
import dev.agentpush.poc.ApprovalOption
import dev.agentpush.poc.Attachment
import dev.agentpush.poc.ContextUsage
import dev.agentpush.poc.ComposeMentionOverride
import dev.agentpush.poc.ComposerDispatchPlan
import dev.agentpush.poc.ComposeResult
import dev.agentpush.poc.CompletedPath
import dev.agentpush.poc.DispatchStatus
import dev.agentpush.poc.ReplyStatus
import dev.agentpush.poc.SessionItem
import dev.agentpush.poc.SessionItemType
import dev.agentpush.poc.SessionHistoryState
import dev.agentpush.poc.SessionCommand
import dev.agentpush.poc.SessionStatus
import dev.agentpush.poc.SessionSummary
import dev.agentpush.poc.TranscriptMessage
import dev.agentpush.poc.UploadResult
import dev.agentpush.poc.agentMentionOptionKey
import dev.agentpush.poc.formatClock
import dev.agentpush.poc.latestPendingQuestionItems
import dev.agentpush.poc.capabilityTokenIn
import dev.agentpush.poc.changeAgentMentionPreset
import dev.agentpush.poc.buildSessionMarkdown
import dev.agentpush.poc.parseAgentMentions
import dev.agentpush.poc.phoneConversationKey
import dev.agentpush.poc.relativeTime
import dev.agentpush.poc.sessionMarkdownFileName
import dev.agentpush.poc.titleSourceLabel
import dev.agentpush.poc.media.AttachmentFiles
import dev.agentpush.poc.media.PendingAttachment
import dev.agentpush.poc.media.PendingFiles
import dev.agentpush.poc.ui.AgentGlyph
import dev.agentpush.poc.ui.AgentAvatar
import dev.agentpush.poc.ui.AttachmentCardList
import dev.agentpush.poc.ui.ClaudeOrange
import dev.agentpush.poc.ui.CodexGreen
import dev.agentpush.poc.ui.FullScreenImageViewer
import dev.agentpush.poc.ui.ApprovalItemCard
import dev.agentpush.poc.ui.AssistantItemCard
import dev.agentpush.poc.ui.ErrorItemCard
import dev.agentpush.poc.ui.FileChangeItemCard
import dev.agentpush.poc.ui.ModelPickerSheet
import dev.agentpush.poc.ui.ModelPill
import dev.agentpush.poc.ui.PlanItemCard
import dev.agentpush.poc.ui.PendingTag
import dev.agentpush.poc.ui.ReasoningItemCard
import dev.agentpush.poc.ui.RichMarkdown
import dev.agentpush.poc.ui.SessionStatusBadge
import dev.agentpush.poc.ui.SessionTagStrip
import dev.agentpush.poc.ui.SystemItemRow
import dev.agentpush.poc.ui.ToolCallItemCard
import dev.agentpush.poc.ui.UserItemBubble
import dev.agentpush.poc.ui.DocumentViewerScope
import dev.agentpush.poc.ui.LocalRegisterSnackbarHost
import dev.agentpush.poc.ui.accentColor
import dev.agentpush.poc.ui.rewriteMarkdownImageUrls
import dev.agentpush.poc.ui.theme.HomingMono
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.io.File
import java.time.Instant

internal data class SessionDetailActions(
    val openSession: () -> Unit = {},
    val closeSession: () -> Unit = {},
    val openSessionById: (String) -> Unit = {},
    val sendReply: (
        session: SessionSummary,
        text: String,
        eventId: Long,
        files: List<String>,
        model: String?,
        effort: String?,
        permissionMode: String?,
    ) -> Unit = { _, _, _, _, _, _, _ -> },
    val dispatchAgents: (
        session: SessionSummary,
        eventId: Long,
        mentions: List<AgentMention>,
    ) -> Unit = { _, _, _ -> },
    val forwardDispatch: (AgentDispatch, String?) -> Unit = { _, _ -> },
    val cancelDispatch: (AgentDispatch) -> Unit = {},
    val discardDispatchWorktree: (AgentDispatch) -> Unit = {},
    val compose: (
        session: SessionSummary,
        text: String,
        plan: ComposerDispatchPlan,
        files: List<String>,
        model: String?,
        effort: String?,
        permissionMode: String?,
        mentionOverrides: List<ComposeMentionOverride>,
        onResult: (Boolean) -> Unit,
    ) -> Unit = { _, _, _, _, _, _, _, _, _ -> },
    val previewCompose: (
        session: SessionSummary,
        text: String,
        files: List<String>,
        model: String?,
        effort: String?,
        permissionMode: String?,
        mentionOverrides: List<ComposeMentionOverride>,
        onResult: (ComposeResult?) -> Unit,
    ) -> Unit = { _, _, _, _, _, _, _, _ -> },
    val retryReply: (AgentReply) -> Unit = {},
    val stopSession: (String) -> Unit = {},
    val decideApproval: (
        eventId: Long,
        decision: String,
        onFallbackText: (() -> Unit)?,
        optionId: String?,
    ) -> Unit = { _, _, _, _ -> },
    val refreshTranscript: (String) -> Unit = {},
    val loadEarlierContext: (String) -> Unit = {},
    val setReasoningVisible: (String, Boolean) -> Unit = { _, _ -> },
    val setPinned: (String, Boolean) -> Unit = { _, _ -> },
    val setMuted: (String, Boolean) -> Unit = { _, _ -> },
    val setArchived: (String, Boolean) -> Unit = { _, _ -> },
    val setRemindOnDone: (String, Boolean) -> Unit = { _, _ -> },
    val renameSession: (String, String) -> Unit = { _, _ -> },
    val cancelQueuedReplies: (String) -> Unit = {},
    val cancelQueuedReply: (AgentReply, (Boolean) -> Unit) -> Unit = { _, _ -> },
    val uploadAttachment: (
        eventId: Long?,
        cwd: String?,
        file: java.io.File,
        name: String,
        mime: String,
        onProgress: (Float) -> Unit,
        onResult: (Result<UploadResult>) -> Unit,
    ) -> Unit = { _, _, _, _, _, _, _ -> },
    val saveDraft: (sessionId: String, text: String) -> Unit = { _, _ -> },
    val clearDraft: (sessionId: String) -> Unit = {},
    val toggleAway: (Boolean) -> Unit = {},
    val loadCommands: suspend (sessionId: String) -> List<SessionCommand> = { emptyList() },
    val completeFiles: suspend (cwd: String, prefix: String) -> List<CompletedPath> =
        { _, _ -> emptyList() },
)

@Composable
fun SessionScreen(
    sessionId: String,
    viewModel: AgentViewModel,
    snackbarHostState: SnackbarHostState,
    onBack: () -> Unit,
    onOpenBrowser: (url: String, title: String) -> Unit,
    onOpenSession: (String) -> Unit = {},
    navigationAsDrawer: Boolean = false,
    otherUrgentCount: Int = 0,
    onJumpUrgent: (() -> Unit)? = null,
    returnToSessionTitle: String? = null,
    onReturnPreviousSession: (() -> Unit)? = null,
    onQuickClear: (() -> Unit)? = null,
    projectSiblings: List<SessionSummary> = emptyList(),
    projectHistoryIds: Set<String> = emptySet(),
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val sessionCwd = state.sessionMeta[sessionId]?.cwd.orEmpty()
    DocumentViewerScope(
        loadPath = { path -> viewModel.readProjectFile(path, sessionCwd) },
        loadAttachment = { attachment -> viewModel.readAttachmentText(attachment) },
        onOpenUrl = { url -> onOpenBrowser(url, "网页") },
        settings = state.settings,
        imageUrlForPath = { path -> AgentApi.projectRawFileUrl(state.settings, path, sessionCwd) },
    ) {
    SessionDetailContent(
        state = state,
        sessionId = sessionId,
        snackbarHostState = snackbarHostState,
        onBack = onBack,
        onOpenBrowser = onOpenBrowser,
        navigationAsDrawer = navigationAsDrawer,
        otherUrgentCount = otherUrgentCount,
        onJumpUrgent = onJumpUrgent,
        returnToSessionTitle = returnToSessionTitle,
        onReturnPreviousSession = onReturnPreviousSession,
        onQuickClear = onQuickClear,
        projectSiblings = projectSiblings,
        projectHistoryIds = projectHistoryIds,
        actions = SessionDetailActions(
            openSession = { viewModel.openSession(sessionId) },
            closeSession = { viewModel.closeSessionStream() },
            openSessionById = onOpenSession,
            sendReply = { session, text, eventId, files, model, effort, permissionMode ->
                viewModel.sendReply(
                    session = session,
                    text = text,
                    eventId = eventId,
                    files = files,
                    model = model,
                    effort = effort,
                    permissionMode = permissionMode,
                )
            },
            dispatchAgents = { session, eventId, mentions ->
                viewModel.dispatchAgents(session, eventId, mentions)
            },
            forwardDispatch = viewModel::forwardDispatch,
            cancelDispatch = viewModel::cancelDispatch,
            discardDispatchWorktree = viewModel::discardDispatchWorktree,
            compose = { session, text, plan, files, model, effort, permissionMode, mentionOverrides, onResult ->
                viewModel.compose(
                    session = session,
                    text = text,
                    plan = plan,
                    files = files,
                    model = model,
                    effort = effort,
                    permissionMode = permissionMode,
                    mentionOverrides = mentionOverrides,
                    onResult = onResult,
                )
            },
            previewCompose = { session, text, files, model, effort, permissionMode, mentionOverrides, onResult ->
                viewModel.previewCompose(
                    session = session,
                    text = text,
                    files = files,
                    model = model,
                    effort = effort,
                    permissionMode = permissionMode,
                    mentionOverrides = mentionOverrides,
                    onResult = onResult,
                )
            },
            retryReply = viewModel::retryReply,
            stopSession = viewModel::stopSession,
            decideApproval = { eventId, decision, fallback, optionId ->
                viewModel.decideApproval(eventId, decision, fallback, optionId)
            },
            refreshTranscript = { viewModel.refreshTranscript(sessionId) },
            loadEarlierContext = viewModel::loadEarlierContext,
            setReasoningVisible = viewModel::setReasoningVisible,
            setPinned = viewModel::setPinned,
            setMuted = viewModel::setMuted,
            setArchived = viewModel::setArchived,
            setRemindOnDone = viewModel::setRemindOnDone,
            renameSession = viewModel::renameSession,
            toggleAway = viewModel::toggleAwayMonitoring,
            cancelQueuedReplies = viewModel::cancelQueuedReplies,
            cancelQueuedReply = viewModel::cancelQueuedReply,
            uploadAttachment = { eventId, cwd, file, name, mime, onProgress, onResult ->
                viewModel.uploadAttachment(
                    eventId = eventId,
                    cwd = cwd,
                    file = file,
                    name = name,
                    mime = mime,
                    onProgress = onProgress,
                    onResult = onResult,
                )
            },
            saveDraft = viewModel::updateDraft,
            clearDraft = viewModel::clearDraft,
            loadCommands = viewModel::loadSessionCommands,
            completeFiles = viewModel::completeFiles,
        ),
    )
    }
}
@Composable
internal fun SessionDetailContent(
    state: AgentUiState,
    sessionId: String,
    snackbarHostState: SnackbarHostState,
    onBack: () -> Unit,
    onOpenBrowser: (url: String, title: String) -> Unit,
    actions: SessionDetailActions = SessionDetailActions(),
    navigationAsDrawer: Boolean = false,
    otherUrgentCount: Int = 0,
    onJumpUrgent: (() -> Unit)? = null,
    returnToSessionTitle: String? = null,
    onReturnPreviousSession: (() -> Unit)? = null,
    onQuickClear: (() -> Unit)? = null,
    projectSiblings: List<SessionSummary> = emptyList(),
    projectHistoryIds: Set<String> = emptySet(),
) {
    val registerSnackbarHost = LocalRegisterSnackbarHost.current
    DisposableEffect(registerSnackbarHost) {
        val unregister = registerSnackbarHost?.invoke()
        onDispose { unregister?.invoke() }
    }
    val session = remember(
        state.events,
        state.replies,
        state.sessionMeta,
        state.transcriptCache,
        state.sessionItems,
        state.agentDirectory,
        state.phoneConversations,
        state.activeComputerKey,
        sessionId,
    ) {
        state.phoneSession(sessionId)
    }
    val contextKey = session?.let {
        phoneConversationKey(it.computerId.ifBlank { state.activeComputerKey }, it.source, sessionId)
    }
    val itemsState = state.sessionItems[sessionId]?.takeIf {
        it.contextKey.isBlank() || it.contextKey == contextKey
    }
    val history = itemsState?.history ?: SessionHistoryState(managed = true)
    val hasStructuredItems = itemsState?.available == true &&
        itemsState.items.isNotEmpty()
    val agentProfile = remember(state.agentDirectory, session?.brand?.wire) {
        state.agentDirectory.profile(session?.brand?.wire.orEmpty())
    }
    val settings = state.settings
    val proxyCapability = remember(session) {
        val fromEvents = session?.events
            ?.flatMap { it.attachments }
            ?.firstNotNullOfOrNull { capabilityTokenIn(it.url) }
        fromEvents ?: session?.transcript?.messages
            ?.flatMap { it.attachments }
            ?.firstNotNullOfOrNull { capabilityTokenIn(it.url) }
    }
    val context = LocalContext.current
    val clipboard = LocalClipboardManager.current
    val scope = rememberCoroutineScope()
    val listState = rememberLazyListState()
    var sessionMissing by remember(sessionId) { mutableStateOf(false) }
    var transcriptOnlyAgent by rememberSaveable { mutableStateOf(false) }
    var infoExpanded by rememberSaveable { mutableStateOf(false) }
    var searchOpen by rememberSaveable(sessionId) { mutableStateOf(false) }
    var modelPickerOpen by remember { mutableStateOf(false) }
    var selectedModel by rememberSaveable(sessionId) { mutableStateOf("") }
    var selectedEffort by rememberSaveable(sessionId) { mutableStateOf("") }
    var selectedPermissionMode by rememberSaveable(sessionId) { mutableStateOf("") }
    var pendingDockExpanded by rememberSaveable(sessionId) { mutableStateOf(false) }
    var queueSheetOpen by rememberSaveable(sessionId) { mutableStateOf(false) }
    val composerFocusRequester = remember { FocusRequester() }
    var commands by remember(sessionId) { mutableStateOf<List<SessionCommand>>(emptyList()) }
    var showNewContent by remember { mutableStateOf(false) }
    var imageViewer by remember { mutableStateOf<ImageViewerState?>(null) }
    var localhostPrompt by remember { mutableStateOf<String?>(null) }
    var composerText by rememberSaveable(sessionId) { mutableStateOf("") }
    var pendingAttachments by remember { mutableStateOf<List<PendingAttachment>>(emptyList()) }
    var cameraFile by remember { mutableStateOf<java.io.File?>(null) }
    var composePreview by remember(sessionId) { mutableStateOf<ComposeResult?>(null) }
    var composePreviewLoading by remember(sessionId) { mutableStateOf(false) }
    var composeSending by remember(sessionId) { mutableStateOf(false) }
    var mentionOptions by remember(sessionId) {
        mutableStateOf<Map<String, AgentMentionOptions>>(emptyMap())
    }

    val effectiveModel = selectedModel.ifBlank {
        session?.model?.takeIf { it.isNotBlank() }
            ?: agentProfile.defaultModel
            ?: agentProfile.defaultModelOption?.id.orEmpty()
    }
    val effectiveEffort = selectedEffort.ifBlank { session?.effort.orEmpty() }
    val permissionModes = agentProfile.capabilities.permissionModes
    val effectivePermissionMode = selectedPermissionMode.ifBlank {
        session?.permissionMode.orEmpty().ifBlank { "default" }
    }
    val modelOverrideActive = selectedModel.isNotBlank()
    val canSwitchModel = agentProfile.capabilities.switchModel &&
        agentProfile.models.isNotEmpty()
    val dispatchTargets = remember(state.agentDirectory, session?.brand?.wire) {
        state.agentDirectory.agents.values
            .filter { it.capabilities.dispatch }
            .sortedBy { it.source }
    }
    val dispatchTargetIds = remember(dispatchTargets) {
        dispatchTargets.map { it.source }.toSet()
    }
    val dispatchPlan = remember(composerText, dispatchTargetIds) {
        parseAgentMentions(composerText, dispatchTargetIds)
    }
    val activeMentionOptions = remember(mentionOptions, dispatchPlan) {
        dispatchPlan.mentions.mapIndexed { index, mention ->
            val key = agentMentionOptionKey(dispatchPlan.mentions, index)
            key to (mentionOptions[key] ?: AgentMentionOptions())
        }.toMap()
    }
    val mentionOverrides = remember(dispatchPlan, activeMentionOptions) {
        dispatchPlan.mentions.mapIndexed { index, mention ->
            val key = agentMentionOptionKey(dispatchPlan.mentions, index)
            val options = activeMentionOptions[key] ?: AgentMentionOptions()
            ComposeMentionOverride(
                agent = mention.agentId,
                target = options.target,
                context = listOf("recent", "files", "diff").filter { it in options.context },
                workspace = options.workspace,
            )
        }
    }
    val hasUnsupportedDispatch = dispatchPlan.mentions.any {
        !it.dispatchable
    }
    val relatedDispatches = remember(state.dispatches, sessionId) {
        state.dispatches.values
            .filter { it.parentSessionId == sessionId }
            .sortedByDescending { it.createdAt }
    }
    val inboundDispatches = remember(state.dispatches, sessionId) {
        state.dispatches.values
            .filter { it.targetSessionId == sessionId }
            .sortedByDescending { it.createdAt }
    }
    val dispatchTargetStatuses = remember(state.sessionMeta, relatedDispatches) {
        relatedDispatches.associate { dispatch ->
            dispatch.id to dispatch.targetSessionId
                .takeIf { it.isNotBlank() }
                ?.let { state.sessionMeta[it]?.status }
                ?.let { SessionStatus.fromWire(it) }
        }
    }

    LaunchedEffect(sessionId) {
        actions.openSession()
    }
    LaunchedEffect(sessionId) {
        commands = actions.loadCommands(sessionId)
    }
    LaunchedEffect(sessionId, state.sessionDrafts[sessionId]) {
        val draft = state.sessionDrafts[sessionId].orEmpty()
        if (composerText.isBlank() && draft.isNotBlank()) {
            composerText = draft
        }
    }
    DisposableEffect(sessionId) {
        onDispose { actions.closeSession() }
    }
    LaunchedEffect(sessionId, session) {
        if (session == null) {
            delay(7_000)
            sessionMissing = true
        } else {
            sessionMissing = false
        }
    }

    fun openUrl(url: String) {
        val target = browserHttpUrl(url)
        if (target != null && isBrowserLocalUrl(target)) {
            localhostPrompt = url
        } else {
            onOpenBrowser(url, "网页")
        }
    }

    fun copyText(label: String, value: String) {
        if (value.isBlank()) {
            return
        }
        clipboard.setText(AnnotatedString(value))
        scope.launch { snackbarHostState.showSnackbar("已复制$label") }
    }

    fun upload(pending: PendingAttachment) {
        val eventId = session?.replyEventId
        val cwd = session?.cwd.orEmpty()
        if (eventId == null && cwd.isBlank()) {
            Toast.makeText(
                context,
                "这个会话没有可用于上传的 cwd",
                Toast.LENGTH_SHORT,
            ).show()
            return
        }
        pendingAttachments = pendingAttachments.map {
            if (it.id == pending.id) it.copy(uploading = true, progress = 0f, error = null) else it
        }
        actions.uploadAttachment(
            eventId,
            cwd,
            pending.file,
            pending.name,
            pending.mime,
            { progress ->
                pendingAttachments = pendingAttachments.map {
                    if (it.id == pending.id) it.copy(progress = progress) else it
                }
            },
            { result ->
                pendingAttachments = pendingAttachments.map {
                    if (it.id != pending.id) {
                        it
                    } else {
                        result.fold(
                            onSuccess = { upload ->
                                it.copy(
                                    uploading = false,
                                    progress = 1f,
                                    uploadedPath = upload.path,
                                    error = null,
                                )
                            },
                            onFailure = { error ->
                                it.copy(
                                    uploading = false,
                                    error = error.message ?: error.javaClass.simpleName,
                                )
                            },
                        )
                    }
                }
            },
        )
    }

    fun addPending(attachment: PendingAttachment) {
        pendingAttachments = pendingAttachments + attachment
        upload(attachment)
    }

    fun addUris(uris: List<android.net.Uri>) {
        if (uris.isEmpty()) return
        scope.launch {
            uris.forEach { uri ->
                runCatching { PendingFiles.fromUri(context, uri) }
                    .onSuccess { addPending(it) }
                    .onFailure {
                        Toast.makeText(
                            context,
                            "读取附件失败：${it.message}",
                            Toast.LENGTH_SHORT,
                        ).show()
                    }
            }
        }
    }

    val photoPicker = rememberLauncherForActivityResult(
        ActivityResultContracts.PickMultipleVisualMedia(10),
    ) { uris -> addUris(uris) }
    val documentPicker = rememberLauncherForActivityResult(
        ActivityResultContracts.OpenMultipleDocuments(),
    ) { uris -> addUris(uris) }
    val cameraLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.TakePicture(),
    ) { success ->
        val file = cameraFile
        if (success && file != null && file.exists() && file.length() > 0) {
            addPending(PendingFiles.cameraAttachment(file))
        }
    }
    fun launchCameraNow() {
        val file = PendingFiles.newCameraFile(context)
        cameraFile = file
        runCatching {
            cameraLauncher.launch(AttachmentFiles.fileUri(context, file))
        }.onFailure {
            Toast.makeText(context, "无法打开相机", Toast.LENGTH_SHORT).show()
        }
    }
    val cameraPermission = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { granted ->
        if (granted) launchCameraNow()
        else Toast.makeText(context, "需要相机权限才能拍照", Toast.LENGTH_SHORT).show()
    }
    fun requestCamera() {
        if (ContextCompat.checkSelfPermission(
                context,
                Manifest.permission.CAMERA,
            ) == PackageManager.PERMISSION_GRANTED
        ) {
            launchCameraNow()
        } else {
            cameraPermission.launch(Manifest.permission.CAMERA)
        }
    }

    val speechLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult(),
    ) { result ->
        if (result.resultCode == Activity.RESULT_OK) {
            val spoken = result.data
                ?.getStringArrayListExtra(RecognizerIntent.EXTRA_RESULTS)
                ?.firstOrNull()
                ?.trim()
                .orEmpty()
            if (spoken.isNotBlank()) {
                composerText = if (composerText.isBlank()) spoken else "$composerText $spoken"
            }
        }
    }

    fun launchSpeech() {
        val intent = Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
            putExtra(
                RecognizerIntent.EXTRA_LANGUAGE_MODEL,
                RecognizerIntent.LANGUAGE_MODEL_FREE_FORM,
            )
            putExtra(RecognizerIntent.EXTRA_LANGUAGE, "zh-CN")
            putExtra(RecognizerIntent.EXTRA_PROMPT, "说出要回复的内容")
        }
        try {
            speechLauncher.launch(intent)
        } catch (_: ActivityNotFoundException) {
            Toast.makeText(context, "设备没有可用的语音识别服务", Toast.LENGTH_SHORT).show()
        }
    }

    val audioPermission = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { granted ->
        if (granted) launchSpeech()
        else Toast.makeText(context, "需要麦克风权限才能语音输入", Toast.LENGTH_SHORT).show()
    }

    fun requestSpeech() {
        if (ContextCompat.checkSelfPermission(
                context,
                Manifest.permission.RECORD_AUDIO,
            ) == PackageManager.PERMISSION_GRANTED
        ) {
            launchSpeech()
        } else {
            audioPermission.launch(Manifest.permission.RECORD_AUDIO)
        }
    }

    val timeline = remember(
        session?.transcript,
        session?.events,
        session?.replies,
        itemsState?.items,
        hasStructuredItems,
        transcriptOnlyAgent,
    ) {
        buildSessionTimeline(
            session = session,
            structuredItems = if (hasStructuredItems) itemsState?.items.orEmpty() else emptyList(),
            onlyAgent = transcriptOnlyAgent,
        )
    }

    val nearBottom by remember {
        derivedStateOf {
            val info = listState.layoutInfo
            val last = info.visibleItemsInfo.lastOrNull()?.index ?: -1
            info.totalItemsCount == 0 || last >= info.totalItemsCount - 2
        }
    }
    val firstUnseenIndex = remember(timeline, session?.seenAt) {
        val seenMillis = session?.seenAt
            ?.takeIf { it.isNotBlank() }
            ?.let { value -> runCatching { Instant.parse(value).toEpochMilli() }.getOrNull() }
        if (seenMillis == null) {
            null
        } else {
            timeline.indexOfFirst { item ->
                runCatching { Instant.parse(item.timestamp).toEpochMilli() }
                    .getOrNull()
                    ?.let { it > seenMillis } == true
            }.takeIf { it >= 0 }
        }
    }
    val timelineHeaderCount =
        (if (inboundDispatches.isNotEmpty()) 1 else 0) +
            (if (relatedDispatches.isNotEmpty()) 1 else 0) +
            (if (history.managed) 1 else 0)
    val lastTimelineListIndex = timelineHeaderCount + timeline.lastIndex
    var previousTimelineSize by rememberSaveable(sessionId, state.activeComputerKey) { mutableStateOf(0) }
    var previousLastKey by rememberSaveable(sessionId, state.activeComputerKey) { mutableStateOf<String?>(null) }
    var positionedTimeline by rememberSaveable(sessionId, state.activeComputerKey) { mutableStateOf(false) }
    var previousHistoryRevision by rememberSaveable(sessionId, state.activeComputerKey) {
        mutableStateOf(history.revision)
    }
    val lastTimelineItem = timeline.lastOrNull()
    LaunchedEffect(sessionId, timeline.size, lastTimelineItem, firstUnseenIndex, timelineHeaderCount, history.revision) {
        if (timeline.isEmpty()) {
            previousTimelineSize = 0
            previousLastKey = null
            return@LaunchedEffect
        }
        val loadedEarlier = positionedTimeline && previousHistoryRevision != history.revision
        previousHistoryRevision = history.revision
        if (loadedEarlier) {
            // LazyColumn retains the visible stable key while older rows are prepended.
            // Earlier context is not new activity and must not trigger a jump to the bottom.
            previousTimelineSize = timeline.size
            previousLastKey = lastTimelineItem?.key
            return@LaunchedEffect
        }
        if (!positionedTimeline) {
            delay(80)
            listState.scrollToItem(firstUnseenIndex?.let { timelineHeaderCount + it } ?: lastTimelineListIndex)
            positionedTimeline = true
            showNewContent = false
        } else if (timeline.size != previousTimelineSize || lastTimelineItem?.key != previousLastKey) {
            delay(80)
            if (nearBottom) {
                listState.animateScrollToItem(lastTimelineListIndex)
                showNewContent = false
            } else if (timeline.size > previousTimelineSize) {
                showNewContent = true
            }
        } else if (nearBottom && session?.running == true) {
            listState.scrollToItem(lastTimelineListIndex)
        }
        previousTimelineSize = timeline.size
        previousLastKey = lastTimelineItem?.key
    }

    val eventForReply = session?.replyEventId
    val canReply = session?.canReply == true
    val hasParentContent = dispatchPlan.message.isNotBlank() ||
        pendingAttachments.isNotEmpty()
    val canSend = canReply &&
        (hasParentContent || dispatchPlan.hasMentions) &&
        dispatchPlan.mentionsHaveTasks &&
        !hasUnsupportedDispatch &&
        pendingAttachments.none { it.uploading || it.error != null || it.uploadedPath == null }
    val queuedReplyCount = remember(session?.replies) {
        session?.replies.orEmpty().count {
            it.id > 0 && it.status == ReplyStatus.QUEUED
        }
    }
    val queuedReplies = remember(session?.replies) {
        session?.replies.orEmpty()
            .filter { it.id > 0 && it.status == ReplyStatus.QUEUED }
            .sortedBy { it.ts }
    }

    fun resetComposerAfterSend() {
        composerText = ""
        actions.clearDraft(sessionId)
        pendingAttachments = emptyList()
        selectedModel = ""
        selectedEffort = ""
        composePreview = null
        mentionOptions = emptyMap()
        scope.launch {
            delay(100)
            if (timeline.isNotEmpty()) {
                listState.animateScrollToItem(lastTimelineListIndex)
            }
        }
    }

    fun sendComposeNow() {
        val current = session ?: return
        composeSending = true
        val files = pendingAttachments.mapNotNull { it.uploadedPath }
        actions.compose(
            current,
            composerText.trim(),
            dispatchPlan,
            files,
            effectiveModel.takeIf { selectedModel.isNotBlank() },
            effectiveEffort.takeIf {
                selectedModel.isNotBlank() || selectedEffort.isNotBlank()
            },
            effectivePermissionMode.takeIf {
                selectedPermissionMode.isNotBlank()
            },
            mentionOverrides,
        ) { ok ->
            composeSending = false
            if (ok) resetComposerAfterSend()
        }
    }

    fun requestSend() {
        val current = session ?: return
        val files = pendingAttachments.mapNotNull { it.uploadedPath }
        if (dispatchPlan.hasMentions) {
            if (!settings.previewMentions) {
                sendComposeNow()
                return
            }
            if (composePreviewLoading || composeSending) return
            composePreviewLoading = true
            actions.previewCompose(
                current,
                composerText.trim(),
                files,
                effectiveModel.takeIf { selectedModel.isNotBlank() },
                effectiveEffort.takeIf {
                    selectedModel.isNotBlank() || selectedEffort.isNotBlank()
                },
                effectivePermissionMode.takeIf {
                    selectedPermissionMode.isNotBlank()
                },
                mentionOverrides,
            ) { result ->
                composePreviewLoading = false
                if (result != null) composePreview = result
            }
            return
        }
        val text = dispatchPlan.message
        if (text.isNotBlank() || files.isNotEmpty()) {
            actions.sendReply(
                current,
                text,
                eventForReply ?: 0L,
                files,
                effectiveModel.takeIf { selectedModel.isNotBlank() },
                effectiveEffort.takeIf {
                    selectedModel.isNotBlank() || selectedEffort.isNotBlank()
                },
                effectivePermissionMode.takeIf {
                    selectedPermissionMode.isNotBlank()
                },
            )
            resetComposerAfterSend()
        }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbarHostState) },
        containerColor = Color.Transparent,
        topBar = {
            SessionHeader(
                session = session,
                infoExpanded = infoExpanded,
                transcriptOnlyAgent = transcriptOnlyAgent,
                profile = agentProfile,
                itemsState = itemsState,
                onBack = onBack,
                onToggleInfo = { infoExpanded = !infoExpanded },
                onOpenSearch = { searchOpen = true },
                onRefreshTranscript = { actions.refreshTranscript(sessionId) },
                onToggleOnlyAgent = { transcriptOnlyAgent = !transcriptOnlyAgent },
                onToggleReasoning = {
                    actions.setReasoningVisible(
                        sessionId,
                        itemsState?.includeReasoning != true,
                    )
                },
                onCopySessionId = {
                    copyText("Session ID", session?.sessionId.orEmpty())
                },
                onCopy = ::copyText,
                onRename = { title ->
                    session?.let { actions.renameSession(it.sessionId, title) }
                },
                onExport = {
                    val current = session ?: return@SessionHeader
                    scope.launch {
                        runCatching {
                            val markdown = buildSessionMarkdown(
                                current,
                                itemsState?.items.orEmpty(),
                            )
                            val directory = File(context.cacheDir, "exports").apply { mkdirs() }
                            val file = File(directory, sessionMarkdownFileName(current))
                            file.writeText(markdown)
                            val uri = FileProvider.getUriForFile(
                                context,
                                "${context.packageName}.files",
                                file,
                            )
                            context.startActivity(
                                Intent.createChooser(
                                    Intent(Intent.ACTION_SEND).apply {
                                        type = "text/markdown"
                                        putExtra(Intent.EXTRA_STREAM, uri)
                                        putExtra(Intent.EXTRA_SUBJECT, current.title)
                                        addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                                    },
                                    "导出会话",
                                ),
                            )
                        }.onFailure {
                            snackbarHostState.showSnackbar("导出失败：${it.message}")
                        }
                    }
                },
                onStop = {
                    session?.let { actions.stopSession(it.sessionId) }
                },
                onTogglePinned = {
                    session?.let { actions.setPinned(it.sessionId, !it.pinned) }
                },
                onToggleMuted = {
                    session?.let { actions.setMuted(it.sessionId, !it.muted) }
                },
                onToggleArchived = {
                    session?.let {
                        actions.setArchived(it.sessionId, !it.archived)
                        if (!it.archived) onBack()
                    }
                },
                onToggleRemindOnDone = {
                    session?.let {
                        actions.setRemindOnDone(it.sessionId, !it.remindOnDone)
                    }
                },
                navigationAsDrawer = navigationAsDrawer,
                otherUrgentCount = otherUrgentCount,
                onJumpUrgent = onJumpUrgent,
                returnToSessionTitle = returnToSessionTitle,
                onReturnPreviousSession = onReturnPreviousSession,
                onQuickClear = onQuickClear,
                projectSiblings = projectSiblings,
                projectHistoryIds = projectHistoryIds,
                onOpenSibling = actions.openSessionById,
                awayEnabled = state.away,
                onToggleAway = { actions.toggleAway(!state.away) },
            )
        },
        bottomBar = {
            if (session != null) {
                Column {
                    session.pendingEvent?.let { pending ->
                        val pendingQuestions = remember(pending.questions, itemsState?.items) {
                            pending.questions.ifEmpty {
                                latestPendingQuestionItems(itemsState?.items.orEmpty())
                            }
                        }
                        SessionPendingDock(
                            session = session,
                            event = pending,
                            expanded = pendingDockExpanded,
                            onToggleExpanded = {
                                pendingDockExpanded = !pendingDockExpanded
                            },
                            onDecision = { eventId, decision, optionId ->
                                actions.decideApproval(
                                    eventId,
                                    decision,
                                    {
                                        actions.sendReply(
                                            session,
                                            if (decision == "allow") "同意" else "拒绝",
                                            eventId,
                                            emptyList(),
                                            null,
                                            null,
                                            null,
                                        )
                                    },
                                    optionId,
                                )
                            },
                            onReply = {
                                composerFocusRequester.requestFocus()
                                scope.launch {
                                    delay(80)
                                    composerFocusRequester.requestFocus()
                                    if (timeline.isNotEmpty()) {
                                        listState.animateScrollToItem(lastTimelineListIndex)
                                    }
                                }
                            },
                            pendingQuestions = pendingQuestions,
                            onSelectOption = { answer ->
                                actions.sendReply(
                                    session,
                                    answer,
                                    pending.id,
                                    emptyList(),
                                    null,
                                    null,
                                    null,
                                )
                            },
                        )
                    }
                    composePreview?.let { preview ->
                        ComposePreviewCard(
                            preview = preview,
                            sending = composeSending,
                            onConfirm = { sendComposeNow() },
                            onCancel = { composePreview = null },
                        )
                    }
                    SessionComposer(
                    text = composerText,
                    onTextChange = {
                        composerText = it
                        if (composePreview != null) composePreview = null
                        actions.saveDraft(sessionId, it)
                    },
                    pending = pendingAttachments,
                    modelLabel = effectiveModel,
                    effortLabel = effortDisplayLabel(effectiveEffort),
                    showEffortPill = agentProfile.capabilities.effort,
                    permissionLabel = permissionModeDisplayLabel(effectivePermissionMode),
                    permissionModes = permissionModes,
                    onPermissionChange = { selectedPermissionMode = it },
                    modelOverride = modelOverrideActive,
                    canSwitchModel = canSwitchModel,
                    modelDisabledReason = when {
                        !agentProfile.capabilities.switchModel &&
                            session.brand == dev.agentpush.poc.AgentBrand.WORKBUDDY ->
                            "WorkBuddy 不支持切换模型"
                        !agentProfile.capabilities.switchModel ->
                            "当前 Agent 不支持切换模型"
                        agentProfile.models.isEmpty() ->
                            "还没有拿到模型列表"
                        else -> ""
                    },
                    onModelClick = { modelPickerOpen = true },
                    dispatchTargets = dispatchTargets,
                    mentions = dispatchPlan.mentions,
                    mentionOptions = activeMentionOptions,
                    onMentionPresetChange = { mention, preset ->
                        val updated = changeAgentMentionPreset(
                            composerText,
                            mention,
                            preset,
                        )
                        if (updated != composerText) {
                            composerText = updated
                            composePreview = null
                            actions.saveDraft(sessionId, updated)
                        }
                    },
                    onMentionOptionsChange = { optionKey, options ->
                        mentionOptions = mentionOptions + (optionKey to options)
                        composePreview = null
                    },
                    commands = commands,
                    cwd = session.cwd,
                    completeFiles = actions.completeFiles,
                    enabled = canReply,
                    canSend = canSend,
                    queuedCount = queuedReplyCount,
                    onCancelQueued = {
                        queueSheetOpen = true
                    },
                    onRemove = { id ->
                        pendingAttachments = pendingAttachments.filterNot { it.id == id }
                    },
                    onRetryUpload = { upload(it) },
                    onPhotoPicker = {
                        runCatching {
                            photoPicker.launch(
                                PickVisualMediaRequest(
                                    ActivityResultContracts.PickVisualMedia.ImageOnly,
                                ),
                            )
                        }.onFailure {
                            Toast.makeText(context, "无法打开相册", Toast.LENGTH_SHORT).show()
                        }
                    },
                    onCamera = { requestCamera() },
                    onFilePicker = { documentPicker.launch(arrayOf("*/*")) },
                    onSpeech = { requestSpeech() },
                    running = session.running,
                    runningSince = session.runningSince,
                    canStop = session.capabilities.stop && session.running,
                    onStop = { actions.stopSession(session.sessionId) },
                    onSend = { requestSend() },
                    inputFocusRequester = composerFocusRequester,
                    previewLoading = composePreviewLoading,
                    )
                }
            }
        },
    ) { padding ->
        Box(
            modifier = Modifier
                .fillMaxSize()
                .background(MaterialTheme.colorScheme.background),
        ) {
            if (session == null) {
                SessionMissing(
                    missing = sessionMissing,
                    onBack = onBack,
                    modifier = Modifier.padding(padding),
                )
            } else {
                LazyColumn(
                    state = listState,
                    modifier = Modifier
                        .fillMaxSize()
                        .padding(padding)
                        .testTag("session-timeline"),
                    contentPadding = PaddingValues(
                        start = 14.dp,
                        end = 14.dp,
                        top = 12.dp,
                        bottom = 16.dp,
                    ),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    if (history.managed) {
                        item(key = "phone-context-boundary") {
                            SessionHistoryBoundary(
                                history = history,
                                onLoadEarlier = { actions.loadEarlierContext(sessionId) },
                            )
                        }
                    }
                    if (inboundDispatches.isNotEmpty()) {
                        item(key = "inbound-dispatches") {
                            InboundDispatchPanel(
                                dispatches = inboundDispatches,
                                onOpenSession = actions.openSessionById,
                            )
                        }
                    }
                    if (relatedDispatches.isNotEmpty()) {
                        item(key = "cross-agent-dispatches") {
                            CrossAgentDispatchPanel(
                                dispatches = relatedDispatches,
                                targetStatuses = dispatchTargetStatuses,
                                onOpenSession = actions.openSessionById,
                                onForward = actions.forwardDispatch,
                                onCancel = actions.cancelDispatch,
                                onDiscard = actions.discardDispatchWorktree,
                                onReply = { dispatch ->
                                    composerText = "@${dispatch.targetAgent} "
                                    scope.launch {
                                        if (timeline.isNotEmpty()) {
                                            listState.animateScrollToItem(lastTimelineListIndex)
                                        }
                                    }
                                },
                            )
                        }
                    }
                    if (timeline.isEmpty()) {
                        item(key = "empty-timeline") {
                            Text(
                                text = session.transcript?.reason
                                    ?.takeIf { it.isNotBlank() }
                                    ?: "这个会话还没有可显示的内容。",
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .padding(vertical = 60.dp),
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                            )
                        }
                    }
                    items(timeline.size, key = { "${session.sessionId}:${timeline[it].key}" }) { index ->
                        Column {
                            if (index == firstUnseenIndex) {
                                NewContentDivider()
                            }
                            Box {
                            when (val item = timeline[index]) {
                            is TimelineItem.Item -> {
                                SessionTimelineCard(
                                    item = item.value,
                                    session = session,
                                    settings = settings,
                                    approvalOptions = session.latestEvent
                                        ?.takeIf { it.isPermission }
                                        ?.approvalOptions
                                        .orEmpty(),
                                    reasoningDurationSeconds = if (
                                        item.value.type == SessionItemType.REASONING
                                    ) {
                                        reasoningDuration(timeline, index)
                                    } else {
                                        null
                                    },
                                    onOpenUrl = ::openUrl,
                                    onOpenBrowser = onOpenBrowser,
                                    onOpenImage = { attachments, imageIndex ->
                                        imageViewer = ImageViewerState(attachments, imageIndex)
                                    },
                                    onCopyCode = {
                                        clipboard.setText(AnnotatedString(it))
                                        scope.launch { snackbarHostState.showSnackbar("代码已复制") }
                                    },
                                    onCopyText = {
                                        clipboard.setText(AnnotatedString(it))
                                        scope.launch { snackbarHostState.showSnackbar("已复制") }
                                    },
                                    onDecision = { eventId, decision, optionId ->
                                        if (eventId != null) {
                                            actions.decideApproval(
                                                eventId,
                                                decision,
                                                {
                                                    actions.sendReply(
                                                        session,
                                                        if (decision == "allow") "同意" else "拒绝",
                                                        eventId,
                                                        emptyList(),
                                                        null,
                                                        null,
                                                        null,
                                                    )
                                                },
                                                optionId,
                                            )
                                        } else {
                                            actions.sendReply(
                                                session,
                                                if (decision == "allow") "同意" else "拒绝",
                                                session.replyEventId ?: 0L,
                                                emptyList(),
                                                null,
                                                null,
                                                null,
                                            )
                                        }
                                    },
                                    onSelectQuestionAnswer = { answer ->
                                        actions.sendReply(
                                            session,
                                            answer,
                                            session.pendingEvent?.id ?: session.replyEventId ?: 0L,
                                            emptyList(),
                                            null,
                                            null,
                                            null,
                                        )
                                    },
                                )
                            }
                            is TimelineItem.ProcessGroup -> {
                                var expanded by rememberSaveable(item.key) {
                                    mutableStateOf(false)
                                }
                                ToolProcessGroupCard(
                                    steps = item.values,
                                    expanded = expanded,
                                    onToggle = { expanded = !expanded },
                                ) {
                                    item.values.forEach { step ->
                                        SessionTimelineCard(
                                            item = step,
                                            session = session,
                                            settings = settings,
                                            approvalOptions = emptyList(),
                                            reasoningDurationSeconds = null,
                                            onOpenUrl = ::openUrl,
                                            onOpenBrowser = onOpenBrowser,
                                            onOpenImage = { attachments, imageIndex ->
                                                imageViewer = ImageViewerState(attachments, imageIndex)
                                            },
                                            onCopyCode = {
                                                clipboard.setText(AnnotatedString(it))
                                                scope.launch {
                                                    snackbarHostState.showSnackbar("代码已复制")
                                                }
                                            },
                                            onCopyText = {
                                                clipboard.setText(AnnotatedString(it))
                                                scope.launch {
                                                    snackbarHostState.showSnackbar("已复制")
                                                }
                                            },
                                            onDecision = { eventId, decision, optionId ->
                                                if (eventId != null) {
                                                    actions.decideApproval(
                                                        eventId,
                                                        decision,
                                                        {
                                                            actions.sendReply(
                                                                session,
                                                                if (decision == "allow") "同意" else "拒绝",
                                                                eventId,
                                                                emptyList(),
                                                                null,
                                                                null,
                                                                null,
                                                            )
                                                        },
                                                        optionId,
                                                    )
                                                }
                                            },
                                        )
                                    }
                                }
                            }
                            is TimelineItem.Message -> TranscriptMessageRow(
                                message = item.value,
                                settings = settings,
                                brand = session.brand,
                                displayName = session.brand.displayName,
                                onOpenUrl = ::openUrl,
                                onOpenBrowser = onOpenBrowser,
                                onOpenImage = { attachments, imageIndex ->
                                    imageViewer = ImageViewerState(attachments, imageIndex)
                                },
                                onCopyCode = {
                                    clipboard.setText(AnnotatedString(it))
                                    scope.launch { snackbarHostState.showSnackbar("代码已复制") }
                                },
                            )

                            is TimelineItem.Event -> AgentEventCard(
                                event = item.value,
                                session = session,
                                settings = settings,
                                onReply = { text ->
                                    actions.sendReply(
                                        session,
                                        text,
                                        item.value.id,
                                        emptyList(),
                                        null,
                                        null,
                                        null,
                                    )
                                },
                                onDecision = { eventId, decision, optionId ->
                                    actions.decideApproval(
                                        eventId,
                                        decision,
                                        {
                                            actions.sendReply(
                                                session,
                                                if (decision == "allow") "同意" else "拒绝",
                                                eventId,
                                                emptyList(),
                                                null,
                                                null,
                                                null,
                                            )
                                        },
                                        optionId,
                                    )
                                },
                                onOpenUrl = ::openUrl,
                                onOpenBrowser = onOpenBrowser,
                                onOpenImage = { attachments, imageIndex ->
                                    imageViewer = ImageViewerState(attachments, imageIndex)
                                },
                                onCopyCode = {
                                    clipboard.setText(AnnotatedString(it))
                                    scope.launch { snackbarHostState.showSnackbar("代码已复制") }
                                },
                                onCopyText = {
                                    clipboard.setText(AnnotatedString(item.value.text))
                                    scope.launch { snackbarHostState.showSnackbar("已复制全文") }
                                },
                                onShareText = { text ->
                                    context.startActivity(
                                        Intent.createChooser(
                                            Intent(Intent.ACTION_SEND).apply {
                                                type = "text/plain"
                                                putExtra(Intent.EXTRA_TEXT, text)
                                            },
                                            "分享回复",
                                        ),
                                    )
                                },
                                proxyCapability = proxyCapability,
                            )

                            is TimelineItem.Reply -> UserReplyBubble(
                                reply = item.value,
                                onRetry = { actions.retryReply(item.value) },
                                onLongClick = {
                                    clipboard.setText(AnnotatedString(item.value.text))
                                    scope.launch { snackbarHostState.showSnackbar("已复制回复") }
                                },
                            )
                            }
                            }
                        }
                    }
                }
                if (!nearBottom || showNewContent) {
                    Surface(
                        onClick = {
                            scope.launch {
                                if (timeline.isNotEmpty()) {
                                    listState.animateScrollToItem(lastTimelineListIndex)
                                }
                                showNewContent = false
                            }
                        },
                        color = MaterialTheme.colorScheme.primary,
                        shape = RoundedCornerShape(50),
                        shadowElevation = 6.dp,
                        modifier = Modifier
                            .align(Alignment.BottomCenter)
                            .padding(bottom = 12.dp)
                            .testTag("jump-to-latest"),
                    ) {
                        Text(
                            text = if (showNewContent) "有新内容 ↓" else "回到最新 ↓",
                            modifier = Modifier.padding(horizontal = 14.dp, vertical = 8.dp),
                            style = MaterialTheme.typography.labelLarge,
                            color = MaterialTheme.colorScheme.onPrimary,
                        )
                    }
                }
            }
        }
    }

    SessionSheets(
        session = session,
        modelPickerOpen = modelPickerOpen,
        agentProfile = agentProfile,
        effectiveModel = effectiveModel,
        effectiveEffort = effectiveEffort,
        onDismissModelPicker = { modelPickerOpen = false },
        onApplyModel = { model, effort ->
            selectedModel = model
            selectedEffort = effort
        },
        imageViewer = imageViewer,
        settings = settings,
        onDismissImageViewer = { imageViewer = null },
        localhostPrompt = localhostPrompt,
        proxyCapability = proxyCapability,
        onDismissLocalhostPrompt = { localhostPrompt = null },
        onOpenBrowser = onOpenBrowser,
    )
    SessionQueuedRepliesSheet(
        open = queueSheetOpen,
        replies = queuedReplies,
        onDismiss = { queueSheetOpen = false },
        onEdit = { reply ->
            actions.cancelQueuedReply(reply) { cancelled ->
                if (cancelled) {
                    composerText = if (composerText.isBlank()) {
                        reply.text
                    } else {
                        "$composerText\n\n${reply.text}"
                    }
                    actions.saveDraft(sessionId, composerText)
                    queueSheetOpen = false
                    composerFocusRequester.requestFocus()
                }
            }
        },
        onCancel = { reply ->
            actions.cancelQueuedReply(reply) {}
        },
        onCancelAll = {
            session?.let { actions.cancelQueuedReplies(it.sessionId) }
            queueSheetOpen = false
        },
    )
    SessionSearchSheet(
        open = searchOpen,
        timeline = timeline,
        onDismiss = { searchOpen = false },
        onJump = { index ->
            scope.launch {
                delay(120)
                listState.animateScrollToItem(timelineHeaderCount + index)
            }
        },
    )
}

@Composable
private fun SessionTimelineCard(
    item: SessionItem,
    session: SessionSummary,
    settings: AppSettings,
    approvalOptions: List<ApprovalOption>,
    reasoningDurationSeconds: Long?,
    onOpenUrl: (String) -> Unit,
    onOpenBrowser: (String, String) -> Unit,
    onOpenImage: (List<Attachment>, Int) -> Unit,
    onCopyCode: (String) -> Unit,
    onCopyText: (String) -> Unit,
    onDecision: (Long?, String, String?) -> Unit,
    onSelectQuestionAnswer: ((String) -> Unit)? = null,
) {
    SessionItemRow(
        item = item,
        session = session,
        settings = settings,
        approvalOptions = approvalOptions,
        reasoningDurationSeconds = reasoningDurationSeconds,
        onOpenUrl = onOpenUrl,
        onOpenBrowser = onOpenBrowser,
        onOpenImage = onOpenImage,
        onCopyCode = onCopyCode,
        onCopyText = onCopyText,
        onDecision = onDecision,
        onSelectQuestionAnswer = onSelectQuestionAnswer,
    )
}

@Composable
private fun SessionMissing(
    missing: Boolean,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(modifier = modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        if (missing) {
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(10.dp),
                modifier = Modifier.padding(horizontal = 32.dp),
            ) {
                Icon(
                    Icons.Outlined.ErrorOutline,
                    contentDescription = null,
                    modifier = Modifier.size(36.dp),
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text("找不到这个会话", style = MaterialTheme.typography.titleMedium)
                Text(
                    "它可能还没有同步，或已经被中继清理。返回收件箱可重新加载。",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                )
                OutlinedButton(onClick = onBack) { Text("返回收件箱") }
            }
        } else {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                CircularProgressIndicator(modifier = Modifier.size(28.dp))
                Spacer(Modifier.height(10.dp))
                Text(
                    "正在加载会话…",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}
