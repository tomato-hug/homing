package dev.agentpush.poc.ui.screens

import android.Manifest
import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.PackageManager
import android.speech.RecognizerIntent
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Bedtime
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Mic
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.material.icons.outlined.NotificationsOff
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Shield
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.agentpush.poc.AgentEvent
import dev.agentpush.poc.AgentViewModel
import dev.agentpush.poc.ConnectionState
import dev.agentpush.poc.TransportKind
import dev.agentpush.poc.InboxEntryKind
import dev.agentpush.poc.SessionItemType
import dev.agentpush.poc.SessionStatus
import dev.agentpush.poc.SessionSummary
import dev.agentpush.poc.elapsedSince
import dev.agentpush.poc.formatDiagnosticTime
import dev.agentpush.poc.relativeTime
import dev.agentpush.poc.sessionSummaryLine
import dev.agentpush.poc.ui.AgentAvatar
import dev.agentpush.poc.ui.HomingCard
import dev.agentpush.poc.ui.HomingPigeon
import dev.agentpush.poc.ui.HomingPigeonScene
import dev.agentpush.poc.ui.LocalHomingStrings
import dev.agentpush.poc.ui.PigeonNeck
import dev.agentpush.poc.ui.PigeonScene
import dev.agentpush.poc.ui.MacStatusPill
import dev.agentpush.poc.ui.PendingTag
import dev.agentpush.poc.ui.PigeonState
import dev.agentpush.poc.ui.UnreadBadge
import dev.agentpush.poc.protocol.PairedComputerStore
import dev.agentpush.poc.ui.theme.HomingMono
import dev.agentpush.poc.ui.theme.HomingPalette
import kotlinx.coroutines.delay

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomingInboxScreen(
    viewModel: AgentViewModel,
    onOpenSession: (String) -> Unit,
    onNewSession: () -> Unit,
    onOpenAllSessions: () -> Unit,
    onAddComputer: (() -> Unit)? = null,
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    var searchOpen by rememberSaveable { mutableStateOf(false) }
    var search by rememberSaveable { mutableStateOf("") }
    var bobKey by remember { mutableIntStateOf(0) }
    var bobActive by remember { mutableStateOf(false) }
    var lastSeenEventId by remember { mutableLongStateOf(0L) }
    var waitingDrafts by remember { mutableStateOf<Map<String, String>>(emptyMap()) }
    var voiceTarget by remember { mutableStateOf<String?>(null) }
    var pendingVoiceTarget by remember { mutableStateOf<String?>(null) }

    val speechLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult(),
    ) { result ->
        val target = voiceTarget
        if (result.resultCode == Activity.RESULT_OK && target != null) {
            val spoken = result.data
                ?.getStringArrayListExtra(RecognizerIntent.EXTRA_RESULTS)
                ?.firstOrNull()
                ?.trim()
                .orEmpty()
            if (spoken.isNotBlank()) {
                val current = waitingDrafts[target].orEmpty()
                waitingDrafts = waitingDrafts + (
                    target to if (current.isBlank()) spoken else "$current $spoken"
                    )
            }
        }
        voiceTarget = null
    }

    fun launchSpeech(target: String) {
        voiceTarget = target
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
            voiceTarget = null
            Toast.makeText(context, "设备没有可用的语音识别服务", Toast.LENGTH_SHORT).show()
        }
    }

    val audioPermission = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { granted ->
        val target = pendingVoiceTarget
        pendingVoiceTarget = null
        if (granted && target != null) {
            launchSpeech(target)
        } else if (!granted) {
            Toast.makeText(context, "需要麦克风权限才能语音输入", Toast.LENGTH_SHORT).show()
        }
    }

    fun requestSpeech(target: String) {
        if (ContextCompat.checkSelfPermission(
                context,
                Manifest.permission.RECORD_AUDIO,
            ) == PackageManager.PERMISSION_GRANTED
        ) {
            launchSpeech(target)
        } else {
            pendingVoiceTarget = target
            audioPermission.launch(Manifest.permission.RECORD_AUDIO)
        }
    }

    val mobileInbox = state.mobileInbox
    val allSessions = remember(state) {
        mobileInbox.visibleSessions(state.sessions())
            .filterNot { it.archived || it.isEmpty }
            .filter { state.phoneConversations.isInboxVisible(it, state.activeComputerKey) }
    }
    val query = search.trim()
    val sessions = remember(allSessions, query) {
        if (query.isBlank()) {
            allSessions
        } else {
            allSessions.filter { session ->
                session.title.contains(query, true) ||
                    session.project.contains(query, true) ||
                    session.workspace.contains(query, true) ||
                    session.cwd.contains(query, true) ||
                    session.branch.contains(query, true) ||
                    sessionSummaryLine(session).contains(query, true)
            }
        }
    }
    val inbox = state.inbox?.takeIf { it.available }
    val useServerInbox = inbox != null && query.isBlank()
    val sessionsById = remember(allSessions) {
        allSessions.associateBy { it.sessionId }
    }
    val permissions = remember(sessions, inbox, useServerInbox, sessionsById) {
        if (useServerInbox) {
            inbox.waiting
                .filter { it.kind == InboxEntryKind.APPROVAL && it.read != true }
                .mapNotNull { sessionsById[it.sessionId] }
                .distinctBy { it.sessionId }
        } else {
            sessions.filter { it.hasPermissionRequest() }
        }
    }
    val waiting = remember(sessions, inbox, useServerInbox, sessionsById) {
        if (useServerInbox) {
            inbox.waiting
                .filter {
                    (it.kind == InboxEntryKind.NEEDS_REPLY || it.kind == null) &&
                        it.read != true
                }
                .mapNotNull { sessionsById[it.sessionId] }
                .distinctBy { it.sessionId }
        } else {
            sessions.filter { it.hasWaitingInput() && !it.hasPermissionRequest() }
        }
    }
    val errors = remember(sessions, inbox, useServerInbox, sessionsById) {
        if (useServerInbox) {
            inbox.waiting
                .filter { it.kind == InboxEntryKind.ERROR && it.read != true }
                .mapNotNull { sessionsById[it.sessionId] }
                .distinctBy { it.sessionId }
        } else {
            sessions.filter { it.status == SessionStatus.ERROR }
        }
    }
    val running = remember(sessions, inbox, useServerInbox, sessionsById) {
        if (useServerInbox) {
            inbox.running
                .mapNotNull { sessionsById[it.sessionId] }
                .distinctBy { it.sessionId }
        } else {
            sessions.filter { it.running }
        }
    }
    val completed = remember(sessions, inbox, useServerInbox, sessionsById) {
        if (useServerInbox) {
            inbox.recentDone
                .mapNotNull { sessionsById[it.sessionId] }
                .distinctBy { it.sessionId }
        } else {
            sessions.filterNot {
                it.running ||
                    it.hasPermissionRequest() ||
                    it.hasWaitingInput() ||
                    it.status == SessionStatus.ERROR
            }.sortedByDescending { it.lastTs }.take(8)
        }
    }
    val attentionCount = permissions.size + waiting.size + errors.size
    val allRunningCount = running.size
    val latestEventId = state.events.keys.maxOrNull() ?: 0L

    LaunchedEffect(latestEventId) {
        if (lastSeenEventId == 0L) {
            lastSeenEventId = latestEventId
            return@LaunchedEffect
        }
        if (latestEventId > lastSeenEventId) {
            lastSeenEventId = latestEventId
            bobKey += 1
            bobActive = true
            delay(900)
            bobActive = false
        }
    }

    var now by remember { mutableLongStateOf(System.currentTimeMillis()) }
    LaunchedEffect(running.isNotEmpty()) {
        while (running.isNotEmpty()) {
            now = System.currentTimeMillis()
            delay(1_000)
        }
    }

    val pigeonState = when {
        state.isRefreshing || state.connection == ConnectionState.RECONNECTING ->
            PigeonState.Flap
        bobActive -> PigeonState.BobOnce
        else -> PigeonState.Idle
    }

    val strings = LocalHomingStrings.current
    val hasPairedTarget = remember(state.settings.token, state.lastUpdatedAt) {
        state.settings.token.isNotBlank() || PairedComputerStore(context).load().isNotEmpty()
    }
    Box(modifier = Modifier.fillMaxSize()) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .statusBarsPadding(),
        ) {
            InboxTopBar(
                connection = state.connection,
                transportKind = state.transportKind,
                pigeonState = pigeonState,
                animationKey = bobKey,
                searchOpen = searchOpen,
                onToggleSearch = {
                    searchOpen = !searchOpen
                    if (!searchOpen) search = ""
                },
                inboxModeEnabled = mobileInbox.enabled,
                onToggleInboxMode = {
                    viewModel.setInboxMode(!mobileInbox.enabled)
                },
                waitingCount = attentionCount,
            )
            if (searchOpen) {
                InboxSearchField(
                    value = search,
                    onValueChange = { search = it },
                )
            }
            PhoneCleanupControls(
                inboxHasContent = attentionCount > 0 || running.isNotEmpty() || completed.isNotEmpty(),
                canUndo = state.phoneConversations.canUndo,
                hasHiddenSessions = state.phoneConversations.hasHiddenSessions,
                onClearInbox = viewModel::clearPhoneInbox,
                onUndo = viewModel::undoPhoneCleanup,
                onRestore = viewModel::restorePhoneSessions,
            )
            PullToRefreshBox(
                isRefreshing = state.isRefreshing,
                onRefresh = viewModel::refresh,
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f),
            ) {
                LazyColumn(
                    modifier = Modifier
                        .fillMaxSize()
                        .testTag("homing-inbox"),
                    contentPadding = PaddingValues(
                        start = 16.dp,
                        end = 16.dp,
                        top = 10.dp,
                        bottom = 104.dp,
                    ),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    item(key = "headline") {
                        InboxHeadline(
                            attentionCount = attentionCount,
                            runningCount = allRunningCount,
                            inboxEnabled = mobileInbox.enabled,
                        )
                    }
                    if (state.connection != ConnectionState.CONNECTED) {
                        if (!hasPairedTarget && onAddComputer != null) {
                            item(key = "unpaired-welcome") {
                                HomingCard {
                                    Column(
                                        modifier = Modifier
                                            .fillMaxWidth()
                                            .padding(18.dp),
                                        verticalArrangement = Arrangement.spacedBy(8.dp),
                                    ) {
                                        Text(
                                            text = strings.pairWelcomeTitle,
                                            style = MaterialTheme.typography.titleMedium,
                                            fontWeight = FontWeight.SemiBold,
                                            color = HomingPalette.Text,
                                        )
                                        Text(
                                            text = strings.pairWelcomeDesc,
                                            style = MaterialTheme.typography.bodyMedium,
                                            color = HomingPalette.Muted,
                                        )
                                        Spacer(Modifier.height(4.dp))
                                        Button(
                                            onClick = onAddComputer,
                                            modifier = Modifier
                                                .fillMaxWidth()
                                                .height(46.dp),
                                            shape = RoundedCornerShape(10.dp),
                                        ) {
                                            Text(strings.pairScanCta)
                                        }
                                    }
                                }
                            }
                        } else {
                            item(key = "offline") {
                                InboxOfflineCard(
                                    reconnecting = state.connection == ConnectionState.RECONNECTING,
                                    reason = state.connectionDiagnostics.lastDisconnectReason,
                                    lastConnected = state.connectionDiagnostics.lastConnectedAt,
                                    lastDisconnected = state.connectionDiagnostics.lastDisconnectedAt,
                                    onRetry = viewModel::reconnect,
                                )
                            }
                        }
                    }
                    if (!mobileInbox.enabled) {
                        item(key = "mobile-inbox-mode") {
                            MobileInboxModeCard(
                                mode = mobileInbox,
                                onToggle = viewModel::setInboxMode,
                            )
                        }
                    }
                    if (running.isNotEmpty()) {
                        item(key = "running-strip") {
                            RunningStrip(
                                sessions = running,
                                now = now,
                                onOpenSession = onOpenSession,
                            )
                        }
                    }
                    item(key = "crash-card") {
                        LastCrashCard()
                    }
                    if (attentionCount == 0) {
                        item(key = "all-clear") {
                            InboxAllClearState(
                                runningCount = allRunningCount,
                                inboxEnabled = mobileInbox.enabled,
                            )
                        }
                    }
                    if (permissions.isNotEmpty()) {
                        item(key = "priority-label") {
                            SectionLabel("等你处理", attentionCount)
                        }
                        items(permissions, key = { "permission-${it.sessionId}" }) { session ->
                            PermissionPriorityCard(
                                session = session,
                                onOpen = { onOpenSession(session.sessionId) },
                                onAllow = { viewModel.approve(session) },
                                onDeny = { viewModel.deny(session) },
                            )
                        }
                    }
                    if (waiting.isNotEmpty()) {
                        if (permissions.isEmpty()) {
                            item(key = "priority-label") {
                                SectionLabel("等你回复", waiting.size)
                            }
                        }
                        items(waiting, key = { "waiting-${it.sessionId}" }) { session ->
                            WaitingInputCard(
                                session = session,
                                draft = waitingDrafts[session.sessionId].orEmpty(),
                                onDraftChange = { value ->
                                    waitingDrafts = waitingDrafts + (session.sessionId to value)
                                },
                                onSpeech = { requestSpeech(session.sessionId) },
                                onSend = {
                                    val text = waitingDrafts[session.sessionId].orEmpty().trim()
                                    if (text.isNotBlank()) {
                                        viewModel.sendReply(
                                            session = session,
                                            text = text,
                                            eventId = session.pendingEvent?.id
                                                ?: session.latestEvent?.id
                                                ?: 0L,
                                        )
                                        waitingDrafts = waitingDrafts - session.sessionId
                                    }
                                },
                                onOpen = { onOpenSession(session.sessionId) },
                            )
                        }
                    }
                    if (errors.isNotEmpty()) {
                        if (permissions.isEmpty() && waiting.isEmpty()) {
                            item(key = "priority-label") {
                                SectionLabel("需要重试", errors.size)
                            }
                        }
                        items(errors, key = { "error-${it.sessionId}" }) { session ->
                            ErrorPriorityCard(
                                session = session,
                                onOpen = { onOpenSession(session.sessionId) },
                                onRetry = {
                                    val failed = session.replies.lastOrNull {
                                        it.status == dev.agentpush.poc.ReplyStatus.FAILED
                                    }
                                    if (failed != null) {
                                        viewModel.retryReply(failed)
                                    } else {
                                        onOpenSession(session.sessionId)
                                    }
                                },
                            )
                        }
                    }
                    if (completed.isNotEmpty()) {
                        item(key = "completed-header") {
                            CompletedHeader(
                                showAll = attentionCount == 0,
                                onMarkAllRead = viewModel::markAllRead,
                                onOpenAll = onOpenAllSessions,
                            )
                        }
                        item(key = "completed-list") {
                            CompletedCard(
                                sessions = completed,
                                state = state,
                                onOpenSession = onOpenSession,
                            )
                        }
                    }
                }
            }
        }
        ExtendedFloatingActionButton(
            onClick = onNewSession,
            modifier = Modifier
                .align(Alignment.BottomEnd)
                .padding(end = 18.dp, bottom = 18.dp)
                .height(56.dp)
                .testTag("new-session-fab"),
            shape = RoundedCornerShape(16.dp),
            containerColor = HomingPalette.Brand,
            contentColor = HomingPalette.Background,
            icon = {
                Icon(
                    Icons.Outlined.Add,
                    contentDescription = null,
                    modifier = Modifier.size(21.dp),
                )
            },
            text = {
                Text(
                    text = "新任务",
                    style = MaterialTheme.typography.labelLarge,
                )
            },
        )
    }
}

