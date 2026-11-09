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
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.outlined.AttachFile
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.Language
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
import androidx.compose.material3.Switch
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
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.agentpush.poc.AgentApi
import dev.agentpush.poc.AgentEvent
import dev.agentpush.poc.AgentReply
import dev.agentpush.poc.AgentUiState
import dev.agentpush.poc.AgentViewModel
import dev.agentpush.poc.Attachment
import dev.agentpush.poc.ContextUsage
import dev.agentpush.poc.ReplyStatus
import dev.agentpush.poc.SessionItem
import dev.agentpush.poc.SessionItemType
import dev.agentpush.poc.SessionStatus
import dev.agentpush.poc.SessionSummary
import dev.agentpush.poc.TranscriptMessage
import dev.agentpush.poc.UploadResult
import dev.agentpush.poc.formatClock
import dev.agentpush.poc.capabilityTokenIn
import dev.agentpush.poc.relativeTime
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
import dev.agentpush.poc.ui.accentColor
import dev.agentpush.poc.ui.rewriteMarkdownImageUrls
import dev.agentpush.poc.ui.theme.HomingMono
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.net.URI

private sealed interface TimelineItem {
    val key: String
    val timestamp: String
    val order: Int

    data class Message(
        val value: TranscriptMessage,
        override val order: Int,
    ) : TimelineItem {
        override val key: String = "transcript-$order-${value.role}"
        override val timestamp: String = value.ts
    }

    data class Event(
        val value: AgentEvent,
        override val order: Int,
    ) : TimelineItem {
        override val key: String = "event-$order-${value.id}"
        override val timestamp: String = value.ts
    }

    data class Reply(
        val value: AgentReply,
        override val order: Int,
    ) : TimelineItem {
        override val key: String = "reply-$order-${value.id}"
        override val timestamp: String = value.ts
    }

    data class Item(
        val value: SessionItem,
        override val order: Int,
    ) : TimelineItem {
        override val key: String = "item-$order-${value.id}"
        override val timestamp: String = value.ts
    }
}

internal data class SessionDetailActions(
    val openSession: () -> Unit = {},
    val closeSession: () -> Unit = {},
    val sendReply: (
        session: SessionSummary,
        text: String,
        eventId: Long,
        files: List<String>,
        model: String?,
        effort: String?,
    ) -> Unit = { _, _, _, _, _, _ -> },
    val retryReply: (AgentReply) -> Unit = {},
    val stopSession: (String) -> Unit = {},
    val decideApproval: (
        eventId: Long,
        decision: String,
        onFallbackText: (() -> Unit)?,
    ) -> Unit = { _, _, _ -> },
    val refreshTranscript: (String) -> Unit = {},
    val setPinned: (String, Boolean) -> Unit = { _, _ -> },
    val setMuted: (String, Boolean) -> Unit = { _, _ -> },
    val setArchived: (String, Boolean) -> Unit = { _, _ -> },
    val uploadAttachment: (
        eventId: Long?,
        cwd: String?,
        file: java.io.File,
        name: String,
        mime: String,
        onProgress: (Float) -> Unit,
        onResult: (Result<UploadResult>) -> Unit,
    ) -> Unit = { _, _, _, _, _, _, _ -> },
)

private data class ImageViewerState(
    val attachments: List<Attachment>,
    val index: Int,
)

@Composable
fun SessionScreen(
    sessionId: String,
    viewModel: AgentViewModel,
    snackbarHostState: SnackbarHostState,
    onBack: () -> Unit,
    onOpenBrowser: (url: String, title: String) -> Unit,
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    SessionDetailContent(
        state = state,
        sessionId = sessionId,
        snackbarHostState = snackbarHostState,
        onBack = onBack,
        onOpenBrowser = onOpenBrowser,
        actions = SessionDetailActions(
            openSession = { viewModel.openSession(sessionId) },
            closeSession = { viewModel.closeSessionStream() },
            sendReply = { session, text, eventId, files, model, effort ->
                viewModel.sendReply(
                    session = session,
                    text = text,
                    eventId = eventId,
                    files = files,
                    model = model,
                    effort = effort,
                )
            },
            retryReply = viewModel::retryReply,
            stopSession = viewModel::stopSession,
            decideApproval = { eventId, decision, fallback ->
                viewModel.decideApproval(eventId, decision, fallback)
            },
            refreshTranscript = { viewModel.refreshTranscript(sessionId) },
            setPinned = viewModel::setPinned,
            setMuted = viewModel::setMuted,
            setArchived = viewModel::setArchived,
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
        ),
    )
}

