package computer_use

import (
	"fmt"
	"sync"
	"time"
)

// ErrRateLimited is returned when the action-rate cap is exceeded. It exists so
// callers / tests can distinguish a safety stop from a backend failure.
var ErrRateLimited = fmt.Errorf("computer-use action rate limit exceeded")

// rateLimitedBackend wraps a ComputerBackend with a sliding-window action cap
// (default 60/min, matching SP-063 §4.7). It is a runaway-loop backstop: a
// model stuck in a click loop can't drive the OS faster than the cap before the
// user notices.
type rateLimitedBackend struct {
	inner     ComputerBackend
	maxPerMin int
	now       func() time.Time

	mu     sync.Mutex
	window []time.Time
}

// NewRateLimitedBackend wraps inner with a cap of maxPerMin actions per rolling
// 60s window. A maxPerMin <= 0 disables the cap.
func NewRateLimitedBackend(inner ComputerBackend, maxPerMin int) *rateLimitedBackend {
	return &rateLimitedBackend{inner: inner, maxPerMin: maxPerMin, now: time.Now}
}

// allow records an action and reports whether it is within the cap.
func (r *rateLimitedBackend) allow() bool {
	if r.maxPerMin <= 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	cutoff := now.Add(-time.Minute)
	// Drop timestamps older than the window.
	kept := r.window[:0]
	for _, t := range r.window {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	r.window = kept
	if len(r.window) >= r.maxPerMin {
		return false
	}
	r.window = append(r.window, now)
	return true
}

func (r *rateLimitedBackend) Screenshot(region *Rect) ([]byte, Size, error) {
	if !r.allow() {
		return nil, Size{}, ErrRateLimited
	}
	return r.inner.Screenshot(region)
}

func (r *rateLimitedBackend) MouseClick(x, y int, button MouseButton, double bool) error {
	if !r.allow() {
		return ErrRateLimited
	}
	return r.inner.MouseClick(x, y, button, double)
}

func (r *rateLimitedBackend) MouseDrag(from, to Point, button MouseButton) error {
	if !r.allow() {
		return ErrRateLimited
	}
	return r.inner.MouseDrag(from, to, button)
}

func (r *rateLimitedBackend) MoveTo(x, y int) error {
	if !r.allow() {
		return ErrRateLimited
	}
	return r.inner.MoveTo(x, y)
}

func (r *rateLimitedBackend) KeyboardType(text string) error {
	if !r.allow() {
		return ErrRateLimited
	}
	return r.inner.KeyboardType(text)
}

func (r *rateLimitedBackend) KeyboardPress(key string) error {
	if !r.allow() {
		return ErrRateLimited
	}
	return r.inner.KeyboardPress(key)
}

func (r *rateLimitedBackend) Scroll(dir ScrollDir, amount int, at *Point) error {
	if !r.allow() {
		return ErrRateLimited
	}
	return r.inner.Scroll(dir, amount, at)
}
