package dev.agentpush.poc

import android.app.Application
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class)
class CrashReporterTest {
    @Test
    fun persistsReportWithSessionVersionDeviceAndStack() {
        val app = ApplicationProvider.getApplicationContext<Application>()
        CrashReporter.install(app)
        CrashReporter.clearLastReport()
        try {
            CrashReporter.setLastSession("session/with:special ?& id")
            CrashReporter.record(
                throwable = IllegalStateException("rendering session detail exploded"),
                origin = "unittest",
                threadName = "main",
            )

            val report = CrashReporter.loadLastReport()
            assertNotNull(report)
            report!!
            assertEquals("session/with:special ?& id", report.sessionId)
            assertEquals("unittest", report.origin)
            assertEquals("main", report.thread)
            assertTrue(report.appVersion.isNotBlank())
            assertTrue(report.stack.contains("IllegalStateException"))
            assertTrue(report.device.contains("Android"))
            assertTrue(report.pendingUpload)
        } finally {
            CrashReporter.clearLastReport()
        }
    }

    @Test
    fun clearRemovesPersistedReport() {
        val app = ApplicationProvider.getApplicationContext<Application>()
        CrashReporter.install(app)
        CrashReporter.record(IllegalArgumentException("boom"), "unittest")
        assertNotNull(CrashReporter.loadLastReport())
        CrashReporter.clearLastReport()
        assertFalse(CrashReporter.loadLastReport() != null)
    }
}
