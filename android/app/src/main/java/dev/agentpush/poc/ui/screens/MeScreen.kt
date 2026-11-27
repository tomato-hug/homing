package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
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
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.ArrowForward
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Code
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Computer
import androidx.compose.material.icons.outlined.DeleteOutline
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.QrCodeScanner
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.agentpush.poc.AgentCatalog
import dev.agentpush.poc.AgentCatalogEntry
import dev.agentpush.poc.AgentProfile
import dev.agentpush.poc.AgentViewModel
import dev.agentpush.poc.BuildConfig
import dev.agentpush.poc.ConnectionState
import dev.agentpush.poc.TransportKind
import dev.agentpush.poc.ReminderPreferences
import dev.agentpush.poc.ReminderSettings
import dev.agentpush.poc.formatDiagnosticTime
import dev.agentpush.poc.protocol.PairedComputer
import dev.agentpush.poc.protocol.PairedComputerStore
import dev.agentpush.poc.ui.AgentAvatar
import dev.agentpush.poc.ui.HomingCard
import dev.agentpush.poc.ui.HomingPigeon
import dev.agentpush.poc.ui.LocalHomingStrings
import dev.agentpush.poc.ui.PigeonState
import dev.agentpush.poc.ui.accentColor
import dev.agentpush.poc.ui.serverHostLabel
import dev.agentpush.poc.ui.theme.HomingMono
import dev.agentpush.poc.ui.theme.HomingPalette
import dev.agentpush.poc.ui.HomingPigeonScene
import dev.agentpush.poc.ui.PigeonScene
import androidx.compose.runtime.mutableIntStateOf

