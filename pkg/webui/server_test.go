//go:build !js

package webui

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/validation"
	"github.com/stretchr/testify/assert"
)

// TestCheckPortAvailable verifies port availability checking
func TestCheckPortAvailable(t *testing.T) {
	// Create actual server to bind port on the same interface CheckPortAvailable tests
	server := &http.Server{
		Addr: "127.0.0.1:0",
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		t.Fatalf("Failed to bind listener: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	// Start server
	go func() {
		_ = server.Serve(listener)
	}()

	// Give server time to bind
	time.Sleep(100 * time.Millisecond)

	// Port should be unavailable now
	if CheckPortAvailable(port) {
		t.Errorf("Expected port %d to be unavailable after binding", port)
	}

	// Shutdown server
	_ = server.Close()
	time.Sleep(200 * time.Millisecond)

	// On some systems, ports may stay in TIME_WAIT, check a few times
	available := false
	for i := 0; i < 3; i++ {
		if CheckPortAvailable(port) {
			available = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// If still not available after wait, log it but don't fail (system-dependent)
	if !available {
		t.Logf("Note: port %d not immediately available after close (acceptable - TIME_WAIT state)", port)
	}
}

// TestFindAvailablePort verifies port finding logic
func TestFindAvailablePort(t *testing.T) {
	// Get available port
	port, err := FindAvailablePort(DaemonPort)
	if err != nil {
		t.Fatalf("FindAvailablePort failed: %v", err)
	}

	if port < DaemonPort || port > DaemonPort+99 {
		t.Errorf("Expected port in range [%d, %d], got %d", DaemonPort, DaemonPort+99, port)
	}

	// Verify it's actually available
	if !CheckPortAvailable(port) {
		t.Errorf("Found port %d is not available", port)
	}
}

// TestStartFailsWhenPortAlreadyInUse verifies startup state remains consistent on bind failures.
func TestStartFailsWhenPortAlreadyInUse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve test port: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	server, err := NewReactWebServer(&agent.Agent{}, events.NewEventBus(), port, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = server.Start(ctx)
	if err == nil {
		t.Fatalf("expected Start to fail when port %d is already in use", port)
	}
	if server.IsRunning() {
		t.Fatalf("server should not report running after failed start on port %d", port)
	}
}

// TestMultipleServersOnDifferentPorts verifies multiple servers can start simultaneously
func TestMultipleServersOnDifferentPorts(t *testing.T) {
	// Skip if agent initialization would fail
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create mock agents for testing
	agent1 := &agent.Agent{}
	agent2 := &agent.Agent{}
	eventBus1 := events.NewEventBus()
	eventBus2 := events.NewEventBus()

	// Find two different ports
	port1, err := FindAvailablePort(DaemonPort)
	if err != nil {
		t.Fatalf("FindAvailablePort failed for port1: %v", err)
	}
	port2, err := FindAvailablePort(port1 + 1)
	if err != nil {
		t.Fatalf("FindAvailablePort failed for port2: %v", err)
	}

	if port1 == port2 {
		t.Fatalf("Expected different ports, got same port %d", port1)
	}

	// Create two web servers
	server1, err := NewReactWebServer(agent1, eventBus1, port1, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	server2, err := NewReactWebServer(agent2, eventBus2, port2, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Start both servers
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel1()
	defer cancel2()

	if err := server1.Start(ctx1); err != nil {
		t.Fatalf("Failed to start server1: %v", err)
	}
	if err := server2.Start(ctx2); err != nil {
		t.Fatalf("Failed to start server2: %v", err)
	}

	// Give both servers time to start
	time.Sleep(200 * time.Millisecond)

	// Verify both servers are running
	if !server1.IsRunning() {
		t.Error("Server 1 is not running")
	}
	if !server2.IsRunning() {
		t.Error("Server 2 is not running")
	}

	// Verify both ports are bound
	if CheckPortAvailable(port1) {
		t.Errorf("Port %d should be bound to server1", port1)
	}
	if CheckPortAvailable(port2) {
		t.Errorf("Port %d should be bound to server2", port2)
	}

	// Verify health endpoints work
	resp1, err := http.Get(fmt.Sprintf("http://localhost:%d/health", port1))
	if err != nil {
		t.Errorf("Failed to reach server1 health endpoint: %v", err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 from server1, got %d", resp1.StatusCode)
	}

	resp2, err := http.Get(fmt.Sprintf("http://localhost:%d/health", port2))
	if err != nil {
		t.Errorf("Failed to reach server2 health endpoint: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 from server2, got %d", resp2.StatusCode)
	}

	// Shutdown both servers
	if err := server1.Shutdown(); err != nil {
		t.Errorf("Failed to shutdown server1: %v", err)
	}
	if err := server2.Shutdown(); err != nil {
		t.Errorf("Failed to shutdown server2: %v", err)
	}

	// Wait for shutdown to complete
	time.Sleep(100 * time.Millisecond)

	// Verify both servers stopped
	if server1.IsRunning() {
		t.Error("Server 1 should be stopped")
	}
	if server2.IsRunning() {
		t.Error("Server 2 should be stopped")
	}

	// Verify both ports are now available
	if !CheckPortAvailable(port1) {
		t.Errorf("Port %d should be available after shutdown", port1)
	}
	if !CheckPortAvailable(port2) {
		t.Errorf("Port %d should be available after shutdown", port2)
	}
}

// TestCustomBindAddress verifies the server binds to a custom address
func TestCustomBindAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Find an available port to avoid conflicts with running servers
	port, err := FindAvailablePort(DaemonPort + 500)
	if err != nil {
		t.Fatalf("FindAvailablePort failed: %v", err)
	}

	server, err := NewReactWebServer(&agent.Agent{}, events.NewEventBus(), port, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Shutdown()

	// Verify the server is using the correct bind address
	if server.bindAddr != "127.0.0.1" {
		t.Errorf("Server bindAddr = %s, want \"127.0.0.1\"", server.bindAddr)
	}

	// Verify the server is running
	if !server.IsRunning() {
		t.Error("Server should be running")
	}
}

// TestBindAddrStoredCorrectly verifies the bind address is correctly stored
// on the server after construction and start.
func TestBindAddrStoredCorrectly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use a non-zero port to avoid DaemonPort (56000) conflicts
	// This test verifies that the bindAddr is correctly stored, not dynamic port allocation
	port, err := FindAvailablePort(DaemonPort + 200)
	if err != nil {
		t.Fatalf("FindAvailablePort failed: %v", err)
	}

	server, err := NewReactWebServer(&agent.Agent{}, events.NewEventBus(), port, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Shutdown()

	// Port should match what we specified
	if server.GetPort() != port {
		t.Errorf("Server port = %d, want %d", server.GetPort(), port)
	}

	// Verify the server is using the correct bind address
	if server.bindAddr != "127.0.0.1" {
		t.Errorf("Server bindAddr = %s, want \"127.0.0.1\"", server.bindAddr)
	}
}

func TestDisplayAddr(t *testing.T) {
	tests := []struct{ input, want string }{
		{"127.0.0.1", "localhost"},
		{"0.0.0.0", "localhost"},
		{"::", "localhost"},
		{"::1", "localhost"},
		{"192.168.1.1", "192.168.1.1"},
		{"10.0.0.5", "10.0.0.5"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := DisplayAddr(tt.input); got != tt.want {
				t.Errorf("DisplayAddr(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatListenAddr(t *testing.T) {
	tests := []struct {
		host string
		port int
		want string
	}{
		{"127.0.0.1", 8080, "127.0.0.1:8080"},
		{"0.0.0.0", 443, "0.0.0.0:443"},
		{"::", 56000, "[::]:56000"},
		{"::1", 8080, "[::1]:8080"},
		{"fe80::1", 9090, "[fe80::1]:9090"},
	}
	for _, tt := range tests {
		name := fmt.Sprintf("%s:%d", tt.host, tt.port)
		t.Run(name, func(t *testing.T) {
			if got := formatListenAddr(tt.host, tt.port); got != tt.want {
				t.Errorf("formatListenAddr(%q, %d) = %q, want %q", tt.host, tt.port, got, tt.want)
			}
		})
	}
}

// TestLooksLikeUserHome locks the heuristic that decides whether a daemonRoot
// candidate is plausibly a per-user home directory. The runtime uses this to
// trigger a /etc/passwd fallback when a stale launchd/systemd plist leaks a
// system path as $HOME (the failure mode that scoped the workspace browser
// to the wrong directory in service mode).
func TestLooksLikeUserHome(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		// System paths a service manager may inherit — NOT a user home.
		{"", false},
		{"/", false},
		{"/var/root", false},
		{"/var/empty", false},
		{"/nonexistent", false},
		{"/usr", false},
		{"/etc", false},
		{"/tmp", false},
		{"/var", false},
		{"/private", false},

		// Real user homes — must be accepted on every platform.
		{"/Users/alice", true},
		{"/Users/alice/", true},
		{"/home/bob", true},
		{"/root", true},

		// Custom/container/NFS mounts — give the benefit of the doubt rather
		// than nuking a env-supplied path with a /etc/passwd lookup that may
		// itself be wrong on a non-standard mount.
		{"/workspace", true},
		{"/data/users/charlie", true},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			if got := looksLikeUserHome(c.path); got != c.want {
				t.Errorf("looksLikeUserHome(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

func TestParseGofmtError_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		msg      string
		wantLine int
		wantCol  int
		wantOK   bool
	}{
		{
			name:     "standard input format",
			msg:      "<standard input>:42:5: expected declaration, found 'fmt'",
			wantLine: 42,
			wantCol:  5,
			wantOK:   true,
		},
		{
			name:     "stdin shorthand",
			msg:      "<stdin>:10:2: expected 'package'",
			wantLine: 10,
			wantCol:  2,
			wantOK:   true,
		},
		{
			name:     "with syntax error prefix",
			msg:      "syntax error: <standard input>:7:12: missing import path",
			wantLine: 7,
			wantCol:  12,
			wantOK:   true,
		},
		{
			name:     "empty string",
			msg:      "",
			wantLine: 0,
			wantCol:  0,
			wantOK:   false,
		},
		{
			name:     "no colons at all",
			msg:      "just some error text",
			wantLine: 0,
			wantCol:  0,
			wantOK:   false,
		},
		{
			name:     "single colon only",
			msg:      "something:else",
			wantLine: 0,
			wantCol:  0,
			wantOK:   false,
		},
		{
			name:     "non-numeric line",
			msg:      "<stdin>:abc:5: error",
			wantLine: 0,
			wantCol:  0,
			wantOK:   false,
		},
		{
			name:     "non-numeric column",
			msg:      "<stdin>:10:xyz: error",
			wantLine: 0,
			wantCol:  0,
			wantOK:   false,
		},
		{
			name:     "line 1 col 1",
			msg:      "<stdin>:1:1: error message",
			wantLine: 1,
			wantCol:  1,
			wantOK:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			line, col, ok := parseGofmtError(tt.msg)
			if ok != tt.wantOK {
				t.Errorf("parseGofmtError(%q): ok = %v; want %v", tt.msg, ok, tt.wantOK)
			}
			if line != tt.wantLine {
				t.Errorf("parseGofmtError(%q): line = %d; want %d", tt.msg, line, tt.wantLine)
			}
			if col != tt.wantCol {
				t.Errorf("parseGofmtError(%q): col = %d; want %d", tt.msg, col, tt.wantCol)
			}
		})
	}
}

func TestLineColToOffsets_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		line     int
		col      int
		content  string
		wantFrom int
	}{
		{
			name:     "line 1 col 1 on single line",
			line:     1,
			col:      1,
			content:  "hello world",
			wantFrom: 0,
		},
		{
			name:     "line 1 col 1 on multiline",
			line:     1,
			col:      1,
			content:  "hello\nworld",
			wantFrom: 0,
		},
		{
			name:     "line 2 col 1",
			line:     2,
			col:      1,
			content:  "hello\nworld",
			wantFrom: 6,
		},
		{
			name:     "line 1 col 7 (start of world)",
			line:     1,
			col:      7,
			content:  "hello world",
			wantFrom: 6,
		},
		{
			name:     "line 0 clamped to 1",
			line:     0,
			col:      1,
			content:  "hello",
			wantFrom: 0,
		},
		{
			name:     "col 0 clamped to 1",
			line:     1,
			col:      0,
			content:  "hello",
			wantFrom: 0,
		},
		{
			name:     "line beyond content",
			line:     100,
			col:      1,
			content:  "hello",
			wantFrom: 5,
		},
		{
			name:     "empty content",
			line:     1,
			col:      1,
			content:  "",
			wantFrom: 0,
		},
		{
			name:     "col beyond line length",
			line:     1,
			col:      100,
			content:  "short",
			wantFrom: 5,
		},
		{
			name:     "negative line",
			line:     -1,
			col:      1,
			content:  "hello",
			wantFrom: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			from, to := lineColToOffsets(tt.line, tt.col, tt.content)
			if from != tt.wantFrom {
				t.Errorf("lineColToOffsets(%d, %d, %q): from = %d; want %d", tt.line, tt.col, tt.content, from, tt.wantFrom)
			}
			// to must be >= from
			if to < from {
				t.Errorf("lineColToOffsets(%d, %d, %q): to (%d) < from (%d)", tt.line, tt.col, tt.content, to, from)
			}
		})
	}
}

func TestExtendToTokenEnd_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		content    string
		byteOffset int
		want       int
	}{
		{
			name:       "middle of identifier",
			content:    "package main",
			byteOffset: 2,
			want:       7, // "package" ends at 7
		},
		{
			name:       "start of identifier",
			content:    "package main",
			byteOffset: 0,
			want:       7,
		},
		{
			name:       "at space (delimiter)",
			content:    "package main",
			byteOffset: 7,
			want:       8, // space is delimiter, so extend by 1
		},
		{
			name:       "beyond content length",
			content:    "hello",
			byteOffset: 10,
			want:       10,
		},
		{
			name:       "negative offset clamped to 0",
			content:    "hello",
			byteOffset: -1,
			want:       5, // extends to end of "hello"
		},
		{
			name:       "empty content with 0 offset",
			content:    "",
			byteOffset: 0,
			want:       0,
		},
		{
			name:       "at end of content",
			content:    "hello",
			byteOffset: 5,
			want:       5,
		},
		{
			name:       "middle of word in sentence",
			content:    "var x = 42",
			byteOffset: 4, // at 'x'
			want:       5, // 'x' is followed by space
		},
		{
			name:       "at equals sign",
			content:    "var x = 42",
			byteOffset: 6, // at '='
			want:       7, // '=' is delimiter, extend by 1
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extendToTokenEnd(tt.content, tt.byteOffset)
			if got != tt.want {
				t.Errorf("extendToTokenEnd(%q, %d) = %d; want %d", tt.content, tt.byteOffset, got, tt.want)
			}
		})
	}
}

func TestIsExtDelimiter_ZC(t *testing.T) {
	t.Parallel()
	delimiters := []rune{
		' ', '\t', '\n', '\r',
		'(', ')', '{', '}', '[', ']',
		',', ';', ':', '+', '-', '*', '/',
		'=', '!', '<', '>', '&', '|', '^', '%',
		'"', '\'',
	}
	for _, ch := range delimiters {
		t.Run(string(ch), func(t *testing.T) {
			t.Parallel()
			if !isExtDelimiter(ch) {
				t.Errorf("isExtDelimiter(%c) = false; want true", ch)
			}
		})
	}

	nonDelimiters := []rune{
		'a', 'z', 'A', 'Z',
		'0', '9',
		'.', '_', '#', '@', '$',
		'~', '`', '\\', '?',
	}
	for _, ch := range nonDelimiters {
		t.Run(string(ch), func(t *testing.T) {
			t.Parallel()
			if isExtDelimiter(ch) {
				t.Errorf("isExtDelimiter(%c) = true; want false", ch)
			}
		})
	}
}

func TestValidationToFrontend_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		d            validation.Diagnostic
		content      string
		wantSeverity string
		wantMessage  string
		wantSource   string
	}{
		{
			name: "simple diagnostic",
			d: validation.Diagnostic{
				Path:     "file.go",
				Line:     10,
				Column:   5,
				Severity: "error",
				Message:  "unexpected token",
				Source:   "gofmt",
			},
			content:      "package main\n\nvar x = 10",
			wantSeverity: "error",
			wantMessage:  "unexpected token",
			wantSource:   "gofmt",
		},
		{
			name: "goimports spans entire file",
			d: validation.Diagnostic{
				Path:     "file.go",
				Line:     1,
				Column:   1,
				Severity: "warning",
				Message:  "unused import",
				Source:   "goimports",
			},
			content:      "package main\nimport \"unused\"",
			wantSeverity: "warning",
			wantMessage:  "unused import",
			wantSource:   "goimports",
		},
		{
			name: "empty diagnostic",
			d: validation.Diagnostic{
				Path:     "",
				Line:     0,
				Column:   0,
				Severity: "",
				Message:  "",
				Source:   "",
			},
			content:      "hello",
			wantSeverity: "",
			wantMessage:  "",
			wantSource:   "",
		},
		{
			name: "gofmt with parseable error message",
			d: validation.Diagnostic{
				Path:     "file.go",
				Line:     0,
				Column:   0,
				Severity: "error",
				Message:  "syntax error: <standard input>:3:2: expected declaration",
				Source:   "gofmt",
			},
			content:      "line1\nline2\nline3",
			wantSeverity: "error",
			wantMessage:  "syntax error: <standard input>:3:2: expected declaration",
			wantSource:   "gofmt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := validationToFrontend(tt.d, tt.content)
			if got.Severity != tt.wantSeverity {
				t.Errorf("validationToFrontend: Severity = %q; want %q", got.Severity, tt.wantSeverity)
			}
			if got.Message != tt.wantMessage {
				t.Errorf("validationToFrontend: Message = %q; want %q", got.Message, tt.wantMessage)
			}
			if got.Source != tt.wantSource {
				t.Errorf("validationToFrontend: Source = %q; want %q", got.Source, tt.wantSource)
			}
			// Verify offsets are valid: From <= To <= len(content)
			if got.From > got.To {
				t.Errorf("validationToFrontend: From (%d) > To (%d)", got.From, got.To)
			}
			if got.To > len(tt.content) {
				t.Errorf("validationToFrontend: To (%d) > len(content) (%d)", got.To, len(tt.content))
			}
		})
	}
}

func TestValidationToFrontend_Offsets_ZC(t *testing.T) {
	t.Parallel()
	// Verify that offsets are actually computed correctly
	content := "line1\nline2\nline3"
	d := validation.Diagnostic{
		Line:     0,
		Column:   0,
		Severity: "error",
		Message:  "syntax error: <standard input>:3:2: expected declaration",
		Source:   "gofmt",
	}
	got := validationToFrontend(d, content)
	// Line 3, col 2: line1\n = 6 bytes, line2\n = 6 bytes, so offset = 12 + 1 = 13
	// But actually: line 3 starts at offset 12, col 2 = offset 13
	if got.From != 13 {
		t.Errorf("From = %d; want 13 (line 3, col 2 in %q)", got.From, content)
	}
}

func TestDiagnosticToOffsets_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		d        validation.Diagnostic
		content  string
		wantFrom int
	}{
		{
			name: "goimports line=1 col=1 spans entire file",
			d: validation.Diagnostic{
				Line:    1,
				Column:  1,
				Source:  "goimports",
				Message: "unused import",
			},
			content:  "package main\nimport \"fmt\"",
			wantFrom: 0,
		},
		{
			name: "gofmt with parseable error",
			d: validation.Diagnostic{
				Line:    0,
				Column:  0,
				Source:  "gofmt",
				Message: "syntax error: <standard input>:2:5: expected token",
			},
			content:  "line1\nline2\nline3",
			wantFrom: 10, // line 2 start=6, col 5 → 6+4=10
		},
		{
			name: "fallback uses diagnostic line/col",
			d: validation.Diagnostic{
				Line:    2,
				Column:  1,
				Source:  "some-other",
				Message: "some error",
			},
			content:  "line1\nline2",
			wantFrom: 6, // line 2, col 1 = offset 6
		},
		{
			name: "line=0 col=0 non-gofmt falls back to entire content",
			d: validation.Diagnostic{
				Line:    0,
				Column:  0,
				Source:  "unknown",
				Message: "error",
			},
			content:  "hello",
			wantFrom: 0,
		},
		{
			name: "goimports with non-1/1 falls through to line/col",
			d: validation.Diagnostic{
				Line:    2,
				Column:  3,
				Source:  "goimports",
				Message: "error",
			},
			content:  "line1\nline2\nline3",
			wantFrom: 8, // line 2 start=6, col 3 → 6+2=8
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			from, to := diagnosticToOffsets(tt.d, tt.content)
			if from != tt.wantFrom {
				t.Errorf("diagnosticToOffsets: from = %d; want %d", from, tt.wantFrom)
			}
			// to must be >= from and <= len(content)
			if to < from {
				t.Errorf("diagnosticToOffsets: to (%d) < from (%d)", to, from)
			}
			if to > len(tt.content) {
				t.Errorf("diagnosticToOffsets: to (%d) > len(content) (%d)", to, len(tt.content))
			}
		})
	}
}

