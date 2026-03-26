package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/factory"
	"github.com/alantheprice/ledit/pkg/mcp"
)

type fakeMCPManager struct {
	tools         []mcp.MCPTool
	callResult    *mcp.MCPToolCallResult
	lastServer    string
	lastTool      string
	lastArguments map[string]interface{}
}

func (f *fakeMCPManager) AddServer(config mcp.MCPServerConfig) error  { return nil }
func (f *fakeMCPManager) RemoveServer(name string) error              { return nil }
func (f *fakeMCPManager) GetServer(name string) (mcp.MCPServer, bool) { return nil, false }
func (f *fakeMCPManager) ListServers() []mcp.MCPServer                { return nil }
func (f *fakeMCPManager) StartAll(ctx context.Context) error          { return nil }
func (f *fakeMCPManager) StopAll(ctx context.Context) error           { return nil }

func (f *fakeMCPManager) GetAllTools(ctx context.Context) ([]mcp.MCPTool, error) {
	return f.tools, nil
}

func (f *fakeMCPManager) CallTool(ctx context.Context, serverName, toolName string, args map[string]interface{}) (*mcp.MCPToolCallResult, error) {
	f.lastServer = serverName
	f.lastTool = toolName
	f.lastArguments = args

	if f.callResult != nil {
		return f.callResult, nil
	}

	return &mcp.MCPToolCallResult{
		Content: []mcp.MCPContent{{Type: "text", Text: "ok"}},
		IsError: false,
	}, nil
}

