package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// qwenLeakedMarkup is the production shape observed when a Qwen-family
// checkpoint narrates a tool call as text (qwen3.8-27b via NInfer): prose,
// a reasoning-tag remnant, then a <tool_call>-wrapped block whose parameters
// use the tag-suffix spelling and whose function closer omits the name.
const qwenLeakedMarkup = `The full suite is running in the background. While it runs, let me dispatch a read-only review: </thinking>

<tool_call> <function=run_subagent> <parameter=persona> researcher </parameter> <parameter=prompt> READ-ONLY review. Do NOT edit any files. </parameter> <parameter=context> Key files: src/utils/e2eFlag.js, metro.config.js </parameter> </function> </tool_call>`

func qwenTestParser() *FallbackParser {
	return NewFallbackParser(FallbackParserOptions{
		KnownToolNames: func(name string) bool { return name == "run_subagent" },
	})
}

func mustRecoverSingle(t *testing.T, content string) ToolCall {
	t.Helper()
	res := qwenTestParser().Parse(content)
	if res == nil {
		t.Fatal("parse result is nil")
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d: %+v", len(res.ToolCalls), res.ToolCalls)
	}
	return res.ToolCalls[0]
}

// The leaked markup must recover into a structured call with correctly typed
// arguments, and the cleaned content must not retain any of the markup
// (wrapper, function tags, parameter tags).
func TestFallbackParsesQwenLeakedMarkup(t *testing.T) {
	tc := mustRecoverSingle(t, qwenLeakedMarkup)
	if tc.Function.Name != "run_subagent" {
		t.Fatalf("name = %q, want run_subagent", tc.Function.Name)
	}
	if tc.Type != "function" {
		t.Errorf("type = %q, want function", tc.Type)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not valid JSON: %q: %v", tc.Function.Arguments, err)
	}
	if args["persona"] != "researcher" {
		t.Errorf("persona = %v (%T), want researcher", args["persona"], args["persona"])
	}
	if args["prompt"] != "READ-ONLY review. Do NOT edit any files." {
		t.Errorf("prompt = %v", args["prompt"])
	}

	res := qwenTestParser().Parse(qwenLeakedMarkup)
	for _, residue := range []string{"<tool_call>", "</tool_call>", "<function=", "</function", "<parameter", "</parameter>"} {
		if strings.Contains(res.CleanedContent, residue) {
			t.Errorf("cleaned content retains %q: %q", residue, res.CleanedContent)
		}
	}
}

// Bare </function> closer without the tool name must not swallow subsequent
// content: a second block after the first must still be extracted.
func TestFallbackXMLBareCloserStopsAtBlockEnd(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{
		KnownToolNames: func(name string) bool { return name == "run_subagent" || name == "search" },
	})
	content := "<function=search>\n<parameter=query>hello</parameter>\n</function>\n\n<function=run_subagent>\n<parameter=persona>coder</parameter>\n</function>"
	res := fp.Parse(content)
	if len(res.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d: %+v", len(res.ToolCalls), res.ToolCalls)
	}
	if res.ToolCalls[0].Function.Name != "search" || res.ToolCalls[1].Function.Name != "run_subagent" {
		t.Fatalf("order wrong: %+v", res.ToolCalls)
	}
}

// The tag-suffix parameter spelling must coexist with the quoted-attribute
// spelling in one block, and named closers must keep working.
func TestFallbackXMLParameterSpellingsCoexist(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{
		KnownToolNames: func(name string) bool { return name == "run_command" },
	})
	content := "<function=run_command>\n<parameter=mode>fast</parameter>\n<parameter type=\"string\" name=\"command\">ls -la</parameter>\n</function=run_command>"
	res := fp.Parse(content)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d: %+v", len(res.ToolCalls), res.ToolCalls)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(res.ToolCalls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not valid JSON: %q: %v", res.ToolCalls[0].Function.Arguments, err)
	}
	if args["mode"] != "fast" {
		t.Errorf("mode = %v, want fast", args["mode"])
	}
	if args["command"] != "ls -la" {
		t.Errorf("command = %v, want ls -la", args["command"])
	}
}

// A leaked block naming a tool that was not offered must not become a call
// (the KnownToolNames gate). At runtime conversation.go only consumes the
// result when ToolCalls is non-empty, so the original content still reaches
// the user even though CleanedContent strips the block here.
func TestFallbackXMLUnknownToolNotRecovered(t *testing.T) {
	res := qwenTestParser().Parse("<tool_call><function=delete_everything><parameter=path>/</parameter></function></tool_call>")
	if len(res.ToolCalls) != 0 {
		t.Fatalf("unexpected recovery: %+v", res.ToolCalls)
	}
}

// Content after the </tool_call> wrapper must survive cleanup — the wrapper
// extension may only absorb tags that hug the block, never trailing prose.
func TestFallbackXMLContentAfterWrapperSurvives(t *testing.T) {
	content := "<tool_call><function=run_subagent><parameter=persona>coder</parameter></function></tool_call>\n\nDone dispatching."
	res := qwenTestParser().Parse(content)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(res.ToolCalls))
	}
	if !strings.Contains(res.CleanedContent, "Done dispatching.") {
		t.Errorf("trailing prose lost: %q", res.CleanedContent)
	}
}

// Empty parameter values are valid (the model omitted an optional arg) and
// must serialize as "" rather than dropping the key or failing extraction.
func TestFallbackXMLEmptyParameterValue(t *testing.T) {
	res := qwenTestParser().Parse("<function=run_subagent><parameter=persona>coder</parameter><parameter=context></parameter></function>")
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(res.ToolCalls))
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(res.ToolCalls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not valid JSON: %q", err)
	}
	if v, ok := args["context"]; !ok || v != "" {
		t.Errorf("context = %v (present=%v), want empty string present", v, ok)
	}
}

// Two sequential blocks each wrapped in their own <tool_call> pair — the
// common multi-call emission. Both must extract, in order.
func TestFallbackXMLTwoWrappedBlocks(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{
		KnownToolNames: func(name string) bool { return name == "search" || name == "run_subagent" },
	})
	content := "<tool_call><function=search><parameter=query>a</parameter></function></tool_call>\n<tool_call><function=run_subagent><parameter=persona>coder</parameter></function></tool_call>"
	res := fp.Parse(content)
	if len(res.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d: %+v", len(res.ToolCalls), res.ToolCalls)
	}
	if res.ToolCalls[0].Function.Name != "search" || res.ToolCalls[1].Function.Name != "run_subagent" {
		t.Fatalf("wrong extraction order: %+v", res.ToolCalls)
	}
}
