package utils

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type capturingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *capturingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *capturingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(_ string) slog.Handler       { return h }

func TestSafeGo_ExecutesFn(t *testing.T) {
	h := &capturingHandler{}
	logger := slog.New(h)
	done := make(chan struct{})
	SafeGo(logger, "test", func() {
		close(done)
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SafeGo did not execute fn within 2s")
	}
}

func TestSafeGo_RecoversFromPanicAndLogs(t *testing.T) {
	h := &capturingHandler{}
	logger := slog.New(h)
	ran := make(chan struct{})
	SafeGo(logger, "panic-test", func() {
		panic("boom")
	})
	SafeGo(logger, "verify", func() {
		close(ran)
	})
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("SafeGo did not recover from panic — verify goroutine never ran")
	}
	// Give the panic recovery goroutine time to finish logging
	time.Sleep(50 * time.Millisecond)

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.records) == 0 {
		t.Fatal("expected at least one log record from panic recovery")
	}
	rec := h.records[0]
	if rec.Level != slog.LevelError {
		t.Errorf("expected Error level, got %v", rec.Level)
	}
	var name, panicVal, stack string
	rec.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "name":
			name = a.Value.String()
		case "panic":
			panicVal = a.Value.String()
		case "stack":
			stack = a.Value.String()
		}
		return true
	})
	if name != "panic-test" {
		t.Errorf("expected name=panic-test, got %q", name)
	}
	if panicVal != "boom" {
		t.Errorf("expected panic=boom, got %q", panicVal)
	}
	if stack == "" {
		t.Error("expected non-empty stack trace")
	}
}

func TestSafeGo_NilLoggerFallback(t *testing.T) {
	done := make(chan struct{})
	SafeGo(nil, "nil-logger-test", func() {
		close(done)
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SafeGo with nil logger did not execute fn")
	}
}

func TestSafeGo_ExtraAttrs(t *testing.T) {
	h := &capturingHandler{}
	logger := slog.New(h)
	done := make(chan struct{})
	SafeGo(logger, "attrs-test", func() {
		panic("attr-boom")
	}, slog.String("chat_id", "abc123"))
	SafeGo(logger, "verify", func() {
		close(done)
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("verify goroutine never ran")
	}
	// Give the panic recovery goroutine time to finish logging
	time.Sleep(50 * time.Millisecond)

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.records) == 0 {
		t.Fatal("expected at least one log record")
	}
	var chatID string
	h.records[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "chat_id" {
			chatID = a.Value.String()
		}
		return true
	})
	if chatID != "abc123" {
		t.Errorf("expected chat_id=abc123, got %q", chatID)
	}
}
