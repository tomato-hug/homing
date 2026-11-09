package dev.agentpush.poc

import com.goterl.lazysodium.LazySodiumJava
import com.goterl.lazysodium.SodiumJava
import dev.agentpush.poc.protocol.DeviceIdentity
import dev.agentpush.poc.protocol.HomingCrypto
import dev.agentpush.poc.protocol.PairedComputer
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.util.Base64
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class CloudNotificationClientTest {
    private val crypto = HomingCrypto(LazySodiumJava(SodiumJava()))
    private val hostKeys = crypto.keypairFromSeed("homing-test-host-seed-0000000001")
    private val clientKeys = crypto.keypairFromSeed("homing-test-client-seed-00000001")
    private val hostId = crypto.deviceId(hostKeys.publicKey)
    private val clientIdentity = DeviceIdentity(
        publicKey = clientKeys.publicKey,
        secretKey = clientKeys.secretKey,
        deviceId = crypto.deviceId(clientKeys.publicKey),
        fingerprint = crypto.fingerprint(clientKeys.publicKey),
    )

    @Test
    fun decryptsNotifyAndAcknowledgesEnvelope() {
        val server = MockWebServer()
        val serverSocket = AtomicReference<WebSocket>()
        val notifyReceived = CountDownLatch(1)
        val acknowledged = CountDownLatch(1)
        val serverError = AtomicReference<Throwable>()
        val receivedPayload = AtomicReference<NotifyPayload>()
        val nonce = crypto.toBase64(ByteArray(32) { (it + 3).toByte() })
        val eventId = "demo-envelope-1"

        server.enqueue(MockResponse().withWebSocketUpgrade(
            object : WebSocketListener() {
                override fun onOpen(webSocket: WebSocket, response: Response) {
                    serverSocket.set(webSocket)
                    webSocket.send(JSONObject()
                        .put("t", "challenge")
                        .put("nonce", nonce)
                        .toString())
                }

                override fun onMessage(webSocket: WebSocket, text: String) {
                    try {
                        val message = JSONObject(text)
                        when (message.optString("t")) {
                            "hello" -> {
                                val signature = Base64.getDecoder()
                                    .decode(message.getString("sig"))
                                assertTrue(
                                    crypto.verifyHelloSignature(
                                        clientIdentity.publicKey,
                                        nonce,
                                        message.getString("ts"),
                                        signature,
                                    ),
                                )
                                webSocket.send("""{"t":"hello.ok"}""")
                                val envelope = crypto.createEnvelope(
                                    from = hostId,
                                    to = clientIdentity.deviceId,
                                    payloadJson = JSONObject()
                                        .put("type", "event")
                                        .put("name", "notify")
                                        .put(
                                            "data",
                                            JSONObject()
                                                .put("type", "notify")
                                                .put("level", "P0")
                                                .put("source", "codex")
                                                .put("session_id", "demo-session")
                                                .put("event_id", 42)
                                                .put("title", "示例：需要授权")
                                                .put("summary", "示例摘要")
                                                .put("at", "2026-11-09T08:00:00.000Z"),
                                        )
                                        .toString(),
                                    sender = hostKeys,
                                    recipientPublicKey = clientKeys.publicKey,
                                    id = eventId,
                                )
                                webSocket.send(JSONObject()
                                    .put("v", envelope.v)
                                    .put("t", envelope.t)
                                    .put("id", envelope.id)
                                    .put("from", envelope.from)
                                    .put("to", envelope.to)
                                    .put("ts", envelope.ts)
                                    .put("nonce", envelope.nonce)
                                    .put("ct", envelope.ct)
                                    .toString())
                            }
                            "ack" -> {
                                if (message.optString("id") == eventId) {
                                    acknowledged.countDown()
                                }
                            }
                        }
                    } catch (error: Throwable) {
                        serverError.set(error)
                    }
                }
            },
        ))
        server.start()

        val computer = PairedComputer(
            deviceId = hostId,
            name = "演示 Mac",
            server = server.url("/v1/ws").toString(),
            publicKeyBase64 = crypto.toBase64(hostKeys.publicKey),
            fingerprint = crypto.fingerprint(hostKeys.publicKey),
            pairedAt = 1L,
            lastSeen = 1L,
        )
        val client = CloudNotificationClient(
            identity = clientIdentity,
            computersProvider = { listOf(computer) },
            listener = object : CloudNotificationClient.Listener {
                override fun onNotification(
                    computer: PairedComputer,
                    payload: NotifyPayload,
                ): Boolean {
                    assertEquals(hostId, computer.deviceId)
                    receivedPayload.set(payload)
                    notifyReceived.countDown()
                    return true
                }
            },
            crypto = crypto,
        )
        try {
            client.start()
            assertTrue(notifyReceived.await(5, TimeUnit.SECONDS))
            assertTrue(acknowledged.await(5, TimeUnit.SECONDS))
            val payload = receivedPayload.get()
            assertEquals("P0", payload.level)
            assertEquals("demo-session", payload.sessionId)
            assertEquals("示例：需要授权", payload.title)
            serverError.get()?.let { throw AssertionError("fake relay failed", it) }
        } finally {
            client.stop()
            serverSocket.get()?.close(1000, "test complete")
            server.shutdown()
        }
    }
}
