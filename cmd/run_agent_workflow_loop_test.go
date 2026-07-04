//go:build !js

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// newTestLoopAgent builds an Agent wired to a ScriptedClient, isolated in a
// temp config directory so tests never touch the real config.
func newTestLoopAgent(t *testing.T, client *agent.ScriptedClient) *agent.Agent {
	t.Helper()

	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	ag, err := agent.NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	t.Cleanup(ag.Shutdown)
	return ag
}

// writeTempTodoFile creates a TODO.md with the given items, returning its path.
func writeTempTodoFile(t *testing.T, dir string, items []string) string {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("## Test Items\n")
	for _, item := range items {
		sb.WriteString("- [ ] " + item + "\n")
	}
	path := filepath.Join(dir, "TODO.md")
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		t.Fatalf("write TODO.md: %v", err)
	}
	return path
}

// writeGatePromptFile writes the gate system prompt file and returns its path.
func writeGatePromptFile(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "gate_prompt.md")
	if err := os.WriteFile(path, []byte("Parse the TODO section into a delegation prompt."), 0644); err != nil {
		t.Fatalf("write gate_prompt.md: %v", err)
	}
	return path
}

// gateResponse builds a ScriptedResponse mimicking a successful gate call.
func gateResponse(title, prompt string, skip bool) *agent.ScriptedResponse {
	skipStr := "false"
	if skip {
		skipStr = "true"
	}
	return &agent.ScriptedResponse{
		Content: fmt.Sprintf(`{"title": %q, "prompt": %q, "skip": %s}`, title, prompt, skipStr),
	}
}

// triageResponse builds a ScriptedResponse mimicking a triage gate call.
func triageResponse(action, reason string) *agent.ScriptedResponse {
	return &agent.ScriptedResponse{
		Content: fmt.Sprintf(`{"action": %q, "reason": %q}`, action, reason),
	}
}

// =============================================================================
// TestRunAgentWorkflowLoop — integration tests for the TODO loop
// =============================================================================

