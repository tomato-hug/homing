package dev.agentpush.poc

import java.io.BufferedReader
import java.io.EOFException
import java.io.IOException
import java.io.InputStreamReader
import java.net.HttpURLConnection

class SessionLiveStream(
    private val sessionId: String,
    private val settingsProvider: () -> AppSettings,
    private val onFrame: (eventName: String, data: String) -> Unit,
    private val onConnection: (connected: Boolean, reason: String?) -> Unit = { _, _ -> },
) {
    @Volatile
    private var running = false

    @Volatile
    private var connection: HttpURLConnection? = null

    private var worker: Thread? = null

    fun start() {
        if (running) return
        running = true
        worker = Thread({ loop() }, "agent-session-live").apply {
            isDaemon = true
            start()
        }
    }

    fun stop() {
        running = false
        connection?.disconnect()
        connection = null
        worker?.interrupt()
        worker = null
    }

    private fun loop() {
        var delayMillis = 1_000L
        while (running) {
            try {
                read()
                if (running) {
                    throw EOFException("中继关闭了会话 SSE")
                }
            } catch (error: Exception) {
                if (!running) break
                onConnection(false, disconnectReason(error))
            } finally {
                connection?.disconnect()
                connection = null
            }
            if (!running) break
            try {
                Thread.sleep(delayMillis)
            } catch (_: InterruptedException) {
                if (!running) break
            }
            delayMillis = (delayMillis * 2).coerceAtMost(15_000L)
        }
    }

    private fun read() {
        val current = AgentApi.openStreamConnection(settingsProvider(), sessionId)
        connection = current
        val code = current.responseCode
        if (code !in 200..299) {
            val detail = current.errorStream
                ?.bufferedReader(Charsets.UTF_8)
                ?.use { it.readText() }
                .orEmpty()
            throw AgentApiException(code, "HTTP $code ${detail.take(140)}".trim())
        }
        onConnection(true, null)
        val reader = BufferedReader(
            InputStreamReader(current.inputStream, Charsets.UTF_8),
        )
        val dataLines = mutableListOf<String>()
        var eventName = ""

        fun dispatch() {
            if (dataLines.isEmpty()) {
                eventName = ""
                return
            }
            val data = dataLines.joinToString("\n")
            val name = eventName
            dataLines.clear()
            eventName = ""
            onFrame(name, data)
        }

        reader.use {
            while (running) {
                val line = it.readLine() ?: break
                when {
                    line.isEmpty() -> dispatch()
                    line.startsWith(":") -> Unit
                    line.startsWith("event:") ->
                        eventName = line.substringAfter("event:").trim()
                    line.startsWith("data:") ->
                        dataLines.add(line.substringAfter("data:").removePrefix(" "))
                }
            }
            dispatch()
        }
    }

    private fun disconnectReason(error: Throwable): String {
        val type = when (error) {
            is AgentApiException -> "AgentApiException"
            is EOFException -> "EOFException"
            is IOException -> "IOException"
            else -> error.javaClass.simpleName
        }
        val message = error.message
            ?.replace(Regex("\\s+"), " ")
            ?.trim()
            ?.take(100)
        return if (message.isNullOrBlank()) type else "$type: $message"
    }
}
