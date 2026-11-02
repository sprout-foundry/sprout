//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"strings"
	"syscall/js"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// jsFunc builds a JS function from source, so a test handler can throw or
// return a Promise the way a host page's would.
func jsFunc(params, body string) js.Value {
	return js.Global().Get("Function").New(params, body)
}

const startChangeSpec = `{
	"name": "start_change",
	"description": "Hand a change to the project's task queue.",
	"parameters": {
		"type": "object",
		"properties": {
			"title": {"type": "string", "description": "Short title"},
			"kind": {"type": "string", "enum": ["fix", "feature"]},
			"steps": {"type": "array", "items": {"type": "string"}},
			"scope": {"type": "object", "properties": {"paths": {"type": "array", "items": {"type": "string"}}}}
		},
		"required": ["title"]
	}
}`

func TestParseHostToolSpec_BuildsDefinition(t *testing.T) {
	def, timeout, err := parseHostToolSpec(startChangeSpec)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if def.Name != "start_change" || def.Description == "" {
		t.Fatalf("name/description = %q/%q", def.Name, def.Description)
	}
	if timeout != defaultHostToolTimeout {
		t.Errorf("timeout = %v, want default %v", timeout, defaultHostToolTimeout)
	}
	if len(def.Required) != 1 || def.Required[0] != "title" {
		t.Errorf("required = %v, want [title]", def.Required)
	}
	byName := map[string]tools.ParameterDef{}
	for _, p := range def.Parameters {
		byName[p.Name] = p
	}
	if p := byName["title"]; p.Type != "string" || !p.Required || p.Description != "Short title" {
		t.Errorf("title param = %+v", p)
	}
	if p := byName["kind"]; len(p.Enum) != 2 {
		t.Errorf("kind enum = %v", p.Enum)
	}
	if p := byName["steps"]; p.Type != "array" || p.Items["type"] != "string" {
		t.Errorf("steps param = %+v", p)
	}
	if p := byName["scope"]; p.Type != "object" {
		t.Errorf("scope type = %q", p.Type)
	}
	if def.Schema == nil {
		t.Fatal("schema not kept")
	}
}

func TestParseHostToolSpec_Rejects(t *testing.T) {
	cases := map[string]string{
		"not json":       `{`,
		"bad name":       `{"name": "9tool", "description": "d"}`,
		"spaced name":    `{"name": "start change", "description": "d"}`,
		"no description": `{"name": "t"}`,
		"non-object":     `{"name": "t", "description": "d", "parameters": {"type": "string"}}`,
	}
	for label, raw := range cases {
		if _, _, err := parseHostToolSpec(raw); err == nil {
			t.Errorf("%s: expected an error", label)
		}
	}
}

