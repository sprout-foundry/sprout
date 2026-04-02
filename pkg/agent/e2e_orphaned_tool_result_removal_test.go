package agent

import (
	"fmt"
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_OrphanedToolResultRemovalAfterCompaction verifies that when
// checkpoint compaction replaces a message range containing assistant
// tool_calls and their tool results, the orphaned tool result messages
// (whose tool_call_id no longer matches any remaining assistant) are
// properly removed from the prepared message list.
//
// This exercises the full prepareMessages pipeline:
//  1. BuildCheckpointCompactedMessages replaces the buffer zone with a summary
//  2. sanitizeToolMessages removes first-pass orphans
//  3. removeOrphanedToolResults removes any remaining orphans (final safety net)
//
// Additionally, a non-orphaned tool results pair in the recent tail is verified
// to survive, proving the cleanup is targeted (removes only orphans).
func TestE2E_OrphanedToolResultRemovalAfterCompaction(t *testing.T) {
	t.Parallel()

	// ---------------------------------------------------------------------------
	// 1. Wire up agent with optimizer (no LLM client — Go summaries only)
	// ---------------------------------------------------------------------------
	mainClient := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, mainClient)
	agent.optimizer = NewConversationOptimizer(true, false)

	// ---------------------------------------------------------------------------
	// 2. Build message layout
	// ---------------------------------------------------------------------------
	//   Buffer zone  (0..7):   8 messages with tool calls + tool results
	//   Middle zone  (8..23): 16 messages with long content (token pressure)
	//   Recent tail  (24..34):11 messages with a tool call/result pair + fillers
	//
	// Total = 35 messages

	const (
		bufferOrphanToolCallID = "call_orphan_abc"   // will become orphan
		tailValidToolCallID   = "call_valid_def"     // will survive
	)

	messages := make([]api.Message, 0, 35)

	// -- Buffer zone (indices 0-7): contains tool call → tool result that
	//    will become orphaned after checkpoint compaction.
	messages = append(messages, api.Message{
		Role:    "user",
		Content: "Start task: read the auth configuration file.",
	})
	messages = append(messages, api.Message{
		Role:    "assistant",
		Content: "",
		ToolCalls: []api.ToolCall{{
			ID:   bufferOrphanToolCallID,
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "read_file", Arguments: `{"path":"/etc/auth/config.yaml"}`},
		}},
	})
	messages = append(messages, api.Message{
		Role:       "tool",
		Content:    "# Auth Configuration\napiVersion: v1\nkind: Config\nmetadata:\n  name: auth",
		ToolCallId: bufferOrphanToolCallID,
	})
	// More buffer messages (indices 3-7)
	messages = append(messages, api.Message{
		Role:    "assistant",
		Content: "I've read the auth config. Now examining the token validation module.",
	})
	messages = append(messages, api.Message{
		Role:    "user",
		Content: "Continue examining.",
	})
	messages = append(messages, api.Message{
		Role:    "assistant",
		Content: "Checking the session management handler next.",
	})
	messages = append(messages, api.Message{
		Role:    "user",
		Content: "Proceed.",
	})
	messages = append(messages, api.Message{
		Role:    "assistant",
		Content: "Reviewing middleware chain configuration now.",
	})

	// -- Middle zone (indices 8-23): 8 user/assistant pairs with long content
	//    to keep token count above the compaction threshold even after first pass.
	for i := 0; i < 8; i++ {
		messages = append(messages, api.Message{
			Role: "assistant",
			Content: fmt.Sprintf(
				"Detailed analysis of authentication component %d: reviewed the token validation logic, session management handlers, middleware chain configuration, error handling patterns, input validation strategies, and security considerations for the refactored module with comprehensive edge case coverage and backward compatibility analysis across all supported API endpoints and client integrations.", i),
		})
		messages = append(messages, api.Message{
			Role: "user",
			Content: fmt.Sprintf(
				"Continue analyzing component %d, check error handling, edge cases, test coverage, backward compatibility, and integration points for the authentication refactoring across all supported platforms and environments.", i),
		})
	}

	// -- Recent tail (indices 24..34): 11 messages — first pair is a tool call/result
	messages = append(messages, api.Message{
		Role:    "user",
		Content: "Search for usage of the token validator.",
	})
	messages = append(messages, api.Message{
		Role:    "assistant",
		Content: "",
		ToolCalls: []api.ToolCall{{
			ID:   tailValidToolCallID,
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "search_files", Arguments: `{"pattern":"validateToken"}`},
		}},
	})
	messages = append(messages, api.Message{
		Role:       "tool",
		Content:    "Found 5 matches for validateToken across 3 files.",
		ToolCallId: tailValidToolCallID,
	})
	// Fill rest of recent tail (8 more messages)
	for i := 0; i < 4; i++ {
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("Analyzing result %d from the token validator search.", i),
		})
		messages = append(messages, api.Message{
			Role:    "user",
			Content: "Go on.",
		})
	}

	require.Len(t, messages, 35, "expected exactly 35 test messages")
	agent.messages = messages
	originalCount := len(agent.messages)

	// ---------------------------------------------------------------------------
	// 3. Record a checkpoint covering the buffer zone (indices 0-7)
	// ---------------------------------------------------------------------------
	agent.RecordTurnCheckpoint(0, 7)
	require.True(t, agent.HasTurnCheckpoints(), "expected turn checkpoints recorded")

	// ---------------------------------------------------------------------------
	// 4. Find maxContextTokens that triggers compaction threshold
	// ---------------------------------------------------------------------------
	ch := NewConversationHandler(agent)
	findCompactionMaxContext(t, agent, ch)

	// ---------------------------------------------------------------------------
	// 5. Call prepareMessages — the e2e entry point
	// ---------------------------------------------------------------------------
	prepared := ch.prepareMessages(nil)

	t.Logf("Orphaned tool result removal: original=%d, final=%d, checkpoints=%v",
		originalCount, len(agent.messages), agent.HasTurnCheckpoints())

	// ---------------------------------------------------------------------------
	// Assertions
	// ---------------------------------------------------------------------------

	// (a) Checkpoint compaction fired: agent.messages should be updated.
	assert.Less(t, len(agent.messages), originalCount,
		"expected agent.messages to be reduced after checkpoint compaction: got %d, want < %d",
		len(agent.messages), originalCount)

	// (b) The orphaned tool result (call_orphan_abc) must NOT appear in prepared
	//     messages. Its assistant was compacted away, so it's orphaned.
	for i, msg := range prepared {
		if msg.Role == "tool" && msg.ToolCallId == bufferOrphanToolCallID {
			t.Errorf("prepared[%d]: found orphaned tool result with tool_call_id=%q that should have been removed",
				i, bufferOrphanToolCallID)
		}
	}

	// (c) The valid tool result (call_valid_def) in the recent tail MUST survive.
	//     The assistant in the recent tail still has this tool_call_id.
	var foundValidToolResult bool
	for _, msg := range prepared {
		if msg.Role == "tool" && msg.ToolCallId == tailValidToolCallID {
			foundValidToolResult = true
			break
		}
	}
	assert.True(t, foundValidToolResult,
		"expected the non-orphaned tool result (%s) in the recent tail to be preserved",
		tailValidToolCallID)

	// (d) Recent tail user messages should be preserved.
	var foundRecentUserMsg bool
	for _, msg := range prepared {
		if msg.Role == "user" && strings.Contains(msg.Content, "Search for usage of the token validator") {
			foundRecentUserMsg = true
			break
		}
	}
	assert.True(t, foundRecentUserMsg,
		"expected recent tail user messages to be preserved after compaction")

	// (e) Checkpoint summary should exist (proving compaction fired).
	var foundCompactionHeader bool
	for _, msg := range prepared {
		if strings.Contains(msg.Content, "Compacted earlier conversation state:") {
			foundCompactionHeader = true
			break
		}
	}
	assert.True(t, foundCompactionHeader,
		"expected checkpoint summary with 'Compacted earlier conversation state:' header")

	// (f) The checkpoint summary should mention the buffer zone content.
	var foundBufferZoneRef bool
	for _, msg := range prepared {
		if strings.Contains(msg.Content, "Start task:") || strings.Contains(msg.Content, "auth configuration") {
			foundBufferZoneRef = true
			break
		}
	}
	assert.True(t, foundBufferZoneRef,
		"expected checkpoint summary to reference the buffer zone content")
}