@Composable
internal fun InboxTopBar(
    connection: ConnectionState,
    transportKind: TransportKind,
    pigeonState: PigeonState,
    animationKey: Int,
    searchOpen: Boolean,
    onToggleSearch: () -> Unit,
    inboxModeEnabled: Boolean? = null,
    onToggleInboxMode: (() -> Unit)? = null,
    waitingCount: Int = 0,
    showStatusPill: Boolean = true,
) {
    // 顶栏鸽子按 pigeon_v2 场景切换：
    // 离线打盹（sleep 循环）→ 重连时唤醒（wake）→ 有事等你衔信张望（wait 循环）；
    // 来新动态播一次收信（recv），点品牌标或下拉刷新播一次轻点（tap）。
    var oneShot by remember { mutableStateOf<PigeonScene?>(null) }
    var oneShotKey by remember { mutableIntStateOf(0) }
    var seenAnimationKey by remember { mutableIntStateOf(animationKey) }
    LaunchedEffect(animationKey) {
        if (animationKey != seenAnimationKey) {
            seenAnimationKey = animationKey
            if (pigeonState == PigeonState.BobOnce || pigeonState == PigeonState.Idle) {
                oneShot = PigeonScene.RECV
                oneShotKey += 1
            }
        }
    }
    LaunchedEffect(pigeonState) {
        if (pigeonState == PigeonState.Flap) {
            oneShot = PigeonScene.TAP
            oneShotKey += 1
        }
    }
    var wasOffline by remember { mutableStateOf(false) }
    LaunchedEffect(connection) {
        if (connection == ConnectionState.CONNECTED && wasOffline) {
            oneShot = PigeonScene.WAKE
            oneShotKey += 1
        }
        wasOffline = connection != ConnectionState.CONNECTED
    }
    val loopScene = when {
        connection == ConnectionState.DISCONNECTED -> PigeonScene.SLEEP
        connection == ConnectionState.RECONNECTING -> PigeonScene.WAKE
        waitingCount > 0 -> PigeonScene.WAIT
        else -> null
    }
    val scene = oneShot ?: loopScene
    val neck = when {
        connection == ConnectionState.DISCONNECTED -> PigeonNeck.OFFLINE
        waitingCount > 0 -> PigeonNeck.WAITING
        else -> PigeonNeck.NORMAL
    }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 56.dp)
            .padding(start = 16.dp, end = 6.dp, top = 6.dp, bottom = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Row(
            modifier = Modifier
                .clip(RoundedCornerShape(12.dp))
                .clickable {
                    oneShot = PigeonScene.TAP
                    oneShotKey += 1
                }
                .padding(end = 6.dp)
                .testTag("inbox-brand"),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(9.dp),
        ) {
            HomingPigeonScene(
                scene = scene,
                size = 32.dp,
                loop = oneShot == null && loopScene != null,
                neck = neck,
                playKey = if (oneShot != null) oneShotKey else loopScene,
                contentDescription = "Homing",
                onFinished = { if (oneShot != null) oneShot = null },
            )
            // 品牌名永远单行
            Text(
                text = "Homing",
                style = MaterialTheme.typography.titleLarge.copy(
                    fontSize = 21.sp,
                    fontWeight = FontWeight.SemiBold,
                    letterSpacing = (-0.2).sp,
                ),
                color = HomingPalette.Text,
                maxLines = 1,
                softWrap = false,
                modifier = Modifier.testTag("inbox-brand-title"),
            )
        }
        // 连接胶囊独占品牌名和按钮之间的全部空间（以前和一个 Spacer 平分，被压成一半）
        Box(
            modifier = Modifier
                .weight(1f)
                .padding(horizontal = 6.dp),
            contentAlignment = Alignment.CenterEnd,
        ) {
            if (showStatusPill) {
                MacStatusPill(connection = connection, transportKind = transportKind)
            }
        }
        if (inboxModeEnabled != null && onToggleInboxMode != null) {
            IconButton(
                onClick = onToggleInboxMode,
                modifier = Modifier
                    .size(48.dp)
                    .testTag("toggle-inbox-mode"),
            ) {
                Icon(
                    imageVector = if (inboxModeEnabled) {
                        Icons.Outlined.NotificationsActive
                    } else {
                        Icons.Outlined.NotificationsOff
                    },
                    contentDescription = if (inboxModeEnabled) {
                        "关闭收件箱模式"
                    } else {
                        "开启收件箱模式"
                    },
                    tint = if (inboxModeEnabled) HomingPalette.Brand else HomingPalette.Muted,
                    modifier = Modifier.size(24.dp),
                )
            }
        }
        IconButton(
            onClick = onToggleSearch,
            modifier = Modifier.size(48.dp),
        ) {
            Icon(
                imageVector = if (searchOpen) Icons.Outlined.Close else Icons.Outlined.Search,
                contentDescription = if (searchOpen) "关闭搜索" else "搜索",
                tint = HomingPalette.Body,
                modifier = Modifier.size(24.dp),
            )
        }
    }
}

