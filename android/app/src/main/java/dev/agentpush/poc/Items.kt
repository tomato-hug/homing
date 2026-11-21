package dev.agentpush.poc

import org.json.JSONArray
import org.json.JSONObject
import java.time.Instant

enum class SessionItemType {
    USER,
    ASSISTANT,
    REASONING,
    TOOL_CALL,
    FILE_CHANGE,
    PLAN,
    APPROVAL,
    ERROR,
    SYSTEM,
    OTHER,
}

data class ItemTool(
    val name: String = "",
    val inputSummary: String = "",
    val outputTail: String = "",
    val exitCode: Int? = null,
    val status: String = "",
)

data class ItemFileChange(
    val path: String,
    val added: Int = 0,
    val removed: Int = 0,
    val diff: String = "",
)

data class ItemPlanStep(
    val step: String,
    val status: String,
)

data class SessionItem(
    val id: String,
    val ts: String,
    val type: SessionItemType,
    val role: String = "",
    val partial: Boolean = false,
    val truncated: Boolean = false,
    val text: String = "",
    val tool: ItemTool = ItemTool(),
    val files: List<ItemFileChange> = emptyList(),
    val plan: List<ItemPlanStep> = emptyList(),
    val model: String = "",
    val eventId: Long? = null,
    val rawType: String = "",
    val isToolResult: Boolean = false,
    val itemKey: String = "",
) {
    val isFailed: Boolean
        get() = type == SessionItemType.ERROR ||
            tool.status.equals("error", true) ||
            tool.status.equals("failed", true) ||
            (tool.exitCode != null && tool.exitCode != 0)

    companion object {
        fun fromJson(json: JSONObject, fallbackId: String = ""): SessionItem? {
            val rawType = json.stringOrBlank("type").lowercase()
            val role = json.stringOrBlank("role").lowercase()
            val toolJson = json.optJSONObject("tool")
            val toolString = json.opt("tool")
                ?.takeIf { it is String }
                ?.toString()
                .orEmpty()
            val toolName = when {
                toolJson != null -> toolJson.stringOrBlank("name")
                    .ifBlank { toolJson.stringOrBlank("tool") }
                toolString.isNotBlank() && toolString != "null" -> toolString
                else -> json.stringOrBlank("tool_name")
                    .ifBlank { json.stringOrBlank("name") }
            }
            val text = json.stringOrBlank("text")
                .ifBlank { json.stringOrBlank("content") }
                .ifBlank { json.stringOrBlank("message") }
                .ifBlank {
                    if (rawType in setOf("error", "system")) {
                        json.stringOrBlank("error")
                    } else {
                        ""
                    }
                }
            val type = resolveType(rawType, role, toolName, json)
            val inputSummary = json.stringOrBlank("input_summary")
                .ifBlank { json.stringOrBlank("command") }
                .ifBlank {
                    toolJson?.stringOrBlank("input_summary")
                        .orEmpty()
                }
                .ifBlank { if (type == SessionItemType.TOOL_CALL) text else "" }
            val outputTail = json.stringOrBlank("output_tail")
                .ifBlank { json.stringOrBlank("output") }
                .ifBlank {
                    toolJson?.stringOrBlank("output_tail")
                        .orEmpty()
                }
                .ifBlank { if (type == SessionItemType.TOOL_CALL) text else "" }
            val exitCode = readNullableInt(
                json,
                "exit_code",
            ) ?: toolJson?.let { readNullableInt(it, "exit_code") }
            val status = json.stringOrBlank("status")
                .ifBlank { toolJson?.stringOrBlank("status").orEmpty() }
            val files = parseFiles(json.optJSONArray("files"))
                .ifEmpty { parseFiles(toolJson?.optJSONArray("files")) }
            val plan = parsePlan(json.optJSONArray("plan"))
                .ifEmpty { parsePlan(toolJson?.optJSONArray("plan")) }
                .ifEmpty { planFromText(text) }
            val id = json.stringOrBlank("id")
                .ifBlank { fallbackId }
                .ifBlank { "${json.stringOrBlank("ts")}-$rawType-${text.take(24)}" }
            // Reader row numbers are not notification IDs. Only an explicit linkage
            // can claim a received message or permission event.
            val eventId = readNullableLong(json, "event_id")

            if (
                type == SessionItemType.OTHER &&
                text.isBlank() &&
                files.isEmpty() &&
                plan.isEmpty()
            ) {
                return null
            }
            return SessionItem(
                id = id,
                itemKey = json.stringOrBlank("item_key"),
                ts = json.stringOrBlank("ts")
                    .ifBlank { json.stringOrBlank("timestamp") }
                    .ifBlank { Instant.now().toString() },
                type = type,
                role = role,
                partial = json.optBoolean("partial", false),
                truncated = json.optBoolean("truncated", false),
                text = text,
                tool = ItemTool(
                    name = toolName,
                    inputSummary = inputSummary,
                    outputTail = outputTail,
                    exitCode = exitCode,
                    status = status,
                ),
                files = files,
                plan = plan,
                model = json.stringOrBlank("model"),
                eventId = eventId,
                rawType = rawType,
                isToolResult = rawType == "tool_result" ||
                    rawType == "function_call_output" ||
                    role == "tool",
            )
        }

        private fun resolveType(
            rawType: String,
            role: String,
            toolName: String,
            json: JSONObject,
        ): SessionItemType {
            if (
                rawType in setOf("tool_result", "function_call_output", "tool-output") ||
                role == "tool"
            ) {
                return SessionItemType.TOOL_CALL
            }
            return when (rawType) {
                "", "text", "message", "input_text", "output_text" ->
                    if (role == "user") SessionItemType.USER else SessionItemType.ASSISTANT
                "user" -> SessionItemType.USER
                "assistant" -> if (toolName.isNotBlank()) {
                    SessionItemType.TOOL_CALL
                } else {
                    SessionItemType.ASSISTANT
                }
                "thinking", "reasoning" -> SessionItemType.REASONING
                "command", "tool_call", "tool_use", "function_call" ->
                    SessionItemType.TOOL_CALL
                "patch", "file_change", "file-change", "edit", "write" ->
                    SessionItemType.FILE_CHANGE
                "plan", "update_plan" -> SessionItemType.PLAN
                "approval", "permission", "permission_request" ->
                    SessionItemType.APPROVAL
                "error", "failed" -> SessionItemType.ERROR
                "system", "status" -> SessionItemType.SYSTEM
                else -> when {
                    role == "user" -> SessionItemType.USER
                    role == "assistant" && toolName.isNotBlank() ->
                        if (json.has("files")) {
                            SessionItemType.FILE_CHANGE
                        } else {
                            SessionItemType.TOOL_CALL
                        }
                    role == "assistant" -> SessionItemType.ASSISTANT
                    json.has("plan") -> SessionItemType.PLAN
                    json.has("files") -> SessionItemType.FILE_CHANGE
                    else -> SessionItemType.OTHER
                }
            }
        }

        private fun parseFiles(array: JSONArray?): List<ItemFileChange> {
            if (array == null) return emptyList()
            return buildList {
                for (index in 0 until array.length()) {
                    val value = array.opt(index)
                    if (value is String && value.isNotBlank()) {
                        add(ItemFileChange(path = value))
                    } else if (value is JSONObject) {
                        val path = value.stringOrBlank("path")
                            .ifBlank { value.stringOrBlank("file") }
                        if (path.isNotBlank()) {
                            add(
                                ItemFileChange(
                                    path = path,
                                    added = value.optInt("added", 0),
                                    removed = value.optInt("removed", 0),
                                    diff = value.stringOrBlank("diff"),
                                ),
                            )
                        }
                    }
                }
            }
        }

        private fun parsePlan(array: JSONArray?): List<ItemPlanStep> {
            if (array == null) return emptyList()
            return buildList {
                for (index in 0 until array.length()) {
                    val value = array.opt(index)
                    if (value is String && value.isNotBlank()) {
                        add(ItemPlanStep(value, "pending"))
                    } else if (value is JSONObject) {
                        val step = value.stringOrBlank("step")
                            .ifBlank { value.stringOrBlank("content") }
                            .ifBlank { value.stringOrBlank("text") }
                        if (step.isNotBlank()) {
                            add(
                                ItemPlanStep(
                                    step = step,
                                    status = value.stringOrBlank("status")
                                        .ifBlank { "pending" },
                                ),
                            )
                        }
                    }
                }
            }
        }

        private fun planFromText(text: String): List<ItemPlanStep> {
            if (!text.trimStart().startsWith("{")) return emptyList()
            val json = runCatching { JSONObject(text) }.getOrNull() ?: return emptyList()
            return parsePlan(json.optJSONArray("plan"))
        }

        private fun readNullableInt(json: JSONObject, key: String): Int? {
            if (!json.has(key) || json.isNull(key)) return null
            return runCatching { json.getInt(key) }.getOrNull()
        }

        private fun readNullableLong(json: JSONObject, key: String): Long? {
            if (!json.has(key) || json.isNull(key)) return null
            return runCatching { json.getLong(key) }.getOrNull()
        }
    }
}

