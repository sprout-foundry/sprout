package agent_audit

import (
	"context"
	"sync/atomic"
	"time"
)

// sinkPtr holds the process-wide audit sink. It is an atomic pointer so the
// provider can emit from any goroutine while the agent installs or clears the
// sink at startup/shutdown, with no lock on the hot path.
var sinkPtr atomic.Pointer[Sink]

// SetSink installs the process-wide audit sink. Pass nil to remove it. The
// last installer wins, mirroring the process-global audit log the sink writes
// to; an agent that owns the sink clears it on shutdown.
func SetSink(s Sink) {
	if s == nil {
		sinkPtr.Store(nil)
		return
	}
	sinkPtr.Store(&s)
}

// CurrentSink returns the installed sink, or nil when none is set.
func CurrentSink() Sink {
	p := sinkPtr.Load()
	if p == nil {
		return nil
	}
	return *p
}

// EmitCall records a model-call event. It fills the event's context-derived
// fields (chat id, session id), its trigger, and its timestamp, then forwards
// to the installed sink. Nil-safe: a no-op when no sink is installed.
func EmitCall(ctx context.Context, ev CallEvent) {
	s := CurrentSink()
	if s == nil {
		return
	}
	enrichCall(ctx, &ev)
	s.EmitCall(ev)
}

// EmitTool records a tool-execution event. It fills the context-derived
// fields and timestamp, then forwards to the installed sink. Nil-safe.
func EmitTool(ctx context.Context, ev ToolEvent) {
	s := CurrentSink()
	if s == nil {
		return
	}
	if cc, ok := CallContextFrom(ctx); ok {
		if ev.ChatID == "" {
			ev.ChatID = cc.ChatID
		}
		if ev.SessionID == "" {
			ev.SessionID = cc.SessionID
		}
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	if ev.Kind == "" {
		ev.Kind = KindToolCall
	}
	s.EmitTool(ev)
}

func enrichCall(ctx context.Context, ev *CallEvent) {
	if cc, ok := CallContextFrom(ctx); ok {
		if ev.ChatID == "" {
			ev.ChatID = cc.ChatID
		}
		if ev.SessionID == "" {
			ev.SessionID = cc.SessionID
		}
	}
	if ev.Trigger == "" {
		ev.Trigger = TriggerUserTurn
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	if ev.Kind == "" {
		ev.Kind = KindModelCall
	}
}