@Composable
internal fun InboxSearchField(
    value: String,
    onValueChange: (String) -> Unit,
) {
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp)
            .testTag("inbox-search"),
        placeholder = {
            Text(
                "搜索标题、项目、工作区或摘要",
                style = MaterialTheme.typography.bodyMedium,
            )
        },
        leadingIcon = {
            Icon(Icons.Outlined.Search, contentDescription = null, modifier = Modifier.size(20.dp))
        },
        singleLine = true,
        shape = RoundedCornerShape(12.dp),
    )
}

@Composable
private fun InboxHeadline(
    attentionCount: Int,
    runningCount: Int,
    inboxEnabled: Boolean,
) {
    Column(modifier = Modifier.padding(top = 4.dp, bottom = 2.dp)) {
        Text(
            text = when {
                !inboxEnabled -> "收件箱已清空"
                attentionCount == 0 -> "暂时没有新动态"
                else -> "$attentionCount 件事等你"
            },
            style = MaterialTheme.typography.displaySmall.copy(
                fontSize = 28.sp,
                lineHeight = 34.sp,
                fontWeight = FontWeight.SemiBold,
            ),
            color = HomingPalette.Text,
        )
        Text(
            text = when {
                !inboxEnabled -> "开启后只接收新回复和授权"
                runningCount > 0 -> "另有 $runningCount 个在运行"
                else -> "保持开启，新动态会自动出现"
            },
            modifier = Modifier.padding(top = 3.dp),
            style = MaterialTheme.typography.bodySmall.copy(fontSize = 13.sp),
            color = HomingPalette.Muted,
        )
    }
}