internal fun SessionItem.toJson(): JSONObject {
    val typeName = rawType.ifBlank {
        when (type) {
            SessionItemType.USER -> "user"
            SessionItemType.ASSISTANT -> "assistant"
            SessionItemType.REASONING -> "reasoning"
            SessionItemType.TOOL_CALL -> if (isToolResult) "tool_result" else "tool_call"
            SessionItemType.FILE_CHANGE -> "file_change"
            SessionItemType.PLAN -> "plan"
            SessionItemType.APPROVAL -> "permission"
            SessionItemType.ERROR -> "error"
            SessionItemType.SYSTEM -> "system"
            SessionItemType.OTHER -> "other"
        }
    }
    return JSONObject()
        .put("id", id)
        .put("item_key", itemKey)
        .put("ts", ts)
        .put("type", typeName)
        .put("role", role)
        .put("partial", partial)
        .put("truncated", truncated)
        .put("text", text)
        .put(
            "tool",
            JSONObject()
                .put("name", tool.name)
                .put("input_summary", tool.inputSummary)
                .put("output_tail", tool.outputTail)
                .put("exit_code", tool.exitCode ?: JSONObject.NULL)
                .put("status", tool.status),
        )
        .put(
            "files",
            JSONArray().apply {
                files.forEach { file ->
                    put(
                        JSONObject()
                            .put("path", file.path)
                            .put("added", file.added)
                            .put("removed", file.removed)
                            .put("diff", file.diff),
                    )
                }
            },
        )
        .put(
            "plan",
            JSONArray().apply {
                plan.forEach { step ->
                    put(JSONObject().put("step", step.step).put("status", step.status))
                }
            },
        )
        .put("model", model)
        .put("event_id", eventId ?: JSONObject.NULL)
}

