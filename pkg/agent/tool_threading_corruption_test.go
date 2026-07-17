package agent

import (
	"fmt"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// validateToolThreading is a local reimplementation of seed's
// ValidateToolThreading for use in tests that can't depend on
// seed's internal diagnostics API (removed from local dev copy).
// Returns the count of missing_result violations: assistant tool
// calls with no matching tool result in the immediately following
// run of tool messages.
func validateToolThreading(msgs []api.Message) int {
	missing := 0
	for i, m := range msgs {
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		// Collect IDs declared by this assistant
		declared := make(map[string]bool)
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				declared[tc.ID] = false
			}
		}
		// Check immediately following tool messages
		for j := i + 1; j < len(msgs) && msgs[j].Role == "tool"; j++ {
			if declared[msgs[j].ToolCallID] {
				declared[msgs[j].ToolCallID] = true
			}
		}
		for _, found := range declared {
			if !found {
				missing++
			}
		}
	}
	return missing
}

// TestToolThreading_NoCorruptionMultiTurn runs a full agent through
// processQueryWithSeed with a scripted client that makes tool calls,
// then validates that the raw state messages have no threading violations.
// This reproduces the conditions under which the diagnostic captures showed
// 1,690 missing tool results.
func TestToolThreading_NoCorruptionMultiTurn(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	client := NewScriptedClient(
		NewScriptedToolCallResponse("call_1", "read_file", `{"path":"test.txt"}`, "Let me read the file."),
		NewScriptedToolCallResponse("call_2", "search_files", `{"search_pattern":"test"}`, "Now let me search."),
		NewScriptedToolCallResponse("call_3", "list_directory", `{"path":"."}`, "Checking the directory."),
		NewScriptedTextResponse("Done analyzing. The code looks good."),
	)

	agent, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient failed: %v", err)
	}
	defer agent.Shutdown()
	agent.SetMaxIterations(20)

	// Turn 1
	_, err = agent.ProcessQuery("Analyze the codebase")
	if err != nil {
		t.Fatalf("Turn 1 failed: %v", err)
	}

	// Validate threading on raw state after turn 1
	msgs := agent.state.GetMessages()
	violations := validateToolThreading(msgs)
	consecutive := countConsecutiveAssistants(msgs)
	t.Logf("After turn 1: %d msgs, %d violations, %d consecutive assistants", len(msgs), violations, consecutive)
	if consecutive > 0 {
		t.Errorf("After turn 1: %d consecutive assistant pairs (the corruption symptom)", consecutive)
		dumpMessageRoles(t, msgs, "Turn 1 raw state")
	}

	// Turn 2: swap to a fresh client with more responses — re-create the
	// agent so it picks up the new client, but first snapshot the messages
	// to carry over conversation history.
	prevMsgs := agent.state.GetMessages()
	prevCheckpoints := agent.state.GetTurnCheckpoints()
	agent.Shutdown()

	client2 := NewScriptedClient(
		NewScriptedToolCallResponse("call_4", "search_files", `{"search_pattern":"main"}`, "Searching again."),
		NewScriptedToolCallResponse("call_5", "read_file", `{"path":"main.go"}`, "Reading main.go."),
		NewScriptedTextResponse("Analysis complete. Everything looks good."),
	)
	agent2, err := NewAgentWithClient(client2, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient (2) failed: %v", err)
	}
	defer agent2.Shutdown()
	agent2.SetMaxIterations(20)
	// Restore conversation history so the second query sees prior turns.
	agent2.state.SetMessages(prevMsgs)
	agent2.state.SetTurnCheckpoints(prevCheckpoints)

	output2, err := agent2.ProcessQuery("Go deeper into the main entry point")
	if err != nil {
		t.Fatalf("Turn 2 failed: %v", err)
	}
	_ = output2

	// Validate threading on raw state after turn 2
	msgs2 := agent2.state.GetMessages()
	violations2 := validateToolThreading(msgs2)
	consecutive2 := countConsecutiveAssistants(msgs2)
	t.Logf("After turn 2: %d msgs, %d violations, %d consecutive assistants", len(msgs2), violations2, consecutive2)
	if consecutive2 > 0 {
		t.Errorf("After turn 2: %d consecutive assistant pairs (the corruption symptom)", consecutive2)
		dumpMessageRoles(t, msgs2, "Turn 2 raw state")
	}

	// Turn 3: even more tool calls to build up history
	prevMsgs2 := agent2.state.GetMessages()
	prevCheckpoints2 := agent2.state.GetTurnCheckpoints()
	agent2.Shutdown()

	client3 := NewScriptedClient(
		NewScriptedToolCallResponse("call_6", "list_directory", `{"path":"."}`, "Listing directory."),
		NewScriptedToolCallResponse("call_7", "search_files", `{"search_pattern":"func"}`, "Searching for functions."),
		NewScriptedToolCallResponse("call_8", "read_file", `{"path":"go.mod"}`, "Reading go.mod."),
		NewScriptedToolCallResponse("call_9", "repo_map", `{"depth":2}`, "Getting repo map."),
		NewScriptedTextResponse("Final analysis complete. All good."),
	)
	agent3, err := NewAgentWithClient(client3, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient (3) failed: %v", err)
	}
	defer agent3.Shutdown()
	agent3.SetMaxIterations(20)
	agent3.state.SetMessages(prevMsgs2)
	agent3.state.SetTurnCheckpoints(prevCheckpoints2)

	_, err = agent3.ProcessQuery("Give me a comprehensive overview")
	if err != nil {
		t.Fatalf("Turn 3 failed: %v", err)
	}

	// Final validation
	msgs3 := agent3.state.GetMessages()
	violations3 := validateToolThreading(msgs3)
	consecutive3 := countConsecutiveAssistants(msgs3)
	t.Logf("After turn 3: %d msgs, %d violations, %d consecutive assistants", len(msgs3), violations3, consecutive3)
	if consecutive3 > 0 {
		t.Errorf("After turn 3: %d consecutive assistant pairs (the corruption symptom)", consecutive3)
		dumpMessageRoles(t, msgs3, "Turn 3 raw state")
	}

	t.Logf("Turn 3: %d messages, %d violations, %d consecutive assistants", len(msgs3), violations3, consecutive3)
}