func TestRunAgentWorkflowLoop(t *testing.T) {
	tests := []struct {
		name string
		// items in the TODO file.
		items []string
		// Scripted responses for gate / triage LLM calls, consumed in order.
		responses []*agent.ScriptedResponse
		// processResults defines the return values for each processQueryFn call.
		// Index matches call order. nil means success.
		processResults []error
		// buildCmd is the shell command for build verification.
		buildCmd string
		// expected outcomes
		wantComplete  bool
		wantError     bool
		wantChecked   int // number of items marked [x]
		wantProcessed int // itemsProcessed counter
		wantFailed    int // itemsFailed counter
		wantSkipped   int // itemsSkipped counter
		// contextTimeout, if > 0, sets a context deadline to prevent infinite
		// loops (e.g. when an incomplete item keeps being retried without
		// being marked done).
		contextTimeout string // Go duration string like "500ms"
	}{
		{
			name:     "full_success_path",
			items:    []string{"Implement feature X"},
			responses: []*agent.ScriptedResponse{
				gateResponse("Implement X", "Do the implementation", false),
			},
			processResults: []error{nil},
			buildCmd:       "true",
			wantComplete:   true,
			wantError:      false,
			wantChecked:    1,
			wantProcessed:  1,
		},
		{
			name:     "max_iterations_incomplete",
			items:    []string{"Refactor module Y"},
			responses: []*agent.ScriptedResponse{
				gateResponse("Refactor Y", "Refactor the module", false),
			},
			processResults: []error{fmt.Errorf("max iterations reached (50)")},
			buildCmd:       "true",
			wantComplete:   false,
			wantError:      true,
			wantChecked:    0,
			wantProcessed:  0,
			wantFailed:     1,
			// The item stays unchecked, causing the loop to re-find it forever.
			// We use a short timeout to halt.
			contextTimeout: "500ms",
		},
		{
			name:     "triage_skip",
			items:    []string{"Fix critical bug Z"},
			responses: []*agent.ScriptedResponse{
				gateResponse("Fix bug Z", "Fix the bug", false),
				triageResponse("skip", "blocking issue requires manual intervention"),
			},
			processResults: []error{nil},
			buildCmd:       "false",
			// After triage skip, the item is NOT marked [x] (correct behaviour —
			// the triage said it wasn't even attempted). The loop re-finds it
			// and runs out of scripted responses. The context timeout triggers
			// a "loop cancelled" error, which is the expected outcome.
			contextTimeout: "500ms",
			wantComplete:   false,
			wantError:      true,
			wantChecked:    0,
			wantProcessed:  0,
			wantFailed:     0,
			wantSkipped:    1,
		},
		{
			name:     "build_failure_then_retry_then_success",
			items:    []string{"Add new API endpoint W"},
			responses: []*agent.ScriptedResponse{
				gateResponse("Add API W", "Add the endpoint", false),
				triageResponse("retry", "transient build error, likely fixable"),
			},
			processResults: []error{nil, nil}, // first call + retry call
			// Counter-based: fails on first invocation (count=0), passes on
			// second (count=1). The retry processQueryFn writes the counter.
			buildCmd:      `count=$(cat /tmp/test_sprout_retry_count 2>/dev/null || echo 0); echo $((count+1)) > /tmp/test_sprout_retry_count; [ $count -gt 0 ]`,
			wantComplete:  true,
			wantError:     false,
			wantChecked:   1,
			wantProcessed: 1,
		},
		{
			name: "resume_from_checkpoint",
			// Item 1 at line 2, Item 2 at line 3, Item 3 at line 4.
			items: []string{"Item 1", "Item 2", "Item 3"},
			// Only 2 gate responses — for Item 2 and Item 3. If the off-by-one
			// bug is present (startAfter = 3 instead of 2), the loop skips
			// Item 2 at index 2 and jumps to Item 3, consuming the second
			// response for the wrong item, then runs out of scripted
			// responses with item 2 still unchecked — causing a panic or
			// infinite loop. If the fix is present, both items are processed
			// correctly and the loop completes with 2 checked.
			responses: []*agent.ScriptedResponse{
				gateResponse("Item 2", "Process item 2", false),
				gateResponse("Item 3", "Process item 3", false),
			},
			processResults: []error{nil, nil},
			buildCmd:       "true",
			wantComplete:   true,   // loop processes all items and completes
			wantError:      false,
			wantChecked:    3,     // Item 1 was pre-marked [x]; Items 2,3 get marked
			wantProcessed:  2,
		},
		{
			name: "checkpoint_file_persisted",
			// 2 items — process both through to completion, then verify
			// the orchestration state file was persisted during the run.
			items: []string{"Item 1", "Item 2"},
			responses: []*agent.ScriptedResponse{
				gateResponse("Item 1", "Process item 1", false),
				gateResponse("Item 2", "Process item 2", false),
			},
			processResults: []error{nil, nil},
			buildCmd:       "true",
			wantComplete:   true,
			wantError:      false,
			wantChecked:    2,
			wantProcessed:  2,
		},
		{
			name: "resume_from_checkpoint_file",
			// Item 1 at line 2, Item 2 at line 3, Item 3 at line 4.
			// The file is pre-written with Item 1 already [x] to simulate
			// a previous run that processed-and-committed it. The checkpoint
			// file (written in the test body) has CurrentTodoLineNum=3 to
			// resume from Item 2.
			items: []string{"Item 1", "Item 2", "Item 3"},
			responses: []*agent.ScriptedResponse{
				gateResponse("Item 2", "Process item 2", false),
				gateResponse("Item 3", "Process item 3", false),
			},
			processResults: []error{nil, nil},
			buildCmd:       "true",
			wantComplete:   true,
			wantError:      false,
			wantChecked:    3,  // Item 1 pre-marked [x]; Items 2,3 get marked
			wantProcessed:  2,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// Clean up stateful files used by some test scenarios.
			os.Remove("/tmp/test_sprout_retry_count")

					dir := t.TempDir()
		todoPath := writeTempTodoFile(t, dir, tt.items)
		// For the resume test, Item 1 was already processed before the
		// interruption — mark it [x] so the loop doesn't re-find it.
		if tt.name == "resume_from_checkpoint" || tt.name == "resume_from_checkpoint_file" {
			content := `## Test Items
- [x] Item 1
- [ ] Item 2
- [ ] Item 3
`
			if err := os.WriteFile(todoPath, []byte(content), 0644); err != nil {
				t.Fatalf("write resume TODO.md: %v", err)
			}
		}
		gatePromptPath := writeGatePromptFile(t, dir)
		client := agent.NewScriptedClient(tt.responses...)
		client.SetModel("test:test")

		chatAgent := newTestLoopAgent(t, client)
		eventBus := events.NewEventBus()

			// Override processQueryFn for this test.
			oldProcessFn := processQueryFn
			callCount := 0
			processQueryFn = func(ctx context.Context, _ *agent.Agent, _ *events.EventBus, query string) error {
				if callCount >= len(tt.processResults) {
					t.Fatalf("processQueryFn called %d times but only %d results configured", callCount+1, len(tt.processResults))
				}
				err := tt.processResults[callCount]
				callCount++

				// For the retry scenario: write the build counter so the
				// retry build check passes.
				if tt.name == "build_failure_then_retry_then_success" && callCount > 1 {
					// Second (retry) call: prime the counter so `[ $count -gt 0 ]` passes.
					os.WriteFile("/tmp/test_sprout_retry_count", []byte("1\n"), 0644)
				}

				_ = query
				return err
			}
			defer func() { processQueryFn = oldProcessFn }()

			cfg := &AgentWorkflowConfig{
				Loop: &AgentWorkflowLoopConfig{
					TodoFile:       todoPath,
					GatePromptFile: gatePromptPath,
					MaxRetries:     1,
					MaxIterations:  50,
					BuildCommand:   tt.buildCmd,
				},
			}

			// For tests that exercise the file-based checkpoint persistence,
			// enable orchestration with a temp state file.
			orchestrated := tt.name == "checkpoint_file_persisted" || tt.name == "resume_from_checkpoint_file"
			if orchestrated {
				stateFile := filepath.Join(dir, ".sprout", "workflow_state.json")
				eventsFile := filepath.Join(dir, ".sprout", "workflow_events.jsonl")
				cfg.Orchestration = &AgentWorkflowOrchestrationConfig{
					Enabled:               true,
					StateFile:             stateFile,
					EventsFile:            eventsFile,
					ConversationSessionID: "test-loop",
				}
			}

			var state *workflowExecutionState
			if tt.name == "resume_from_checkpoint_file" {
				// Simulate a previous interrupted run: write a checkpoint
				// state file with CurrentTodoLineNum=3 (Item 2, 1-based).
				// Item 1 at line 2 is already [x] in the TODO file.
				stateFile := cfg.Orchestration.StateFile
				checkpointData, _ := json.Marshal(workflowExecutionState{
					Version:            1,
					InitialCompleted:   true,
					NextStepIndex:      0,
					CurrentTodoLineNum: 3,
					Complete:           false,
				})
				if err := os.MkdirAll(filepath.Dir(stateFile), 0755); err != nil {
					t.Fatalf("mkdir state dir: %v", err)
				}
				if err := os.WriteFile(stateFile, checkpointData, 0600); err != nil {
					t.Fatalf("write checkpoint state: %v", err)
				}
				loaded, err := loadWorkflowExecutionState(cfg)
				if err != nil {
					t.Fatalf("loadWorkflowExecutionState: %v", err)
				}
				state = loaded
			} else {
				state = &workflowExecutionState{
					Version:       1,
					NextStepIndex: 0,
				}
			}

			// Resume from checkpoint: simulate a kill after processing the
			// first item. Item 1 is at line 2 (already [x]), Item 2 at line 3.
			if tt.name == "resume_from_checkpoint" {
				state.CurrentTodoLineNum = 3 // resume from Item 2
			}

			// Build context with optional timeout.
			ctx := context.Background()
			if tt.contextTimeout != "" {
				d, err := time.ParseDuration(tt.contextTimeout)
				if err != nil {
					t.Fatalf("invalid contextTimeout %q: %v", tt.contextTimeout, err)
				}
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, d)
				defer cancel()
			}

			yielded, err := runAgentWorkflowLoop(ctx, chatAgent, eventBus, cfg, state)

			if (err != nil) != tt.wantError {
				t.Errorf("runAgentWorkflowLoop error = %v, want error = %v", err, tt.wantError)
			}
			if state.Complete != tt.wantComplete {
				t.Errorf("state.Complete = %v, want %v", state.Complete, tt.wantComplete)
			}
			if yielded {
				t.Errorf("yielded = true, want false")
			}

			// Count [x] items in the TODO file.
			gotChecked := countChecked(t, todoPath)
			if gotChecked != tt.wantChecked {
				t.Errorf("checked [x] items = %d, want %d", gotChecked, tt.wantChecked)
			}

			// Verify the orchestration state file for checkpoint/resume tests.
			if tt.name == "checkpoint_file_persisted" {
				stateFile := cfg.Orchestration.StateFile
				if _, statErr := os.Stat(stateFile); os.IsNotExist(statErr) {
					t.Errorf("orchestration state file %q not found after loop completed", stateFile)
				} else {
					data, readErr := os.ReadFile(stateFile)
					if readErr != nil {
						t.Fatalf("read state file: %v", readErr)
					}
					var persistedState workflowExecutionState
					if jsonErr := json.Unmarshal(data, &persistedState); jsonErr != nil {
						t.Fatalf("unmarshal state file: %v", jsonErr)
					}
					if !persistedState.Complete {
						t.Errorf("persisted state.Complete = false, want true")
					}
					if persistedState.CurrentTodoLineNum != 0 {
						t.Errorf("persisted state.CurrentTodoLineNum = %d, want 0", persistedState.CurrentTodoLineNum)
					}
				}
			}
			if tt.name == "resume_from_checkpoint_file" {
				stateFile := cfg.Orchestration.StateFile
				if _, statErr := os.Stat(stateFile); os.IsNotExist(statErr) {
					t.Errorf("orchestration state file %q not found after loop completed", stateFile)
				} else {
					data, readErr := os.ReadFile(stateFile)
					if readErr != nil {
						t.Fatalf("read state file: %v", readErr)
					}
					var persistedState workflowExecutionState
					if jsonErr := json.Unmarshal(data, &persistedState); jsonErr != nil {
						t.Fatalf("unmarshal state file: %v", jsonErr)
					}
					if !persistedState.Complete {
						t.Errorf("persisted state.Complete = false, want true (loop completed all items)")
					}
					if persistedState.CurrentTodoLineNum != 0 {
						t.Errorf("persisted state.CurrentTodoLineNum = %d, want 0 (completed loop clears checkpoint)", persistedState.CurrentTodoLineNum)
					}
				}
			}
		})
	}
}

