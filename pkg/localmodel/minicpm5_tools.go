//go:build darwin && arm64 && cgo

package localmodel

// MiniCPM5 tool-calling support: native format per the official chat_template.jinja.
//
// MiniCPM5 (a Llama-architecture model shipping model_type "llama") uses
// its own tool protocol:
//   - declarations: a <tools> block of JSON objects in the system prompt
//   - calls: <function name="…"><param name="k">v</param></function>
//   - responses: user-role message with <tool_response>…</tool_response>
//   - replay: prior assistant tool calls serialized to the native markup
//
// Unlike Qwen's XML format, the native format has no outer <tool_call>
// envelope and no <function=name> — the call is the whole thing.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sinter/llm"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// minicpm5Arch is the normalized architecture string used when the model
// is MiniCPM5 (detected by tokenizer, not by model_type).
const minicpm5Arch = "minicpm5"

// isMiniCPM5Tokenizer reports whether the tokenizer.json at path carries
// the /think and /no_think control tokens on consecutive ids — MiniCPM5's
// layout (ids 130080/130081 on the shipped model), stricter than sinter's
// own detector (llm.Tokenizer.isMiniCPM5 also checks for <|im_start|>, which
// Qwen shares) so a Qwen model whose /think ids sit apart stays on its own
// arch. config.json cannot be trusted: MiniCPM5 ships model_type "llama"
// (same as other Llama models) and its official MLX export has an empty {}
// config.json entirely.
func isMiniCPM5Tokenizer(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var tok struct {
		AddedTokens []struct {
			Content string `json:"content"`
			ID      int    `json:"id"`
		} `json:"added_tokens"`
	}
	if json.Unmarshal(data, &tok) != nil {
		return false
	}
	thinkID, noThinkID := -1, -1
	for _, t := range tok.AddedTokens {
		switch t.Content {
		case "/think":
			thinkID = t.ID
		case "/no_think":
			noThinkID = t.ID
		}
	}
	// MiniCPM5 emits the pair adjacently (ids 130080/130081 on the shipped
	// model); Qwen3's /think sits elsewhere in its added-token list. Both
	// tokens AND adjacency means a false positive needs a deliberate
	// imitation of the tokenizer layout, not a shared control token.
	return thinkID >= 0 && noThinkID == thinkID+1
}

// minicpm5Detector caches the tokenizer verdict per model directory: the
// tokenizer.json of a real model can be multiple MB, and buildPrompt,
// staticPromptPrefix and the tool-call parse path would otherwise re-read
// it several times per request. Keyed on (dir, mtime, size) so a model
// re-downloaded or repaired into the same directory re-detects instead of
// pinning a stale verdict for the process lifetime.
var minicpm5Detector = struct {
	sync.Mutex
	verdicts map[minicpm5DirKey]bool
}{verdicts: map[minicpm5DirKey]bool{}}

// minicpm5DirKey identifies one on-disk tokenizer.json state.
type minicpm5DirKey struct {
	dir   string
	mtime int64
	size  int64
}

// isMiniCPM5Model reports whether the model directory is a MiniCPM5 install.
func isMiniCPM5Model(dir string) bool {
	tokPath := filepath.Join(dir, "tokenizer.json")
	info, err := os.Stat(tokPath)
	if err != nil {
		return false
	}
	key := minicpm5DirKey{dir: dir, mtime: info.ModTime().UnixNano(), size: info.Size()}
	minicpm5Detector.Lock()
	defer minicpm5Detector.Unlock()
	if known, ok := minicpm5Detector.verdicts[key]; ok {
		return known
	}
	known := isMiniCPM5Tokenizer(tokPath)
	// Drop the dir's previous verdicts so the map holds one entry per dir,
	// not one per re-download's (mtime, size).
	for k := range minicpm5Detector.verdicts {
		if k.dir == dir && k != key {
			delete(minicpm5Detector.verdicts, k)
		}
	}
	minicpm5Detector.verdicts[key] = known
	return known
}

// effectiveArch returns the normalized arch string used for tool-call
// handling. MiniCPM5 (a llama-architecture model) is detected by its
// tokenizer, so the caller passes cfg.Arch plus the model dir to decide.
func effectiveArch(cfg llm.ModelConfig, dir string) string {
	if isMiniCPM5Model(dir) {
		return minicpm5Arch
	}
	return cfg.Arch
}

