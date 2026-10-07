package preview

import (
	"time"
)

// DefaultReadyTimeout bounds how long Start waits for the dev port to
// answer after spawning the dev command: it mirrors verify's dev-server
// timeout (a cold dev-server start without stalling a turn).
const DefaultReadyTimeout = 30 * time.Second

// Status is one of the preview pane's four lifecycle states:
// the words the pane's status prop uses, so the API and the
// pane speak the same language.
type Status string

// The four lifecycle states a dev server can be in.
const (
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopped  Status = "stopped"
	StatusFailed   Status = "failed"
)

// State is the lifecycle snapshot the preview pane renders: the state, the
// embed URL while running, the reason while stopped/failed, and whether a
// running server was detected on the port rather than started by the
// manager.
type State struct {
	Status   Status `json:"status"`
	URL      string `json:"url,omitempty"`
	Error    string `json:"error,omitempty"`
	Detected bool   `json:"detected,omitempty"`
	// Hosted marks that the URL is a platform-registered preview (the agent
	// called register_preview_port in a hosted workspace) rather than a
	// local dev server managed by this package. The pane embeds the hosted
	// URL directly; local start/restart/stop actions never apply to it.
	Hosted bool `json:"hosted,omitempty"`
}

// Option customizes a Manager.
type Option func(*Manager)

// WithReadyTimeout overrides how long Start waits for the dev port to
// answer after spawning the dev command (DefaultReadyTimeout otherwise).
func WithReadyTimeout(d time.Duration) Option {
	return func(m *Manager) {
		if d > 0 {
			m.readyTimeout = d
		}
	}
}

// New returns the preview manager for a project root: the manager that
// starts or detects that project's dev server.
func New(root string, opts ...Option) *Manager {
	m := &Manager{
		root:         root,
		readyTimeout: DefaultReadyTimeout,
		status:       StatusStopped,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}
