package dev.agentpush.poc.ui

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.PowerManager
import android.provider.Settings
import androidx.activity.compose.BackHandler
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.tween
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Bolt
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.CleaningServices
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Computer
import androidx.compose.material.icons.outlined.Groups
import androidx.compose.material.icons.outlined.History
import androidx.compose.material.icons.outlined.KeyboardArrowDown
import androidx.compose.material.icons.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.Language
import androidx.compose.material.icons.outlined.Menu
import androidx.compose.material.icons.outlined.PowerSettingsNew
import androidx.compose.material.icons.outlined.QrCodeScanner
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Shield
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
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
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.pointer.positionChange
import androidx.compose.ui.input.pointer.util.VelocityTracker
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.agentpush.poc.AgentBrand
import dev.agentpush.poc.AgentUiState
import dev.agentpush.poc.AgentViewModel
import dev.agentpush.poc.ConnectionState
import dev.agentpush.poc.SessionStatus
import dev.agentpush.poc.SessionSummary
import dev.agentpush.poc.TransportKind
import dev.agentpush.poc.ProjectSessionGroup
import dev.agentpush.poc.defaultAgentDisplayName
import dev.agentpush.poc.groupSessionsByProject
import dev.agentpush.poc.projectGroupKey
import dev.agentpush.poc.epochMillis
import dev.agentpush.poc.phoneActivity
import dev.agentpush.poc.protocol.PairedComputerStore
import dev.agentpush.poc.relativeTime
import dev.agentpush.poc.ui.screens.SessionScreen
import dev.agentpush.poc.ui.theme.HomingPalette
import kotlinx.coroutines.launch
import kotlin.math.abs
import kotlin.math.roundToInt

private const val WORKSPACE_PREFS = "homing_workspace_prefs"
private const val KEY_LAST_ACTIVE_SESSION = "last_active_session_id"

data class AgentCapsuleItem(
    val source: String,
    val label: String,
    val totalCount: Int,
    val urgentCount: Int,
    val processRunning: Boolean = false,
)

data class WakeableAgentCandidate(
    val id: String,
    val label: String,
    val activeOnline: Boolean,
    val processRunning: Boolean,
    val canWake: Boolean,
    val wakeMode: String,
    val connected: Boolean,
)

internal fun isSessionUrgent(session: SessionSummary): Boolean {
    if (session.archived || session.isEmpty) return false
    return session.status == SessionStatus.NEEDS_PERMISSION ||
        session.status == SessionStatus.WAITING_INPUT ||
        session.status == SessionStatus.ERROR ||
        session.pendingEvent != null
}

internal fun isAgentActiveInWorkspace(
    source: String,
    state: AgentUiState,
    rawSessions: List<SessionSummary>,
): Boolean {
    val clean = source.trim().lowercase()
    if (clean.isBlank()) return false
    val ws = state.mobileWorkspace
    if (clean in ws.wokenAgentIds) return true
    if (
        rawSessions.any { session ->
            session.source.equals(clean, ignoreCase = true) &&
                (
                    session.running ||
                        isSessionUrgent(session) ||
                        session.sessionId in ws.restoredSessionIds ||
                        session.sessionId in ws.liveSessionIds
                    )
        }
    ) {
        return true
    }
    if (!state.agentDirectory.available) {
        return rawSessions.any { it.source.equals(clean, ignoreCase = true) }
    }
    val profile = state.agentDirectory.agents[clean]
    return profile?.activeOnline == true || profile?.processRunning == true
}

internal fun isSessionVisibleInWorkspace(
    session: SessionSummary,
    state: AgentUiState,
    rawSessions: List<SessionSummary>,
): Boolean {
    if (session.archived || session.isEmpty || isSyntheticHelperSession(session)) return false
    val ws = state.mobileWorkspace
    val isLivePriority = session.running || isSessionUrgent(session)
    if (isLivePriority) return true

    val dismissed = ws.dismissedSessions[session.sessionId]
    if (dismissed != null && !session.phoneActivity().isNewerThan(dismissed)) {
        return false
    }

    if (session.sessionId in ws.restoredSessionIds || session.sessionId in ws.liveSessionIds) {
        return true
    }

    // 纯磁盘扫描的历史会话（没有任何 Homing 事件）绝不自动塞进活跃工作区，仅在「恢复历史」按需恢复。
    if (session.events.isEmpty()) {
        return false
    }

    if (!isAgentActiveInWorkspace(session.source, state, rawSessions)) {
        return false
    }

    val maxSessionEventId = session.events.maxOfOrNull { it.id } ?: 0L
    val awayPeriod = state.home?.away?.current

    if (ws.clearedAtEpochMs > 0L) {
        // 未开启离席监听时，电脑前已结束的对话不自动进入手机侧栏（仅显示运行中/待处理/手机主动会话）。
        if (!state.away) {
            return false
        }
        val newerByBaseline = ws.baselineReady &&
            (ws.baselineMaxEventId <= 0L || maxSessionEventId > ws.baselineMaxEventId)
        if (!newerByBaseline) {
            return false
        }
    }

    if (state.away && awayPeriod != null) {
        val afterId = awayPeriod.startedAfterEventId
        if (afterId != null && maxSessionEventId <= afterId) {
            return false
        }
        val awayStartMs = epochMillis(awayPeriod.startedAt)
        val sessionMs = epochMillis(session.lastTs)
        if (afterId == null && awayStartMs > 0L && sessionMs in 1 until awayStartMs) {
            return false
        }
    }

    return true
}

internal fun buildWakeableAgentCandidates(state: AgentUiState): List<WakeableAgentCandidate> {
    val map = linkedMapOf<String, WakeableAgentCandidate>()
    val woken = state.mobileWorkspace.wokenAgentIds

    state.agentDirectory.agents.values.forEach { profile ->
        val id = profile.source.trim().lowercase()
        if (id.isBlank()) return@forEach
        val active = profile.activeOnline || profile.processRunning || id in woken
        val canWake = profile.canWake || profile.installed || profile.available
        if (!canWake && !active) return@forEach
        map[id] = WakeableAgentCandidate(
            id = id,
            label = profile.label,
            activeOnline = active,
            processRunning = profile.processRunning,
            canWake = canWake,
            wakeMode = profile.wakeMode,
            connected = true,
        )
    }

    state.agentCatalog.agents.forEach { entry ->
        val id = entry.id.trim().lowercase()
        if (id.isBlank()) return@forEach
        val existing = map[id]
        if (!entry.connected && !entry.detected && existing == null) return@forEach
        val active = existing?.activeOnline == true || id in woken
        val canWake = existing?.canWake == true || entry.detected || entry.available || entry.connected
        if (!canWake && !active) return@forEach
        map[id] = WakeableAgentCandidate(
            id = id,
            label = existing?.label?.takeIf { it.isNotBlank() } ?: entry.displayName,
            activeOnline = active,
            processRunning = existing?.processRunning == true,
            canWake = canWake,
            wakeMode = existing?.wakeMode.orEmpty(),
            connected = entry.connected || existing?.connected == true,
        )
    }

    return map.values.sortedWith(
        compareByDescending<WakeableAgentCandidate> { it.activeOnline }
            .thenByDescending { it.connected }
            .thenBy { it.label.lowercase() },
    )
}