// ─── Prompt formatting ──────────────────────────────────────────────────

// formatMiniCPM5ToolsPrompt renders the MiniCPM5 tool-calling system block:
// a <tools> JSON envelope in the system prompt with the template's own
// tool-usage guidelines.
func formatMiniCPM5ToolsPrompt(tools []api.Tool) string {
	var sb strings.Builder
	sb.WriteString("<|im_start|>system\n# Tools\n\nYou are provided with function signatures within <tools></tools> XML tags:\n<tools>")
	for _, tool := range tools {
		sb.WriteString("\n")
		sb.WriteString(miniCPM5ToolDeclJSON(tool))
	}
	sb.WriteString("\n</tools>\n\nTool usage guidelines:\n- You may call zero or more functions. If no function calls are needed, just answer normally and do not include any <function ... </function>.\n- When calling a function, return an XML object within <function ... </function> using:\n<function name=\"function-name\"><param name=\"param-name\">param-value</param></function>\n- param-value may be multi-line. If it contains <, & or newline characters, wrap it in a CDATA block: <param name=\"param-name\"><![CDATA[...multi-line value...]]></param>")
	sb.WriteString("<|im_end|>\n")
	return sb.String()
}

// miniCPM5ToolDeclJSON renders one tool declaration as the JSON envelope
// MiniCPM5's template serializes with tojson.
func miniCPM5ToolDeclJSON(tool api.Tool) string {
	propsMap, required := miniCPM5Parameters(tool.Function.Parameters)
	type fn struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  struct {
			Type       string                   `json:"type"`
			Properties map[string]miniCPM5Param `json:"properties"`
			Required   []string                 `json:"required,omitempty"`
		} `json:"parameters"`
	}
	type decl struct {
		Type     string `json:"type"`
		Function fn     `json:"function"`
	}
	var d decl
	d.Type = "function"
	d.Function.Name = tool.Function.Name
	d.Function.Description = tool.Function.Description
	d.Function.Parameters.Type = "object"
	d.Function.Parameters.Properties = propsMap
	d.Function.Parameters.Required = required
	b, err := json.Marshal(d)
	if err != nil {
		return fmt.Sprintf(`{"type":"function","function":{"name":%q,"description":%q}}`, tool.Function.Name, tool.Function.Description)
	}
	return string(b)
}

// miniCPM5Param describes one parameter's type and description.
type miniCPM5Param struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// miniCPM5Parameters extracts the properties (type+description per name)
// and required list from a tool's parameters schema.
func miniCPM5Parameters(params interface{}) (map[string]miniCPM5Param, []string) {
	out := map[string]miniCPM5Param{}
	var required []string
	if params == nil {
		return out, required
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return out, required
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return out, required
	}
	if props, ok := m["properties"].(map[string]interface{}); ok {
		for k, v := range props {
			p := miniCPM5Param{}
			if pm, ok := v.(map[string]interface{}); ok {
				if t, ok := pm["type"].(string); ok {
					p.Type = t
				}
				if d, ok := pm["description"].(string); ok {
					p.Description = d
				}
			}
			out[k] = p
		}
	}
	if req, ok := m["required"].([]interface{}); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				required = append(required, s)
			}
		}
	}
	return out, required
}

// ─── Tool-call parsing ────────────────────────────────────────────────

// minicpm5WalkRe matches a CDATA block, a param open/close, or a call
// open/close, so findMiniCPM5Call can walk the markup with a small state
// machine: inside an open <param>'s value (CDATA or plain), "</function>" is
// the value's text, not the call's close. Leftmost-first matching gives the
// CDATA block priority over tags inside it. Hoisted: it runs on every parse.
var (
	minicpm5OpenRe = regexp.MustCompile(`<function\s+name="[A-Za-z0-9_.-]+"\s*>`)
	minicpm5WalkRe = regexp.MustCompile(`<!\[CDATA\[.*?\]\]>|<param\s+name="[A-Za-z0-9_.-]+"\s*>|</param>|</function>|<function\s+name="[A-Za-z0-9_.-]+"\s*>`)
)

