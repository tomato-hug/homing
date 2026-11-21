package dev.agentpush.poc

import android.content.Context
import org.json.JSONArray
import org.json.JSONObject

/** Phone display state. None of these operations changes a host's read/archive flags. */
data class PhoneReceipt(val eventId: Long, val timestamp: String, val receivedAt: Long)

data class PhoneActivity(val eventId: Long = 0L, val timestamp: String = "") {
    fun isNewerThan(cutoff: PhoneActivity): Boolean =
        eventId > cutoff.eventId || epochMillis(timestamp) > epochMillis(cutoff.timestamp)
}

data class PhoneCleanupSnapshot(
    val inbox: Map<String, PhoneActivity>,
    val hidden: Map<String, PhoneActivity>,
)

data class PhoneConversationState(
    val receipts: Map<String, PhoneReceipt> = emptyMap(),
    val clearedInbox: Map<String, PhoneActivity> = emptyMap(),
    val hiddenSessions: Map<String, PhoneActivity> = emptyMap(),
    val histories: Map<String, SessionHistoryState> = emptyMap(),
    val undo: PhoneCleanupSnapshot? = null,
) {
    val hasHiddenSessions: Boolean get() = hiddenSessions.isNotEmpty()
    val canUndo: Boolean get() = undo != null

    fun receive(key: String, eventId: Long, timestamp: String, now: Long = System.currentTimeMillis()): PhoneConversationState {
        if (eventId <= 0L || receipts.containsKey(key)) return this
        return copy(receipts = receipts + (key to PhoneReceipt(eventId, timestamp, now)))
    }

    fun clearInbox(activity: Map<String, PhoneActivity>): PhoneConversationState = copy(
        clearedInbox = clearedInbox + activity,
        undo = PhoneCleanupSnapshot(clearedInbox, hiddenSessions),
    )

    fun hide(activity: Map<String, PhoneActivity>): PhoneConversationState = copy(
        hiddenSessions = hiddenSessions + activity,
        undo = PhoneCleanupSnapshot(clearedInbox, hiddenSessions),
    )

    fun undoCleanup(): PhoneConversationState = undo?.let {
        copy(clearedInbox = it.inbox, hiddenSessions = it.hidden, undo = null)
    } ?: this

    fun restoreSessions(): PhoneConversationState = copy(
        hiddenSessions = emptyMap(),
        undo = PhoneCleanupSnapshot(clearedInbox, hiddenSessions),
    )

    fun isSessionHidden(session: SessionSummary, fallbackComputerId: String = ""): Boolean {
        val key = phoneConversationKey(session.computerId.ifBlank { fallbackComputerId }, session.source, session.sessionId)
        return hiddenSessions[key]?.let { !session.phoneActivity().isNewerThan(it) } ?: false
    }

    fun isInboxVisible(session: SessionSummary, fallbackComputerId: String = ""): Boolean {
        if (isSessionHidden(session, fallbackComputerId)) return false
        val key = phoneConversationKey(session.computerId.ifBlank { fallbackComputerId }, session.source, session.sessionId)
        return clearedInbox[key]?.let { session.phoneActivity().isNewerThan(it) } ?: true
    }

    fun visibleHome(home: HomeFeed, fallbackComputerId: String = ""): HomeFeed {
        fun visible(meta: SessionMeta, activity: PhoneActivity): Boolean {
            val key = meta.phoneKey(fallbackComputerId)
            return listOfNotNull(clearedInbox[key], hiddenSessions[key]).all(activity::isNewerThan)
        }
        val attention = home.attention.groupBy { it.session.phoneKey(fallbackComputerId) }
            .values.map { entries ->
                val latest = entries.maxWith(compareBy<HomeAttention> { epochMillis(it.at) }.thenBy { it.eventId })
                latest.copy(unseenCount = entries.maxOf { it.unseenCount })
            }.filter { visible(it.session, PhoneActivity(it.eventId, it.at.ifBlank { it.session.lastTs })) }
        val attentionKeys = attention.map { it.session.phoneKey(fallbackComputerId) }.toSet()
        val running = home.running.distinctBy { it.phoneKey(fallbackComputerId) }
            .filter { it.phoneKey(fallbackComputerId) !in attentionKeys && visible(it, PhoneActivity(timestamp = it.lastTs)) }
        val displayedKeys = attentionKeys + running.map { it.phoneKey(fallbackComputerId) }
        val resume = home.resume.distinctBy { it.phoneKey(fallbackComputerId) }
            .filter { it.phoneKey(fallbackComputerId) !in displayedKeys && visible(it, PhoneActivity(timestamp = it.lastTs)) }
        return home.copy(attention = attention, running = running, resume = resume,
            needsYouCount = attention.size, awayNewCount = 0, runningCount = running.size,
            earlierCount = 0, earlierNextCursor = "")
    }
}

