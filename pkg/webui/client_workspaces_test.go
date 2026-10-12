//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type projectTestServer struct {
	ws         *ReactWebServer
	daemonRoot string
	project    string
	worktree   string
}

func newProjectTestServer(t *testing.T) projectTestServer {
	t.Helper()
	daemonRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(daemonRoot, "proj")
	worktree := filepath.Join(daemonRoot, "proj-wt")
	for _, dir := range []string{project, worktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = daemonRoot
	ws.workspaceRoot = daemonRoot
	return projectTestServer{ws: ws, daemonRoot: daemonRoot, project: project, worktree: worktree}
}

// addWorktreeChat binds a new chat of clientID to the worktree directory.
func (p projectTestServer) addWorktreeChat(t *testing.T, clientID string) string {
	t.Helper()
	p.ws.mutex.Lock()
	defer p.ws.mutex.Unlock()
	ctx := p.ws.getOrCreateClientContextLocked(clientID)
	cs := newChatSession("chat-wt", "Worktree chat")
	cs.setWorktreePath(p.worktree)
	ctx.ChatSessions[cs.ID] = cs
	return cs.ID
}

func (p projectTestServer) switchChat(t *testing.T, clientID, chatID string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/switch", strings.NewReader(fmt.Sprintf(`{"id":%q}`, chatID)))
	req.Header.Set(webClientIDHeader, clientID)
	rec := httptest.NewRecorder()
	p.ws.handleAPIChatSessionsSwitch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("switch to %s: %d %s", chatID, rec.Code, rec.Body.String())
	}
}

func (p projectTestServer) clientRoots(clientID string) (workspace, project string) {
	p.ws.mutex.RLock()
	defer p.ws.mutex.RUnlock()
	ctx := p.ws.clientContexts[clientID]
	return ctx.WorkspaceRoot, ctx.ProjectRoot
}

// nextWorkspaceChanged waits for the next workspace_changed event addressed
// to clientID and returns its workspace_root.
func nextWorkspaceChanged(t *testing.T, ch <-chan events.UIEvent, clientID string) string {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type != events.EventTypeWorkspaceChanged {
				continue
			}
			data, _ := ev.Data.(map[string]interface{})
			if data["client_id"] != clientID {
				continue
			}
			root, _ := data["workspace_root"].(string)
			return root
		case <-timeout:
			t.Fatalf("no workspace_changed event for %s", clientID)
			return ""
		}
	}
}

func subscribeEvents(t *testing.T, ws *ReactWebServer) <-chan events.UIEvent {
	t.Helper()
	name := "test-" + t.Name()
	ch := ws.eventBus.Subscribe(name)
	t.Cleanup(func() { ws.eventBus.Unsubscribe(name) })
	return ch
}

func TestChatSwitchReturnsToSelectedProject(t *testing.T) {
	p := newProjectTestServer(t)
	const client = "tab-a"
	if _, err := p.ws.setClientWorkspaceRoot(client, p.project); err != nil {
		t.Fatal(err)
	}
	wtChat := p.addWorktreeChat(t, client)
	evs := subscribeEvents(t, p.ws)

	p.switchChat(t, client, wtChat)
	if root, _ := p.clientRoots(client); root != p.worktree {
		t.Fatalf("root after switching to the worktree chat = %q, want %q", root, p.worktree)
	}
	if got := nextWorkspaceChanged(t, evs, client); got != p.worktree {
		t.Errorf("workspace_changed root = %q, want %q", got, p.worktree)
	}

	p.switchChat(t, client, defaultChatID)
	if root, project := p.clientRoots(client); root != p.project || project != p.project {
		t.Fatalf("roots after switching back = (%q, %q), want the selected project %q", root, project, p.project)
	}
	if got := nextWorkspaceChanged(t, evs, client); got != p.project {
		t.Errorf("workspace_changed root = %q, want %q", got, p.project)
	}
	if got := p.ws.GetWorkspaceRoot(); got != p.daemonRoot {
		t.Errorf("a non-default client's switch moved the server root to %q", got)
	}
}

func TestWorktreeClearReturnsToSelectedProject(t *testing.T) {
	p := newProjectTestServer(t)
	const client = "tab-b"
	if _, err := p.ws.setClientWorkspaceRoot(client, p.project); err != nil {
		t.Fatal(err)
	}
	wtChat := p.addWorktreeChat(t, client)
	p.switchChat(t, client, wtChat)
	evs := subscribeEvents(t, p.ws)

	req := httptest.NewRequest(http.MethodPost, "/api/chat-session/"+wtChat+"/worktree", strings.NewReader(`{"worktree_path":""}`))
	req.Header.Set(webClientIDHeader, client)
	rec := httptest.NewRecorder()
	p.ws.handleAPIChatSessionWorktreeSet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear worktree: %d %s", rec.Code, rec.Body.String())
	}

	if root, _ := p.clientRoots(client); root != p.project {
		t.Fatalf("root after clearing the worktree = %q, want the project %q", root, p.project)
	}
	if got := nextWorkspaceChanged(t, evs, client); got != p.project {
		t.Errorf("workspace_changed root = %q, want %q", got, p.project)
	}
}

