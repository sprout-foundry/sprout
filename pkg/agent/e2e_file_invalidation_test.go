package agent

import (
	"fmt"
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeToolCall is a test helper that builds an api.ToolCall with the given
// id, function name, and arguments JSON string.
func makeToolCall(id, name, args string) api.ToolCall {
	return api.ToolCall{
		ID:   id,
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: name, Arguments: args},
	}
}

// fileReadContent formats a tool-call result string for read_file.
// This matches the pattern expected by ConversationOptimizer.extractFilePath:
//
//	"Tool call result for read_file:\s*([^\s\n]+)"
//
// and ConversationOptimizer.extractFileContent (everything after the first
// newline).
func fileReadContent(filePath, content string) string {
	return fmt.Sprintf("Tool call result for read_file: %s\n%s", filePath, content)
}

// addReadFileTurn appends a user request, an assistant tool_call, and the tool
// result to messages. It returns the updated slice.
func addReadFileTurn(messages []api.Message, callID, userMsg, filePath, fileContent string) []api.Message {
	messages = append(messages, api.Message{Role: "user", Content: userMsg})
	messages = append(messages, api.Message{
		Role:      "assistant",
		Content:   "",
		ToolCalls: []api.ToolCall{makeToolCall(callID, "read_file", `{"path":"`+filePath+`"}`)},
	})
	messages = append(messages, api.Message{
		Role:       "tool",
		Content:    fileReadContent(filePath, fileContent),
		ToolCallId: callID,
	})
	return messages
}

// addFillerExchange appends a simple user→assistant exchange to messages.
func addFillerExchange(messages []api.Message, n int) []api.Message {
	messages = append(messages, api.Message{Role: "user", Content: fmt.Sprintf("step %d", n)})
	messages = append(messages, api.Message{Role: "assistant", Content: fmt.Sprintf("working on step %d", n)})
	return messages
}

// ---------------------------------------------------------------------------
// Helpers: count messages matching predicates in prepared slice
// ---------------------------------------------------------------------------

// countOptimizedFileReads counts tool results containing both [OPTIMIZED] and
// the given file path substring.
func countOptimizedFileReads(prepared []api.Message, filePath string) int {
	n := 0
	for _, msg := range prepared {
		if msg.Role == "tool" &&
			strings.Contains(msg.Content, "[OPTIMIZED]") &&
			strings.Contains(msg.Content, filePath) {
			n++
		}
	}
	return n
}

