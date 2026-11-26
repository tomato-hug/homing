package dev.agentpush.poc

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import dev.agentpush.poc.protocol.PairedComputerStore
import dev.agentpush.poc.ui.AgentApp
import dev.agentpush.poc.ui.theme.AgentInboxTheme

private const val STATE_PENDING_PAIR_LINK = "pendingPairLink"

class MainActivity : ComponentActivity() {
    private val viewModel: AgentViewModel by viewModels()
    private var pendingSessionId by mutableStateOf<String?>(null)
    private var pendingPairLink by mutableStateOf<String?>(null)
    private val notificationPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) {
        maybeStartService()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        NotificationHelper.createChannels(this)
        val initialSessionId = intent.getStringExtra(NotificationHelper.EXTRA_SESSION_ID)
        if (savedInstanceState == null) {
            AgentStore.get(this).onAppOpened(requestedSessionId = initialSessionId)
        }
        pendingSessionId = initialSessionId
        pendingPairLink = if (savedInstanceState == null) {
            pairLinkOf(intent)
        } else {
            // A consumed launch link must not navigate back to pairing when
            // Android recreates this Activity for rotation or split screen.
            savedInstanceState.getString(STATE_PENDING_PAIR_LINK)
        }

        setContent {
            AgentInboxTheme {
                AgentApp(
                    viewModel = viewModel,
                    pendingSessionId = pendingSessionId,
                    onPendingSessionConsumed = { pendingSessionId = null },
                    pendingPairLink = pendingPairLink,
                    onPendingPairLinkConsumed = { pendingPairLink = null },
                )
            }
        }

        requestNotificationsAndStart()
    }

    override fun onStart() {
        super.onStart()
        viewModel.refresh()
    }

    override fun onSaveInstanceState(outState: Bundle) {
        outState.putString(STATE_PENDING_PAIR_LINK, pendingPairLink)
        super.onSaveInstanceState(outState)
    }

    override fun onDestroy() {
        if (isFinishing && !isChangingConfigurations) {
            AgentStore.get(this).markAppClosedForWorkspaceReset()
        }
        super.onDestroy()
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        val initialSessionId = intent.getStringExtra(NotificationHelper.EXTRA_SESSION_ID)
        if (!initialSessionId.isNullOrBlank()) {
            AgentStore.get(this).markLiveWorkspaceSession(initialSessionId)
        }
        pendingSessionId = initialSessionId
        pairLinkOf(intent)?.let { pendingPairLink = it }
    }

    /** homing://pair?… 由电脑二维码的落地页或系统相机打开 */
    private fun pairLinkOf(intent: Intent?): String? {
        val data = intent?.data ?: return null
        if (intent.action != Intent.ACTION_VIEW) return null
        return if (data.scheme == "homing" && data.host == "pair") data.toString() else null
    }

    private fun requestNotificationsAndStart() {
        val settings = AppSettings.load(this)
        val hasPairedComputer = PairedComputerStore(this).load().isNotEmpty()
        if (settings.token.isBlank() && !hasPairedComputer) {
            // Defer notification permission prompt until the user pairs a computer.
            return
        }
        if (Build.VERSION.SDK_INT >= 33 &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) !=
            PackageManager.PERMISSION_GRANTED
        ) {
            notificationPermissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
            return
        }
        maybeStartService()
    }

    private fun maybeStartService() {
        val settings = AppSettings.load(this)
        if (settings.token.isBlank() && PairedComputerStore(this).load().isEmpty()) {
            return
        }
        runCatching { viewModel.startPushService() }
    }
}