data class SessionTurn(
    val state: String,
    val ts: String = "",
    val seq: Int = 0,
) {
    companion object {
        fun fromJson(json: JSONObject): SessionTurn? {
            val state = json.stringOrBlank("state").lowercase()
            if (state.isBlank()) return null
            return SessionTurn(
                state = state,
                ts = json.stringOrBlank("ts"),
                seq = json.optInt("seq", 0),
            )
        }
    }
}

data class SessionItemsState(
    val items: List<SessionItem> = emptyList(),
    val turns: List<SessionTurn> = emptyList(),
    val reason: String? = null,
    val available: Boolean = true,
    val loadedAt: Long = 0L,
    val running: Boolean = false,
    val runningSince: String = "",
    val includeReasoning: Boolean = false,
    val contextKey: String = "",
    val history: SessionHistoryState = SessionHistoryState(),
) {
    val lastItem: SessionItem?
        get() = items.lastOrNull()

    companion object {
        fun fromTurns(turns: List<SessionTurn>): SessionItemsState {
            val last = turns.lastOrNull()
            val running = last?.state in setOf("started", "running")
            return SessionItemsState(
                turns = turns,
                running = running,
                runningSince = if (running) last?.ts.orEmpty() else "",
            )
        }
    }
}

fun normalizeSessionItems(items: List<SessionItem>): List<SessionItem> {
    val output = mutableListOf<SessionItem>()
    for (item in items) {
        if (item.isNoise()) continue
        if (item.isToolResult) {
            val index = output.indexOfLast { candidate ->
                candidate.type == SessionItemType.TOOL_CALL &&
                    candidate.tool.outputTail.isBlank() &&
                    (
                        item.tool.name.isBlank() ||
                            candidate.tool.name.isBlank() ||
                            candidate.tool.name == item.tool.name
                        )
            }
            if (index >= 0) {
                val target = output[index]
                output[index] = target.copy(
                    tool = target.tool.copy(
                        name = target.tool.name.ifBlank { item.tool.name },
                        outputTail = item.text.ifBlank { item.tool.outputTail },
                        exitCode = item.tool.exitCode ?: target.tool.exitCode,
                        status = item.tool.status.ifBlank { target.tool.status },
                    ),
                )
            } else {
                output += item.copy(
                    type = SessionItemType.TOOL_CALL,
                    tool = item.tool.copy(
                        name = item.tool.name.ifBlank { "工具" },
                        outputTail = item.tool.outputTail.ifBlank { item.text },
                    ),
                )
            }
            continue
        }
        output += item
    }
    val unique = LinkedHashMap<String, SessionItem>(output.size)
    for (item in output) {
        unique[item.itemKey.ifBlank { item.id }] = item
    }
    return unique.values.toList()
}

