package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowForward
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Code
import androidx.compose.material.icons.outlined.Computer
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.agentpush.poc.AgentBrand
import dev.agentpush.poc.AgentViewModel
import dev.agentpush.poc.BuildConfig
import dev.agentpush.poc.ConnectionState
import dev.agentpush.poc.ReminderPreferences
import dev.agentpush.poc.ReminderSettings
import dev.agentpush.poc.formatDiagnosticTime
import dev.agentpush.poc.protocol.PairedComputer
import dev.agentpush.poc.protocol.PairedComputerStore
import dev.agentpush.poc.ui.AgentGlyph
import dev.agentpush.poc.ui.HomingCard
import dev.agentpush.poc.ui.HomingPigeon
import dev.agentpush.poc.ui.PigeonState
import dev.agentpush.poc.ui.accentColor
import dev.agentpush.poc.ui.serverHostLabel
import dev.agentpush.poc.ui.theme.HomingMono
import dev.agentpush.poc.ui.theme.HomingPalette

private data class AgentEntry(
    val name: String,
    val brand: AgentBrand?,
    val wire: String,
    val note: String,
)

private val agentEntries = listOf(
    AgentEntry("Codex", AgentBrand.CODEX, "codex", "CLI 与 IDE 会话"),
    AgentEntry("Claude Code", AgentBrand.CLAUDE, "claude", "Stop / Notification 钩子"),
    AgentEntry("WorkBuddy", AgentBrand.WORKBUDDY, "workbuddy", "桌面端活跃会话"),
    AgentEntry("OpenCode", null, "opencode", "等待接入"),
    AgentEntry("ZCode", null, "zcode", "等待接入"),
)

@Composable
fun MeScreen(
    viewModel: AgentViewModel,
    onOpenSettings: () -> Unit,
    onAddComputer: () -> Unit,
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    var reminders by remember { mutableStateOf(ReminderPreferences.load(context)) }
    val computerStore = remember { PairedComputerStore(context) }
    var computers by remember { mutableStateOf(computerStore.load()) }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .statusBarsPadding()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp),
    ) {
        Text(
            text = "我的",
            modifier = Modifier.padding(top = 12.dp, bottom = 12.dp),
            style = MaterialTheme.typography.displaySmall.copy(fontSize = 28.sp),
            color = HomingPalette.Text,
        )
        MacConnectionCard(
            server = state.settings.baseUrl,
            connection = state.connection,
            lastConnected = state.connectionDiagnostics.lastConnectedAt,
            lastDisconnected = state.connectionDiagnostics.lastDisconnectedAt,
            reason = state.connectionDiagnostics.lastDisconnectReason,
            away = state.away,
            onAwayChange = viewModel::setAway,
            onReconnect = viewModel::reconnect,
            onOpenSettings = onOpenSettings,
        )
        Spacer(Modifier.height(18.dp))
        SectionTitle("我的电脑")
        Spacer(Modifier.height(7.dp))
        CloudComputersCard(
            computers = computers,
            onAddComputer = onAddComputer,
        )
        Spacer(Modifier.height(18.dp))
        SectionTitle("Agent")
        Spacer(Modifier.height(7.dp))
        AgentDirectoryCard(
            connectedWires = state.agentDirectory.agents.keys,
            directoryAvailable = state.agentDirectory.available,
        )
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
            HomingPigeon(
                state = PigeonState.Idle,
                size = 46.dp,
                showTile = false,
                contentDescription = "Homing",
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
}

@Composable
private fun MacConnectionCard(
    server: String,
    connection: ConnectionState,
    lastConnected: Long?,
    lastDisconnected: Long?,
    reason: String?,
    away: Boolean,
    onAwayChange: (Boolean) -> Unit,
    onReconnect: () -> Unit,
    onOpenSettings: () -> Unit,
) {
    HomingCard {
        Column(modifier = Modifier.padding(15.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "局域网直连 · 我的 Mac",
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
                ConnectionStatePill(connection)
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
                Button(
                    onClick = onReconnect,
                    modifier = Modifier
                        .weight(1f)
                        .height(48.dp),
                    shape = RoundedCornerShape(10.dp),
                ) {
                    Text("重新连接")
                }
                OutlinedButton(
                    onClick = onOpenSettings,
                    modifier = Modifier
                        .weight(1f)
                        .height(48.dp),
                    shape = RoundedCornerShape(10.dp),
                ) {
                    Text("重新配对")
                }
            }
            HorizontalDivider(
                modifier = Modifier.padding(vertical = 11.dp),
                color = HomingPalette.NavBorder,
            )
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "离开模式",
                        style = MaterialTheme.typography.labelLarge,
                        color = HomingPalette.Text,
                    )
                    Text(
                        text = "离开 Mac 时收紧提示，由中继保存事件",
                        style = MaterialTheme.typography.labelSmall,
                        color = HomingPalette.Muted,
                    )
                }
                Switch(
                    checked = away,
                    onCheckedChange = onAwayChange,
                    colors = SwitchDefaults.colors(
                        checkedThumbColor = HomingPalette.Background,
                        checkedTrackColor = HomingPalette.Brand,
                    ),
                )
            }
        }
    }
}

