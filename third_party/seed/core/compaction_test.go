package core

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// ==============================================================================
// 1. Compact() — Main entry point & algorithm phases
// ==============================================================================

func TestCompact_NoOp_UnderLimit(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hi"},
		{Role: "assistant", Content: "Hello!"},
	}
	result := Compact(messages, 10000)

	if result.Strategy != "none" {
		t.Errorf("expected strategy 'none', got %q", result.Strategy)
	}
	if len(result.Messages) != len(messages) {
		t.Errorf("expected no compaction, got %d messages", len(result.Messages))
	}
	if result.TokensBefore != result.TokensAfter {
		t.Errorf("expected TokensBefore == TokensAfter")
	}
}

func TestCompact_NoOp_TooFewMessages(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Hi"},
		{Role: "assistant", Content: "Hello"},
	}
	result := Compact(messages, 10)

	if result.Strategy != "none" {
		t.Errorf("expected strategy 'none', got %q", result.Strategy)
	}
	if len(result.Messages) != len(messages) {
		t.Errorf("expected no compaction for < minMessages, got %d", len(result.Messages))
	}
}

func TestCompact_Phase1_CheckpointDrop(t *testing.T) {
	messages := []Message{{Role: "system", Content: "You are helpful."}}
	// 5 large checkpoint summaries to trigger compaction and make checkpoint_drop sufficient
	for i := 0; i < 5; i++ {
		messages = append(messages, Message{
			Role:    "user",
			Content: "Summary " + strings.Repeat("x", 990),
			Meta:    map[string]string{MetaKeyCheckpoint: "true"},
		})
	}
	// 30 recent non-checkpoint messages (protected zone filler)
	for i := 0; i < 30; i++ {
		messages = append(messages, Message{Role: "user", Content: strings.Repeat("a", 50)})
		messages = append(messages, Message{Role: "assistant", Content: strings.Repeat("b", 50)})
	}

	tokenLimit := roughTokens(messages) - 500 // Over limit by enough to need checkpoint drops
	result := Compact(messages, tokenLimit)

	if result.Strategy != "checkpoint_drop" {
		t.Errorf("expected strategy 'checkpoint_drop', got %q", result.Strategy)
	}
	if len(result.Messages) >= len(messages) {
		t.Errorf("expected message reduction, got %d (was %d)", len(result.Messages), len(messages))
	}
}

func TestCompact_Phase2_TurnDrop(t *testing.T) {
	messages := []Message{{Role: "system", Content: "You are helpful."}}
	// 2 small checkpoints (not enough to satisfy the limit alone)
	for i := 0; i < 2; i++ {
		messages = append(messages, Message{
			Role:    "user",
			Content: "CP" + string(rune('0'+i)),
			Meta:    map[string]string{MetaKeyCheckpoint: "true"},
		})
	}
	// 3 large turns (enough content to need turn drop after checkpoint removal)
	for i := 1; i <= 3; i++ {
		messages = append(messages, Message{
			Role:    "user",
			Content: "Turn" + string(rune('0'+i)) + " User " + strings.Repeat("x", 990),
		})
		messages = append(messages, Message{
			Role:    "assistant",
			Content: "Turn" + string(rune('0'+i)) + " Assistant " + strings.Repeat("x", 990),
		})
	}
	// 20 recent message pairs
	for i := 0; i < 20; i++ {
		messages = append(messages, Message{Role: "user", Content: "recent user " + string(rune('a'+i))})
		messages = append(messages, Message{Role: "assistant", Content: "recent assistant " + string(rune('a'+i))})
	}

	tokenLimit := int(float64(roughTokens(messages)) * 0.9)
	result := Compact(messages, tokenLimit)

	if result.Strategy != "tool_trim" {
		t.Errorf("expected strategy 'tool_trim', got %q", result.Strategy)
	}
	if len(result.Messages) >= len(messages) {
		t.Errorf("expected message reduction, got %d (was %d)", len(result.Messages), len(messages))
	}
	// Checkpoint messages should have been dropped first
	for _, m := range result.Messages {
		if m.Meta != nil && m.Meta[MetaKeyCheckpoint] == "true" {
			t.Error("checkpoint messages should have been dropped before turn drop")
		}
	}
}

func TestCompact_Phase3_Truncation(t *testing.T) {
	// Build messages that can't be reduced by checkpoints or turns alone:
	// no checkpoints, 30+ pairs of large user/assistant messages.
	messages := []Message{{Role: "system", Content: "You are helpful."}}
	for i := 0; i < 30; i++ {
		messages = append(messages, Message{Role: "user", Content: strings.Repeat("a", 5000)})
		messages = append(messages, Message{Role: "assistant", Content: strings.Repeat("b", 5000)})
	}

	result := Compact(messages, 100)

	if result.Strategy != "truncation" {
		t.Errorf("expected strategy 'truncation', got %q", result.Strategy)
	}
	if len(result.Messages) >= len(messages) {
		t.Errorf("expected message reduction, got %d (was %d)", len(result.Messages), len(messages))
	}
	if result.TokensSaved() <= 0 {
		t.Error("expected positive tokens saved")
	}
}

func TestCompact_Phase3_Emergency(t *testing.T) {
	// The "emergency" path (strategy="emergency") requires that after turn drop,
	// enough messages remain for emergencyTruncate's Phase 3 to drop some.
	// This is most reliably tested via direct emergencyTruncate calls.
	//
	// Here we verify that Compact() does not return "none" for over-limit messages,
	// and that the result includes the "emergency" path when message dropping
	// actually occurs. The strategy will be the one that Compact() selects;
	// we just confirm compaction happened.
	messages := []Message{{Role: "system", Content: "You are helpful."}}
	for i := 0; i < 40; i++ {
		messages = append(messages, Message{
			Role: "user", Content: "User " + strings.Repeat("x", 200),
		})
		messages = append(messages, Message{
			Role: "assistant", Content: "Assistant " + strings.Repeat("y", 200),
		})
	}

	result := Compact(messages, 100)

	// At minimum, the algorithm should produce an active compaction strategy
	if result.Strategy == "none" {
		t.Error("expected compaction, got 'none'")
	}
	if len(result.Messages) >= len(messages) {
		t.Errorf("expected message reduction, got %d (was %d)", len(result.Messages), len(messages))
	}
}

