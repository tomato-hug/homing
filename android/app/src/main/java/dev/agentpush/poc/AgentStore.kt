package dev.agentpush.poc

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import dev.agentpush.poc.protocol.PairedComputer
import dev.agentpush.poc.protocol.PairedComputerStore
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import org.json.JSONObject
import java.time.Instant
import java.util.concurrent.atomic.AtomicLong

class AgentStore private constructor(context: Context) {
    private val applicationContext = context.applicationContext
    private val diagnosticsPreferences = applicationContext.getSharedPreferences(
        DIAGNOSTICS_PREFS,
        Context.MODE_PRIVATE,
    )
    private val sessionFlagsStore = SessionFlagsStore(applicationContext)
    private val mobileInboxPreferences = MobileInboxPreferences(applicationContext)
    private val phonePreferences = PhoneConversationPreferences(applicationContext)
    private val workspacePreferences = applicationContext.getSharedPreferences(
        WORKSPACE_PREFS_NAME,
        Context.MODE_PRIVATE,
    )
    @Volatile
    private var workspaceOpenInProcess = false
    private val _state = MutableStateFlow(
        AgentUiState(
            settings = AppSettings.load(applicationContext),
            connectionDiagnostics = loadDiagnostics(),
            localSessionFlags = sessionFlagsStore.load(),
            mobileInbox = mobileInboxPreferences.load(),
            phoneConversations = phonePreferences.load(),
            activeComputerKey = PairedComputerStore(applicationContext).active()?.deviceId
                ?: AppSettings.load(applicationContext).baseUrl,
        ),
    )
    private val nextLocalReplyId = AtomicLong(0L)
    private val computerGeneration = AtomicLong(0L)
    private val detailRequests = java.util.concurrent.ConcurrentHashMap<String, AtomicLong>()
    private var inboxRefreshJob: Job? = null
    private val draftSaveJobs = mutableMapOf<String, Job>()
    private val itemSaveJobs = mutableMapOf<String, Job>()
    private val outboxMutex = Mutex()
    private val refreshMutex = Mutex()
    // A delivered SSE event may be newer than an unfinished page: never use
    // the visible event map's maximum ID as the bulk synchronization cursor.
    private val eventSyncCursors = mutableMapOf<String, Long>()
    private val itemPersistenceMutex = Mutex()
    private val phoneItemCache = java.util.concurrent.ConcurrentHashMap<String, SessionItemsState>()
    private val phoneDraftCache = java.util.concurrent.ConcurrentHashMap<String, String>()
    private val metadataSyncedAt = java.util.concurrent.ConcurrentHashMap<String, Long>()
    private val unreachableSecondaryComputers = java.util.concurrent.ConcurrentHashMap<String, Long>()
    private val backgroundScope = CoroutineScope(
        SupervisorJob() + Dispatchers.IO + CrashReporter.coroutineExceptionHandler,
    )
    private val localCache = LocalCache(applicationContext)
    private val cloudLink = CloudLinkManager.get(applicationContext)
    private val computerStore = PairedComputerStore(applicationContext)
    private val lanTransport = LanTransport(
        settingsProvider = { _state.value.settings },
    )
    private val transportRouter = TransportRouter(
        lan = lanTransport,
        cloud = CloudTransport(cloudLink),
        probeLan = { lanTransport.probeHealth() },
        cloudAvailable = { cloudLink.preferredComputer() != null },
        canUseLan = { isLocalLanNetworkAvailable(applicationContext) },
    )
    private var aggregateHomeJob: Job? = null

    val state: StateFlow<AgentUiState> = _state.asStateFlow()
    private val localCacheReady = backgroundScope.async {
        restoreLocalCache()
    }

    init {
        AgentApi.bindTransport(transportRouter) { _state.value.settings }
        cloudLink.setNotificationHandler { computerId, payload ->
            val bodyText = payload.previewText.ifBlank { payload.summary }
            if (isIdlePromptNoiseEvent(payload.kind, bodyText)) {
                return@setNotificationHandler true
            }
            ingestNotificationPreview(
                computerId.deviceId.ifBlank { _state.value.activeComputerKey },
                payload,
            )
            if (!shouldSurfaceIncomingSession(payload.sessionId)) {
                return@setNotificationHandler true
            }
            val session = session(payload.sessionId)
            if (session?.muted == true || session?.archived == true) {
                true
            } else {
                runCatching {
                    NotificationHelper.notifyAgentAlert(applicationContext, payload)
                    true
                }.getOrDefault(false)
            }
        }
        backgroundScope.launch {
            runCatching { transportRouter.ensureRoute() }
        }
        backgroundScope.launch {
            transportRouter.kind.collect { kind ->
                _state.update { it.copy(transportKind = kind) }
                if (kind == TransportKind.CLOUD) {
                    if (cloudLink.connected.value) {
                        onConnectionEstablished()
                    } else {
                        onConnectionLost("正在连接云端")
                    }
                }
            }
        }
        backgroundScope.launch {
            cloudLink.connected.collect { connected ->
                if (transportRouter.kind.value != TransportKind.CLOUD) return@collect
                if (connected) {
                    onConnectionEstablished()
                } else {
                    onConnectionLost(
                        cloudLink.statusDetail.value?.takeIf { it.isNotBlank() }
                            ?: "云端连接已断开",
                    )
                }
            }
        }
        backgroundScope.launch {
            cloudLink.statusDetail.collect { detail ->
                if (transportRouter.kind.value != TransportKind.CLOUD) return@collect
                if (detail.isNullOrBlank()) return@collect
                if (!cloudLink.connected.value) {
                    val diagnostics = _state.value.connectionDiagnostics.copy(
                        lastDisconnectReason = detail,
                    )
                    persistDiagnostics(diagnostics)
                    _state.update { current ->
                        current.copy(connectionDiagnostics = diagnostics)
                    }
                } else {
                    _state.update { current ->
                        current.copy(error = detail)
                    }
                }
            }
        }
        backgroundScope.launch {
            cloudLink.events()
                .filter { event ->
                    val active = computerStore.active()?.deviceId.orEmpty()
                    event.computerId.isBlank() ||
                        active.isBlank() ||
                        event.computerId == active
                }
                .collect { event ->
                applyLiveEvent(event.name, event.data)
            }
        }
    }

    internal fun shouldSurfaceIncomingSession(sessionId: String): Boolean {
        val clean = sessionId.trim()
        val current = _state.value
        if (current.away) return true
        if (clean.isBlank()) return false
        return clean in current.mobileWorkspace.liveSessionIds ||
            clean in current.mobileWorkspace.restoredSessionIds
    }

    internal fun ingestNotificationPreview(computerKey: String, payload: NotifyPayload) {
        val bodyCheck = payload.previewText.ifBlank { payload.summary }
        if (isIdlePromptNoiseEvent(payload.kind, bodyCheck)) {
            return
        }
        val resolvedComputer = computerKey.ifBlank { _state.value.activeComputerKey }
        if (payload.eventId > 0 && payload.sessionId.isNotBlank()) {
            updatePhone {
                it.receive(
                    phoneConversationKey(resolvedComputer, payload.source, payload.sessionId),
                    payload.eventId,
                    payload.at,
                )
            }
        }
        // 0-RTT notification snippet: pre-populate local event + session metadata
        // immediately so tapping the notification renders the reply in 0 ms even
        // over high-latency cellular relays.
        val bodyText = payload.previewText.ifBlank { payload.summary }
        if (
            !payload.merged &&
            payload.eventId > 0 &&
            payload.sessionId.isNotBlank() &&
            bodyText.isNotBlank() &&
            (resolvedComputer.isBlank() || resolvedComputer == _state.value.activeComputerKey)
        ) {
            val inferredKind = payload.kind.ifBlank {
                when (payload.level.uppercase()) {
                    "P0" -> "permission"
                    else -> "reply"
                }
            }
            val ts = payload.at.ifBlank { Instant.now().toString() }
            var inserted = false
            _state.update { current ->
                if (current.events.containsKey(payload.eventId)) {
                    return@update current
                }
                inserted = true
                val previewEvent = AgentEvent(
                    id = payload.eventId,
                    ts = ts,
                    source = payload.source,
                    kind = inferredKind,
                    sessionId = payload.sessionId,
                    cwd = current.sessionMeta[payload.sessionId]?.cwd.orEmpty(),
                    project = current.sessionMeta[payload.sessionId]?.project.orEmpty(),
                    title = payload.title,
                    text = bodyText,
                    read = false,
                )
                val incomingMeta = SessionMeta(
                    computerId = resolvedComputer,
                    sessionId = payload.sessionId,
                    source = payload.source,
                    title = payload.title,
                    titleSource = "event",
                    status = SessionStatus.fromKind(inferredKind)?.wire.orEmpty(),
                    lastTs = ts,
                    lastPreview = previewEvent.shortSummary(160),
                    unread = 1,
                )
                current.copy(
                    events = current.events + (payload.eventId to previewEvent),
                    sessionMeta = current.sessionMeta + (
                        payload.sessionId to mergeMeta(
                            current.sessionMeta[payload.sessionId],
                            incomingMeta,
                        )
                        ),
                )
            }
            if (inserted) {
                clearReadForNewContent(payload.sessionId, ts)
                if (shouldSurfaceIncomingSession(payload.sessionId)) {
                    markLiveWorkspaceSession(payload.sessionId, payload.source)
                }
                backgroundScope.launch { persistLocalSnapshot() }
            }
        } else if (payload.sessionId.isNotBlank() && shouldSurfaceIncomingSession(payload.sessionId)) {
            markLiveWorkspaceSession(payload.sessionId, payload.source)
        }
    }

    suspend fun refreshAll() = refreshMutex.withLock { refreshAllLocked() }

    private suspend fun refreshAllLocked() {
        localCacheReady.await()
        _state.update { it.copy(isRefreshing = true, error = null) }
        val settings = _state.value.settings
        val scope = _state.value.activeComputerKey
        val generation = computerGeneration.get()
        val since = synchronized(this) { eventSyncCursors[scope] ?: 0L }
        val nowMillis = System.currentTimeMillis()
        val needMetadataRefresh = _state.value.agentDirectory.agents.isEmpty() ||
            !_state.value.agentCatalog.available ||
            nowMillis - (metadataSyncedAt[scope] ?: 0L) >= METADATA_REFRESH_INTERVAL_MILLIS
        try {
            val bootstrapped = AgentApi.fetchMobileBootstrap(
                settings = settings,
                since = since,
                sessionLimit = MOBILE_SESSION_LIMIT,
                includeMeta = needMetadataRefresh,
            )
            val bundle = if (bootstrapped != null) {
                RefreshBundle(
                    events = bootstrapped.events,
                    sessions = SessionFetch(
                        items = bootstrapped.sessions,
                        apiAvailable = bootstrapped.sessionsOk,
                        error = null,
                    ),
                    projects = bootstrapped.projects,
                    away = bootstrapped.away ?: _state.value.away,
                    awayAuthoritative = bootstrapped.away != null,
                    agentDirectory = bootstrapped.agentDirectory ?: _state.value.agentDirectory,
                    agentCatalog = bootstrapped.agentCatalog ?: _state.value.agentCatalog,
                    inbox = bootstrapped.inbox,
                    home = bootstrapped.home,
                    dispatches = bootstrapped.dispatches,
                )
            } else {
                coroutineScope {
                    val events = async { AgentApi.fetchEventPage(settings, since) }
                    val sessions = async {
                        runCatching { AgentApi.fetchSessions(settings, limit = MOBILE_SESSION_LIMIT) }.fold(
                            onSuccess = { SessionFetch(it, true, null) },
                            onFailure = { error -> SessionFetch(emptyList(), false, error) },
                        )
                    }
                    val projects = async {
                        runCatching { AgentApi.fetchProjects(settings) }.getOrDefault(emptyList())
                    }
                    val awayResult = async {
                        runCatching { AgentApi.getAway(settings) }
                    }
                    val agents = async {
                        if (needMetadataRefresh) {
                            runCatching { AgentApi.fetchAgentDirectory(settings) }
                                .getOrDefault(_state.value.agentDirectory)
                        } else {
                            _state.value.agentDirectory
                        }
                    }
                    val agentCatalog = async {
                        if (needMetadataRefresh) {
                            runCatching { AgentApi.fetchAgentCatalog(settings) }
                                .getOrDefault(_state.value.agentCatalog)
                        } else {
                            _state.value.agentCatalog
                        }
                    }
                    val inbox = async {
                        runCatching { AgentApi.fetchInbox(settings) }
                            .getOrDefault(InboxFeed(available = false))
                    }
                    val home = async {
                        runCatching { AgentApi.fetchHome(settings) }.getOrNull()
                    }
                    val dispatches = async {
                        runCatching { AgentApi.fetchDispatches(settings) }
                            .getOrDefault(emptyList())
                    }
                    val resolvedAway = awayResult.await()
                    RefreshBundle(
                        events = events.await(),
                        sessions = sessions.await(),
                        projects = projects.await(),
                        away = resolvedAway.getOrDefault(_state.value.away),
                        awayAuthoritative = resolvedAway.isSuccess,
                        agentDirectory = agents.await(),
                        agentCatalog = agentCatalog.await(),
                        inbox = inbox.await(),
                        home = home.await(),
                        dispatches = dispatches.await(),
                    )
                }
            }
            synchronized(this) {
                if (_state.value.activeComputerKey != scope || computerGeneration.get() != generation) return
                mergeEvents(bundle.events.events)
                eventSyncCursors[scope] = bundle.events.nextSince
                mergeSessionMeta(
                    sessions = (bundle.sessions.items + bundle.inbox.sessions)
                        .distinctBy { it.sessionId },
                    apiAvailable = bundle.sessions.apiAvailable || bundle.inbox.available,
                    error = bundle.sessions.error,
                )
                mergeDispatches(bundle.dispatches)
                val syncedInbox = if (
                    bundle.awayAuthoritative &&
                    _state.value.mobileInbox.enabled != bundle.away
                ) {
                    mobileInboxPreferences.setEnabled(bundle.away)
                } else {
                    null
                }
                _state.update {
                    val ws = it.mobileWorkspace
                    val nextWorkspace = if (!ws.baselineReady && !bundle.events.hasMore) {
                        ws.copy(
                            baselineMaxEventId = maxOf(ws.baselineMaxEventId, bundle.events.nextSince),
                            baselineReady = true,
                        )
                    } else if (!ws.baselineReady) {
                        ws.copy(
                            baselineMaxEventId = maxOf(ws.baselineMaxEventId, bundle.events.nextSince),
                        )
                    } else {
                        ws
                    }
                    it.copy(
                        projects = bundle.projects,
                        away = bundle.away,
                        mobileInbox = syncedInbox ?: it.mobileInbox,
                        agentDirectory = bundle.agentDirectory,
                        agentCatalog = bundle.agentCatalog,
                        mobileWorkspace = nextWorkspace,
                        isRefreshing = false,
                        error = null,
                        lastUpdatedAt = System.currentTimeMillis(),
                        inbox = bundle.inbox.takeIf { inbox -> inbox.available },
                        home = bundle.home,
                    )
                }
                if (needMetadataRefresh &&
                    (bundle.agentDirectory.agents.isNotEmpty() || bundle.agentCatalog.available)
                ) {
                    metadataSyncedAt[scope] = System.currentTimeMillis()
                }
                receiveVisibleHome(bundle.home)
            }
            persistLocalSnapshot()
            scheduleAggregatedHomeRefresh()
            backgroundScope.launch { flushOutbox() }
            // Display the first bundle immediately, then catch up in bounded
            // requests. Each successfully merged page can be retried safely.
            var page = bundle.events
            while (page.hasMore) {
                if (_state.value.activeComputerKey != scope || computerGeneration.get() != generation) return
                page = AgentApi.fetchEventPage(settings, page.nextSince)
                synchronized(this) {
                    if (_state.value.activeComputerKey != scope || computerGeneration.get() != generation) return
                    mergeEvents(page.events)
                    eventSyncCursors[scope] = page.nextSince
                    _state.update { current ->
                        val ws = current.mobileWorkspace
                        if (!ws.baselineReady) {
                            current.copy(
                                mobileWorkspace = ws.copy(
                                    baselineMaxEventId = maxOf(ws.baselineMaxEventId, page.nextSince),
                                    baselineReady = !page.hasMore,
                                ),
                            )
                        } else {
                            current
                        }
                    }
                }
                persistLocalSnapshot()
            }
        } catch (error: Exception) {
            if (_state.value.activeComputerKey != scope || computerGeneration.get() != generation) return
            _state.update {
                it.copy(
                    isRefreshing = false,
                    error = friendlyError(error),
                )
            }
            throw error
        }
    }

