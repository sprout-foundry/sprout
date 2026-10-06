package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePlatform records what a runner reports and serves queued tasks.
type fakePlatform struct {
	mu         sync.Mutex
	tasks      []WorkspaceTask
	heartbeats []Heartbeat
	results    []WorkspaceResult
	statuses   map[string]string
	keys       []string
	pollStatus int
}

func (f *fakePlatform) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /runners/device", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(DeviceStart{DeviceCode: "dev", UserCode: "ABCD-EFGH", VerificationURI: "https://p/runners/link", Interval: 1, ExpiresIn: 600})
	})
	mux.HandleFunc("POST /runners/device/poll", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		status := f.pollStatus
		f.mu.Unlock()
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(Credentials{RunnerID: "r-1", APIKey: "srk_key"}) //nolint:gosec // G117: fake platform issuing a test key
	})
	mux.HandleFunc("POST /runners/r-1/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		var hb Heartbeat
		_ = json.NewDecoder(r.Body).Decode(&hb)
		f.record(r, func() { f.heartbeats = append(f.heartbeats, hb) })
	})
	mux.HandleFunc("GET /runners/r-1/workspace-tasks", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		tasks := f.tasks
		f.tasks = nil
		f.keys = append(f.keys, r.Header.Get("X-Runner-Key"))
		f.mu.Unlock()
		if tasks == nil {
			tasks = []WorkspaceTask{}
		}
		_ = json.NewEncoder(w).Encode(tasks)
	})
	mux.HandleFunc("POST /runners/r-1/workspace-result", func(w http.ResponseWriter, r *http.Request) {
		var res WorkspaceResult
		_ = json.NewDecoder(r.Body).Decode(&res)
		f.record(r, func() { f.results = append(f.results, res) })
	})
	mux.HandleFunc("POST /runners/r-1/workspace/{ws}/status", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.record(r, func() { f.statuses[r.PathValue("ws")] = body["status"] })
	})
	return mux
}

func (f *fakePlatform) record(r *http.Request, fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, r.Header.Get("X-Runner-Key"))
	fn()
}

func (f *fakePlatform) setPollStatus(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pollStatus = status
}

func (f *fakePlatform) push(t WorkspaceTask) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks = append(f.tasks, t)
}

// fakeLauncher "starts" a workspace by pointing at a fake daemon.
type fakeLauncher struct {
	port    int
	mu      sync.Mutex
	started []string
	stopped []string
}

func (l *fakeLauncher) Start(_ context.Context, t WorkspaceTask) (*Workspace, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.started = append(l.started, t.WorkspaceID)
	return &Workspace{ID: t.WorkspaceID, Port: l.port, Handle: "h-" + t.WorkspaceID}, nil
}

func (l *fakeLauncher) Stop(_ context.Context, ws *Workspace) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ws != nil {
		l.stopped = append(l.stopped, ws.ID)
	}
	return nil
}

func (l *fakeLauncher) Destroy(_ context.Context, id string, _ *Workspace) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stopped = append(l.stopped, id)
	return nil
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestRunnerServesWorkspaceLifecycle(t *testing.T) {
	plat := &fakePlatform{statuses: map[string]string{}, pollStatus: http.StatusOK}
	platSrv := httptest.NewServer(plat.handler())
	t.Cleanup(platSrv.Close)
	listen, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	launcher := &fakeLauncher{port: fakeDaemon(t)}
	host := NewHostServer()
	r := &Runner{
		State:    &State{PlatformURL: platSrv.URL, RunnerID: "r-1", Mode: ModeNative, PublicURL: "https://me.example/", ListenAddr: "127.0.0.1:" + strconv.Itoa(listen)},
		Client:   NewClient(platSrv.URL, Credentials{RunnerID: "r-1", APIKey: "srk_key"}),
		Launcher: launcher,
		Host:     host,
		Sandbox:  "seatbelt",
		Version:  "v-test",
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	waitFor(t, "first heartbeat", func() bool {
		plat.mu.Lock()
		defer plat.mu.Unlock()
		return len(plat.heartbeats) > 0
	})
	plat.mu.Lock()
	hb := plat.heartbeats[0]
	plat.mu.Unlock()
	if hb.Mode != ModeNative || hb.Sandbox != "seatbelt" || hb.RunnerVersion != "v-test" || hb.DirectURL != "https://me.example/" {
		t.Errorf("heartbeat must report mode, sandbox and version; got %+v", hb)
	}

	plat.push(WorkspaceTask{WorkspaceID: "ws-1", Action: "start", RepoURL: "https://github.com/o/r", TxnSecret: "s3cret"})
	waitFor(t, "start result", func() bool {
		plat.mu.Lock()
		defer plat.mu.Unlock()
		return len(plat.results) > 0
	})
	plat.mu.Lock()
	res := plat.results[0]
	plat.mu.Unlock()
	if res.Status != "running" || res.ConnectionURL != "https://me.example/daemon/ws-1" || res.ContainerID != "h-ws-1" {
		t.Errorf("unexpected start result %+v", res)
	}
	resp, err := http.Post("http://"+r.State.ListenAddr+"/daemon/ws-1/api/txn/run", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("an unauthenticated call through the live host server must be refused, got %d", resp.StatusCode)
	}

	plat.push(WorkspaceTask{WorkspaceID: "ws-1", Action: "stop"})
	waitFor(t, "stop status", func() bool {
		plat.mu.Lock()
		defer plat.mu.Unlock()
		return plat.statuses["ws-1"] == "stopped"
	})
	if _, bound := host.lookup("ws-1"); bound {
		t.Error("a stopped workspace must be unbound from the host server")
	}

	plat.push(WorkspaceTask{WorkspaceID: "ws-2", Action: "start", RepoURL: "https://github.com/o/r"})
	waitFor(t, "refused start", func() bool {
		plat.mu.Lock()
		defer plat.mu.Unlock()
		return len(plat.results) > 1
	})
	plat.mu.Lock()
	refused := plat.results[1]
	plat.mu.Unlock()
	if refused.Status != "failed" {
		t.Errorf("a start without a txn secret must be refused, got %+v", refused)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	plat.mu.Lock()
	defer plat.mu.Unlock()
	for _, k := range plat.keys {
		if k != "srk_key" {
			t.Fatalf("every authenticated call must carry the runner key; saw %q", k)
		}
	}
}

func TestRunnerRequiresAReachableURL(t *testing.T) {
	r := &Runner{State: &State{Mode: ModeContainer, ListenAddr: "127.0.0.1:0"}}
	if err := r.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "public URL") {
		t.Fatalf("want a clear error about the missing public URL, got %v", err)
	}
}

func TestPollLinkMapsDeviceFlowOutcomes(t *testing.T) {
	plat := &fakePlatform{statuses: map[string]string{}}
	srv := httptest.NewServer(plat.handler())
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, Credentials{})
	for status, want := range map[int]error{
		http.StatusPreconditionRequired: ErrAuthorizationPending,
		http.StatusTooManyRequests:      ErrSlowDown,
		http.StatusForbidden:            ErrAccessDenied,
		http.StatusGone:                 ErrExpiredToken,
	} {
		plat.setPollStatus(status)
		if _, err := c.PollLink(context.Background(), "dev"); !errors.Is(err, want) {
			t.Errorf("status %d: got %v, want %v", status, err, want)
		}
	}
	plat.setPollStatus(http.StatusOK)
	creds, err := c.PollLink(context.Background(), "dev")
	if err != nil || creds.RunnerID != "r-1" || creds.APIKey != "srk_key" {
		t.Fatalf("approved poll: got %+v, %v", creds, err)
	}
}
