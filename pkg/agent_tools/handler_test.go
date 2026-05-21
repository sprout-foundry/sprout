package tools

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// mockHandler — a configurable ToolHandler for testing
// ---------------------------------------------------------------------------

type mockHandler struct {
	name        string
	definition  ToolDefinition
	validateErr error
	result      ToolResult
	execErr     error
}

func (m *mockHandler) Name() string {
	return m.name
}

func (m *mockHandler) Definition() ToolDefinition {
	return m.definition
}

func (m *mockHandler) Validate(_ map[string]any) error {
	return m.validateErr
}

func (m *mockHandler) Execute(_ context.Context, _ ToolEnv, _ map[string]any) (ToolResult, error) {
	return m.result, m.execErr
}

// newMockHandler creates a mockHandler with sensible defaults.
func newMockHandler(name string) *mockHandler {
	return &mockHandler{
		name: name,
		definition: ToolDefinition{
			Name:        name,
			Description: "mock tool for " + name,
			Parameters:  []ParameterDef{},
		},
		result: ToolResult{Output: "mock-" + name},
	}
}

// ---------------------------------------------------------------------------
// ToolRegistry Tests
// ---------------------------------------------------------------------------

func TestToolRegistryNew(t *testing.T) {
	t.Parallel()
	reg := NewToolRegistry()
	if reg == nil {
		t.Fatal("NewToolRegistry returned nil")
	}
	if len(reg.Names()) != 0 {
		t.Fatalf("new registry should be empty, got %d names", len(reg.Names()))
	}
}

func TestToolRegistryRegister(t *testing.T) {
	t.Parallel()
	reg := NewToolRegistry()
	h := newMockHandler("read_file")

	if err := reg.Register(h); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found, ok := reg.Lookup("read_file")
	if !ok {
		t.Fatal("Lookup did not find registered tool")
	}
	if found.Name() != "read_file" {
		t.Errorf("Lookup returned wrong tool: got %q, want %q", found.Name(), "read_file")
	}
}

func TestToolRegistryRegisterDuplicate(t *testing.T) {
	t.Parallel()
	reg := NewToolRegistry()
	h1 := newMockHandler("read_file")
	h2 := newMockHandler("read_file")

	if err := reg.Register(h1); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	err := reg.Register(h2)
	if err == nil {
		t.Fatal("Registering duplicate name should return error")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("error message should mention already registered, got: %v", err)
	}

	// Original handler should still be there
	found, ok := reg.Lookup("read_file")
	if !ok || found != h1 {
		t.Fatal("Original tool should still be registered after failed duplicate")
	}
}

func TestToolRegistryLookupNotFound(t *testing.T) {
	t.Parallel()
	reg := NewToolRegistry()
	h, ok := reg.Lookup("nonexistent")
	if ok {
		t.Fatal("Lookup should return false for unknown tool")
	}
	if h != nil {
		t.Fatal("Lookup should return nil handler for unknown tool")
	}
}

func TestToolRegistryAll(t *testing.T) {
	t.Parallel()
	reg := NewToolRegistry()
	h1 := newMockHandler("alpha")
	h2 := newMockHandler("beta")
	reg.Register(h1)
	reg.Register(h2)

	// Get the map
	all := reg.All()

	// Verify contents
	if len(all) != 2 {
		t.Fatalf("All() returned %d tools, want 2", len(all))
	}
	if _, ok := all["alpha"]; !ok {
		t.Error("All() missing alpha")
	}
	if _, ok := all["beta"]; !ok {
		t.Error("All() missing beta")
	}

	// Verify it's a copy: modifying the returned map must not affect the registry
	all["gamma"] = newMockHandler("gamma")
	if len(reg.All()) != 2 {
		t.Fatal("Modifying the returned map affected the registry — All() must return a copy")
	}
}

func TestToolRegistryForPersona(t *testing.T) {
	t.Parallel()
	reg := NewToolRegistry()
	h1 := newMockHandler("tool_a")
	h2 := newMockHandler("tool_b")
	reg.Register(h1)
	reg.Register(h2)

	tools := reg.ForPersona("coder")
	if len(tools) != 2 {
		t.Fatalf("ForPersona returned %d tools, want 2", len(tools))
	}
	// ForPersona currently returns all tools regardless of persona
	for _, name := range []string{"tool_a", "tool_b"} {
		if _, ok := tools[name]; !ok {
			t.Errorf("ForPersona missing tool %q", name)
		}
	}
}

