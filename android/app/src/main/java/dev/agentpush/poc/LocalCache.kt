package dev.agentpush.poc

import android.content.Context
import androidx.room.Dao
import androidx.room.Database
import androidx.room.Entity
import androidx.room.Index
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import androidx.room.Room
import androidx.room.RoomDatabase
import androidx.room.Transaction
import androidx.room.migration.Migration
import androidx.sqlite.db.SupportSQLiteDatabase
import org.json.JSONArray
import org.json.JSONObject

@Entity(tableName = "home_snapshot")
data class HomeSnapshotEntity(
    @PrimaryKey val id: Int = 1,
    val payload: String,
    val updatedAt: Long,
)

@Entity(tableName = "sessions")
data class SessionSnapshotEntity(
    @PrimaryKey val sessionId: String,
    val payload: String,
    val sortKey: String,
    val updatedAt: Long,
)

@Entity(tableName = "drafts")
data class DraftEntity(
    @PrimaryKey val sessionId: String,
    val text: String,
    val updatedAt: Long,
)

@Entity(tableName = "outbox")
data class OutboxEntity(
    @PrimaryKey(autoGenerate = true) val id: Long = 0L,
    val localReplyId: Long,
    val sessionId: String,
    val eventId: Long?,
    val source: String,
    val text: String,
    val filesJson: String,
    val model: String,
    val effort: String,
    val permissionMode: String,
    val createdAt: Long,
    val lastError: String? = null,
)

@Entity(
    tableName = "items",
    primaryKeys = ["sessionId", "itemId"],
    indices = [Index("sessionId")],
)
data class ItemSnapshotEntity(
    val sessionId: String,
    val itemId: String,
    val position: Int,
    val payload: String,
    val updatedAt: Long,
)

@Dao
interface LocalCacheDao {
    @Query("SELECT payload FROM home_snapshot WHERE id = 1")
    suspend fun homeSnapshot(): String?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun putHomeSnapshot(snapshot: HomeSnapshotEntity)

    @Query("SELECT * FROM sessions ORDER BY sortKey DESC")
    suspend fun sessions(): List<SessionSnapshotEntity>

    @Query("DELETE FROM sessions")
    suspend fun deleteSessions()

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun putSessions(sessions: List<SessionSnapshotEntity>)

    @Transaction
    suspend fun replaceSessions(sessions: List<SessionSnapshotEntity>) {
        deleteSessions()
        if (sessions.isNotEmpty()) putSessions(sessions)
    }

    @Query("SELECT * FROM drafts")
    suspend fun drafts(): List<DraftEntity>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun putDraft(draft: DraftEntity)

    @Query("DELETE FROM drafts WHERE sessionId = :sessionId")
    suspend fun deleteDraft(sessionId: String)

    @Query("DELETE FROM home_snapshot")
    suspend fun clearHomeSnapshot()

    @Query("DELETE FROM sessions")
    suspend fun clearSessions()

    @Query("DELETE FROM drafts")
    suspend fun clearDrafts()

    @Query("SELECT * FROM items ORDER BY sessionId ASC, position ASC")
    suspend fun allItems(): List<ItemSnapshotEntity>

    @Query("DELETE FROM items WHERE sessionId = :sessionId")
    suspend fun deleteItems(sessionId: String)

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun putItems(items: List<ItemSnapshotEntity>)

    @Transaction
    suspend fun replaceItems(sessionId: String, items: List<ItemSnapshotEntity>) {
        deleteItems(sessionId)
        if (items.isNotEmpty()) putItems(items)
    }

    @Query("DELETE FROM items")
    suspend fun clearItems()

    @Query("SELECT * FROM outbox ORDER BY id ASC")
    suspend fun outbox(): List<OutboxEntity>

    @Insert
    suspend fun enqueueOutbox(item: OutboxEntity): Long

    @Query("DELETE FROM outbox WHERE id = :id")
    suspend fun deleteOutbox(id: Long)

    @Query("UPDATE outbox SET lastError = :error WHERE id = :id")
    suspend fun markOutboxError(id: Long, error: String?)

