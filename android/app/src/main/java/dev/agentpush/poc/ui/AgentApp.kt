package dev.agentpush.poc.ui

import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.FolderOpen
import androidx.compose.material.icons.outlined.Inbox
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import dev.agentpush.poc.AgentViewModel
import dev.agentpush.poc.SessionStatus
import dev.agentpush.poc.capabilityTokenIn
import dev.agentpush.poc.ui.screens.AddComputerScreen
import dev.agentpush.poc.ui.screens.BrowserScreen
import dev.agentpush.poc.ui.screens.HomingInboxScreen
import dev.agentpush.poc.ui.screens.HomingHomeScreen
import dev.agentpush.poc.ui.screens.HomingSplashScreen
import dev.agentpush.poc.ui.screens.MeScreen
import dev.agentpush.poc.ui.screens.NewSessionScreen
import dev.agentpush.poc.ui.screens.ProjectsScreen
import dev.agentpush.poc.ui.screens.SessionScreen
import dev.agentpush.poc.ui.screens.SettingsScreen
import dev.agentpush.poc.ui.theme.HomingPalette
import java.util.Base64

private const val ROUTE_INBOX = "inbox"
private const val ROUTE_PROJECTS = "projects"
private const val ROUTE_ME = "me"
private const val ROUTE_SETTINGS = "settings"
private const val ROUTE_ADD_COMPUTER = "add-computer"
private const val ROUTE_NEW_SESSION = "new-session?cwd={cwd}"
private const val ROUTE_SESSION = "session/{sessionToken}"
private const val ROUTE_BROWSER = "browser/{urlToken}/{titleToken}"

@Composable
fun AgentApp(
    viewModel: AgentViewModel,
    pendingSessionId: String?,
    onPendingSessionConsumed: () -> Unit,
) {
    val navController = rememberNavController()
    val snackbarHostState = remember { SnackbarHostState() }
    val snackbarMessage = viewModel.snackbar.collectAsStateWithLifecycle().value
    val appState = viewModel.state.collectAsStateWithLifecycle().value
    val pendingNavigationSession =
        viewModel.pendingNavigationSession.collectAsStateWithLifecycle().value
    var splashDone by rememberSaveable {
        mutableStateOf(pendingSessionId != null)
    }

    LaunchedEffect(snackbarMessage) {
        val message = snackbarMessage ?: return@LaunchedEffect
        snackbarHostState.showSnackbar(message)
        viewModel.consumeSnackbar()
    }

    LaunchedEffect(pendingSessionId, splashDone) {
        if (!splashDone) return@LaunchedEffect
        val sessionId = pendingSessionId ?: return@LaunchedEffect
        navController.navigate(sessionRoute(sessionId)) {
            launchSingleTop = true
        }
        onPendingSessionConsumed()
    }

    LaunchedEffect(pendingNavigationSession, splashDone) {
        if (!splashDone) return@LaunchedEffect
        val sessionId = pendingNavigationSession ?: return@LaunchedEffect
        navController.navigate(sessionRoute(sessionId)) {
            launchSingleTop = true
        }
        viewModel.consumePendingNavigation()
    }

    if (!splashDone) {
        HomingSplashScreen(
            connection = appState.connection,
            transportKind = appState.transportKind,
            onFinished = { splashDone = true },
        )
        return
    }

    val backStackEntry by navController.currentBackStackEntryAsState()
    val currentRoute = backStackEntry?.destination?.route
    val showBottomBar = currentRoute == ROUTE_INBOX ||
        currentRoute == ROUTE_PROJECTS ||
        currentRoute == ROUTE_ME
    val visibleHome = remember(appState.home, appState.mobileInbox) {
        appState.home?.let(appState.mobileInbox::visibleHome)
    }
    val attentionCount = remember(appState, visibleHome) {
        val home = visibleHome
        if (home?.available == true) {
            home.attention.size
        } else if (!appState.mobileInbox.enabled) {
            0
        } else {
            appState.sessions().count { session ->
                appState.mobileInbox.hasNewActivity(session) &&
                    !session.archived && !session.isEmpty && (
                    session.status == SessionStatus.NEEDS_PERMISSION ||
                        session.status == SessionStatus.WAITING_INPUT ||
                        session.status == SessionStatus.ERROR ||
                        session.pendingEvent != null
                    )
            }
        }
    }

    Scaffold(
        containerColor = HomingPalette.Background,
        contentWindowInsets = WindowInsets(0, 0, 0, 0),
        snackbarHost = { SnackbarHost(snackbarHostState) },
        bottomBar = {
            if (showBottomBar) {
                HomingBottomBar(
                    currentRoute = currentRoute.orEmpty(),
                    attentionCount = attentionCount,
                    onSelect = { route ->
                        navController.navigate(route) {
                            popUpTo(ROUTE_INBOX) { saveState = true }
                            launchSingleTop = true
                            restoreState = true
                        }
                    },
                )
            }
        },
    ) { padding ->
        NavHost(
            navController = navController,
            startDestination = ROUTE_INBOX,
            modifier = Modifier.padding(padding),
        ) {
            composable(ROUTE_INBOX) {
                val home = visibleHome
                if (home?.available == true) {
                    HomingHomeScreen(
                        viewModel = viewModel,
                        state = appState,
                        home = home,
                        onOpenSession = { sessionId, computerId ->
                            viewModel.activateComputer(computerId)
                            navController.navigate(sessionRoute(sessionId))
                        },
                        onNewSession = {
                            navController.navigate(newSessionRoute())
                        },
                        onOpenAllSessions = {
                            navController.navigate(ROUTE_PROJECTS) {
                                popUpTo(ROUTE_INBOX) { saveState = true }
                                launchSingleTop = true
                            }
                        },
                    )
                } else {
                    HomingInboxScreen(
                        viewModel = viewModel,
                        onOpenSession = { sessionId ->
                            navController.navigate(sessionRoute(sessionId))
                        },
                        onNewSession = {
                            navController.navigate(newSessionRoute())
                        },
                        onOpenAllSessions = {
                            navController.navigate(ROUTE_PROJECTS) {
                                popUpTo(ROUTE_INBOX) { saveState = true }
                                launchSingleTop = true
                            }
                        },
                    )
                }
            }
            composable(ROUTE_PROJECTS) {
                ProjectsScreen(
                    viewModel = viewModel,
                    onOpenSession = { sessionId ->
                        navController.navigate(sessionRoute(sessionId))
                    },
                    onNewSession = { cwd ->
                        navController.navigate(newSessionRoute(cwd))
                    },
                    onOpenSettings = { navController.navigate(ROUTE_SETTINGS) },
                )
            }
            composable(ROUTE_ME) {
                MeScreen(
                    viewModel = viewModel,
                    onOpenSettings = { navController.navigate(ROUTE_SETTINGS) },
                    onAddComputer = { navController.navigate(ROUTE_ADD_COMPUTER) },
                )
            }
            composable(ROUTE_ADD_COMPUTER) {
                AddComputerScreen(
                    onBack = { navController.popBackStack() },
                    onPaired = { navController.popBackStack() },
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
                        navController.navigate(sessionRoute(targetSessionId))
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
                        navController.navigate(sessionRoute(sessionId)) {
                            popUpTo(ROUTE_NEW_SESSION) { inclusive = true }
                        }
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
                )
            }
        }
    }
}

