package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.agentpush.poc.relativeTime
import dev.agentpush.poc.ui.theme.HomingMono
import dev.agentpush.poc.ui.theme.HomingPalette

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun SessionSearchSheet(
    open: Boolean,
    timeline: List<TimelineItem>,
    onDismiss: () -> Unit,
    onJump: (timelineIndex: Int) -> Unit,
) {
    if (!open) return
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    val focusRequester = remember { FocusRequester() }
    var query by rememberSaveable { mutableStateOf("") }
    val results = remember(timeline, query) {
        searchSessionTimeline(timeline, query)
    }

    LaunchedEffect(Unit) {
        kotlinx.coroutines.delay(180)
        runCatching { focusRequester.requestFocus() }
    }

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        containerColor = HomingPalette.Background,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(bottom = 18.dp),
        ) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 18.dp, end = 8.dp, bottom = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = "搜索这个会话",
                    modifier = Modifier.weight(1f),
                    style = MaterialTheme.typography.titleMedium,
                    color = HomingPalette.Text,
                )
                if (query.isNotBlank()) {
                    Text(
                        text = "${results.size} 条结果",
                        style = MaterialTheme.typography.labelSmall,
                        color = HomingPalette.Muted,
                    )
                }
                IconButton(onClick = onDismiss) {
                    Icon(Icons.Outlined.Close, contentDescription = "关闭搜索")
                }
            }
            OutlinedTextField(
                value = query,
                onValueChange = { query = it },
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp)
                    .focusRequester(focusRequester)
                    .testTag("session-search-field"),
                singleLine = true,
                placeholder = { Text("搜索回复、工具调用、文件路径…") },
                leadingIcon = {
                    Icon(
                        Icons.Outlined.Search,
                        contentDescription = null,
                        modifier = Modifier.size(20.dp),
                    )
                },
                trailingIcon = {
                    if (query.isNotBlank()) {
                        IconButton(onClick = { query = "" }) {
                            Icon(Icons.Outlined.Close, contentDescription = "清除搜索")
                        }
                    }
                },
                shape = RoundedCornerShape(14.dp),
            )
            if (query.isBlank()) {
                Text(
                    text = "输入关键词后，点结果即可跳到对应消息。思考内容默认不会被加载。",
                    modifier = Modifier.padding(horizontal = 18.dp, vertical = 18.dp),
                    style = MaterialTheme.typography.bodySmall,
                    color = HomingPalette.Muted,
                )
            } else if (results.isEmpty()) {
                Text(
                    text = "没有找到匹配内容",
                    modifier = Modifier.padding(horizontal = 18.dp, vertical = 22.dp),
                    style = MaterialTheme.typography.bodyMedium,
                    color = HomingPalette.Muted,
                )
            } else {
                LazyColumn(
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(max = 520.dp)
                        .testTag("session-search-results"),
                    contentPadding = PaddingValues(horizontal = 12.dp, vertical = 10.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    items(
                        results,
                        key = { "${it.timelineIndex}-${it.text.hashCode()}" },
                    ) { result ->
                        Surface(
                            onClick = {
                                onDismiss()
                                onJump(result.timelineIndex)
                            },
                            modifier = Modifier.fillMaxWidth(),
                            color = HomingPalette.Card,
                            shape = RoundedCornerShape(12.dp),
                        ) {
                            Column(
                                modifier = Modifier.padding(horizontal = 12.dp, vertical = 10.dp),
                            ) {
                                Row(
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(7.dp),
                                ) {
                                    Surface(
                                        color = HomingPalette.Brand.copy(alpha = 0.13f),
                                        shape = RoundedCornerShape(50),
                                    ) {
                                        Text(
                                            text = result.role,
                                            modifier = Modifier.padding(
                                                horizontal = 7.dp,
                                                vertical = 2.dp,
                                            ),
                                            style = MaterialTheme.typography.labelSmall,
                                            color = HomingPalette.BrandSoft,
                                        )
                                    }
                                    Text(
                                        text = relativeTime(result.timestamp),
                                        style = MaterialTheme.typography.labelSmall.copy(
                                            fontFamily = HomingMono,
                                            fontSize = 10.sp,
                                        ),
                                        color = HomingPalette.Muted,
                                    )
                                }
                                Text(
                                    text = highlightSearchSnippet(result.snippet, query),
                                    modifier = Modifier.padding(top = 6.dp),
                                    style = MaterialTheme.typography.bodySmall.copy(
                                        lineHeight = 19.sp,
                                    ),
                                    color = HomingPalette.Body,
                                    maxLines = 4,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

private fun highlightSearchSnippet(text: String, query: String): AnnotatedString {
    val needle = query.trim()
    if (needle.isBlank()) return AnnotatedString(text)
    val start = text.indexOf(needle, ignoreCase = true)
    if (start < 0) return AnnotatedString(text)
    return buildAnnotatedString {
        append(text.substring(0, start))
        withStyle(
            SpanStyle(
                background = HomingPalette.Warning.copy(alpha = 0.28f),
                color = HomingPalette.Text,
                fontWeight = FontWeight.SemiBold,
            ),
        ) {
            append(text.substring(start, start + needle.length))
        }
        append(text.substring(start + needle.length))
    }
}

