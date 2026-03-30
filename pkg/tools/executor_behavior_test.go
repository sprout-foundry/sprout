package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/utils"
)

// ---------- Inline mock types (self-contained, no dependency on executor_test.go) ----------

type bhMockRegistry struct {
	tools map[string]Tool
}

func newBHMockRegistry(tools ...Tool) *bhMockRegistry {
	m := &bhMockRegistry{tools: make(map[string]Tool)}
	for _, t := range tools {
		m.tools[t.Name()] = t
	}
	return m
}

func (m *bhMockRegistry) RegisterTool(tool Tool) error          { m.tools[tool.Name()] = tool; return nil }
func (m *bhMockRegistry) GetTool(name string) (Tool, bool)     { t, ok := m.tools[name]; return t, ok }
func (m *bhMockRegistry) UnregisterTool(name string) error     { delete(m.tools, name); return nil }
func (m *bhMockRegistry) ListTools() []Tool                    { out := make([]Tool, 0, len(m.tools)); for _, t := range m.tools { out = append(out, t) }; return out }
func (m *bhMockRegistry) ListToolsByCategory(category string) []Tool { return m.ListTools() }

// bhMockTool is a configurable mock implementing Tool.
type bhMockTool struct {
	name               string
	desc               string
	cat                string
	available          bool
	canExec            bool
	requiredPerms      []string
	estimatedDuration  time.Duration
	executeFn          func(ctx context.Context, params Parameters) (*Result, error)
}

func (m *bhMockTool) Name() string                { return m.name }
func (m *bhMockTool) Description() string          { return m.desc }
func (m *bhMockTool) Category() string             { return m.cat }
func (m *bhMockTool) IsAvailable() bool            { return m.available }
func (m *bhMockTool) CanExecute(ctx context.Context, params Parameters) bool {
	return m.canExec
}
func (m *bhMockTool) RequiredPermissions() []string { return m.requiredPerms }
func (m *bhMockTool) EstimatedDuration() time.Duration { return m.estimatedDuration }
func (m *bhMockTool) Execute(ctx context.Context, params Parameters) (*Result, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, params)
	}
	return &Result{Success: true, Output: "ok"}, nil
}

func newAvailableTool(name string) *bhMockTool {
	return &bhMockTool{
		name:              name,
		desc:              "test tool " + name,
		cat:               "test",
		available:         true,
		canExec:           true,
		requiredPerms:     nil,
		estimatedDuration: 5 * time.Second,
	}
}

// ---------- Helper to build an executor with permissive permissions ----------

func newTestExecutor(tools ...Tool) *Executor {
	return NewExecutor(
		newBHMockRegistry(tools...),
		NewSimplePermissionChecker(nil), // allow all (no required perms on our mocks)
		utils.GetLogger(true),
		&configuration.Config{},
	)
}

// =========================================================================
// 1) ExecuteTool with nil tool returns error
// =========================================================================

func TestExecuteTool_NilTool_ReturnsError(t *testing.T) {
	exec := NewExecutor(
		newBHMockRegistry(),
		NewSimplePermissionChecker(nil),
		utils.GetLogger(true),
		&configuration.Config{},
	)

	result, err := exec.ExecuteTool(context.Background(), nil, Parameters{})
	if err == nil {
		t.Fatal("expected error for nil tool, got nil")
	}
	if result != nil {
		t.Error("expected nil result for nil tool")
	}
	if !strings.Contains(err.Error(), "nil tool") {
		t.Errorf("error message should mention nil tool, got: %v", err)
	}
}

// =========================================================================
// 2) ExecuteTool with unavailable tool (IsAvailable false) returns Success=false
// =========================================================================

func TestExecuteTool_UnavailableTool_ReturnsFalse(t *testing.T) {
	tool := &bhMockTool{
		name:       "offline_tool",
		available:  false,
		canExec:    true,
	}
	exec := newTestExecutor(tool)

	result, err := exec.ExecuteTool(context.Background(), tool, Parameters{})
	if err != nil {
		t.Fatalf("unexpected error, got: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false for unavailable tool")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "not available") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'not available' error, got: %v", result.Errors)
	}
}

// =========================================================================
// 3) ExecuteTool timeout handling – tool sleeps longer than its timeout
// =========================================================================

