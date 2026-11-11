package dev.agentpush.poc.protocol

import android.content.Context
import org.json.JSONArray
import org.json.JSONObject

data class PairedComputer(
    val deviceId: String,
    val name: String,
    val server: String,
    val publicKeyBase64: String,
    val fingerprint: String,
    val pairedAt: Long,
    val lastSeen: Long,
    val lanServers: List<String> = emptyList(),
) {
    fun toJson(): JSONObject = JSONObject()
        .put("device_id", deviceId)
        .put("name", name)
        .put("server", server)
        .put("public_key", publicKeyBase64)
        .put("fingerprint", fingerprint)
        .put("paired_at", pairedAt)
        .put("last_seen", lastSeen)
        .put("lan_servers", JSONArray(lanServers))

    fun serverCandidates(): List<String> = buildList {
        if (server.isNotBlank()) add(server)
        lanServers.forEach { candidate ->
            if (candidate.isNotBlank() && candidate !in this) add(candidate)
        }
    }

    companion object {
        fun fromJson(json: JSONObject): PairedComputer? {
            val deviceId = json.optString("device_id")
            val publicKey = json.optString("public_key")
            if (deviceId.isBlank() || publicKey.isBlank()) return null
            return PairedComputer(
                deviceId = deviceId,
                name = json.optString("name").ifBlank { "我的 Mac" },
                server = json.optString("server"),
                publicKeyBase64 = publicKey,
                fingerprint = json.optString("fingerprint"),
                pairedAt = json.optLong("paired_at", 0L),
                lastSeen = json.optLong("last_seen", 0L),
                lanServers = json.optJSONArray("lan_servers")
                    ?.let(::stringList)
                    .orEmpty()
                    .ifEmpty {
                        json.optJSONArray("lan_urls")
                            ?.let(::stringList)
                            .orEmpty()
                    },
            )
        }

        private fun stringList(array: JSONArray): List<String> = buildList {
            for (index in 0 until array.length()) {
                array.optString(index)
                    .trim()
                    .takeIf { it.isNotBlank() }
                    ?.let(::add)
            }
        }.distinct().take(8)
    }
}

class PairedComputerStore(context: Context) {
    private val preferences =
        context.applicationContext.getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE)

    fun load(): List<PairedComputer> {
        val raw = preferences.getString(KEY_COMPUTERS, null) ?: return emptyList()
        return runCatching {
            val array = JSONArray(raw)
            buildList {
                for (index in 0 until array.length()) {
                    array.optJSONObject(index)
                        ?.let(PairedComputer::fromJson)
                        ?.let(::add)
                }
            }
        }.getOrDefault(emptyList())
    }

    fun save(computer: PairedComputer) {
        val next = load()
            .filterNot { it.deviceId == computer.deviceId }
            .plus(computer)
            .sortedByDescending { it.pairedAt }
        val array = JSONArray()
        next.forEach { array.put(it.toJson()) }
        preferences.edit().putString(KEY_COMPUTERS, array.toString()).apply()
    }

    fun remove(deviceId: String) {
        val array = JSONArray()
        load().filterNot { it.deviceId == deviceId }.forEach { array.put(it.toJson()) }
        preferences.edit().putString(KEY_COMPUTERS, array.toString()).apply()
        if (selectedDeviceId() == deviceId) {
            preferences.edit().remove(KEY_SELECTED).apply()
        }
    }

    fun updateLastSeen(deviceId: String, timestamp: Long = System.currentTimeMillis()) {
        val computer = load().firstOrNull { it.deviceId == deviceId } ?: return
        save(computer.copy(lastSeen = timestamp))
    }

    fun select(deviceId: String) {
        if (deviceId.isBlank()) {
            preferences.edit().remove(KEY_SELECTED).apply()
            return
        }
        if (load().none { it.deviceId == deviceId }) return
        preferences.edit().putString(KEY_SELECTED, deviceId).apply()
    }

    fun selectedDeviceId(): String = preferences.getString(KEY_SELECTED, "").orEmpty()

    fun active(): PairedComputer? {
        val computers = load()
        return computers.firstOrNull { it.deviceId == selectedDeviceId() }
            ?: computers.maxByOrNull { it.lastSeen }
    }

    companion object {
        private const val PREFERENCES = "homing_paired_computers_v1"
        private const val KEY_COMPUTERS = "computers"
        private const val KEY_SELECTED = "selected_device_id"
    }
}