@Composable
internal fun SessionDetailContent(
    state: AgentUiState,
    sessionId: String,
    snackbarHostState: SnackbarHostState,
    onBack: () -> Unit,
    onOpenBrowser: (url: String, title: String) -> Unit,
    actions: SessionDetailActions = SessionDetailActions(),
) {
    val session = remember(
        state.events,
        state.replies,
        state.sessionMeta,
        state.transcriptCache,
        state.sessionItems,
        state.agentDirectory,
        sessionId,
    ) {
        state.session(sessionId)
    }
    val itemsState = state.sessionItems[sessionId]
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
    var modelPickerOpen by remember { mutableStateOf(false) }
    var selectedModel by rememberSaveable(sessionId) { mutableStateOf("") }
    var selectedEffort by rememberSaveable(sessionId) { mutableStateOf("") }
    var showNewContent by remember { mutableStateOf(false) }
    var imageViewer by remember { mutableStateOf<ImageViewerState?>(null) }
    var localhostPrompt by remember { mutableStateOf<String?>(null) }
    var composerText by rememberSaveable { mutableStateOf("") }
    var pendingAttachments by remember { mutableStateOf<List<PendingAttachment>>(emptyList()) }
    var cameraFile by remember { mutableStateOf<java.io.File?>(null) }

    val effectiveModel = selectedModel.ifBlank {
        session?.model?.takeIf { it.isNotBlank() }
            ?: agentProfile.defaultModel
            ?: agentProfile.defaultModelOption?.id.orEmpty()
    }
    val effectiveEffort = selectedEffort.ifBlank { session?.effort.orEmpty() }
    val modelOverrideActive = selectedModel.isNotBlank()
    val canSwitchModel = agentProfile.capabilities.switchModel &&
        agentProfile.models.isNotEmpty()

    LaunchedEffect(sessionId) {
        actions.openSession()
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
        val uri = runCatching { URI(url) }.getOrNull()
        val host = uri?.host?.lowercase().orEmpty()
        if (host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]") {
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
        buildList {
            if (hasStructuredItems) {
                val replies = session?.replies.orEmpty()
                val matchedReplies = mutableSetOf<Long>()
                var order = 0
                itemsState?.items.orEmpty().forEach { item ->
                    when (item.type) {
                        SessionItemType.USER -> {
                            if (!transcriptOnlyAgent) {
                                val duplicate = replies.lastOrNull { reply ->
                                    reply.id !in matchedReplies &&
                                        normalizeComparableText(reply.text) ==
                                        normalizeComparableText(item.text)
                                }
                                if (duplicate != null) {
                                    matchedReplies += duplicate.id
                                    add(TimelineItem.Reply(duplicate, order++))
                                } else {
                                    add(TimelineItem.Item(item, order++))
                                }
                            }
                        }
                        else -> add(TimelineItem.Item(item, order++))
                    }
                }
                if (!transcriptOnlyAgent) {
                    replies.filter { it.id !in matchedReplies }.forEach { reply ->
                        add(TimelineItem.Reply(reply, order++))
                    }
                }
            } else {
                val transcript = session?.transcript?.messages.orEmpty()
                if (transcript.isNotEmpty()) {
                    var order = 0
                    transcript.forEach { message ->
                        if (!transcriptOnlyAgent || message.isAssistant) {
                            add(TimelineItem.Message(message, order++))
                        }
                    }
                    session?.pendingEvent?.let { pending ->
                        add(TimelineItem.Event(pending, order++))
                    }
                    if (!transcriptOnlyAgent) {
                        session?.replies?.forEach { reply ->
                            add(TimelineItem.Reply(reply, order++))
                        }
                    }
                } else {
                    var order = 0
                    session?.events?.forEach { event ->
                        if (!transcriptOnlyAgent || event.kind != "needs_input") {
                            add(TimelineItem.Event(event, order++))
                        }
                    }
                    if (!transcriptOnlyAgent) {
                        session?.replies?.forEach { reply ->
                            add(TimelineItem.Reply(reply, order++))
                        }
                    }
                }
            }
        }
    }

    val nearBottom by remember {
        derivedStateOf {
            val info = listState.layoutInfo
            val last = info.visibleItemsInfo.lastOrNull()?.index ?: -1
            info.totalItemsCount == 0 || last >= info.totalItemsCount - 2
        }
    }
    var previousTimelineSize by remember(sessionId) { mutableStateOf(0) }
    LaunchedEffect(timeline.size, timeline.lastOrNull()?.key) {
        if (timeline.isEmpty()) {
            previousTimelineSize = 0
            return@LaunchedEffect
        }
        delay(80)
        if (previousTimelineSize == 0 || nearBottom) {
            listState.animateScrollToItem(timeline.lastIndex)
            showNewContent = false
        } else if (timeline.size > previousTimelineSize) {
            showNewContent = true
        }
        previousTimelineSize = timeline.size
    }

    val eventForReply = session?.replyEventId
    val canReply = session?.canReply == true
    val canSend = canReply &&
        (composerText.isNotBlank() || pendingAttachments.isNotEmpty()) &&
        pendingAttachments.none { it.uploading || it.error != null || it.uploadedPath == null }

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
                onToggleOnlyAgent = { transcriptOnlyAgent = !transcriptOnlyAgent },
                onCopySessionId = {
                    copyText("Session ID", session?.sessionId.orEmpty())
                },
                onCopy = ::copyText,
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
            )
        },
        bottomBar = {
            if (session != null) {
                SessionComposer(
                    text = composerText,
                    onTextChange = { composerText = it },
                    pending = pendingAttachments,
                    modelLabel = effectiveModel,
                    effortLabel = effortDisplayLabel(effectiveEffort),
                    showEffortPill = agentProfile.capabilities.effort,
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
                    enabled = canReply,
                    canSend = canSend,
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
                    onCamera = {
                        val file = PendingFiles.newCameraFile(context)
                        cameraFile = file
                        runCatching {
                            cameraLauncher.launch(AttachmentFiles.fileUri(context, file))
                        }.onFailure {
                            Toast.makeText(context, "无法打开相机", Toast.LENGTH_SHORT).show()
                        }
                    },
                    onFilePicker = { documentPicker.launch(arrayOf("*/*")) },
                    onSpeech = { requestSpeech() },
                    running = session.running,
                    runningSince = session.runningSince,
                    canStop = session.capabilities.stop && session.running,
                    onStop = { actions.stopSession(session.sessionId) },
                    onSend = {
                        val files = pendingAttachments.mapNotNull { it.uploadedPath }
                        val text = composerText.trim()
                        if (text.isNotBlank() || files.isNotEmpty()) {
                            actions.sendReply(
                                session,
                                text,
                                eventForReply ?: 0L,
                                files,
                                effectiveModel.takeIf { selectedModel.isNotBlank() },
                                effectiveEffort.takeIf {
                                    selectedModel.isNotBlank() || selectedEffort.isNotBlank()
                                },
                            )
                            composerText = ""
                            pendingAttachments = emptyList()
                            selectedModel = ""
                            selectedEffort = ""
                            scope.launch {
                                delay(100)
                                if (timeline.isNotEmpty()) {
                                    listState.animateScrollToItem(timeline.lastIndex)
                                }
                            }
                        }
                    },
                )
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
                    item(key = "transcript-tools") {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(
                                text = when {
                                    hasStructuredItems -> "实时条目 · ${timeline.size} 条"
                                    session.transcript == null -> "完整对话加载中…"
                                    session.transcript.messages.isEmpty() ->
                                        session.transcript.reason ?: "仅有事件记录，已回退显示"
                                    else -> "完整对话 · ${timeline.size} 条"
                                },
                                modifier = Modifier.weight(1f),
                                style = MaterialTheme.typography.labelMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                maxLines = 2,
                                overflow = TextOverflow.Ellipsis,
                            )
                            Text(
                                text = "只看 Agent",
                                style = MaterialTheme.typography.labelMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                            Switch(
                                checked = transcriptOnlyAgent,
                                onCheckedChange = { transcriptOnlyAgent = it },
                                modifier = Modifier.size(width = 40.dp, height = 27.dp),
                            )
                            IconButton(
                                onClick = { actions.refreshTranscript(sessionId) },
                                modifier = Modifier.size(34.dp),
                            ) {
                                Icon(
                                    Icons.Outlined.Language,
                                    contentDescription = "重新加载完整对话",
                                    modifier = Modifier.size(17.dp),
                                )
                            }
                        }
                    }
                    if (timeline.isEmpty()) {
                        item(key = "empty-timeline") {
                            Text(
                                text = "这个会话还没有可显示的内容。",
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .padding(vertical = 60.dp),
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                            )
                        }
                    }
                    items(timeline.size, key = { timeline[it].key }) { index ->
                        Box(modifier = Modifier.animateItem()) {
                            when (val item = timeline[index]) {
                            is TimelineItem.Item -> {
                                SessionItemRow(
                                    item = item.value,
                                    session = session,
                                    settings = settings,
                                    reasoningDurationSeconds = if (
                                        item.value.type == SessionItemType.REASONING
                                    ) {
                                        reasoningDuration(timeline, index)
                                    } else {
                                        null
                                    },
                                    onOpenUrl = ::openUrl,
                                    onCopyCode = {
                                        clipboard.setText(AnnotatedString(it))
                                        scope.launch { snackbarHostState.showSnackbar("代码已复制") }
                                    },
                                    onCopyText = {
                                        clipboard.setText(AnnotatedString(it))
                                        scope.launch { snackbarHostState.showSnackbar("已复制") }
                                    },
                                    onDecision = { eventId, allow ->
                                        val decision = if (allow) "allow" else "deny"
                                        if (eventId != null) {
                                            actions.decideApproval(
                                                eventId,
                                                decision,
                                                {
                                                    actions.sendReply(
                                                        session,
                                                        if (allow) "同意" else "拒绝",
                                                        eventId,
                                                        emptyList(),
                                                        null,
                                                        null,
                                                    )
                                                },
                                            )
                                        } else {
                                            actions.sendReply(
                                                session,
                                                if (allow) "同意" else "拒绝",
                                                session.replyEventId ?: 0L,
                                                emptyList(),
                                                null,
                                                null,
                                            )
                                        }
                                    },
                                )
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
                if (showNewContent) {
                    Surface(
                        onClick = {
                            scope.launch {
                                if (timeline.isNotEmpty()) {
                                    listState.animateScrollToItem(timeline.lastIndex)
                                }
                                showNewContent = false
                            }
                        },
                        modifier = Modifier
                            .align(Alignment.BottomCenter)
                            .padding(bottom = 12.dp),
                        color = MaterialTheme.colorScheme.primary,
                        shape = RoundedCornerShape(50),
                        shadowElevation = 6.dp,
                    ) {
                        Text(
                            text = "有新内容 ↓",
                            modifier = Modifier.padding(horizontal = 14.dp, vertical = 8.dp),
                            style = MaterialTheme.typography.labelLarge,
                            color = MaterialTheme.colorScheme.onPrimary,
                        )
                    }
                }
            }
        }
    }

    if (modelPickerOpen && session != null) {
        ModelPickerSheet(
            profile = agentProfile.copy(
                models = agentProfile.models,
                defaultModel = agentProfile.defaultModel
                    .ifBlank { session.model },
            ),
            currentModel = effectiveModel,
            currentEffort = effectiveEffort,
            onDismiss = { modelPickerOpen = false },
            onApply = { model, effort ->
                selectedModel = model
                selectedEffort = effort
            },
        )
    }

    imageViewer?.let { viewer ->
        FullScreenImageViewer(
            attachments = viewer.attachments,
            initialIndex = viewer.index,
            settings = settings,
            onDismiss = { imageViewer = null },
        )
    }

    localhostPrompt?.let { url ->
        val port = runCatching { URI(url).port }.getOrDefault(-1)
        AlertDialog(
            onDismissRequest = { localhostPrompt = null },
            title = { Text("这是 Mac 本机地址") },
            text = {
                Text(
                    "localhost / 127.0.0.1 在手机上指向手机自己，无法直接访问。" +
                        if (port > 0) {
                            "\n\n若中继已开启端口代理，可通过中继访问 $port 端口；未开启时会显示错误。"
                        } else {
                            ""
                        },
                )
            },
            confirmButton = {
                if (port > 0) {
                    TextButton(
                        onClick = {
                            localhostPrompt = null
                            val uri = runCatching { URI(url) }.getOrNull()
                            val path = uri?.rawPath?.takeIf { it.isNotBlank() } ?: "/"
                            val query = uri?.rawQuery?.let { "?$it" }.orEmpty()
                            onOpenBrowser(
                                AgentApi.proxyUrl(
                                    settings,
                                    port,
                                    path.trimStart('/') + query,
                                    proxyCapability,
                                ),
                                "通过中继 · :$port",
                            )
                        },
                    ) {
                        Text("通过中继打开")
                    }
                }
            },
            dismissButton = {
                TextButton(onClick = { localhostPrompt = null }) {
                    Text("取消")
                }
            },
        )
    }
}

@Composable
private fun SessionHeader(
    session: SessionSummary?,
    infoExpanded: Boolean,
    transcriptOnlyAgent: Boolean,
    profile: dev.agentpush.poc.AgentProfile,
    itemsState: dev.agentpush.poc.SessionItemsState?,
    onBack: () -> Unit,
    onToggleInfo: () -> Unit,
    onToggleOnlyAgent: () -> Unit,
    onCopySessionId: () -> Unit,
    onCopy: (String, String) -> Unit,
    onStop: () -> Unit,
    onTogglePinned: () -> Unit,
    onToggleMuted: () -> Unit,
    onToggleArchived: () -> Unit,
) {
    var menuExpanded by remember { mutableStateOf(false) }
    Surface(
        color = MaterialTheme.colorScheme.background.copy(alpha = 0.98f),
        tonalElevation = 0.dp,
        border = BorderStroke(0.dp, Color.Transparent),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .statusBarsPadding(),
        ) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 4.dp, end = 8.dp, top = 4.dp, bottom = 6.dp),
                verticalAlignment = Alignment.Top,
            ) {
                IconButton(onClick = onBack) {
                    Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = "返回")
                }
                if (session != null) {
                    AgentGlyph(
                        brand = session.brand,
                        size = 22.dp,
                        modifier = Modifier.padding(top = 10.dp),
                    )
                    Spacer(Modifier.width(8.dp))
                    Column(
                        modifier = Modifier.weight(1f),
                    ) {
                        Text(
                            text = session.title,
                            style = TextStyle(
                                fontSize = 17.sp,
                                lineHeight = 22.sp,
                                fontWeight = FontWeight.Bold,
                                color = MaterialTheme.colorScheme.onSurface,
                            ),
                            maxLines = 2,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.combinedClickable(
                                onClick = onToggleInfo,
                                onLongClick = onToggleInfo,
                            ).testTag("session-title"),
                        )
                        Spacer(Modifier.height(4.dp))
                        Text(
                            text = buildString {
                                append(session.brand.displayName)
                                append(" · ")
                                append(session.project.ifBlank { "未命名项目" })
                                session.branch.takeIf {
                                    it.isNotBlank() && !it.equals("HEAD", true)
                                }?.let {
                                    append(" · ")
                                    append(it)
                                }
                            },
                            style = TextStyle(
                                fontFamily = dev.agentpush.poc.ui.theme.HomingMono,
                                fontSize = 11.5.sp,
                                lineHeight = 16.sp,
                                color = dev.agentpush.poc.ui.theme.HomingPalette.Muted,
                            ),
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                    Column(
                        horizontalAlignment = Alignment.End,
                        verticalArrangement = Arrangement.spacedBy(2.dp),
                    ) {
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(3.dp),
                        ) {
                            HeaderRunState(session = session, itemsState = itemsState)
                            IconButton(
                                onClick = { menuExpanded = true },
                                modifier = Modifier.size(36.dp),
                            ) {
                                Icon(
                                    Icons.Outlined.MoreVert,
                                    contentDescription = "更多",
                                    modifier = Modifier.size(21.dp),
                                )
                            }
                            DropdownMenu(
                                expanded = menuExpanded,
                                onDismissRequest = { menuExpanded = false },
                            ) {
                                DropdownMenuItem(
                                    text = { Text(if (infoExpanded) "收起元数据" else "查看元数据") },
                                    onClick = {
                                        menuExpanded = false
                                        onToggleInfo()
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text("复制 session_id") },
                                    leadingIcon = {
                                        Icon(Icons.Outlined.ContentCopy, contentDescription = null)
                                    },
                                    onClick = {
                                        menuExpanded = false
                                        onCopySessionId()
                                    },
                                )
                                DropdownMenuItem(
                                    text = {
                                        Text(
                                            if (transcriptOnlyAgent) {
                                                "显示全部消息"
                                            } else {
                                                "只看 Agent 回复"
                                            },
                                        )
                                    },
                                    leadingIcon = {
                                        Icon(
                                            if (transcriptOnlyAgent) {
                                                Icons.Outlined.Schedule
                                            } else {
                                                Icons.Outlined.CheckCircle
                                            },
                                            contentDescription = null,
                                        )
                                    },
                                    onClick = {
                                        menuExpanded = false
                                        onToggleOnlyAgent()
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text(if (session.pinned) "取消置顶" else "置顶会话") },
                                    onClick = {
                                        menuExpanded = false
                                        onTogglePinned()
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text(if (session.muted) "恢复提醒" else "静音会话") },
                                    onClick = {
                                        menuExpanded = false
                                        onToggleMuted()
                                    },
                                )
                                DropdownMenuItem(
                                    text = {
                                        Text(if (session.archived) "恢复归档" else "归档会话")
                                    },
                                    onClick = {
                                        menuExpanded = false
                                        onToggleArchived()
                                    },
                                )
                            }
                        }
                        if (session.running && session.capabilities.stop) {
                            TextButton(
                                onClick = onStop,
                                modifier = Modifier.height(32.dp),
                                contentPadding = PaddingValues(horizontal = 7.dp),
                            ) {
                                Icon(
                                    Icons.Outlined.Stop,
                                    contentDescription = null,
                                    modifier = Modifier.size(14.dp),
                                )
                                Spacer(Modifier.width(2.dp))
                                Text("停止", style = MaterialTheme.typography.labelSmall)
                            }
                        }
                    }
                } else {
                    Text("会话", style = MaterialTheme.typography.titleLarge)
                }
            }
            val contextUsage = session?.contextUsage?.takeIf { it.used > 0 }
            if (contextUsage != null) {
                ContextUsageBar(
                    usage = contextUsage,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 16.dp, vertical = 2.dp),
                )
            }
            if (infoExpanded && session != null) {
                Surface(
                    color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.45f),
                    modifier = Modifier
                        .fillMaxWidth()
                            .padding(horizontal = 14.dp, vertical = 5.dp),
                    shape = RoundedCornerShape(10.dp),
                    border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
                ) {
                    Column(modifier = Modifier.padding(12.dp)) {
                        MetadataRow(
                            label = "来源",
                            value = "${session.brand.displayName} · ${session.brand.wire}",
                            onCopy = onCopy,
                        )
                        MetadataRow(
                            label = "完整目录",
                            value = session.cwd.ifBlank { "未提供" },
                            onCopy = onCopy,
                        )
                        MetadataRow(
                            label = "Session ID",
                            value = session.sessionId.ifBlank { "未提供" },
                            onCopy = onCopy,
                            monospace = true,
                        )
                        MetadataRow(
                            label = "标题来源",
                            value = titleSourceLabel(session.titleSource) +
                                if (session.titleSource.isNotBlank()) {
                                    " · ${session.titleSource}"
                                } else {
                                    ""
                                },
                            onCopy = onCopy,
                        )
                        MetadataRow(
                            label = "模型",
                            value = buildString {
                                append(session.model.ifBlank { "未提供" })
                                if (session.effort.isNotBlank()) {
                                    append(" · ")
                                    append(session.effort)
                                }
                            },
                            onCopy = onCopy,
                        )
                        MetadataRow(
                            label = "Git 分支",
                            value = session.branch.ifBlank { "未提供" },
                            onCopy = onCopy,
                            monospace = true,
                        )
                        if (session.permissionMode.isNotBlank()) {
                            MetadataRow(
                                label = "权限模式",
                                value = session.permissionMode,
                                onCopy = onCopy,
                                monospace = true,
                            )
                        }
                        session.contextUsage?.takeIf { it.used > 0 }?.let { usage ->
                            MetadataRow(
                                label = "上下文",
                                value = if (usage.limit > 0) {
                                    "${usage.usedLabel} / ${usage.limitLabel}（${usage.percent}%）"
                                } else {
                                    "已用 ${usage.usedLabel} tokens"
                                },
                                onCopy = onCopy,
                                monospace = true,
                            )
                        }
                        MetadataRow(
                            label = "能力",
                            value = buildString {
                                append("回复")
                                append(if (profile.capabilities.reply) "✓" else "×")
                                append(" 新建")
                                append(if (profile.capabilities.newSession) "✓" else "×")
                                append(" 切模型")
                                append(if (profile.capabilities.switchModel) "✓" else "×")
                                append(" 停止")
                                append(if (profile.capabilities.stop) "✓" else "×")
                            },
                            onCopy = { _, _ -> },
                            copyable = false,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun HeaderTag(text: String, emphasized: Boolean = false) {
    Surface(
        color = if (emphasized) {
            MaterialTheme.colorScheme.primary.copy(alpha = 0.1f)
        } else {
            MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.55f)
        },
        shape = RoundedCornerShape(5.dp),
    ) {
        Text(
            text = text,
            modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp),
            style = MaterialTheme.typography.labelSmall.copy(fontSize = 11.sp),
            color = if (emphasized) {
                MaterialTheme.colorScheme.primary
            } else {
                MaterialTheme.colorScheme.onSurfaceVariant
            },
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

@Composable
private fun HeaderRunState(
    session: SessionSummary,
    itemsState: dev.agentpush.poc.SessionItemsState?,
) {
    if (session.running) {
        var now by remember { mutableStateOf(System.currentTimeMillis()) }
        LaunchedEffect(session.runningSince) {
            while (true) {
                now = System.currentTimeMillis()
                delay(1_000)
            }
        }
        Surface(
            color = CodexGreen.copy(alpha = 0.11f),
            shape = RoundedCornerShape(50),
        ) {
            Row(
                modifier = Modifier.padding(horizontal = 7.dp, vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(5.dp),
            ) {
                CircularProgressIndicator(
                    modifier = Modifier.size(11.dp),
                    strokeWidth = 2.dp,
                    color = CodexGreen,
                )
                Text(
                    text = "运行中" +
                        elapsedLabel(session.runningSince, now).let {
                            if (it.isBlank()) "" else " $it"
                        },
                    style = MaterialTheme.typography.labelSmall,
                    color = CodexGreen,
                    maxLines = 1,
                )
            }
        }
        return
    }
    val liveTurn = itemsState?.turns?.lastOrNull()
    if (liveTurn?.state == "failed") {
        SessionStatusBadge(status = SessionStatus.ERROR)
        return
    }
    SessionStatusBadge(status = session.status)
}

@Composable
private fun ContextUsageBar(
    usage: ContextUsage,
    modifier: Modifier = Modifier,
) {
    val color = when {
        usage.fraction >= 0.85f -> MaterialTheme.colorScheme.error
        usage.fraction >= 0.65f -> ClaudeOrange
        else -> CodexGreen
    }
    Column(
        modifier = if (usage.limit > 0) {
            modifier.testTag("context-usage-summary")
        } else {
            modifier.testTag("context-usage-pill")
        },
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(bottom = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = if (usage.limit > 0) {
                    "上下文 ${usage.usedLabel} / ${usage.limitLabel}"
                } else {
                    "上下文已用 ${usage.usedLabel} tokens"
                },
                style = MaterialTheme.typography.labelSmall.copy(
                    fontFamily = dev.agentpush.poc.ui.theme.HomingMono,
                    fontSize = 10.sp,
                ),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
            )
            Spacer(Modifier.weight(1f))
            if (usage.limit > 0) {
                Text(
                    text = "${usage.percent}%",
                    style = MaterialTheme.typography.labelSmall.copy(
                        fontFamily = dev.agentpush.poc.ui.theme.HomingMono,
                        fontSize = 10.sp,
                    ),
                    color = color,
                )
            }
        }
        if (usage.limit > 0) {
            LinearProgressIndicator(
                progress = { usage.fraction },
                modifier = Modifier
                    .fillMaxWidth()
                    .height(3.dp)
                    .testTag("context-usage-bar"),
                color = color,
                trackColor = MaterialTheme.colorScheme.surfaceVariant,
            )
        }
    }
}

private fun elapsedLabel(since: String, now: Long): String {
    if (since.isBlank()) return ""
    val start = runCatching { java.time.Instant.parse(since).toEpochMilli() }.getOrNull()
        ?: return ""
    val seconds = ((now - start) / 1000L).coerceAtLeast(0L)
    return when {
        seconds < 60 -> "${seconds}s"
        seconds < 3600 -> "${seconds / 60}m ${seconds % 60}s"
        else -> "${seconds / 3600}h ${(seconds % 3600) / 60}m"
    }
}

private fun effortDisplayLabel(value: String): String {
    return when (value.trim().lowercase()) {
        "", "default" -> "默认"
        "none", "minimal", "min" -> "最低"
        "low" -> "低"
        "medium", "mid" -> "中"
        "high", "xhigh", "ultra", "max", "highest" -> "高"
        else -> value
    }
}

@Composable
private fun MetadataRow(
    label: String,
    value: String,
    onCopy: (String, String) -> Unit,
    monospace: Boolean = false,
    copyable: Boolean = true,
) {
    val canCopy = copyable && value.isNotBlank() && value != "未提供"
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 2.dp),
        verticalAlignment = Alignment.Top,
    ) {
        Text(
            text = label,
            modifier = Modifier
                .width(74.dp)
                .padding(top = 2.dp),
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            text = value,
            modifier = Modifier
                .weight(1f)
                .padding(top = 1.dp),
            style = MaterialTheme.typography.bodySmall.copy(
                fontFamily = if (monospace) {
                    HomingMono
                } else {
                    androidx.compose.ui.text.font.FontFamily.Default
                },
            ),
            color = MaterialTheme.colorScheme.onSurface,
        )
        IconButton(
            onClick = { onCopy(label, value) },
            enabled = canCopy,
            modifier = Modifier.size(26.dp),
        ) {
            Icon(
                imageVector = Icons.Outlined.ContentCopy,
                contentDescription = "复制$label",
                modifier = Modifier.size(14.dp),
                tint = if (canCopy) {
                    MaterialTheme.colorScheme.onSurfaceVariant
                } else {
                    MaterialTheme.colorScheme.outline
                },
            )
        }
    }
}

@Composable
private fun SessionItemRow(
    item: SessionItem,
    session: SessionSummary,
    settings: dev.agentpush.poc.AppSettings,
    reasoningDurationSeconds: Long? = null,
    onOpenUrl: (String) -> Unit,
    onCopyCode: (String) -> Unit,
    onCopyText: (String) -> Unit,
    onDecision: (Long?, Boolean) -> Unit,
) {
    when (item.type) {
        SessionItemType.USER -> UserItemBubble(
            item = item,
            onLongClick = { onCopyText(item.text) },
        )
        SessionItemType.ASSISTANT -> AssistantItemCard(
            item = item,
            brand = session.brand,
            settings = settings,
            onOpenUrl = onOpenUrl,
            onCopyCode = onCopyCode,
        )
        SessionItemType.REASONING -> ReasoningItemCard(item, reasoningDurationSeconds)
        SessionItemType.TOOL_CALL -> ToolCallItemCard(
            item = item,
            onCopy = {
                onCopyText(
                    buildString {
                        if (item.tool.name.isNotBlank()) appendLine(item.tool.name)
                        if (item.tool.inputSummary.isNotBlank()) {
                            appendLine(item.tool.inputSummary)
                        }
                        if (item.tool.outputTail.isNotBlank()) {
                            appendLine(item.tool.outputTail)
                        }
                    }.trim(),
                )
            },
        )
        SessionItemType.FILE_CHANGE -> FileChangeItemCard(
            item = item,
            onCopy = {
                onCopyText(
                    item.files.joinToString("\n") { file ->
                        "${file.path}  +${file.added} -${file.removed}" +
                            if (file.diff.isNotBlank()) "\n${file.diff}" else ""
                    }.ifBlank { item.text },
                )
            },
        )
        SessionItemType.PLAN -> PlanItemCard(item)
        SessionItemType.APPROVAL -> ApprovalItemCard(
            item = item.copy(
                eventId = item.eventId
                    ?: session.latestEvent?.takeIf { it.isPermission }?.id,
            ),
            canApprove = session.capabilities.approve,
            onDecision = onDecision,
        )
        SessionItemType.ERROR -> ErrorItemCard(item)
        SessionItemType.SYSTEM -> SystemItemRow(item)
        SessionItemType.OTHER -> if (item.text.isNotBlank()) {
            AssistantItemCard(
                item = item.copy(type = SessionItemType.ASSISTANT),
                brand = session.brand,
                settings = settings,
                onOpenUrl = onOpenUrl,
                onCopyCode = onCopyCode,
            )
        }
    }
}

@Composable
private fun TranscriptMessageRow(
    message: TranscriptMessage,
    settings: dev.agentpush.poc.AppSettings,
    brand: dev.agentpush.poc.AgentBrand,
    displayName: String,
    onOpenUrl: (String) -> Unit,
    onOpenBrowser: (String, String) -> Unit,
    onOpenImage: (List<Attachment>, Int) -> Unit,
    onCopyCode: (String) -> Unit,
) {
    if (message.isUser) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.End,
        ) {
            Surface(
                modifier = Modifier.widthIn(max = 340.dp),
                color = MaterialTheme.colorScheme.secondaryContainer,
                shape = RoundedCornerShape(16.dp),
            ) {
                Column(modifier = Modifier.padding(13.dp)) {
                    Text(
                        text = "我在电脑上的提问",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSecondaryContainer.copy(alpha = 0.7f),
                    )
                    Spacer(Modifier.height(4.dp))
                    Text(
                        text = message.text,
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onSecondaryContainer,
                    )
                    AttachmentCardList(
                        attachments = message.attachments,
                        settings = settings,
                        onOpenHtml = { attachment ->
                            onOpenBrowser(
                                AgentApi.attachmentUrl(settings, attachment),
                                attachment.name,
                            )
                        },
                        onOpenImage = { index ->
                            onOpenImage(message.attachments.filter { it.isImage }, index)
                        },
                        modifier = Modifier.padding(top = 8.dp),
                    )
                }
            }
        }
        return
    }

    AgentMessageCard(
        text = message.text,
        attachments = message.attachments,
        settings = settings,
        brand = brand,
        displayName = displayName,
        timestamp = message.ts,
        onOpenUrl = onOpenUrl,
        onOpenBrowser = onOpenBrowser,
        onOpenImage = onOpenImage,
        onCopyCode = onCopyCode,
    )
}

