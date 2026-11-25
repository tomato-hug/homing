package dev.agentpush.poc

import org.json.JSONObject
import org.json.JSONTokener
import java.io.ByteArrayOutputStream
import java.util.Base64
import java.util.zip.GZIPInputStream

/** Decodes only explicitly advertised RPC body encodings, after authentication. */
internal object RpcBodyCodec {
    const val MAX_DECODED_BYTES = 8 * 1024 * 1024

    fun decode(payload: JSONObject): Any? {
        val encoding = payload.optString("body_encoding")
        if (encoding.isBlank()) return payload.opt("body")?.takeUnless { it == JSONObject.NULL }
        require(encoding == "gzip+base64") { "不支援的雲端回應編碼" }
        val compressed = Base64.getDecoder().decode(payload.getString("body_base64"))
        val decoded = ByteArrayOutputStream()
        GZIPInputStream(compressed.inputStream()).use { input ->
            val buffer = ByteArray(8192)
            while (true) {
                val count = input.read(buffer)
                if (count < 0) break
                require(decoded.size() + count <= MAX_DECODED_BYTES) { "雲端回應過大" }
                decoded.write(buffer, 0, count)
            }
        }
        return JSONTokener(decoded.toString(Charsets.UTF_8.name())).nextValue()
            .takeUnless { it == JSONObject.NULL }
    }
}