func TestCompact_PhaseProgression_FullCascade(t *testing.T) {
	// Checkpoints → turns → truncation, all needed.
	messages := []Message{{Role: "system", Content: "You are helpful."}}
	// Large checkpoints
	for i := 0; i < 3; i++ {
		messages = append(messages, Message{
			Role:    "user",
			Content: "CP " + strings.Repeat("x", 990),
			Meta:    map[string]string{MetaKeyCheckpoint: "true"},
		})
	}
	// Large turns
	for i := 1; i <= 2; i++ {
		messages = append(messages, Message{
			Role:    "user",
			Content: "Turn " + string(rune('0'+i)) + " " + strings.Repeat("m", 990),
		})
		messages = append(messages, Message{
			Role:    "assistant",
			Content: "Turn " + string(rune('0'+i)) + " " + strings.Repeat("n", 990),
		})
	}
	// Many recent messages to fill out the conversation
	for i := 0; i < 30; i++ {
		messages = append(messages, Message{Role: "user", Content: strings.Repeat("r", 50)})
		messages = append(messages, Message{Role: "assistant", Content: strings.Repeat("s", 50)})
	}

	result := Compact(messages, 100)

	// The exact strategy depends on how far the cascade reaches.
	// It will at least drop checkpoints, possibly drops turns, then truncates.
	if result.Strategy == "none" {
		t.Error("expected some compaction to occur")
	}
	if result.Strategy != "checkpoint_drop" && result.Strategy != "tool_trim" &&
		result.Strategy != "truncation" && result.Strategy != "emergency" {
		t.Errorf("unexpected strategy: %q", result.Strategy)
	}
	if len(result.Messages) >= len(messages) {
		t.Errorf("expected message reduction, got %d (was %d)", len(result.Messages), len(messages))
	}
}

func TestCompact_TargetTokens(t *testing.T) {
	// targetTokens = int(float64(tokenLimit) * emergencyTargetFraction)
	// emergencyTargetFraction = 0.85
	// With tokenLimit=1000, the target threshold is 850 tokens.
	// Messages just over 850 tokens but under 1000 should NOT be compacted (no compaction happens
	// only when tokensBefore > tokenLimit).
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: strings.Repeat("a", 900*4)}, // ~900 tokens
		{Role: "assistant", Content: "short"},
	}
	_ = 1000 // token limit for reference
	result := Compact(messages, 1000)
	// 900 tokens > 1000? No, so no compaction.
	if result.Strategy != "none" {
		t.Errorf("expected 'none' for under-limit messages, got %q", result.Strategy)
	}
}

func TestCompact_ResultMetadata(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: strings.Repeat("a", 400)},      // ~100 tokens
		{Role: "assistant", Content: strings.Repeat("b", 400)}, // ~100 tokens
	}
	// Add recent filler
	for i := 0; i < 30; i++ {
		messages = append(messages, Message{Role: "user", Content: strings.Repeat("x", 500)})
		messages = append(messages, Message{Role: "assistant", Content: strings.Repeat("y", 500)})
	}

	tokensBefore := roughTokens(messages)
	result := Compact(messages, 100)

	if result.TokensBefore != tokensBefore {
		t.Errorf("expected TokensBefore=%d, got %d", tokensBefore, result.TokensBefore)
	}
	if result.TokensAfter > result.TokensBefore {
		t.Error("TokensAfter should never exceed TokensBefore")
	}
	if result.TokensSaved() != result.TokensBefore-result.TokensAfter {
		t.Error("TokensSaved should be TokensBefore - TokensAfter")
	}
	if result.MessageCountDelta(len(messages)) != len(messages)-len(result.Messages) {
		t.Error("MessageCountDelta should be correct")
	}
}

// ==============================================================================
// 2. Protected boundary (recentToKeep = 24)
// ==============================================================================

func TestProtectedBoundary_RecentMessagesPreserved(t *testing.T) {
	messages := []Message{{Role: "system", Content: "You are helpful."}}
	// Add old turns (to be dropped)
	for i := 0; i < 3; i++ {
		messages = append(messages, Message{Role: "user", Content: strings.Repeat("x", 990)})
		messages = append(messages, Message{Role: "assistant", Content: strings.Repeat("y", 990)})
	}
	// Add recent message pairs (must be preserved)
	var recentMsgs []Message
	for i := 0; i < 20; i++ {
		content := "recent " + string(rune('a'+i))
		messages = append(messages, Message{Role: "user", Content: content})
		messages = append(messages, Message{Role: "assistant", Content: "resp " + content})
		recentMsgs = append(recentMsgs, Message{Role: "user", Content: content})
		recentMsgs = append(recentMsgs, Message{Role: "assistant", Content: "resp " + content})
	}

	result := Compact(messages, 100)

	// Last defaultRecentToKeep messages of the result should match the tail of recentMsgs
	recentTail := messages[len(messages)-defaultRecentToKeep:]
	resultTail := result.Messages[len(result.Messages)-len(recentTail):]
	if len(resultTail) != len(recentTail) {
		t.Errorf("recent boundary mismatch: expected %d messages, got %d", len(recentTail), len(resultTail))
		return
	}
	for i := range resultTail {
		if resultTail[i].Content != recentTail[i].Content {
			t.Errorf("recent message %d modified: expected %q, got %q", i, recentTail[i].Content, resultTail[i].Content)
		}
	}
}

func TestProtectedBoundary_SystemMessageAlwaysPreserved(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Query"},
		{Role: "assistant", Content: "Answer"},
	}
	for i := 0; i < 30; i++ {
		messages = append(messages, Message{Role: "user", Content: strings.Repeat("x", 500)})
		messages = append(messages, Message{Role: "assistant", Content: strings.Repeat("y", 500)})
	}

	result := Compact(messages, 100)
	if len(result.Messages) == 0 {
		t.Fatal("result should not be empty")
	}
	if result.Messages[0].Role != "system" {
		t.Error("system message should always be first")
	}
	if result.Messages[0].Content != "You are helpful." {
		t.Error("system message content should be preserved")
	}
}

// ==============================================================================
// 3. dropOldestCheckpointSummaries
// ==============================================================================

func TestDropCheckpoints_DropsOldestFirst(t *testing.T) {
	// Build messages: system + 3 checkpoint summaries + filler messages
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Summary 1", Meta: map[string]string{MetaKeyCheckpoint: "true"}},
		{Role: "user", Content: "Summary 2", Meta: map[string]string{MetaKeyCheckpoint: "true"}},
		{Role: "user", Content: "Summary 3", Meta: map[string]string{MetaKeyCheckpoint: "true"}},
	}
	for i := 0; i < 30; i++ {
		messages = append(messages, Message{Role: "user", Content: strings.Repeat("x", 50)})
		messages = append(messages, Message{Role: "assistant", Content: strings.Repeat("y", 50)})
	}

	tokensBefore := roughTokens(messages)
	// Each checkpoint is ~12 tokens (9 chars/4 + 10 overhead).
	// Set target high enough that 2 checkpoints are dropped but 1 remains.
	// After 2 drops: tokensBefore - 24. Set target just above that.
	targetTokens := tokensBefore - 24

	result := dropOldestCheckpointSummaries(messages, 0, nil, targetTokens)

	// At least 2 checkpoints should be dropped (the oldest ones)
	dropped := len(messages) - len(result)
	if dropped < 2 {
		t.Errorf("expected at least 2 checkpoints dropped, got %d", dropped)
	}
	// Oldest checkpoint messages should be absent from the result
	for _, m := range result {
		if m.Content == "Summary 1" || m.Content == "Summary 2" {
			t.Errorf("oldest checkpoint %q should have been dropped", m.Content)
		}
	}
}

