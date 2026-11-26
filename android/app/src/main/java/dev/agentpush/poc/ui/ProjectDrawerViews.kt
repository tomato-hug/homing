package dev.agentpush.poc.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.KeyboardArrowDown
import androidx.compose.material.icons.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
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
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.agentpush.poc.AgentBrand
import dev.agentpush.poc.ProjectSessionGroup
import dev.agentpush.poc.SessionSummary
import dev.agentpush.poc.defaultAgentDisplayName
import dev.agentpush.poc.groupSessionsByProject
import dev.agentpush.poc.relativeTime
import dev.agentpush.poc.ui.theme.HomingPalette

/** Overlapping agent marks for a project that several agents worked in. */
@Composable
internal fun AgentGlyphStack(sources: List<String>, size: androidx.compose.ui.unit.Dp = 20.dp) {
    val shown = sources.take(3)
    Box(modifier = Modifier.width(size + (size * 0.55f) * (shown.size - 1).coerceAtLeast(0)).heightIn(min = size)) {
        shown.forEachIndexed { index, source ->
            Box(
                modifier = Modifier
                    .offset(x = size * 0.55f * index)
                    .size(size)
                    .clip(CircleShape)
                    .background(HomingPalette.NavBackground),
                contentAlignment = Alignment.Center,
            ) {
                AgentGlyph(brand = AgentBrand.fromWire(source), source = source, size = size * 0.86f)
            }
        }
    }
}

/** One project = one drawer entry; expands to the sessions inside it. */
@Composable
internal fun DrawerProjectRow(
    group: ProjectSessionGroup,
    activeSessionId: String,
    expanded: Boolean,
    onToggle: () -> Unit,
    onOpenSession: (SessionSummary) -> Unit,
) {
    val containsActive = group.sessions.any { it.sessionId == activeSessionId }
    Column(modifier = Modifier.fillMaxWidth().testTag("drawer-project-${group.name}")) {
        Surface(
            onClick = { onOpenSession(group.sessions.firstOrNull { it.sessionId == activeSessionId } ?: group.primary) },
            color = if (containsActive) HomingPalette.Raised else Color.Transparent,
            shape = RoundedCornerShape(10.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth().padding(start = 10.dp, end = 4.dp, top = 8.dp, bottom = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(9.dp),
            ) {
                AgentGlyphStack(group.sources)
                Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        Text(
                            text = group.name,
                            modifier = Modifier.weight(1f, fill = false),
                            style = TextStyle(
                                fontSize = 14.sp,
                                fontWeight = if (containsActive) FontWeight.SemiBold else FontWeight.Medium,
                                color = HomingPalette.Text,
                            ),
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        if (group.sessions.size > 1) {
                            Text(
                                text = "${group.sessions.size}",
                                modifier = Modifier
                                    .clip(RoundedCornerShape(999.dp))
                                    .background(HomingPalette.Card)
                                    .padding(horizontal = 6.dp, vertical = 1.dp),
                                style = TextStyle(fontSize = 10.5.sp, color = HomingPalette.Muted, fontWeight = FontWeight.SemiBold),
                            )
                        }
                        Spacer(Modifier.weight(1f))
                        Text(
                            text = relativeTime(group.primary.lastTs),
                            style = TextStyle(fontSize = 11.sp, color = HomingPalette.Muted),
                        )
                    }
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(5.dp)) {
                        Text(
                            text = group.primary.title,
                            modifier = Modifier.weight(1f),
                            style = TextStyle(fontSize = 11.5.sp, color = HomingPalette.Muted),
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        if (group.runningCount > 0) {
                            Box(Modifier.size(7.dp).clip(CircleShape).background(HomingPalette.Brand))
                        } else if (group.sessions.any { it.unreadCount > 0 }) {
                            Box(Modifier.size(7.dp).clip(CircleShape).background(HomingPalette.BrandSoft))
                        }
                    }
                }
                if (group.sessions.size > 1) {
                    IconButton(onClick = onToggle, modifier = Modifier.size(30.dp)) {
                        Icon(
                            if (expanded) Icons.Outlined.KeyboardArrowDown else Icons.Outlined.KeyboardArrowRight,
                            contentDescription = if (expanded) "收起项目会话" else "展开项目会话",
                            tint = HomingPalette.Muted,
                            modifier = Modifier.size(18.dp),
                        )
                    }
                } else {
                    Spacer(Modifier.width(6.dp))
                }
            }
        }
        if (expanded) {
            group.sessions.forEach { session ->
                val active = session.sessionId == activeSessionId
                Surface(
                    onClick = { onOpenSession(session) },
                    color = if (active) HomingPalette.Card else Color.Transparent,
                    shape = RoundedCornerShape(8.dp),
                    modifier = Modifier.padding(start = 30.dp),
                ) {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 8.dp, vertical = 6.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(7.dp),
                    ) {
                        AgentGlyph(brand = session.brand, source = session.source, size = 15.dp)
                        Text(
                            text = session.title,
                            modifier = Modifier.weight(1f),
                            style = TextStyle(
                                fontSize = 12.5.sp,
                                color = if (active) HomingPalette.Text else HomingPalette.Body,
                                fontWeight = if (active) FontWeight.SemiBold else FontWeight.Normal,
                            ),
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        if (session.running) {
                            Box(Modifier.size(6.dp).clip(CircleShape).background(HomingPalette.Brand))
                        }
                        Text(relativeTime(session.lastTs), style = TextStyle(fontSize = 10.5.sp, color = HomingPalette.Muted))
                    }
                }
            }
        }
    }
}