    @Query("DELETE FROM outbox")
    suspend fun clearOutbox()
}

@Database(
    entities = [
        HomeSnapshotEntity::class,
        SessionSnapshotEntity::class,
        DraftEntity::class,
        OutboxEntity::class,
        ItemSnapshotEntity::class,
    ],
    version = 3,
    exportSchema = false,
)
abstract class HomingDatabase : RoomDatabase() {
    abstract fun localCacheDao(): LocalCacheDao

    companion object {
        @Volatile
        private var instance: HomingDatabase? = null

        fun get(context: Context): HomingDatabase {
            return instance ?: synchronized(this) {
                instance ?: Room.databaseBuilder(
                    context.applicationContext,
                    HomingDatabase::class.java,
                    "homing-cache.db",
                ).addMigrations(MIGRATION_1_2, MIGRATION_2_3).build().also { instance = it }
            }
        }

        private val MIGRATION_1_2 = object : Migration(1, 2) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL(
                    """
                    CREATE TABLE IF NOT EXISTS `outbox` (
                        `id` INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
                        `localReplyId` INTEGER NOT NULL,
                        `sessionId` TEXT NOT NULL,
                        `eventId` INTEGER,
                        `source` TEXT NOT NULL,
                        `text` TEXT NOT NULL,
                        `filesJson` TEXT NOT NULL,
                        `model` TEXT NOT NULL,
                        `effort` TEXT NOT NULL,
                        `permissionMode` TEXT NOT NULL,
                        `createdAt` INTEGER NOT NULL,
                        `lastError` TEXT
                    )
                    """.trimIndent(),
                )
            }
        }

        private val MIGRATION_2_3 = object : Migration(2, 3) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL(
                    """
                    CREATE TABLE IF NOT EXISTS `items` (
                        `sessionId` TEXT NOT NULL,
                        `itemId` TEXT NOT NULL,
                        `position` INTEGER NOT NULL,
                        `payload` TEXT NOT NULL,
                        `updatedAt` INTEGER NOT NULL,
                        PRIMARY KEY(`sessionId`, `itemId`)
                    )
                    """.trimIndent(),
                )
                db.execSQL(
                    "CREATE INDEX IF NOT EXISTS `index_items_sessionId` ON `items` (`sessionId`)",
                )
            }
        }
    }
}

internal data class CachedSnapshot(
    val sessions: List<SessionMeta>,
    val inbox: InboxFeed?,
    val home: HomeFeed?,
    val projects: List<ProjectInfo>,
    val away: Boolean,
    val drafts: Map<String, String>,
    val items: Map<String, List<SessionItem>>,
    val savedAt: Long?,
    val phoneComputerKey: String = "",
    val phoneEvents: List<AgentEvent> = emptyList(),
)

internal class LocalCache(context: Context) {
    private val dao = HomingDatabase.get(context).localCacheDao()

    suspend fun load(): CachedSnapshot {
        val sessions = dao.sessions().mapNotNull { row ->
            runCatching { SessionMeta.fromJson(JSONObject(row.payload)) }.getOrNull()
        }
        val home = dao.homeSnapshot()?.let { raw ->
            runCatching { decodeHome(JSONObject(raw)) }.getOrNull()
        }
        val items = dao.allItems()
            .groupBy { it.sessionId }
            .mapValues { (sessionId, rows) ->
                rows.sortedBy { it.position }.mapNotNull { row ->
                    runCatching {
                        SessionItem.fromJson(JSONObject(row.payload), fallbackId = row.itemId)
                    }.getOrNull()
                }
            }
        return CachedSnapshot(
            sessions = sessions,
            inbox = home?.inbox,
            home = home?.home,
            projects = home?.projects.orEmpty(),
            away = home?.away ?: false,
            drafts = dao.drafts().associate { it.sessionId to it.text },
            items = items,
            savedAt = home?.savedAt,
            phoneComputerKey = home?.phoneComputerKey.orEmpty(),
            phoneEvents = home?.phoneEvents.orEmpty(),
        )
    }

