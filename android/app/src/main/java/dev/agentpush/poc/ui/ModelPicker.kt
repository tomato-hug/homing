package dev.agentpush.poc.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.FilterChipDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import dev.agentpush.poc.AgentModel
import dev.agentpush.poc.AgentProfile
import dev.agentpush.poc.R
import dev.agentpush.poc.ui.theme.HomingMono

@Composable
fun ModelPill(
    label: String,
    enabled: Boolean,
    overridden: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Surface(
        modifier = modifier,
        onClick = onClick,
        enabled = enabled,
        shape = RoundedCornerShape(50),
        color = when {
            !enabled -> MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.4f)
            overridden -> MaterialTheme.colorScheme.primary.copy(alpha = 0.14f)
            else -> MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.65f)
        },
        border = BorderStroke(
            1.dp,
            if (enabled) {
                MaterialTheme.colorScheme.outlineVariant
            } else {
                MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f)
            },
        ),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 10.dp, vertical = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(5.dp),
        ) {
            Text(
                text = label.ifBlank { "未选择模型" },
                style = MaterialTheme.typography.labelMedium,
                color = if (enabled) {
                    MaterialTheme.colorScheme.onSurface
                } else {
                    MaterialTheme.colorScheme.outline
                },
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Icon(
                Icons.Outlined.ExpandMore,
                contentDescription = "选择模型",
                modifier = Modifier.size(15.dp),
                tint = if (enabled) {
                    MaterialTheme.colorScheme.onSurfaceVariant
                } else {
                    MaterialTheme.colorScheme.outline
                },
            )
            if (overridden) {
                Text(
                    text = "下一条",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.primary,
                    fontWeight = FontWeight.Bold,
                )
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun ModelPickerSheet(
    profile: AgentProfile,
    currentModel: String,
    currentEffort: String,
    onDismiss: () -> Unit,
    onApply: (model: String, effort: String) -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    val initial = profile.models.firstOrNull { it.id == currentModel }
        ?: profile.defaultModelOption
    var selectedId by remember(currentModel, profile.source) {
        mutableStateOf(initial?.id.orEmpty())
    }
    var selectedEffort by remember(currentEffort, profile.source) {
        mutableStateOf(currentEffort)
    }
    val selected = profile.models.firstOrNull { it.id == selectedId }
    var search by remember(profile.source, profile.models.size) { mutableStateOf("") }
    val visibleModels = profile.models.filter {
        it.id.contains(search.trim(), ignoreCase = true) || it.displayName.contains(search.trim(), ignoreCase = true)
    }
    val providers = visibleModels.groupBy { it.provider.ifBlank { "默认" } }
        .toSortedMap(compareBy { if (it == "默认") "" else it })

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .testTag("model-picker")
                .navigationBarsPadding()
                .padding(bottom = 8.dp),
        ) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 20.dp, vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "模型",
                        style = MaterialTheme.typography.titleLarge,
                        fontWeight = FontWeight.Bold,
                    )
                    Text(
                        text = "只对下一条消息生效",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                TextButton(onClick = onDismiss) { Text("取消") }
            }
            if (profile.models.size > 12) {
                OutlinedTextField(
                    value = search, onValueChange = { search = it }, singleLine = true,
                    placeholder = { Text(stringResource(R.string.model_search)) },
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 8.dp).testTag("model-search"),
                )
            }
            if (profile.models.isEmpty()) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(32.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        text = stringResource(R.string.model_list_unavailable),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            } else {
                if (visibleModels.isEmpty()) Text(stringResource(R.string.model_search_empty), modifier = Modifier.padding(20.dp))
                LazyColumn(modifier = Modifier.weight(1f, fill = false)) {
                    providers.forEach { (provider, models) ->
                        item(key = "provider-$provider") {
                            Text(
                                text = provider,
                                modifier = Modifier.padding(
                                    start = 20.dp,
                                    end = 20.dp,
                                    top = 14.dp,
                                    bottom = 4.dp,
                                ),
                                style = MaterialTheme.typography.labelMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        items(models, key = { it.id }) { model ->
                            ModelRow(
                                model = model,
                                selected = model.id == selectedId,
                                current = model.id == currentModel,
                                default = model.isDefault ||
                                    model.id == profile.defaultModel,
                                onClick = {
                                    selectedId = model.id
                                    if (
                                        selectedEffort.isNotBlank() &&
                                        selectedEffort !in model.efforts
                                    ) {
                                        selectedEffort = ""
                                    }
                                },
                            )
                        }
                    }
                }
                if (
                    selected != null &&
                    selected.efforts.isNotEmpty() &&
                    profile.capabilities.effort
                ) {
                    Column(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 20.dp, vertical = 8.dp),
                    ) {
                        Text(
                            text = "推理强度",
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        val choices = effortChoices(selected.efforts)
                        FlowRow(
                            modifier = Modifier.padding(top = 6.dp),
                            horizontalArrangement = Arrangement.spacedBy(7.dp),
                        ) {
                            choices.forEach { effort ->
                                FilterChip(
                                    selected = selectedEffort == effort.wire,
                                    onClick = {
                                        selectedEffort = if (selectedEffort == effort.wire) {
                                            ""
                                        } else {
                                            effort.wire
                                        }
                                    },
                                    label = { Text(effort.label) },
                                    shape = RoundedCornerShape(50),
                                    colors = FilterChipDefaults.filterChipColors(
                                        selectedContainerColor =
                                            MaterialTheme.colorScheme.primaryContainer,
                                    ),
                                )
                            }
                        }
                    }
                } else if (selected != null) {
                    Text(
                        text = if (profile.source == "claude") {
                            "Claude 不支持推理强度"
                        } else if (!profile.capabilities.effort) {
                            "当前 Agent 不支持推理强度"
                        } else {
                            "当前模型不支持推理强度"
                        },
                        modifier = Modifier.padding(horizontal = 20.dp, vertical = 8.dp),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 20.dp, vertical = 10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    if (profile.defaultModel.isNotBlank()) {
                        TextButton(
                            onClick = {
                                selectedId = profile.defaultModel
                                selectedEffort = ""
                            },
                        ) {
                            Text("恢复默认")
                        }
                    }
                    Spacer(Modifier.weight(1f))
                    Button(
                        onClick = {
                            if (selectedId.isNotBlank()) {
                                onApply(selectedId, selectedEffort)
                            }
                            onDismiss()
                        },
                        enabled = selectedId.isNotBlank(),
                        shape = RoundedCornerShape(12.dp),
                    ) {
                        Text("用于下一条消息")
                    }
                }
            }
        }
    }
}

@Composable
private fun ModelRow(
    model: AgentModel,
    selected: Boolean,
    current: Boolean,
    default: Boolean,
    onClick: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 20.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        RadioButton(selected = selected, onClick = onClick)
        Column(
            modifier = Modifier
                .weight(1f)
                .padding(start = 5.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = model.displayName,
                    style = MaterialTheme.typography.bodyLarge,
                    fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                if (current) {
                    ModelTag(
                        text = "当前",
                        modifier = Modifier.padding(start = 7.dp),
                        accent = true,
                    )
                }
                if (default) {
                    ModelTag(
                        text = "默认",
                        modifier = Modifier.padding(start = 7.dp),
                        accent = false,
                    )
                }
            }
            if (model.id != model.displayName) {
                Text(
                    text = model.id,
                    style = MaterialTheme.typography.bodySmall.copy(fontFamily = HomingMono),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        if (selected) {
            Icon(
                Icons.Outlined.CheckCircle,
                contentDescription = null,
                modifier = Modifier.size(19.dp),
                tint = MaterialTheme.colorScheme.primary,
            )
        }
    }
}

private data class EffortChoice(val wire: String, val label: String)

private fun effortChoices(efforts: List<String>): List<EffortChoice> {
    if (efforts.isEmpty()) return emptyList()
    val normalized = efforts.map { it.lowercase() }
    fun pick(vararg values: String): String? {
        return values.firstOrNull { it in normalized }
    }
    return buildList {
        pick("none", "minimal", "min")?.let { add(EffortChoice(it, "最低")) }
        pick("low")?.let { add(EffortChoice(it, "低")) }
        pick("medium", "mid")?.let { add(EffortChoice(it, "中")) }
        pick("high", "xhigh", "ultra", "max", "highest")?.let {
            add(EffortChoice(it, "高"))
        }
    }.ifEmpty {
        efforts.take(4).map { EffortChoice(it, it) }
    }
}

@Composable
private fun ModelTag(
    text: String,
    modifier: Modifier = Modifier,
    accent: Boolean,
) {
    Surface(
        modifier = modifier,
        color = if (accent) {
            MaterialTheme.colorScheme.primary.copy(alpha = 0.13f)
        } else {
            MaterialTheme.colorScheme.surfaceVariant
        },
        shape = RoundedCornerShape(50),
    ) {
        Text(
            text = text,
            modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp),
            style = MaterialTheme.typography.labelSmall,
            color = if (accent) {
                MaterialTheme.colorScheme.primary
            } else {
                MaterialTheme.colorScheme.onSurfaceVariant
            },
        )
    }
}
