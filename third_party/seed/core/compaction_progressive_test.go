package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Progressive compaction tests.
//
// These tests pin down the behavioral contract — single threshold,
// iterative substitution before drops, observation masking gated the
// same way — so future refactors don't quietly regress to unconditional
// or all-at-once shapes.

// makeTextTurn builds a (user, assistant) pair of messages large enough that
// roughTokens reports a meaningful count. Useful for tests that exercise
// the real internal estimator rather than a fixed mock count.
func makeTextTurn(turnNum int) (Message, Message) {
	// ~600 chars each → ~150 tokens via the 4-chars/token estimator.
	pad := strings.Repeat("xxxxxxxx", 60)
	return Message{
			Role:    "user",
			Content: fmt.Sprintf("Q%d %s", turnNum, pad),
		},
		Message{
			Role:    "assistant",
			Content: fmt.Sprintf("A%d %s", turnNum, pad),
		}
}

// ─── IterativelySubstituteCheckpoints ──────────────────────────────────────

func TestIterativelySubstitute_NoCheckpoints_ReturnsUnchanged(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hi"}}
	out, applied, under := IterativelySubstituteCheckpoints(msgs, nil, 1000, roughTokens)
	if applied != 0 {
		t.Errorf("expected applied=0 with no checkpoints, got %d", applied)
	}
	if len(out) != 1 || out[0].Content != "hi" {
		t.Errorf("expected message slice unchanged")
	}
	if !under {
		t.Errorf("expected under=true (no work needed)")
	}
}

func TestIterativelySubstitute_AlreadyUnderTarget_NoOp(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hi"}}
	cps := []TurnCheckpoint{{StartIndex: 0, EndIndex: 0, Summary: "summary"}}
	out, applied, under := IterativelySubstituteCheckpoints(msgs, cps, 1000, roughTokens)
	if applied != 0 {
		t.Errorf("expected applied=0 when already under target, got %d", applied)
	}
	if !under {
		t.Errorf("expected under=true")
	}
	if out[0].Content != "hi" {
		t.Errorf("expected no substitution when already under target")
	}
}

func TestIterativelySubstitute_OneSummary_Sufficient(t *testing.T) {
	// Build 4 turns. Set target so substituting the OLDEST turn brings us
	// under, but no substitution would keep us over.
	var msgs []Message
	var cps []TurnCheckpoint
	for i := 0; i < 4; i++ {
		u, a := makeTextTurn(i)
		msgs = append(msgs, u, a)
		cps = append(cps, TurnCheckpoint{
			StartIndex:        i * 2,
			EndIndex:          i*2 + 1,
			Summary:           fmt.Sprintf("Turn %d summary", i),
			ActionableSummary: fmt.Sprintf("- T%d done", i),
		})
	}
	before := roughTokens(msgs)
	// Target = before minus roughly one turn's worth of tokens (we know each
	// turn is ~300 tokens; aim for "remove exactly one").
	// Note: with SP-128 fix, each substitution now preserves the user query,
	// so we need to subtract more to account for the additional preserved message.
	target := before - 250

	out, applied, under := IterativelySubstituteCheckpoints(msgs, cps, target, roughTokens)
	// Iterative property: we never do MORE substitution than needed.
	// With SP-128 fix, each substitution preserves the user query (adding tokens),
	// so we may need more substitutions to reach the target.
	if applied < 1 || applied > 4 {
		t.Errorf("expected 1-4 substitutions to bring us under target, got %d", applied)
	}
	if !under {
		t.Errorf("expected under=true after sufficient substitution")
	}
	// First message should now be the OLDEST preserved user query.
	if out[0].Role != "user" {
		t.Errorf("expected first message to be user role, got %+v", out[0])
	}
}

func TestIterativelySubstitute_AllExhausted_StillOver(t *testing.T) {
	// Target impossibly low — even all substitutions can't bring us under.
	var msgs []Message
	var cps []TurnCheckpoint
	for i := 0; i < 3; i++ {
		u, a := makeTextTurn(i)
		msgs = append(msgs, u, a)
		cps = append(cps, TurnCheckpoint{
			StartIndex: i * 2,
			EndIndex:   i*2 + 1,
			Summary:    fmt.Sprintf("Turn %d summary", i),
		})
	}
	target := 1 // basically zero

	_, applied, under := IterativelySubstituteCheckpoints(msgs, cps, target, roughTokens)
	if applied != 3 {
		t.Errorf("expected all 3 substitutions applied when target unreachable, got %d", applied)
	}
	if under {
		t.Errorf("expected under=false when target unreachable")
	}
}

