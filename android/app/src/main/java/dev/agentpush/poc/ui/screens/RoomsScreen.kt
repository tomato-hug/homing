package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
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
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.Groups
import androidx.compose.material3.Button
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.agentpush.poc.AgentBrand
import dev.agentpush.poc.AgentViewModel
import dev.agentpush.poc.Room
import dev.agentpush.poc.R
import dev.agentpush.poc.RoomAgentChoice
import dev.agentpush.poc.RoomApi
import dev.agentpush.poc.RoomMessage
import dev.agentpush.poc.RoomParticipant
import dev.agentpush.poc.RoomSeat
import dev.agentpush.poc.ui.AgentAvatar
import dev.agentpush.poc.ui.HomingCard
import dev.agentpush.poc.ui.HomingPigeon
import dev.agentpush.poc.ui.PigeonState
import dev.agentpush.poc.ui.RichMarkdown
import dev.agentpush.poc.ui.theme.HomingPalette
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

// ---- 列表 -----------------------------------------------------------------------

@Composable
fun RoomsScreen(
    viewModel: AgentViewModel,
    onOpenRoom: (String) -> Unit,
) {
    val state = viewModel.state.collectAsStateWithLifecycle().value
    var rooms by remember { mutableStateOf<List<Room>>(emptyList()) }
    var agents by remember { mutableStateOf<List<RoomAgentChoice>>(emptyList()) }
    var loaded by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var createOpen by remember { mutableStateOf(false) }
    var historyOpen by remember { mutableStateOf(false) }

    LaunchedEffect(state.settings) {
        while (true) {
            runCatching { RoomApi.list(state.settings) }
                .onSuccess {
                    rooms = it.rooms
                    agents = it.agents
                    error = null
                }
                .onFailure { error = roomErrorText(it) }
            loaded = true
            delay(4_000)
        }
    }

    val active = rooms.filter { !it.ended }
    val history = rooms.filter { it.ended }

    LazyColumn(
        modifier = Modifier
            .fillMaxSize()
            .statusBarsPadding()
            .testTag("rooms-screen"),
        contentPadding = androidx.compose.foundation.layout.PaddingValues(
            start = 16.dp, end = 16.dp, top = 12.dp, bottom = 24.dp,
        ),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        item(key = "header") {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "会议室",
                        style = MaterialTheme.typography.headlineMedium,
                        fontWeight = FontWeight.Bold,
                        color = HomingPalette.Text,
                        maxLines = 1,
                    )
                    Text(
                        text = "把几个 Agent 拉到一起讨论；只读，不改代码，结束时给你会议纪要",
                        style = MaterialTheme.typography.bodySmall,
                        color = HomingPalette.Muted,
                    )
                }
                Button(
                    onClick = { createOpen = true },
                    enabled = agents.size >= 2,
                    modifier = Modifier.testTag("rooms-create"),
                ) {
                    Icon(Icons.Outlined.Add, contentDescription = null, modifier = Modifier.size(18.dp))
                    Spacer(Modifier.width(4.dp))
                    Text("开会")
                }
            }
        }
        error?.let { message ->
            item(key = "error") {
                Text(
                    text = message,
                    style = MaterialTheme.typography.bodySmall,
                    color = HomingPalette.Error,
                )
            }
        }
        if (loaded && agents.size < 2) {
            item(key = "need-agents") {
                Text(
                    text = "开会至少需要两个能从 Homing 新建会话的 Agent（目前是 ${agents.size} 个）。" +
                        "在电脑上装好 Codex 或 Claude Code 后再来。",
                    style = MaterialTheme.typography.bodySmall,
                    color = HomingPalette.Warning,
                )
            }
        }
        if (loaded && rooms.isEmpty()) {
            item(key = "empty") {
                HomingCard {
                    Column(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(vertical = 28.dp, horizontal = 20.dp),
                        horizontalAlignment = Alignment.CenterHorizontally,
                    ) {
                        HomingPigeon(state = PigeonState.Idle, size = 52.dp)
                        Text(
                            text = "还没有开过会",
                            modifier = Modifier.padding(top = 8.dp),
                            style = MaterialTheme.typography.titleMedium,
                            color = HomingPalette.Text,
                        )
                        Text(
                            text = "选一个议题和工作目录，挑两三个 Agent，它们会轮流发言、互相回应",
                            modifier = Modifier.padding(top = 4.dp),
                            style = MaterialTheme.typography.bodySmall,
                            color = HomingPalette.Muted,
                        )
                    }
                }
            }
        }
        if (active.isNotEmpty()) {
            item(key = "active-label") { RoomSectionLabel("进行中", active.size) }
            items(active, key = { "room-${it.id}" }) { room ->
                RoomCard(room = room, onClick = { onOpenRoom(room.id) })
            }
        }
        if (history.isNotEmpty()) {
            item(key = "history-label") {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable { historyOpen = !historyOpen }
                        .padding(vertical = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    RoomSectionLabel("历史会议", history.size, modifier = Modifier.weight(1f))
                    Icon(
                        if (historyOpen) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore,
                        contentDescription = if (historyOpen) "收起历史会议" else "展开历史会议",
                        tint = HomingPalette.Muted,
                    )
                }
            }
            if (historyOpen) {
                items(history, key = { "history-${it.id}" }) { room ->
                    RoomCard(room = room, onClick = { onOpenRoom(room.id) })
                }
            }
        }
    }

    if (createOpen) {
        CreateRoomSheet(
            viewModel = viewModel,
            agents = agents,
            onDismiss = { createOpen = false },
            onCreated = { room ->
                createOpen = false
                rooms = listOf(room) + rooms.filterNot { it.id == room.id }
                onOpenRoom(room.id)
            },
        )
    }
}

