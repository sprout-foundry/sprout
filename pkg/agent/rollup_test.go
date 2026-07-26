package agent

import (
	"strings"
	"sync"
	"testing"
)

// makeLevelZeroCheckpoints returns n synthetic per-turn checkpoints, each
// covering a 5-message span and carrying a deterministic summary so tests
// can assert what got folded.
func makeLevelZeroCheckpoints(n int) []TurnCheckpoint {
	out := make([]TurnCheckpoint, n)
	for i := 0; i < n; i++ {
		out[i] = TurnCheckpoint{
			ID:                "cp-" + itoa(i),
			StartIndex:        i * 5,
			EndIndex:          i*5 + 4,
			Summary:           "turn " + itoa(i) + " summary",
			ActionableSummary: "turn " + itoa(i) + " actionable",
			Level:             0,
			CoveredTurns:      0, // legacy default; rollup math treats this as 1
		}
	}
	return out
}

func itoa(n int) string {
	// Tiny inline to avoid pulling strconv in the helper.
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	s := string(buf[i:])
	if neg {
		return "-" + s
	}
	return s
}

// TestPickRollupTarget_TriggersAtThreshold confirms a rollup fires when the
// level-0 count, *minus* the recency window, reaches the trigger.
//
// Trigger requires (count − recentTurnsToPreserveDefault) ≥ rollupTriggerCount,
// i.e. (30 − 10) = 20 = trigger threshold.
func TestPickRollupTarget_TriggersAtThreshold(t *testing.T) {
	checkpoints := makeLevelZeroCheckpoints(rollupTriggerCount + recentTurnsToPreserveDefault)
	start, end, level, ok := pickRollupTarget(checkpoints, recentTurnsToPreserveDefault)
	if !ok {
		t.Fatalf("expected rollup to fire at threshold; counts: %d", len(checkpoints))
	}
	if level != 0 {
		t.Fatalf("expected level=0 target, got %d", level)
	}
	if start != 0 {
		t.Fatalf("expected start at oldest entry (0), got %d", start)
	}
	if end != rollupSourceCount-1 {
		t.Fatalf("expected end=%d (rollupSourceCount-1), got %d", rollupSourceCount-1, end)
	}
}

// TestPickRollupTarget_PreservesRecencyWindow guards the most-recent K
// per-turn checkpoints from being folded. The window has exactly K entries
// so the trigger should not fire.
func TestPickRollupTarget_PreservesRecencyWindow(t *testing.T) {
	// One fewer than the trigger threshold + recency window — should not fire.
	checkpoints := makeLevelZeroCheckpoints(rollupTriggerCount + recentTurnsToPreserveDefault - 1)
	_, _, _, ok := pickRollupTarget(checkpoints, recentTurnsToPreserveDefault)
	if ok {
		t.Fatalf("rollup fired before threshold reached (count=%d)", len(checkpoints))
	}
}

// TestPickRollupTarget_NoOpUnderThreshold verifies the worker stays quiet
// when there's nothing to fold.
func TestPickRollupTarget_NoOpUnderThreshold(t *testing.T) {
	checkpoints := makeLevelZeroCheckpoints(5)
	_, _, _, ok := pickRollupTarget(checkpoints, recentTurnsToPreserveDefault)
	if ok {
		t.Fatalf("rollup fired with only 5 checkpoints; want no-op")
	}
}

// TestPickRollupTarget_LevelOnePromotion ensures that once enough Level-1
// rollups accumulate, they get folded into a Level-2 rollup. Phase 2c's
// hierarchical-rollup claim is the whole point of bounded-list behavior.
func TestPickRollupTarget_LevelOnePromotion(t *testing.T) {
	// rollupTriggerCount Level-1 entries + some Level-0 entries below
	// the trigger threshold so only the Level-1 work is eligible.
	checkpoints := make([]TurnCheckpoint, 0, rollupTriggerCount+5)
	for i := 0; i < rollupTriggerCount; i++ {
		checkpoints = append(checkpoints, TurnCheckpoint{
			ID:           "rollup-" + itoa(i),
			StartIndex:   i * 100,
			EndIndex:     i*100 + 99,
			Summary:      "rollup " + itoa(i),
			Level:        1,
			CoveredTurns: rollupSourceCount,
		})
	}
	for i := 0; i < 5; i++ {
		checkpoints = append(checkpoints, TurnCheckpoint{
			ID:         "cp-" + itoa(i),
			StartIndex: rollupTriggerCount*100 + i*5,
			EndIndex:   rollupTriggerCount*100 + i*5 + 4,
			Summary:    "turn " + itoa(i),
			Level:      0,
		})
	}

	start, end, level, ok := pickRollupTarget(checkpoints, recentTurnsToPreserveDefault)
	if !ok {
		t.Fatalf("expected level-1 rollup to fire")
	}
	if level != 1 {
		t.Fatalf("expected level=1 target, got %d", level)
	}
	if start != 0 || end != rollupSourceCount-1 {
		t.Fatalf("expected oldest %d level-1 entries [0..%d], got [%d..%d]", rollupSourceCount, rollupSourceCount-1, start, end)
	}
}

