package dev.agentpush.poc

import java.time.Instant

data class ComputerHomeResult(
    val computerId: String,
    val computerName: String,
    val feed: HomeFeed? = null,
    val error: String = "",
)

/**
 * Merges the home feeds of multiple paired computers without changing the
 * single-computer API or session ids. Every rendered session gets the computer
 * that owns it, so opening a card can switch transports first.
 */
fun mergeComputerHomes(
    results: List<ComputerHomeResult>,
    activeComputerId: String = "",
    resumeLimit: Int = 5,
): HomeFeed {
    val ordered = results.sortedWith(
        compareByDescending<ComputerHomeResult> { it.computerId == activeComputerId }
            .thenBy { it.computerName.lowercase() },
    )
    val statuses = ordered.map { result ->
        HomeComputerStatus(
            computerId = result.computerId,
            name = result.computerName,
            available = result.feed != null,
            error = result.error,
            attentionCount = result.feed?.attention?.size ?: 0,
            runningCount = result.feed?.runningCount ?: 0,
            resumeCount = result.feed?.resume?.size ?: 0,
            earlierCount = result.feed?.earlierCount ?: 0,
        )
    }
    val available = ordered.mapNotNull { result ->
        result.feed?.let { feed -> result to labelHome(feed, result.computerId, result.computerName) }
    }
    if (available.isEmpty()) {
        return HomeFeed(computers = statuses, available = false)
    }

    val active = available.firstOrNull { (result, _) -> result.computerId == activeComputerId }
        ?.second
        ?: available.first().second
    val attention = available
        .flatMap { (_, feed) -> feed.attention }
        .distinctBy { "${it.session.computerId}:${it.sessionId}:${it.reason}:${it.eventId}" }
        .sortedWith(
            compareBy<HomeAttention> { attentionRank(it.reason) }
                .thenByDescending { instantMillis(it.at) },
        )
    val running = available
        .flatMap { (_, feed) -> feed.running }
        .distinctBy { "${it.computerId}:${it.sessionId}" }
        .sortedByDescending { instantMillis(it.runningSince.ifBlank { it.lastTs }) }
    val resume = available
        .flatMap { (_, feed) -> feed.resume }
        .distinctBy { "${it.computerId}:${it.sessionId}" }
        .sortedByDescending { instantMillis(it.lastTs) }
        .take(resumeLimit.coerceAtLeast(0))
    val generatedAt = available
        .map { (_, feed) -> feed.generatedAt }
        .filter { it.isNotBlank() }
        .maxOrNull()
        .orEmpty()

    return HomeFeed(
        away = active.away,
        attention = attention,
        running = running,
        resume = resume,
        computers = statuses,
        earlierCount = available.sumOf { it.second.earlierCount },
        earlierNextCursor = active.earlierNextCursor,
        needsYouCount = available.sumOf { it.second.needsYouCount },
        awayNewCount = available.sumOf { it.second.awayNewCount },
        runningCount = available.sumOf { it.second.runningCount },
        newActivityScope = active.newActivityScope,
        generatedAt = generatedAt,
        available = true,
    )
}

private fun labelHome(feed: HomeFeed, computerId: String, computerName: String): HomeFeed {
    fun label(meta: SessionMeta): SessionMeta = meta.copy(
        computerId = computerId,
        computerName = computerName,
    )
    return feed.copy(
        attention = feed.attention.map { item ->
            item.copy(session = label(item.session))
        },
        running = feed.running.map(::label),
        resume = feed.resume.map(::label),
    )
}

private fun attentionRank(reason: String): Int = when (reason) {
    "approval", "permission" -> 0
    "needs_reply", "needs_input" -> 1
    "error" -> 2
    "collab_result" -> 3
    "done" -> 4
    else -> 5
}

private fun instantMillis(value: String): Long {
    if (value.isBlank()) return 0L
    return runCatching { Instant.parse(value).toEpochMilli() }.getOrDefault(0L)
}
