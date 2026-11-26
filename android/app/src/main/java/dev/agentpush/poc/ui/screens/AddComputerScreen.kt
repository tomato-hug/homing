package dev.agentpush.poc.ui.screens

import android.content.Context
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.QrCodeScanner
import androidx.compose.material3.Button
import androidx.compose.material3.FilterChip
import androidx.compose.material3.FilterChipDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.journeyapps.barcodescanner.ScanContract
import com.journeyapps.barcodescanner.ScanOptions
import dev.agentpush.poc.AgentStore
import dev.agentpush.poc.protocol.DeviceIdentityStore
import dev.agentpush.poc.protocol.PairedComputer
import dev.agentpush.poc.protocol.PairedComputerStore
import dev.agentpush.poc.protocol.PairingInvite
import dev.agentpush.poc.protocol.ProtocolPairingClient
import dev.agentpush.poc.PushService
import dev.agentpush.poc.ui.HomingCard
import dev.agentpush.poc.ui.HomingPigeonScene
import dev.agentpush.poc.ui.LocalHomingStrings
import dev.agentpush.poc.ui.PigeonCrop
import dev.agentpush.poc.ui.PigeonScene
import dev.agentpush.poc.ui.theme.HomingMono
import dev.agentpush.poc.ui.theme.HomingPalette
import kotlinx.coroutines.launch

private sealed interface PairingUiState {
    data object Idle : PairingUiState
    data object Connecting : PairingUiState
    data class Waiting(
        val hostId: String,
        val sas: List<String>,
        val fingerprint: String,
    ) : PairingUiState
    data class Accepted(val computer: PairedComputer) : PairingUiState
    data class Failed(val message: String) : PairingUiState
}

