package dev.agentpush.poc.ui.screens

import android.app.Activity
import android.content.Intent
import android.speech.RecognizerIntent
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
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
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.AudioFile
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Mic
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
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
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.agentpush.poc.AgentBrand
import dev.agentpush.poc.AgentProfile
import dev.agentpush.poc.AgentViewModel
import dev.agentpush.poc.NewSessionResult
import dev.agentpush.poc.ProjectInfo
import dev.agentpush.poc.isAgentActiveOnMac
import dev.agentpush.poc.workspaceFromCwd
import dev.agentpush.poc.media.PendingAttachment
import dev.agentpush.poc.media.PendingFiles
import dev.agentpush.poc.ui.AgentAvatar
import dev.agentpush.poc.ui.ModelPickerSheet
import dev.agentpush.poc.ui.ModelPill
import dev.agentpush.poc.ui.accentColor
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NewSessionScreen(
    viewModel: AgentViewModel,
    initialCwd: String = "",
    onBack: () -> Unit,
    onCreated: (String) -> Unit,
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var agent by rememberSaveable { mutableStateOf("codex") }
    var cwd by rememberSaveable(initialCwd) { mutableStateOf(initialCwd) }
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
    var selectedPermissionModes by remember { mutableStateOf<Map<String, String>>(emptyMap()) }
    var refreshingAgents by remember { mutableStateOf(false) }
    var agentRefreshError by remember { mutableStateOf<String?>(null) }

    val projects = state.projects
    var wakingAgents by remember { mutableStateOf(setOf<String>()) }
    var continueOnlyAgent by remember { mutableStateOf<AgentProfile?>(null) }
    val agentStateOf: (AgentProfile) -> NewSessionAgentState = { profile ->
        val active = state.isAgentActiveOnMac(profile.source)
        when {
            profile.source in wakingAgents -> NewSessionAgentState.WAKING
            active && profile.supportsNewSession && profile.available -> NewSessionAgentState.READY
            active && !profile.supportsNewSession -> NewSessionAgentState.CONTINUE_ONLY
            !active && (profile.canWake || profile.installed) -> NewSessionAgentState.NEEDS_WAKE
            else -> NewSessionAgentState.UNAVAILABLE
        }
    }
    val creatableAgents = remember(state.agentDirectory, state.mobileWorkspace.wokenAgentIds, wakingAgents) {
        state.agentDirectory.agents.values
            .filter {
                it.supportsNewSession ||
                    it.capabilities.reply ||
                    it.source == AgentBrand.WORKBUDDY.wire ||
                    state.isAgentActiveOnMac(it.source)
            }
            .filter { it.installed || it.available || it.canWake || state.isAgentActiveOnMac(it.source) }
            .sortedWith(
                compareBy<AgentProfile> { agentStateOf(it).ordinal }
                    .thenBy(agentSortKey)
                    .thenBy { it.source },
            )
    }
    val agentProfile = state.agentDirectory.profile(agent)
    val selectedAgentOnline = agentStateOf(agentProfile) == NewSessionAgentState.READY
    val onlineAgentCount = creatableAgents.count { agentStateOf(it) == NewSessionAgentState.READY }
    val modelForAgent = selectedModels[agent].orEmpty()
    val effortForAgent = selectedEfforts[agent].orEmpty()
    val permissionModes = agentProfile.capabilities.permissionModes
    val permissionForAgent = selectedPermissionModes[agent].orEmpty()
        .ifBlank { "default" }
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
    LaunchedEffect(creatableAgents, agent) {
        if (agentStateOf(state.agentDirectory.profile(agent)) != NewSessionAgentState.READY) {
            creatableAgents.firstOrNull { agentStateOf(it) == NewSessionAgentState.READY }
                ?.let { agent = it.source }
        }
    }
    LaunchedEffect(Unit) {
        while (true) {
            refreshingAgents = true
            val result = viewModel.refreshAgentDirectory()
            refreshingAgents = false
            agentRefreshError = result.exceptionOrNull()?.message
            delay(20_000)
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
            NewSessionAgentPicker(
                agents = creatableAgents,
                selectedAgent = agent.takeIf { selectedAgentOnline }.orEmpty(),
                onlineCount = onlineAgentCount,
                loading = refreshingAgents && creatableAgents.isEmpty(),
                directoryAvailable = state.agentDirectory.available,
                error = agentRefreshError,
                onSelect = { agent = it },
                agentState = agentStateOf,
                onWake = { source ->
                    if (source !in wakingAgents) {
                        wakingAgents = wakingAgents + source
                        viewModel.wakeAgent(source) { result ->
                            scope.launch {
                                runCatching { viewModel.refreshAgentDirectory() }
                                wakingAgents = wakingAgents - source
                                if (result.isSuccess &&
                                    state.agentDirectory.profile(source).supportsNewSession
                                ) {
                                    agent = source
                                }
                            }
                        }
                    }
                },
                onContinueOnly = { continueOnlyAgent = it },
                onRefresh = {
                    if (!refreshingAgents) {
                        scope.launch {
                            refreshingAgents = true
                            val result = viewModel.refreshAgentDirectory()
                            refreshingAgents = false
                            agentRefreshError = result.exceptionOrNull()?.message
                        }
                    }
                },
            )

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

            if (permissionModes.isNotEmpty()) {
                SectionLabel("权限模式")
                LazyRow(horizontalArrangement = Arrangement.spacedBy(7.dp)) {
                    items(permissionModes) { mode ->
                        FilterChip(
                            selected = permissionForAgent == mode,
                            onClick = {
                                selectedPermissionModes =
                                    selectedPermissionModes + (agent to mode)
                            },
                            label = { Text(permissionModeLabel(mode)) },
                            shape = RoundedCornerShape(50),
                        )
                    }
                }
                Text(
                    text = "权限模式会随新会话的启动参数一起提交；Agent 不支援的模式不会出现在这里。",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
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
                    placeholder = { Text("Mac 上的项目绝对路径") },
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
                        permissionMode = permissionForAgent
                            .takeIf { it.isNotBlank() && it != "default" },
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
                enabled = !creating &&
                    selectedAgentOnline &&
                    cwd.isNotBlank() &&
                    text.isNotBlank(),
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
                    Text(
                        uploadStage ?: if (selectedAgentOnline) {
                            "启动 ${agentDisplayName(agentProfile)} 任务"
                        } else {
                            "没有可启动的 Agent"
                        },
                    )
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

    continueOnlyAgent?.let { profile ->
        val latest = remember(state.sessionMeta, state.events, profile.source) {
            state.sessions()
                .filter { it.source.equals(profile.source, ignoreCase = true) && !it.archived && !it.isEmpty }
                .maxByOrNull { it.lastTs }
        }
        AlertDialog(
            onDismissRequest = { continueOnlyAgent = null },
            title = { Text("${agentDisplayName(profile)} 已在线") },
            text = {
                Text(
                    if (latest != null) {
                        "${agentDisplayName(profile)} 桌面端不支持由外部新建独立会话。可以直接在它最近的会话「${latest.title}」里继续发消息，内容会注入到电脑上正在使用的会话。"
                    } else {
                        "${agentDisplayName(profile)} 桌面端不支持由外部新建独立会话，目前也没有可续接的会话。请先在电脑上的 ${agentDisplayName(profile)} 里开启一个会话。"
                    },
                )
            },
            confirmButton = {
                if (latest != null) {
                    TextButton(
                        onClick = {
                            continueOnlyAgent = null
                            viewModel.restoreHistoricalSessions(listOf(latest.sessionId), itemLimit = 10) {
                                onCreated(latest.sessionId)
                            }
                        },
                        modifier = Modifier.testTag("new-session-continue-latest"),
                    ) {
                        Text("打开最近会话继续")
                    }
                }
            },
            dismissButton = {
                TextButton(onClick = { continueOnlyAgent = null }) { Text("取消") }
            },
        )
    }
}

enum class NewSessionAgentState {
    READY,
    CONTINUE_ONLY,
    WAKING,
    NEEDS_WAKE,
    UNAVAILABLE,
}

internal fun newSessionAgentStateLabel(source: String, state: NewSessionAgentState): String =
    when (state) {
        NewSessionAgentState.READY -> newSessionAvailabilityLabel(source, true)
        NewSessionAgentState.CONTINUE_ONLY -> "已在线 · 仅可续聊"
        NewSessionAgentState.WAKING -> "正在唤醒…"
        NewSessionAgentState.NEEDS_WAKE -> "未唤醒 · 点击唤醒"
        NewSessionAgentState.UNAVAILABLE -> newSessionAvailabilityLabel(source, false)
    }

@Composable
internal fun NewSessionAgentPicker(
    agents: List<AgentProfile>,
    selectedAgent: String,
    onlineCount: Int,
    loading: Boolean,
    directoryAvailable: Boolean,
    error: String?,
    onSelect: (String) -> Unit,
    onRefresh: () -> Unit,
    agentState: (AgentProfile) -> NewSessionAgentState = { profile ->
        if (profile.isOnlineForNewSession) NewSessionAgentState.READY else NewSessionAgentState.UNAVAILABLE
    },
    onWake: (String) -> Unit = {},
    onContinueOnly: (AgentProfile) -> Unit = {},
) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        SectionLabel("选择 Agent")
        Spacer(Modifier.weight(1f))
        if (loading) {
            CircularProgressIndicator(
                modifier = Modifier.size(15.dp),
                strokeWidth = 2.dp,
            )
            Spacer(Modifier.width(8.dp))
        }
        IconButton(
            onClick = onRefresh,
            enabled = !loading,
            modifier = Modifier
                .size(36.dp)
                .testTag("new-session-agent-refresh"),
        ) {
            Icon(
                Icons.Outlined.Refresh,
                contentDescription = "刷新 Agent 状态",
                modifier = Modifier.size(18.dp),
            )
        }
    }
    Text(
        text = when {
            !directoryAvailable && !loading -> "正在等待电脑返回 Agent 状态…"
            onlineCount == 0 -> "电脑上暂无已唤醒的 Agent；点击灰色卡片即可远程唤醒"
            else -> "$onlineCount 个已唤醒的 Agent 可直接新建任务；未唤醒的 Agent 点击卡片即可远程唤醒"
        },
        style = MaterialTheme.typography.labelSmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    if (!error.isNullOrBlank()) {
        Text(
            text = "Agent 状态刷新失败：$error",
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.error,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
    }
    if (agents.isEmpty()) {
        Surface(
            modifier = Modifier
                .fillMaxWidth()
                .testTag("new-session-agent-empty"),
            color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.34f),
            shape = RoundedCornerShape(13.dp),
        ) {
            Text(
                text = if (loading) {
                    "正在检测可用的 Agent…"
                } else {
                    "没有可新建会话的 Agent，请先在电脑端完成接入。"
                },
                modifier = Modifier.padding(horizontal = 14.dp, vertical = 13.dp),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    } else {
        LazyRow(
            modifier = Modifier
                .fillMaxWidth()
                .testTag("new-session-agent-list"),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            items(agents, key = { it.source }) { profile ->
                val cardState = agentState(profile)
                NewSessionAgentCard(
                    profile = profile,
                    cardState = cardState,
                    selected = selectedAgent == profile.source,
                    onClick = {
                        when (cardState) {
                            NewSessionAgentState.READY -> onSelect(profile.source)
                            NewSessionAgentState.NEEDS_WAKE -> onWake(profile.source)
                            NewSessionAgentState.CONTINUE_ONLY -> onContinueOnly(profile)
                            NewSessionAgentState.WAKING,
                            NewSessionAgentState.UNAVAILABLE,
                            -> Unit
                        }
                    },
                )
            }
        }
    }
}

@Composable
private fun NewSessionAgentCard(
    profile: AgentProfile,
    cardState: NewSessionAgentState,
    selected: Boolean,
    onClick: () -> Unit,
) {
    val online = cardState == NewSessionAgentState.READY
    val clickable = cardState != NewSessionAgentState.UNAVAILABLE && cardState != NewSessionAgentState.WAKING
    val brand = profile.brand
    val accent = brand.accentColor()
    Surface(
        onClick = onClick,
        enabled = clickable,
        modifier = Modifier
            .width(166.dp)
            .heightIn(min = 72.dp)
            .alpha(
                when (cardState) {
                    NewSessionAgentState.READY -> 1f
                    NewSessionAgentState.CONTINUE_ONLY -> 0.9f
                    NewSessionAgentState.NEEDS_WAKE, NewSessionAgentState.WAKING -> 0.72f
                    NewSessionAgentState.UNAVAILABLE -> 0.38f
                },
            )
            .testTag("new-session-agent-${profile.source}"),
        shape = RoundedCornerShape(14.dp),
        color = if (selected && online) {
            accent.copy(alpha = 0.13f)
        } else {
            MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.34f)
        },
        border = BorderStroke(
            1.dp,
            if (selected && online) {
                accent.copy(alpha = 0.72f)
            } else {
                MaterialTheme.colorScheme.outline.copy(alpha = 0.18f)
            },
        ),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 11.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(9.dp),
        ) {
            AgentAvatar(
                brand = brand,
                source = profile.source,
                size = 31.dp,
            )
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = agentDisplayName(profile),
                    style = MaterialTheme.typography.bodyMedium,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.onSurface,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Row(
                    modifier = Modifier.padding(top = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(5.dp),
                ) {
                    Box(
                        modifier = Modifier
                            .size(6.dp)
                            .clip(CircleShape)
                            .background(
                                if (online) {
                                    MaterialTheme.colorScheme.primary
                                } else {
                                    MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.5f)
                                },
                            ),
                    )
                    Text(
                        text = newSessionAgentStateLabel(profile.source, cardState),
                        modifier = Modifier.testTag(
                            "new-session-agent-status-${profile.source}",
                        ),
                        style = MaterialTheme.typography.labelSmall,
                        color = if (online) {
                            MaterialTheme.colorScheme.primary
                        } else {
                            MaterialTheme.colorScheme.onSurfaceVariant
                        },
                    )
                }
            }
        }
    }
}

private val agentSortKey: (AgentProfile) -> Int = { profile ->
    when (profile.source) {
        "codex" -> 0
        "claude" -> 1
        "workbuddy" -> 2
        else -> 10
    }
}

private fun agentDisplayName(profile: AgentProfile): String =
    profile.displayName.ifBlank { dev.agentpush.poc.defaultAgentDisplayName(profile.source) }

private fun permissionModeLabel(value: String): String {
    return when (value.trim().lowercase().replace('-', '_')) {
        "", "default", "manual" -> "默认"
        "plan" -> "计划"
        "accept_edits", "auto" -> "自动接受编辑"
        "read_only", "readonly" -> "只读"
        else -> value
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

/**
 * 新建会话列表里每个 Agent 的状态文字。这里说的是“能不能从手机新建”，不是桌面端开没开：
 * Codex、Claude Code 由电脑在后台直接运行命令行启动；WorkBuddy 要靠正在运行的桌面端。
 */
internal fun newSessionAvailabilityLabel(source: String, available: Boolean): String =
    when {
        available && (source == "codex" || source == "claude") -> "可后台启动"
        available && source == "workbuddy" -> "桌面端已打开"
        available -> "可启动"
        source == "workbuddy" -> "需先打开 WorkBuddy"
        else -> "未就绪"
    }
