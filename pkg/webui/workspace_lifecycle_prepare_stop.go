//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

const (
	defaultPrepareStopDeadline = 60 * time.Second
	maxPrepareStopDeadline     = 10 * time.Minute
	// prepareStopPollInterval is how often the drain re-checks for runs that
	// have wound down after being asked to stop.
	prepareStopPollInterval = 100 * time.Millisecond
)

// prepareStopRequest is the POST /api/lifecycle/prepare-stop body.
type prepareStopRequest struct {
	DeadlineSeconds int    `json:"deadline_seconds"`
	Reason          string `json:"reason"`
}

// wipCheckpoint is one uncommitted-work checkpoint written by prepare-stop.
type wipCheckpoint struct {
	Root   string `json:"root"`
	WipRef string `json:"wip_ref"`
	WipSHA string `json:"wip_sha"`
}

// prepareStopResult is the POST /api/lifecycle/prepare-stop response. The
// top-level wip_ref/wip_sha describe the primary workspace's checkpoint;
// checkpoints lists every workspace root that had uncommitted work.
type prepareStopResult struct {
	Checkpointed       bool            `json:"checkpointed"`
	WipRef             string          `json:"wip_ref,omitempty"`
	WipSHA             string          `json:"wip_sha,omitempty"`
	InterruptedQueries int             `json:"interrupted_queries"`
	CancelledQueries   int             `json:"cancelled_queries"`
	Checkpoints        []wipCheckpoint `json:"checkpoints"`
	Errors             []string        `json:"errors,omitempty"`
}

