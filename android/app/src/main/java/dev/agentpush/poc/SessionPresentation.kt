package dev.agentpush.poc

import java.time.Instant

enum class SessionTagKind {
    PROJECT,
    BRANCH,
    MODEL,
    WORKSPACE,
}

data class SessionTagValue(
    val kind: SessionTagKind,
    val text: String,
)

enum class SessionGrouping {
    PROJECT,
    AGENT,
    TIME,
}

data class SessionGroup(
    val key: String,
    val project: String,
    val workspace: String,
    val branch: String,
    val sessions: List<SessionSummary>,
) {
    val latestTs: String
        get() = sessions.maxOfOrNull { it.lastTs }.orEmpty()
}

enum class SessionQuickAction(val label: String) {
    APPROVE("批准"),
    REPLY("回复"),
    VIEW("查看"),
}

fun SessionStatus.quickAction(): SessionQuickAction {
    return when (this) {
        SessionStatus.NEEDS_PERMISSION -> SessionQuickAction.APPROVE
        SessionStatus.WAITING_INPUT -> SessionQuickAction.REPLY
        else -> SessionQuickAction.VIEW
    }
}

fun sessionTags(session: SessionSummary): List<SessionTagValue> {
    return buildList {
        session.project.trim().takeIf { it.isNotEmpty() }?.let {
            add(SessionTagValue(SessionTagKind.PROJECT, it))
        }
        session.branch.trim()
            .takeIf { it.isNotEmpty() && !it.equals("HEAD", ignoreCase = true) }
            ?.let {
                add(SessionTagValue(SessionTagKind.BRANCH, it))
            }
        session.model.trim().takeIf { it.isNotEmpty() }?.let {
            add(SessionTagValue(SessionTagKind.MODEL, it))
        }
        session.workspace.trim().takeIf { it.isNotEmpty() }?.let {
            add(SessionTagValue(SessionTagKind.WORKSPACE, it))
        }
    }
}

fun sessionSummaryLine(session: SessionSummary, limit: Int = 180): String {
    val raw = session.lastPreview
        .ifBlank { session.latestEvent?.text.orEmpty() }
        .ifBlank { session.latestEvent?.title.orEmpty() }
        .ifBlank { "正在等待这个会话的第一条回复…" }
    return raw
        .replace(Regex("```[\\s\\S]*?```"), " [代码] ")
        .replace(Regex("!\\[[^\\]]*]\\([^)]*\\)"), " [图片] ")
        .replace(Regex("\\[([^\\]]+)]\\([^)]*\\)"), "$1")
        .replace(Regex("[#>*_`~]"), "")
        .replace(Regex("\\s+"), " ")
        .trim()
        .take(limit)
}

fun groupSessions(
    sessions: List<SessionSummary>,
    grouping: SessionGrouping,
): List<SessionGroup> {
    if (grouping == SessionGrouping.TIME) {
        return listOf(
            SessionGroup(
                key = "__time__",
                project = "",
                workspace = "",
                branch = "",
                sessions = sessions,
            ),
        )
    }

    if (grouping == SessionGrouping.AGENT) {
        return sessions
            .groupBy { it.source.ifBlank { it.brand.wire } }
            .map { (source, agentSessions) ->
                val latest = agentSessions.firstOrNull()
                SessionGroup(
                    key = "agent:$source",
                    project = latest?.agentLabel?.ifBlank { latest.brand.displayName }
                        ?: "Agent",
                    workspace = "",
                    branch = "",
                    sessions = agentSessions,
                )
            }
            .sortedByDescending { group ->
                group.latestTs.takeIf { it.isNotBlank() } ?: Instant.EPOCH.toString()
            }
    }

    return sessions
        .groupBy { it.cwd.ifBlank { "__unassigned__" } }
        .map { (cwd, groupSessions) ->
            val latest = groupSessions.firstOrNull()
            SessionGroup(
                key = cwd,
                project = latest?.project?.ifBlank { "未分类项目" } ?: "未分类项目",
                workspace = latest?.workspace.orEmpty(),
                branch = latest?.branch.orEmpty(),
                sessions = groupSessions,
            )
        }
        .sortedByDescending { group ->
            group.latestTs.takeIf { it.isNotBlank() } ?: Instant.EPOCH.toString()
        }
}

fun sessionMarkdownFileName(session: SessionSummary): String {
    val base = session.title
        .trim()
        .ifBlank { "homing-session" }
        .replace(Regex("[\\\\/:*?\"<>|\\p{Cntrl}]"), "-")
        .replace(Regex("\\s+"), "-")
        .trim('-')
        .take(64)
        .ifBlank { "homing-session" }
    return "$base.md"
}