    suspend fun saveSnapshot(
        sessions: Collection<SessionMeta>,
        inbox: InboxFeed?,
        home: HomeFeed?,
        projects: List<ProjectInfo>,
        away: Boolean,
        drafts: Map<String, String>,
        savedAt: Long = System.currentTimeMillis(),
        phoneComputerKey: String = "",
        phoneEvents: Collection<AgentEvent> = emptyList(),
    ) {
        val sessionRows = sessions
            .filter { it.sessionId.isNotBlank() }
            .map {
                SessionSnapshotEntity(
                    sessionId = it.sessionId,
                    payload = it.toJson().toString(),
                    sortKey = it.lastTs,
                    updatedAt = savedAt,
                )
            }
        dao.replaceSessions(sessionRows)
        dao.putHomeSnapshot(
            HomeSnapshotEntity(
                payload = encodeHome(inbox, home, projects, away, savedAt, phoneComputerKey, phoneEvents).toString(),
                updatedAt = savedAt,
            ),
        )
        drafts.forEach { (sessionId, text) ->
            if (sessionId.isNotBlank() && text.isNotBlank()) {
                dao.putDraft(DraftEntity(sessionId, text, savedAt))
            }
        }
    }

    suspend fun saveDraft(sessionId: String, text: String) {
        if (sessionId.isBlank()) return
        if (text.isBlank()) {
            dao.deleteDraft(sessionId)
        } else {
            dao.putDraft(DraftEntity(sessionId, text, System.currentTimeMillis()))
        }
    }

    suspend fun saveItems(sessionId: String, items: List<SessionItem>) {
        if (sessionId.isBlank()) return
        val now = System.currentTimeMillis()
        // Explicitly requested pages are never silently trimmed while their cursor advances.
        val recent = if (sessionId.startsWith("[")) items else items.takeLast(MAX_ITEMS_PER_SESSION)
        dao.replaceItems(
            sessionId,
            recent.mapIndexed { index, item ->
                ItemSnapshotEntity(
                    sessionId = sessionId,
                    itemId = item.itemKey.ifBlank { item.id },
                    position = index,
                    payload = item.toJson().toString(),
                    updatedAt = now,
                )
            },
        )
    }

    suspend fun clear() {
        dao.clearHomeSnapshot()
        dao.clearSessions()
        dao.clearDrafts()
        dao.clearOutbox()
        dao.clearItems()
    }

    suspend fun queueReply(item: OutboxReply): Long {
        return dao.enqueueOutbox(
            OutboxEntity(
                localReplyId = item.localReplyId,
                sessionId = item.sessionId,
                eventId = item.eventId,
                source = item.source,
                text = item.text,
                filesJson = JSONArray(item.files).toString(),
                model = item.model,
                effort = item.effort,
                permissionMode = item.permissionMode,
                createdAt = System.currentTimeMillis(),
                lastError = item.lastError,
            ),
        )
    }

    suspend fun outbox(): List<OutboxReply> {
        return dao.outbox().map { row ->
            OutboxReply(
                id = row.id,
                localReplyId = row.localReplyId,
                sessionId = row.sessionId,
                eventId = row.eventId,
                source = row.source,
                text = row.text,
                files = runCatching { JSONArray(row.filesJson) }.getOrNull()
                    ?.let { array ->
                        buildList {
                            for (index in 0 until array.length()) {
                                array.optString(index)
                                    .takeIf { it.isNotBlank() }
                                    ?.let(::add)
                            }
                        }
                    }
                    .orEmpty(),
                model = row.model,
                effort = row.effort,
                permissionMode = row.permissionMode,
                createdAt = row.createdAt,
                lastError = row.lastError,
            )
        }
    }

    suspend fun deleteOutbox(id: Long) {
        dao.deleteOutbox(id)
    }

    suspend fun markOutboxError(id: Long, error: String?) {
        dao.markOutboxError(id, error)
    }

    companion object {
        const val MAX_ITEMS_PER_SESSION = 300
    }
}