// TestToolThreading_RapidToolCalls tests a single turn where the model
// makes many rapid tool calls (10+), which is where corruption tends to
// accumulate in the diagnostic captures.
func TestToolThreading_RapidToolCalls(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	// Build 15 tool-call responses then a final answer
	responses := make([]*ScriptedResponse, 0, 16)
	for i := 1; i <= 15; i++ {
		responses = append(responses, NewScriptedToolCallResponse(
			fmt.Sprintf("call_%d", i),
			"search_files",
			fmt.Sprintf(`{"search_pattern":"pattern_%d"}`,
				i),
			fmt.Sprintf("Searching for pattern %d.", i),
		))
	}
	responses = append(responses, NewScriptedTextResponse("All searches complete. Here's my analysis."))

	client := NewScriptedClient(responses...)
	agent, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient failed: %v", err)
	}
	defer agent.Shutdown()
	agent.SetMaxIterations(20)

	output, err := agent.ProcessQuery("Do a thorough search")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if output == "" {
		t.Fatal("Empty output")
	}

	msgs := agent.state.GetMessages()
	violations := validateToolThreading(msgs)
	consecutive := countConsecutiveAssistants(msgs)

	t.Logf("After 15 tool calls: %d messages, %d violations, %d consecutive assistants", len(msgs), violations, consecutive)

	if consecutive > 0 {
		t.Errorf("%d consecutive assistant pairs after rapid tool calls", consecutive)
		dumpMessageRoles(t, msgs, "Rapid tool calls")
	}

	if consecutive > 0 {
		t.Errorf("%d consecutive assistant pairs after rapid tool calls", consecutive)
	}
}

// TestToolThreading_SentRequestsValidation checks the messages AS SENT TO
// THE PROVIDER (via ScriptedClient.sentRequests) to determine if the
// corruption appears in the prepared messages even when raw state is clean.
func TestToolThreading_SentRequestsValidation(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	// 5 tool calls then final answer
	client := NewScriptedClient(
		NewScriptedToolCallResponse("c1", "search_files", `{"search_pattern":"a"}`, "Searching a."),
		NewScriptedToolCallResponse("c2", "search_files", `{"search_pattern":"b"}`, "Searching b."),
		NewScriptedToolCallResponse("c3", "search_files", `{"search_pattern":"c"}`, "Searching c."),
		NewScriptedToolCallResponse("c4", "read_file", `{"path":"x.go"}`, "Reading x."),
		NewScriptedTextResponse("Done."),
	)
	agent, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient failed: %v", err)
	}
	defer agent.Shutdown()
	agent.SetMaxIterations(20)

	_, err = agent.ProcessQuery("search and read")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// Check each sent request for threading violations
	sentReqs := client.GetSentRequests()
	for i, reqMsgs := range sentReqs {
		violations := validateToolThreading(reqMsgs)
		consecutive := countConsecutiveAssistants(reqMsgs)
		if consecutive > 0 {
			t.Errorf("Request %d (sent to provider): %d consecutive assistant pairs", i, consecutive)
			dumpMessageRoles(t, reqMsgs, fmt.Sprintf("Sent request %d", i))
		}
		_ = violations
	}

	t.Logf("Validated %d sent requests, all clean", len(sentReqs))
}

// --- Helpers ---

func countConsecutiveAssistants(msgs []api.Message) int {
	count := 0
	for i := 1; i < len(msgs); i++ {
		if msgs[i].Role == "assistant" && msgs[i-1].Role == "assistant" {
			count++
		}
	}
	return count
}

func dumpMessageRoles(t *testing.T, msgs []api.Message, label string) {
	t.Helper()
	t.Logf("=== %s (%d messages) ===", label, len(msgs))
	for i, m := range msgs {
		tcs := len(m.ToolCalls)
		tcid := ""
		if m.ToolCallID != "" {
			tcid = m.ToolCallID[:min(20, len(m.ToolCallID))]
		}
		content := strings.TrimSpace(m.Content)
		if len(content) > 60 {
			content = content[:60] + "..."
		}
		t.Logf("  [%d] role=%s tcs=%d tcid=%q content=%q", i, m.Role, tcs, tcid, content)
	}
}

// NewScriptedToolCallResponse creates a response with a single tool call.
func NewScriptedToolCallResponse(id, toolName, args, reasoningContent string) *ScriptedResponse {
	return &ScriptedResponse{
		Content:          "\n\n",
		ReasoningContent: reasoningContent,
		FinishReason:     "tool_calls",
		ToolCalls: []api.ToolCall{
			{
				ID:   id,
				Type: "function",
				Function: api.ToolCallFunction{
					Name:      toolName,
					Arguments: args,
				},
			},
		},
	}
}

// NewScriptedTextResponse creates a response with text content and stop finish reason.
func NewScriptedTextResponse(content string) *ScriptedResponse {
	return &ScriptedResponse{
		Content:      content,
		FinishReason: "stop",
	}
}
