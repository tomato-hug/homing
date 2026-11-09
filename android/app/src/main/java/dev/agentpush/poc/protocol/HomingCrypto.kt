package dev.agentpush.poc.protocol

import com.goterl.lazysodium.LazySodium
import com.goterl.lazysodium.LazySodiumAndroid
import com.goterl.lazysodium.SodiumAndroid
import com.goterl.lazysodium.interfaces.Box
import com.goterl.lazysodium.interfaces.GenericHash
import com.goterl.lazysodium.interfaces.Sign
import com.goterl.lazysodium.utils.Key
import java.nio.charset.StandardCharsets
import java.security.SecureRandom
import java.util.Base64

data class HomingKeyPair(
    val publicKey: ByteArray,
    val secretKey: ByteArray,
)

data class CurveKeyPair(
    val publicKey: ByteArray,
    val secretKey: ByteArray,
)

data class HomingEnvelope(
    val v: Int = 1,
    val t: String = "msg",
    val id: String,
    val from: String,
    val to: String,
    val ts: String,
    val nonce: String,
    val ct: String,
)

/**
 * Pure libsodium operations shared by the Android client and JVM vector tests.
 * The Android app should use [HomingCryptoAndroid.instance].
 */
class HomingCrypto(private val sodium: LazySodium) {
    fun keypairFromSeed(seedText: String): HomingKeyPair {
        val seed = seedText.toByteArray(StandardCharsets.UTF_8)
        require(seed.size == Sign.ED25519_SEEDBYTES) {
            "Ed25519 seed must be ${Sign.ED25519_SEEDBYTES} bytes"
        }
        val pair = sodium.cryptoSignSeedKeypair(seed)
        return HomingKeyPair(
            publicKey = pair.publicKey.asBytes,
            secretKey = pair.secretKey.asBytes,
        )
    }

    fun generateKeypair(): HomingKeyPair {
        val pair = sodium.cryptoSignKeypair()
        return HomingKeyPair(
            publicKey = pair.publicKey.asBytes,
            secretKey = pair.secretKey.asBytes,
        )
    }

    fun curveKeys(keys: HomingKeyPair): CurveKeyPair {
        val publicKey = ByteArray(Sign.CURVE25519_PUBLICKEYBYTES)
        val secretKey = ByteArray(Sign.CURVE25519_SECRETKEYBYTES)
        check(sodium.convertPublicKeyEd25519ToCurve25519(publicKey, keys.publicKey)) {
            "could not convert Ed25519 public key"
        }
        check(sodium.convertSecretKeyEd25519ToCurve25519(secretKey, keys.secretKey)) {
            "could not convert Ed25519 secret key"
        }
        return CurveKeyPair(publicKey, secretKey)
    }

    fun deviceId(publicKey: ByteArray): String {
        return base32Lower(hash(16, publicKey))
    }

    fun fingerprint(publicKey: ByteArray): String {
        return hash(8, publicKey).toHex()
    }

    fun verifyHostFingerprint(publicKey: ByteArray, expected: String): Boolean {
        return fingerprint(publicKey).equals(expected.trim(), ignoreCase = true)
    }

    fun sasWords(
        hostPublicKey: ByteArray,
        clientPublicKey: ByteArray,
        words: List<String>,
    ): List<String> {
        require(words.size == 2048) { "BIP39 word list must contain 2048 words" }
        val digest = hash(8, hostPublicKey + clientPublicKey)
        var bits = 0L
        for (index in 0 until 6) {
            bits = (bits shl 8) or (digest[index].toLong() and 0xffL)
        }
        bits = bits ushr 4
        return (3 downTo 0).map { group ->
            val index = ((bits ushr (group * 11)) and 0x7ffL).toInt()
            words[index]
        }
    }

    fun helloSignature(secretKey: ByteArray, nonceBase64: String, ts: String): ByteArray {
        val message = fromBase64(nonceBase64) + ts.toByteArray(StandardCharsets.UTF_8)
        val signature = ByteArray(Sign.ED25519_BYTES)
        check(
            sodium.cryptoSignDetached(
                signature,
                message,
                message.size.toLong(),
                secretKey,
            ),
        ) { "could not sign hello" }
        return signature
    }

    fun verifyHelloSignature(
        publicKey: ByteArray,
        nonceBase64: String,
        ts: String,
        signature: ByteArray,
    ): Boolean {
        val message = fromBase64(nonceBase64) + ts.toByteArray(StandardCharsets.UTF_8)
        return sodium.cryptoSignVerifyDetached(
            signature,
            message,
            message.size,
            publicKey,
        )
    }

