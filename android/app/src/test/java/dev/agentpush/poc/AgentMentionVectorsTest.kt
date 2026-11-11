package dev.agentpush.poc

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Test
import java.io.File

class AgentMentionVectorsTest {
    @Test
    fun `shared mention vectors match the Kotlin parser`() {
        val file = generateSequence(File(System.getProperty("user.dir"))) { it.parentFile }
            .map { File(it, "internal/orchestrator/testdata/mentions.json") }
            .firstOrNull(File::isFile)
            ?: error("找不到 internal/orchestrator/testdata/mentions.json")
        val root = JSONObject(file.readText())
        val dispatchable = root.getJSONArray("dispatchable_agents")
            .let { array -> (0 until array.length()).map { array.getString(it) }.toSet() }
        val cases = root.getJSONArray("cases")

        for (index in 0 until cases.length()) {
            val vector = cases.getJSONObject(index)
            val name = vector.getString("name")
            val plan = parseAgentMentions(vector.getString("input"), dispatchable)
            val expected = vector.getJSONArray("mentions")
            assertEquals("$name: parent message", vector.getString("message"), plan.message)
            assertEquals("$name: mention count", expected.length(), plan.mentions.size)
            for (mentionIndex in 0 until expected.length()) {
                val want = expected.getJSONObject(mentionIndex)
                val got = plan.mentions[mentionIndex]
                assertEquals("$name: agent", want.getString("agent"), got.agentId)
                assertEquals("$name: preset", want.getString("preset"), got.preset)
                assertEquals("$name: text", want.getString("text"), got.text)
                assertEquals(
                    "$name: dispatchable",
                    want.getBoolean("dispatchable"),
                    got.dispatchable,
                )
            }
        }
    }
}
