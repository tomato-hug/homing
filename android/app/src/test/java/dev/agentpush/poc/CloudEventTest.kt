package dev.agentpush.poc

import com.goterl.lazysodium.LazySodiumJava
import com.goterl.lazysodium.SodiumJava
import dev.agentpush.poc.protocol.DeviceIdentity
import dev.agentpush.poc.protocol.HomingCrypto
import dev.agentpush.poc.protocol.HomingEnvelope
import dev.agentpush.poc.protocol.PairedComputer
import kotlinx.coroutines.runBlocking
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
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class CloudEventTest {
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
    fun dispatchesEncryptedNamedEventAndAcknowledges(): Unit = runBlocking {
        val server = MockWebServer()
        val serverSocket = AtomicReference<WebSocket>()
        val connected = CountDownLatch(1)
        val eventReceived = CountDownLatch(1)
        val acknowledged = CountDownLatch(1)
        val eventName = AtomicReference<String>()
        val eventSession = AtomicReference<String>()
        val envelopeId = "demo-event-envelope-1"
        val nonce = crypto.toBase64(ByteArray(32) { (it + 11).toByte() })

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
                    val message = JSONObject(text)
                    when (message.optString("t")) {
                        "hello" -> {
                            webSocket.send("""{"t":"hello.ok"}""")
                            val envelope = crypto.createEnvelope(
                                from = hostId,
                                to = clientIdentity.deviceId,
                                payloadJson = JSONObject()
                                    .put("type", "event")
                                    .put("name", "item")
                                    .put(
                                        "data",
                                        JSONObject()
                                            .put("session_id", "demo-session")
                                            .put("item", JSONObject().put("text", "示例条目")),
                                    )
                                    .toString(),
                                sender = hostKeys,
                                recipientPublicKey = clientKeys.publicKey,
                                id = envelopeId,
                            )
                            webSocket.send(envelope.toRelayJson().toString())
                        }
                        "ack" -> {
                            if (message.optString("id") == envelopeId) {
                                acknowledged.countDown()
                            }
                        }
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
                ): Boolean = true

                override fun onEvent(
                    computer: PairedComputer,
                    name: String,
                    data: JSONObject,
                ): Boolean {
                    eventName.set(name)
                    eventSession.set(data.optString("session_id"))
                    eventReceived.countDown()
                    return true
                }

                override fun onStateChanged(
                    server: String,
                    state: CloudNotificationClient.State,
                    detail: String?,
                ) {
                    if (state == CloudNotificationClient.State.CONNECTED) {
                        connected.countDown()
                    }
                }
            },
            crypto = crypto,
        )
        try {
            client.start()
            assertTrue(connected.await(5, TimeUnit.SECONDS))
            assertTrue(eventReceived.await(5, TimeUnit.SECONDS))
            assertTrue(acknowledged.await(5, TimeUnit.SECONDS))
            assertEquals("item", eventName.get())
            assertEquals("demo-session", eventSession.get())
        } finally {
            client.stop()
            serverSocket.get()?.close(1000, "test complete")
            server.shutdown()
        }
    }

    private fun HomingEnvelope.toRelayJson(): JSONObject = JSONObject()
        .put("v", v)
        .put("t", t)
        .put("id", id)
        .put("from", from)
        .put("to", to)
        .put("ts", ts)
        .put("nonce", nonce)
        .put("ct", ct)

}
