//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/events"
)

func newLifecycleServer(t *testing.T, root string) *ReactWebServer {
	t.Helper()
	ws := &ReactWebServer{
		eventBus:       events.NewEventBus(),
		clientContexts: map[string]*webClientContext{},
		workspaceRoot:  root,
	}
	ws.clientContexts["c1"] = &webClientContext{
		ChatSessions:  map[string]*chatSession{"chat-1": newChatSession("chat-1", "Chat")},
		DefaultChatID: "chat-1",
	}
	return ws
}

func setChatActive(ws *ReactWebServer, active bool) {
	ws.mutex.Lock()
	ws.clientContexts["c1"].setChatQueryActive("chat-1", active, "q")
	ws.mutex.Unlock()
}

func getActivity(t *testing.T, ws *ReactWebServer) map[string]interface{} {
	t.Helper()
	rec := httptest.NewRecorder()
	ws.handleAPILifecycleActivity(rec, httptest.NewRequest(http.MethodGet, "/api/lifecycle/activity", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("activity status %d: %s", rec.Code, rec.Body)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestLifecycleActivity_IdleShape(t *testing.T) {
	ws := newLifecycleServer(t, t.TempDir())
	got := getActivity(t, ws)
	for _, key := range []string{"busy", "active_queries", "busy_reasons", "last_user_activity_at", "last_agent_activity_at", "accepting_work"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing key %q in %v", key, got)
		}
	}
	if got["busy"] != false || got["accepting_work"] != true || got["active_queries"] != float64(0) {
		t.Fatalf("idle activity = %v", got)
	}
	if got["last_user_activity_at"] != nil || got["last_agent_activity_at"] != nil {
		t.Fatalf("timestamps should be null before any activity: %v", got)
	}
	if reasons, ok := got["busy_reasons"].([]interface{}); !ok || len(reasons) != 0 {
		t.Fatalf("busy_reasons = %#v, want []", got["busy_reasons"])
	}
}

func TestLifecycleActivity_BusyReasons(t *testing.T) {
	ws := newLifecycleServer(t, t.TempDir())

	setChatActive(ws, true)
	got := getActivity(t, ws)
	if got["busy"] != true || got["active_queries"] != float64(1) {
		t.Fatalf("active chat: %v", got)
	}
	if r := got["busy_reasons"].([]interface{}); len(r) != 1 || r[0] != busyReasonQuery {
		t.Fatalf("reasons = %v", r)
	}
	setChatActive(ws, false)

	done := make(chan struct{})
	ws.terminalManager = &TerminalManager{sessions: map[string]*TerminalSession{
		"bg": {IsBackground: true, bgDone: done},
	}}
	if r := getActivity(t, ws)["busy_reasons"].([]interface{}); len(r) != 1 || r[0] != busyReasonTask {
		t.Fatalf("running background task reasons = %v", r)
	}
	close(done)
	if getActivity(t, ws)["busy"] != false {
		t.Fatal("finished background task still reported busy")
	}

	ws.lifecycle.checkpointing.Store(true)
	if r := getActivity(t, ws)["busy_reasons"].([]interface{}); len(r) != 1 || r[0] != busyReasonCheckpoint {
		t.Fatalf("checkpoint reasons = %v", r)
	}
}

func TestLifecycleActivity_TracksUserAndAgentActivity(t *testing.T) {
	ws := newLifecycleServer(t, t.TempDir())
	handler := ws.userActivityMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/file?path=a", nil),
		httptest.NewRequest(http.MethodPost, "/api/sync", nil),
		httptest.NewRequest(http.MethodPost, "/api/lifecycle/prepare-stop", nil),
		httptest.NewRequest(http.MethodGet, "/api/query/status", nil),
	} {
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}
	ws.noteClientMessage(AllowedMessageTypePing)
	ws.noteClientMessage(AllowedMessageTypeHeartbeat)
	ws.stampChatEvent(events.EventTypeStreamChunk, map[string]interface{}{"chunk": "x"})
	if got := getActivity(t, ws); got["last_user_activity_at"] != nil || got["last_agent_activity_at"] != nil {
		t.Fatalf("passive traffic counted as activity: %v", got)
	}

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/file", strings.NewReader("{}")))
	ws.stampChatEvent(events.EventTypeToolStart, map[string]interface{}{"chat_id": "chat-1", "client_id": "c1"})
	got := getActivity(t, ws)
	user, _ := got["last_user_activity_at"].(string)
	agentAt, _ := got["last_agent_activity_at"].(string)
	for name, v := range map[string]string{"user": user, "agent": agentAt} {
		ts, err := time.Parse(time.RFC3339, v)
		if err != nil || time.Since(ts) > time.Minute || !strings.HasSuffix(v, "Z") {
			t.Errorf("%s activity timestamp %q is not a recent RFC 3339 UTC time", name, v)
		}
	}

	ws.lifecycle.lastUserNanos.Store(0)
	ws.noteClientMessage(AllowedMessageTypeProviderChange)
	if getActivity(t, ws)["last_user_activity_at"] == nil {
		t.Fatal("interactive websocket message was not counted")
	}
}

func TestPrepareStop_DrainsPersistsCheckpointsAndIsIdempotent(t *testing.T) {
	root := initCheckpointRepo(t)
	writeFile(t, filepath.Join(root, "tracked.txt"), "work in progress\n")
	ws := newLifecycleServer(t, root)
	setChatActive(ws, true)

	var mu sync.Mutex
	var interrupted []string
	ws.lifecycle.interrupt = func(clientID, chatID string, _ *agent.Agent) {
		mu.Lock()
		interrupted = append(interrupted, clientID+"/"+chatID)
		mu.Unlock()
		// The run notices the interrupt and unwinds shortly after.
		go func() {
			time.Sleep(50 * time.Millisecond)
			setChatActive(ws, false)
		}()
	}
	sub := ws.eventBus.Subscribe("prepare-stop-test")
	t.Cleanup(func() { ws.eventBus.Unsubscribe("prepare-stop-test") })

	first, ok := ws.prepareStop(context.Background(), "idle", 10*time.Second)
	if !ok {
		t.Fatal("prepareStop reported not ok")
	}
	if first.InterruptedQueries != 1 || first.CancelledQueries != 0 {
		t.Fatalf("interrupted=%d cancelled=%d, want 1/0", first.InterruptedQueries, first.CancelledQueries)
	}
	if len(interrupted) != 1 || interrupted[0] != "c1/chat-1" {
		t.Fatalf("interrupt calls = %v", interrupted)
	}
	if !first.Checkpointed || !strings.HasPrefix(first.WipRef, wipRefPrefix) || first.WipSHA == "" {
		t.Fatalf("checkpoint result = %+v", first)
	}
	if got := gitOut(t, root, "show", first.WipSHA+":tracked.txt"); got != "work in progress" {
		t.Fatalf("checkpointed tracked.txt = %q", got)
	}
	if ws.acceptingWork() || getActivity(t, ws)["accepting_work"] != false {
		t.Fatal("daemon still accepts work after prepare-stop")
	}

	sawNotice := false
	for !sawNotice {
		select {
		case ev := <-sub:
			if ev.Type == events.EventTypeWorkspaceStopping {
				sawNotice = true
				if ev.Data.(map[string]interface{})["reason"] != "idle" {
					t.Fatalf("notice payload = %v", ev.Data)
				}
			}
		case <-time.After(time.Second):
			t.Fatal("prepare-stop did not relay workspace_stopping")
		}
	}

	writeFile(t, filepath.Join(root, "tracked.txt"), "changed again\n")
	second, _ := ws.prepareStop(context.Background(), "idle", 10*time.Second)
	if second.WipSHA != first.WipSHA || second.WipRef != first.WipRef || second.InterruptedQueries != first.InterruptedQueries {
		t.Fatalf("second call = %+v, want the first result %+v", second, first)
	}
	if refs := gitOut(t, root, "for-each-ref", "--format=%(refname)", "refs/sprout/wip/"); strings.Count(refs, "\n")+1 != 2 {
		t.Fatalf("idempotent call wrote more refs:\n%s", refs)
	}
}

func TestPrepareStop_CancelsRunsLeftAtDeadline(t *testing.T) {
	ws := newLifecycleServer(t, t.TempDir())
	setChatActive(ws, true)
	calls := 0
	ws.lifecycle.interrupt = func(string, string, *agent.Agent) { calls++ }

	start := time.Now()
	res, _ := ws.prepareStop(context.Background(), "max_runtime", time.Second)
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("prepare-stop overran its deadline: %s", took)
	}
	if res.InterruptedQueries != 1 || res.CancelledQueries != 1 || calls != 2 {
		t.Fatalf("result %+v, interrupt calls %d; want 1 interrupted, 1 cancelled, 2 calls", res, calls)
	}
	if res.Checkpointed {
		t.Fatal("non-repo workspace reported a checkpoint")
	}
}

