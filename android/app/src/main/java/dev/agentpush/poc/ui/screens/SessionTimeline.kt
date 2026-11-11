package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import dev.agentpush.poc.AgentEvent
import dev.agentpush.poc.AgentReply
import dev.agentpush.poc.AppSettings
import dev.agentpush.poc.ApprovalOption
import dev.agentpush.poc.Attachment
import dev.agentpush.poc.ReplyStatus
import dev.agentpush.poc.SessionItem
import dev.agentpush.poc.SessionItemType
import dev.agentpush.poc.SessionSummary
import dev.agentpush.poc.TranscriptMessage
import dev.agentpush.poc.formatClock
import dev.agentpush.poc.media.PendingAttachment
import dev.agentpush.poc.ui.AgentAvatar
import dev.agentpush.poc.ui.ApprovalItemCard
import dev.agentpush.poc.ui.AssistantItemCard
import dev.agentpush.poc.ui.AttachmentCardList
import dev.agentpush.poc.ui.ClaudeOrange
import dev.agentpush.poc.ui.CodexGreen
import dev.agentpush.poc.ui.ErrorItemCard
import dev.agentpush.poc.ui.FileChangeItemCard
import dev.agentpush.poc.ui.PendingTag
import dev.agentpush.poc.ui.PlanItemCard
import dev.agentpush.poc.ui.ReasoningItemCard
import dev.agentpush.poc.ui.RichMarkdown
import dev.agentpush.poc.ui.SystemItemRow
import dev.agentpush.poc.ui.ToolCallItemCard
import dev.agentpush.poc.ui.UserItemBubble
import dev.agentpush.poc.ui.accentColor
import dev.agentpush.poc.ui.rewriteMarkdownImageUrls
import dev.agentpush.poc.AgentApi

internal sealed interface TimelineItem {
    val key: String
    val timestamp: String
    val order: Int

    data class Message(
        val value: TranscriptMessage,
        override val order: Int,
    ) : TimelineItem {
        override val key: String = "transcript-$order-${value.role}"
        override val timestamp: String = value.ts
    }

    data class Event(
        val value: AgentEvent,
        override val order: Int,
    ) : TimelineItem {
        override val key: String = "event-$order-${value.id}"
        override val timestamp: String = value.ts
    }

    data class Reply(
        val value: AgentReply,
        override val order: Int,
    ) : TimelineItem {
        override val key: String = "reply-$order-${value.id}"
        override val timestamp: String = value.ts
    }

    data class Item(
        val value: SessionItem,
        override val order: Int,
    ) : TimelineItem {
        override val key: String = "item-$order-${value.id}"
        override val timestamp: String = value.ts
    }

    data class ProcessGroup(
        val values: List<SessionItem>,
        override val order: Int,
    ) : TimelineItem {
        override val key: String =
            "process-${values.firstOrNull()?.id.orEmpty()}-${values.lastOrNull()?.id.orEmpty()}"
        override val timestamp: String = values.firstOrNull()?.ts.orEmpty()
    }
}

/**
 * Tool calls are execution detail, not the answer the user came to read. Keep
 * consecutive calls together so the default timeline is one compact row, while
 * preserving every original card behind an explicit expand action.
 */
internal fun List<TimelineItem>.foldToolProcesses(): List<TimelineItem> {
    val output = mutableListOf<TimelineItem>()
    val pending = mutableListOf<TimelineItem.Item>()

    fun flush() {
        if (pending.isEmpty()) return
        output += TimelineItem.ProcessGroup(
            values = pending.map { it.value },
            order = pending.first().order,
        )
        pending.clear()
    }

    forEach { item ->
        if (
            item is TimelineItem.Item &&
            item.value.type == SessionItemType.TOOL_CALL
        ) {
            pending += item
        } else {
            flush()
            output += item
        }
    }
    flush()
    return output
}

