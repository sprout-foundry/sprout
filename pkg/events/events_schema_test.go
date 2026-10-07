package events

// These tests lock the cross-language event contract: the committed
// docs/api/events.schema.json (generated from the @sprout/events TypeScript
// types by packages/events/scripts/generate-events-schema.mjs) is validated
// against representative Go event payloads, so a rename or type change on
// either side of the wire fails here instead of surfacing as a silent drift
// at runtime. The payloads are built with the same constructors and structs
// the agent publishes, then marshalled the way the wire carries them (the
// UIEvent envelope) before being checked against the schema.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// schemaFile is the repo-root-relative path of the committed event schema.
const schemaFile = "docs/api/events.schema.json"

// eventsSchemaRepoRoot walks up from the test's working directory to the
// directory holding go.mod (the repo root). The events package test CWD is the
// package directory, so os.Getwd() does not reach the repo root directly.
func eventsSchemaRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolve working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil { // #nosec G304 -- walked-up repo-root path
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found walking up from %s", dir)
		}
		dir = parent
	}
}

// readEventSchemaFile returns the committed schema's bytes, or an error naming
// the regeneration command. It is the shared seam the missing-file test drives.
func readEventSchemaFile(root string) ([]byte, error) {
	return os.ReadFile(filepath.Join(root, schemaFile)) // #nosec G304 -- repo-root-joined contract path
}

// loadEventSchemaDoc reads and parses the committed schema, failing the test
// (with a regeneration hint) when it is missing or unparseable.
func loadEventSchemaDoc(t *testing.T, root string) map[string]any {
	t.Helper()
	b, err := readEventSchemaFile(root)
	if err != nil {
		t.Fatalf("read %s: %v (run 'node packages/events/scripts/generate-events-schema.mjs' to generate it)", schemaFile, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse %s: %v", schemaFile, err)
	}
	return doc
}

// xEventTypesMap returns the schema's event-type → payload-schema extension map.
// Each value is either a "$ref" string (a named $def) or an inline object
// schema (the any/Record payload types).
func xEventTypesMap(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	m, ok := doc["x-eventTypes"].(map[string]any)
	if !ok || len(m) == 0 {
		t.Fatalf("%s has no x-eventTypes extension (the event-type → payload map)", schemaFile)
	}
	return m
}

// payloadSchemaTarget resolves an event type to the schema that governs its
// payload: the x-eventTypes entry (a $ref string or an inline object schema).
func payloadSchemaTarget(t *testing.T, eventTypes map[string]any, eventType string) any {
	t.Helper()
	target, ok := eventTypes[eventType]
	if !ok {
		t.Fatalf("event type %q is not mapped in x-eventTypes; the schema and the Go event constants have drifted", eventType)
	}
	return target
}

// compilePayloadSchema compiles the schema that governs an event's payload.
// The x-eventTypes target is a schema object that is either a $ref ({"$ref":
// "#/$defs/X"}, resolved against the committed file) or an inline object
// schema (the any/Record payload types, registered under a synthetic URL — one
// per event type so distinct payloads do not collide).
func compilePayloadSchema(t *testing.T, c *jsonschema.Compiler, root string, eventType string, target any) *jsonschema.Schema {
	t.Helper()
	obj, ok := target.(map[string]any)
	if !ok {
		t.Fatalf("x-eventTypes target for %q is not a schema object (got %T)", eventType, target)
		return nil
	}
	if ref, hasRef := obj["$ref"]; hasRef {
		refStr, ok := ref.(string)
		if !ok {
			t.Fatalf("x-eventTypes $ref for %q is not a string (got %T)", eventType, ref)
			return nil
		}
		file := filepath.Join(root, schemaFile)
		sch, err := c.Compile(file + refStr)
		if err != nil {
			t.Fatalf("compile %s fragment %q: %v", schemaFile, refStr, err)
		}
		return sch
	}
	url := "https://sprout.local/events-schema/" + eventType
	if err := c.AddResource(url, obj); err != nil {
		t.Fatalf("register inline schema for %s: %v", eventType, err)
	}
	sch, err := c.Compile(url)
	if err != nil {
		t.Fatalf("compile inline schema for %s: %v", eventType, err)
	}
	return sch
}

// roundTripToJSONValue marshals v the way the wire does (through the UIEvent
// envelope for payloads) and unmarshals it back into a JSON value, so the
// schema validator sees exactly the JSON the network would deliver.
func roundTripToJSONValue(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal wire value: %v", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal wire value: %v", err)
	}
	return out
}