@Composable
private fun RoomSectionLabel(text: String, count: Int, modifier: Modifier = Modifier) {
    Row(modifier = modifier, verticalAlignment = Alignment.CenterVertically) {
        Text(
            text = text,
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.SemiBold,
            color = HomingPalette.Text,
        )
        Spacer(Modifier.width(8.dp))
        Text(text = count.toString(), style = MaterialTheme.typography.labelMedium, color = HomingPalette.Muted)
    }
}

@Composable
private fun RoomCard(room: Room, onClick: () -> Unit) {
    HomingCard(
        modifier = Modifier
            .clickable(onClick = onClick)
            .testTag("room-card-${room.id}"),
        borderColor = if (room.status == "running") {
            HomingPalette.Brand.copy(alpha = 0.45f)
        } else {
            HomingPalette.Border.copy(alpha = 0.4f)
        },
    ) {
        Column(modifier = Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = room.title,
                    modifier = Modifier.weight(1f),
                    style = MaterialTheme.typography.titleMedium,
                    color = HomingPalette.Text,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                RoomStatusPill(room)
            }
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                room.participants.forEach { p ->
                    AgentAvatar(brand = AgentBrand.fromWire(p.agent), source = p.agent, size = 24.dp)
                }
                Spacer(Modifier.width(2.dp))
                Text(
                    text = room.speakingParticipant?.let { "${it.name} 正在发言…" }
                        ?: room.participants.joinToString("、") { it.name },
                    style = MaterialTheme.typography.labelMedium,
                    color = if (room.speakingParticipant != null) HomingPalette.Brand else HomingPalette.Muted,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            if (room.preview.isNotBlank()) {
                Text(
                    text = room.preview,
                    style = MaterialTheme.typography.bodySmall,
                    color = HomingPalette.Body,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

@Composable
private fun RoomStatusPill(room: Room) {
    val color = when (room.status) {
        "running", "summarizing" -> HomingPalette.Brand
        "paused" -> HomingPalette.Warning
        else -> HomingPalette.Muted
    }
    Surface(
        color = color.copy(alpha = 0.12f),
        shape = RoundedCornerShape(50),
    ) {
        Text(
            text = room.statusLabel,
            modifier = Modifier.padding(horizontal = 9.dp, vertical = 3.dp),
            style = MaterialTheme.typography.labelSmall,
            color = color,
            maxLines = 1,
        )
    }
}

// ---- 开会 -----------------------------------------------------------------------

@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
private fun CreateRoomSheet(
    viewModel: AgentViewModel,
    agents: List<RoomAgentChoice>,
    onDismiss: () -> Unit,
    onCreated: (Room) -> Unit,
) {
    val state = viewModel.state.collectAsStateWithLifecycle().value
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    val scope = rememberCoroutineScope()
    var topic by remember { mutableStateOf("") }
    var cwd by remember { mutableStateOf(state.projects.firstOrNull()?.cwd.orEmpty()) }
    var rounds by remember { mutableStateOf(2) }
    var selected by remember { mutableStateOf(agents.take(2).map { it.id }) }
    var roles by remember { mutableStateOf(mapOf<String, String>()) }
    var creating by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }

    LaunchedEffect(Unit) {
        if (state.projects.isEmpty()) viewModel.refreshProjects()
    }
    LaunchedEffect(state.projects) {
        if (cwd.isBlank()) cwd = state.projects.firstOrNull()?.cwd.orEmpty()
    }

    val canStart = !creating && topic.isNotBlank() && cwd.isNotBlank() && selected.size in 2..4

    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = sheetState) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .imePadding()
                .navigationBarsPadding()
                .padding(horizontal = 20.dp, vertical = 4.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Text("开会", style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
            OutlinedTextField(
                value = topic,
                onValueChange = { topic = it },
                modifier = Modifier
                    .fillMaxWidth()
                    .testTag("room-topic"),
                label = { Text("议题") },
                placeholder = { Text("例如：登录模块要不要拆成独立服务？") },
                minLines = 2,
                maxLines = 6,
            )

            Text("工作目录", style = MaterialTheme.typography.labelLarge, color = HomingPalette.Body)
            if (state.projects.isNotEmpty()) {
                FlowRow(
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    state.projects.take(8).forEach { project ->
                        FilterChip(
                            selected = cwd == project.cwd,
                            onClick = { cwd = project.cwd },
                            label = { Text(project.project.ifBlank { project.cwd }, maxLines = 1) },
                        )
                    }
                }
            }
            OutlinedTextField(
                value = cwd,
                onValueChange = { cwd = it },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("目录路径") },
                singleLine = true,
                supportingText = { Text("参会的 Agent 会在这个目录里阅读代码，不会修改") },
            )

            Text("参会 Agent（2–4 个）", style = MaterialTheme.typography.labelLarge, color = HomingPalette.Body)
            agents.forEach { choice ->
                val isSelected = choice.id in selected
                Row(verticalAlignment = Alignment.CenterVertically) {
                    FilterChip(
                        selected = isSelected,
                        onClick = {
                            selected = if (isSelected) {
                                selected - choice.id
                            } else if (selected.size < 4) {
                                selected + choice.id
                            } else {
                                selected
                            }
                        },
                        label = { Text(choice.name, maxLines = 1) },
                        leadingIcon = {
                            AgentAvatar(
                                brand = AgentBrand.fromWire(choice.id),
                                source = choice.id,
                                size = 20.dp,
                            )
                        },
                        modifier = Modifier.testTag("room-agent-${choice.id}"),
                    )
                    if (isSelected) {
                        Spacer(Modifier.width(8.dp))
                        OutlinedTextField(
                            value = roles[choice.id].orEmpty(),
                            onValueChange = { roles = roles + (choice.id to it.take(40)) },
                            modifier = Modifier.weight(1f),
                            placeholder = { Text("角色（可选），如：审查") },
                            singleLine = true,
                        )
                    }
                }
            }

            Text("轮数", style = MaterialTheme.typography.labelLarge, color = HomingPalette.Body)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                listOf(1, 2, 3).forEach { n ->
                    FilterChip(
                        selected = rounds == n,
                        onClick = { rounds = n },
                        label = { Text("$n 轮") },
                    )
                }
            }

            error?.let {
                Text(it, style = MaterialTheme.typography.bodySmall, color = HomingPalette.Error)
            }
            Button(
                onClick = {
                    creating = true
                    error = null
                    scope.launch {
                        runCatching {
                            RoomApi.create(
                                settings = state.settings,
                                topic = topic.trim(),
                                cwd = cwd.trim(),
                                rounds = rounds,
                                seats = selected.map { RoomSeat(it, roles[it].orEmpty().trim()) },
                            )
                        }.onSuccess(onCreated)
                            .onFailure { error = roomErrorText(it) }
                        creating = false
                    }
                },
                enabled = canStart,
                modifier = Modifier
                    .fillMaxWidth()
                    .height(48.dp)
                    .testTag("room-start"),
            ) {
                Text(if (creating) "正在召集…" else "开始会议")
            }
            Spacer(Modifier.height(8.dp))
        }
    }
}

// ---- 房间 -----------------------------------------------------------------------

@Composable
fun RoomScreen(
    viewModel: AgentViewModel,
    roomId: String,
    onBack: () -> Unit,
    onOpenSession: (String) -> Unit,
) {
    val state = viewModel.state.collectAsStateWithLifecycle().value
    val scope = rememberCoroutineScope()
    var room by remember(roomId) { mutableStateOf<Room?>(null) }
    var error by remember(roomId) { mutableStateOf<String?>(null) }
    var draft by remember(roomId) { mutableStateOf("") }
    var busy by remember(roomId) { mutableStateOf(false) }
    var revision by remember(roomId) { mutableStateOf(0) }
    val listState = rememberLazyListState()

    LaunchedEffect(roomId, state.settings) {
        while (true) {
            val observed = revision
            runCatching { RoomApi.get(state.settings, roomId) }
                .onSuccess {
                    if (observed == revision) { room = it; error = null }
                }
                .onFailure { if (observed == revision) error = roomErrorText(it) }
            delay(if (room?.ended == true) 15_000 else 2_500)
        }
    }
    val messageCount = room?.messages?.size ?: 0
    val speaking = room?.speakingParticipant
    LaunchedEffect(messageCount, speaking?.agent) {
        val total = listState.layoutInfo.totalItemsCount
        if (total > 0) listState.animateScrollToItem(total - 1)
    }

    fun act(afterSuccess: () -> Unit = {}, block: suspend () -> Room) {
        if (busy) return
        busy = true
        revision++
        scope.launch {
            runCatching { block() }
                .onSuccess {
                    room = it
                    error = null
                    afterSuccess()
                }
                .onFailure { error = roomErrorText(it) }
            busy = false
            revision++
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .statusBarsPadding()
            .testTag("room-screen"),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(start = 4.dp, end = 12.dp, top = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = onBack) {
                Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = "返回")
            }
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = room?.title ?: "会议室",
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold,
                    color = HomingPalette.Text,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = room?.statusLabel ?: "加载中…",
                    style = MaterialTheme.typography.labelSmall,
                    color = HomingPalette.Muted,
                    maxLines = 1,
                )
            }
            room?.let { RoomStatusPill(it) }
        }
        room?.let { current ->
            ParticipantsBar(current, onOpenSession = onOpenSession)
            if (current.note.isNotBlank()) {
                Text(
                    text = current.note,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                    style = MaterialTheme.typography.labelSmall,
                    color = HomingPalette.Warning,
                )
            }
        }
        error?.let {
            Text(
                text = it,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                style = MaterialTheme.typography.labelSmall,
                color = HomingPalette.Error,
            )
        }

        LazyColumn(
            state = listState,
            modifier = Modifier
                .weight(1f)
                .fillMaxWidth(),
            contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            room?.let { current ->
                item(key = "topic") { TopicCard(current) }
                items(current.messages, key = { "msg-${it.seq}" }) { message ->
                    RoomMessageView(current, message)
                }
                current.speakingParticipant?.let { p ->
                    item(key = "speaking") { SpeakingRow(p) }
                }
            }
        }

        room?.let { current ->
            RoomControls(
                room = current,
                draft = draft,
                busy = busy,
                onDraftChange = { draft = it },
                onSend = {
                    val text = draft.trim()
                    if (text.isNotEmpty()) {
                        act(afterSuccess = { if (draft.trim() == text) draft = "" }) {
                            RoomApi.say(state.settings, current.id, text)
                        }
                    }
                },
                onAction = { action -> act { RoomApi.control(state.settings, current.id, action) } },
                onCall = { agent -> act { RoomApi.control(state.settings, current.id, "call", agent) } },
            )
        }
    }
}

