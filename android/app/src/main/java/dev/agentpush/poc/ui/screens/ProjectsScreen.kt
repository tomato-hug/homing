package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Archive
import androidx.compose.material.icons.outlined.Bookmark
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.FilterList
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material.icons.outlined.NotificationsOff
import androidx.compose.material.icons.outlined.PushPin
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.SelectAll
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.FilterChipDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.Surface
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.material3.rememberSwipeToDismissBoxState
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
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.agentpush.poc.AgentViewModel
import dev.agentpush.poc.AgentBrand
import dev.agentpush.poc.SessionGrouping
import dev.agentpush.poc.SessionSearchHit
import dev.agentpush.poc.SessionStatus
import dev.agentpush.poc.SessionSummary
import dev.agentpush.poc.relativeTime
import dev.agentpush.poc.ui.AgentAvatar
import dev.agentpush.poc.ui.theme.HomingMono
import dev.agentpush.poc.ui.theme.HomingPalette
import kotlinx.coroutines.launch
import kotlinx.coroutines.delay
import kotlin.math.absoluteValue

private data class AgentFilterChoice(
    val source: String,
    val label: String,
)

private enum class SessionStatusFilter(val wire: String, val label: String) {
    ALL("", "全部"),
    RUNNING("running", "运行中"),
    NEEDS_YOU("needs", "等你"),
    COLLAB("collab", "有协作"),
    UNREAD("unread", "仅未读"),
}

private data class SessionRowNode(
    val session: SessionSummary,
    val depth: Int,
)

private enum class SessionRowAction {
    PIN,
    UNPIN,
    RESIDENT,
    UNRESIDENT,
    MUTE,
    UNMUTE,
    ARCHIVE,
    HIDE_ON_PHONE,
    MARK_READ,
}