func TestToolExecutorHandlesMCPMetaList(t *testing.T) {
	manager := &fakeMCPManager{
		tools: []mcp.MCPTool{{
			Name:        "hello",
			Description: "say hello",
			ServerName:  "test",
		}},
	}

	agent := &Agent{
		mcpManager:   manager,
		interruptCtx: context.Background(),
		outputMutex:  &sync.Mutex{},
	}

	executor := NewToolExecutor(agent)

	tc := api.ToolCall{ID: "call_1", Type: "function"}
	tc.Function.Name = "mcp_tools"
	args := map[string]interface{}{"action": "list"}
	payload, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	tc.Function.Arguments = string(payload)

	msg := executor.executeSingleTool(tc)

	if !strings.Contains(msg.Content, "mcp_test_hello") {
		t.Fatalf("expected list output to include MCP tool name, got: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "Available MCP tools (1)") {
		t.Fatalf("expected count in output, got: %q", msg.Content)
	}
}

func TestToolExecutorFallbacksToMCPExecution(t *testing.T) {
	manager := &fakeMCPManager{
		tools: []mcp.MCPTool{{
			Name:        "hello",
			Description: "say hello",
			ServerName:  "test",
		}},
		callResult: &mcp.MCPToolCallResult{
			Content: []mcp.MCPContent{{Type: "text", Text: "hi"}},
			IsError: false,
		},
	}

	agent := &Agent{
		mcpManager:   manager,
		interruptCtx: context.Background(),
		outputMutex:  &sync.Mutex{},
	}

	executor := NewToolExecutor(agent)

	tc := api.ToolCall{ID: "call_2", Type: "function"}
	tc.Function.Name = "mcp_test_hello"
	tc.Function.Arguments = "{}"

	msg := executor.executeSingleTool(tc)

	if msg.Content != "hi" {
		t.Fatalf("expected MCP call result 'hi', got: %q", msg.Content)
	}
	if manager.lastServer != "test" || manager.lastTool != "hello" {
		t.Fatalf("unexpected MCP call routing: server=%q tool=%q", manager.lastServer, manager.lastTool)
	}
}

func TestToolExecutorTranslatesLegacyMCPNames(t *testing.T) {
	manager := &fakeMCPManager{
		tools: []mcp.MCPTool{{
			Name:        "search",
			Description: "search GitHub",
			ServerName:  "github",
		}},
	}

	agent := &Agent{
		mcpManager:   manager,
		interruptCtx: context.Background(),
		outputMutex:  &sync.Mutex{},
	}

	executor := NewToolExecutor(agent)

	tc := api.ToolCall{ID: "call_legacy", Type: "function"}
	tc.Function.Name = "github:search"
	tc.Function.Arguments = "{}"

	msg := executor.executeSingleTool(tc)

	if msg.Content != "ok" {
		t.Fatalf("expected MCP call result 'ok', got: %q", msg.Content)
	}
	if manager.lastServer != "github" || manager.lastTool != "search" {
		t.Fatalf("unexpected MCP call routing: server=%q tool=%q", manager.lastServer, manager.lastTool)
	}
}

func TestToolExecutorAppliesOpenFileAlias(t *testing.T) {
	agent := &Agent{
		client:       &providerOverrideClient{TestClient: &factory.TestClient{}, provider: "openrouter"},
		interruptCtx: context.Background(),
		outputMutex:  &sync.Mutex{},
	}

	executor := NewToolExecutor(agent)

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "alias_open_file.txt")
	if err := os.WriteFile(filePath, []byte("alias path works"), 0o644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	tc := api.ToolCall{ID: "call_open_file_alias", Type: "function"}
	tc.Function.Name = "open_file"
	tc.Function.Arguments = `{"path":"` + filePath + `"}`

	msg := executor.executeSingleTool(tc)
	if !strings.Contains(msg.Content, "alias path works") {
		t.Fatalf("expected open_file alias to resolve to read_file, got: %q", msg.Content)
	}
}

func TestSanitizeToolFailureMessage_RedactsAndTruncates(t *testing.T) {
	msg := "HTTP 500: failed for data:application/pdf;base64," + strings.Repeat("A", 6000)
	safe := sanitizeToolFailureMessage(msg)
	if strings.Contains(safe, "base64,AAAA") {
		t.Fatalf("expected base64 payload to be redacted")
	}
	if !strings.Contains(safe, "base64,[REDACTED]") {
		t.Fatalf("expected redaction marker in sanitized error")
	}
	if len(safe) > maxToolFailureMessageChars+20 {
		t.Fatalf("expected sanitized message to be bounded, got length=%d", len(safe))
	}
}

func TestParseToolArgumentsWithRepair_MissingClosingBrace(t *testing.T) {
	args, repaired, err := parseToolArgumentsWithRepair(`{"path":"README.md"`)
	if err != nil {
		t.Fatalf("expected repaired args, got error: %v", err)
	}
	if !repaired {
		t.Fatalf("expected repair flag to be true")
	}
	if got, ok := args["path"].(string); !ok || got != "README.md" {
		t.Fatalf("unexpected parsed args: %#v", args)
	}
}

func TestParseToolArgumentsWithRepair_ExtractsObjectFromTrailingText(t *testing.T) {
	args, repaired, err := parseToolArgumentsWithRepair("{\"path\":\"README.md\"}\nNow I will continue...")
	if err != nil {
		t.Fatalf("expected repaired args, got error: %v", err)
	}
	if !repaired {
		t.Fatalf("expected repair flag to be true")
	}
	if got, ok := args["path"].(string); !ok || got != "README.md" {
		t.Fatalf("unexpected parsed args: %#v", args)
	}
}

func TestParseToolArgumentsWithRepair_RemovesTrailingCommas(t *testing.T) {
	args, repaired, err := parseToolArgumentsWithRepair(`{"path":"README.md","data":{"k":"v",},}`)
	if err != nil {
		t.Fatalf("expected repaired args, got error: %v", err)
	}
	if !repaired {
		t.Fatalf("expected repair flag to be true")
	}
	if got, ok := args["path"].(string); !ok || got != "README.md" {
		t.Fatalf("unexpected parsed args: %#v", args)
	}
}

func TestExecuteSingleTool_UsesRepairedArguments(t *testing.T) {
	agent := &Agent{
		client:       &providerOverrideClient{TestClient: &factory.TestClient{}, provider: "openrouter"},
		interruptCtx: context.Background(),
		outputMutex:  &sync.Mutex{},
	}
	executor := NewToolExecutor(agent)

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "repaired_args.txt")
	if err := os.WriteFile(filePath, []byte("repaired args work"), 0o644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	tc := api.ToolCall{ID: "call_repair", Type: "function"}
	tc.Function.Name = "read_file"
	tc.Function.Arguments = `{"path":"` + filePath + `"`

	msg := executor.executeSingleTool(tc)
	if !strings.Contains(msg.Content, "repaired args work") {
		t.Fatalf("expected repaired arguments to allow tool execution, got: %q", msg.Content)
	}
}

type providerOverrideClient struct {
	*factory.TestClient
	provider string
}

func (c *providerOverrideClient) GetProvider() string {
	return c.provider
}

func TestCanExecuteInParallelFetchURL(t *testing.T) {
	agent := &Agent{
		client:       &providerOverrideClient{TestClient: &factory.TestClient{}, provider: "openrouter"},
		interruptCtx: context.Background(),
		outputMutex:  &sync.Mutex{},
	}
	executor := NewToolExecutor(agent)

	calls := []api.ToolCall{
		{Type: "function"},
		{Type: "function"},
	}
	calls[0].Function.Name = "fetch_url"
	calls[0].Function.Arguments = `{"url":"https://example.com/a"}`
	calls[1].Function.Name = "fetch_url"
	calls[1].Function.Arguments = `{"url":"https://example.com/b"}`

	if !executor.canExecuteInParallel(calls) {
		t.Fatalf("expected fetch_url batch to execute in parallel")
	}
}

func TestCanExecuteInParallelMixedBatchDenied(t *testing.T) {
	agent := &Agent{
		client:       &providerOverrideClient{TestClient: &factory.TestClient{}, provider: "openrouter"},
		interruptCtx: context.Background(),
		outputMutex:  &sync.Mutex{},
	}
	executor := NewToolExecutor(agent)

	calls := []api.ToolCall{
		{Type: "function"},
		{Type: "function"},
	}
	calls[0].Function.Name = "fetch_url"
	calls[0].Function.Arguments = `{"url":"https://example.com/a"}`
	calls[1].Function.Name = "read_file"
	calls[1].Function.Arguments = `{"path":"README.md"}`

	if executor.canExecuteInParallel(calls) {
		t.Fatalf("expected mixed tool batch to remain sequential")
	}
}

func TestCanExecuteInParallelProviderOrderingRestrictions(t *testing.T) {
	agent := &Agent{
		client:       &providerOverrideClient{TestClient: &factory.TestClient{}, provider: "deepseek"},
		interruptCtx: context.Background(),
		outputMutex:  &sync.Mutex{},
	}
	executor := NewToolExecutor(agent)

	calls := []api.ToolCall{
		{Type: "function"},
		{Type: "function"},
	}
	calls[0].Function.Name = "fetch_url"
	calls[0].Function.Arguments = `{"url":"https://example.com/a"}`
	calls[1].Function.Name = "fetch_url"
	calls[1].Function.Arguments = `{"url":"https://example.com/b"}`

	if executor.canExecuteInParallel(calls) {
		t.Fatalf("expected deepseek provider to keep strict sequential ordering")
	}
}

func TestFormatToolCall_TodoWriteIncludesChecklistSummary(t *testing.T) {
	tc := api.ToolCall{Type: "function"}
	tc.Function.Name = "TodoWrite"
	tc.Function.Arguments = `{"todos":[{"content":"First","status":"pending"},{"content":"Second","status":"in_progress"},{"content":"Third","status":"completed"}]}`

	got := formatToolCall(tc)
	if !strings.Contains(got, "todos=3") {
		t.Fatalf("expected todo count in formatted call, got: %q", got)
	}
	if !strings.Contains(got, "[ ]=1") || !strings.Contains(got, "[~]=1") || !strings.Contains(got, "[x]=1") {
		t.Fatalf("expected status breakdown in formatted call, got: %q", got)
	}
}

func TestFormatToolCall_UsesRepairPath(t *testing.T) {
	tc := api.ToolCall{Type: "function"}
	tc.Function.Name = "run_subagent"
	tc.Function.Arguments = `{"prompt":"review the diff","persona":"code_reviewer"}`

	got := formatToolCall(tc)
	if !strings.Contains(got, "run_subagent") {
		t.Fatalf("expected tool name in formatted call, got: %q", got)
	}

	tc.Function.Arguments = `{"prompt":"review the diff","persona":"code_reviewer"`
	got = formatToolCall(tc)
	if !strings.Contains(got, "run_subagent") {
		t.Fatalf("expected repaired formatted call to still include tool name, got: %q", got)
	}
}

func TestTodoStatusSymbol(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{status: "pending", want: "[ ]"},
		{status: "in_progress", want: "[~]"},
		{status: "completed", want: "[x]"},
		{status: "cancelled", want: "[-]"},
		{status: "other", want: "[?]"},
	}

	for _, tt := range tests {
		if got := todoStatusSymbol(tt.status); got != tt.want {
			t.Fatalf("todoStatusSymbol(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestCanExecuteInParallelSearchFiles(t *testing.T) {
	agent := &Agent{
		client:       &providerOverrideClient{TestClient: &factory.TestClient{}, provider: "openrouter"},
		interruptCtx: context.Background(),
		outputMutex:  &sync.Mutex{},
	}
	executor := NewToolExecutor(agent)

	calls := []api.ToolCall{
		{Type: "function"},
		{Type: "function"},
	}
	calls[0].Function.Name = "search_files"
	calls[0].Function.Arguments = `{"search_pattern":"foo","file_glob":"*.go"}`
	calls[1].Function.Name = "search_files"
	calls[1].Function.Arguments = `{"search_pattern":"bar","file_glob":"*.go"}`

	if !executor.canExecuteInParallel(calls) {
		t.Fatalf("expected search_files batch to execute in parallel")
	}
}

func TestCanExecuteInParallelSearchFilesProviderRestrictions(t *testing.T) {
	agent := &Agent{
		client:       &providerOverrideClient{TestClient: &factory.TestClient{}, provider: "minimax"},
		interruptCtx: context.Background(),
		outputMutex:  &sync.Mutex{},
	}
	executor := NewToolExecutor(agent)

	calls := []api.ToolCall{
		{Type: "function"},
		{Type: "function"},
	}
	calls[0].Function.Name = "search_files"
	calls[0].Function.Arguments = `{"search_pattern":"foo","file_glob":"*.go"}`
	calls[1].Function.Name = "search_files"
	calls[1].Function.Arguments = `{"search_pattern":"bar","file_glob":"*.go"}`

	if executor.canExecuteInParallel(calls) {
		t.Fatalf("expected minimax provider to keep strict sequential ordering")
	}
}

func TestConstrainToolResultForModel_NonFetchURLUnchanged(t *testing.T) {
	input := strings.Repeat("a", defaultFetchURLResultMaxChars+1000)
	got := constrainToolResultForModel("read_file", nil, input)
	if got != input {
		t.Fatalf("expected non-fetch tool output to remain unchanged")
	}
}

func TestConstrainToolResultForModel_FetchURLTruncatesLargeOutput(t *testing.T) {
	t.Setenv("LEDIT_FETCH_URL_MAX_CHARS", "100")
	input := strings.Repeat("x", 220)

	got := constrainToolResultForModel("fetch_url", map[string]interface{}{"url": "https://example.com"}, input)
	if !strings.Contains(got, "FETCH_URL OUTPUT TRUNCATED FOR MODEL CONTEXT") {
		t.Fatalf("expected truncation marker in output")
	}
	if !strings.Contains(got, "Full output saved to ") {
		t.Fatalf("expected saved path marker in output")
	}
	if !strings.HasPrefix(got, strings.Repeat("x", 70)) {
		t.Fatalf("expected head segment to be preserved")
	}
	if !strings.HasSuffix(got, strings.Repeat("x", 30)) {
		t.Fatalf("expected tail segment to be preserved")
	}
}

func TestConstrainToolResultForModel_FetchURLRespectsSmallOutput(t *testing.T) {
	t.Setenv("LEDIT_FETCH_URL_MAX_CHARS", "500")
	input := strings.Repeat("b", 120)

	got := constrainToolResultForModel("fetch_url", nil, input)
	if got != input {
		t.Fatalf("expected output below limit to remain unchanged")
	}
}

func TestConstrainToolResultForModel_FetchURLSavesFullOutputToArchive(t *testing.T) {
	t.Setenv("LEDIT_FETCH_URL_MAX_CHARS", "100")
	archiveDir := t.TempDir()
	t.Setenv("LEDIT_FETCH_URL_ARCHIVE_DIR", archiveDir)

	input := strings.Repeat("z", 220)
	got := constrainToolResultForModel("fetch_url", map[string]interface{}{"url": "https://example.com/long"}, input)

	if !strings.Contains(got, "Full output saved to ") {
		t.Fatalf("expected full output path in truncation notice")
	}

	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("failed to read archive dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 archived file, got %d", len(entries))
	}

	path := filepath.Join(archiveDir, entries[0].Name())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read archived file: %v", err)
	}

	text := string(data)
	if !strings.Contains(text, "URL: https://example.com/long") {
		t.Fatalf("expected URL header in archived file")
	}
	if !strings.Contains(text, input) {
		t.Fatalf("expected full fetch output in archived file")
	}
}

func TestConstrainToolResultForModel_AnalyzeImageContentCompactsJSONResult(t *testing.T) {
	longText := strings.Repeat("menu item with description\n", 400)
	raw, err := json.Marshal(tools.ImageAnalysisResponse{
		Success:        true,
		ToolInvoked:    true,
		InputResolved:  true,
		OCRAttempted:   true,
		InputType:      "remote_url",
		InputPath:      "https://example.com/menu.png",
		ExtractedText:  longText,
		OriginalChars:  len(longText),
		ReturnedChars:  len(longText),
		FullOutputPath: "/tmp/vision/menu.txt",
		Analysis: &tools.VisionAnalysis{
			ImagePath:   "https://example.com/menu.png",
			Description: longText,
		},
	})
	if err != nil {
		t.Fatalf("marshal OCR result: %v", err)
	}

	got := constrainToolResultForModel("analyze_image_content", nil, string(raw))

	if got == string(raw) {
		t.Fatalf("expected analyze_image_content result to be compacted")
	}
	if !strings.Contains(got, "analyze_image_content result:") {
		t.Fatalf("expected compacted header, got: %q", got)
	}
	if !strings.Contains(got, "- full_output_path: /tmp/vision/menu.txt") {
		t.Fatalf("expected full output path to be preserved, got: %q", got)
	}
	if !strings.Contains(got, "[EXCERPT TRUNCATED:") {
		t.Fatalf("expected excerpt truncation marker, got: %q", got)
	}
	if strings.Count(got, "menu item with description") > 200 {
		t.Fatalf("expected OCR text to be substantially compacted")
	}
}

func TestConstrainToolResultForModel_AnalyzeImageContentLeavesInvalidJSONUntouched(t *testing.T) {
	input := "not-json"
	got := constrainToolResultForModel("analyze_image_content", nil, input)
	if got != input {
		t.Fatalf("expected invalid OCR payload to remain unchanged")
	}
}
