package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent/subagents"
)

// Git-write personas (the orchestrator family) get the incremental-commit
// git bullet in their preamble; everyone else keeps the read-only one.
func TestCreateSubagent_PreambleFollowsGitWriteCapability(t *testing.T) {
	_, runner := newReviewTestRunner(t)

	orch, err := runner.createSubagent(SubagentOptions{Persona: "orchestrator"}, context.Background())
	if err != nil {
		t.Fatalf("createSubagent(orchestrator): %v", err)
	}
	defer orch.Shutdown()
	if !strings.Contains(orch.systemPrompt, "Commit incrementally") {
		t.Errorf("orchestrator preamble missing incremental-commit rule:\n%.400s", orch.systemPrompt)
	}
	if strings.Contains(orch.systemPrompt, "Do NOT commit or push") {
		t.Errorf("orchestrator preamble carries the read-only commit ban:\n%.400s", orch.systemPrompt)
	}

	coder, err := runner.createSubagent(SubagentOptions{Persona: "coder"}, context.Background())
	if err != nil {
		t.Fatalf("createSubagent(coder): %v", err)
	}
	defer coder.Shutdown()
	if strings.Contains(coder.systemPrompt, "Commit incrementally") {
		t.Errorf("coder preamble must not carry the commit rule:\n%.400s", coder.systemPrompt)
	}
	if !strings.Contains(coder.systemPrompt, "Do NOT commit or push") {
		t.Errorf("coder preamble missing the read-only git rule:\n%.400s", coder.systemPrompt)
	}
}

// A timed-out run must surface its recent progress as the result output so
// the primary can see what the subagent was doing and recover the work
// instead of starting blind.
func TestSubagentTimeoutTail(t *testing.T) {
	log := []SubagentProgressEntry{
		{Phase: "spawn", Message: "task started"},
		{Phase: "output", Message: "reading pkg/foo"},
		{Phase: "tool_call", Message: "go build ./..."},
	}
	var mu sync.Mutex
	out := subagentTimeoutTail(log, &mu)
	if !strings.Contains(out, subagents.TimeoutTailHeader) {
		t.Errorf("tail missing header:\n%s", out)
	}
	for _, want := range []string{"task started", "reading pkg/foo", "go build ./..."} {
		if !strings.Contains(out, want) {
			t.Errorf("tail missing entry %q:\n%s", want, out)
		}
	}

	// Bounded to the last entries.
	long := make([]SubagentProgressEntry, 40)
	for i := range long {
		long[i] = SubagentProgressEntry{Phase: "output", Message: strings.Repeat("x", i+1)}
	}
	trimmed := subagentTimeoutTail(long, &mu)
	if strings.Count(trimmed, "\n- ") > 8 {
		t.Errorf("tail not bounded to 8 entries:\n%s", trimmed)
	}

	empty := subagentTimeoutTail(nil, &mu)
	if !strings.Contains(empty, "Recent activity") {
		t.Errorf("empty log still needs the recovery guidance:\n%s", empty)
	}
}