@Composable
private fun InboxOfflineCard(
    reconnecting: Boolean,
    reason: String?,
    lastConnected: Long?,
    lastDisconnected: Long?,
    onRetry: () -> Unit,
) {
    HomingCard(borderColor = HomingPalette.Warning.copy(alpha = 0.45f)) {
        Row(
            modifier = Modifier.padding(horizontal = 13.dp, vertical = 11.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (reconnecting) {
                CircularProgressIndicator(
                    modifier = Modifier.size(18.dp),
                    strokeWidth = 2.dp,
                    color = HomingPalette.Warning,
                )
            } else {
                Icon(
                    Icons.Outlined.ErrorOutline,
                    contentDescription = null,
                    tint = HomingPalette.Warning,
                    modifier = Modifier.size(20.dp),
                )
            }
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = if (reconnecting) "正在飞回 Mac…" else "暂时没有连上 Mac",
                    style = MaterialTheme.typography.labelLarge,
                    color = HomingPalette.Text,
                )
                Text(
                    text = buildString {
                        append("连接 ")
                        append(formatDiagnosticTime(lastConnected))
                        append(" · 断开 ")
                        append(formatDiagnosticTime(lastDisconnected))
                        reason?.takeIf { it.isNotBlank() }?.let {
                            append("\n")
                            append(it.take(90))
                        }
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = HomingPalette.Muted,
                    maxLines = 3,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            TextButton(onClick = onRetry) { Text("重试") }
        }
    }
}

@Composable
private fun RunningStrip(
    sessions: List<SessionSummary>,
    now: Long,
    onOpenSession: (String) -> Unit,
) {
    LazyRow(
        contentPadding = PaddingValues(horizontal = 1.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(sessions, key = { "running-${it.sessionId}" }) { session ->
            Surface(
                onClick = { onOpenSession(session.sessionId) },
                shape = RoundedCornerShape(50),
                color = HomingPalette.Brand.copy(alpha = 0.09f),
                border = BorderStroke(1.dp, HomingPalette.Brand.copy(alpha = 0.24f)),
            ) {
                Row(
                    modifier = Modifier
                        .height(40.dp)
                        .padding(horizontal = 13.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(7.dp),
                ) {
                    Box(
                        modifier = Modifier
                            .size(7.dp)
                            .clip(CircleShape)
                            .background(HomingPalette.Brand),
                    )
                    Text(
                        text = "${session.brand.displayName} · ${session.project}",
                        style = MaterialTheme.typography.labelMedium,
                        color = HomingPalette.Body,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        text = elapsedSince(session.runningSince, now).ifBlank { "运行中" },
                        style = MaterialTheme.typography.labelSmall.copy(
                            fontFamily = HomingMono,
                            fontSize = 11.sp,
                        ),
                        color = HomingPalette.Brand,
                        maxLines = 1,
                    )
                }
            }
        }
    }
}

@Composable
private fun SectionLabel(text: String, count: Int) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = text,
            style = MaterialTheme.typography.titleMedium.copy(fontSize = 16.sp),
            color = HomingPalette.Text,
        )
        Spacer(Modifier.width(7.dp))
        Surface(
            color = HomingPalette.Raised,
            shape = CircleShape,
        ) {
            Text(
                text = count.toString(),
                modifier = Modifier.padding(horizontal = 7.dp, vertical = 1.dp),
                style = MaterialTheme.typography.labelSmall,
                color = HomingPalette.Muted,
            )
        }
    }
}

@Composable
private fun PermissionPriorityCard(
    session: SessionSummary,
    onOpen: () -> Unit,
    onAllow: () -> Unit,
    onDeny: () -> Unit,
) {
    HomingCard(borderColor = HomingPalette.Warning.copy(alpha = 0.55f)) {
        Column(modifier = Modifier.padding(14.dp)) {
            Row(verticalAlignment = Alignment.Top) {
                Icon(
                    Icons.Outlined.Shield,
                    contentDescription = null,
                    tint = HomingPalette.Warning,
                    modifier = Modifier.size(22.dp),
                )
                Spacer(Modifier.width(9.dp))
                Column(modifier = Modifier.weight(1f)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            text = session.title,
                            modifier = Modifier
                                .weight(1f)
                                .testTag("permission-title"),
                            style = MaterialTheme.typography.titleMedium,
                            color = HomingPalette.Text,
                            maxLines = 2,
                            overflow = TextOverflow.Ellipsis,
                        )
                        PendingTag("需要授权")
                    }
                    Spacer(Modifier.height(3.dp))
                    MonoMetaLine(session)
                }
            }
            Spacer(Modifier.height(10.dp))
            CommandBlock(session.pendingEvent, session)
            Spacer(Modifier.height(11.dp))
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(9.dp),
            ) {
                OutlinedButton(
                    onClick = onDeny,
                    modifier = Modifier
                        .weight(1f)
                        .height(48.dp),
                    shape = RoundedCornerShape(10.dp),
                ) {
                    Text("拒绝")
                }
                Button(
                    onClick = onAllow,
                    modifier = Modifier
                        .weight(1f)
                        .height(48.dp),
                    shape = RoundedCornerShape(10.dp),
                    colors = ButtonDefaults.buttonColors(
                        containerColor = HomingPalette.Brand,
                        contentColor = HomingPalette.Background,
                    ),
                ) {
                    Text("允许")
                }
                TextButton(
                    onClick = onOpen,
                    modifier = Modifier.height(48.dp),
                ) {
                    Text("查看")
                }
            }
        }
    }
}

