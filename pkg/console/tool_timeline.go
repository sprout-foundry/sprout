// Package console (continued) — tool execution timeline subscriber.
package console

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// activeTool tracks a single in-flight tool call for timeline rendering.
type activeTool struct {
	displayName string
	startedAt   time.Time
}

// ToolTimeline subscribes to the event bus and prints a live, glyph-prefixed
// timeline of tool executions to the console. Each tool start emits an
// action arrow; each tool end emits a success/error glyph with elapsed time.
//
// Format:
//
//	→ read_file /foo/bar.go · Started
//	✓ read_file /foo/bar.go · 0.32s
//	✗ shell_cmd "rm -rf /" · 1.20s: Permission denied
//
// The zero value is unusable — construct via NewToolTimeline.
type ToolTimeline struct {
	bus     *events.EventBus
	w       io.Writer
	mu      sync.Mutex
	active  map[string]*activeTool // keyed by toolCallID
	done    chan struct{}
	flushed chan struct{} // signaled after each event is fully processed
}

// NewToolTimeline creates a ToolTimeline that writes to w and subscribes to
// bus. If w is nil, os.Stderr is used. Call Stop() when the timeline is no
// longer needed (e.g., at session teardown).
func NewToolTimeline(bus *events.EventBus, w io.Writer) *ToolTimeline {
	if w == nil {
		w = os.Stderr
	}
	tl := &ToolTimeline{
		bus:     bus,
		w:       w,
		active:  make(map[string]*activeTool),
		done:    make(chan struct{}),
		flushed: make(chan struct{}),
	}

	ch := bus.Subscribe("tool_timeline")
	go tl.run(ch)
	return tl
}

// Flush returns a channel that is closed after the next event is fully
// processed (written to the output). Call Flush() before publishing an event,
// then block on the returned channel to wait for processing to complete.
// Safe to call concurrently.
func (tl *ToolTimeline) Flush() <-chan struct{} {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.flushed = make(chan struct{})
	return tl.flushed
}

