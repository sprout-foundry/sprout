package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/alantheprice/ledit/pkg/mcp"
	"github.com/alantheprice/ledit/pkg/webcontent"
)

// githubRouterMCPManager is a test MCPManager that supports GetServer
// with a configurable fakeMCPServer and tracks CallTool invocations.
type githubRouterMCPManager struct {
	servers      map[string]mcp.MCPServer
	callResult   *mcp.MCPToolCallResult
	callErr      error
	lastServer   string
	lastTool     string
	lastArgs     map[string]interface{}
	callCount    int
	mu           sync.Mutex
}

func newGitHubRouterMCPManager() *githubRouterMCPManager {
	return &githubRouterMCPManager{
		servers: make(map[string]mcp.MCPServer),
	}
}

func (m *githubRouterMCPManager) withGitHubServer(running bool) *githubRouterMCPManager {
	m.servers["github"] = &routerFakeMCPServer{name: "github", running: running}
	return m
}

func (m *githubRouterMCPManager) withCallResult(r *mcp.MCPToolCallResult) *githubRouterMCPManager {
	m.callResult = r
	return m
}

func (m *githubRouterMCPManager) withCallErr(err error) *githubRouterMCPManager {
	m.callErr = err
	return m
}

func (m *githubRouterMCPManager) AddServer(config mcp.MCPServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers[config.Name] = nil // just record existence
	return nil
}
func (m *githubRouterMCPManager) RemoveServer(name string) error     { return nil }
func (m *githubRouterMCPManager) GetServer(name string) (mcp.MCPServer, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.servers[name]
	return s, ok
}
func (m *githubRouterMCPManager) ListServers() []mcp.MCPServer       { return nil }
func (m *githubRouterMCPManager) StartAll(ctx context.Context) error  { return nil }
func (m *githubRouterMCPManager) StopAll(ctx context.Context) error   { return nil }
func (m *githubRouterMCPManager) GetAllTools(ctx context.Context) ([]mcp.MCPTool, error) {
	return nil, nil
}
func (m *githubRouterMCPManager) CallTool(ctx context.Context, serverName, toolName string, args map[string]interface{}) (*mcp.MCPToolCallResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastServer = serverName
	m.lastTool = toolName
	m.lastArgs = args
	m.callCount++
	if m.callErr != nil {
		return nil, m.callErr
	}
	return m.callResult, nil
}

// routerFakeMCPServer is a minimal fake MCPServer for router tests.
type routerFakeMCPServer struct {
	name    string
	running bool
}

func (f *routerFakeMCPServer) Start(_ context.Context) error                              { f.running = true; return nil }
func (f *routerFakeMCPServer) Stop(_ context.Context) error                               { f.running = false; return nil }
func (f *routerFakeMCPServer) IsRunning() bool                                            { return f.running }
func (f *routerFakeMCPServer) GetName() string                                            { return f.name }
func (f *routerFakeMCPServer) GetConfig() mcp.MCPServerConfig                              { return mcp.MCPServerConfig{} }
func (f *routerFakeMCPServer) Initialize(_ context.Context) error                          { return nil }
func (f *routerFakeMCPServer) ListTools(_ context.Context) ([]mcp.MCPTool, error)         { return nil, nil }
func (f *routerFakeMCPServer) CallTool(_ context.Context, _ mcp.MCPToolCallRequest) (*mcp.MCPToolCallResult, error) {
	return nil, nil
}
func (f *routerFakeMCPServer) ListResources(_ context.Context) ([]mcp.MCPResource, error)  { return nil, nil }
func (f *routerFakeMCPServer) ReadResource(_ context.Context, _ string) (*mcp.MCPContent, error) { return nil, nil }
func (f *routerFakeMCPServer) ListPrompts(_ context.Context) ([]mcp.MCPPrompt, error)          { return nil, nil }
func (f *routerFakeMCPServer) GetPrompt(_ context.Context, _ string, _ map[string]interface{}) (*mcp.MCPContent, error) {
	return nil, nil
}

// testAgent creates a minimal Agent for testing with the given MCPManager.
func testAgent(mgr mcp.MCPManager) *Agent {
	return &Agent{
		mcpManager:  mgr,
		interruptCtx: context.Background(),
		outputMutex:  &sync.Mutex{},
	}
}