func TestDeleteAllPublishesWorkspaceChanged(t *testing.T) {
	p := newProjectTestServer(t)
	const client = "tab-c"
	if _, err := p.ws.setClientWorkspaceRoot(client, p.project); err != nil {
		t.Fatal(err)
	}
	wtChat := p.addWorktreeChat(t, client)
	p.switchChat(t, client, wtChat)
	evs := subscribeEvents(t, p.ws)

	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/delete-all", nil)
	req.Header.Set(webClientIDHeader, client)
	rec := httptest.NewRecorder()
	p.ws.handleAPIChatSessionsDeleteAll(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete all: %d %s", rec.Code, rec.Body.String())
	}
	if got := nextWorkspaceChanged(t, evs, client); got != p.project {
		t.Errorf("workspace_changed root = %q, want %q", got, p.project)
	}
}

func TestNewClientContextRecordsProjectRoot(t *testing.T) {
	p := newProjectTestServer(t)
	for _, client := range []string{defaultWebClientID, "fresh-tab"} {
		p.ws.getOrCreateClientContext(client)
		if _, project := p.clientRoots(client); project != p.daemonRoot {
			t.Errorf("%s: ProjectRoot = %q, want the root it was created in %q", client, project, p.daemonRoot)
		}
	}
}

func TestClientWorkspacesSurviveRestart(t *testing.T) {
	p := newProjectTestServer(t)
	storePath := filepath.Join(t.TempDir(), clientWorkspacesFileName)
	p.ws.clientWorkspacesPath = storePath

	if _, err := p.ws.setClientWorkspaceRoot("tab-d", p.project); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	restarted.daemonRoot = p.daemonRoot
	restarted.workspaceRoot = p.daemonRoot
	restarted.mutex.Lock()
	restarted.clientWorkspacesPath = storePath
	restarted.loadClientWorkspacesLocked()
	restarted.mutex.Unlock()

	ctx := restarted.getOrCreateClientContext("tab-d")
	if ctx.WorkspaceRoot != p.project || ctx.ProjectRoot != p.project {
		t.Fatalf("restarted context roots = (%q, %q), want %q", ctx.WorkspaceRoot, ctx.ProjectRoot, p.project)
	}
}

func TestLoadClientWorkspacesDropsInvalidEntries(t *testing.T) {
	p := newProjectTestServer(t)
	outside := t.TempDir()
	stale := time.Now().Add(-2 * clientWorkspaceMaxAge)
	stored := persistedClientWorkspaces{Version: 1, Clients: map[string]persistedClientWorkspace{
		"live":     {Path: p.project, UpdatedAt: time.Now()},
		"stale":    {Path: p.project, UpdatedAt: stale},
		"vanished": {Path: filepath.Join(p.daemonRoot, "gone"), UpdatedAt: time.Now()},
		"outside":  {Path: outside, UpdatedAt: time.Now()},
		"default":  {Path: p.project, UpdatedAt: time.Now()},
	}}
	for i := 0; i < maxPersistedClientWorkspaces+10; i++ {
		stored.Clients[fmt.Sprintf("old-%d", i)] = persistedClientWorkspace{Path: p.worktree, UpdatedAt: time.Now().Add(-time.Hour - time.Duration(i)*time.Minute)}
	}
	data, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(t.TempDir(), clientWorkspacesFileName)
	if err := os.WriteFile(storePath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	p.ws.mutex.Lock()
	defer p.ws.mutex.Unlock()
	p.ws.clientWorkspaces = map[string]string{}
	p.ws.clientWorkspacesPath = storePath
	p.ws.loadClientWorkspacesLocked()

	if got := p.ws.clientWorkspaces["live"]; got != p.project {
		t.Errorf("live entry = %q, want %q", got, p.project)
	}
	for _, id := range []string{"stale", "vanished", "outside", "default"} {
		if got, ok := p.ws.clientWorkspaces[id]; ok {
			t.Errorf("entry %q was loaded (%q), want it dropped", id, got)
		}
	}
	if n := len(p.ws.clientWorkspaces); n > maxPersistedClientWorkspaces {
		t.Errorf("loaded %d entries, want at most %d", n, maxPersistedClientWorkspaces)
	}
}

// TestWorkspaceGetDoesNotRaceChatSwitch exercises the GET handler against
// concurrent root changes; run with -race to check the locking.
func TestWorkspaceGetDoesNotRaceChatSwitch(t *testing.T) {
	p := newProjectTestServer(t)
	const client = "tab-e"
	if _, err := p.ws.setClientWorkspaceRoot(client, p.project); err != nil {
		t.Fatal(err)
	}
	wtChat := p.addWorktreeChat(t, client)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			target := wtChat
			if i%2 == 1 {
				target = defaultChatID
			}
			req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/switch", strings.NewReader(fmt.Sprintf(`{"id":%q}`, target)))
			req.Header.Set(webClientIDHeader, client)
			rec := httptest.NewRecorder()
			p.ws.handleAPIChatSessionsSwitch(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("switch: %d", rec.Code)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
			req.Header.Set(webClientIDHeader, client)
			rec := httptest.NewRecorder()
			p.ws.handleAPIWorkspaceGet(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("GET /api/workspace: %d", rec.Code)
			}
		}
	}()
	wg.Wait()
}