    suspend fun refreshReplies(sessionId: String) {
        val replies = AgentApi.fetchReplies(_state.value.settings, sessionId)
        mergeReplies(replies)
    }

    suspend fun refreshTranscript(sessionId: String, force: Boolean = false) {
        refreshSessionDetails(sessionId, force)
    }

    suspend fun refreshSessionDetails(
        sessionId: String,
        force: Boolean = false,
        includeReasoning: Boolean? = null,
    ) {
        if (sessionId.isBlank()) return
        val scope = _state.value.activeComputerKey
        val generation = computerGeneration.get()
        val requestCounter = detailRequests.getOrPut("$scope:$sessionId") { AtomicLong(0L) }
        val requestId = requestCounter.incrementAndGet()
        fun currentRequest(): Boolean = _state.value.activeComputerKey == scope &&
            computerGeneration.get() == generation && requestCounter.get() == requestId
        val settings = _state.value.settings
        val previousSource = _state.value.sessionMeta[sessionId]?.source.orEmpty()
        val detail = runCatching { AgentApi.fetchSession(settings, sessionId) }.getOrNull()
            ?.takeIf { previousSource.isBlank() || it.source == previousSource }
        if (!currentRequest()) return
        if (detail != null) {
            _state.update { current -> if (!currentRequest()) current else current.copy(sessionMeta = current.sessionMeta +
                (sessionId to mergeMeta(current.sessionMeta[sessionId], detail))) }
        }
        preparePhoneContext(sessionId)
        val existing = _state.value.sessionItems[sessionId] ?: return
        if (!force && System.currentTimeMillis() - existing.loadedAt < 1_500L) return
        val source = _state.value.sessionMeta[sessionId]?.source.orEmpty()
        val showReasoning = includeReasoning ?: existing.includeReasoning
        // A notification for another paired computer may arrive before its event body.
        // Fetch only from the known receipt boundary, never the earlier transcript.
        val anchor = existing.history.anchorEventId
        if (anchor > 0L && _state.value.events[anchor]?.source != source) {
            val received = runCatching { AgentApi.fetchEvents(settings, since = anchor - 1L) }.getOrDefault(emptyList())
                .filter { it.sessionId == sessionId && it.source == source && it.id >= anchor }
            _state.update { current -> if (!currentRequest()) current else current.copy(
                events = current.events + received.associateBy { it.id },
            ) }
            if (!currentRequest()) return
        }
        val result = if (existing.history.anchorEventId > 0L && source.isNotBlank()) {
            runCatching {
                AgentApi.fetchSessionItems(settings, sessionId, includeReasoning = showReasoning,
                    source = source, anchorEventId = existing.history.anchorEventId)
            }.getOrElse { SessionItemsResult(available = false, reason = friendlyError(it)) }
        } else {
            SessionItemsResult(available = false, reason = "收到新消息后可载入更早上下文。")
        }
        if (!currentRequest()) return
        _state.update { current ->
            if (!currentRequest()) return@update current
            val previous = current.sessionItems[sessionId] ?: existing
            if (previous.contextKey != existing.contextKey || previous.history.anchorEventId != existing.history.anchorEventId) return@update current
            val turnState = result.turns.takeIf { it.isNotEmpty() }?.let(SessionItemsState::fromTurns)
            val running = turnState?.running ?: detail?.running ?: previous.running
            val history = if (result.history != null) {
                result.history.copy(
                    beforeCursor = if (previous.history.loadedRounds > 0) previous.history.beforeCursor else result.history.beforeCursor,
                    status = if (previous.history.loadedRounds > 0) previous.history.status else result.history.status,
                    loadedRounds = previous.history.loadedRounds, revision = previous.history.revision,
                    loading = previous.history.loading, error = result.reason,
                )
            } else previous.history.copy(error = result.reason)
            val next = previous.copy(
                items = if (result.available) mergePhoneItems(previous.items, result.items, prepend = false)
                    .filterNot { !showReasoning && it.type == SessionItemType.REASONING } else previous.items,
                turns = result.turns.ifEmpty { previous.turns }, reason = result.reason,
                available = result.available || previous.items.isNotEmpty(),
                loadedAt = System.currentTimeMillis(), running = running,
                runningSince = if (running) turnState?.runningSince?.ifBlank { null }
                    ?: detail?.runningSince?.ifBlank { null } ?: previous.runningSince else "",
                includeReasoning = showReasoning, history = history,
            )
            current.copy(sessionItems = current.sessionItems + (sessionId to next))
        }
        savePhoneItems(sessionId)
    }

    suspend fun loadEarlierContext(sessionId: String) {
        val scope = _state.value.activeComputerKey
        val generation = computerGeneration.get()
        val before = _state.value.sessionItems[sessionId] ?: return
        val cursor = before.history.beforeCursor ?: return
        if (before.history.loading || before.history.anchorEventId <= 0L) return
        val source = _state.value.sessionMeta[sessionId]?.source.orEmpty()
        var claimed = false
        _state.update { current ->
            val latest = current.sessionItems[sessionId] ?: return@update current
            if (latest.history.loading || latest.history.beforeCursor != cursor || latest.history.revision != before.history.revision) {
                claimed = false
                return@update current
            }
            claimed = true
            current.copy(sessionItems = current.sessionItems + (sessionId to latest.copy(history = latest.history.copy(loading = true, error = null))))
        }
        if (!claimed) return
        val response = runCatching {
            AgentApi.fetchSessionItems(_state.value.settings, sessionId, before = cursor,
                includeReasoning = before.includeReasoning, source = source, anchorEventId = before.history.anchorEventId)
        }
        if (_state.value.activeComputerKey != scope || computerGeneration.get() != generation) return
        _state.update { current ->
            if (current.activeComputerKey != scope || computerGeneration.get() != generation) return@update current
            val previous = current.sessionItems[sessionId] ?: return@update current
            if (previous.contextKey != before.contextKey || previous.history.anchorEventId != before.history.anchorEventId ||
                previous.history.beforeCursor != cursor || previous.history.revision != before.history.revision) return@update current
            val result = response.getOrNull()
            val success = result?.available == true && result.history != null && result.roundsReturned in 1..10
            val next = if (success) previous.copy(
                items = mergePhoneItems(previous.items, result!!.items, prepend = true),
                available = true, history = result.history!!.copy(
                    loadedRounds = previous.history.loadedRounds + result.roundsReturned,
                    revision = previous.history.revision + 1,
                ),
            ) else previous.copy(history = previous.history.copy(loading = false,
                error = response.exceptionOrNull()?.let(::friendlyError) ?: result?.reason ?: "暂时无法取得更早的完整对话，请稍后重试。"))
            current.copy(sessionItems = current.sessionItems + (sessionId to next))
        }
        savePhoneItems(sessionId)
    }

    private suspend fun savePhoneItems(sessionId: String) {
        val items = _state.value.sessionItems[sessionId] ?: return
        persistPhoneItems(items)
    }

    private suspend fun persistPhoneItems(items: SessionItemsState) = itemPersistenceMutex.withLock {
        if (items.contextKey.isBlank()) return@withLock
        phoneItemCache[items.contextKey] = items
        // Persist rows first. A failed disk write must never advance the saved cursor.
        if (runCatching { localCache.saveItems(items.contextKey, items.items) }.isSuccess) {
            updatePhone { it.copy(histories = it.histories + (items.contextKey to items.history.copy(loading = false))) }
        }
    }

    suspend fun setSessionReasoning(sessionId: String, enabled: Boolean) {
        if (sessionId.isBlank()) return
        if (enabled) {
            refreshSessionDetails(sessionId, force = true, includeReasoning = true)
            return
        }
        _state.update { current ->
            val existing = current.sessionItems[sessionId] ?: return@update current
            current.copy(
                sessionItems = current.sessionItems + (
                    sessionId to existing.copy(
                        items = existing.items.filterNot {
                            it.type == SessionItemType.REASONING
                        },
                        includeReasoning = false,
                        loadedAt = System.currentTimeMillis(),
                    )
                    ),
            )
        }
    }

    fun applyItem(json: JSONObject) {
        val sessionId = json.stringOrBlank("session_id")
        if (sessionId.isBlank()) return
        val itemJson = json.optJSONObject("item")
        val rawItem = itemJson ?: json
        val managed = _state.value.sessionItems[sessionId]?.history?.managed == true
        if (managed) {
            val timestamp = rawItem.stringOrBlank("ts").ifBlank { rawItem.stringOrBlank("timestamp") }
            if (epochMillis(timestamp) <= 0L) return
            val source = json.stringOrBlank("source").ifBlank { rawItem.stringOrBlank("source") }
            if (source.isNotBlank() && source != _state.value.sessionMeta[sessionId]?.source) return
        }
        val item = if (itemJson != null) {
            SessionItem.fromJson(itemJson, fallbackId = "item-${System.nanoTime()}")
        } else {
            SessionItem.fromJson(json, fallbackId = "item-${System.nanoTime()}")
        } ?: return
        if (
            item.type == SessionItemType.REASONING &&
            _state.value.sessionItems[sessionId]?.includeReasoning != true
        ) {
            return
        }
        _state.update { current ->
            val previous = current.sessionItems[sessionId]
                ?: SessionItemsState(running = true)
            if (previous.history.managed) {
                val receipt = current.phoneConversations.receipts[previous.contextKey]
                val boundary = receipt?.timestamp?.let(::epochMillis) ?: previous.loadedAt
                if (epochMillis(item.ts) <= boundary) return@update current
            }
            current.copy(
                sessionItems = current.sessionItems + (
                    sessionId to appendSessionItem(previous, item)
                    ),
            )
        }
        scheduleItemCache(sessionId)
        scheduleInboxRefresh(delayMillis = 500L)
    }

