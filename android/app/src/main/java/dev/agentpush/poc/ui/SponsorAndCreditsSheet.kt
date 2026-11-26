package dev.agentpush.poc.ui

import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.MediaStore
import android.widget.Toast
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material.icons.outlined.OpenInNew
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import dev.agentpush.poc.BuildConfig

enum class SponsorChannel(
    val id: String,
    val title: String,
    val assetPath: String,
    val saveFileName: String,
    val scanHint: String,
    val accent: Color,
) {
    WECHAT(
        id = "wechat",
        title = "微信赞赏码",
        assetPath = "sponsor/wechat_reward.png",
        saveFileName = "Homing_WeChat_Reward.png",
        scanHint = "保存后打开微信「扫一扫 → 相册」即可赞赏支持",
        accent = Color(0xFF07C160),
    ),
    ALIPAY(
        id = "alipay",
        title = "支付宝收款码",
        assetPath = "sponsor/alipay_reward.png",
        saveFileName = "Homing_Alipay_Tip.png",
        scanHint = "保存后打开支付宝「扫一扫 → 相册」即可打赏支持",
        accent = Color(0xFF1677FF),
    ),
}

internal fun loadSponsorBitmap(context: Context, assetPath: String): Bitmap? =
    runCatching {
        context.assets.open(assetPath).use { BitmapFactory.decodeStream(it) }
    }.getOrNull()

internal fun saveSponsorBitmapToGallery(
    context: Context,
    bitmap: Bitmap,
    fileName: String,
): Boolean = runCatching {
    val resolver = context.contentResolver
    val values = ContentValues().apply {
        put(MediaStore.Images.Media.DISPLAY_NAME, fileName)
        put(MediaStore.Images.Media.MIME_TYPE, "image/png")
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            put(MediaStore.Images.Media.RELATIVE_PATH, Environment.DIRECTORY_PICTURES + "/Homing")
            put(MediaStore.Images.Media.IS_PENDING, 1)
        }
    }
    val uri = resolver.insert(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, values)
        ?: return false
    resolver.openOutputStream(uri)?.use { out ->
        if (!bitmap.compress(Bitmap.CompressFormat.PNG, 100, out)) {
            return false
        }
    } ?: return false
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
        values.clear()
        values.put(MediaStore.Images.Media.IS_PENDING, 0)
        resolver.update(uri, values, null, null)
    }
    true
}.getOrDefault(false)