@Composable
private fun WaitingInputCard(
    session: SessionSummary,
    draft: String,
    onDraftChange: (String) -> Unit,
    onSpeech: () -> Unit,
    onSend: () -> Unit,
    onOpen: () -> Unit,
) {
    HomingCard(borderColor = HomingPalette.Warning.copy(alpha = 0.30f)) {
        Column(modifier = Modifier.padding(14.dp)) {
            Row(verticalAlignment = Alignment.Top) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = session.title,
                        style = MaterialTheme.typography.titleMedium,
                        color = HomingPalette.Text,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Spacer(Modifier.height(3.dp))
                    MonoMetaLine(session)
                }
                PendingTag("等你回复")
            }
            val pending = session.pendingEvent
            val question = pending?.text
                ?.ifBlank { pending.title }
                ?: session.latestReplyText
            if (question.isNotBlank()) {
                Text(
                    text = question.take(1_000),
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(top = 10.dp),
                    style = MaterialTheme.typography.bodyMedium.copy(
                        fontSize = 15.sp,
                        lineHeight = 23.sp,
                    ),
                    color = HomingPalette.Body,
                    maxLines = 6,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Spacer(Modifier.height(10.dp))
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(5.dp),
            ) {
                OutlinedTextField(
                    value = draft,
                    onValueChange = onDraftChange,
                    modifier = Modifier.weight(1f),
                    placeholder = { Text("直接回复…", style = MaterialTheme.typography.bodyMedium) },
                    maxLines = 3,
                    shape = RoundedCornerShape(12.dp),
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
                    keyboardActions = KeyboardActions(onSend = { onSend() }),
                )
                IconButton(onClick = onSpeech, modifier = Modifier.size(48.dp)) {
                    Icon(
                        Icons.Outlined.Mic,
                        contentDescription = "语音输入",
                        tint = HomingPalette.Body,
                    )
                }
                Surface(
                    onClick = onSend,
                    enabled = draft.isNotBlank(),
                    modifier = Modifier.size(48.dp),
                    shape = CircleShape,
                    color = if (draft.isNotBlank()) {
                        HomingPalette.Brand
                    } else {
                        HomingPalette.Raised
                    },
                    contentColor = if (draft.isNotBlank()) {
                        HomingPalette.Background
                    } else {
                        HomingPalette.Muted
                    },
                ) {
                    Box(contentAlignment = Alignment.Center) {
                        Icon(
                            Icons.AutoMirrored.Outlined.Send,
                            contentDescription = "发送",
                            modifier = Modifier.size(20.dp),
                        )
                    }
                }
            }
            TextButton(
                onClick = onOpen,
                contentPadding = PaddingValues(horizontal = 4.dp, vertical = 0.dp),
            ) {
                Text("打开完整会话", style = MaterialTheme.typography.labelMedium)
            }
        }
    }
}