func TestToolRegistryNames(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		reg := NewToolRegistry()
		names := reg.Names()
		if names == nil {
			t.Fatal("Names() returned nil for empty registry")
		}
		if len(names) != 0 {
			t.Fatalf("empty registry should return empty slice, got %d names", len(names))
		}
	})

	t.Run("with tools", func(t *testing.T) {
		t.Parallel()
		reg := NewToolRegistry()
		reg.Register(newMockHandler("read_file"))
		reg.Register(newMockHandler("write_file"))
		reg.Register(newMockHandler("git"))
		names := reg.Names()
		if len(names) != 3 {
			t.Fatalf("expected 3 names, got %d", len(names))
		}
	})
}

func TestToolRegistryNamesSorted(t *testing.T) {
	t.Parallel()
	reg := NewToolRegistry()
	// Register in non-alphabetical order
	reg.Register(newMockHandler("zulu"))
	reg.Register(newMockHandler("alpha"))
	reg.Register(newMockHandler("mike"))

	names := reg.Names()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}

	expected := []string{"alpha", "mike", "zulu"}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want)
		}
	}
}

func TestToolRegistryConcurrency(t *testing.T) {
	t.Parallel()
	reg := NewToolRegistry()

	const goroutines = 50
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)

	// Concurrent Register calls (unique names to avoid collisions)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h := newMockHandler("concurrent-tool") // same name — some will fail, that's ok
			err := reg.Register(h)
			if err != nil && !strings.Contains(err.Error(), "already registered") {
				errCh <- err
			}
		}(i)
	}

	// Concurrent Lookup calls
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reg.Lookup("nonexistent")
		}()
	}

	// Concurrent All calls
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = reg.All()
		}()
	}

	// Concurrent Names calls
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = reg.Names()
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("unexpected error from concurrent operation: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ToolHandler Interface (via mock) Tests
// ---------------------------------------------------------------------------

func TestMockToolHandler(t *testing.T) {
	t.Parallel()

	h := &mockHandler{
		name: "test_tool",
		definition: ToolDefinition{
			Name:        "test_tool",
			Description: "a test tool",
			Parameters:  []ParameterDef{{Name: "foo", Type: "string", Required: true}},
		},
		validateErr: nil,
		result:      ToolResult{Output: "success"},
		execErr:     nil,
	}

	// Name
	if h.Name() != "test_tool" {
		t.Errorf("Name() = %q, want %q", h.Name(), "test_tool")
	}

	// Definition
	def := h.Definition()
	if def.Name != "test_tool" {
		t.Errorf("Definition().Name = %q, want %q", def.Name, "test_tool")
	}
	if def.Description != "a test tool" {
		t.Errorf("Definition().Description = %q, want %q", def.Description, "a test tool")
	}
	if len(def.Parameters) != 1 {
		t.Fatalf("Definition().Parameters has %d items, want 1", len(def.Parameters))
	}
	if def.Parameters[0].Name != "foo" {
		t.Errorf("Definition().Parameters[0].Name = %q, want %q", def.Parameters[0].Name, "foo")
	}

	// Validate — no error
	err := h.Validate(nil)
	if err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}

	// Execute — success
	res, err := h.Execute(context.Background(), ToolEnv{}, nil)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if res.Output != "success" {
		t.Errorf("Execute().Output = %q, want %q", res.Output, "success")
	}

	// Validate — with error
	h.validateErr = context.Canceled
	err = h.Validate(nil)
	if err != context.Canceled {
		t.Errorf("Validate() error = %v, want %v", err, context.Canceled)
	}

	// Execute — with error
	h.validateErr = nil
	h.execErr = context.DeadlineExceeded
	h.result = ToolResult{Output: "failed", IsError: true}
	res, err = h.Execute(context.Background(), ToolEnv{}, nil)
	if err != context.DeadlineExceeded {
		t.Errorf("Execute() error = %v, want %v", err, context.DeadlineExceeded)
	}
	if !res.IsError {
		t.Error("Execute() should return IsError=true result")
	}
}