// countChecked returns the number of "[x]" or "[X]" markers in a file.
func countChecked(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	s := string(data)
	n := 0
	for i := 0; i+4 < len(s); i++ {
		if s[i] == '-' && s[i+1] == ' ' && s[i+2] == '[' && (s[i+3] == 'x' || s[i+3] == 'X') && s[i+4] == ']' {
			n++
		}
	}
	return n
}

// =============================================================================
// Checkpoint persistence unit tests
// =============================================================================

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_atomic.txt")

	data := []byte("hello world\nline 2\n")
	if err := writeFileAtomic(path, data, 0600); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("content mismatch: got %q, want %q", string(got), string(data))
	}

	// Overwrite atomically with new content.
	data2 := []byte("replacement content")
	if err := writeFileAtomic(path, data2, 0600); err != nil {
		t.Fatalf("writeFileAtomic overwrite: %v", err)
	}
	got2, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back after overwrite: %v", err)
	}
	if string(got2) != string(data2) {
		t.Errorf("after overwrite: got %q, want %q", string(got2), string(data2))
	}

	// No temp files left behind after successful writes.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp_") {
			t.Errorf("stale temp file left behind: %s", e.Name())
		}
	}
}

func TestLoopCheckpointFile_PersistAndLoad(t *testing.T) {
	dir := t.TempDir()

	// 1. No file → returns 0.
	lineNum, err := loadLoopCheckpoint(dir)
	if err != nil {
		t.Fatalf("loadLoopCheckpoint on fresh dir: %v", err)
	}
	if lineNum != 0 {
		t.Errorf("expected 0 for missing file, got %d", lineNum)
	}

	// 2. Persist and load back.
	if err := persistLoopCheckpoint(dir, 42); err != nil {
		t.Fatalf("persistLoopCheckpoint: %v", err)
	}
	lineNum, err = loadLoopCheckpoint(dir)
	if err != nil {
		t.Fatalf("loadLoopCheckpoint after persist: %v", err)
	}
	if lineNum != 42 {
		t.Errorf("expected 42, got %d", lineNum)
	}

	// 3. Overwrite with different value.
	if err := persistLoopCheckpoint(dir, 99); err != nil {
		t.Fatalf("persistLoopCheckpoint overwrite: %v", err)
	}
	lineNum, err = loadLoopCheckpoint(dir)
	if err != nil {
		t.Fatalf("loadLoopCheckpoint after overwrite: %v", err)
	}
	if lineNum != 99 {
		t.Errorf("expected 99, got %d", lineNum)
	}

	// 4. Remove checkpoint.
	removeLoopCheckpoint(dir)
	lineNum, err = loadLoopCheckpoint(dir)
	if err != nil {
		t.Fatalf("loadLoopCheckpoint after removal: %v", err)
	}
	if lineNum != 0 {
		t.Errorf("expected 0 after removal, got %d", lineNum)
	}

	// 5. No temp files left behind.
	checkpointDir := filepath.Join(dir, ".sprout")
	entries, err := os.ReadDir(checkpointDir)
	if err != nil {
		t.Fatalf("read .sprout dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp_") {
			t.Errorf("stale temp file left in .sprout: %s", e.Name())
		}
	}
}

