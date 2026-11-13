package dev.agentpush.poc

import kotlinx.coroutines.flow.take
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Test
import org.json.JSONObject
import java.util.concurrent.TimeUnit

class LanTransportTest {
    @Test
    fun publicHealthDoesNotSelectHttpWithoutDeviceToken() = runBlocking {
        val server = MockWebServer()
        server.enqueue(MockResponse().setBody("""{"ok":true}"""))
        server.start()
        try {
            val transport = LanTransport({ AppSettings(server.url("/").toString(), "") })
            assertFalse(transport.probeHealth())
            assertEquals(0, server.requestCount)
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun stalledSseDoesNotBlockCloudSwitch() = runBlocking {
        val server = MockWebServer()
        server.enqueue(MockResponse().setHeader("Content-Type", "text/event-stream")
            .setBody("data: {}\n\n").setBodyDelay(6, TimeUnit.SECONDS))
        server.start()
        val settings = AppSettings(server.url("/").toString().trimEnd('/'), "demo-token")
        val lan = LanTransport({ settings }, OkHttpClient.Builder().readTimeout(6, TimeUnit.SECONDS).build())
        val cloud = object : HomingTransport {
            override val kind = TransportKind.CLOUD
            override fun events(sessionId: String?) = flowOf(TransportEvent("item", JSONObject().put("session_id", "demo-cloud")))
            override suspend fun request(method: String, path: String, body: JSONObject?) = error("unused demo request")
            override suspend fun upload(path: String, bytes: ByteArray, name: String) = error("unused demo upload")
            override suspend fun download(url: String) = error("unused demo download")
        }
        var lanAvailable = true
        val router = TransportRouter(lan, cloud, { lanAvailable }, { true })
        try {
            val received = async(Dispatchers.Default) { withTimeout(5_000) { router.events().first() } }
            assertNotNull(withContext(Dispatchers.IO) { server.takeRequest(2, TimeUnit.SECONDS) })
            lanAvailable = false
            assertEquals("demo-cloud", received.await().data.getString("session_id"))
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun sseReconnectSendsLastEventId() = runBlocking {
        val server = MockWebServer()
        server.enqueue(
            MockResponse()
                .setHeader("Content-Type", "text/event-stream")
                .setBody(
                    "id: e7.i12\n" +
                        "event: item\n" +
                        "data: {\"session_id\":\"demo-session\",\"item\":{\"id\":\"item-1\",\"seq\":12}}\n\n",
                ),
        )
        server.enqueue(
            MockResponse()
                .setHeader("Content-Type", "text/event-stream")
                .setBody(
                    "id: e8.i13\n" +
                        "event: item\n" +
                        "data: {\"session_id\":\"demo-session\",\"item\":{\"id\":\"item-2\",\"seq\":13}}\n\n",
                ),
        )
        server.start()
        try {
            val settings = AppSettings(
                baseUrl = server.url("/").toString().trimEnd('/'),
                token = "demo-token",
            )
            val transport = LanTransport(settingsProvider = { settings })

            val events = transport.events("demo-session").take(2).toList()

            assertEquals(listOf("e7.i12", "e8.i13"), events.map { it.id })
            assertEquals(listOf("item", "item"), events.map { it.name })
            val first = server.takeRequest()
            val second = server.takeRequest()
            assertEquals("/api/stream?session_id=demo-session", first.path)
            assertEquals(null, first.getHeader("Last-Event-ID"))
            assertEquals("e7.i12", second.getHeader("Last-Event-ID"))
        } finally {
            server.shutdown()
        }
    }
}