@Composable
fun AboutAndSponsorCard(
    onOpenSponsor: () -> Unit,
    onOpenCredits: () -> Unit,
) {
    val context = LocalContext.current
    val clipboard = LocalClipboardManager.current
    val showDirectTip = BuildConfig.ENABLE_DIRECT_DONATION

    SectionCard {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Text(
                text = "关于 Homing · 致谢与支持",
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Surface(
                color = MaterialTheme.colorScheme.primary.copy(alpha = 0.14f),
                shape = RoundedCornerShape(999.dp),
            ) {
                Text(
                    text = "Apache-2.0 开源",
                    modifier = Modifier.padding(horizontal = 9.dp, vertical = 3.dp),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.primary,
                    fontWeight = FontWeight.SemiBold,
                )
            }
        }
        Spacer(Modifier.height(6.dp))
        Text(
            text = "Homing 坚持开源、无广告、端到端零知识加密。如果它帮你在离开电脑时省去了切 App 与配网络的麻烦，欢迎点亮 Star 或打赏分担公网中继服务器带宽。",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(10.dp))
        Surface(
            onClick = {
                clipboard.setText(AnnotatedString("512365124"))
                Toast.makeText(context, "已复制问题反馈交流 QQ 群号：512365124", Toast.LENGTH_SHORT).show()
            },
            shape = RoundedCornerShape(11.dp),
            color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.45f),
            modifier = Modifier
                .fillMaxWidth()
                .testTag("settings-copy-qq-group"),
        ) {
            Row(
                modifier = Modifier.padding(horizontal = 12.dp, vertical = 9.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "问题反馈交流 QQ 群：512365124",
                        style = MaterialTheme.typography.labelLarge,
                        fontWeight = FontWeight.SemiBold,
                    )
                    Text(
                        text = "部署答疑 · 中继接入 · Bug 反馈（点此一键复制群号）",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Text(
                    text = "复制",
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.primary,
                    fontWeight = FontWeight.SemiBold,
                )
            }
        }
        Spacer(Modifier.height(12.dp))
        if (showDirectTip) {
            Button(
                onClick = onOpenSponsor,
                modifier = Modifier
                    .fillMaxWidth()
                    .testTag("settings-open-sponsor"),
                shape = RoundedCornerShape(13.dp),
            ) {
                Icon(
                    Icons.Outlined.FavoriteBorder,
                    contentDescription = null,
                    modifier = Modifier.size(17.dp),
                )
                Spacer(Modifier.width(7.dp))
                Text("请作者喝杯咖啡（微信 / 支付宝）", fontWeight = FontWeight.SemiBold)
            }
            Spacer(Modifier.height(8.dp))
        }
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            OutlinedButton(
                onClick = onOpenCredits,
                modifier = Modifier
                    .weight(1f)
                    .testTag("settings-open-credits"),
                shape = RoundedCornerShape(13.dp),
            ) {
                Text("致谢与开源许可")
            }
            OutlinedButton(
                onClick = {
                    runCatching {
                        context.startActivity(
                            Intent(Intent.ACTION_VIEW, Uri.parse("https://github.com/tomato-hug/homing")),
                        )
                    }
                },
                modifier = Modifier
                    .weight(1f)
                    .testTag("settings-open-github"),
                shape = RoundedCornerShape(13.dp),
            ) {
                Text("GitHub 仓库")
                Spacer(Modifier.width(4.dp))
                Icon(Icons.Outlined.OpenInNew, contentDescription = null, modifier = Modifier.size(15.dp))
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SponsorBottomSheet(
    onDismiss: () -> Unit,
) {
    val context = LocalContext.current
    var selectedId by rememberSaveable { mutableStateOf(SponsorChannel.WECHAT.id) }
    val channel = SponsorChannel.entries.firstOrNull { it.id == selectedId } ?: SponsorChannel.WECHAT
    val qrBitmap = remember(context, channel.assetPath) {
        loadSponsorBitmap(context, channel.assetPath)
    }

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = MaterialTheme.colorScheme.surface,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .navigationBarsPadding()
                .padding(horizontal = 20.dp, vertical = 8.dp)
                .verticalScroll(rememberScrollState())
                .testTag("sponsor-bottom-sheet"),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                text = "支持 Homing 开源与公网中继 ☕",
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.Bold,
            )
            Spacer(Modifier.height(6.dp))
            Text(
                text = "所有赞赏将直接用于分担官方公网盲中继服务器的带宽成本。打赏可备注昵称列入 SPONSORS.md 鸣谢榜（支持匿名），也可获取官方中继 Sponsor 提额兑换码。",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(14.dp))

            Row(
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                SponsorChannel.entries.forEach { item ->
                    FilterChip(
                        selected = channel == item,
                        onClick = { selectedId = item.id },
                        label = { Text(item.title) },
                        modifier = Modifier.testTag("sponsor-tab-${item.id}"),
                    )
                }
            }

            Spacer(Modifier.height(12.dp))
            Surface(
                shape = RoundedCornerShape(18.dp),
                border = BorderStroke(1.5.dp, channel.accent.copy(alpha = 0.45f)),
                color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.35f),
                modifier = Modifier.padding(horizontal = 12.dp),
            ) {
                Column(
                    modifier = Modifier.padding(16.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    if (qrBitmap != null) {
                        Image(
                            bitmap = qrBitmap.asImageBitmap(),
                            contentDescription = channel.title,
                            modifier = Modifier
                                .size(width = 230.dp, height = 265.dp)
                                .clip(RoundedCornerShape(12.dp)),
                            contentScale = ContentScale.Fit,
                        )
                    } else {
                        Box(
                            modifier = Modifier
                                .size(220.dp)
                                .background(MaterialTheme.colorScheme.surfaceVariant, RoundedCornerShape(12.dp)),
                            contentAlignment = Alignment.Center,
                        ) {
                            Text("赞赏码加载失败", style = MaterialTheme.typography.bodySmall)
                        }
                    }
                    Spacer(Modifier.height(8.dp))
                    Text(
                        text = channel.scanHint,
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }

            Spacer(Modifier.height(14.dp))
            Button(
                onClick = {
                    val bmp = qrBitmap
                    if (bmp != null && saveSponsorBitmapToGallery(context, bmp, channel.saveFileName)) {
                        Toast.makeText(
                            context,
                            "已保存「${channel.title}」到系统相册（Pictures/Homing）",
                            Toast.LENGTH_LONG,
                        ).show()
                    } else {
                        Toast.makeText(context, "保存失败，可直接截图扫码", Toast.LENGTH_SHORT).show()
                    }
                },
                colors = ButtonDefaults.buttonColors(containerColor = channel.accent, contentColor = Color.White),
                shape = RoundedCornerShape(13.dp),
                modifier = Modifier
                    .fillMaxWidth()
                    .testTag("sponsor-save-qr"),
            ) {
                Icon(Icons.Outlined.Download, contentDescription = null, modifier = Modifier.size(18.dp))
                Spacer(Modifier.width(7.dp))
                Text("保存${channel.title}到相册", fontWeight = FontWeight.SemiBold)
            }
            Spacer(Modifier.height(16.dp))
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CreditsAndLicensesSheet(
    onDismiss: () -> Unit,
) {
    val context = LocalContext.current
    val fontLicense = remember(context) {
        runCatching {
            context.assets.open("licenses/JetBrainsMono-OFL.txt")
                .bufferedReader()
                .use { it.readText() }
        }.getOrDefault("JetBrains Mono — SIL Open Font License 1.1")
    }

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = MaterialTheme.colorScheme.surface,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .navigationBarsPadding()
                .padding(horizontal = 20.dp, vertical = 8.dp)
                .verticalScroll(rememberScrollState())
                .testTag("credits-bottom-sheet"),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(
                text = "致谢与开源许可",
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.Bold,
            )

            CreditBlock(
                title = "1. 架构灵感与生态致谢",
                body = "感谢 Magpie（yetone/magpie）在单文件 Go + Wails 跨平台桌面架构上的先行探索；感谢 Claude Code、OpenAI Codex、WorkBuddy、OpenCode、Gemini CLI、Kimi CLI 等开放 CLI / Hook / ACP 协议生态，以及 @lobehub/icons 提供的开源品牌图标资源。",
            )
            CreditBlock(
                title = "2. 核心开源组件与密码学基石",
                body = "• 服务端与桌面端：Go 标准库、Wails、modernc.org/sqlite、coder/websocket、golang.org/x/crypto（Ed25519 / X25519 / NaCl Box）。\n• Android 客户端：Jetpack Compose、OkHttp、Lazysodium、ZXing、Multiplatform Markdown Renderer、Coil。\n• 等宽字体：JetBrains Mono（SIL Open Font License 1.1）。",
            )
            CreditBlock(
                title = "3. 贡献者与赞助者鸣谢榜",
                body = "感谢每一位提交 Issue / PR 与分担公网中继带宽的伙伴，完整赞助鸣谢名单维护于 GitHub 仓库 SPONSORS.md。",
            )

            HorizontalDivider()
            Text(
                text = "Apache License 2.0 & JetBrains Mono OFL 1.1",
                style = MaterialTheme.typography.titleSmall,
                fontWeight = FontWeight.SemiBold,
            )
            Text(
                text = "Homing is licensed under the Apache License, Version 2.0.\n\n$fontLicense",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(16.dp))
        }
    }
}

@Composable
private fun CreditBlock(title: String, body: String) {
    Surface(
        shape = RoundedCornerShape(12.dp),
        color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.35f),
        modifier = Modifier.fillMaxWidth(),
    ) {
        Column(modifier = Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(title, style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold)
            Text(
                body,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}