@Composable
fun MeScreen(
    viewModel: AgentViewModel,
    onOpenSettings: () -> Unit,
    onAddComputer: () -> Unit,
    onBack: (() -> Unit)? = null,
) {
    val strings = LocalHomingStrings.current
    val state by viewModel.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    var reminders by remember { mutableStateOf(ReminderPreferences.load(context)) }
    val computerStore = remember { PairedComputerStore(context) }
    var computers by remember { mutableStateOf(computerStore.load()) }
    var activeDeviceId by remember {
        mutableStateOf(computerStore.active()?.deviceId.orEmpty())
    }
    var renamingComputer by remember { mutableStateOf<PairedComputer?>(null) }
    var renameText by remember { mutableStateOf("") }
    var removingComputer by remember { mutableStateOf<PairedComputer?>(null) }
    var addAgentOpen by remember { mutableStateOf(false) }
    var busyAgentIds by remember { mutableStateOf<Set<String>>(emptySet()) }

    LaunchedEffect(state.lastUpdatedAt, state.transportKind, state.connection) {
        computers = computerStore.load()
        activeDeviceId = computerStore.active()?.deviceId.orEmpty()
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .statusBarsPadding()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 12.dp, bottom = 12.dp),
        ) {
            if (onBack != null) {
                IconButton(onClick = onBack, modifier = Modifier.padding(end = 4.dp)) {
                    Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = "返回")
                }
            }
            Text(
                text = strings.meTitle,
                modifier = Modifier.weight(1f),
                style = MaterialTheme.typography.displaySmall.copy(fontSize = 28.sp),
                color = HomingPalette.Text,
            )
            OutlinedButton(
                onClick = onAddComputer,
                shape = RoundedCornerShape(10.dp),
            ) {
                Icon(
                    Icons.Outlined.QrCodeScanner,
                    contentDescription = null,
                    modifier = Modifier.size(16.dp),
                )
                Spacer(Modifier.width(5.dp))
                Text("扫码配对")
            }
        }
        MacConnectionCard(
            server = if (state.transportKind == TransportKind.CLOUD) {
                computers.firstOrNull { it.deviceId == activeDeviceId }?.server
                    ?: state.settings.baseUrl
            } else {
                state.settings.baseUrl
            },
            connection = state.connection,
            transportKind = state.transportKind,
            lastConnected = state.connectionDiagnostics.lastConnectedAt,
            lastDisconnected = state.connectionDiagnostics.lastDisconnectedAt,
            reason = state.connectionDiagnostics.lastDisconnectReason,
            onReconnect = viewModel::reconnect,
            onOpenSettings = onOpenSettings,
            onAddComputer = onAddComputer,
        )
        Spacer(Modifier.height(18.dp))
        SectionTitle(strings.myComputers)
        Spacer(Modifier.height(7.dp))
        CloudComputersCard(
            computers = computers,
            activeDeviceId = activeDeviceId,
            onSelect = { computer ->
                computerStore.select(computer.deviceId)
                activeDeviceId = computer.deviceId
                viewModel.activateComputer(computer.deviceId)
                viewModel.refresh()
            },
            onRename = { computer ->
                renamingComputer = computer
                renameText = computer.name
            },
            onRemove = { computer ->
                removingComputer = computer
            },
            onAddComputer = onAddComputer,
        )
        Spacer(Modifier.height(18.dp))
        SectionTitle("Agent")
        Spacer(Modifier.height(7.dp))
        val catalog = state.agentCatalog
        if (catalog.available) {
            AgentAccessCard(
                catalog = catalog,
                busyIds = busyAgentIds,
                onSetConnected = { entry, connected ->
                    busyAgentIds = busyAgentIds + entry.id
                    viewModel.setAgentConnected(entry.id, connected) {
                        busyAgentIds = busyAgentIds - entry.id
                    }
                },
                onAddAgent = { addAgentOpen = true },
            )
        } else {
            DetectedAgentCard(
                profiles = state.agentDirectory.agents.values
                    .filter { it.installed }
                    .sortedBy { it.label.lowercase() },
            )
        }
        Spacer(Modifier.height(18.dp))
        SectionTitle("提醒")
        Spacer(Modifier.height(7.dp))
        ReminderCard(
            reminders = reminders,
            onChange = {
                reminders = it
                ReminderPreferences.save(context, it)
            },
        )
        Spacer(Modifier.height(18.dp))
        SettingsEntry(onClick = onOpenSettings)
        Spacer(Modifier.height(34.dp))
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(bottom = 28.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            // 「我的」页的品牌标：点一下播 pigeon_v2「轻点」
            var tapKey by remember { mutableIntStateOf(0) }
            HomingPigeonScene(
                scene = if (tapKey > 0) PigeonScene.TAP else null,
                size = 52.dp,
                playKey = tapKey,
                contentDescription = "Homing",
                modifier = Modifier
                    .clip(RoundedCornerShape(14.dp))
                    .clickable { tapKey += 1 },
            )
            Text(
                text = "Homing ${BuildConfig.VERSION_NAME}",
                modifier = Modifier.padding(top = 7.dp),
                style = MaterialTheme.typography.labelSmall.copy(
                    fontFamily = HomingMono,
                    fontSize = 11.sp,
                ),
                color = HomingPalette.Muted,
            )
        }
    }

    if (addAgentOpen) {
        AgentPickerSheet(
            catalog = state.agentCatalog,
            busyIds = busyAgentIds,
            onConnect = { entry ->
                busyAgentIds = busyAgentIds + entry.id
                viewModel.setAgentConnected(entry.id, true) { result ->
                    busyAgentIds = busyAgentIds - entry.id
                    if (result.isSuccess) addAgentOpen = false
                }
            },
            onDismiss = { addAgentOpen = false },
        )
    }

    renamingComputer?.let { target ->
        AlertDialog(
            onDismissRequest = { renamingComputer = null },
            title = { Text(strings.renameComputerTitle) },
            text = {
                OutlinedTextField(
                    value = renameText,
                    onValueChange = { renameText = it },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        val trimmed = renameText.trim()
                        if (trimmed.isNotBlank()) {
                            computerStore.save(target.copy(name = trimmed))
                            computers = computerStore.load()
                        }
                        renamingComputer = null
                    },
                ) {
                    Text(strings.save)
                }
            },
            dismissButton = {
                TextButton(onClick = { renamingComputer = null }) {
                    Text(strings.cancel)
                }
            },
        )
    }

    removingComputer?.let { target ->
        AlertDialog(
            onDismissRequest = { removingComputer = null },
            title = { Text(strings.unpairTitle) },
            text = { Text("${target.name}\n\n${strings.unpairConfirm}") },
            confirmButton = {
                TextButton(
                    onClick = {
                        computerStore.remove(target.deviceId)
                        computers = computerStore.load()
                        activeDeviceId = computerStore.active()?.deviceId.orEmpty()
                        dev.agentpush.poc.AgentStore.get(context).refreshCloudComputers()
                        viewModel.reconnect()
                        removingComputer = null
                    },
                ) {
                    Text(strings.remove, color = MaterialTheme.colorScheme.error)
                }
            },
            dismissButton = {
                TextButton(onClick = { removingComputer = null }) {
                    Text(strings.cancel)
                }
            },
        )
    }
}

