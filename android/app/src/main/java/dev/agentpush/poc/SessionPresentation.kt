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