internal data class OutboxReply(
    val id: Long = 0L,
    val localReplyId: Long,
    val sessionId: String,
    val eventId: Long?,
    val source: String,
    val text: String,
    val files: List<String> = emptyList(),
    val model: String = "",
    val effort: String = "",
    val permissionMode: String = "",
    val createdAt: Long = 0L,
    val lastError: String? = null,
)

private data class CachedHome(
    val inbox: InboxFeed?,
    val home: HomeFeed?,
    val projects: List<ProjectInfo>,
    val away: Boolean,
    val savedAt: Long,
    val phoneComputerKey: String,
    val phoneEvents: List<AgentEvent>,
)

private fun encodeHome(
    inbox: InboxFeed?,
    home: HomeFeed?,
    projects: List<ProjectInfo>,
    away: Boolean,
    savedAt: Long,
    phoneComputerKey: String,
    phoneEvents: Collection<AgentEvent>,
): JSONObject {
    return JSONObject()
        .put("version", 1)
        .put("phone_computer", phoneComputerKey)
        .put("phone_events", JSONArray().apply { phoneEvents.forEach { put(it.toJson()) } })
        .put("saved_at", savedAt)
        .put("away", away)
        .put("projects", JSONArray().apply {
            projects.forEach { put(it.toJson()) }
        })
        .put("inbox", inbox?.toJson())
        .put("home", home?.toJson())
}

private fun decodeHome(json: JSONObject): CachedHome {
    val projects = json.optJSONArray("projects")
    val inboxJson = json.optJSONObject("inbox")
    val homeJson = json.optJSONObject("home")
    return CachedHome(
        inbox = inboxJson?.let(::decodeInbox),
        home = homeJson?.let { runCatching { HomeFeed.fromJson(it) }.getOrNull() },
        projects = buildList {
            if (projects != null) {
                for (index in 0 until projects.length()) {
                    projects.optJSONObject(index)?.let(::add)
                }
            }
        }.map(ProjectInfo::fromJson),
        away = json.optBoolean("away", false),
        savedAt = json.optLong("saved_at", 0L),
        phoneComputerKey = json.optString("phone_computer"),
        phoneEvents = json.optJSONArray("phone_events")?.let { array ->
            (0 until array.length()).mapNotNull { index -> array.optJSONObject(index)?.let(AgentEvent::fromJson) }
        }.orEmpty(),
    )
}

private fun ProjectInfo.toJson(): JSONObject {
    return JSONObject()
        .put("cwd", cwd)
        .put("project", project)
        .put("count", count)
        .put("last_ts", lastTs)
        .put("sources", JSONArray(sources))
}

private fun InboxEntry.toJson(): JSONObject {
    return JSONObject()
        .put("kind", kind?.wire)
        .put("session_id", sessionId)
        .put("event_id", eventId)
        .put("title", title)
        .put("summary", summary)
        .put("at", at)
        .put("pinned", pinned)
        .put("read", read)
        .put("session", sessionMeta?.toJson())
}

private fun InboxFeed.toJson(): JSONObject {
    return JSONObject()
        .put("waiting", JSONArray().apply { waiting.forEach { put(it.toJson()) } })
        .put("running", JSONArray().apply { running.forEach { put(it.toJson()) } })
        .put("recent_done", JSONArray().apply { recentDone.forEach { put(it.toJson()) } })
        .put("waiting_count", waitingCount)
        .put("running_count", runningCount)
        .put("generated_at", generatedAt)
        .put("available", available)
}

private fun decodeInbox(json: JSONObject): InboxFeed {
    fun entries(key: String): List<InboxEntry> {
        val array = json.optJSONArray(key) ?: return emptyList()
        return buildList {
            for (index in 0 until array.length()) {
                array.optJSONObject(index)?.let(InboxEntry::fromJson)?.let(::add)
            }
        }
    }
    val waiting = entries("waiting")
    val running = entries("running")
    return InboxFeed(
        waiting = waiting,
        running = running,
        recentDone = entries("recent_done"),
        waitingCount = json.optInt("waiting_count", waiting.size),
        runningCount = json.optInt("running_count", running.size),
        generatedAt = json.stringOrBlank("generated_at"),
        available = json.optBoolean("available", true),
    )
}
