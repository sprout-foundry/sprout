//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall/js"

	"github.com/sprout-foundry/sprout/pkg/personas"
)

// Host-scope personas (SP-175). A host mints a persona and decides
// everything it can do. Two routes reach the same definition:
//
//   - createAgent({persona: <definition>}) / runAgent options — one chat's
//     agent (see agent_options.go);
//   - definePersona(definition) — the runtime, so every agent this module
//     builds from then on has it, for a host that runs several agents.
//
// Definitions live only in memory and are never written to disk. The host is
// the creator, so they are trusted; they still may not reuse a core persona's
// id or alias, because core personas are immutable.

var (
	hostPersonaMu   sync.RWMutex
	hostPersonaDefs = map[string]personas.Definition{}
	// hostPersonaGen advances on every define/undefine, so a cached chat
	// agent built before the change is rebuilt on its next run.
	hostPersonaGen atomic.Uint64
)

// hostPersonaGeneration reports the current host-persona generation.
func hostPersonaGeneration() uint64 { return hostPersonaGen.Load() }

func hostPersonaJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"definePersona":       js.FuncOf(definePersonaFunc),
		"undefinePersona":     js.FuncOf(undefinePersonaFunc),
		"listDefinedPersonas": js.FuncOf(listDefinedPersonasFunc),
		"validatePersona":     js.FuncOf(validatePersonaFunc),
	}
}

// definedHostPersonas returns a snapshot of the runtime's host personas,
// sorted by id so every agent gets them in the same order.
func definedHostPersonas() []personas.Definition {
	hostPersonaMu.RLock()
	defer hostPersonaMu.RUnlock()
	ids := make([]string, 0, len(hostPersonaDefs))
	for id := range hostPersonaDefs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]personas.Definition, 0, len(ids))
	for _, id := range ids {
		out = append(out, hostPersonaDefs[id])
	}
	return out
}

// validateHostPersonaDefinition checks a host definition against the core
// catalog. It returns the errors that make the definition unusable; warnings
// (an unknown tool name, say) are reported but do not block it.
func validateHostPersonaDefinition(raw string) (personas.Definition, []string, []string, error) {
	var def personas.Definition
	if err := json.Unmarshal([]byte(raw), &def); err != nil {
		return def, []string{fmt.Sprintf("parse persona definition: %v", err)}, []string{}, err
	}
	core, err := personas.DefaultDefinitions()
	if err != nil {
		return def, []string{fmt.Sprintf("load core persona catalog: %v", err)}, []string{}, err
	}
	problems, warnings := personas.ValidateDefinition(core, def)
	// js.ValueOf cannot convert a nil slice: a nil here panics the module.
	if problems == nil {
		problems = []string{}
	}
	if warnings == nil {
		warnings = []string{}
	}
	def.ID = personas.NormalizeID(def.ID)
	def.Name = strings.TrimSpace(def.Name)
	def.Description = strings.TrimSpace(def.Description)
	return def, problems, warnings, nil
}

// definePersonaFunc: definePersona(definitionJSON: string)
// → {ok: true, id} | {ok: false, errors: string[], warnings?: string[]}.
func definePersonaFunc(_ js.Value, args []js.Value) interface{} {
	raw := argString(args, 0, "")
	if strings.TrimSpace(raw) == "" {
		return map[string]interface{}{"ok": false, "errors": jsStrings([]string{"persona definition JSON is required (first arg)"}), "warnings": jsStrings(nil)}
	}
	def, problems, warnings, err := validateHostPersonaDefinition(raw)
	if err != nil || len(problems) > 0 {
		return map[string]interface{}{"ok": false, "errors": jsStrings(problems), "warnings": jsStrings(warnings)}
	}

	hostPersonaMu.Lock()
	hostPersonaDefs[def.ID] = def
	hostPersonaMu.Unlock()
	hostPersonaGen.Add(1)

	// A cached chat agent is rebuilt on its next run, so it picks the
	// definition up.
	return map[string]interface{}{"ok": true, "id": def.ID, "warnings": jsStrings(warnings)}
}

// jsStrings converts a []string into the []any js.ValueOf accepts. Go's
// syscall/js converts only []any, so a bare []string panics the module.
func jsStrings(values []string) []interface{} {
	out := make([]interface{}, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}

// validatePersonaFunc: validatePersona(definitionJSON: string)
// → {ok: true, id, warnings} | {ok: false, errors, warnings}. It checks a
// definition without registering it, so a host learns its mistakes at
// createAgent time rather than at the first turn.
func validatePersonaFunc(_ js.Value, args []js.Value) interface{} {
	raw := argString(args, 0, "")
	if strings.TrimSpace(raw) == "" {
		return map[string]interface{}{"ok": false, "errors": jsStrings([]string{"persona definition JSON is required (first arg)"}), "warnings": jsStrings(nil)}
	}
	def, problems, warnings, err := validateHostPersonaDefinition(raw)
	if err != nil || len(problems) > 0 {
		return map[string]interface{}{"ok": false, "errors": jsStrings(problems), "warnings": jsStrings(warnings)}
	}
	return map[string]interface{}{"ok": true, "id": def.ID, "warnings": jsStrings(warnings)}
}

// undefinePersonaFunc: undefinePersona(id: string) → {removed: bool}.
func undefinePersonaFunc(_ js.Value, args []js.Value) interface{} {
	id := personas.NormalizeID(argString(args, 0, ""))
	if id == "" {
		return map[string]interface{}{"removed": false}
	}
	hostPersonaMu.Lock()
	if _, ok := hostPersonaDefs[id]; !ok {
		hostPersonaMu.Unlock()
		return map[string]interface{}{"removed": false}
	}
	delete(hostPersonaDefs, id)
	hostPersonaMu.Unlock()
	hostPersonaGen.Add(1)
	return map[string]interface{}{"removed": true}
}

// listDefinedPersonasFunc: listDefinedPersonas() → string[].
func listDefinedPersonasFunc(_ js.Value, _ []js.Value) interface{} {
	defs := definedHostPersonas()
	out := make([]interface{}, len(defs))
	for i, def := range defs {
		out[i] = def.ID
	}
	return out
}
