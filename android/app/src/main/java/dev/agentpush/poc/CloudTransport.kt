package dev.agentpush.poc

import dev.agentpush.poc.protocol.PairedComputer
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.filter
import org.json.JSONObject
import java.util.Base64

interface CloudLink {
    val connected: StateFlow<Boolean>
    val statusDetail: StateFlow<String?>

    fun ensureStarted()

    fun preferredComputer(): PairedComputer?

    suspend fun awaitConnected(timeoutMillis: Long = DEFAULT_CONNECT_TIMEOUT_MILLIS): Boolean

    suspend fun request(
        computer: PairedComputer,
        method: String,
        path: String,
        body: JSONObject? = null,
        timeoutMillis: Long = DEFAULT_RPC_TIMEOUT_MILLIS,
    ): CloudRpcResult

    suspend fun uploadFile(
        computer: PairedComputer,
        path: String,
        bytes: ByteArray,
        contentType: String = "application/octet-stream",
    ): CloudRpcResult

    suspend fun downloadFile(
        computer: PairedComputer,
        path: String,
    ): ByteArray

    fun events(): Flow<TransportEvent>

    companion object {
        const val DEFAULT_CONNECT_TIMEOUT_MILLIS = 6_000L
        const val DEFAULT_RPC_TIMEOUT_MILLIS = 20_000L
    }
}

class CloudTransport(
    private val link: CloudLink,
) : HomingTransport {
    override val kind: TransportKind = TransportKind.CLOUD

    override suspend fun request(
        method: String,
        path: String,
        body: JSONObject?,
    ): TransportResponse {
        val computer = link.preferredComputer()
            ?: throw CloudRpcException("尚未配對可用的電腦")
        link.ensureStarted()
        if (!link.awaitConnected()) {
            throw CloudRpcException("雲端通道尚未連上電腦")
        }
        val result = link.request(
            computer = computer,
            method = method,
            path = path,
            body = body,
        )
        return TransportResponse(
            status = result.status,
            body = result.body,
            raw = transportBodyJson(result.body),
        )
    }

    override fun events(sessionId: String?): Flow<TransportEvent> {
        val all = link.events().filter { event ->
            val active = link.preferredComputer()?.deviceId.orEmpty()
            event.computerId.isBlank() || active.isBlank() || event.computerId == active
        }
        if (sessionId.isNullOrBlank()) return all
        return all.filter { event ->
            val eventSession = event.data.optString("session_id")
            eventSession.isBlank() || eventSession == sessionId
        }
    }

    override suspend fun upload(
        path: String,
        bytes: ByteArray,
        name: String,
    ): JSONObject {
        if (bytes.size > CLOUD_FILE_LIMIT_BYTES) {
            throw CloudRpcException("单文件上限 20 MB")
        }
        val computer = link.preferredComputer()
            ?: throw CloudRpcException("尚未配對可用的電腦")
        link.ensureStarted()
        if (!link.awaitConnected()) {
            throw CloudRpcException("雲端通道尚未連上電腦")
        }
        val response = if (bytes.size <= CLOUD_UPLOAD_LIMIT_BYTES) {
            request(
                method = "POST",
                path = path,
                body = JSONObject()
                    .put("data_base64", Base64.getEncoder().encodeToString(bytes))
                    .put("content_type", "application/octet-stream"),
            )
        } else {
            val result = link.uploadFile(computer, path, bytes)
            TransportResponse(
                status = result.status,
                body = result.body,
                raw = transportBodyJson(result.body),
            )
        }
        if (response.status !in 200..299) {
            throw AgentApiException(
                response.status,
                (response.body as? JSONObject)?.optString("error")
                    ?.takeIf { it.isNotBlank() }
                    ?: "云端上传失败",
            )
        }
        return response.body as? JSONObject
            ?: throw AgentApiException(response.status, "云端上传响应不是 JSON")
    }

    override suspend fun download(url: String): ByteArray {
        val path = extractDownloadPath(url)
            ?: throw CloudRpcException("附件地址無效")
        if (!isAllowedDownloadPath(path)) {
            throw CloudRpcException("云端附件下载只允许能力链接或项目媒体预览")
        }
        val computer = link.preferredComputer()
            ?: throw CloudRpcException("尚未配對可用的電腦")
        link.ensureStarted()
        if (!link.awaitConnected()) {
            throw CloudRpcException("雲端通道尚未連上電腦")
        }
        return link.downloadFile(computer, path)
    }

    private fun extractDownloadPath(rawUrl: String): String? {
        return runCatching {
            val encoded = rawUrl.trim().replace(" ", "%20")
            val uri = java.net.URI(encoded)
            if (uri.host.isNullOrBlank()) {
                val stripped = encoded.substringAfter("://", encoded)
                val qIdx = stripped.indexOf('?')
                val sIdx = stripped.indexOf('/')
                when {
                    stripped.startsWith("/") -> stripped
                    sIdx >= 0 -> stripped.substring(sIdx)
                    qIdx >= 0 -> stripped.substring(qIdx)
                    else -> "/$stripped"
                }
            } else {
                buildString {
                    append(uri.rawPath.ifBlank { "/" })
                    uri.rawQuery?.let { append('?').append(it) }
                }
            }
        }.getOrNull()
    }

    private fun isAllowedDownloadPath(path: String): Boolean {
        if (path.contains("..")) return false
        return path.startsWith("/f/") || path.startsWith("/api/fs/raw?")
    }

    fun isAvailable(): Boolean = link.preferredComputer() != null

    companion object {
        private const val CLOUD_UPLOAD_LIMIT_BYTES = 64 * 1024
        private const val CLOUD_FILE_LIMIT_BYTES = 20 * 1024 * 1024
    }
}
