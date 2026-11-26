package dev.agentpush.poc

import android.app.Application
import android.os.Build
import android.util.Log
import kotlinx.coroutines.CoroutineExceptionHandler
import org.json.JSONObject
import java.io.File
import java.time.Instant
import java.util.concurrent.atomic.AtomicBoolean

data class CrashReport(
    val appVersion: String = "",
    val ts: String = "",
    val sessionId: String = "",
    val stack: String = "",
    val device: String = "",
    val thread: String = "",
    val origin: String = "",
    val pendingUpload: Boolean = true,
) {
    fun toJson(): JSONObject {
        return JSONObject()
            .put("app_version", appVersion)
            .put("ts", ts)
            .put("session_id", sessionId)
            .put("stack", stack)
            .put("device", device)
            .put("thread", thread)
            .put("origin", origin)
            .put("pending_upload", pendingUpload)
    }

    companion object {
        fun fromJson(json: JSONObject): CrashReport {
            return CrashReport(
                appVersion = json.optString("app_version", ""),
                ts = json.optString("ts", ""),
                sessionId = json.optString("session_id", ""),
                stack = json.optString("stack", ""),
                device = json.optString("device", ""),
                thread = json.optString("thread", ""),
                origin = json.optString("origin", ""),
                pendingUpload = json.optBoolean("pending_upload", true),
            )
        }
    }
}

object CrashReporter {
    private const val TAG = "AgentCrash"
    private const val PREFS = "agent_push_crash"
    private const val KEY_LAST_SESSION = "last_session_id"
    private const val KEY_ENABLED = "crash_reporting_enabled"
    private val recording = AtomicBoolean(false)

    fun isEnabled(context: android.content.Context): Boolean {
        return context.applicationContext
            .getSharedPreferences(PREFS, Application.MODE_PRIVATE)
            .getBoolean(KEY_ENABLED, true)
    }

    fun setEnabled(context: android.content.Context, enabled: Boolean) {
        context.applicationContext
            .getSharedPreferences(PREFS, Application.MODE_PRIVATE)
            .edit()
            .putBoolean(KEY_ENABLED, enabled)
            .apply()
        if (!enabled) clearLastReport()
    }

    @Volatile
    private var application: Application? = null

    @Volatile
    private var lastSessionId: String = ""

    @Volatile
    private var installed = false

    val coroutineExceptionHandler = CoroutineExceptionHandler { _, throwable ->
        record(throwable, origin = "coroutine", threadName = "coroutine")
    }

    fun install(app: Application) {
        application = app
        lastSessionId = app.getSharedPreferences(PREFS, Application.MODE_PRIVATE)
            .getString(KEY_LAST_SESSION, "")
            .orEmpty()
        if (installed) return
        installed = true
        val previous = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { thread, throwable ->
            record(
                throwable = throwable,
                origin = "uncaught",
                threadName = thread.name,
            )
            previous?.uncaughtException(thread, throwable)
                ?: run {
                    android.os.Process.killProcess(android.os.Process.myPid())
                    kotlin.system.exitProcess(10)
                }
        }
    }

    fun setLastSession(sessionId: String) {
        lastSessionId = sessionId
        application?.getSharedPreferences(PREFS, Application.MODE_PRIVATE)
            ?.edit()
            ?.putString(KEY_LAST_SESSION, sessionId)
            ?.apply()
    }

    fun record(
        throwable: Throwable,
        origin: String,
        threadName: String = Thread.currentThread().name,
    ) {
        if (!recording.compareAndSet(false, true)) return
        try {
            val app = application ?: return
            if (!isEnabled(app)) return
            val report = CrashReport(
                appVersion = appVersion(app),
                ts = Instant.now().toString(),
                sessionId = lastSessionId,
                stack = Log.getStackTraceString(throwable).take(60_000),
                device = deviceLabel(),
                thread = threadName,
                origin = origin,
                pendingUpload = true,
            )
            val file = crashFile(app)
            file.parentFile?.mkdirs()
            file.writeText(report.toJson().toString(2), Charsets.UTF_8)
            Log.e(TAG, "Captured $origin crash\n${report.stack}")
        } catch (error: Throwable) {
            Log.e(TAG, "Unable to persist crash report", error)
        } finally {
            recording.set(false)
        }
    }

    fun loadLastReport(): CrashReport? {
        val app = application ?: return null
        val file = crashFile(app)
        if (!file.isFile) return null
        return runCatching {
            CrashReport.fromJson(JSONObject(file.readText(Charsets.UTF_8)))
        }.getOrNull()
    }

    fun clearLastReport() {
        val app = application ?: return
        runCatching { crashFile(app).delete() }
    }

    suspend fun uploadPending(settings: AppSettings) {
        if (settings.token.isBlank()) return
        val app = application ?: return
        if (!isEnabled(app)) return
        val file = crashFile(app)
        if (!file.isFile) return
        val report = runCatching {
            CrashReport.fromJson(JSONObject(file.readText(Charsets.UTF_8)))
        }.getOrNull() ?: return
        if (!report.pendingUpload) return
        val payload = JSONObject()
            .put("app_version", report.appVersion)
            .put("ts", report.ts)
            .put("session_id", report.sessionId)
            .put("stack", report.stack)
            .put("device", report.device)
            .put("thread", report.thread)
            .put("origin", report.origin)
        val sent = runCatching {
            AgentApi.reportClientLog(settings, payload)
        }.getOrDefault(false)
        if (sent) {
            runCatching {
                file.writeText(
                    report.copy(pendingUpload = false).toJson().toString(2),
                    Charsets.UTF_8,
                )
            }
        }
    }

    fun shareText(report: CrashReport): String {
        return buildString {
            appendLine("Homing ${report.appVersion}")
            appendLine(report.ts)
            appendLine(report.device)
            appendLine("session_id: ${report.sessionId.ifBlank { "(none)" }}")
            appendLine("thread: ${report.thread}")
            appendLine(report.stack)
        }.trimEnd()
    }

    private fun crashFile(app: Application): File {
        return File(File(app.filesDir, "crash"), "last-crash.json")
    }

    private fun appVersion(app: Application): String {
        return runCatching {
            app.packageManager.getPackageInfo(app.packageName, 0).versionName.orEmpty()
        }.getOrDefault(BuildConfig.VERSION_NAME)
    }

    private fun deviceLabel(): String {
        return "${Build.MANUFACTURER} ${Build.MODEL} · ${Build.BRAND} · " +
            "Android ${Build.VERSION.RELEASE} (SDK ${Build.VERSION.SDK_INT}) · ${Build.DISPLAY}"
    }
}