func TestLoopCheckpointFile_InvalidContent(t *testing.T) {
	dir := t.TempDir()
	cpDir := filepath.Join(dir, ".sprout")
	os.MkdirAll(cpDir, 0755)

	// Write garbage to the checkpoint file.
	badPath := filepath.Join(cpDir, "todo_loop_checkpoint.txt")
	os.WriteFile(badPath, []byte("not a number\n"), 0600)

	// Should treat as missing — returns 0, no error.
	lineNum, err := loadLoopCheckpoint(dir)
	if err != nil {
		t.Fatalf("loadLoopCheckpoint with invalid content: %v", err)
	}
	if lineNum != 0 {
		t.Errorf("expected 0 for invalid content, got %d", lineNum)
	}

	// Empty file.
	os.WriteFile(badPath, []byte(""), 0600)
	lineNum, err = loadLoopCheckpoint(dir)
	if err != nil {
		t.Fatalf("loadLoopCheckpoint with empty file: %v", err)
	}
	if lineNum != 0 {
		t.Errorf("expected 0 for empty file, got %d", lineNum)
	}

	// Whitespace-only file.
	os.WriteFile(badPath, []byte("   \n  \n"), 0600)
	lineNum, err = loadLoopCheckpoint(dir)
	if err != nil {
		t.Fatalf("loadLoopCheckpoint with whitespace file: %v", err)
	}
	if lineNum != 0 {
		t.Errorf("expected 0 for whitespace file, got %d", lineNum)
	}

	// Zero value.
	os.WriteFile(badPath, []byte("0\n"), 0600)
	lineNum, err = loadLoopCheckpoint(dir)
	if err != nil {
		t.Fatalf("loadLoopCheckpoint with zero: %v", err)
	}
	if lineNum != 0 {
		t.Errorf("expected 0 for zero line number, got %d", lineNum)
	}
}

