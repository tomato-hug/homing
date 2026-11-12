package dev.agentpush.poc

import dev.agentpush.poc.protocol.PairingInvite
import org.junit.Assert.assertEquals
import org.junit.Test
import java.net.URLEncoder

class PairingLandingLinkTest {
    private val inner = PairingInvite(
        server = "ws://192.168.1.3:18787/v1/ws",
        token = "tok-123",
        hostId = "host-1",
        fingerprint = "ab:cd",
    ).toUri()

    @Test
    fun consoleQrLandingLinkUnwrapsToPairingInvite() {
        val landing = "http://192.168.1.3:18787/get#p=" +
            URLEncoder.encode(inner, "UTF-8") + "&c=123456"
        val invite = PairingInvite.parse(landing)
        assertEquals("ws://192.168.1.3:18787/v1/ws", invite.server)
        assertEquals("tok-123", invite.token)
        assertEquals("host-1", invite.hostId)
    }

    @Test
    fun plainHomingLinkStillParses() {
        assertEquals("tok-123", PairingInvite.parse(inner).token)
    }

    @Test(expected = IllegalArgumentException::class)
    fun landingLinkWithoutInviteIsRejected() {
        PairingInvite.parse("http://192.168.1.3:18787/get")
    }
}