private data class BottomDestination(
    val route: String,
    val label: String,
)

private val bottomDestinations = listOf(
    BottomDestination(ROUTE_INBOX, "收件箱"),
    BottomDestination(ROUTE_PROJECTS, "会话"),
    BottomDestination(ROUTE_ME, "我的"),
)

@Composable
private fun HomingBottomBar(
    currentRoute: String,
    attentionCount: Int,
    onSelect: (String) -> Unit,
) {
    Surface(color = HomingPalette.NavBackground) {
        Column {
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(1.dp)
                    .background(HomingPalette.NavBorder),
            )
            NavigationBar(
                containerColor = HomingPalette.NavBackground,
                tonalElevation = 0.dp,
            ) {
                bottomDestinations.forEach { destination ->
                    val selected = currentRoute == destination.route
                    NavigationBarItem(
                        selected = selected,
                        onClick = { onSelect(destination.route) },
                        icon = {
                            if (destination.route == ROUTE_INBOX && attentionCount > 0) {
                                BadgedBox(
                                    badge = {
                                        Badge(
                                            containerColor = HomingPalette.Warning,
                                            contentColor = HomingPalette.Background,
                                        ) {
                                            Text(
                                                text = if (attentionCount > 99) {
                                                    "99+"
                                                } else {
                                                    attentionCount.toString()
                                                },
                                                fontSize = 9.sp,
                                            )
                                        }
                                    },
                                ) {
                                    BottomIcon(route = destination.route)
                                }
                            } else {
                                BottomIcon(route = destination.route)
                            }
                        },
                        label = {
                            Text(
                                text = destination.label,
                                style = MaterialTheme.typography.labelSmall.copy(fontSize = 11.sp),
                            )
                        },
                        colors = NavigationBarItemDefaults.colors(
                            selectedIconColor = HomingPalette.Brand,
                            selectedTextColor = HomingPalette.Brand,
                            indicatorColor = HomingPalette.Brand.copy(alpha = 0.11f),
                            unselectedIconColor = HomingPalette.Muted,
                            unselectedTextColor = HomingPalette.Muted,
                        ),
                    )
                }
            }
        }
    }
}

@Composable
private fun BottomIcon(route: String) {
    Icon(
        imageVector = when (route) {
            ROUTE_PROJECTS -> Icons.Outlined.FolderOpen
            ROUTE_ME -> Icons.Outlined.Person
            else -> Icons.Outlined.Inbox
        },
        contentDescription = null,
    )
}

private fun sessionRoute(sessionId: String): String {
    val token = encodeToken(sessionId)
    return "session/$token"
}

private fun decodeSessionToken(token: String): String {
    return decodeToken(token)
}

private fun browserRoute(url: String, title: String): String {
    return "browser/${encodeToken(url)}/${encodeToken(title.ifBlank { "网页" })}"
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