@Composable
private fun CloudComputersCard(
    computers: List<PairedComputer>,
    onAddComputer: () -> Unit,
) {
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
                            text = "还没有通过云中转添加电脑",
                            style = MaterialTheme.typography.bodyMedium,
                            color = HomingPalette.Text,
                        )
                        Text(
                            text = "扫码或在电脑上生成 6 位配对码",
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
                            .padding(horizontal = 14.dp, vertical = 11.dp),
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
                                    fontSize = 10.5.sp,
                                ),
                                color = HomingPalette.Muted,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                        }
                        Surface(
                            color = HomingPalette.Brand.copy(alpha = 0.12f),
                            shape = RoundedCornerShape(50),
                        ) {
                            Text(
                                text = "已配对",
                                modifier = Modifier.padding(horizontal = 8.dp, vertical = 3.dp),
                                style = MaterialTheme.typography.labelSmall,
                                color = HomingPalette.Brand,
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
                Text("添加电脑")
            }
        }
    }
}

@Composable
private fun AgentDirectoryCard(
    connectedWires: Set<String>,
    directoryAvailable: Boolean,
) {
    HomingCard {
        Column {
            agentEntries.forEachIndexed { index, entry ->
                if (index > 0) {
                    HorizontalDivider(
                        modifier = Modifier.padding(start = 54.dp),
                        color = HomingPalette.NavBorder,
                    )
                }
                val connected = entry.wire in connectedWires ||
                    (directoryAvailable && entry.brand != null && connectedWires.isEmpty())
                AgentRow(entry = entry, connected = connected)
            }
        }
    }
}

@Composable
private fun AgentRow(entry: AgentEntry, connected: Boolean) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 14.dp, vertical = 11.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        val tint = entry.brand?.accentColor() ?: HomingPalette.Muted
        Box(
            modifier = Modifier
                .size(34.dp)
                .clip(CircleShape)
                .background(tint.copy(alpha = 0.13f)),
            contentAlignment = Alignment.Center,
        ) {
            if (entry.brand != null) {
                AgentGlyph(brand = entry.brand, size = 18.dp, tint = tint)
            } else {
                Icon(
                    imageVector = if (entry.wire == "opencode") {
                        Icons.Outlined.Code
                    } else {
                        Icons.Outlined.Terminal
                    },
                    contentDescription = null,
                    tint = tint,
                    modifier = Modifier.size(18.dp),
                )
            }
        }
        Spacer(Modifier.width(10.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = entry.name,
                style = MaterialTheme.typography.bodyMedium.copy(
                    fontSize = 14.sp,
                    fontWeight = FontWeight.Medium,
                ),
                color = HomingPalette.Text,
            )
            Text(
                text = entry.note,
                style = MaterialTheme.typography.labelSmall,
                color = HomingPalette.Muted,
            )
        }
        Surface(
            color = if (connected) {
                HomingPalette.Brand.copy(alpha = 0.12f)
            } else {
                HomingPalette.Raised
            },
            shape = RoundedCornerShape(50),
        ) {
            Text(
                text = if (connected) "已接入" else "待接入",
                modifier = Modifier.padding(horizontal = 8.dp, vertical = 3.dp),
                style = MaterialTheme.typography.labelSmall,
                color = if (connected) HomingPalette.Brand else HomingPalette.Muted,
            )
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
private fun ConnectionStatePill(connection: ConnectionState) {
    val color = when (connection) {
        ConnectionState.CONNECTED -> HomingPalette.Brand
        ConnectionState.RECONNECTING -> HomingPalette.Warning
        ConnectionState.DISCONNECTED -> HomingPalette.Error
    }
    val label = when (connection) {
        ConnectionState.CONNECTED -> "已连接"
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
