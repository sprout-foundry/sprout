package providers

import (
	"strings"
)

// specialTokenNeutralizations maps control-token literals that appear in
// tool output and model prose to inert look-alikes. When a provider's
// tokenizer assigns special meaning to these exact byte sequences
// (Qwen: <|im_start|>/<|im_end|>/<|endoftext|>, Llama: <|eot_id|>),
// replaying them verbatim in message content lets the tokenizer encode
// them as the actual control-token IDs. That both corrupts what the
// model reads (the server may skip rendering them) and — for tokens
// equal to the model's EOS — can terminate a *new* generation the
// moment the model emits one, killing responses mid-sentence.
//
// The mapped substitutions are visually near-identical (Unicode angle
// brackets instead of ASCII) so the model can still recognize and
// reason about the tokens it is being shown; only the exact byte
// sequence is changed, which is the thing that mattered.
var specialTokenNeutralizations = map[string]string{
	"<|im_start|>":  "⟨im_start⟩",
	"<|im_end|>":    "⟨im_end⟩",
	"<|endoftext|>": "⟨endoftext⟩",
	"<|tool_call|>": "⟨tool_call⟩",
	"<|eot_id|>":    "⟨eot_id⟩",
	"<|channel|>":   "⟨channel⟩",
}

// NeutralizeSpecialTokens replaces provider special-token literals in
// message content with inert look-alikes. It deliberately never touches
// tool-call arguments: arguments carry the work product (file contents
// the model is writing), and corrupting those corrupts the user's code.
// The model physically cannot emit the real token IDs in its own output
// (the serving stack renders them out or terminates on them), so
// arguments are not a re-priming vector.
func NeutralizeSpecialTokens(s string) string {
	if !strings.Contains(s, "<|") {
		return s
	}
	for raw, neutral := range specialTokenNeutralizations {
		s = strings.ReplaceAll(s, raw, neutral)
	}
	return s
}

// neutralizeSpecialTokensInConverted applies token neutralization to the
// already-converted wire messages. Runs before the repair passes so
// content comparisons during repair operate on the same neutralized
// form. Only "content" fields of user/tool/assistant messages are
// touched; tool_calls are skipped (arguments are the work product).
func neutralizeSpecialTokensInConverted(converted []map[string]interface{}) []map[string]interface{} {
	changed := false
	for i := range converted {
		role, _ := converted[i]["role"].(string)
		if role == "system" {
			continue
		}
		content, ok := converted[i]["content"].(string)
		if !ok {
			continue
		}
		neutral := NeutralizeSpecialTokens(content)
		if neutral == content {
			continue
		}
		if !changed {
			// Copy-on-write: preserve the caller's slice until first change.
			cp := make([]map[string]interface{}, len(converted))
			copy(cp, converted)
			converted = cp
			changed = true
		}
		converted[i]["content"] = neutral
	}
	return converted
}