func TestDropCheckpoints_RespectsRecentBoundary(t *testing.T) {
	messages := []Message{{Role: "system", Content: "You are helpful."}}
	for i := 0; i < 20; i++ {
		messages = append(messages, Message{Role: "user", Content: strings.Repeat("x", 50)})
		messages = append(messages, Message{Role: "assistant", Content: strings.Repeat("y", 50)})
	}
	// This checkpoint is within the recent zone (last 24 positions)
	messages = append(messages, Message{
		Role:    "user",
		Content: "Recent checkpoint",
		Meta:    map[string]string{MetaKeyCheckpoint: "true"},
	})

	// 42 total messages, recentStart = 42 - 24 = 18
	// Checkpoint at index 41, which is >= 18, so it's in the recent zone
	// Even with targetTokens=0, it should not be dropped
	result := DropOldestCheckpointSummaries(messages, 0)

	// The checkpoint must still be present (recent boundary protects it)
	found := false
	for _, m := range result {
		if m.Content == "Recent checkpoint" {
			found = true
			break
		}
	}
	if !found {
		t.Error("checkpoint within recent zone should not be dropped")
	}
}

func TestDropCheckpoints_NoCheckpointsPresent(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
	}
	for i := 0; i < 10; i++ {
		messages = append(messages, Message{Role: "user", Content: strings.Repeat("x", 100)})
		messages = append(messages, Message{Role: "assistant", Content: strings.Repeat("y", 100)})
	}

	result := dropOldestCheckpointSummaries(messages, 0, nil, 0)

	if len(result) != len(messages) {
		t.Errorf("expected no messages dropped, got %d (was %d)", len(result), len(messages))
	}
}

func TestDropCheckpoints_StopsAtTarget(t *testing.T) {
	messages := []Message{{Role: "system", Content: "You are helpful."}}
	// 4 large checkpoint summaries (~250 tokens each)
	for i := 0; i < 4; i++ {
		messages = append(messages, Message{
			Role:    "user",
			Content: "CP " + strings.Repeat("x", 990),
			Meta:    map[string]string{MetaKeyCheckpoint: "true"},
		})
	}
	for i := 0; i < 30; i++ {
		messages = append(messages, Message{Role: "user", Content: strings.Repeat("a", 50)})
		messages = append(messages, Message{Role: "assistant", Content: strings.Repeat("b", 50)})
	}

	tokensBefore := roughTokens(messages)
	targetTokens := tokensBefore - 500 // enough to drop ~2 checkpoints

	result := dropOldestCheckpointSummaries(messages, 0, nil, targetTokens)

	dropped := len(messages) - len(result)
	if dropped < 2 {
		t.Errorf("expected at least 2 checkpoints dropped, got %d", dropped)
	}
}

func TestDropCheckpoints_DoesNotMutateOriginal(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Checkpoint", Meta: map[string]string{MetaKeyCheckpoint: "true"}},
	}
	for i := 0; i < 30; i++ {
		messages = append(messages, Message{Role: "user", Content: strings.Repeat("x", 100)})
		messages = append(messages, Message{Role: "assistant", Content: strings.Repeat("y", 100)})
	}

	origLen := len(messages)
	origContent := messages[1].Content
	_ = dropOldestCheckpointSummaries(messages, 0, nil, 0)

	if len(messages) != origLen {
		t.Errorf("original slice mutated: was %d, now %d", origLen, len(messages))
	}
	if messages[1].Content != origContent {
		t.Error("original message content modified")
	}
}

func TestDropCheckpoints_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		messages []Message
	}{
		{
			name:     "nil input",
			messages: nil,
		},
		{
			name:     "empty slice",
			messages: []Message{},
		},
		{
			name: "few messages",
			messages: []Message{
				{Role: "user", Content: "CP", Meta: map[string]string{MetaKeyCheckpoint: "true"}},
				{Role: "assistant", Content: "Response"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dropOldestCheckpointSummaries(tt.messages, 0, nil, 0)
			if result == nil {
				t.Error("expected non-nil result")
			}
			if len(result) == 0 {
				return // nil/empty input → empty result is acceptable
			}
			// Few messages: should return unchanged
			if len(result) != len(tt.messages) {
				t.Errorf("expected no messages dropped for %d messages, got %d", len(tt.messages), len(result))
			}
		})
	}
}

// ==============================================================================
// 4. truncateOldContentHeadTail & emergencyTruncate
// ==============================================================================

func TestTruncateOldContentHeadTail_PreservesHeadAndTail(t *testing.T) {
	head := strings.Repeat("H", oldMsgHeadChars)
	tail := strings.Repeat("T", oldMsgTailChars)
	mid := strings.Repeat("M", 500)
	longContent := head + mid + tail

	result := truncateOldContentHeadTail(longContent)

	if !strings.Contains(result, "[truncated]") {
		t.Error("expected truncation marker in result")
	}
	if !strings.HasPrefix(result, head) {
		t.Error("expected head preserved")
	}
	if !strings.HasSuffix(result, tail) {
		t.Error("expected tail preserved")
	}
	markerLen := utf8.RuneCountInString("\n... [truncated] ...\n")
	expectedLen := oldMsgHeadChars + markerLen + oldMsgTailChars
	if utf8.RuneCountInString(result) != expectedLen {
		t.Errorf("expected total length %d, got %d", expectedLen, utf8.RuneCountInString(result))
	}
	if strings.Contains(result, "MMMM") {
		t.Error("middle content should be truncated")
	}
}

func TestTruncateOldContentHeadTail_ShortContentNoOp(t *testing.T) {
	// Short content
	short := strings.Repeat("S", 900)
	if truncateOldContentHeadTail(short) != short {
		t.Error("short content should be unchanged")
	}

	// Exactly at boundary
	boundary := strings.Repeat("B", oldMsgHeadChars+oldMsgTailChars)
	if truncateOldContentHeadTail(boundary) != boundary {
		t.Error("boundary content should be unchanged")
	}
}

