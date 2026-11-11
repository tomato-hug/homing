package dev.agentpush.poc

import org.json.JSONObject

data class AgentCatalogEntry(
    val id: String,
    val name: String = "",
    val vendor: String = "",
    val icon: String = "",
    val readiness: String = "",
    val detected: Boolean = false,
    val connected: Boolean = false,
    val available: Boolean = false,
    val canConnect: Boolean = false,
    val sessions: Boolean = false,
    val reply: Boolean = false,
    val hookInstalled: Boolean = false,
    val notes: String = "",
) {
    val displayName: String
        get() = name.ifBlank { id.replace('-', ' ').replace('_', ' ') }

    val logoSource: String
        get() = icon.ifBlank { id }

    companion object {
        fun fromJson(json: JSONObject): AgentCatalogEntry? {
            val id = json.stringOrBlank("id")
                .ifBlank { json.stringOrBlank("source") }
                .lowercase()
            if (id.isBlank()) return null
            return AgentCatalogEntry(
                id = id,
                name = json.stringOrBlank("name")
                    .ifBlank { json.stringOrBlank("display_name") },
                vendor = json.stringOrBlank("vendor"),
                icon = json.stringOrBlank("icon"),
                readiness = json.stringOrBlank("readiness"),
                detected = json.optBoolean("detected", false),
                connected = json.optBoolean("connected", false),
                available = json.optBoolean("available", false),
                canConnect = json.optBoolean("can_connect", false),
                sessions = json.optBoolean("sessions", false),
                reply = json.optBoolean("reply", false),
                hookInstalled = json.optBoolean("hook_installed", false),
                notes = json.stringOrBlank("notes"),
            )
        }
    }
}

data class AgentCatalog(
    val agents: List<AgentCatalogEntry> = emptyList(),
    val connectedCount: Int = 0,
    val detectedCount: Int = 0,
    val available: Boolean = false,
) {
    val connected: List<AgentCatalogEntry>
        get() = agents.filter { it.connected }

    val connectable: List<AgentCatalogEntry>
        get() = agents
            .filter { !it.connected && it.canConnect }
            .sortedWith(
                compareByDescending<AgentCatalogEntry> { it.detected }
                    .thenBy { it.displayName.lowercase() },
            )

    companion object {
        fun fromJson(root: JSONObject): AgentCatalog {
            val agents = buildList {
                val array = root.optJSONArray("agents") ?: return@buildList
                for (index in 0 until array.length()) {
                    array.optJSONObject(index)
                        ?.let(AgentCatalogEntry::fromJson)
                        ?.let(::add)
                }
            }
            val summary = root.optJSONObject("summary")
            return AgentCatalog(
                agents = agents,
                connectedCount = summary?.optInt("connected", 0)
                    ?: agents.count { it.connected },
                detectedCount = summary?.optInt("detected", 0)
                    ?: agents.count { it.detected },
                available = true,
            )
        }
    }
}

data class AgentActionResult(
    val ok: Boolean,
    val message: String = "",
)
