package dev.agentpush.poc.ui.screens

import dev.agentpush.poc.SessionItemType

internal data class SessionSearchResult(
    val timelineIndex: Int,
    val role: String,
    val text: String,
    val snippet: String,
    val timestamp: String,
)

internal fun searchSessionTimeline(
    timeline: List<TimelineItem>,
    query: String,
    limit: Int = 50,
): List<SessionSearchResult> {
    val needle = query.trim()
    if (needle.isBlank()) return emptyList()
    return buildList {
        timeline.forEachIndexed { index, item ->
            if (size >= limit) return@buildList
            val searchable = searchableText(item)
            val match = searchable.indexOf(needle, ignoreCase = true)
            if (match < 0) return@forEachIndexed
            add(
                SessionSearchResult(
                    timelineIndex = index,
                    role = roleLabel(item),
                    text = searchable,
                    snippet = snippetAround(searchable, match, needle.length),
                    timestamp = item.timestamp,
                ),
            )
        }
    }
}

internal fun searchableText(item: TimelineItem): String {
    return when (item) {
        is TimelineItem.Message -> item.value.text
        is TimelineItem.Event -> listOf(
            item.value.title,
            item.value.text,
        ).filter { it.isNotBlank() }.joinToString(" ")
        is TimelineItem.Reply -> item.value.text
        is TimelineItem.ProcessGroup -> item.values.joinToString(" ") { value ->
            searchableText(TimelineItem.Item(value, 0))
        }
        is TimelineItem.Item -> buildList {
            val value = item.value
            if (value.text.isNotBlank()) add(value.text)
            if (value.tool.name.isNotBlank()) add(value.tool.name)
            if (value.tool.inputSummary.isNotBlank()) add(value.tool.inputSummary)
            if (value.tool.outputTail.isNotBlank()) add(value.tool.outputTail)
            value.files.forEach { file ->
                add(file.path)
                if (file.diff.isNotBlank()) add(file.diff)
            }
            value.plan.forEach { step -> add(step.step) }
        }.joinToString(" ")
    }.replace(Regex("\\s+"), " ").trim()
}

private fun roleLabel(item: TimelineItem): String {
    return when (item) {
        is TimelineItem.Message ->
            if (item.value.isUser) "你" else "Agent"
        is TimelineItem.Reply -> "你"
        is TimelineItem.ProcessGroup -> "工具过程"
        is TimelineItem.Event -> when (item.value.kind) {
            "permission" -> "授权"
            "needs_input" -> "提问"
            "error" -> "错误"
            else -> "事件"
        }
        is TimelineItem.Item -> when (item.value.type) {
            SessionItemType.USER -> "你"
            SessionItemType.ASSISTANT -> "Agent"
            SessionItemType.REASONING -> "思考"
            SessionItemType.TOOL_CALL -> "工具"
            SessionItemType.FILE_CHANGE -> "文件"
            SessionItemType.PLAN -> "计划"
            SessionItemType.APPROVAL -> "授权"
            SessionItemType.ERROR -> "错误"
            SessionItemType.SYSTEM -> "系统"
            SessionItemType.OTHER -> "记录"
        }
    }
}

private fun snippetAround(text: String, match: Int, matchLength: Int): String {
    val start = (match - 36).coerceAtLeast(0)
    val end = (match + matchLength + 72).coerceAtMost(text.length)
    return buildString {
        if (start > 0) append("…")
        append(text.substring(start, end).trim())
        if (end < text.length) append("…")
    }
}

