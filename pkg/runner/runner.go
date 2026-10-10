package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	heartbeatInterval = 30 * time.Second
	pollInterval      = 5 * time.Second
)

// Runner serves workspaces for one platform account until its context ends.
type Runner struct {
	State    *State
	Client   *Client
	Launcher Launcher
	Host     *HostServer
	// Sandbox and Version are reported in heartbeats.
	Sandbox string
	Version string
	Log     *slog.Logger

	mu      sync.Mutex
	running map[string]*Workspace
	busy    map[string]*sync.Mutex
}

// Run serves the host server, heartbeats and executes workspace tasks.
func (r *Runner) Run(ctx context.Context) error {
	if r.Log == nil {
		r.Log = slog.Default()
	}
	r.running = make(map[string]*Workspace)
	r.busy = make(map[string]*sync.Mutex)

	if r.relayed() {
		go r.relayLoop(ctx)
		r.Log.Info("runner online via platform relay", "mode", r.State.Mode, "sandbox", r.Sandbox)
	} else {
		if err := r.listen(ctx); err != nil {
			return err
		}
		r.Log.Info("runner online", "mode", r.State.Mode, "sandbox", r.Sandbox, "listen", r.State.ListenAddr, "public_url", r.State.PublicURL)
	}

	r.heartbeat(ctx)
	hb := time.NewTicker(heartbeatInterval)
	poll := time.NewTicker(pollInterval)
	defer hb.Stop()
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			r.stopAll()
			return nil
		case <-hb.C:
			r.heartbeat(ctx)
		case <-poll.C:
			r.poll(ctx)
		}
	}
}

func (r *Runner) heartbeat(ctx context.Context) {
	r.mu.Lock()
	n := len(r.running)
	r.mu.Unlock()
	status := "online"
	if n > 0 {
		status = "busy"
	}
	err := r.Client.SendHeartbeat(ctx, Heartbeat{
		Status: status, RunningTasks: n,
		Mode: r.State.Mode, Sandbox: r.Sandbox, RunnerVersion: r.Version,
		LocalDirs:    r.State.LocalDirs,
		Capabilities: r.capabilities(),
		DirectURL:    r.State.PublicURL,
		Relayed:      r.relayed(),
	})
	if err != nil && ctx.Err() == nil {
		r.Log.Warn("heartbeat failed", "err", err)
	}
}

// capabilities lists the optional start-task features this runner serves, so
// the platform sends a feature only to runners that understand it (an older
// runner would ignore workspace_dir or misread a gateway key).
func (r *Runner) capabilities() []string {
	caps := []string{CapabilityTaskErrors}
	if len(r.State.LocalDirs) > 0 {
		caps = append(caps, CapabilityLocalDirs)
	}
	if r.State.Mode != ModeContainer {
		caps = append(caps, CapabilityGateway)
	}
	return caps
}

func (r *Runner) poll(ctx context.Context) {
	tasks, err := r.Client.PollWorkspaceTasks(ctx)
	if err != nil {
		if ctx.Err() == nil {
			r.Log.Warn("polling workspace tasks failed", "err", err)
		}
		return
	}
	for _, t := range tasks {
		go r.handle(ctx, t)
	}
}

// lockFor serializes tasks per workspace: a stop must not race its start.
func (r *Runner) lockFor(id string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.busy[id] == nil {
		r.busy[id] = &sync.Mutex{}
	}
	return r.busy[id]
}

func (r *Runner) handle(ctx context.Context, t WorkspaceTask) {
	l := r.lockFor(t.WorkspaceID)
	l.Lock()
	defer l.Unlock()
	log := r.Log.With("workspace", t.WorkspaceID, "action", t.Action)
	switch t.Action {
	case "start":
		r.start(ctx, t, log)
	case "stop":
		r.Host.Unbind(t.WorkspaceID)
		if err := r.Launcher.Stop(ctx, r.take(t.WorkspaceID)); err != nil {
			log.Warn("stop failed", "err", err)
		}
		r.report(ctx, t.WorkspaceID, "stopped", log)
	case "destroy":
		r.Host.Unbind(t.WorkspaceID)
		if err := r.Launcher.Destroy(ctx, t.WorkspaceID, r.take(t.WorkspaceID)); err != nil {
			log.Warn("destroy failed", "err", err)
		}
		r.report(ctx, t.WorkspaceID, "terminated", log)
	default:
		log.Warn("ignoring unknown workspace action")
	}
}