@Composable
fun AddComputerScreen(
    onBack: () -> Unit,
    onPaired: () -> Unit,
    initialInvite: String? = null,
    onInviteConsumed: () -> Unit = {},
) {
    val strings = LocalHomingStrings.current
    val context = LocalContext.current
    val preferences = remember {
        context.getSharedPreferences("homing_cloud_pairing", Context.MODE_PRIVATE)
    }
    val identity = remember { DeviceIdentityStore(context).loadOrCreate() }
    val scope = rememberCoroutineScope()
    val words = remember {
        context.assets.open("bip39-chinese-simplified.txt")
            .bufferedReader()
            .useLines { lines -> lines.filter { it.isNotBlank() }.toList() }
    }
    val store = remember { PairedComputerStore(context) }
    val client = remember { ProtocolPairingClient(identity, words) }
    var mode by rememberSaveable { mutableStateOf(0) }
    var server by rememberSaveable {
        mutableStateOf(
            preferences.getString("server", null)
                ?.takeIf { it.isNotBlank() && it != "wss://your-homing-server/v1/ws" }
                ?: "wss://homing.douqu.online/v1/ws",
        )
    }
    var code by rememberSaveable { mutableStateOf("") }
    var status by remember { mutableStateOf<PairingUiState>(PairingUiState.Idle) }

    DisposableEffect(Unit) {
        onDispose { client.close() }
    }

    fun startPairing(
        invite: PairingInvite? = null,
        code6: String? = null,
    ) {
        val targetServer = invite?.server ?: server
        preferences.edit().putString("server", targetServer).apply()
        status = PairingUiState.Connecting
        client.start(
            server = targetServer,
            token = invite?.token,
            code6 = code6,
            expectedHostId = invite?.hostId,
            expectedFingerprint = invite?.fingerprint,
            listener = object : ProtocolPairingClient.Listener {
                override fun onConnecting() {
                    status = PairingUiState.Connecting
                }

                override fun onWaiting(
                    hostId: String,
                    sas: List<String>,
                    fingerprint: String,
                ) {
                    status = PairingUiState.Waiting(hostId, sas, fingerprint)
                }

                override fun onAccepted(computer: PairedComputer) {
                    store.save(computer)
                    val agentStore = AgentStore.get(context)
                    agentStore.refreshCloudComputers()
                    scope.launch {
                        runCatching { agentStore.refreshAll() }
                    }
                    PushService.requestReconnect(context)
                    status = PairingUiState.Accepted(computer)
                }

                override fun onError(message: String) {
                    status = PairingUiState.Failed(message)
                }
            },
        )
    }

    // 从电脑二维码的落地页（/get）或系统相机点进来：带着配对链接，直接开始配对
    LaunchedEffect(initialInvite) {
        val raw = initialInvite ?: return@LaunchedEffect
        onInviteConsumed()
        runCatching { PairingInvite.parse(raw) }
            .onSuccess { invite ->
                server = invite.server
                startPairing(invite = invite)
            }
            .onFailure {
                status = PairingUiState.Failed(it.message ?: "配对链接无效")
            }
    }

    val scanner = rememberLauncherForActivityResult(ScanContract()) { result ->
        val contents = result.contents
        if (contents != null) {
            runCatching { PairingInvite.parse(contents) }
                .onSuccess { invite ->
                    server = invite.server
                    startPairing(invite = invite)
                }
                .onFailure {
                    status = PairingUiState.Failed(
                        it.message ?: "二维码不是 Homing 配对码",
                    )
                }
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .statusBarsPadding()
            .navigationBarsPadding()
            .imePadding()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = onBack) {
                Icon(
                    Icons.AutoMirrored.Outlined.ArrowBack,
                    contentDescription = "返回",
                    tint = HomingPalette.Text,
                )
            }
            Text(
                text = strings.addComputer,
                style = MaterialTheme.typography.titleLarge.copy(
                    fontSize = 20.sp,
                    fontWeight = FontWeight.SemiBold,
                ),
                color = HomingPalette.Text,
            )
        }
        Text(
            text = strings.addComputerDesc,
            modifier = Modifier.padding(top = 4.dp, bottom = 14.dp),
            style = MaterialTheme.typography.bodyMedium.copy(lineHeight = 22.sp),
            color = HomingPalette.Muted,
        )

        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            PairingModeChip(
                selected = mode == 0,
                label = strings.scanQrTab,
                icon = { Icon(Icons.Outlined.QrCodeScanner, null, Modifier.size(17.dp)) },
                onClick = { mode = 0 },
            )
            PairingModeChip(
                selected = mode == 1,
                label = strings.pairCodeTab,
                icon = { Icon(Icons.Outlined.Key, null, Modifier.size(17.dp)) },
                onClick = { mode = 1 },
            )
        }

        Spacer(Modifier.height(14.dp))
        if (mode == 0) {
            HomingCard {
                Column(
                    modifier = Modifier.padding(18.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Icon(
                        Icons.Outlined.QrCodeScanner,
                        contentDescription = null,
                        tint = HomingPalette.Brand,
                        modifier = Modifier.size(54.dp),
                    )
                    Text(
                        text = strings.scanQrTitle,
                        modifier = Modifier.padding(top = 10.dp),
                        style = MaterialTheme.typography.titleMedium,
                        color = HomingPalette.Text,
                    )
                    Text(
                        text = strings.scanQrDesc,
                        modifier = Modifier.padding(top = 4.dp),
                        style = MaterialTheme.typography.labelSmall.copy(lineHeight = 18.sp),
                        color = HomingPalette.Muted,
                        textAlign = TextAlign.Center,
                    )
                    Button(
                        onClick = {
                            scanner.launch(
                                ScanOptions()
                                    .setPrompt(strings.scanQrTitle)
                                    .setDesiredBarcodeFormats(ScanOptions.QR_CODE)
                                    .setBeepEnabled(false)
                                    .setOrientationLocked(false),
                            )
                        },
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = 16.dp)
                            .height(48.dp),
                        shape = RoundedCornerShape(10.dp),
                    ) {
                        Text(strings.openCameraScan)
                    }
                }
            }
        } else {
            HomingCard {
                Column(modifier = Modifier.padding(16.dp)) {
                    Text(
                        text = strings.relayServerLabel,
                        style = MaterialTheme.typography.labelLarge,
                        color = HomingPalette.Text,
                    )
                    OutlinedTextField(
                        value = server,
                        onValueChange = { server = it },
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = 6.dp),
                        singleLine = true,
                        placeholder = { Text("wss://your-server/v1/ws") },
                        textStyle = MaterialTheme.typography.bodySmall.copy(
                            fontFamily = HomingMono,
                            fontSize = 12.sp,
                        ),
                    )
                    Text(
                        text = "6 位配对码",
                        modifier = Modifier.padding(top = 13.dp),
                        style = MaterialTheme.typography.labelLarge,
                        color = HomingPalette.Text,
                    )
                    OutlinedTextField(
                        value = code,
                        onValueChange = { value ->
                            code = value.filter { it in '0'..'9' }.take(6)
                        },
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = 6.dp),
                        singleLine = true,
                        keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(
                            keyboardType = KeyboardType.NumberPassword,
                        ),
                        placeholder = { Text("123456") },
                        textStyle = MaterialTheme.typography.headlineSmall.copy(
                            fontFamily = HomingMono,
                            letterSpacing = 5.sp,
                        ),
                    )
                    Button(
                        onClick = { startPairing(code6 = code) },
                        enabled = code.length == 6 && server.isNotBlank(),
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = 16.dp)
                            .height(48.dp),
                        shape = RoundedCornerShape(10.dp),
                    ) {
                        Text("开始配对")
                    }
                }
            }
        }

        Spacer(Modifier.height(16.dp))
        when (val current = status) {
            PairingUiState.Idle -> Unit
            PairingUiState.Connecting -> StatusCard(
                icon = { Icon(Icons.Outlined.Key, null, tint = HomingPalette.Brand) },
                title = "正在连接电脑…",
                detail = "正在验证服务器和设备身份。",
            )
            is PairingUiState.Waiting -> SasCard(current)
            is PairingUiState.Accepted -> AcceptedCard(current.computer, onPaired)
            is PairingUiState.Failed -> StatusCard(
                icon = {
                    Icon(
                        Icons.Outlined.ErrorOutline,
                        null,
                        tint = HomingPalette.Error,
                    )
                },
                title = "配对没有完成",
                detail = current.message,
                accent = HomingPalette.Error,
                action = {
                    OutlinedButton(
                        onClick = { status = PairingUiState.Idle },
                        modifier = Modifier.height(44.dp),
                        shape = RoundedCornerShape(10.dp),
                    ) {
                        Text("重试")
                    }
                },
            )
        }
        Spacer(Modifier.height(30.dp))
    }
}