// ---------------------------------------------------------------------------
// ToolResult Tests
// ---------------------------------------------------------------------------

func TestToolResultBasic(t *testing.T) {
	t.Parallel()
	r := ToolResult{Output: "hello"}
	if r.Output != "hello" {
		t.Errorf("Output = %q, want %q", r.Output, "hello")
	}
	if r.IsError {
		t.Error("IsError should be false by default")
	}
	if r.TokenUsage != 0 {
		t.Error("TokenUsage should be 0 by default")
	}
	if len(r.Images) != 0 {
		t.Error("Images should be empty by default")
	}
	if r.StructuredOut != nil {
		t.Error("StructuredOut should be nil by default")
	}
}

func TestToolResultWithImages(t *testing.T) {
	t.Parallel()
	r := ToolResult{
		Output: "see image",
		Images: []ImageData{
			{URI: "/tmp/img.png", MIMEType: "image/png"},
			{URI: "/tmp/img.jpg", MIMEType: "image/jpeg"},
		},
	}
	if len(r.Images) != 2 {
		t.Fatalf("Images has %d items, want 2", len(r.Images))
	}
	if r.Images[0].URI != "/tmp/img.png" {
		t.Errorf("Images[0].URI = %q, want %q", r.Images[0].URI, "/tmp/img.png")
	}
	if r.Images[0].MIMEType != "image/png" {
		t.Errorf("Images[0].MIMEType = %q, want %q", r.Images[0].MIMEType, "image/png")
	}
	if r.Images[1].URI != "/tmp/img.jpg" {
		t.Errorf("Images[1].URI = %q, want %q", r.Images[1].URI, "/tmp/img.jpg")
	}
}

func TestToolResultWithStructuredOut(t *testing.T) {
	t.Parallel()
	data := map[string]int{"key": 42}
	r := ToolResult{
		Output:          "structured",
		StructuredOut:   data,
	}
	got, ok := r.StructuredOut.(map[string]int)
	if !ok {
		t.Fatalf("StructuredOut is not map[string]int, got %T", r.StructuredOut)
	}
	if got["key"] != 42 {
		t.Errorf("StructuredOut[key] = %d, want 42", got["key"])
	}
}

func TestToolResultWithError(t *testing.T) {
	t.Parallel()
	r := ToolResult{
		Output:  "something went wrong",
		IsError: true,
	}
	if !r.IsError {
		t.Error("IsError should be true")
	}
	if r.Output != "something went wrong" {
		t.Errorf("Output = %q, want %q", r.Output, "something went wrong")
	}
}

func TestToolResultTokenUsage(t *testing.T) {
	t.Parallel()
	r := ToolResult{
		Output:     "done",
		TokenUsage: 150,
	}
	if r.TokenUsage != 150 {
		t.Errorf("TokenUsage = %d, want 150", r.TokenUsage)
	}
}

// ---------------------------------------------------------------------------
// ToolDefinition Tests
// ---------------------------------------------------------------------------

func TestToolDefinitionFields(t *testing.T) {
	t.Parallel()
	def := ToolDefinition{
		Name:        "read_file",
		Description: "Read the contents of a file",
		Parameters: []ParameterDef{
			{Name: "path", Type: "string", Required: true, Description: "Path to the file"},
			{Name: "range", Type: "array", Required: false, Description: "Line range"},
		},
	}

	if def.Name != "read_file" {
		t.Errorf("Name = %q, want %q", def.Name, "read_file")
	}
	if def.Description != "Read the contents of a file" {
		t.Errorf("Description = %q, want %q", def.Description, "Read the contents of a file")
	}
	if len(def.Parameters) != 2 {
		t.Fatalf("Parameters has %d items, want 2", len(def.Parameters))
	}

	p0 := def.Parameters[0]
	if p0.Name != "path" {
		t.Errorf("Parameters[0].Name = %q, want %q", p0.Name, "path")
	}
	if p0.Type != "string" {
		t.Errorf("Parameters[0].Type = %q, want %q", p0.Type, "string")
	}
	if !p0.Required {
		t.Error("Parameters[0].Required should be true")
	}

	p1 := def.Parameters[1]
	if p1.Required {
		t.Error("Parameters[1].Required should be false")
	}
}

