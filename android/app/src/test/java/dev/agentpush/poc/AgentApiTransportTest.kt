package dev.agentpush.poc

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.nio.file.Files

class AgentApiTransportTest {
    @After
    fun resetTransport() {
        AgentApi.bindTransport(null)
    }

    @Test
    fun probeUsesExplicitSettingsInsteadOfActiveRouter() = runBlocking {
        val server = MockWebServer()
        server.enqueue(jsonResponse("[]"))
        server.enqueue(jsonResponse("""{"sessions":[]}"""))
        server.enqueue(jsonResponse("""{"projects":[]}"""))
        server.start()
        try {
            val oldSettings = AppSettings("http://127.0.0.1:1", "demo-old-token")
            val newSettings = AppSettings(server.url("/").toString().trimEnd('/'), "demo-new-token")
            val lan = CountingTransport(TransportKind.LAN)
            val cloud = CountingTransport(TransportKind.CLOUD)
            AgentApi.bindTransport(
                TransportRouter(
                    lan = lan,
                    cloud = cloud,
                    probeLan = { true },
                    cloudAvailable = { true },
                ),
                activeSettings = { oldSettings },
            )

            val message = AgentApi.probe(newSettings)

            assertTrue(message.contains("0 个会话"))
            assertEquals(0, lan.requestCount)
            assertEquals(0, cloud.requestCount)
            assertEquals("/api/events?since=0", server.takeRequest().path)
            assertEquals(
                "Bearer demo-new-token",
                server.takeRequest().getHeader("Authorization"),
            )
            assertEquals("/api/projects", server.takeRequest().path)
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun cloudUploadRejectsOversizedFileBeforeReadingBytes() = runBlocking {
        val file = Files.createTempFile("homing-upload", ".bin").toFile()
        try {
            file.writeBytes(ByteArray(20 * 1024 * 1024 + 1))
            val settings = AppSettings("http://127.0.0.1:1", "demo-token")
            val lan = CountingTransport(TransportKind.LAN)
            val cloud = CountingTransport(TransportKind.CLOUD)
            AgentApi.bindTransport(
                TransportRouter(
                    lan = lan,
                    cloud = cloud,
                    probeLan = { false },
                    cloudAvailable = { true },
                ),
                activeSettings = { settings },
            )

            val error = runCatching {
                AgentApi.upload(
                    settings = settings,
                    eventId = 1L,
                    file = file,
                    name = "demo.bin",
                    mime = "application/octet-stream",
                )
            }.exceptionOrNull()

            assertTrue(error is CloudRpcException)
            assertEquals(0, cloud.uploadCount)
        } finally {
            file.delete()
        }
    }

    @Test
    fun sessionItemsRequestsReasoningOnlyAfterOptIn() = runBlocking {
        val server = MockWebServer()
        server.enqueue(jsonResponse("""{"items":[{"id":"demo-1","type":"message","role":"assistant","text":"最终结果"}]}"""))
        server.enqueue(jsonResponse("""{"items":[{"id":"demo-2","type":"reasoning","role":"assistant","text":"思考过程"}]}"""))
        server.start()
        try {
            val settings = AppSettings(server.url("/").toString().trimEnd('/'), "demo-token")

            AgentApi.fetchSessionItems(settings, "demo-session-1")
            val normal = server.takeRequest()
            assertEquals("/api/sessions/demo-session-1/items?limit=300", normal.path)
            assertFalse(normal.path.orEmpty().contains("include_reasoning"))

            AgentApi.fetchSessionItems(
                settings,
                "demo-session-1",
                includeReasoning = true,
            )
            val optedIn = server.takeRequest()
            assertTrue(optedIn.path.orEmpty().contains("include_reasoning=1"))
        } finally {
            server.shutdown()
        }
    }

    private fun jsonResponse(body: String): MockResponse =
        MockResponse()
            .setResponseCode(200)
            .setHeader("Content-Type", "application/json")
            .setBody(body)

    private class CountingTransport(
        override val kind: TransportKind,
    ) : HomingTransport {
        var requestCount = 0
        var uploadCount = 0

        override suspend fun request(
            method: String,
            path: String,
            body: JSONObject?,
        ): TransportResponse {
            requestCount += 1
            return TransportResponse(200, JSONObject(), "{}")
        }

        override fun events(sessionId: String?): Flow<TransportEvent> = emptyFlow()

        override suspend fun upload(
            path: String,
            bytes: ByteArray,
            name: String,
        ): JSONObject {
            uploadCount += 1
            return JSONObject().put("ok", true)
        }

        override suspend fun download(url: String): ByteArray = ByteArray(0)
    }
}