@Composable
private fun PairingModeChip(
    selected: Boolean,
    label: String,
    icon: @Composable () -> Unit,
    onClick: () -> Unit,
) {
    FilterChip(
        selected = selected,
        onClick = onClick,
        label = { Text(label) },
        leadingIcon = icon,
        shape = RoundedCornerShape(50),
        colors = FilterChipDefaults.filterChipColors(
            selectedContainerColor = HomingPalette.Brand.copy(alpha = 0.16f),
            selectedLabelColor = HomingPalette.Brand,
            selectedLeadingIconColor = HomingPalette.Brand,
            containerColor = HomingPalette.Card,
            labelColor = HomingPalette.Muted,
            iconColor = HomingPalette.Muted,
        ),
    )
}

@Composable
private fun SasCard(state: PairingUiState.Waiting) {
    HomingCard {
        Column(
            modifier = Modifier.padding(16.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                text = "核对这四个词",
                style = MaterialTheme.typography.titleMedium,
                color = HomingPalette.Text,
            )
            Text(
                text = "电脑上应显示完全相同的四个词。一致才继续，不一致请拒绝。",
                modifier = Modifier.padding(top = 4.dp),
                style = MaterialTheme.typography.labelSmall.copy(lineHeight = 18.sp),
                color = HomingPalette.Muted,
                textAlign = TextAlign.Center,
            )
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = 14.dp),
                horizontalArrangement = Arrangement.spacedBy(7.dp),
            ) {
                state.sas.forEachIndexed { index, word ->
                    Column(
                        modifier = Modifier
                            .weight(1f)
                            .background(
                                HomingPalette.Raised,
                                RoundedCornerShape(10.dp),
                            )
                            .padding(vertical = 11.dp),
                        horizontalAlignment = Alignment.CenterHorizontally,
                    ) {
                        Text(
                            text = "${index + 1}",
                            style = MaterialTheme.typography.labelSmall.copy(
                                fontFamily = HomingMono,
                                fontSize = 10.sp,
                            ),
                            color = HomingPalette.Muted,
                        )
                        Text(
                            text = word,
                            modifier = Modifier.padding(top = 3.dp),
                            style = MaterialTheme.typography.titleLarge.copy(
                                fontSize = 20.sp,
                                fontWeight = FontWeight.SemiBold,
                            ),
                            color = HomingPalette.Brand,
                        )
                    }
                }
            }
            Text(
                text = "电脑指纹 ${state.fingerprint}",
                modifier = Modifier.padding(top = 12.dp),
                style = MaterialTheme.typography.labelSmall.copy(
                    fontFamily = HomingMono,
                    fontSize = 10.sp,
                ),
                color = HomingPalette.Muted,
            )
            Text(
                text = "等待电脑确认…",
                modifier = Modifier.padding(top = 8.dp),
                style = MaterialTheme.typography.labelLarge,
                color = HomingPalette.Warning,
            )
        }
    }
}