// TestUnionFileChanges_LastWriteWins exercises the merge semantics: when a
// file shows up in multiple source checkpoints, the most-recent op
// overrides earlier ones (the rolled-up span ended with that op).
func TestUnionFileChanges_LastWriteWins(t *testing.T) {
	sources := []TurnCheckpoint{
		{FileChanges: []CheckpointFileChange{{Path: "a.go", Op: "A"}, {Path: "b.go", Op: "M"}}},
		{FileChanges: []CheckpointFileChange{{Path: "a.go", Op: "M"}}},                          // modify the file added earlier
		{FileChanges: []CheckpointFileChange{{Path: "b.go", Op: "D"}, {Path: "c.go", Op: "A"}}}, // delete b, add c
	}
	got := unionFileChanges(sources)
	want := map[string]string{
		"a.go": "M",
		"b.go": "D",
		"c.go": "A",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d entries, got %d: %+v", len(want), len(got), got)
	}
	for _, fc := range got {
		if want[fc.Path] != fc.Op {
			t.Errorf("path %s: got op=%s, want %s", fc.Path, fc.Op, want[fc.Path])
		}
	}
}

// TestUnionFileChanges_PreservesFirstSeenOrder verifies the merge keeps
// first-appearance order for stable UI rendering across runs.
func TestUnionFileChanges_PreservesFirstSeenOrder(t *testing.T) {
	sources := []TurnCheckpoint{
		{FileChanges: []CheckpointFileChange{{Path: "z.go", Op: "A"}}},
		{FileChanges: []CheckpointFileChange{{Path: "a.go", Op: "A"}}},
		{FileChanges: []CheckpointFileChange{{Path: "z.go", Op: "M"}}},
	}
	got := unionFileChanges(sources)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0].Path != "z.go" || got[1].Path != "a.go" {
		t.Fatalf("order not preserved: %+v", got)
	}
}

// TestLatestRevisionID returns the most recent non-empty RevisionID so the
// rolled-up checkpoint references the diff that still resolves under
// view_history (older revisions may have been GC'd).
func TestLatestRevisionID(t *testing.T) {
	sources := []TurnCheckpoint{
		{RevisionID: "rev-1"},
		{RevisionID: ""},
		{RevisionID: "rev-3"},
		{RevisionID: ""},
	}
	if got := latestRevisionID(sources); got != "rev-3" {
		t.Fatalf("got %q, want rev-3", got)
	}
}

