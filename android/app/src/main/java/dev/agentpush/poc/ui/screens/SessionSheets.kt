package dev.agentpush.poc.ui.screens

import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import dev.agentpush.poc.AgentApi
import dev.agentpush.poc.AgentProfile
import dev.agentpush.poc.AppSettings
import dev.agentpush.poc.Attachment
import dev.agentpush.poc.SessionSummary
import dev.agentpush.poc.ui.FullScreenImageViewer
import dev.agentpush.poc.ui.ModelPickerSheet
import java.net.URI

internal data class ImageViewerState(
    val attachments: List<Attachment>,
    val index: Int,
)

@Composable
internal fun SessionSheets(
    session: SessionSummary?,
    modelPickerOpen: Boolean,
    agentProfile: AgentProfile,
    effectiveModel: String,
    effectiveEffort: String,
    onDismissModelPicker: () -> Unit,
    onApplyModel: (String, String) -> Unit,
    imageViewer: ImageViewerState?,
    settings: AppSettings,
    onDismissImageViewer: () -> Unit,
    localhostPrompt: String?,
    proxyCapability: String?,
    onDismissLocalhostPrompt: () -> Unit,
    onOpenBrowser: (String, String) -> Unit,
) {
    if (modelPickerOpen && session != null) {
        ModelPickerSheet(
            profile = agentProfile.copy(
                models = agentProfile.models,
                defaultModel = agentProfile.defaultModel.ifBlank { session.model },
            ),
            currentModel = effectiveModel,
            currentEffort = effectiveEffort,
            onDismiss = onDismissModelPicker,
            onApply = onApplyModel,
        )
    }

    imageViewer?.let { viewer ->
        FullScreenImageViewer(
            attachments = viewer.attachments,
            initialIndex = viewer.index,
            settings = settings,
            onDismiss = onDismissImageViewer,
        )
    }

    localhostPrompt?.let { url ->
        val port = runCatching { URI(url).port }.getOrDefault(-1)
        AlertDialog(
            onDismissRequest = onDismissLocalhostPrompt,
            title = { Text("这是 Mac 本机地址") },
            text = {
                Text(
                    "localhost / 127.0.0.1 在手机上指向手机自己，无法直接访问。" +
                        if (port > 0) {
                            "\n\n若中继已开启端口代理，可通过中继访问 $port 端口；未开启时会显示错误。"
                        } else {
                            ""
                        },
                )
            },
            confirmButton = {
                if (port > 0) {
                    TextButton(
                        onClick = {
                            onDismissLocalhostPrompt()
                            val uri = runCatching { URI(url) }.getOrNull()
                            val path = uri?.rawPath?.takeIf { it.isNotBlank() } ?: "/"
                            val query = uri?.rawQuery?.let { "?$it" }.orEmpty()
                            onOpenBrowser(
                                AgentApi.proxyUrl(
                                    settings,
                                    port,
                                    path.trimStart('/') + query,
                                    proxyCapability,
                                ),
                                "通过中继 · :$port",
                            )
                        },
                    ) {
                        Text("通过中继打开")
                    }
                }
            },
            dismissButton = {
                TextButton(onClick = onDismissLocalhostPrompt) {
                    Text("取消")
                }
            },
        )
    }
}