func TestLoadWorkflowExecutionState_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	sf := filepath.Join(tmpDir, "state.json")
	os.WriteFile(sf, []byte{}, 0644)

	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{Prompt: "t"}},
		Orchestration: &AgentWorkflowOrchestrationConfig{
			Enabled:    true,
			StateFile:  sf,
			EventsFile: filepath.Join(tmpDir, "events.jsonl"),
		},
	}
	cfg.validate()

	// Empty file should return a fresh state, not an error.
	state, err := loadWorkflowExecutionState(cfg)
	if err != nil {
		t.Fatalf("unexpected error for empty state file: %v", err)
	}
	if state.Version != 1 || state.Complete {
		t.Errorf("expected fresh state for empty file, got version=%d complete=%v", state.Version, state.Complete)
	}
}

func TestLoadWorkflowExecutionState_WhitespaceOnlyFile(t *testing.T) {
	tmpDir := t.TempDir()
	sf := filepath.Join(tmpDir, "state.json")
	os.WriteFile(sf, []byte("   \n  \n"), 0644)

	cfg := &AgentWorkflowConfig{
		Steps: []AgentWorkflowStep{{Prompt: "t"}},
		Orchestration: &AgentWorkflowOrchestrationConfig{
			Enabled:    true,
			StateFile:  sf,
			EventsFile: filepath.Join(tmpDir, "events.jsonl"),
		},
	}
	cfg.validate()

	state, err := loadWorkflowExecutionState(cfg)
	if err != nil {
		t.Fatalf("unexpected error for whitespace state file: %v", err)
	}
	if state.Version != 1 || state.Complete {
		t.Errorf("expected fresh state for whitespace file, got version=%d complete=%v", state.Version, state.Complete)
	}
}

