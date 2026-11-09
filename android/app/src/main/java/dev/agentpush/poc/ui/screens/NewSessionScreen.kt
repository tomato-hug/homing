package dev.agentpush.poc.ui.screens

import android.app.Activity
import android.content.Intent
import android.speech.RecognizerIntent
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
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
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.AudioFile
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Mic
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.FilterChipDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.agentpush.poc.AgentBrand
import dev.agentpush.poc.AgentViewModel
import dev.agentpush.poc.NewSessionResult
import dev.agentpush.poc.ProjectInfo
import dev.agentpush.poc.workspaceFromCwd
import dev.agentpush.poc.media.PendingAttachment
import dev.agentpush.poc.media.PendingFiles
import dev.agentpush.poc.ui.AgentAvatar
import dev.agentpush.poc.ui.ModelPickerSheet
import dev.agentpush.poc.ui.ModelPill
import dev.agentpush.poc.ui.accentColor
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NewSessionScreen(
    viewModel: AgentViewModel,
    onBack: () -> Unit,
    onCreated: (String) -> Unit,
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var agent by rememberSaveable { mutableStateOf("codex") }
    var cwd by rememberSaveable { mutableStateOf("") }
    var text by rememberSaveable { mutableStateOf("") }
    var attachments by remember { mutableStateOf<List<PendingAttachment>>(emptyList()) }
    var creating by remember { mutableStateOf(false) }
    var uploadStage by remember { mutableStateOf<String?>(null) }
    var reusedWarning by remember { mutableStateOf<NewSessionResult?>(null) }
    var projectMenu by remember { mutableStateOf(false) }
    var modelPickerOpen by remember { mutableStateOf(false) }
    var workspaceFilter by rememberSaveable { mutableStateOf("") }
    var selectedModels by remember { mutableStateOf<Map<String, String>>(emptyMap()) }
    var selectedEfforts by remember { mutableStateOf<Map<String, String>>(emptyMap()) }

    val projects = state.projects
    val agentProfile = state.agentDirectory.profile(agent)
    val modelForAgent = selectedModels[agent].orEmpty()
    val effortForAgent = selectedEfforts[agent].orEmpty()
    val effectiveModel = modelForAgent.ifBlank {
        agentProfile.defaultModel.ifBlank {
            agentProfile.defaultModelOption?.id.orEmpty()
        }
    }
    val workspaces = remember(projects) {
        projects.mapNotNull { workspaceFromCwd(it.cwd).takeIf(String::isNotBlank) }
            .distinct()
            .sorted()
    }
    val filteredProjects = remember(projects, workspaceFilter) {
        if (workspaceFilter.isBlank()) {
            projects
        } else {
            projects.filter { workspaceFromCwd(it.cwd) == workspaceFilter }
        }
    }
    LaunchedEffect(Unit) {
        viewModel.refreshProjects { result ->
            if (cwd.isBlank()) {
                cwd = result.getOrNull()?.firstOrNull()?.cwd
                    ?: state.sessions().firstOrNull()?.cwd.orEmpty()
            }
        }
    }

    fun addUris(uris: List<android.net.Uri>) {
        if (uris.isEmpty()) return
        scope.launch {
            uris.forEach { uri ->
                runCatching { PendingFiles.fromUri(context, uri) }
                    .onSuccess { attachments = attachments + it }
                    .onFailure {
                        Toast.makeText(
                            context,
                            "读取附件失败：${it.message}",
                            Toast.LENGTH_SHORT,
                        ).show()
                    }
            }
        }
    }

    val photoPicker = rememberLauncherForActivityResult(
        ActivityResultContracts.PickMultipleVisualMedia(10),
    ) { addUris(it) }
    val filePicker = rememberLauncherForActivityResult(
        ActivityResultContracts.OpenMultipleDocuments(),
    ) { addUris(it) }
    val speechLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult(),
    ) { result ->
        if (result.resultCode == Activity.RESULT_OK) {
            result.data?.getStringArrayListExtra(RecognizerIntent.EXTRA_RESULTS)
                ?.firstOrNull()
                ?.takeIf { it.isNotBlank() }
                ?.let { text = if (text.isBlank()) it else "$text $it" }
        }
    }

    fun launchSpeech() {
        runCatching {
            speechLauncher.launch(
                Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
                    putExtra(
                        RecognizerIntent.EXTRA_LANGUAGE_MODEL,
                        RecognizerIntent.LANGUAGE_MODEL_FREE_FORM,
                    )
                    putExtra(RecognizerIntent.EXTRA_LANGUAGE, "zh-CN")
                    putExtra(RecognizerIntent.EXTRA_PROMPT, "说出任务内容")
                },
            )
        }.onFailure {
            Toast.makeText(context, "设备没有可用的语音识别服务", Toast.LENGTH_SHORT).show()
        }
    }

    val audioPermission = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { granted ->
        if (granted) launchSpeech()
    }

    Scaffold(
        containerColor = MaterialTheme.colorScheme.background,
        topBar = {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .statusBarsPadding()
                    .padding(start = 5.dp, end = 16.dp, top = 5.dp, bottom = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                IconButton(onClick = onBack) {
                    Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = "返回")
                }
                Text(
                    text = "新建任务",
                    style = MaterialTheme.typography.headlineSmall,
                    fontWeight = FontWeight.Bold,
                )
            }
        },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .verticalScroll(rememberScrollState())
                .navigationBarsPadding()
                .imePadding()
                .padding(horizontal = 18.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            SectionLabel("选择 Agent")
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                listOf(
                    "codex" to AgentBrand.CODEX,
                    "claude" to AgentBrand.CLAUDE,
                    "workbuddy" to AgentBrand.WORKBUDDY,
                ).forEach { (wire, brand) ->
                    FilterChip(
                        selected = agent == wire,
                        onClick = { agent = wire },
                        label = { Text(brand.displayName) },
                        leadingIcon = { AgentAvatar(brand = brand, size = 23.dp) },
                        shape = RoundedCornerShape(50),
                        colors = FilterChipDefaults.filterChipColors(
                            selectedContainerColor = brand.accentColor().copy(alpha = 0.16f),
                        ),
                    )
                }
            }

            SectionLabel("模型")
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                ModelPill(
                    label = effectiveModel.ifBlank { "未选择模型" },
                    enabled = agentProfile.capabilities.switchModel &&
                        agentProfile.models.isNotEmpty(),
                    overridden = modelForAgent.isNotBlank(),
                    onClick = { modelPickerOpen = true },
                )
                Text(
                    text = when {
                        !agentProfile.capabilities.switchModel &&
                            agent == AgentBrand.WORKBUDDY.wire ->
                            "WorkBuddy 不支持切换模型，将使用默认模型"
                        !agentProfile.capabilities.switchModel ->
                            "当前 Agent 不支持指定模型，将使用默认模型"
                        agentProfile.models.isEmpty() ->
                            "还没有拿到模型列表，将使用默认模型"
                        !agentProfile.capabilities.effort &&
                            agent == AgentBrand.CLAUDE.wire ->
                            "Claude 不支持推理强度"
                        effortForAgent.isNotBlank() ->
                            "推理强度：$effortForAgent"
                        else -> "可选，默认使用 Agent 当前模型"
                    },
                    modifier = Modifier.weight(1f),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }

            SectionLabel("工作区")
            if (workspaces.isEmpty()) {
                Text(
                    text = "中继还没有返回工作区列表，可直接填写项目路径。",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            } else {
                LazyRow(horizontalArrangement = Arrangement.spacedBy(7.dp)) {
                    item {
                        FilterChip(
                            selected = workspaceFilter.isBlank(),
                            onClick = { workspaceFilter = "" },
                            label = { Text("全部") },
                            shape = RoundedCornerShape(50),
                        )
                    }
                    items(workspaces) { workspace ->
                        FilterChip(
                            selected = workspaceFilter == workspace,
                            onClick = { workspaceFilter = workspace },
                            label = {
                                Text(
                                    workspace,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            },
                            shape = RoundedCornerShape(50),
                        )
                    }
                }
            }

            SectionLabel("项目目录")
            Box {
                OutlinedTextField(
                    value = cwd,
                    onValueChange = { cwd = it },
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text("Mac 上的项目绝对路径") },
                    placeholder = { Text("/Users/you/Projects/example") },
                    singleLine = true,
                    shape = RoundedCornerShape(13.dp),
                    trailingIcon = {
                        TextButton(onClick = { projectMenu = true }) {
                            Text("选择")
                        }
                    },
                )
                DropdownMenu(
                    expanded = projectMenu,
                    onDismissRequest = { projectMenu = false },
                ) {
                    if (filteredProjects.isEmpty()) {
                        DropdownMenuItem(
                            text = { Text("暂无历史项目，请手动输入路径") },
                            onClick = { projectMenu = false },
                        )
                    }
                    filteredProjects.forEach { project ->
                        ProjectMenuItem(
                            project = project,
                            onClick = {
                                cwd = project.cwd
                                projectMenu = false
                            },
                        )
                    }
                }
            }

            SectionLabel("任务内容")
            OutlinedTextField(
                value = text,
                onValueChange = { text = it },
                modifier = Modifier
                    .fillMaxWidth()
                    .height(170.dp),
                placeholder = { Text("描述你希望 Agent 完成的工作…") },
                textStyle = MaterialTheme.typography.bodyLarge,
                shape = RoundedCornerShape(14.dp),
                trailingIcon = {
                    IconButton(
                        onClick = {
                            if (androidx.core.content.ContextCompat.checkSelfPermission(
                                    context,
                                    android.Manifest.permission.RECORD_AUDIO,
                                ) == android.content.pm.PackageManager.PERMISSION_GRANTED
                            ) {
                                launchSpeech()
                            } else {
                                audioPermission.launch(android.Manifest.permission.RECORD_AUDIO)
                            }
                        },
                    ) {
                        Icon(Icons.Outlined.Mic, contentDescription = "语音输入")
                    }
                },
            )

            SectionLabel("附件")
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(
                    onClick = {
                        photoPicker.launch(
                            PickVisualMediaRequest(
                                ActivityResultContracts.PickVisualMedia.ImageOnly,
                            ),
                        )
                    },
                    shape = RoundedCornerShape(12.dp),
                ) {
                    Text("相册")
                }
                OutlinedButton(
                    onClick = { filePicker.launch(arrayOf("*/*")) },
                    shape = RoundedCornerShape(12.dp),
                ) {
                    Text("文件")
                }
            }
            if (attachments.isNotEmpty()) {
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    attachments.forEach { attachment ->
                        Surface(
                            modifier = Modifier.fillMaxWidth(),
                            color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.45f),
                            shape = RoundedCornerShape(10.dp),
                        ) {
                            Row(
                                modifier = Modifier.padding(start = 11.dp, end = 4.dp, top = 5.dp, bottom = 5.dp),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                Icon(
                                    Icons.Outlined.AudioFile,
                                    contentDescription = null,
                                    modifier = Modifier.size(18.dp),
                                )
                                Text(
                                    text = attachment.name,
                                    modifier = Modifier
                                        .weight(1f)
                                        .padding(horizontal = 8.dp),
                                    style = MaterialTheme.typography.bodySmall,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                                IconButton(
                                    onClick = {
                                        attachments = attachments.filterNot {
                                            it.id == attachment.id
                                        }
                                    },
                                    modifier = Modifier.size(32.dp),
                                ) {
                                    Icon(Icons.Outlined.Close, "移除", modifier = Modifier.size(17.dp))
                                }
                            }
                        }
                    }
                    Text(
                        text = "附件会先按当前 cwd 上传，随后和新任务一起提交。" +
                            "中继要求 cwd 已在 /api/projects 中；手输目录可能无法上传附件。",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }

            Button(
                onClick = {
                    if (cwd.isBlank()) {
                        Toast.makeText(context, "请填写项目目录", Toast.LENGTH_SHORT).show()
                        return@Button
                    }
                    if (text.isBlank()) {
                        Toast.makeText(context, "请填写任务内容", Toast.LENGTH_SHORT).show()
                        return@Button
                    }
                    creating = true
                    uploadStage = if (attachments.isNotEmpty()) "准备上传附件…" else null
                    viewModel.createSession(
                        source = agent,
                        cwd = cwd.trim(),
                        text = text.trim(),
                        localFiles = attachments,
                        model = effectiveModel.takeIf { modelForAgent.isNotBlank() },
                        effort = effortForAgent.takeIf { it.isNotBlank() },
                        onUploadProgress = { index, total, name, progress ->
                            if (creating) {
                                uploadStage = "上传 ${index + 1}/$total · $name " +
                                    "${(progress * 100).toInt()}%"
                            }
                        },
                    ) { result ->
                        creating = false
                        uploadStage = null
                        if (result.ok) {
                            if (result.status == "reused_active") {
                                reusedWarning = result
                            } else if (result.sessionId != null) {
                                onCreated(result.sessionId)
                            } else if (result.requestId != null) {
                                Toast.makeText(
                                    context,
                                    "Claude 会话正在启动，就绪后会自动打开",
                                    Toast.LENGTH_LONG,
                                ).show()
                                onBack()
                            } else {
                                Toast.makeText(
                                    context,
                                    "任务已启动，会话 ID 将由首条事件带回",
                                    Toast.LENGTH_LONG,
                                ).show()
                                onBack()
                            }
                        } else {
                            Toast.makeText(
                                context,
                                result.error ?: "启动失败",
                                Toast.LENGTH_LONG,
                            ).show()
                        }
                    }
                },
                enabled = !creating && cwd.isNotBlank() && text.isNotBlank(),
                modifier = Modifier
                    .fillMaxWidth()
                    .height(50.dp),
                shape = RoundedCornerShape(14.dp),
            ) {
                if (creating) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(18.dp),
                        strokeWidth = 2.dp,
                        color = MaterialTheme.colorScheme.onPrimary,
                    )
                    Spacer(Modifier.width(8.dp))
                    Text("正在启动…")
                } else {
                    Text(uploadStage ?: "启动任务")
                }
            }
            Spacer(Modifier.height(28.dp))
        }
    }

    if (modelPickerOpen) {
        ModelPickerSheet(
            profile = agentProfile,
            currentModel = effectiveModel,
            currentEffort = effortForAgent,
            onDismiss = { modelPickerOpen = false },
            onApply = { model, effort ->
                selectedModels = selectedModels + (agent to model)
                selectedEfforts = selectedEfforts + (agent to effort)
            },
        )
    }

    reusedWarning?.let { warning ->
        AlertDialog(
            onDismissRequest = {
                reusedWarning = null
                warning.sessionId?.let(onCreated) ?: onBack()
            },
            title = { Text("已发到 WorkBuddy 当前会话") },
            text = {
                Text(
                    warning.warning
                        ?: "WorkBuddy 当前版本不能新建会话，任务已发进桌面正在使用的会话，并非新会话。",
                )
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        reusedWarning = null
                        warning.sessionId?.let(onCreated) ?: onBack()
                    },
                ) {
                    Text("我知道了")
                }
            },
        )
    }
}

@Composable
private fun SectionLabel(text: String) {
    Text(
        text = text,
        style = MaterialTheme.typography.titleSmall,
        fontWeight = FontWeight.SemiBold,
        color = MaterialTheme.colorScheme.onSurface,
    )
}

@Composable
private fun ProjectMenuItem(project: ProjectInfo, onClick: () -> Unit) {
    DropdownMenuItem(
        text = {
            Column {
                Text(project.project, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(
                    project.cwd,
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        },
        onClick = onClick,
    )
}
