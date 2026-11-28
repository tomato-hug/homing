package dev.agentpush.poc

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.channelFlow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.isActive
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.withContext
import okhttp3.Call
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.io.EOFException
import java.net.URLEncoder
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

class LanTransport(
    private val settingsProvider: () -> AppSettings,
    private val httpClient: OkHttpClient = defaultClient(),
    private val probeClient: OkHttpClient = defaultProbeClient(),
) : HomingTransport {
    override val kind: TransportKind = TransportKind.LAN

    override suspend fun request(
        method: String,
        path: String,
        body: JSONObject?,
    ): TransportResponse = withContext(Dispatchers.IO) {
        val settings = settingsProvider()
        if (settings.baseUrl.isBlank() || settings.token.isBlank()) {
            throw AgentApiException(0, "尚未配置局域网服务器或 Token")
        }
        val normalizedMethod = method.trim().uppercase().ifBlank { "GET" }
        val request = Request.Builder()
            .url(settings.baseUrl.trimEnd('/') + path)
            .header("Authorization", "Bearer ${settings.token}")
            .header("Accept", "application/json")
            .method(
                normalizedMethod,
                when {
                    body != null -> body.toString().toRequestBody(JSON_MEDIA_TYPE)
                    normalizedMethod in METHODS_WITH_BODY -> ByteArray(0)
                        .toRequestBody(JSON_MEDIA_TYPE)
                    else -> null
                },
            )
            .build()
        httpClient.newCall(request).execute().use { response ->
            val raw = response.body?.string().orEmpty()
            TransportResponse(
                status = response.code,
                body = parseTransportBody(raw),
                raw = raw,
            )
        }
    }

    override fun events(sessionId: String?): Flow<TransportEvent> = channelFlow {
        val activeCall = AtomicReference<Call?>(null)
        launch {
            var backoffMillis = INITIAL_RECONNECT_MILLIS
            var lastEventId = ""
            while (isActive) {
                val settings = settingsProvider()
                if (settings.baseUrl.isBlank() || settings.token.isBlank()) {
                    delay(backoffMillis)
                    backoffMillis = nextBackoff(backoffMillis)
                    continue
                }
                try {
                    val query = sessionId?.takeIf { it.isNotBlank() }?.let {
                        "?session_id=${URLEncoder.encode(it, Charsets.UTF_8.name())}"
                    }.orEmpty()
                    val requestBuilder = Request.Builder()
                        .url(settings.baseUrl.trimEnd('/') + "/api/stream$query")
                        .header("Authorization", "Bearer ${settings.token}")
                        .header("Accept", "text/event-stream")
                        .header("Cache-Control", "no-cache")
                    if (lastEventId.isNotBlank()) {
                        requestBuilder.header("Last-Event-ID", lastEventId)
                    }
                    val call = httpClient.newCall(requestBuilder.build())
                    activeCall.set(call)
                    currentCoroutineContext().ensureActive()
                    call.execute().use { response ->
                        if (response.code !in 200..299) {
                            throw AgentApiException(
                                response.code,
                                "HTTP ${response.code}",
                            )
                        }
                        backoffMillis = INITIAL_RECONNECT_MILLIS
                        val source = response.body?.source()
                            ?: throw EOFException("SSE 响应没有正文")
                        val dataLines = mutableListOf<String>()
                        var eventName = ""
                        var eventId = ""

                        suspend fun dispatch() {
                            if (dataLines.isEmpty()) {
                                eventName = ""
                                eventId = ""
                                return
                            }
                            val raw = dataLines.joinToString("\n")
                            val name = eventName.ifBlank { "message" }
                            val id = eventId
                            dataLines.clear()
                            eventName = ""
                            eventId = ""
                            val data = runCatching { JSONObject(raw) }.getOrNull() ?: return
                            if (id.isNotBlank()) {
                                lastEventId = id
                            }
                            send(TransportEvent(name = name, data = data, id = id))
                        }

                        while (isActive) {
                            val line = source.readUtf8Line() ?: break
                            when {
                                line.isEmpty() -> dispatch()
                                line.startsWith(":") -> Unit
                                line.startsWith("event:") ->
                                    eventName = line.substringAfter("event:").trim()
                                line.startsWith("id:") ->
                                    eventId = line.substringAfter("id:").trim()
                                line.startsWith("data:") ->
                                    dataLines.add(line.substringAfter("data:").removePrefix(" "))
                            }
                        }
                        dispatch()
                    }
                    if (isActive) {
                        throw EOFException("中继关闭了 SSE 连接")
                    }
                } catch (error: Exception) {
                    if (!isActive) break
                    if (error is AgentApiException && error.statusCode !in 0..599) {
                        throw error
                    }
                } finally {
                    activeCall.getAndSet(null)?.cancel()
                }
                if (!isActive) break
                delay(backoffMillis)
                backoffMillis = nextBackoff(backoffMillis)
            }
        }
        awaitClose { activeCall.getAndSet(null)?.cancel() }
    }.flowOn(Dispatchers.IO)

    override suspend fun upload(
        path: String,
        bytes: ByteArray,
        name: String,
    ): JSONObject = withContext(Dispatchers.IO) {
        val settings = settingsProvider()
        val separator = if (path.contains('?')) "&" else "?"
        val url = settings.baseUrl.trimEnd('/') + path +
            "$separator${"name"}=${URLEncoder.encode(name, Charsets.UTF_8.name())}"
        val request = Request.Builder()
            .url(url)
            .header("Authorization", "Bearer ${settings.token}")
            .header("Accept", "application/json")
            .post(bytes.toRequestBody("application/octet-stream".toMediaType()))
            .build()
        httpClient.newCall(request).execute().use { response ->
            val raw = response.body?.string().orEmpty()
            if (response.code !in 200..299) {
                throw AgentApiException(response.code, apiError(raw, response.code))
            }
            runCatching { JSONObject(raw) }.getOrElse {
                throw AgentApiException(response.code, "上传响应不是 JSON")
            }
        }
    }

    override suspend fun download(url: String): ByteArray = withContext(Dispatchers.IO) {
        val settings = settingsProvider()
        val absolute = when {
            url.startsWith(AgentApi.CLOUD_ATTACHMENT_ORIGIN) && settings.baseUrl.isNotBlank() ->
                settings.baseUrl.trimEnd('/') + "/" +
                    url.removePrefix(AgentApi.CLOUD_ATTACHMENT_ORIGIN).trimStart('/')
            url.startsWith("http://") || url.startsWith("https://") -> url
            else -> settings.baseUrl.trimEnd('/') + "/" + url.trimStart('/')
        }
        val request = Request.Builder()
            .url(absolute)
            .header("Authorization", "Bearer ${settings.token}")
            .get()
            .build()
        httpClient.newCall(request).execute().use { response ->
            if (response.code !in 200..299) {
                val raw = response.body?.string().orEmpty()
                throw AgentApiException(response.code, apiError(raw, response.code))
            }
            response.body?.bytes() ?: throw AgentApiException(
                response.code,
                "附件响应没有正文",
            )
        }
    }

    suspend fun probeHealth(): Boolean = withContext(Dispatchers.IO) {
        val settings = settingsProvider()
        // E2E-only pairing uses encrypted RPC, not legacy HTTP bearer auth.
        if (settings.baseUrl.isBlank() || settings.token.isBlank()) return@withContext false
        val request = Request.Builder()
            .url(settings.baseUrl.trimEnd('/') + "/health")
            .get()
            .build()
        runCatching {
            probeClient.newCall(request).execute().use { it.code in 200..299 }
        }.getOrDefault(false)
    }

    private fun apiError(raw: String, status: Int): String {
        return runCatching { JSONObject(raw).optString("error") }
            .getOrNull()
            ?.takeIf { it.isNotBlank() }
            ?: raw.take(240).ifBlank { "HTTP $status" }
    }

    private fun nextBackoff(current: Long): Long {
        return (current * 2).coerceAtMost(MAX_RECONNECT_MILLIS)
    }

    companion object {
        private const val INITIAL_RECONNECT_MILLIS = 1_000L
        private const val MAX_RECONNECT_MILLIS = 15_000L
        private val METHODS_WITH_BODY = setOf("POST", "PATCH", "PUT")
        private val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()

        private fun defaultClient(): OkHttpClient {
            return OkHttpClient.Builder()
                .connectTimeout(6, TimeUnit.SECONDS)
                .readTimeout(60, TimeUnit.SECONDS)
                .build()
        }

        private fun defaultProbeClient(): OkHttpClient {
            return OkHttpClient.Builder()
                .connectTimeout(800, TimeUnit.MILLISECONDS)
                .readTimeout(800, TimeUnit.MILLISECONDS)
                .callTimeout(800, TimeUnit.MILLISECONDS)
                .build()
        }
    }
}
