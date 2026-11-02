//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"syscall/js"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// Host tools: tools the host page implements in JavaScript and offers to the
// agent. A host registers one with registerHostTool(spec, handler); from then
// on it is in the tool registry like any built-in tool, so it is advertised to
// the model (subject to the active persona's allowed_tools, which must name
// it) and a call to it runs the JS handler and returns its string result to
// the model.
//
// The handler is called as handler(argsJSON, {chatId, toolName}) and returns a
// string or a Promise of one. A thrown error or rejected Promise becomes the
// tool's error, which the model sees as a failed call.

func hostToolJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"registerHostTool":   js.FuncOf(registerHostToolFunc),
		"unregisterHostTool": js.FuncOf(unregisterHostToolFunc),
		"listHostTools":      js.FuncOf(listHostToolsFunc),
	}
}

// defaultHostToolTimeout bounds one host tool call when the spec names no
// timeout. Host tools hand work to the host (a task queue, a sandboxed
// fetch), so they get the registry's ceiling rather than a short one.
const defaultHostToolTimeout = 5 * time.Minute

// hostToolNamePattern is the function-name shape every provider accepts.
var hostToolNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)

// hostToolSpec is the JSON a host registers a tool with.
type hostToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	TimeoutMs   int            `json:"timeoutMs"`
}

// hostTool is a ToolHandler whose Execute calls into the host page.
type hostTool struct {
	def     tools.ToolDefinition
	timeout time.Duration
	call    func(ctx context.Context, argsJSON, chatID string) (string, error)
}

var hostToolsMu sync.Mutex

func (h *hostTool) Name() string                       { return h.def.Name }
func (h *hostTool) Definition() tools.ToolDefinition   { return h.def }
func (h *hostTool) Aliases() []string                  { return nil }
func (h *hostTool) Timeout() time.Duration             { return h.timeout }
func (h *hostTool) MaxResultSize() int                 { return 0 }
func (h *hostTool) SafeForParallel() bool              { return false }
func (h *hostTool) Interactive() bool                  { return false }
func (h *hostTool) Validate(args map[string]any) error { return validateRequired(h.def, args) }

func (h *hostTool) Execute(ctx context.Context, env tools.ToolEnv, args map[string]any) (tools.ToolResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("host tool %s: encode arguments: %w", h.def.Name, err)
	}
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	out, err := h.call(ctx, string(argsJSON), env.ChatID)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("host tool %s: %w", h.def.Name, err)
	}
	return tools.ToolResult{Output: out}, nil
}

// validateRequired rejects a call missing a required argument, so the model
// learns of the mistake before the host sees the call.
func validateRequired(def tools.ToolDefinition, args map[string]any) error {
	for _, name := range def.Required {
		if _, ok := args[name]; !ok {
			return fmt.Errorf("missing required argument %q", name)
		}
	}
	return nil
}

// parseHostToolSpec validates a registration and builds the tool definition:
// the host's JSON schema is advertised as is (Definition.Schema), and its
// top-level properties become Parameters for the registry's argument
// coercion.
func parseHostToolSpec(raw string) (tools.ToolDefinition, time.Duration, error) {
	var spec hostToolSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return tools.ToolDefinition{}, 0, fmt.Errorf("parse tool spec: %w", err)
	}
	spec.Name = strings.TrimSpace(spec.Name)
	if !hostToolNamePattern.MatchString(spec.Name) {
		return tools.ToolDefinition{}, 0, fmt.Errorf("invalid tool name %q: letters, digits, '_' and '-', at most 64, not starting with a digit", spec.Name)
	}
	if strings.TrimSpace(spec.Description) == "" {
		return tools.ToolDefinition{}, 0, fmt.Errorf("tool %s: description is required", spec.Name)
	}

	schema := spec.Parameters
	if schema == nil {
		schema = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	if t, ok := schema["type"]; ok && t != "object" {
		return tools.ToolDefinition{}, 0, fmt.Errorf("tool %s: parameters must be an object schema, got type %v", spec.Name, t)
	}
	schema["type"] = "object"
	properties, _ := schema["properties"].(map[string]any)
	if properties == nil {
		properties = map[string]any{}
		schema["properties"] = properties
	}

	var required []string
	if list, ok := schema["required"].([]any); ok {
		for _, v := range list {
			if s, ok := v.(string); ok && s != "" {
				required = append(required, s)
			}
		}
	}
	requiredSet := make(map[string]bool, len(required))
	for _, name := range required {
		requiredSet[name] = true
	}

	params := make([]tools.ParameterDef, 0, len(properties))
	for name, rawProp := range properties {
		prop, _ := rawProp.(map[string]any)
		p := tools.ParameterDef{Name: name, Type: schemaType(prop), Required: requiredSet[name]}
		if d, ok := prop["description"].(string); ok {
			p.Description = d
		}
		if items, ok := prop["items"].(map[string]any); ok {
			p.Items = items
		}
		if enum, ok := prop["enum"].([]any); ok {
			for _, v := range enum {
				if s, ok := v.(string); ok {
					p.Enum = append(p.Enum, s)
				}
			}
		}
		params = append(params, p)
	}

	timeout := defaultHostToolTimeout
	if spec.TimeoutMs > 0 {
		timeout = time.Duration(spec.TimeoutMs) * time.Millisecond
	}
	return tools.ToolDefinition{
		Name:        spec.Name,
		Description: spec.Description,
		Parameters:  params,
		Required:    required,
		Schema:      schema,
	}, timeout, nil
}

