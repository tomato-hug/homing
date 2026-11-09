package dev.agentpush.poc.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

sealed interface MarkdownBlock {
    data class Heading(val level: Int, val text: String) : MarkdownBlock
    data class Paragraph(val text: String) : MarkdownBlock
    data class Bullet(val text: String, val number: Int? = null) : MarkdownBlock
    data class Code(val code: String) : MarkdownBlock
}

fun parseBasicMarkdown(source: String): List<MarkdownBlock> {
    val blocks = mutableListOf<MarkdownBlock>()
    val paragraph = StringBuilder()
    var codeFence = false
    val code = StringBuilder()

    fun flushParagraph() {
        if (paragraph.isNotBlank()) {
            blocks += MarkdownBlock.Paragraph(paragraph.toString().trim())
            paragraph.clear()
        }
    }

    for (rawLine in source.lines()) {
        val line = rawLine.trimEnd()
        val trimmed = line.trimStart()
        if (trimmed.startsWith("```")) {
            if (codeFence) {
                blocks += MarkdownBlock.Code(code.toString().trimEnd())
                code.clear()
                codeFence = false
            } else {
                flushParagraph()
                codeFence = true
            }
            continue
        }
        if (codeFence) {
            if (code.isNotEmpty()) {
                code.append('\n')
            }
            code.append(rawLine)
            continue
        }

        when {
            line.isBlank() -> flushParagraph()
            trimmed.matches(Regex("^#{1,3}\\s+.*")) -> {
                flushParagraph()
                val level = trimmed.takeWhile { it == '#' }.length.coerceIn(1, 3)
                blocks += MarkdownBlock.Heading(
                    level = level,
                    text = trimmed.removePrefix("#".repeat(level)).trim(),
                )
            }
            trimmed.matches(Regex("^[-*+]\\s+.*")) -> {
                flushParagraph()
                blocks += MarkdownBlock.Bullet(trimmed.substring(1).trim())
            }
            trimmed.matches(Regex("^\\d+[.)]\\s+.*")) -> {
                flushParagraph()
                val number = trimmed.takeWhile { it.isDigit() }.toIntOrNull()
                blocks += MarkdownBlock.Bullet(
                    text = trimmed.dropWhile { it.isDigit() }.drop(1).trim(),
                    number = number,
                )
            }
            else -> {
                if (paragraph.isNotEmpty()) {
                    paragraph.append('\n')
                }
                paragraph.append(trimmed)
            }
        }
    }
    if (codeFence) {
        blocks += MarkdownBlock.Code(code.toString())
    }
    flushParagraph()
    return blocks
}

fun inlineMarkdown(source: String): AnnotatedString {
    return buildAnnotatedString {
        var index = 0
        while (index < source.length) {
            when {
                source.startsWith("**", index) -> {
                    val end = source.indexOf("**", index + 2)
                    if (end > index + 2) {
                        withStyle(SpanStyle(fontWeight = FontWeight.Bold)) {
                            append(source.substring(index + 2, end))
                        }
                        index = end + 2
                    } else {
                        append(source[index])
                        index += 1
                    }
                }
                source[index] == '`' -> {
                    val end = source.indexOf('`', index + 1)
                    if (end > index + 1) {
                        withStyle(
                            SpanStyle(
                                fontFamily = FontFamily.Monospace,
                                background = MaterialThemeCodeBackground,
                                fontSize = 14.sp,
                            ),
                        ) {
                            append(source.substring(index + 1, end))
                        }
                        index = end + 1
                    } else {
                        append(source[index])
                        index += 1
                    }
                }
                else -> {
                    append(source[index])
                    index += 1
                }
            }
        }
    }
}

private val MaterialThemeCodeBackground = Color(0x1F5C6B82)

@Composable
fun MarkdownContent(
    source: String,
    modifier: Modifier = Modifier,
    onCopyCode: (String) -> Unit,
) {
    val blocks = parseBasicMarkdown(source)
    Column(
        modifier = modifier,
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        blocks.forEach { block ->
            when (block) {
                is MarkdownBlock.Heading -> Text(
                    text = inlineMarkdown(block.text),
                    fontSize = when (block.level) {
                        1 -> 21.sp
                        2 -> 18.sp
                        else -> 16.sp
                    },
                    lineHeight = when (block.level) {
                        1 -> 28.sp
                        2 -> 26.sp
                        else -> 24.sp
                    },
                    fontWeight = FontWeight.Bold,
                    color = MaterialTheme.colorScheme.onSurface,
                )

                is MarkdownBlock.Paragraph -> Text(
                    text = inlineMarkdown(block.text),
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurface,
                )

                is MarkdownBlock.Bullet -> Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Text(
                        text = block.number?.let { "$it." } ?: "•",
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.primary,
                    )
                    Text(
                        text = inlineMarkdown(block.text),
                        modifier = Modifier.weight(1f),
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                }

                is MarkdownBlock.Code -> Text(
                    text = block.code,
                    modifier = Modifier
                        .fillMaxWidth()
                        .background(
                            MaterialTheme.colorScheme.surfaceVariant,
                            RoundedCornerShape(12.dp),
                        )
                        .horizontalScroll(rememberScrollState())
                        .padding(12.dp)
                        .pointerInput(block.code) {
                            detectTapGestures(
                                onLongPress = { onCopyCode(block.code) },
                            )
                        },
                    fontFamily = FontFamily.Monospace,
                    fontSize = 13.sp,
                    lineHeight = 20.sp,
                    color = MaterialTheme.colorScheme.onSurface,
                    softWrap = false,
                )
            }
        }
    }
}
