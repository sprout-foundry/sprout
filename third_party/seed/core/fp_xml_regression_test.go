package core

import (
	"encoding/json"
	"testing"
	"time"
)

// Regression: xmlGetAttr spun forever when the attribute scan stopped at the
// '=' of a non-matching attribute — the non-advancing continue meant the next
// iteration re-read an empty name at the same index. Observed in production as
// a wedged sprout process pinned at 100% CPU inside FallbackParser.
func TestFallbackParserXMLNonMatchingAttributeTerminates(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	inputs := []string{
		// Attribute other than "name" before the matching one.
		`<function=run_command>
<parameter type="string" name="command">ls -la</parameter>
</function=run_command>`,
		// Attribute other than "name" only.
		`<tool=read_file>
<parameter kind="primary" name="path">/tmp/x</parameter>
</tool=read_file>`,
		// Unterminated quote on a non-matching attribute.
		`<function=run_command>
<parameter type="string name="command">ls</parameter>
</function=run_command>`,
		// Non-matching attribute with no value at all.
		`<function=run_command>
<parameter flag name="command">ls</parameter>
</function=run_command>`,
		// Matching attribute still first (control case).
		`<function=run_command>
<parameter name="command">ls -la</parameter>
</function=run_command>`,
	}
	for _, in := range inputs {
		done := make(chan struct{})
		go func(content string) {
			fp.Parse(content)
			close(done)
		}(in)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("FallbackParser.Parse hung on input: %q", in)
		}
	}
}

func TestFallbackParserXMLNonMatchingAttributeExtractsName(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	res := fp.Parse(`<function=run_command>
<parameter type="string" name="command">ls -la</parameter>
</function=run_command>`)
	if res == nil {
		t.Fatal("expected non-nil parse result")
	}
	var found bool
	for _, tc := range res.ToolCalls {
		if tc.Function.Name == "run_command" {
			found = true
			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				t.Fatalf("invalid arguments JSON %q: %v", tc.Function.Arguments, err)
			}
			if got := args["command"]; got != "ls -la" {
				t.Errorf("command argument = %v, want ls -la", got)
			}
		}
	}
	if !found {
		t.Errorf("run_command tool call not found; calls=%+v", res.ToolCalls)
	}
}

func TestXMLGetAttrSkipsNonMatchingAttributes(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	cases := []struct {
		attrs string
		name  string
		want  string
	}{
		{` type="string" name="command"`, "name", "command"},
		{` name="command" type="string"`, "name", "command"},
		{` flag name="command"`, "name", "command"},
		{` type="string"`, "name", ""},
		// Unterminated quote on a non-matching attribute swallows the rest;
		// the load-bearing property is that this terminates.
		{` type="unterminated name="x"`, "name", ""},
		{` name='single quoted'`, "name", "single quoted"},
		{` name=unquoted`, "name", "unquoted"},
		{` name=`, "name", ""},
		{` type="a" name="cmd" other='b c'`, "name", "cmd"},
		{``, "name", ""},
		{` name = "spaced"`, "name", ""},
	}
	for _, tc := range cases {
		if got := fp.xmlGetAttr(tc.attrs, tc.name); got != tc.want {
			t.Errorf("xmlGetAttr(%q, %q) = %q, want %q", tc.attrs, tc.name, got, tc.want)
		}
	}
}