// minicpm5ParamRe matches one <param name="k">v</param>. CDATA optional;
// the CDATA alternative is greedy-free but distinguished from the plain
// alternative by which submatch participated (index 2 vs 3) — see
// parseMiniCPM5ParamValue's caller. An empty CDATA body must match here,
// not fall through to the plain branch.
var minicpm5ParamRe = regexp.MustCompile(`(?s)<param\s+name="([A-Za-z0-9_.-]+)"\s*>\s*(?:<!\[CDATA\[(.*?)\]\]>|(.*?))</param>`)

// parseMiniCPM5ToolCalls finds every <function name="…">…</function> block
// in model output, returning the remaining visible content and the calls.
func parseMiniCPM5ToolCalls(text string) (string, []api.ToolCall) {
	if !strings.Contains(text, "<function") {
		return text, nil
	}
	var plain strings.Builder
	rest := text
	var calls []api.ToolCall
	for {
		start, end := findMiniCPM5Call(rest)
		if start < 0 {
			plain.WriteString(rest)
			break
		}
		plain.WriteString(rest[:start])
		body := rest[start:end]
		if call, ok := parseMiniCPM5CallBody(body); ok {
			calls = append(calls, call)
		}
		if end >= len(rest) {
			break
		}
		rest = rest[end:]
	}
	return strings.TrimSpace(plain.String()), calls
}

// findMiniCPM5Call locates the next <function name="…"> opening and its
// matching </function> close. A literal "</function>" inside a param value —
// a model writing code — must not end the call, so the walk tracks state:
// a CDATA block is opaque, and inside an open <param>'s value (CDATA or
// plain) tags are the value's text until </param>. depth counts unclosed
// call opens; end points just past the matching </function> (or len(s) for
// an unterminated call).
func findMiniCPM5Call(s string) (start, end int) {
	open := minicpm5OpenRe.FindStringIndex(s)
	if open == nil {
		return -1, -1
	}
	start = open[0]
	depth := 1
	base := open[1] // rest's offset within s
	rest := s[base:]
	var paramDepth int
	for depth > 0 {
		loc := minicpm5WalkRe.FindStringSubmatchIndex(rest)
		if loc == nil {
			return start, len(s)
		}
		tag := rest[loc[0]:loc[1]]
		absEnd := base + loc[1]
		rest = rest[loc[1]:]
		base += loc[1]
		switch {
		case strings.HasPrefix(tag, "<![CDATA["):
			// A param value's CDATA block: opaque, its tags are value text.
			// It sits inside an already-counted <param> open, so depth is
			// unchanged.
		case strings.HasPrefix(tag, "<param"):
			// param open: tags until its </param> are value text
			paramDepth++
		case strings.HasPrefix(tag, "</param"):
			if paramDepth > 0 {
				paramDepth--
			}
		case paramDepth > 0:
			// a </function> or <function…> inside a param value is text
		case strings.HasPrefix(tag, "</function>"):
			depth--
			if depth == 0 {
				return start, absEnd
			}
		default:
			depth++
		}
	}
	return start, len(s)
}

// miniCPM5ArgsFromParams extracts name→value pairs from minicpm5ParamRe
// matches (FindAllStringSubmatchIndex + FindAllStringSubmatch over the same
// body). Group 2 is the CDATA branch, group 3 the plain one; which branch
// participated is decided by the submatch's offsets (non-negative when the
// group took part), not by emptiness — an empty CDATA body must stay CDATA,
// and a plain value containing the literal "<![CDATA[" must stay plain.
// The split-CDATA escape for a literal "]]>" (see miniCPM5RenderCall) is
// decoded back here, per XML's CDATA semantics.
func miniCPM5ArgsFromParams(locs [][]int, all [][]string) map[string]interface{} {
	args := map[string]interface{}{}
	for i, loc := range locs {
		m := all[i]
		val := m[3]
		if loc[4] >= 0 && loc[5] >= 0 {
			val = strings.ReplaceAll(m[2], "]]]]><![CDATA[>", "]]>")
		}
		args[m[1]] = parseMiniCPM5ParamValue(val)
	}
	return args
}

