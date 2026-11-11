package dev.agentpush.poc

data class AgentMention(
    val agentId: String,
    val text: String,
    val preset: String = "ask",
    val dispatchable: Boolean = true,
    val tokenStart: Int = 0,
    val tokenEnd: Int = 0,
)

data class AgentMentionOptions(
    val target: String = "auto",
    val context: Set<String> = linkedSetOf("recent", "files"),
    val workspace: String = "auto",
)

data class ComposerDispatchPlan(
    val message: String,
    val mentions: List<AgentMention>,
) {
    val hasMentions: Boolean
        get() = mentions.isNotEmpty()

    val mentionsHaveTasks: Boolean
        get() = mentions.all { it.text.isNotBlank() }
}

val agentMentionPresets = setOf("ask", "do", "review", "handoff")

/**
 * UI-only identity for one mention occurrence. The transport protocol still
 * keys overrides by mention order, so two tasks for the same agent are allowed
 * to carry different target/context choices without one overwriting the other.
 */
fun agentMentionOptionKey(mentions: List<AgentMention>, index: Int): String {
    val mention = mentions.getOrNull(index) ?: return ""
    val ordinal = mentions.take(index + 1).count { it.agentId == mention.agentId }
    return "${mention.agentId}#$ordinal"
}

private val agentMentionPattern = Regex(
    "(?<![\\p{L}\\p{N}._%+-])@([a-z0-9][a-z0-9._-]{0,63})" +
        "(?::([a-z-]{1,24}))?\\b",
    RegexOption.IGNORE_CASE,
)

/**
 * Splits a composer message into the normal parent-agent reply and one task per
 * `@agent` mention. Text before the first recognised mention is sent to the
 * current session; everything from a mention to the next mention belongs to
 * that target. Mentions inside fenced/inline code and email addresses are left
 * untouched.
 */
fun parseAgentMentions(
    input: String,
    dispatchableAgents: Set<String>,
): ComposerDispatchPlan {
    if (input.isBlank() || dispatchableAgents.isEmpty()) {
        return ComposerDispatchPlan(input.trim(), emptyList())
    }
    val known = dispatchableAgents.map { it.lowercase() }.toSet()
    val matches = mutableListOf<MentionMatch>()
    var fenceMarker = ""
    var lineStart = 0
    while (lineStart <= input.length) {
        val newline = input.indexOf('\n', lineStart)
        val lineEnd = if (newline >= 0) newline else input.length
        val line = input.substring(lineStart, lineEnd).removeSuffix("\r")
        val trimmed = line.trimStart()
        val marker = when {
            trimmed.startsWith("```") -> "```"
            trimmed.startsWith("~~~") -> "~~~"
            else -> ""
        }
        if (marker.isNotEmpty()) {
            if (fenceMarker.isEmpty()) {
                fenceMarker = marker
            } else if (fenceMarker == marker) {
                fenceMarker = ""
            }
        } else if (fenceMarker.isEmpty()) {
            for (match in agentMentionPattern.findAll(line)) {
                val agentId = match.groupValues[1].lowercase()
                if (insideInlineCode(line, match.range.first)) {
                    continue
                }
                matches += MentionMatch(
                    agentId = agentId,
                    preset = match.groupValues[2]
                        .lowercase()
                        .ifBlank { "ask" },
                    start = lineStart + match.range.first,
                    end = lineStart + match.range.last + 1,
                    dispatchable = agentId in known,
                )
            }
        }
        if (newline < 0) break
        lineStart = newline + 1
    }
    if (matches.isEmpty()) {
        return ComposerDispatchPlan(input.trim(), emptyList())
    }

    val mentions = matches.mapIndexed { index, match ->
        val taskEnd = matches.getOrNull(index + 1)?.start ?: input.length
        AgentMention(
            agentId = match.agentId,
            text = input.substring(match.end, taskEnd).trim(),
            preset = match.preset,
            dispatchable = match.dispatchable,
            tokenStart = match.start,
            tokenEnd = match.end,
        )
    }
    val messageEnd = matches.first().start
    return ComposerDispatchPlan(
        message = input.substring(0, messageEnd).trim(),
        mentions = mentions,
    )
}

/**
 * Rewrites only the mention token, preserving the task text and surrounding
 * message. The server remains the source of truth for the resulting plan.
 */
fun changeAgentMentionPreset(
    input: String,
    mention: AgentMention,
    preset: String,
): String {
    val normalized = preset.lowercase().takeIf { it in agentMentionPresets } ?: "ask"
    if (mention.tokenStart < 0 || mention.tokenEnd <= mention.tokenStart ||
        mention.tokenEnd > input.length
    ) {
        return input
    }
    val token = input.substring(mention.tokenStart, mention.tokenEnd)
    val base = token.substringBefore(':')
    val replacement = if (normalized == "ask") base else "$base:$normalized"
    return input.replaceRange(mention.tokenStart, mention.tokenEnd, replacement)
}

private data class MentionMatch(
    val agentId: String,
    val preset: String,
    val start: Int,
    val end: Int,
    val dispatchable: Boolean,
)

private fun insideInlineCode(line: String, index: Int): Boolean {
    var ticks = 0
    for (position in 0 until index.coerceAtMost(line.length)) {
        if (line[position] == '`') ticks++
    }
    return ticks % 2 == 1
}
