package dev.agentpush.poc.ui.screens

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ContentPaste
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material3.Button
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.agentpush.poc.AgentViewModel
import dev.agentpush.poc.CrashReporter
import dev.agentpush.poc.KeepAliveHelper
import dev.agentpush.poc.RomKeepAliveProfile
import dev.agentpush.poc.ui.AppLanguage
import dev.agentpush.poc.ui.AboutAndSponsorCard
import dev.agentpush.poc.ui.CodexGreen
import dev.agentpush.poc.ui.CreditsAndLicensesSheet
import dev.agentpush.poc.ui.LanguagePreferences
import dev.agentpush.poc.ui.LocalHomingStrings
import dev.agentpush.poc.ui.SectionCard
import dev.agentpush.poc.ui.SponsorBottomSheet

@Composable
fun SettingsScreen(
    viewModel: AgentViewModel,
    snackbarHostState: SnackbarHostState,
    onBack: () -> Unit,
) {
    val strings = LocalHomingStrings.current
    val currentLanguage by LanguagePreferences.language.collectAsStateWithLifecycle()
    val state by viewModel.state.collectAsStateWithLifecycle()
    val settingsUi by viewModel.settingsUi.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val clipboard = LocalClipboardManager.current
    var server by rememberSaveable { mutableStateOf(state.settings.baseUrl) }
    var token by rememberSaveable { mutableStateOf(state.settings.token) }
    var permissionTick by remember { mutableIntStateOf(0) }
    var batteryTick by remember { mutableIntStateOf(0) }
    var confirmClearCache by remember { mutableStateOf(false) }
    var crashReportingEnabled by remember { mutableStateOf(CrashReporter.isEnabled(context)) }
    var licensesOpen by remember { mutableStateOf(false) }
    var sponsorOpen by remember { mutableStateOf(false) }

    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) {
        permissionTick += 1
        batteryTick += 1
    }

    val notificationsGranted = remember(permissionTick) {
        Build.VERSION.SDK_INT < 33 ||
            ContextCompat.checkSelfPermission(
                context,
                Manifest.permission.POST_NOTIFICATIONS,
            ) == PackageManager.PERMISSION_GRANTED
    }
    val batteryIgnored = remember(batteryTick) {
        KeepAliveHelper.isIgnoringBatteryOptimizations(context)
    }
    val detectedRom = remember { KeepAliveHelper.detectCurrentRom() }
    var selectedRomId by rememberSaveable { mutableStateOf(detectedRom.id) }
    val selectedRom = RomKeepAliveProfile.entries.firstOrNull { it.id == selectedRomId } ?: detectedRom
    val permissionLauncher = rememberLauncherForActivityResult(
        contract = ActivityResultContracts.RequestPermission(),
    ) {
        permissionTick += 1
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbarHostState) },
        containerColor = MaterialTheme.colorScheme.background,
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .verticalScroll(rememberScrollState())
                .padding(bottom = 28.dp),
        ) {
            SettingsHeader(strings.settingsTitle, onBack)
            Column(
                modifier = Modifier.padding(horizontal = 20.dp),
                verticalArrangement = Arrangement.spacedBy(14.dp),
            ) {
                SectionCard {
                    SectionTitle(strings.languageSection)
                    Text(
                        text = strings.languageDesc,
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Spacer(Modifier.height(10.dp))
                    Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        AppLanguage.entries.forEach { lang ->
                            FilterChip(
                                selected = currentLanguage == lang,
                                onClick = { LanguagePreferences.save(context, lang) },
                                label = { Text(lang.displayName) },
                            )
                        }
                    }
                }

                SectionCard {
                    SectionTitle(strings.connectionSection)
                    Text(
                        text = strings.connectionDesc,
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Spacer(Modifier.height(14.dp))
                    OutlinedTextField(
                        value = server,
                        onValueChange = { server = it },
                        modifier = Modifier.fillMaxWidth(),
                        label = { Text(strings.serverAddress) },
                        placeholder = { Text("http://192.168.x.x:18787") },
                        singleLine = true,
                        shape = RoundedCornerShape(14.dp),
                    )
                    Spacer(Modifier.height(10.dp))
                    OutlinedTextField(
                        value = token,
                        onValueChange = { token = it },
                        modifier = Modifier.fillMaxWidth(),
                        label = { Text("Token") },
                        singleLine = true,
                        visualTransformation = PasswordVisualTransformation(),
                        shape = RoundedCornerShape(14.dp),
                        trailingIcon = {
                            TextButton(
                                onClick = {
                                    clipboard.getText()?.text?.trim()?.let { pasted ->
                                        if (pasted.isNotBlank()) token = pasted
                                    }
                                },
                            ) {
                                Icon(
                                    imageVector = Icons.Outlined.ContentPaste,
                                    contentDescription = null,
                                    modifier = Modifier.size(17.dp),
                                )
                                Spacer(Modifier.width(3.dp))
                                Text("粘贴", style = MaterialTheme.typography.labelMedium)
                            }
                        },
                    )
                    Spacer(Modifier.height(14.dp))
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        OutlinedButton(
                            onClick = { viewModel.testConnection(server, token) },
                            enabled = !settingsUi.testing && !settingsUi.saving,
                            modifier = Modifier.weight(1f),
                            shape = RoundedCornerShape(13.dp),
                        ) {
                            if (settingsUi.testing) {
                                CircularProgressIndicator(
                                    modifier = Modifier.size(16.dp),
                                    strokeWidth = 2.dp,
                                )
                                Spacer(Modifier.width(7.dp))
                            }
                            Text("测试连接")
                        }
                        Button(
                            onClick = { viewModel.saveAndReconnect(server, token) },
                            enabled = !settingsUi.testing && !settingsUi.saving,
                            modifier = Modifier.weight(1f),
                            shape = RoundedCornerShape(13.dp),
                        ) {
                            if (settingsUi.saving) {
                                CircularProgressIndicator(
                                    modifier = Modifier.size(16.dp),
                                    strokeWidth = 2.dp,
                                    color = MaterialTheme.colorScheme.onPrimary,
                                )
                                Spacer(Modifier.width(7.dp))
                            }
                            Text("保存并重连")
                        }
                    }
                    settingsUi.message?.let { message ->
                        Spacer(Modifier.height(11.dp))
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(7.dp),
                        ) {
                            val color = if (settingsUi.testSucceeded == false) {
                                MaterialTheme.colorScheme.error
                            } else {
                                CodexGreen
                            }
                            Icon(
                                imageVector = if (settingsUi.testSucceeded == false) {
                                    Icons.Outlined.ErrorOutline
                                } else {
                                    Icons.Outlined.CheckCircle
                                },
                                contentDescription = null,
                                tint = color,
                                modifier = Modifier.size(17.dp),
                            )
                            Text(
                                text = message,
                                style = MaterialTheme.typography.bodySmall,
                                color = color,
                            )
                        }
                    }
                }

                SectionCard {
                    SectionTitle("协作")
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(14.dp),
                    ) {
                        Column(modifier = Modifier.weight(1f)) {
                            Text(
                                text = "发送前预览",
                                style = MaterialTheme.typography.bodyLarge,
                                fontWeight = FontWeight.Medium,
                            )
                            Spacer(Modifier.height(3.dp))
                            Text(
                                text = "有 @Agent 时先显示收件人和任务拆分，确认后再发送。",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        Switch(
                            checked = state.settings.previewMentions,
                            onCheckedChange = { viewModel.setPreviewMentions(it) },
                        )
                    }
                }

                SectionCard {
                    SectionTitle("系统权限")
                    PermissionRow(
                        title = "通知权限",
                        value = if (notificationsGranted) "已允许" else "未允许",
                        good = notificationsGranted,
                        actionLabel = if (notificationsGranted) null else "授权",
                        onAction = {
                            if (Build.VERSION.SDK_INT >= 33) {
                                permissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
                            } else {
                                context.startActivity(
                                    Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS).apply {
                                        putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName)
                                    },
                                )
                            }
                        },
                    )
                    Spacer(Modifier.height(13.dp))
                    PermissionRow(
                        title = "电池优化白名单",
                        value = if (batteryIgnored) "已允许后台无限制运行" else "可能被系统省电策略限制",
                        good = batteryIgnored,
                        actionLabel = if (batteryIgnored) null else "一键加白名单",
                        onAction = { KeepAliveHelper.requestIgnoreBatteryOptimizations(context) },
                    )
                    if (!notificationsGranted) {
                        Spacer(Modifier.height(4.dp))
                        TextButton(
                            onClick = {
                                context.startActivity(
                                    Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS).apply {
                                        putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName)
                                    },
                                )
                            },
                        ) {
                            Text("打开系统通知设置")
                        }
                    }
                }

                SectionCard {
                    SectionTitle("多机型后台保活适配")
                    Text(
                        text = "已自动识别本机系统：${detectedRom.shortLabel}（支持开机/升级自恢复与局域网休眠不断连，也可切换查看其他品牌设置）",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Spacer(Modifier.height(10.dp))
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .horizontalScroll(rememberScrollState()),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        RomKeepAliveProfile.entries.forEach { rom ->
                            FilterChip(
                                selected = selectedRom == rom,
                                onClick = { selectedRomId = rom.id },
                                modifier = Modifier.testTag("keepalive-rom-${rom.id}"),
                                label = {
                                    Text(
                                        if (rom == detectedRom) "本机 · ${rom.shortLabel}" else rom.shortLabel,
                                    )
                                },
                            )
                        }
                    }
                    Spacer(Modifier.height(8.dp))
                    Text(
                        text = selectedRom.fullTitle,
                        style = MaterialTheme.typography.labelLarge,
                        fontWeight = FontWeight.SemiBold,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Spacer(Modifier.height(4.dp))
                    selectedRom.steps.forEach { step ->
                        GuideStep(step.number, step.title, step.body)
                    }
                    Spacer(Modifier.height(4.dp))
                    Text(
                        text = selectedRom.note,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Spacer(Modifier.height(12.dp))
                    OutlinedButton(
                        onClick = { KeepAliveHelper.openOemAutoStartSettings(context, selectedRom) },
                        modifier = Modifier
                            .fillMaxWidth()
                            .testTag("settings-open-oem-autostart"),
                        shape = RoundedCornerShape(13.dp),
                    ) {
                        Text(selectedRom.autoStartButtonLabel)
                    }
                    if (!batteryIgnored) {
                        Spacer(Modifier.height(8.dp))
                        Button(
                            onClick = { KeepAliveHelper.requestIgnoreBatteryOptimizations(context) },
                            modifier = Modifier
                                .fillMaxWidth()
                                .testTag("settings-request-battery-unrestricted"),
                            shape = RoundedCornerShape(13.dp),
                        ) {
                            Text("一键申请后台无限制白名单")
                        }
                    }
                }

                SectionCard {
                    SectionTitle(strings.localDataSection)
                    Text(
                        text = strings.localDataDesc,
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Spacer(Modifier.height(12.dp))
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(14.dp),
                    ) {
                        Column(modifier = Modifier.weight(1f)) {
                            Text(
                                text = strings.crashReportingTitle,
                                style = MaterialTheme.typography.bodyLarge,
                                fontWeight = FontWeight.Medium,
                            )
                            Spacer(Modifier.height(3.dp))
                            Text(
                                text = strings.crashReportingDesc,
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        Switch(
                            checked = crashReportingEnabled,
                            onCheckedChange = {
                                crashReportingEnabled = it
                                CrashReporter.setEnabled(context, it)
                            },
                        )
                    }
                    Spacer(Modifier.height(12.dp))
                    OutlinedButton(
                        onClick = { confirmClearCache = true },
                        modifier = Modifier.fillMaxWidth(),
                        shape = RoundedCornerShape(13.dp),
                    ) {
                        Text(strings.clearLocalData)
                    }
                }

                AboutAndSponsorCard(
                    onOpenSponsor = { sponsorOpen = true },
                    onOpenCredits = { licensesOpen = true },
                )

                Text(
                    text = "Homing ${dev.agentpush.poc.BuildConfig.VERSION_NAME}",
                    modifier = Modifier.fillMaxWidth(),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                )
            }
        }
    }

    if (confirmClearCache) {
        AlertDialog(
            onDismissRequest = { confirmClearCache = false },
            title = { Text(strings.clearLocalDataTitle) },
            text = {
                Text(strings.clearLocalDataConfirm)
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        confirmClearCache = false
                        viewModel.clearLocalCache()
                    },
                ) {
                    Text(strings.confirm)
                }
            },
            dismissButton = {
                TextButton(onClick = { confirmClearCache = false }) {
                    Text(strings.cancel)
                }
            },
        )
    }

    if (sponsorOpen) {
        SponsorBottomSheet(onDismiss = { sponsorOpen = false })
    }

    if (licensesOpen) {
        CreditsAndLicensesSheet(onDismiss = { licensesOpen = false })
    }
}