// flush closes the most recently armed Flush channel to signal that the
// current event has been fully processed. The channel is consumed once;
// subsequent calls are no-ops until Flush() arms a new channel.
func (tl *ToolTimeline) flush() {
	tl.mu.Lock()
	ch := tl.flushed
	tl.flushed = nil
	tl.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

// Stop unsubscribes from the event bus and waits for the event loop to exit.
// Safe to call multiple times; subsequent calls are no-ops.
func (tl *ToolTimeline) Stop() {
	select {
	case <-tl.done:
		// Already stopped.
		return
	default:
	}
	tl.bus.Unsubscribe("tool_timeline")
	// Unsubscribe closes the channel, which causes run() to return and close
	// tl.done.
	<-tl.done
}

func (tl *ToolTimeline) run(ch <-chan events.UIEvent) {
	defer close(tl.done)
	for ev := range ch {
		switch ev.Type {
		case events.EventTypeToolStart:
			tl.handleToolStart(ev)
		case events.EventTypeToolEnd:
			tl.handleToolEnd(ev)
		}
	}
}

func (tl *ToolTimeline) handleToolStart(ev events.UIEvent) {
	defer tl.flush()

	data, ok := ev.Data.(map[string]interface{})
	if !ok {
		return
	}

	toolCallID, _ := data["tool_call_id"].(string)
	if toolCallID == "" {
		return
	}

	displayName, _ := data["display_name"].(string)
	if displayName == "" {
		displayName, _ = data["tool_name"].(string)
	}
	if displayName == "" {
		return
	}

	tl.mu.Lock()
	tl.active[toolCallID] = &activeTool{
		displayName: displayName,
		startedAt:   ev.Timestamp,
	}
	tl.mu.Unlock()

	// Print the start line. Skip if we can't acquire the lock — a missed
	// start line is harmless (the end line still shows duration).
	if !TryLockOutput() {
		return
	}
	GlyphAction.Fprintln(tl.w, displayName+" · Started")
	UnlockOutput()
}

func (tl *ToolTimeline) handleToolEnd(ev events.UIEvent) {
	defer tl.flush()

	data, ok := ev.Data.(map[string]interface{})
	if !ok {
		return
	}

	toolCallID, _ := data["tool_call_id"].(string)
	if toolCallID == "" {
		return
	}

	status, _ := data["status"].(string)
	var duration time.Duration
	if d, ok := data["duration_ms"].(int64); ok {
		duration = time.Duration(d) * time.Millisecond
	} else if d, ok := data["duration_ms"].(float64); ok {
		duration = time.Duration(d * float64(time.Millisecond))
	}
	errorMsg, _ := data["error"].(string)

	// Look up the display name from the active map; fall back to tool_name.
	tl.mu.Lock()
	at, found := tl.active[toolCallID]
	if found {
		delete(tl.active, toolCallID)
	}
	tl.mu.Unlock()

	var displayName string
	if found {
		displayName = at.displayName
	} else {
		displayName, _ = data["tool_name"].(string)
	}
	if displayName == "" {
		return
	}

	// Fallback: if duration_ms was missing from the payload but we have a
	// recorded start time, compute duration from the active tool's startedAt.
	if duration == 0 && found {
		duration = ev.Timestamp.Sub(at.startedAt)
	}

	// Format duration as X.XXs.
	durationStr := fmt.Sprintf("%.2fs", duration.Seconds())

	// Truncate error message to fit a single timeline line while
	// preserving both the error type (first line) and the actionable
	// location (tail). For most errors the tail carries the file:line
	// or response status that tells the user what actually went wrong;
	// the head-only truncation dropped that.
	//
	// Format:
	//   - ≤80 runes: emit as-is
	//   - >80 runes: emit first line (≤40 runes), ellipsis, then last
	//     37 runes. Keeps "panic: index out of range" plus the
	//     "foo.go:42" tail that makes the error actionable.
	errorMsg = truncateErrorForTimeline(errorMsg, 80)

	// Pick the glyph and format based on status.
	if !TryLockOutput() {
		return
	}
	defer UnlockOutput()

	switch status {
	case "completed":
		GlyphSuccess.Fprintln(tl.w, displayName+" · "+durationStr)
	case "failed":
		if errorMsg != "" {
			GlyphError.Fprintln(tl.w, displayName+" · "+durationStr+": "+errorMsg)
		} else {
			GlyphError.Fprintln(tl.w, displayName+" · "+durationStr)
		}
	default:
		// Unknown status (e.g., "cancelled", "timed_out") — still show the
		// line so the timeline isn't silent, but use a dim glyph to
		// visually distinguish it from completed/failed results.
		GlyphDim.Fprintln(tl.w, displayName+" · "+durationStr)
	}
}

// truncateErrorForTimeline collapses error text to ≤max runes while
// preserving the most diagnostic bits. The previous head-only truncation
// kept the error preamble ("panic: runtime error: index out of range")
// but dropped the tail where the file/line lives — exactly the part
// the user needs to act on.
//
// Strategy when the message exceeds the budget:
//
//   1. Split on \n. The first line usually carries the error type
//      ("panic:", "Error:", "Traceback (most recent call last):"…).
//   2. Take the first line, capped at 40 runes.
//   3. Append " … " separator.
//   4. Append the tail: the last (max - firstLen - 3) runes of the
//      full message, where the tail is itself truncated only if needed.
//
// Rune-safe (not byte-safe) so multi-byte UTF-8 isn't corrupted.
func truncateErrorForTimeline(msg string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(msg)
	if len(runes) <= max {
		return msg
	}

	const headCap = 40
	if max <= headCap+4 {
		// Budget too tight to fit head + " … " + tail. Fall back to
		// the legacy head-only truncation so the user at least sees
		// the error type.
		return string(runes[:max-1]) + "…"
	}

	// Head: first newline, capped at headCap runes.
	head := string(runes)
	if nlIdx := strings.IndexRune(head, '\n'); nlIdx >= 0 {
		headRunes := []rune(head[:nlIdx])
		if len(headRunes) > headCap {
			headRunes = headRunes[:headCap]
		}
		head = string(headRunes)
	} else {
		headRunes := []rune(head)
		if len(headRunes) > headCap {
			headRunes = headRunes[:headCap]
		}
		head = string(headRunes)
	}

	// Tail: last (max - len(head) - 3) runes, where the 3 is " … ".
	tailBudget := max - len([]rune(head)) - 3
	if tailBudget < 4 {
		tailBudget = 4
	}
	if tailBudget > len(runes) {
		tailBudget = len(runes)
	}
	tail := string(runes[len(runes)-tailBudget:])

	return head + " … " + tail
}
