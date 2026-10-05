//go:build !js

package cliui

// progress_summary_test.go — tests for the deterministic SP-151
// progress-event template summaries (item 151.6) and the CLI render path
// (HandleProgressEvent). The template tests are pure exact-string
// assertions; the handler tests capture the fmt.Print fallback of
// console.PrintExternal (no reader active in a test process), so no
// real TTY is required.

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestProgressMilestoneSummary(t *testing.T) {
	cases := []struct {
		name string
		data map[string]interface{}
		want string
	}{
		{
			name: "finished with title and files",
			data: map[string]interface{}{
				"run_id":        "run-1",
				"phase":         "finished",
				"scope_title":   "sign-up form",
				"files_touched": 4,
			},
			want: "Finished: sign-up form (4 files)",
		},
		{
			name: "finished with title, no files key",
			data: map[string]interface{}{
				"phase":       "finished",
				"scope_title": "sign-up form",
			},
			want: "Finished: sign-up form",
		},
		{
			name: "finished with zero files omits the count",
			data: map[string]interface{}{
				"phase":         "finished",
				"scope_title":   "sign-up form",
				"files_touched": 0,
			},
			want: "Finished: sign-up form",
		},
		{
			name: "started",
			data: map[string]interface{}{
				"phase":       "started",
				"scope_title": "billing",
			},
			want: "Started: billing",
		},
		{
			name: "scope id falls back when the title is absent",
			data: map[string]interface{}{
				"phase":    "started",
				"scope_id": "item-3",
			},
			want: "Started: item-3",
		},
		{
			name: "finished with no scope falls back to the run",
			data: map[string]interface{}{
				"phase": "finished",
			},
			want: "Finished: run",
		},
		{
			name: "finished with no scope but files",
			data: map[string]interface{}{
				"phase":         "finished",
				"files_touched": 2,
			},
			want: "Finished: run (2 files)",
		},
		{
			name: "coalesced batch renders the count only",
			data: map[string]interface{}{
				"run_id": "run-1",
				"milestones": []interface{}{
					map[string]interface{}{"scope_id": "a", "phase": "finished"},
					map[string]interface{}{"scope_id": "b", "phase": "finished"},
					map[string]interface{}{"scope_id": "c", "phase": "started"},
				},
			},
			want: "Milestones: 3",
		},
		{
			name: "unknown phase with no title says nothing",
			data: map[string]interface{}{
				"phase": "paused",
			},
			want: "",
		},
		{
			name: "missing phase with a title still renders",
			data: map[string]interface{}{
				"scope_title": "billing",
			},
			want: "Started: billing",
		},
		{
			name: "files_touched round-tripped as a JSON float",
			data: map[string]interface{}{
				"phase":         "finished",
				"scope_title":   "sign-up form",
				"files_touched": float64(4),
			},
			want: "Finished: sign-up form (4 files)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ProgressMilestoneSummary(c.data); got != c.want {
				t.Errorf("ProgressMilestoneSummary(%v) = %q; want %q", c.data, got, c.want)
			}
		})
	}
}

func TestProgressVerificationSummary(t *testing.T) {
	cases := []struct {
		name string
		data map[string]interface{}
		want string
	}{
		{
			name: "all passed",
			data: map[string]interface{}{
				"checks": []interface{}{
					map[string]interface{}{"kind": "build", "passed": true},
					map[string]interface{}{"kind": "test", "passed": true},
				},
			},
			want: "Checks: 2/2 passed",
		},
		{
			name: "one failed",
			data: map[string]interface{}{
				"checks": []interface{}{
					map[string]interface{}{"kind": "build", "passed": true},
					map[string]interface{}{"kind": "test", "passed": false, "excerpt": "FAIL"},
				},
			},
			want: "Checks: 1/2 passed",
		},
		{
			name: "skipped checks are not counted as passed",
			data: map[string]interface{}{
				"checks": []interface{}{
					map[string]interface{}{"kind": "build", "passed": true},
					map[string]interface{}{"kind": "lint", "skipped": true, "reason": "no trusted command"},
				},
			},
			want: "Checks: 1/2 passed",
		},
		{
			name: "all skipped",
			data: map[string]interface{}{
				"checks": []interface{}{
					map[string]interface{}{"kind": "lint", "skipped": true},
					map[string]interface{}{"kind": "smoke", "skipped": true},
				},
			},
			want: "Checks: 0/2 passed",
		},
		{
			name: "nothing ran says nothing",
			data: map[string]interface{}{
				"checks": []interface{}{},
			},
			want: "",
		},
		{
			name: "missing checks key says nothing",
			data: map[string]interface{}{
				"run_id": "run-1",
			},
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ProgressVerificationSummary(c.data); got != c.want {
				t.Errorf("ProgressVerificationSummary(%v) = %q; want %q", c.data, got, c.want)
			}
		})
	}
}

