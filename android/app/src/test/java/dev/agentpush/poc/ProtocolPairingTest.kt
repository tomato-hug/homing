package dev.agentpush.poc

import android.app.Application
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import dev.agentpush.poc.protocol.PairedComputer
import dev.agentpush.poc.protocol.PairedComputerStore
import dev.agentpush.poc.protocol.PairingHostVerifier
import dev.agentpush.poc.protocol.PairingInvite
import dev.agentpush.poc.protocol.ProtocolUrls
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

class PairingInviteTest {
    @Test
    fun roundTripsEncodedServerTokenAndHostFields() {
        val invite = PairingInvite(
            server = "wss://relay.example.test:8443/v1/ws?region=cn",
            token = "demo+token/with=padding&symbols",
            hostId = "host-device-id",
            fingerprint = "0011223344556677",
        )

        assertEquals(invite, PairingInvite.parse(invite.toUri()))
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectsNonHomingQrPayload() {
        PairingInvite.parse("https://example.test/pair?t=demo")
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectsIncompletePairingPayload() {
        PairingInvite.parse("homing://pair?s=wss%3A%2F%2Frelay.example.test&t=demo")
    }

    @Test
    fun normalizesHttpAndHttpsServerAddresses() {
        assertEquals(
            "ws://192.168.1.2:18787/v1/ws",
            ProtocolUrls.normalizeWebSocketUrl("http://192.168.1.2:18787"),
        )
        assertEquals(
            "wss://relay.example.test/v1/ws",
            ProtocolUrls.normalizeWebSocketUrl("relay.example.test"),
        )
        assertEquals(
            "wss://relay.example.test/custom/socket",
            ProtocolUrls.normalizeWebSocketUrl("https://relay.example.test/custom/socket"),
        )
    }
}

class PairingHostVerifierTest {
    @Test
    fun acceptsMatchingHostIdAndFingerprint() {
        assertNull(
            PairingHostVerifier.check(
                hostId = "host-a",
                expectedHostId = "host-a",
                actualFingerprint = "0011223344556677",
                expectedFingerprint = "0011223344556677",
            ),
        )
    }

    @Test
    fun abortsWhenFingerprintDoesNotMatch() {
        val error = PairingHostVerifier.check(
            hostId = "host-a",
            expectedHostId = "host-a",
            actualFingerprint = "0011223344556677",
            expectedFingerprint = "8899aabbccddeeff",
        )
        assertTrue(error?.contains("指纹不一致") == true)
    }

    @Test
    fun abortsWhenHostIdDoesNotMatch() {
        val error = PairingHostVerifier.check(
            hostId = "host-b",
            expectedHostId = "host-a",
            actualFingerprint = "0011223344556677",
            expectedFingerprint = "0011223344556677",
        )
        assertTrue(error?.contains("电脑 ID") == true)
    }
}

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class PairedComputerStoreTest {
    private val app = ApplicationProvider.getApplicationContext<Application>()
    private val preferences = app.getSharedPreferences(
        "homing_paired_computers_v1",
        Context.MODE_PRIVATE,
    )

    @Before
    fun clearStoredComputers() {
        preferences.edit().clear().commit()
    }

    @Test
    fun savesUpdatesAndRemovesPairedComputers() {
        val store = PairedComputerStore(app)
        val first = computer("host-a", "书房 Mac", 100L)
        val second = computer("host-b", "笔记本", 200L)

        store.save(first)
        store.save(second)
        assertEquals(listOf("host-b", "host-a"), store.load().map { it.deviceId })

        store.updateLastSeen("host-a", 999L)
        assertEquals(999L, store.load().first { it.deviceId == "host-a" }.lastSeen)

        store.remove("host-b")
        assertEquals(listOf("host-a"), store.load().map { it.deviceId })
    }

    @Test
    fun malformedCacheFallsBackToEmptyList() {
        preferences.edit().putString("computers", "{not-json").commit()
        assertTrue(PairedComputerStore(app).load().isEmpty())
    }

    private fun computer(id: String, name: String, pairedAt: Long): PairedComputer {
        return PairedComputer(
            deviceId = id,
            name = name,
            server = "wss://relay.example.test/v1/ws",
            publicKeyBase64 = "demo-public-key",
            fingerprint = "0011223344556677",
            pairedAt = pairedAt,
            lastSeen = pairedAt,
        )
    }
}