internal fun summarizeToolProcess(steps: List<SessionItem>): String {
    if (steps.isEmpty()) return ""
    val counts = linkedMapOf<String, Int>()
    steps.forEach { step ->
        val label = toolProcessLabel(step.tool.name)
        counts[label] = (counts[label] ?: 0) + 1
    }
    val visible = counts.entries
        .take(3)
        .joinToString(" · ") { (label, count) ->
            if (count > 1) "$label ×$count" else label
        }
    return if (counts.size > 3) "$visible · 等 ${counts.size} 种" else visible
}

private fun toolProcessLabel(name: String): String {
    return when (name.trim().lowercase()) {
        "apply_patch", "patch" -> "应用补丁"
        "exec_command", "shell", "bash", "command" -> "执行命令"
        "write_stdin" -> "终端输入"
        "read_file", "read" -> "读取文件"
        "grep", "rg", "search" -> "搜索"
        "web_search" -> "网页搜索"
        "view_image" -> "查看图片"
        else -> name.trim().ifBlank { "工具调用" }
    }
}

@Composable
internal fun NewContentDivider() {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 5.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Box(
            modifier = Modifier
                .weight(1f)
                .height(1.dp)
                .background(MaterialTheme.colorScheme.primary.copy(alpha = 0.35f)),
        )
        Text(
            text = "新内容",
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.primary,
        )
        Box(
            modifier = Modifier
                .weight(1f)
                .height(1.dp)
                .background(MaterialTheme.colorScheme.primary.copy(alpha = 0.35f)),
        )
    }
}

@Composable
internal fun SessionItemRow(
    item: SessionItem,
    session: SessionSummary,
    settings: AppSettings,
    approvalOptions: List<ApprovalOption> = emptyList(),
    reasoningDurationSeconds: Long? = null,
    onOpenUrl: (String) -> Unit,
    onCopyCode: (String) -> Unit,
    onCopyText: (String) -> Unit,
    onDecision: (Long?, String, String?) -> Unit,
) {
    when (item.type) {
        SessionItemType.USER -> UserItemBubble(
            item = item,
            onLongClick = { onCopyText(item.text) },
        )
        SessionItemType.ASSISTANT -> AssistantItemCard(
            item = item,
            brand = session.brand,
            settings = settings,
            onOpenUrl = onOpenUrl,
            onCopyCode = onCopyCode,
        )
        SessionItemType.REASONING -> ReasoningItemCard(item, reasoningDurationSeconds)
        SessionItemType.TOOL_CALL -> ToolCallItemCard(
            item = item,
            onCopy = {
                onCopyText(
                    buildString {
                        if (item.tool.name.isNotBlank()) appendLine(item.tool.name)
                        if (item.tool.inputSummary.isNotBlank()) {
                            appendLine(item.tool.inputSummary)
                        }
                        if (item.tool.outputTail.isNotBlank()) {
                            appendLine(item.tool.outputTail)
                        }
                    }.trim(),
                )
            },
        )
        SessionItemType.FILE_CHANGE -> FileChangeItemCard(
            item = item,
            onCopy = {
                onCopyText(
                    item.files.joinToString("\n") { file ->
                        "${file.path}  +${file.added} -${file.removed}" +
                            if (file.diff.isNotBlank()) "\n${file.diff}" else ""
                    }.ifBlank { item.text },
                )
            },
        )
        SessionItemType.PLAN -> PlanItemCard(item)
        SessionItemType.APPROVAL -> ApprovalItemCard(
            item = item.copy(
                eventId = item.eventId
                    ?: session.latestEvent?.takeIf { it.isPermission }?.id,
            ),
            canApprove = session.capabilities.approve,
            approvalOptions = approvalOptions,
            onDecision = onDecision,
        )
        SessionItemType.ERROR -> ErrorItemCard(item)
        SessionItemType.SYSTEM -> SystemItemRow(item)
        SessionItemType.OTHER -> if (item.text.isNotBlank()) {
            AssistantItemCard(
                item = item.copy(type = SessionItemType.ASSISTANT),
                brand = session.brand,
                settings = settings,
                onOpenUrl = onOpenUrl,
                onCopyCode = onCopyCode,
            )
        }
    }
}

