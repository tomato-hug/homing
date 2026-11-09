package dev.agentpush.poc.ui.screens

import android.content.Intent
import android.widget.Toast
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.FolderOpen
import androidx.compose.material.icons.outlined.Inbox
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.FilterChip
import androidx.compose.material3.FilterChipDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.agentpush.poc.AgentBrand
import dev.agentpush.poc.AgentViewModel
import dev.agentpush.poc.ConnectionDiagnostics
import dev.agentpush.poc.ConnectionState
import dev.agentpush.poc.CrashReporter
import dev.agentpush.poc.SessionGroup
import dev.agentpush.poc.SessionGrouping
import dev.agentpush.poc.SessionQuickAction
import dev.agentpush.poc.SessionStatus
import dev.agentpush.poc.SessionSummary
import dev.agentpush.poc.formatDiagnosticTime
import dev.agentpush.poc.groupSessions
import dev.agentpush.poc.relativeTime
import dev.agentpush.poc.quickAction
import dev.agentpush.poc.sessionSummaryLine
import dev.agentpush.poc.ui.AgentGlyph
import dev.agentpush.poc.ui.ConnectionPill
import dev.agentpush.poc.ui.OfflineBanner
import dev.agentpush.poc.ui.SessionStatusBadge
import dev.agentpush.poc.ui.SessionTagStrip
import dev.agentpush.poc.ui.UnreadBadge
import dev.agentpush.poc.ui.statusDisplayColor

private data class AgentFilter(
    val wire: String,
    val label: String,
    val brand: AgentBrand?,
)