// handleAPILifecyclePrepareStop handles POST /api/lifecycle/prepare-stop.
func (ws *ReactWebServer) handleAPILifecyclePrepareStop(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var req prepareStopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}
	if req.DeadlineSeconds < 0 {
		writeJSONErr(w, http.StatusBadRequest, "invalid_deadline", "deadline_seconds must not be negative")
		return
	}
	deadline := time.Duration(req.DeadlineSeconds) * time.Second
	if deadline == 0 {
		deadline = defaultPrepareStopDeadline
	}
	if deadline > maxPrepareStopDeadline {
		deadline = maxPrepareStopDeadline
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "user"
	}

	result, ok := ws.prepareStop(r.Context(), reason, deadline)
	if !ok {
		writeJSONErr(w, http.StatusServiceUnavailable, "prepare_stop_in_progress", "prepare-stop is still running; retry to collect its result")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// prepareStop drains the daemon ahead of a workspace stop. It runs once per
// process: later calls wait for (and return) the first call's result. ok is
// false only when the caller's ctx ended while another call was still
// running. The drain itself is detached from ctx so a client hang-up cannot
// leave it half done.
func (ws *ReactWebServer) prepareStop(ctx context.Context, reason string, deadline time.Duration) (prepareStopResult, bool) {
	ls := &ws.lifecycle
	ls.prepareMu.Lock()
	if done := ls.prepareDone; done != nil {
		ls.prepareMu.Unlock()
		select {
		case <-done:
			return ls.prepareResult, true
		case <-ctx.Done():
			return prepareStopResult{}, false
		}
	}
	done := make(chan struct{})
	ls.prepareDone = done
	ls.prepareMu.Unlock()

	result := ws.runPrepareStop(reason, deadline)

	ls.prepareMu.Lock()
	ls.prepareResult = result
	ls.prepareMu.Unlock()
	close(done)
	return result, true
}

// runPrepareStop performs the drain steps in contract order: stop accepting
// work, stop in-flight runs, persist chats, checkpoint uncommitted work, sync.
func (ws *ReactWebServer) runPrepareStop(reason string, deadline time.Duration) prepareStopResult {
	start := time.Now()
	ws.lifecycle.stopping.Store(true)
	ws.log().Info("prepare-stop started", slog.String("reason", reason), slog.Duration("deadline", deadline))
	ws.publishWorkspaceStopping(reason, int(deadline/time.Second))

	result := prepareStopResult{Checkpoints: []wipCheckpoint{}}

	// Leave room after the drain for persistence and the checkpoint.
	reserve := deadline / 4
	if reserve > 15*time.Second {
		reserve = 15 * time.Second
	}
	drainBy := start.Add(deadline - reserve)

	running := ws.runningChats()
	result.InterruptedQueries = len(running)
	for _, rc := range running {
		ws.interruptRunningChat(rc)
	}
	ws.disableAllWakeups()
	for len(running) > 0 && time.Now().Before(drainBy) {
		time.Sleep(prepareStopPollInterval)
		running = ws.runningChats()
	}
	if len(running) > 0 {
		// Past the drain window: cancel whatever is left. The run goroutines
		// release their own active-query state when they unwind.
		result.CancelledQueries = len(running)
		for _, rc := range running {
			ws.interruptRunningChat(rc)
		}
		ws.log().Warn("prepare-stop deadline reached with runs still active", slog.Int("runs", len(running)))
	}

	ws.persistAllChats()

	ws.lifecycle.checkpointing.Store(true)
	ts := time.Now().Unix()
	ctx, cancel := context.WithDeadline(context.Background(), start.Add(deadline))
	for i, root := range ws.lifecycleWorkspaceRoots() {
		cp, err := checkpointWorkingTree(ctx, root, ts, i == 0, reason)
		if err != nil {
			ws.log().Warn("prepare-stop checkpoint failed", slog.String("root", root), slog.Any("err", err))
			result.Errors = append(result.Errors, root+": "+err.Error())
			continue
		}
		if cp == nil || containsCheckpointSHA(result.Checkpoints, cp.WipSHA) {
			continue
		}
		result.Checkpoints = append(result.Checkpoints, *cp)
		if !result.Checkpointed {
			result.Checkpointed = true
			result.WipRef = cp.WipRef
			result.WipSHA = cp.WipSHA
		}
	}
	cancel()
	ws.lifecycle.checkpointing.Store(false)

	syncFilesystem()
	ws.log().Info("prepare-stop finished",
		slog.Bool("checkpointed", result.Checkpointed),
		slog.Int("interrupted", result.InterruptedQueries),
		slog.Int("cancelled", result.CancelledQueries),
		slog.Duration("took", time.Since(start)),
	)
	return result
}

func containsCheckpointSHA(cps []wipCheckpoint, sha string) bool {
	for _, cp := range cps {
		if cp.WipSHA == sha {
			return true
		}
	}
	return false
}

// interruptRunningChat asks one run to stop at its next safe point — the
// same path as the user's Stop button (TriggerInterrupt also cancels the
// run's subagents).
func (ws *ReactWebServer) interruptRunningChat(rc runningChat) {
	if fn := ws.lifecycle.interrupt; fn != nil {
		fn(rc.clientID, rc.chatID, rc.agent)
		return
	}
	if rc.agent != nil {
		rc.agent.TriggerInterrupt()
	}
}

// chatAgentRef is a chat's agent with the IDs needed to store its snapshot.
type chatAgentRef struct {
	clientID string
	chatID   string
	agent    *agent.Agent
}

// chatAgents lists every live chat agent.
func (ws *ReactWebServer) chatAgents() []chatAgentRef {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	var out []chatAgentRef
	for clientID, ctx := range ws.clientContexts {
		if ctx == nil {
			continue
		}
		for chatID, cs := range ctx.ChatSessions {
			if cs == nil {
				continue
			}
			cs.mu.RLock()
			a := cs.Agent
			cs.mu.RUnlock()
			if a != nil {
				out = append(out, chatAgentRef{clientID: clientID, chatID: chatID, agent: a})
			}
		}
		if len(ctx.ChatSessions) == 0 && ctx.Agent != nil {
			out = append(out, chatAgentRef{clientID: clientID, chatID: ctx.DefaultChatID, agent: ctx.Agent})
		}
	}
	return out
}

// disableAllWakeups keeps chat agents from starting self-resumed turns once
// the daemon is draining.
func (ws *ReactWebServer) disableAllWakeups() {
	for _, ref := range ws.chatAgents() {
		ref.agent.DisableWakeup()
	}
	ws.agent.DisableWakeup()
}

// persistAllChats refreshes every chat's in-memory snapshot and writes each
// conversation to its session file.
func (ws *ReactWebServer) persistAllChats() {
	for _, ref := range ws.chatAgents() {
		if ref.agent.IsShutdown() {
			continue
		}
		snapshot, err := ref.agent.ExportState()
		if err != nil {
			ws.log().Warn("prepare-stop chat export failed", slog.String("chat_id", ref.chatID), slog.Any("err", err))
		} else {
			ws.mutex.Lock()
			if ctx := ws.clientContexts[ref.clientID]; ctx != nil {
				ctx.setChatSessionState(ref.chatID, snapshot)
			}
			ws.mutex.Unlock()
		}
		ref.agent.FlushState()
	}
	if ws.agent != nil {
		ws.agent.FlushState()
	}
}

// rejectWhileStopping answers a new query once prepare-stop has run: the
// chat gets a terminal error event (so every open view ends the turn with a
// clear reason) and the request a 503. Returns true when it rejected.
func (ws *ReactWebServer) rejectWhileStopping(w http.ResponseWriter, clientID, chatID string) bool {
	if ws.acceptingWork() {
		return false
	}
	const msg = "The workspace is stopping and is not accepting new work"
	ws.publishClientEventWithChat(clientID, chatID, "error", map[string]interface{}{
		"message":  msg,
		"code":     errCodeWorkspaceStopping,
		"terminal": true,
	})
	writeJSONErr(w, http.StatusServiceUnavailable, errCodeWorkspaceStopping, msg)
	return true
}