@Composable
private fun MacConnectionCard(
    server: String,
    connection: ConnectionState,
    transportKind: TransportKind,
    lastConnected: Long?,
    lastDisconnected: Long?,
    reason: String?,
    onReconnect: () -> Unit,
    onOpenSettings: () -> Unit,
    onAddComputer: () -> Unit,
) {
    val needsAuth = reason?.let {
        it.contains("中继账号") || it.contains("Relay Key") || it.contains("配对")
    } == true && connection != ConnectionState.CONNECTED
    HomingCard {
        Column(modifier = Modifier.padding(15.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "${transportKind.label} · 我的 Mac",
                        style = MaterialTheme.typography.titleMedium,
                        color = HomingPalette.Text,
                    )
                    Text(
                        text = serverHostLabel(server),
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
                ConnectionStatePill(connection, transportKind)
            }
            Spacer(Modifier.height(10.dp))
            Text(
                text = "最近连接 ${formatDiagnosticTime(lastConnected)} · " +
                    "最近断开 ${formatDiagnosticTime(lastDisconnected)}",
                style = MaterialTheme.typography.labelSmall.copy(
                    fontFamily = HomingMono,
                    fontSize = 10.5.sp,
                    lineHeight = 16.sp,
                ),
                color = HomingPalette.Muted,
            )
            if (!reason.isNullOrBlank()) {
                Text(
                    text = "断开原因：${reason.take(150)}",
                    modifier = Modifier.padding(top = 2.dp),
                    style = MaterialTheme.typography.labelSmall.copy(lineHeight = 16.sp),
                    color = HomingPalette.Muted,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = 12.dp),
                horizontalArrangement = Arrangement.spacedBy(9.dp),
            ) {
                if (needsAuth) {
                    Button(
                        onClick = onAddComputer,
                        modifier = Modifier
                            .weight(1f)
                            .height(48.dp),
                        shape = RoundedCornerShape(10.dp),
                    ) {
                        Icon(
                            Icons.Outlined.QrCodeScanner,
                            contentDescription = null,
                            modifier = Modifier.size(16.dp),
                        )
                        Spacer(Modifier.width(5.dp))
                        Text("扫码配对")
                    }
                } else {
                    Button(
                        onClick = onReconnect,
                        modifier = Modifier
                            .weight(1f)
                            .height(48.dp),
                        shape = RoundedCornerShape(10.dp),
                    ) {
                        Text("重新连接")
                    }
                }
                OutlinedButton(
                    onClick = if (needsAuth) onReconnect else onOpenSettings,
                    modifier = Modifier
                        .weight(1f)
                        .height(48.dp),
                    shape = RoundedCornerShape(10.dp),
                ) {
                    Text(if (needsAuth) "重试连接" else "重新配对")
                }
            }
        }
    }
}