func TestExecuteTool_Timeout_ExceedsDuration(t *testing.T) {
	tool := &bhMockTool{
		name:       "slow_tool",
		available:  true,
		canExec:    true,
		executeFn: func(ctx context.Context, params Parameters) (*Result, error) {
			select {
			case <-time.After(5 * time.Second):
				return &Result{Success: true, Output: "done"}, nil
			case <-ctx.Done():
				return &Result{Success: false, Errors: []string{"context canceled due to timeout"}}, nil
			}
		},
	}
	// Set a short timeout via Parameters (overrides EstimatedDuration)
	exec := newTestExecutor(tool)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	result, err := exec.ExecuteTool(ctx, tool, Parameters{Timeout: 50 * time.Millisecond})
	if err != nil {
		// context.DeadlineExceeded or similar is acceptable
		t.Logf("ExecuteTool returned error (acceptable): %v", err)
	}
	if result != nil && result.Success {
		t.Error("expected tool to not succeed (should have timed out)")
	}
	if result != nil {
		t.Logf("execution time: %v", result.ExecutionTime)
	}
}

// =========================================================================
// 4) ExecuteToolByName when tool not found
// =========================================================================

func TestExecuteToolByName_NotFound(t *testing.T) {
	exec := newTestExecutor() // empty registry

	result, err := exec.ExecuteToolByName(context.Background(), "no_such_tool", Parameters{})
	if err != nil {
		t.Fatalf("unexpected error, got: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false when tool not found by name")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "not found") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'not found' error, got: %v", result.Errors)
	}
}

// =========================================================================
// 5) ExecuteToolByName when tool IS found
// =========================================================================

func TestExecuteToolByName_Found(t *testing.T) {
	tool := newAvailableTool("found_tool")
	exec := newTestExecutor(tool)

	result, err := exec.ExecuteToolByName(context.Background(), "found_tool", Parameters{})
	if err != nil {
		t.Fatalf("unexpected error, got: %v", err)
	}
	if !result.Success {
		t.Errorf("expected Success=true, got errors: %v", result.Errors)
	}
}

// =========================================================================
// 6) normalizeArgsForTool – various tools and aliases
// =========================================================================

// 6a) edit_file_section aliases: old_string->old_text, from->old_text, new_string->new_text, to->new_text
func TestNormalizeArgsForTool_EditFileSection_Aliases(t *testing.T) {
	// old_string -> old_text
	args := map[string]interface{}{"file_path": "f.txt", "old_string": "abc", "new_text": "def"}
	out := normalizeArgsForTool("edit_file_section", args)
	if v, ok := out["old_text"]; !ok || v != "abc" {
		t.Errorf("old_string should map to old_text, got old_text=%v", out["old_text"])
	}

	// from -> old_text
	args = map[string]interface{}{"file_path": "f.txt", "from": "abc", "new_text": "def"}
	out = normalizeArgsForTool("edit_file_section", args)
	if v, ok := out["old_text"]; !ok || v != "abc" {
		t.Errorf("from should map to old_text, got old_text=%v", out["old_text"])
	}

	// new_string -> new_text
	args = map[string]interface{}{"file_path": "f.txt", "old_text": "abc", "new_string": "def"}
	out = normalizeArgsForTool("edit_file_section", args)
	if v, ok := out["new_text"]; !ok || v != "def" {
		t.Errorf("new_string should map to new_text, got new_text=%v", out["new_text"])
	}

	// to -> new_text
	args = map[string]interface{}{"file_path": "f.txt", "old_text": "abc", "to": "def"}
	out = normalizeArgsForTool("edit_file_section", args)
	if v, ok := out["new_text"]; !ok || v != "def" {
		t.Errorf("to should map to new_text, got new_text=%v", out["new_text"])
	}
}

// 6b) edit_file_section: alias does NOT override existing target key
func TestNormalizeArgsForTool_EditFileSection_NoOverride(t *testing.T) {
	// old_text already exists — old_string should be ignored
	args := map[string]interface{}{"file_path": "f.txt", "old_text": "original", "old_string": "alias", "new_text": "def"}
	out := normalizeArgsForTool("edit_file_section", args)
	if v := out["old_text"]; v != "original" {
		t.Errorf("existing old_text should NOT be overwritten by old_string alias, got: %v", v)
	}

	// new_text already exists — to should be ignored
	args = map[string]interface{}{"file_path": "f.txt", "old_text": "abc", "new_text": "original", "to": "alias"}
	out = normalizeArgsForTool("edit_file_section", args)
	if v := out["new_text"]; v != "original" {
		t.Errorf("existing new_text should NOT be overwritten by to alias, got: %v", v)
	}
}