// countPreservedFileReads counts tool results that contain the read_file header
// for the given path but do NOT have the [OPTIMIZED] marker.
func countPreservedFileReads(prepared []api.Message, filePath string) int {
	n := 0
	for _, msg := range prepared {
		if msg.Role == "tool" &&
			strings.Contains(msg.Content, fmt.Sprintf("Tool call result for read_file: %s", filePath)) &&
			!strings.Contains(msg.Content, "[OPTIMIZED]") {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Test 1: After edit + invalidation, different content prevents false redundancy
// ---------------------------------------------------------------------------

// TestE2E_FileInvalidationPreventsStaleRedundancy exercises the full
// prepareMessages pipeline in a multi-turn scenario that mirrors how the
// real agent uses InvalidateFile:
//
//  1. Turn 1: read file (content v1) → prepareMessages → optimizer tracks v1
//  2. Between turns: edit_file succeeds → InvalidateFile clears cached v1
//  3. Turn 2: read same file (content v2) → prepareMessages → optimizer
//     re-tracks from scratch with v1 (old) and v2 (new)
//
// After step 3, because OptimizeConversation re-tracks all reads from the
// message list, the two reads end up with different content hashes (v1 ≠ v2),
// so isRedundantFileRead returns false for the older read — both snapshots
// are preserved.  The model needs to see both the before and after content
// to understand what the edit changed.
//
// InvalidateFile's role: it clears the optimizer's fileReads cache between
// turns, ensuring that stale tracking from a previous prepareMessages call
// does not bleed into the next one.  While OptimizeConversation re-tracks
// from scratch, InvalidateFile provides inter-turn state hygiene and
// ensures GetOptimizationStats (used for debugging) reflects only the
// current turn's state.
func TestE2E_FileInvalidationPreventsStaleRedundancy(t *testing.T) {
	t.Parallel()

	// -----------------------------------------------------------------------
	// 1. Agent + handler setup
	// -----------------------------------------------------------------------
	mainClient := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, mainClient)
	agent.optimizer = NewConversationOptimizer(true, false) // enabled, not debug

	const filePath = "config.go"
	const oldContent = "old content v1"
	const newContent = "new content v2"

	// -----------------------------------------------------------------------
	// 2. Turn 1: read file with old content
	//
	//   Index  Role        Purpose
	//     0    user        "Read the config"
	//     1    assistant   tool_call → read_file(config.go)
	//     2    tool        file read result (content v1)
	//   3-18   (filler)    8 user/assistant exchanges (16 msgs) → gap ≥ 15
	//    19    user        "Summarize findings"
	//    20    assistant   "Findings summary"
	//
	// Total = 21 messages.
	// -----------------------------------------------------------------------
	turn1Messages := make([]api.Message, 0, 21)
	turn1Messages = addReadFileTurn(turn1Messages, "call_read_old", "Read the config", filePath, oldContent)
	for i := 1; i <= 8; i++ {
		turn1Messages = addFillerExchange(turn1Messages, i)
	}
	turn1Messages = append(turn1Messages, api.Message{Role: "user", Content: "Summarize findings"})
	turn1Messages = append(turn1Messages, api.Message{Role: "assistant", Content: "Findings summary"})

	require.Len(t, turn1Messages, 21, "expected exactly 21 turn-1 messages")

	// -----------------------------------------------------------------------
	// 3. prepareMessages for turn 1 — optimizer tracks the v1 read
	// -----------------------------------------------------------------------
	agent.messages = turn1Messages
	ch := NewConversationHandler(agent)
	prepared1 := ch.prepareMessages(nil)

	// Verify the v1 read is tracked in the optimizer's stats.
	stats1 := agent.optimizer.GetOptimizationStats()
	assert.Equal(t, 1, stats1["tracked_files"],
		"expected 1 tracked file after turn 1 prepareMessages")
	assert.Equal(t, []string{filePath}, stats1["file_paths"],
		"expected config.go to be tracked after turn 1")

	// Only one read of config.go (no duplicate), so nothing is [OPTIMIZED].
	assert.Equal(t, 0, countOptimizedFileReads(prepared1, filePath),
		"turn 1: should have 0 [OPTIMIZED] reads (only 1 read of config.go)")
	assert.Equal(t, 1, countPreservedFileReads(prepared1, filePath),
		"turn 1: should have 1 preserved read of config.go")

	// -----------------------------------------------------------------------
	// 4. Between turns: simulate edit_file → InvalidateFile
	//
	// In production, tool_handlers_file.go calls InvalidateFile on the
	// optimizer after a successful edit_file or write_file.
	// -----------------------------------------------------------------------
	agent.optimizer.InvalidateFile(filePath)

	// Verify the cache is cleared.
	statsAfterInvalidation := agent.optimizer.GetOptimizationStats()
	assert.Equal(t, 0, statsAfterInvalidation["tracked_files"],
		"expected 0 tracked files immediately after InvalidateFile before turn 2")

	// -----------------------------------------------------------------------
	// 5. Turn 2: edit happened, now re-read the file (content changed)
	//
	//   Index  Role        Purpose
	//   0-20   (turn 1)    all 21 messages from turn 1 (including old read)
	//    21    user        "Edit the config"
	//    22    assistant   "Edited the config"
	//    23    user        "Read the config again"
	//    24    assistant   tool_call → read_file(config.go)
	//    25    tool        file read result (content v2 — changed)
	//    26    user        "Continue"
	//    27    assistant   "Done."
	//
	// Total = 28 messages.
	//
	// Gap between old read (index 2) and new read (index 25):
	//   25 - 2 = 23 ≥ 15
	//
	// Content hashes differ (v1 ≠ v2) regardless of InvalidateFile,
	// so isRedundantFileRead returns false for the old read.
	// Both snapshots are preserved because the model needs both.
	// -----------------------------------------------------------------------
	turn2Messages := make([]api.Message, 0, 28)
	turn2Messages = append(turn2Messages, turn1Messages...)
	turn2Messages = append(turn2Messages, api.Message{Role: "user", Content: "Edit the config"})
	turn2Messages = append(turn2Messages, api.Message{Role: "assistant", Content: "Edited the config"})
	turn2Messages = addReadFileTurn(turn2Messages, "call_read_new", "Read the config again", filePath, newContent)
	turn2Messages = append(turn2Messages, api.Message{Role: "user", Content: "Continue"})
	turn2Messages = append(turn2Messages, api.Message{Role: "assistant", Content: "Done."})

	require.Len(t, turn2Messages, 28, "expected exactly 28 turn-2 messages")
	agent.messages = turn2Messages

	// -----------------------------------------------------------------------
	// 6. prepareMessages for turn 2 — optimizer re-tracks both reads
	// -----------------------------------------------------------------------
	prepared2 := ch.prepareMessages(nil)

	// -----------------------------------------------------------------------
	// 7. Assertions
	// -----------------------------------------------------------------------
	expectedLen := len(agent.messages) + 1 // +1 for system prompt prepended

	// (a) System prompt is prepended; all messages survive (no compaction
	//     since maxContextTokens defaults to 0).
	require.NotEmpty(t, prepared2, "expected non-empty prepared messages")
	assert.Equal(t, "system", prepared2[0].Role, "first message should be the system prompt")
	assert.Equal(t, expectedLen, len(prepared2),
		"expected system prompt + all %d messages (no compaction, no pruning)", len(agent.messages))

	// (b) Neither file read should be marked [OPTIMIZED] because the content
	//     hashes differ (v1 vs v2). OptimizeConversation re-tracks from
	//     scratch: old read (hash v1) → new read (hash v2) in map.  When
	//     checking old read, record.ContentHash (v2) ≠ currentHash (v1).
	assert.Equal(t, 0, countOptimizedFileReads(prepared2, filePath),
		"expected 0 [OPTIMIZED] reads for config.go after edit (different content), got %d",
		countOptimizedFileReads(prepared2, filePath))

	// (c) Both reads should be preserved with original content.
	assert.Equal(t, 2, countPreservedFileReads(prepared2, filePath),
		"expected 2 preserved reads for config.go (both v1 and v2 snapshots), got %d",
		countPreservedFileReads(prepared2, filePath))

	// (d) The old read still contains "old content v1".
	var foundOldContent bool
	for _, msg := range prepared2 {
		if msg.Role == "tool" && msg.ToolCallId == "call_read_old" {
			assert.Contains(t, msg.Content, oldContent,
				"old read should preserve original content (v1)")
			foundOldContent = true
		}
	}
	assert.True(t, foundOldContent, "expected old tool result (call_read_old) to be present")

	// (e) The new read still contains "new content v2".
	var foundNewContent bool
	for _, msg := range prepared2 {
		if msg.Role == "tool" && msg.ToolCallId == "call_read_new" {
			assert.Contains(t, msg.Content, newContent,
				"new read should preserve original content (v2)")
			foundNewContent = true
		}
	}
	assert.True(t, foundNewContent, "expected new tool result (call_read_new) to be present")

	// (f) Recent conversation messages survive.
	var foundContinueMsg bool
	for _, msg := range prepared2 {
		if msg.Role == "user" && strings.Contains(msg.Content, "Continue") {
			foundContinueMsg = true
		}
	}
	assert.True(t, foundContinueMsg,
		"expected recent user messages to survive the pipeline")

	// (g) The optimizer tracked the file across both reads.
	//     After re-tracking, the map has one entry (latest read wins).
	stats2 := agent.optimizer.GetOptimizationStats()
	assert.Equal(t, 1, stats2["tracked_files"],
		"expected 1 tracked file after turn 2 (latest read wins)")
}

// ---------------------------------------------------------------------------
// Test 2: Control — same content + gap ≥ 15 triggers redundancy
// ---------------------------------------------------------------------------

// TestE2E_FileReadRedundancyWithSameContentControl is the baseline control
// test that verifies the ConversationOptimizer correctly marks older file
// reads as [OPTIMIZED] when:
//  - The same file is read twice with identical content
//  - The gap between reads is ≥ 15 messages
//  - No file invalidation occurs (the file was not edited between reads)
//
// This proves the redundancy mechanism works before testing the invalidation
// scenario in TestE2E_FileInvalidationPreventsStaleRedundancy.
func TestE2E_FileReadRedundancyWithSameContentControl(t *testing.T) {
	t.Parallel()

	// -----------------------------------------------------------------------
	// 1. Agent + handler setup
	// -----------------------------------------------------------------------
	mainClient := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, mainClient)
	agent.optimizer = NewConversationOptimizer(true, false)

	const filePath = "config.go"
	const sameContent = "same content line 1"

	// -----------------------------------------------------------------------
	// 2. Build message layout
	//
	//   Index  Role        Purpose
	//     0    user        "Read the config"
	//     1    assistant   tool_call → read_file(config.go)
	//     2    tool        OLD file read result (content A)
	//   3-18   (filler)    8 user/assistant exchanges (16 msgs) → gap ≥ 15
	//    19    user        "Read config again"
	//    20    assistant   tool_call → read_file(config.go)
	//    21    tool        NEW file read result (same content A)
	//    22    user        "Continue"
	//    23    assistant   "Done."
	//
	// Total = 24 messages.
	// Indices are in agent.messages (0-based, no system prompt).
	// OptimizeConversation runs on agent.messages before system prompt
	// is prepended, so the message gap is 21 - 2 = 19 ≥ 15.
	// ∴ The older read at index 2 should be [OPTIMIZED].
	// -----------------------------------------------------------------------
	messages := make([]api.Message, 0, 24)

	// -- First read of config.go --
	messages = addReadFileTurn(messages, "call_read_old", "Read the config", filePath, sameContent)

	// -- 8 filler exchanges (indices 3–18, 16 messages) --
	for i := 1; i <= 8; i++ {
		messages = addFillerExchange(messages, i)
	}

	// -- Second read of config.go with SAME content (no edit took place) --
	messages = addReadFileTurn(messages, "call_read_new", "Read config again", filePath, sameContent)

	// -- Closing messages --
	messages = append(messages, api.Message{Role: "user", Content: "Continue"})
	messages = append(messages, api.Message{Role: "assistant", Content: "Done."})

	require.Len(t, messages, 24, "expected exactly 24 test messages (verify filler count)")
	agent.messages = messages

	// -----------------------------------------------------------------------
	// 3. Run the full prepareMessages pipeline
	//    NOTE: No InvalidateFile call — the file was not edited.
	// -----------------------------------------------------------------------
	ch := NewConversationHandler(agent)
	prepared := ch.prepareMessages(nil)

	// -----------------------------------------------------------------------
	// 4. Assertions
	// -----------------------------------------------------------------------
	expectedLen := len(agent.messages) + 1 // +1 for system prompt prepended

	// (a) System prompt + all messages (no compaction).
	require.NotEmpty(t, prepared, "expected non-empty prepared messages")
	assert.Equal(t, "system", prepared[0].Role, "first message should be the system prompt")
	assert.Equal(t, expectedLen, len(prepared),
		"expected system prompt + all %d messages (no compaction, no pruning)", len(agent.messages))

	// (b) The OLDER read (call_read_old) is marked [OPTIMIZED] because:
	//     - content hashes match (same content)
	//     - old index (2) < record.MessageIndex (21)
	//     - messageGap = 21 - 2 = 19 ≥ 15
	assert.Equal(t, 1, countOptimizedFileReads(prepared, filePath),
		"expected exactly 1 [OPTIMIZED] read for config.go (same content, gap ≥ 15), got %d",
		countOptimizedFileReads(prepared, filePath))

	// (c) The most recent read is preserved with original content.
	assert.Equal(t, 1, countPreservedFileReads(prepared, filePath),
		"expected exactly 1 preserved read for config.go (the most recent), got %d",
		countPreservedFileReads(prepared, filePath))

	// (d) The optimized message contains the [OPTIMIZED] marker and mentions
	//     the file path.
	var foundOptimizedMsg bool
	for _, msg := range prepared {
		if msg.Role == "tool" && msg.ToolCallId == "call_read_old" {
			assert.Contains(t, msg.Content, "[OPTIMIZED]",
				"older read should be marked [OPTIMIZED]")
			assert.Contains(t, msg.Content, filePath,
				"[OPTIMIZED] summary should mention the file path")
			assert.NotContains(t, msg.Content, sameContent,
				"[OPTIMIZED] summary should NOT contain the original file content")
			foundOptimizedMsg = true
		}
	}
	assert.True(t, foundOptimizedMsg,
		"expected the older tool result (call_read_old) to be present (rewritten)")

	// (e) The most recent read preserves its original content.
	var foundPreservedMsg bool
	for _, msg := range prepared {
		if msg.Role == "tool" && msg.ToolCallId == "call_read_new" {
			assert.Contains(t, msg.Content, sameContent,
				"newer read should preserve original content")
			assert.NotContains(t, msg.Content, "[OPTIMIZED]",
				"newer read should NOT be marked [OPTIMIZED]")
			foundPreservedMsg = true
		}
	}
	assert.True(t, foundPreservedMsg,
		"expected the newer tool result (call_read_new) to be present with original content")

	// (f) Recent messages survive.
	var foundDoneMsg bool
	for _, msg := range prepared {
		if msg.Role == "assistant" && strings.Contains(msg.Content, "Done.") {
			foundDoneMsg = true
		}
	}
	assert.True(t, foundDoneMsg,
		"expected recent assistant messages to survive the pipeline")
}

// ---------------------------------------------------------------------------
// Test 3: InvalidateFile + same content still triggers optimization
// ---------------------------------------------------------------------------

// TestE2E_FileInvalidationSameContentStillOptimizes proves that calling
// InvalidateFile between turns does NOT prevent redundancy optimization when
// the file content is actually unchanged.  InvalidateFile clears the
// optimizer's fileReads cache for inter-turn state hygiene, but the real
// redundancy decision is made by content-hash comparison inside
// OptimizeConversation, which re-tracks all reads from scratch on each
// prepareMessages call.
//
// Scenario:
//  1. Turn 1: read file ("same content") → prepareMessages → optimizer tracks
//  2. Between turns: InvalidateFile clears tracking (simulates an edit that
//     didn't actually change content)
//  3. Turn 2: read same file (same content) → prepareMessages → optimizer
//     re-tracks both reads; content hashes match, gap ≥ 15
//
// Expected: the older read is marked [OPTIMIZED] because content hashes match
// and the message gap is ≥ 15, regardless of the InvalidateFile call.
func TestE2E_FileInvalidationSameContentStillOptimizes(t *testing.T) {
	t.Parallel()

	// -----------------------------------------------------------------------
	// 1. Agent + handler setup
	// -----------------------------------------------------------------------
	mainClient := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, mainClient)
	agent.optimizer = NewConversationOptimizer(true, false) // enabled, not debug

	const filePath = "config.go"
	const sameContent = "same content"

	// -----------------------------------------------------------------------
	// 2. Turn 1: read file with content
	//
	//   Index  Role        Purpose
	//     0    user        "Read the config"
	//     1    assistant   tool_call → read_file(config.go) = call_read_old
	//     2    tool        file read result ("same content")
	//   3-18   (filler)    8 user/assistant exchanges (16 msgs)
	//    19    user        "Summarize findings"
	//    20    assistant   "Findings summary"
	//
	// Total = 21 messages.
	// -----------------------------------------------------------------------
	turn1Messages := make([]api.Message, 0, 21)
	turn1Messages = addReadFileTurn(turn1Messages, "call_read_old", "Read the config", filePath, sameContent)
	for i := 1; i <= 8; i++ {
		turn1Messages = addFillerExchange(turn1Messages, i)
	}
	turn1Messages = append(turn1Messages, api.Message{Role: "user", Content: "Summarize findings"})
	turn1Messages = append(turn1Messages, api.Message{Role: "assistant", Content: "Findings summary"})

	require.Len(t, turn1Messages, 21, "expected exactly 21 turn-1 messages")

	// -----------------------------------------------------------------------
	// 3. prepareMessages for turn 1 — optimizer tracks the read
	// -----------------------------------------------------------------------
	agent.messages = turn1Messages
	ch := NewConversationHandler(agent)
	prepared1 := ch.prepareMessages(nil)

	// Verify the read is tracked in the optimizer's stats.
	stats1 := agent.optimizer.GetOptimizationStats()
	assert.Equal(t, 1, stats1["tracked_files"],
		"expected 1 tracked file after turn 1 prepareMessages")

	// Only one read, so nothing is [OPTIMIZED].
	assert.Equal(t, 0, countOptimizedFileReads(prepared1, filePath),
		"turn 1: should have 0 [OPTIMIZED] reads (only 1 read of config.go)")
	assert.Equal(t, 1, countPreservedFileReads(prepared1, filePath),
		"turn 1: should have 1 preserved read of config.go")

	// -----------------------------------------------------------------------
	// 4. Between turns: InvalidateFile (simulates an "edit" that didn't
	//    actually change the file content).
	// -----------------------------------------------------------------------
	agent.optimizer.InvalidateFile(filePath)

	statsAfterInvalidation := agent.optimizer.GetOptimizationStats()
	assert.Equal(t, 0, statsAfterInvalidation["tracked_files"],
		"expected 0 tracked files immediately after InvalidateFile")

	// -----------------------------------------------------------------------
	// 5. Turn 2: "edit" happened, re-read the file (SAME content)
	//
	//   Index  Role        Purpose
	//   0-20   (turn 1)    all 21 messages from turn 1
	//    21    user        "Maybe edit the config"
	//    22    assistant   "Edited the config"
	//    23    user        "Read the config again"
	//    24    assistant   tool_call → read_file(config.go) = call_read_new
	//    25    tool        file read result (SAME content: "same content")
	//    26    user        "Continue"
	//    27    assistant   "Done."
	//
	// Total = 28 messages.
	//
	// Gap between old read (index 2) and new read (index 25):
	//   25 - 2 = 23 ≥ 15 → threshold met.
	// Content hashes match (same content).
	// ∴ The older read at index 2 should be [OPTIMIZED].
	// -----------------------------------------------------------------------
	turn2Messages := make([]api.Message, 0, 28)
	turn2Messages = append(turn2Messages, turn1Messages...)
	turn2Messages = append(turn2Messages, api.Message{Role: "user", Content: "Maybe edit the config"})
	turn2Messages = append(turn2Messages, api.Message{Role: "assistant", Content: "Edited the config"})
	turn2Messages = addReadFileTurn(turn2Messages, "call_read_new", "Read the config again", filePath, sameContent)
	turn2Messages = append(turn2Messages, api.Message{Role: "user", Content: "Continue"})
	turn2Messages = append(turn2Messages, api.Message{Role: "assistant", Content: "Done."})

	require.Len(t, turn2Messages, 28, "expected exactly 28 turn-2 messages")
	agent.messages = turn2Messages

	// -----------------------------------------------------------------------
	// 6. prepareMessages for turn 2 — optimizer re-tracks both reads
	// -----------------------------------------------------------------------
	prepared2 := ch.prepareMessages(nil)

	// -----------------------------------------------------------------------
	// 7. Assertions
	// -----------------------------------------------------------------------
	expectedLen := len(agent.messages) + 1 // +1 for system prompt prepended

	// (a) System prompt is prepended; all messages survive.
	require.NotEmpty(t, prepared2, "expected non-empty prepared messages")
	assert.Equal(t, "system", prepared2[0].Role, "first message should be the system prompt")
	assert.Equal(t, expectedLen, len(prepared2),
		"expected system prompt + all %d messages (no compaction, no pruning)", len(agent.messages))

	// (b) The OLDER read (call_read_old) IS marked [OPTIMIZED] because:
	//     - content hashes match (same content, despite InvalidateFile)
	//     - gap = 25 - 2 = 23 ≥ 15
	//     InvalidateFile only clears the inter-turn cache; OptimizeConversation
	//     re-tracks from scratch and the hash comparison still matches.
	assert.Equal(t, 1, countOptimizedFileReads(prepared2, filePath),
		"expected exactly 1 [OPTIMIZED] read for config.go (same content after invalidation, gap ≥ 15), got %d",
		countOptimizedFileReads(prepared2, filePath))

	// (c) The most recent read is preserved with original content.
	assert.Equal(t, 1, countPreservedFileReads(prepared2, filePath),
		"expected exactly 1 preserved read for config.go (the most recent), got %d",
		countPreservedFileReads(prepared2, filePath))

	// (d) The optimized message contains the [OPTIMIZED] marker and does NOT
	//     contain the original file content.
	var foundOptimizedMsg bool
	for _, msg := range prepared2 {
		if msg.Role == "tool" && msg.ToolCallId == "call_read_old" {
			assert.Contains(t, msg.Content, "[OPTIMIZED]",
				"older read should be marked [OPTIMIZED]")
			assert.Contains(t, msg.Content, filePath,
				"[OPTIMIZED] summary should mention the file path")
			assert.NotContains(t, msg.Content, sameContent,
				"[OPTIMIZED] summary should NOT contain the original file content")
			foundOptimizedMsg = true
		}
	}
	assert.True(t, foundOptimizedMsg,
		"expected the older tool result (call_read_old) to be present (rewritten)")

	// (e) The most recent read preserves its original content and is NOT
	//     marked [OPTIMIZED].
	var foundPreservedMsg bool
	for _, msg := range prepared2 {
		if msg.Role == "tool" && msg.ToolCallId == "call_read_new" {
			assert.Contains(t, msg.Content, sameContent,
				"newer read should preserve original content")
			assert.NotContains(t, msg.Content, "[OPTIMIZED]",
				"newer read should NOT be marked [OPTIMIZED]")
			foundPreservedMsg = true
		}
	}
	assert.True(t, foundPreservedMsg,
		"expected the newer tool result (call_read_new) to be present with original content")

	// (f) The optimizer tracked the file (latest read wins in the map).
	stats2 := agent.optimizer.GetOptimizationStats()
	assert.Equal(t, 1, stats2["tracked_files"],
		"expected 1 tracked file after turn 2 (latest read wins)")
}

