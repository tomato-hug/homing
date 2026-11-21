package dev.agentpush.poc.ui.screens

import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import dev.agentpush.poc.AgentProfile
import dev.agentpush.poc.AppSettings
import dev.agentpush.poc.Attachment
import dev.agentpush.poc.SessionSummary
import dev.agentpush.poc.ui.FullScreenImageViewer
import dev.agentpush.poc.ui.ModelPickerSheet

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
        val port = browserHttpUrl(url)?.port ?: -1
        val proxy = browserProxyUrl(url, settings.baseUrl, proxyCapability)
        AlertDialog(
            onDismissRequest = onDismissLocalhostPrompt,
            title = { Text("这是 Mac 本机地址") },
            text = {
                Text(
                    "localhost / 127.0.0.1 在手机上指向手机自己，无法直接访问。" +
                        if (proxy != null) {
                            "\n\n仅当手机能直连这台电脑，且已开启 :$port 端口代理时可尝试访问。云端中转不转发电脑网页；部分网页跳转或表单暂不支持。"
                        } else {
                            "\n\n此地址暂不支持端口代理，请在电脑上打开。"
                        },
                )
            },
            confirmButton = {
                if (proxy != null) {
                    TextButton(
                        onClick = {
                            onDismissLocalhostPrompt()
                            onOpenBrowser(
                                proxy,
                                "电脑网页 · :$port",
                            )
                        },
                    ) {
                        Text("尝试直连代理")
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