    fun applyTurn(json: JSONObject) {
        val sessionId = json.stringOrBlank("session_id")
        if (sessionId.isBlank()) return
        val state = json.stringOrBlank("state").lowercase()
        if (state.isBlank()) return
        val turn = SessionTurn(
            state = state,
            ts = json.stringOrBlank("ts"),
        )
        _state.update { current ->
            val previous = current.sessionItems[sessionId]
                ?: SessionItemsState(running = true)
            current.copy(
                sessionItems = current.sessionItems + (
                    sessionId to applyTurnToItems(previous, turn)
                    ),
                sessionMeta = current.sessionMeta.mapValues { (key, meta) ->
                    if (key != sessionId) {
                        meta
                    } else {
                        when (state) {
                            "started", "running" -> meta.copy(
                                status = "running",
                                running = true,
                                runningSince = turn.ts.ifBlank { meta.runningSince },
                            )
                            "failed" -> meta.copy(
                                status = "error",
                                running = false,
                                runningSince = "",
                            )
                            "finished", "complete", "completed" -> meta.copy(
                                status = "done",
                                running = false,
                                runningSince = "",
                            )
                            "idle" -> meta.copy(
                                status = if (SessionStatus.fromWire(meta.status) == SessionStatus.RUNNING) {
                                    "idle"
                                } else {
                                    meta.status
                                },
                                running = false,
                                runningSince = "",
                            )
                            else -> meta
                        }
                    }
                },
            )
        }
        scheduleInboxRefresh(delayMillis = 350L)
    }

    suspend fun refreshProjects() {
        val projects = AgentApi.fetchProjects(_state.value.settings)
        _state.update { it.copy(projects = projects) }
    }

    suspend fun refreshAgentDirectory(): AgentDirectory {
        val directory = AgentApi.fetchAgentDirectory(_state.value.settings)
        _state.update { it.copy(agentDirectory = directory) }
        return directory
    }

    fun mergeCreatedSession(sessionId: String, source: String, cwd: String) {
        val meta = SessionMeta(
            sessionId = sessionId,
            source = source,
            title = projectFromCwd(cwd),
            titleSource = "project",
            project = projectFromCwd(cwd),
            workspace = workspaceFromCwd(cwd),
            cwd = cwd,
            status = "running",
            lastTs = Instant.now().toString(),
            unread = 0,
        )
        val mobileInbox = if (sessionId.isNotBlank()) {
            mobileInboxPreferences.setResident(sessionId, true)
        } else {
            _state.value.mobileInbox
        }
        _state.update {
            it.copy(
                sessionMeta = it.sessionMeta + (sessionId to meta),
                transcriptCache = it.transcriptCache + (sessionId to TranscriptPage()),
                mobileInbox = mobileInbox,
                mobileWorkspace = it.mobileWorkspace.copy(
                    restoredSessionIds = it.mobileWorkspace.restoredSessionIds + sessionId,
                    liveSessionIds = it.mobileWorkspace.liveSessionIds + sessionId,
                    dismissedSessions = it.mobileWorkspace.dismissedSessions - sessionId,
                    wokenAgentIds = if (source.isNotBlank()) {
                        it.mobileWorkspace.wokenAgentIds + source.lowercase()
                    } else {
                        it.mobileWorkspace.wokenAgentIds
                    },
                ),
            )
        }
    }

    fun setInboxMode(enabled: Boolean): MobileInboxMode {
        val next = mobileInboxPreferences.setEnabled(enabled)
        _state.update { it.copy(mobileInbox = next) }
        return next
    }

    fun setSessionResident(sessionId: String, resident: Boolean): MobileInboxMode {
        val next = mobileInboxPreferences.setResident(sessionId, resident)
        _state.update { it.copy(mobileInbox = next) }
        return next
    }

    suspend fun setAgentConnected(agentId: String, connected: Boolean): AgentActionResult {
        val result = AgentApi.setAgentConnected(
            settings = _state.value.settings,
            agentId = agentId,
            connected = connected,
        )
        val catalog = runCatching { AgentApi.fetchAgentCatalog(_state.value.settings) }
            .getOrDefault(_state.value.agentCatalog)
        val directory = runCatching { AgentApi.fetchAgentDirectory(_state.value.settings) }
            .getOrDefault(_state.value.agentDirectory)
        _state.update {
            it.copy(
                agentCatalog = catalog,
                agentDirectory = directory,
            )
        }
        return result
    }

    suspend fun wakeAgent(agentId: String): WakeAgentResult {
        val cleanId = agentId.trim().lowercase()
        val result = AgentApi.wakeAgent(
            settings = _state.value.settings,
            agentId = cleanId,
        )
        val scope = _state.value.activeComputerKey
        metadataSyncedAt.remove(scope)
        _state.update { current ->
            val existing = current.agentDirectory.profile(cleanId)
            val updatedProfile = existing.copy(
                source = cleanId,
                displayName = result.displayName.ifBlank { existing.displayName },
                available = true,
                installed = true,
                processRunning = result.processRunning,
                activeOnline = result.activeOnline,
                canWake = true,
                wakeMode = result.wakeMode.ifBlank { existing.wakeMode },
            )
            current.copy(
                agentDirectory = current.agentDirectory.copy(
                    agents = current.agentDirectory.agents + (cleanId to updatedProfile),
                    available = true,
                ),
                mobileWorkspace = current.mobileWorkspace.copy(
                    wokenAgentIds = current.mobileWorkspace.wokenAgentIds + cleanId,
                ),
            )
        }
        val directory = runCatching { AgentApi.fetchAgentDirectory(_state.value.settings) }
            .getOrNull()
            ?.takeIf { it.available }
        val catalog = runCatching { AgentApi.fetchAgentCatalog(_state.value.settings) }
            .getOrNull()
            ?.takeIf { it.available }
        if (directory != null || catalog != null) {
            _state.update { current ->
                val baseDir = directory ?: current.agentDirectory
                val mergedProfile = baseDir.profile(cleanId).copy(
                    source = cleanId,
                    displayName = baseDir.agents[cleanId]?.displayName?.ifBlank { null }
                        ?: result.displayName.ifBlank { current.agentDirectory.profile(cleanId).displayName },
                    available = true,
                    installed = true,
                    processRunning = baseDir.agents[cleanId]?.processRunning == true || result.processRunning,
                    activeOnline = baseDir.agents[cleanId]?.activeOnline == true || result.activeOnline,
                    canWake = true,
                    wakeMode = baseDir.agents[cleanId]?.wakeMode?.ifBlank { null }
                        ?: result.wakeMode.ifBlank { current.agentDirectory.profile(cleanId).wakeMode },
                )
                current.copy(
                    agentDirectory = baseDir.copy(
                        agents = baseDir.agents + (cleanId to mergedProfile),
                        available = true,
                    ),
                    agentCatalog = catalog ?: current.agentCatalog,
                )
            }
        }
        return result
    }

    fun onAppOpened(requestedSessionId: String? = null) {
        val pendingReset = workspacePreferences.getBoolean(KEY_PENDING_RESET_ON_OPEN, false)
        if (!workspaceOpenInProcess || pendingReset) {
            resetWorkspaceOnAppOpen(keepSessionId = requestedSessionId)
        }
        workspaceOpenInProcess = true
        workspacePreferences.edit()
            .putBoolean(KEY_PENDING_RESET_ON_OPEN, false)
            .apply()
    }

    fun markAppClosedForWorkspaceReset() {
        workspaceOpenInProcess = false
        workspacePreferences.edit()
            .putBoolean(KEY_PENDING_RESET_ON_OPEN, true)
            .remove(KEY_LAST_ACTIVE_SESSION_PREF)
            .apply()
    }

    fun resetWorkspaceOnAppOpen(
        keepSessionId: String? = null,
        nowMillis: Long = System.currentTimeMillis(),
    ) {
        val keep = keepSessionId?.trim()?.takeIf { it.isNotBlank() }?.let { setOf(it) } ?: emptySet()
        workspacePreferences.edit().apply {
            putBoolean(KEY_PENDING_RESET_ON_OPEN, false)
            if (keep.isEmpty()) {
                remove(KEY_LAST_ACTIVE_SESSION_PREF)
            } else {
                putString(KEY_LAST_ACTIVE_SESSION_PREF, keep.first())
            }
        }.apply()
        _state.update { current ->
            val maxEvent = current.events.keys.maxOrNull() ?: 0L
            val hasLoadedOnce = current.lastUpdatedAt != null || maxEvent > 0L
            current.copy(
                mobileWorkspace = MobileWorkspaceState(
                    clearedAtEpochMs = nowMillis,
                    baselineMaxEventId = maxEvent,
                    baselineReady = hasLoadedOnce,
                    restoredSessionIds = keep,
                    liveSessionIds = keep,
                    dismissedSessions = emptyMap(),
                    wokenAgentIds = emptySet(),
                    undo = null,
                ),
            )
        }
    }

    internal fun markLiveWorkspaceSession(sessionId: String, @Suppress("UNUSED_PARAMETER") source: String = "") {
        val clean = sessionId.trim()
        if (clean.isBlank()) return
        _state.update { current ->
            val ws = current.mobileWorkspace
            if (clean in ws.liveSessionIds && clean !in ws.dismissedSessions) {
                return@update current
            }
            // A new event only surfaces its session. It must not mark the agent as
            // "woken": activity status comes from the Mac's process probe or an
            // explicit wake from this phone.
            current.copy(
                mobileWorkspace = ws.copy(
                    liveSessionIds = ws.liveSessionIds + clean,
                    dismissedSessions = ws.dismissedSessions - clean,
                ),
            )
        }
    }

    fun quickClearWorkspaceSessions(
        candidates: List<SessionSummary>,
        nowMillis: Long = System.currentTimeMillis(),
    ): Int {
        val clearable = candidates.filterNot { session ->
            session.running ||
                session.status == SessionStatus.NEEDS_PERMISSION ||
                session.status == SessionStatus.WAITING_INPUT ||
                session.status == SessionStatus.ERROR ||
                session.pendingEvent != null
        }
        if (clearable.isEmpty()) return 0
        val clearableIds = clearable.map { it.sessionId }.toSet()
        val dismissedMap = clearable.associate { it.sessionId to it.phoneActivity() }
        _state.update { current ->
            val ws = current.mobileWorkspace
            val snapshot = MobileWorkspaceUndo(
                clearedAtEpochMs = ws.clearedAtEpochMs,
                baselineMaxEventId = ws.baselineMaxEventId,
                baselineReady = ws.baselineReady,
                restoredSessionIds = ws.restoredSessionIds,
                liveSessionIds = ws.liveSessionIds,
                dismissedSessions = ws.dismissedSessions,
            )
            val maxEvent = current.events.keys.maxOrNull() ?: ws.baselineMaxEventId
            current.copy(
                mobileWorkspace = ws.copy(
                    clearedAtEpochMs = maxOf(ws.clearedAtEpochMs, nowMillis),
                    baselineMaxEventId = maxOf(ws.baselineMaxEventId, maxEvent),
                    baselineReady = true,
                    restoredSessionIds = ws.restoredSessionIds - clearableIds,
                    liveSessionIds = ws.liveSessionIds - clearableIds,
                    dismissedSessions = ws.dismissedSessions + dismissedMap,
                    undo = snapshot,
                ),
            )
        }
        return clearable.size
    }

    fun dismissWorkspaceSession(session: SessionSummary): Boolean {
        if (
            session.running ||
            session.status == SessionStatus.NEEDS_PERMISSION ||
            session.status == SessionStatus.WAITING_INPUT ||
            session.status == SessionStatus.ERROR ||
            session.pendingEvent != null
        ) {
            return false
        }
        _state.update { current ->
            val ws = current.mobileWorkspace
            val snapshot = MobileWorkspaceUndo(
                clearedAtEpochMs = ws.clearedAtEpochMs,
                baselineMaxEventId = ws.baselineMaxEventId,
                baselineReady = ws.baselineReady,
                restoredSessionIds = ws.restoredSessionIds,
                liveSessionIds = ws.liveSessionIds,
                dismissedSessions = ws.dismissedSessions,
            )
            current.copy(
                mobileWorkspace = ws.copy(
                    restoredSessionIds = ws.restoredSessionIds - session.sessionId,
                    liveSessionIds = ws.liveSessionIds - session.sessionId,
                    dismissedSessions = ws.dismissedSessions + (session.sessionId to session.phoneActivity()),
                    undo = snapshot,
                ),
            )
        }
        return true
    }

    fun undoWorkspaceCleanup(): Boolean {
        var restored = false
        _state.update { current ->
            val snap = current.mobileWorkspace.undo ?: return@update current
            restored = true
            current.copy(
                mobileWorkspace = current.mobileWorkspace.copy(
                    clearedAtEpochMs = snap.clearedAtEpochMs,
                    baselineMaxEventId = snap.baselineMaxEventId,
                    baselineReady = snap.baselineReady,
                    restoredSessionIds = snap.restoredSessionIds,
                    liveSessionIds = snap.liveSessionIds,
                    dismissedSessions = snap.dismissedSessions,
                    undo = null,
                ),
            )
        }
        return restored
    }