// --- tryRouteGitHubToMCP tests ---

func TestTryRouteGitHubToMCP_NilManager(t *testing.T) {
	a := &Agent{}
	content, handled, err := a.tryRouteGitHubToMCP(context.Background(), "https://github.com/owner/repo/issues/42")
	if handled {
		t.Error("expected handled=false when mcpManager is nil")
	}
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}

func TestTryRouteGitHubToMCP_NonGitHubURL(t *testing.T) {
	mgr := newGitHubRouterMCPManager()
	a := testAgent(mgr)

	content, handled, err := a.tryRouteGitHubToMCP(context.Background(), "https://example.com/page")
	if handled {
		t.Error("expected handled=false for non-GitHub URL")
	}
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}

func TestTryRouteGitHubToMCP_GitHubServerNotAvailable(t *testing.T) {
	mgr := newGitHubRouterMCPManager()
	// Don't add a github server
	a := testAgent(mgr)

	content, handled, err := a.tryRouteGitHubToMCP(context.Background(), "https://github.com/owner/repo/issues/42")
	if handled {
		t.Error("expected handled=false when GitHub MCP server is not configured")
	}
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}

func TestTryRouteGitHubToMCP_GitHubServerNotRunning(t *testing.T) {
	mgr := newGitHubRouterMCPManager().withGitHubServer(false)
	a := testAgent(mgr)

	content, handled, err := a.tryRouteGitHubToMCP(context.Background(), "https://github.com/owner/repo/issues/42")
	if handled {
		t.Error("expected handled=false when GitHub MCP server is not running")
	}
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}

func TestTryRouteGitHubToMCP_IssueURL(t *testing.T) {
	mgr := newGitHubRouterMCPManager().withGitHubServer(true).withCallResult(&mcp.MCPToolCallResult{
		Content: []mcp.MCPContent{{Type: "text", Text: "Issue #42: Fix bug"}},
		IsError: false,
	})
	a := testAgent(mgr)

	content, handled, err := a.tryRouteGitHubToMCP(context.Background(), "https://github.com/alantheprice/ledit/issues/42")
	if !handled {
		t.Fatal("expected handled=true for issue URL")
	}
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if content != "Issue #42: Fix bug" {
		t.Errorf("unexpected content: %q", content)
	}

	// Verify the MCP call was made correctly.
	if mgr.lastServer != "github" {
		t.Errorf("expected server 'github', got %q", mgr.lastServer)
	}
	if mgr.lastTool != "get_issue" {
		t.Errorf("expected tool 'get_issue', got %q", mgr.lastTool)
	}
	if mgr.lastArgs["owner"] != "alantheprice" {
		t.Errorf("expected owner 'alantheprice', got %v", mgr.lastArgs["owner"])
	}
	if mgr.lastArgs["repo"] != "ledit" {
		t.Errorf("expected repo 'ledit', got %v", mgr.lastArgs["repo"])
	}
	if mgr.lastArgs["issue_number"] != float64(42) {
		t.Errorf("expected issue_number 42.0, got %v", mgr.lastArgs["issue_number"])
	}
}

func TestTryRouteGitHubToMCP_PullRequestURL(t *testing.T) {
	mgr := newGitHubRouterMCPManager().withGitHubServer(true).withCallResult(&mcp.MCPToolCallResult{
		Content: []mcp.MCPContent{{Type: "text", Text: "PR #7: Add feature"}},
		IsError: false,
	})
	a := testAgent(mgr)

	content, handled, err := a.tryRouteGitHubToMCP(context.Background(), "https://github.com/alantheprice/ledit/pull/7")
	if !handled {
		t.Fatal("expected handled=true for pull request URL")
	}
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if content != "PR #7: Add feature" {
		t.Errorf("unexpected content: %q", content)
	}

	if mgr.lastTool != "get_pull_request" {
		t.Errorf("expected tool 'get_pull_request', got %q", mgr.lastTool)
	}
	if mgr.lastArgs["pull_number"] != float64(7) {
		t.Errorf("expected pull_number 7.0, got %v", mgr.lastArgs["pull_number"])
	}
}

