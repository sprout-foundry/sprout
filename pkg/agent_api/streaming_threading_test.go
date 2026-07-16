package api

import (
	"testing"
)

// TestStreamingBuilder_QwenPatternMultipleToolCalls simulates the SSE
// streaming pattern from a Qwen model making multiple tool calls in one
// response. Each tool call arrives as a separate SSE chunk with the correct
// index (0, 1, 2). This is the well-formed case.
func TestStreamingBuilder_QwenPatternMultipleToolCalls(t *testing.T) {
	builder := NewStreamingResponseBuilder(nil)

	// Chunk 1: reasoning content
	_ = builder.ProcessChunk(&StreamingChatResponse{
		Choices: []StreamingChoice{{
			Index: 0,
			Delta: StreamingDelta{
				ReasoningContent: "Let me search for that.",
			},
		}},
	})

	// Chunk 2: tool call 0 — ID and name
	_ = builder.ProcessChunk(&StreamingChatResponse{
		Choices: []StreamingChoice{{
			Index: 0,
			Delta: StreamingDelta{
				ToolCalls: []StreamingToolCall{{
					Index: 0,
					ID:    "call_001",
					Type:  "function",
					Function: &StreamingToolCallFunction{
						Name:      "search_files",
						Arguments: `{"search_pattern":"test"}`,
					},
				}},
			},
		}},
	})

	// Chunk 3: tool call 1 — ID and name
	_ = builder.ProcessChunk(&StreamingChatResponse{
		Choices: []StreamingChoice{{
			Index: 0,
			Delta: StreamingDelta{
				ToolCalls: []StreamingToolCall{{
					Index: 1,
					ID:    "call_002",
					Type:  "function",
					Function: &StreamingToolCallFunction{
						Name:      "read_file",
						Arguments: `{"path":"main.go"}`,
					},
				}},
			},
		}},
	})

	// Chunk 4: finish reason
	finishReason := "tool_calls"
	_ = builder.ProcessChunk(&StreamingChatResponse{
		Choices: []StreamingChoice{{
			Index:        0,
			FinishReason: &finishReason,
		}},
	})

	resp := builder.GetResponse()
	if resp == nil || len(resp.Choices) == 0 {
		t.Fatal("nil response or no choices")
	}

	choice := resp.Choices[0]
	if choice.FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want %q", choice.FinishReason, "tool_calls")
	}
	if len(choice.Message.ToolCalls) != 2 {
		t.Fatalf("tool_calls count = %d, want 2", len(choice.Message.ToolCalls))
	}
	if choice.Message.ToolCalls[0].ID != "call_001" {
		t.Errorf("tc[0].ID = %q, want call_001", choice.Message.ToolCalls[0].ID)
	}
	if choice.Message.ToolCalls[1].ID != "call_002" {
		t.Errorf("tc[1].ID = %q, want call_002", choice.Message.ToolCalls[1].ID)
	}
}

// TestStreamingBuilder_QwenPatternIndexCollision tests the case where the
// model/backend sends ALL tool calls with index: 0. This is a known issue
// with some OpenAI-compatible backends. The builder should NOT overwrite
// — it should accumulate.
//
// Currently this test DOCUMENTS the buggy behavior: index collision causes
// tool calls to be silently overwritten. Only the last tool call survives.
func TestStreamingBuilder_QwenPatternIndexCollision(t *testing.T) {
	builder := NewStreamingResponseBuilder(nil)

	// Tool call A — index 0
	_ = builder.ProcessChunk(&StreamingChatResponse{
		Choices: []StreamingChoice{{
			Index: 0,
			Delta: StreamingDelta{
				ToolCalls: []StreamingToolCall{{
					Index: 0,
					ID:    "call_A",
					Type:  "function",
					Function: &StreamingToolCallFunction{
						Name:      "search_files",
						Arguments: `{"search_pattern":"a"}`,
					},
				}},
			},
		}},
	})

	// Tool call B — ALSO index 0 (bug: should be index 1)
	_ = builder.ProcessChunk(&StreamingChatResponse{
		Choices: []StreamingChoice{{
			Index: 0,
			Delta: StreamingDelta{
				ToolCalls: []StreamingToolCall{{
					Index: 0,
					ID:    "call_B",
					Type:  "function",
					Function: &StreamingToolCallFunction{
						Name:      "read_file",
						Arguments: `{"path":"b.go"}`,
					},
				}},
			},
		}},
	})

	finishReason := "tool_calls"
	_ = builder.ProcessChunk(&StreamingChatResponse{
		Choices: []StreamingChoice{{
			Index:        0,
			FinishReason: &finishReason,
		}},
	})

	resp := builder.GetResponse()
	choice := resp.Choices[0]

	// BUG: Only 1 tool call survives because both used index 0.
	// The second overwrites the first in the map.
	t.Logf("Index collision result: %d tool calls (IDs: %v)", len(choice.Message.ToolCalls), toolCallIDs(choice.Message.ToolCalls))
	if len(choice.Message.ToolCalls) != 1 {
		t.Errorf("index collision: got %d tool calls, want 1 (documents current behavior)", len(choice.Message.ToolCalls))
	}
}