@Composable
private fun AcceptedCard(computer: PairedComputer, onDone: () -> Unit) {
    HomingCard {
        Column(
            modifier = Modifier.padding(16.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            // 配对成功：鸽子从电脑飞到手机，落定后停在那一帧
            HomingPigeonScene(
                scene = PigeonScene.PAIR,
                size = 84.dp,
                crop = PigeonCrop.STAGE,
                hold = true,
                contentDescription = "配对成功",
            )
            Text(
                text = "电脑已添加",
                modifier = Modifier.padding(top = 8.dp),
                style = MaterialTheme.typography.titleMedium,
                color = HomingPalette.Text,
            )
            Text(
                text = "指纹 ${computer.fingerprint}",
                modifier = Modifier.padding(top = 4.dp),
                style = MaterialTheme.typography.labelSmall.copy(
                    fontFamily = HomingMono,
                    fontSize = 10.sp,
                ),
                color = HomingPalette.Muted,
            )
            Button(
                onClick = onDone,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = 14.dp)
                    .height(48.dp),
                shape = RoundedCornerShape(10.dp),
            ) {
                Text("完成")
            }
        }
    }
}

@Composable
private fun StatusCard(
    icon: @Composable () -> Unit,
    title: String,
    detail: String,
    accent: Color = HomingPalette.Text,
    action: (@Composable () -> Unit)? = null,
) {
    HomingCard {
        Column(modifier = Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Box(modifier = Modifier.size(24.dp), contentAlignment = Alignment.Center) {
                    icon()
                }
                Spacer(Modifier.width(9.dp))
                Text(
                    text = title,
                    style = MaterialTheme.typography.titleSmall,
                    color = accent,
                )
            }
            Text(
                text = detail,
                modifier = Modifier.padding(top = 6.dp),
                style = MaterialTheme.typography.bodySmall.copy(lineHeight = 20.sp),
                color = HomingPalette.Muted,
            )
            if (action != null) {
                Box(modifier = Modifier.padding(top = 12.dp)) { action() }
            }
        }
    }
}