@Composable
fun WorkspaceDrawerShell(
    viewModel: AgentViewModel,
    state: AgentUiState,
    hasConfiguredTarget: Boolean,
    snackbarHostState: SnackbarHostState,
    requestedSessionId: String?,
    onRequestedSessionConsumed: () -> Unit,
    onNewSession: (cwd: String) -> Unit,
    onAddComputer: () -> Unit,
    onOpenRooms: () -> Unit,
    onOpenMe: () -> Unit,
    onOpenSettings: () -> Unit,
    onOpenBrowser: (url: String, title: String) -> Unit,
) {
    val context = LocalContext.current
    val strings = LocalHomingStrings.current
    val prefs = remember(context) {
        context.applicationContext.getSharedPreferences(WORKSPACE_PREFS, Context.MODE_PRIVATE)
    }

    val rawSessions = remember(
        state.events,
        state.replies,
        state.sessionMeta,
        state.sessionItems,
        state.localSessionFlags,
        state.agentDirectory,
    ) {
        state.sessions().filter { !it.archived && !it.isEmpty && !isSyntheticHelperSession(it) }
    }

    val visibleSessions = remember(
        rawSessions,
        state.mobileWorkspace,
        state.agentDirectory,
        state.away,
        state.home,
    ) {
        rawSessions.filter { session ->
            isSessionVisibleInWorkspace(session, state, rawSessions)
        }
    }

    val urgentSessions = remember(visibleSessions) {
        visibleSessions.filter(::isSessionUrgent)
    }

    val wakeCandidates = remember(
        state.agentDirectory,
        state.agentCatalog,
        state.mobileWorkspace.wokenAgentIds,
    ) {
        buildWakeableAgentCandidates(state)
    }

    var activeSessionId by rememberSaveable {
        mutableStateOf(
            requestedSessionId?.takeIf { it.isNotBlank() }
                ?: if (state.mobileWorkspace.clearedAtEpochMs > 0L) {
                    ""
                } else {
                    prefs.getString(KEY_LAST_ACTIVE_SESSION, null).orEmpty()
                },
        )
    }
    var returnFromUrgentSessionId by rememberSaveable { mutableStateOf("") }
    var selectedAgentSource by rememberSaveable { mutableStateOf("") }
    var searchQuery by rememberSaveable { mutableStateOf("") }
    var autoPeekChecked by rememberSaveable { mutableStateOf(false) }
    var wakeSheetOpen by rememberSaveable { mutableStateOf(false) }
    var restoreSheetOpen by rememberSaveable { mutableStateOf(false) }

    // Honor explicit deep links (notification tap, newly created session, restored session, room jump)
    LaunchedEffect(requestedSessionId) {
        val target = requestedSessionId?.trim().orEmpty()
        if (target.isNotBlank()) {
            activeSessionId = target
            returnFromUrgentSessionId = ""
            prefs.edit().putString(KEY_LAST_ACTIVE_SESSION, target).apply()
            onRequestedSessionConsumed()
        }
    }

    // Clear activeSessionId if it was dismissed or cleaned out of the mobile workspace
    val resolvedSessionId = remember(activeSessionId, visibleSessions, urgentSessions) {
        val explicit = activeSessionId.takeIf { id ->
            id.isNotBlank() && visibleSessions.any { it.sessionId == id }
        }
        explicit
            ?: urgentSessions.firstOrNull()?.sessionId
            ?: visibleSessions.firstOrNull()?.sessionId
            ?: ""
    }

    LaunchedEffect(resolvedSessionId) {
        if (resolvedSessionId != activeSessionId) {
            activeSessionId = resolvedSessionId
            if (resolvedSessionId.isNotBlank()) {
                prefs.edit().putString(KEY_LAST_ACTIVE_SESSION, resolvedSessionId).apply()
            } else {
                prefs.edit().remove(KEY_LAST_ACTIVE_SESSION).apply()
            }
        }
    }

    // Unpaired onboarding: if no computer is paired and no cached sessions exist, show clean setup screen
    if (!hasConfiguredTarget && rawSessions.isEmpty()) {
        UnpairedWorkspaceWelcome(
            onScanPair = onAddComputer,
            onManualSetup = onOpenSettings,
        )
        return
    }
    val needsRelayAuth = state.connection != ConnectionState.CONNECTED &&
        isRelayAuthReason(state.connectionDiagnostics.lastDisconnectReason)

    BoxWithConstraints(
        modifier = Modifier
            .fillMaxSize()
            .background(HomingPalette.Background)
            .testTag("workspace-drawer-shell"),
    ) {
        val density = LocalDensity.current
        val containerWidthPx = with(density) { maxWidth.toPx() }
        val drawerWidthDp = remember(maxWidth) {
            minOf(maxWidth * 0.85f, 326.dp)
        }
        val drawerWidthPx = with(density) { drawerWidthDp.toPx().coerceAtLeast(1f) }
        val edgeExclusionPx = with(density) { 24.dp.toPx() }
        val scope = rememberCoroutineScope()

        val drawerOffsetPx = remember { Animatable(-drawerWidthPx) }
        var drawerOpen by rememberSaveable { mutableStateOf(false) }

        fun openDrawer() {
            drawerOpen = true
            scope.launch {
                drawerOffsetPx.animateTo(0f, tween(durationMillis = 210))
            }
        }

        fun closeDrawer() {
            drawerOpen = false
            scope.launch {
                drawerOffsetPx.animateTo(-drawerWidthPx, tween(durationMillis = 190))
            }
        }

        fun showCleanupUndoSnackbar(message: String, previousActiveId: String) {
            scope.launch {
                val res = snackbarHostState.showSnackbar(
                    message = message,
                    actionLabel = "撤销",
                    duration = SnackbarDuration.Short,
                )
                if (res == SnackbarResult.ActionPerformed) {
                    if (viewModel.undoWorkspaceCleanup() && previousActiveId.isNotBlank()) {
                        activeSessionId = previousActiveId
                    }
                }
            }
        }

        fun triggerQuickClearAll() {
            val prevActive = resolvedSessionId
            val cleared = viewModel.quickClearWorkspaceSessions(visibleSessions)
            if (cleared > 0) {
                val keptCount = visibleSessions.count { it.running || isSessionUrgent(it) }
                val suffix = if (keptCount > 0) "（保留 $keptCount 个运行/待处理任务）" else ""
                showCleanupUndoSnackbar("已快速清理 $cleared 个会话$suffix", prevActive)
            } else {
                scope.launch {
                    snackbarHostState.showSnackbar(
                        if (visibleSessions.isEmpty()) "当前工作区已是清空状态" else "运行中或待处理任务不会被清理",
                    )
                }
            }
        }

        fun triggerHeaderQuickClear() {
            val current = visibleSessions.firstOrNull { it.sessionId == resolvedSessionId } ?: return
            val prevActive = resolvedSessionId
            if (viewModel.dismissWorkspaceSession(current)) {
                showCleanupUndoSnackbar("已从移动端清理「${current.title}」", prevActive)
            } else {
                val othersCleared = viewModel.quickClearWorkspaceSessions(visibleSessions)
                if (othersCleared > 0) {
                    showCleanupUndoSnackbar("当前任务正在运行，已清理其余 $othersCleared 个会话", prevActive)
                } else {
                    scope.launch {
                        snackbarHostState.showSnackbar("当前会话正在运行或等待你处理，暂不清理")
                    }
                }
            }
        }

        // Sync initial / resized closed offset
        LaunchedEffect(drawerWidthPx, drawerOpen) {
            val target = if (drawerOpen) 0f else -drawerWidthPx
            if (!drawerOffsetPx.isRunning) {
                drawerOffsetPx.snapTo(target)
            }
        }

        // Red-Team Fix #1: When >= 2 agents simultaneously need approval on cold launch,
        // automatically open the drawer so parallel emergencies are immediately visible.
        LaunchedEffect(urgentSessions.size, requestedSessionId) {
            if (!autoPeekChecked && visibleSessions.isNotEmpty()) {
                autoPeekChecked = true
                if (requestedSessionId.isNullOrBlank() && urgentSessions.size >= 2) {
                    openDrawer()
                }
            }
        }

        BackHandler(enabled = drawerOpen) {
            closeDrawer()
        }

        val otherUrgent = remember(urgentSessions, resolvedSessionId) {
            urgentSessions.filter { it.sessionId != resolvedSessionId }
        }
        val returnSessionTitle = remember(returnFromUrgentSessionId, visibleSessions) {
            returnFromUrgentSessionId.takeIf { it.isNotBlank() && it != resolvedSessionId }
                ?.let { id -> visibleSessions.firstOrNull { it.sessionId == id }?.title }
        }

        fun selectSession(sessionId: String, computerId: String = "", clearReturn: Boolean = true) {
            if (computerId.isNotBlank()) {
                viewModel.activateComputer(computerId)
            }
            activeSessionId = sessionId
            if (clearReturn) {
                returnFromUrgentSessionId = ""
            }
            prefs.edit().putString(KEY_LAST_ACTIVE_SESSION, sessionId).apply()
            closeDrawer()
        }

        val currentSession = remember(visibleSessions, resolvedSessionId) {
            visibleSessions.firstOrNull { it.sessionId == resolvedSessionId }
                ?: rawSessions.firstOrNull { it.sessionId == resolvedSessionId }
        }
        val visibleIds = remember(visibleSessions) { visibleSessions.map { it.sessionId }.toSet() }
        // One project = one drawer entry. The conversation header offers every
        // session of that project, including recent history that is not on the
        // phone yet (restored with its latest 10 records on tap).
        val projectSiblings = remember(currentSession, visibleSessions, rawSessions) {
            val current = currentSession ?: return@remember emptyList()
            val key = current.projectGroupKey()
            val visibleInProject = visibleSessions.filter {
                it.projectGroupKey() == key && !isSyntheticHelperSession(it)
            }
            val history = rawSessions
                .filter {
                    it.projectGroupKey() == key &&
                        it.sessionId !in visibleIds &&
                        !isSyntheticHelperSession(it)
                }
                .sortedByDescending { it.lastTs }
                .take(6)
            (visibleInProject.sortedByDescending { it.lastTs } + history).distinctBy { it.sessionId }
        }
        val projectHistoryIds = remember(projectSiblings, visibleIds) {
            projectSiblings.map { it.sessionId }.filterNot { it in visibleIds }.toSet()
        }

        fun openProjectSession(sessionId: String) {
            if (sessionId in projectHistoryIds) {
                viewModel.restoreHistoricalSessions(listOf(sessionId), itemLimit = 10) { restored ->
                    if (!restored.isNullOrBlank()) selectSession(restored)
                }
            } else {
                selectSession(sessionId)
            }
        }

        // Main Content Layer with Non-Edge, Scroll-Safe Horizontal Drag
        Box(
            modifier = Modifier
                .fillMaxSize()
                .pointerInput(drawerWidthPx, drawerOpen) {
                    val touchSlop = viewConfiguration.touchSlop
                    awaitEachGesture {
                        val down = awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Main)
                        // Red-Team Fix #2a: Leave leftmost/rightmost 24dp to ColorOS/Android System Back
                        if (!drawerOpen && (down.position.x < edgeExclusionPx || down.position.x > containerWidthPx - edgeExclusionPx)) {
                            return@awaitEachGesture
                        }
                        val velocityTracker = VelocityTracker()
                        velocityTracker.addPosition(down.uptimeMillis, down.position)
                        var totalDx = 0f
                        var totalDy = 0f
                        var dragging = false

                        while (true) {
                            val event = awaitPointerEvent(PointerEventPass.Main)
                            val change = event.changes.firstOrNull { it.id == down.id } ?: break
                            if (!change.pressed) {
                                velocityTracker.addPosition(change.uptimeMillis, change.position)
                                break
                            }
                            // Red-Team Fix #2b: If an inner horizontalScroll (CodeBlock, Table, chip strip)
                            // already consumed horizontal movement, yield 100% to the child!
                            if (!dragging && change.isConsumed) {
                                return@awaitEachGesture
                            }
                            val delta = change.positionChange()
                            totalDx += delta.x
                            totalDy += delta.y
                            velocityTracker.addPosition(change.uptimeMillis, change.position)

                            if (!dragging) {
                                if (abs(totalDy) > touchSlop && abs(totalDy) >= abs(totalDx)) {
                                    return@awaitEachGesture
                                }
                                if (abs(totalDx) > touchSlop && abs(totalDx) > abs(totalDy) * 1.3f) {
                                    val validOpenSwipe = !drawerOpen && totalDx > 0f
                                    val validCloseSwipe = drawerOpen && totalDx < 0f
                                    if (!validOpenSwipe && !validCloseSwipe) {
                                        return@awaitEachGesture
                                    }
                                    dragging = true
                                }
                            }

                            if (dragging) {
                                change.consume()
                                val nextOffset = (drawerOffsetPx.value + delta.x)
                                    .coerceIn(-drawerWidthPx, 0f)
                                scope.launch {
                                    drawerOffsetPx.snapTo(nextOffset)
                                }
                            }
                        }

                        if (dragging) {
                            val vx = velocityTracker.calculateVelocity().x
                            val shouldOpen = when {
                                vx > 420f -> true
                                vx < -420f -> false
                                else -> drawerOffsetPx.value > -drawerWidthPx * 0.62f
                            }
                            if (shouldOpen) openDrawer() else closeDrawer()
                        }
                    }
                },
        ) {
            if (resolvedSessionId.isNotBlank()) {
                androidx.compose.runtime.key(resolvedSessionId) {
                    SessionScreen(
                        sessionId = resolvedSessionId,
                        viewModel = viewModel,
                        snackbarHostState = snackbarHostState,
                        onBack = ::openDrawer,
                        onOpenBrowser = onOpenBrowser,
                        onOpenSession = { targetId -> openProjectSession(targetId) },
                        projectSiblings = projectSiblings,
                        projectHistoryIds = projectHistoryIds,
                        navigationAsDrawer = true,
                        otherUrgentCount = otherUrgent.size,
                        onJumpUrgent = {
                            if (otherUrgent.size == 1) {
                                val target = otherUrgent.first()
                                returnFromUrgentSessionId = resolvedSessionId
                                selectSession(target.sessionId, target.computerId, clearReturn = false)
                            } else {
                                openDrawer()
                            }
                        },
                        returnToSessionTitle = returnSessionTitle,
                        onReturnPreviousSession = {
                            val backId = returnFromUrgentSessionId
                            returnFromUrgentSessionId = ""
                            if (backId.isNotBlank()) {
                                selectSession(backId)
                            }
                        },
                        onQuickClear = ::triggerHeaderQuickClear,
                    )
                }
                if (needsRelayAuth) {
                    RelayAuthActionBanner(
                        reason = state.connectionDiagnostics.lastDisconnectReason,
                        onScanPair = onAddComputer,
                        onRetry = viewModel::reconnect,
                        modifier = Modifier
                            .align(Alignment.TopCenter)
                            .statusBarsPadding()
                            .padding(top = 56.dp, start = 12.dp, end = 12.dp),
                    )
                }
            } else {
                EmptyWorkspaceConversation(
                    state = state,
                    historicalCount = remember(rawSessions) { groupSessionsByProject(rawSessions).size },
                    inactiveWakeableCount = wakeCandidates.count { !it.activeOnline && it.canWake },
                    needsRelayAuth = needsRelayAuth,
                    onOpenDrawer = ::openDrawer,
                    onNewSession = { onNewSession("") },
                    onToggleAway = { viewModel.toggleAwayMonitoring(!state.away) },
                    onOpenWakeSheet = { wakeSheetOpen = true },
                    onOpenRestoreSheet = { restoreSheetOpen = true },
                    onScanPair = onAddComputer,
                    onRetry = viewModel::reconnect,
                )
            }
        }

        // Dimmed Scrim
        val openFraction = ((drawerOffsetPx.value + drawerWidthPx) / drawerWidthPx).coerceIn(0f, 1f)
        if (openFraction > 0.01f) {
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .background(Color.Black.copy(alpha = 0.56f * openFraction))
                    .clickable(
                        interactionSource = remember { MutableInteractionSource() },
                        indication = null,
                        onClick = ::closeDrawer,
                    )
                    .testTag("workspace-drawer-scrim"),
            )
        }

        // Left Drawer Sheet
        if (openFraction > 0.005f || drawerOpen) {
            Surface(
                modifier = Modifier
                    .width(drawerWidthDp)
                    .fillMaxHeight()
                    .offset { IntOffset(drawerOffsetPx.value.roundToInt(), 0) }
                    .testTag("workspace-drawer"),
                color = HomingPalette.NavBackground,
                tonalElevation = 6.dp,
                shadowElevation = 12.dp,
                border = BorderStroke(1.dp, HomingPalette.NavBorder),
            ) {
                WorkspaceDrawerContent(
                    viewModel = viewModel,
                    state = state,
                    rawSessions = rawSessions,
                    visibleSessions = visibleSessions,
                    urgentSessions = urgentSessions,
                    wakeCandidates = wakeCandidates,
                    activeSessionId = resolvedSessionId,
                    selectedAgentSource = selectedAgentSource,
                    onSelectAgentSource = { selectedAgentSource = it },
                    searchQuery = searchQuery,
                    onSearchQueryChange = { searchQuery = it },
                    onSelectSession = { session ->
                        selectSession(session.sessionId, session.computerId)
                    },
                    onNewSession = {
                        val activeCwd = visibleSessions.firstOrNull { it.sessionId == resolvedSessionId }?.cwd.orEmpty()
                        closeDrawer()
                        onNewSession(activeCwd)
                    },
                    onQuickClear = ::triggerQuickClearAll,
                    onOpenWakeSheet = { wakeSheetOpen = true },
                    onOpenRestoreSheet = { restoreSheetOpen = true },
                    onOpenRooms = {
                        closeDrawer()
                        onOpenRooms()
                    },
                    onOpenMe = {
                        closeDrawer()
                        onOpenMe()
                    },
                    onAddComputer = {
                        closeDrawer()
                        onAddComputer()
                    },
                    onOpenSettings = {
                        closeDrawer()
                        onOpenSettings()
                    },
                    onOpenBrowserHome = {
                        closeDrawer()
                        onOpenBrowser("about:blank", "浏览器")
                    },
                    strings = strings,
                )
            }
        }

        if (wakeSheetOpen) {
            WakeAgentsBottomSheet(
                candidates = wakeCandidates,
                onWakeAgent = { id, onDone ->
                    viewModel.wakeAgent(id) {
                        onDone()
                    }
                },
                onDismiss = { wakeSheetOpen = false },
            )
        }

        if (restoreSheetOpen) {
            RestoreHistoryBottomSheet(
                candidates = rawSessions,
                alreadyVisibleIds = visibleSessions.map { it.sessionId }.toSet(),
                onRestore = { ids ->
                    restoreSheetOpen = false
                    closeDrawer()
                    viewModel.restoreHistoricalSessions(ids, itemLimit = 10) { firstId ->
                        if (!firstId.isNullOrBlank()) {
                            activeSessionId = firstId
                            prefs.edit().putString(KEY_LAST_ACTIVE_SESSION, firstId).apply()
                        }
                    }
                },
                onDismiss = { restoreSheetOpen = false },
            )
        }
    }
}