private const val SESSIONS_PER_PROJECT_RESTORE = 3

/**
 * History restore grouped by project. Selecting a project restores its most
 * recent sessions (each with its latest 10 records); expanding a project
 * allows picking individual sessions.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun RestoreHistoryBottomSheet(
    candidates: List<SessionSummary>,
    alreadyVisibleIds: Set<String>,
    onRestore: (List<String>) -> Unit,
    onDismiss: () -> Unit,
) {
    var filterText by rememberSaveable { mutableStateOf("") }
    var selectedIds by rememberSaveable { mutableStateOf(emptyList<String>()) }
    var expandedKeys by rememberSaveable { mutableStateOf(emptyList<String>()) }

    val groups = remember(candidates, alreadyVisibleIds, filterText) {
        val q = filterText.trim().lowercase()
        val hidden = candidates.filter { it.sessionId !in alreadyVisibleIds }
        groupSessionsByProject(hidden)
            .map { group -> group.copy(sessions = group.sessions.sortedByDescending { it.lastTs }) }
            .filter { group ->
                q.isBlank() ||
                    group.name.lowercase().contains(q) ||
                    group.cwd.lowercase().contains(q) ||
                    group.sessions.any { s ->
                        s.title.lowercase().contains(q) ||
                            s.agentLabel.lowercase().contains(q) ||
                            s.source.lowercase().contains(q)
                    }
            }
            .sortedByDescending { group -> group.sessions.firstOrNull()?.lastTs.orEmpty() }
    }

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = androidx.compose.material3.rememberModalBottomSheetState(
            skipPartiallyExpanded = true,
        ),
        containerColor = HomingPalette.Card,
        contentColor = HomingPalette.Text,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .navigationBarsPadding()
                .padding(horizontal = 16.dp, vertical = 6.dp)
                .testTag("restore-history-sheet"),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Text(
                text = "按项目恢复历史会话",
                style = MaterialTheme.typography.titleMedium.copy(fontWeight = FontWeight.Bold),
                color = HomingPalette.Text,
            )
            Text(
                text = "勾选项目即恢复其最近 $SESSIONS_PER_PROJECT_RESTORE 个会话（每个载入最近 10 条记录），在会话顶部可切换同项目的其他会话；展开项目可单独挑选。",
                style = TextStyle(fontSize = 12.sp, color = HomingPalette.Muted),
            )
            Surface(
                color = HomingPalette.Raised,
                shape = RoundedCornerShape(10.dp),
                border = BorderStroke(1.dp, HomingPalette.NavBorder),
            ) {
                Row(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 10.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    Icon(Icons.Outlined.Search, contentDescription = null, modifier = Modifier.size(16.dp), tint = HomingPalette.Muted)
                    Box(modifier = Modifier.weight(1f)) {
                        if (filterText.isEmpty()) {
                            Text("搜索项目、会话或 Agent…", style = TextStyle(fontSize = 13.sp, color = HomingPalette.Muted))
                        }
                        BasicTextField(
                            value = filterText,
                            onValueChange = { filterText = it },
                            singleLine = true,
                            textStyle = TextStyle(fontSize = 13.sp, color = HomingPalette.Text),
                            cursorBrush = SolidColor(HomingPalette.Brand),
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                }
            }

            if (groups.isEmpty()) {
                Text(
                    text = "暂无可恢复的历史项目。",
                    modifier = Modifier.padding(vertical = 24.dp),
                    style = TextStyle(fontSize = 13.sp, color = HomingPalette.Muted),
                )
            } else {
                LazyColumn(
                    modifier = Modifier.fillMaxWidth().heightIn(max = 400.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    items(groups, key = { "restore-project-${it.key}" }) { group ->
                        val defaultPick = group.sessions.take(SESSIONS_PER_PROJECT_RESTORE).map { it.sessionId }
                        val pickedInGroup = group.sessions.count { it.sessionId in selectedIds }
                        val projectChecked = pickedInGroup > 0
                        val expanded = group.key in expandedKeys
                        Surface(
                            color = if (projectChecked) HomingPalette.Raised else HomingPalette.Background.copy(alpha = 0.45f),
                            shape = RoundedCornerShape(10.dp),
                            border = BorderStroke(1.dp, if (projectChecked) HomingPalette.Brand else HomingPalette.NavBorder),
                            modifier = Modifier.testTag("restore-project-${group.name}"),
                        ) {
                            Column {
                                Row(
                                    modifier = Modifier.fillMaxWidth().padding(start = 2.dp, end = 4.dp, top = 4.dp, bottom = 4.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                                ) {
                                    Checkbox(
                                        checked = projectChecked,
                                        onCheckedChange = { next ->
                                            val ids = group.sessions.map { it.sessionId }.toSet()
                                            selectedIds = if (next) {
                                                (selectedIds.filterNot { it in ids } + defaultPick).distinct()
                                            } else {
                                                selectedIds.filterNot { it in ids }
                                            }
                                        },
                                    )
                                    Icon(Icons.Outlined.Folder, contentDescription = null, tint = HomingPalette.Muted, modifier = Modifier.size(16.dp))
                                    Column(modifier = Modifier.weight(1f)) {
                                        Text(
                                            text = group.name,
                                            style = TextStyle(fontSize = 13.5.sp, fontWeight = FontWeight.SemiBold, color = HomingPalette.Text),
                                            maxLines = 1,
                                            overflow = TextOverflow.Ellipsis,
                                        )
                                        Text(
                                            text = buildString {
                                                append(group.sessions.size).append(" 个会话")
                                                append(" · ")
                                                append(group.sources.joinToString("、") { defaultAgentDisplayName(it) })
                                                if (pickedInGroup > 0) append(" · 已选 ").append(pickedInGroup)
                                            },
                                            style = TextStyle(fontSize = 11.5.sp, color = HomingPalette.Muted),
                                            maxLines = 1,
                                            overflow = TextOverflow.Ellipsis,
                                        )
                                    }
                                    AgentGlyphStack(group.sources, size = 18.dp)
                                    IconButton(
                                        onClick = {
                                            expandedKeys = if (expanded) expandedKeys - group.key else expandedKeys + group.key
                                        },
                                        modifier = Modifier.size(30.dp),
                                    ) {
                                        Icon(
                                            if (expanded) Icons.Outlined.KeyboardArrowDown else Icons.Outlined.KeyboardArrowRight,
                                            contentDescription = if (expanded) "收起" else "展开",
                                            tint = HomingPalette.Muted,
                                        )
                                    }
                                }
                                if (expanded) {
                                    group.sessions.forEach { session ->
                                        val checked = session.sessionId in selectedIds
                                        Row(
                                            modifier = Modifier
                                                .fillMaxWidth()
                                                .padding(start = 28.dp, end = 10.dp)
                                                .testTag("restore-item-${session.sessionId}"),
                                            verticalAlignment = Alignment.CenterVertically,
                                            horizontalArrangement = Arrangement.spacedBy(6.dp),
                                        ) {
                                            Checkbox(
                                                checked = checked,
                                                onCheckedChange = { next ->
                                                    selectedIds = if (next) selectedIds + session.sessionId else selectedIds - session.sessionId
                                                },
                                            )
                                            AgentGlyph(brand = session.brand, source = session.source, size = 15.dp)
                                            Text(
                                                text = session.title,
                                                modifier = Modifier.weight(1f),
                                                style = TextStyle(fontSize = 12.5.sp, color = HomingPalette.Body),
                                                maxLines = 1,
                                                overflow = TextOverflow.Ellipsis,
                                            )
                                            Text(relativeTime(session.lastTs), style = TextStyle(fontSize = 10.5.sp, color = HomingPalette.Muted))
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }

            val projectCount = groups.count { g -> g.sessions.any { it.sessionId in selectedIds } }
            Button(
                onClick = { onRestore(selectedIds) },
                enabled = selectedIds.isNotEmpty(),
                colors = ButtonDefaults.buttonColors(
                    containerColor = HomingPalette.Brand,
                    contentColor = HomingPalette.Background,
                ),
                shape = RoundedCornerShape(10.dp),
                modifier = Modifier.fillMaxWidth().padding(bottom = 12.dp).testTag("confirm-restore-history-btn"),
            ) {
                Text(
                    text = if (selectedIds.isEmpty()) {
                        "请选择需要恢复的项目"
                    } else {
                        "恢复 $projectCount 个项目（${selectedIds.size} 个会话）"
                    },
                    fontWeight = FontWeight.SemiBold,
                )
            }
        }
    }
}
