package computer_use

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditRecord is one line in a session's computer-use audit log. Screenshots
// are recorded by size only (not contents) to keep the log small and avoid
// persisting potentially sensitive screen captures.
type AuditRecord struct {
	Time   string         `json:"time"` // RFC3339, supplied by the caller's clock
	Action string         `json:"action"`
	Args   map[string]any `json:"args,omitempty"`
	Err    string         `json:"error,omitempty"`
}

// auditingBackend wraps a ComputerBackend and appends every action to a
// per-session JSONL log under the configured directory. It is the outermost
// decorator so it captures the action the user actually authorized.
type auditingBackend struct {
	inner  ComputerBackend
	mu     sync.Mutex
	w      *os.File
	now    func() time.Time
	closed bool
}

// NewAuditingBackend wraps inner, writing audit records to
// <dir>/<sessionID>.jsonl. The directory is created if missing. If the log
// cannot be opened, the error is returned and the caller should fall back to
// the unwrapped backend rather than failing the whole feature.
func NewAuditingBackend(inner ComputerBackend, dir, sessionID string) (*auditingBackend, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create audit dir: %w", err)
	}
	path := filepath.Join(dir, sanitizeSession(sessionID)+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	return &auditingBackend{inner: inner, w: f, now: time.Now}, nil
}

// Close releases the underlying log file. Subsequent records are no-ops.
func (a *auditingBackend) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil
	}
	a.closed = true
	if a.w == nil {
		return nil
	}
	err := a.w.Close()
	a.w = nil
	return err
}

func (a *auditingBackend) record(action string, args map[string]any, opErr error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.w == nil {
		return
	}
	rec := AuditRecord{Time: a.now().UTC().Format(time.RFC3339), Action: action, Args: args}
	if opErr != nil {
		rec.Err = opErr.Error()
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_, _ = a.w.Write(append(line, '\n'))
}

func (a *auditingBackend) Screenshot(region *Rect) ([]byte, Size, error) {
	img, dims, err := a.inner.Screenshot(region)
	a.record("screenshot", map[string]any{"region": region, "bytes": len(img)}, err)
	return img, dims, err
}

func (a *auditingBackend) MouseClick(x, y int, button MouseButton, double bool) error {
	err := a.inner.MouseClick(x, y, button, double)
	a.record("mouse_click", map[string]any{"x": x, "y": y, "button": string(button), "double": double}, err)
	return err
}

func (a *auditingBackend) MouseDrag(from, to Point, button MouseButton) error {
	err := a.inner.MouseDrag(from, to, button)
	a.record("mouse_drag", map[string]any{"from": from, "to": to, "button": string(button)}, err)
	return err
}

func (a *auditingBackend) KeyboardType(text string) error {
	err := a.inner.KeyboardType(text)
	// Record the character count, not the text, to avoid logging secrets typed
	// into password fields.
	a.record("keyboard_type", map[string]any{"len": len(text)}, err)
	return err
}

func (a *auditingBackend) KeyboardPress(key string) error {
	err := a.inner.KeyboardPress(key)
	a.record("keyboard_press", map[string]any{"key": key}, err)
	return err
}

func (a *auditingBackend) Scroll(dir ScrollDir, amount int, at *Point) error {
	err := a.inner.Scroll(dir, amount, at)
	a.record("scroll", map[string]any{"dir": string(dir), "amount": amount, "at": at}, err)
	return err
}

func sanitizeSession(s string) string {
	if s == "" {
		return "session"
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}