func TestTruncateHeadTail_Function(t *testing.T) {
	s := strings.Repeat("a", 100)
	result := truncateHeadTail(s, 10, 10)

	if !strings.Contains(result, "[truncated]") {
		t.Error("expected truncation marker")
	}
	if !strings.HasPrefix(result, "aaaaaaaaaa") {
		t.Error("expected head preserved")
	}
	if !strings.HasSuffix(result, "aaaaaaaaaa") {
		t.Error("expected tail preserved")
	}

	// Short input: no truncation
	short := strings.Repeat("x", 15)
	result2 := truncateHeadTail(short, 10, 10)
	if result2 != short {
		t.Error("short input should not be truncated")
	}
}

func TestEmergencyTruncate_Phase1_ToolResultTrim(t *testing.T) {
	// Scenario where Phase 1 trimming of tool result is sufficient to reach target.
	// target = 0.85 * tokenLimit.
	// Set up: 20 recent pairs (~180 tokens) + small system/tool/assistant messages
	// + a huge tool result that, when trimmed to 1500 chars, brings us under 850 tokens.
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hi"},
		{Role: "assistant", Content: "Calling", ToolCalls: []ToolCall{
			{ID: "tc1", Type: "function", Function: ToolCallFunction{Name: "search", Arguments: "{}"}},
		}},
		{Role: "tool", Content: strings.Repeat("x", 10000), ToolCallID: "tc1"},
		{Role: "assistant", Content: "Done"},
	}
	// Only 5 recent pairs to keep total manageable
	for i := 0; i < 5; i++ {
		messages = append(messages, Message{Role: "user", Content: "recent " + string(rune('a'+i))})
		messages = append(messages, Message{Role: "assistant", Content: "recent " + string(rune('a'+i))})
	}

	result, _ := emergencyTruncate(messages, 1000)

	// Phase 1 always runs; verify the tool result was trimmed
	toolResultContent := ""
	for _, m := range result {
		if m.Role == "tool" {
			toolResultContent = m.Content
			break
		}
	}
	if toolResultContent == "" {
		t.Fatal("expected a tool message in result")
	}
	if len(toolResultContent) > toolResultMaxChars {
		t.Errorf("tool result should be trimmed to at most %d chars, got %d", toolResultMaxChars, len(toolResultContent))
	}
	if !strings.Contains(toolResultContent, "[truncated]") {
		t.Error("tool result should contain truncation marker")
	}
}

func TestEmergencyTruncate_Phase2_OldMessageTruncation(t *testing.T) {
	// Build messages where truncating old messages (Phase 2) is sufficient.
	// Each old user/assistant message is 1400 chars (> oldMsgMaxChars=1200).
	// After truncation to head+tail: 1022 chars, saving ~95 tokens per message.
	//
	// Need enough messages that old messages are outside the recentToKeep zone
	// so emergencyTruncate's Phase 2 actually targets them.
	messages := []Message{{Role: "system", Content: "You are helpful."}}
	// 15 old pairs (30 messages at indices 1-30)
	for i := 0; i < 15; i++ {
		messages = append(messages, Message{
			Role: "user", Content: "User " + strings.Repeat("O", oldMsgMaxChars+200),
		})
		messages = append(messages, Message{
			Role: "assistant", Content: "Assistant " + strings.Repeat("A", oldMsgMaxChars+200),
		})
	}
	// 25 recent pairs to push recentStart past the old messages
	for i := 0; i < 25; i++ {
		messages = append(messages, Message{Role: "user", Content: "recent u " + string(rune('a'+i))})
		messages = append(messages, Message{Role: "assistant", Content: "recent a " + string(rune('a'+i))})
	}

	// 51 messages total → recentStart = 51 - 24 = 27.
	// Old messages at indices 1-30 are outside the recent zone (i < 27).
	//
	// Token math:
	//   30 old messages * ~361 tokens ≈ 10830
	//   50 recent messages * ~12 tokens ≈ 600
	//   system ≈ 10
	//   total ≈ 11440
	// After Phase 2: 30 * ~265 + 610 ≈ 8560
	// tokenLimit=10000 → targetTokens=8500. 8560 > 8500, so Phase 3 drops some messages.
	// That's fine — Phase 2 still truncated the old messages first.
	result, _ := emergencyTruncate(messages, 10000)

	// Old messages should be head+tail truncated. Check old message indices (1-30).
	markerLen := utf8.RuneCountInString("\n... [truncated] ...\n")
	expectedLen := oldMsgHeadChars + markerLen + oldMsgTailChars

	truncatedCount := 0
	for i := 1; i < 31 && i < len(result); i++ {
		if result[i].Role == "system" || result[i].Content == "" {
			continue
		}
		if strings.Contains(result[i].Content, "[truncated]") {
			truncatedCount++
			contentLen := utf8.RuneCountInString(result[i].Content)
			if contentLen != expectedLen {
				t.Errorf("message %d: expected length %d, got %d", i, expectedLen, contentLen)
			}
		}
	}
	if truncatedCount == 0 {
		t.Error("expected at least one truncated old message, got none")
	}
}

func TestEmergencyTruncate_Phase3_MessageDropping(t *testing.T) {
	// 40 pairs of moderate messages (under oldMsgMaxChars so Phase 2 can't help).
	// Very low tokenLimit forces Phase 3 message dropping.
	messages := []Message{{Role: "system", Content: "You are helpful."}}
	for i := 0; i < 40; i++ {
		messages = append(messages, Message{Role: "user", Content: strings.Repeat("x", 500)})
		messages = append(messages, Message{Role: "assistant", Content: strings.Repeat("y", 500)})
	}

	result, msgsDropped := emergencyTruncate(messages, 100)

	if !msgsDropped {
		t.Error("expected msgsDropped=true")
	}
	if len(result) >= len(messages) {
		t.Errorf("expected message reduction, got %d (was %d)", len(result), len(messages))
	}
	if result[0].Role != "system" {
		t.Error("system message should be preserved")
	}
}

func TestEmergencyTruncate_Phase3_NeverSplitsToolPairs(t *testing.T) {
	// The oldest non-anchor messages outside the recent zone are a tool pair:
	// an assistant carrying tool calls plus its tool result. Phase 3 never
	// splits them — it drops the plain filler behind them first, and takes the
	// pair as a unit only once nothing else is left. recentStart is 30, so
	// indices 2 and 3 are the first candidates.
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "calling", ToolCalls: []ToolCall{
			{ID: "tc1", Type: "function", Function: ToolCallFunction{Name: "search", Arguments: "{}"}},
		}},
		{Role: "tool", Content: strings.Repeat("x", 3000), ToolCallID: "tc1"},
	}
	// 25 recent pairs push the tool pair outside the recent zone.
	for i := 0; i < 25; i++ {
		messages = append(messages, Message{Role: "user", Content: "recent u " + string(rune('a'+i))})
		messages = append(messages, Message{Role: "assistant", Content: "recent a " + string(rune('a'+i))})
	}

	result, msgsDropped := emergencyTruncate(messages, 100)

	if !msgsDropped {
		t.Fatal("expected some messages dropped: plain filler outside the recent zone is droppable")
	}
	// The invariant is that the pair is never SPLIT: dropping the call while
	// keeping its result (or the reverse) produces a request strict providers
	// reject. Dropping both is legitimate once the loop has consumed
	// everything else outside the recent zone.
	var hasCall, hasResult bool
	for _, m := range result {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			hasCall = true
		}
		if m.Role == "tool" && m.ToolCallID == "tc1" {
			hasResult = true
		}
	}
	if hasCall != hasResult {
		t.Errorf("tool pair split: call present=%v, result present=%v", hasCall, hasResult)
	}
	// Filler before the surviving recent zone must have been dropped, proving
	// the loop made progress by skipping the pair rather than stopping early.
	if len(result) >= len(messages) {
		t.Errorf("expected message reduction, got %d (was %d)", len(result), len(messages))
	}
}

