package dev.agentpush.poc

import com.goterl.lazysodium.LazySodiumJava
import com.goterl.lazysodium.SodiumJava
import dev.agentpush.poc.protocol.DeviceIdentity
import dev.agentpush.poc.protocol.HomingCrypto
import dev.agentpush.poc.protocol.HomingEnvelope
import dev.agentpush.poc.protocol.PairedComputer
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.async
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.supervisorScope
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.io.ByteArrayOutputStream
import java.util.Base64
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import java.util.concurrent.atomic.AtomicInteger

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class CloudRpcTest {
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
    fun mutationWithLostReceiptIsNotRetriedOnAnotherAddress() = runBlocking {
        verifyAddressFallback("POST", expectFallback = false)
    }

    @Test
    fun readWithLostReceiptCanUseAnotherAddress() = runBlocking {
        verifyAddressFallback("GET", expectFallback = true)
    }

    @Test
    fun cancelledRpcDoesNotContactAnotherAddress() = runBlocking {
        verifyAddressFallback("GET", expectFallback = false, cancelAfterSend = true)
    }

    private suspend fun verifyAddressFallback(method: String, expectFallback: Boolean, cancelAfterSend: Boolean = false) {
        val connected = CountDownLatch(2)
        val firstReceived = CountDownLatch(1)
        val backupReceived = CountDownLatch(1)
        val calls = listOf(AtomicInteger(), AtomicInteger())
        val serverError = AtomicReference<Throwable>()
        val sockets = java.util.concurrent.ConcurrentLinkedQueue<WebSocket>()
        val servers = List(2) { index ->
            MockWebServer().apply {
                enqueue(MockResponse().withWebSocketUpgrade(object : WebSocketListener() {
                    override fun onOpen(webSocket: WebSocket, response: Response) {
                        sockets.add(webSocket)
                        webSocket.send(JSONObject().put("t", "challenge")
                            .put("nonce", crypto.toBase64(ByteArray(32) { 7 })).toString())
                    }

                    override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                        webSocket.close(code, reason)
                    }

                    override fun onMessage(webSocket: WebSocket, text: String) {
                        try {
                            val message = JSONObject(text)
                            when (message.optString("t")) {
                                "hello" -> webSocket.send("""{"t":"hello.ok"}""")
                                "msg" -> {
                                    val request = JSONObject(crypto.openEnvelope(
                                        message.toEnvelope(), clientIdentity.publicKey, hostKeys.secretKey,
                                    ))
                                    assertEquals(method, request.getString("method"))
                                    assertEquals("/api/demo", request.getString("path"))
                                    calls[index].incrementAndGet()
                                    if (index == 0) {
                                        firstReceived.countDown()
                                        // Simulate an executed request whose acknowledgement is lost.
                                        if (!cancelAfterSend) webSocket.close(1011, "demo acknowledgement lost")
                                    } else {
                                        backupReceived.countDown()
                                        val envelope = crypto.createEnvelope(
                                            from = hostId, to = clientIdentity.deviceId,
                                            payloadJson = JSONObject().put("type", "rpc_result")
                                                .put("rid", request.getString("rid")).put("status", 200)
                                                .put("body", JSONObject().put("ok", true)).toString(),
                                            sender = hostKeys, recipientPublicKey = clientKeys.publicKey,
                                        )
                                        webSocket.send(envelope.toRelayJson().toString())
                                    }
                                }
                            }
                        } catch (error: Throwable) {
                            serverError.set(error)
                        }
                    }
                }))
                start()
            }
        }
        val computer = pairedComputer(servers[0]).copy(
            lanServers = listOf(servers[1].url("/v1/ws").toString()),
        )
        val client = CloudNotificationClient(
            identity = clientIdentity, computersProvider = { listOf(computer) }, crypto = crypto,
            listener = object : CloudNotificationClient.Listener {
                override fun onNotification(computer: PairedComputer, payload: NotifyPayload) = true
                override fun onStateChanged(server: String, state: CloudNotificationClient.State, detail: String?) {
                    if (state == CloudNotificationClient.State.CONNECTED) connected.countDown()
                }
            },
        )
        try {
            client.start()
            assertTrue("both demo addresses must connect", connected.await(3, TimeUnit.SECONDS))
            val result = supervisorScope {
                val pending = async(start = CoroutineStart.UNDISPATCHED) {
                    client.requestRPC(computer, method, "/api/demo", timeoutMillis = 3_000)
                }
                if (cancelAfterSend) {
                    assertTrue(firstReceived.await(3, TimeUnit.SECONDS))
                    pending.cancelAndJoin()
                    assertTrue(pending.isCancelled)
                    assertFalse("cancelled RPC must not reach another address", backupReceived.await(200, TimeUnit.MILLISECONDS))
                    Result.failure<CloudRpcResult>(CancellationException("demo cancellation"))
                } else {
                    runCatching { pending.await() }
                }
            }
            serverError.get()?.let { throw AssertionError("demo websocket failed", it) }
            if (expectFallback) assertEquals(200, result.getOrThrow().status)
            else assertTrue("uncertain mutation must be reported", result.isFailure)
            assertEquals(1, calls[0].get())
            assertEquals(if (expectFallback) 1 else 0, calls[1].get())
        } finally {
            client.stop()
            sockets.forEach { it.close(1000, "demo done") }
            servers.forEach { it.shutdown() }
        }
    }

    @Test
    fun encryptedAgentsRpcRoundTrip(): Unit = runBlocking {
        val server = MockWebServer()
        val serverSocket = AtomicReference<WebSocket>()
        val connected = CountDownLatch(1)
        val requestReceived = CountDownLatch(1)
        val acknowledged = CountDownLatch(1)
        val serverError = AtomicReference<Throwable>()
        val resultEnvelopeId = "demo-rpc-result-1"
        val nonce = crypto.toBase64(ByteArray(32) { (it + 5).toByte() })

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
                            "hello" -> webSocket.send("""{"t":"hello.ok"}""")
                            "ack" -> {
                                if (message.optString("id") == resultEnvelopeId) {
                                    acknowledged.countDown()
                                }
                            }
                            "msg" -> {
                                assertFalse(
                                    "encrypted relay frame must not expose API paths",
                                    text.contains("/api/agents"),
                                )
                                val envelope = message.toEnvelope()
                                assertEquals(hostId, envelope.to)
                                assertEquals(clientIdentity.deviceId, envelope.from)
                                val payload = JSONObject(
                                    crypto.openEnvelope(
                                        envelope = envelope,
                                        senderPublicKey = clientIdentity.publicKey,
                                        recipientSecretKey = hostKeys.secretKey,
                                    ),
                                )
                                assertEquals("rpc", payload.getString("type"))
                                assertEquals("GET", payload.getString("method"))
                                assertEquals("/api/agents", payload.getString("path"))
                                assertTrue(payload.getString("rid").isNotBlank())
                                requestReceived.countDown()

                                val body = JSONObject()
                                    .put(
                                        "agents",
                                        JSONArray().put(
                                            JSONObject()
                                                .put("source", "codex")
                                                .put("display_name", "Codex")
                                                .put("available", true),
                                        ),
                                    )
                                    .put("generated_at", "2026-11-10T12:00:00.000Z")
                                val result = crypto.createEnvelope(
                                    from = hostId,
                                    to = clientIdentity.deviceId,
                                    payloadJson = JSONObject()
                                        .put("type", "rpc_result")
                                        .put("rid", payload.getString("rid"))
                                        .put("status", 200)
                                        .put("body", body)
                                        .toString(),
                                    sender = hostKeys,
                                    recipientPublicKey = clientKeys.publicKey,
                                    id = resultEnvelopeId,
                                )
                                webSocket.send(result.toRelayJson().toString())
                            }
                        }
                    } catch (error: Throwable) {
                        serverError.set(error)
                    }
                }
            },
        ))
        server.start()

        val computer = pairedComputer(server)
        val client = CloudNotificationClient(
            identity = clientIdentity,
            computersProvider = { listOf(computer) },
            listener = object : CloudNotificationClient.Listener {
                override fun onNotification(
                    computer: PairedComputer,
                    payload: NotifyPayload,
                ): Boolean = true

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

            val result = client.requestRPC(
                computer = computer,
                method = "GET",
                path = "/api/agents",
                timeoutMillis = 5_000,
            )

            assertTrue(requestReceived.await(5, TimeUnit.SECONDS))
            assertTrue(acknowledged.await(5, TimeUnit.SECONDS))
            assertEquals(200, result.status)
            val body = result.body as JSONObject
            val agents = body.getJSONArray("agents")
            assertEquals(1, agents.length())
            assertEquals("codex", agents.getJSONObject(0).getString("source"))
            assertNotNull(result.requestId)
            assertFalse(result.requestId.isBlank())
            serverError.get()?.let { throw AssertionError("fake relay failed", it) }
        } finally {
            client.stop()
            serverSocket.get()?.close(1000, "test complete")
            server.shutdown()
        }
    }

    @Test
    fun usesAdvertisedLanEndpointWhenCloudRelayIsDown(): Unit = runBlocking {
        val lan = MockWebServer()
        val lanSocket = AtomicReference<WebSocket>()
        val lanConnected = CountDownLatch(1)
        val requestReceived = CountDownLatch(1)
        val serverError = AtomicReference<Throwable>()
        val nonce = crypto.toBase64(ByteArray(32) { (it + 13).toByte() })

        lan.enqueue(MockResponse().withWebSocketUpgrade(
            object : WebSocketListener() {
                override fun onOpen(webSocket: WebSocket, response: Response) {
                    lanSocket.set(webSocket)
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
                                webSocket.send("""{"t":"hello.ok"}""")
                                lanConnected.countDown()
                            }
                            "msg" -> {
                                val envelope = message.toEnvelope()
                                val payload = JSONObject(
                                    crypto.openEnvelope(
                                        envelope = envelope,
                                        senderPublicKey = clientIdentity.publicKey,
                                        recipientSecretKey = hostKeys.secretKey,
                                    ),
                                )
                                requestReceived.countDown()
                                val result = crypto.createEnvelope(
                                    from = hostId,
                                    to = clientIdentity.deviceId,
                                    payloadJson = JSONObject()
                                        .put("type", "rpc_result")
                                        .put("rid", payload.getString("rid"))
                                        .put("status", 200)
                                        .put("body", JSONObject().put("transport", "lan"))
                                        .toString(),
                                    sender = hostKeys,
                                    recipientPublicKey = clientKeys.publicKey,
                                )
                                webSocket.send(result.toRelayJson().toString())
                            }
                        }
                    } catch (error: Throwable) {
                        serverError.set(error)
                    }
                }
            },
        ))
        lan.start()

        val computer = PairedComputer(
            deviceId = hostId,
            name = "演示 Mac",
            server = "ws://127.0.0.1:1/v1/ws",
            publicKeyBase64 = crypto.toBase64(hostKeys.publicKey),
            fingerprint = crypto.fingerprint(hostKeys.publicKey),
            pairedAt = 1L,
            lastSeen = 1L,
            lanServers = listOf(lan.url("/v1/ws").toString()),
        )
        val client = CloudNotificationClient(
            identity = clientIdentity,
            computersProvider = { listOf(computer) },
            listener = object : CloudNotificationClient.Listener {
                override fun onNotification(
                    computer: PairedComputer,
                    payload: NotifyPayload,
                ): Boolean = true
            },
            crypto = crypto,
        )
        try {
            client.start()
            assertTrue(lanConnected.await(5, TimeUnit.SECONDS))
            val result = client.requestRPC(
                computer = computer,
                method = "GET",
                path = "/api/home",
                timeoutMillis = 5_000,
            )

            assertTrue(requestReceived.await(5, TimeUnit.SECONDS))
            assertEquals(200, result.status)
            assertEquals("lan", (result.body as JSONObject).getString("transport"))
            serverError.get()?.let { throw AssertionError("fake LAN host failed", it) }
        } finally {
            client.stop()
            lanSocket.get()?.close(1000, "test complete")
            lan.shutdown()
        }
    }

    @Test
    fun requestTimesOutWithoutMatchingResult(): Unit = runBlocking {
        val server = MockWebServer()
        val serverSocket = AtomicReference<WebSocket>()
        val connected = CountDownLatch(1)
        val requestReceived = CountDownLatch(1)
        val nonce = crypto.toBase64(ByteArray(32) { (it + 7).toByte() })

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
                        "hello" -> webSocket.send("""{"t":"hello.ok"}""")
                        "msg" -> requestReceived.countDown()
                    }
                }
            },
        ))
        server.start()

        val computer = pairedComputer(server)
        val client = CloudNotificationClient(
            identity = clientIdentity,
            computersProvider = { listOf(computer) },
            listener = object : CloudNotificationClient.Listener {
                override fun onNotification(
                    computer: PairedComputer,
                    payload: NotifyPayload,
                ): Boolean = true

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
            val error = runCatching {
                client.requestRPC(
                    computer = computer,
                    method = "GET",
                    path = "/api/agents",
                    timeoutMillis = 200,
                )
            }.exceptionOrNull()

            assertTrue(error is CloudRpcException)
            assertEquals("云端请求超时", error?.message)
            assertTrue(requestReceived.await(5, TimeUnit.SECONDS))
        } finally {
            client.stop()
            serverSocket.get()?.close(1000, "test complete")
            server.shutdown()
        }
    }

    @Test
    fun rejectsUnsupportedTargetsBeforeConnecting(): Unit = runBlocking {
        val computer = PairedComputer(
            deviceId = hostId,
            name = "演示 Mac",
            server = "ws://127.0.0.1:1/v1/ws",
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
            },
            crypto = crypto,
        )
        try {
            val pathError = runCatching {
                client.requestRPC(computer, "GET", "/console/agents")
            }.exceptionOrNull()
            assertTrue(pathError is IllegalArgumentException)

            val methodError = runCatching {
                client.requestRPC(computer, "TRACE", "/api/agents")
            }.exceptionOrNull()
            assertTrue(methodError is CloudRpcException)
            assertEquals("云端请求方法不受支持", methodError?.message)
        } finally {
            client.stop()
        }
    }

    @Test
    fun chunkedFileUploadAndDownloadRoundTrip(): Unit = runBlocking {
        val server = MockWebServer()
        val serverSocket = AtomicReference<WebSocket>()
        val connected = CountDownLatch(1)
        val uploadChunks = CountDownLatch(3)
        val downloadRequested = CountDownLatch(1)
        val serverError = AtomicReference<Throwable>()
        val uploaded = ByteArrayOutputStream()
        val uploadBytes = ByteArray(110_000) { (it % 251).toByte() }
        val downloadBytes = ByteArray(90_000) { (255 - it % 241).toByte() }
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
                    try {
                        val message = JSONObject(text)
                        when (message.optString("t")) {
                            "hello" -> webSocket.send("""{"t":"hello.ok"}""")
                            "msg" -> {
                                val envelope = message.toEnvelope()
                                val payload = JSONObject(
                                    crypto.openEnvelope(
                                        envelope = envelope,
                                        senderPublicKey = clientIdentity.publicKey,
                                        recipientSecretKey = hostKeys.secretKey,
                                    ),
                                )
                                when (payload.getString("type")) {
                                    "file.chunk" -> {
                                        uploaded.write(
                                            Base64.getDecoder().decode(
                                                payload.getString("data"),
                                            ),
                                        )
                                        uploadChunks.countDown()
                                        if (payload.getBoolean("last")) {
                                            val result = crypto.createEnvelope(
                                                from = hostId,
                                                to = clientIdentity.deviceId,
                                                payloadJson = JSONObject()
                                                    .put("type", "file.result")
                                                    .put("fid", payload.getString("fid"))
                                                    .put("status", 200)
                                                    .put(
                                                        "body",
                                                        JSONObject()
                                                            .put("ok", true)
                                                            .put("size", uploaded.size()),
                                                    )
                                                    .toString(),
                                                sender = hostKeys,
                                                recipientPublicKey = clientKeys.publicKey,
                                            )
                                            webSocket.send(result.toRelayJson().toString())
                                        }
                                    }
                                    "file.request" -> {
                                        downloadRequested.countDown()
                                        val fid = payload.getString("fid")
                                        listOf(
                                            downloadBytes.copyOfRange(0, 50_000),
                                            downloadBytes.copyOfRange(50_000, downloadBytes.size),
                                        ).forEachIndexed { index, bytes ->
                                            val chunk = crypto.createEnvelope(
                                                from = hostId,
                                                to = clientIdentity.deviceId,
                                                payloadJson = JSONObject()
                                                    .put("type", "file.chunk")
                                                    .put("fid", fid)
                                                    .put("seq", index)
                                                    .put("last", index == 1)
                                                    .put(
                                                        "data",
                                                        Base64.getEncoder().encodeToString(bytes),
                                                    )
                                                    .toString(),
                                                sender = hostKeys,
                                                recipientPublicKey = clientKeys.publicKey,
                                            )
                                            webSocket.send(chunk.toRelayJson().toString())
                                        }
                                    }
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

        val computer = pairedComputer(server)
        val client = CloudNotificationClient(
            identity = clientIdentity,
            computersProvider = { listOf(computer) },
            listener = object : CloudNotificationClient.Listener {
                override fun onNotification(
                    computer: PairedComputer,
                    payload: NotifyPayload,
                ): Boolean = true

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

            val upload = client.uploadFile(
                computer = computer,
                path = "/api/upload?event_id=1&name=demo.bin",
                bytes = uploadBytes,
                contentType = "application/octet-stream",
                timeoutMillis = 5_000,
            )

            assertTrue(uploadChunks.await(5, TimeUnit.SECONDS))
            assertEquals(200, upload.status)
            assertEquals(uploadBytes.size, uploaded.size())
            assertTrue(uploaded.toByteArray().contentEquals(uploadBytes))

            val downloaded = client.downloadFile(
                computer = computer,
                path = "/f/demo-token/demo.bin",
                timeoutMillis = 5_000,
            )

            assertTrue(downloadRequested.await(5, TimeUnit.SECONDS))
            assertTrue(downloaded.contentEquals(downloadBytes))
            serverError.get()?.let { throw AssertionError("fake relay failed", it) }
        } finally {
            client.stop()
            serverSocket.get()?.close(1000, "test complete")
            server.shutdown()
        }
    }

    private fun pairedComputer(server: MockWebServer): PairedComputer {
        return PairedComputer(
            deviceId = hostId,
            name = "演示 Mac",
            server = server.url("/v1/ws").toString(),
            publicKeyBase64 = crypto.toBase64(hostKeys.publicKey),
            fingerprint = crypto.fingerprint(hostKeys.publicKey),
            pairedAt = 1L,
            lastSeen = 1L,
        )
    }

    private fun JSONObject.toEnvelope(): HomingEnvelope {
        return HomingEnvelope(
            v = getInt("v"),
            t = getString("t"),
            id = getString("id"),
            from = getString("from"),
            to = getString("to"),
            ts = getString("ts"),
            nonce = getString("nonce"),
            ct = getString("ct"),
        )
    }

    private fun HomingEnvelope.toRelayJson(): JSONObject {
        return JSONObject()
            .put("v", v)
            .put("t", t)
            .put("id", id)
            .put("from", from)
            .put("to", to)
            .put("ts", ts)
            .put("nonce", nonce)
            .put("ct", ct)
    }
}
