package dev.agentpush.poc.ui

import android.net.Uri
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import dev.agentpush.poc.AgentViewModel
import dev.agentpush.poc.AppSettings
import dev.agentpush.poc.capabilityTokenIn
import dev.agentpush.poc.protocol.PairedComputerStore
import dev.agentpush.poc.ui.screens.AddComputerScreen
import dev.agentpush.poc.ui.screens.BrowserScreen
import dev.agentpush.poc.ui.screens.HomingSplashScreen
import dev.agentpush.poc.ui.screens.MeScreen
import dev.agentpush.poc.ui.screens.NewSessionScreen
import dev.agentpush.poc.ui.screens.ProjectsScreen
import dev.agentpush.poc.ui.screens.RoomScreen
import dev.agentpush.poc.ui.screens.RoomsScreen
import dev.agentpush.poc.ui.screens.SessionScreen
import dev.agentpush.poc.ui.screens.SettingsScreen
import dev.agentpush.poc.ui.theme.HomingPalette
import java.util.Base64

private const val ROUTE_WORKSPACE = "workspace"
private const val ROUTE_PROJECTS = "projects"
private const val ROUTE_ME = "me"
private const val ROUTE_ROOMS = "rooms"
private const val ROUTE_ROOM = "room/{roomToken}"
private const val ROUTE_SETTINGS = "settings"
private const val ROUTE_ADD_COMPUTER = "add-computer"
private const val ROUTE_NEW_SESSION = "new-session?cwd={cwd}"
private const val ROUTE_SESSION = "session/{sessionToken}"
private const val ROUTE_BROWSER = "browser/{urlToken}/{titleToken}"

internal val LocalRegisterSnackbarHost = staticCompositionLocalOf<(() -> () -> Unit)?> { null }
internal val LocalAppSettings = staticCompositionLocalOf { AppSettings(baseUrl = "", token = "") }