// JSON array encoding avoids delimiter collisions in computer/source/session names.
fun phoneConversationKey(computer: String, source: String, sessionId: String): String =
    JSONArray(listOf(computer, source, sessionId)).toString()

internal fun SessionMeta.phoneKey(fallbackComputerId: String): String =
    phoneConversationKey(computerId.ifBlank { fallbackComputerId }, source, sessionId)

internal fun SessionSummary.phoneActivity(): PhoneActivity = PhoneActivity(
    events.maxOfOrNull { it.id } ?: 0L,
    listOf(lastTs, runningSince, latestEvent?.ts.orEmpty()).maxByOrNull(::epochMillis).orEmpty(),
)

internal class PhoneConversationPreferences(context: Context) {
    private val preferences = context.applicationContext.getSharedPreferences("homing_phone_conversations_v1", Context.MODE_PRIVATE)

    fun load(): PhoneConversationState = runCatching {
        val root = JSONObject(preferences.getString("state", "{}").orEmpty())
        fun activities(name: String): Map<String, PhoneActivity> {
            val json = root.optJSONObject(name) ?: return emptyMap()
            return json.keys().asSequence().associateWith { key ->
                val value = json.getJSONObject(key)
                PhoneActivity(value.optLong("event"), value.optString("ts"))
            }
        }
        val receipts = root.optJSONObject("receipts") ?: JSONObject()
        val histories = root.optJSONObject("histories") ?: JSONObject()
        PhoneConversationState(
            receipts = receipts.keys().asSequence().associateWith { key ->
                val value = receipts.getJSONObject(key)
                PhoneReceipt(value.getLong("event"), value.optString("ts"), value.optLong("received"))
            },
            clearedInbox = activities("inbox"), hiddenSessions = activities("hidden"),
            histories = histories.keys().asSequence().associateWith { SessionHistoryState.fromJson(histories.getJSONObject(it)) },
        )
    }.getOrDefault(PhoneConversationState())

    @Synchronized fun save(state: PhoneConversationState) {
        fun activities(values: Map<String, PhoneActivity>) = JSONObject().apply {
            values.forEach { (key, value) -> put(key, JSONObject().put("event", value.eventId).put("ts", value.timestamp)) }
        }
        val root = JSONObject().put("inbox", activities(state.clearedInbox)).put("hidden", activities(state.hiddenSessions))
            .put("receipts", JSONObject().apply {
                state.receipts.forEach { (key, value) -> put(key, JSONObject().put("event", value.eventId).put("ts", value.timestamp).put("received", value.receivedAt)) }
            }).put("histories", JSONObject().apply {
                state.histories.forEach { (key, value) -> put(key, value.toJson()) }
            })
        preferences.edit().putString("state", root.toString()).apply()
    }
}

data class SessionHistoryState(
    val managed: Boolean = false,
    val anchorEventId: Long = 0,
    val beforeCursor: String? = null,
    val status: String = "unknown",
    val reason: String? = null,
    val loading: Boolean = false,
    val error: String? = null,
    val loadedRounds: Int = 0,
    val revision: Int = 0,
) {
    internal fun toJson(): JSONObject = JSONObject().put("anchor", anchorEventId)
        .put("before", beforeCursor).put("status", status).put("reason", reason)
        .put("rounds", loadedRounds).put("revision", revision)

    companion object {
        internal fun fromJson(json: JSONObject) = SessionHistoryState(
            managed = true, anchorEventId = json.optLong("anchor"),
            beforeCursor = json.optString("before").takeIf { it.isNotBlank() && it != "null" },
            status = json.optString("status", "unknown"),
            reason = json.optString("reason").takeIf { it.isNotBlank() && it != "null" },
            loadedRounds = json.optInt("rounds"), revision = json.optInt("revision"),
        )
    }
}

/** Retain explicit earlier pages on refresh; mutable provider rows upsert by server identity. */
internal fun mergePhoneItems(existing: List<SessionItem>, incoming: List<SessionItem>, prepend: Boolean): List<SessionItem> {
    fun key(item: SessionItem): String = item.itemKey.ifBlank { "live:${item.id}" }
    fun confirms(old: SessionItem, item: SessionItem): Boolean = old.itemKey.isBlank() && item.itemKey.isNotBlank() &&
        item.type == old.type && item.ts == old.ts && item.text == old.text && item.tool == old.tool &&
        item.files == old.files && item.plan == old.plan && item.role == old.role && item.model == old.model
    val retained = existing.toMutableList()
    val added = mutableListOf<SessionItem>()
    for (item in incoming) {
        var index = retained.indexOfFirst { key(it) == key(item) }
        if (index < 0) {
            val candidates = retained.withIndex().filter { confirms(it.value, item) }
            if (candidates.size == 1) index = candidates.single().index
        }
        if (index >= 0) retained[index] = item else added += item
    }
    return (if (prepend) added + retained else retained + added).distinctBy(::key)
}
