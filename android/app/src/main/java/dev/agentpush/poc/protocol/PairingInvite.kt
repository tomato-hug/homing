package dev.agentpush.poc.protocol

import java.net.URI
import java.net.URLDecoder
import java.net.URLEncoder
import java.nio.charset.StandardCharsets

data class PairingInvite(
    val server: String,
    val token: String,
    val hostId: String,
    val fingerprint: String,
) {
    fun toUri(): String {
        fun encode(value: String): String =
            URLEncoder.encode(value, StandardCharsets.UTF_8.name())
        return "homing://pair?s=${encode(server)}&t=${encode(token)}" +
            "&h=${encode(hostId)}&fp=${encode(fingerprint)}"
    }

    companion object {
        fun parse(value: String): PairingInvite {
            val uri = URI(value)
            require(uri.scheme == "homing" && uri.host == "pair") {
                "不是 Homing 配对二维码"
            }
            val params = parseQuery(uri.rawQuery.orEmpty())
            val server = params["s"]
            val token = params["t"]
            val hostId = params["h"]
            val fingerprint = params["fp"]
            require(!server.isNullOrBlank() && !token.isNullOrBlank() &&
                !hostId.isNullOrBlank() && !fingerprint.isNullOrBlank()
            ) { "配对二维码字段不完整" }
            return PairingInvite(server, token, hostId, fingerprint)
        }

        private fun parseQuery(query: String): Map<String, String> {
            if (query.isBlank()) return emptyMap()
            return query.split("&").mapNotNull { pair ->
                val index = pair.indexOf('=')
                if (index <= 0) return@mapNotNull null
                val key = URLDecoder.decode(pair.substring(0, index), StandardCharsets.UTF_8.name())
                val value = URLDecoder.decode(pair.substring(index + 1), StandardCharsets.UTF_8.name())
                key to value
            }.toMap()
        }
    }
}

object ProtocolUrls {
    fun normalizeWebSocketUrl(server: String): String {
        val raw = server.trim().ifBlank { throw IllegalArgumentException("服务器地址不能为空") }
        val withScheme = if (raw.contains("://")) raw else "wss://$raw"
        val uri = URI(withScheme)
        val scheme = when (uri.scheme?.lowercase()) {
            "https", "wss" -> "wss"
            "http", "ws" -> "ws"
            else -> throw IllegalArgumentException("服务器地址必须使用 ws、wss、http 或 https")
        }
        val path = uri.rawPath?.takeIf { it.isNotBlank() && it != "/" } ?: "/v1/ws"
        return URI(
            scheme,
            uri.rawUserInfo,
            uri.host,
            uri.port,
            path,
            uri.rawQuery,
            null,
        ).toString()
    }
}
