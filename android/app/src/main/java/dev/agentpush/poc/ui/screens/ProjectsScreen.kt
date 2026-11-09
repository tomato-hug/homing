package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.FilterChip
import androidx.compose.material3.FilterChipDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.agentpush.poc.AgentBrand
import dev.agentpush.poc.AgentViewModel
import dev.agentpush.poc.SessionGrouping
import dev.agentpush.poc.SessionStatus
import dev.agentpush.poc.SessionSummary
import dev.agentpush.poc.relativeTime
import dev.agentpush.poc.ui.AgentAvatar
import dev.agentpush.poc.ui.SessionStatusBadge
import dev.agentpush.poc.ui.accentColor
import dev.agentpush.poc.ui.theme.HomingMono
import dev.agentpush.poc.ui.theme.HomingPalette

private data class ProjectAgentFilter(
    val wire: String,
    val label: String,
)

private val projectAgentFilters = listOf(
    ProjectAgentFilter("", "全部"),
    ProjectAgentFilter("codex", "Codex"),
    ProjectAgentFilter("claude", "Claude"),
    ProjectAgentFilter("workbuddy", "WorkBuddy"),
)

@Composable
fun ProjectsScreen(
    viewModel: AgentViewModel,
    onOpenSession: (String) -> Unit,
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    var grouping by rememberSaveable { mutableStateOf(SessionGrouping.PROJECT) }
    var agentFilter by rememberSaveable { mutableStateOf("") }
    var temporaryExpanded by rememberSaveable { mutableStateOf(false) }
    var archivedExpanded by rememberSaveable { mutableStateOf(false) }
    val sourceSessions = remember(state) { state.sessions() }
    val allSessions = remember(sourceSessions, agentFilter) {
        sourceSessions
            .filterNot { it.archived || it.isEmpty }
            .filter { agentFilter.isBlank() || it.brand.wire == agentFilter }
    }
    val archivedSessions = remember(sourceSessions, agentFilter) {
        sourceSessions
            .filter { it.archived && !it.isEmpty }
            .filter { agentFilter.isBlank() || it.brand.wire == agentFilter }
            .sortedByDescending { it.lastTs }
    }
    val (temporary, regular) = remember(allSessions) {
        allSessions.partition { it.isTemp || it.isTemporaryWorkspace() }
    }
    val projectGroups = remember(regular) {
        regular.groupBy { it.cwd.ifBlank { "__unassigned__" } }
            .map { (key, sessions) -> key to sessions.sortedByDescending { it.lastTs } }
            .sortedByDescending { (_, sessions) -> sessions.firstOrNull()?.lastTs.orEmpty() }
    }
    val timeline = remember(regular) {
        regular.sortedByDescending { it.lastTs }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .statusBarsPadding(),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp, vertical = 9.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = "项目",
                modifier = Modifier.weight(1f),
                style = MaterialTheme.typography.displaySmall.copy(fontSize = 28.sp),
                color = HomingPalette.Text,
            )
            Text(
                text = "${regular.size} 个会话",
                style = MaterialTheme.typography.labelSmall,
                color = HomingPalette.Muted,
            )
        }
        ProjectSegmentedControl(
            grouping = grouping,
            onChange = { grouping = it },
        )
        LazyRow(
            modifier = Modifier.padding(vertical = 6.dp),
            contentPadding = PaddingValues(horizontal = 16.dp),
            horizontalArrangement = Arrangement.spacedBy(7.dp),
        ) {
            items(projectAgentFilters, key = { it.wire.ifBlank { "all" } }) { filter ->
                val count = if (filter.wire.isBlank()) {
                    state.sessions().count { !it.archived }
                } else {
                    state.sessions().count { !it.archived && it.brand.wire == filter.wire }
                }
                FilterChip(
                    selected = agentFilter == filter.wire,
                    onClick = { agentFilter = filter.wire },
                    label = {
                        Text(
                            text = if (filter.wire.isBlank()) {
                                "${filter.label} $count"
                            } else {
                                filter.label
                            },
                            style = MaterialTheme.typography.labelMedium,
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
        }
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(horizontal = 16.dp)
                .testTag("projects-list"),
            contentPadding = PaddingValues(top = 4.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(11.dp),
        ) {
            if (regular.isEmpty() && temporary.isEmpty()) {
                item(key = "empty") {
                    ProjectEmptyState()
                }
            }
            if (grouping == SessionGrouping.PROJECT) {
                items(projectGroups, key = { "project-${it.first}" }) { (cwd, sessions) ->
                    ProjectCard(
                        cwd = cwd,
                        sessions = sessions,
                        onOpenSession = onOpenSession,
                    )
                }
            } else if (timeline.isNotEmpty()) {
                item(key = "timeline") {
                    TimelineCard(
                        sessions = timeline,
                        onOpenSession = onOpenSession,
                    )
                }
            }
            if (temporary.isNotEmpty()) {
                item(key = "temporary") {
                    TemporarySessionsCard(
                        sessions = temporary,
                        expanded = temporaryExpanded,
                        onToggle = { temporaryExpanded = !temporaryExpanded },
                        onOpenSession = onOpenSession,
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
    }
}

@Composable
private fun ProjectSegmentedControl(
    grouping: SessionGrouping,
    onChange: (SessionGrouping) -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(HomingPalette.Raised.copy(alpha = 0.72f))
            .padding(3.dp),
    ) {
        SegmentButton(
            label = "按项目",
            selected = grouping == SessionGrouping.PROJECT,
            modifier = Modifier.weight(1f),
            onClick = { onChange(SessionGrouping.PROJECT) },
        )
        SegmentButton(
            label = "按时间",
            selected = grouping == SessionGrouping.TIME,
            modifier = Modifier.weight(1f),
            onClick = { onChange(SessionGrouping.TIME) },
        )
    }
}

@Composable
private fun SegmentButton(
    label: String,
    selected: Boolean,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
) {
    Surface(
        onClick = onClick,
        modifier = modifier.height(48.dp),
        shape = RoundedCornerShape(10.dp),
        color = if (selected) HomingPalette.Card else Color.Transparent,
        contentColor = if (selected) HomingPalette.Text else HomingPalette.Muted,
    ) {
        Box(contentAlignment = Alignment.Center) {
            Text(
                text = label,
                style = MaterialTheme.typography.labelLarge,
                fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Medium,
            )
        }
    }
}

@Composable
private fun ProjectCard(
    cwd: String,
    sessions: List<SessionSummary>,
    onOpenSession: (String) -> Unit,
) {
    val first = sessions.firstOrNull() ?: return
    val brands = sessions.map { it.brand }.distinct()
    Surface(
        color = HomingPalette.Card,
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(1.dp, HomingPalette.NavBorder),
    ) {
        Column {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 14.dp, vertical = 12.dp),
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
                            append("${sessions.size} 个会话")
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
                StackedAgents(brands = brands)
            }
            HorizontalDivider(color = HomingPalette.NavBorder)
            sessions.forEachIndexed { index, session ->
                if (index > 0) {
                    HorizontalDivider(
                        modifier = Modifier.padding(start = 14.dp),
                        color = HomingPalette.NavBorder,
                    )
                }
                ProjectSessionRow(
                    session = session,
                    onClick = { onOpenSession(session.sessionId) },
                )
            }
            Spacer(Modifier.height(2.dp))
            Text(
                text = cwd.ifBlank { "未提供 cwd" },
                modifier = Modifier.padding(horizontal = 14.dp, vertical = 4.dp),
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

@Composable
private fun TimelineCard(
    sessions: List<SessionSummary>,
    onOpenSession: (String) -> Unit,
) {
    Surface(
        color = HomingPalette.Card,
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(1.dp, HomingPalette.NavBorder),
    ) {
        Column {
            Text(
                text = "最近会话",
                modifier = Modifier.padding(horizontal = 14.dp, vertical = 12.dp),
                style = MaterialTheme.typography.titleMedium,
                color = HomingPalette.Text,
            )
            HorizontalDivider(color = HomingPalette.NavBorder)
            sessions.forEachIndexed { index, session ->
                if (index > 0) {
                    HorizontalDivider(
                        modifier = Modifier.padding(start = 14.dp),
                        color = HomingPalette.NavBorder,
                    )
                }
                ProjectSessionRow(
                    session = session,
                    onClick = { onOpenSession(session.sessionId) },
                )
            }
        }
    }
}

@Composable
private fun ProjectSessionRow(
    session: SessionSummary,
    onClick: () -> Unit,
) {
    Surface(
        onClick = onClick,
        color = Color.Transparent,
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .height(56.dp)
                .padding(horizontal = 14.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(9.dp),
        ) {
            Box(
                modifier = Modifier
                    .size(7.dp)
                    .clip(CircleShape)
                    .background(statusDotColor(session.status)),
            )
            Text(
                text = session.title,
                modifier = Modifier.weight(1f),
                style = MaterialTheme.typography.bodyMedium.copy(
                    fontSize = 14.sp,
                    fontWeight = FontWeight.Medium,
                ),
                color = HomingPalette.Text,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Surface(
                color = session.brand.accentColor().copy(alpha = 0.12f),
                shape = RoundedCornerShape(50),
            ) {
                Text(
                    text = session.brand.displayName,
                    modifier = Modifier.padding(horizontal = 7.dp, vertical = 2.dp),
                    style = MaterialTheme.typography.labelSmall,
                    color = session.brand.accentColor(),
                    maxLines = 1,
                )
            }
            Text(
                text = relativeTime(session.lastTs).ifBlank { "—" },
                style = MaterialTheme.typography.labelSmall,
                color = HomingPalette.Muted,
                maxLines = 1,
            )
        }
    }
}

@Composable
private fun StackedAgents(brands: List<AgentBrand>) {
    if (brands.isEmpty()) return
    Box(
        modifier = Modifier
            .width(20.dp + ((brands.size - 1).coerceAtLeast(0) * 12).dp)
            .height(20.dp),
    ) {
        brands.forEachIndexed { index, brand ->
            AgentAvatar(
                brand = brand,
                size = 20.dp,
                modifier = Modifier.offset(x = (index * 12).dp),
            )
        }
    }
}

@Composable
private fun TemporarySessionsCard(
    sessions: List<SessionSummary>,
    expanded: Boolean,
    onToggle: () -> Unit,
    onOpenSession: (String) -> Unit,
) {
    Surface(
        onClick = onToggle,
        color = HomingPalette.Card,
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(1.dp, HomingPalette.NavBorder),
    ) {
        Column {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(56.dp)
                    .padding(horizontal = 14.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = "临时会话 ${sessions.size} 个",
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
            if (expanded) {
                HorizontalDivider(color = HomingPalette.NavBorder)
                sessions.forEachIndexed { index, session ->
                    if (index > 0) {
                        HorizontalDivider(
                            modifier = Modifier.padding(start = 14.dp),
                            color = HomingPalette.NavBorder,
                        )
                    }
                    ProjectSessionRow(
                        session = session,
                        onClick = { onOpenSession(session.sessionId) },
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
            Surface(
                onClick = onToggle,
                color = Color.Transparent,
            ) {
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
private fun ProjectEmptyState() {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 70.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = "还没有项目",
            style = MaterialTheme.typography.titleMedium,
            color = HomingPalette.Text,
        )
        Text(
            text = "在 Mac 上开始一个 agent 会话，项目会自动出现在这里。",
            modifier = Modifier.padding(top = 6.dp),
            style = MaterialTheme.typography.bodySmall,
            color = HomingPalette.Muted,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
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

private fun SessionSummary.isTemporaryWorkspace(): Boolean {
    val normalized = cwd.replace('\\', '/')
    return normalized.startsWith("/tmp/") ||
        normalized == "/tmp" ||
        normalized.startsWith("/private/tmp/") ||
        normalized.startsWith("/var/folders/")
}