func TestProgressCompleteSummary(t *testing.T) {
	cases := []struct {
		name string
		data map[string]interface{}
		want string
	}{
		{
			name: "verified with the final checks",
			data: map[string]interface{}{
				"verified": true,
				"verification": map[string]interface{}{
					"passed": true,
					"checks": []interface{}{
						map[string]interface{}{"kind": "build", "passed": true},
						map[string]interface{}{"kind": "test", "passed": true},
						map[string]interface{}{"kind": "smoke", "passed": true},
					},
				},
			},
			want: "Run complete — verified (Checks: 3/3 passed)",
		},
		{
			name: "verified without a nested verification",
			data: map[string]interface{}{
				"verified": true,
			},
			want: "Run complete — verified",
		},
		{
			name: "verified with an empty nested checks list",
			data: map[string]interface{}{
				"verified": true,
				"verification": map[string]interface{}{
					"checks": []interface{}{},
				},
			},
			want: "Run complete — verified",
		},
		{
			name: "not verified with a reason",
			data: map[string]interface{}{
				"not_verified_reason": "verification disabled",
			},
			want: "Run complete — not verified (verification disabled)",
		},
		{
			name: "not verified without a reason says nothing",
			data: map[string]interface{}{
				"run_id": "run-1",
			},
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ProgressCompleteSummary(c.data); got != c.want {
				t.Errorf("ProgressCompleteSummary(%v) = %q; want %q", c.data, got, c.want)
			}
		})
	}
}

func TestProgressQuestionSummary(t *testing.T) {
	if got := ProgressQuestionSummary(map[string]interface{}{
		"question": "Which DB?",
		"options":  []interface{}{"postgres", "sqlite"},
	}); got != "Needs a decision: Which DB?" {
		t.Errorf("ProgressQuestionSummary() = %q; want %q", got, "Needs a decision: Which DB?")
	}
	if got := ProgressQuestionSummary(map[string]interface{}{}); got != "" {
		t.Errorf("empty question: got %q; want \"\"", got)
	}
}

// TestProgressEventSummary_DispatchesByEventType verifies the dispatch:
// each of the four progress event types reaches its template, anything
// else renders nothing, and a progress event whose template is empty
// renders nothing.
func TestProgressEventSummary_DispatchesByEventType(t *testing.T) {
	milestone := map[string]interface{}{
		"phase":         "finished",
		"scope_title":   "sign-up form",
		"files_touched": 4,
	}
	verification := map[string]interface{}{
		"checks": []interface{}{
			map[string]interface{}{"passed": true},
			map[string]interface{}{"passed": true},
		},
	}
	complete := map[string]interface{}{"verified": true}
	question := map[string]interface{}{"question": "Which DB?"}

	cases := []struct {
		name string
		typ  string
		data map[string]interface{}
		want string
	}{
		{"milestone", events.EventTypeProgressMilestone, milestone, "Finished: sign-up form (4 files)"},
		{"verification", events.EventTypeProgressVerification, verification, "Checks: 2/2 passed"},
		{"complete", events.EventTypeProgressComplete, complete, "Run complete — verified"},
		{"question", events.EventTypeProgressQuestion, question, "Needs a decision: Which DB?"},
		{"unknown type", events.EventTypeAgentMessage, milestone, ""},
		{"non-progress type", "totally_unknown", question, ""},
		{"empty template", events.EventTypeProgressVerification, map[string]interface{}{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ProgressEventSummary(c.typ, c.data); got != c.want {
				t.Errorf("ProgressEventSummary(%q) = %q; want %q", c.typ, got, c.want)
			}
		})
	}
}