    suspend fun restoreHistoricalSessions(
        sessionIds: List<String>,
        itemLimit: Int = 10,
    ): Int {
        val cleanIds = sessionIds.map { it.trim() }.filter { it.isNotBlank() }.distinct()
        if (cleanIds.isEmpty()) return 0
        val limit = itemLimit.coerceIn(1, 50)
        val settings = _state.value.settings
        val scope = _state.value.activeComputerKey

        _state.update { current ->
            val ws = current.mobileWorkspace
            current.copy(
                mobileWorkspace = ws.copy(
                    restoredSessionIds = ws.restoredSessionIds + cleanIds,
                    dismissedSessions = ws.dismissedSessions - cleanIds.toSet(),
                ),
            )
        }

        for (sessionId in cleanIds) {
            val detail = runCatching { AgentApi.fetchSession(settings, sessionId) }.getOrNull()
            if (detail != null) {
                _state.update { current ->
                    current.copy(
                        sessionMeta = current.sessionMeta + (
                            sessionId to mergeMeta(current.sessionMeta[sessionId], detail)
                            ),
                    )
                }
            }
            val source = _state.value.sessionMeta[sessionId]?.source
                ?.takeIf { it.isNotBlank() }
                ?: _state.value.session(sessionId)?.source.orEmpty()

            val itemsResult = runCatching {
                AgentApi.fetchSessionItems(
                    settings = settings,
                    sessionId = sessionId,
                    limit = limit,
                    includeReasoning = false,
                    source = source,
                    anchorEventId = null,
                )
            }.getOrNull()

            val latestItems = if (itemsResult != null && itemsResult.items.isNotEmpty()) {
                itemsResult.items.takeLast(limit)
            } else {
                val transcript = runCatching {
                    AgentApi.fetchTranscript(settings, sessionId, limit = limit)
                }.getOrNull()
                transcript?.messages.orEmpty().takeLast(limit).mapIndexed { index, msg ->
                    val role = msg.role.lowercase()
                    val itemType = if (role == "user") SessionItemType.USER else SessionItemType.ASSISTANT
                    SessionItem(
                        id = "restored-$sessionId-$index",
                        ts = msg.ts.ifBlank { Instant.now().toString() },
                        type = itemType,
                        role = role,
                        text = msg.text,
                        itemKey = "restored:$sessionId:$index:${msg.ts}",
                    )
                }
            }

            preparePhoneContext(sessionId)
            val currentAfterPrepare = _state.value
            val session = currentAfterPrepare.session(sessionId)
            val contextKey = phoneConversationKey(
                session?.computerId?.ifBlank { scope } ?: scope,
                source,
                sessionId,
            )
            val receipt = currentAfterPrepare.phoneConversations.receipts[contextKey]
            val turnState = itemsResult?.turns?.takeIf { it.isNotEmpty() }?.let(SessionItemsState::fromTurns)
            val nextItemsState = SessionItemsState(
                items = latestItems,
                turns = itemsResult?.turns.orEmpty(),
                available = latestItems.isNotEmpty(),
                loadedAt = System.currentTimeMillis(),
                running = turnState?.running ?: detail?.running ?: session?.running ?: false,
                runningSince = turnState?.runningSince?.ifBlank { null }
                    ?: detail?.runningSince.orEmpty(),
                includeReasoning = false,
                contextKey = contextKey,
                history = SessionHistoryState(
                    managed = true,
                    anchorEventId = receipt?.eventId ?: 0L,
                    status = "restored_latest_$limit",
                    loadedRounds = 1,
                    revision = (currentAfterPrepare.sessionItems[sessionId]?.history?.revision ?: 0) + 1,
                ),
            )
            _state.update { current ->
                current.copy(
                    sessionItems = current.sessionItems + (sessionId to nextItemsState),
                )
            }
            savePhoneItems(sessionId)
        }
        return cleanIds.size
    }

    suspend fun refreshAway() {
        runCatching {
            val away = AgentApi.getAway(_state.value.settings)
            _state.update { it.copy(away = away) }
        }
    }

    fun ingestEvent(event: AgentEvent): Boolean {
        if (isIdlePromptNoiseEvent(event.kind, event.text)) {
            return false
        }
        if (event.sessionId.isNotBlank()) {
            updatePhone { it.receive(phoneConversationKey(_state.value.activeComputerKey, event.source, event.sessionId), event.id, event.ts) }
        }
        var isNew = false
        _state.update { current ->
            val existing = current.events[event.id]
            isNew = existing == null
            val merged = if (existing != null) {
                event.copy(read = existing.read || event.read)
            } else {
                event
            }
            val eventMeta = event.sessionMeta
            val minimalMeta = if (event.sessionId.isBlank()) {
                null
            } else {
                SessionMeta(
                    sessionId = event.sessionId,
                    source = event.source,
                    title = event.title,
                    titleSource = "event",
                    project = event.project,
                    workspace = workspaceFromCwd(event.cwd),
                    cwd = event.cwd,
                    status = SessionStatus.fromKind(event.kind)?.wire.orEmpty(),
                    lastTs = event.ts,
                    lastPreview = event.shortSummary(120),
                    unread = 0,
                )
            }
            val mergedMeta = when {
                eventMeta != null -> mergeMeta(
                    current.sessionMeta[event.sessionId],
                    eventMeta.copy(
                        sessionId = event.sessionId,
                        source = event.source.ifBlank { eventMeta.source },
                        cwd = eventMeta.cwd.ifBlank { event.cwd },
                        project = eventMeta.project.ifBlank { event.project },
                        workspace = eventMeta.workspace.ifBlank {
                            workspaceFromCwd(event.cwd)
                        },
                    ),
                )
                current.sessionMeta[event.sessionId] == null &&
                    !current.sessionApiAvailable -> minimalMeta
                else -> null
            }
            current.copy(
                events = current.events + (event.id to merged),
                sessionMeta = if (mergedMeta != null && event.sessionId.isNotBlank()) {
                    current.sessionMeta + (event.sessionId to mergedMeta)
                } else {
                    current.sessionMeta
                },
            )
        }
        event.sessionMeta?.let { meta ->
            cacheRemoteFlags(listOf(meta))?.let { flags ->
                _state.update { it.copy(localSessionFlags = flags) }
            }
        }
        if (isNew && event.sessionId.isNotBlank()) {
            clearReadForNewContent(event.sessionId, event.ts)
            if (shouldSurfaceIncomingSession(event.sessionId)) {
                markLiveWorkspaceSession(event.sessionId, event.source)
            }
            scheduleInboxRefresh()
            backgroundScope.launch {
                runCatching { refreshTranscript(event.sessionId) }
                persistLocalSnapshot()
            }
        }
        return isNew
    }

    fun applySessionUpdate(json: JSONObject) {
        val sessionId = json.stringOrBlank("session_id").ifBlank { json.stringOrBlank("id") }
        if (sessionId.isBlank()) return
        val incoming = SessionMeta.fromJson(json, fallbackSessionId = sessionId)
        val remoteFlags = cacheRemoteFlags(listOf(incoming))
        val running = if (json.has("running") && !json.isNull("running")) {
            json.optBoolean("running", false)
        } else {
            null
        }
        _state.update { current ->
            val items = current.sessionItems[sessionId]
            current.copy(
                sessionMeta = current.sessionMeta + (
                    sessionId to mergeMeta(current.sessionMeta[sessionId], incoming)
                    ),
                localSessionFlags = remoteFlags ?: current.localSessionFlags,
                sessionApiAvailable = true,
                sessionItems = if (items != null && running != null) {
                    current.sessionItems + (
                        sessionId to items.copy(
                            running = running,
                            runningSince = if (running) {
                                incoming.runningSince.ifBlank { items.runningSince }
                            } else {
                                ""
                            },
                        )
                        )
                } else {
                    current.sessionItems
                },
            )
        }
        scheduleInboxRefresh(delayMillis = 300L)
    }

    fun applyAwayUpdate(json: JSONObject) {
        val enabled = json.optBoolean("enabled", false)
        val period = AwayPeriod.fromJson(
            json.optJSONObject("current") ?: json.optJSONObject("period"),
        )
        val nextInbox = mobileInboxPreferences.setEnabled(enabled)
        val nowMillis = System.currentTimeMillis()
        _state.update { current ->
            val turningOn = enabled && !current.away
            val maxEvent = current.events.keys.maxOrNull() ?: current.mobileWorkspace.baselineMaxEventId
            val nextWorkspace = if (turningOn) {
                current.mobileWorkspace.copy(
                    clearedAtEpochMs = maxOf(current.mobileWorkspace.clearedAtEpochMs, nowMillis),
                    baselineMaxEventId = maxOf(current.mobileWorkspace.baselineMaxEventId, maxEvent),
                    baselineReady = true,
                    liveSessionIds = emptySet(),
                )
            } else {
                current.mobileWorkspace
            }
            val nextHome = current.home?.copy(
                away = HomeAway(
                    enabled = enabled,
                    current = if (enabled) period ?: current.home.away.current else null,
                    period = period ?: current.home.away.period,
                ),
            )
            current.copy(
                away = enabled,
                mobileInbox = nextInbox,
                mobileWorkspace = nextWorkspace,
                home = nextHome,
            )
        }
    }

    fun applyLiveEvent(eventName: String, json: JSONObject) {
        when (eventName) {
            "away" -> applyAwayUpdate(json)
            "session" -> applySessionUpdate(json)
            "item" -> applyItem(json)
            "turn" -> applyTurn(json)
            "reply_status" -> applyReplyStatus(json)
            "dispatch" -> applyDispatchUpdate(json)
            "notify" -> Unit
            "", "message" -> {
                val event = AgentEvent.fromJson(json)
                if (event.id > 0) ingestEvent(event)
            }
        }
    }

    fun mergeDispatches(dispatches: List<AgentDispatch>) {
        if (dispatches.isEmpty()) return
        _state.update { current ->
            val merged = current.dispatches.toMutableMap()
            for (incoming in dispatches) {
                if (incoming.id.isBlank()) continue
                val existing = merged[incoming.id]
                if (
                    existing != null &&
                    existing.updatedAt.isNotBlank() &&
                    incoming.updatedAt.isNotBlank() &&
                    existing.updatedAt > incoming.updatedAt
                ) {
                    continue
                }
                merged[incoming.id] = incoming
            }
            val trimmed = merged.values
                .sortedByDescending { it.createdAt }
                .take(200)
                .associateBy { it.id }
            current.copy(dispatches = trimmed)
        }
    }

    fun applyDispatchUpdate(json: JSONObject) {
        val dispatch = AgentDispatch.fromJson(json)
        if (dispatch.id.isBlank()) return
        mergeDispatches(listOf(dispatch))
    }

    fun applyReplyStatus(json: JSONObject) {
        val replyId = json.optLong("reply_id", 0L)
        val eventId = json.optLong("event_id", 0L)
        val sessionId = json.stringOrBlank("session_id")
        val status = ReplyStatus.fromWire(json.stringOrBlank("status"))
        val message = json.stringOrBlank("message").takeIf { it.isNotBlank() }
        _state.update { current ->
            val byId = if (replyId > 0) current.replies[replyId] else null
            val target = byId ?: current.replies.values
                .filter {
                    if (replyId > 0) {
                        it.isLocal
                    } else {
                        true
                    }
                }
                .filter {
                    (eventId == 0L || it.eventId == eventId) &&
                        (sessionId.isBlank() || it.sessionId == sessionId)
                }
                .maxByOrNull { it.ts }
                ?: return@update current
            val canonicalId = if (replyId > 0) replyId else target.id
            val incoming = target.copy(
                id = canonicalId,
                status = status,
                message = message,
                isLocal = false,
            )
            val updated = reconcileIncomingReply(target, incoming)
            current.copy(
                replies = current.replies
                    .minus(target.id)
                    .plus(updated.id to updated),
            )
        }
    }

    suspend fun testConnection(baseUrl: String, token: String): String {
        val settings = AppSettings(
            baseUrl = AppSettings.normalizeBaseUrl(baseUrl),
            token = token.trim(),
        )
        return AgentApi.probe(settings)
    }

    fun updateDraft(sessionId: String, text: String) {
        val before = _state.value
        val meta = before.sessionMeta[sessionId]
        val key = phoneConversationKey(meta?.computerId?.ifBlank { before.activeComputerKey } ?: before.activeComputerKey,
            meta?.source.orEmpty(), sessionId)
        if (text.isBlank()) phoneDraftCache.remove(key) else phoneDraftCache[key] = text

        if (sessionId.isBlank()) return
        _state.update { current ->
            current.copy(
                sessionDrafts = if (text.isBlank()) {
                    current.sessionDrafts - sessionId
                } else {
                    current.sessionDrafts + (sessionId to text)
                },
            )
        }
        synchronized(draftSaveJobs) {
            draftSaveJobs.remove(key)?.cancel()
            draftSaveJobs[key] = backgroundScope.launch {
                delay(350)
                runCatching { localCache.saveDraft(key, text.trimEnd()) }
                synchronized(draftSaveJobs) { draftSaveJobs.remove(key) }
            }
        }
    }

