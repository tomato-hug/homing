package dev.agentpush.poc.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.agentpush.poc.AgentUiState
import dev.agentpush.poc.AgentViewModel
import dev.agentpush.poc.ConnectionState
import dev.agentpush.poc.HomeFeed
import dev.agentpush.poc.ui.HomingCard
import dev.agentpush.poc.ui.HomingPigeon
import dev.agentpush.poc.ui.PigeonState
import dev.agentpush.poc.ui.theme.HomingPalette
import kotlinx.coroutines.delay

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomingHomeScreen(
    viewModel: AgentViewModel,
    state: AgentUiState,
    home: HomeFeed,
    onOpenSession: (String, String) -> Unit,
    onNewSession: () -> Unit,
    onOpenAllSessions: () -> Unit,
) {
    var searchOpen by rememberSaveable { mutableStateOf(false) }
    var search by rememberSaveable { mutableStateOf("") }
    var attentionExpanded by rememberSaveable { mutableStateOf(false) }
    var now by remember { mutableLongStateOf(System.currentTimeMillis()) }
    var animationKey by remember { mutableIntStateOf(0) }
    var attentionDrafts by remember { mutableStateOf<Map<String, String>>(emptyMap()) }

    LaunchedEffect(home.running.size) {
        now = System.currentTimeMillis()
        if (home.running.isEmpty()) {
            return@LaunchedEffect
        }
        while (true) {
            now = System.currentTimeMillis()
            delay(1_000)
        }
    }
    LaunchedEffect(home.generatedAt) {
        animationKey += 1
    }
    LaunchedEffect(state.mobileInbox.enabled, state.mobileInbox.sinceMillis) {
        attentionExpanded = false
    }

    val query = search.trim()
    val attention = remember(home.attention, query) {
        if (query.isBlank()) {
            home.attention
        } else {
            home.attention.filter { item ->
                item.headline.contains(query, true) ||
                    item.summary.contains(query, true) ||
                    item.session.title.contains(query, true) ||
                    item.session.project.contains(query, true) ||
                    item.session.workspace.contains(query, true) ||
                    item.session.computerName.contains(query, true) ||
                    item.reason.contains(query, true)
            }
        }
    }
    val resume = remember(home.resume, query) {
        if (query.isBlank()) {
            home.resume
        } else {
            home.resume.filter { meta ->
                meta.title.contains(query, true) ||
                    meta.project.contains(query, true) ||
                    meta.workspace.contains(query, true) ||
                    meta.computerName.contains(query, true) ||
                    meta.source.contains(query, true)
            }
        }
    }
    val attentionCount = home.attention.size
    val showComputer = home.computers.size > 1
    val visibleAttention = remember(attention, attentionExpanded) {
        if (attentionExpanded) attention else attention.take(3)
    }
    val hiddenAttentionCount = (attention.size - visibleAttention.size).coerceAtLeast(0)

    Box(modifier = Modifier.fillMaxSize()) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .statusBarsPadding(),
        ) {
            InboxTopBar(
                connection = state.connection,
                transportKind = state.transportKind,
                pigeonState = if (state.isRefreshing) PigeonState.Flap else PigeonState.Idle,
                animationKey = animationKey,
                searchOpen = searchOpen,
                onToggleSearch = {
                    searchOpen = !searchOpen
                    if (!searchOpen) search = ""
                },
                inboxModeEnabled = state.mobileInbox.enabled,
                onToggleInboxMode = {
                    viewModel.setInboxMode(!state.mobileInbox.enabled)
                },
            )
            if (searchOpen) {
                InboxSearchField(value = search, onValueChange = { search = it })
            }
            PullToRefreshBox(
                isRefreshing = state.isRefreshing,
                onRefresh = viewModel::refresh,
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f),
            ) {
                LazyColumn(
                    modifier = Modifier
                        .fillMaxSize()
                        .testTag("homing-home"),
                    contentPadding = PaddingValues(
                        start = 16.dp,
                        end = 16.dp,
                        top = 10.dp,
                        bottom = 104.dp,
                    ),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    item(key = "headline") {
                        Column(modifier = Modifier.padding(top = 4.dp)) {
                            Text(
                                text = when {
                                    !state.mobileInbox.enabled -> "收件箱是空的"
                                    attentionCount > 0 -> "$attentionCount 件新动态"
                                    else -> "暂时没有新动态"
                                },
                                style = MaterialTheme.typography.displaySmall.copy(
                                    fontSize = 28.sp,
                                    lineHeight = 34.sp,
                                    fontWeight = FontWeight.SemiBold,
                                ),
                                color = HomingPalette.Text,
                            )
                            Text(
                                text = when {
                                    !state.mobileInbox.enabled ->
                                        "开启后只接收开启之后的新动态"
                                    home.runningCount > 0 ->
                                        "另有 ${home.runningCount} 个在运行"
                                    else ->
                                        "保持开启，新回复会自动出现"
                                },
                                modifier = Modifier.padding(top = 3.dp),
                                style = MaterialTheme.typography.bodySmall.copy(fontSize = 13.sp),
                                color = HomingPalette.Muted,
                            )
                        }
                    }
                    if (state.connection != ConnectionState.CONNECTED) {
                        item(key = "offline") {
                            HomingCard(borderColor = HomingPalette.Warning.copy(alpha = 0.45f)) {
                                Text(
                                    text = if (state.connection == ConnectionState.RECONNECTING) {
                                        "正在重新连接电脑…"
                                    } else {
                                        "当前离线，显示的是上次同步的首页"
                                    },
                                    modifier = Modifier.padding(horizontal = 13.dp, vertical = 11.dp),
                                    style = MaterialTheme.typography.bodySmall,
                                    color = HomingPalette.Warning,
                                )
                            }
                        }
                    }
                    if (!state.mobileInbox.enabled) {
                        item(key = "mobile-inbox-mode") {
                            MobileInboxModeCard(
                                mode = state.mobileInbox,
                                onToggle = viewModel::setInboxMode,
                            )
                        }
                    }
                    if (state.mobileInbox.enabled && home.running.isNotEmpty()) {
                        item(key = "running") {
                            HomeRunningStrip(
                                sessions = home.running,
                                now = now,
                                showComputer = showComputer,
                                onOpenSession = onOpenSession,
                            )
                        }
                    }
                    if (state.mobileInbox.enabled) {
                        item(key = "attention-label") {
                            HomeSectionLabel("新动态", attention.size)
                        }
                        if (attention.isEmpty()) {
                            item(key = "all-clear") {
                                HomingCard {
                                    Column(
                                        modifier = Modifier
                                            .fillMaxWidth()
                                            .padding(vertical = 20.dp),
                                        horizontalAlignment = Alignment.CenterHorizontally,
                                    ) {
                                        HomingPigeon(
                                            state = PigeonState.Idle,
                                            size = 46.dp,
                                            showTile = false,
                                        )
                                        Text(
                                            text = if (query.isBlank()) {
                                                "暂无新动态"
                                            } else {
                                                "没有匹配的新动态"
                                            },
                                            modifier = Modifier.padding(top = 8.dp),
                                            style = MaterialTheme.typography.titleMedium,
                                            color = HomingPalette.Text,
                                        )
                                        Text(
                                            text = "保持开启，新回复和授权会出现在这里",
                                            modifier = Modifier.padding(top = 4.dp),
                                            style = MaterialTheme.typography.bodySmall,
                                            color = HomingPalette.Muted,
                                        )
                                    }
                                }
                            }
                        } else {
                            items(
                            visibleAttention,
                            key = {
                                "${it.session.computerId}-${it.sessionId}-${it.reason}-${it.eventId}"
                            },
                        ) { item ->
                            HomeAttentionCard(
                                item = item,
                                draft = attentionDrafts[item.sessionId].orEmpty(),
                                showComputer = showComputer,
                                onDraftChange = { value ->
                                    attentionDrafts = attentionDrafts + (item.sessionId to value)
                                },
                                onOpen = {
                                    onOpenSession(item.sessionId, item.session.computerId)
                                },
                                onApprovalOption = { eventId, decision, optionId ->
                                    viewModel.activateComputer(item.session.computerId)
                                    viewModel.decideApproval(
                                        eventId = eventId,
                                        decision = decision,
                                        optionId = optionId,
                                    )
                                    viewModel.markSessionRead(item.sessionId)
                                },
                                onSend = {
                                    val text = attentionDrafts[item.sessionId].orEmpty().trim()
                                    if (text.isNotBlank()) {
                                        viewModel.activateComputer(item.session.computerId)
                                        viewModel.sendReplyTo(
                                            sessionId = item.sessionId,
                                            source = item.session.source,
                                            eventId = item.eventId,
                                            text = text,
                                        )
                                        attentionDrafts = attentionDrafts - item.sessionId
                                    }
                                },
                                onMarkSeen = {
                                    viewModel.activateComputer(item.session.computerId)
                                    viewModel.markSessionRead(item.sessionId)
                                },
                            )
                        }
                            if (hiddenAttentionCount > 0) {
                                item(key = "attention-more") {
                                Surface(
                                    onClick = { attentionExpanded = true },
                                    color = HomingPalette.Raised.copy(alpha = 0.55f),
                                    shape = RoundedCornerShape(10.dp),
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .testTag("attention-show-more"),
                                ) {
                                    Row(
                                        modifier = Modifier.padding(
                                            horizontal = 14.dp,
                                            vertical = 10.dp,
                                        ),
                                        verticalAlignment = Alignment.CenterVertically,
                                    ) {
                                        Text(
                                            text = "还有 $hiddenAttentionCount 件等你处理",
                                            modifier = Modifier.weight(1f),
                                            style = MaterialTheme.typography.bodySmall,
                                            color = HomingPalette.Body,
                                        )
                                        TextButton(onClick = { attentionExpanded = true }) {
                                            Text("查看全部", color = HomingPalette.BrandSoft)
                                        }
                                    }
                                }
                                }
                            } else if (attentionExpanded && attention.size > 3) {
                                item(key = "attention-collapse") {
                                TextButton(
                                    onClick = { attentionExpanded = false },
                                    modifier = Modifier.fillMaxWidth(),
                                ) {
                                    Text("收起较早的动态", color = HomingPalette.Muted)
                                }
                                }
                            }
                        }
                    }
                    if (resume.isNotEmpty()) {
                        item(key = "resume") {
                            ResumeZone(
                                sessions = resume,
                                now = now,
                                showComputer = showComputer,
                                onOpenSession = onOpenSession,
                                onNewSession = onNewSession,
                                onOpenAll = onOpenAllSessions,
                            )
                        }
                    }
                    if (home.earlierCount > 0) {
                        item(key = "earlier") {
                            HomeEarlierZone(
                                count = home.earlierCount,
                                onOpenAll = onOpenAllSessions,
                            )
                        }
                    }
                }
            }
        }
        ExtendedFloatingActionButton(
            onClick = onNewSession,
            modifier = Modifier
                .align(Alignment.BottomEnd)
                .padding(end = 18.dp, bottom = 18.dp)
                .height(56.dp)
                .testTag("new-session-fab"),
            shape = RoundedCornerShape(16.dp),
            containerColor = HomingPalette.Brand,
            contentColor = HomingPalette.Background,
            icon = {
                Icon(Icons.Outlined.Add, contentDescription = null, modifier = Modifier.size(21.dp))
            },
            text = {
                Text("新会话", style = MaterialTheme.typography.labelLarge)
            },
        )
    }
}
