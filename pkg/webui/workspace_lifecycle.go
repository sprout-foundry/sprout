//go:build !js

package webui

// workspace_lifecycle.go — the workspace lifecycle surface the hosting platform drives
// (docs: workspace lifecycle contract §1). The daemon is the source of truth
// for "is this workspace busy, and when was it last used":
//
//   - GET  /api/lifecycle/activity      busy state + last user/agent activity
//   - POST /api/lifecycle/prepare-stop  drain, persist and checkpoint before a stop
//   - POST /api/lifecycle/notice        relay a workspace_stopping warning to clients
//
// Auth matches /api/sync: GET passes the auth middleware, the POSTs need the
// daemon bearer token when one is configured.

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// Busy reasons reported by /api/lifecycle/activity.
const (
	busyReasonQuery      = "query"
	busyReasonAutomation = "automation"
	busyReasonTask       = "task"
	busyReasonCheckpoint = "checkpoint"
)

// errCodeWorkspaceStopping is the error code a query gets once prepare-stop
// has run and the daemon no longer accepts work.
const errCodeWorkspaceStopping = "workspace_stopping"

// lifecycleState is the daemon's activity and stop bookkeeping. The zero
// value is ready to use.
type lifecycleState struct {
	lastUserNanos  atomic.Int64
	lastAgentNanos atomic.Int64
	stopping       atomic.Bool
	checkpointing  atomic.Bool

	prepareMu     sync.Mutex
	prepareDone   chan struct{}
	prepareResult prepareStopResult

	// interrupt stops one running chat. Nil means agent.TriggerInterrupt;
	// tests substitute it to observe the drain without a live model.
	interrupt func(clientID, chatID string, a *agent.Agent)
}

// agentActivityEventTypes are the bus events that mean the agent is doing
// work on a chat.
var agentActivityEventTypes = map[string]struct{}{
	events.EventTypeQueryStarted:     {},
	events.EventTypeQueryProgress:    {},
	events.EventTypeQueryCompleted:   {},
	events.EventTypeStreamChunk:      {},
	events.EventTypeToolStart:        {},
	events.EventTypeToolEnd:          {},
	events.EventTypeAgentMessage:     {},
	events.EventTypeSubagentActivity: {},
	events.EventTypeSteerDelivered:   {},
	events.EventTypeTodoUpdate:       {},
}

// passiveClientMessageTypes are websocket frames a client sends on its own
// (keepalives, subscriptions, lifecycle signals); they are not user activity.
var passiveClientMessageTypes = map[string]struct{}{
	AllowedMessageTypePing:         {},
	AllowedMessageTypePong:         {},
	AllowedMessageTypeHeartbeat:    {},
	AllowedMessageTypeSubscribe:    {},
	AllowedMessageTypeRequestStats: {},
	"unsubscribe":                  {},
	"pause":                        {},
	"resume":                       {},
	"session_close":                {},
	"session_takeover":             {},
}

// markUserActivity records human input (chat send, terminal input, a file
// edit from the UI, an interactive websocket message).
func (ws *ReactWebServer) markUserActivity() {
	ws.lifecycle.lastUserNanos.Store(time.Now().UnixNano())
}

// noteAgentEvent records agent activity for chat-scoped agent events. Called
// from the event bus publish hook, so it must stay cheap.
func (ws *ReactWebServer) noteAgentEvent(eventType string, payload map[string]interface{}) {
	if _, ok := agentActivityEventTypes[eventType]; !ok {
		return
	}
	if chatID, _ := payload["chat_id"].(string); strings.TrimSpace(chatID) == "" {
		return
	}
	ws.lifecycle.lastAgentNanos.Store(time.Now().UnixNano())
}

// noteClientMessage records an inbound websocket frame as user activity
// unless it is a passive keepalive/lifecycle frame.
func (ws *ReactWebServer) noteClientMessage(msgType string) {
	if _, passive := passiveClientMessageTypes[msgType]; passive {
		return
	}
	ws.markUserActivity()
}

// acceptingWork reports whether new queries may start.
func (ws *ReactWebServer) acceptingWork() bool {
	return !ws.lifecycle.stopping.Load()
}

