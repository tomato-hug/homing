package dev.agentpush.poc

import android.os.Looper
import com.goterl.lazysodium.LazySodiumJava
import com.goterl.lazysodium.SodiumJava
import dev.agentpush.poc.protocol.DeviceIdentity
import dev.agentpush.poc.protocol.HomingCrypto
import dev.agentpush.poc.protocol.PairedComputer
import dev.agentpush.poc.protocol.ProtocolPairingClient
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows
import org.robolectric.annotation.Config
import java.util.Base64
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class ProtocolPairingClientTest {
    private val crypto = HomingCrypto(LazySodiumJava(SodiumJava()))
    private val host = crypto.keypairFromSeed("homing-test-host-seed-0000000001")
    private val clientKeys = crypto.keypairFromSeed("homing-test-client-seed-00000001")
    private val hostId = crypto.deviceId(host.publicKey)
    private val hostFingerprint = crypto.fingerprint(host.publicKey)
    private val clientIdentity = DeviceIdentity(
        publicKey = clientKeys.publicKey,
        secretKey = clientKeys.secretKey,
        deviceId = crypto.deviceId(clientKeys.publicKey),
        fingerprint = crypto.fingerprint(clientKeys.publicKey),
    )
    private val words = javaClass.getResourceAsStream("/bip39-chinese-simplified.txt")!!
        .bufferedReader()
        .useLines { lines -> lines.filter { it.isNotBlank() }.toList() }

    @Test
    fun helloPairingCodeSasAndAcceptFlowCompletes() {
        val server = MockWebServer()
        val waiting = CountDownLatch(1)
        val accepted = CountDownLatch(1)
        val error = AtomicReference<String>()
        val sasResult = AtomicReference<List<String>>()
        val acceptedComputer = AtomicReference<PairedComputer>()
        val serverError = AtomicReference<Throwable>()
        val serverSocket = AtomicReference<WebSocket>()
        val hello = AtomicReference<JSONObject>()
        val nonce = crypto.toBase64(ByteArray(32) { it.toByte() })

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
                                hello.set(message)
                                val signature = Base64.getDecoder()
                                    .decode(message.getString("sig"))
                                assertTrue(
                                    crypto.verifyHelloSignature(
                                        publicKey = clientIdentity.publicKey,
                                        nonceBase64 = nonce,
                                        ts = message.getString("ts"),
                                        signature = signature,
                                    ),
                                )
                                assertEquals(
                                    clientIdentity.deviceId,
                                    message.getString("device_id"),
                                )
                                webSocket.send("""{"t":"hello.ok"}""")
                            }
                            "pair.join" -> {
                                assertEquals("123456", message.getString("code6"))
                                webSocket.send(JSONObject()
                                    .put("t", "pair.waiting")
                                    .put("host_id", hostId)
                                    .put("host_pub", crypto.toBase64(host.publicKey))
                                    .put("host_fingerprint", hostFingerprint)
                                    .toString())
                            }
                            "ping" -> webSocket.send("""{"t":"pong"}""")
                        }
                    } catch (throwable: Throwable) {
                        serverError.set(throwable)
                    }
                }
            },
        ))
        server.start()

        val pairing = ProtocolPairingClient(
            identity = clientIdentity,
            words = words,
            crypto = crypto,
        )
        try {
            pairing.start(
                server = server.url("/v1/ws").toString(),
                token = null,
                code6 = "123456",
                expectedHostId = hostId,
                expectedFingerprint = hostFingerprint,
                listener = object : ProtocolPairingClient.Listener {
                    override fun onConnecting() = Unit

                    override fun onWaiting(
                        hostId: String,
                        sas: List<String>,
                        fingerprint: String,
                    ) {
                        assertEquals(this@ProtocolPairingClientTest.hostId, hostId)
                        assertEquals(hostFingerprint, fingerprint)
                        sasResult.set(sas)
                        waiting.countDown()
                    }

                    override fun onAccepted(computer: PairedComputer) {
                        acceptedComputer.set(computer)
                        accepted.countDown()
                    }

                    override fun onError(message: String) {
                        error.set(message)
                    }
                },
            )
            awaitPosted(waiting, "pair.waiting")
            assertTrue(error.get().isNullOrBlank())
            assertNotNull(hello.get())
            assertEquals(
                listOf("掌", "践", "橡", "议"),
                sasResult.get(),
            )

            serverSocket.get().send(JSONObject()
                .put("t", "pair.accepted")
                .put("host_id", hostId)
                .put("host_pub", crypto.toBase64(host.publicKey))
                .put(
                    "lan_urls",
                    JSONArray()
                        .put("http://192.168.1.20:18787")
                        .put("ws://10.0.0.8:18787/v1/ws"),
                )
                .toString())
            awaitPosted(accepted, "pair.accepted")
            assertTrue(error.get().isNullOrBlank())
            assertNotNull(acceptedComputer.get())
            assertEquals(hostId, acceptedComputer.get().deviceId)
            assertEquals(hostFingerprint, acceptedComputer.get().fingerprint)
            assertEquals(
                crypto.toBase64(host.publicKey),
                acceptedComputer.get().publicKeyBase64,
            )
            assertEquals(
                listOf(
                    "ws://192.168.1.20:18787/v1/ws",
                    "ws://10.0.0.8:18787/v1/ws",
                ),
                acceptedComputer.get().lanServers,
            )
            serverError.get()?.let { throw AssertionError("fake server failed", it) }
        } finally {
            pairing.close()
            shutdown(server, serverSocket)
        }
    }

    @Test
    fun mismatchedHostFingerprintAbortsBeforeSas() {
        val server = MockWebServer()
        val failed = CountDownLatch(1)
        val error = AtomicReference<String>()
        val waitingCalled = AtomicReference(false)
        val serverSocket = AtomicReference<WebSocket>()

        server.enqueue(MockResponse().withWebSocketUpgrade(
            object : WebSocketListener() {
                override fun onOpen(webSocket: WebSocket, response: Response) {
                    serverSocket.set(webSocket)
                    webSocket.send(JSONObject()
                        .put("t", "challenge")
                        .put("nonce", crypto.toBase64(ByteArray(32) { 7 }))
                        .toString())
                }

                override fun onMessage(webSocket: WebSocket, text: String) {
                    when (JSONObject(text).optString("t")) {
                        "hello" -> webSocket.send("""{"t":"hello.ok"}""")
                        "pair.join" -> webSocket.send(JSONObject()
                            .put("t", "pair.waiting")
                            .put("host_id", hostId)
                            .put("host_pub", crypto.toBase64(host.publicKey))
                            .put("host_fingerprint", "0000000000000000")
                            .toString())
                    }
                }
            },
        ))
        server.start()

        val pairing = ProtocolPairingClient(
            identity = clientIdentity,
            words = words,
            crypto = crypto,
        )
        try {
            pairing.start(
                server = server.url("/v1/ws").toString(),
                token = "demo-pair-token",
                code6 = null,
                expectedHostId = hostId,
                expectedFingerprint = "8899aabbccddeeff",
                listener = object : ProtocolPairingClient.Listener {
                    override fun onConnecting() = Unit

                    override fun onWaiting(
                        hostId: String,
                        sas: List<String>,
                        fingerprint: String,
                    ) {
                        waitingCalled.set(true)
                    }

                    override fun onAccepted(computer: PairedComputer) = Unit

                    override fun onError(message: String) {
                        error.set(message)
                        failed.countDown()
                    }
                },
            )
            awaitPosted(failed, "fingerprint rejection")
            assertTrue(error.get()?.contains("指纹不一致") == true)
            assertEquals(false, waitingCalled.get())
        } finally {
            pairing.close()
            shutdown(server, serverSocket)
        }
    }

    private fun awaitPosted(latch: CountDownLatch, description: String) {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(5)
        while (System.nanoTime() < deadline) {
            if (latch.await(20, TimeUnit.MILLISECONDS)) return
            Shadows.shadowOf(Looper.getMainLooper()).idle()
        }
        throw AssertionError("timed out waiting for $description")
    }

    private fun shutdown(server: MockWebServer, socket: AtomicReference<WebSocket>) {
        socket.get()?.close(1000, "test complete")
        Thread.sleep(150)
        server.shutdown()
    }
}
