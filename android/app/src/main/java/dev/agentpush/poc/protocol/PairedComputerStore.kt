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
) {
    fun toJson(): JSONObject = JSONObject()
        .put("device_id", deviceId)
        .put("name", name)
        .put("server", server)
        .put("public_key", publicKeyBase64)
        .put("fingerprint", fingerprint)
        .put("paired_at", pairedAt)
        .put("last_seen", lastSeen)

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
            )
        }
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
    }

    fun updateLastSeen(deviceId: String, timestamp: Long = System.currentTimeMillis()) {
        val computer = load().firstOrNull { it.deviceId == deviceId } ?: return
        save(computer.copy(lastSeen = timestamp))
    }

    companion object {
        private const val PREFERENCES = "homing_paired_computers_v1"
        private const val KEY_COMPUTERS = "computers"
    }
}
