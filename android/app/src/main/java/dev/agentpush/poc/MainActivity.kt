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

class MainActivity : ComponentActivity() {
    private val viewModel: AgentViewModel by viewModels()
    private var pendingSessionId by mutableStateOf<String?>(null)
    private val notificationPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) {
        maybeStartService()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        NotificationHelper.createChannels(this)
        pendingSessionId = intent.getStringExtra(NotificationHelper.EXTRA_SESSION_ID)

        setContent {
            AgentInboxTheme {
                AgentApp(
                    viewModel = viewModel,
                    pendingSessionId = pendingSessionId,
                    onPendingSessionConsumed = { pendingSessionId = null },
                )
            }
        }

        requestNotificationsAndStart()
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        pendingSessionId = intent.getStringExtra(NotificationHelper.EXTRA_SESSION_ID)
    }

    private fun requestNotificationsAndStart() {
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