// lifecycleActivity is the GET /api/lifecycle/activity body.
type lifecycleActivity struct {
	Busy                bool     `json:"busy"`
	ActiveQueries       int      `json:"active_queries"`
	BusyReasons         []string `json:"busy_reasons"`
	LastUserActivityAt  *string  `json:"last_user_activity_at"`
	LastAgentActivityAt *string  `json:"last_agent_activity_at"`
	AcceptingWork       bool     `json:"accepting_work"`
}

func formatActivityTime(nanos int64) *string {
	if nanos == 0 {
		return nil
	}
	s := time.Unix(0, nanos).UTC().Format(time.RFC3339)
	return &s
}

// activitySnapshot computes the current lifecycle activity.
func (ws *ReactWebServer) activitySnapshot() lifecycleActivity {
	queries := ws.countActiveQueries()
	reasons := make([]string, 0, 4)
	if queries > 0 {
		reasons = append(reasons, busyReasonQuery)
	}
	if ws.hasRunningAutomation() {
		reasons = append(reasons, busyReasonAutomation)
	}
	if ws.hasRunningBackgroundTask() {
		reasons = append(reasons, busyReasonTask)
	}
	if ws.lifecycle.checkpointing.Load() {
		reasons = append(reasons, busyReasonCheckpoint)
	}
	return lifecycleActivity{
		Busy:                len(reasons) > 0,
		ActiveQueries:       queries,
		BusyReasons:         reasons,
		LastUserActivityAt:  formatActivityTime(ws.lifecycle.lastUserNanos.Load()),
		LastAgentActivityAt: formatActivityTime(ws.lifecycle.lastAgentNanos.Load()),
		AcceptingWork:       ws.acceptingWork(),
	}
}

// runningChat identifies a chat with a run in flight.
type runningChat struct {
	clientID string
	chatID   string
	agent    *agent.Agent
}

// runningChats lists every chat with an in-flight run: chats flagged
// ActiveQuery by the query runner, plus chat agents that started a turn on
// their own (wakeup resumes) and the shared CLI agent.
func (ws *ReactWebServer) runningChats() []runningChat {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	var out []runningChat
	seen := map[*agent.Agent]bool{}
	for clientID, ctx := range ws.clientContexts {
		if ctx == nil {
			continue
		}
		if len(ctx.ChatSessions) == 0 {
			if ctx.ActiveQuery || ctx.Agent.IsQueryInProgress() {
				out = append(out, runningChat{clientID: clientID, chatID: ctx.DefaultChatID, agent: ctx.Agent})
				seen[ctx.Agent] = true
			}
			continue
		}
		for chatID, cs := range ctx.ChatSessions {
			if cs == nil {
				continue
			}
			cs.mu.RLock()
			active, a := cs.ActiveQuery, cs.Agent
			cs.mu.RUnlock()
			if active || a.IsQueryInProgress() {
				out = append(out, runningChat{clientID: clientID, chatID: chatID, agent: a})
				if a != nil {
					seen[a] = true
				}
			}
		}
	}
	if ws.agent != nil && !seen[ws.agent] && ws.agent.IsQueryInProgress() {
		out = append(out, runningChat{clientID: defaultWebClientID, agent: ws.agent})
	}
	return out
}

// countActiveQueries is the number of in-flight runs across all chats.
func (ws *ReactWebServer) countActiveQueries() int {
	return len(ws.runningChats())
}

// lifecycleWorkspaceRoots returns the distinct workspace roots the daemon
// serves: its launch workspace, every client's project and current root, and
// every chat worktree.
func (ws *ReactWebServer) lifecycleWorkspaceRoots() []string {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	seen := map[string]bool{}
	var roots []string
	add := func(root string) {
		root = strings.TrimSpace(root)
		if root == "" || seen[root] {
			return
		}
		seen[root] = true
		roots = append(roots, root)
	}
	add(ws.workspaceRoot)
	for _, ctx := range ws.clientContexts {
		if ctx == nil {
			continue
		}
		add(ctx.ProjectRoot)
		add(ctx.WorkspaceRoot)
		for _, cs := range ctx.ChatSessions {
			if cs != nil {
				add(cs.getWorktreePath())
			}
		}
	}
	return roots
}