    private fun scheduleItemCache(sessionId: String) {
        val snapshot = _state.value.sessionItems[sessionId] ?: return
        val key = snapshot.contextKey
        if (key.isBlank()) return
        synchronized(itemSaveJobs) {
            itemSaveJobs.remove(key)?.cancel()
            itemSaveJobs[key] = backgroundScope.launch {
                delay(500)
                val latest = _state.value.sessionItems[sessionId]?.takeIf { it.contextKey == key }
                    ?: phoneItemCache[key] ?: snapshot
                persistPhoneItems(latest)
                synchronized(itemSaveJobs) { itemSaveJobs.remove(key) }
            }
        }
    }

    fun clearDraft(sessionId: String) {
        updateDraft(sessionId, "")
    }

    suspend fun clearLocalCache() {
        synchronized(draftSaveJobs) {
            draftSaveJobs.values.forEach { it.cancel() }
            draftSaveJobs.clear()
        }
        synchronized(itemSaveJobs) {
            itemSaveJobs.values.forEach { it.cancel() }
            itemSaveJobs.clear()
        }
        localCache.clear()
        phoneItemCache.clear()
        phoneDraftCache.clear()
        updatePhone { it.copy(histories = emptyMap()) }
        _state.update {
            it.copy(
                sessionDrafts = emptyMap(),
                inbox = null,
                projects = emptyList(),
                sessionMeta = emptyMap(),
                transcriptCache = emptyMap(),
                sessionItems = emptyMap(),
                lastUpdatedAt = null,
            )
        }
    }

    @Synchronized fun saveSettings(baseUrl: String, token: String): AppSettings {
        val saved = AppSettings.save(
            applicationContext,
            baseUrl,
            token,
            _state.value.settings.previewMentions,
        )
        val previous = _state.value
        if (saved.baseUrl != previous.settings.baseUrl && computerStore.active() == null) {
            rememberPhoneState(previous)
            computerGeneration.incrementAndGet()
            _state.update { it.copy(settings = saved, activeComputerKey = saved.baseUrl,
                sessionMeta = emptyMap(), events = emptyMap(), replies = emptyMap(), sessionItems = emptyMap(),
                transcriptCache = emptyMap(), sessionDrafts = emptyMap(), home = null, inbox = null, isRefreshing = false) }
        } else _state.update { it.copy(settings = saved) }
        invalidateTransportRoute()
        return saved
    }

    fun setPreviewMentions(enabled: Boolean): AppSettings {
        val current = _state.value.settings
        val saved = AppSettings.save(
            applicationContext,
            current.baseUrl,
            current.token,
            enabled,
        )
        _state.update { it.copy(settings = saved) }
        return saved
    }

    fun invalidateTransportRoute() {
        transportRouter.invalidate()
        backgroundScope.launch {
            runCatching { transportRouter.ensureRoute(force = true) }
        }
    }

    fun refreshCloudComputers() {
        cloudLink.refreshComputers()
        if (!cloudLink.connected.value) {
            cloudLink.reconnectNow()
        }
        computerStore.active()?.deviceId?.let(::activateComputer)
        invalidateTransportRoute()
    }

    @Synchronized fun activateComputer(deviceId: String) {
        if (deviceId.isBlank()) return
        if (deviceId == _state.value.activeComputerKey) return
        if (computerStore.load().none { it.deviceId == deviceId }) return
        rememberPhoneState(_state.value)
        computerGeneration.incrementAndGet()
        eventSyncCursors.remove(deviceId)
        computerStore.select(deviceId)
        cloudLink.selectComputer(deviceId)
        _state.update { it.copy(activeComputerKey = deviceId, sessionMeta = emptyMap(), events = emptyMap(),
            replies = emptyMap(), sessionItems = emptyMap(), transcriptCache = emptyMap(),
            sessionDrafts = emptyMap(), dispatches = emptyMap(), inbox = null, home = null,
            projects = emptyList(), isRefreshing = false) }
        transportRouter.invalidate()
    }

    private fun rememberPhoneState(state: AgentUiState) {
        state.sessionItems.values.filter { it.contextKey.isNotBlank() }.forEach { items ->
            phoneItemCache[items.contextKey] = items
            backgroundScope.launch { persistPhoneItems(items) }
        }
    }

    fun activatePhoneSession(meta: SessionMeta) {
        activateComputer(meta.computerId)
        _state.update { current ->
            val changedSource = current.sessionMeta[meta.sessionId]?.source?.let { it != meta.source } == true
            if (changedSource) current.sessionItems[meta.sessionId]?.takeIf { it.contextKey.isNotBlank() }
                ?.let { phoneItemCache[it.contextKey] = it }
            current.copy(sessionMeta = current.sessionMeta + (meta.sessionId to meta),
                sessionItems = if (changedSource) current.sessionItems - meta.sessionId else current.sessionItems,
                sessionDrafts = if (changedSource) current.sessionDrafts - meta.sessionId else current.sessionDrafts)
        }
        preparePhoneContext(meta.sessionId)
    }

    @Synchronized private fun updatePhone(transform: (PhoneConversationState) -> PhoneConversationState) {
        _state.update { it.copy(phoneConversations = transform(it.phoneConversations)) }
        phonePreferences.save(_state.value.phoneConversations)
    }

    private fun receiveVisibleHome(home: HomeFeed?) {
        if (home == null) return
        val current = _state.value
        val visible = current.mobileInbox.visibleHome(home)
        updatePhone { previous ->
            visible.attention.sortedBy { epochMillis(it.at) }.fold(previous) { next, item ->
                next.receive(item.session.phoneKey(current.activeComputerKey), item.eventId,
                    item.at.ifBlank { item.session.lastTs })
            }
        }
    }

    fun clearPhoneInbox() {
        val current = _state.value
        val activity = current.mobileInbox.visibleSessions(current.sessions())
            .associate { session -> phoneConversationKey(session.computerId.ifBlank { current.activeComputerKey },
                session.source, session.sessionId) to session.phoneActivity() }.toMutableMap()
        current.home?.let(current.mobileInbox::visibleHome)?.let { home ->
            home.attention.forEach { item ->
                val key = item.session.phoneKey(current.activeComputerKey)
                val next = PhoneActivity(item.eventId, item.at.ifBlank { item.session.lastTs })
                val old = activity[key] ?: PhoneActivity()
                activity[key] = PhoneActivity(maxOf(old.eventId, next.eventId),
                    listOf(old.timestamp, next.timestamp).maxByOrNull(::epochMillis).orEmpty())
            }
            (home.running + home.resume).forEach { meta ->
                val key = meta.phoneKey(current.activeComputerKey)
                val old = activity[key] ?: PhoneActivity()
                activity[key] = old.copy(timestamp = listOf(old.timestamp, meta.lastTs).maxByOrNull(::epochMillis).orEmpty())
            }
        }
        updatePhone { it.clearInbox(activity) }
    }

    fun hideSessionOnPhone(session: SessionSummary) = hideSessionsOnPhone(listOf(session))

    fun hideSessionsOnPhone(sessions: List<SessionSummary>) {
        val computer = _state.value.activeComputerKey
        updatePhone { it.hide(sessions.associate { session ->
            phoneConversationKey(session.computerId.ifBlank { computer }, session.source, session.sessionId) to session.phoneActivity()
        }) }
    }

    fun undoPhoneCleanup() = updatePhone { it.undoCleanup() }
    fun restorePhoneSessions() = updatePhone { it.restoreSessions() }

    /** Opening an existing row establishes an anchor at the received row, never at the
     * oldest event from a server backlog. Live notifications establish it sooner. */
    fun preparePhoneContext(sessionId: String) {
        val current = _state.value
        val session = current.session(sessionId) ?: return
        val key = phoneConversationKey(session.computerId.ifBlank { current.activeComputerKey }, session.source, sessionId)
        if (current.phoneConversations.receipts[key] == null) {
            session.events.lastOrNull { it.source == session.source }?.let { event ->
                updatePhone { it.receive(key, event.id, event.ts) }
            }
        }
        _state.update { state ->
            val receipt = state.phoneConversations.receipts[key]
            val old = state.sessionItems[sessionId]?.takeIf { it.contextKey == key } ?: phoneItemCache[key]
            val compatible = old?.contextKey == key && old.history.managed &&
                (old.history.anchorEventId == receipt?.eventId || (receipt == null && old.history.anchorEventId == 0L))
            val next = if (compatible) old!! else SessionItemsState(
                contextKey = key, history = SessionHistoryState(managed = true, anchorEventId = receipt?.eventId ?: 0L),
                running = old?.running ?: session.running, runningSince = old?.runningSince ?: session.runningSince,
                loadedAt = System.currentTimeMillis(), available = false,
            )
            state.copy(sessionItems = state.sessionItems + (sessionId to next), transcriptCache = state.transcriptCache - sessionId,
                sessionDrafts = phoneDraftCache[key]?.let { state.sessionDrafts + (sessionId to it) } ?: state.sessionDrafts)
        }
    }

    fun activeComputerId(): String = computerStore.active()?.deviceId.orEmpty()

    fun setConnectionState(connection: ConnectionState) {
        _state.update { it.copy(connection = connection) }
    }

    suspend fun setSessionPinned(sessionId: String, pinned: Boolean) {
        val flags = sessionFlagsStore.update(sessionId) { it.copy(pinned = pinned) }
        _state.update { current ->
            current.copy(
                localSessionFlags = flags,
                sessionMeta = current.sessionMeta.mapValues { (key, meta) ->
                    if (key == sessionId) meta.copy(pinned = pinned) else meta
                },
            )
        }
        persistSessionFlags(sessionId, mapOf("pinned" to pinned))
    }

    suspend fun setSessionMuted(sessionId: String, muted: Boolean) {
        val flags = sessionFlagsStore.update(sessionId) { it.copy(muted = muted) }
        _state.update { current ->
            current.copy(
                localSessionFlags = flags,
                sessionMeta = current.sessionMeta.mapValues { (key, meta) ->
                    if (key == sessionId) meta.copy(muted = muted) else meta
                },
                inbox = if (muted) current.inbox?.withoutWaiting(sessionId) else current.inbox,
            )
        }
        persistSessionFlags(sessionId, mapOf("muted" to muted))
    }

    suspend fun setSessionArchived(sessionId: String, archived: Boolean) {
        val flags = sessionFlagsStore.update(sessionId) { it.copy(archived = archived) }
        _state.update { current ->
            current.copy(
                localSessionFlags = flags,
                sessionMeta = current.sessionMeta.mapValues { (key, meta) ->
                    if (key == sessionId) meta.copy(archived = archived) else meta
                },
                inbox = if (archived) {
                    current.inbox?.withoutSession(sessionId)
                } else {
                    current.inbox
                },
            )
        }
        persistSessionFlags(sessionId, mapOf("archived" to archived))
    }

    suspend fun setSessionRemindOnDone(sessionId: String, enabled: Boolean) {
        _state.update { current ->
            current.copy(
                sessionMeta = current.sessionMeta.mapValues { (key, meta) ->
                    if (key == sessionId) meta.copy(remindOnDone = enabled) else meta
                },
            )
        }
        persistSessionFlags(sessionId, mapOf("remind_on_done" to enabled))
    }

    suspend fun setSessionTitle(sessionId: String, title: String): Boolean {
        val clean = title.trim().take(120)
        if (sessionId.isBlank() || clean.isBlank()) return false
        _state.update { current ->
            current.copy(
                sessionMeta = current.sessionMeta.mapValues { (key, meta) ->
                    if (key == sessionId) {
                        meta.copy(title = clean, titleSource = "homing")
                    } else {
                        meta
                    }
                },
            )
        }
        val updated = runCatching {
            AgentApi.patchSession(
                settings = _state.value.settings,
                sessionId = sessionId,
                title = clean,
            )
        }.getOrNull() ?: return false
        mergeSessionMeta(
            sessions = listOf(updated),
            apiAvailable = true,
            error = null,
        )
        scheduleInboxRefresh(delayMillis = 200L)
        return true
    }

    suspend fun cancelQueuedReplies(sessionId: String): Int {
        if (sessionId.isBlank()) return 0
        val queued = _state.value.replies.values
            .filter {
                it.sessionId == sessionId &&
                    it.id > 0 &&
                    it.status == ReplyStatus.QUEUED
            }
            .sortedBy { it.ts }
        var cancelled = 0
        for (reply in queued) {
            val receipt = runCatching {
                AgentApi.cancelReply(_state.value.settings, reply.id)
            }.getOrNull() ?: continue
            if (!receipt.ok) continue
            cancelled += 1
            _state.update { current ->
                val existing = current.replies[reply.id] ?: reply
                current.copy(
                    replies = current.replies + (
                        reply.id to existing.copy(
                            status = receipt.status,
                            message = receipt.message,
                        )
                        ),
                )
            }
        }
        if (cancelled > 0) scheduleInboxRefresh(delayMillis = 200L)
        return cancelled
    }

    suspend fun cancelQueuedReply(reply: AgentReply): Boolean {
        if (reply.id <= 0 || reply.status != ReplyStatus.QUEUED) return false
        val receipt = runCatching {
            AgentApi.cancelReply(_state.value.settings, reply.id)
        }.getOrNull() ?: return false
        if (!receipt.ok) return false
        _state.update { current ->
            val existing = current.replies[reply.id] ?: reply
            current.copy(
                replies = current.replies + (
                    reply.id to existing.copy(
                        status = receipt.status,
                        message = receipt.message,
                    )
                    ),
            )
        }
        scheduleInboxRefresh(delayMillis = 200L)
        return true
    }