func (r *Runner) start(ctx context.Context, t WorkspaceTask, log *slog.Logger) {
	if reason := r.refusal(&t); reason != "" {
		log.Error("refusing start task", "reason", reason)
		_ = r.Client.SubmitWorkspaceResult(ctx, WorkspaceResult{WorkspaceID: t.WorkspaceID, Status: "failed", Error: reason})
		return
	}
	if old := r.take(t.WorkspaceID); old != nil {
		r.Host.Unbind(t.WorkspaceID)
		_ = r.Launcher.Stop(ctx, old)
	}
	ws, err := r.Launcher.Start(ctx, t)
	if err != nil {
		log.Error("start failed", "err", err)
		_ = r.Client.SubmitWorkspaceResult(ctx, WorkspaceResult{WorkspaceID: t.WorkspaceID, Status: "failed", Error: "workspace failed to start on the runner"})
		return
	}
	r.mu.Lock()
	r.running[t.WorkspaceID] = ws
	r.mu.Unlock()
	r.Host.Bind(t.WorkspaceID, ws.Port, t.TxnSecret)
	result := WorkspaceResult{
		WorkspaceID: t.WorkspaceID,
		ContainerID: ws.Handle,
		Port:        ws.Port,
		Status:      "running",
	}
	if !r.relayed() {
		// Relayed workspaces report no URL: the platform reaches them
		// through this runner's tunnel.
		result.ConnectionURL = strings.TrimRight(r.State.PublicURL, "/") + "/daemon/" + t.WorkspaceID
	}
	if err := r.Client.SubmitWorkspaceResult(ctx, result); err != nil {
		// The platform never learned about it: don't leave it serving.
		log.Error("reporting started workspace failed; stopping it", "err", err)
		r.Host.Unbind(t.WorkspaceID)
		_ = r.Launcher.Stop(ctx, r.take(t.WorkspaceID))
		return
	}
	log.Info("workspace running", "port", ws.Port)
}

// refusal checks a start task before any launcher sees it and returns why the
// runner will not serve it, or "" when it will. A local-directory task has
// its path replaced by the symlink-resolved form the allowlist matched. The
// reasons go back to the platform verbatim, so they name the rule, never a
// secret.
func (r *Runner) refusal(t *WorkspaceTask) string {
	if t.TxnSecret == "" {
		// Without a secret the host server could not authenticate the
		// platform's calls; refuse rather than serve an open daemon.
		return "start task carries no txn secret"
	}
	if t.WorkspaceDir != "" {
		if t.RepoURL != "" {
			// A local directory is the user's real files: nothing may
			// clone into it, reset it or treat it as a disposable
			// checkout, so a task naming both is never served.
			return "start task names both a repo and a local directory"
		}
		resolved, err := resolveLocalTaskDir(t.WorkspaceDir)
		if err != nil || !allowedLocalDir(resolved, r.State.LocalDirs) {
			// The runner serves exactly the directories its owner
			// named on this machine, resolved the same way for both —
			// a symlinked path the allowlist does not name never
			// reaches the launchers.
			return "start task names a directory this runner does not serve"
		}
		t.WorkspaceDir = resolved
	}
	if IsGatewayProvider(t.LLMProvider) {
		switch {
		case r.State.Mode == ModeContainer:
			// The gateway provider file lives in the workspace's
			// host-side config dir, which the container cannot see.
			return "gateway workspaces need host execution (native or bare-metal mode)"
		case t.LLMKey == "":
			return "gateway start task carries no workspace key"
		case t.RepoURL != "":
			return "gateway workspaces are repo-less; the start task names a repo"
		}
		if _, err := gatewayEndpoint(t.GatewayURL, t.PlatformAPIURL); err != nil {
			return err.Error()
		}
	}
	return ""
}

func (r *Runner) take(id string) *Workspace {
	r.mu.Lock()
	defer r.mu.Unlock()
	ws := r.running[id]
	delete(r.running, id)
	return ws
}

func (r *Runner) report(ctx context.Context, id, status string, log *slog.Logger) {
	if err := r.Client.UpdateWorkspaceStatus(ctx, id, status); err != nil {
		log.Warn("reporting status failed", "status", status, "err", err)
	}
}

func (r *Runner) stopAll() {
	r.mu.Lock()
	all := r.running
	r.running = map[string]*Workspace{}
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for id, ws := range all {
		r.Host.Unbind(id)
		_ = r.Launcher.Stop(ctx, ws)
	}
}

// relayed reports whether the platform reaches this runner through the
// tunnel it dials (no public URL configured) rather than directly.
func (r *Runner) relayed() bool { return r.State.PublicURL == "" }

// listen serves the host server for direct mode.
func (r *Runner) listen(ctx context.Context) error {
	ln, err := net.Listen("tcp", r.State.ListenAddr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", r.State.ListenAddr, err)
	}
	srv := &http.Server{Handler: r.Host.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { //nolint:gosec // G118: shutdown starts after ctx is done, so it needs its own deadline
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.Log.Error("host server stopped", "err", err)
		}
	}()
	return nil
}