// TestLatestRevisionID_AllEmpty is the change-tracker-disabled case: no
// revision IDs anywhere, so the rollup carries an empty RevisionID and
// the model won't try to call view_history on it.
func TestLatestRevisionID_AllEmpty(t *testing.T) {
	sources := []TurnCheckpoint{{}, {}, {}}
	if got := latestRevisionID(sources); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

// TestBuildRollupInputBlocks_RendersChronologically verifies the rendered
// input matches the format the rollup prompt template expects: numbered
// blocks, oldest first, with the level annotation on rollup sources.
func TestBuildRollupInputBlocks_RendersChronologically(t *testing.T) {
	sources := []TurnCheckpoint{
		{Summary: "first turn", FileChanges: []CheckpointFileChange{{Path: "a.go", Op: "M"}}},
		{Summary: "second rollup", Level: 1, CoveredTurns: 20},
	}
	got := buildRollupInputBlocks(sources)
	if !strings.Contains(got, "### Source 1") {
		t.Errorf("missing Source 1 header: %q", got)
	}
	if !strings.Contains(got, "### Source 2 (level 1 rollup covering 20 turns)") {
		t.Errorf("missing rollup-level annotation: %q", got)
	}
	if !strings.Contains(got, "first turn") || !strings.Contains(got, "second rollup") {
		t.Errorf("missing summary bodies: %q", got)
	}
	if !strings.Contains(got, "Files: M a.go") {
		t.Errorf("missing file manifest: %q", got)
	}
	// Source 1 must come before Source 2 (chronological order).
	if strings.Index(got, "Source 1") > strings.Index(got, "Source 2") {
		t.Errorf("blocks out of order: %q", got)
	}
}

// TestReplaceWithRollup_SplicesCorrectly verifies the splice logic via a
// minimal Agent constructed for the test. Boundary cases (out-of-range
// indices) are no-ops to avoid corrupting state when concurrent edits
// invalidate the picked range.
func TestReplaceWithRollup_SplicesCorrectly(t *testing.T) {
	a := newRollupTestAgent(t, makeLevelZeroCheckpoints(5))
	rollup := TurnCheckpoint{ID: "rollup-1", Summary: "rolled", Level: 1, CoveredTurns: 3}

	a.replaceWithRollup(1, 3, rollup)

	got := a.copyTurnCheckpoints()
	if len(got) != 3 {
		t.Fatalf("expected 3 checkpoints after splice, got %d", len(got))
	}
	if got[0].ID != "cp-0" {
		t.Errorf("expected cp-0 at index 0, got %s", got[0].ID)
	}
	if got[1].ID != "rollup-1" {
		t.Errorf("expected rollup-1 at index 1, got %s", got[1].ID)
	}
	if got[2].ID != "cp-4" {
		t.Errorf("expected cp-4 at index 2, got %s", got[2].ID)
	}
}

// TestReplaceWithRollup_RejectsBadRange protects state from corruption when
// pickRollupTarget races with a concurrent /compact that shrinks the list.
func TestReplaceWithRollup_RejectsBadRange(t *testing.T) {
	a := newRollupTestAgent(t, makeLevelZeroCheckpoints(3))
	before := a.copyTurnCheckpoints()

	a.replaceWithRollup(0, 99, TurnCheckpoint{ID: "junk"})

	after := a.copyTurnCheckpoints()
	if len(before) != len(after) {
		t.Fatalf("state mutated despite bad range: before=%d after=%d", len(before), len(after))
	}
}

// newRollupTestAgent constructs the minimum Agent surface the rollup splice
// code needs — state with a checkpoint mutex and a preloaded checkpoint
// list. Avoids the full agent constructor which pulls in LLM clients.
func newRollupTestAgent(t *testing.T, initial []TurnCheckpoint) *Agent {
	t.Helper()
	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.state.SetTurnCheckpoints(initial)
	return a
}

// --- Enable/Disable streaming ---

func TestStreaming_EnableDisable(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}

	if a.IsStreamingEnabled() {
		t.Error("streaming should start disabled")
	}

	a.SetStreamingEnabled(true)
	if !a.IsStreamingEnabled() {
		t.Error("streaming should be enabled after SetStreamingEnabled(true)")
	}

	a.SetStreamingEnabled(false)
	if a.IsStreamingEnabled() {
		t.Error("streaming should be disabled after SetStreamingEnabled(false)")
	}
}

// --- Mutex auto-creation on enable ---

func TestStreaming_EnablesMutex(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(nil)

	a.SetStreamingEnabled(true)

	mu := a.output.GetOutputMutex()
	if mu == nil {
		t.Error("expected output mutex to be created when streaming enabled and mutex was nil")
	}
}

func TestStreaming_DoesNotReplaceExistingMutex(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	existingMutex := &sync.Mutex{}
	a.output.SetOutputMutex(existingMutex)

	a.SetStreamingEnabled(true)

	mu := a.output.GetOutputMutex()
	if mu != existingMutex {
		t.Error("expected existing mutex to be preserved, not replaced")
	}
}

// --- DisableStreaming clears state ---

func TestStreaming_DisableClearsState(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	a.output.SetStreamingCallback(func(s string) {})
	a.output.SetFlushCallback(func() {})
	a.output.SetStreamingEnabled(true)

	a.DisableStreaming()

	if a.IsStreamingEnabled() {
		t.Error("streaming should be disabled")
	}
	if a.output.GetStreamingCallback() != nil {
		t.Error("streaming callback should be nil after DisableStreaming")
	}
	if a.output.GetFlushCallback() != nil {
		t.Error("flush callback should be nil after DisableStreaming")
	}
}

// --- SetStreamingCallback / EnableStreaming ---

func TestStreaming_SetStreamingCallback(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var received string
	a.SetStreamingCallback(func(s string) { received = s })

	cb := a.output.GetStreamingCallback()
	if cb == nil {
		t.Fatal("expected callback to be set")
	}
	cb("test chunk")
	if received != "test chunk" {
		t.Errorf("expected 'test chunk', got %q", received)
	}
}

func TestStreaming_EnableStreaming(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var captured string
	a.EnableStreaming(func(s string) { captured = s })

	if !a.IsStreamingEnabled() {
		t.Error("streaming should be enabled after EnableStreaming")
	}

	cb := a.output.GetStreamingCallback()
	if cb == nil {
		t.Fatal("expected callback to be set")
	}
	cb("enable-streaming-test")
	if captured != "enable-streaming-test" {
		t.Errorf("expected 'enable-streaming-test', got %q", captured)
	}
}

