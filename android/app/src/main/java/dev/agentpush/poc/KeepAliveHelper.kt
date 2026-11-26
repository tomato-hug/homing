package dev.agentpush.poc

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
import android.widget.Toast

data class KeepAliveStep(
    val number: String,
    val title: String,
    val body: String,
)

enum class RomKeepAliveProfile(
    val id: String,
    val shortLabel: String,
    val fullTitle: String,
    val autoStartButtonLabel: String,
    val steps: List<KeepAliveStep>,
    val note: String,
) {
    HYPEROS_MIUI(
        id = "xiaomi",
        shortLabel = "小米澎湃/MIUI",
        fullTitle = "小米 / Redmi · HyperOS & MIUI 保活指引",
        autoStartButtonLabel = "打开小米自启动设置",
        steps = listOf(
            KeepAliveStep("1", "开启后台自启动", "应用设置 → 权限管理 → 后台自启动（或自启动），开启「Homing」。"),
            KeepAliveStep("2", "省电策略设为「无限制」", "应用信息 → 省电策略，选择「无限制」（避免锁屏 5 分钟后断连）。"),
            KeepAliveStep("3", "最近任务下拉加锁", "打开多任务卡片，长按或下拉「Homing」点击锁形图标锁定。"),
        ),
        note = "HyperOS 2.0 路径为：应用信息 → 电量消耗 → 后台省电策略 → 无限制，并开启自启动。",
    ),
    HARMONY_EMUI(
        id = "huawei",
        shortLabel = "华为鸿蒙/EMUI",
        fullTitle = "华为 · HarmonyOS / EMUI 保活指引",
        autoStartButtonLabel = "打开华为应用启动管理",
        steps = listOf(
            KeepAliveStep("1", "关闭「自动管理」改为手动三开", "手机管家 → 应用启动管理 → 找到「Homing」关闭自动管理，手动开启「允许自启动、允许关联启动、允许后台活动」。"),
            KeepAliveStep("2", "允许忽略电池优化", "点击上方「一键申请无限制白名单」并选择「允许」。"),
            KeepAliveStep("3", "多任务卡片下拉锁定", "进入最近任务界面，将「Homing」卡片向下滑动出现锁标即可。"),
        ),
        note = "鸿蒙系统若开启了「纯净模式」或超级省电，仍会限制后台常驻，日常使用保持普通模式即可。",
    ),
    MAGICOS(
        id = "honor",
        shortLabel = "荣耀 MagicOS",
        fullTitle = "荣耀 · MagicOS 保活指引",
        autoStartButtonLabel = "打开荣耀应用启动管理",
        steps = listOf(
            KeepAliveStep("1", "应用启动管理改为手动管理", "系统管家 → 应用启动管理 → 关闭「Homing」自动开关，勾选允许自启动、关联启动与后台活动。"),
            KeepAliveStep("2", "电池优化白名单", "允许「Homing」忽略电池优化，保持休眠期间网络连接不断开。"),
            KeepAliveStep("3", "锁定多任务卡片", "最近任务界面下拉或点击右上角锁定「Homing」。"),
        ),
        note = "MagicOS 8/9 建议同时在设置 → 电池 → 更多电池设置中开启「休眠时始终保持网络连接」。",
    ),
    ORIGINOS_FUNTOUCH(
        id = "vivo",
        shortLabel = "vivo/iQOO",
        fullTitle = "vivo / iQOO · OriginOS 保活指引",
        autoStartButtonLabel = "打开 vivo 后台高耗电/自启动",
        steps = listOf(
            KeepAliveStep("1", "允许后台高耗电 / 无限制", "应用信息 → 电量 → 后台耗电管理，选择「允许后台高耗电」（OriginOS 4/5 为「无限制」）。"),
            KeepAliveStep("2", "开启自启动权限", "i 管家 → 应用管理 → 权限管理 → 自启动，开启「Homing」。"),
            KeepAliveStep("3", "最近任务卡片加锁", "多任务界面点击「Homing」卡片顶部菜单选择「锁定」。"),
        ),
        note = "OriginOS 仅在断线重连瞬间申请短时唤醒锁，云中继稳态下几乎不额外耗电。",
    ),
    COLOROS_OXYGEN(
        id = "oppo",
        shortLabel = "OPPO/一加/realme",
        fullTitle = "OPPO / 一加 / realme · ColorOS 保活指引",
        autoStartButtonLabel = "打开 ColorOS 自启动管理",
        steps = listOf(
            KeepAliveStep("1", "允许自启动", "设置 → 应用 → 自启动，把「Homing」设为允许。"),
            KeepAliveStep("2", "耗电管理允许完全后台行为", "应用信息 → 耗电管理，开启「允许完全后台行为」与「允许应用自启动」。"),
            KeepAliveStep("3", "锁定最近任务", "在最近任务卡片右上角菜单选择「锁定」，防止一键清理误杀。"),
        ),
        note = "一加 OxygenOS 与真我 realme UI 与 ColorOS 底层一致，按上述三步设置即可稳定常驻。",
    ),
    ONE_UI(
        id = "samsung",
        shortLabel = "三星 One UI",
        fullTitle = "三星 · One UI 保活指引",
        autoStartButtonLabel = "打开三星设备维护/电池设置",
        steps = listOf(
            KeepAliveStep("1", "电池设为「不受限制」", "应用信息 → 电池 → 选择「不受限制 (Unrestricted)」。"),
            KeepAliveStep("2", "加入「从不自动休眠应用」", "设置 → 设备维护 → 电池 → 后台使用限制 → 从不自动休眠的应用程序 → 添加「Homing」。"),
            KeepAliveStep("3", "最近任务锁定", "多任务界面轻触「Homing」图标 → 选择「保持打开」。"),
        ),
        note = "One UI 的深度休眠（Deep Sleeping Apps）会冻结未加白名单的应用，加入「从不自动休眠」后即可彻底解决。",
    ),
    FLYME(
        id = "meizu",
        shortLabel = "魅族 Flyme",
        fullTitle = "魅族 · Flyme 保活指引",
        autoStartButtonLabel = "打开 Flyme 后台管理",
        steps = listOf(
            KeepAliveStep("1", "后台管理设为允许后台运行", "手机管家 → 权限管理 → 后台管理 → 将「Homing」设为「允许后台运行」。"),
            KeepAliveStep("2", "允许忽略电池优化", "点击上方按钮加入系统电池优化白名单。"),
        ),
        note = "Flyme 多任务界面下滑或长按卡片同样可锁定应用。",
    ),
    ROG_ZUI(
        id = "other_cn",
        shortLabel = "华硕/联想/Moto",
        fullTitle = "华硕 ROG / 联想拯救者 / Moto 保活指引",
        autoStartButtonLabel = "打开智能管家/自启管理",
        steps = listOf(
            KeepAliveStep("1", "开启自启动管理", "智能管家 / 安全中心 → 自启管理 → 允许「Homing」后台自启。"),
            KeepAliveStep("2", "电池设为无限制", "应用信息 → 电池用量 → 选择「无限制」。"),
        ),
        note = "部分游戏手机开启极限电竞模式时会清理非白名单后台，建议将 Homing 加入游戏助手免清理名单。",
    ),
    STOCK_ANDROID(
        id = "stock",
        shortLabel = "Pixel/原生安卓",
        fullTitle = "Google Pixel / Nothing / Sony / 原生 Android 保活指引",
        autoStartButtonLabel = "打开应用电池用量设置",
        steps = listOf(
            KeepAliveStep("1", "电池用量设为「无限制 (Unrestricted)」", "点击上方「一键申请无限制白名单」，或在应用信息 → 应用电池用量中选择「无限制」。"),
            KeepAliveStep("2", "保持常驻通知开启", "Android 13–15 依赖前台消息服务通知保活，请勿在系统设置中关闭「后台连接服务」通知渠道。"),
        ),
        note = "原生 Android 无额外厂商自启动锁，完成电池「无限制」授权后即可开机与后台稳定保活。",
    ),
}

