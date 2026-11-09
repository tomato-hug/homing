package dev.agentpush.poc

import com.goterl.lazysodium.LazySodiumJava
import com.goterl.lazysodium.SodiumJava
import dev.agentpush.poc.protocol.HomingCrypto
import dev.agentpush.poc.protocol.HomingEnvelope
import org.json.JSONObject
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Base64

class HomingCryptoTest {
    private val crypto = HomingCrypto(LazySodiumJava(SodiumJava()))

    private fun resource(name: String): String {
        return javaClass.getResourceAsStream("/$name")!!
            .bufferedReader()
            .use { it.readText() }
    }

    private fun hex(value: String): ByteArray {
        return value.chunked(2)
            .map { it.toInt(16).toByte() }
            .toByteArray()
    }

    private fun hex(bytes: ByteArray): String {
        return bytes.joinToString("") { "%02x".format(it) }
    }

    @Test
    fun crossLanguageVectorsMatch() {
        val vectors = JSONObject(resource("test-vectors.json"))
        val words = resource("bip39-chinese-simplified.txt")
            .lineSequence()
            .filter { it.isNotBlank() }
            .toList()
        assertEquals(2048, words.size)

        val hostVector = vectors.getJSONObject("host")
        val clientVector = vectors.getJSONObject("client")
        val host = crypto.keypairFromSeed(hostVector.getString("seed"))
        val client = crypto.keypairFromSeed(clientVector.getString("seed"))

        assertEquals(hostVector.getString("publicKeyHex"), hex(host.publicKey))
        assertEquals(clientVector.getString("publicKeyHex"), hex(client.publicKey))
        assertEquals(
            hostVector.getString("curvePublicKeyHex"),
            hex(crypto.curveKeys(host).publicKey),
        )
        assertEquals(
            clientVector.getString("curvePublicKeyHex"),
            hex(crypto.curveKeys(client).publicKey),
        )
        assertEquals(hostVector.getString("deviceId"), crypto.deviceId(host.publicKey))
        assertEquals(hostVector.getString("fingerprint"), crypto.fingerprint(host.publicKey))
        assertEquals(clientVector.getString("deviceId"), crypto.deviceId(client.publicKey))

        val sas = vectors.getJSONObject("sas")
        assertEquals(
            sas.getJSONArray("words").let { array ->
                (0 until array.length()).map(array::getString)
            },
            crypto.sasWords(host.publicKey, client.publicKey, words),
        )
        assertTrue(
            crypto.verifyHostFingerprint(
                host.publicKey,
                hostVector.getString("fingerprint"),
            ),
        )
        assertFalse(
            crypto.verifyHostFingerprint(host.publicKey, "0000000000000000"),
        )
    }

    @Test
    fun helloSignatureAndEnvelopeVectorsMatch() {
        val vectors = JSONObject(resource("test-vectors.json"))
        val host = crypto.keypairFromSeed(vectors.getJSONObject("host").getString("seed"))
        val client = crypto.keypairFromSeed(vectors.getJSONObject("client").getString("seed"))
        val hello = vectors.getJSONObject("hello")
        val signature = hex(hello.getString("signatureHex"))
        assertTrue(
            crypto.verifyHelloSignature(
                host.publicKey,
                hello.getString("nonce"),
                hello.getString("ts"),
                signature,
            ),
        )
        assertArrayEquals(
            signature,
            crypto.helloSignature(
                host.secretKey,
                hello.getString("nonce"),
                hello.getString("ts"),
            ),
        )
        assertFalse(
            crypto.verifyHelloSignature(
                host.publicKey,
                hello.getString("nonce"),
                hello.getString("ts") + "x",
                signature,
            ),
        )

        val envelopeJson = vectors.getJSONObject("envelope")
        val envelope = HomingEnvelope(
            v = envelopeJson.getInt("v"),
            t = envelopeJson.getString("t"),
            id = envelopeJson.getString("id"),
            from = envelopeJson.getString("from"),
            to = envelopeJson.getString("to"),
            ts = envelopeJson.getString("ts"),
            nonce = envelopeJson.getString("nonce"),
            ct = envelopeJson.getString("ct"),
        )
        val payload = crypto.openEnvelope(envelope, host.publicKey, client.secretKey)
        assertEquals(
            envelopeJson.getJSONObject("payload").toString(),
            JSONObject(payload).toString(),
        )
        val rebuilt = crypto.createEnvelope(
            from = envelope.from,
            to = envelope.to,
            payloadJson = payload,
            sender = host,
            recipientPublicKey = client.publicKey,
            id = envelope.id,
            ts = envelope.ts,
            nonce = Base64.getDecoder().decode(envelope.nonce),
        )
        assertEquals(envelope.ct, rebuilt.ct)
    }
}