func TestPrepareStop_RejectsNewQueries(t *testing.T) {
	ws := newLifecycleServer(t, t.TempDir())
	ws.lifecycle.stopping.Store(true)
	sub := ws.eventBus.Subscribe("reject-test")
	t.Cleanup(func() { ws.eventBus.Unsubscribe("reject-test") })

	rec := httptest.NewRecorder()
	ws.runChatQuery(rec, httptest.NewRequest(http.MethodPost, "/api/query", nil), "c1", "chat-1", "hello", chatQueryOptions{})
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), errCodeWorkspaceStopping) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	select {
	case ev := <-sub:
		data := ev.Data.(map[string]interface{})
		if ev.Type != events.EventTypeError || data["terminal"] != true || data["chat_id"] != "chat-1" || data["code"] != errCodeWorkspaceStopping {
			t.Fatalf("rejection event = %s %v", ev.Type, data)
		}
	case <-time.After(time.Second):
		t.Fatal("no error event for the rejected query")
	}
	if ws.chatQueryStatus("c1", "chat-1") {
		t.Fatal("rejected query was marked active")
	}
}

func TestLifecycleRoutes_AuthAndNotice(t *testing.T) {
	ws, _ := newTestWebServer(t)
	ws.authToken = "secret"
	srv := httptest.NewServer(authTokenMiddleware(ws.authToken)(ws.setupRoutes(context.Background())))
	t.Cleanup(srv.Close)
	sub := ws.eventBus.Subscribe("notice-test")
	t.Cleanup(func() { ws.eventBus.Unsubscribe("notice-test") })

	do := func(method, path, body, token string) *http.Response {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}

	if resp := do(http.MethodGet, "/api/lifecycle/activity", "", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("GET activity: %d", resp.StatusCode)
	}
	for _, path := range []string{"/api/lifecycle/prepare-stop", "/api/lifecycle/notice"} {
		if resp := do(http.MethodPost, path, `{}`, ""); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unauthenticated POST %s: %d, want 401", path, resp.StatusCode)
		}
	}

	resp := do(http.MethodPost, "/api/lifecycle/notice", `{"reason":"idle","deadline_seconds":600}`, "secret")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("notice: %d", resp.StatusCode)
	}
	select {
	case ev := <-sub:
		data := ev.Data.(map[string]interface{})
		if ev.Type != events.EventTypeWorkspaceStopping || data["reason"] != "idle" || data["deadline_seconds"] != 600 {
			t.Fatalf("notice event = %s %v", ev.Type, data)
		}
		if !ws.shouldForwardEventToConnection(ev, &ConnectionInfo{ClientID: "any", ChatID: "chat-x"}) {
			t.Fatal("workspace_stopping is not forwarded to connected clients")
		}
	case <-time.After(time.Second):
		t.Fatal("notice did not publish workspace_stopping")
	}
	if ws.acceptingWork() == false {
		t.Fatal("a notice must not stop the daemon accepting work")
	}

	resp = do(http.MethodPost, "/api/lifecycle/prepare-stop", `{"deadline_seconds":-1}`, "secret")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("negative deadline: %d, want 400", resp.StatusCode)
	}
}