    private suspend fun persistSessionFlags(
        sessionId: String,
        fields: Map<String, Boolean>,
    ) {
        if (sessionId.isBlank() || fields.isEmpty()) return
        val updated = runCatching {
            AgentApi.patchSession(_state.value.settings, sessionId, fields)
        }.getOrNull() ?: return
        mergeSessionMeta(
            sessions = listOf(updated),
            apiAvailable = true,
            error = null,
        )
        scheduleInboxRefresh(delayMillis = 200L)
    }

    private fun applySessionFlags(flags: Map<String, LocalSessionFlags>) {
        _state.update { it.copy(localSessionFlags = flags) }
    }

    fun markConnecting() {
        _state.update { current ->
            if (current.connection == ConnectionState.CONNECTED) {
                current
            } else {
                current.copy(connection = ConnectionState.RECONNECTING)
            }
        }
    }

    fun onConnectionEstablished() {
        val now = System.currentTimeMillis()
        val diagnostics = _state.value.connectionDiagnostics.copy(lastConnectedAt = now)
        persistDiagnostics(diagnostics)
        _state.update { current ->
            current.copy(
                connection = ConnectionState.CONNECTED,
                connectionDiagnostics = diagnostics,
            )
        }
        backgroundScope.launch { flushOutbox() }
    }

    fun onConnectionLost(reason: String) {
        val now = System.currentTimeMillis()
        val diagnostics = _state.value.connectionDiagnostics.copy(
            lastDisconnectedAt = now,
            lastDisconnectReason = reason,
        )
        persistDiagnostics(diagnostics)
        _state.update { current ->
            current.copy(
                connection = ConnectionState.RECONNECTING,
                connectionDiagnostics = diagnostics,
            )
        }
    }

    fun markSessionReadLocally(sessionId: String) {
        val flags = sessionFlagsStore.update(sessionId) {
            it.copy(read = true, readAt = System.currentTimeMillis())
        }
        _state.update { current ->
            val ids = current.events.values
                .filter { it.sessionKey == sessionId && !it.read }
                .map { it.id }
                .toSet()
            if (ids.isEmpty()) {
                current.copy(
                    localSessionFlags = flags,
                    sessionMeta = current.sessionMeta.mapValues { (key, meta) ->
                        if (key == sessionId) {
                            meta.copy(read = true, unread = 0)
                        } else {
                            meta
                        }
                    },
                    inbox = current.inbox?.withSessionRead(sessionId),
                )
            } else {
                current.copy(
                    localSessionFlags = flags,
                    events = current.events.mapValues { (id, event) ->
                        if (id in ids) event.copy(read = true) else event
                    },
                    sessionMeta = current.sessionMeta.mapValues { (key, meta) ->
                        if (key == sessionId) {
                            meta.copy(read = true, unread = 0)
                        } else {
                            meta
                        }
                    },
                    inbox = current.inbox?.withSessionRead(sessionId),
                )
            }
        }
    }

    suspend fun markSessionRead(sessionId: String) {
        val events = _state.value.events.values
            .filter { it.sessionKey == sessionId && !it.read }
        val ids = events.map { it.id }
        markSessionReadLocally(sessionId)
        val patched = patchSessionReadRemote(sessionId)
        if (!patched && ids.isNotEmpty()) {
            runCatching {
                AgentApi.markRead(_state.value.settings, ids)
            }
        }
        scheduleAggregatedHomeRefresh()
    }

    suspend fun markAllRead() {
        val ids = _state.value.events.values
            .filterNot { it.read }
            .map { it.id }
        val sessionIds = _state.value.sessions().map { it.sessionId }
        val flags = sessionFlagsStore.updateMany(sessionIds) {
            it.copy(read = true, readAt = System.currentTimeMillis())
        }
        _state.update { current ->
            current.copy(
                localSessionFlags = flags,
                events = current.events.mapValues { (_, event) -> event.copy(read = true) },
                sessionMeta = current.sessionMeta.mapValues { (_, meta) ->
                    meta.copy(read = true, unread = 0)
                },
                inbox = current.inbox?.let { feed ->
                    sessionIds.fold(feed) { next, sessionId ->
                        next.withSessionRead(sessionId)
                    }
                },
            )
        }
        var patchAvailable = sessionIds.isNotEmpty()
        for (sessionId in sessionIds) {
            if (!patchSessionReadRemote(sessionId)) {
                patchAvailable = false
            }
        }
        if (!patchAvailable && ids.isNotEmpty()) {
            runCatching { AgentApi.markRead(_state.value.settings, ids) }
        }
    }

    suspend fun markEventsRead(ids: Collection<Long>, sessionId: String? = null) {
        if (ids.isEmpty()) {
            return
        }
        _state.update { current ->
            current.copy(
                events = current.events.mapValues { (id, event) ->
                    if (id in ids) event.copy(read = true) else event
                },
                sessionMeta = current.sessionMeta.mapValues { (key, meta) ->
                    if (sessionId == null || key == sessionId) {
                        meta.copy(read = true, unread = 0)
                    } else {
                        meta
                    }
                },
                inbox = sessionId?.let { current.inbox?.withSessionRead(it) } ?: current.inbox,
            )
        }
        if (sessionId != null) {
            markSessionReadLocally(sessionId)
        }
        val patched = sessionId != null && patchSessionReadRemote(sessionId)
        if (!patched) {
            AgentApi.markRead(_state.value.settings, ids)
        }
    }

    private suspend fun patchSessionReadRemote(sessionId: String): Boolean {
        val updated = runCatching {
            AgentApi.patchSession(
                _state.value.settings,
                sessionId,
                mapOf("read" to true),
            )
        }.getOrNull() ?: return false
        mergeSessionMeta(
            sessions = listOf(updated),
            apiAvailable = true,
            error = null,
        )
        return true
    }

    suspend fun sendReply(
        sessionId: String,
        eventId: Long?,
        source: String,
        text: String,
        files: List<String> = emptyList(),
        model: String? = null,
        effort: String? = null,
        permissionMode: String? = null,
    ): AgentReply {
        val localId = nextLocalReplyId.decrementAndGet()
        val optimistic = AgentReply(
            id = localId,
            eventId = eventId ?: 0L,
            sessionId = sessionId,
            text = text,
            ts = Instant.now().toString(),
            status = ReplyStatus.QUEUED,
            isLocal = true,
            files = files,
            model = model.orEmpty(),
            effort = effort.orEmpty(),
            permissionMode = permissionMode.orEmpty(),
        )
        _state.update { current ->
            current.copy(
                replies = current.replies + (localId to optimistic),
                events = current.events.mapValues { (_, event) ->
                    if (event.id == eventId) event.copy(read = true) else event
                },
            )
        }

        return try {
            val receipt = AgentApi.reply(
                _state.value.settings,
                eventId,
                sessionId,
                source,
                text,
                files,
                model,
                effort,
                permissionMode,
            )
            val serverReply = AgentReply(
                id = receipt.replyId.takeIf { it > 0 } ?: localId,
                eventId = eventId ?: 0L,
                sessionId = sessionId,
                text = text,
                ts = optimistic.ts,
                status = receipt.status,
                message = receipt.message,
                isLocal = receipt.replyId <= 0,
                files = files,
                model = model.orEmpty(),
                effort = effort.orEmpty(),
                permissionMode = permissionMode.orEmpty(),
            )
            var reconciled = serverReply
            _state.update { current ->
                val existing = current.replies[serverReply.id]
                    ?: current.replies[localId]
                val merged = reconcileIncomingReply(existing, serverReply)
                reconciled = merged
                current.copy(
                    replies = current.replies
                        .minus(localId)
                        .plus(merged.id to merged),
                )
            }
            if (serverReply.status != ReplyStatus.FAILED) {
                markSessionReadLocally(sessionId)
                patchSessionReadRemote(sessionId)
            }
            reconciled
        } catch (error: Exception) {
            if (shouldQueueReply(error)) {
                localCacheReady.await()
                val queued = optimistic.copy(
                    status = ReplyStatus.OUTBOX,
                    message = "待联网发送",
                )
                runCatching {
                    localCache.queueReply(
                        OutboxReply(
                            localReplyId = localId,
                            sessionId = sessionId,
                            eventId = eventId,
                            source = source,
                            text = text,
                            files = files,
                            model = model.orEmpty(),
                            effort = effort.orEmpty(),
                            permissionMode = permissionMode.orEmpty(),
                            lastError = friendlyError(error),
                        ),
                    )
                }
                _state.update { current ->
                    current.copy(replies = current.replies + (localId to queued))
                }
                queued
            } else {
                val failed = optimistic.copy(
                    status = ReplyStatus.FAILED,
                    message = friendlyError(error),
                )
                _state.update { current ->
                    current.copy(replies = current.replies + (localId to failed))
                }
                failed
            }
        }
    }

    suspend fun retryReply(reply: AgentReply): AgentReply {
        _state.update { current ->
            current.copy(
                replies = current.replies + (
                    reply.id to reply.copy(
                        status = ReplyStatus.QUEUED,
                        message = null,
                    )
                    ),
            )
        }
        return try {
            val receipt = AgentApi.reply(
                _state.value.settings,
                reply.eventId,
                reply.sessionId,
                session(reply.sessionId)?.brand?.wire.orEmpty(),
                reply.text,
                reply.files,
                reply.model.takeIf { it.isNotBlank() },
                reply.effort.takeIf { it.isNotBlank() },
                reply.permissionMode.takeIf { it.isNotBlank() },
            )
            val retried = reply.copy(
                id = receipt.replyId.takeIf { it > 0 } ?: reply.id,
                status = receipt.status,
                message = receipt.message,
                ts = Instant.now().toString(),
                isLocal = receipt.replyId <= 0,
            )
            var reconciled = retried
            _state.update { current ->
                val existing = current.replies[retried.id]
                    ?: current.replies[reply.id]
                val merged = reconcileIncomingReply(existing, retried)
                reconciled = merged
                current.copy(
                    replies = current.replies
                        .minus(reply.id)
                        .plus(merged.id to merged),
                )
            }
            if (retried.status != ReplyStatus.FAILED) {
                markSessionReadLocally(reply.sessionId)
                patchSessionReadRemote(reply.sessionId)
            }
            reconciled
        } catch (error: Exception) {
            val failed = reply.copy(
                status = ReplyStatus.FAILED,
                message = friendlyError(error),
            )
            _state.update { current ->
                current.copy(replies = current.replies + (reply.id to failed))
            }
            failed
        }
    }

    private fun shouldQueueReply(error: Throwable): Boolean {
        if (error is AgentApiException) return false
        return error is java.io.IOException
    }

    suspend fun flushOutbox() {
        if (!outboxMutex.tryLock()) return
        try {
            localCacheReady.await()
            for (queued in localCache.outbox()) {
                try {
                    val receipt = AgentApi.reply(
                        settings = _state.value.settings,
                        eventId = queued.eventId,
                        sessionId = queued.sessionId,
                        source = queued.source,
                        text = queued.text,
                        files = queued.files,
                        model = queued.model.takeIf { it.isNotBlank() },
                        effort = queued.effort.takeIf { it.isNotBlank() },
                        permissionMode = queued.permissionMode.takeIf { it.isNotBlank() },
                    )
                    val serverReply = AgentReply(
                        id = receipt.replyId.takeIf { it > 0 } ?: queued.localReplyId,
                        eventId = queued.eventId ?: 0L,
                        sessionId = queued.sessionId,
                        text = queued.text,
                        ts = Instant.ofEpochMilli(queued.createdAt).toString(),
                        status = receipt.status,
                        message = receipt.message,
                        isLocal = receipt.replyId <= 0,
                        files = queued.files,
                        model = queued.model,
                        effort = queued.effort,
                        permissionMode = queued.permissionMode,
                    )
                    _state.update { current ->
                        current.copy(
                            replies = current.replies
                                .minus(queued.localReplyId)
                                .plus(serverReply.id to serverReply),
                        )
                    }
                    localCache.deleteOutbox(queued.id)
                    markSessionReadLocally(queued.sessionId)
                    patchSessionReadRemote(queued.sessionId)
                } catch (error: Exception) {
                    if (error is AgentApiException && error.statusCode in 400..499) {
                        _state.update { current ->
                            val failed = (current.replies[queued.localReplyId]
                                ?: AgentReply(
                                    id = queued.localReplyId,
                                    eventId = queued.eventId ?: 0L,
                                    sessionId = queued.sessionId,
                                    text = queued.text,
                                    ts = Instant.ofEpochMilli(queued.createdAt).toString(),
                                    status = ReplyStatus.OUTBOX,
                                )).copy(
                                status = ReplyStatus.FAILED,
                                message = friendlyError(error),
                            )
                            current.copy(
                                replies = current.replies + (failed.id to failed),
                            )
                        }
                        localCache.deleteOutbox(queued.id)
                    } else {
                        localCache.markOutboxError(queued.id, friendlyError(error))
                        break
                    }
                }
            }
        } finally {
            outboxMutex.unlock()
        }
    }