// ---------------------------------------------------------------------------
// Test 4: Gap < 15 between same-content reads prevents optimization
// ---------------------------------------------------------------------------

// TestE2E_FileReadRedundancyGapBelowThreshold verifies that the minimum-gap
// threshold (15 messages) prevents false redundancy optimization even when
// two reads of the same file have identical content.
//
// When the message gap between two same-content reads is below the threshold,
// both reads are preserved because the model may need the earlier read's
// immediate context.  Only when the gap is sufficiently large (≥ 15) does
// the optimizer consider the older read redundant.
func TestE2E_FileReadRedundancyGapBelowThreshold(t *testing.T) {
	t.Parallel()

	// -----------------------------------------------------------------------
	// 1. Agent + handler setup
	// -----------------------------------------------------------------------
	mainClient := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, mainClient)
	agent.optimizer = NewConversationOptimizer(true, false)

	const filePath = "config.go"
	const stdContent = "std content"

	// -----------------------------------------------------------------------
	// 2. Build message layout with only 2 filler exchanges (4 messages)
	//    between two same-content reads.
	//
	//   Index  Role        Purpose
	//     0    user        "Read the config"
	//     1    assistant   tool_call → read_file(config.go) = call_read_old
	//     2    tool        file read result ("std content")
	//     3    user        gap filler 1
	//     4    assistant   gap filler 1
	//     5    user        gap filler 2
	//     6    assistant   gap filler 2
	//     7    user        "Read config again"
	//     8    assistant   tool_call → read_file(config.go) = call_read_new
	//     9    tool        file read result ("std content")
	//
	// Total = 10 messages.
	//
	// Gap between old read (index 2) and new read (index 9):
	//   9 - 2 = 7 < 15 → threshold NOT met.
	// Content hashes match, but gap is too small.
	// ∴ Neither read should be [OPTIMIZED]; both are preserved.
	// -----------------------------------------------------------------------
	messages := make([]api.Message, 0, 10)

	// -- First read of config.go --
	messages = addReadFileTurn(messages, "call_read_old", "Read the config", filePath, stdContent)

	// -- 2 filler exchanges (indices 3–6, 4 messages) — not enough for gap ≥ 15 --
	for i := 1; i <= 2; i++ {
		messages = addFillerExchange(messages, i)
	}

	// -- Second read of config.go with SAME content --
	messages = addReadFileTurn(messages, "call_read_new", "Read config again", filePath, stdContent)

	require.Len(t, messages, 10, "expected exactly 10 test messages")
	agent.messages = messages

	// -----------------------------------------------------------------------
	// 3. Run the full prepareMessages pipeline
	// -----------------------------------------------------------------------
	ch := NewConversationHandler(agent)
	prepared := ch.prepareMessages(nil)

	// -----------------------------------------------------------------------
	// 4. Assertions
	// -----------------------------------------------------------------------
	expectedLen := len(agent.messages) + 1 // +1 for system prompt prepended

	// (a) System prompt + all messages (no compaction).
	require.NotEmpty(t, prepared, "expected non-empty prepared messages")
	assert.Equal(t, "system", prepared[0].Role, "first message should be the system prompt")
	assert.Equal(t, expectedLen, len(prepared),
		"expected system prompt + all %d messages (no compaction, no pruning)", len(agent.messages))

	// (b) NO reads should be [OPTIMIZED] because the gap (7) < 15.
	assert.Equal(t, 0, countOptimizedFileReads(prepared, filePath),
		"expected 0 [OPTIMIZED] reads for config.go (gap %d < 15), got %d",
		9-2, countOptimizedFileReads(prepared, filePath))

	// (c) Both reads should be preserved with original content.
	assert.Equal(t, 2, countPreservedFileReads(prepared, filePath),
		"expected exactly 2 preserved reads for config.go (gap < 15), got %d",
		countPreservedFileReads(prepared, filePath))

	// (d) The older read (call_read_old) preserves its original content and
	//     is NOT marked [OPTIMIZED].
	var foundOldRead bool
	for _, msg := range prepared {
		if msg.Role == "tool" && msg.ToolCallId == "call_read_old" {
			assert.Contains(t, msg.Content, stdContent,
				"older read should preserve original content (gap < 15)")
			assert.NotContains(t, msg.Content, "[OPTIMIZED]",
				"older read should NOT be marked [OPTIMIZED] (gap < 15)")
			foundOldRead = true
		}
	}
	assert.True(t, foundOldRead,
		"expected the older tool result (call_read_old) to be present with original content")

	// (e) The newer read (call_read_new) also preserves its original content.
	var foundNewRead bool
	for _, msg := range prepared {
		if msg.Role == "tool" && msg.ToolCallId == "call_read_new" {
			assert.Contains(t, msg.Content, stdContent,
				"newer read should preserve original content")
			assert.NotContains(t, msg.Content, "[OPTIMIZED]",
				"newer read should NOT be marked [OPTIMIZED]")
			foundNewRead = true
		}
	}
	assert.True(t, foundNewRead,
		"expected the newer tool result (call_read_new) to be present with original content")
}