func TestSanitizePathComponent_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "simple alphanumeric",
			input: "main",
			want:  "main",
		},
		{
			name:  "with hyphens and underscores",
			input: "my-branch_v2",
			want:  "my-branch_v2",
		},
		{
			name:  "with dots",
			input: "feature.v2.1",
			want:  "feature.v2.1",
		},
		{
			name:  "slashes replaced",
			input: "feature/new-branch",
			want:  "feature_new-branch",
		},
		{
			name:  "spaces replaced",
			input: "my branch",
			want:  "my_branch",
		},
		{
			name:  "special characters replaced",
			input: "branch@name!test",
			want:  "branch_name_test",
		},
		{
			name:  "unicode replaced",
			input: "分支",
			want:  "__",
		},
		{
			name:  "mixed safe and unsafe",
			input: "feat/add-auth_2024",
			want:  "feat_add-auth_2024",
		},
		{
			name:  "only special chars",
			input: "!@#$%",
			want:  "_____",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizePathComponent(tt.input)
			if got != tt.want {
				t.Errorf("sanitizePathComponent(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// settings_api_helpers.go — validateReasoningEffort
// ---------------------------------------------------------------------------

func TestValidateReasoningEffort_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		err   bool
	}{
		{"", false}, // empty is valid
		{"low", false},
		{"medium", false},
		{"high", false},
		{"invalid", true},
		{"LOW", true}, // case-sensitive
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := validateReasoningEffort(tt.input)
			if (got != nil) != tt.err {
				t.Errorf("validateReasoningEffort(%q) error = %v, wantErr %v", tt.input, got, tt.err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// settings_api_helpers.go — validateHistoryScope
// ---------------------------------------------------------------------------

func TestValidateHistoryScope_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		err   bool
	}{
		{"project", false},
		{"global", false},
		{"local", true},
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := validateHistoryScope(tt.input)
			if (got != nil) != tt.err {
				t.Errorf("validateHistoryScope(%q) error=%v, wantErr %v", tt.input, got, tt.err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// settings_api_helpers.go — validateAPITimeout
// ---------------------------------------------------------------------------

func TestValidateAPITimeout_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input int
		err   bool
	}{
		{30, false},
		{1, false},
		{0, true},
		{-1, true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.input), func(t *testing.T) {
			t.Parallel()
			got := validateAPITimeout(tt.input)
			if (got != nil) != tt.err {
				t.Errorf("validateAPITimeout(%d) error=%v, wantErr %v", tt.input, got, tt.err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// settings_api_helpers.go — extractPathSegment
// ---------------------------------------------------------------------------

func TestExtractPathSegment_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path   string
		prefix string
		want   string
	}{
		{"/api/settings/mcp/servers/myserver", "/api/settings/mcp/servers/", "myserver"},
		{"/api/settings/mcp/servers/myserver/", "/api/settings/mcp/servers/", "myserver"},
		{"/api/other", "/api/settings/mcp/servers/", ""},
		{"nomatch", "/prefix", ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			got := extractPathSegment(tt.path, tt.prefix)
			if got != tt.want {
				t.Errorf("extractPathSegment(%q, %q) = %q, want %q", tt.path, tt.prefix, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// settings_api_helpers.go — asInt
// ---------------------------------------------------------------------------

func TestAsInt_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input interface{}
		want  int
		ok    bool
	}{
		{float64(42), 42, true},
		{int(42), 42, true},
		{int64(42), 42, true},
		{"42", 0, false},
		{nil, 0, false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%T", tt.input), func(t *testing.T) {
			t.Parallel()
			got, ok := asInt(tt.input)
			if ok != tt.ok || got != tt.want {
				t.Errorf("asInt(%v) = (%d, %v), want (%d, %v)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// settings_api_helpers.go — writeJSON / writeJSONError / writeJSONErr
// ---------------------------------------------------------------------------

func TestWriteJSON_ZC(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]string{"key": "value"})
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"key"`) || !strings.Contains(body, `"value"`) {
		t.Errorf("response body should contain key/value: %s", body)
	}
}

func TestWriteJSONError_ZC(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	writeJSONError(w, http.StatusBadRequest, "test error")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "test error") {
		t.Errorf("response body should contain error: %s", body)
	}
}

func TestWriteJSONErr_ZC(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	writeJSONErr(w, http.StatusInternalServerError, "INTERNAL", "something broke")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "INTERNAL") || !strings.Contains(body, "something broke") {
		t.Errorf("response body should contain code/message: %s", body)
	}
}

// ---------------------------------------------------------------------------
// search_api.go — getContextLines
// ---------------------------------------------------------------------------

func TestGetContextLines_ZC(t *testing.T) {
	t.Parallel()
	t.Run("zero_context", func(t *testing.T) {
		got := getContextLines([]string{"a", "b", "c"}, 3, 0, true)
		if got != nil {
			t.Errorf("expected nil for zero context, got %v", got)
		}
	})
	t.Run("before_lines", func(t *testing.T) {
		buffer := []string{"line0", "line1", "line2", "line3", "line4"}
		got := getContextLines(buffer, 5, 2, true)
		if len(got) != 2 {
			t.Fatalf("expected 2 context lines, got %d", len(got))
		}
		if got[0] != "line2" || got[1] != "line3" {
			t.Errorf("expected [line2, line3], got %v", got)
		}
	})
	t.Run("more_context_than_buffer", func(t *testing.T) {
		t.Parallel()
		buffer := []string{"line0", "line1"}
		got := getContextLines(buffer, 2, 5, true)
		if len(got) != 1 || got[0] != "line0" {
			t.Errorf("expected [line0], got %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// search_api.go — compileSearchPattern
// ---------------------------------------------------------------------------

func TestCompileSearchPattern_ZC(t *testing.T) {
	t.Parallel()
	t.Run("plain_text", func(t *testing.T) {
		re, err := compileSearchPattern("hello", false, false, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !re.MatchString("hello world") {
			t.Error("should match plain text")
		}
	})
	t.Run("case_insensitive", func(t *testing.T) {
		re, err := compileSearchPattern("Hello", false, false, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !re.MatchString("hello world") {
			t.Error("should match case-insensitive")
		}
	})
	t.Run("case_sensitive", func(t *testing.T) {
		re, err := compileSearchPattern("Hello", true, false, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if re.MatchString("hello world") {
			t.Error("should NOT match case-sensitive")
		}
	})
	t.Run("whole_word", func(t *testing.T) {
		re, err := compileSearchPattern("test", false, true, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !re.MatchString("this is a test here") {
			t.Error("should match whole word")
		}
	})
	t.Run("regex_mode", func(t *testing.T) {
		re, err := compileSearchPattern("foo.*bar", false, false, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !re.MatchString("fooxyzbar") {
			t.Error("should match regex pattern")
		}
	})
	t.Run("regex_escapes_special", func(t *testing.T) {
		re, err := compileSearchPattern("func()", false, false, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !re.MatchString("call func() now") {
			t.Error("should match escaped special chars")
		}
	})
	t.Run("invalid_regex", func(t *testing.T) {
		_, err := compileSearchPattern("(:", false, false, true)
		if err == nil {
			t.Error("should return error for invalid regex")
		}
	})
}

// ---------------------------------------------------------------------------
// search_api.go — parsePatterns
// ---------------------------------------------------------------------------

func TestParsePatterns_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"*.go", 1},
		{"*.go, *.js", 2},
		{"*.go, , *.js", 2},
		{"  *.go  ,  *.js  ", 2},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := parsePatterns(tt.input)
			if len(got) != tt.want {
				t.Errorf("parsePatterns(%q) = %d patterns, want %d", tt.input, len(got), tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// search_api.go — matchesAnyPattern
// ---------------------------------------------------------------------------

func TestMatchesAnyPattern_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path     string
		patterns []string
		want     bool
	}{
		{"main.go", []string{"*.go"}, true},
		{"main.go", []string{"*.js"}, false},
		{"src/main.go", []string{"*.go"}, true},
		{"src/main.go", []string{"main.go"}, true},
		{"src/test.js", []string{"*.go", "*.js"}, true},
		{"readme.md", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			got := matchesAnyPattern(tt.path, tt.patterns)
			if got != tt.want {
				t.Errorf("matchesAnyPattern(%q, %v) = %v, want %v", tt.path, tt.patterns, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// api_misc.go — tryParseMultipartFile
// ---------------------------------------------------------------------------

func TestTryParseMultipartFile_ZC(t *testing.T) {
	t.Parallel()
	t.Run("not_multipart", func(t *testing.T) {
		_, ok := tryParseMultipartFile([]byte("hello"), "application/json")
		if ok {
			t.Error("should return false for non-multipart content type")
		}
	})
	t.Run("invalid_multipart", func(t *testing.T) {
		_, ok := tryParseMultipartFile([]byte("garbage"), "multipart/form-data; boundary=----test")
		// Parsing garbage multipart should return false
		if ok {
			t.Error("should return false for invalid multipart data")
		}
	})
}

func newMinimalTestServer(t *testing.T) *ReactWebServer {
	t.Helper()
	return &ReactWebServer{
		eventBus:       events.NewEventBus(),
		clientContexts: make(map[string]*webClientContext),
	}
}

// =====================================================================
// incrementActiveQueriesWithQuery
// =====================================================================

func TestQueryCoverage_IncrementActiveQueriesWithQuery_Basic(t *testing.T) {
	ws := newMinimalTestServer(t)

	ws.incrementActiveQueriesWithQuery("client1", "test query")

	assert.Equal(t, 1, ws.activeQueries, "activeQueries should be 1 after increment")
	ctx := ws.clientContexts["client1"]
	assert.NotNil(t, ctx, "client context should be created")
	assert.True(t, ctx.ActiveQuery, "ActiveQuery should be true")
	assert.Equal(t, "test query", ctx.CurrentQuery, "CurrentQuery should be set")
}

func TestQueryCoverage_IncrementActiveQueriesWithQuery_IncrementsCounter(t *testing.T) {
	ws := newMinimalTestServer(t)

	ws.incrementActiveQueriesWithQuery("c1", "query1")
	ws.incrementActiveQueriesWithQuery("c2", "query2")

	assert.Equal(t, 2, ws.activeQueries, "activeQueries should be 2")
}

func TestQueryCoverage_IncrementActiveQueriesWithQuery_CreatesNewContext(t *testing.T) {
	ws := newMinimalTestServer(t)

	// No contexts exist yet
	assert.Nil(t, ws.clientContexts["new-client"])

	ws.incrementActiveQueriesWithQuery("new-client", "hello")

	ctx := ws.clientContexts["new-client"]
	assert.NotNil(t, ctx, "should create context for new client")
	assert.True(t, ctx.ActiveQuery)
	assert.Equal(t, "hello", ctx.CurrentQuery)
}

func TestQueryCoverage_IncrementActiveQueriesWithQuery_ExistingContext(t *testing.T) {
	ws := newMinimalTestServer(t)

	// Seed an existing context
	ws.mutex.Lock()
	ws.clientContexts["c1"] = &webClientContext{
		WorkspaceRoot: "/tmp",
	}
	ws.mutex.Unlock()

	ws.incrementActiveQueriesWithQuery("c1", "new query")

	ctx := ws.clientContexts["c1"]
	assert.True(t, ctx.ActiveQuery)
	assert.Equal(t, "new query", ctx.CurrentQuery)
	assert.Equal(t, 1, ws.activeQueries)
}

// =====================================================================
// hasActiveQuery
// =====================================================================

func TestQueryCoverage_HasActiveQuery_InitiallyFalse(t *testing.T) {
	ws := newMinimalTestServer(t)

	assert.False(t, ws.hasActiveQuery(), "should be false when no queries active")
}

func TestQueryCoverage_HasActiveQuery_AfterIncrement(t *testing.T) {
	ws := newMinimalTestServer(t)

	ws.incrementActiveQueriesWithQuery("c1", "q")

	assert.True(t, ws.hasActiveQuery(), "should be true after increment")
}

func TestQueryCoverage_HasActiveQuery_AfterDecrement(t *testing.T) {
	ws := newMinimalTestServer(t)

	ws.incrementActiveQueriesWithQuery("c1", "q")
	ws.decrementActiveQueries("c1")

	assert.False(t, ws.hasActiveQuery(), "should be false after decrement")
}

// =====================================================================
// incrementActiveQueries (already 100%, but test edge cases)
// =====================================================================

func TestQueryCoverage_IncrementActiveQueries_Basic(t *testing.T) {
	ws := newMinimalTestServer(t)

	ws.incrementActiveQueries("c1")

	assert.Equal(t, 1, ws.activeQueries)
	ctx := ws.clientContexts["c1"]
	assert.NotNil(t, ctx)
	assert.True(t, ctx.ActiveQuery)
}

// =====================================================================
// Concurrent access
// =====================================================================

func TestQueryCoverage_ConcurrentIncrements(t *testing.T) {
	ws := newMinimalTestServer(t)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ws.incrementActiveQueriesWithQuery("client", "query")
		}()
	}
	wg.Wait()

	assert.Equal(t, 100, ws.activeQueries, "should handle concurrent increments")
}

func TestQueryCoverage_DecrementNeverNegative(t *testing.T) {
	ws := newMinimalTestServer(t)

	ws.decrementActiveQueries("nonexistent")

	assert.Equal(t, 0, ws.activeQueries, "decrement should not go negative")
}