    suspend fun previewCompose(
        sessionId: String,
        source: String,
        text: String,
        files: List<String>,
        model: String?,
        effort: String?,
        permissionMode: String?,
        mentionOverrides: List<ComposeMentionOverride> = emptyList(),
    ): ComposeResult = AgentApi.compose(
        settings = _state.value.settings,
        sessionId = sessionId,
        source = source,
        text = text,
        files = files,
        model = model,
        effort = effort,
        permissionMode = permissionMode,
        mentionOverrides = mentionOverrides,
        dryRun = true,
    )

    suspend fun compose(
        sessionId: String,
        source: String,
        text: String,
        parentText: String,
        files: List<String>,
        model: String?,
        effort: String?,
        permissionMode: String? = null,
        mentionOverrides: List<ComposeMentionOverride> = emptyList(),
    ): ComposeResult {
        val localId = if (parentText.isBlank()) {
            0L
        } else {
            nextLocalReplyId.decrementAndGet()
        }
        if (localId < 0) {
            val optimistic = AgentReply(
                id = localId,
                eventId = 0L,
                sessionId = sessionId,
                text = parentText,
                ts = Instant.now().toString(),
                status = ReplyStatus.QUEUED,
                isLocal = true,
                files = files,
                model = model.orEmpty(),
                effort = effort.orEmpty(),
                permissionMode = permissionMode.orEmpty(),
            )
            _state.update { current ->
                current.copy(replies = current.replies + (localId to optimistic))
            }
        }
        return try {
            val result = AgentApi.compose(
                settings = _state.value.settings,
                sessionId = sessionId,
                source = source,
                text = text,
                files = files,
                model = model,
                effort = effort,
                permissionMode = permissionMode,
                mentionOverrides = mentionOverrides,
            )
            mergeDispatches(result.dispatches)
            val receipt = result.reply
            if (localId < 0) {
                _state.update { current ->
                    val existing = current.replies[localId]
                    val updated = if (receipt != null && parentText.isNotBlank()) {
                        AgentReply(
                            id = receipt.replyId.takeIf { it > 0 } ?: localId,
                            eventId = 0L,
                            sessionId = sessionId,
                            text = parentText,
                            ts = existing?.ts ?: Instant.now().toString(),
                            status = ReplyStatus.fromWire(receipt.status),
                            message = receipt.message.takeIf { it.isNotBlank() },
                            isLocal = receipt.replyId <= 0,
                            files = files,
                            model = model.orEmpty(),
                            effort = effort.orEmpty(),
                            permissionMode = permissionMode.orEmpty(),
                        )
                    } else {
                        null
                    }
                    val merged = updated?.let {
                        reconcileIncomingReply(current.replies[localId], it)
                    }
                    val replies = current.replies - localId
                    current.copy(
                        replies = if (merged != null) {
                            replies + (merged.id to merged)
                        } else {
                            replies
                        },
                    )
                }
            }
            if (receipt?.status != "failed") {
                markSessionReadLocally(sessionId)
                patchSessionReadRemote(sessionId)
            }
            result
        } catch (error: Exception) {
            if (localId < 0) {
                _state.update { current ->
                    val existing = current.replies[localId] ?: return@update current
                    val clientError = error is AgentApiException &&
                        error.statusCode in 400..499
                    if (clientError) {
                        current.copy(replies = current.replies - localId)
                    } else {
                        current.copy(
                            replies = current.replies + (
                                localId to existing.copy(
                                    status = ReplyStatus.FAILED,
                                    message = friendlyError(error),
                                )
                                ),
                        )
                    }
                }
            }
            throw error
        }
    }

    suspend fun dispatch(
        parentSessionId: String,
        parentSource: String,
        parentEventId: Long?,
        targetAgent: String,
        text: String,
        cwd: String,
    ): AgentDispatch {
        val result = AgentApi.dispatch(
            settings = _state.value.settings,
            parentSessionId = parentSessionId,
            parentSource = parentSource,
            parentEventId = parentEventId,
            targetAgent = targetAgent,
            text = text,
            cwd = cwd,
        )
        val dispatch = result.dispatch
            ?: throw AgentApiException(0, result.error ?: "派发失败")
        if (!result.ok) {
            throw AgentApiException(0, result.error ?: dispatch.message.ifBlank { "派发失败" })
        }
        mergeDispatches(listOf(dispatch))
        return dispatch
    }

    suspend fun refreshDispatches(parentSessionId: String) {
        if (parentSessionId.isBlank()) return
        val dispatches = AgentApi.fetchDispatches(
            settings = _state.value.settings,
            parentSessionId = parentSessionId,
        )
        mergeDispatches(dispatches)
    }

    suspend fun forwardDispatch(dispatchId: String, text: String? = null): CommandResult {
        val result = AgentApi.forwardDispatch(
            settings = _state.value.settings,
            dispatchId = dispatchId,
            text = text,
        )
        if (!result.ok) {
            throw AgentApiException(0, result.message ?: "转交失败")
        }
        val dispatch = _state.value.dispatches[dispatchId]
        if (dispatch != null) {
            mergeDispatches(
                listOf(
                    dispatch.copy(
                        status = DispatchStatus.COMPLETED,
                        message = "结论已转交给父 Agent",
                    ),
                ),
            )
        }
        return result
    }

    suspend fun replyDispatch(dispatchId: String, text: String): AgentDispatch {
        val result = AgentApi.replyDispatch(
            settings = _state.value.settings,
            dispatchId = dispatchId,
            text = text,
        )
        val dispatch = result.dispatch
            ?: throw AgentApiException(0, result.error ?: "追问失败")
        if (!result.ok) {
            throw AgentApiException(0, result.error ?: dispatch.message.ifBlank { "追问失败" })
        }
        mergeDispatches(listOf(dispatch))
        return dispatch
    }

    suspend fun cancelDispatch(dispatchId: String): AgentDispatch {
        val result = AgentApi.cancelDispatch(
            settings = _state.value.settings,
            dispatchId = dispatchId,
        )
        val dispatch = result.dispatch
            ?: throw AgentApiException(0, result.error ?: "取消失败")
        if (!result.ok) {
            throw AgentApiException(0, result.error ?: dispatch.message.ifBlank { "取消失败" })
        }
        mergeDispatches(listOf(dispatch))
        return dispatch
    }

    suspend fun discardDispatchWorktree(dispatchId: String): AgentDispatch {
        val result = AgentApi.discardDispatchWorktree(
            settings = _state.value.settings,
            dispatchId = dispatchId,
        )
        val dispatch = result.dispatch
            ?: throw AgentApiException(0, result.error ?: "丢弃 worktree 失败")
        if (!result.ok) {
            throw AgentApiException(0, result.error ?: dispatch.message.ifBlank { "丢弃 worktree 失败" })
        }
        mergeDispatches(listOf(dispatch))
        return dispatch
    }

    suspend fun setAway(enabled: Boolean): Boolean {
        val previous = _state.value.away
        val nowMillis = System.currentTimeMillis()
        _state.update { current ->
            val maxEvent = current.events.keys.maxOrNull() ?: current.mobileWorkspace.baselineMaxEventId
            val nextWorkspace = if (enabled && !previous) {
                current.mobileWorkspace.copy(
                    clearedAtEpochMs = maxOf(current.mobileWorkspace.clearedAtEpochMs, nowMillis),
                    baselineMaxEventId = maxOf(current.mobileWorkspace.baselineMaxEventId, maxEvent),
                    baselineReady = true,
                    liveSessionIds = emptySet(),
                )
            } else {
                current.mobileWorkspace
            }
            current.copy(away = enabled, mobileWorkspace = nextWorkspace)
        }
        return try {
            val actual = AgentApi.setAway(_state.value.settings, enabled)
            val home = runCatching { AgentApi.fetchHome(_state.value.settings) }.getOrNull()
            val nextInbox = mobileInboxPreferences.setEnabled(actual)
            _state.update {
                it.copy(
                    away = actual,
                    mobileInbox = nextInbox,
                    home = home ?: it.home,
                )
            }
            scheduleAggregatedHomeRefresh()
            actual
        } catch (error: Exception) {
            _state.update { it.copy(away = previous) }
            throw error
        }
    }

    suspend fun clearAwayPeriod(id: String): Boolean {
        if (id.isBlank()) return false
        val cleared = AgentApi.clearAwayPeriod(_state.value.settings, id)
        if (cleared) {
            val home = runCatching { AgentApi.fetchHome(_state.value.settings) }.getOrNull()
            _state.update {
                it.copy(
                    away = home?.away?.enabled ?: it.away,
                    home = home ?: it.home,
                )
            }
            scheduleAggregatedHomeRefresh()
        }
        return cleared
    }

    suspend fun stopSession(sessionId: String): CommandResult {
        return AgentApi.stopSession(_state.value.settings, sessionId)
    }

    suspend fun decideApproval(
        eventId: Long,
        decision: String,
        sessionId: String? = null,
        optionId: String? = null,
    ): CommandResult {
        val result = AgentApi.decideApproval(
            _state.value.settings,
            eventId,
            decision,
            optionId,
        )
        val isNotPending = !result.ok && (
            result.errorCode == "not_pending" ||
                result.message == "not_pending"
            )
        if (result.ok || isNotPending) {
            val targetSession = sessionId
                ?.takeIf { it.isNotBlank() }
                ?: _state.value.events[eventId]?.sessionKey
            _state.update { current ->
                current.copy(
                    settledApprovalIds = current.settledApprovalIds + eventId,
                    events = current.events.mapValues { (id, ev) ->
                        if (id == eventId) ev.copy(read = true) else ev
                    },
                )
            }
            if (!targetSession.isNullOrBlank()) {
                markSessionReadLocally(targetSession)
                patchSessionReadRemote(targetSession)
            } else {
                runCatching { AgentApi.markRead(_state.value.settings, listOf(eventId)) }
            }
            if (isNotPending) {
                return CommandResult(
                    ok = false,
                    message = "该授权请求已在电脑端处理或已过期，已自动清除",
                    errorCode = "not_pending",
                )
            }
        }
        return result
    }

    fun session(sessionId: String): SessionSummary? = _state.value.session(sessionId)

    fun sessionEvents(sessionId: String) = transportRouter.events(sessionId)

    internal fun replaceStateForTest(state: AgentUiState) {
        computerGeneration.incrementAndGet()
        phoneItemCache.clear()
        phoneDraftCache.clear()
        detailRequests.clear()
        _state.value = state
    }

    private fun mergeEvents(events: List<AgentEvent>) {
        val fresh = mutableListOf<AgentEvent>()
        _state.update { current ->
            val merged = current.events
                .filterValues { !isIdlePromptNoiseEvent(it.kind, it.text) }
                .toMutableMap()
            for (event in events) {
                if (isIdlePromptNoiseEvent(event.kind, event.text)) {
                    continue
                }
                val existing = merged[event.id]
                if (existing == null) {
                    fresh += event
                }
                merged[event.id] = if (existing != null) {
                    event.copy(read = existing.read || event.read)
                } else {
                    event
                }
            }
            current.copy(events = merged)
        }
        for (event in fresh) {
            if (event.sessionId.isNotBlank()) {
                clearReadForNewContent(event.sessionId, event.ts)
            }
        }
    }

    private fun clearReadForNewContent(sessionId: String, ts: String) {
        val flags = _state.value.localSessionFlags[sessionId] ?: return
        if (!flags.read) return
        val eventMillis = runCatching { Instant.parse(ts).toEpochMilli() }
            .getOrDefault(Long.MAX_VALUE)
        if (eventMillis <= flags.readAt) return
        val updated = sessionFlagsStore.update(sessionId) { it.copy(read = false) }
        _state.update { current ->
            current.copy(
                localSessionFlags = updated,
                sessionMeta = current.sessionMeta.mapValues { (key, meta) ->
                    if (key == sessionId) {
                        meta.copy(
                            read = false,
                            unread = meta.unread.coerceAtLeast(1),
                        )
                    } else {
                        meta
                    }
                },
            )
        }
    }

    private fun mergeReplies(replies: List<AgentReply>) {
        _state.update { current ->
            val merged = current.replies.toMutableMap()
            for (reply in replies) {
                if (reply.id > 0) {
                    merged[reply.id] = reconcileIncomingReply(
                        merged[reply.id],
                        reply,
                    )
                }
            }
            current.copy(replies = merged)
        }
    }

    private fun mergeSessionMeta(
        sessions: List<SessionMeta>,
        apiAvailable: Boolean,
        error: Throwable?,
    ) {
        val remoteFlags = cacheRemoteFlags(sessions)
        _state.update { current ->
            val merged = current.sessionMeta.toMutableMap()
            for (session in sessions) {
                merged[session.sessionId] = mergeMeta(
                    merged[session.sessionId],
                    session,
                )
            }
            current.copy(
                sessionMeta = merged,
                localSessionFlags = remoteFlags ?: current.localSessionFlags,
                sessionApiAvailable = if (error == null) {
                    apiAvailable
                } else {
                    current.sessionApiAvailable && apiAvailable
                },
            )
        }
    }

