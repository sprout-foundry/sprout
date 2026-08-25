package semantic

import (
	"bufio"
	"bytes"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// runGoInlayHints retrieves inlay hints for the current file using gopls.
// gopls does not have a dedicated CLI subcommand for inlay hints, so we start
// a gopls server and send a textDocument/inlayHint JSON-RPC request.
func runGoInlayHints(input ToolInput) (ToolResult, error) {
	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: false},
			Error:        "gopls_not_available",
		}, nil
	}

	tmpDir, err := os.MkdirTemp("", "sprout-gopls-inlay-*")
	if err != nil {
		return ToolResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "gopls.sock")
	remoteAddr := "unix;" + socketPath

	cmd := exec.Command(goplsPath, "serve", "-listen="+remoteAddr)
	cmd.Dir = input.WorkspaceRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: false},
			Error:        "gopls_server_start_failed",
		}, nil
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// Wait for socket
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, statErr := os.Stat(socketPath); statErr == nil {
			break
		}
		if cmd.ProcessState != nil {
			return ToolResult{
				Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: false},
				Error:        "gopls_server_not_ready",
			}, nil
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Connect to gopls via the unix socket
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: false},
			Error:        "gopls_socket_connect_failed",
		}, nil
	}
	defer conn.Close()

	writer := bufio.NewWriter(conn)
	reader := bufio.NewReader(conn)

	id := 1

	// Initialize
	initReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "initialize",
		"params": map[string]interface{}{
			"processId": os.Getpid(),
			"capabilities": map[string]interface{}{
				"textDocument": map[string]interface{}{
					"inlayHint": map[string]interface{}{
						"dynamicRegistration": false,
					},
				},
			},
		},
	}
	id++
	writeJSONRPC(writer, initReq)

	// Read initialize response
	resp, err := readJSONRPC(reader)
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: false},
			Error:        "gopls_init_failed",
		}, nil
	}
	_ = resp

	// Initialize initialized notification
	initDone := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]interface{}{},
	}
	writeJSONRPC(writer, initDone)

	// Open document
	didOpen := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":        "file://" + input.FilePath,
				"languageId": "go",
				"version":    1,
				"text":       input.Content,
			},
		},
	}
	writeJSONRPC(writer, didOpen)

	// Flush to ensure server processes the didOpen
	if err := writer.Flush(); err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: false},
			Error:        "gopls_write_failed",
		}, nil
	}

	// Give server time to process the document
	time.Sleep(500 * time.Millisecond)

	// InlayHint request
	inlayReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "textDocument/inlayHint",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri": "file://" + input.FilePath,
			},
		},
	}
	writeJSONRPC(writer, inlayReq)
	_ = writer.Flush()

	// Read response
	inlayResp, err := readJSONRPC(reader)
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: true},
			InlayHints:   []ToolInlayHint{},
		}, nil
	}

	// Parse inlay hints from the response
	hints := parseGoplsInlayHints(inlayResp, input.Content)

	return ToolResult{
		Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: true},
		InlayHints:   hints,
	}, nil
}

// runGoInlayHintsWithRemote retrieves inlay hints by connecting to an existing
// gopls server via a unix socket. This function does not spawn a new gopls process.
func runGoInlayHintsWithRemote(input ToolInput, _, remoteAddr string) (ToolResult, error) {
	// Extract socket path from remoteAddr (format: "unix;/path/to/socket")
	if !strings.HasPrefix(remoteAddr, "unix;") {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: false},
			Error:        "invalid_remote_address",
		}, nil
	}
	socketPath := strings.TrimPrefix(remoteAddr, "unix;")

	// Connect to the existing gopls server socket
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: false},
			Error:        "gopls_socket_connect_failed",
		}, nil
	}
	defer conn.Close()

	writer := bufio.NewWriter(conn)
	reader := bufio.NewReader(conn)

	id := 1

	// Initialize
	initReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "initialize",
		"params": map[string]interface{}{
			"processId": os.Getpid(),
			"capabilities": map[string]interface{}{
				"textDocument": map[string]interface{}{
					"inlayHint": map[string]interface{}{
						"dynamicRegistration": false,
					},
				},
			},
		},
	}
	id++
	writeJSONRPC(writer, initReq)

	// Read initialize response
	resp, err := readJSONRPC(reader)
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: false},
			Error:        "gopls_init_failed",
		}, nil
	}
	_ = resp

	// Send initialized notification
	initDone := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]interface{}{},
	}
	writeJSONRPC(writer, initDone)

	// Open document
	didOpen := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":        "file://" + input.FilePath,
				"languageId": "go",
				"version":    1,
				"text":       input.Content,
			},
		},
	}
	writeJSONRPC(writer, didOpen)

	// Flush to ensure server processes the didOpen
	if err := writer.Flush(); err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: false},
			Error:        "gopls_write_failed",
		}, nil
	}

	// Give server time to process the document
	time.Sleep(500 * time.Millisecond)

	// InlayHint request
	inlayReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "textDocument/inlayHint",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri": "file://" + input.FilePath,
			},
		},
	}
	writeJSONRPC(writer, inlayReq)
	_ = writer.Flush()

	// Read response
	inlayResp, err := readJSONRPC(reader)
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: true},
			InlayHints:   []ToolInlayHint{},
		}, nil
	}

	// Parse inlay hints from the response
	hints := parseGoplsInlayHints(inlayResp, input.Content)

	return ToolResult{
		Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: true},
		InlayHints:   hints,
	}, nil
}

// parseGoplsInlayHints extracts inlay hints from gopls JSON-RPC response.
func parseGoplsInlayHints(resp map[string]interface{}, content string) []ToolInlayHint {
	var hints []ToolInlayHint

	result, ok := resp["result"]
	if !ok {
		return hints
	}

	arr, ok := result.([]interface{})
	if !ok {
		return hints
	}

	for _, item := range arr {
		hintMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		pos, ok := hintMap["position"].(map[string]interface{})
		if !ok {
			continue
		}

		line, _ := pos["line"].(float64)
		char, _ := pos["character"].(float64)

		labelParts, ok := hintMap["label"].([]interface{})
		if !ok {
			// Single string label
			if labelStr, ok := hintMap["label"].(string); ok {
				offset := goLineColToOffset(content, int(line)+1, int(char)+1)
				hints = append(hints, ToolInlayHint{
					From:  offset,
					To:    offset,
					Label: labelStr,
					Kind: func() string {
						if kindFloat, ok := hintMap["kind"].(float64); ok {
							return mapInlayHintKind(int(kindFloat))
						}
						return "none"
					}(),
				})
			}
			continue
		}

		// Array label parts - join them
		var label string
		for _, p := range labelParts {
			partMap, ok := p.(map[string]interface{})
			if !ok {
				continue
			}
			if val, ok := partMap["value"].(string); ok {
				label += val
			}
		}

		offset := goLineColToOffset(content, int(line)+1, int(char)+1)
		hints = append(hints, ToolInlayHint{
			From:  offset,
			To:    offset,
			Label: label,
			Kind: func() string {
				if kindFloat, ok := hintMap["kind"].(float64); ok {
					return mapInlayHintKind(int(kindFloat))
				}
				return "none"
			}(),
		})
	}

	return hints
}

// mapInlayHintKind maps LSP inlay hint kind integer to string.
// 1 = InlayHintKindType, 2 = InlayHintKindParameter
func mapInlayHintKind(kind int) string {
	switch kind {
	case 1:
		return "type"
	case 2:
		return "parameter"
	default:
		return "none"
	}
}