@Composable
private fun WorkspaceDrawerContent(
    viewModel: AgentViewModel,
    state: AgentUiState,
    rawSessions: List<SessionSummary>,
    visibleSessions: List<SessionSummary>,
    urgentSessions: List<SessionSummary>,
    wakeCandidates: List<WakeableAgentCandidate>,
    activeSessionId: String,
    selectedAgentSource: String,
    onSelectAgentSource: (String) -> Unit,
    searchQuery: String,
    onSearchQueryChange: (String) -> Unit,
    onSelectSession: (SessionSummary) -> Unit,
    onNewSession: () -> Unit,
    onQuickClear: () -> Unit,
    onOpenWakeSheet: () -> Unit,
    onOpenRestoreSheet: () -> Unit,
    onOpenRooms: () -> Unit,
    onOpenMe: () -> Unit,
    onAddComputer: () -> Unit,
    onOpenSettings: () -> Unit,
    onOpenBrowserHome: () -> Unit,
    strings: HomingStrings,
) {
    val context = LocalContext.current
    val computerStore = remember(context) { PairedComputerStore(context) }
    val computers = remember(state.lastUpdatedAt, state.activeComputerKey) {
        computerStore.load()
    }
    val activeComputerName = remember(computers, state.activeComputerKey) {
        computers.firstOrNull { it.deviceId == state.activeComputerKey }?.name
            ?.takeIf { it.isNotBlank() }
            ?: computers.firstOrNull()?.name?.takeIf { it.isNotBlank() }
            ?: "我的电脑"
    }

    val batteryOptimized = remember(context, state.lastUpdatedAt) {
        runCatching {
            val pm = context.getSystemService(PowerManager::class.java)
            pm != null && !pm.isIgnoringBatteryOptimizations(context.packageName)
        }.getOrDefault(false)
    }

    // Only display capsules for agents currently active/open on this Mac!
    val capsules = remember(visibleSessions, rawSessions, state.agentDirectory, state.mobileWorkspace) {
        val grouped = visibleSessions.groupBy { it.source.lowercase() }
        val sources = (grouped.keys + state.agentDirectory.agents.keys.map { it.lowercase() } + state.mobileWorkspace.wokenAgentIds)
            .filter { it.isNotBlank() }
            .distinct()
        sources.mapNotNull { src ->
            if (!isAgentActiveInWorkspace(src, state, rawSessions)) {
                return@mapNotNull null
            }
            val list = grouped[src].orEmpty()
            val profile = state.agentDirectory.profile(src)
            val label = list.firstOrNull()?.agentLabel?.takeIf { it.isNotBlank() }
                ?: profile.label.takeIf { it.isNotBlank() }
                ?: defaultAgentDisplayName(src)
            val urgent = list.count(::isSessionUrgent)
            AgentCapsuleItem(
                source = src,
                label = label,
                totalCount = list.size,
                urgentCount = urgent,
                processRunning = profile.processRunning || profile.activeOnline,
            )
        }.sortedWith(
            compareByDescending<AgentCapsuleItem> { it.urgentCount }
                .thenByDescending { it.totalCount }
                .thenByDescending { it.processRunning }
                .thenBy { it.label },
        )
    }

    val clearableCount = remember(visibleSessions) {
        visibleSessions.count { !it.running && !isSessionUrgent(it) }
    }
    val inactiveWakeableCount = remember(wakeCandidates) {
        wakeCandidates.count { !it.activeOnline && it.canWake }
    }
    val restorableCount = remember(rawSessions, visibleSessions) {
        val visibleIds = visibleSessions.map { it.sessionId }.toSet()
        groupSessionsByProject(rawSessions.filter { it.sessionId !in visibleIds }).size
    }

    val cleanQuery = searchQuery.trim().lowercase()
    val filteredSessions = remember(visibleSessions, selectedAgentSource, cleanQuery) {
        visibleSessions.filter { session ->
            val agentMatch = selectedAgentSource.isBlank() ||
                session.source.equals(selectedAgentSource, ignoreCase = true)
            val queryMatch = cleanQuery.isBlank() ||
                session.title.lowercase().contains(cleanQuery) ||
                session.project.lowercase().contains(cleanQuery) ||
                session.lastPreview.lowercase().contains(cleanQuery) ||
                session.agentLabel.lowercase().contains(cleanQuery)
            agentMatch && queryMatch
        }
    }
    val filteredUrgent = remember(filteredSessions) {
        filteredSessions.filter(::isSessionUrgent)
    }
    val regularSessions = remember(filteredSessions, filteredUrgent) {
        val urgentIds = filteredUrgent.map { it.sessionId }.toSet()
        filteredSessions.filterNot { it.sessionId in urgentIds }
    }
    val projectGroups = remember(regularSessions) { groupSessionsByProject(regularSessions) }
    val allProjectCount = remember(visibleSessions) { groupSessionsByProject(visibleSessions).size }
    val activeProjectKey = remember(visibleSessions, activeSessionId) {
        visibleSessions.firstOrNull { it.sessionId == activeSessionId }?.projectGroupKey().orEmpty()
    }
    var expandedProjects by rememberSaveable { mutableStateOf(listOf<String>()) }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .statusBarsPadding()
            .navigationBarsPadding(),
    ) {
        // 1. Search + New Session Row
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(start = 12.dp, end = 12.dp, top = 10.dp, bottom = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Surface(
                modifier = Modifier.weight(1f),
                color = HomingPalette.Card,
                shape = RoundedCornerShape(10.dp),
                border = BorderStroke(1.dp, HomingPalette.NavBorder),
            ) {
                Row(
                    modifier = Modifier.padding(horizontal = 10.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    Icon(
                        Icons.Outlined.Search,
                        contentDescription = null,
                        modifier = Modifier.size(16.dp),
                        tint = HomingPalette.Muted,
                    )
                    Box(modifier = Modifier.weight(1f)) {
                        if (searchQuery.isEmpty()) {
                            Text(
                                text = "搜索活跃会话…",
                                style = TextStyle(fontSize = 13.sp, color = HomingPalette.Muted),
                                maxLines = 1,
                            )
                        }
                        BasicTextField(
                            value = searchQuery,
                            onValueChange = onSearchQueryChange,
                            singleLine = true,
                            textStyle = TextStyle(fontSize = 13.sp, color = HomingPalette.Text),
                            cursorBrush = SolidColor(HomingPalette.Brand),
                            modifier = Modifier
                                .fillMaxWidth()
                                .testTag("drawer-search-input"),
                        )
                    }
                    if (searchQuery.isNotEmpty()) {
                        Icon(
                            Icons.Outlined.Close,
                            contentDescription = "清除搜索",
                            modifier = Modifier
                                .size(15.dp)
                                .clickable { onSearchQueryChange("") },
                            tint = HomingPalette.Muted,
                        )
                    }
                }
            }
            Surface(
                onClick = onNewSession,
                color = HomingPalette.Brand,
                contentColor = HomingPalette.Background,
                shape = RoundedCornerShape(10.dp),
                modifier = Modifier.testTag("drawer-new-session"),
            ) {
                Row(
                    modifier = Modifier.padding(horizontal = 10.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(3.dp),
                ) {
                    Icon(Icons.Outlined.Add, contentDescription = null, modifier = Modifier.size(16.dp))
                    Text(
                        text = "新任务",
                        style = TextStyle(fontSize = 12.5.sp, fontWeight = FontWeight.SemiBold),
                    )
                }
            }
        }

        // 1b. Quick Away Monitoring Switch (syncs with PC topbar / tray)
        Surface(
            onClick = { viewModel.toggleAwayMonitoring(!state.away) },
            color = if (state.away) {
                HomingPalette.Brand.copy(alpha = 0.13f)
            } else {
                HomingPalette.Card
            },
            shape = RoundedCornerShape(10.dp),
            border = BorderStroke(
                1.dp,
                if (state.away) HomingPalette.Brand.copy(alpha = 0.55f) else HomingPalette.NavBorder,
            ),
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 12.dp, vertical = 3.dp)
                .testTag("drawer-away-toggle"),
        ) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 10.dp, vertical = 7.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Box(
                    modifier = Modifier
                        .size(8.dp)
                        .clip(CircleShape)
                        .background(if (state.away) HomingPalette.Added else HomingPalette.Muted),
                )
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = if (state.away) "离席监听开启中" else "离席监听已关闭",
                        style = TextStyle(
                            fontSize = 12.5.sp,
                            fontWeight = FontWeight.SemiBold,
                            color = if (state.away) HomingPalette.Text else HomingPalette.Body,
                        ),
                        maxLines = 1,
                    )
                    Text(
                        text = if (state.away) {
                            "仅接收开启后的新消息 · 自动防电脑休眠"
                        } else {
                            "在电脑前工作不打扰 · 离开时点此开启"
                        },
                        style = TextStyle(fontSize = 10.5.sp, color = HomingPalette.Muted),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                Surface(
                    color = if (state.away) HomingPalette.Card else HomingPalette.Brand,
                    contentColor = if (state.away) HomingPalette.Text else HomingPalette.Background,
                    shape = RoundedCornerShape(999.dp),
                    border = if (state.away) BorderStroke(1.dp, HomingPalette.NavBorder) else null,
                ) {
                    Text(
                        text = if (state.away) "关闭监听" else "开启监听",
                        modifier = Modifier.padding(horizontal = 9.dp, vertical = 4.dp),
                        style = TextStyle(
                            fontSize = 11.sp,
                            fontWeight = FontWeight.SemiBold,
                            color = if (state.away) HomingPalette.Text else HomingPalette.Background,
                        ),
                    )
                }
            }
        }

        // 2. Active-Only Multi-Agent Capsules + Remote Wake Pill
        LazyRow(
            modifier = Modifier
                .fillMaxWidth()
                .testTag("drawer-agent-capsules"),
            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            item(key = "capsule-all") {
                DrawerAgentCapsule(
                    label = "全部 $allProjectCount",
                    urgentCount = urgentSessions.size,
                    selected = selectedAgentSource.isBlank(),
                    source = "",
                    onClick = { onSelectAgentSource("") },
                )
            }
            items(capsules, key = { "capsule-${it.source}" }) { item ->
                DrawerAgentCapsule(
                    label = if (item.totalCount > 0) "${item.label} ${item.totalCount}" else item.label,
                    urgentCount = item.urgentCount,
                    selected = selectedAgentSource.equals(item.source, ignoreCase = true),
                    source = item.source,
                    onClick = {
                        onSelectAgentSource(
                            if (selectedAgentSource.equals(item.source, ignoreCase = true)) "" else item.source,
                        )
                    },
                )
            }
            item(key = "capsule-wake-agent") {
                Surface(
                    onClick = onOpenWakeSheet,
                    color = HomingPalette.Card,
                    shape = RoundedCornerShape(999.dp),
                    border = BorderStroke(1.dp, HomingPalette.Brand.copy(alpha = 0.55f)),
                    modifier = Modifier.testTag("drawer-wake-agent-btn"),
                ) {
                    Row(
                        modifier = Modifier.padding(horizontal = 10.dp, vertical = 5.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(4.dp),
                    ) {
                        Icon(
                            Icons.Outlined.PowerSettingsNew,
                            contentDescription = null,
                            modifier = Modifier.size(13.dp),
                            tint = HomingPalette.Brand,
                        )
                        Text(
                            text = if (inactiveWakeableCount > 0) {
                                "唤醒 Agent ($inactiveWakeableCount)"
                            } else {
                                "唤醒 Agent"
                            },
                            style = TextStyle(
                                fontSize = 11.5.sp,
                                fontWeight = FontWeight.SemiBold,
                                color = HomingPalette.Brand,
                            ),
                        )
                    }
                }
            }
        }

        // 2b. Quick Cleanup + Restore Last 10 Records Strip
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 12.dp, vertical = 5.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Surface(
                onClick = onQuickClear,
                color = HomingPalette.Card,
                shape = RoundedCornerShape(8.dp),
                border = BorderStroke(1.dp, HomingPalette.NavBorder),
                modifier = Modifier
                    .weight(1f)
                    .testTag("drawer-quick-clear"),
            ) {
                Row(
                    modifier = Modifier.padding(horizontal = 8.dp, vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.Center,
                ) {
                    Icon(
                        Icons.Outlined.CleaningServices,
                        contentDescription = null,
                        modifier = Modifier.size(14.dp),
                        tint = if (clearableCount > 0) HomingPalette.Text else HomingPalette.Muted,
                    )
                    Spacer(Modifier.width(4.dp))
                    Text(
                        text = if (clearableCount > 0) "快速清理 ($clearableCount)" else "快速清理",
                        style = TextStyle(
                            fontSize = 11.5.sp,
                            fontWeight = FontWeight.Medium,
                            color = if (clearableCount > 0) HomingPalette.Text else HomingPalette.Muted,
                        ),
                        maxLines = 1,
                    )
                }
            }

            Surface(
                onClick = onOpenRestoreSheet,
                color = HomingPalette.Card,
                shape = RoundedCornerShape(8.dp),
                border = BorderStroke(1.dp, HomingPalette.NavBorder),
                modifier = Modifier
                    .weight(1f)
                    .testTag("drawer-restore-history-btn"),
            ) {
                Row(
                    modifier = Modifier.padding(horizontal = 8.dp, vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.Center,
                ) {
                    Icon(
                        Icons.Outlined.History,
                        contentDescription = null,
                        modifier = Modifier.size(14.dp),
                        tint = HomingPalette.Body,
                    )
                    Spacer(Modifier.width(4.dp))
                    Text(
                        text = if (restorableCount > 0) "恢复历史 ($restorableCount)" else "恢复历史",
                        style = TextStyle(
                            fontSize = 11.5.sp,
                            fontWeight = FontWeight.Medium,
                            color = HomingPalette.Text,
                        ),
                        maxLines = 1,
                    )
                }
            }
        }

        HorizontalDivider(color = HomingPalette.NavBorder)

        // 3. Pinned Urgent Section + Chronological Active Session List
        LazyColumn(
            modifier = Modifier
                .weight(1f)
                .fillMaxWidth()
                .testTag("drawer-session-list"),
            contentPadding = PaddingValues(horizontal = 10.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            if (filteredUrgent.isNotEmpty()) {
                item(key = "urgent-header") {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 4.dp, vertical = 4.dp)
                            .testTag("drawer-urgent-section"),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(4.dp),
                    ) {
                        Icon(
                            Icons.Outlined.Bolt,
                            contentDescription = null,
                            modifier = Modifier.size(14.dp),
                            tint = HomingPalette.Warning,
                        )
                        Text(
                            text = "待你处理 (${filteredUrgent.size})",
                            style = TextStyle(
                                fontSize = 12.sp,
                                fontWeight = FontWeight.SemiBold,
                                color = HomingPalette.Warning,
                            ),
                        )
                    }
                }
                items(filteredUrgent, key = { "urgent-${it.sessionId}" }) { session ->
                    DrawerUrgentSessionCard(
                        session = session,
                        active = session.sessionId == activeSessionId,
                        onOpen = { onSelectSession(session) },
                        onQuickDecision = { decision ->
                            val pending = session.pendingEvent
                            if (pending != null && pending.isPermission) {
                                viewModel.decideApproval(
                                    eventId = pending.id,
                                    decision = decision,
                                    onFallbackText = {
                                        viewModel.sendReply(
                                            session = session,
                                            text = if (decision == "allow") "同意" else "拒绝",
                                            eventId = pending.id,
                                            files = emptyList(),
                                            model = null,
                                            effort = null,
                                            permissionMode = null,
                                        )
                                    },
                                    optionId = null,
                                )
                            } else {
                                onSelectSession(session)
                            }
                        },
                    )
                }
                if (regularSessions.isNotEmpty()) {
                    item(key = "recent-header") {
                        Text(
                            text = "项目 (${projectGroups.size})",
                            modifier = Modifier.padding(start = 4.dp, top = 8.dp, bottom = 2.dp),
                            style = TextStyle(
                                fontSize = 11.5.sp,
                                fontWeight = FontWeight.Medium,
                                color = HomingPalette.Muted,
                            ),
                        )
                    }
                }
            }

            if (filteredUrgent.isEmpty() && regularSessions.isEmpty()) {
                item(key = "drawer-empty") {
                    Column(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(vertical = 28.dp, horizontal = 14.dp),
                        horizontalAlignment = Alignment.CenterHorizontally,
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        Text(
                            text = when {
                                cleanQuery.isNotBlank() || selectedAgentSource.isNotBlank() -> "没有匹配的活跃会话"
                                capsules.isEmpty() -> "电脑上暂无已打开的活跃 Agent"
                                else -> "当前视图已清空"
                            },
                            style = MaterialTheme.typography.titleSmall,
                            color = HomingPalette.Text,
                        )
                        Text(
                            text = if (capsules.isEmpty()) {
                                "可点击上方「唤醒 Agent」远程打开电脑上的 Agent，或从「恢复历史」载入最近 10 条对话记录。"
                            } else {
                                "后台运行中的任务会自动显示在这里；也可点击「恢复历史」挑会话载入最近 10 条记录。"
                            },
                            style = MaterialTheme.typography.bodySmall,
                            color = HomingPalette.Muted,
                            textAlign = TextAlign.Center,
                        )
                    }
                }
            }

            items(projectGroups, key = { "project-${it.key}" }) { group ->
                val collapsedMarker = "collapsed:${group.key}"
                val expanded = group.key in expandedProjects ||
                    (group.key == activeProjectKey && collapsedMarker !in expandedProjects)
                DrawerProjectRow(
                    group = group,
                    activeSessionId = activeSessionId,
                    expanded = expanded && group.sessions.size > 1,
                    onToggle = {
                        expandedProjects = if (expanded) {
                            expandedProjects - group.key + collapsedMarker
                        } else {
                            expandedProjects - collapsedMarker + group.key
                        }
                    },
                    onOpenSession = onSelectSession,
                )
            }
        }

        // 4. Compact Drawer Footer (Keep-Alive Helper + Computer Status + Rooms + Me/Settings)
        HorizontalDivider(color = HomingPalette.NavBorder)
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .background(HomingPalette.NavBackground)
                .padding(horizontal = 12.dp, vertical = 10.dp)
                .testTag("drawer-footer"),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            if (batteryOptimized) {
                Surface(
                    onClick = { dev.agentpush.poc.KeepAliveHelper.requestIgnoreBatteryOptimizations(context) },
                    color = HomingPalette.Warning.copy(alpha = 0.12f),
                    shape = RoundedCornerShape(8.dp),
                    border = BorderStroke(1.dp, HomingPalette.Warning.copy(alpha = 0.45f)),
                    modifier = Modifier.testTag("drawer-keepalive-exemption"),
                ) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 10.dp, vertical = 6.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        Icon(
                            Icons.Outlined.Shield,
                            contentDescription = null,
                            modifier = Modifier.size(14.dp),
                            tint = HomingPalette.Warning,
                        )
                        Text(
                            text = "开启后台无限制保活（未关闭时防断连）",
                            modifier = Modifier.weight(1f),
                            style = TextStyle(
                                fontSize = 11.5.sp,
                                fontWeight = FontWeight.Medium,
                                color = HomingPalette.Warning,
                            ),
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                }
            }

            val drawerNeedsRelayAuth = state.connection != ConnectionState.CONNECTED &&
                isRelayAuthReason(state.connectionDiagnostics.lastDisconnectReason)
            Surface(
                onClick = onOpenMe,
                color = HomingPalette.Card,
                shape = RoundedCornerShape(10.dp),
                border = BorderStroke(
                    1.dp,
                    if (drawerNeedsRelayAuth) HomingPalette.Warning.copy(alpha = 0.5f) else HomingPalette.NavBorder,
                ),
            ) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(start = 10.dp, end = 6.dp, top = 8.dp, bottom = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Box(
                        modifier = Modifier
                            .size(8.dp)
                            .clip(CircleShape)
                            .background(
                                when (state.connection) {
                                    ConnectionState.CONNECTED -> HomingPalette.Added
                                    ConnectionState.RECONNECTING -> HomingPalette.Warning
                                    ConnectionState.DISCONNECTED -> HomingPalette.Error
                                },
                            ),
                    )
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = activeComputerName,
                            style = TextStyle(
                                fontSize = 13.sp,
                                fontWeight = FontWeight.SemiBold,
                                color = HomingPalette.Text,
                            ),
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        Text(
                            text = when {
                                state.connection == ConnectionState.CONNECTED ->
                                    if (state.transportKind == TransportKind.CLOUD) "云端加密后台保活中" else "局域网直连保活中"
                                drawerNeedsRelayAuth -> "需要绑定中继账号 · 点右侧扫码"
                                state.connection == ConnectionState.RECONNECTING -> "正在重连电脑…"
                                else -> "未连接 · 点此查看或重连"
                            },
                            style = TextStyle(
                                fontSize = 11.sp,
                                color = if (drawerNeedsRelayAuth) HomingPalette.Warning else HomingPalette.Muted,
                            ),
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                    IconButton(
                        onClick = onAddComputer,
                        modifier = Modifier
                            .size(32.dp)
                            .testTag("drawer-scan-pair"),
                    ) {
                        Icon(
                            Icons.Outlined.QrCodeScanner,
                            contentDescription = "扫码配对",
                            modifier = Modifier.size(18.dp),
                            tint = if (drawerNeedsRelayAuth) HomingPalette.Warning else HomingPalette.Brand,
                        )
                    }
                }
            }

            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Surface(
                    onClick = onOpenRooms,
                    color = HomingPalette.Card,
                    shape = RoundedCornerShape(9.dp),
                    modifier = Modifier
                        .weight(1f)
                        .testTag("drawer-open-rooms"),
                ) {
                    Row(
                        modifier = Modifier.padding(horizontal = 10.dp, vertical = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.Center,
                    ) {
                        Icon(
                            Icons.Outlined.Groups,
                            contentDescription = null,
                            modifier = Modifier.size(16.dp),
                            tint = HomingPalette.Body,
                        )
                        Spacer(Modifier.width(5.dp))
                        Text(
                            text = strings.tabRooms,
                            style = TextStyle(fontSize = 12.5.sp, fontWeight = FontWeight.Medium, color = HomingPalette.Text),
                        )
                    }
                }
                Surface(
                    onClick = onOpenMe,
                    color = HomingPalette.Card,
                    shape = RoundedCornerShape(9.dp),
                    modifier = Modifier
                        .weight(1f)
                        .testTag("drawer-open-me"),
                ) {
                    Row(
                        modifier = Modifier.padding(horizontal = 10.dp, vertical = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.Center,
                    ) {
                        Icon(
                            Icons.Outlined.Computer,
                            contentDescription = null,
                            modifier = Modifier.size(16.dp),
                            tint = HomingPalette.Body,
                        )
                        Spacer(Modifier.width(5.dp))
                        Text(
                            text = "电脑",
                            style = TextStyle(fontSize = 12.5.sp, fontWeight = FontWeight.Medium, color = HomingPalette.Text),
                            maxLines = 1,
                        )
                    }
                }
                IconButton(
                    onClick = onOpenBrowserHome,
                    modifier = Modifier
                        .size(36.dp)
                        .clip(RoundedCornerShape(9.dp))
                        .background(HomingPalette.Card)
                        .testTag("drawer-open-browser"),
                ) {
                    Icon(
                        Icons.Outlined.Language,
                        contentDescription = "内置浏览器",
                        modifier = Modifier.size(17.dp),
                        tint = HomingPalette.Body,
                    )
                }
                IconButton(
                    onClick = onOpenSettings,
                    modifier = Modifier
                        .size(36.dp)
                        .clip(RoundedCornerShape(9.dp))
                        .background(HomingPalette.Card)
                        .testTag("drawer-open-settings"),
                ) {
                    Icon(
                        Icons.Outlined.Settings,
                        contentDescription = strings.settingsTitle,
                        modifier = Modifier.size(17.dp),
                        tint = HomingPalette.Body,
                    )
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun WakeAgentsBottomSheet(
    candidates: List<WakeableAgentCandidate>,
    onWakeAgent: (agentId: String, onDone: () -> Unit) -> Unit,
    onDismiss: () -> Unit,
) {
    var wakingIds by remember { mutableStateOf(setOf<String>()) }

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        containerColor = HomingPalette.Card,
        contentColor = HomingPalette.Text,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .navigationBarsPadding()
                .padding(horizontal = 16.dp, vertical = 6.dp)
                .testTag("wake-agents-sheet"),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(
                        text = "唤醒本机已对接的 Agent",
                        style = MaterialTheme.typography.titleMedium.copy(fontWeight = FontWeight.Bold),
                        color = HomingPalette.Text,
                    )
                    Text(
                        text = "会话抽屉默认只显示本机已打开的活跃 Agent；点击即可远程唤醒上线。",
                        style = TextStyle(fontSize = 12.sp, color = HomingPalette.Muted),
                    )
                }
            }

            if (candidates.isEmpty()) {
                Text(
                    text = "暂未检测到可唤醒的已安装 Agent，请先确认电脑在线并在桌面端完成接入。",
                    modifier = Modifier.padding(vertical = 20.dp),
                    style = TextStyle(fontSize = 13.sp, color = HomingPalette.Muted),
                )
            } else {
                LazyColumn(
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(max = 420.dp),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                    contentPadding = PaddingValues(bottom = 16.dp),
                ) {
                    items(candidates, key = { "wake-row-${it.id}" }) { item ->
                        val isWaking = item.id in wakingIds
                        Surface(
                            color = HomingPalette.Raised,
                            shape = RoundedCornerShape(12.dp),
                            border = BorderStroke(
                                1.dp,
                                if (item.activeOnline) HomingPalette.Brand.copy(alpha = 0.45f) else HomingPalette.NavBorder,
                            ),
                        ) {
                            Row(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .padding(horizontal = 12.dp, vertical = 10.dp),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(10.dp),
                            ) {
                                AgentAvatar(
                                    brand = AgentBrand.fromWire(item.id),
                                    source = item.id,
                                    size = 34.dp,
                                )
                                Column(
                                    modifier = Modifier.weight(1f),
                                    verticalArrangement = Arrangement.spacedBy(2.dp),
                                ) {
                                    Text(
                                        text = item.label,
                                        style = TextStyle(
                                            fontSize = 14.sp,
                                            fontWeight = FontWeight.SemiBold,
                                            color = HomingPalette.Text,
                                        ),
                                    )
                                    Text(
                                        text = when {
                                            item.processRunning -> "本机进程运行中 · 在线活跃"
                                            item.activeOnline -> "已唤醒在线 · 可直接发起会话"
                                            item.wakeMode == "app" -> "未打开 · 点击在电脑后台启动桌面端"
                                            item.wakeMode == "serve" -> "未运行 · 点击启动本机后台服务"
                                            else -> "待命离线 · 点击激活上线"
                                        },
                                        style = TextStyle(
                                            fontSize = 11.5.sp,
                                            color = if (item.activeOnline) HomingPalette.Added else HomingPalette.Muted,
                                        ),
                                    )
                                }
                                if (item.activeOnline) {
                                    Row(
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(4.dp),
                                    ) {
                                        Icon(
                                            Icons.Outlined.CheckCircle,
                                            contentDescription = null,
                                            modifier = Modifier.size(15.dp),
                                            tint = HomingPalette.Added,
                                        )
                                        Text(
                                            text = "已在线",
                                            style = TextStyle(
                                                fontSize = 12.sp,
                                                fontWeight = FontWeight.SemiBold,
                                                color = HomingPalette.Added,
                                            ),
                                        )
                                    }
                                } else {
                                    Button(
                                        onClick = {
                                            if (!isWaking) {
                                                wakingIds = wakingIds + item.id
                                                onWakeAgent(item.id) {
                                                    wakingIds = wakingIds - item.id
                                                }
                                            }
                                        },
                                        enabled = !isWaking,
                                        colors = ButtonDefaults.buttonColors(
                                            containerColor = HomingPalette.Brand,
                                            contentColor = HomingPalette.Background,
                                        ),
                                        contentPadding = PaddingValues(horizontal = 12.dp, vertical = 6.dp),
                                        shape = RoundedCornerShape(8.dp),
                                        modifier = Modifier.testTag("wake-agent-${item.id}"),
                                    ) {
                                        if (isWaking) {
                                            CircularProgressIndicator(
                                                modifier = Modifier.size(14.dp),
                                                strokeWidth = 2.dp,
                                                color = HomingPalette.Background,
                                            )
                                        } else {
                                            Text(
                                                text = "唤醒",
                                                style = TextStyle(fontSize = 12.sp, fontWeight = FontWeight.SemiBold),
                                            )
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun DrawerAgentCapsule(
    label: String,
    urgentCount: Int,
    selected: Boolean,
    source: String,
    onClick: () -> Unit,
) {
    Surface(
        onClick = onClick,
        color = if (selected) HomingPalette.Brand else HomingPalette.Card,
        contentColor = if (selected) HomingPalette.Background else HomingPalette.Text,
        shape = RoundedCornerShape(999.dp),
        border = if (selected) null else BorderStroke(1.dp, HomingPalette.NavBorder),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 10.dp, vertical = 5.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(5.dp),
        ) {
            if (source.isNotBlank()) {
                if (selected) {
                    Box(
                        modifier = Modifier
                            .size(18.dp)
                            .clip(CircleShape)
                            .background(HomingPalette.Background.copy(alpha = 0.88f)),
                        contentAlignment = Alignment.Center,
                    ) {
                        AgentGlyph(
                            brand = AgentBrand.fromWire(source),
                            source = source,
                            size = 12.dp,
                            tint = HomingPalette.Text,
                        )
                    }
                } else {
                    AgentGlyph(
                        brand = AgentBrand.fromWire(source),
                        source = source,
                        size = 14.dp,
                        tint = HomingPalette.Text,
                    )
                }
            }
            Text(
                text = label,
                style = TextStyle(
                    fontSize = 12.sp,
                    fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Medium,
                ),
            )
            if (urgentCount > 0) {
                Box(
                    modifier = Modifier
                        .clip(RoundedCornerShape(999.dp))
                        .background(if (selected) HomingPalette.Background else HomingPalette.Warning)
                        .padding(horizontal = 5.dp, vertical = 1.dp),
                ) {
                    Text(
                        text = urgentCount.toString(),
                        style = TextStyle(
                            fontSize = 10.sp,
                            fontWeight = FontWeight.Bold,
                            color = if (selected) HomingPalette.Warning else HomingPalette.Background,
                        ),
                    )
                }
            }
        }
    }
}

@Composable
private fun DrawerUrgentSessionCard(
    session: SessionSummary,
    active: Boolean,
    onOpen: () -> Unit,
    onQuickDecision: (String) -> Unit,
) {
    val pending = session.pendingEvent
    val canInlineApprove = pending != null &&
        pending.isPermission &&
        pending.approvalOptions.size <= 2
    val agentName = session.agentLabel.ifBlank { defaultAgentDisplayName(session.source) }

    Surface(
        onClick = onOpen,
        color = if (active) HomingPalette.Raised else HomingPalette.Card,
        shape = RoundedCornerShape(10.dp),
        border = BorderStroke(
            width = 1.dp,
            color = if (active) HomingPalette.Brand else HomingPalette.Warning.copy(alpha = 0.65f),
        ),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 10.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(5.dp),
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                AgentGlyph(
                    brand = session.brand,
                    source = session.source,
                    size = 16.dp,
                )
                Text(
                    text = session.title,
                    modifier = Modifier.weight(1f),
                    style = TextStyle(
                        fontSize = 13.5.sp,
                        fontWeight = FontWeight.SemiBold,
                        color = HomingPalette.Text,
                    ),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = when {
                        session.status == SessionStatus.NEEDS_PERMISSION || pending?.isPermission == true -> "待授权"
                        session.status == SessionStatus.ERROR -> "出错"
                        else -> "等你回复"
                    },
                    style = TextStyle(
                        fontSize = 11.sp,
                        fontWeight = FontWeight.SemiBold,
                        color = if (session.status == SessionStatus.ERROR) HomingPalette.Error else HomingPalette.Warning,
                    ),
                )
            }
            if (session.lastPreview.isNotBlank()) {
                Text(
                    text = session.lastPreview,
                    style = TextStyle(fontSize = 12.sp, color = HomingPalette.Body),
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Text(
                    text = "$agentName · ${session.project.ifBlank { "未命名项目" }}",
                    style = TextStyle(fontSize = 11.sp, color = HomingPalette.Muted),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                if (canInlineApprove) {
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        Surface(
                            onClick = { onQuickDecision("deny") },
                            color = HomingPalette.Raised,
                            shape = RoundedCornerShape(6.dp),
                        ) {
                            Text(
                                text = "拒绝",
                                modifier = Modifier.padding(horizontal = 8.dp, vertical = 3.dp),
                                style = TextStyle(fontSize = 11.5.sp, color = HomingPalette.Body),
                            )
                        }
                        Surface(
                            onClick = { onQuickDecision("allow") },
                            color = HomingPalette.Brand,
                            shape = RoundedCornerShape(6.dp),
                        ) {
                            Text(
                                text = "允许",
                                modifier = Modifier.padding(horizontal = 8.dp, vertical = 3.dp),
                                style = TextStyle(
                                    fontSize = 11.5.sp,
                                    fontWeight = FontWeight.SemiBold,
                                    color = HomingPalette.Background,
                                ),
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun DrawerSessionRow(
    session: SessionSummary,
    active: Boolean,
    onClick: () -> Unit,
) {
    val agentName = session.agentLabel.ifBlank { defaultAgentDisplayName(session.source) }
    Surface(
        onClick = onClick,
        color = if (active) HomingPalette.Raised else Color.Transparent,
        shape = RoundedCornerShape(10.dp),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 10.dp, vertical = 9.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(9.dp),
        ) {
            AgentGlyph(
                brand = session.brand,
                source = session.source,
                size = 19.dp,
            )
            Column(
                modifier = Modifier.weight(1f),
                verticalArrangement = Arrangement.spacedBy(2.dp),
            ) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    Text(
                        text = session.title,
                        modifier = Modifier.weight(1f),
                        style = TextStyle(
                            fontSize = 14.sp,
                            fontWeight = if (active || session.unreadCount > 0) {
                                FontWeight.SemiBold
                            } else {
                                FontWeight.Normal
                            },
                            color = HomingPalette.Text,
                        ),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        text = relativeTime(session.lastTs),
                        style = TextStyle(fontSize = 11.sp, color = HomingPalette.Muted),
                    )
                }
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(5.dp),
                ) {
                    Text(
                        text = buildString {
                            append(agentName)
                            append(" · ")
                            append(session.project.ifBlank { "未命名项目" })
                        },
                        modifier = Modifier.weight(1f),
                        style = TextStyle(fontSize = 11.5.sp, color = HomingPalette.Muted),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    if (session.running) {
                        Box(
                            modifier = Modifier
                                .size(7.dp)
                                .clip(CircleShape)
                                .background(HomingPalette.Brand),
                        )
                    } else if (session.unreadCount > 0) {
                        Box(
                            modifier = Modifier
                                .size(7.dp)
                                .clip(CircleShape)
                                .background(HomingPalette.BrandSoft),
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun EmptyWorkspaceConversation(
    state: AgentUiState,
    historicalCount: Int,
    inactiveWakeableCount: Int,
    needsRelayAuth: Boolean,
    onOpenDrawer: () -> Unit,
    onNewSession: () -> Unit,
    onToggleAway: () -> Unit,
    onOpenWakeSheet: () -> Unit,
    onOpenRestoreSheet: () -> Unit,
    onScanPair: () -> Unit,
    onRetry: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .statusBarsPadding()
            .navigationBarsPadding(),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 8.dp, vertical = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                IconButton(
                    onClick = onOpenDrawer,
                    modifier = Modifier.testTag("open-workspace-drawer"),
                ) {
                    Icon(Icons.Outlined.Menu, contentDescription = "打开会话抽屉", tint = HomingPalette.Text)
                }
                Text(
                    text = "Homing",
                    style = MaterialTheme.typography.titleMedium,
                    color = HomingPalette.Text,
                )
            }
            Row(
                modifier = Modifier.padding(end = 6.dp),
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Surface(
                    onClick = onToggleAway,
                    color = if (state.away) HomingPalette.Brand.copy(alpha = 0.14f) else HomingPalette.Card,
                    shape = RoundedCornerShape(999.dp),
                    border = BorderStroke(
                        1.dp,
                        if (state.away) HomingPalette.Brand.copy(alpha = 0.55f) else HomingPalette.NavBorder,
                    ),
                    modifier = Modifier.testTag("empty-header-away-toggle"),
                ) {
                    Row(
                        modifier = Modifier.padding(horizontal = 9.dp, vertical = 5.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(5.dp),
                    ) {
                        Box(
                            modifier = Modifier
                                .size(7.dp)
                                .clip(CircleShape)
                                .background(if (state.away) HomingPalette.Added else HomingPalette.Muted),
                        )
                        Text(
                            text = if (state.away) "离席监听中" else "开启监听",
                            style = TextStyle(
                                fontSize = 11.5.sp,
                                fontWeight = FontWeight.SemiBold,
                                color = if (state.away) HomingPalette.Brand else HomingPalette.Text,
                            ),
                        )
                    }
                }
                Surface(
                    onClick = onOpenWakeSheet,
                    color = HomingPalette.Card,
                    shape = RoundedCornerShape(999.dp),
                    border = BorderStroke(1.dp, HomingPalette.NavBorder),
                ) {
                    Row(
                        modifier = Modifier.padding(horizontal = 9.dp, vertical = 5.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(4.dp),
                    ) {
                        Icon(
                            Icons.Outlined.PowerSettingsNew,
                            contentDescription = null,
                            modifier = Modifier.size(13.dp),
                            tint = HomingPalette.Brand,
                        )
                        Text(
                            text = "唤醒 Agent",
                            style = TextStyle(fontSize = 11.5.sp, color = HomingPalette.Text),
                        )
                    }
                }
            }
        }
        if (needsRelayAuth) {
            RelayAuthActionBanner(
                reason = state.connectionDiagnostics.lastDisconnectReason,
                onScanPair = onScanPair,
                onRetry = onRetry,
                modifier = Modifier.padding(horizontal = 14.dp, vertical = 6.dp),
            )
        }
        Box(
            modifier = Modifier
                .weight(1f)
                .fillMaxWidth()
                .padding(horizontal = 28.dp),
            contentAlignment = Alignment.Center,
        ) {
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                HomingPigeon(state = PigeonState.Idle, size = 56.dp)
                Text(
                    text = when {
                        state.isRefreshing -> "正在同步电脑上的活跃状态…"
                        state.away -> "离席监听开启中 · 等待电脑新消息"
                        else -> "离席监听已关闭 · 在电脑前工作不打扰"
                    },
                    style = MaterialTheme.typography.titleMedium,
                    color = HomingPalette.Text,
                    textAlign = TextAlign.Center,
                )
                Text(
                    text = if (state.away) {
                        "离开电脑期间产生的新回复与权限请求会自动出现在这里；过往旧项目已自动过滤，可在「恢复历史」按需查看。"
                    } else {
                        "离开电脑前点下方「开启离席监听」，手机将只接收开启后的新动态；电脑上的旧项目不会刷屏，可随时从「恢复历史」调取。"
                    },
                    style = MaterialTheme.typography.bodyMedium,
                    color = HomingPalette.Muted,
                    textAlign = TextAlign.Center,
                )
                Row(
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Button(
                        onClick = onToggleAway,
                        colors = ButtonDefaults.buttonColors(
                            containerColor = if (state.away) HomingPalette.Card else HomingPalette.Brand,
                            contentColor = if (state.away) HomingPalette.Text else HomingPalette.Background,
                        ),
                        border = if (state.away) BorderStroke(1.dp, HomingPalette.Brand.copy(alpha = 0.55f)) else null,
                        shape = RoundedCornerShape(10.dp),
                        modifier = Modifier.testTag("empty-away-toggle"),
                    ) {
                        Text(
                            text = if (state.away) "关闭离席监听" else "开启离席监听",
                            fontWeight = FontWeight.SemiBold,
                        )
                    }
                    OutlinedButton(
                        onClick = onNewSession,
                        border = BorderStroke(1.dp, HomingPalette.NavBorder),
                        shape = RoundedCornerShape(10.dp),
                    ) {
                        Icon(Icons.Outlined.Add, contentDescription = null, modifier = Modifier.size(17.dp), tint = HomingPalette.Brand)
                        Spacer(Modifier.width(5.dp))
                        Text("发起新任务", color = HomingPalette.Text, fontWeight = FontWeight.SemiBold)
                    }
                }

                Row(
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    OutlinedButton(
                        onClick = onOpenWakeSheet,
                        border = BorderStroke(1.dp, HomingPalette.NavBorder),
                        shape = RoundedCornerShape(10.dp),
                        modifier = Modifier.testTag("empty-wake-agent-btn"),
                    ) {
                        Icon(
                            Icons.Outlined.PowerSettingsNew,
                            contentDescription = null,
                            modifier = Modifier.size(16.dp),
                            tint = HomingPalette.Brand,
                        )
                        Spacer(Modifier.width(5.dp))
                        Text(
                            text = if (inactiveWakeableCount > 0) {
                                "唤醒 Agent ($inactiveWakeableCount)"
                            } else {
                                "唤醒 Agent"
                            },
                            color = HomingPalette.Text,
                            fontSize = 12.5.sp,
                        )
                    }

                    if (historicalCount > 0) {
                        OutlinedButton(
                            onClick = onOpenRestoreSheet,
                            border = BorderStroke(1.dp, HomingPalette.NavBorder),
                            shape = RoundedCornerShape(10.dp),
                            modifier = Modifier.testTag("empty-restore-history-btn"),
                        ) {
                            Icon(
                                Icons.Outlined.History,
                                contentDescription = null,
                                modifier = Modifier.size(16.dp),
                                tint = HomingPalette.Body,
                            )
                            Spacer(Modifier.width(5.dp))
                            Text(
                                text = "恢复历史 ($historicalCount)",
                                color = HomingPalette.Text,
                                fontSize = 12.5.sp,
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun UnpairedWorkspaceWelcome(
    onScanPair: () -> Unit,
    onManualSetup: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(HomingPalette.Background)
            .statusBarsPadding()
            .navigationBarsPadding()
            .padding(horizontal = 28.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        HomingPigeon(state = PigeonState.Idle, size = 68.dp)
        Spacer(Modifier.height(16.dp))
        Text(
            text = "离席后，一个入口接管所有桌面 Agent",
            style = MaterialTheme.typography.titleLarge.copy(fontWeight = FontWeight.Bold),
            color = HomingPalette.Text,
            textAlign = TextAlign.Center,
        )
        Spacer(Modifier.height(8.dp))
        Text(
            text = "在 Mac 打开 Homing 守护中心，扫码即可端到端加密配对 Claude Code、Codex、WorkBuddy 等多 Agent 会话，无需切换多个 App。",
            style = MaterialTheme.typography.bodyMedium,
            color = HomingPalette.Muted,
            textAlign = TextAlign.Center,
        )
        Spacer(Modifier.height(24.dp))
        Button(
            onClick = onScanPair,
            modifier = Modifier
                .fillMaxWidth()
                .testTag("welcome-scan-pair"),
            colors = ButtonDefaults.buttonColors(
                containerColor = HomingPalette.Brand,
                contentColor = HomingPalette.Background,
            ),
            shape = RoundedCornerShape(12.dp),
        ) {
            Icon(Icons.Outlined.QrCodeScanner, contentDescription = null, modifier = Modifier.size(18.dp))
            Spacer(Modifier.width(8.dp))
            Text("扫码连接我的电脑", fontWeight = FontWeight.SemiBold)
        }
        Spacer(Modifier.height(10.dp))
        OutlinedButton(
            onClick = onManualSetup,
            modifier = Modifier.fillMaxWidth(),
            shape = RoundedCornerShape(12.dp),
            border = BorderStroke(1.dp, HomingPalette.NavBorder),
        ) {
            Text("手动填写连接地址", color = HomingPalette.Body)
        }
    }
}

internal fun isRelayAuthReason(reason: String?): Boolean {
    if (reason.isNullOrBlank()) return false
    return reason.contains("中继账号") || reason.contains("Relay Key") || reason.contains("配对")
}

@Composable
private fun RelayAuthActionBanner(
    reason: String?,
    onScanPair: () -> Unit,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Surface(
        modifier = modifier
            .fillMaxWidth()
            .testTag("relay-auth-banner"),
        shape = RoundedCornerShape(12.dp),
        color = HomingPalette.Card,
        border = BorderStroke(1.dp, HomingPalette.Warning.copy(alpha = 0.55f)),
        tonalElevation = 4.dp,
        shadowElevation = 4.dp,
    ) {
        Column(
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 10.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Icon(
                    Icons.Outlined.QrCodeScanner,
                    contentDescription = null,
                    modifier = Modifier.size(18.dp),
                    tint = HomingPalette.Warning,
                )
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "需要绑定中继账号",
                        style = MaterialTheme.typography.labelLarge,
                        color = HomingPalette.Text,
                    )
                    Text(
                        text = reason?.takeIf { it.isNotBlank() }
                            ?: "电脑端填写 Relay Key 后，点下方「扫码配对」扫一次电脑二维码即可连通",
                        style = MaterialTheme.typography.bodySmall,
                        color = HomingPalette.Muted,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.End,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                TextButton(onClick = onRetry) { Text("重试") }
                Spacer(Modifier.width(6.dp))
                Button(
                    onClick = onScanPair,
                    modifier = Modifier.testTag("banner-scan-pair"),
                    colors = ButtonDefaults.buttonColors(
                        containerColor = HomingPalette.Brand,
                        contentColor = HomingPalette.Background,
                    ),
                    shape = RoundedCornerShape(9.dp),
                ) {
                    Icon(
                        Icons.Outlined.QrCodeScanner,
                        contentDescription = null,
                        modifier = Modifier.size(16.dp),
                    )
                    Spacer(Modifier.width(5.dp))
                    Text("扫码配对", fontWeight = FontWeight.SemiBold)
                }
            }
        }
    }
}

internal fun isSyntheticHelperSession(session: SessionSummary): Boolean {
    if (session.sessionId.startsWith("relay-selftest-")) {
        return true
    }
    val preview = session.lastPreview.trim()
    if (dev.agentpush.poc.ui.screens.isInternalTitleJsonPayload(preview)) {
        return true
    }
    val latestText = session.latestEvent?.text?.trim().orEmpty()
    if (session.events.size <= 1 && dev.agentpush.poc.ui.screens.isInternalTitleJsonPayload(latestText)) {
        return true
    }
    return false
}