@Composable
fun ProjectsScreen(
    viewModel: AgentViewModel,
    onOpenSession: (String) -> Unit,
    onNewSession: (String) -> Unit,
    onOpenSettings: () -> Unit = {},
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val searchHits by viewModel.sessionSearchHits.collectAsStateWithLifecycle()
    val searchNotice by viewModel.sessionSearchNotice.collectAsStateWithLifecycle()
    var grouping by rememberSaveable { mutableStateOf(SessionGrouping.PROJECT) }
    var query by rememberSaveable { mutableStateOf("") }
    var searchOpen by rememberSaveable { mutableStateOf(false) }
    var filterSheetOpen by rememberSaveable { mutableStateOf(false) }
    var residentPickerOpen by rememberSaveable { mutableStateOf(false) }
    var groupingMenuExpanded by remember { mutableStateOf(false) }
    var selectedAgents by remember { mutableStateOf<Set<String>>(emptySet()) }
    var statusFilter by rememberSaveable { mutableStateOf(SessionStatusFilter.ALL.wire) }
    var temporaryExpanded by rememberSaveable { mutableStateOf(false) }
    var archivedExpanded by rememberSaveable { mutableStateOf(false) }
    var selectionMode by rememberSaveable { mutableStateOf(false) }
    var selectedSessionIds by remember { mutableStateOf<Set<String>>(emptySet()) }
    val scope = rememberCoroutineScope()
    val rowSnackbar = remember { SnackbarHostState() }
    val searchFocusRequester = remember { FocusRequester() }

    val mobileInbox = state.mobileInbox
    val allSavedSessions = remember(state) {
        state.sessions()
            .filterNot { it.archived || it.isEmpty }
            .sortedByDescending { it.lastTs }
    }
    val sessions = remember(state) {
        mobileInbox.visibleSessions(allSavedSessions)
            .filterNot { state.phoneConversations.isSessionHidden(it, state.activeComputerKey) }
    }
    val visibleSessionIds = remember(sessions) { sessions.map { it.sessionId }.toSet() }
    val visibleSearchHits = remember(searchHits, visibleSessionIds) {
        searchHits.filter { it.sessionId in visibleSessionIds }
    }
    val directory = state.agentDirectory
    val agentChoices = remember(directory, sessions) {
        val sources = sessions
            .map { it.source.ifBlank { it.brand.wire } }
            .toSet()
        sources
            .filter { it.isNotBlank() }
            .map { source ->
                AgentFilterChoice(
                    source = source,
                    label = directory.profile(source).label
                        .ifBlank { sessions.firstOrNull { it.source == source }?.agentLabel.orEmpty() }
                        .ifBlank { source },
                )
            }
            .sortedBy { it.label.lowercase() }
    }
    val status = SessionStatusFilter.entries.firstOrNull { it.wire == statusFilter }
        ?: SessionStatusFilter.ALL
    val localMatches = remember(sessions, selectedAgents, status, query) {
        sessions
            .filterNot { it.archived || it.isEmpty }
            .filter { session ->
                selectedAgents.isEmpty() ||
                    session.source.ifBlank { session.brand.wire } in selectedAgents
            }
            .filter { session -> session.matches(status) }
            .filter { session ->
                val needle = query.trim()
                needle.isBlank() ||
                    session.title.contains(needle, ignoreCase = true) ||
                    session.project.contains(needle, ignoreCase = true) ||
                    session.cwd.contains(needle, ignoreCase = true) ||
                    session.lastPreview.contains(needle, ignoreCase = true) ||
                    session.agentLabel.contains(needle, ignoreCase = true)
            }
    }
    val archivedSessions = remember(sessions, selectedAgents, query) {
        sessions
            .filter { it.archived && !it.isEmpty }
            .filter { session ->
                selectedAgents.isEmpty() ||
                    session.source.ifBlank { session.brand.wire } in selectedAgents
            }
            .filter { session ->
                val needle = query.trim()
                needle.isBlank() ||
                    session.title.contains(needle, ignoreCase = true) ||
                    session.project.contains(needle, ignoreCase = true) ||
                    session.cwd.contains(needle, ignoreCase = true)
            }
            .sortedByDescending { it.lastTs }
    }
    val (temporary, regular) = remember(localMatches) {
        localMatches.partition { it.isTemp || it.isTemporaryWorkspace() }
    }
    val projectGroups = remember(regular) {
        regular.groupBy { it.cwd.ifBlank { "__unassigned__" } }
            .map { (key, groupSessions) -> key to hierarchicalSessions(groupSessions) }
            .sortedByDescending { (_, groupSessions) ->
                groupSessions.firstOrNull()?.session?.lastTs.orEmpty()
            }
    }
    val agentGroups = remember(regular) {
        regular.groupBy { it.source.ifBlank { it.brand.wire }.ifBlank { "agent" } }
            .map { (source, groupSessions) ->
                val label = groupSessions.firstOrNull()?.agentLabel
                    ?.ifBlank { directory.profile(source).label }
                    ?: directory.profile(source).label
                Triple(source, label.ifBlank { "Agent" }, hierarchicalSessions(groupSessions))
            }
            .sortedByDescending { (_, _, groupSessions) ->
                groupSessions.firstOrNull()?.session?.lastTs.orEmpty()
            }
    }
    val timeline = remember(regular) {
        hierarchicalSessions(regular.sortedByDescending { it.lastTs })
    }
    val allVisibleIds = remember(localMatches) { localMatches.map { it.sessionId }.toSet() }

    LaunchedEffect(searchOpen) {
        if (searchOpen) {
            delay(140)
            runCatching { searchFocusRequester.requestFocus() }
        }
    }
    LaunchedEffect(query) {
        if (query.isBlank()) viewModel.clearSessionSearch()
    }
    LaunchedEffect(selectionMode) {
        if (!selectionMode) selectedSessionIds = emptySet()
    }

    fun offerUndo(message: String, undo: () -> Unit) {
        scope.launch {
            rowSnackbar.currentSnackbarData?.dismiss()
            val result = rowSnackbar.showSnackbar(
                message = message,
                actionLabel = "撤销",
                withDismissAction = false,
                duration = SnackbarDuration.Short,
            )
            if (result == SnackbarResult.ActionPerformed) {
                undo()
            }
        }
    }

    fun handleRowAction(session: SessionSummary, action: SessionRowAction) {
        val id = session.sessionId
        when (action) {
            SessionRowAction.PIN -> {
                viewModel.setPinned(id, true)
                offerUndo("已置顶") { viewModel.setPinned(id, false) }
            }
            SessionRowAction.UNPIN -> {
                viewModel.setPinned(id, false)
                offerUndo("已取消置顶") { viewModel.setPinned(id, true) }
            }
            SessionRowAction.RESIDENT -> {
                viewModel.setSessionResident(id, true)
            }
            SessionRowAction.UNRESIDENT -> {
                viewModel.setSessionResident(id, false)
            }
            SessionRowAction.MUTE -> {
                viewModel.setMuted(id, true)
                offerUndo("已静音") { viewModel.setMuted(id, false) }
            }
            SessionRowAction.UNMUTE -> {
                viewModel.setMuted(id, false)
                offerUndo("已恢复通知") { viewModel.setMuted(id, true) }
            }
            SessionRowAction.ARCHIVE -> {
                viewModel.setArchived(id, true)
                offerUndo("已归档") { viewModel.setArchived(id, false) }
            }
            SessionRowAction.HIDE_ON_PHONE -> {
                viewModel.hideSessionOnPhone(session)
            }
            SessionRowAction.MARK_READ -> {
                viewModel.markSessionRead(id)
                scope.launch {
                    rowSnackbar.currentSnackbarData?.dismiss()
                    rowSnackbar.showSnackbar("已标为已读")
                }
            }
        }
    }

    val runningVisible = localMatches.count { it.status == SessionStatus.RUNNING }
    val waitingVisible = localMatches.count { it.matches(SessionStatusFilter.NEEDS_YOU) }
    val summary = buildList {
        add("${localMatches.size} 个会话")
        add(
            if (mobileInbox.enabled) {
                "${formatInboxStart(mobileInbox.sinceMillis)} 起"
            } else {
                "${mobileInbox.residentSessionIds.size} 个常驻"
            },
        )
        if (runningVisible > 0) add("$runningVisible 运行中")
        if (waitingVisible > 0) add("$waitingVisible 等你")
        if (selectedAgents.isNotEmpty()) add("${selectedAgents.size} 个 Agent 筛选")
    }.joinToString(" · ")

    Box(modifier = Modifier.fillMaxSize()) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .statusBarsPadding(),
        ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(start = 16.dp, end = 7.dp, top = 5.dp, bottom = 3.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = "会话",
                    style = MaterialTheme.typography.headlineSmall.copy(
                        fontSize = 25.sp,
                        fontWeight = FontWeight.SemiBold,
                    ),
                    color = HomingPalette.Text,
                )
                Text(
                    text = summary,
                    style = MaterialTheme.typography.labelSmall,
                    color = HomingPalette.Muted,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            IconButton(
                onClick = {
                    searchOpen = !searchOpen
                    if (!searchOpen) {
                        query = ""
                        viewModel.clearSessionSearch()
                    }
                },
                modifier = Modifier
                    .size(44.dp)
                    .testTag("projects-search-toggle"),
            ) {
                Icon(
                    imageVector = if (searchOpen) Icons.Outlined.Close else Icons.Outlined.Search,
                    contentDescription = if (searchOpen) "关闭搜索" else "搜索会话",
                    tint = if (searchOpen) HomingPalette.Brand else HomingPalette.Body,
                )
            }
            IconButton(
                onClick = { filterSheetOpen = true },
                modifier = Modifier
                    .size(44.dp)
                    .testTag("projects-filter-toggle"),
            ) {
                Icon(
                    Icons.Outlined.FilterList,
                    contentDescription = "筛选 Agent",
                    tint = if (selectedAgents.isEmpty()) HomingPalette.Body else HomingPalette.Brand,
                )
            }
            TextButton(
                onClick = {
                    if (selectionMode) {
                        selectionMode = false
                    } else {
                        selectionMode = true
                    }
                },
                enabled = localMatches.isNotEmpty() || selectionMode,
                modifier = Modifier.height(44.dp),
            ) {
                Text(if (selectionMode) "取消" else "选择")
            }
        }

        if (searchOpen) {
            OutlinedTextField(
                value = query,
                onValueChange = {
                    query = it
                    if (it.isBlank()) viewModel.clearSessionSearch()
                },
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp, vertical = 2.dp)
                    .focusRequester(searchFocusRequester)
                    .testTag("projects-search"),
                singleLine = true,
                placeholder = {
                    Text(
                        text = "标题、项目、路径；回车搜正文",
                        style = MaterialTheme.typography.bodySmall,
                    )
                },
                leadingIcon = {
                    Icon(
                        Icons.Outlined.Search,
                        contentDescription = null,
                        modifier = Modifier.size(20.dp),
                    )
                },
                trailingIcon = {
                    if (query.isNotBlank()) {
                        IconButton(onClick = {
                            query = ""
                            viewModel.clearSessionSearch()
                        }) {
                            Icon(Icons.Outlined.Close, contentDescription = "清除搜索")
                        }
                    }
                },
                shape = RoundedCornerShape(14.dp),
                colors = OutlinedTextFieldDefaults.colors(
                    focusedContainerColor = HomingPalette.Card,
                    unfocusedContainerColor = HomingPalette.Card.copy(alpha = 0.7f),
                    focusedBorderColor = HomingPalette.Brand.copy(alpha = 0.7f),
                    unfocusedBorderColor = HomingPalette.NavBorder,
                ),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                keyboardActions = KeyboardActions(
                    onSearch = { viewModel.searchSessionContent(query) },
                ),
            )
        }

        LazyRow(
            modifier = Modifier.padding(vertical = 3.dp),
            contentPadding = PaddingValues(horizontal = 16.dp),
            horizontalArrangement = Arrangement.spacedBy(7.dp),
        ) {
            item(key = "inbox-mode") {
                Surface(
                    onClick = { viewModel.setInboxMode(!mobileInbox.enabled) },
                    color = if (mobileInbox.enabled) {
                        HomingPalette.Brand.copy(alpha = 0.14f)
                    } else {
                        HomingPalette.Raised.copy(alpha = 0.72f)
                    },
                    contentColor = if (mobileInbox.enabled) {
                        HomingPalette.Brand
                    } else {
                        HomingPalette.Body
                    },
                    shape = RoundedCornerShape(50),
                    modifier = Modifier
                        .height(38.dp)
                        .testTag("projects-inbox-mode"),
                ) {
                    Row(
                        modifier = Modifier.padding(horizontal = 12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        Icon(
                            Icons.Outlined.Notifications,
                            contentDescription = null,
                            modifier = Modifier.size(15.dp),
                        )
                        Text(
                            text = if (mobileInbox.enabled) "收件中" else "收件箱",
                            style = MaterialTheme.typography.labelMedium,
                        )
                    }
                }
            }
            item(key = "grouping") {
                Box {
                    Surface(
                        onClick = { groupingMenuExpanded = true },
                        color = HomingPalette.Raised.copy(alpha = 0.72f),
                        contentColor = HomingPalette.Body,
                        shape = RoundedCornerShape(50),
                        modifier = Modifier
                            .height(38.dp)
                            .testTag("session-grouping-toggle"),
                    ) {
                        Row(
                            modifier = Modifier.padding(start = 12.dp, end = 8.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(4.dp),
                        ) {
                            Text(
                                text = when (grouping) {
                                    SessionGrouping.PROJECT -> "按项目"
                                    SessionGrouping.AGENT -> "按 Agent"
                                    SessionGrouping.TIME -> "按时间"
                                },
                                style = MaterialTheme.typography.labelMedium,
                            )
                            Icon(
                                Icons.Outlined.ExpandMore,
                                contentDescription = null,
                                modifier = Modifier.size(16.dp),
                            )
                        }
                    }
                    DropdownMenu(
                        expanded = groupingMenuExpanded,
                        onDismissRequest = { groupingMenuExpanded = false },
                    ) {
                        SessionGrouping.entries.forEach { option ->
                            DropdownMenuItem(
                                text = {
                                    Text(
                                        when (option) {
                                            SessionGrouping.PROJECT -> "按项目"
                                            SessionGrouping.AGENT -> "按 Agent"
                                            SessionGrouping.TIME -> "按时间"
                                        },
                                    )
                                },
                                trailingIcon = {
                                    if (option == grouping) {
                                        Icon(
                                            Icons.Outlined.Check,
                                            contentDescription = null,
                                            modifier = Modifier.size(17.dp),
                                        )
                                    }
                                },
                                onClick = {
                                    grouping = option
                                    groupingMenuExpanded = false
                                },
                            )
                        }
                    }
                }
            }
            item(key = "residents") {
                Surface(
                    onClick = { residentPickerOpen = true },
                    color = HomingPalette.Raised.copy(alpha = 0.72f),
                    contentColor = HomingPalette.Body,
                    shape = RoundedCornerShape(50),
                    modifier = Modifier
                        .height(38.dp)
                        .testTag("projects-residents"),
                ) {
                    Row(
                        modifier = Modifier.padding(horizontal = 12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        Icon(
                            Icons.Outlined.Bookmark,
                            contentDescription = null,
                            modifier = Modifier.size(15.dp),
                        )
                        Text(
                            text = "常驻 ${mobileInbox.residentSessionIds.size}",
                            style = MaterialTheme.typography.labelMedium,
                        )
                    }
                }
            }
            items(SessionStatusFilter.entries, key = { "status-${it.wire}" }) { option ->
                ProjectFilterChip(
                    label = option.label,
                    selected = status == option,
                    onClick = { statusFilter = option.wire },
                )
            }
        }

        PhoneCleanupControls(
            canUndo = state.phoneConversations.canUndo,
            hasHiddenSessions = state.phoneConversations.hasHiddenSessions,
            onUndo = viewModel::undoPhoneCleanup,
            onRestore = viewModel::restorePhoneSessions,
        )
        LazyColumn(
            modifier = Modifier
                .weight(1f)
                .padding(horizontal = 16.dp)
                .testTag("projects-list"),
            contentPadding = PaddingValues(top = 3.dp, bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(11.dp),
        ) {
            if (query.isNotBlank() && searchNotice.isNotBlank()) {
                item(key = "content-search-notice") {
                    SessionSearchNoticeCard(
                        message = searchNotice,
                        onOpenSettings = onOpenSettings,
                    )
                }
            }
            if (query.isNotBlank() && visibleSearchHits.isNotEmpty()) {
                item(key = "content-search") {
                    SearchResultsCard(
                        query = query,
                        hits = visibleSearchHits,
                        sessions = sessions,
                        onOpenSession = onOpenSession,
                    )
                }
            }

            if (localMatches.isEmpty() && temporary.isEmpty()) {
                item(key = "empty") {
                    val hasFilters = query.isNotBlank() ||
                        selectedAgents.isNotEmpty() ||
                        status != SessionStatusFilter.ALL
                    if (hasFilters) {
                        ProjectEmptyState(hasFilters = true)
                    } else {
                        ProjectInboxEmptyState(
                            inboxEnabled = mobileInbox.enabled,
                            residentCount = mobileInbox.residentSessionIds.size,
                            onEnableInbox = { viewModel.setInboxMode(true) },
                            onChooseResidents = { residentPickerOpen = true },
                            onNewSession = { onNewSession("") },
                        )
                    }
                }
            }
            if (grouping == SessionGrouping.PROJECT) {
                items(projectGroups, key = { "project-${it.first}" }) { (cwd, nodes) ->
                    ProjectCard(
                        cwd = cwd,
                        nodes = nodes,
                        selectionMode = selectionMode,
                        selectedIds = selectedSessionIds,
                        residentIds = mobileInbox.residentSessionIds,
                        onSelect = { session ->
                            selectedSessionIds = selectedSessionIds.toggle(session.sessionId)
                        },
                        onOpenSession = onOpenSession,
                        onNewSession = { onNewSession(cwd.takeUnless { it == "__unassigned__" }.orEmpty()) },
                        onRowAction = ::handleRowAction,
                    )
                }
            } else if (grouping == SessionGrouping.AGENT) {
                items(agentGroups, key = { "agent-group-${it.first}" }) { (_, label, nodes) ->
                    SessionListCard(
                        title = label,
                        subtitle = "${nodes.size} 个会话",
                        nodes = nodes,
                        selectionMode = selectionMode,
                        selectedIds = selectedSessionIds,
                        residentIds = mobileInbox.residentSessionIds,
                        onSelect = { session ->
                            selectedSessionIds = selectedSessionIds.toggle(session.sessionId)
                        },
                        onOpenSession = onOpenSession,
                        onRowAction = ::handleRowAction,
                    )
                }
            } else if (timeline.isNotEmpty()) {
                item(key = "timeline") {
                    SessionListCard(
                        title = "最近会话",
                        subtitle = "${timeline.size} 个会话",
                        nodes = timeline,
                        selectionMode = selectionMode,
                        selectedIds = selectedSessionIds,
                        residentIds = mobileInbox.residentSessionIds,
                        onSelect = { session ->
                            selectedSessionIds = selectedSessionIds.toggle(session.sessionId)
                        },
                        onOpenSession = onOpenSession,
                        onRowAction = ::handleRowAction,
                    )
                }
            }
            if (temporary.isNotEmpty()) {
                item(key = "temporary") {
                    TemporarySessionsCard(
                        nodes = hierarchicalSessions(temporary),
                        expanded = temporaryExpanded,
                        onToggle = { temporaryExpanded = !temporaryExpanded },
                        selectionMode = selectionMode,
                        selectedIds = selectedSessionIds,
                        residentIds = mobileInbox.residentSessionIds,
                        onSelect = { session ->
                            selectedSessionIds = selectedSessionIds.toggle(session.sessionId)
                        },
                        onOpenSession = onOpenSession,
                        onRowAction = ::handleRowAction,
                    )
                }
            }
            if (archivedSessions.isNotEmpty()) {
                item(key = "archived") {
                    ArchivedSessionsCard(
                        sessions = archivedSessions,
                        expanded = archivedExpanded,
                        onToggle = { archivedExpanded = !archivedExpanded },
                        onOpenSession = onOpenSession,
                        onRestore = { session ->
                            viewModel.setArchived(session.sessionId, false)
                        },
                    )
                }
            }
        }

        if (selectionMode) {
            SelectionActionBar(
                selectedCount = selectedSessionIds.size,
                allSelected = selectedSessionIds.isNotEmpty() &&
                    selectedSessionIds.containsAll(allVisibleIds),
                onSelectAll = {
                    selectedSessionIds = if (selectedSessionIds.containsAll(allVisibleIds)) {
                        emptySet()
                    } else {
                        allVisibleIds
                    }
                },
                onMarkRead = {
                    selectedSessionIds.forEach(viewModel::markSessionRead)
                    selectionMode = false
                },
                onHideOnPhone = {
                    viewModel.hideSessionsOnPhone(sessions.filter { it.sessionId in selectedSessionIds })
                    selectionMode = false
                },
                onMute = {
                    selectedSessionIds.forEach { viewModel.setMuted(it, true) }
                    selectionMode = false
                },
            )
        }
    }
        SnackbarHost(
            hostState = rowSnackbar,
            modifier = Modifier
                .align(Alignment.BottomCenter)
                .padding(horizontal = 16.dp, vertical = 76.dp),
        )
    }

    if (filterSheetOpen) {
        SessionAgentFilterSheet(
            choices = agentChoices,
            selected = selectedAgents,
            onToggle = { source ->
                selectedAgents = selectedAgents.toggle(source)
            },
            onAll = { selectedAgents = emptySet() },
            onDismiss = { filterSheetOpen = false },
        )
    }
    if (residentPickerOpen) {
        ResidentSessionSheet(
            sessions = allSavedSessions,
            selectedIds = mobileInbox.residentSessionIds,
            onToggle = { sessionId, selected ->
                viewModel.setSessionResident(sessionId, selected)
            },
            onDismiss = { residentPickerOpen = false },
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SessionAgentFilterSheet(
    choices: List<AgentFilterChoice>,
    selected: Set<String>,
    onToggle: (String) -> Unit,
    onAll: () -> Unit,
    onDismiss: () -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        containerColor = HomingPalette.Background,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(bottom = 18.dp),
        ) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 18.dp, end = 8.dp, bottom = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "筛选 Agent",
                        style = MaterialTheme.typography.titleMedium,
                        color = HomingPalette.Text,
                    )
                    Text(
                        text = if (selected.isEmpty()) {
                            "当前显示全部 Agent"
                        } else {
                            "已选择 ${selected.size} 个 Agent"
                        },
                        style = MaterialTheme.typography.labelSmall,
                        color = HomingPalette.Muted,
                    )
                }
                TextButton(onClick = onAll, enabled = selected.isNotEmpty()) {
                    Text("全部")
                }
                IconButton(onClick = onDismiss) {
                    Icon(Icons.Outlined.Close, contentDescription = "关闭筛选")
                }
            }
            LazyColumn(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(max = 520.dp)
                    .testTag("agent-filter-list"),
                contentPadding = PaddingValues(horizontal = 14.dp, vertical = 4.dp),
                verticalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                items(choices, key = { "filter-${it.source}" }) { choice ->
                    val checked = choice.source in selected
                    Surface(
                        onClick = { onToggle(choice.source) },
                        color = if (checked) {
                            HomingPalette.Brand.copy(alpha = 0.09f)
                        } else {
                            Color.Transparent
                        },
                        shape = RoundedCornerShape(11.dp),
                        modifier = Modifier
                            .fillMaxWidth()
                            .testTag("agent-filter-${choice.source}"),
                    ) {
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .height(48.dp)
                                .padding(horizontal = 10.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(10.dp),
                        ) {
                            AgentAvatar(
                                brand = AgentBrand.fromWire(choice.source),
                                source = choice.source,
                                size = 24.dp,
                            )
                            Text(
                                text = choice.label,
                                modifier = Modifier.weight(1f),
                                style = MaterialTheme.typography.bodyMedium,
                                color = if (checked) HomingPalette.Brand else HomingPalette.Text,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                            if (checked) {
                                Icon(
                                    Icons.Outlined.Check,
                                    contentDescription = null,
                                    modifier = Modifier.size(18.dp),
                                    tint = HomingPalette.Brand,
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ResidentSessionSheet(
    sessions: List<SessionSummary>,
    selectedIds: Set<String>,
    onToggle: (String, Boolean) -> Unit,
    onDismiss: () -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        containerColor = HomingPalette.Background,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(bottom = 18.dp),
        ) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 18.dp, end = 8.dp, bottom = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "常驻会话",
                        style = MaterialTheme.typography.titleMedium,
                        color = HomingPalette.Text,
                    )
                    Text(
                        text = "常驻会话会一直出现在手机，不受收件箱时间限制",
                        style = MaterialTheme.typography.labelSmall,
                        color = HomingPalette.Muted,
                    )
                }
                IconButton(onClick = onDismiss) {
                    Icon(Icons.Outlined.Close, contentDescription = "关闭常驻会话")
                }
            }
            LazyColumn(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(max = 560.dp)
                    .testTag("resident-session-list"),
                contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                verticalArrangement = Arrangement.spacedBy(3.dp),
            ) {
                items(sessions, key = { "resident-${it.sessionId}" }) { session ->
                    val checked = session.sessionId in selectedIds
                    Surface(
                        onClick = { onToggle(session.sessionId, !checked) },
                        color = if (checked) {
                            HomingPalette.Brand.copy(alpha = 0.08f)
                        } else {
                            Color.Transparent
                        },
                        shape = RoundedCornerShape(11.dp),
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(horizontal = 10.dp, vertical = 8.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(9.dp),
                        ) {
                            AgentAvatar(
                                brand = session.brand,
                                source = session.source,
                                size = 30.dp,
                            )
                            Column(modifier = Modifier.weight(1f)) {
                                Text(
                                    text = session.title.ifBlank { session.sessionId },
                                    style = MaterialTheme.typography.bodyMedium,
                                    color = HomingPalette.Text,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                                Text(
                                    text = "${session.agentLabel} · ${session.project}",
                                    style = MaterialTheme.typography.labelSmall.copy(
                                        fontSize = 10.5.sp,
                                    ),
                                    color = HomingPalette.Muted,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                            Checkbox(
                                checked = checked,
                                onCheckedChange = { selected ->
                                    onToggle(session.sessionId, selected)
                                },
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun ProjectInboxEmptyState(
    inboxEnabled: Boolean,
    residentCount: Int,
    onEnableInbox: () -> Unit,
    onChooseResidents: () -> Unit,
    onNewSession: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 18.dp, vertical = 46.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(
            modifier = Modifier
                .size(48.dp)
                .clip(CircleShape)
                .background(HomingPalette.Raised),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                Icons.Outlined.Bookmark,
                contentDescription = null,
                modifier = Modifier.size(23.dp),
                tint = HomingPalette.Brand,
            )
        }
        Text(
            text = if (inboxEnabled) "还没有新会话" else "手机不显示历史会话",
            modifier = Modifier.padding(top = 13.dp),
            style = MaterialTheme.typography.titleMedium,
            color = HomingPalette.Text,
        )
        Text(
            text = if (inboxEnabled) {
                "开启后出现的活动会自动进入这里；常驻会话会一直保留。"
            } else if (residentCount > 0) {
                "已有 $residentCount 个常驻会话。开启收件箱后，新活动会自动出现。"
            } else {
                "开启收件箱后只显示新动态，也可以从历史里挑选会话常驻。"
            },
            modifier = Modifier
                .padding(top = 6.dp)
                .padding(horizontal = 6.dp),
            style = MaterialTheme.typography.bodySmall,
            color = HomingPalette.Muted,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
        )
        Row(
            modifier = Modifier.padding(top = 16.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            if (!inboxEnabled) {
                Button(
                    onClick = onEnableInbox,
                    shape = RoundedCornerShape(10.dp),
                ) {
                    Text("开启收件箱")
                }
            }
            OutlinedButton(
                onClick = onChooseResidents,
                shape = RoundedCornerShape(10.dp),
            ) {
                Text("选择常驻")
            }
            TextButton(onClick = onNewSession) {
                Text("新会话")
            }
        }
    }
}

private fun Set<String>.toggle(id: String): Set<String> {
    return if (id in this) this - id else this + id
}

private fun SessionSummary.matches(filter: SessionStatusFilter): Boolean {
    return when (filter) {
        SessionStatusFilter.ALL -> true
        SessionStatusFilter.RUNNING -> status == SessionStatus.RUNNING
        SessionStatusFilter.NEEDS_YOU -> pending || status == SessionStatus.NEEDS_PERMISSION ||
            status == SessionStatus.WAITING_INPUT || status == SessionStatus.ERROR
        SessionStatusFilter.COLLAB -> collab?.role?.isNotBlank() == true
        SessionStatusFilter.UNREAD -> unreadCount > 0 || !read
    }
}

private fun hierarchicalSessions(sessions: List<SessionSummary>): List<SessionRowNode> {
    val byId = sessions.associateBy { it.sessionId }
    val children = sessions
        .filter { it.collab?.isChild == true }
        .groupBy { it.collab?.parentSessionId.orEmpty() }
    val roots = sessions.filter { session ->
        val parentId = session.collab?.parentSessionId.orEmpty()
        session.collab?.isChild != true || parentId !in byId
    }
    val result = mutableListOf<SessionRowNode>()
    val seen = mutableSetOf<String>()

    fun append(session: SessionSummary, depth: Int) {
        if (!seen.add(session.sessionId)) return
        result += SessionRowNode(session, depth.coerceAtMost(2))
        children[session.sessionId].orEmpty().forEach { child ->
            append(child, depth + 1)
        }
    }

    roots.forEach { append(it, 0) }
    sessions.filter { it.sessionId !in seen }.forEach { append(it, 0) }
    return result
}

@Composable
private fun ProjectFilterChip(
    label: String,
    selected: Boolean,
    onClick: () -> Unit,
) {
    FilterChip(
        selected = selected,
        onClick = onClick,
        label = {
            Text(
                text = label,
                style = MaterialTheme.typography.labelMedium,
                maxLines = 1,
            )
        },
        shape = RoundedCornerShape(50),
        colors = FilterChipDefaults.filterChipColors(
            containerColor = Color.Transparent,
            selectedContainerColor = HomingPalette.Brand.copy(alpha = 0.14f),
            selectedLabelColor = HomingPalette.Brand,
        ),
    )
}

@Composable
private fun ProjectCard(
    cwd: String,
    nodes: List<SessionRowNode>,
    selectionMode: Boolean,
    selectedIds: Set<String>,
    residentIds: Set<String>,
    onSelect: (SessionSummary) -> Unit,
    onOpenSession: (String) -> Unit,
    onNewSession: () -> Unit,
    onRowAction: (SessionSummary, SessionRowAction) -> Unit,
) {
    val first = nodes.firstOrNull()?.session ?: return
    val agents = nodes.map { it.session }.distinctBy { it.source }
    Surface(
        color = HomingPalette.Card,
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(1.dp, HomingPalette.NavBorder),
    ) {
        Column {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 14.dp, end = 8.dp, top = 10.dp, bottom = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = first.project.ifBlank { "未命名项目" },
                        style = MaterialTheme.typography.titleMedium.copy(
                            fontSize = 16.sp,
                            fontWeight = FontWeight.SemiBold,
                        ),
                        color = HomingPalette.Text,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        text = buildString {
                            first.branch.takeIf { it.isNotBlank() }?.let {
                                append("⑂ ")
                                append(it)
                            }
                            if (isNotEmpty()) append(" · ")
                            append("${nodes.size} 个会话")
                            append(" · ")
                            append(relativeTime(first.lastTs).ifBlank { "暂无活动" })
                        },
                        modifier = Modifier.padding(top = 3.dp),
                        style = MaterialTheme.typography.labelSmall.copy(
                            fontFamily = HomingMono,
                            fontSize = 11.sp,
                        ),
                        color = HomingPalette.Muted,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                StackedAgentBadges(agents = agents)
                TextButton(
                    onClick = onNewSession,
                    enabled = cwd != "__unassigned__",
                    modifier = Modifier.height(44.dp),
                ) {
                    Text("新建", fontSize = 12.sp)
                }
            }
            HorizontalDivider(color = HomingPalette.NavBorder)
            nodes.forEachIndexed { index, node ->
                if (index > 0) {
                    HorizontalDivider(
                        modifier = Modifier.padding(start = 14.dp),
                        color = HomingPalette.NavBorder,
                    )
                }
                ProjectSessionRow(
                    node = node,
                    selectionMode = selectionMode,
                    selected = node.session.sessionId in selectedIds,
                    isResident = node.session.sessionId in residentIds,
                    onSelect = { onSelect(node.session) },
                    onClick = { onOpenSession(node.session.sessionId) },
                    onAction = onRowAction,
                )
            }
            if (cwd != "__unassigned__") {
                Text(
                    text = cwd,
                    modifier = Modifier.padding(horizontal = 14.dp, vertical = 5.dp),
                    style = MaterialTheme.typography.labelSmall.copy(
                        fontFamily = HomingMono,
                        fontSize = 10.sp,
                    ),
                    color = HomingPalette.Muted.copy(alpha = 0.7f),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

@Composable
private fun SessionListCard(
    title: String,
    subtitle: String,
    nodes: List<SessionRowNode>,
    selectionMode: Boolean,
    selectedIds: Set<String>,
    residentIds: Set<String>,
    onSelect: (SessionSummary) -> Unit,
    onOpenSession: (String) -> Unit,
    onRowAction: (SessionSummary, SessionRowAction) -> Unit,
) {
    Surface(
        color = HomingPalette.Card,
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(1.dp, HomingPalette.NavBorder),
    ) {
        Column {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 14.dp, vertical = 11.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = title,
                    modifier = Modifier.weight(1f),
                    style = MaterialTheme.typography.titleMedium,
                    color = HomingPalette.Text,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = subtitle,
                    style = MaterialTheme.typography.labelSmall,
                    color = HomingPalette.Muted,
                )
            }
            HorizontalDivider(color = HomingPalette.NavBorder)
            nodes.forEachIndexed { index, node ->
                if (index > 0) {
                    HorizontalDivider(
                        modifier = Modifier.padding(start = 14.dp),
                        color = HomingPalette.NavBorder,
                    )
                }
                ProjectSessionRow(
                    node = node,
                    selectionMode = selectionMode,
                    selected = node.session.sessionId in selectedIds,
                    isResident = node.session.sessionId in residentIds,
                    onSelect = { onSelect(node.session) },
                    onClick = { onOpenSession(node.session.sessionId) },
                    onAction = onRowAction,
                )
            }
        }
    }
}

@Composable
private fun ProjectSessionRow(
    node: SessionRowNode,
    selectionMode: Boolean,
    selected: Boolean,
    isResident: Boolean,
    onSelect: () -> Unit,
    onClick: () -> Unit,
    onAction: (SessionSummary, SessionRowAction) -> Unit,
) {
    val session = node.session
    var menuExpanded by remember(session.sessionId) { mutableStateOf(false) }
    val row: @Composable (() -> Unit) = {
        ProjectSessionRowContent(
            node = node,
            session = session,
            selected = selected,
            selectionMode = selectionMode,
            isResident = isResident,
            onClick = { if (selectionMode) onSelect() else onClick() },
            onLongClick = if (selectionMode) null else ({ menuExpanded = true }),
            onHidePhone = { onAction(session, SessionRowAction.HIDE_ON_PHONE) },
        )
    }

    if (selectionMode) {
        row()
        return
    }

    val dismissState = rememberSwipeToDismissBoxState()
    val scope = rememberCoroutineScope()
    val showSwipeBackground =
        dismissState.dismissDirection != SwipeToDismissBoxValue.Settled
    Box {
        SwipeToDismissBox(
            state = dismissState,
            backgroundContent = {
                if (showSwipeBackground) {
                    SwipeSessionRowBackground(session)
                }
            },
            enableDismissFromStartToEnd = true,
            enableDismissFromEndToStart = true,
            onDismiss = { value ->
                when (value) {
                    SwipeToDismissBoxValue.StartToEnd ->
                        onAction(
                            session,
                            if (session.pinned) SessionRowAction.UNPIN else SessionRowAction.PIN,
                        )
                    SwipeToDismissBoxValue.EndToStart ->
                        onAction(session, SessionRowAction.HIDE_ON_PHONE)
                    SwipeToDismissBoxValue.Settled -> Unit
                }
                scope.launch { dismissState.reset() }
            },
        ) {
            row()
        }
        DropdownMenu(
            expanded = menuExpanded,
            onDismissRequest = { menuExpanded = false },
        ) {
            DropdownMenuItem(
                text = { Text("仅从手机移除") },
                leadingIcon = { Icon(Icons.Outlined.Close, contentDescription = null) },
                onClick = {
                    menuExpanded = false
                    onAction(session, SessionRowAction.HIDE_ON_PHONE)
                },
            )
            DropdownMenuItem(
                text = { Text(if (session.pinned) "取消置顶" else "置顶会话") },
                leadingIcon = { Icon(Icons.Outlined.PushPin, contentDescription = null) },
                onClick = {
                    menuExpanded = false
                    onAction(
                        session,
                        if (session.pinned) SessionRowAction.UNPIN else SessionRowAction.PIN,
                    )
                },
            )
            DropdownMenuItem(
                text = {
                    Text(
                        if (isResident) {
                            "取消常驻"
                        } else {
                            "设为常驻"
                        },
                    )
                },
                leadingIcon = {
                    Icon(Icons.Outlined.Bookmark, contentDescription = null)
                },
                onClick = {
                    menuExpanded = false
                    onAction(
                        session,
                        if (isResident) {
                            SessionRowAction.UNRESIDENT
                        } else {
                            SessionRowAction.RESIDENT
                        },
                    )
                },
            )
            DropdownMenuItem(
                text = { Text(if (session.muted) "恢复通知" else "静音会话") },
                leadingIcon = {
                    Icon(
                        if (session.muted) Icons.Outlined.Notifications else Icons.Outlined.NotificationsOff,
                        contentDescription = null,
                    )
                },
                onClick = {
                    menuExpanded = false
                    onAction(
                        session,
                        if (session.muted) SessionRowAction.UNMUTE else SessionRowAction.MUTE,
                    )
                },
            )
            DropdownMenuItem(
                text = { Text("标为已读") },
                leadingIcon = { Icon(Icons.Outlined.Check, contentDescription = null) },
                onClick = {
                    menuExpanded = false
                    onAction(session, SessionRowAction.MARK_READ)
                },
            )
            DropdownMenuItem(
                text = { Text("归档会话") },
                leadingIcon = { Icon(Icons.Outlined.Archive, contentDescription = null) },
                onClick = {
                    menuExpanded = false
                    onAction(session, SessionRowAction.ARCHIVE)
                },
            )
        }
    }
}

@Composable
private fun ProjectSessionRowContent(
    node: SessionRowNode,
    session: SessionSummary,
    selected: Boolean,
    selectionMode: Boolean,
    isResident: Boolean,
    onClick: () -> Unit,
    onLongClick: (() -> Unit)?,
    onHidePhone: () -> Unit,
) {
    Surface(
        color = if (selected) {
            HomingPalette.Brand.copy(alpha = 0.08f)
        } else {
            HomingPalette.Card
        },
        modifier = Modifier
            .fillMaxWidth()
            .clipToBounds()
            .testTag("session-row-${session.sessionId}"),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = 62.dp)
                .combinedClickable(
                    onClick = onClick,
                    onLongClick = onLongClick,
                )
                .padding(
                    start = 14.dp + (node.depth * 18).dp,
                    end = 12.dp,
                    top = 8.dp,
                    bottom = 8.dp,
                ),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(9.dp),
        ) {
            if (selectionMode) {
                Checkbox(
                    checked = selected,
                    onCheckedChange = { onClick() },
                )
            } else {
                Box(
                    modifier = Modifier
                        .size(7.dp)
                        .clip(CircleShape)
                        .background(statusDotColor(session.status)),
                )
            }
            Column(modifier = Modifier.weight(1f)) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(7.dp),
                ) {
                    if (node.depth > 0) {
                        Text(
                            text = "↳",
                            style = MaterialTheme.typography.labelMedium,
                            color = HomingPalette.Muted,
                        )
                    }
                    Text(
                        text = session.title,
                        modifier = Modifier.weight(1f),
                        style = MaterialTheme.typography.bodyMedium.copy(
                            fontSize = 14.sp,
                            fontWeight = if (session.unreadCount > 0) {
                                FontWeight.SemiBold
                            } else {
                                FontWeight.Medium
                            },
                        ),
                        color = HomingPalette.Text,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    if (session.unreadCount > 0) {
                        Box(
                            modifier = Modifier
                                .size(18.dp)
                                .clip(CircleShape)
                                .background(HomingPalette.Brand),
                            contentAlignment = Alignment.Center,
                        ) {
                            Text(
                                text = if (session.unreadCount > 9) {
                                    "9+"
                                } else {
                                    session.unreadCount.toString()
                                },
                                color = HomingPalette.Background,
                                fontSize = 9.sp,
                                fontWeight = FontWeight.Bold,
                            )
                        }
                    }
                }
                Row(
                    modifier = Modifier.padding(top = 3.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(5.dp),
                ) {
                    AgentAvatar(
                        brand = session.brand,
                        source = session.source,
                        size = 18.dp,
                    )
                    Text(
                        text = session.agentLabel.ifBlank {
                            directoryLabel(session.source.ifBlank { session.brand.wire })
                        },
                        modifier = Modifier.weight(1f),
                        style = MaterialTheme.typography.labelSmall.copy(fontSize = 10.5.sp),
                        color = accentForSource(session.source),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    if (isResident) {
                        Icon(
                            Icons.Outlined.Bookmark,
                            contentDescription = "常驻",
                            modifier = Modifier.size(13.dp),
                            tint = HomingPalette.Brand,
                        )
                    }
                    if (session.pinned) {
                        Icon(
                            Icons.Outlined.PushPin,
                            contentDescription = "置顶",
                            modifier = Modifier.size(13.dp),
                            tint = HomingPalette.Warning,
                        )
                    }
                    Text(
                        text = relativeTime(session.lastTs).ifBlank { "—" },
                        style = MaterialTheme.typography.labelSmall.copy(fontSize = 10.5.sp),
                        color = HomingPalette.Muted,
                        maxLines = 1,
                    )
                }
            }
            if (!selectionMode) {
                IconButton(
                    onClick = onHidePhone,
                    modifier = Modifier.testTag("phone-hide-session-${session.sessionId}"),
                ) {
                    Icon(
                        Icons.Outlined.Close,
                        contentDescription = "仅从手机移除：${session.title}",
                        tint = HomingPalette.Muted,
                        modifier = Modifier.size(18.dp),
                    )
                }
            }
        }
    }
}

@Composable
private fun SwipeSessionRowBackground(session: SessionSummary) {
    Row(
        modifier = Modifier
            .fillMaxSize()
            .background(HomingPalette.Card)
            .padding(horizontal = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        SwipeActionHint(
            label = if (session.pinned) "取消置顶" else "置顶",
            icon = Icons.Outlined.PushPin,
            color = HomingPalette.Brand,
        )
        SwipeActionHint(
            label = "仅从手机移除",
            icon = Icons.Outlined.Close,
            color = HomingPalette.Error,
        )
    }
}

@Composable
private fun SwipeActionHint(
    label: String,
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    color: Color,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(5.dp),
    ) {
        Icon(icon, contentDescription = null, modifier = Modifier.size(16.dp), tint = color)
        Text(label, style = MaterialTheme.typography.labelSmall, color = color)
    }
}

@Composable
private fun StackedAgentBadges(agents: List<SessionSummary>) {
    if (agents.isEmpty()) return
    Row(horizontalArrangement = Arrangement.spacedBy((-5).dp)) {
        agents.take(3).forEach { session ->
            AgentAvatar(
                brand = session.brand,
                source = session.source,
                size = 22.dp,
            )
        }
    }
}

@Composable
private fun TemporarySessionsCard(
    nodes: List<SessionRowNode>,
    expanded: Boolean,
    onToggle: () -> Unit,
    selectionMode: Boolean,
    selectedIds: Set<String>,
    residentIds: Set<String>,
    onSelect: (SessionSummary) -> Unit,
    onOpenSession: (String) -> Unit,
    onRowAction: (SessionSummary, SessionRowAction) -> Unit,
) {
    Surface(
        color = HomingPalette.Card,
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(1.dp, HomingPalette.NavBorder),
    ) {
        Column {
            Surface(onClick = onToggle, color = Color.Transparent) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(56.dp)
                        .padding(horizontal = 14.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        text = "临时会话 ${nodes.size} 个",
                        modifier = Modifier.weight(1f),
                        style = MaterialTheme.typography.labelLarge,
                        color = HomingPalette.Body,
                    )
                    Icon(
                        imageVector = if (expanded) {
                            Icons.Outlined.ExpandLess
                        } else {
                            Icons.Outlined.ExpandMore
                        },
                        contentDescription = if (expanded) "收起临时会话" else "展开临时会话",
                        tint = HomingPalette.Muted,
                    )
                }
            }
            if (expanded) {
                HorizontalDivider(color = HomingPalette.NavBorder)
                nodes.forEachIndexed { index, node ->
                    if (index > 0) {
                        HorizontalDivider(
                            modifier = Modifier.padding(start = 14.dp),
                            color = HomingPalette.NavBorder,
                        )
                    }
                    ProjectSessionRow(
                        node = node,
                        selectionMode = selectionMode,
                        selected = node.session.sessionId in selectedIds,
                        isResident = node.session.sessionId in residentIds,
                        onSelect = { onSelect(node.session) },
                        onClick = { onOpenSession(node.session.sessionId) },
                        onAction = onRowAction,
                    )
                }
            }
        }
    }
}

@Composable
private fun ArchivedSessionsCard(
    sessions: List<SessionSummary>,
    expanded: Boolean,
    onToggle: () -> Unit,
    onOpenSession: (String) -> Unit,
    onRestore: (SessionSummary) -> Unit,
) {
    Surface(
        color = HomingPalette.Card,
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(1.dp, HomingPalette.NavBorder),
    ) {
        Column {
            Surface(onClick = onToggle, color = Color.Transparent) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(56.dp)
                        .padding(horizontal = 14.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        text = "已归档 ${sessions.size} 个",
                        modifier = Modifier.weight(1f),
                        style = MaterialTheme.typography.labelLarge,
                        color = HomingPalette.Muted,
                    )
                    Icon(
                        imageVector = if (expanded) {
                            Icons.Outlined.ExpandLess
                        } else {
                            Icons.Outlined.ExpandMore
                        },
                        contentDescription = if (expanded) "收起归档" else "展开归档",
                        tint = HomingPalette.Muted,
                    )
                }
            }
            if (expanded) {
                HorizontalDivider(color = HomingPalette.NavBorder)
                sessions.forEachIndexed { index, session ->
                    if (index > 0) {
                        HorizontalDivider(
                            modifier = Modifier.padding(start = 14.dp),
                            color = HomingPalette.NavBorder,
                        )
                    }
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .height(56.dp)
                            .padding(horizontal = 14.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        Text(
                            text = session.title,
                            modifier = Modifier
                                .weight(1f)
                                .clickable { onOpenSession(session.sessionId) },
                            style = MaterialTheme.typography.bodyMedium,
                            color = HomingPalette.Muted,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        TextButton(
                            onClick = { onRestore(session) },
                            modifier = Modifier.height(48.dp),
                        ) {
                            Text("恢复")
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun SearchResultsCard(
    query: String,
    hits: List<SessionSearchHit>,
    sessions: List<SessionSummary>,
    onOpenSession: (String) -> Unit,
) {
    val byId = remember(sessions) { sessions.associateBy { it.sessionId } }
    Surface(
        color = HomingPalette.Card,
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(1.dp, HomingPalette.Brand.copy(alpha = 0.45f)),
    ) {
        Column {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 14.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(
                    Icons.Outlined.Search,
                    contentDescription = null,
                    tint = HomingPalette.Brand,
                    modifier = Modifier.size(18.dp),
                )
                Text(
                    text = "正文命中 ${hits.size} 处",
                    modifier = Modifier
                        .weight(1f)
                        .padding(start = 8.dp),
                    style = MaterialTheme.typography.labelLarge,
                    color = HomingPalette.Text,
                )
            }
            HorizontalDivider(color = HomingPalette.NavBorder)
            hits.forEachIndexed { index, hit ->
                if (index > 0) {
                    HorizontalDivider(
                        modifier = Modifier.padding(start = 14.dp),
                        color = HomingPalette.NavBorder,
                    )
                }
                val session = byId[hit.sessionId]
                Surface(
                    onClick = { onOpenSession(hit.sessionId) },
                    color = Color.Transparent,
                ) {
                    Column(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 14.dp, vertical = 10.dp),
                    ) {
                        Text(
                            text = session?.title
                                ?: hit.title.ifBlank { hit.sessionId },
                            style = MaterialTheme.typography.bodyMedium,
                            color = HomingPalette.Text,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        Text(
                            text = highlightSnippet(hit.snippet.ifBlank { query }, query),
                            modifier = Modifier.padding(top = 3.dp),
                            style = MaterialTheme.typography.bodySmall.copy(
                                fontFamily = HomingMono,
                                fontSize = 11.sp,
                            ),
                            color = HomingPalette.Muted,
                            maxLines = 3,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun SessionSearchNoticeCard(
    message: String,
    onOpenSettings: () -> Unit,
) {
    Surface(
        color = HomingPalette.Card,
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(1.dp, HomingPalette.Warning.copy(alpha = 0.55f)),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 14.dp, vertical = 12.dp),
            verticalAlignment = Alignment.Top,
            horizontalArrangement = Arrangement.spacedBy(9.dp),
        ) {
            Icon(
                Icons.Outlined.Search,
                contentDescription = null,
                tint = HomingPalette.Warning,
                modifier = Modifier
                    .padding(top = 1.dp)
                    .size(18.dp),
            )
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = "无法读取正文搜索",
                    style = MaterialTheme.typography.labelLarge,
                    color = HomingPalette.Text,
                )
                Text(
                    text = message,
                    modifier = Modifier.padding(top = 3.dp),
                    style = MaterialTheme.typography.bodySmall,
                    color = HomingPalette.Muted,
                )
            }
            TextButton(
                onClick = onOpenSettings,
                modifier = Modifier.height(32.dp),
            ) {
                Text("打开设置", style = MaterialTheme.typography.labelSmall)
            }
        }
    }
}

private fun highlightSnippet(text: String, query: String): AnnotatedString {
    val needle = query.trim()
    if (needle.isBlank()) return AnnotatedString(text)
    val start = text.indexOf(needle, ignoreCase = true)
    if (start < 0) return AnnotatedString(text)
    return buildAnnotatedString {
        append(text.substring(0, start))
        withStyle(
            SpanStyle(
                background = HomingPalette.Warning.copy(alpha = 0.28f),
                color = HomingPalette.Text,
                fontWeight = FontWeight.SemiBold,
            ),
        ) {
            append(text.substring(start, start + needle.length))
        }
        append(text.substring(start + needle.length))
    }
}

@Composable
private fun SelectionActionBar(
    selectedCount: Int,
    allSelected: Boolean,
    onSelectAll: () -> Unit,
    onMarkRead: () -> Unit,
    onHideOnPhone: () -> Unit,
    onMute: () -> Unit,
) {
    Surface(
        color = HomingPalette.NavBackground,
        border = BorderStroke(1.dp, HomingPalette.NavBorder),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 10.dp, vertical = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            Text(
                text = "已选 $selectedCount",
                modifier = Modifier.padding(horizontal = 5.dp),
                style = MaterialTheme.typography.labelMedium,
                color = HomingPalette.Muted,
            )
            SelectionAction(
                label = if (allSelected) "取消全选" else "全选",
                icon = Icons.Outlined.SelectAll,
                enabled = selectedCount > 0 || !allSelected,
                onClick = onSelectAll,
                modifier = Modifier.weight(1f),
            )
            SelectionAction(
                label = "已读",
                icon = Icons.Outlined.Check,
                enabled = selectedCount > 0,
                onClick = onMarkRead,
                modifier = Modifier.weight(1f),
            )
            SelectionAction(
                label = "仅从手机移除",
                icon = Icons.Outlined.Close,
                enabled = selectedCount > 0,
                onClick = onHideOnPhone,
                modifier = Modifier.weight(1f),
            )
            SelectionAction(
                label = "静音",
                icon = Icons.Outlined.NotificationsOff,
                enabled = selectedCount > 0,
                onClick = onMute,
                modifier = Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun SelectionAction(
    label: String,
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    TextButton(
        onClick = onClick,
        enabled = enabled,
        modifier = modifier.height(48.dp),
    ) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Icon(icon, contentDescription = null, modifier = Modifier.size(18.dp))
            Text(label, fontSize = 10.sp)
        }
    }
}

@Composable
private fun ProjectEmptyState(hasFilters: Boolean) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 70.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = if (hasFilters) "没有匹配的会话" else "还没有项目",
            style = MaterialTheme.typography.titleMedium,
            color = HomingPalette.Text,
        )
        Text(
            text = if (hasFilters) {
                "清除搜索或 Agent / 状态筛选后再看。"
            } else {
                "在 Mac 上开始一个 agent 会话，项目会自动出现在这里。"
            },
            modifier = Modifier.padding(top = 6.dp),
            style = MaterialTheme.typography.bodySmall,
            color = HomingPalette.Muted,
        )
    }
}

@Composable
private fun statusDotColor(status: SessionStatus): Color {
    return when (status) {
        SessionStatus.RUNNING -> HomingPalette.Brand
        SessionStatus.WAITING_INPUT,
        SessionStatus.NEEDS_PERMISSION,
        -> HomingPalette.Warning
        SessionStatus.ERROR -> HomingPalette.Error
        SessionStatus.COMPLETED -> HomingPalette.Added
        SessionStatus.IDLE -> HomingPalette.Muted
    }
}

private fun accentForSource(source: String): Color {
    val palette = listOf(
        Color(0xFF4ADE80),
        Color(0xFFFB923C),
        Color(0xFF60A5FA),
        Color(0xFFC084FC),
        Color(0xFFF472B6),
        Color(0xFF2DD4BF),
        Color(0xFFFACC15),
    )
    val normalized = source.ifBlank { "agent" }
    return palette[normalized.hashCode().absoluteValue % palette.size]
}

private fun directoryLabel(source: String): String {
    if (source.isBlank()) return "Agent"
    return when (source.lowercase()) {
        "codex" -> "Codex"
        "claude" -> "Claude"
        "workbuddy" -> "WorkBuddy"
        else -> source.replace('-', ' ').replace('_', ' ')
            .split(' ')
            .filter(String::isNotBlank)
            .joinToString(" ") { word ->
                word.replaceFirstChar { it.uppercase() }
            }
            .ifBlank { "Agent" }
    }
}

private fun SessionSummary.isTemporaryWorkspace(): Boolean {
    val normalized = cwd.replace('\\', '/')
    return normalized.startsWith("/tmp/") ||
        normalized == "/tmp" ||
        normalized.startsWith("/private/tmp/") ||
        normalized.startsWith("/var/folders/")
}
