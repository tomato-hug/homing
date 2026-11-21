package dev.agentpush.poc

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.compose.runtime.MutableState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = AgentInboxApp::class)
class PairingLaunchLifecycleTest {
    private fun invite(marker: String) = Intent(Intent.ACTION_VIEW,
        Uri.parse("homing://pair?demo=$marker"))

    // Exercise Activity state before attaching Compose, so these lifecycle tests
    // do not start pairing, create device keys, or contact a relay.
    @Suppress("UNCHECKED_CAST")
    private fun pending(activity: MainActivity): MutableState<String?> {
        val field = MainActivity::class.java.getDeclaredField("pendingPairLink\$delegate")
        field.isAccessible = true
        return field.get(activity) as MutableState<String?>
    }

    @Test fun consumedInviteDoesNotReplayAfterActivityRecreation() {
        val intent = invite("demo-first")
        val first = Robolectric.buildActivity(MainActivity::class.java, intent).create()
        assertEquals(intent.dataString, pending(first.get()).value)
        pending(first.get()).value = null // AgentApp has consumed the navigation.
        val saved = Bundle()
        first.saveInstanceState(saved).destroy()

        val recreated = Robolectric.buildActivity(MainActivity::class.java, intent).create(saved)
        try {
            assertNull(pending(recreated.get()).value)
            val next = invite("demo-next")
            recreated.newIntent(next)
            assertEquals(next.dataString, pending(recreated.get()).value)
        } finally {
            recreated.destroy()
        }
    }

    @Test fun unconsumedInviteSurvivesActivityRecreation() {
        val intent = invite("demo-pending")
        val first = Robolectric.buildActivity(MainActivity::class.java, intent).create()
        val saved = Bundle()
        first.saveInstanceState(saved).destroy()
        val recreated = Robolectric.buildActivity(MainActivity::class.java, intent).create(saved)
        try {
            assertEquals(intent.dataString, pending(recreated.get()).value)
        } finally {
            recreated.destroy()
        }
    }
}