    fun createEnvelope(
        from: String,
        to: String,
        payloadJson: String,
        sender: HomingKeyPair,
        recipientPublicKey: ByteArray,
        id: String = ulid(),
        ts: String = java.time.Instant.now().toString(),
        nonce: ByteArray = randomBytes(Box.NONCEBYTES),
    ): HomingEnvelope {
        require(nonce.size == Box.NONCEBYTES) { "crypto_box nonce must be 24 bytes" }
        val senderCurve = curveKeys(sender)
        val recipientCurvePublic = ByteArray(Sign.CURVE25519_PUBLICKEYBYTES)
        check(
            sodium.convertPublicKeyEd25519ToCurve25519(
                recipientCurvePublic,
                recipientPublicKey,
            ),
        ) { "could not convert recipient key" }
        val message = payloadJson.toByteArray(StandardCharsets.UTF_8)
        val ciphertext = ByteArray(message.size + Box.MACBYTES)
        check(
            sodium.cryptoBoxEasy(
                ciphertext,
                message,
                message.size.toLong(),
                nonce,
                recipientCurvePublic,
                senderCurve.secretKey,
            ),
        ) { "could not encrypt envelope" }
        return HomingEnvelope(
            id = id,
            from = from,
            to = to,
            ts = ts,
            nonce = toBase64(nonce),
            ct = toBase64(ciphertext),
        )
    }

    fun openEnvelope(
        envelope: HomingEnvelope,
        senderPublicKey: ByteArray,
        recipientSecretKey: ByteArray,
    ): String {
        require(envelope.v == 1 && envelope.t == "msg") { "unsupported envelope" }
        val nonce = fromBase64(envelope.nonce)
        val ciphertext = fromBase64(envelope.ct)
        val senderCurvePublic = ByteArray(Sign.CURVE25519_PUBLICKEYBYTES)
        check(
            sodium.convertPublicKeyEd25519ToCurve25519(
                senderCurvePublic,
                senderPublicKey,
            ),
        ) { "could not convert sender key" }
        val recipientCurveSecret = ByteArray(Sign.CURVE25519_SECRETKEYBYTES)
        check(
            sodium.convertSecretKeyEd25519ToCurve25519(
                recipientCurveSecret,
                recipientSecretKey,
            ),
        ) { "could not convert recipient secret key" }
        val plaintext = ByteArray(ciphertext.size - Box.MACBYTES)
        check(
            sodium.cryptoBoxOpenEasy(
                plaintext,
                ciphertext,
                ciphertext.size.toLong(),
                nonce,
                senderCurvePublic,
                recipientCurveSecret,
            ),
        ) { "could not decrypt envelope" }
        return String(plaintext, StandardCharsets.UTF_8)
    }

    fun randomBytes(size: Int): ByteArray = sodium.randomBytesBuf(size)

    fun ulid(now: Long = System.currentTimeMillis()): String {
        val time = now and ((1L shl 48) - 1)
        return encodeCrockford(time, 10) + encodeCrockford(randomLong(), 16)
    }

    fun toBase64(bytes: ByteArray): String = Base64.getEncoder().encodeToString(bytes)

    fun fromBase64(value: String): ByteArray = Base64.getDecoder().decode(value)

    private fun hash(size: Int, message: ByteArray): ByteArray {
        require(size in 1..GenericHash.BYTES_MAX)
        val output = ByteArray(size)
        check(sodium.cryptoGenericHash(output, size, message, message.size.toLong())) {
            "could not calculate BLAKE2b"
        }
        return output
    }

    private fun randomLong(): Long {
        val bytes = SecureRandom().generateSeed(8)
        var value = 0L
        for (byte in bytes) value = (value shl 8) or (byte.toLong() and 0xffL)
        return value
    }

    private fun encodeCrockford(input: Long, length: Int): String {
        var value = input
        val output = CharArray(length)
        for (index in length - 1 downTo 0) {
            output[index] = CROCKFORD[(value and 31L).toInt()]
            value = value ushr 5
        }
        return String(output)
    }

    private fun ByteArray.toHex(): String {
        val output = CharArray(size * 2)
        forEachIndexed { index, byte ->
            val value = byte.toInt() and 0xff
            output[index * 2] = Character.forDigit(value ushr 4, 16)
            output[index * 2 + 1] = Character.forDigit(value and 0x0f, 16)
        }
        return String(output)
    }

    private fun base32Lower(bytes: ByteArray): String {
        var buffer = 0L
        var bits = 0
        val output = StringBuilder()
        for (byte in bytes) {
            buffer = (buffer shl 8) or (byte.toLong() and 0xffL)
            bits += 8
            while (bits >= 5) {
                bits -= 5
                output.append(BASE32[((buffer ushr bits) and 31L).toInt()])
                buffer = buffer and ((1L shl bits) - 1L)
            }
        }
        if (bits > 0) {
            output.append(BASE32[((buffer shl (5 - bits)) and 31L).toInt()])
        }
        return output.toString()
    }

    private val com.goterl.lazysodium.utils.Key.asBytes: ByteArray
        get() = getAsBytes()

    companion object {
        private const val BASE32 = "abcdefghijklmnopqrstuvwxyz234567"
        private const val CROCKFORD = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
    }
}

object HomingCryptoAndroid {
    val instance: HomingCrypto by lazy {
        HomingCrypto(LazySodiumAndroid(SodiumAndroid()))
    }
}