// captureStdout swaps os.Stdout for a pipe for the duration of the test
// and returns whatever was written. console.PrintExternal with no input
// / steer reader active falls through to fmt.Print, which is what a
// test process (no REPL) exercises.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()
	fn()
	w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("drain stdout pipe: %v", err)
	}
	r.Close()
	return buf.String()
}

// TestHandleProgressEvent_Render covers the CLI render path (item
// 151.6): a milestone event prints its one-line summary through
// console.PrintExternal (captured via the fmt.Print fallback — no real
// TTY required) and invalidates the collapse run + thinking state.
func TestHandleProgressEvent_Render(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	state := NewTerminalSubscriberState(nil, nil)
	indicator := console.NewActivityIndicator(&bytes.Buffer{})
	footer := console.NewStatusFooter(&bytes.Buffer{}, nil)

	out := captureStdout(t, func() {
		state.HandleProgressEvent(events.EventTypeProgressMilestone, map[string]interface{}{
			"phase":         "finished",
			"scope_title":   "sign-up form",
			"files_touched": 4,
		}, indicator, footer)
	})
	if !strings.Contains(out, "Finished: sign-up form (4 files)") {
		t.Errorf("stdout missing the milestone summary; got %q", out)
	}

	// A completed-and-verified run renders too, and the notice must
	// break the pending collapse run + clear the thinking flag.
	state.thinkingActive = true
	state.run = &ToolRunState{Name: "shell_command", Count: 2}
	out = captureStdout(t, func() {
		state.HandleProgressEvent(events.EventTypeProgressComplete, map[string]interface{}{
			"verified": true,
			"verification": map[string]interface{}{
				"checks": []interface{}{
					map[string]interface{}{"passed": true},
					map[string]interface{}{"passed": true},
				},
			},
		}, indicator, footer)
	})
	if !strings.Contains(out, "Run complete — verified (Checks: 2/2 passed)") {
		t.Errorf("stdout missing the complete summary; got %q", out)
	}
	if state.run != nil {
		t.Error("rendered progress notice must break the collapse run (s.run = nil)")
	}
	if state.thinkingActive {
		t.Error("rendered progress notice must clear the thinking flag")
	}
}

// TestHandleProgressEvent_QuestionNotRendered pins the deliberate
// no-render for progress_question: the interactive ask_user prompt
// already shows the decision, so the handler must print nothing and
// must not touch the terminal state (spinner / thinking flag /
// collapse run). The primed state below is observable proof of the
// early return: the render path sets thinkingActive = false and
// s.run = nil, so if either changed the no-op contract broke.
func TestHandleProgressEvent_QuestionNotRendered(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	state := NewTerminalSubscriberState(nil, nil)
	indicator := console.NewActivityIndicator(&bytes.Buffer{})
	footer := console.NewStatusFooter(&bytes.Buffer{}, nil)

	state.thinkingActive = true
	state.run = &ToolRunState{Name: "shell_command", Count: 2}

	out := captureStdout(t, func() {
		state.HandleProgressEvent(events.EventTypeProgressQuestion, map[string]interface{}{
			"question": "Which DB?",
		}, indicator, footer)
	})
	if strings.Contains(out, "Needs a decision") {
		t.Errorf("progress_question must not render in the terminal; got %q", out)
	}
	if !state.thinkingActive {
		t.Error("question event cleared thinkingActive; the no-op path must not touch spinner state")
	}
	if state.run == nil {
		t.Error("question event broke the collapse run; the no-op path must not touch s.run")
	}
}

// TestHandleProgressEvent_EmptySummaryNotRendered verifies that
// progress events whose template has nothing to say print nothing.
func TestHandleProgressEvent_EmptySummaryNotRendered(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	state := NewTerminalSubscriberState(nil, nil)
	indicator := console.NewActivityIndicator(&bytes.Buffer{})
	footer := console.NewStatusFooter(&bytes.Buffer{}, nil)

	out := captureStdout(t, func() {
		state.HandleProgressEvent(events.EventTypeProgressVerification, map[string]interface{}{}, indicator, footer)
		state.HandleProgressEvent(events.EventTypeProgressMilestone, map[string]interface{}{"phase": "weird"}, indicator, footer)
		state.HandleProgressEvent(events.EventTypeProgressQuestion, map[string]interface{}{}, indicator, footer)
	})
	if out != "" {
		t.Errorf("empty templates must print nothing; got %q", out)
	}
}
