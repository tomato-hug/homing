package dev.agentpush.poc

import android.content.Context
import java.time.Instant

/**
 * A phone-local inbox session. The relay keeps all history, but the phone only
 * surfaces activity after the user deliberately turns this mode on. Resident
 * sessions are the explicit exception: they stay available until the user
 * removes them.
 */
data class MobileInboxMode(
    val enabled: Boolean = false,
    val sinceMillis: Long = 0L,
    val residentSessionIds: Set<String> = emptySet(),
) {
    fun isResident(sessionId: String): Boolean = sessionId in residentSessionIds

    fun isSessionVisible(sessionId: String, lastActivity: String): Boolean {
        if (isResident(sessionId)) return true
        if (!enabled) return false
        return epochMillis(lastActivity) >= sinceMillis
    }

    fun isSessionVisible(session: SessionMeta): Boolean {
        return isSessionVisible(
            sessionId = session.sessionId,
            lastActivity = latestActivity(session.lastTs, session.runningSince),
        )
    }

    fun isSessionVisible(session: SessionSummary): Boolean {
        return isSessionVisible(
            sessionId = session.sessionId,
            lastActivity = latestActivity(session.lastTs, session.runningSince),
        )
    }

    fun isAttentionVisible(item: HomeAttention): Boolean {
        if (!enabled) return false
        val timestamp = item.at.ifBlank { item.session.lastTs }
        return epochMillis(timestamp) >= sinceMillis
    }

    fun hasNewActivity(session: SessionSummary): Boolean {
        if (!enabled) return false
        return epochMillis(latestActivity(session.lastTs, session.runningSince)) >= sinceMillis
    }

    fun visibleSessions(sessions: List<SessionSummary>): List<SessionSummary> {
        return sessions.filter(::isSessionVisible)
    }

    /**
     * Applies the same cutoff to every home zone. Counts are recomputed from
     * the filtered lists so the headline, badge and rendered cards cannot
     * disagree with each other.
     */
    fun visibleHome(home: HomeFeed): HomeFeed {
        val attention = if (enabled) home.attention.filter(::isAttentionVisible) else emptyList()
        val running = home.running.filter(::isSessionVisible)
        val resume = home.resume.filter(::isSessionVisible)
        return home.copy(
            attention = attention,
            running = running,
            resume = resume,
            earlierCount = 0,
            earlierNextCursor = "",
            needsYouCount = attention.size,
            awayNewCount = 0,
            runningCount = running.size,
        )
    }
}

class MobileInboxPreferences(context: Context) {
    private val preferences = context.applicationContext.getSharedPreferences(
        PREFS_NAME,
        Context.MODE_PRIVATE,
    )

    fun load(): MobileInboxMode {
        return MobileInboxMode(
            enabled = preferences.getBoolean(KEY_ENABLED, false),
            sinceMillis = preferences.getLong(KEY_SINCE, 0L).coerceAtLeast(0L),
            residentSessionIds = preferences
                .getStringSet(KEY_RESIDENT_SESSIONS, emptySet())
                .orEmpty()
                .filter(String::isNotBlank)
                .toSet(),
        )
    }

    fun setEnabled(enabled: Boolean, nowMillis: Long = System.currentTimeMillis()): MobileInboxMode {
        val current = load()
        val next = if (enabled) {
            current.copy(
                enabled = true,
                sinceMillis = if (current.enabled && current.sinceMillis > 0L) {
                    current.sinceMillis
                } else {
                    nowMillis
                },
            )
        } else {
            current.copy(enabled = false, sinceMillis = 0L)
        }
        save(next)
        return next
    }

    fun setResident(sessionId: String, resident: Boolean): MobileInboxMode {
        if (sessionId.isBlank()) return load()
        val current = load()
        val nextIds = if (resident) {
            current.residentSessionIds + sessionId
        } else {
            current.residentSessionIds - sessionId
        }
        val next = current.copy(residentSessionIds = nextIds)
        save(next)
        return next
    }

    private fun save(mode: MobileInboxMode) {
        preferences.edit()
            .putBoolean(KEY_ENABLED, mode.enabled)
            .putLong(KEY_SINCE, mode.sinceMillis)
            .putStringSet(KEY_RESIDENT_SESSIONS, mode.residentSessionIds)
            .apply()
    }

    private companion object {
        const val PREFS_NAME = "homing_mobile_inbox"
        const val KEY_ENABLED = "enabled"
        const val KEY_SINCE = "since_millis"
        const val KEY_RESIDENT_SESSIONS = "resident_session_ids"
    }
}

internal fun epochMillis(timestamp: String): Long {
    if (timestamp.isBlank()) return 0L
    return runCatching { Instant.parse(timestamp).toEpochMilli() }.getOrDefault(0L)
}

private fun latestActivity(lastActivity: String, runningSince: String): String {
    return if (epochMillis(runningSince) > epochMillis(lastActivity)) {
        runningSince
    } else {
        lastActivity
    }
}