// =============================================================================
// Fallback checkpoint integration with loop
// =============================================================================

func TestFallbackCheckpoint_WithoutOrchestration(t *testing.T) {
	dir := t.TempDir()
	todoPath := writeTempTodoFile(t, dir, []string{"Item 1", "Item 2"})
	gatePromptPath := writeGatePromptFile(t, dir)

	client := agent.NewScriptedClient(
		gateResponse("Item 1", "Process item 1", false),
		gateResponse("Item 2", "Process item 2", false),
	)
	client.SetModel("test:test")

	chatAgent := newTestLoopAgent(t, client)
	eventBus := events.NewEventBus()

	oldProcessFn := processQueryFn
	callCount := 0
	processQueryFn = func(ctx context.Context, _ *agent.Agent, _ *events.EventBus, query string) error {
		callCount++
		_ = query
		return nil
	}
	defer func() { processQueryFn = oldProcessFn }()

	// No orchestration config — loop should fall back to lightweight checkpoint.
	cfg := &AgentWorkflowConfig{
		Loop: &AgentWorkflowLoopConfig{
			TodoFile:       todoPath,
			GatePromptFile: gatePromptPath,
			MaxRetries:     1,
			MaxIterations:  50,
			BuildCommand:   "true",
		},
	}
	state := &workflowExecutionState{Version: 1}

	ctx := context.Background()
	yielded, err := runAgentWorkflowLoop(ctx, chatAgent, eventBus, cfg, state)

	if err != nil {
		t.Fatalf("runAgentWorkflowLoop error: %v", err)
	}
	if yielded {
		t.Errorf("yielded = true, want false")
	}
	if !state.Complete {
		t.Errorf("state.Complete = false, want true")
	}
	if callCount != 2 {
		t.Errorf("processQueryFn called %d times, want 2", callCount)
	}

	// After successful completion, the fallback checkpoint file should
	// be removed (not present).
	checkpointPath := loopCheckpointFilePath(dir)
	if _, statErr := os.Stat(checkpointPath); !os.IsNotExist(statErr) {
		t.Errorf("fallback checkpoint file %q still exists after completion", checkpointPath)
	}
}

