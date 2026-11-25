package dev.agentpush.poc

import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.ByteArrayOutputStream
import java.util.Base64
import java.util.zip.GZIPOutputStream

class RpcBodyCodecTest {
    @Test fun legacyAndCompressedBodiesHaveIdenticalMeaning() {
        val body = JSONObject().put("text", "demo 合成測試\n".repeat(1000))
        assertEquals(body.toString(), RpcBodyCodec.decode(JSONObject().put("body", body)).toString())
        assertEquals(body.toString(), RpcBodyCodec.decode(compressed(body.toString())).toString())
        assertNull(RpcBodyCodec.decode(compressed("null")))
    }

    @Test fun invalidOrOversizeCompressedBodiesFailExplicitly() {
        for (payload in listOf(
            JSONObject().put("body_encoding", "unknown"),
            JSONObject().put("body_encoding", "gzip+base64").put("body_base64", "!invalid"),
            compressed("x".repeat(RpcBodyCodec.MAX_DECODED_BYTES + 1)),
        )) assertNotNull(runCatching { RpcBodyCodec.decode(payload) }.exceptionOrNull())
    }

    private fun compressed(text: String): JSONObject {
        val out = ByteArrayOutputStream()
        GZIPOutputStream(out).use { it.write(text.toByteArray(Charsets.UTF_8)) }
        return JSONObject().put("body_encoding", "gzip+base64")
            .put("body_base64", Base64.getEncoder().encodeToString(out.toByteArray()))
    }
}