// TestStreamingBuilder_QwenPatternIncrementalArgs tests the case where tool
// call arguments arrive incrementally across multiple chunks (the standard
// OpenAI streaming pattern).
func TestStreamingBuilder_QwenPatternIncrementalArgs(t *testing.T) {
	builder := NewStreamingResponseBuilder(nil)

	// Chunk 1: tool call 0 — ID and name, empty args
	_ = builder.ProcessChunk(&StreamingChatResponse{
		Choices: []StreamingChoice{{
			Index: 0,
			Delta: StreamingDelta{
				ToolCalls: []StreamingToolCall{{
					Index:    0,
					ID:       "call_001",
					Type:     "function",
					Function: &StreamingToolCallFunction{Name: "search_files"},
				}},
			},
		}},
	})

	// Chunk 2: tool call 0 — args fragment 1
	_ = builder.ProcessChunk(&StreamingChatResponse{
		Choices: []StreamingChoice{{
			Index: 0,
			Delta: StreamingDelta{
				ToolCalls: []StreamingToolCall{{
					Index:    0,
					Function: &StreamingToolCallFunction{Arguments: `{"search`},
				}},
			},
		}},
	})

	// Chunk 3: tool call 0 — args fragment 2
	_ = builder.ProcessChunk(&StreamingChatResponse{
		Choices: []StreamingChoice{{
			Index: 0,
			Delta: StreamingDelta{
				ToolCalls: []StreamingToolCall{{
					Index:    0,
					Function: &StreamingToolCallFunction{Arguments: `_pattern":"test"}`},
				}},
			},
		}},
	})

	// Chunk 4: finish
	finishReason := "tool_calls"
	_ = builder.ProcessChunk(&StreamingChatResponse{
		Choices: []StreamingChoice{{
			Index:        0,
			FinishReason: &finishReason,
		}},
	})

	resp := builder.GetResponse()
	choice := resp.Choices[0]

	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls count = %d, want 1", len(choice.Message.ToolCalls))
	}
	args := choice.Message.ToolCalls[0].Function.Arguments
	if args != `{"search_pattern":"test"}` {
		t.Errorf("accumulated args = %q, want %q", args, `{"search_pattern":"test"}`)
	}
}

// TestStreamingBuilder_QwenPatternNoFinishReason tests what happens when the
// streaming response has tool_calls but NO finish_reason chunk. This is the
// pattern that triggers the default-to-"stop" code in handleStreamingResponse.
func TestStreamingBuilder_QwenPatternNoFinishReason(t *testing.T) {
	builder := NewStreamingResponseBuilder(nil)

	// Tool call with content "\n\n"
	_ = builder.ProcessChunk(&StreamingChatResponse{
		Choices: []StreamingChoice{{
			Index: 0,
			Delta: StreamingDelta{
				Content: "\n\n",
			},
		}},
	})
	_ = builder.ProcessChunk(&StreamingChatResponse{
		Choices: []StreamingChoice{{
			Index: 0,
			Delta: StreamingDelta{
				ToolCalls: []StreamingToolCall{{
					Index: 0,
					ID:    "call_001",
					Type:  "function",
					Function: &StreamingToolCallFunction{
						Name:      "search_files",
						Arguments: `{"search_pattern":"test"}`,
					},
				}},
			},
		}},
	})
	// NO finish_reason chunk — stream just ends

	resp := builder.GetResponse()
	choice := resp.Choices[0]

	t.Logf("No finish_reason: finish_reason=%q, content=%q, tool_calls=%d",
		choice.FinishReason, choice.Message.Content, len(choice.Message.ToolCalls))

	// The builder doesn't set a default finish_reason — it stays empty.
	// The fix in handleStreamingResponse sets it to "stop" when content != "".
	// But content IS "\n\n" which is non-empty, so it gets "stop" instead
	// of "tool_calls".
	if choice.FinishReason != "" {
		t.Errorf("expected empty finish_reason from builder, got %q", choice.FinishReason)
	}
	if len(choice.Message.ToolCalls) != 1 {
		t.Errorf("tool_calls count = %d, want 1", len(choice.Message.ToolCalls))
	}
}

// TestStreamingBuilder_DefaultToStopWithToolCalls is the CRITICAL test:
// it reproduces the bug where handleStreamingResponse sets finish_reason
// to "stop" when content is "\n\n" (non-empty) but tool_calls ARE present.
// This causes seed to take the "stop" path, potentially skipping tool execution.
func TestStreamingBuilder_DefaultToStopWithToolCalls(t *testing.T) {
	builder := NewStreamingResponseBuilder(nil)

	// Simulate Qwen pattern: content "\n\n", tool call, no finish_reason
	_ = builder.ProcessChunk(&StreamingChatResponse{
		Choices: []StreamingChoice{{
			Index: 0,
			Delta: StreamingDelta{
				Content: "\n\n",
			},
		}},
	})
	_ = builder.ProcessChunk(&StreamingChatResponse{
		Choices: []StreamingChoice{{
			Index: 0,
			Delta: StreamingDelta{
				ToolCalls: []StreamingToolCall{{
					Index: 0,
					ID:    "call_001",
					Type:  "function",
					Function: &StreamingToolCallFunction{
						Name:      "search_files",
						Arguments: "{}",
					},
				}},
			},
		}},
	})

	resp := builder.GetResponse()
	choice := resp.Choices[0]

	// Simulate what handleStreamingResponse does AFTER getting the response:
	if choice.FinishReason == "" && choice.Message.Content != "" {
		choice.FinishReason = "stop"
	}

	t.Logf("After default-to-stop: finish_reason=%q, content=%q, tool_calls=%d",
		choice.FinishReason, choice.Message.Content, len(choice.Message.ToolCalls))

	// BUG: finish_reason is "stop" even though there are tool calls.
	// seed's runLoop handles this correctly (tool_calls present falls through
	// to execution), but it's the wrong signal.
	if choice.FinishReason != "stop" {
		t.Errorf("expected default-to-stop behavior, got %q", choice.FinishReason)
	}
	if len(choice.Message.ToolCalls) != 1 {
		t.Errorf("tool_calls should be present: got %d, want 1", len(choice.Message.ToolCalls))
	}
}

func toolCallIDs(tcs []ToolCall) []string {
	ids := make([]string, len(tcs))
	for i, tc := range tcs {
		ids[i] = tc.ID
	}
	return ids
}