// validateEventPayload marshals the payload the way the agent publishes it
// (inside a UIEvent) and asserts it satisfies the schema the committed
// x-eventTypes map assigns to that event type.
func validateEventPayload(t *testing.T, root, eventType string, payload any) {
	t.Helper()
	doc := loadEventSchemaDoc(t, root)
	eventTypes := xEventTypesMap(t, doc)
	target := payloadSchemaTarget(t, eventTypes, eventType)
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	sch := compilePayloadSchema(t, c, root, eventType, target)

	// Wrap in the real Go wire envelope, then validate the data field against
	// the payload schema (the envelope's `data` is `any`; the payload schema is
	// the meaningful check).
	evt := UIEvent{Type: eventType, Data: payload}
	env := roundTripToJSONValue(t, evt).(map[string]any)
	if err := sch.Validate(env["data"]); err != nil {
		t.Errorf("event %q payload does not validate against %s: %v", eventType, schemaFile, err)
	}
}

// TestEventPayloadsValidateAgainstSchema marshals a representative Go payload
// for each of a span of event types and validates each against the committed
// schema. The set covers every payload shape the schema models: plain
// string/number/bool/float fields, optional fields, nested objects, arrays of
// objects, nested arrays of strings, the Record<string,unknown> payload
// shapes, and the self-referential progress_milestone.milestones array.
func TestEventPayloadsValidateAgainstSchema(t *testing.T) {
	root := eventsSchemaRepoRoot(t)

	// Each case builds its payload with the same constructor/struct the agent
	// publishes (or a hand-built map mirroring one) so the test exercises the
	// real wire shape, then validates it against the schema.
	cases := []struct {
		eventType string
		payload   any
		note      string
	}{
		// Plain strings and optional string fields.
		{"query_started", QueryStartedEventWithDisplay("do the thing", "Do the thing", "auto-resume", "anthropic", "claude"), "strings + optional display/source"},
		{"query_progress", QueryProgressEvent("working", 2, 100), "string + numbers"},
		// Float and duration-derived integer.
		{"query_completed", QueryCompletedEvent("q", "resp", 100, 0.5, 3*time.Second), "float cost + int duration_ms"},
		{"stream_chunk", StreamChunkEvent("chunk", "text"), "string fields"},
		{"error", ErrorEvent("failed", errors.New("boom")), "string + optional error"},
		// Integer field (tool_index) and truncation-free arguments.
		{"tool_start", ToolStartEvent("write_file", "call1", "{}", "Write", "coder", false, "", 2), "strings + int tool_index"},
		// Bool (result_truncated) and int (duration_ms).
		{"tool_end", ToolEndEvent("call1", "write_file", "completed", "ok", "", 3*time.Second), "bool + int"},
		// All-optional numeric fields.
		{"metrics_update", MetricsUpdateEvent(10, 5, 100, 2, 0.75), "numbers + float cost"},
		// Required string + int64 numbers.
		{"file_content_changed", FileContentChangedEvent("/a.go", 1234, 56), "string + int64"},
		// Required string/int set (seq), optional conflict omitted.
		{"workspace_patch", WorkspacePatchEvent("/a.go", "content", "write", 3), "string + int seq"},
		// Many float/int telemetry fields.
		{"context_management_diagnostic", ContextManagementDiagnosticEvent(100, 200, 300, 0.5, 0.1, 0.2, 0.1, 4, 8, 10, 20, 5), "floats + ints"},
		// Required set + bool success + timestamp.
		{"compact_completed", CompactCompletedEvent("manual", 10, 5, 100, nil), "strings + ints + bool"},
		// Struct payload with omitempty fields.
		{"rate_limited", &RateLimitedEvent{Provider: "p", Attempt: 1, MaxAttempts: 3, RetryAfterMS: 500, Message: "slow"}, "struct + omitempty"},
		// Record<string,unknown> (summary) via a hand-built map.
		{"session_changed", map[string]any{"change": "renamed", "summary": map[string]any{"title": "new"}}, "Record<string,unknown>"},
		// Nested array of objects (options).
		{"ask_user_request", AskUserRequestEvent("req1", AskUserRequest{Question: "Which?", Header: "Auth", Options: []AskUserRequestOption{{Label: "a", Value: "1"}, {Label: "b"}}, MultiSelect: true, Default: "1"}, "client1"), "nested options array"},
		// Self-referential array (milestones of the same type).
		{"progress_milestone", map[string]any{
			"run_id": "run1", "plan_revision": 2, "phase": MilestonePhaseFinished, "elapsed_ms": 1200,
			"milestones": []any{
				map[string]any{"run_id": "run1", "plan_revision": 2, "phase": MilestonePhaseFinished, "elapsed_ms": 900},
				map[string]any{"run_id": "run1", "plan_revision": 2, "phase": MilestonePhaseFinished, "elapsed_ms": 300},
			},
		}, "self-referential milestones array"},
		// Struct with a nested array of objects (checks[].items is []string).
		{"progress_verification", ProgressVerificationData{
			RunID: "run1", PlanRevision: 2, Passed: true,
			Checks: []ProgressVerificationCheck{{Kind: "build", Items: []string{"a1", "a2"}, Command: "go test", Passed: true, Excerpt: "ok"}},
		}, "nested checks array + items []string"},
		// Struct with a nested object $ref (verification) + bool.
		{"progress_complete", ProgressCompleteData{
			RunID: "run1", PlanRevision: 2, Verified: true,
			Verification: &ProgressVerificationData{RunID: "run1", PlanRevision: 2, Passed: true, Checks: []ProgressVerificationCheck{{Kind: "build", Passed: true}}},
		}, "nested verification $ref"},
		// Inline any/Record payload type (no named $def).
		{"terminal_output", map[string]any{"session_id": "s1", "output": "line"}, "inline Record payload"},
		// Required bool (connected).
		{"connection_status", map[string]any{"connected": true, "session_id": "s1"}, "required bool"},
		// Nested hunks (array of objects) + timestamp. Each hunk carries the
		// full EditHunk shape (id, old/new line counts, lines[]).
		{"edit_approval_request", EditApprovalRequestEvent("r1", "/f.go", "diff", []map[string]any{{
			"id": "h1", "old_start": 1, "old_lines": 1, "new_start": 1, "new_lines": 2,
			"lines":     []map[string]any{{"type": "add", "content": "+x"}, {"type": "context", "content": "y"}},
			"add_count": 1, "del_count": 0,
		}}), "nested hunks array"},
		// Nested parts (array of objects) + timestamp.
		{"shell_approval_request", ShellApprovalRequestEvent("r1", "cmd", []ShellApprovalPartArg{{ID: "p1", Text: "cmd", Kind: "shell", Semantic: "run", Risk: "low"}}, "unified", "low"), "nested parts array"},
		// All-optional activity fields + extras spread.
		{"subagent_activity", SubagentActivityEvent("call1", "run_subagent", "spawn", "spawning", map[string]any{"persona": "coder"}), "optional fields + extras"},
		// Required source/counts + timestamp.
		{"compact_started", CompactStartedEvent("manual", 10, 2), "required numbers + timestamp"},
		// Required strings (replacement/original/reason).
		{"language_guard_replacement", LanguageGuardReplacementEvent("chat1", "notice", "full text", "mid_stream_switch"), "required strings"},
		// Struct with required run/plan/question.
		{"progress_question", ProgressQuestionData{RunID: "run1", PlanRevision: 1, Question: "Which branch?", Header: "Git", WhyItMatters: "merge target"}, "struct + optional options"},
		// Required request_id/tool_name/risk_level/reasoning + extras.
		{"security_approval_request", SecurityApprovalRequestEvent("r1", "shell_command", "high", "rm -rf", map[string]string{"command": "rm -rf /"}), "required strings + extras"},
		// Required request_id/prompt + bool default_response.
		{"security_prompt_request", SecurityPromptRequestEvent("r1", "Approve?", true, map[string]string{}), "required strings + bool"},
		// Required reason + timestamp; optional request_id.
		{"input_required", InputRequiredEvent("ask_user", "r1"), "required string + timestamp"},
		// Required request_id/command/prompt + timestamp.
		{"password_request", PasswordRequestEvent("r1", "sudo apt install", "[sudo] password for user:"), "required strings + timestamp"},
	}
	for _, c := range cases {
		validateEventPayload(t, root, c.eventType, c.payload)
	}
}

