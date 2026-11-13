package dev.agentpush.poc

import kotlinx.coroutines.flow.Flow
import org.json.JSONArray
import org.json.JSONObject
import org.json.JSONTokener

enum class TransportKind(val label: String) {
    LAN("局域网"),
    CLOUD("云端"),
}

data class TransportResponse(
    val status: Int,
    val body: Any?,
    val raw: String,
)

data class TransportEvent(
    val name: String,
    val data: JSONObject,
    val id: String = "",
    val computerId: String = "",
)

interface HomingTransport {
    suspend fun request(
        method: String,
        path: String,
        body: JSONObject? = null,
    ): TransportResponse

    fun events(sessionId: String? = null): Flow<TransportEvent>

    suspend fun upload(
        path: String,
        bytes: ByteArray,
        name: String,
    ): JSONObject

    suspend fun download(url: String): ByteArray

    val kind: TransportKind
}

// Mutations can have executed even when their acknowledgement was lost.
internal fun canReplayTransportRequest(method: String): Boolean =
    method.trim().uppercase().ifBlank { "GET" } in setOf("GET", "HEAD")

internal fun parseTransportBody(raw: String): Any? {
    if (raw.isBlank()) return null
    return runCatching { JSONTokener(raw).nextValue() }.getOrNull()
}

internal fun transportBodyJson(body: Any?): String {
    return when (body) {
        null, JSONObject.NULL -> ""
        is JSONObject -> body.toString()
        is JSONArray -> body.toString()
        is String -> body
        else -> runCatching { JSONObject.wrap(body)?.toString() }.getOrNull()
            ?: body.toString()
    }
}