    private fun cacheRemoteFlags(
        sessions: List<SessionMeta>,
    ): Map<String, LocalSessionFlags>? {
        val remote = sessions
            .filter {
                it.sessionId.isNotBlank() &&
                    (
                        it.read != null ||
                            it.pinned != null ||
                            it.muted != null ||
                            it.archived != null
                        )
            }
            .associateBy { it.sessionId }
        if (remote.isEmpty()) return null
        val now = System.currentTimeMillis()
        var updated = sessionFlagsStore.load()
        for ((sessionId, incoming) in remote) {
            updated = sessionFlagsStore.update(sessionId) { local ->
                val remoteRead = incoming.read
                local.copy(
                    read = remoteRead ?: local.read,
                    pinned = incoming.pinned ?: local.pinned,
                    muted = incoming.muted ?: local.muted,
                    archived = incoming.archived ?: local.archived,
                    readAt = when {
                        remoteRead == true && !local.read -> now
                        remoteRead == false -> 0L
                        else -> local.readAt
                    },
                )
            }
        }
        return updated
    }

    private fun scheduleAggregatedHomeRefresh() {
        val now = System.currentTimeMillis()
        val activeId = computerStore.active()?.deviceId.orEmpty()
        val computers = computerStore.load()
            .filter { it.deviceId.isNotBlank() && it.server.isNotBlank() }
            .filter { computer ->
                if (computer.deviceId == activeId) return@filter true
                val lastFailed = unreachableSecondaryComputers[computer.deviceId] ?: 0L
                if (lastFailed > 0L && now - lastFailed < SECONDARY_COMPUTER_COOLDOWN_MILLIS) {
                    return@filter false
                }
                val lastSeen = maxOf(computer.lastSeen, computer.pairedAt)
                lastSeen == 0L || now - lastSeen <= STALE_COMPUTER_THRESHOLD_MILLIS
            }
        if (computers.size <= 1) return
        aggregateHomeJob?.cancel()
        aggregateHomeJob = backgroundScope.launch {
            refreshAggregatedHome(computers, activeId)
        }
    }

    private suspend fun refreshAggregatedHome(
        computers: List<PairedComputer>,
        activeId: String,
    ) {
        cloudLink.ensureStarted()
        if (!cloudLink.awaitConnected(4_000L)) return
        val results = coroutineScope {
            computers.map { computer ->
                async {
                    fetchComputerHome(
                        computer = computer,
                        isActive = computer.deviceId == activeId,
                    )
                }
            }.map { it.await() }
        }
        val responsiveSecondaries = results.count {
            it.computerId != activeId && it.feed != null
        }
        // If no secondary computer actually answered, keep the clean single-computer
        // home feed instead of flapping the header to "N computers" with dead peers.
        if (responsiveSecondaries == 0) return
        val merged = mergeComputerHomes(
            results = results.filter { it.feed != null || it.computerId == activeId },
            activeComputerId = activeId,
        )
        _state.update { current ->
            current.copy(
                home = merged,
                away = merged.away.enabled,
            )
        }
        receiveVisibleHome(merged)
        persistLocalSnapshot()
    }

    private suspend fun fetchComputerHome(
        computer: PairedComputer,
        isActive: Boolean,
    ): ComputerHomeResult {
        return runCatching {
            val response = cloudLink.request(
                computer = computer,
                method = "GET",
                path = "/api/home",
                timeoutMillis = if (isActive) 8_000L else SECONDARY_COMPUTER_TIMEOUT_MILLIS,
            )
            if (response.status !in 200..299) {
                throw AgentApiException(
                    response.status,
                    (response.body as? JSONObject)?.optString("error")
                        ?.takeIf { it.isNotBlank() }
                        ?: "读取首页失败",
                )
            }
            val json = response.body as? JSONObject
                ?: JSONObject(transportBodyJson(response.body))
            if (!isActive) {
                unreachableSecondaryComputers.remove(computer.deviceId)
                computerStore.updateLastSeen(computer.deviceId)
            }
            ComputerHomeResult(
                computerId = computer.deviceId,
                computerName = computer.name,
                feed = HomeFeed.fromJson(json),
            )
        }.getOrElse { error ->
            if (!isActive) {
                unreachableSecondaryComputers[computer.deviceId] = System.currentTimeMillis()
            }
            ComputerHomeResult(
                computerId = computer.deviceId,
                computerName = computer.name,
                error = friendlyError(error),
            )
        }
    }

    private fun scheduleInboxRefresh(delayMillis: Long = 600L) {
        inboxRefreshJob?.cancel()
        inboxRefreshJob = backgroundScope.launch {
            delay(delayMillis)
            val settings = _state.value.settings
            val scope = _state.value.activeComputerKey
            val generation = computerGeneration.get()
            val home = runCatching { AgentApi.fetchHome(settings) }.getOrNull()
            val inbox = runCatching { AgentApi.fetchInbox(settings) }.getOrNull()
                ?.takeIf { it.available }
            synchronized(this@AgentStore) {
                if (_state.value.activeComputerKey != scope || computerGeneration.get() != generation || (home == null && inbox == null)) return@launch
                val sessions = buildList {
                    if (inbox != null) addAll(inbox.sessions)
                    if (home != null) {
                        addAll(home.attentionSessions)
                        addAll(home.running)
                        addAll(home.resume)
                    }
                }.distinctBy { it.sessionId }
                mergeSessionMeta(
                    sessions = sessions,
                    apiAvailable = true,
                    error = null,
                )
                _state.update {
                    it.copy(
                        inbox = inbox ?: it.inbox,
                        home = home ?: it.home,
                        away = home?.away?.enabled ?: it.away,
                    )
                }
                receiveVisibleHome(home)
            }
            scheduleAggregatedHomeRefresh()
        }
    }

    private fun friendlyError(error: Throwable): String {
        return error.message?.take(240) ?: error.javaClass.simpleName
    }

    private fun loadDiagnostics(): ConnectionDiagnostics {
        return ConnectionDiagnostics(
            lastConnectedAt = diagnosticsPreferences
                .getLong(KEY_LAST_CONNECTED_AT, -1L)
                .takeIf { it > 0 },
            lastDisconnectedAt = diagnosticsPreferences
                .getLong(KEY_LAST_DISCONNECTED_AT, -1L)
                .takeIf { it > 0 },
            lastDisconnectReason = diagnosticsPreferences
                .getString(KEY_LAST_DISCONNECT_REASON, null)
                ?.takeIf { it.isNotBlank() },
        )
    }

    private fun persistDiagnostics(diagnostics: ConnectionDiagnostics) {
        diagnosticsPreferences.edit()
            .putLong(KEY_LAST_CONNECTED_AT, diagnostics.lastConnectedAt ?: -1L)
            .putLong(KEY_LAST_DISCONNECTED_AT, diagnostics.lastDisconnectedAt ?: -1L)
            .putString(KEY_LAST_DISCONNECT_REASON, diagnostics.lastDisconnectReason)
            .apply()
    }

    private suspend fun restoreLocalCache() {
        runCatching {
            val cached = localCache.load()
            phoneDraftCache.putAll(cached.drafts.filterKeys { it.startsWith("[") })
            cached.items.forEach { (key, rows) ->
                val history = _state.value.phoneConversations.histories[key]
                if (history != null) phoneItemCache[key] = SessionItemsState(
                    items = rows.filterNot { it.type == SessionItemType.REASONING },
                    history = history, contextKey = key, loadedAt = 0L)
            }
            _state.update { current ->
                // A snapshot must belong to the selected computer before metadata can restore.
                val sessions = cached.sessions.filter { it.computerId == current.activeComputerKey }
                val items = sessions.mapNotNull { meta ->
                    val key = meta.phoneKey(current.activeComputerKey)
                    val history = current.phoneConversations.histories[key] ?: return@mapNotNull null
                    val rows = cached.items[key] ?: return@mapNotNull null
                    meta.sessionId to SessionItemsState(items = rows.filterNot { it.type == SessionItemType.REASONING },
                        history = history, contextKey = key, loadedAt = 0L)
                }.toMap()
                if (current.lastUpdatedAt == null && current.sessionMeta.isEmpty()) current.copy(
                    sessionMeta = sessions.associateBy { it.sessionId },
                    sessionItems = items + current.sessionItems,
                    sessionDrafts = sessions.mapNotNull { meta -> phoneDraftCache[meta.phoneKey(current.activeComputerKey)]
                        ?.let { meta.sessionId to it } }.toMap() + current.sessionDrafts,
                    events = (if (cached.phoneComputerKey == current.activeComputerKey) cached.phoneEvents.associateBy { it.id } else emptyMap()) + current.events,
                    sessionApiAvailable = sessions.isNotEmpty(),
                ) else current
            }
        }
    }

    private suspend fun persistLocalSnapshot() {
        val current = _state.value
        runCatching {
            localCache.saveSnapshot(
                sessions = current.sessionMeta.values.map { it.copy(computerId = it.computerId.ifBlank { current.activeComputerKey }) },
                inbox = current.inbox,
                home = current.home,
                projects = current.projects,
                away = current.away,
                drafts = phoneDraftCache.toMap(),
                phoneComputerKey = current.activeComputerKey,
                phoneEvents = current.events.values.filter { event ->
                    val key = phoneConversationKey(current.activeComputerKey, event.source, event.sessionId)
                    current.phoneConversations.receipts[key]?.let { event.id >= it.eventId } == true
                },
            )
        }
    }

    companion object {
        private const val DIAGNOSTICS_PREFS = "agent_push_connection"
        private const val WORKSPACE_PREFS_NAME = "homing_workspace_prefs"
        private const val KEY_PENDING_RESET_ON_OPEN = "pending_reset_on_open"
        private const val KEY_LAST_ACTIVE_SESSION_PREF = "last_active_session_id"
        private const val KEY_LAST_CONNECTED_AT = "last_connected_at"
        private const val KEY_LAST_DISCONNECTED_AT = "last_disconnected_at"
        private const val KEY_LAST_DISCONNECT_REASON = "last_disconnect_reason"

        @Volatile
        private var instance: AgentStore? = null

        fun get(context: Context): AgentStore {
            return instance ?: synchronized(this) {
                instance ?: AgentStore(context).also { instance = it }
            }
        }
    }
}

private data class RefreshBundle(
    val events: EventPage,
    val sessions: SessionFetch,
    val projects: List<ProjectInfo>,
    val away: Boolean,
    val awayAuthoritative: Boolean = false,
    val agentDirectory: AgentDirectory,
    val agentCatalog: AgentCatalog,
    val inbox: InboxFeed,
    val home: HomeFeed?,
    val dispatches: List<AgentDispatch>,
)

private data class SessionFetch(
    val items: List<SessionMeta>,
    val apiAvailable: Boolean,
    val error: Throwable?,
)

private const val MOBILE_SESSION_LIMIT = 120
private const val METADATA_REFRESH_INTERVAL_MILLIS = 20_000L
private const val SECONDARY_COMPUTER_TIMEOUT_MILLIS = 3_000L
private const val SECONDARY_COMPUTER_COOLDOWN_MILLIS = 5 * 60_000L
private const val STALE_COMPUTER_THRESHOLD_MILLIS = 48L * 60L * 60L * 1000L

internal fun isLocalLanNetworkAvailable(context: Context): Boolean {
    return runCatching {
        val cm = context.getSystemService(ConnectivityManager::class.java) ?: return true
        val active = cm.activeNetwork ?: return false
        val caps = cm.getNetworkCapabilities(active) ?: return true
        caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) ||
            caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET)
    }.getOrDefault(true)
}

private fun mergeMeta(existing: SessionMeta?, incoming: SessionMeta): SessionMeta {
    if (existing == null) return incoming
    return existing.copy(
        computerId = incoming.computerId.ifBlank { existing.computerId },
        computerName = incoming.computerName.ifBlank { existing.computerName },
        source = incoming.source.ifBlank { existing.source },
        title = incoming.title.ifBlank { existing.title },
        titleSource = incoming.titleSource.ifBlank { existing.titleSource },
        workspace = incoming.workspace.ifBlank { existing.workspace },
        project = incoming.project.ifBlank { existing.project },
        branch = incoming.branch.ifBlank { existing.branch },
        cwd = incoming.cwd.ifBlank { existing.cwd },
        model = incoming.model.ifBlank { existing.model },
        status = incoming.status.ifBlank { existing.status },
        unread = incoming.unread.takeIf { it >= 0 } ?: existing.unread,
        lastTs = incoming.lastTs.ifBlank { existing.lastTs },
        lastPreview = incoming.lastPreview.ifBlank { existing.lastPreview },
        eventCount = maxOf(existing.eventCount, incoming.eventCount),
        requestId = incoming.requestId.ifBlank { existing.requestId },
        effort = incoming.effort.ifBlank { existing.effort },
        permissionMode = incoming.permissionMode.ifBlank { existing.permissionMode },
        contextUsage = incoming.contextUsage ?: existing.contextUsage,
        running = incoming.running ?: existing.running,
        runningSince = if (incoming.running == false) "" else incoming.runningSince.ifBlank { existing.runningSince },
        read = incoming.read ?: existing.read,
        pinned = incoming.pinned ?: existing.pinned,
        muted = incoming.muted ?: existing.muted,
        archived = incoming.archived ?: existing.archived,
        isTemp = incoming.isTemp ?: existing.isTemp,
        isEmpty = incoming.isEmpty ?: existing.isEmpty,
    )
}