func TestEmergencyTruncate_RecentMessagesNotTruncated(t *testing.T) {
	// Only recent messages (no old messages to truncate, no tool results).
	// Emergency truncation should only touch old messages (Phase 2), not recent ones.
	// Phase 3 drops from the front, but recent messages are at the end.
	messages := []Message{{Role: "system", Content: "You are helpful."}}
	var recentContents []string
	for i := 0; i < 30; i++ {
		uc := "recent user " + string(rune('a'+i))
		ac := "recent assistant " + string(rune('a'+i))
		messages = append(messages, Message{Role: "user", Content: uc})
		messages = append(messages, Message{Role: "assistant", Content: ac})
		recentContents = append(recentContents, uc, ac)
	}

	result, _ := emergencyTruncate(messages, 100)

	// Verify the most recent defaultRecentToKeep messages are intact.
	// The function preserves the last recentToKeep messages at the end.
	preservedLen := defaultRecentToKeep
	if preservedLen > len(result) {
		preservedLen = len(result) - 1 // leave room for system message
	}
	// Last N-1 messages should match the original tail
	originalTail := messages[len(messages)-preservedLen:]
	resultTail := result[len(result)-preservedLen:]
	if len(resultTail) != len(originalTail) {
		t.Errorf("recent tail length mismatch: expected %d, got %d", len(originalTail), len(resultTail))
		return
	}
	for i := range originalTail {
		if resultTail[i].Content != originalTail[i].Content {
			t.Errorf("recent message %d modified: expected %q, got %q", i, originalTail[i].Content, resultTail[i].Content)
		}
	}
}

// ==============================================================================
// 5. dropOldestTurns with fallback
// ==============================================================================

func TestDropTurns_DropsCompleteTurns(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Turn1 User " + strings.Repeat("x", 500)},
		{Role: "assistant", Content: "Turn1 Assistant " + strings.Repeat("x", 500)},
		{Role: "user", Content: "Turn2 User " + strings.Repeat("x", 500)},
		{Role: "assistant", Content: "Turn2 Assistant " + strings.Repeat("x", 500)},
		{Role: "user", Content: "Turn3 User " + strings.Repeat("x", 500)},
		{Role: "assistant", Content: "Turn3 Assistant " + strings.Repeat("x", 500)},
	}
	for i := 0; i < 20; i++ {
		messages = append(messages, Message{Role: "user", Content: "recent " + string(rune('a'+i))})
		messages = append(messages, Message{Role: "assistant", Content: "recent " + string(rune('b'+i))})
	}

	targetTokens := roughTokens(messages) - roughTokens(messages[3:5]) // Turn2's own size
	result := dropOldestTurns(messages, 0, nil, targetTokens)

	if len(result) >= len(messages)-1 {
		t.Errorf("expected at least 1 turn dropped, got %d (was %d)", len(result), len(messages))
	}
	// The opening turn is the task anchor: it stays verbatim however hard the
	// pressure, because dropping it is what makes a long session forget what
	// it was asked to do.
	for _, m := range result {
		if strings.HasPrefix(m.Content, "Turn2 User ") || strings.HasPrefix(m.Content, "Turn2 Assistant ") {
			t.Errorf("Turn2 should be entirely dropped, found: %q", m.Content)
		}
	}
	var keptTurn1 bool
	for _, m := range result {
		if strings.HasPrefix(m.Content, "Turn1 User ") {
			keptTurn1 = true
		}
	}
	if !keptTurn1 {
		t.Error("Turn1 is the task anchor and must survive the drop pass")
	}
}

func TestDropTurns_WithToolChains(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		// Turn 1 with tool chain
		{Role: "user", Content: "tool_user_question"},
		{Role: "assistant", Content: "tool_call", ToolCalls: []ToolCall{
			{ID: "tc1", Type: "function", Function: ToolCallFunction{Name: "search", Arguments: "{}"}},
		}},
		{Role: "tool", Content: "tool_result", ToolCallID: "tc1"},
		{Role: "assistant", Content: "tool_final"},
		// Turn 2 simple
		{Role: "user", Content: "turn2_question"},
		{Role: "assistant", Content: "turn2_answer"},
	}
	for i := 0; i < 20; i++ {
		messages = append(messages, Message{Role: "user", Content: "recent " + string(rune('a'+i))})
		messages = append(messages, Message{Role: "assistant", Content: "recent " + string(rune('b'+i))})
	}

	targetTokens := roughTokens(messages) - roughTokens(messages[5:7]) // turn2's own size
	result := dropOldestTurns(messages, 0, nil, targetTokens)

	// The anchor is the opening user message; its tool chain stays attached
	// to it, so a protected anchor can never orphan a tool result.
	for _, m := range result {
		if m.Content == "turn2_question" || m.Content == "turn2_answer" {
			t.Errorf("turn message should be dropped: %q", m.Content)
		}
	}
	for _, want := range []string{"tool_user_question", "tool_result", "tool_final"} {
		var found bool
		for _, m := range result {
			if m.Content == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q belongs to the task anchor and must survive", want)
		}
	}
}