object KeepAliveHelper {
    fun isIgnoringBatteryOptimizations(context: Context): Boolean {
        val pm = context.getSystemService(PowerManager::class.java) ?: return false
        return pm.isIgnoringBatteryOptimizations(context.packageName)
    }

    fun detectCurrentRom(): RomKeepAliveProfile =
        detectRomProfile(
            manufacturer = Build.MANUFACTURER.orEmpty(),
            brand = Build.BRAND.orEmpty(),
            display = Build.DISPLAY.orEmpty(),
            sysProp = ::readSystemProperty,
        )

    internal fun detectRomProfile(
        manufacturer: String,
        brand: String,
        display: String,
        sysProp: (String) -> String = { "" },
    ): RomKeepAliveProfile {
        val fingerprint = "$manufacturer $brand $display".lowercase()
        val hasMiui = sysProp("ro.miui.ui.version.name").isNotBlank() ||
            sysProp("ro.mi.os.version.name").isNotBlank()
        val hasMagic = sysProp("ro.magic.version").isNotBlank() ||
            sysProp("msc.config.magic.version").isNotBlank()
        val hasHarmonyOrEmui = sysProp("hw_sc.build.platform.version").isNotBlank() ||
            sysProp("ro.build.version.emui").isNotBlank()
        val hasVivo = sysProp("ro.vivo.os.version").isNotBlank()
        val hasColorOs = sysProp("ro.build.version.opporom").isNotBlank() ||
            sysProp("ro.build.version.oplusrom").isNotBlank()

        return when {
            hasMiui || anyToken(fingerprint, "xiaomi", "redmi", "poco", "blackshark") ->
                RomKeepAliveProfile.HYPEROS_MIUI
            hasMagic || anyToken(fingerprint, "honor") ->
                RomKeepAliveProfile.MAGICOS
            hasHarmonyOrEmui || anyToken(fingerprint, "huawei") ->
                RomKeepAliveProfile.HARMONY_EMUI
            hasVivo || anyToken(fingerprint, "vivo", "iqoo") ->
                RomKeepAliveProfile.ORIGINOS_FUNTOUCH
            hasColorOs || anyToken(fingerprint, "oppo", "oneplus", "realme", "oplus") ->
                RomKeepAliveProfile.COLOROS_OXYGEN
            anyToken(fingerprint, "samsung") ->
                RomKeepAliveProfile.ONE_UI
            anyToken(fingerprint, "meizu", "flyme") ->
                RomKeepAliveProfile.FLYME
            anyToken(fingerprint, "asus", "lenovo", "motorola", "zuk", "zui") ->
                RomKeepAliveProfile.ROG_ZUI
            else -> RomKeepAliveProfile.STOCK_ANDROID
        }
    }