func TestParameterDefFields(t *testing.T) {
	t.Parallel()
	p := ParameterDef{
		Name:        "workspace_root",
		Type:        "string",
		Required:    true,
		Description: "The workspace root directory",
	}
	if p.Name != "workspace_root" {
		t.Errorf("Name = %q, want %q", p.Name, "workspace_root")
	}
	if p.Type != "string" {
		t.Errorf("Type = %q, want %q", p.Type, "string")
	}
	if !p.Required {
		t.Error("Required should be true")
	}
	if p.Description != "The workspace root directory" {
		t.Errorf("Description = %q, want %q", p.Description, "The workspace root directory")
	}
}

// ---------------------------------------------------------------------------
// ToolEnv Tests
// ---------------------------------------------------------------------------

func TestToolEnvFields(t *testing.T) {
	t.Parallel()
	sw := &strings.Builder{}
	am := &mockApprovalManager{approved: true}

	env := ToolEnv{
		EventBus:      nil, // hard to construct without events package internals
		WorkspaceRoot: "/home/user/project",
		OutputWriter:  sw,
		ApprovalManager: am,
		MaxTokensFunc: func() int { return 4096 },
	}

	if env.WorkspaceRoot != "/home/user/project" {
		t.Errorf("WorkspaceRoot = %q, want %q", env.WorkspaceRoot, "/home/user/project")
	}
	if env.OutputWriter == nil {
		t.Fatal("OutputWriter should not be nil")
	}
	if env.ApprovalManager == nil {
		t.Fatal("ApprovalManager should not be nil")
	}
	tokens := env.MaxTokensFunc()
	if tokens != 4096 {
		t.Errorf("MaxTokensFunc() = %d, want 4096", tokens)
	}
}

// mockApprovalManager implements ApprovalManager for testing.
type mockApprovalManager struct {
	approved    bool
	reason      string
	userComment string
}

func (m *mockApprovalManager) RequestApproval(_, _, _, _ string, _ map[string]string) ApprovalResult {
	return ApprovalResult{
		Approved:    m.approved,
		Reason:      m.reason,
		UserComment: m.userComment,
	}
}

// ---------------------------------------------------------------------------
// AllTools Tests
// ---------------------------------------------------------------------------