@Composable
private fun AgentMessageCard(
    text: String,
    attachments: List<Attachment>,
    settings: dev.agentpush.poc.AppSettings,
    brand: dev.agentpush.poc.AgentBrand,
    displayName: String,
    timestamp: String,
    onOpenUrl: (String) -> Unit,
    onOpenBrowser: (String, String) -> Unit,
    onOpenImage: (List<Attachment>, Int) -> Unit,
    onCopyCode: (String) -> Unit,
) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        color = MaterialTheme.colorScheme.surface,
        shape = RoundedCornerShape(16.dp),
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
        tonalElevation = 1.dp,
    ) {
        Column(modifier = Modifier.padding(14.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = displayName,
                    style = MaterialTheme.typography.labelMedium,
                    color = brand.accentColor(),
                )
                Spacer(Modifier.weight(1f))
                if (timestamp.isNotBlank()) {
                    Text(
                        text = formatClock(timestamp),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            Spacer(Modifier.height(6.dp))
            RichMarkdown(
                source = rewriteMarkdownImageUrls(
                    text.ifBlank { "（无文字内容）" },
                    attachments,
                    settings,
                ),
                onCopyCode = onCopyCode,
                onOpenUrl = onOpenUrl,
            )
            AttachmentCardList(
                attachments = attachments,
                settings = settings,
                onOpenHtml = { attachment ->
                    onOpenBrowser(
                        AgentApi.attachmentUrl(settings, attachment),
                        attachment.name,
                    )
                },
                onOpenImage = { index ->
                    onOpenImage(attachments.filter { it.isImage }, index)
                },
                modifier = Modifier.padding(top = 8.dp),
            )
        }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun AgentEventCard(
    event: AgentEvent,
    session: SessionSummary,
    settings: dev.agentpush.poc.AppSettings,
    onReply: (String) -> Unit,
    onOpenUrl: (String) -> Unit,
    onOpenBrowser: (String, String) -> Unit,
    onOpenImage: (List<Attachment>, Int) -> Unit,
    onCopyCode: (String) -> Unit,
    onCopyText: () -> Unit,
    onShareText: (String) -> Unit,
    proxyCapability: String?,
) {
    val replied = session.replies.any {
        it.eventId == event.id && it.status != ReplyStatus.FAILED
    }
    val isLatest = session.latestEvent?.id == event.id
    val pending = event.isPending && isLatest && !replied
    val actionablePermission = event.isPermission && isLatest && !replied
    val accent = event.brand.accentColor()
    var menu by remember(event.id) { mutableStateOf(false) }

    Column(
        modifier = Modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(5.dp),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(7.dp),
        ) {
            AgentAvatar(brand = event.brand, size = 27.dp)
            Text(event.brand.displayName, style = MaterialTheme.typography.labelLarge, color = accent)
            if (pending) {
                PendingTag(if (event.isPermission) "需要授权" else "等待输入")
            }
            Spacer(Modifier.weight(1f))
            Text(
                text = formatClock(event.ts),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Box {
            Surface(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 34.dp)
                    .combinedClickable(
                        onClick = {},
                        onLongClick = { menu = true },
                    ),
                color = if (pending) {
                    accent.copy(alpha = 0.075f)
                } else {
                    MaterialTheme.colorScheme.surface
                },
                shape = RoundedCornerShape(
                    topStart = 6.dp,
                    topEnd = 18.dp,
                    bottomEnd = 18.dp,
                    bottomStart = 18.dp,
                ),
                border = BorderStroke(
                    1.dp,
                    if (pending) accent.copy(alpha = 0.3f)
                    else MaterialTheme.colorScheme.outlineVariant,
                ),
                tonalElevation = 1.dp,
            ) {
                Column(modifier = Modifier.padding(14.dp)) {
                    RichMarkdown(
                        source = rewriteMarkdownImageUrls(
                            event.text.ifBlank { event.title },
                            event.attachments,
                            settings,
                        ),
                        onCopyCode = onCopyCode,
                        onOpenUrl = onOpenUrl,
                    )
                    AttachmentCardList(
                        attachments = event.attachments,
                        settings = settings,
                        onOpenHtml = { attachment ->
                            onOpenBrowser(
                                AgentApi.attachmentUrl(settings, attachment),
                                attachment.name,
                            )
                        },
                        onOpenImage = { index ->
                            onOpenImage(event.attachments.filter { it.isImage }, index)
                        },
                        modifier = Modifier.padding(top = 8.dp),
                    )
                    if (event.localhostLinks.isNotEmpty()) {
                        Spacer(Modifier.height(9.dp))
                        Surface(
                            color = ClaudeOrange.copy(alpha = 0.1f),
                            shape = RoundedCornerShape(8.dp),
                        ) {
                            Column(modifier = Modifier.padding(9.dp)) {
                                Text(
                                    text = "正文包含 Mac 本机端口链接，手机上无法直接访问。可尝试通过中继代理打开：",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                                Row(
                                    modifier = Modifier.horizontalScroll(rememberScrollState()),
                                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                                ) {
                                    event.localhostLinks.forEach { port ->
                                        TextButton(
                                            onClick = {
                                                onOpenBrowser(
                                                    AgentApi.proxyUrl(
                                                        settings,
                                                        port,
                                                        capability = proxyCapability,
                                                    ),
                                                    "通过中继 · :$port",
                                                )
                                            },
                                        ) {
                                            Text(":$port")
                                        }
                                    }
                                }
                            }
                        }
                    }
                    if (actionablePermission) {
                        Spacer(Modifier.height(13.dp))
                        Row(
                            horizontalArrangement = Arrangement.spacedBy(9.dp),
                            modifier = Modifier.fillMaxWidth(),
                        ) {
                            Button(
                                onClick = { onReply("同意") },
                                modifier = Modifier.weight(1f),
                                shape = RoundedCornerShape(11.dp),
                            ) {
                                Text("同意")
                            }
                            OutlinedButton(
                                onClick = { onReply("拒绝") },
                                modifier = Modifier.weight(1f),
                                shape = RoundedCornerShape(11.dp),
                            ) {
                                Text("拒绝")
                            }
                        }
                    }
                }
            }
            DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                DropdownMenuItem(
                    text = { Text("复制全文") },
                    leadingIcon = { Icon(Icons.Outlined.ContentCopy, null) },
                    onClick = {
                        menu = false
                        onCopyText()
                    },
                )
                DropdownMenuItem(
                    text = { Text("分享") },
                    leadingIcon = { Icon(Icons.Outlined.Share, null) },
                    onClick = {
                        menu = false
                        onShareText(event.text)
                    },
                )
                DropdownMenuItem(
                    text = { Text("选择文字") },
                    leadingIcon = { Icon(Icons.Outlined.MoreVert, null) },
                    onClick = { menu = false },
                )
            }
        }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun UserReplyBubble(
    reply: AgentReply,
    onRetry: () -> Unit,
    onLongClick: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 56.dp),
        horizontalAlignment = Alignment.End,
    ) {
        Surface(
            modifier = Modifier
                .widthIn(max = 340.dp)
                .combinedClickable(
                    onClick = {},
                    onLongClick = onLongClick,
                ),
            color = MaterialTheme.colorScheme.primary,
            shape = RoundedCornerShape(
                topStart = 18.dp,
                topEnd = 6.dp,
                bottomEnd = 18.dp,
                bottomStart = 18.dp,
            ),
        ) {
            Column(modifier = Modifier.padding(horizontal = 14.dp, vertical = 10.dp)) {
                Text(
                    text = reply.text,
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onPrimary,
                )
                if (reply.files.isNotEmpty()) {
                    Text(
                        text = "附件 ${reply.files.size} 个",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onPrimary.copy(alpha = 0.75f),
                    )
                }
                if (reply.model.isNotBlank()) {
                    Text(
                        text = "本次 · ${reply.model}" +
                            if (reply.effort.isNotBlank()) " · ${reply.effort}" else "",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onPrimary.copy(alpha = 0.72f),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
        }
        Spacer(Modifier.height(4.dp))
        ReplyStatusLabel(
            reply = reply,
            onClick = if (reply.status == ReplyStatus.FAILED) onRetry else null,
        )
    }
}

@Composable
private fun ReplyStatusLabel(reply: AgentReply, onClick: (() -> Unit)?) {
    val color = if (reply.status == ReplyStatus.FAILED) {
        MaterialTheme.colorScheme.error
    } else {
        MaterialTheme.colorScheme.onSurfaceVariant
    }
    val icon = when (reply.status) {
        ReplyStatus.QUEUED, ReplyStatus.HELD -> Icons.Outlined.Schedule
        ReplyStatus.INJECTED -> Icons.Outlined.CheckCircle
        ReplyStatus.FAILED -> Icons.Outlined.ErrorOutline
    }
    Row(
        modifier = Modifier
            .clip(RoundedCornerShape(8.dp))
            .then(if (onClick != null) Modifier.combinedClickable(
                onClick = onClick,
                onLongClick = onClick,
            ) else Modifier)
            .padding(horizontal = 5.dp, vertical = 3.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Icon(icon, contentDescription = null, modifier = Modifier.size(13.dp), tint = color)
        Text(
            text = if (reply.status == ReplyStatus.FAILED) {
                "发送失败，点此重试${reply.message?.let { " · ${it.take(80)}" }.orEmpty()}"
            } else {
                reply.status.label
            },
            style = MaterialTheme.typography.bodySmall,
            color = color,
        )
    }
}

@Composable
private fun SessionComposer(
    text: String,
    onTextChange: (String) -> Unit,
    pending: List<PendingAttachment>,
    modelLabel: String,
    effortLabel: String,
    showEffortPill: Boolean,
    modelOverride: Boolean,
    canSwitchModel: Boolean,
    modelDisabledReason: String,
    onModelClick: () -> Unit,
    enabled: Boolean,
    canSend: Boolean,
    onRemove: (String) -> Unit,
    onRetryUpload: (PendingAttachment) -> Unit,
    onPhotoPicker: () -> Unit,
    onCamera: () -> Unit,
    onFilePicker: () -> Unit,
    onSpeech: () -> Unit,
    running: Boolean,
    runningSince: String,
    canStop: Boolean,
    onStop: () -> Unit,
    onSend: () -> Unit,
) {
    var attachMenu by remember { mutableStateOf(false) }
    var now by remember { mutableStateOf(System.currentTimeMillis()) }
    LaunchedEffect(running, runningSince) {
        while (running) {
            now = System.currentTimeMillis()
            delay(1_000)
        }
    }
    Surface(
        color = MaterialTheme.colorScheme.surface.copy(alpha = 0.98f),
        tonalElevation = 8.dp,
        shadowElevation = 12.dp,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .navigationBarsPadding()
                .imePadding()
                .testTag("session-composer"),
        ) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 12.dp, end = 10.dp, top = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                ModelPill(
                    label = modelLabel,
                    enabled = canSwitchModel,
                    overridden = modelOverride,
                    onClick = onModelClick,
                    modifier = Modifier.testTag("model-pill"),
                )
                if (showEffortPill) {
                    ModelPill(
                        label = "强度 · ${effortLabel.ifBlank { "默认" }}",
                        enabled = canSwitchModel,
                        overridden = modelOverride,
                        onClick = onModelClick,
                        modifier = Modifier.testTag("effort-pill"),
                    )
                }
                if (!canSwitchModel && modelDisabledReason.isNotBlank()) {
                    Text(
                        text = modelDisabledReason,
                        modifier = Modifier.weight(1f),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.outline,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                } else if (modelOverride) {
                    Text(
                        text = "仅下一条",
                        style = MaterialTheme.typography.labelSmall,
                        color = ClaudeOrange,
                    )
                    Spacer(Modifier.weight(1f))
                } else {
                    Spacer(Modifier.weight(1f))
                }
                if (running) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(11.dp),
                        strokeWidth = 2.dp,
                        color = CodexGreen,
                    )
                    Text(
                        text = "处理中" +
                            elapsedLabel(runningSince, now).let {
                                if (it.isBlank()) "" else " $it"
                            },
                        style = MaterialTheme.typography.labelSmall,
                        color = CodexGreen,
                        maxLines = 1,
                    )
                    if (canStop) {
                        TextButton(
                            onClick = onStop,
                            modifier = Modifier.size(width = 52.dp, height = 30.dp),
                        ) {
                            Icon(
                                Icons.Outlined.Stop,
                                contentDescription = null,
                                modifier = Modifier.size(15.dp),
                            )
                            Text("停止", style = MaterialTheme.typography.labelSmall)
                        }
                    }
                }
            }
            if (pending.isNotEmpty()) {
                LazyRow(
                    modifier = Modifier.fillMaxWidth(),
                    contentPadding = PaddingValues(horizontal = 12.dp, vertical = 7.dp),
                    horizontalArrangement = Arrangement.spacedBy(7.dp),
                ) {
                    items(pending, key = { it.id }) { attachment ->
                        PendingAttachmentChip(
                            attachment = attachment,
                            onRemove = { onRemove(attachment.id) },
                            onRetry = { onRetryUpload(attachment) },
                        )
                    }
                }
            }
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 8.dp, vertical = 8.dp),
                verticalAlignment = Alignment.Bottom,
                horizontalArrangement = Arrangement.spacedBy(5.dp),
            ) {
                Box {
                    IconButton(onClick = { attachMenu = true }, enabled = enabled) {
                        Icon(Icons.Outlined.AttachFile, contentDescription = "添加附件")
                    }
                    DropdownMenu(expanded = attachMenu, onDismissRequest = { attachMenu = false }) {
                        DropdownMenuItem(
                            text = { Text("相册（可多选）") },
                            onClick = {
                                attachMenu = false
                                onPhotoPicker()
                            },
                        )
                        DropdownMenuItem(
                            text = { Text("拍照") },
                            onClick = {
                                attachMenu = false
                                onCamera()
                            },
                        )
                        DropdownMenuItem(
                            text = { Text("任意文件") },
                            onClick = {
                                attachMenu = false
                                onFilePicker()
                            },
                        )
                    }
                }
                IconButton(onClick = onSpeech, enabled = enabled) {
                    Icon(Icons.Outlined.Mic, contentDescription = "语音输入")
                }
                OutlinedTextField(
                    value = text,
                    onValueChange = onTextChange,
                    modifier = Modifier.weight(1f),
                    placeholder = {
                        Text(if (enabled) "回复这个会话…" else "等待会话就绪…")
                    },
                    enabled = enabled,
                    maxLines = 5,
                    shape = RoundedCornerShape(18.dp),
                    textStyle = MaterialTheme.typography.bodyLarge,
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
                    keyboardActions = KeyboardActions(onSend = { if (canSend) onSend() }),
                    colors = TextFieldDefaults.colors(
                        focusedContainerColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.42f),
                        unfocusedContainerColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.32f),
                        focusedIndicatorColor = Color.Transparent,
                        unfocusedIndicatorColor = Color.Transparent,
                    ),
                )
                Surface(
                    onClick = onSend,
                    enabled = canSend,
                    modifier = Modifier.size(48.dp),
                    shape = CircleShape,
                    color = if (canSend) {
                        MaterialTheme.colorScheme.primary
                    } else {
                        MaterialTheme.colorScheme.surfaceVariant
                    },
                    contentColor = if (canSend) {
                        MaterialTheme.colorScheme.onPrimary
                    } else {
                        MaterialTheme.colorScheme.onSurfaceVariant
                    },
                ) {
                    Box(contentAlignment = Alignment.Center) {
                        Icon(
                            Icons.AutoMirrored.Outlined.Send,
                            contentDescription = "发送",
                            modifier = Modifier.size(21.dp),
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun PendingAttachmentChip(
    attachment: PendingAttachment,
    onRemove: () -> Unit,
    onRetry: () -> Unit,
) {
    Surface(
        modifier = Modifier.width(190.dp),
        color = if (attachment.error != null) {
            MaterialTheme.colorScheme.errorContainer.copy(alpha = 0.7f)
        } else {
            MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.65f)
        },
        shape = RoundedCornerShape(10.dp),
    ) {
        Column(modifier = Modifier.padding(start = 9.dp, end = 2.dp, top = 5.dp, bottom = 6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = attachment.name,
                    modifier = Modifier.weight(1f),
                    style = MaterialTheme.typography.labelMedium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                IconButton(onClick = onRemove, modifier = Modifier.size(27.dp)) {
                    Icon(Icons.Outlined.Close, "移除", modifier = Modifier.size(15.dp))
                }
            }
            if (attachment.uploading) {
                LinearProgressIndicator(
                    progress = { attachment.progress },
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(end = 7.dp, top = 3.dp),
                )
                Text(
                    text = "上传中 ${(attachment.progress * 100).toInt()}%",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            } else if (attachment.error != null) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = attachment.error.take(42),
                        modifier = Modifier.weight(1f),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onErrorContainer,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    TextButton(onClick = onRetry, modifier = Modifier.size(width = 48.dp, height = 28.dp)) {
                        Text("重试", style = MaterialTheme.typography.labelSmall)
                    }
                }
            } else {
                Text(
                    text = "已上传 · ${attachment.size / 1024} KB",
                    style = MaterialTheme.typography.labelSmall,
                    color = CodexGreen,
                )
            }
        }
    }
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

private fun normalizeComparableText(value: String): String {
    return value.replace(Regex("\\s+"), " ").trim()
}

private fun reasoningDuration(timeline: List<TimelineItem>, index: Int): Long? {
    val current = timeline.getOrNull(index) ?: return null
    val start = runCatching {
        java.time.Instant.parse(current.timestamp).toEpochMilli()
    }.getOrNull() ?: return null
    val next = timeline.drop(index + 1)
        .firstOrNull { it.timestamp.isNotBlank() }
        ?.timestamp
        ?: return null
    val end = runCatching { java.time.Instant.parse(next).toEpochMilli() }.getOrNull()
        ?: return null
    val seconds = ((end - start) / 1_000L).coerceAtLeast(0L)
    return seconds.takeIf { it in 1..3_600 }
}
