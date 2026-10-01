package providers

// generic_provider_messages_cleanup.go — the post-construction cleanup
// passes over the provider wire map: dropOrphanToolResults, stripUnansweredToolCalls,
// the assistant merge / drop-empty passes, the tool-image delivery insertion,
// and the map helpers. Split out of generic_provider_messages.go.

import (
	"fmt"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// dropOrphanToolResults walks a converted message slice and drops any
// tool-role message whose tool_call_id is not declared by the nearest
// preceding assistant message's tool_calls block. Used as a final
// invariant guard before sending to strict-syntax providers. Does NOT
// drop the parent assistant message — providers handle an empty trailing
// assistant fine, and dropping it risks removing important context (tool
// calls the model already saw, plus any reasoning attached to it).
//
// Parallel tool calls: an assistant may issue multiple tool_calls at once
// (e.g. c1, c2). The results come back as a contiguous block of tool
// messages: tool(c1), tool(c2). For the second tool message, the
// immediately-preceding entry is another tool message, not the assistant.
// The correct check is: walk backward past any consecutive tool messages
// to find the nearest assistant, then match tool_call_id against that
// assistant's tool_calls list.
func dropOrphanToolResults(converted []map[string]interface{}) []map[string]interface{} {
	if len(converted) == 0 {
		return converted
	}
	out := make([]map[string]interface{}, 0, len(converted))
	for i, entry := range converted {
		role, _ := entry["role"].(string)
		if role != "tool" {
			out = append(out, entry)
			continue
		}
		// An empty tool_call_id means the message can't be matched to a
		// specific parent — preserve it rather than treating it as a
		// guaranteed orphan. (Misconfigured providers with
		// IncludeToolCallID=false fall in this case; we don't want to
		// strip their tool results.)
		want, _ := entry["tool_call_id"].(string)
		if want == "" {
			out = append(out, entry)
			continue
		}
		// Find the nearest assistant by walking backward past any
		// interleaved tool messages (parallel tool-call block).
		orphan := true
		for j := i - 1; j >= 0; j-- {
			prev := converted[j]
			prevRole, _ := prev["role"].(string)
			if prevRole == "assistant" {
				tcs, _ := prev["tool_calls"].([]map[string]interface{})
				for _, tc := range tcs {
					if id, _ := tc["id"].(string); id == want {
						orphan = false
					}
				}
				break // matched or not; stop at the first assistant
			}
			if prevRole != "tool" {
				break // any other role boundary (user/system/...) breaks the block
			}
		}
		if orphan {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// stripUnansweredToolCalls removes tool_calls from assistant messages whose
// tool_call IDs have no corresponding tool result in the immediately
// following messages. Strict-syntax providers (MiniMax, DeepSeek) reject
// requests where an assistant message declares tool_calls but no tool
// results follow — HTTP 400 error 2013 "tool call result does not follow
// tool call". This happens when checkpoint compaction or structural
// compaction consumes tool results but leaves their parent assistant
// messages in the history.
//
// The function walks forward from each assistant+tool_calls message,
// collecting following tool results (contiguous block). If not every
// tool_call ID has a result, the tool_calls are stripped so the provider
// doesn't reject the message. The assistant content is preserved so the
// model retains the assistant's text contribution. Must run BEFORE
// dropOrphanToolResults — stripping tool_calls from a partial-parallel
// block can leave previously-matched tool results orphaned, and the
// dropOrphanToolResults pass cleans those up.
func stripUnansweredToolCalls(converted []map[string]interface{}) []map[string]interface{} {
	if len(converted) == 0 {
		return converted
	}

	result := make([]map[string]interface{}, 0, len(converted))

	for i := 0; i < len(converted); i++ {
		entry := converted[i]
		role, _ := entry["role"].(string)
		if role != "assistant" {
			result = append(result, entry)
			continue
		}

		tcs, hasTCs := entry["tool_calls"].([]map[string]interface{})
		if !hasTCs || len(tcs) == 0 {
			result = append(result, entry)
			continue
		}

		// Collect tool_call IDs declared by this assistant message.
		declaredIDs := make(map[string]bool, len(tcs))
		for _, tc := range tcs {
			if id, _ := tc["id"].(string); id != "" {
				declaredIDs[id] = false // false = no result found yet
			}
		}
		if len(declaredIDs) == 0 {
			result = append(result, entry)
			continue
		}

		// Scan forward through the contiguous tool-result block. Providers
		// with ConvertToolRoleToUser emit tool results as role "user" with
		// a tool_call_id field, so check both roles.
		for j := i + 1; j < len(converted); j++ {
			nextRole, _ := converted[j]["role"].(string)
			if nextRole == "tool" {
				// Standard tool role — always part of the result block.
			} else if nextRole == "user" {
				// ConvertToolRoleToUser case — only treat as a result if
				// it carries a tool_call_id. A plain user message ends the block.
				if _, hasTCID := converted[j]["tool_call_id"]; !hasTCID {
					break
				}
			} else {
				break
			}
			if tid, _ := converted[j]["tool_call_id"].(string); tid != "" {
				if _, ok := declaredIDs[tid]; ok {
					declaredIDs[tid] = true
				}
			}
		}

		// Check if all declared IDs have results.
		allAnswered := true
		for _, answered := range declaredIDs {
			if !answered {
				allAnswered = false
				break
			}
		}

		if allAnswered {
			result = append(result, entry)
			continue
		}

		// Some tool_calls are unanswered. Strip the tool_calls field
		// so the provider doesn't reject the message. Keep the content
		// so the assistant's text contribution survives.
		stripped := make(map[string]interface{}, len(entry))
		for k, v := range entry {
			if k == "tool_calls" {
				continue
			}
			stripped[k] = v
		}
		result = append(result, stripped)
	}

	return result
}

// mergeConsecutiveAssistants merges back-to-back assistant messages into one,
// combining their content and keeping the tool_calls from whichever messages
// still have them. After stripUnansweredToolCalls removes orphaned tool_calls
// from earlier assistant messages, the conversation can contain two or more
// consecutive assistant entries — e.g. an earlier one whose calls were
// stripped (content only) followed by a later one with live tool_calls.
//
// This also handles the IncludeToolCallID=false case where ID-based matching
// in stripUnansweredToolCalls is a no-op: when two assistants each declare
// tool_calls with no tool results between them, the first one's calls are
// inherently orphaned. The merge drops the first's tool_calls and keeps the
// second's (which still has its results following).
//
// Empty assistants (no content AND no tool_calls) are dropped entirely —
// they carry no information and create role-alternation violations.
//
// The function works on a copy; the caller's slice is never mutated.
func mergeConsecutiveAssistants(converted []map[string]interface{}) []map[string]interface{} {
	if len(converted) == 0 {
		return converted
	}

	result := make([]map[string]interface{}, 0, len(converted))

	for _, entry := range converted {
		role, _ := entry["role"].(string)

		// If this is an assistant and the previous result entry is also
		// an assistant, merge into the previous instead of appending.
		if role == "assistant" && len(result) > 0 {
			prevRole, _ := result[len(result)-1]["role"].(string)
			if prevRole == "assistant" {
				prev := result[len(result)-1]
				mergeAssistantInto(prev, entry)
				continue
			}
		}

		// Non-assistant, or first message — append a copy.
		result = append(result, copyMap(entry))
	}

	// Second pass: drop assistant messages that ended up with no content
	// and no tool_calls after merging.
	result = dropEmptyAssistants(result)

	return result
}

// mergeAssistantInto folds src into dst (both role "assistant"). Content is
// concatenated with a newline separator (skipping empty/whitespace-only
// content). When consecutive assistant messages appear, the earlier one's
// tool_calls are inherently orphaned (no tool results exist between the two
// assistants), so src's tool_calls always replace dst's. Reasoning fields are
// kept from whichever side has them, preferring the earlier (original
// reasoning context).
func mergeAssistantInto(dst, src map[string]interface{}) {
	// Merge content.
	dstContent, _ := dst["content"].(string)
	srcContent, _ := src["content"].(string)
	if strings.TrimSpace(srcContent) != "" {
		if strings.TrimSpace(dstContent) != "" {
			dst["content"] = dstContent + "\n" + srcContent
		} else {
			dst["content"] = srcContent
		}
	}

	// Tool calls: the earlier assistant's (dst) are orphaned — no tool
	// results exist between consecutive assistants. Always replace with
	// src's if src has any; otherwise strip dst's.
	srcTCs, srcHas := src["tool_calls"].([]map[string]interface{})
	if srcHas && len(srcTCs) > 0 {
		dst["tool_calls"] = srcTCs
	} else {
		// Neither side should have orphaned tool_calls after merge.
		delete(dst, "tool_calls")
	}

	// Preserve reasoning from src if dst lacks it.
	for _, key := range []string{"reasoning_content", "reasoning", "reasoning_details"} {
		if _, ok := dst[key]; !ok {
			if v, ok := src[key]; ok {
				dst[key] = v
			}
		}
	}
}

// dropEmptyAssistants removes assistant entries that have neither content nor
// tool_calls after the merge pass. These are purely noise — typically the
// remnants of an assistant message whose tool_calls were stripped and whose
// content was empty (a common pattern for Qwen models that emit only "\n\n").
func dropEmptyAssistants(converted []map[string]interface{}) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(converted))
	for _, entry := range converted {
		role, _ := entry["role"].(string)
		if role == "assistant" {
			content, _ := entry["content"].(string)
			tcs, _ := entry["tool_calls"].([]map[string]interface{})
			if strings.TrimSpace(content) == "" && len(tcs) == 0 {
				continue
			}
		}
		result = append(result, entry)
	}
	return result
}

func copyMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// toolImageBatch records one tool result's images and the converted-index
// position the delivery message should take (right after that tool message).
type toolImageBatch struct {
	afterIndex int
	images     []api.ImageData
}

// insertToolResultImageDeliveries delivers images attached to tool results
// as user messages at their historical positions — one delivery per tool
// batch, inserted directly after the tool run it belongs to. Strict
// backends require string content on tool messages ("tool messages must
// contain string content"), so the pixels cannot ride inside the tool
// message itself; a follow-up user turn is the OpenAI-compat delivery
// pattern. Positional insertion keeps the wire prefix cache-stable across
// iterations: a delivery emitted in iteration N never moves when iteration
// N+1 appends more history, so provider prompt caching (vLLM auto-prefix,
// Anthropic cache_control) keeps hitting.
//
// Seed strips images for non-vision models upstream, so images arriving
// here are bound for a multimodal model; a provider that still rejects
// them self-heals via reconcileVisionCapability (strip + one retry +
// learned known-false).
func (p *GenericProvider) insertToolResultImageDeliveries(converted []map[string]interface{}, batches []toolImageBatch) []map[string]interface{} {
	if len(batches) == 0 {
		return converted
	}

	caps := api.VisionCapabilitiesOrDefault(p.VisionCapabilities())

	// Batches are recorded in emission order; deliver back-to-front so
	// earlier afterIndex values stay valid as later insertions grow the
	// slice. Adjacent batches (afterIndex differs by exactly 1 — nothing
	// but the next tool result between them) belong to ONE tool run: they
	// must merge into a single delivery after the run's last tool result.
	// Inserting between two results of the same run would break the strict
	// tool-result threading most backends enforce.
	type pendingDelivery struct {
		afterIndex int
		images     []api.ImageData
	}
	var deliveries []pendingDelivery
	for _, b := range batches {
		if n := len(deliveries); n > 0 && b.afterIndex-deliveries[n-1].afterIndex == 1 {
			deliveries[n-1].afterIndex = b.afterIndex
			deliveries[n-1].images = append(deliveries[n-1].images, b.images...)
			continue
		}
		deliveries = append(deliveries, pendingDelivery(b))
	}

	for i := len(deliveries) - 1; i >= 0; i-- {
		d := deliveries[i]
		images := d.images
		omitted := 0
		if caps.MaxImageCount > 0 && len(images) > caps.MaxImageCount {
			omitted = len(images) - caps.MaxImageCount
			images = images[len(images)-caps.MaxImageCount:]
		}
		note := "Images from the tool results above, in call order."
		if omitted > 0 {
			note += fmt.Sprintf(" %d older image(s) omitted to stay within the per-request image cap.", omitted)
		}

		insertAt := d.afterIndex
		// Two fold paths avoid inserting a user message adjacent to an
		// existing user message (the main loop merges those, but this
		// insertion runs post-loop):
		//
		//   1. ConvertToolRoleToUser providers render the tool result
		//      itself as a user message → fold into it (append note).
		//   2. A transient user message may directly follow the tool run
		//      ("Please continue…") → fold into it (prepend note).
		if insertAt > 0 {
			prev := converted[insertAt-1]
			if role, _ := prev["role"].(string); role == "user" {
				if s, ok := prev["content"].(string); ok {
					prev["content"] = p.buildMultiModalContent(s+"\n"+note, images)
					continue
				}
			}
		}
		if insertAt < len(converted) {
			next := converted[insertAt]
			if role, _ := next["role"].(string); role == "user" {
				if s, ok := next["content"].(string); ok {
					next["content"] = p.buildMultiModalContent(note+"\n"+s, images)
					continue
				}
			}
		}

		delivery := map[string]interface{}{
			"role":    "user",
			"content": p.buildMultiModalContent(note, images),
		}
		converted = append(converted, nil)
		copy(converted[insertAt+1:], converted[insertAt:])
		converted[insertAt] = delivery
	}
	return converted
}
