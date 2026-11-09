package dev.agentpush.poc.protocol

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import java.security.KeyStore
import java.util.Base64
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

data class DeviceIdentity(
    val publicKey: ByteArray,
    val secretKey: ByteArray,
    val deviceId: String,
    val fingerprint: String,
) {
    val publicKeyBase64: String
        get() = Base64.getEncoder().encodeToString(publicKey)
}

/**
 * Stores the Ed25519 seed/secret wrapped by an Android Keystore AES key.
 * The public key and derived identifiers are not secret.
 */
class DeviceIdentityStore(context: Context) {
    private val appContext = context.applicationContext
    private val preferences =
        appContext.getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE)
    private val crypto = HomingCryptoAndroid.instance

    @Synchronized
    fun loadOrCreate(): DeviceIdentity {
        val storedPublic = preferences.getString(KEY_PUBLIC, null)
        val storedSecret = preferences.getString(KEY_SECRET, null)
        val storedIv = preferences.getString(KEY_IV, null)
        if (storedPublic != null && storedSecret != null && storedIv != null) {
            runCatching {
                val secret = decrypt(
                    Base64.getDecoder().decode(storedSecret),
                    Base64.getDecoder().decode(storedIv),
                )
                val publicKey = Base64.getDecoder().decode(storedPublic)
                if (secret.size == 64 && publicKey.size == 32) {
                    return identity(publicKey, secret)
                }
            }
        }

        val generated = crypto.generateKeypair()
        val encrypted = encrypt(generated.secretKey)
        preferences.edit()
            .putString(KEY_PUBLIC, Base64.getEncoder().encodeToString(generated.publicKey))
            .putString(KEY_SECRET, Base64.getEncoder().encodeToString(encrypted.ciphertext))
            .putString(KEY_IV, Base64.getEncoder().encodeToString(encrypted.iv))
            .apply()
        return identity(generated.publicKey, generated.secretKey)
    }

    @Synchronized
    fun clear() {
        preferences.edit().clear().apply()
    }

    private fun identity(publicKey: ByteArray, secretKey: ByteArray): DeviceIdentity {
        return DeviceIdentity(
            publicKey = publicKey,
            secretKey = secretKey,
            deviceId = crypto.deviceId(publicKey),
            fingerprint = crypto.fingerprint(publicKey),
        )
    }

    private fun encrypt(plaintext: ByteArray): EncryptedBytes {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, secretKey())
        return EncryptedBytes(cipher.doFinal(plaintext), cipher.iv)
    }

    private fun decrypt(ciphertext: ByteArray, iv: ByteArray): ByteArray {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(
            Cipher.DECRYPT_MODE,
            secretKey(),
            GCMParameterSpec(128, iv),
        )
        return cipher.doFinal(ciphertext)
    }

    private fun secretKey(): SecretKey {
        val keyStore = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }
        (keyStore.getEntry(KEY_ALIAS, null) as? KeyStore.SecretKeyEntry)
            ?.let { return it.secretKey }
        val generator = KeyGenerator.getInstance(
            KeyProperties.KEY_ALGORITHM_AES,
            ANDROID_KEYSTORE,
        )
        generator.init(
            KeyGenParameterSpec.Builder(
                KEY_ALIAS,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return generator.generateKey()
    }

    private data class EncryptedBytes(
        val ciphertext: ByteArray,
        val iv: ByteArray,
    )

    companion object {
        private const val PREFERENCES = "homing_device_identity_v1"
        private const val KEY_PUBLIC = "public_key"
        private const val KEY_SECRET = "secret_key"
        private const val KEY_IV = "secret_iv"
        private const val KEY_ALIAS = "homing-device-identity-v1"
        private const val ANDROID_KEYSTORE = "AndroidKeyStore"
        private const val TRANSFORMATION = "AES/GCM/NoPadding"
    }
}