func TestDropTurns_RespectsRecentBoundary(t *testing.T) {
	messages := []Message{{Role: "system", Content: "You are helpful."}}
	// 3 turns before recent zone
	for i := 1; i <= 3; i++ {
		messages = append(messages, Message{Role: "user", Content: "Turn" + string(rune('0'+i)) + "_User"})
		messages = append(messages, Message{Role: "assistant", Content: "Turn" + string(rune('0'+i)) + "_Assistant"})
	}
	var recentContents []string
	for i := 0; i < 20; i++ {
		uc := "Recent User " + string(rune('a'+i))
		ac := "Recent Assistant " + string(rune('a'+i))
		messages = append(messages, Message{Role: "user", Content: uc})
		messages = append(messages, Message{Role: "assistant", Content: ac})
		recentContents = append(recentContents, uc, ac)
	}

	totalMsgs := len(messages)
	result := dropOldestTurns(messages, 0, nil, 0)

	if len(result) >= totalMsgs-1 {
		t.Errorf("expected at least 1 turn dropped, got %d (was %d)", len(result), totalMsgs)
	}
	// The last defaultRecentToKeep messages should be preserved intact.
	// As turns are dropped, the recent zone shifts, so not all 40 recent
	// messages survive — only the final recentToKeep positions.
	originalTail := messages[len(messages)-defaultRecentToKeep:]
	resultTail := result[len(result)-defaultRecentToKeep:]
	if len(resultTail) < len(originalTail) {
		t.Errorf("not enough messages preserved: expected %d, got %d", len(originalTail), len(resultTail))
		return
	}
	for i := range originalTail {
		if resultTail[i].Content != originalTail[i].Content {
			t.Errorf("tail message %d modified: expected %q, got %q",
				i, originalTail[i].Content, resultTail[i].Content)
		}
	}
}

func TestDropTurns_BoundarySpanningTurnProtected(t *testing.T) {
	// A turn starting at index 21 but extending to index 25, when recentStart = 23.
	messages := []Message{{Role: "system", Content: "You are helpful."}}
	// 10 fill pairs → 20 messages (indices 1-20)
	for i := 0; i < 10; i++ {
		messages = append(messages, Message{Role: "user", Content: "fill u " + string(rune('a'+i))})
		messages = append(messages, Message{Role: "assistant", Content: "fill a " + string(rune('a'+i))})
	}
	// Turn with tool chain: user(21), assistant(22), tool(23), assistant(24)
	messages = append(messages, Message{Role: "user", Content: "boundary_user"})
	messages = append(messages, Message{Role: "assistant", Content: "boundary_assistant", ToolCalls: []ToolCall{
		{ID: "tc1", Type: "function", Function: ToolCallFunction{Name: "search", Arguments: "{}"}},
	}})
	messages = append(messages, Message{Role: "tool", Content: "boundary_tool_result", ToolCallID: "tc1"})
	messages = append(messages, Message{Role: "assistant", Content: "boundary_final"})
	// 11 more pairs to reach 47 messages; recentStart = 47 - 24 = 23
	for i := 0; i < 11; i++ {
		messages = append(messages, Message{Role: "user", Content: "recent u " + string(rune('A'+i))})
		messages = append(messages, Message{Role: "assistant", Content: "recent a " + string(rune('A'+i))})
	}

	result := dropOldestTurns(messages, 0, nil, 0)

	boundaryPresent := 0
	for _, m := range result {
		switch m.Content {
		case "boundary_user", "boundary_assistant", "boundary_tool_result", "boundary_final":
			boundaryPresent++
		}
	}
	if boundaryPresent != 4 {
		t.Errorf("boundary-spanning turn partially dropped: found %d of 4 messages", boundaryPresent)
	}
}

func TestDropTurns_FallbackToIndividualDrop(t *testing.T) {
	// Only assistant messages before recent zone → no complete turns → fallback
	messages := []Message{{Role: "system", Content: "You are helpful."}}
	for i := 1; i <= 30; i++ {
		messages = append(messages, Message{Role: "assistant", Content: "NoTurn A" + string(rune('a'+i))})
	}

	tokensBefore := roughTokens(messages)
	targetTokens := tokensBefore - 50

	result := dropOldestTurns(messages, 0, nil, targetTokens)

	droppedCount := len(messages) - len(result)
	if droppedCount < 1 {
		t.Errorf("expected at least 1 message dropped via fallback, got %d", droppedCount)
	}
	for _, m := range result {
		if strings.HasPrefix(m.Content, "NoTurn Aa") {
			t.Errorf("first assistant message should have been dropped: %q", m.Content)
		}
	}
}

func TestDropTurns_DoesNotMutateOriginal(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Turn User " + strings.Repeat("x", 500)},
		{Role: "assistant", Content: "Turn Assistant " + strings.Repeat("x", 500)},
	}
	for i := 0; i < 20; i++ {
		messages = append(messages, Message{Role: "user", Content: "recent " + string(rune('a'+i))})
		messages = append(messages, Message{Role: "assistant", Content: "recent " + string(rune('b'+i))})
	}

	origLen := len(messages)
	origContent := messages[1].Content
	_ = dropOldestTurns(messages, 0, nil, 0)

	if len(messages) != origLen {
		t.Errorf("original slice mutated: was %d, now %d", origLen, len(messages))
	}
	if messages[1].Content != origContent {
		t.Error("original message content was modified")
	}
}

func TestDropTurns_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		messages []Message
	}{
		{
			name:     "nil input",
			messages: nil,
		},
		{
			name:     "empty slice",
			messages: []Message{},
		},
		{
			name: "few messages",
			messages: []Message{
				{Role: "user", Content: "u1"},
				{Role: "assistant", Content: "a1"},
			},
		},
		{
			name: "under target",
			messages: []Message{
				{Role: "system", Content: "You are helpful."},
				{Role: "user", Content: "Hi"},
				{Role: "assistant", Content: "Hello"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := 10000
			if tt.name == "few messages" {
				target = 0
			}
			result := dropOldestTurns(tt.messages, 0, nil, target)
			if result == nil {
				if tt.messages == nil {
					// nil input → may return nil or empty
					return
				}
				t.Error("expected non-nil result")
				return
			}
			if len(tt.messages) > 0 && len(tt.messages) <= defaultMinMessages {
				// Few messages: should return unchanged
				if len(result) != len(tt.messages) {
					t.Errorf("expected no messages dropped for %d messages, got %d", len(tt.messages), len(result))
				}
			}
		})
	}
}

// ==============================================================================
// 6. Helper functions: identifyTurnEnd, findOldestCompleteTurn, findDropableMessage
// ==============================================================================