func TestTryRouteGitHubToMCP_RepoURL(t *testing.T) {
	mgr := newGitHubRouterMCPManager().withGitHubServer(true).withCallResult(&mcp.MCPToolCallResult{
		Content: []mcp.MCPContent{{Type: "text", Text: "Repository: ledit"}},
		IsError: false,
	})
	a := testAgent(mgr)

	content, handled, err := a.tryRouteGitHubToMCP(context.Background(), "https://github.com/alantheprice/ledit")
	if !handled {
		t.Fatal("expected handled=true for repo URL")
	}
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if content != "Repository: ledit" {
		t.Errorf("unexpected content: %q", content)
	}

	if mgr.lastTool != "search_repositories" {
		t.Errorf("expected tool 'search_repositories', got %q", mgr.lastTool)
	}
	expectedQ := "repo:alantheprice/ledit"
	if mgr.lastArgs["q"] != expectedQ {
		t.Errorf("expected q %q, got %v", expectedQ, mgr.lastArgs["q"])
	}
}

func TestTryRouteGitHubToMCP_FileURL_NotHandled(t *testing.T) {
	// File URLs should not be routed to MCP — they're handled by the raw rewrite.
	mgr := newGitHubRouterMCPManager().withGitHubServer(true)
	a := testAgent(mgr)

	_, handled, _ := a.tryRouteGitHubToMCP(context.Background(), "https://github.com/alantheprice/ledit/blob/main/README.md")
	if handled {
		t.Error("expected handled=false for file (blob) URL")
	}
	if mgr.callCount != 0 {
		t.Error("expected no MCP calls for file URL")
	}
}

func TestTryRouteGitHubToMCP_DirectoryURL_NotHandled(t *testing.T) {
	mgr := newGitHubRouterMCPManager().withGitHubServer(true)
	a := testAgent(mgr)

	_, handled, _ := a.tryRouteGitHubToMCP(context.Background(), "https://github.com/alantheprice/ledit/tree/main/pkg")
	if handled {
		t.Error("expected handled=false for directory (tree) URL")
	}
	if mgr.callCount != 0 {
		t.Error("expected no MCP calls for directory URL")
	}
}

func TestTryRouteGitHubToMCP_GistURL_NotHandled(t *testing.T) {
	mgr := newGitHubRouterMCPManager().withGitHubServer(true)
	a := testAgent(mgr)

	_, handled, _ := a.tryRouteGitHubToMCP(context.Background(), "https://gist.github.com/abc123")
	if handled {
		t.Error("expected handled=false for gist URL")
	}
	if mgr.callCount != 0 {
		t.Error("expected no MCP calls for gist URL")
	}
}