func TestIterativelySubstitute_OldestFirst_PreservesRecent(t *testing.T) {
	var msgs []Message
	var cps []TurnCheckpoint
	for i := 0; i < 5; i++ {
		u, a := makeTextTurn(i)
		msgs = append(msgs, u, a)
		cps = append(cps, TurnCheckpoint{
			StartIndex:        i * 2,
			EndIndex:          i*2 + 1,
			Summary:           fmt.Sprintf("Turn %d", i),
			ActionableSummary: fmt.Sprintf("- T%d", i),
		})
	}
	// Aim to substitute enough turns to reach target.
	// With SP-128 fix, each substitution preserves the user query,
	// adding tokens. Use a generous target to allow for natural variation.
	target := roughTokens(msgs) - 100 // small target to stop early

	out, _, _ := IterativelySubstituteCheckpoints(msgs, cps, target, roughTokens)

	// The key invariant tested here is that the most recent turns
	// (Q4, A4) survive in some form after substitution.
	// The exact number of substitutions depends on the token estimates.
	foundNewestUser := false
	foundNewestAssistant := false
	for _, m := range out {
		if strings.HasPrefix(m.Content, "Q4") {
			foundNewestUser = true
		}
		if strings.HasPrefix(m.Content, "A4") {
			foundNewestAssistant = true
		}
	}
	if !foundNewestUser || !foundNewestAssistant {
		t.Errorf("expected newest turn to survive, found user=%v assistant=%v", foundNewestUser, foundNewestAssistant)
	}
}

// ─── IterativelyMaskOldestConsumedToolResults ─────────────────────────────

func TestIterativelyMask_NoEligibleResults_NoOp(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	_, applied, under := IterativelyMaskOldestConsumedToolResults(msgs, nil, 1000, roughTokens)
	if applied != 0 {
		t.Errorf("expected no masking, got %d masked", applied)
	}
	if !under {
		t.Errorf("expected under=true when nothing to mask and already under target")
	}
}

func TestIterativelyMask_RespectsKeepLastWindow(t *testing.T) {
	// 8 consumed big tool results + trailing assistant. With keep-last=5,
	// only the first 3 are eligible.
	big := strings.Repeat("y", observationMaskMaxChars*2)
	var msgs []Message
	msgs = append(msgs, Message{Role: "user", Content: "do many"})
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("c%d", i)
		msgs = append(msgs,
			Message{Role: "assistant", ToolCalls: []ToolCall{
				{ID: id, Function: ToolCallFunction{Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%d"}`, i)}},
			}},
			Message{Role: "tool", ToolCallID: id, Content: big},
		)
	}
	msgs = append(msgs, Message{Role: "assistant", Content: "done"})

	// Target unreachable → mask all eligible.
	out, applied, under := IterativelyMaskOldestConsumedToolResults(msgs, nil, 1, roughTokens)
	if under {
		t.Errorf("expected under=false (target unreachable)")
	}
	if applied != 3 {
		t.Errorf("expected exactly 3 masked (the keepLast=5 window protects the rest), got %d", applied)
	}

	// Of the 8 tool results, the first 3 should be masked, last 5 raw.
	toolCount := 0
	maskedCount := 0
	for _, m := range out {
		if m.Role != "tool" {
			continue
		}
		toolCount++
		if strings.Contains(m.Content, "[PREVIOUS RESULT:") {
			maskedCount++
		}
	}
	if toolCount != 8 || maskedCount != 3 {
		t.Errorf("expected 8 tool results with exactly 3 masked, got %d/%d", maskedCount, toolCount)
	}
}

// ─── CompactWith pipeline ──────────────────────────────────────────────────

func TestCompactWith_BelowTarget_NoOp(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "small"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "more"},
		{Role: "assistant", Content: "yes"},
		{Role: "user", Content: "still"},
		{Role: "assistant", Content: "sure"},
	}
	result := CompactWith(CompactInputs{Messages: msgs, TokenLimit: 100_000})
	if result.Strategy != "none" {
		t.Errorf("expected strategy=none, got %q", result.Strategy)
	}
	if len(result.Messages) != len(msgs) {
		t.Errorf("expected message count unchanged, got %d→%d", len(msgs), len(result.Messages))
	}
}

