package dev.agentpush.poc

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import dev.agentpush.poc.protocol.PairedComputer
import dev.agentpush.poc.protocol.PairedComputerStore
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class PairedComputerSelectionTest {
    @Test
    fun selectedComputerSurvivesReorderingAndFallsBackAfterRemoval() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        context.getSharedPreferences("homing_paired_computers_v1", Context.MODE_PRIVATE)
            .edit()
            .clear()
            .commit()
        val store = PairedComputerStore(context)
        store.save(computer("computer-a", "書房 Mac", pairedAt = 10L, lastSeen = 20L))
        store.save(computer("computer-b", "客廳 Mac", pairedAt = 20L, lastSeen = 30L))

        assertEquals("computer-b", store.active()?.deviceId)

        store.select("computer-a")
        assertEquals("computer-a", store.active()?.deviceId)

        store.updateLastSeen("computer-b", 99L)
        assertEquals("computer-a", store.active()?.deviceId)

        store.remove("computer-a")
        assertEquals("computer-b", store.active()?.deviceId)

        store.remove("computer-b")
        assertNull(store.active())
    }

    @Test
    fun lanFallbackEndpointsSurviveStorage() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        context.getSharedPreferences("homing_paired_computers_v1", Context.MODE_PRIVATE)
            .edit()
            .clear()
            .commit()
        val store = PairedComputerStore(context)
        store.save(
            computer("computer-a", "書房 Mac", pairedAt = 10L, lastSeen = 20L)
                .copy(
                    lanServers = listOf(
                        "ws://192.168.1.20:18787/v1/ws",
                        "ws://10.0.0.8:18787/v1/ws",
                    ),
                ),
        )

        val restored = store.load().single()
        assertEquals(
            listOf(
                "wss://relay.example/v1/ws",
                "ws://192.168.1.20:18787/v1/ws",
                "ws://10.0.0.8:18787/v1/ws",
            ),
            restored.serverCandidates(),
        )
    }

    private fun computer(
        id: String,
        name: String,
        pairedAt: Long,
        lastSeen: Long,
    ): PairedComputer = PairedComputer(
        deviceId = id,
        name = name,
        server = "wss://relay.example/v1/ws",
        publicKeyBase64 = "demo-public-key",
        fingerprint = "demo-fingerprint",
        pairedAt = pairedAt,
        lastSeen = lastSeen,
    )
}
