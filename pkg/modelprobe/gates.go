package modelprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// fastGate is a single-turn capability check: present system+user+tools, then
// validate ONLY the model's first response. No tool execution, no follow-up.
type fastGate struct {
	name     string
	system   string
	user     string
	tools    []api.Tool
	expect   string // human-readable expectation, used in the fail reason
	validate func(msg api.Message) bool
}

// fastGates is the battery of fast-fail checks every usable model must pass.
// They probe distinct, decisive capabilities in a single response each:
// emitting a tool call, using sprout's bespoke repo_map tool, selecting the
// right tool among several, producing a correctly-computed argument, and obeying
// an ordering constraint stated in the system prompt.
func fastGates() []fastGate {
	return []fastGate{
		{
			name:   "emits_tool_call",
			system: "You are a coding assistant with file tools. Act using the tools; do not answer in prose.",
			user:   "Open and read the file `config.json` so you can inspect it.",
			tools:  []api.Tool{fn("read_file", "Read a file's contents.", props("path", "path to read"), "path")},
			expect: "call read_file with a path referencing config.json",
			validate: func(m api.Message) bool {
				a, ok := toolArgs(m, "read_file")
				return ok && strings.Contains(strings.ToLower(argString(a, "path")), "config")
			},
		},
		{
			name:   "uses_repo_map",
			system: "You are a coding agent. A `repo_map` tool returns a structural overview of a codebase (files and their top-level symbols). Prefer it to orient yourself before reading individual files.",
			user:   "You've just been dropped into an unfamiliar repository. Before reading any individual file, get an overview of the codebase's structure.",
			tools: []api.Tool{
				fn("repo_map", "Generate a lightweight overview of the codebase: file paths and top-level symbols with line numbers. Use before reading files to find what's relevant.", props("directory", "directory to scan (default: .)")),
				fn("read_file", "Read a file's contents.", props("path", "path to read"), "path"),
			},
			expect: "call the bespoke repo_map tool (not read_file) to get an overview",
			validate: func(m api.Message) bool {
				return hasToolNamed(m, "repo_map")
			},
		},
		{
			name:   "selects_right_tool",
			system: "You have tools to navigate a codebase. Choose the most appropriate one.",
			user:   "I need to know which files reference the function `processPayment`. Use the right tool to find them.",
			tools: []api.Tool{
				fn("read_file", "Read a single known file's contents.", props("path", "path to read"), "path"),
				fn("write_file", "Overwrite a file.", props2("path", "path", "content", "content"), "path", "content"),
				fn("search_code", "Search the codebase for a string or symbol and return matching files.", props("query", "text or symbol to search for"), "query"),
			},
			expect: "call search_code with a query for processPayment (not read_file)",
			validate: func(m api.Message) bool {
				a, ok := toolArgs(m, "search_code")
				return ok && strings.Contains(strings.ToLower(argString(a, "query")), "processpayment")
			},
		},
		{
			name:   "computes_argument",
			system: "Respond by calling the tool with the correct, computed value. Do not ask questions.",
			user:   "The current version is `1.4.2`. Apply a semantic-versioning MINOR bump (increment the minor number, reset patch to 0) and call set_version with the resulting version.",
			tools:  []api.Tool{fn("set_version", "Set the project version.", props("version", "the new semantic version, e.g. 1.2.3"), "version")},
			expect: "call set_version with version 1.5.0",
			validate: func(m api.Message) bool {
				a, ok := toolArgs(m, "set_version")
				return ok && strings.TrimSpace(argString(a, "version")) == "1.5.0"
			},
		},
		{
			name:   "follows_ordering_constraint",
			system: "IMPORTANT: Before doing anything else, you MUST first call `acknowledge_task` with a one-sentence restatement of the task. Do not call any other tool until you have called acknowledge_task.",
			user:   "Refactor the auth module to remove the deprecated token-refresh path.",
			tools: []api.Tool{
				fn("acknowledge_task", "Acknowledge and restate the task before starting.", props("summary", "one-sentence restatement"), "summary"),
				fn("read_file", "Read a file's contents.", props("path", "path to read"), "path"),
				fn("edit_file", "Edit a file.", props2("path", "path", "patch", "edit to apply"), "path", "patch"),
			},
			expect: "make acknowledge_task the FIRST tool call, per the system prompt",
			validate: func(m api.Message) bool {
				return firstToolName(m) == "acknowledge_task"
			},
		},
	}
}

// runFastGates runs the gates in order and fast-fails at the first miss, so a
// weak model is rejected cheaply without running the remaining gates or the
// complex stage.
func runFastGates(ctx context.Context, client api.ClientInterface) tierOutcome {
	gates := fastGates()
	var st driveStats
	passed := 0
	for _, g := range gates {
		msgs := []api.Message{{Role: "system", Content: g.system}, {Role: "user", Content: g.user}}
		resp, err := client.SendChatRequest(ctx, msgs, g.tools, "", false)
		st.turns++
		if err != nil {
			st.err = err
			return tierOutcome{stats: st}
		}
		st.prompt += resp.Usage.PromptTokens
		st.compl += resp.Usage.CompletionTokens

		var msg api.Message
		if len(resp.Choices) > 0 {
			msg = resp.Choices[0].Message
		}
		traceTurn("gate:"+g.name, st.turns, resp, msg)
		if len(msg.ToolCalls) > 0 {
			st.anyTool = true
		}
		if !g.validate(msg) {
			return tierOutcome{
				score:  float64(passed) / float64(len(gates)),
				passed: false,
				reason: fmt.Sprintf("gate %q failed (expected: %s)", g.name, g.expect),
				stats:  st,
			}
		}
		passed++
	}
	return tierOutcome{score: 1.0, passed: true, reason: "all fast gates passed", stats: st}
}

// --- tool-definition + response-inspection helpers ---

func fn(name, desc string, properties map[string]api.ToolParameter, required ...string) api.Tool {
	return api.Tool{Type: "function", Function: api.ToolFunction{
		Name: name, Description: desc,
		Parameters: api.ToolParameters{Type: "object", Properties: properties, Required: required},
	}}
}

func props(name, desc string) map[string]api.ToolParameter {
	return map[string]api.ToolParameter{name: strParam(desc)}
}

func props2(n1, d1, n2, d2 string) map[string]api.ToolParameter {
	return map[string]api.ToolParameter{n1: strParam(d1), n2: strParam(d2)}
}

func hasToolNamed(m api.Message, name string) bool {
	for _, tc := range m.ToolCalls {
		if tc.Function.Name == name {
			return true
		}
	}
	return false
}

func firstToolName(m api.Message) string {
	if len(m.ToolCalls) > 0 {
		return m.ToolCalls[0].Function.Name
	}
	return ""
}

// toolArgs returns the parsed arguments of the first call to the named tool.
func toolArgs(m api.Message, name string) (map[string]any, bool) {
	for _, tc := range m.ToolCalls {
		if tc.Function.Name != name {
			continue
		}
		var a map[string]any
		if json.Unmarshal([]byte(tc.Function.Arguments), &a) != nil {
			return nil, false
		}
		return a, true
	}
	return nil, false
}

func argString(a map[string]any, key string) string {
	s, _ := a[key].(string)
	return s
}