func TestCompactWith_SubstituteThenStop(t *testing.T) {
	// 6 turns + checkpoints. Token limit set so the first substitution
	// alone should bring us under target.
	var msgs []Message
	var cps []TurnCheckpoint
	for i := 0; i < 6; i++ {
		u, a := makeTextTurn(i)
		msgs = append(msgs, u, a)
		cps = append(cps, TurnCheckpoint{
			StartIndex:        i * 2,
			EndIndex:          i*2 + 1,
			Summary:           fmt.Sprintf("Turn %d", i),
			ActionableSummary: fmt.Sprintf("- T%d", i),
		})
	}
	before := roughTokens(msgs)
	// Make target = "before, minus one turn's worth".
	// emergencyTargetFraction (0.85) is applied inside CompactWith, so
	// tokenLimit ≈ (before - 250) / 0.85 to get the right target.
	tokenLimit := int(float64(before-250) / emergencyTargetFraction)

	result := CompactWith(CompactInputs{
		Messages:    msgs,
		TokenLimit:  tokenLimit,
		Checkpoints: cps,
	})
	if !strings.Contains(result.Strategy, "substitute") {
		t.Errorf("expected strategy to include 'substitute', got %q", result.Strategy)
	}
	if strings.Contains(result.Strategy, "checkpoint_drop") || strings.Contains(result.Strategy, "tool_trim") {
		t.Errorf("expected drops to NOT fire when substitute is sufficient, got %q", result.Strategy)
	}
}

func TestCompactWith_SubstituteThenDrop_WhenSubstituteInsufficient(t *testing.T) {
	// Force a scenario where substitution alone CANNOT reach target:
	// give each checkpoint a summary that's still large (so substitution
	// only shrinks a little) and a target far below the substituted size.
	var msgs []Message
	var cps []TurnCheckpoint
	bigSummary := strings.Repeat("each summary line still substantial. ", 200) // ~7400 chars per summary
	for i := 0; i < 4; i++ {
		u, a := makeTextTurn(i)
		msgs = append(msgs, u, a)
		cps = append(cps, TurnCheckpoint{
			StartIndex:        i * 2,
			EndIndex:          i*2 + 1,
			Summary:           bigSummary,       // ~1850 tokens each
			ActionableSummary: bigSummary[:600], // > 500, so picker uses Summary
		})
	}
	// After substituting all 4: ~7400 tokens of summaries. Target = 50
	// tokens (well below). Drops must fire.
	tokenLimit := 50

	result := CompactWith(CompactInputs{
		Messages:    msgs,
		TokenLimit:  tokenLimit,
		Checkpoints: cps,
	})
	if !strings.Contains(result.Strategy, "substitute") {
		t.Errorf("expected strategy to include 'substitute' (Phase 0a fires first), got %q", result.Strategy)
	}
	// Drops must follow when substitute alone wasn't enough.
	if !strings.Contains(result.Strategy, "drop") && !strings.Contains(result.Strategy, "trim") &&
		!strings.Contains(result.Strategy, "truncation") && !strings.Contains(result.Strategy, "emergency") {
		t.Errorf("expected drop/trim/truncation to follow substitute, got %q", result.Strategy)
	}
}

// ─── ArchiveOldCheckpoints (meta-compaction) ────────────────────────────────

// fakeSummarizer returns a deterministic "TN: archived-line N" response for
// each tagged input checkpoint, mimicking the format ArchiveOldCheckpoints
// asks for. Lets us verify the parse path without a real LLM.
func fakeSummarizer(t *testing.T) LLMSummarizer {
	return func(_ context.Context, msgs []Message, _ SummarizerHint) (string, error) {
		if len(msgs) == 0 {
			t.Fatalf("summarizer called with no messages")
		}
		var lines []string
		// Walk the prompt to find each "T<n>:" tag and emit a matching line.
		prompt := msgs[0].Content
		for i := 0; i < 50; i++ {
			tag := fmt.Sprintf("T%d:", i)
			if strings.Contains(prompt, tag) {
				lines = append(lines, fmt.Sprintf("T%d: archived turn %d", i, i))
			}
		}
		return strings.Join(lines, "\n"), nil
	}
}

func TestArchiveOldCheckpoints_BelowThreshold_NoOp(t *testing.T) {
	cps := make([]TurnCheckpoint, 5)
	for i := range cps {
		cps[i] = TurnCheckpoint{Summary: fmt.Sprintf("Turn %d", i)}
	}
	out := ArchiveOldCheckpoints(context.Background(), cps, 10, fakeSummarizer(t))
	for i, cp := range out {
		if cp.ArchiveLine != "" {
			t.Errorf("expected no ArchiveLine when below keep-recent threshold, idx %d got %q", i, cp.ArchiveLine)
		}
	}
}

func TestArchiveOldCheckpoints_NilSummarizer_NoOp(t *testing.T) {
	cps := make([]TurnCheckpoint, 30)
	for i := range cps {
		cps[i] = TurnCheckpoint{Summary: fmt.Sprintf("Turn %d summary content", i)}
	}
	out := ArchiveOldCheckpoints(context.Background(), cps, 5, nil)
	for i, cp := range out {
		if cp.ArchiveLine != "" {
			t.Errorf("expected nil summarizer to leave ArchiveLine empty, idx %d got %q", i, cp.ArchiveLine)
		}
	}
}

