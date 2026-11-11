package dev.agentpush.poc

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentMentionParserTest {
    private val agents = setOf("codex", "claude", "workbuddy")

    @Test
    fun `splits parent text and multiple agent tasks`() {
        val plan = parseAgentMentions(
            "先修正按钮\n@claude 检查登录流程\n@codex 跑完整测试",
            agents,
        )
        assertEquals("先修正按钮", plan.message)
        assertEquals(listOf("claude", "codex"), plan.mentions.map { it.agentId })
        assertEquals(
            listOf("检查登录流程", "跑完整测试"),
            plan.mentions.map { it.text },
        )
    }

    @Test
    fun `dispatch only message leaves the parent reply empty`() {
        val plan = parseAgentMentions("@Claude 检查登录流程", agents)
        assertEquals("", plan.message)
        assertEquals("claude", plan.mentions.single().agentId)
        assertEquals("检查登录流程", plan.mentions.single().text)
    }

    @Test
    fun `handles windows line endings without shifting mention tasks`() {
        val plan = parseAgentMentions(
            "先修正按钮\r\n@claude 检查登录流程\r\n@codex 跑测试",
            agents,
        )
        assertEquals("先修正按钮", plan.message)
        assertEquals(listOf("检查登录流程", "跑测试"), plan.mentions.map { it.text })
    }

    @Test
    fun `keeps emails untouched and marks unknown agents unavailable`() {
        val input = "寄到 dev@claude.example，并让 @kimi 看一下"
        val plan = parseAgentMentions(input, agents)
        assertEquals("寄到 dev@claude.example，并让", plan.message)
        assertEquals("kimi", plan.mentions.single().agentId)
        assertFalse(plan.mentions.single().dispatchable)
    }

    @Test
    fun `ignores mentions inside fenced and inline code`() {
        val input = """
            修改下面的示例：
            `@claude 不应执行`
            ```
            @codex 也不应执行
            ```
        """.trimIndent()
        val plan = parseAgentMentions(input, agents)
        assertFalse(plan.hasMentions)
        assertEquals(input, plan.message)
    }

    @Test
    fun `reports a mention without a task as incomplete`() {
        val plan = parseAgentMentions("普通回复\n@claude", agents)
        assertEquals("普通回复", plan.message)
        assertTrue(plan.hasMentions)
        assertFalse(plan.mentionsHaveTasks)
    }

    @Test
    fun `parses presets and defaults to ask`() {
        val plan = parseAgentMentions(
            "先看这个\n@claude:review 审查改动\n@codex:do 修好测试\n@workbuddy 总结一下",
            agents,
        )
        assertEquals("先看这个", plan.message)
        assertEquals(listOf("review", "do", "ask"), plan.mentions.map { it.preset })
        assertEquals(listOf("审查改动", "修好测试", "总结一下"), plan.mentions.map { it.text })
    }

    @Test
    fun `rewrites only the selected mention preset`() {
        val input = "前言\n@claude:review 检查登录\n@codex:do 修复测试\n结尾"
        val plan = parseAgentMentions(input, agents)

        val changedFirst = changeAgentMentionPreset(
            input,
            plan.mentions[0],
            "ask",
        )
        assertEquals(
            "前言\n@claude 检查登录\n@codex:do 修复测试\n结尾",
            changedFirst,
        )

        val changedSecond = changeAgentMentionPreset(
            input,
            plan.mentions[1],
            "handoff",
        )
        assertEquals(
            "前言\n@claude:review 检查登录\n@codex:handoff 修复测试\n结尾",
            changedSecond,
        )
    }

    @Test
    fun `same agent mentions keep independent option identities`() {
        val plan = parseAgentMentions(
            "@claude:review 检查登录\n@claude:do 修复测试\n@codex 跑构建",
            agents,
        )
        assertEquals(
            listOf("claude#1", "claude#2", "codex#1"),
            plan.mentions.indices.map { agentMentionOptionKey(plan.mentions, it) },
        )
        assertEquals("", agentMentionOptionKey(plan.mentions, 99))
    }
}