fun buildSessionMarkdown(
    session: SessionSummary,
    items: List<SessionItem> = emptyList(),
): String {
    val out = StringBuilder()
    out.append("# ").append(session.title.ifBlank { "未命名会话" }).append("\n\n")
    out.append("- Agent：").append(session.brand.displayName).append('\n')
    session.project.takeIf { it.isNotBlank() }?.let {
        out.append("- 项目：").append(it).append('\n')
    }
    session.cwd.takeIf { it.isNotBlank() }?.let {
        out.append("- 目录：`").append(it).append("`\n")
    }
    session.branch.takeIf { it.isNotBlank() }?.let {
        out.append("- 分支：`").append(it).append("`\n")
    }
    session.model.takeIf { it.isNotBlank() }?.let {
        out.append("- 模型：`").append(it).append("`\n")
    }
    out.append("- Session ID：`").append(session.sessionId).append("`\n")
    out.append("- 导出时间：").append(Instant.now()).append("\n\n---\n\n")

    if (items.isNotEmpty()) {
        items.forEach { item -> out.appendSessionItemMarkdown(item) }
    } else if (session.transcript?.messages?.isNotEmpty() == true) {
        session.transcript.messages.forEach { message ->
            out.append("### ")
                .append(if (message.isUser) "用户" else session.brand.displayName)
                .append('\n')
            message.ts.takeIf { it.isNotBlank() }?.let {
                out.append("> ").append(it).append("\n\n")
            }
            out.append(message.text.trim()).append("\n\n")
        }
    } else {
        val chronological = buildList {
            session.events.forEach { add(it.ts to it) }
            session.replies.forEach { add(it.ts to it) }
        }.sortedBy { it.first }
        chronological.forEach { (_, value) ->
            when (value) {
                is AgentEvent -> out.append("### ")
                    .append(
                        if (value.kind == "permission" || value.kind == "needs_input") {
                            "等待处理"
                        } else {
                            session.brand.displayName
                        },
                    )
                    .append('\n')
                    .append("> ").append(value.ts).append("\n\n")
                    .append(value.text.ifBlank { value.title }.trim())
                    .append("\n\n")
                is AgentReply -> out.append("### 我的回复\n")
                    .append("> ").append(value.ts).append("\n\n")
                    .append(value.text.trim()).append("\n\n")
            }
        }
    }
    return out.toString().trimEnd() + "\n"
}

private fun StringBuilder.appendSessionItemMarkdown(item: SessionItem) {
    when (item.type) {
        SessionItemType.USER -> append("### 用户\n")
        SessionItemType.ASSISTANT -> append("### Agent\n")
        SessionItemType.REASONING -> append("### 思考\n")
        SessionItemType.TOOL_CALL -> append("### 工具 · ")
            .append(item.tool.name.ifBlank { "调用" }).append('\n')
        SessionItemType.FILE_CHANGE -> append("### 文件改动\n")
        SessionItemType.PLAN -> append("### 计划\n")
        SessionItemType.APPROVAL -> append("### 授权请求\n")
        SessionItemType.ERROR -> append("### 错误\n")
        SessionItemType.SYSTEM -> append("### 系统\n")
        SessionItemType.OTHER -> append("### 条目\n")
    }
    item.ts.takeIf { it.isNotBlank() }?.let {
        append("> ").append(it).append("\n\n")
    }
    item.text.trim().takeIf { it.isNotBlank() }?.let {
        append(it).append("\n\n")
    }
    if (item.type == SessionItemType.TOOL_CALL) {
        item.tool.inputSummary.trim().takeIf { it.isNotBlank() }?.let {
            append("```text\n").append(it).append("\n```\n\n")
        }
        item.tool.outputTail.trim().takeIf { it.isNotBlank() }?.let {
            append("输出（已同步部分）：\n\n```text\n")
                .append(it)
                .append("\n```\n\n")
        }
    }
    if (item.files.isNotEmpty()) {
        item.files.forEach { file ->
            append("- `").append(file.path).append("` +")
                .append(file.added).append(" -").append(file.removed).append('\n')
            if (file.diff.isNotBlank()) {
                append("\n```diff\n").append(file.diff.trimEnd()).append("\n```\n")
            }
        }
        append('\n')
    }
    if (item.plan.isNotEmpty()) {
        item.plan.forEach { step ->
            append("- [")
                .append(if (step.status.equals("completed", true)) "x" else " ")
                .append("] ")
                .append(step.step)
                .append('\n')
        }
        append('\n')
    }
}