fun SessionItem.isNoise(): Boolean {
    if (type != SessionItemType.USER && type != SessionItemType.ASSISTANT) return false
    val trimmed = text.trimStart()
    return trimmed.startsWith("<tools>") ||
        trimmed.startsWith("<environment_context>") ||
        trimmed.startsWith("<user_instructions>") ||
        trimmed.startsWith("<system")
}

fun appendSessionItem(
    state: SessionItemsState,
    item: SessionItem,
): SessionItemsState {
    if (item.isNoise()) return state
    fun sameIdentity(existing: SessionItem): Boolean = if (item.itemKey.isNotBlank()) {
        existing.itemKey == item.itemKey
    } else existing.itemKey.isBlank() && existing.id == item.id
    val index = state.items.indexOfFirst(::sameIdentity)
    val nextItem = if (index >= 0) {
        mergeSessionItem(state.items[index], item)
    } else {
        item
    }
    val merged = if (index >= 0) {
        state.items.filterNot(::sameIdentity).toMutableList().also {
            it.add(index.coerceAtMost(it.size), nextItem)
        }
    } else {
        state.items + nextItem
    }
    return state.copy(
        items = normalizeSessionItems(merged),
        available = true,
        running = true,
        runningSince = state.runningSince.ifBlank { item.ts },
        loadedAt = System.currentTimeMillis(),
    )
}

private fun mergeSessionItem(existing: SessionItem, incoming: SessionItem): SessionItem {
    if (!incoming.truncated) {
        if (!incoming.partial || incoming.text.length >= existing.text.length) {
            return incoming
        }
        return incoming.copy(text = existing.text)
    }
    return incoming.copy(
        text = if (existing.text.length > incoming.text.length) existing.text else incoming.text,
        tool = incoming.tool.copy(
            name = incoming.tool.name.ifBlank { existing.tool.name },
            inputSummary = if (existing.tool.inputSummary.length > incoming.tool.inputSummary.length) {
                existing.tool.inputSummary
            } else {
                incoming.tool.inputSummary
            },
            outputTail = if (existing.tool.outputTail.length > incoming.tool.outputTail.length) {
                existing.tool.outputTail
            } else {
                incoming.tool.outputTail
            },
            exitCode = incoming.tool.exitCode ?: existing.tool.exitCode,
            status = incoming.tool.status.ifBlank { existing.tool.status },
        ),
        files = if (existing.files.isNotEmpty()) existing.files else incoming.files,
        plan = if (incoming.plan.isNotEmpty()) incoming.plan else existing.plan,
        truncated = existing.truncated,
    )
}

fun applyTurnToItems(
    state: SessionItemsState,
    turn: SessionTurn,
): SessionItemsState {
    val turns = (state.turns + turn)
        .distinctBy { "${it.seq}-${it.state}-${it.ts}" }
        .sortedWith(compareBy<SessionTurn> { it.seq }.thenBy { it.ts })
    val running = when (turn.state) {
        "started", "running" -> true
        "idle", "finished", "failed", "complete", "completed" -> false
        else -> state.running
    }
    return state.copy(
        turns = turns,
        running = running,
        runningSince = if (running) {
            state.runningSince.ifBlank { turn.ts }
        } else {
            ""
        },
        loadedAt = System.currentTimeMillis(),
    )
}