// TestEventEnvelopeValidatesAgainstRootSchema asserts the top-level event
// envelope (the UIEvent JSON shape: type, data, id, timestamp) satisfies the
// schema's root object, proving the envelope is the right top-level shape.
func TestEventEnvelopeValidatesAgainstRootSchema(t *testing.T) {
	root := eventsSchemaRepoRoot(t)

	evt := UIEvent{
		ID:        "evt-1",
		Type:      "query_progress",
		Timestamp: time.Now().UTC(),
		Data:      QueryProgressEvent("working", 1, 10),
	}
	env := roundTripToJSONValue(t, evt)

	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	// Compile the whole committed document (the envelope is its root schema).
	sch, err := c.Compile(filepath.Join(root, schemaFile))
	if err != nil {
		t.Fatalf("compile %s: %v", schemaFile, err)
	}
	if err := sch.Validate(env); err != nil {
		t.Fatalf("event envelope does not validate against the root schema: %v", err)
	}
}

// TestEventPayloadSchemaEnforced proves the tests actually exercise the
// schema: a payload that violates a field's type, or that omits a required
// field, must FAIL validation. Without these the suite could pass vacuously
// if the schema were ever broadened to accept anything.
func TestEventPayloadSchemaEnforced(t *testing.T) {
	root := eventsSchemaRepoRoot(t)
	doc := loadEventSchemaDoc(t, root)
	eventTypes := xEventTypesMap(t, doc)
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)

	// query_started requires `query` to be a string; a number must fail.
	sch := compilePayloadSchema(t, c, root, "query_started", payloadSchemaTarget(t, eventTypes, "query_started"))
	badType := roundTripToJSONValue(t, UIEvent{Type: "query_started", Data: map[string]any{"query": 123}}).(map[string]any)
	if err := sch.Validate(badType["data"]); err == nil {
		t.Error("expected a number where the schema requires a string to FAIL validation; the schema is not enforcing types")
	}

	// compact_completed requires the `success` field; omitting it must fail.
	schCC := compilePayloadSchema(t, c, root, "compact_completed", payloadSchemaTarget(t, eventTypes, "compact_completed"))
	missingRequired := map[string]any{"source": "manual", "before_message_count": 1, "after_message_count": 1, "summary_chars": 1, "timestamp": time.Now().UTC().Format(time.RFC3339)}
	// Round-trip so the value is plain JSON (no struct tags).
	ccPayload := roundTripToJSONValue(t, missingRequired)
	if err := schCC.Validate(ccPayload); err == nil {
		t.Error("expected a missing required field to FAIL validation; the schema is not enforcing required fields")
	}
}

// TestEventSchemaMissingFails confirms the loader reports a missing schema
// with the regeneration hint rather than panicking, so a checkout that has
// not run the generator fails loudly.
func TestEventSchemaMissingFails(t *testing.T) {
	empty := t.TempDir()
	if _, err := readEventSchemaFile(empty); err == nil {
		t.Fatal("expected readEventSchemaFile to fail for a root without the schema")
	}
}