@Composable
private fun ErrorPriorityCard(
    session: SessionSummary,
    onOpen: () -> Unit,
    onRetry: () -> Unit,
) {
    HomingCard(borderColor = HomingPalette.Error.copy(alpha = 0.48f)) {
        Column(modifier = Modifier.padding(14.dp)) {
            Row(verticalAlignment = Alignment.Top) {
                Icon(
                    Icons.Outlined.Close,
                    contentDescription = null,
                    tint = HomingPalette.Error,
                    modifier = Modifier.size(22.dp),
                )
                Spacer(Modifier.width(9.dp))
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = session.title,
                        style = MaterialTheme.typography.titleMedium,
                        color = HomingPalette.Text,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Spacer(Modifier.height(3.dp))
                    MonoMetaLine(session)
                    Text(
                        text = sessionSummaryLine(session, 300).ifBlank { "会话执行失败" },
                        modifier = Modifier.padding(top = 7.dp),
                        style = MaterialTheme.typography.bodyMedium,
                        color = HomingPalette.Error,
                        maxLines = 4,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = 8.dp),
                horizontalArrangement = Arrangement.End,
            ) {
                TextButton(onClick = onOpen) { Text("查看详情") }
                Spacer(Modifier.width(6.dp))
                Button(
                    onClick = onRetry,
                    modifier = Modifier.height(48.dp),
                    shape = RoundedCornerShape(10.dp),
                ) {
                    Text("重试")
                }
            }
        }
    }
}