func TestTryRouteGitHubToMCP_MCPError_FallsThrough(t *testing.T) {
	mgr := newGitHubRouterMCPManager().
		withGitHubServer(true).
		withCallErr(fmt.Errorf("server unreachable"))
	a := testAgent(mgr)

	content, handled, err := a.tryRouteGitHubToMCP(context.Background(), "https://github.com/owner/repo/issues/1")
	if handled {
		t.Error("expected handled=false when MCP call fails")
	}
	if err != nil {
		t.Errorf("expected nil error (graceful fallback), got %v", err)
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}

func TestTryRouteGitHubToMCP_MCPResultIsError_FallsThrough(t *testing.T) {
	mgr := newGitHubRouterMCPManager().withGitHubServer(true).withCallResult(&mcp.MCPToolCallResult{
		Content: []mcp.MCPContent{{Type: "text", Text: "Not found"}},
		IsError: true,
	})
	a := testAgent(mgr)

	_, handled, err := a.tryRouteGitHubToMCP(context.Background(), "https://github.com/owner/repo/issues/99999")
	if handled {
		t.Error("expected handled=false when MCP returns IsError=true")
	}
	if err != nil {
		t.Errorf("expected nil error (graceful fallback), got %v", err)
	}
}

func TestTryRouteGitHubToMCP_MultipleContentParts(t *testing.T) {
	// Verify formatMCPResult concatenates multiple content parts.
	mgr := newGitHubRouterMCPManager().withGitHubServer(true).withCallResult(&mcp.MCPToolCallResult{
		Content: []mcp.MCPContent{
			{Type: "text", Text: "Line 1"},
			{Type: "text", Text: "Line 2"},
		},
		IsError: false,
	})
	a := testAgent(mgr)

	content, handled, err := a.tryRouteGitHubToMCP(context.Background(), "https://github.com/owner/repo/issues/3")
	if !handled {
		t.Fatal("expected handled=true")
	}
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if content != "Line 1\nLine 2" {
		t.Errorf("expected concatenated content, got %q", content)
	}
}

func TestTryRouteGitHubToMCP_NilResult_Handled(t *testing.T) {
	// A nil result from CallTool (nil, nil) should still be handled with "No result".
	mgr := newGitHubRouterMCPManager().withGitHubServer(true).withCallResult(nil)
	a := testAgent(mgr)

	content, handled, err := a.tryRouteGitHubToMCP(context.Background(), "https://github.com/owner/repo/issues/1")
	if !handled {
		t.Fatal("expected handled=true even with nil result")
	}
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if content != "No result" {
		t.Errorf("expected 'No result', got %q", content)
	}
}

// --- buildGitHubMCPArgs tests ---

func TestBuildGitHubMCPArgs_Issue(t *testing.T) {
	args, err := buildGitHubMCPArgs(webcontent.GitHubURLInfo{
		Type:   "issue",
		Owner:  "octocat",
		Repo:   "hello-world",
		Number: 42,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args["owner"] != "octocat" {
		t.Errorf("expected owner 'octocat', got %v", args["owner"])
	}
	if args["repo"] != "hello-world" {
		t.Errorf("expected repo 'hello-world', got %v", args["repo"])
	}
	if args["issue_number"] != float64(42) {
		t.Errorf("expected issue_number 42.0, got %v", args["issue_number"])
	}
}

func TestBuildGitHubMCPArgs_PullRequest(t *testing.T) {
	args, err := buildGitHubMCPArgs(webcontent.GitHubURLInfo{
		Type:   "pull_request",
		Owner:  "octocat",
		Repo:   "hello-world",
		Number: 7,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args["pull_number"] != float64(7) {
		t.Errorf("expected pull_number 7.0, got %v", args["pull_number"])
	}
}

func TestBuildGitHubMCPArgs_Repo(t *testing.T) {
	args, err := buildGitHubMCPArgs(webcontent.GitHubURLInfo{
		Type:  "repo",
		Owner: "octocat",
		Repo:  "hello-world",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args["q"] != "repo:octocat/hello-world" {
		t.Errorf("expected q 'repo:octocat/hello-world', got %v", args["q"])
	}
}

func TestBuildGitHubMCPArgs_UnsupportedType(t *testing.T) {
	_, err := buildGitHubMCPArgs(webcontent.GitHubURLInfo{Type: "file"})
	if err == nil {
		t.Error("expected error for unsupported type 'file'")
	}
}

func TestBuildGitHubMCPArgs_Commit(t *testing.T) {
	args, err := buildGitHubMCPArgs(webcontent.GitHubURLInfo{
		Type:  "commit",
		Owner: "octocat",
		Repo:  "hello-world",
		Ref:   "abc123def",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args["sha"] != "abc123def" {
		t.Errorf("expected sha 'abc123def', got %v", args["sha"])
	}
}

func TestBuildGitHubMCPArgs_Discussion(t *testing.T) {
	args, err := buildGitHubMCPArgs(webcontent.GitHubURLInfo{
		Type:   "discussion",
		Owner:  "octocat",
		Repo:   "hello-world",
		Number: 42,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args["discussion_number"] != float64(42) {
		t.Errorf("expected discussion_number 42.0, got %v", args["discussion_number"])
	}
}

func TestBuildGitHubMCPArgs_ActionsRun(t *testing.T) {
	args, err := buildGitHubMCPArgs(webcontent.GitHubURLInfo{
		Type:   "actions_run",
		Owner:  "octocat",
		Repo:   "hello-world",
		Number: 12345,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args["method"] != "get_workflow_run" {
		t.Errorf("expected method 'get_workflow_run', got %v", args["method"])
	}
	if args["resource_id"] != "12345" {
		t.Errorf("expected resource_id '12345', got %v", args["resource_id"])
	}
}

func TestBuildGitHubMCPArgs_Release(t *testing.T) {
	args, err := buildGitHubMCPArgs(webcontent.GitHubURLInfo{
		Type:  "release",
		Owner: "octocat",
		Repo:  "hello-world",
		Ref:   "v1.0.0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args["tag"] != "v1.0.0" {
		t.Errorf("expected tag 'v1.0.0', got %v", args["tag"])
	}
}

func TestTryRouteGitHubToMCP_CommitURL(t *testing.T) {
	mgr := newGitHubRouterMCPManager().withGitHubServer(true).withCallResult(&mcp.MCPToolCallResult{
		Content: []mcp.MCPContent{{Type: "text", Text: "Commit abc123"}},
		IsError: false,
	})
	a := testAgent(mgr)

	content, handled, err := a.tryRouteGitHubToMCP(context.Background(), "https://github.com/owner/repo/commit/abc123")
	if !handled {
		t.Fatal("expected handled=true for commit URL")
	}
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if mgr.lastTool != "get_commit" {
		t.Errorf("expected tool 'get_commit', got %q", mgr.lastTool)
	}
	if content != "Commit abc123" {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestTryRouteGitHubToMCP_DiscussionURL(t *testing.T) {
	mgr := newGitHubRouterMCPManager().withGitHubServer(true).withCallResult(&mcp.MCPToolCallResult{
		Content: []mcp.MCPContent{{Type: "text", Text: "Discussion #42"}},
		IsError: false,
	})
	a := testAgent(mgr)

	content, handled, err := a.tryRouteGitHubToMCP(context.Background(), "https://github.com/owner/repo/discussions/42")
	if !handled {
		t.Fatal("expected handled=true for discussion URL")
	}
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if mgr.lastTool != "get_discussion" {
		t.Errorf("expected tool 'get_discussion', got %q", mgr.lastTool)
	}
	_ = content
}

func TestTryRouteGitHubToMCP_ActionsRunURL(t *testing.T) {
	mgr := newGitHubRouterMCPManager().withGitHubServer(true).withCallResult(&mcp.MCPToolCallResult{
		Content: []mcp.MCPContent{{Type: "text", Text: "Run #12345 succeeded"}},
		IsError: false,
	})
	a := testAgent(mgr)

	content, handled, err := a.tryRouteGitHubToMCP(context.Background(), "https://github.com/owner/repo/actions/runs/12345")
	if !handled {
		t.Fatal("expected handled=true for actions run URL")
	}
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if mgr.lastTool != "actions_get" {
		t.Errorf("expected tool 'actions_get', got %q", mgr.lastTool)
	}
	if content != "Run #12345 succeeded" {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestTryRouteGitHubToMCP_ReleaseTagURL(t *testing.T) {
	mgr := newGitHubRouterMCPManager().withGitHubServer(true).withCallResult(&mcp.MCPToolCallResult{
		Content: []mcp.MCPContent{{Type: "text", Text: "Release v1.0.0"}},
		IsError: false,
	})
	a := testAgent(mgr)

	content, handled, err := a.tryRouteGitHubToMCP(context.Background(), "https://github.com/owner/repo/releases/tag/v1.0.0")
	if !handled {
		t.Fatal("expected handled=true for release tag URL")
	}
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if mgr.lastTool != "get_release_by_tag" {
		t.Errorf("expected tool 'get_release_by_tag', got %q", mgr.lastTool)
	}
	_ = content
}

func TestTryRouteGitHubToMCP_ReleaseNumericID_NotRouted(t *testing.T) {
	// /releases/123 has no tag, so get_release_by_tag can't be used — fall through.
	mgr := newGitHubRouterMCPManager().withGitHubServer(true)
	a := testAgent(mgr)

	_, handled, _ := a.tryRouteGitHubToMCP(context.Background(), "https://github.com/owner/repo/releases/123")
	if handled {
		t.Error("expected handled=false for release numeric ID (no tag)")
	}
}
