package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// NormalizedToolCalls is a slice of ToolCall values that have been validated
// and cleaned by the ToolCallNormalizer. It is a distinct type to make it
// clear in API signatures which calls have been normalized.
type NormalizedToolCalls []ToolCall

// ToolCallNormalizer cleans up structured tool calls returned by the model
// before they are executed. It handles common model output irregularities:
//
//  1. Strips <|channel|> suffix from tool names
//  2. Generates synthetic IDs for tool calls missing one
//  3. Deduplicates by ID+arguments (first occurrence wins)
//  4. Repairs malformed JSON arguments
//  5. Normalizes Type field to "function"
type ToolCallNormalizer struct {
	// seq is a monotonically increasing counter used to guarantee unique
	// synthetic IDs even when multiple calls are normalized in the same
	// nanosecond.
	seq uint64
}

// NewToolCallNormalizer creates a new ToolCallNormalizer.
func NewToolCallNormalizer() *ToolCallNormalizer {
	return &ToolCallNormalizer{}
}

// Normalize processes a slice of ToolCall values and returns a cleaned,
// deduplicated NormalizedToolCalls slice. Calls with empty names after
// normalization or unrepairable JSON arguments are dropped.
func (n *ToolCallNormalizer) Normalize(calls []ToolCall) NormalizedToolCalls {
	if len(calls) == 0 {
		return nil
	}

	// Phase 1: normalize each call individually.
	normalized := make([]ToolCall, 0, len(calls))
	for _, tc := range calls {
		tc = n.normalizeOne(tc)
		if tc.Function.Name == "" {
			continue
		}
		// Drop calls with unrepairable JSON arguments — the executor
		// expects valid JSON and may panic on malformed input.
		if !json.Valid([]byte(tc.Function.Arguments)) {
			continue
		}
		normalized = append(normalized, tc)
	}

	// Phase 2: deduplicate by ID+arguments.
	return n.deduplicate(normalized)
}

// normalizeOne applies all normalization steps to a single ToolCall.
func (n *ToolCallNormalizer) normalizeOne(tc ToolCall) ToolCall {
	// 1. Strip <|channel|> suffix from tool name.
	tc.Function.Name = n.stripChannelSuffix(tc.Function.Name)

	// 2. Generate missing ID.
	if tc.ID == "" {
		tc.ID = n.generateID(tc.Function.Name)
	}

	// 3. Repair JSON arguments.
	tc.Function.Arguments = n.repairJSON(tc.Function.Arguments)

	// 4. Normalize Type to "function".
	tc.Type = "function"

	return tc
}

// stripChannelSuffix removes the <|channel|> suffix (e.g., "<|channel|>0",
// "<|channel|>12") from tool names. Some models append this suffix to
// indicate the output channel.
func (n *ToolCallNormalizer) stripChannelSuffix(name string) string {
	idx := strings.Index(name, "<|channel|>")
	if idx == -1 {
		return name
	}
	return name[:idx]
}

// generateID creates a synthetic ID for a tool call that is missing one.
// The format is "call_{name}_{unix_nano}_{seq}" where seq is a monotonically
// increasing counter to guarantee uniqueness even under rapid normalization.
func (n *ToolCallNormalizer) generateID(name string) string {
	seq := atomic.AddUint64(&n.seq, 1)
	return fmt.Sprintf("call_%s_%d_%d", name, time.Now().UnixNano(), seq)
}

// repairJSON takes a JSON string and returns a canonical compact version.
// If the input is empty, it returns "{}". If the input is not valid JSON
// but can be repaired (e.g., trailing commas), it attempts repair.
// Otherwise, returns the input as-is so the caller can decide whether to
// drop the call.
func (n *ToolCallNormalizer) repairJSON(args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return "{}"
	}

	// Already valid JSON — canonicalize to compact form.
	if json.Valid([]byte(args)) {
		return n.canonicalize(args)
	}

	// Attempt repair strategies.
	if repaired := n.tryRepair(args); repaired != "" {
		return repaired
	}

	// Could not repair; return original so the caller can decide whether to drop.
	return args
}

// tryRepair attempts common JSON repair strategies:
//
//  1. Remove trailing commas before } or ]
//  2. Wrap bare key-value content in braces
func (n *ToolCallNormalizer) tryRepair(args string) string {
	// Strategy 1: Remove trailing commas before } or ].
	fixed := strings.ReplaceAll(args, ",}", "}")
	fixed = strings.ReplaceAll(fixed, ",]", "]")
	if json.Valid([]byte(fixed)) {
		return n.canonicalize(fixed)
	}

	// Strategy 2: Wrap bare key-value content in braces.
	trimmed := strings.TrimSpace(args)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		wrapped := "{" + trimmed + "}"
		if json.Valid([]byte(wrapped)) {
			return n.canonicalize(wrapped)
		}
	}

	return ""
}

// canonicalize re-marshals valid JSON into compact canonical form,
// preserving numeric precision via json.Number. It expects a single
// JSON value (enforced by the caller via json.Valid).
func (n *ToolCallNormalizer) canonicalize(s string) string {
	var parsed any
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	if err := dec.Decode(&parsed); err != nil {
		return s
	}
	out, err := json.Marshal(parsed)
	if err != nil {
		return s
	}
	return string(out)
}

// deduplicate removes duplicate tool calls based on ID+arguments.
// The first occurrence is kept; subsequent duplicates are dropped.
//
// The dedup key uses \x00 as a separator between ID and arguments.
// This is safe because tool call IDs and JSON argument strings cannot
// contain null bytes (JSON spec prohibits raw control characters).
func (n *ToolCallNormalizer) deduplicate(calls []ToolCall) NormalizedToolCalls {
	if len(calls) == 0 {
		return nil
	}
	if len(calls) == 1 {
		return NormalizedToolCalls(calls)
	}

	seen := make(map[string]bool, len(calls))
	result := make(NormalizedToolCalls, 0, len(calls))
	for _, tc := range calls {
		key := tc.ID + "\x00" + tc.Function.Arguments
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, tc)
	}
	return result
}