@Composable
private fun CompletedHeader(
    showAll: Boolean,
    onMarkAllRead: () -> Unit,
    onOpenAll: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 5.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            Icons.Outlined.Check,
            contentDescription = null,
            tint = HomingPalette.Brand,
            modifier = Modifier.size(18.dp),
        )
        Spacer(Modifier.width(7.dp))
        Text(
            text = "刚完成",
            modifier = Modifier.weight(1f),
            style = MaterialTheme.typography.titleMedium.copy(fontSize = 16.sp),
            color = HomingPalette.Text,
        )
        TextButton(onClick = if (showAll) onOpenAll else onMarkAllRead) {
            Text(if (showAll) "全部会话" else "全部已读")
        }
    }
}

@Composable
private fun CompletedCard(
    sessions: List<SessionSummary>,
    state: dev.agentpush.poc.AgentUiState,
    onOpenSession: (String) -> Unit,
) {
    HomingCard {
        Column {
            sessions.forEachIndexed { index, session ->
                if (index > 0) {
                    Box(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 13.dp)
                            .height(1.dp)
                            .background(HomingPalette.NavBorder),
                    )
                }
                CompletedRow(
                    session = session,
                    changedFiles = changedFileCount(state, session.sessionId),
                    onClick = { onOpenSession(session.sessionId) },
                )
            }
        }
    }
}

