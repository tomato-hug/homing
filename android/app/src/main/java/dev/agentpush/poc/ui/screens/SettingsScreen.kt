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
import dev.agentpush.poc.ui.CodexGreen
import dev.agentpush.poc.ui.SectionCard

@Composable
fun SettingsScreen(
    viewModel: AgentViewModel,
    snackbarHostState: SnackbarHostState,
    onBack: () -> Unit,
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val settingsUi by viewModel.settingsUi.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val clipboard = LocalClipboardManager.current
    var server by rememberSaveable { mutableStateOf(state.settings.baseUrl) }
    var token by rememberSaveable { mutableStateOf(state.settings.token) }
    var permissionTick by remember { mutableIntStateOf(0) }
    var batteryTick by remember { mutableIntStateOf(0) }
    var confirmClearCache by remember { mutableStateOf(false) }

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
        context.getSystemService(PowerManager::class.java)
            .isIgnoringBatteryOptimizations(context.packageName)
    }
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
            SettingsHeader(onBack)
            Column(
                modifier = Modifier.padding(horizontal = 20.dp),
                verticalArrangement = Arrangement.spacedBy(14.dp),
            ) {
                SectionCard {
                    SectionTitle("连接")
                    Text(
                        text = "填写 Mac 上中继服务的地址和 token。",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Spacer(Modifier.height(14.dp))
                    OutlinedTextField(
                        value = server,
                        onValueChange = { server = it },
                        modifier = Modifier.fillMaxWidth(),
                        label = { Text("服务器地址") },
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
                        value = if (batteryIgnored) "已允许后台运行" else "可能被系统限制",
                        good = batteryIgnored,
                        actionLabel = if (batteryIgnored) null else "去设置",
                        onAction = { requestBatteryWhitelist(context) },
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
                    SectionTitle("ColorOS 保活指引")
                    GuideStep("1", "允许自启动", "设置 → 应用 → 自启动，把「Homing」设为允许。")
                    GuideStep("2", "允许后台运行", "应用信息 → 耗电管理，关闭「智能限制后台运行」或选「允许后台运行」。")
                    GuideStep("3", "锁定最近任务", "在最近任务卡片上长按本应用并选择「锁定」，减少被清理的概率。")
                    Spacer(Modifier.height(3.dp))
                    Text(
                        text = "ColorOS 各版本文案略有差异，核心是同时允许自启动、后台运行和通知。",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }

                SectionCard {
                    SectionTitle("本地数据")
                    Text(
                        text = "Homing 会在本机保存首頁快照、最近会话和未发送草稿，方便飞航模式或弱网时打开。",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Spacer(Modifier.height(12.dp))
                    OutlinedButton(
                        onClick = { confirmClearCache = true },
                        modifier = Modifier.fillMaxWidth(),
                        shape = RoundedCornerShape(13.dp),
                    ) {
                        Text("清除本地快照与草稿")
                    }
                }

                Text(
                    text = "Homing ${dev.agentpush.poc.BuildConfig.VERSION_NAME} · PoC",
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
            title = { Text("清除本地数据？") },
            text = {
                Text("这不会删除 Mac 上的会话或 Homing 配置，只会清掉这台手机上缓存的首頁快照、会话摘要和草稿。")
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        confirmClearCache = false
                        viewModel.clearLocalCache()
                    },
                ) {
                    Text("清除")
                }
            },
            dismissButton = {
                TextButton(onClick = { confirmClearCache = false }) {
                    Text("取消")
                }
            },
        )
    }
}

@Composable
private fun SettingsHeader(onBack: () -> Unit) {
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
            text = "设置",
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

private fun requestBatteryWhitelist(context: android.content.Context) {
    val directIntent = Intent(
        Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS,
        Uri.parse("package:${context.packageName}"),
    )
    try {
        context.startActivity(directIntent)
    } catch (_: Exception) {
        context.startActivity(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS))
    }
}
