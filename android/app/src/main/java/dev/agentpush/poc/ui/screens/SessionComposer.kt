package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.CallSplit
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.outlined.AlternateEmail
import androidx.compose.material.icons.outlined.AttachFile
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.Mic
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.Stop
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TextFieldDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import dev.agentpush.poc.AgentBrand
import dev.agentpush.poc.AgentMention
import dev.agentpush.poc.AgentMentionOptions
import dev.agentpush.poc.AgentProfile
import dev.agentpush.poc.CompletedPath
import dev.agentpush.poc.ComposeResult
import dev.agentpush.poc.SessionCommand
import dev.agentpush.poc.agentMentionOptionKey
import dev.agentpush.poc.media.PendingAttachment
import dev.agentpush.poc.ui.CodexGreen
import dev.agentpush.poc.ui.ModelPill
import dev.agentpush.poc.ui.accentColor
import dev.agentpush.poc.ui.theme.HomingMono
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

@Composable
internal fun ComposePreviewCard(
    preview: ComposeResult,
    sending: Boolean,
    onConfirm: () -> Unit,
    onCancel: () -> Unit,
) {
    Surface(
        color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.98f),
        tonalElevation = 6.dp,
        shadowElevation = 8.dp,
        shape = RoundedCornerShape(topStart = 18.dp, topEnd = 18.dp),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 14.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(7.dp),
            ) {
                Icon(
                    Icons.AutoMirrored.Outlined.CallSplit,
                    contentDescription = null,
                    modifier = Modifier.size(17.dp),
                    tint = CodexGreen,
                )
                Text(
                    text = "发送前预览",
                    style = MaterialTheme.typography.titleSmall,
                    fontWeight = FontWeight.SemiBold,
                )
                Spacer(Modifier.weight(1f))
                TextButton(onClick = onCancel, enabled = !sending) {
                    Text("取消")
                }
            }
            if (preview.plan.message.isNotBlank()) {
                Text(
                    text = "发给当前会话",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(
                    text = preview.plan.message,
                    style = MaterialTheme.typography.bodySmall,
                    maxLines = 3,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            if (preview.plan.mentions.isNotEmpty()) {
                Text(
                    text = "将建立 ${preview.plan.mentions.size} 个协作任务",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                for (mention in preview.plan.mentions) {
                    val brand = AgentBrand.fromWire(mention.agent)
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        Surface(
                            color = brand.accentColor().copy(alpha = 0.16f),
                            shape = CircleShape,
                        ) {
                            Text(
                                text = "@${brand.displayName}",
                                modifier = Modifier.padding(horizontal = 8.dp, vertical = 2.dp),
                                style = MaterialTheme.typography.labelSmall,
                                color = brand.accentColor(),
                                fontWeight = FontWeight.SemiBold,
                            )
                        }
                        Text(
                            text = buildString {
                                append(mention.presetLabel)
                                append(" · ")
                                append(
                                    when (mention.workspace) {
                                        "readonly" -> "唯读"
                                        "worktree" -> "独立分支"
                                        else -> "共享目录"
                                    },
                                )
                                if (mention.target == "continue") append(" · 接着上一条")
                                if (mention.context.isNotEmpty()) {
                                    append(" · 背景：")
                                    append(
                                        mention.context.joinToString("、") {
                                            when (it) {
                                                "recent" -> "最近对话"
                                                "files" -> "改动文件"
                                                "diff" -> "git diff"
                                                else -> it
                                            }
                                        },
                                    )
                                }
                            },
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    Text(
                        text = mention.text,
                        style = MaterialTheme.typography.bodySmall,
                        maxLines = 3,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(
                    text = "确认后才会发送",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.outline,
                )
                Spacer(Modifier.weight(1f))
                Button(
                    onClick = onConfirm,
                    enabled = !sending,
                    shape = RoundedCornerShape(12.dp),
                ) {
                    if (sending) {
                        CircularProgressIndicator(
                            modifier = Modifier.size(14.dp),
                            strokeWidth = 2.dp,
                            color = MaterialTheme.colorScheme.onPrimary,
                        )
                        Spacer(Modifier.width(6.dp))
                    }
                    Text("确认发送")
                }
            }
        }
    }
}

@Composable
internal fun SessionComposer(
    text: String,
    onTextChange: (String) -> Unit,
    pending: List<PendingAttachment>,
    modelLabel: String,
    effortLabel: String,
    showEffortPill: Boolean,
    permissionLabel: String,
    permissionModes: List<String>,
    onPermissionChange: (String) -> Unit,
    modelOverride: Boolean,
    canSwitchModel: Boolean,
    modelDisabledReason: String,
    onModelClick: () -> Unit,
    dispatchTargets: List<AgentProfile>,
    mentions: List<AgentMention>,
    mentionOptions: Map<String, AgentMentionOptions>,
    onMentionPresetChange: (AgentMention, String) -> Unit,
    onMentionOptionsChange: (String, AgentMentionOptions) -> Unit,
    commands: List<SessionCommand>,
    cwd: String,
    completeFiles: suspend (String, String) -> List<CompletedPath>,
    enabled: Boolean,
    canSend: Boolean,
    queuedCount: Int,
    onCancelQueued: () -> Unit,
    onRemove: (String) -> Unit,
    onRetryUpload: (PendingAttachment) -> Unit,
    onPhotoPicker: () -> Unit,
    onCamera: () -> Unit,
    onFilePicker: () -> Unit,
    onSpeech: () -> Unit,
    running: Boolean,
    runningSince: String,
    canStop: Boolean,
    onStop: () -> Unit,
    onSend: () -> Unit,
    inputFocusRequester: FocusRequester? = null,
    previewLoading: Boolean = false,
) {
    var attachMenu by remember { mutableStateOf(false) }
    var mentionMenu by remember { mutableStateOf(false) }
    var permissionMenu by remember { mutableStateOf(false) }
    var commandMenu by remember { mutableStateOf(false) }
    var fileCompletions by remember { mutableStateOf<List<CompletedPath>>(emptyList()) }
    var filesLoading by remember { mutableStateOf(false) }
    var now by remember { mutableStateOf(System.currentTimeMillis()) }
    val scope = rememberCoroutineScope()

    fun loadFileCompletions(prefix: String) {
        if (cwd.isBlank()) {
            fileCompletions = emptyList()
            return
        }
        filesLoading = true
        scope.launch {
            fileCompletions = runCatching {
                completeFiles(cwd, prefix)
            }.getOrDefault(emptyList())
            filesLoading = false
        }
    }
    LaunchedEffect(running, runningSince) {
        while (running) {
            now = System.currentTimeMillis()
            delay(1_000)
        }
    }
    Surface(
        color = MaterialTheme.colorScheme.surface.copy(alpha = 0.98f),
        tonalElevation = 8.dp,
        shadowElevation = 12.dp,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .navigationBarsPadding()
                .imePadding()
                .padding(horizontal = 12.dp, vertical = 8.dp)
                .testTag("session-composer"),
            verticalArrangement = Arrangement.spacedBy(7.dp),
        ) {
            if (previewLoading) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(7.dp),
                ) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(13.dp),
                        strokeWidth = 2.dp,
                        color = CodexGreen,
                    )
                    Text(
                        text = "正在生成发送预览…",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            if (queuedCount > 0) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    Icon(
                        Icons.Outlined.Schedule,
                        contentDescription = null,
                        modifier = Modifier.size(15.dp),
                        tint = MaterialTheme.colorScheme.tertiary,
                    )
                    Text(
                        text = "已排队 $queuedCount 条回复",
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Spacer(Modifier.weight(1f))
                    TextButton(
                        onClick = onCancelQueued,
                        enabled = enabled,
                        modifier = Modifier.height(30.dp),
                    ) {
                        Text("管理", style = MaterialTheme.typography.labelMedium)
                    }
                }
            }
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                Row(
                    modifier = Modifier
                        .weight(1f)
                        .horizontalScroll(rememberScrollState()),
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    ModelPill(
                        label = modelLabel,
                        enabled = canSwitchModel,
                        overridden = modelOverride,
                        onClick = onModelClick,
                        modifier = Modifier.testTag("model-pill"),
                    )
                    if (showEffortPill) {
                        ModelPill(
                            label = "强度 · ${effortLabel.ifBlank { "默认" }}",
                            enabled = canSwitchModel,
                            overridden = modelOverride,
                            onClick = onModelClick,
                            modifier = Modifier.testTag("effort-pill"),
                        )
                    }
                    if (permissionModes.isNotEmpty()) {
                        Box {
                            ModelPill(
                                label = "权限 · $permissionLabel",
                                enabled = enabled,
                                overridden = permissionLabel != "默认",
                                onClick = { permissionMenu = true },
                                modifier = Modifier.testTag("permission-pill"),
                            )
                            DropdownMenu(
                                expanded = permissionMenu,
                                onDismissRequest = { permissionMenu = false },
                            ) {
                                for (mode in permissionModes) {
                                    DropdownMenuItem(
                                        text = { Text(permissionModeDisplayLabel(mode)) },
                                        onClick = {
                                            permissionMenu = false
                                            onPermissionChange(mode)
                                        },
                                    )
                                }
                            }
                        }
                    }
                    if (!canSwitchModel && modelDisabledReason.isNotBlank()) {
                        Text(
                            text = modelDisabledReason,
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.outline,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    } else if (modelOverride) {
                        Text(
                            text = "仅下一条",
                            style = MaterialTheme.typography.labelSmall,
                            color = dev.agentpush.poc.ui.ClaudeOrange,
                            maxLines = 1,
                        )
                    }
                }
                if (running) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(11.dp),
                        strokeWidth = 2.dp,
                        color = CodexGreen,
                    )
                    Text(
                        text = elapsedLabel(runningSince, now).ifBlank { "处理中" },
                        style = MaterialTheme.typography.labelSmall,
                        color = CodexGreen,
                        maxLines = 1,
                    )
                    if (canStop) {
                        TextButton(
                            onClick = onStop,
                            modifier = Modifier.size(width = 52.dp, height = 30.dp),
                        ) {
                            Icon(
                                Icons.Outlined.Stop,
                                contentDescription = null,
                                modifier = Modifier.size(15.dp),
                            )
                            Text("停止", style = MaterialTheme.typography.labelSmall)
                        }
                    }
                }
            }
            if (pending.isNotEmpty()) {
                LazyRow(
                    modifier = Modifier.fillMaxWidth(),
                    contentPadding = PaddingValues(horizontal = 12.dp, vertical = 7.dp),
                    horizontalArrangement = Arrangement.spacedBy(7.dp),
                ) {
                    items(pending, key = { it.id }) { attachment ->
                        PendingAttachmentChip(
                            attachment = attachment,
                            onRemove = { onRemove(attachment.id) },
                            onRetry = { onRetryUpload(attachment) },
                        )
                    }
                }
            }
            if (mentions.isNotEmpty()) {
                LazyRow(
                    modifier = Modifier.fillMaxWidth(),
                    contentPadding = PaddingValues(vertical = 1.dp),
                    horizontalArrangement = Arrangement.spacedBy(7.dp),
                ) {
                    items(mentions.size, key = { it }) { index ->
                        val optionKey = agentMentionOptionKey(mentions, index)
                        MentionChip(
                            mention = mentions[index],
                            available = dispatchTargets.any {
                                it.source == mentions[index].agentId
                            },
                            options = mentionOptions[optionKey]
                                ?: AgentMentionOptions(),
                            onPresetChange = {
                                onMentionPresetChange(mentions[index], it)
                            },
                            onOptionsChange = {
                                onMentionOptionsChange(optionKey, it)
                            },
                        )
                    }
                }
            }
            OutlinedTextField(
                value = text,
                onValueChange = { next ->
                    onTextChange(next)
                    if (next.length > text.length && next.endsWith("@")) {
                        mentionMenu = true
                        loadFileCompletions("")
                    } else if (next.length > text.length && next.endsWith("/")) {
                        if (commands.isNotEmpty()) commandMenu = true
                    } else if (mentionMenu && trailingAtPrefix(next) != null) {
                        loadFileCompletions(trailingAtPrefix(next).orEmpty())
                    }
                },
                modifier = Modifier
                    .fillMaxWidth()
                    .then(
                        if (inputFocusRequester != null) {
                            Modifier.focusRequester(inputFocusRequester)
                        } else {
                            Modifier
                        },
                    )
                    .testTag("session-input"),
                placeholder = {
                    Text(
                        if (!enabled) {
                            "等待会话就绪…"
                        } else if (dispatchTargets.isNotEmpty()) {
                            "回复这个会话，使用下方 @ 可派生子任务"
                        } else {
                            "回复这个会话…"
                        },
                    )
                },
                enabled = enabled,
                minLines = 2,
                maxLines = 6,
                shape = RoundedCornerShape(18.dp),
                textStyle = MaterialTheme.typography.bodyLarge,
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
                keyboardActions = KeyboardActions(onSend = { if (canSend) onSend() }),
                colors = TextFieldDefaults.colors(
                    focusedContainerColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.42f),
                    unfocusedContainerColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.32f),
                    focusedIndicatorColor = Color.Transparent,
                    unfocusedIndicatorColor = Color.Transparent,
                ),
            )
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .testTag("composer-toolbar"),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(2.dp),
            ) {
                if (commands.isNotEmpty()) {
                    Box {
                        IconButton(
                            onClick = { commandMenu = true },
                            enabled = enabled,
                        ) {
                            Text(
                                text = "/",
                                style = MaterialTheme.typography.titleMedium,
                                fontFamily = HomingMono,
                                fontWeight = FontWeight.Bold,
                            )
                        }
                        DropdownMenu(
                            expanded = commandMenu,
                            onDismissRequest = { commandMenu = false },
                        ) {
                            for (command in commands) {
                                DropdownMenuItem(
                                    text = {
                                        Column {
                                            Text(
                                                text = "/${command.name}" +
                                                    command.argumentHint
                                                        .takeIf { it.isNotBlank() }
                                                        ?.let { " $it" }
                                                        .orEmpty(),
                                                fontFamily = HomingMono,
                                            )
                                            if (command.description.isNotBlank()) {
                                                Text(
                                                    text = command.description,
                                                    style = MaterialTheme.typography.labelSmall,
                                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                                    maxLines = 2,
                                                    overflow = TextOverflow.Ellipsis,
                                                )
                                            }
                                        }
                                    },
                                    onClick = {
                                        commandMenu = false
                                        onTextChange(insertSlashCommand(text, command.name))
                                    },
                                )
                            }
                        }
                    }
                }
                if (dispatchTargets.isNotEmpty() || cwd.isNotBlank()) {
                    Box {
                        IconButton(
                            onClick = {
                                mentionMenu = true
                                loadFileCompletions(trailingAtPrefix(text).orEmpty())
                            },
                            enabled = enabled,
                        ) {
                            Icon(Icons.Outlined.AlternateEmail, contentDescription = "跨 Agent 派发")
                        }
                        DropdownMenu(
                            expanded = mentionMenu,
                            onDismissRequest = { mentionMenu = false },
                        ) {
                            if (dispatchTargets.isNotEmpty()) {
                                DropdownMenuItem(
                                    text = {
                                        Text(
                                            "Agent",
                                            style = MaterialTheme.typography.labelMedium,
                                        )
                                    },
                                    onClick = {},
                                    enabled = false,
                                )
                                for (target in dispatchTargets) {
                                    DropdownMenuItem(
                                        text = {
                                            Text(
                                                "@${target.source} · " +
                                                    AgentBrand.fromWire(target.source).displayName,
                                            )
                                        },
                                        onClick = {
                                            mentionMenu = false
                                            val prefix = when {
                                                text.isBlank() -> ""
                                                text.last().isWhitespace() -> ""
                                                else -> "\n"
                                            }
                                            onTextChange(text + prefix + "@${target.source} ")
                                        },
                                    )
                                }
                            }
                            DropdownMenuItem(
                                text = {
                                    Text("文件", style = MaterialTheme.typography.labelMedium)
                                },
                                onClick = {},
                                enabled = false,
                            )
                            when {
                                cwd.isBlank() -> DropdownMenuItem(
                                    text = { Text("当前会话没有 cwd") },
                                    onClick = {},
                                    enabled = false,
                                )
                                filesLoading -> DropdownMenuItem(
                                    text = { Text("正在搜索文件…") },
                                    onClick = {},
                                    enabled = false,
                                )
                                fileCompletions.isEmpty() -> DropdownMenuItem(
                                    text = { Text("输入 @路径 可搜索文件") },
                                    onClick = {},
                                    enabled = false,
                                )
                                else -> fileCompletions.take(8).forEach { file ->
                                    DropdownMenuItem(
                                        text = {
                                            Text(
                                                text = file.path,
                                                fontFamily = HomingMono,
                                                maxLines = 1,
                                                overflow = TextOverflow.Ellipsis,
                                            )
                                        },
                                        onClick = {
                                            mentionMenu = false
                                            onTextChange(insertFileMention(text, file.path))
                                        },
                                    )
                                }
                            }
                        }
                    }
                }
                Box {
                    IconButton(onClick = { attachMenu = true }, enabled = enabled) {
                        Icon(Icons.Outlined.AttachFile, contentDescription = "添加附件")
                    }
                    DropdownMenu(expanded = attachMenu, onDismissRequest = { attachMenu = false }) {
                        DropdownMenuItem(
                            text = { Text("相册（可多选）") },
                            onClick = {
                                attachMenu = false
                                onPhotoPicker()
                            },
                        )
                        DropdownMenuItem(
                            text = { Text("拍照") },
                            onClick = {
                                attachMenu = false
                                onCamera()
                            },
                        )
                        DropdownMenuItem(
                            text = { Text("任意文件") },
                            onClick = {
                                attachMenu = false
                                onFilePicker()
                            },
                        )
                    }
                }
                IconButton(onClick = onSpeech, enabled = enabled) {
                    Icon(Icons.Outlined.Mic, contentDescription = "语音输入")
                }
                Spacer(Modifier.weight(1f))
                Surface(
                    onClick = onSend,
                    enabled = canSend,
                    modifier = Modifier.size(48.dp),
                    shape = CircleShape,
                    color = if (canSend) {
                        MaterialTheme.colorScheme.primary
                    } else {
                        MaterialTheme.colorScheme.surfaceVariant
                    },
                    contentColor = if (canSend) {
                        MaterialTheme.colorScheme.onPrimary
                    } else {
                        MaterialTheme.colorScheme.onSurfaceVariant
                    },
                ) {
                    Box(contentAlignment = Alignment.Center) {
                        Icon(
                            Icons.AutoMirrored.Outlined.Send,
                            contentDescription = "发送",
                            modifier = Modifier.size(21.dp),
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun MentionChip(
    mention: AgentMention,
    available: Boolean,
    options: AgentMentionOptions,
    onPresetChange: (String) -> Unit,
    onOptionsChange: (AgentMentionOptions) -> Unit,
) {
    val brand = AgentBrand.fromWire(mention.agentId)
    val accent = brand.accentColor()
    val invalid = mention.text.isBlank() || !available
    var expanded by remember(mention.agentId) { mutableStateOf(false) }
    val presetLabel = when (mention.preset) {
        "do" -> "动手"
        "review" -> "审查"
        "handoff" -> "交棒"
        else -> "咨询"
    }
    Box {
        Surface(
            onClick = { expanded = true },
            color = if (invalid) {
                MaterialTheme.colorScheme.errorContainer.copy(alpha = 0.7f)
            } else {
                accent.copy(alpha = 0.13f)
            },
            shape = RoundedCornerShape(50),
            border = BorderStroke(
                1.dp,
                if (invalid) MaterialTheme.colorScheme.error.copy(alpha = 0.45f)
                else accent.copy(alpha = 0.35f),
            ),
        ) {
            Row(
                modifier = Modifier.padding(start = 9.dp, end = 6.dp, top = 5.dp, bottom = 5.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(5.dp),
            ) {
                Icon(
                    Icons.AutoMirrored.Outlined.CallSplit,
                    contentDescription = null,
                    modifier = Modifier.size(13.dp),
                    tint = if (invalid) MaterialTheme.colorScheme.error else accent,
                )
                Text(
                    text = "@${mention.agentId} · $presetLabel",
                    style = MaterialTheme.typography.labelMedium,
                    color = if (invalid) MaterialTheme.colorScheme.error else accent,
                )
                when {
                    mention.text.isBlank() -> Text(
                        text = "请补写任务",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                    !available -> Text(
                        text = "当前不可调度",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                    else -> Text(
                        text = mention.text.replace(Regex("\\s+"), " ").take(42),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                Icon(
                    Icons.Outlined.ExpandMore,
                    contentDescription = "调整 @${mention.agentId} 协作设置",
                    modifier = Modifier.size(14.dp),
                    tint = if (invalid) {
                        MaterialTheme.colorScheme.error
                    } else {
                        MaterialTheme.colorScheme.onSurfaceVariant
                    },
                )
            }
        }
        DropdownMenu(
            expanded = expanded,
            onDismissRequest = { expanded = false },
        ) {
            Text(
                text = "预设",
                modifier = Modifier.padding(horizontal = 12.dp, vertical = 6.dp),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            for ((preset, label) in listOf(
                "ask" to "咨询 · 调查并给意见",
                "do" to "动手 · 完成一块工作",
                "review" to "审查 · 检查最近改动",
                "handoff" to "交棒 · 由它接着做",
            )) {
                DropdownMenuItem(
                    text = { Text(label) },
                    leadingIcon = {
                        if (mention.preset == preset) {
                            Icon(
                                Icons.Outlined.Check,
                                contentDescription = null,
                                modifier = Modifier.size(16.dp),
                            )
                        } else {
                            Spacer(Modifier.size(16.dp))
                        }
                    },
                    onClick = {
                        expanded = false
                        onPresetChange(preset)
                    },
                )
            }
            HorizontalDivider()
            Text(
                text = "协作线",
                modifier = Modifier.padding(horizontal = 12.dp, vertical = 6.dp),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            for ((target, label) in listOf(
                "auto" to "自动选择",
                "new" to "新开一条",
                "continue" to "接续最近一条",
            )) {
                DropdownMenuItem(
                    text = { Text(label) },
                    leadingIcon = {
                        if (options.target == target) {
                            Icon(
                                Icons.Outlined.Check,
                                contentDescription = null,
                                modifier = Modifier.size(16.dp),
                            )
                        } else {
                            Spacer(Modifier.size(16.dp))
                        }
                    },
                    onClick = {
                        onOptionsChange(options.copy(target = target))
                    },
                )
            }
            HorizontalDivider()
            Text(
                text = "工作区",
                modifier = Modifier.padding(horizontal = 12.dp, vertical = 6.dp),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            for ((workspace, label) in listOf(
                "auto" to "按预设自动选择",
                "shared" to "共享当前目录",
                "readonly" to "共享目录 · 只读",
                "worktree" to "独立 worktree",
            )) {
                DropdownMenuItem(
                    text = { Text(label) },
                    leadingIcon = {
                        if (options.workspace == workspace) {
                            Icon(
                                Icons.Outlined.Check,
                                contentDescription = null,
                                modifier = Modifier.size(16.dp),
                            )
                        } else {
                            Spacer(Modifier.size(16.dp))
                        }
                    },
                    onClick = {
                        onOptionsChange(options.copy(workspace = workspace))
                    },
                )
            }
            HorizontalDivider()
            Text(
                text = "附带背景",
                modifier = Modifier.padding(horizontal = 12.dp, vertical = 6.dp),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            for ((part, label) in listOf(
                "recent" to "最近对话",
                "files" to "改动文件",
                "diff" to "git diff",
            )) {
                DropdownMenuItem(
                    text = { Text(label) },
                    leadingIcon = {
                        if (part in options.context) {
                            Icon(
                                Icons.Outlined.Check,
                                contentDescription = null,
                                modifier = Modifier.size(16.dp),
                            )
                        } else {
                            Spacer(Modifier.size(16.dp))
                        }
                    },
                    onClick = {
                        val context = if (part in options.context) {
                            options.context - part
                        } else {
                            options.context + part
                        }
                        onOptionsChange(
                            options.copy(
                                context = linkedSetOf<String>().apply {
                                    for (item in listOf("recent", "files", "diff")) {
                                        if (item in context) add(item)
                                    }
                                },
                            ),
                        )
                    },
                )
            }
        }
    }
}

@Composable
private fun PendingAttachmentChip(
    attachment: PendingAttachment,
    onRemove: () -> Unit,
    onRetry: () -> Unit,
) {
    Surface(
        modifier = Modifier.width(190.dp),
        color = if (attachment.error != null) {
            MaterialTheme.colorScheme.errorContainer.copy(alpha = 0.7f)
        } else {
            MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.65f)
        },
        shape = RoundedCornerShape(10.dp),
    ) {
        Column(modifier = Modifier.padding(start = 9.dp, end = 2.dp, top = 5.dp, bottom = 6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = attachment.name,
                    modifier = Modifier.weight(1f),
                    style = MaterialTheme.typography.labelMedium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                IconButton(onClick = onRemove, modifier = Modifier.size(27.dp)) {
                    Icon(Icons.Outlined.Close, "移除", modifier = Modifier.size(15.dp))
                }
            }
            if (attachment.uploading) {
                LinearProgressIndicator(
                    progress = { attachment.progress },
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(end = 7.dp, top = 3.dp),
                )
                Text(
                    text = "上传中 ${(attachment.progress * 100).toInt()}%",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            } else if (attachment.error != null) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = attachment.error.take(42),
                        modifier = Modifier.weight(1f),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onErrorContainer,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    TextButton(
                        onClick = onRetry,
                        modifier = Modifier.size(width = 48.dp, height = 28.dp),
                    ) {
                        Text("重试", style = MaterialTheme.typography.labelSmall)
                    }
                }
            } else {
                Text(
                    text = "已上传 · ${attachment.size / 1024} KB",
                    style = MaterialTheme.typography.labelSmall,
                    color = CodexGreen,
                )
            }
        }
    }
}

internal fun effortDisplayLabel(value: String): String {
    return when (value.trim().lowercase()) {
        "", "default" -> "默认"
        "none", "minimal", "min" -> "最低"
        "low" -> "低"
        "medium", "mid" -> "中"
        "high", "xhigh", "ultra", "max", "highest" -> "高"
        else -> value
    }
}

internal fun permissionModeDisplayLabel(value: String): String {
    return when (value.trim().lowercase().replace('-', '_')) {
        "", "default", "manual" -> "默认"
        "plan" -> "计划"
        "accept_edits", "accept-edits", "auto" -> "自动接受编辑"
        "read_only", "readonly" -> "只读"
        "bypass_permissions", "bypass" -> "绕过权限"
        else -> value
    }
}

internal fun trailingAtPrefix(text: String): String? {
    val match = Regex("(?:^|\\s)@([^\\s@]*)$").find(text) ?: return null
    return match.groupValues.getOrNull(1).orEmpty()
}

internal fun insertFileMention(text: String, path: String): String {
    val match = Regex("(?:^|\\s)@([^\\s@]*)$").find(text)
    val start = if (match != null) {
        match.range.first + if (text.getOrNull(match.range.first)?.isWhitespace() == true) 1 else 0
    } else {
        text.length
    }
    val prefix = text.substring(0, start)
    val spacer = if (prefix.isBlank() || prefix.last().isWhitespace()) "" else " "
    return prefix + spacer + "@" + path + " "
}

internal fun insertSlashCommand(text: String, name: String): String {
    val match = Regex("(?:^|\\s)/([^\\s/]*)$").find(text)
    val start = if (match != null) {
        match.range.first + if (text.getOrNull(match.range.first)?.isWhitespace() == true) 1 else 0
    } else {
        text.length
    }
    val prefix = text.substring(0, start)
    val spacer = if (prefix.isBlank() || prefix.last().isWhitespace()) "" else " "
    return prefix + spacer + "/" + name + " "
}