@Composable
private fun CompletedRow(
    session: SessionSummary,
    changedFiles: Int,
    onClick: () -> Unit,
) {
    Surface(
        onClick = onClick,
        color = Color.Transparent,
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 13.dp, vertical = 11.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            AgentAvatar(
                brand = session.brand,
                source = session.source,
                size = 24.dp,
            )
            Spacer(Modifier.width(10.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = session.title,
                    style = MaterialTheme.typography.titleSmall.copy(fontSize = 14.sp),
                    color = HomingPalette.Text,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = buildString {
                        append(session.brand.displayName)
                        append(" · ")
                        append(session.project)
                        append(" · ")
                        append(if (changedFiles > 0) "改了 $changedFiles 个文件" else "已回复")
                    },
                    modifier = Modifier.padding(top = 2.dp),
                    style = MaterialTheme.typography.labelSmall.copy(
                        fontFamily = HomingMono,
                        fontSize = 11.sp,
                    ),
                    color = HomingPalette.Muted,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            if (session.unreadCount > 0) {
                Spacer(Modifier.width(8.dp))
                Box(
                    modifier = Modifier
                        .size(7.dp)
                        .clip(CircleShape)
                        .background(HomingPalette.Brand),
                )
                Spacer(Modifier.width(6.dp))
                UnreadBadge(session.unreadCount)
            } else {
                Text(
                    text = relativeTime(session.lastTs),
                    style = MaterialTheme.typography.labelSmall,
                    color = HomingPalette.Muted,
                    maxLines = 1,
                )
            }
        }
    }
}

@Composable
private fun InboxAllClearState(
    runningCount: Int,
    inboxEnabled: Boolean,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 22.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        HomingPigeon(
            state = PigeonState.Idle,
            size = 112.dp,
            showTile = false,
            contentDescription = "待机的鸽子",
        )
        Text(
            text = when {
                !inboxEnabled -> "还没有开启收件箱模式"
                runningCount > 0 -> "$runningCount 个会话仍在运行"
                else -> "没有等你处理的事"
            },
            modifier = Modifier.padding(top = 12.dp),
            style = MaterialTheme.typography.titleMedium,
            color = HomingPalette.Text,
        )
        Text(
            text = if (inboxEnabled) {
                "agent 需要你回复或授权时，会出现在这里，同时推送到通知栏。"
            } else {
                "开启后，只显示开启之后出现的新动态；常驻会话仍可在会话页查看。"
            },
            modifier = Modifier
                .padding(top = 5.dp)
                .padding(horizontal = 18.dp),
            style = MaterialTheme.typography.bodySmall.copy(lineHeight = 20.sp),
            color = HomingPalette.Muted,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
        )
    }
}

@Composable
private fun MonoMetaLine(session: SessionSummary) {
    Text(
        text = buildString {
            append(session.brand.displayName)
            append(" · ")
            append(session.project)
            session.branch.takeIf { it.isNotBlank() }?.let {
                append(" · ")
                append(it)
            }
        },
        style = MaterialTheme.typography.labelSmall.copy(
            fontFamily = HomingMono,
            fontSize = 11.sp,
            lineHeight = 16.sp,
        ),
        color = HomingPalette.Muted,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
    )
}

@Composable
private fun CommandBlock(event: AgentEvent?, session: SessionSummary) {
    val command = event?.text
        ?.ifBlank { event.title }
        ?.ifBlank { session.lastPreview }
        .orEmpty()
    if (command.isBlank()) return
    Surface(
        color = HomingPalette.NavBackground,
        shape = RoundedCornerShape(9.dp),
        border = BorderStroke(1.dp, HomingPalette.Border.copy(alpha = 0.45f)),
    ) {
        Text(
            text = command.take(1_200),
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(max = 150.dp)
                .horizontalScroll(rememberScrollState())
                .padding(horizontal = 11.dp, vertical = 9.dp),
            style = TextStyle(
                fontFamily = HomingMono,
                fontSize = 11.5.sp,
                lineHeight = 17.sp,
                color = HomingPalette.Body,
            ),
            maxLines = 8,
            softWrap = false,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

private fun SessionSummary.hasPermissionRequest(): Boolean {
    return status == SessionStatus.NEEDS_PERMISSION ||
        pendingEvent?.kind == "permission"
}

private fun SessionSummary.hasWaitingInput(): Boolean {
    return status == SessionStatus.WAITING_INPUT ||
        pendingEvent?.kind == "needs_input"
}

private fun changedFileCount(
    state: dev.agentpush.poc.AgentUiState,
    sessionId: String,
): Int {
    return state.sessionItems[sessionId]
        ?.items
        .orEmpty()
        .filter { it.type == SessionItemType.FILE_CHANGE }
        .flatMap { it.files }
        .map { it.path }
        .filter { it.isNotBlank() }
        .distinct()
        .size
}