@Composable
fun AgentApp(
    viewModel: AgentViewModel,
    pendingSessionId: String?,
    onPendingSessionConsumed: () -> Unit,
    pendingPairLink: String? = null,
    onPendingPairLinkConsumed: () -> Unit = {},
) {
    val context = LocalContext.current
    val language by LanguagePreferences.language.collectAsStateWithLifecycle()
    val strings = remember(language) { stringsFor(language) }
    val navController = rememberNavController()
    var pairLinkForScreen by remember { mutableStateOf<String?>(null) }
    var workspaceRequestedSessionId by rememberSaveable { mutableStateOf<String?>(null) }
    val snackbarHostState = remember { SnackbarHostState() }
    val snackbarMessage = viewModel.snackbar.collectAsStateWithLifecycle().value
    val appState = viewModel.state.collectAsStateWithLifecycle().value
    val pendingNavigationSession =
        viewModel.pendingNavigationSession.collectAsStateWithLifecycle().value
    val hasConfiguredTarget = remember(appState.settings.token, appState.lastUpdatedAt, appState.activeComputerKey) {
        appState.settings.token.isNotBlank() || PairedComputerStore(context).load().isNotEmpty()
    }
    var splashDone by rememberSaveable {
        mutableStateOf(pendingSessionId != null || pendingPairLink != null || !hasConfiguredTarget)
    }
    var nestedSnackbarHosts by remember { mutableIntStateOf(0) }

    LaunchedEffect(snackbarMessage) {
        val message = snackbarMessage ?: return@LaunchedEffect
        snackbarHostState.showSnackbar(message)
        viewModel.consumeSnackbar()
    }

    LaunchedEffect(pendingPairLink) {
        val link = pendingPairLink ?: return@LaunchedEffect
        pairLinkForScreen = link
        onPendingPairLinkConsumed()
        navController.navigate(ROUTE_ADD_COMPUTER) { launchSingleTop = true }
    }

    LaunchedEffect(pendingSessionId, splashDone) {
        if (!splashDone) return@LaunchedEffect
        val sessionId = pendingSessionId ?: return@LaunchedEffect
        workspaceRequestedSessionId = sessionId
        navController.popBackStack(ROUTE_WORKSPACE, inclusive = false)
        onPendingSessionConsumed()
    }

    LaunchedEffect(pendingNavigationSession, splashDone) {
        if (!splashDone) return@LaunchedEffect
        val sessionId = pendingNavigationSession ?: return@LaunchedEffect
        workspaceRequestedSessionId = sessionId
        navController.popBackStack(ROUTE_WORKSPACE, inclusive = false)
        viewModel.consumePendingNavigation()
    }

    val registerSnackbarHost: () -> () -> Unit = remember {
        {
            nestedSnackbarHosts += 1
            var disposed = false
            {
                if (!disposed) {
                    disposed = true
                    nestedSnackbarHosts = (nestedSnackbarHosts - 1).coerceAtLeast(0)
                }
            }
        }
    }

    CompositionLocalProvider(
        LocalHomingStrings provides strings,
        LocalRegisterSnackbarHost provides registerSnackbarHost,
        LocalAppSettings provides appState.settings,
    ) {
        Box(modifier = Modifier.fillMaxSize()) {
            Scaffold(
                containerColor = HomingPalette.Background,
                contentWindowInsets = WindowInsets(0, 0, 0, 0),
                snackbarHost = {
                    if (nestedSnackbarHosts == 0) {
                        SnackbarHost(snackbarHostState)
                    }
                },
            ) { padding ->
                NavHost(
                    navController = navController,
                    startDestination = ROUTE_WORKSPACE,
                    modifier = Modifier.padding(padding),
                ) {
                    composable(ROUTE_WORKSPACE) {
                        WorkspaceDrawerShell(
                            viewModel = viewModel,
                            state = appState,
                            hasConfiguredTarget = hasConfiguredTarget,
                            snackbarHostState = snackbarHostState,
                            requestedSessionId = workspaceRequestedSessionId,
                            onRequestedSessionConsumed = { workspaceRequestedSessionId = null },
                            onNewSession = { cwd ->
                                navController.navigate(newSessionRoute(cwd))
                            },
                            onAddComputer = {
                                navController.navigate(ROUTE_ADD_COMPUTER)
                            },
                            onOpenRooms = {
                                navController.navigate(ROUTE_ROOMS) { launchSingleTop = true }
                            },
                            onOpenMe = {
                                navController.navigate(ROUTE_ME) { launchSingleTop = true }
                            },
                            onOpenSettings = {
                                navController.navigate(ROUTE_SETTINGS) { launchSingleTop = true }
                            },
                            onOpenBrowser = { url, title ->
                                navController.navigate(browserRoute(url, title))
                            },
                        )
                    }
                    composable(ROUTE_PROJECTS) {
                        ProjectsScreen(
                            viewModel = viewModel,
                            onOpenSession = { sessionId ->
                                workspaceRequestedSessionId = sessionId
                                navController.popBackStack(ROUTE_WORKSPACE, inclusive = false)
                            },
                            onNewSession = { cwd ->
                                navController.navigate(newSessionRoute(cwd))
                            },
                            onOpenSettings = { navController.navigate(ROUTE_SETTINGS) },
                        )
                    }
                    composable(ROUTE_ROOMS) {
                        RoomsScreen(
                            viewModel = viewModel,
                            onOpenRoom = { roomId -> navController.navigate("room/${encodeToken(roomId)}") },
                            onBack = { navController.popBackStack() },
                        )
                    }
                    composable(
                        route = ROUTE_ROOM,
                        arguments = listOf(navArgument("roomToken") { type = NavType.StringType }),
                    ) { entry ->
                        val roomId = decodeToken(entry.arguments?.getString("roomToken").orEmpty())
                        RoomScreen(
                            viewModel = viewModel,
                            roomId = roomId,
                            onBack = { navController.popBackStack() },
                            onOpenSession = { sessionId ->
                                workspaceRequestedSessionId = sessionId
                                navController.popBackStack(ROUTE_WORKSPACE, inclusive = false)
                            },
                        )
                    }
                    composable(ROUTE_ME) {
                        MeScreen(
                            viewModel = viewModel,
                            onOpenSettings = { navController.navigate(ROUTE_SETTINGS) },
                            onAddComputer = { navController.navigate(ROUTE_ADD_COMPUTER) },
                            onBack = { navController.popBackStack() },
                        )
                    }
                    composable(ROUTE_ADD_COMPUTER) {
                        AddComputerScreen(
                            onBack = { navController.popBackStack() },
                            onPaired = { navController.popBackStack() },
                            initialInvite = pairLinkForScreen,
                            onInviteConsumed = { pairLinkForScreen = null },
                        )
                    }
                    composable(
                        route = ROUTE_SESSION,
                        arguments = listOf(navArgument("sessionToken") { type = NavType.StringType }),
                    ) { entry ->
                        val token = entry.arguments?.getString("sessionToken").orEmpty()
                        val sessionId = decodeSessionToken(token)
                        SessionScreen(
                            sessionId = sessionId,
                            viewModel = viewModel,
                            snackbarHostState = snackbarHostState,
                            onBack = { navController.popBackStack() },
                            onOpenBrowser = { url, title ->
                                navController.navigate(browserRoute(url, title))
                            },
                            onOpenSession = { targetSessionId ->
                                workspaceRequestedSessionId = targetSessionId
                                navController.popBackStack(ROUTE_WORKSPACE, inclusive = false)
                            },
                        )
                    }
                    composable(
                        route = ROUTE_NEW_SESSION,
                        arguments = listOf(
                            navArgument("cwd") {
                                type = NavType.StringType
                                defaultValue = ""
                            },
                        ),
                    ) { entry ->
                        NewSessionScreen(
                            viewModel = viewModel,
                            initialCwd = decodeToken(entry.arguments?.getString("cwd").orEmpty()),
                            onBack = { navController.popBackStack() },
                            onCreated = { sessionId ->
                                workspaceRequestedSessionId = sessionId
                                navController.popBackStack(ROUTE_WORKSPACE, inclusive = false)
                            },
                        )
                    }
                    composable(
                        route = ROUTE_BROWSER,
                        arguments = listOf(
                            navArgument("urlToken") { type = NavType.StringType },
                            navArgument("titleToken") { type = NavType.StringType },
                        ),
                    ) { entry ->
                        val url = decodeToken(entry.arguments?.getString("urlToken").orEmpty())
                        val title = decodeToken(entry.arguments?.getString("titleToken").orEmpty())
                        BrowserScreen(
                            url = url,
                            title = title,
                            serverBase = appState.settings.baseUrl,
                            authToken = appState.settings.token,
                            proxyCapability = appState.events.values
                                .asSequence()
                                .flatMap { it.attachments.asSequence() }
                                .mapNotNull { capabilityTokenIn(it.url) }
                                .firstOrNull(),
                            onBack = { navController.popBackStack() },
                        )
                    }
                    composable(ROUTE_SETTINGS) {
                        SettingsScreen(
                            viewModel = viewModel,
                            snackbarHostState = snackbarHostState,
                            onBack = { navController.popBackStack() },
                            onAddComputer = { navController.navigate(ROUTE_ADD_COMPUTER) },
                        )
                    }
                }
            }
            if (!splashDone) {
                HomingSplashScreen(
                    connection = appState.connection,
                    transportKind = appState.transportKind,
                    skipConnectionWait = !hasConfiguredTarget,
                    onFinished = { splashDone = true },
                )
            }
        }
    }
}

private fun sessionRoute(sessionId: String): String {
    val token = encodeToken(sessionId)
    return "session/$token"
}

private fun decodeSessionToken(token: String): String {
    return decodeToken(token)
}

private fun browserRoute(url: String, title: String): String {
    val cleanUrl = url.trim().ifBlank { "about:blank" }
    return "browser/${encodeToken(cleanUrl)}/${encodeToken(title.ifBlank { "网页" })}"
}

private fun newSessionRoute(cwd: String = ""): String {
    return "new-session?cwd=${encodeToken(cwd)}"
}

internal fun encodeToken(value: String): String {
    return Base64.getUrlEncoder()
        .withoutPadding()
        .encodeToString(value.toByteArray(Charsets.UTF_8))
}

internal fun decodeToken(token: String): String {
    return runCatching {
        String(Base64.getUrlDecoder().decode(token), Charsets.UTF_8)
    }.getOrDefault(Uri.decode(token))
}