// parseMiniCPM5CallBody parses a single <function name="…">…</function> body:
// the name from the open tag, params from the interior up to the matching
// close (findMiniCPM5Call already resolved which close that is, so a literal
// "</function>" inside a param value does not truncate the args).
func parseMiniCPM5CallBody(body string) (api.ToolCall, bool) {
	trimmed := strings.TrimSpace(body)
	open := minicpm5OpenRe.FindStringIndex(trimmed)
	if open == nil {
		return api.ToolCall{}, false
	}
	rel := trimmed[open[0]:open[1]]
	relName := rel[strings.Index(rel, `"`)+1:]
	nameEnd := strings.Index(relName, `"`)
	if nameEnd < 0 {
		return api.ToolCall{}, false
	}
	name := relName[:nameEnd]
	if name == "" {
		return api.ToolCall{}, false
	}
	afterOpen := trimmed[open[1]:]
	if close := strings.LastIndex(afterOpen, "</function>"); close >= 0 {
		afterOpen = afterOpen[:close]
	}
	locs := minicpm5ParamRe.FindAllStringSubmatchIndex(afterOpen, -1)
	strs := minicpm5ParamRe.FindAllStringSubmatch(afterOpen, -1)
	return miniCPM5ToolCall(name, miniCPM5ArgsFromParams(locs, strs)), true
}

// parseMiniCPM5ParamValue coerces a raw param string to a JSON-typed value:
// numbers and booleans become typed; everything else stays a string.
func parseMiniCPM5ParamValue(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		if n == float64(int64(n)) {
			return int64(n)
		}
		return n
	}
	// Try JSON marshal/unmarshal for structured values (arrays, objects).
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		return v
	}
	return s
}

// miniCPM5ToolCall builds an api.ToolCall from a name + args map.
func miniCPM5ToolCall(name string, args map[string]interface{}) api.ToolCall {
	argsJSON, _ := json.Marshal(args)
	return api.ToolCall{
		ID:   fmt.Sprintf("call_minicpm5_%d", time.Now().UnixNano()),
		Type: "function",
		Function: api.ToolCallFunction{
			Name:      name,
			Arguments: string(argsJSON),
		},
	}
}

// ─── Assistant tool-call replay ─────────────────────────────────────────

// formatMiniCPM5AssistantToolCalls renders prior assistant tool_calls in
// MiniCPM5's native format for conversation history replay. Multiple calls
// concatenate, matching the template's tool_calls loop.
func formatMiniCPM5AssistantToolCalls(toolCalls []api.ToolCall) string {
	var sb strings.Builder
	for _, tc := range toolCalls {
		args := map[string]interface{}{}
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		sb.WriteString(miniCPM5RenderCall(tc.Function.Name, args))
	}
	return sb.String()
}

// miniCPM5RenderCall renders one call as <function name="…"><param …/></function>.
// Values containing <, & or newlines are wrapped in CDATA, per the template's
// rule. CDATA cannot contain "]]>", so a value carrying it is split as
// ]]]]><![CDATA[> — the standard XML escape — keeping replay parseable.
func miniCPM5RenderCall(name string, args map[string]interface{}) string {
	var sb strings.Builder
	sb.WriteString(`<function name="` + name + `">`)
	for _, k := range sortedInterfaceKeys(args) {
		v := args[k]
		val := miniCPM5RenderValue(v)
		if strings.ContainsAny(val, "<&\n") || strings.Contains(val, "]]>") {
			sb.WriteString(`<param name="` + k + `"><![CDATA[` +
				strings.ReplaceAll(val, "]]>", "]]]]><![CDATA[>") + `]]></param>`)
		} else {
			sb.WriteString(`<param name="` + k + `">` + val + `</param>`)
		}
	}
	sb.WriteString(`</function>`)
	return sb.String()
}

// miniCPM5RenderValue renders an argument value as a plain string.
func miniCPM5RenderValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case nil:
		return ""
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'g', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	default:
		j, _ := json.Marshal(val)
		return string(j)
	}
}

// sortedInterfaceKeys returns the keys of a map[string]interface{} sorted.
func sortedInterfaceKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ─── Tool response ──────────────────────────────────────────────────────

// convertMiniCPM5ToolResponse wraps a tool result in MiniCPM5's user-role
// envelope: <tool_response>…</tool_response> inside a user message.
func convertMiniCPM5ToolResponse(content string) llm.ChatMessage {
	return llm.ChatMessage{Role: "user", Content: "<tool_response>\n" + content + "\n</tool_response>"}
}