// schemaType reads a property's JSON schema type; a union takes its first
// non-null member, and an untyped property is treated as a string.
func schemaType(prop map[string]any) string {
	switch t := prop["type"].(type) {
	case string:
		return t
	case []any:
		for _, v := range t {
			if s, ok := v.(string); ok && s != "null" {
				return s
			}
		}
	}
	return "string"
}

// registerHostTool adds (or replaces) a host tool. A name a built-in tool
// already uses is refused: a host cannot shadow the agent's own tools.
func registerHostTool(def tools.ToolDefinition, timeout time.Duration, call func(ctx context.Context, argsJSON, chatID string) (string, error)) error {
	hostToolsMu.Lock()
	defer hostToolsMu.Unlock()
	reg := tools.GetNewToolRegistry()
	if existing, ok := reg.Lookup(def.Name); ok {
		if _, isHost := existing.(*hostTool); !isHost {
			return fmt.Errorf("tool %s is a built-in tool and cannot be replaced", def.Name)
		}
		reg.Unregister(def.Name)
	}
	return reg.Register(&hostTool{def: def, timeout: timeout, call: call})
}

// unregisterHostTool removes a host tool; built-in tools are never removed.
func unregisterHostTool(name string) bool {
	hostToolsMu.Lock()
	defer hostToolsMu.Unlock()
	reg := tools.GetNewToolRegistry()
	existing, ok := reg.Lookup(name)
	if !ok {
		return false
	}
	if _, isHost := existing.(*hostTool); !isHost {
		return false
	}
	return reg.Unregister(name)
}

// hostToolNames lists the registered host tools, sorted.
func hostToolNames() []string {
	var names []string
	reg := tools.GetNewToolRegistry()
	for _, name := range reg.Names() {
		if h, ok := reg.Lookup(name); ok {
			if _, isHost := h.(*hostTool); isHost {
				names = append(names, name)
			}
		}
	}
	return names
}

// registerHostToolFunc: registerHostTool(specJSON: string, handler: function)
// → {ok: true, name} | {ok: false, error}.
func registerHostToolFunc(_ js.Value, args []js.Value) interface{} {
	raw := argString(args, 0, "")
	if raw == "" {
		return map[string]interface{}{"ok": false, "error": "tool spec JSON is required (first arg)"}
	}
	if len(args) < 2 || args[1].Type() != js.TypeFunction {
		return map[string]interface{}{"ok": false, "error": "handler function is required (second arg)"}
	}
	def, timeout, err := parseHostToolSpec(raw)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	handler := args[1]
	name := def.Name
	call := func(ctx context.Context, argsJSON, chatID string) (string, error) {
		return callHostToolJS(ctx, handler, name, argsJSON, chatID)
	}
	if err := registerHostTool(def, timeout, call); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "name": name}
}

// unregisterHostToolFunc: unregisterHostTool(name: string) → {removed: bool}.
func unregisterHostToolFunc(_ js.Value, args []js.Value) interface{} {
	return map[string]interface{}{"removed": unregisterHostTool(argString(args, 0, ""))}
}

// listHostToolsFunc: listHostTools() → string[].
func listHostToolsFunc(_ js.Value, _ []js.Value) interface{} {
	names := hostToolNames()
	out := make([]interface{}, len(names))
	for i, n := range names {
		out[i] = n
	}
	return out
}

// callHostToolJS invokes the host's handler and waits for its result. The
// agent loop runs on its own goroutine, so blocking here leaves the JS event
// loop free to settle the handler's Promise.
func callHostToolJS(ctx context.Context, handler js.Value, name, argsJSON, chatID string) (string, error) {
	type outcome struct {
		out string
		err error
	}
	ch := make(chan outcome, 1)
	settle := func(o outcome) {
		select {
		case ch <- o:
		default:
		}
	}

	callCtx := js.Global().Get("Object").New()
	callCtx.Set("chatId", chatID)
	callCtx.Set("toolName", name)

	var result js.Value
	func() {
		defer func() {
			if r := recover(); r != nil {
				settle(outcome{err: fmt.Errorf("handler threw: %v", r)})
			}
		}()
		result = handler.Invoke(argsJSON, callCtx)
	}()

	if result.Type() == js.TypeObject && result.Get("then").Type() == js.TypeFunction {
		onResolve := js.FuncOf(func(_ js.Value, pargs []js.Value) interface{} {
			if len(pargs) > 0 {
				settle(outcome{out: hostToolResultString(pargs[0])})
			} else {
				settle(outcome{})
			}
			return nil
		})
		onReject := js.FuncOf(func(_ js.Value, pargs []js.Value) interface{} {
			msg := "handler rejected"
			if len(pargs) > 0 {
				if m := extractJSErrorMessage(pargs[0]); m != "" {
					msg = m
				}
			}
			settle(outcome{err: fmt.Errorf("%s", msg)})
			return nil
		})
		defer onResolve.Release()
		defer onReject.Release()
		result.Call("then", onResolve, onReject)
	} else {
		settle(outcome{out: hostToolResultString(result)})
	}

	select {
	case o := <-ch:
		return o.out, o.err
	case <-ctx.Done():
		return "", fmt.Errorf("no result: %w", ctx.Err())
	}
}

// hostToolResultString turns a handler's return value into the tool result
// the model reads: strings as is, anything else as JSON.
func hostToolResultString(v js.Value) string {
	switch v.Type() {
	case js.TypeString:
		return v.String()
	case js.TypeUndefined, js.TypeNull:
		return ""
	default:
		return js.Global().Get("JSON").Call("stringify", v).String()
	}
}