private val agentFilters = listOf(
    AgentFilter("", "全部", null),
    AgentFilter("codex", "Codex", AgentBrand.CODEX),
    AgentFilter("claude", "Claude", AgentBrand.CLAUDE),
    AgentFilter("workbuddy", "WorkBuddy", AgentBrand.WORKBUDDY),
)

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun InboxScreen(
    viewModel: AgentViewModel,
    snackbarHostState: SnackbarHostState,
    onOpenSession: (String) -> Unit,
    onOpenSettings: () -> Unit,
    onNewSession: () -> Unit,
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    var agentFilter by rememberSaveable { mutableStateOf("") }
    var grouping by rememberSaveable { mutableStateOf(SessionGrouping.PROJECT) }
    var search by rememberSaveable { mutableStateOf("") }

    val allSessions = remember(
        state.events,
        state.replies,
        state.sessionMeta,
        state.transcriptCache,
        state.sessionApiAvailable,
    ) {
        state.sessions()
    }
    val searchedSessions = remember(allSessions, search) {
        val query = search.trim()
        if (query.isBlank()) {
            allSessions
        } else {
            allSessions.filter { session ->
                session.title.contains(query, ignoreCase = true) ||
                    session.project.contains(query, ignoreCase = true) ||
                    session.workspace.contains(query, ignoreCase = true) ||
                    session.cwd.contains(query, ignoreCase = true) ||
                    session.branch.contains(query, ignoreCase = true) ||
                    sessionSummaryLine(session).contains(query, ignoreCase = true)
            }
        }
    }
    val sessions = remember(searchedSessions, agentFilter) {
        if (agentFilter.isBlank()) {
            searchedSessions
        } else {
            searchedSessions.filter { it.brand.wire == agentFilter }
        }
    }
    val groups = remember(sessions, grouping) {
        groupSessions(sessions, grouping)
    }
    val pendingCount = allSessions.count { it.pending }
    val unreadCount = allSessions.sumOf { it.unreadCount }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbarHostState) },
        containerColor = MaterialTheme.colorScheme.background,
        floatingActionButton = {
            ExtendedFloatingActionButton(
                onClick = onNewSession,
                icon = { Icon(Icons.Outlined.Add, contentDescription = null) },
                text = { Text("新建任务") },
                shape = RoundedCornerShape(16.dp),
                containerColor = MaterialTheme.colorScheme.primary,
                contentColor = MaterialTheme.colorScheme.onPrimary,
            )
        },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding),
        ) {
            InboxHeader(
                connection = state.connection,
                diagnostics = state.connectionDiagnostics,
                server = state.settings.baseUrl,
                away = state.away,
                unreadCount = unreadCount,
                pendingCount = pendingCount,
                onAwayChange = viewModel::setAway,
                onMarkAllRead = viewModel::markAllRead,
                onSettings = onOpenSettings,
            )
            LastCrashCard(
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
            )
            AgentFilterRow(
                selected = agentFilter,
                sessions = searchedSessions,
                onSelect = { agentFilter = it },
            )
            SearchField(
                value = search,
                onValueChange = { search = it },
            )
            SessionListControls(
                sessionCount = sessions.size,
                grouping = grouping,
                onGrouping = { grouping = it },
            )
            if (state.connection != ConnectionState.CONNECTED) {
                OfflineBanner(
                    connection = state.connection,
                    onRetry = viewModel::reconnect,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                    reason = state.connectionDiagnostics.lastDisconnectReason,
                )
            }

            PullToRefreshBox(
                isRefreshing = state.isRefreshing,
                onRefresh = viewModel::refresh,
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f),
            ) {
                LazyColumn(
                    modifier = Modifier.fillMaxSize(),
                    contentPadding = PaddingValues(
                        start = 16.dp,
                        end = 16.dp,
                        top = 5.dp,
                        bottom = 96.dp,
                    ),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    if (sessions.isEmpty()) {
                        item {
                            InboxEmptyState(
                                modifier = Modifier.fillParentMaxSize(),
                                connection = state.connection,
                                isRefreshing = state.isRefreshing,
                                hasAny = allSessions.isNotEmpty(),
                                onRetry = viewModel::reconnect,
                                onClearFilter = {
                                    agentFilter = ""
                                    search = ""
                                },
                            )
                        }
                    } else {
                        groups.forEach { group ->
                            if (grouping == SessionGrouping.PROJECT) {
                                item(key = "group-${group.key}") {
                                    ProjectGroupHeader(group)
                                }
                            }
                            items(group.sessions, key = { it.sessionId }) { session ->
                                SwipeableSessionCard(
                                    session = session,
                                    onOpen = { onOpenSession(session.sessionId) },
                                    onMarkRead = {
                                        viewModel.markSessionRead(session.sessionId)
                                    },
                                    onApprove = { viewModel.approve(session) },
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun InboxHeader(
    connection: ConnectionState,
    diagnostics: ConnectionDiagnostics,
    server: String,
    away: Boolean,
    unreadCount: Int,
    pendingCount: Int,
    onAwayChange: (Boolean) -> Unit,
    onMarkAllRead: () -> Unit,
    onSettings: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 16.dp, end = 10.dp, top = 12.dp, bottom = 2.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = "收件箱",
                style = MaterialTheme.typography.headlineMedium,
                color = MaterialTheme.colorScheme.onBackground,
                modifier = Modifier.weight(1f),
            )
            TextButton(
                onClick = onMarkAllRead,
                enabled = unreadCount > 0,
                contentPadding = PaddingValues(horizontal = 8.dp, vertical = 4.dp),
            ) {
                Icon(
                    Icons.Outlined.Check,
                    contentDescription = null,
                    modifier = Modifier.size(16.dp),
                )
                Spacer(Modifier.width(3.dp))
                Text("全部已读", style = MaterialTheme.typography.labelMedium)
            }
            IconButton(onClick = onSettings) {
                Icon(
                    imageVector = Icons.Outlined.Settings,
                    contentDescription = "设置",
                    tint = MaterialTheme.colorScheme.onSurface,
                )
            }
        }
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            ConnectionPill(
                connection = connection,
                server = server,
                modifier = Modifier.weight(1f, fill = false),
            )
            Spacer(Modifier.weight(1f))
            Text(
                text = "离开",
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Switch(
                checked = away,
                onCheckedChange = onAwayChange,
                modifier = Modifier.size(width = 42.dp, height = 28.dp),
            )
        }
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 3.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = buildString {
                    if (unreadCount > 0) append("未读 $unreadCount")
                    if (pendingCount > 0) {
                        if (isNotEmpty()) append(" · ")
                        append("需要你处理 $pendingCount")
                    }
                    if (isEmpty()) append("没有未读会话")
                },
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.weight(1f),
            )
            Text(
                text = "连接 ${formatDiagnosticTime(diagnostics.lastConnectedAt)}",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.outline,
                maxLines = 1,
            )
        }
        Text(
            text = "断开 ${formatDiagnosticTime(diagnostics.lastDisconnectedAt)}" +
                (diagnostics.lastDisconnectReason?.let { " · ${it.take(60)}" } ?: ""),
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.outline,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

@Composable
internal fun LastCrashCard(modifier: Modifier = Modifier) {
    var report by remember { mutableStateOf(CrashReporter.loadLastReport()) }
    val current = report ?: return
    var expanded by rememberSaveable { mutableStateOf(false) }
    val context = LocalContext.current
    val clipboard = LocalClipboardManager.current

    fun share() {
        val intent = Intent(Intent.ACTION_SEND).apply {
            type = "text/plain"
            putExtra(Intent.EXTRA_SUBJECT, "Homing 崩溃报告")
            putExtra(Intent.EXTRA_TEXT, CrashReporter.shareText(current))
        }
        context.startActivity(Intent.createChooser(intent, "分享崩溃报告"))
    }

    Surface(
        modifier = modifier.fillMaxWidth(),
        color = MaterialTheme.colorScheme.errorContainer.copy(alpha = 0.35f),
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.error.copy(alpha = 0.35f)),
    ) {
        Column(modifier = Modifier.padding(horizontal = 12.dp, vertical = 8.dp)) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(
                    imageVector = Icons.Outlined.WarningAmber,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.error,
                    modifier = Modifier.size(18.dp),
                )
                Spacer(Modifier.width(7.dp))
                Text(
                    text = "上次崩溃 · ${current.appVersion.ifBlank { "未知版本" }}",
                    modifier = Modifier.weight(1f),
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                TextButton(onClick = { expanded = !expanded }) {
                    Text(if (expanded) "收起" else "展开")
                }
            }
            Text(
                text = listOf(
                    current.ts,
                    current.device,
                    "session ${current.sessionId.ifBlank { "(none)" }}",
                ).joinToString(" · "),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = if (expanded) 4 else 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (expanded) {
                SelectionContainer {
                    Text(
                        text = current.stack,
                        modifier = Modifier
                            .fillMaxWidth()
                            .heightIn(max = 260.dp)
                            .verticalScroll(rememberScrollState())
                            .padding(top = 7.dp),
                        style = MaterialTheme.typography.bodySmall.copy(
                            fontSize = 11.sp,
                            lineHeight = 16.sp,
                            fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace,
                        ),
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                }
            }
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                TextButton(
                    onClick = {
                        clipboard.setText(AnnotatedString(CrashReporter.shareText(current)))
                        Toast.makeText(context, "崩溃日志已复制", Toast.LENGTH_SHORT).show()
                    },
                ) {
                    Text("复制")
                }
                TextButton(onClick = ::share) {
                    Text("分享")
                }
                Spacer(Modifier.weight(1f))
                TextButton(
                    onClick = {
                        CrashReporter.clearLastReport()
                        report = null
                    },
                ) {
                    Text("清除")
                }
            }
        }
    }
}

@Composable
private fun AgentFilterRow(
    selected: String,
    sessions: List<SessionSummary>,
    onSelect: (String) -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(horizontal = 16.dp, vertical = 5.dp),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        agentFilters.forEach { filter ->
            val count = if (filter.wire.isBlank()) {
                sessions.size
            } else {
                sessions.count { it.brand.wire == filter.wire }
            }
            FilterChip(
                selected = selected == filter.wire,
                onClick = { onSelect(filter.wire) },
                label = {
                    Text(
                        text = "${filter.label} $count",
                        style = MaterialTheme.typography.labelMedium,
                    )
                },
                leadingIcon = filter.brand?.let { brand ->
                    {
                        AgentGlyph(
                            brand = brand,
                            size = 15.dp,
                            tint = if (selected == filter.wire) {
                                MaterialTheme.colorScheme.onPrimaryContainer
                            } else {
                                MaterialTheme.colorScheme.onSurfaceVariant
                            },
                        )
                    }
                },
                shape = RoundedCornerShape(50),
                colors = FilterChipDefaults.filterChipColors(
                    containerColor = Color.Transparent,
                    labelColor = MaterialTheme.colorScheme.onSurfaceVariant,
                    selectedContainerColor = MaterialTheme.colorScheme.primaryContainer,
                    selectedLabelColor = MaterialTheme.colorScheme.onPrimaryContainer,
                    selectedLeadingIconColor = MaterialTheme.colorScheme.onPrimaryContainer,
                ),
                border = FilterChipDefaults.filterChipBorder(
                    enabled = true,
                    selected = selected == filter.wire,
                    borderColor = MaterialTheme.colorScheme.outlineVariant,
                    selectedBorderColor = Color.Transparent,
                ),
            )
        }
    }
}

@Composable
private fun SearchField(value: String, onValueChange: (String) -> Unit) {
    BasicTextField(
        value = value,
        onValueChange = onValueChange,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 3.dp),
        singleLine = true,
        textStyle = MaterialTheme.typography.bodyMedium.copy(
            color = MaterialTheme.colorScheme.onSurface,
        ),
        cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
        decorationBox = { innerTextField ->
            Surface(
                color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.62f),
                shape = RoundedCornerShape(10.dp),
                border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
            ) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(42.dp)
                        .padding(horizontal = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Icon(
                        Icons.Outlined.Search,
                        contentDescription = null,
                        modifier = Modifier.size(18.dp),
                        tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Spacer(Modifier.width(8.dp))
                    Box(modifier = Modifier.weight(1f)) {
                        if (value.isBlank()) {
                            Text(
                                text = "搜索标题、项目、工作区或摘要…",
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.outline,
                                maxLines = 1,
                            )
                        }
                        innerTextField()
                    }
                    if (value.isNotBlank()) {
                        IconButton(
                            onClick = { onValueChange("") },
                            modifier = Modifier.size(28.dp),
                        ) {
                            Icon(
                                Icons.Outlined.Close,
                                contentDescription = "清除搜索",
                                modifier = Modifier.size(16.dp),
                                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }
            }
        },
    )
}

@Composable
private fun SessionListControls(
    sessionCount: Int,
    grouping: SessionGrouping,
    onGrouping: (SessionGrouping) -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = "$sessionCount 个会话",
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.weight(1f),
        )
        GroupingChip(
            label = "按项目",
            selected = grouping == SessionGrouping.PROJECT,
            onClick = { onGrouping(SessionGrouping.PROJECT) },
        )
        Spacer(Modifier.width(5.dp))
        GroupingChip(
            label = "按时间",
            selected = grouping == SessionGrouping.TIME,
            onClick = { onGrouping(SessionGrouping.TIME) },
        )
    }
}

@Composable
private fun GroupingChip(label: String, selected: Boolean, onClick: () -> Unit) {
    Surface(
        onClick = onClick,
        shape = RoundedCornerShape(7.dp),
        color = if (selected) {
            MaterialTheme.colorScheme.primaryContainer
        } else {
            Color.Transparent
        },
        contentColor = if (selected) {
            MaterialTheme.colorScheme.onPrimaryContainer
        } else {
            MaterialTheme.colorScheme.onSurfaceVariant
        },
    ) {
        Text(
            text = label,
            modifier = Modifier.padding(horizontal = 9.dp, vertical = 5.dp),
            style = MaterialTheme.typography.labelSmall,
            fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Medium,
        )
    }
}

@Composable
private fun ProjectGroupHeader(group: SessionGroup) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        color = MaterialTheme.colorScheme.background.copy(alpha = 0.97f),
    ) {
        Column {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = 10.dp, bottom = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(7.dp),
            ) {
                Icon(
                    Icons.Outlined.FolderOpen,
                    contentDescription = null,
                    modifier = Modifier.size(16.dp),
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = group.project,
                        style = MaterialTheme.typography.labelLarge.copy(
                            fontSize = 13.sp,
                            fontWeight = FontWeight.SemiBold,
                        ),
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        text = buildString {
                            if (group.workspace.isNotBlank()) append(group.workspace)
                            if (group.branch.isNotBlank()) {
                                if (isNotEmpty()) append("  ")
                                append("⎇ ${group.branch}")
                            }
                            if (isEmpty()) append("未标注工作区")
                        },
                        style = MaterialTheme.typography.labelSmall.copy(
                            fontSize = 11.5.sp,
                            fontFamily = if (group.branch.isNotBlank()) {
                                androidx.compose.ui.text.font.FontFamily.Monospace
                            } else {
                                androidx.compose.ui.text.font.FontFamily.Default
                            },
                        ),
                        color = MaterialTheme.colorScheme.outline,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                Text(
                    text = "${group.sessions.size} 个",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.outline,
                )
            }
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(1.dp)
                    .background(MaterialTheme.colorScheme.outlineVariant),
            )
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SwipeableSessionCard(
    session: SessionSummary,
    onOpen: () -> Unit,
    onMarkRead: () -> Unit,
    onApprove: () -> Unit,
) {
    val dismissState = rememberSwipeToDismissBoxState(
        confirmValueChange = { value ->
            if (value == SwipeToDismissBoxValue.EndToStart && session.unreadCount > 0) {
                onMarkRead()
            }
            false
        },
        positionalThreshold = { distance -> distance * 0.28f },
    )
    SwipeToDismissBox(
        state = dismissState,
        enableDismissFromStartToEnd = false,
        enableDismissFromEndToStart = session.unreadCount > 0,
        backgroundContent = {
            Row(
                modifier = Modifier
                    .fillMaxSize()
                    .clip(RoundedCornerShape(12.dp))
                    .background(MaterialTheme.colorScheme.primaryContainer)
                    .padding(horizontal = 20.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.End,
            ) {
                Icon(Icons.Outlined.Check, contentDescription = null)
                Spacer(Modifier.width(6.dp))
                Text("标为已读", style = MaterialTheme.typography.labelMedium)
            }
        },
    ) {
        SessionCard(
            session = session,
            onOpen = onOpen,
            onApprove = onApprove,
        )
    }
}

@Composable
private fun SessionCard(
    session: SessionSummary,
    onOpen: () -> Unit,
    onApprove: () -> Unit,
) {
    val quickAction = session.status.quickAction()
    val borderColor = when (session.status) {
        SessionStatus.NEEDS_PERMISSION -> statusDisplayColor(session.status).copy(alpha = 0.36f)
        SessionStatus.WAITING_INPUT -> statusDisplayColor(session.status).copy(alpha = 0.25f)
        else -> MaterialTheme.colorScheme.outlineVariant
    }
    Surface(
        onClick = onOpen,
        modifier = Modifier.fillMaxWidth(),
        color = MaterialTheme.colorScheme.surface,
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(1.dp, borderColor),
        tonalElevation = 0.dp,
    ) {
        Column(modifier = Modifier.padding(horizontal = 12.dp, vertical = 11.dp)) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.Top,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                AgentGlyph(
                    brand = session.brand,
                    size = 18.dp,
                    modifier = Modifier.padding(top = 2.dp),
                    tint = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text = session.title,
                    modifier = Modifier.weight(1f),
                    style = TextStyle(
                        fontSize = 15.sp,
                        lineHeight = 20.sp,
                        fontWeight = FontWeight.Bold,
                        color = MaterialTheme.colorScheme.onSurface,
                    ),
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Spacer(Modifier.height(5.dp))
            SessionTagStrip(
                session = session,
                modifier = Modifier.padding(start = 26.dp),
                maxVisible = 3,
            )
            Spacer(Modifier.height(6.dp))
            Text(
                text = sessionSummaryLine(session),
                modifier = Modifier.padding(start = 26.dp),
                style = TextStyle(
                    fontSize = 12.5.sp,
                    lineHeight = 18.sp,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                ),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(Modifier.height(8.dp))
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 26.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(7.dp),
            ) {
                Text(
                    text = relativeTime(session.lastTs).ifBlank { "尚无活动" },
                    style = MaterialTheme.typography.labelSmall.copy(fontSize = 11.5.sp),
                    color = MaterialTheme.colorScheme.outline,
                    maxLines = 1,
                )
                if (session.unreadCount > 0) {
                    UnreadBadge(session.unreadCount)
                }
                Spacer(Modifier.weight(1f))
                SessionStatusBadge(status = session.status)
                QuickActionButton(
                    action = quickAction,
                    onClick = {
                        if (quickAction == SessionQuickAction.APPROVE) {
                            onApprove()
                        } else {
                            onOpen()
                        }
                    },
                )
            }
        }
    }
}

@Composable
private fun QuickActionButton(action: SessionQuickAction, onClick: () -> Unit) {
    val approvalColor = statusDisplayColor(SessionStatus.NEEDS_PERMISSION)
    val colors = when (action) {
        SessionQuickAction.APPROVE -> ButtonDefaults.buttonColors(
            containerColor = approvalColor.copy(alpha = 0.14f),
            contentColor = approvalColor,
        )
        SessionQuickAction.REPLY -> ButtonDefaults.buttonColors(
            containerColor = MaterialTheme.colorScheme.primaryContainer,
            contentColor = MaterialTheme.colorScheme.onPrimaryContainer,
        )
        SessionQuickAction.VIEW -> ButtonDefaults.buttonColors(
            containerColor = Color.Transparent,
            contentColor = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
    Button(
        onClick = onClick,
        modifier = Modifier.height(30.dp),
        shape = RoundedCornerShape(7.dp),
        contentPadding = PaddingValues(horizontal = 9.dp, vertical = 0.dp),
        colors = colors,
        elevation = ButtonDefaults.buttonElevation(0.dp, 0.dp, 0.dp),
    ) {
        Text(action.label, style = MaterialTheme.typography.labelSmall)
    }
}

@Composable
private fun InboxEmptyState(
    modifier: Modifier,
    connection: ConnectionState,
    isRefreshing: Boolean,
    hasAny: Boolean,
    onRetry: () -> Unit,
    onClearFilter: () -> Unit,
) {
    Box(modifier = modifier, contentAlignment = Alignment.Center) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(10.dp),
            modifier = Modifier.padding(horizontal = 24.dp, vertical = 40.dp),
        ) {
            if (isRefreshing) {
                CircularProgressIndicator(modifier = Modifier.size(28.dp), strokeWidth = 2.5.dp)
            } else {
                Box(
                    modifier = Modifier
                        .size(58.dp)
                        .clip(CircleShape)
                        .background(MaterialTheme.colorScheme.primaryContainer),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        imageVector = Icons.Outlined.Inbox,
                        contentDescription = null,
                        tint = MaterialTheme.colorScheme.primary,
                        modifier = Modifier.size(27.dp),
                    )
                }
            }
            Text(
                text = when {
                    isRefreshing -> "正在同步 Mac 上的会话"
                    connection != ConnectionState.CONNECTED -> "还没有连上 Mac"
                    hasAny -> "没有符合条件的会话"
                    else -> "这里还没有会话"
                },
                style = MaterialTheme.typography.titleMedium,
            )
            Text(
                text = when {
                    connection != ConnectionState.CONNECTED ->
                        "确认手机与 Mac 在同一局域网，检查地址与 token 后重试。"
                    hasAny -> "换一个 Agent 或搜索词试试。"
                    else -> "Claude Code、Codex 或 WorkBuddy 的会话会自动出现在这里。"
                },
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
            )
            if (!isRefreshing) {
                TextButton(
                    onClick = if (connection != ConnectionState.CONNECTED) onRetry else onClearFilter,
                ) {
                    Text(if (connection != ConnectionState.CONNECTED) "重新连接" else "清除筛选")
                }
            }
        }
    }
}