// TestE2E_OrphanedToolResultsBeforeAnyAssistant verifies the edge case
// where orphaned tool results exist in the message list after checkpoint
// compaction (both in the compacted-away buffer zone and as stray entries
// in the recent tail). The full prepareMessages pipeline
// (sanitizeToolMessages + removeOrphanedToolResults) should clean all
// of them from the final prepared message list.
func TestE2E_OrphanedToolResultsBeforeAnyAssistant(t *testing.T) {
	t.Parallel()

	// ---------------------------------------------------------------------------
	// 1. Wire up agent with optimizer (Go summaries only)
	// ---------------------------------------------------------------------------
	mainClient := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, mainClient)
	agent.optimizer = NewConversationOptimizer(true, false)

	// ---------------------------------------------------------------------------
	// 2. Build message layout
	// ---------------------------------------------------------------------------
	//   Buffer zone (0-3): user, assistant with tool_call, tool result, user
	//     → Will be checkpoint-compacted, removing the assistant + its tool result
	//   Recent tail (4-10): 7 messages — all plain text, NO tool calls at all
	//
	// After compaction, the prepared messages contain:
	//   [system][summary][tail messages...]
	// There are NO remaining assistant messages with tool_calls anywhere.
	// The "no valid tool calls" path should strip any residual tool results.
	//
	// To make this test meaningful, we add a tool result in the recent tail
	// that is orphaned (no matching assistant in the tail), testing that
	// the full-cleanup path catches it.

	const (
		orphanToolCallID = "call_edge_case_xyz"
	)

	messages := make([]api.Message, 0, 11)

	// -- Buffer zone (indices 0-3): tool call + tool result → compacted
	messages = append(messages, api.Message{
		Role:    "user",
		Content: "Read the main config file.",
	})
	messages = append(messages, api.Message{
		Role:    "assistant",
		Content: "",
		ToolCalls: []api.ToolCall{{
			ID:   "call_buf_tool",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "read_file", Arguments: `{"path":"/etc/config.yaml"}`},
		}},
	})
	messages = append(messages, api.Message{
		Role:       "tool",
		Content:    "# Config\ndatabase:\n  host: localhost",
		ToolCallId: "call_buf_tool",
	})
	messages = append(messages, api.Message{
		Role:    "user",
		Content: "Continue with the analysis.",
	})

	// -- Recent tail (indices 4..10): 7 messages
	//    Inject a deliberately orphaned tool result at index 5 (between user/assistant)
	messages = append(messages, api.Message{
		Role:    "assistant",
		Content: "Proceeding with the analysis of the configuration.",
	})
	messages = append(messages, api.Message{
		Role:       "tool",
		Content:    "This is a stray tool result that has no matching assistant tool_call.",
		ToolCallId: orphanToolCallID,
	})
	messages = append(messages, api.Message{
		Role:    "user",
		Content: "Summarize findings.",
	})
	for i := 0; i < 2; i++ {
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("Finding %d: the configuration is valid.", i),
		})
		messages = append(messages, api.Message{
			Role:    "user",
			Content: "Continue.",
		})
	}

	require.Len(t, messages, 11, "expected exactly 11 test messages")
	agent.messages = messages
	originalCount := len(agent.messages)

	// ---------------------------------------------------------------------------
	// 3. Record checkpoint covering buffer zone (indices 0-3)
	// ---------------------------------------------------------------------------
	agent.RecordTurnCheckpoint(0, 3)
	require.True(t, agent.HasTurnCheckpoints())

	// ---------------------------------------------------------------------------
	// 4. Find maxContextTokens that triggers compaction
	// ---------------------------------------------------------------------------
	ch := NewConversationHandler(agent)
	findCompactionMaxContext(t, agent, ch)

	// ---------------------------------------------------------------------------
	// 5. Call prepareMessages
	// ---------------------------------------------------------------------------
	prepared := ch.prepareMessages(nil)

	t.Logf("Orphaned-before-assistant: original=%d, final=%d",
		originalCount, len(agent.messages))

	// ---------------------------------------------------------------------------
	// Assertions
	// ---------------------------------------------------------------------------

	// (a) Compaction fired.
	assert.Less(t, len(agent.messages), originalCount,
		"expected compaction to reduce message count")

	// (b) The orphaned tool result in the tail must be removed. No remaining
	//     assistant message has tool_call_id == orphanToolCallID.
	for i, msg := range prepared {
		if msg.Role == "tool" && msg.ToolCallId == orphanToolCallID {
			t.Errorf("prepared[%d]: found orphaned tool result in tail with tool_call_id=%q that should have been removed",
				i, orphanToolCallID)
		}
	}

	// (c) The buffer zone tool result (call_buf_tool) must also be removed
	//     since its assistant was compacted away.
	for i, msg := range prepared {
		if msg.Role == "tool" && msg.ToolCallId == "call_buf_tool" {
			t.Errorf("prepared[%d]: found orphaned buffer zone tool result with tool_call_id=%q that should have been removed",
				i, "call_buf_tool")
		}
	}

	// (d) No tool-role messages at all should remain (both were orphaned).
	for i, msg := range prepared {
		if msg.Role == "tool" {
			t.Errorf("prepared[%d]: found unexpected tool message with tool_call_id=%q, content=%q",
				i, msg.ToolCallId, msg.Content)
		}
	}

	// (e) Recent tail messages should be preserved.
	var foundRecentMsg bool
	for _, msg := range prepared {
		if msg.Role == "user" && strings.Contains(msg.Content, "Summarize findings") {
			foundRecentMsg = true
			break
		}
	}
	assert.True(t, foundRecentMsg,
		"expected recent tail user message to be preserved after compaction")

	// (f) Checkpoint summary should exist.
	var foundCompactionHeader bool
	for _, msg := range prepared {
		if strings.Contains(msg.Content, "Compacted earlier conversation state:") {
			foundCompactionHeader = true
			break
		}
	}
	assert.True(t, foundCompactionHeader,
		"expected checkpoint summary header in prepared messages")
}