@Composable
private fun CloudComputersCard(
    computers: List<PairedComputer>,
    activeDeviceId: String,
    onSelect: (PairedComputer) -> Unit,
    onRename: (PairedComputer) -> Unit,
    onRemove: (PairedComputer) -> Unit,
    onAddComputer: () -> Unit,
) {
    val strings = LocalHomingStrings.current
    HomingCard {
        Column {
            if (computers.isEmpty()) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 14.dp, vertical = 13.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Icon(
                        Icons.Outlined.Computer,
                        contentDescription = null,
                        tint = HomingPalette.Muted,
                        modifier = Modifier.size(22.dp),
                    )
                    Column(
                        modifier = Modifier
                            .weight(1f)
                            .padding(start = 10.dp),
                    ) {
                        Text(
                            text = strings.noCloudComputer,
                            style = MaterialTheme.typography.bodyMedium,
                            color = HomingPalette.Text,
                        )
                        Text(
                            text = strings.noCloudComputerSub,
                            style = MaterialTheme.typography.labelSmall,
                            color = HomingPalette.Muted,
                        )
                    }
                }
            } else {
                computers.forEachIndexed { index, computer ->
                    if (index > 0) {
                        HorizontalDivider(
                            modifier = Modifier.padding(start = 54.dp),
                            color = HomingPalette.NavBorder,
                        )
                    }
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable { onSelect(computer) }
                            .padding(start = 14.dp, end = 6.dp, top = 10.dp, bottom = 10.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Box(
                            modifier = Modifier
                                .size(34.dp)
                                .clip(CircleShape)
                                .background(HomingPalette.Brand.copy(alpha = 0.13f)),
                            contentAlignment = Alignment.Center,
                        ) {
                            Icon(
                                Icons.Outlined.Computer,
                                contentDescription = null,
                                tint = HomingPalette.Brand,
                                modifier = Modifier.size(18.dp),
                            )
                        }
                        Column(
                            modifier = Modifier
                                .weight(1f)
                                .padding(start = 10.dp),
                        ) {
                            Text(
                                text = computer.name,
                                style = MaterialTheme.typography.bodyMedium.copy(
                                    fontWeight = FontWeight.Medium,
                                ),
                                color = HomingPalette.Text,
                            )
                            Text(
                                text = "${serverHostLabel(computer.server)} · " +
                                    computer.fingerprint.take(16),
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
                        Surface(
                            color = if (computer.deviceId == activeDeviceId) {
                                HomingPalette.Brand.copy(alpha = 0.16f)
                            } else {
                                HomingPalette.Raised
                            },
                            shape = RoundedCornerShape(50),
                        ) {
                            Text(
                                text = if (computer.deviceId == activeDeviceId) strings.activeTag else strings.switchTag,
                                modifier = Modifier.padding(horizontal = 8.dp, vertical = 3.dp),
                                style = MaterialTheme.typography.labelSmall,
                                color = if (computer.deviceId == activeDeviceId) {
                                    HomingPalette.Brand
                                } else {
                                    HomingPalette.Muted
                                },
                            )
                        }
                        IconButton(onClick = { onRename(computer) }) {
                            Icon(
                                Icons.Outlined.Edit,
                                contentDescription = strings.rename,
                                tint = HomingPalette.Muted,
                                modifier = Modifier.size(18.dp),
                            )
                        }
                        IconButton(onClick = { onRemove(computer) }) {
                            Icon(
                                Icons.Outlined.DeleteOutline,
                                contentDescription = strings.remove,
                                tint = HomingPalette.Muted,
                                modifier = Modifier.size(18.dp),
                            )
                        }
                    }
                }
            }
            OutlinedButton(
                onClick = onAddComputer,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 14.dp, vertical = 12.dp)
                    .height(46.dp),
                shape = RoundedCornerShape(10.dp),
            ) {
                Icon(
                    Icons.Outlined.Add,
                    contentDescription = null,
                    modifier = Modifier.size(18.dp),
                )
                Spacer(Modifier.width(6.dp))
                Text(strings.addComputer)
            }
        }
    }
}

