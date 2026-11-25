package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// antmlNS is the "antml:" namespace prefix some checkpoints put on tool-call
// tags. It is assembled from parts so the raw literal never appears verbatim
// in this source file.
var antmlNS = "ant" + "ml:"

// antmlTag renders a namespaced tag: antmlTag("parameter") -> "<antml:parameter".
func antmlTag(name string) string { return "<" + antmlNS + name }

// antmlClose renders a namespaced closing tag: "</antml:parameter>".
func antmlClose(name string) string { return "</" + antmlNS + name + ">" }

// antmlLeakedMarkup is the production shape observed when a checkpoint narrates
// a tool call as text using namespaced tags: the tool name rides a
// <antml:parameter name="TOOL"> attribute, with nested <antml:parameter>
// children as the arguments, and no <function=/<tool= opener.
func antmlLeakedMarkup() string {
	open := antmlTag("parameter")
	close := antmlClose("parameter")
	return open + ` name="edit_file">` + "\n" +
		open + ` name="new_str" string="true">async function x() { return 1; }` + close + "\n" +
		open + ` name="old_str" string="true">async function x() { return 2; }` + close + "\n" +
		open + ` name="path" string="true">internal/web/static/notes.js` + close + "\n" +
		close
}

func antmlTestParser() *FallbackParser {
	return NewFallbackParser(FallbackParserOptions{
		KnownToolNames: func(name string) bool { return name == "edit_file" || name == "shell_command" },
	})
}

func TestFallbackParsesAntmlBareBlock(t *testing.T) {
	fp := antmlTestParser()
	content := antmlLeakedMarkup()
	if !fp.ShouldUseFallback(content, false) {
		t.Fatal("ShouldUseFallback = false, want true")
	}
	res := fp.Parse(content)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d: %+v", len(res.ToolCalls), res.ToolCalls)
	}
	tc := res.ToolCalls[0]
	if tc.Function.Name != "edit_file" {
		t.Fatalf("name = %q, want edit_file", tc.Function.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not valid JSON: %q: %v", tc.Function.Arguments, err)
	}
	if args["path"] != "internal/web/static/notes.js" {
		t.Errorf("path = %v", args["path"])
	}
	if args["new_str"] != "async function x() { return 1; }" {
		t.Errorf("new_str = %v", args["new_str"])
	}
	for _, residue := range []string{antmlTag("parameter"), antmlClose("parameter")} {
		if strings.Contains(res.CleanedContent, residue) {
			t.Errorf("cleaned content retains %q: %q", residue, res.CleanedContent)
		}
	}
}

func TestFallbackAntmlWrappedInFunctionCalls(t *testing.T) {
	open := antmlTag("parameter")
	close := antmlClose("parameter")
	content := "<" + antmlNS + "function_calls>\n" +
		open + ` name="shell_command">` + "\n" +
		open + ` name="command" string="true">cd /x` + "\n" + `echo hi` + close + "\n" +
		close + "\n</" + antmlNS + "function_calls>"
	res := antmlTestParser().Parse(content)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d: %+v", len(res.ToolCalls), res.ToolCalls)
	}
	if res.ToolCalls[0].Function.Name != "shell_command" {
		t.Fatalf("name = %q", res.ToolCalls[0].Function.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(res.ToolCalls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("bad JSON: %q", res.ToolCalls[0].Function.Arguments)
	}
	if args["command"] != "cd /x\necho hi" {
		t.Errorf("command = %q", args["command"])
	}
}

func TestFallbackAntmlInvokeForm(t *testing.T) {
	open := antmlTag("parameter")
	close := antmlClose("parameter")
	content := "<" + antmlNS + `invoke name="edit_file">` + "\n" +
		open + ` name="path">a.js` + close + "\n" +
		"</" + antmlNS + "invoke>"
	res := antmlTestParser().Parse(content)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d: %+v", len(res.ToolCalls), res.ToolCalls)
	}
	if res.ToolCalls[0].Function.Name != "edit_file" {
		t.Fatalf("name = %q", res.ToolCalls[0].Function.Name)
	}
}

func TestFallbackAntmlUnknownToolNotRecovered(t *testing.T) {
	open := antmlTag("parameter")
	close := antmlClose("parameter")
	content := open + ` name="delete_everything">` + "\n" +
		open + ` name="path">/` + close + "\n" + close
	res := antmlTestParser().Parse(content)
	if len(res.ToolCalls) != 0 {
		t.Fatalf("unexpected recovery: %+v", res.ToolCalls)
	}
}

func TestFallbackAntmlTwoBlocksInOrder(t *testing.T) {
	open := antmlTag("parameter")
	close := antmlClose("parameter")
	content := open + ` name="edit_file">` + "\n" + open + ` name="path">a.js` + close + "\n" + close + "\n" +
		open + ` name="shell_command">` + "\n" + open + ` name="command">ls` + close + "\n" + close
	res := antmlTestParser().Parse(content)
	if len(res.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d: %+v", len(res.ToolCalls), res.ToolCalls)
	}
	if res.ToolCalls[0].Function.Name != "edit_file" || res.ToolCalls[1].Function.Name != "shell_command" {
		t.Fatalf("wrong order: %+v", res.ToolCalls)
	}
}