@Composable
private fun SettingsHeader(title: String, onBack: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .statusBarsPadding()
            .padding(start = 6.dp, end = 20.dp, top = 6.dp, bottom = 13.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        IconButton(onClick = onBack) {
            Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = "返回")
        }
        Text(
            text = title,
            style = MaterialTheme.typography.headlineMedium,
            fontWeight = FontWeight.Bold,
        )
    }
}

@Composable
private fun SectionTitle(text: String) {
    Text(
        text = text,
        style = MaterialTheme.typography.titleMedium,
        color = MaterialTheme.colorScheme.onSurface,
        modifier = Modifier.padding(bottom = 7.dp),
    )
}

@Composable
private fun PermissionRow(
    title: String,
    value: String,
    good: Boolean,
    actionLabel: String?,
    onAction: () -> Unit,
) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Surface(
            modifier = Modifier.size(9.dp),
            shape = RoundedCornerShape(50),
            color = if (good) CodexGreen else MaterialTheme.colorScheme.error,
        ) {}
        Column(modifier = Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.labelLarge)
            Text(
                value,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        if (actionLabel != null) {
            TextButton(onClick = onAction) {
                Text(actionLabel)
            }
        }
    }
}

@Composable
private fun GuideStep(number: String, title: String, body: String) {
    Row(
        modifier = Modifier.padding(vertical = 6.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Box(
            modifier = Modifier
                .size(23.dp)
                .background(
                    MaterialTheme.colorScheme.primary.copy(alpha = 0.12f),
                    RoundedCornerShape(7.dp),
                ),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                text = number,
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.primary,
            )
        }
        Column(modifier = Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.labelLarge)
            Text(
                body,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}