// 6c) workspace_context: op->action only when action is absent
func TestNormalizeArgsForTool_WorkspaceContext_OpAlias(t *testing.T) {
	// op -> action
	args := map[string]interface{}{"op": "search"}
	out := normalizeArgsForTool("workspace_context", args)
	if v, ok := out["action"]; !ok || v != "search" {
		t.Errorf("op should map to action when action absent, got action=%v", out["action"])
	}

	// action already set — op should NOT override
	args = map[string]interface{}{"action": "read", "op": "search"}
	out = normalizeArgsForTool("workspace_context", args)
	if v := out["action"]; v != "read" {
		t.Errorf("existing action should NOT be overwritten by op alias, got: %v", v)
	}
}

// 6d) workspace_context: keywords->query only when query is absent
func TestNormalizeArgsForTool_WorkspaceContext_KeywordsAlias(t *testing.T) {
	// keywords -> query
	args := map[string]interface{}{"keywords": "my search term"}
	out := normalizeArgsForTool("workspace_context", args)
	if v, ok := out["query"]; !ok || v != "my search term" {
		t.Errorf("keywords should map to query when query absent, got query=%v", out["query"])
	}

	// query already set — keywords should NOT override
	args = map[string]interface{}{"query": "original query", "keywords": "alias query"}
	out = normalizeArgsForTool("workspace_context", args)
	if v := out["query"]; v != "original query" {
		t.Errorf("existing query should NOT be overwritten by keywords alias, got: %v", v)
	}
}

// 6e) workspace_context: search action normalization (case-insensitive)
func TestNormalizeArgsForTool_WorkspaceContext_SearchNormalization(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"search", "search"},
		{"Search", "search"},
		{"SEARCH", "search"},
		{"SeArCh", "search"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			args := map[string]interface{}{"action": tt.input}
			out := normalizeArgsForTool("workspace_context", args)
			if v := out["action"]; v != tt.want {
				t.Errorf("action %q should normalize to %q, got: %v", tt.input, tt.want, v)
			}
		})
	}

	// Non-search action is left untouched
	args := map[string]interface{}{"action": "list"}
	out := normalizeArgsForTool("workspace_context", args)
	if v := out["action"]; v != "list" {
		t.Errorf("non-search action should not change, got: %v", v)
	}
}

// 6f) normalizeArgsForTool with nil args returns nil
func TestNormalizeArgsForTool_NilArgs(t *testing.T) {
	out := normalizeArgsForTool("any_tool", nil)
	if out != nil {
		t.Errorf("expected nil for nil input, got: %v", out)
	}
}

// 6g) normalizeArgsForTool for unknown tool is a no-op
func TestNormalizeArgsForTool_UnknownTool_Noop(t *testing.T) {
	args := map[string]interface{}{"old_string": "abc", "op": "search"}
	out := normalizeArgsForTool("completely_unknown_tool", args)
	if len(out) != 2 {
		t.Errorf("unknown tool should leave args unchanged, got: %v", out)
	}
	if _, ok := out["old_text"]; ok {
		t.Error("old_text should not be created for unknown tool")
	}
	if _, ok := out["action"]; ok {
		t.Error("action should not be created for unknown tool")
	}
}

// =========================================================================
// 7) SimplePermissionChecker.HasPermission with empty/matched/unmatched perms
// =========================================================================

func TestSimplePermissionChecker_HasPermission(t *testing.T) {
	pc := NewSimplePermissionChecker([]string{"read", "write"})

	// All matched
	if !pc.HasPermission([]string{"read"}) {
		t.Error("expected true when single perm is matched")
	}
	if !pc.HasPermission([]string{"read", "write"}) {
		t.Error("expected true when all perms matched")
	}

	// Unmatched permission
	if pc.HasPermission([]string{"delete"}) {
		t.Error("expected false when perm not in allowed set")
	}
	if pc.HasPermission([]string{"read", "delete"}) {
		t.Error("expected false when one perm is unmatched")
	}

	// Empty permission request
	if !pc.HasPermission(nil) {
		t.Error("expected true for nil permissions (vacuously true)")
	}
	if !pc.HasPermission([]string{}) {
		t.Error("expected true for empty permissions")
	}
}