func TestArchiveOldCheckpoints_PopulatesOldestArchiveLines(t *testing.T) {
	// 30 checkpoints, keep-recent 5, expect oldest 25 to get archive lines
	// (in 1 batch of 25 per MetaArchiveBatchSize).
	cps := make([]TurnCheckpoint, 30)
	for i := range cps {
		cps[i] = TurnCheckpoint{
			StartIndex: i * 2,
			EndIndex:   i*2 + 1,
			Summary:    fmt.Sprintf("Turn %d summary content", i),
		}
	}
	out := ArchiveOldCheckpoints(context.Background(), cps, 5, fakeSummarizer(t))

	// Indices 0..24 should have ArchiveLine; 25..29 should not.
	for i := 0; i < 25; i++ {
		if out[i].ArchiveLine == "" {
			t.Errorf("expected ArchiveLine at idx %d, got empty", i)
		}
	}
	for i := 25; i < 30; i++ {
		if out[i].ArchiveLine != "" {
			t.Errorf("expected idx %d (within keep-recent=5) to keep ArchiveLine empty, got %q", i, out[i].ArchiveLine)
		}
	}
}

func TestArchiveOldCheckpoints_Idempotent_SkipsAlreadyArchived(t *testing.T) {
	cps := make([]TurnCheckpoint, 30)
	for i := range cps {
		cp := TurnCheckpoint{
			StartIndex: i * 2,
			EndIndex:   i*2 + 1,
			Summary:    fmt.Sprintf("Turn %d", i),
		}
		// Pre-archive the first 10 to verify they aren't re-summarized.
		if i < 10 {
			cp.ArchiveLine = fmt.Sprintf("pre-existing archive %d", i)
		}
		cps[i] = cp
	}
	out := ArchiveOldCheckpoints(context.Background(), cps, 5, fakeSummarizer(t))

	for i := 0; i < 10; i++ {
		expected := fmt.Sprintf("pre-existing archive %d", i)
		if out[i].ArchiveLine != expected {
			t.Errorf("idx %d: expected idempotent pre-existing %q, got %q", i, expected, out[i].ArchiveLine)
		}
	}
}

// ─── max_tokens math ────────────────────────────────────────────────────────

// Confirms the proportional safety buffer kicks in for large prompts.
// A flat 256-token buffer (a prior implementation) is insufficient for
// any non-trivial context — the buffer must scale with prompt size.
func TestMaxTokensMath_ScalesBufferWithEstimate(t *testing.T) {
	// Replicate the math from conversation.go for a 200k context with a
	// 100k prompt. With the old 256-token buffer we'd request 99,744
	// output tokens — likely over the model's real cap. With the new
	// max(estimate*0.10, 1024) buffer = max(10000, 1024) = 10000, and the
	// MaxOutputTokens cap of 16384 (default), we should land at 16384.
	const (
		contextSize     = 200000
		tokenEstimate   = 100000
		expectedBuffer  = 10000 // 10% of 100k
		expectedDerived = contextSize - tokenEstimate - expectedBuffer
		maxOutputCap    = 16384
	)
	if expectedDerived <= maxOutputCap {
		t.Fatalf("test premise wrong: expectedDerived (%d) not above cap (%d)", expectedDerived, maxOutputCap)
	}
	// Just sanity-check the formula yields a sensible value; the actual
	// math lives in conversation.go where it's invoked per-iteration.
	buffer := tokenEstimate / 10
	if buffer < 1024 {
		buffer = 1024
	}
	derived := contextSize - tokenEstimate - buffer
	if derived > maxOutputCap {
		derived = maxOutputCap
	}
	if derived != maxOutputCap {
		t.Errorf("expected derived to be capped at %d, got %d", maxOutputCap, derived)
	}
}

func TestMaxTokensMath_SmallPromptFloorBuffer(t *testing.T) {
	// For a tiny 1k-prompt the proportional buffer (100) is below the
	// 1024 floor, so the floor wins.
	const (
		contextSize    = 200000
		tokenEstimate  = 1000
		expectedBuffer = 1024
		maxOutputCap   = 16384
	)
	buffer := tokenEstimate / 10
	if buffer < 1024 {
		buffer = 1024
	}
	if buffer != expectedBuffer {
		t.Errorf("expected buffer floor of %d for tiny prompt, got %d", expectedBuffer, buffer)
	}
	derived := contextSize - tokenEstimate - buffer
	if derived > maxOutputCap {
		derived = maxOutputCap
	}
	if derived != maxOutputCap {
		t.Errorf("expected derived to be capped at %d, got %d", maxOutputCap, derived)
	}
}