    private fun anyToken(haystack: String, vararg needles: String): Boolean =
        needles.any { haystack.contains(it) }

    private fun readSystemProperty(key: String): String =
        runCatching {
            val clazz = Class.forName("android.os.SystemProperties")
            val get = clazz.getMethod("get", String::class.java, String::class.java)
            (get.invoke(null, key, "") as? String)?.trim().orEmpty()
        }.getOrDefault("")

    /**
     * Launches Android's 1-tap "Allow Homing to always run in background?" dialog
     * (enabled by REQUEST_IGNORE_BATTERY_OPTIMIZATIONS), gracefully falling back to
     * the system optimization list or app details page on restricted ROMs.
     */
    fun requestIgnoreBatteryOptimizations(context: Context) {
        val pkgUri = Uri.parse("package:${context.packageName}")
        val candidates = listOf(
            Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, pkgUri),
            Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS),
            Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, pkgUri),
        )
        for (intent in candidates) {
            intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            if (runCatching { context.startActivity(intent) }.isSuccess) {
                return
            }
        }
    }

    /**
     * Deep-links into the OEM's proprietary Auto-Start / Background Power Manager,
     * falling back to App Details if the OEM renamed or restricted its internal activity.
     */
    fun openOemAutoStartSettings(context: Context, profile: RomKeepAliveProfile = detectCurrentRom()) {
        val intents = oemIntentsFor(context.packageName, profile)
        for (intent in intents) {
            intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            if (runCatching { context.startActivity(intent) }.isSuccess) {
                return
            }
        }
        val appDetails = Intent(
            Settings.ACTION_APPLICATION_DETAILS_SETTINGS,
            Uri.parse("package:${context.packageName}"),
        ).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        if (runCatching { context.startActivity(appDetails) }.isSuccess) {
            Toast.makeText(
                context,
                "已打开应用信息，请在「电池 / 耗电管理 / 自启动」中设为无限制",
                Toast.LENGTH_LONG,
            ).show()
        }
    }

    internal fun oemIntentsFor(packageName: String, profile: RomKeepAliveProfile): List<Intent> {
        fun comp(pkg: String, cls: String) = Intent().setComponent(ComponentName(pkg, cls))
        return when (profile) {
            RomKeepAliveProfile.HYPEROS_MIUI -> listOf(
                Intent("miui.intent.action.OP_AUTO_START").addCategory(Intent.CATEGORY_DEFAULT),
                comp("com.miui.securitycenter", "com.miui.permcenter.autostart.AutoStartManagementActivity"),
                Intent("miui.intent.action.POWER_HIDE_MODE_ACTIVITY"),
                comp("com.miui.powerkeeper", "com.miui.powerkeeper.ui.HiddenAppsConfigActivity")
                    .putExtra("package_name", packageName)
                    .putExtra("package_label", "Homing"),
            )
            RomKeepAliveProfile.HARMONY_EMUI -> listOf(
                comp("com.huawei.systemmanager", "com.huawei.systemmanager.startupmgr.ui.StartupNormalAppListActivity"),
                comp("com.huawei.systemmanager", "com.huawei.systemmanager.appcontrol.activity.StartupAppControlActivity"),
                comp("com.huawei.systemmanager", "com.huawei.systemmanager.optimize.process.ProtectActivity"),
            )
            RomKeepAliveProfile.MAGICOS -> listOf(
                comp("com.hihonor.systemmanager", "com.hihonor.systemmanager.startupmgr.ui.StartupNormalAppListActivity"),
                comp("com.hihonor.systemmanager", "com.hihonor.systemmanager.optimize.process.ProtectActivity"),
                comp("com.huawei.systemmanager", "com.huawei.systemmanager.startupmgr.ui.StartupNormalAppListActivity"),
            )
            RomKeepAliveProfile.ORIGINOS_FUNTOUCH -> listOf(
                comp("com.vivo.permissionmanager", "com.vivo.permissionmanager.activity.BgStartUpManagerActivity"),
                comp("com.iqoo.secure", "com.iqoo.secure.ui.phoneoptimize.BgStartUpManager"),
                comp("com.iqoo.secure", "com.iqoo.secure.ui.phoneoptimize.AddWhiteListActivity"),
                comp("com.vivo.abe", "com.vivo.applicationbehaviorengine.ui.ExcessivePowerManagerActivity"),
            )
            RomKeepAliveProfile.COLOROS_OXYGEN -> listOf(
                comp("com.oplus.safecenter", "com.oplus.safecenter.startupapp.StartupAppListActivity"),
                comp("com.coloros.safecenter", "com.coloros.safecenter.permission.startup.StartupAppListActivity"),
                comp("com.coloros.safecenter", "com.coloros.safecenter.startupapp.StartupAppListActivity"),
                comp("com.oppo.safe", "com.oppo.safe.permission.startup.StartupAppListActivity"),
                comp("com.oneplus.security", "com.oneplus.security.chainlaunch.view.ChainLaunchAppListActivity"),
            )
            RomKeepAliveProfile.ONE_UI -> listOf(
                comp("com.samsung.android.lool", "com.samsung.android.sm.battery.ui.BatteryActivity"),
                comp("com.samsung.android.lool", "com.samsung.android.sm.ui.battery.BatteryActivity"),
                comp("com.samsung.android.sm", "com.samsung.android.sm.ui.battery.BatteryActivity"),
            )
            RomKeepAliveProfile.FLYME -> listOf(
                Intent("com.meizu.safe.security.SHOW_APPSEC")
                    .addCategory(Intent.CATEGORY_DEFAULT)
                    .putExtra("packageName", packageName),
                comp("com.meizu.safe", "com.meizu.safe.permission.SmartBGActivity"),
            )
            RomKeepAliveProfile.ROG_ZUI -> listOf(
                comp("com.asus.mobilemanager", "com.asus.mobilemanager.autostart.AutoStartActivity"),
                comp("com.asus.mobilemanager", "com.asus.mobilemanager.entry.FunctionActivity"),
                comp("com.zui.safecenter", "com.lenovo.safecenter.MainTab.LeSafeMainActivity"),
            )
            RomKeepAliveProfile.STOCK_ANDROID -> listOf(
                Intent(
                    Settings.ACTION_APPLICATION_DETAILS_SETTINGS,
                    Uri.parse("package:$packageName"),
                ),
            )
        }
    }
}
