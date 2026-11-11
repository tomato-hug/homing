package dev.agentpush.poc

import kotlinx.coroutines.flow.take
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.assertEquals
import org.junit.Test

class LanTransportTest {
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