@Composable
private fun ParticipantsBar(room: Room, onOpenSession: (String) -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 6.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        room.participants.forEach { p ->
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                modifier = Modifier
                    .widthIn(max = 88.dp)
                    .clickable(enabled = p.sessionId.isNotBlank()) { onOpenSession(p.sessionId) }
                    .testTag("room-participant-${p.agent}"),
            ) {
                Box(contentAlignment = Alignment.BottomEnd) {
                    AgentAvatar(brand = AgentBrand.fromWire(p.agent), source = p.agent, size = 36.dp)
                    Box(
                        modifier = Modifier
                            .size(10.dp)
                            .clip(CircleShape)
                            .background(participantColor(p)),
                    )
                }
                Text(
                    text = p.name,
                    style = MaterialTheme.typography.labelSmall,
                    color = HomingPalette.Body,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = participantStatusText(p),
                    style = MaterialTheme.typography.labelSmall,
                    color = participantColor(p),
                    maxLines = 1,
                )
            }
        }
    }
}

private fun participantColor(p: RoomParticipant) = when (p.status) {
    "speaking" -> HomingPalette.Brand
    "needs_you" -> HomingPalette.Warning
    "failed" -> HomingPalette.Error
    else -> HomingPalette.Muted
}

private fun participantStatusText(p: RoomParticipant) = when (p.status) {
    "speaking" -> "发言中"
    "needs_you" -> "等你授权"
    "failed" -> "未能参会"
    else -> p.role.ifBlank { "等待" }
}