func TestParseHostToolSpec_DefaultsAndTimeout(t *testing.T) {
	def, timeout, err := parseHostToolSpec(`{"name": "ping", "description": "d", "timeoutMs": 1500}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if timeout != 1500*time.Millisecond {
		t.Errorf("timeout = %v", timeout)
	}
	if def.Schema["type"] != "object" || def.Schema["properties"] == nil {
		t.Errorf("default schema = %v", def.Schema)
	}
}

func TestRegisterHostTool_AdvertisedWithItsSchema(t *testing.T) {
	def, timeout, err := parseHostToolSpec(startChangeSpec)
	if err != nil {
		t.Fatal(err)
	}
	call := func(context.Context, string, string) (string, error) { return "queued", nil }
	if err := registerHostTool(def, timeout, call); err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(func() { unregisterHostTool("start_change") })

	var found bool
	for _, tool := range agent.BuildToolDefinitions() {
		if tool.Function.Name != "start_change" {
			continue
		}
		found = true
		raw, _ := json.Marshal(tool.Function.Parameters)
		// The nested object schema survives: it is the host's schema, not
		// one rebuilt from the flat parameter list.
		if !strings.Contains(string(raw), `"paths"`) {
			t.Errorf("advertised schema lost nested properties: %s", raw)
		}
	}
	if !found {
		t.Fatal("start_change is not advertised")
	}

	names := hostToolNames()
	if len(names) != 1 || names[0] != "start_change" {
		t.Errorf("hostToolNames = %v", names)
	}

	// Registering the same name again replaces the host tool.
	if err := registerHostTool(def, timeout, call); err != nil {
		t.Errorf("re-register: %v", err)
	}
}

func TestRegisterHostTool_CannotShadowBuiltIn(t *testing.T) {
	def, timeout, err := parseHostToolSpec(`{"name": "read_file", "description": "d"}`)
	if err != nil {
		t.Fatal(err)
	}
	err = registerHostTool(def, timeout, func(context.Context, string, string) (string, error) { return "", nil })
	if err == nil || !strings.Contains(err.Error(), "built-in") {
		t.Fatalf("expected a built-in refusal, got %v", err)
	}
	if unregisterHostTool("read_file") {
		t.Fatal("a built-in tool was unregistered")
	}
	if _, ok := tools.GetNewToolRegistry().Lookup("read_file"); !ok {
		t.Fatal("read_file is gone from the registry")
	}
}

func TestHostToolExecute_PassesArgsAndChat(t *testing.T) {
	var gotArgs, gotChat string
	h := &hostTool{
		def:     tools.ToolDefinition{Name: "start_change"},
		timeout: time.Second,
		call: func(_ context.Context, argsJSON, chatID string) (string, error) {
			gotArgs, gotChat = argsJSON, chatID
			return "task 42", nil
		},
	}
	res, err := h.Execute(context.Background(), tools.ToolEnv{ChatID: "chat-1"}, map[string]any{"title": "Fix nav"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Output != "task 42" {
		t.Errorf("output = %q", res.Output)
	}
	if gotArgs != `{"title":"Fix nav"}` || gotChat != "chat-1" {
		t.Errorf("handler saw args=%s chat=%q", gotArgs, gotChat)
	}
}

func TestHostToolValidate_RequiresDeclaredArgs(t *testing.T) {
	h := &hostTool{def: tools.ToolDefinition{Name: "t", Required: []string{"title"}}}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("missing required argument accepted")
	}
	if err := h.Validate(map[string]any{"title": "x"}); err != nil {
		t.Errorf("valid args rejected: %v", err)
	}
}

func TestCallHostToolJS_Results(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		label   string
		handler js.Value
		want    string
		wantErr string
	}{
		{"sync string", jsFunc("a, c", "return 'sync:' + JSON.parse(a).x + ':' + c.chatId + ':' + c.toolName"), "sync:1:chat-9:t", ""},
		{"promise", jsFunc("a", "return Promise.resolve('async:' + a)"), `async:{"x":1}`, ""},
		{"object result", jsFunc("a", "return Promise.resolve({ok: true})"), `{"ok":true}`, ""},
		{"undefined result", jsFunc("a", "return undefined"), "", ""},
		{"rejection", jsFunc("a", "return Promise.reject(new Error('queue full'))"), "", "queue full"},
		{"throw", jsFunc("a", "throw new Error('boom')"), "", "boom"},
	}
	for _, tc := range cases {
		got, err := callHostToolJS(ctx, tc.handler, "t", `{"x":1}`, "chat-9")
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%s: err = %v, want %q", tc.label, err, tc.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.label, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.label, got, tc.want)
		}
	}
}

func TestCallHostToolJS_TimesOut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	never := jsFunc("a", "return new Promise(function () {})")
	_, err := callHostToolJS(ctx, never, "t", "{}", "")
	if err == nil || !strings.Contains(err.Error(), "no result") {
		t.Fatalf("expected a timeout error, got %v", err)
	}
}

func TestRegisterHostToolFunc_JSBridge(t *testing.T) {
	handler := jsFunc("a", "return 'ok'")
	got := registerHostToolFunc(js.Undefined(), []js.Value{js.ValueOf(`{"name": "bridge_tool", "description": "d"}`), handler}).(map[string]interface{})
	if got["ok"] != true {
		t.Fatalf("register via bridge: %v", got)
	}
	t.Cleanup(func() { unregisterHostTool("bridge_tool") })

	missing := registerHostToolFunc(js.Undefined(), []js.Value{js.ValueOf(`{"name": "x", "description": "d"}`)}).(map[string]interface{})
	if missing["ok"] != false {
		t.Errorf("registration without a handler accepted: %v", missing)
	}

	removed := unregisterHostToolFunc(js.Undefined(), []js.Value{js.ValueOf("bridge_tool")}).(map[string]interface{})
	if removed["removed"] != true {
		t.Errorf("unregister via bridge: %v", removed)
	}
}