func TestIdentifyTurnEnd(t *testing.T) {
	tests := []struct {
		name     string
		msgs     []Message
		startIdx int
		wantEnd  int
	}{
		{
			name: "simple turn: user→assistant→next user",
			msgs: []Message{
				{Role: "system", Content: "sys"},
				{Role: "user", Content: "u1"},
				{Role: "assistant", Content: "a1"},
				{Role: "user", Content: "u2"},
			},
			startIdx: 1,
			wantEnd:  3,
		},
		{
			name: "turn with tool calls",
			msgs: []Message{
				{Role: "system", Content: "sys"},
				{Role: "user", Content: "u1"},
				{Role: "assistant", Content: "a1", ToolCalls: []ToolCall{
					{ID: "tc1", Type: "function", Function: ToolCallFunction{Name: "search", Arguments: "{}"}},
				}},
				{Role: "tool", Content: "result", ToolCallID: "tc1"},
				{Role: "assistant", Content: "a2"},
				{Role: "user", Content: "u2"},
			},
			startIdx: 1,
			wantEnd:  5,
		},
		{
			name: "turn at end of slice",
			msgs: []Message{
				{Role: "system", Content: "sys"},
				{Role: "user", Content: "u1"},
				{Role: "assistant", Content: "a1"},
			},
			startIdx: 1,
			wantEnd:  3,
		},
		{
			name: "consecutive tool results",
			msgs: []Message{
				{Role: "system", Content: "sys"},
				{Role: "user", Content: "u1"},
				{Role: "assistant", Content: "a1"},
				{Role: "tool", Content: "t1", ToolCallID: "tc1"},
				{Role: "tool", Content: "t2", ToolCallID: "tc2"},
				{Role: "assistant", Content: "a2"},
				{Role: "user", Content: "u2"},
			},
			startIdx: 1,
			wantEnd:  6,
		},
		{
			name: "nested tool chain",
			msgs: []Message{
				{Role: "system", Content: "sys"},
				{Role: "user", Content: "u1"},
				{Role: "assistant", Content: "a1"},
				{Role: "tool", Content: "t1", ToolCallID: "tc1"},
				{Role: "assistant", Content: "a2"},
				{Role: "tool", Content: "t2", ToolCallID: "tc2"},
				{Role: "assistant", Content: "a3"},
				{Role: "user", Content: "u2"},
			},
			startIdx: 1,
			wantEnd:  7,
		},
		{
			name:     "out of range startIdx",
			msgs:     []Message{{Role: "system", Content: "sys"}},
			startIdx: 5,
			wantEnd:  5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			end := identifyTurnEnd(tt.msgs, tt.startIdx)
			if end != tt.wantEnd {
				t.Errorf("expected turn end at %d, got %d", tt.wantEnd, end)
			}
		})
	}
}

func TestFindOldestCompleteTurn(t *testing.T) {
	tests := []struct {
		name        string
		msgs        []Message
		recentStart int
		// anchorStart/anchorEnd bound the opening task anchor. The zero
		// value protects nothing, so cases that do not exercise the anchor
		// keep their original expectations.
		anchorStart  int
		anchorEnd    int
		wantStart    int
		wantEnd      int
		wantSpanning int
	}{
		{
			name: "no user messages",
			msgs: []Message{
				{Role: "system", Content: "sys"},
				{Role: "assistant", Content: "a1"},
				{Role: "assistant", Content: "a2"},
			},
			recentStart:  3,
			wantStart:    -1,
			wantEnd:      -1,
			wantSpanning: 0,
		},
		{
			name: "simple turn",
			msgs: []Message{
				{Role: "system", Content: "sys"},
				{Role: "user", Content: "u1"},
				{Role: "assistant", Content: "a1"},
				{Role: "user", Content: "u2"},
				{Role: "assistant", Content: "a2"},
			},
			recentStart:  5,
			wantStart:    1,
			wantEnd:      3,
			wantSpanning: 0,
		},
		{
			name: "tool chain turn",
			msgs: []Message{
				{Role: "system", Content: "sys"},
				{Role: "user", Content: "u1"},
				{Role: "assistant", Content: "a1", ToolCalls: []ToolCall{
					{ID: "tc1", Type: "function", Function: ToolCallFunction{Name: "search", Arguments: "{}"}},
				}},
				{Role: "tool", Content: "result", ToolCallID: "tc1"},
				{Role: "assistant", Content: "a2"},
				{Role: "user", Content: "u2"},
				{Role: "assistant", Content: "a3"},
			},
			recentStart:  7,
			wantStart:    1,
			wantEnd:      5,
			wantSpanning: 0,
		},
		{
			name: "boundary spanning",
			msgs: []Message{
				{Role: "system", Content: "sys"},
				{Role: "user", Content: "u1"},
				{Role: "assistant", Content: "a1"},
				{Role: "tool", Content: "t1", ToolCallID: "tc1"},
				{Role: "assistant", Content: "a2"},
				{Role: "user", Content: "u2"},
			},
			recentStart:  3,
			wantStart:    -1,
			wantEnd:      -1,
			wantSpanning: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, spanning := findOldestCompleteTurn(tt.msgs, tt.recentStart, tt.anchorStart, tt.anchorEnd)
			if start != tt.wantStart || end != tt.wantEnd || spanning != tt.wantSpanning {
				t.Errorf("expected (%d,%d,%d), got (%d,%d,%d)",
					tt.wantStart, tt.wantEnd, tt.wantSpanning, start, end, spanning)
			}
		})
	}
}

func TestFindDropableMessage(t *testing.T) {
	tests := []struct {
		name         string
		msgs         []Message
		recentStart  int
		protectedEnd int
		// anchorStart/anchorEnd bound the opening task anchor. The zero
		// value protects nothing.
		anchorStart int
		anchorEnd   int
		wantIdx     int
	}{
		{
			name: "skips system messages",
			msgs: []Message{
				{Role: "system", Content: "sys"},
				{Role: "system", Content: "sys2"},
				{Role: "user", Content: "u1"},
				{Role: "assistant", Content: "a1"},
			},
			recentStart:  4,
			protectedEnd: 0,
			wantIdx:      2,
		},
		{
			name: "returns first non-system",
			msgs: []Message{
				{Role: "system", Content: "sys"},
				{Role: "assistant", Content: "a1"},
				{Role: "user", Content: "u1"},
			},
			recentStart:  3,
			protectedEnd: 0,
			wantIdx:      1,
		},
		{
			name: "all system messages",
			msgs: []Message{
				{Role: "system", Content: "sys"},
				{Role: "system", Content: "sys2"},
				{Role: "system", Content: "sys3"},
			},
			recentStart:  4,
			protectedEnd: 0,
			wantIdx:      -1,
		},
		{
			name: "all messages in recent zone",
			msgs: []Message{
				{Role: "system", Content: "sys"},
				{Role: "user", Content: "u1"},
			},
			recentStart:  1,
			protectedEnd: 0,
			wantIdx:      -1,
		},
		{
			name: "protects boundary-spanning turn",
			msgs: []Message{
				{Role: "system", Content: "sys"},
				{Role: "user", Content: "u1"},
				{Role: "assistant", Content: "a1"},
				{Role: "tool", Content: "t1"},
				{Role: "assistant", Content: "a2"},
				{Role: "user", Content: "u2"},
			},
			recentStart:  3,
			protectedEnd: 5,
			wantIdx:      -1,
		},
		{
			name: "skips leading tool messages",
			msgs: []Message{
				{Role: "system", Content: "sys"},
				{Role: "tool", Content: "t1", ToolCallID: "tc1"},
				{Role: "tool", Content: "t2", ToolCallID: "tc2"},
				{Role: "user", Content: "u1"},
				{Role: "assistant", Content: "a1"},
			},
			recentStart:  5,
			protectedEnd: 0,
			wantIdx:      3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := findDropableMessage(tt.msgs, tt.recentStart, tt.protectedEnd, tt.anchorStart, tt.anchorEnd)
			if idx != tt.wantIdx {
				t.Errorf("expected index %d, got %d", tt.wantIdx, idx)
			}
		})
	}
}