@Composable
private fun TopicCard(room: Room) {
    HomingCard(borderColor = HomingPalette.Border.copy(alpha = 0.4f)) {
        Column(modifier = Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text("议题", style = MaterialTheme.typography.labelSmall, color = HomingPalette.Muted)
            Text(room.topic, style = MaterialTheme.typography.bodyMedium, color = HomingPalette.Text)
            Text(
                text = "${room.rounds} 轮 · 只读 · ${room.cwd}",
                style = MaterialTheme.typography.labelSmall,
                color = HomingPalette.Muted,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@Composable
private fun RoomMessageView(room: Room, message: RoomMessage) {
    val clipboard = LocalClipboardManager.current
    val uriHandler = LocalUriHandler.current
    when (message.kind) {
        "user" -> Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
            Surface(
                color = HomingPalette.Brand.copy(alpha = 0.16f),
                shape = RoundedCornerShape(14.dp, 4.dp, 14.dp, 14.dp),
                modifier = Modifier.widthIn(max = 300.dp),
            ) {
                Text(
                    text = message.text,
                    modifier = Modifier.padding(horizontal = 12.dp, vertical = 8.dp),
                    style = MaterialTheme.typography.bodyMedium,
                    color = HomingPalette.Text,
                )
            }
        }
        "system" -> Text(
            text = message.text,
            modifier = Modifier.fillMaxWidth(),
            style = MaterialTheme.typography.labelSmall,
            color = HomingPalette.Muted,
        )
        "minutes" -> HomingCard(
            modifier = Modifier.testTag("room-minutes"),
            borderColor = HomingPalette.Brand.copy(alpha = 0.5f),
        ) {
            Column(modifier = Modifier.padding(14.dp)) {
                Text(
                    text = "会议纪要 · ${message.name} 整理",
                    style = MaterialTheme.typography.titleSmall,
                    fontWeight = FontWeight.SemiBold,
                    color = HomingPalette.Brand,
                )
                Spacer(Modifier.height(6.dp))
                RichMarkdown(
                    source = message.text,
                    onCopyCode = { clipboard.setText(AnnotatedString(it)) },
                    onOpenUrl = { runCatching { uriHandler.openUri(it) } },
                )
            }
        }
        else -> {
            val p = room.participants.firstOrNull { it.agent == message.from }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                AgentAvatar(brand = AgentBrand.fromWire(message.from), source = message.from, size = 30.dp)
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "${p?.label ?: message.name} · 第 ${message.round} 轮",
                        style = MaterialTheme.typography.labelSmall,
                        color = HomingPalette.Muted,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Surface(
                        color = HomingPalette.Card,
                        shape = RoundedCornerShape(4.dp, 14.dp, 14.dp, 14.dp),
                        border = BorderStroke(1.dp, HomingPalette.Border.copy(alpha = 0.35f)),
                        modifier = Modifier.padding(top = 3.dp),
                    ) {
                        Box(modifier = Modifier.padding(horizontal = 12.dp, vertical = 8.dp)) {
                            RichMarkdown(
                                source = message.text,
                                onCopyCode = { clipboard.setText(AnnotatedString(it)) },
                                onOpenUrl = { runCatching { uriHandler.openUri(it) } },
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun SpeakingRow(p: RoomParticipant) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier.testTag("room-speaking"),
    ) {
        HomingPigeon(state = PigeonState.Flap, size = 22.dp)
        Text(
            text = if (p.status == "needs_you") {
                "${p.name} 在等你授权，点上方头像进入它的会话处理"
            } else {
                "${p.name} 正在思考和发言…"
            },
            style = MaterialTheme.typography.labelMedium,
            color = if (p.status == "needs_you") HomingPalette.Warning else HomingPalette.Brand,
        )
    }
}

@Composable
@OptIn(ExperimentalLayoutApi::class)
internal fun RoomControls(
    room: Room,
    draft: String,
    busy: Boolean,
    onDraftChange: (String) -> Unit,
    onSend: () -> Unit,
    onAction: (String) -> Unit,
    onCall: (String) -> Unit = {},
) {
    var choosing by remember(room.id) { mutableStateOf(false) }
    val canControl = room.status in listOf("running", "paused") && !room.endRequested
    Surface(color = HomingPalette.NavBackground) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .imePadding()
                .navigationBarsPadding()
                .padding(horizontal = 12.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            if (!canControl) {
                if (room.status == "paused" && room.endRequested) {
                    OutlinedButton(onClick = { onAction("resume") }, enabled = !busy) {
                        Text(stringResource(R.string.room_resume_minutes))
                    }
                }
                Text(
                    text = when {
                        room.ended -> "会议已结束，纪要在上方"
                        room.endRequested -> stringResource(R.string.room_ending)
                        else -> stringResource(R.string.room_summarizing)
                    },
                    style = MaterialTheme.typography.labelMedium,
                    color = HomingPalette.Muted,
                )
                return@Column
            }
            if (room.nextSpeaker.isNotBlank()) {
                Text(stringResource(R.string.room_call_pending, room.participants.firstOrNull { it.agent == room.nextSpeaker }?.name ?: room.nextSpeaker), style = MaterialTheme.typography.labelMedium)
            }
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                OutlinedButton(onClick = { choosing = true }, enabled = !busy && room.callableParticipants.isNotEmpty(), modifier = Modifier.testTag("room-call")) {
                    Text(stringResource(R.string.room_call))
                }
                when (room.status) {
                    "paused" -> OutlinedButton(onClick = { onAction("resume") }, enabled = !busy) { Text("继续") }
                    "running" -> OutlinedButton(onClick = { onAction("pause") }, enabled = !busy) { Text("暂停") }
                }
                if (room.status == "running" || room.status == "paused") {
                    OutlinedButton(
                        onClick = { onAction("extend") },
                        enabled = !busy && room.rounds < 5,
                    ) { Text("加一轮") }
                    TextButton(
                        onClick = { onAction("end") },
                        enabled = !busy,
                        modifier = Modifier.testTag("room-end"),
                    ) { Text("结束并出纪要", color = HomingPalette.Warning) }
                }
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                OutlinedTextField(
                    value = draft,
                    onValueChange = onDraftChange,
                    modifier = Modifier
                        .weight(1f)
                        .testTag("room-input"),
                    placeholder = { Text("插话：下一位发言时会看到") },
                    maxLines = 4,
                    shape = RoundedCornerShape(18.dp),
                )
                IconButton(
                    onClick = onSend,
                    enabled = !busy && draft.isNotBlank(),
                    modifier = Modifier.testTag("room-send"),
                ) {
                    Icon(Icons.AutoMirrored.Outlined.Send, contentDescription = "发言")
                }
            }
        }
    }
    if (choosing && canControl) {
        AlertDialog(
            onDismissRequest = { choosing = false },
            title = { Text(stringResource(R.string.room_call_title)) },
            text = { Column {
                Text(stringResource(R.string.room_call_hint))
                room.callableParticipants.forEach { participant ->
                    TextButton(onClick = { choosing = false; onCall(participant.agent) }, enabled = !busy) { Text(participant.label) }
                }
            } },
            confirmButton = { TextButton(onClick = { choosing = false }) { Text(stringResource(R.string.close)) } },
        )
    }
}

internal fun roomErrorText(error: Throwable): String {
    val message = error.message.orEmpty()
    return when {
        error is dev.agentpush.poc.AgentApiException && error.statusCode == 404 ->
            "电脑端的 Homing 还不支持会议室，请先更新电脑端"
        message.isBlank() -> "连接电脑失败"
        else -> message
    }
}

/** 底部导航「会议室」用的图标。 */
val RoomsIcon = Icons.Outlined.Groups