@Composable
private fun AgentAccessCard(
    catalog: AgentCatalog,
    busyIds: Set<String>,
    onSetConnected: (AgentCatalogEntry, Boolean) -> Unit,
    onAddAgent: () -> Unit,
) {
    val connected = catalog.connected.sortedBy { it.displayName.lowercase() }
    HomingCard {
        Column {
            if (connected.isEmpty()) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 14.dp, vertical = 14.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = "还没有接入 Agent",
                            style = MaterialTheme.typography.bodyMedium,
                            color = HomingPalette.Text,
                        )
                        Text(
                            text = "从本机已检测到的 Agent 中添加",
                            style = MaterialTheme.typography.labelSmall,
                            color = HomingPalette.Muted,
                        )
                    }
                    TextButton(onClick = onAddAgent) {
                        Text("添加")
                    }
                }
            }
            connected.forEachIndexed { index, entry ->
                if (index > 0) {
                    HorizontalDivider(
                        modifier = Modifier.padding(start = 54.dp),
                        color = HomingPalette.NavBorder,
                    )
                }
                AgentRow(
                    entry = entry,
                    busy = entry.id in busyIds,
                    onSetConnected = { onSetConnected(entry, false) },
                )
            }
            if (connected.isNotEmpty()) {
                HorizontalDivider(color = HomingPalette.NavBorder)
                OutlinedButton(
                    onClick = onAddAgent,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 14.dp, vertical = 10.dp)
                        .height(42.dp),
                    shape = RoundedCornerShape(9.dp),
                ) {
                    Icon(
                        Icons.Outlined.Add,
                        contentDescription = null,
                        modifier = Modifier.size(17.dp),
                    )
                    Spacer(Modifier.width(5.dp))
                    Text("添加 Agent")
                }
            }
        }
    }
}

@Composable
private fun AgentRow(
    entry: AgentCatalogEntry,
    busy: Boolean,
    onSetConnected: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 14.dp, vertical = 11.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        AgentAvatar(
            brand = dev.agentpush.poc.AgentBrand.fromWire(entry.id),
            source = entry.logoSource,
            size = 38.dp,
        )
        Spacer(Modifier.width(10.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = entry.displayName,
                style = MaterialTheme.typography.bodyMedium.copy(
                    fontSize = 14.sp,
                    fontWeight = FontWeight.Medium,
                ),
                color = HomingPalette.Text,
            )
            Text(
                text = listOfNotNull(
                    entry.vendor.takeIf { it.isNotBlank() },
                    "已接入".takeIf { entry.connected },
                    "钩子已连接".takeIf { entry.hookInstalled },
                ).joinToString(" · ").ifBlank { "已接入" },
                style = MaterialTheme.typography.labelSmall,
                color = HomingPalette.Muted,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        if (busy) {
            CircularProgressIndicator(
                modifier = Modifier.size(22.dp),
                strokeWidth = 2.dp,
                color = HomingPalette.Brand,
            )
        } else {
            Switch(
                checked = true,
                onCheckedChange = { onSetConnected() },
                modifier = Modifier.testTag("agent-toggle-${entry.id}"),
                colors = SwitchDefaults.colors(
                    checkedThumbColor = HomingPalette.Background,
                    checkedTrackColor = HomingPalette.Brand,
                ),
            )
        }
    }
}

@Composable
private fun DetectedAgentCard(profiles: List<AgentProfile>) {
    HomingCard {
        Column {
            if (profiles.isEmpty()) {
                Text(
                    text = "没有检测到已安装的 Agent",
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 14.dp, vertical = 18.dp),
                    style = MaterialTheme.typography.bodySmall,
                    color = HomingPalette.Muted,
                )
            } else {
                profiles.forEachIndexed { index, profile ->
                    if (index > 0) {
                        HorizontalDivider(
                            modifier = Modifier.padding(start = 54.dp),
                            color = HomingPalette.NavBorder,
                        )
                    }
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 14.dp, vertical = 11.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        AgentAvatar(
                            brand = profile.brand,
                            source = profile.source,
                            size = 38.dp,
                        )
                        Spacer(Modifier.width(10.dp))
                        Column(modifier = Modifier.weight(1f)) {
                            Text(
                                text = profile.label,
                                style = MaterialTheme.typography.bodyMedium.copy(
                                    fontSize = 14.sp,
                                    fontWeight = FontWeight.Medium,
                                ),
                                color = HomingPalette.Text,
                            )
                            Text(
                                text = "本机已检测",
                                style = MaterialTheme.typography.labelSmall,
                                color = HomingPalette.Muted,
                            )
                        }
                        Surface(
                            color = HomingPalette.Brand.copy(alpha = 0.12f),
                            shape = RoundedCornerShape(50),
                        ) {
                            Text(
                                text = "已检测",
                                modifier = Modifier.padding(horizontal = 8.dp, vertical = 3.dp),
                                style = MaterialTheme.typography.labelSmall,
                                color = HomingPalette.Brand,
                            )
                        }
                    }
                }
                Text(
                    text = "当前 Mac 服务未提供接入开关，升级后可直接在这里关闭接入。",
                    modifier = Modifier.padding(horizontal = 14.dp, vertical = 8.dp),
                    style = MaterialTheme.typography.labelSmall.copy(fontSize = 10.5.sp),
                    color = HomingPalette.Muted,
                )
            }
        }
    }
}

@OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)
@Composable
private fun AgentPickerSheet(
    catalog: AgentCatalog,
    busyIds: Set<String>,
    onConnect: (AgentCatalogEntry) -> Unit,
    onDismiss: () -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    val candidates = catalog.connectable
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
                        text = "添加 Agent",
                        style = MaterialTheme.typography.titleMedium,
                        color = HomingPalette.Text,
                    )
                    Text(
                        text = "只列出本机已检测或已准备接入套件的 Agent",
                        style = MaterialTheme.typography.labelSmall,
                        color = HomingPalette.Muted,
                    )
                }
                IconButton(onClick = onDismiss) {
                    Icon(Icons.Outlined.Close, contentDescription = "关闭 Agent 列表")
                }
            }
            if (candidates.isEmpty()) {
                Text(
                    text = "没有更多可接入的 Agent",
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 18.dp, vertical = 28.dp),
                    style = MaterialTheme.typography.bodySmall,
                    color = HomingPalette.Muted,
                )
            } else {
                LazyColumn(
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(max = 560.dp)
                        .testTag("agent-picker-list"),
                    contentPadding = androidx.compose.foundation.layout.PaddingValues(
                        horizontal = 12.dp,
                        vertical = 4.dp,
                    ),
                    verticalArrangement = Arrangement.spacedBy(3.dp),
                ) {
                    items(
                        candidates,
                        key = { "catalog-${it.id}" },
                    ) { entry ->
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(horizontal = 8.dp, vertical = 8.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            AgentAvatar(
                                brand = dev.agentpush.poc.AgentBrand.fromWire(entry.id),
                                source = entry.logoSource,
                                size = 32.dp,
                            )
                            Column(
                                modifier = Modifier
                                    .weight(1f)
                                    .padding(horizontal = 10.dp),
                            ) {
                                Text(
                                    text = entry.displayName,
                                    style = MaterialTheme.typography.bodyMedium,
                                    color = HomingPalette.Text,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                                Text(
                                    text = if (entry.detected) "本机已检测" else "接入套件已准备",
                                    style = MaterialTheme.typography.labelSmall,
                                    color = HomingPalette.Muted,
                                )
                            }
                            if (entry.id in busyIds) {
                                CircularProgressIndicator(
                                    modifier = Modifier.size(21.dp),
                                    strokeWidth = 2.dp,
                                    color = HomingPalette.Brand,
                                )
                            } else {
                                Button(
                                    onClick = { onConnect(entry) },
                                    shape = RoundedCornerShape(9.dp),
                                    contentPadding = androidx.compose.foundation.layout.PaddingValues(
                                        horizontal = 12.dp,
                                        vertical = 7.dp,
                                    ),
                                ) {
                                    Text("接入", fontSize = 12.sp)
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
private fun ReminderCard(
    reminders: ReminderSettings,
    onChange: (ReminderSettings) -> Unit,
) {
    HomingCard {
        Column {
            ReminderRow(
                label = "等你回复",
                description = "agent 停下来等待输入",
                checked = reminders.waitingInput,
                onCheckedChange = { onChange(reminders.copy(waitingInput = it)) },
            )
            ReminderDivider()
            ReminderRow(
                label = "请求授权",
                description = "需要批准命令或文件改动",
                checked = reminders.permission,
                onCheckedChange = { onChange(reminders.copy(permission = it)) },
            )
            ReminderDivider()
            ReminderRow(
                label = "出错",
                description = "回合或工具调用失败",
                checked = reminders.error,
                onCheckedChange = { onChange(reminders.copy(error = it)) },
            )
            ReminderDivider()
            ReminderRow(
                label = "完成",
                description = "agent 完成一次回复",
                checked = reminders.completed,
                onCheckedChange = { onChange(reminders.copy(completed = it)) },
            )
        }
    }
}

@Composable
private fun ReminderRow(
    label: String,
    description: String,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .height(58.dp)
            .padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = label,
                style = MaterialTheme.typography.labelLarge,
                color = HomingPalette.Text,
            )
            Text(
                text = description,
                style = MaterialTheme.typography.labelSmall,
                color = HomingPalette.Muted,
            )
        }
        Switch(
            checked = checked,
            onCheckedChange = onCheckedChange,
            colors = SwitchDefaults.colors(
                checkedThumbColor = HomingPalette.Background,
                checkedTrackColor = HomingPalette.Brand,
            ),
        )
    }
}

@Composable
private fun ReminderDivider() {
    HorizontalDivider(
        modifier = Modifier.padding(start = 14.dp),
        color = HomingPalette.NavBorder,
    )
}

@Composable
private fun SettingsEntry(onClick: () -> Unit) {
    Surface(
        onClick = onClick,
        color = HomingPalette.Card,
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(1.dp, HomingPalette.NavBorder),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .height(56.dp)
                .padding(horizontal = 14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = "连接与通知设置",
                    style = MaterialTheme.typography.labelLarge,
                    color = HomingPalette.Text,
                )
                Text(
                    text = "服务器地址、token、通知权限、电池白名单",
                    style = MaterialTheme.typography.labelSmall,
                    color = HomingPalette.Muted,
                )
            }
            Icon(
                Icons.AutoMirrored.Outlined.ArrowForward,
                contentDescription = null,
                tint = HomingPalette.Muted,
                modifier = Modifier.size(18.dp),
            )
        }
    }
}

@Composable
private fun ConnectionStatePill(
    connection: ConnectionState,
    transportKind: TransportKind,
) {
    val color = when (connection) {
        ConnectionState.CONNECTED -> HomingPalette.Brand
        ConnectionState.RECONNECTING -> HomingPalette.Warning
        ConnectionState.DISCONNECTED -> HomingPalette.Error
    }
    val label = when (connection) {
        ConnectionState.CONNECTED -> "${transportKind.label}已连接"
        ConnectionState.RECONNECTING -> "重连中"
        ConnectionState.DISCONNECTED -> "未连接"
    }
    Surface(
        color = color.copy(alpha = 0.12f),
        shape = RoundedCornerShape(50),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 9.dp, vertical = 5.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Box(
                modifier = Modifier
                    .size(7.dp)
                    .clip(CircleShape)
                    .background(color),
            )
            Text(
                text = label,
                style = MaterialTheme.typography.labelSmall,
                color = color,
            )
        }
    }
}

@Composable
private fun SectionTitle(text: String) {
    Text(
        text = text,
        style = MaterialTheme.typography.titleMedium.copy(fontSize = 16.sp),
        color = HomingPalette.Text,
    )
}