@Composable
internal fun ToolProcessGroupCard(
    steps: List<SessionItem>,
    expanded: Boolean,
    onToggle: () -> Unit,
    content: @Composable () -> Unit,
) {
    val failedCount = steps.count { it.isFailed }
    val running = steps.any {
        it.tool.status.equals("pending", true) ||
            it.tool.status.equals("running", true) ||
            it.partial
    }
    val accent = if (failedCount > 0) {
        MaterialTheme.colorScheme.error
    } else {
        MaterialTheme.colorScheme.primary
    }
    Surface(
        modifier = Modifier
            .fillMaxWidth()
            .testTag("tool-process-group"),
        color = MaterialTheme.colorScheme.surface,
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(
            1.dp,
            if (failedCount > 0) {
                MaterialTheme.colorScheme.error.copy(alpha = 0.45f)
            } else {
                MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.7f)
            },
        ),
    ) {
        Column {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .testTag("tool-process-toggle")
                    .combinedClickable(
                        onClick = onToggle,
                        onLongClick = null,
                    )
                    .padding(horizontal = 12.dp, vertical = 9.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(9.dp),
            ) {
                Box(
                    modifier = Modifier
                        .size(26.dp)
                        .clip(RoundedCornerShape(7.dp))
                        .background(accent.copy(alpha = 0.12f)),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        imageVector = Icons.Outlined.Terminal,
                        contentDescription = null,
                        tint = accent,
                        modifier = Modifier.size(15.dp),
                    )
                }
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "${steps.size} 个工具步骤",
                        style = MaterialTheme.typography.labelLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Text(
                        text = summarizeToolProcess(steps),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                if (failedCount > 0) {
                    Text(
                        text = "$failedCount 个失败",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                } else if (running) {
                    Text(
                        text = "处理中",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.primary,
                    )
                }
                Icon(
                    imageVector = if (expanded) Icons.Outlined.ExpandLess
                    else Icons.Outlined.ExpandMore,
                    contentDescription = if (expanded) "收起工具过程" else "展开工具过程",
                    modifier = Modifier.size(18.dp),
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (expanded) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(1.dp)
                        .background(MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.55f)),
                )
                Column(
                    modifier = Modifier.padding(10.dp),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    content()
                }
            }
        }
    }
}

@Composable
internal fun TranscriptMessageRow(
    message: TranscriptMessage,
    settings: AppSettings,
    brand: dev.agentpush.poc.AgentBrand,
    displayName: String,
    onOpenUrl: (String) -> Unit,
    onOpenBrowser: (String, String) -> Unit,
    onOpenImage: (List<Attachment>, Int) -> Unit,
    onCopyCode: (String) -> Unit,
) {
    if (message.isUser) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.End,
        ) {
            Surface(
                modifier = Modifier.widthIn(max = 340.dp),
                color = MaterialTheme.colorScheme.secondaryContainer,
                shape = RoundedCornerShape(16.dp),
            ) {
                Column(modifier = Modifier.padding(13.dp)) {
                    Text(
                        text = "我在电脑上的提问",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSecondaryContainer.copy(alpha = 0.7f),
                    )
                    Spacer(Modifier.height(4.dp))
                    Text(
                        text = message.text,
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onSecondaryContainer,
                    )
                    AttachmentCardList(
                        attachments = message.attachments,
                        settings = settings,
                        onOpenHtml = { attachment ->
                            onOpenBrowser(
                                AgentApi.attachmentUrl(settings, attachment),
                                attachment.name,
                            )
                        },
                        onOpenImage = { index ->
                            onOpenImage(message.attachments.filter { it.isImage }, index)
                        },
                        modifier = Modifier.padding(top = 8.dp),
                    )
                }
            }
        }
        return
    }

    AgentMessageCard(
        text = message.text,
        attachments = message.attachments,
        settings = settings,
        brand = brand,
        displayName = displayName,
        timestamp = message.ts,
        onOpenUrl = onOpenUrl,
        onOpenBrowser = onOpenBrowser,
        onOpenImage = onOpenImage,
        onCopyCode = onCopyCode,
    )
}