func TestFallbackCheckpoint_ResumeOnRestart(t *testing.T) {
	dir := t.TempDir()
	todoPath := filepath.Join(dir, "TODO.md")
	content := `## Test Items
- [x] Item 1
- [ ] Item 2
- [ ] Item 3
`
	if err := os.WriteFile(todoPath, []byte(content), 0644); err != nil {
		t.Fatalf("write TODO.md: %v", err)
	}
	gatePromptPath := writeGatePromptFile(t, dir)

	// Simulate a partial run crash: Item 2 (line 3) was completed and marked
	// [x], then the fallback checkpoint was written with lineNum+1 = 4 to
	// resume from Item 3 (line 4).
	content = `## Test Items
- [x] Item 1
- [x] Item 2
- [ ] Item 3
`
	if err := os.WriteFile(todoPath, []byte(content), 0644); err != nil {
		t.Fatalf("write TODO.md: %v", err)
	}
	if err := persistLoopCheckpoint(dir, 4); err != nil {
		t.Fatalf("persistLoopCheckpoint: %v", err)
	}

	client := agent.NewScriptedClient(
		gateResponse("Item 3", "Process item 3", false),
	)
	client.SetModel("test:test")

	chatAgent := newTestLoopAgent(t, client)
	eventBus := events.NewEventBus()

	oldProcessFn := processQueryFn
	callCount := 0
	processQueryFn = func(ctx context.Context, _ *agent.Agent, _ *events.EventBus, query string) error {
		callCount++
		_ = query
		return nil
	}
	defer func() { processQueryFn = oldProcessFn }()

	// No orchestration — rely on fallback checkpoint.
	cfg := &AgentWorkflowConfig{
		Loop: &AgentWorkflowLoopConfig{
			TodoFile:       todoPath,
			GatePromptFile: gatePromptPath,
			MaxRetries:     1,
			MaxIterations:  50,
			BuildCommand:   "true",
		},
	}
	state := &workflowExecutionState{Version: 1}

	ctx := context.Background()
	yielded, err := runAgentWorkflowLoop(ctx, chatAgent, eventBus, cfg, state)

	if err != nil {
		t.Fatalf("runAgentWorkflowLoop error: %v", err)
	}
	if yielded {
		t.Errorf("yielded = true, want false")
	}
	if !state.Complete {
		t.Errorf("state.Complete = false, want true")
	}
	if callCount != 1 {
		t.Errorf("processQueryFn called %d times, want 1 (only Item 3)", callCount)
	}

	// Fallback checkpoint should be removed after completion.
	checkpointPath := loopCheckpointFilePath(dir)
	if _, statErr := os.Stat(checkpointPath); !os.IsNotExist(statErr) {
		t.Errorf("fallback checkpoint file %q still exists after completion", checkpointPath)
	}

	// Items 1 and 2 were pre-marked [x], Item 3 was processed and marked [x].
	gotChecked := countChecked(t, todoPath)
	if gotChecked != 3 {
		t.Errorf("checked [x] items = %d, want 3 (2 pre-marked + 1 processed)", gotChecked)
	}
}