// ==============================================================================
// 7. roughTokens
// ==============================================================================

func TestRoughTokens_Basic(t *testing.T) {
	// "Hello world" = 11 chars → 11/4 = 2 tokens + 10 overhead = 12
	messages := []Message{{Role: "user", Content: "Hello world"}}
	tokens := roughTokens(messages)
	if tokens < 10 {
		t.Errorf("expected at least %d overhead tokens, got %d", msgOverhead, tokens)
	}
}

func TestRoughTokens_WithToolCalls(t *testing.T) {
	messages := []Message{{
		Role:    "assistant",
		Content: "Calling function",
		ToolCalls: []ToolCall{
			{ID: "tc1", Type: "function", Function: ToolCallFunction{Name: "search", Arguments: `{"q": "hello"}`}},
		},
	}}
	tokens := roughTokens(messages)
	// Must include tool call overhead (20) + argument tokens + content tokens + message overhead
	if tokens < msgOverhead+toolCallOverhead {
		t.Errorf("expected tool call overhead, got %d", tokens)
	}
}

func TestRoughTokens_Empty(t *testing.T) {
	if roughTokens(nil) != 0 {
		t.Error("expected 0 tokens for nil messages")
	}
	if roughTokens([]Message{}) != 0 {
		t.Error("expected 0 tokens for empty slice")
	}
}

// ==============================================================================
// recentTailStart — token-sized verbatim tail
// ==============================================================================

// uniformMessages builds n messages of equal size so a token budget maps
// predictably onto a message count.
func uniformMessages(n, chars int) []Message {
	msgs := make([]Message, n)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = Message{Role: role, Content: strings.Repeat("x", chars)}
	}
	return msgs
}

func TestRecentTailStart_ScalesWithTokenBudget(t *testing.T) {
	// 100 messages of 400 chars each: 100 content tokens + 10 overhead = 110
	// estimated tokens per message. The slice is long enough that the token
	// walk, not the historical count, decides the boundary.
	msgs := uniformMessages(100, 400)
	perMsg := roughTokens(msgs[0:1])

	small := recentTailStart(msgs, 30*perMsg, nil, 0)
	large := recentTailStart(msgs, 60*perMsg, nil, 0)

	// A small budget keeps more messages than a large one.
	if small <= large {
		t.Fatalf("small budget kept %d messages, large budget kept %d — want small > large", len(msgs)-small, len(msgs)-large)
	}
	if got := len(msgs) - small; got != 30 {
		t.Errorf("small budget (30 messages' worth) kept %d messages, want 30", got)
	}
	if got := len(msgs) - large; got != 60 {
		t.Errorf("large budget (60 messages' worth) kept %d messages, want 60", got)
	}

	// budget <= 0 reproduces the historical 24-message count exactly.
	historical := recentTailStart(msgs, 0, nil, 0)
	if got := len(msgs) - historical; got != defaultRecentToKeep {
		t.Errorf("zero budget kept %d messages, want defaultRecentToKeep = %d", got, defaultRecentToKeep)
	}
	if historical != len(msgs)-defaultRecentToKeep {
		t.Errorf("zero budget start = %d, want %d", historical, len(msgs)-defaultRecentToKeep)
	}
	negative := recentTailStart(msgs, -5, nil, 0)
	if negative != historical {
		t.Errorf("negative budget start = %d, want %d (same as zero budget)", negative, historical)
	}

	// A budget too small to cover the historical count keeps at least the
	// historical count: a budgeted pass is never more destructive than the
	// unbudgeted one it replaces.
	tiny := recentTailStart(msgs, 1, nil, 0)
	if tiny != historical {
		t.Errorf("tiny budget start = %d, want %d (historical count is the floor)", tiny, historical)
	}
	if kept := len(msgs) - tiny; kept < defaultRecentToKeep {
		t.Errorf("tiny budget kept %d messages, want at least %d", kept, defaultRecentToKeep)
	}
}

func TestRecentTailStart_RespectsFloors(t *testing.T) {
	// A single enormous newest message: priced alone it blows any budget, so
	// the token walk would start the tail past the end. The floor must keep
	// at least minRecentTailMessages messages.
	msgs := uniformMessages(50, 100)
	msgs[len(msgs)-1].Content = strings.Repeat("y", 400_000)

	start := recentTailStart(msgs, 10, nil, 0)
	if kept := len(msgs) - start; kept < minRecentTailMessages {
		t.Errorf("huge newest message left %d messages, want at least %d", kept, minRecentTailMessages)
	}
	// The historical count boundary is itself above the floor here, so it wins.
	if want := len(msgs) - defaultRecentToKeep; start != want {
		t.Errorf("start = %d, want %d (historical count floor)", start, want)
	}

	// The result never precedes anchorEnd.
	const anchorEnd = 40
	anchored := recentTailStart(msgs, 1, nil, anchorEnd)
	if anchored < anchorEnd {
		t.Errorf("start = %d, want >= anchorEnd = %d", anchored, anchorEnd)
	}

	// A budget large enough to cover the whole slice still cannot reach back
	// past the anchor.
	hugeBudget := recentTailStart(msgs, 1_000_000, nil, anchorEnd)
	if hugeBudget != anchorEnd {
		t.Errorf("whole-slice budget start = %d, want anchorEnd = %d", hugeBudget, anchorEnd)
	}

	// A caller-supplied estimator is honored over roughTokens: at 1000 tokens
	// per message a 1-token budget is exhausted by the newest message alone,
	// so the historical count is the binding floor.
	counting := func(m []Message) int { return 1000 }
	if got := recentTailStart(msgs, 1, counting, 0); got != len(msgs)-defaultRecentToKeep {
		t.Errorf("with custom estimator start = %d, want %d", got, len(msgs)-defaultRecentToKeep)
	}

	// The minRecentTailMessages floor binds when the historical count would
	// reach past it — i.e. on a slice shorter than defaultRecentToKeep.
	short := uniformMessages(6, 100)
	short[5].Content = strings.Repeat("z", 400_000)
	if got := recentTailStart(short, 1, nil, 0); got != 0 {
		t.Errorf("short slice start = %d, want 0 (whole slice retained)", got)
	}
}