@Composable
private fun AgentMessageCard(
    text: String,
    attachments: List<Attachment>,
    settings: AppSettings,
    brand: dev.agentpush.poc.AgentBrand,
    displayName: String,
    timestamp: String,
    onOpenUrl: (String) -> Unit,
    onOpenBrowser: (String, String) -> Unit,
    onOpenImage: (List<Attachment>, Int) -> Unit,
    onCopyCode: (String) -> Unit,
) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        color = MaterialTheme.colorScheme.surface,
        shape = RoundedCornerShape(16.dp),
        border = androidx.compose.foundation.BorderStroke(
            1.dp,
            MaterialTheme.colorScheme.outlineVariant,
        ),
        tonalElevation = 1.dp,
    ) {
        Column(modifier = Modifier.padding(14.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = displayName,
                    style = MaterialTheme.typography.labelMedium,
                    color = brand.accentColor(),
                )
                Spacer(Modifier.weight(1f))
                if (timestamp.isNotBlank()) {
                    Text(
                        text = formatClock(timestamp),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            Spacer(Modifier.height(6.dp))
            RichMarkdown(
                source = rewriteMarkdownImageUrls(
                    text.ifBlank { "（无文字内容）" },
                    attachments,
                    settings,
                ),
                onCopyCode = onCopyCode,
                onOpenUrl = onOpenUrl,
            )
            AttachmentCardList(
                attachments = attachments,
                settings = settings,
                onOpenHtml = { attachment ->
                    onOpenBrowser(
                        AgentApi.attachmentUrl(settings, attachment),
                        attachment.name,
                    )
                },
                onOpenImage = { index ->
                    onOpenImage(attachments.filter { it.isImage }, index)
                },
                modifier = Modifier.padding(top = 8.dp),
            )
        }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
internal fun AgentEventCard(
    event: AgentEvent,
    session: SessionSummary,
    settings: AppSettings,
    onReply: (String) -> Unit,
    onDecision: (Long, String, String?) -> Unit,
    onOpenUrl: (String) -> Unit,
    onOpenBrowser: (String, String) -> Unit,
    onOpenImage: (List<Attachment>, Int) -> Unit,
    onCopyCode: (String) -> Unit,
    onCopyText: () -> Unit,
    onShareText: (String) -> Unit,
    proxyCapability: String?,
) {
    val replied = session.replies.any {
        it.eventId == event.id &&
            it.status != ReplyStatus.FAILED &&
            it.status != ReplyStatus.CANCELLED
    }
    val isLatest = session.latestEvent?.id == event.id
    val pending = event.isPending && isLatest && !replied
    val actionablePermission = event.isPermission && isLatest && !replied
    val accent = event.brand.accentColor()
    var menu by remember(event.id) { mutableStateOf(false) }

    Column(
        modifier = Modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(5.dp),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(7.dp),
        ) {
            AgentAvatar(
                brand = event.brand,
                source = event.source,
                size = 27.dp,
            )
            Text(event.brand.displayName, style = MaterialTheme.typography.labelLarge, color = accent)
            if (pending) {
                PendingTag(if (event.isPermission) "需要授权" else "等待输入")
            }
            Spacer(Modifier.weight(1f))
            Text(
                text = formatClock(event.ts),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Box {
            Surface(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 34.dp)
                    .combinedClickable(
                        onClick = {},
                        onLongClick = { menu = true },
                    ),
                color = if (pending) {
                    accent.copy(alpha = 0.075f)
                } else {
                    MaterialTheme.colorScheme.surface
                },
                shape = RoundedCornerShape(
                    topStart = 6.dp,
                    topEnd = 18.dp,
                    bottomEnd = 18.dp,
                    bottomStart = 18.dp,
                ),
                border = androidx.compose.foundation.BorderStroke(
                    1.dp,
                    if (pending) accent.copy(alpha = 0.3f)
                    else MaterialTheme.colorScheme.outlineVariant,
                ),
                tonalElevation = 1.dp,
            ) {
                Column(modifier = Modifier.padding(14.dp)) {
                    RichMarkdown(
                        source = rewriteMarkdownImageUrls(
                            event.text.ifBlank { event.title },
                            event.attachments,
                            settings,
                        ),
                        onCopyCode = onCopyCode,
                        onOpenUrl = onOpenUrl,
                    )
                    AttachmentCardList(
                        attachments = event.attachments,
                        settings = settings,
                        onOpenHtml = { attachment ->
                            onOpenBrowser(
                                AgentApi.attachmentUrl(settings, attachment),
                                attachment.name,
                            )
                        },
                        onOpenImage = { index ->
                            onOpenImage(event.attachments.filter { it.isImage }, index)
                        },
                        modifier = Modifier.padding(top = 8.dp),
                    )
                    if (event.localhostLinks.isNotEmpty()) {
                        Spacer(Modifier.height(9.dp))
                        Surface(
                            color = ClaudeOrange.copy(alpha = 0.1f),
                            shape = RoundedCornerShape(8.dp),
                        ) {
                            Column(modifier = Modifier.padding(9.dp)) {
                                Text(
                                    text = "正文包含 Mac 本机端口链接，手机上无法直接访问。可尝试通过中继代理打开：",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                                Row(
                                    modifier = Modifier.horizontalScroll(rememberScrollState()),
                                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                                ) {
                                    event.localhostLinks.forEach { port ->
                                        TextButton(
                                            onClick = {
                                                onOpenBrowser(
                                                    AgentApi.proxyUrl(
                                                        settings,
                                                        port,
                                                        capability = proxyCapability,
                                                    ),
                                                    "通过中继 · :$port",
                                                )
                                            },
                                        ) {
                                            Text(":$port")
                                        }
                                    }
                                }
                            }
                        }
                    }
                    if (actionablePermission) {
                        Spacer(Modifier.height(13.dp))
                        val options = event.approvalOptions.ifEmpty {
                            listOf(
                                ApprovalOption("allow_once", "允许", "allow"),
                                ApprovalOption("reject_once", "拒绝", "deny"),
                            )
                        }
                        if (options.size <= 2) {
                            Row(
                                horizontalArrangement = Arrangement.spacedBy(9.dp),
                                modifier = Modifier.fillMaxWidth(),
                            ) {
                                options.forEach { option ->
                                    ApprovalDecisionButton(
                                        option = option,
                                        modifier = Modifier.weight(1f),
                                        onClick = {
                                            onDecision(
                                                event.id,
                                                option.decision,
                                                option.id.takeIf { it.isNotBlank() },
                                            )
                                        },
                                    )
                                }
                            }
                        } else {
                            Column(
                                verticalArrangement = Arrangement.spacedBy(7.dp),
                                modifier = Modifier.fillMaxWidth(),
                            ) {
                                options.forEach { option ->
                                    ApprovalDecisionButton(
                                        option = option,
                                        modifier = Modifier.fillMaxWidth(),
                                        onClick = {
                                            onDecision(
                                                event.id,
                                                option.decision,
                                                option.id.takeIf { it.isNotBlank() },
                                            )
                                        },
                                    )
                                }
                            }
                        }
                    }
                }
            }
            DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                DropdownMenuItem(
                    text = { Text("复制全文") },
                    leadingIcon = { Icon(Icons.Outlined.ContentCopy, null) },
                    onClick = {
                        menu = false
                        onCopyText()
                    },
                )
                DropdownMenuItem(
                    text = { Text("分享") },
                    leadingIcon = { Icon(Icons.Outlined.Share, null) },
                    onClick = {
                        menu = false
                        onShareText(event.text)
                    },
                )
                DropdownMenuItem(
                    text = { Text("选择文字") },
                    leadingIcon = { Icon(Icons.Outlined.MoreVert, null) },
                    onClick = { menu = false },
                )
            }
        }
    }
}

@Composable
private fun ApprovalDecisionButton(
    option: ApprovalOption,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
) {
    if (option.decision == "deny") {
        OutlinedButton(
            onClick = onClick,
            modifier = modifier,
            shape = RoundedCornerShape(11.dp),
        ) {
            Text(option.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    } else {
        Button(
            onClick = onClick,
            modifier = modifier,
            shape = RoundedCornerShape(11.dp),
        ) {
            Text(option.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
internal fun UserReplyBubble(
    reply: AgentReply,
    onRetry: () -> Unit,
    onLongClick: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 56.dp),
        horizontalAlignment = Alignment.End,
    ) {
        Surface(
            modifier = Modifier
                .widthIn(max = 340.dp)
                .combinedClickable(
                    onClick = {},
                    onLongClick = onLongClick,
                ),
            color = MaterialTheme.colorScheme.primary,
            shape = RoundedCornerShape(
                topStart = 18.dp,
                topEnd = 6.dp,
                bottomEnd = 18.dp,
                bottomStart = 18.dp,
            ),
        ) {
            Column(modifier = Modifier.padding(horizontal = 14.dp, vertical = 10.dp)) {
                Text(
                    text = reply.text,
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onPrimary,
                )
                if (reply.files.isNotEmpty()) {
                    Text(
                        text = "附件 ${reply.files.size} 个",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onPrimary.copy(alpha = 0.75f),
                    )
                }
                if (reply.model.isNotBlank()) {
                    Text(
                        text = "本次 · ${reply.model}" +
                            if (reply.effort.isNotBlank()) " · ${reply.effort}" else "",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onPrimary.copy(alpha = 0.72f),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
        }
        Spacer(Modifier.height(4.dp))
        ReplyStatusLabel(
            reply = reply,
            onClick = if (reply.status == ReplyStatus.FAILED) onRetry else null,
        )
    }
}

@Composable
private fun ReplyStatusLabel(reply: AgentReply, onClick: (() -> Unit)?) {
    val color = when (reply.status) {
        ReplyStatus.FAILED -> MaterialTheme.colorScheme.error
        ReplyStatus.OUTBOX -> MaterialTheme.colorScheme.tertiary
        else -> MaterialTheme.colorScheme.onSurfaceVariant
    }
    val icon = when (reply.status) {
        ReplyStatus.OUTBOX, ReplyStatus.QUEUED, ReplyStatus.HELD -> Icons.Outlined.Schedule
        ReplyStatus.INJECTED -> Icons.Outlined.CheckCircle
        ReplyStatus.CANCELLED -> Icons.Outlined.Close
        ReplyStatus.FAILED -> Icons.Outlined.ErrorOutline
    }
    Row(
        modifier = Modifier
            .clip(RoundedCornerShape(8.dp))
            .then(if (onClick != null) Modifier.combinedClickable(
                onClick = onClick,
                onLongClick = onClick,
            ) else Modifier)
            .padding(horizontal = 5.dp, vertical = 3.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Icon(icon, contentDescription = null, modifier = Modifier.size(13.dp), tint = color)
        Text(
            text = if (reply.status == ReplyStatus.FAILED) {
                "发送失败，点此重试${reply.message?.let { " · ${it.take(80)}" }.orEmpty()}"
            } else if (reply.status == ReplyStatus.OUTBOX) {
                "待联网发送"
            } else {
                reply.status.label
            },
            style = MaterialTheme.typography.bodySmall,
            color = color,
        )
    }
}

internal fun normalizeComparableText(value: String): String {
    return value.replace(Regex("\\s+"), " ").trim()
}

internal fun reasoningDuration(timeline: List<TimelineItem>, index: Int): Long? {
    val current = timeline.getOrNull(index) ?: return null
    val start = runCatching {
        java.time.Instant.parse(current.timestamp).toEpochMilli()
    }.getOrNull() ?: return null
    val next = timeline.drop(index + 1)
        .firstOrNull { it.timestamp.isNotBlank() }
        ?.timestamp
        ?: return null
    val end = runCatching { java.time.Instant.parse(next).toEpochMilli() }.getOrNull()
        ?: return null
    val seconds = ((end - start) / 1_000L).coerceAtLeast(0L)
    return seconds.takeIf { it in 1..3_600 }
}