func TestAllToolsRegistration(t *testing.T) {
	t.Parallel()
	tools := AllTools()
	if tools == nil {
		t.Fatal("AllTools() returned nil")
	}
	if len(tools) != 17 {
		t.Fatalf("AllTools() returned %d tools, want 17", len(tools))
	}

	expectedNames := map[string]string{
		"read_file":               "read_file",
		"list_directory":          "list_directory",
		"fetch_url":               "fetch_url",
		"search_files":            "search_files",
		"repo_map":                "repo_map",
		"list_memories":           "list_memories",
		"read_memory":             "read_memory",
		"rollback_changes":        "rollback_changes",
		"view_history":            "view_history",
		"list_skills":             "list_skills",
		"embedding_index":         "embedding_index",
		"write_file":              "write_file",
		"write_structured_file":   "write_structured_file",
		"edit_file":               "edit_file",
		"shell_command":           "shell_command",
		"save_memory":             "save_memory",
		"search_memories":         "search_memories",
	}

	var foundNames []string
	for _, h := range tools {
		name := h.Name()
		defName := h.Definition().Name
		foundNames = append(foundNames, name)

		if _, ok := expectedNames[name]; !ok {
			t.Errorf("unexpected tool: %q", name)
		}
		if defName != name {
			t.Errorf("tool[%q].Definition().Name = %q, want Name() = %q", name, defName, name)
		}
	}

	// Verify all three expected names are present
	for _, want := range expectedNames {
		found := false
		for _, got := range foundNames {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing expected tool: %q", want)
		}
	}

			// Verify required parameters for each handler
	for _, h := range tools {
		def := h.Definition()
		switch def.Name {
		case "read_file":
			if len(def.Required) != 1 || def.Required[0] != "path" {
				t.Errorf("read_file Required = %v, want [\"path\"]", def.Required)
			}
		case "list_directory":
			if len(def.Required) != 0 {
				t.Errorf("list_directory Required = %v, want nil/empty", def.Required)
			}
		case "fetch_url":
			if len(def.Required) != 1 || def.Required[0] != "url" {
				t.Errorf("fetch_url Required = %v, want [\"url\"]", def.Required)
			}
		case "search_files":
			if len(def.Required) != 1 || def.Required[0] != "search_pattern" {
				t.Errorf("search_files Required = %v, want [\"search_pattern\"]", def.Required)
			}
		case "repo_map":
			if len(def.Required) != 0 {
				t.Errorf("repo_map Required = %v, want nil/empty", def.Required)
			}
		case "list_memories":
			if len(def.Required) != 0 {
				t.Errorf("list_memories Required = %v, want nil/empty", def.Required)
			}
		case "read_memory":
			if len(def.Required) != 1 || def.Required[0] != "name" {
				t.Errorf("read_memory Required = %v, want [\"name\"]", def.Required)
			}
		case "rollback_changes":
			if len(def.Required) != 0 {
				t.Errorf("rollback_changes Required = %v, want nil/empty", def.Required)
			}
		case "view_history":
			if len(def.Required) != 0 {
				t.Errorf("view_history Required = %v, want nil/empty", def.Required)
			}
		case "list_skills":
			if len(def.Required) != 0 {
				t.Errorf("list_skills Required = %v, want nil/empty", def.Required)
			}
		case "embedding_index":
			if len(def.Required) != 1 || def.Required[0] != "operation" {
				t.Errorf("embedding_index Required = %v, want [\"operation\"]", def.Required)
			}
		case "write_file":
			if len(def.Required) != 2 || def.Required[0] != "path" || def.Required[1] != "content" {
				t.Errorf("write_file Required = %v, want [\"path\" \"content\"]", def.Required)
			}
		case "write_structured_file":
			if len(def.Required) != 2 || def.Required[0] != "path" || def.Required[1] != "data" {
				t.Errorf("write_structured_file Required = %v, want [\"path\" \"data\"]", def.Required)
			}
		case "edit_file":
			if len(def.Required) != 3 || def.Required[0] != "path" || def.Required[1] != "old_str" || def.Required[2] != "new_str" {
				t.Errorf("edit_file Required = %v, want [\"path\" \"old_str\" \"new_str\"]", def.Required)
			}
		case "shell_command":
			if len(def.Required) != 0 {
				t.Errorf("shell_command Required = %v, want nil/empty", def.Required)
			}
		case "save_memory":
			if len(def.Required) != 2 || def.Required[0] != "name" || def.Required[1] != "content" {
				t.Errorf("save_memory Required = %v, want [\"name\" \"content\"]", def.Required)
			}
		case "search_memories":
			if len(def.Required) != 1 || def.Required[0] != "query" {
				t.Errorf("search_memories Required = %v, want [\"query\"]", def.Required)
			}
		}
	}
}// ---------------------------------------------------------------------------
// Unregister Tests
// ---------------------------------------------------------------------------

func TestToolRegistryUnregister(t *testing.T) {
	t.Parallel()

	t.Run("existing tool", func(t *testing.T) {
		t.Parallel()
		r := NewToolRegistry()
		h := &mockHandler{name: "test"}
		require.NoError(t, r.Register(h))
		require.True(t, r.Unregister("test"))
		_, found := r.Lookup("test")
		require.False(t, found)
	})

	t.Run("nonexistent tool", func(t *testing.T) {
		t.Parallel()
		r := NewToolRegistry()
		require.False(t, r.Unregister("nonexistent"))
	})
}

// ---------------------------------------------------------------------------
// ApprovalResult Tests
// ---------------------------------------------------------------------------

func TestApprovalResult(t *testing.T) {
	t.Parallel()

	t.Run("approved", func(t *testing.T) {
		t.Parallel()
		r := ApprovalResult{Approved: true, Reason: "approved", UserComment: "looks good"}
		require.True(t, r.Approved)
		require.Equal(t, "approved", r.Reason)
		require.Equal(t, "looks good", r.UserComment)
	})

	t.Run("rejected", func(t *testing.T) {
		t.Parallel()
		r := ApprovalResult{Approved: false, Reason: "timed_out"}
		require.False(t, r.Approved)
		require.Equal(t, "timed_out", r.Reason)
	})
}