// --- SetFlushCallback ---

func TestStreaming_SetFlushCallback(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var flushed bool
	a.SetFlushCallback(func() { flushed = true })

	cb := a.output.GetFlushCallback()
	if cb == nil {
		t.Fatal("expected flush callback to be set")
	}
	cb()
	if !flushed {
		t.Error("expected flush callback to have been called")
	}
}

// --- SetOutputMutex ---

func TestStreaming_SetOutputMutex(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	mu := &sync.Mutex{}
	a.SetOutputMutex(mu)
	if a.output.GetOutputMutex() != mu {
		t.Error("expected same mutex")
	}
}

// --- PublishStreamChunk: router path ---

func TestStreaming_PublishStreamChunk_WithRouter(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var terminalChunks []string
	a.output.SetStreamingCallback(func(s string) {
		terminalChunks = append(terminalChunks, s)
	})
	a.output.SetStreamingEnabled(true)

	router := NewOutputRouter(a, nil)
	a.output.SetOutputRouter(router)

	// Assistant text should go to terminal callback via router
	a.PublishStreamChunk("hello world", "assistant_text")

	if len(terminalChunks) != 1 {
		t.Fatalf("expected 1 terminal chunk, got %d", len(terminalChunks))
	}
	if terminalChunks[0] != "hello world" {
		t.Errorf("expected 'hello world', got %q", terminalChunks[0])
	}
}

func TestStreaming_PublishStreamChunk_WithRouter_ReasoningNotToTerminal(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var terminalChunks []string
	a.output.SetStreamingCallback(func(s string) {
		terminalChunks = append(terminalChunks, s)
	})
	a.output.SetStreamingEnabled(true)

	router := NewOutputRouter(a, nil)
	a.output.SetOutputRouter(router)

	// Reasoning should NOT go to terminal callback via router
	a.PublishStreamChunk("reasoning step", "reasoning")

	if len(terminalChunks) != 0 {
		t.Errorf("expected 0 terminal chunks for reasoning, got %d: %v", len(terminalChunks), terminalChunks)
	}
}

func TestStreaming_PublishStreamChunk_WithRouter_DefaultContentType(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var terminalChunks []string
	a.output.SetStreamingCallback(func(s string) {
		terminalChunks = append(terminalChunks, s)
	})
	a.output.SetStreamingEnabled(true)

	router := NewOutputRouter(a, nil)
	a.output.SetOutputRouter(router)

	// Empty contentType should default to assistant_text
	a.PublishStreamChunk("test content", "")

	if len(terminalChunks) != 1 {
		t.Fatalf("expected 1 terminal chunk, got %d", len(terminalChunks))
	}
	if terminalChunks[0] != "test content" {
		t.Errorf("expected 'test content', got %q", terminalChunks[0])
	}
}

// --- PublishStreamChunk: fallback path (no router) ---

func TestStreaming_PublishStreamChunk_NoRouter(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var callbackChunks []string
	a.output.SetStreamingCallback(func(s string) {
		callbackChunks = append(callbackChunks, s)
	})

	// No router set — should use fallback path
	a.PublishStreamChunk("fallback chunk", "assistant_text")

	if len(callbackChunks) != 1 {
		t.Fatalf("expected 1 callback chunk, got %d", len(callbackChunks))
	}
	if callbackChunks[0] != "fallback chunk" {
		t.Errorf("expected 'fallback chunk', got %q", callbackChunks[0])
	}
}

func TestStreaming_PublishStreamChunk_NoRouter_DefaultContentType(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var received string
	a.output.SetStreamingCallback(func(s string) { received = s })

	// Empty content type should default to "assistant_text"
	a.PublishStreamChunk("test", "")
	if received != "test" {
		t.Errorf("expected 'test', got %q", received)
	}
}

func TestStreaming_PublishStreamChunk_NoRouter_ReasoningNotForwarded(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var callbackCalled bool
	a.output.SetStreamingCallback(func(s string) { callbackCalled = true })

	// Reasoning content should NOT go to streaming callback in fallback path
	a.PublishStreamChunk("reasoning", "reasoning")

	if callbackCalled {
		t.Error("reasoning content should not trigger streaming callback in fallback path")
	}
}

func TestStreaming_PublishStreamChunk_CallbackIntegration(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var buf strings.Builder
	a.output.SetStreamingCallback(func(s string) { buf.WriteString(s) })
	a.output.SetStreamingEnabled(true)

	a.PublishStreamChunk("abc", "")
	if buf.String() != "abc" {
		t.Errorf("expected 'abc' in buffer, got %q", buf.String())
	}
}