// =========================================================================
// 8) CheckToolExecution delegates to HasPermission
// =========================================================================

func TestCheckToolExecution_DelegatesToHasPermission(t *testing.T) {
	toolRequiringRead := &bhMockTool{
		name:          "needs_read",
		requiredPerms: []string{"read_file"},
		available:     true,
		canExec:       true,
	}

	// Permission granted
	grantingPC := NewSimplePermissionChecker([]string{"read_file"})
	if !grantingPC.CheckToolExecution(toolRequiringRead, Parameters{}) {
		t.Error("expected CheckToolExecution to return true when permission allowed")
	}

	// Permission denied
	denyingPC := NewSimplePermissionChecker([]string{"write_file"})
	if denyingPC.CheckToolExecution(toolRequiringRead, Parameters{}) {
		t.Error("expected CheckToolExecution to return false when permission denied")
	}

	// Tool requires multiple perms, one missing
	toolMulti := &bhMockTool{
		name:          "multi_perm",
		requiredPerms: []string{"read_file", "execute_shell"},
		available:     true,
		canExec:       true,
	}
	partialPC := NewSimplePermissionChecker([]string{"read_file"})
	if partialPC.CheckToolExecution(toolMulti, Parameters{}) {
		t.Error("expected false when tool requires multiple perms and one is missing")
	}

	// Tool requires no perms
	toolNoPerms := &bhMockTool{
		name:          "no_perms",
		requiredPerms: nil,
		available:     true,
		canExec:       true,
	}
	emptyPC := NewSimplePermissionChecker(nil)
	if !emptyPC.CheckToolExecution(toolNoPerms, Parameters{}) {
		t.Error("expected true for tool with no required permissions")
	}
}

// =========================================================================
// 9) Session lifecycle: StartSession / EndSession / GetSessionStats
// =========================================================================

func TestSessionLifecycle(t *testing.T) {
	logger := utils.GetLogger(true)
	exec := NewExecutor(
		newBHMockRegistry(),
		NewSimplePermissionChecker(nil),
		logger,
		&configuration.Config{},
	)

	// Start a session – should return a non-empty ID
	sessionID := exec.StartSession()
	if sessionID == "" {
		t.Fatal("expected non-empty session ID from StartSession")
	}

	// Stats should be available immediately
	stats := exec.GetSessionStats(sessionID)
	if stats == nil {
		t.Fatal("expected non-nil stats for active session")
	}
	if stats["session_id"] != sessionID {
		t.Errorf("session_id mismatch: got %v, want %v", stats["session_id"], sessionID)
	}
	if total, ok := stats["total_tool_calls"].(int); ok && total != 0 {
		t.Errorf("expected 0 tool_calls for new session, got %d", total)
	}

	// End the session
	exec.EndSession(sessionID)

	// Stats should be nil after ending
	endedStats := exec.GetSessionStats(sessionID)
	if endedStats != nil {
		t.Errorf("expected nil stats after EndSession, got: %v", endedStats)
	}
}

func TestSessionStats_UnknownSession(t *testing.T) {
	logger := utils.GetLogger(true)
	exec := NewExecutor(
		newBHMockRegistry(),
		NewSimplePermissionChecker(nil),
		logger,
		&configuration.Config{},
	)

	stats := exec.GetSessionStats("does-not-exist")
	if stats != nil {
		t.Errorf("expected nil stats for unknown session, got: %v", stats)
	}
}

func TestSessionMultipleSessions(t *testing.T) {
	logger := utils.GetLogger(true)
	exec := NewExecutor(
		newBHMockRegistry(),
		NewSimplePermissionChecker(nil),
		logger,
		&configuration.Config{},
	)

	id1 := exec.StartSession()
	id2 := exec.StartSession()

	if id1 == id2 {
		t.Error("two consecutive StartSession calls should return different IDs")
	}

	// Both sessions should be independently accessible
	if exec.GetSessionStats(id1) == nil {
		t.Error("session 1 should exist")
	}
	if exec.GetSessionStats(id2) == nil {
		t.Error("session 2 should exist")
	}

	// End one – the other should remain
	exec.EndSession(id1)
	if exec.GetSessionStats(id1) != nil {
		t.Error("session 1 should be gone after EndSession")
	}
	if exec.GetSessionStats(id2) == nil {
		t.Error("session 2 should still exist")
	}

	exec.EndSession(id2) // cleanup
}