// hasRunningAutomation reports whether an automate workflow process started
// from any served workspace is still alive.
func (ws *ReactWebServer) hasRunningAutomation() bool {
	for _, root := range ws.lifecycleWorkspaceRoots() {
		sessions, err := automate.ListSessionFiles(root + "/.sprout")
		if err != nil {
			continue
		}
		for _, s := range sessions {
			if s.EndedAt == nil && automate.IsProcessAlive(s.PID) {
				return true
			}
		}
	}
	return false
}

// hasRunningBackgroundTask reports whether any terminal manager has a
// background command (agent-started long-running shell) still running.
func (ws *ReactWebServer) hasRunningBackgroundTask() bool {
	managers := []*TerminalManager{ws.terminalManager}
	ws.mutex.RLock()
	for _, ctx := range ws.clientContexts {
		if ctx != nil && ctx.Terminal != nil {
			managers = append(managers, ctx.Terminal)
		}
	}
	ws.mutex.RUnlock()
	seen := map[*TerminalManager]bool{}
	for _, tm := range managers {
		if tm == nil || seen[tm] {
			continue
		}
		seen[tm] = true
		if tm.runningBackgroundCount() > 0 {
			return true
		}
	}
	return false
}

// runningBackgroundCount counts background sessions whose command has not
// finished.
func (tm *TerminalManager) runningBackgroundCount() int {
	tm.mutex.RLock()
	sessions := make([]*TerminalSession, 0, len(tm.sessions))
	for _, s := range tm.sessions {
		sessions = append(sessions, s)
	}
	tm.mutex.RUnlock()
	n := 0
	for _, s := range sessions {
		s.mutex.RLock()
		bg, done := s.IsBackground, s.bgDone
		s.mutex.RUnlock()
		if !bg || done == nil {
			continue
		}
		select {
		case <-done:
		default:
			n++
		}
	}
	return n
}

// publishWorkspaceStopping relays a workspace_stopping warning to every
// connected client.
func (ws *ReactWebServer) publishWorkspaceStopping(reason string, deadlineSeconds int) {
	if ws.eventBus == nil {
		return
	}
	ws.eventBus.Publish(events.EventTypeWorkspaceStopping, map[string]interface{}{
		"reason":           reason,
		"at":               time.Now().UTC().Format(time.RFC3339),
		"deadline_seconds": deadlineSeconds,
	})
}

// handleAPILifecycleActivity handles GET /api/lifecycle/activity.
func (ws *ReactWebServer) handleAPILifecycleActivity(w http.ResponseWriter, r *http.Request) {
	if !requireMethods(w, r, http.MethodGet, http.MethodHead) {
		return
	}
	writeJSON(w, http.StatusOK, ws.activitySnapshot())
}

// lifecycleNoticeRequest is the POST /api/lifecycle/notice body.
type lifecycleNoticeRequest struct {
	Reason          string `json:"reason"`
	DeadlineSeconds int    `json:"deadline_seconds"`
}

// handleAPILifecycleNotice handles POST /api/lifecycle/notice: the platform
// warns ahead of a timer-driven stop and the daemon relays it to clients.
func (ws *ReactWebServer) handleAPILifecycleNotice(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var req lifecycleNoticeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}
	if req.DeadlineSeconds < 0 {
		writeJSONErr(w, http.StatusBadRequest, "invalid_deadline", "deadline_seconds must not be negative")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "user"
	}
	ws.publishWorkspaceStopping(reason, req.DeadlineSeconds)
	writeJSON(w, http.StatusOK, map[string]interface{}{"relayed": true})
}

// userEditPathPrefixes are the mutating API surfaces a person drives from
// the UI (editor saves, tree operations, browser→workspace file sync, git
// actions, terminal management, chat sends).
var userEditPathPrefixes = []string{
	"/api/file", "/api/create", "/api/delete", "/api/rename", "/api/upload/",
	"/api/search/replace", "/api/sync/op", "/api/sync/batch", "/api/git/",
	"/api/terminal/", "/api/query", "/api/proxy/chat", "/api/command/",
}

// userActivityMiddleware records a mutating request to a user-edit surface
// as user activity. Reads, polling, and the platform's own sync/txn/lifecycle
// calls are not activity.
func (ws *ReactWebServer) userActivityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isUserEditRequest(r) {
			ws.markUserActivity()
		}
		next.ServeHTTP(w, r)
	})
}

func isUserEditRequest(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	path := r.URL.Path
	if path == "/api/query/status" {
		return false
	}
	for _, prefix := range userEditPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
