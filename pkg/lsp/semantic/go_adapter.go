package semantic

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type goAdapter struct{}

// NewGoAdapter constructs a Go semantic adapter.
func NewGoAdapter() Adapter {
	return goAdapter{}
}

// Run dispatches to the appropriate Go analysis routine.
func (a goAdapter) Run(input ToolInput) (ToolResult, error) {
	switch input.Method {
	case "diagnostics":
		return runGoDiagnostics(input)
	case "definition":
		return runGoDefinition(input)
	case "hover":
		return runGoHover(input)
	case "rename":
		return runGoRename(input)
	case "references":
		return runGoReferences(input)
	case "code_actions":
		return runGoCodeActions(input)
	case "inlay_hints":
		return runGoInlayHints(input)
	case "signature_help":
		return runGoSignatureHelp(input)
	default:
		return ToolResult{Capabilities: Capabilities{}}, nil
	}
}

// runGoDiagnostics reports syntax errors (via gofmt -e) and vet issues
// (via go vet) by writing the current file content to a temp package.
func runGoDiagnostics(input ToolInput) (ToolResult, error) {
	tmpDir, err := os.MkdirTemp("", "sprout-go-diag-*")
	if err != nil {
		return ToolResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	baseName := filepath.Base(input.FilePath)
	if baseName == "" || baseName == "." {
		baseName = "main.go"
	}
	tmpFile := filepath.Join(tmpDir, baseName)

	if err := os.WriteFile(tmpFile, []byte(input.Content), 0600); err != nil {
		return ToolResult{}, err
	}
	const goMod = "module sprout_temp\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0600); err != nil {
		return ToolResult{}, err
	}

	caps := Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true}

	fmtCmd := exec.Command("gofmt", "-e", tmpFile)
	var fmtStderr bytes.Buffer
	fmtCmd.Stderr = &fmtStderr
	fmtCmd.Dir = tmpDir
	_ = fmtCmd.Run()
	diagnostics := parseGofmtErrors(fmtStderr.String(), input.Content)

	if len(diagnostics) == 0 && input.Trigger == "save" {
		vetCmd := exec.Command("go", "vet", "./...")
		var vetStderr bytes.Buffer
		vetCmd.Stderr = &vetStderr
		vetCmd.Dir = tmpDir
		_ = vetCmd.Run()
		diagnostics = append(diagnostics, parseGoVetErrors(vetStderr.String(), input.Content)...)
	}

	return ToolResult{Capabilities: caps, Diagnostics: diagnostics}, nil
}

// runGoDefinition resolves the definition at a position using gopls.
func runGoDefinition(input ToolInput) (ToolResult, error) {
	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: false, Hover: false, Rename: false, CodeActions: true},
			Error:        "gopls_not_available",
		}, nil
	}
	return runGoDefinitionWithRemote(input, goplsPath, "")
}

// runGoHover retrieves hover documentation at a position using gopls.
func runGoHover(input ToolInput) (ToolResult, error) {
	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: false, Rename: false, CodeActions: true},
			Error:        "gopls_not_available",
		}, nil
	}

	pos := input.Position
	if pos == nil {
		pos = &Position{Line: 1, Column: 1}
	}
	posArg := fmt.Sprintf("%s:%d:%d", input.FilePath, pos.Line, pos.Column)

	cmd := exec.Command(goplsPath, "hover", posArg)
	cmd.Dir = input.WorkspaceRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()

	contents := strings.TrimSpace(stdout.String())
	if contents == "" {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, CodeActions: true},
			Hover:        nil,
		}, nil
	}

	return ToolResult{
		Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, CodeActions: true},
		Hover:        &ToolHover{Contents: contents},
	}, nil
}

// runGoReferences finds all references to the symbol at the given position
// across the entire workspace (not just the current file like runGoRename).
func runGoReferences(input ToolInput) (ToolResult, error) {
	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: false, CodeActions: true},
			Error:        "gopls_not_available",
		}, nil
	}

	pos := input.Position
	if pos == nil {
		pos = &Position{Line: 1, Column: 1}
	}
	posArg := fmt.Sprintf("%s:%d:%d", input.FilePath, pos.Line, pos.Column)

	cmd := exec.Command(goplsPath, "references", "-c", "0", posArg)
	cmd.Dir = input.WorkspaceRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()

	re := regexp.MustCompile(`^(.+?):(\d+):(\d+)-(\d+)$`)
	var locations []ToolReferenceLocation
	symbolName := ""

	// Extract the word at the cursor position for the symbol name
	lines := strings.Split(input.Content, "\n")
	if pos.Line >= 1 && pos.Line <= len(lines) {
		lineText := lines[pos.Line-1]
		cols := []rune(lineText)
		wordStart, wordEnd := pos.Column-1, pos.Column-1
		for wordStart > 0 && isIdentRune(cols[wordStart-1]) {
			wordStart--
		}
		for wordEnd < len(cols) && isIdentRune(cols[wordEnd]) {
			wordEnd++
		}
		symbolName = string(cols[wordStart:wordEnd])
	}

	for _, raw := range strings.Split(stdout.String(), "\n") {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		m := re.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		refFile := m[1]
		lineNum, _ := strconv.Atoi(m[2])
		colNum, _ := strconv.Atoi(m[3])
		endCol, _ := strconv.Atoi(m[4])

		// Read the line text from the reference file
		var lineText string
		refBytes, err := os.ReadFile(refFile)
		if err == nil {
			refLines := strings.Split(string(refBytes), "\n")
			if lineNum >= 1 && lineNum <= len(refLines) {
				lineText = strings.TrimRight(refLines[lineNum-1], "\r\n")
			}
		} else {
			log.Printf("[semantic] failed to read reference file %s: %v", refFile, err)
		}

		// Make the file path relative to workspace root
		displayPath := refFile
		if rel, relErr := filepath.Rel(input.WorkspaceRoot, refFile); relErr == nil {
			displayPath = filepath.ToSlash(rel)
		}

		locations = append(locations, ToolReferenceLocation{
			FilePath: displayPath,
			Line:     lineNum,
			StartCol: colNum,
			EndCol:   endCol,
			LineText: lineText,
		})
	}

	// Sort: current file first, then alphabetical
	curRel, _ := filepath.Rel(input.WorkspaceRoot, input.FilePath)
	curRel = filepath.ToSlash(curRel)
	sort.Slice(locations, func(i, j int) bool {
		if locations[i].FilePath == locations[j].FilePath {
			return locations[i].Line < locations[j].Line
		}
		if locations[i].FilePath == curRel {
			return true
		}
		if locations[j].FilePath == curRel {
			return false
		}
		return locations[i].FilePath < locations[j].FilePath
	})

	return ToolResult{
		Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true},
		References:   &ToolReferences{Locations: locations, SymbolName: symbolName},
	}, nil
}

func isIdentRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// runGoRename finds all references to the symbol at the given position.
func runGoRename(input ToolInput) (ToolResult, error) {
	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: false, CodeActions: true},
			Error:        "gopls_not_available",
		}, nil
	}

	pos := input.Position
	if pos == nil {
		pos = &Position{Line: 1, Column: 1}
	}
	posArg := fmt.Sprintf("%s:%d:%d", input.FilePath, pos.Line, pos.Column)

	cmd := exec.Command(goplsPath, "references", "-c", "0", posArg)
	cmd.Dir = input.WorkspaceRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()

	// Parse output lines like: /path/to/file.go:42:10-15
	re := regexp.MustCompile(`^(.+?):(\d+):(\d+)-(\d+)$`)
	var locations []ToolRenameLocation
	currentFile := input.FilePath

	for _, raw := range strings.Split(stdout.String(), "\n") {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		m := re.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		refFile := m[1]
		// Only include locations in the current file
		if filepath.Clean(refFile) != filepath.Clean(currentFile) {
			continue
		}
		lineNum, _ := strconv.Atoi(m[2])
		colNum, _ := strconv.Atoi(m[3])
		endCol, _ := strconv.Atoi(m[4])
		from := goLineColToOffset(input.Content, lineNum, colNum)
		to := goLineColToOffset(input.Content, lineNum, endCol)
		locations = append(locations, ToolRenameLocation{
			FilePath: currentFile,
			From:     from,
			To:       to,
		})
	}

	// Deduplicate by from:to key and sort
	if len(locations) > 1 {
		seen := make(map[string]bool)
		var uniq []ToolRenameLocation
		for _, loc := range locations {
			key := fmt.Sprintf("%d:%d", loc.From, loc.To)
			if !seen[key] {
				seen[key] = true
				uniq = append(uniq, loc)
			}
		}
		locations = uniq
		sort.Slice(locations, func(i, j int) bool {
			return locations[i].From < locations[j].From
		})
	}

	return ToolResult{
		Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true},
		Rename:       &ToolRename{Locations: locations},
	}, nil
}

func runGoDefinitionWithRemote(input ToolInput, goplsPath, remoteAddr string) (ToolResult, error) {
	pos := input.Position
	if pos == nil {
		pos = &Position{Line: 1, Column: 1}
	}
	posArg := fmt.Sprintf("%s:%d:%d", input.FilePath, pos.Line, pos.Column)

	args := make([]string, 0, 3)
	if remoteAddr != "" {
		args = append(args, "-remote="+remoteAddr)
	}
	args = append(args, "definition", posArg)

	cmd := exec.Command(goplsPath, args...)
	cmd.Dir = input.WorkspaceRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()

	defPath, defLine, defCol, ok := parseGoplsDefinition(stdout.String())
	if !ok {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true},
		}, nil
	}
	return ToolResult{
		Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true},
		Definition:   &ToolDefinition{Path: defPath, Line: defLine, Column: defCol},
	}, nil
}

// goLineColToOffset converts a 1-based line:col to a 0-based byte offset in content.
func goLineColToOffset(content string, line, col int) int {
	if line <= 0 {
		line = 1
	}
	if col <= 0 {
		col = 1
	}
	currentLine := 1
	lineStart := 0
	for i, ch := range content {
		if currentLine == line {
			offset := lineStart + col - 1
			if offset > len(content) {
				return len(content)
			}
			return offset
		}
		if ch == '\n' {
			currentLine++
			lineStart = i + 1
		}
	}
	return len(content)
}

var goErrorRE = regexp.MustCompile(`^[^:]+:(\d+):(\d+): (.+)$`)

func parseGofmtErrors(output, content string) []ToolDiagnostic {
	var diags []ToolDiagnostic
	for _, raw := range strings.Split(output, "\n") {
		m := goErrorRE.FindStringSubmatch(strings.TrimSpace(raw))
		if m == nil {
			continue
		}
		lineNum, _ := strconv.Atoi(m[1])
		colNum, _ := strconv.Atoi(m[2])
		from := goLineColToOffset(content, lineNum, colNum)
		diags = append(diags, ToolDiagnostic{
			From:     from,
			To:       from + 1,
			Severity: "error",
			Message:  m[3],
			Source:   "gofmt",
		})
	}
	return diags
}

func parseGoVetErrors(output, content string) []ToolDiagnostic {
	var diags []ToolDiagnostic
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			continue
		}
		m := goErrorRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lineNum, _ := strconv.Atoi(m[1])
		colNum, _ := strconv.Atoi(m[2])
		from := goLineColToOffset(content, lineNum, colNum)
		diags = append(diags, ToolDiagnostic{
			From:     from,
			To:       from + 1,
			Severity: "warning",
			Message:  m[3],
			Source:   "go vet",
		})
	}
	return diags
}

var goplsDefRE = regexp.MustCompile(`^(.+?):(\d+):(\d+)`)

func parseGoplsDefinition(output string) (path string, line, col int, ok bool) {
	for _, raw := range strings.Split(output, "\n") {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		m := goplsDefRE.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		lineNum, _ := strconv.Atoi(m[2])
		colNum, _ := strconv.Atoi(m[3])
		return m[1], lineNum, colNum, true
	}
	return "", 0, 0, false
}

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

// writeJSONRPC writes a JSON-RPC message to the writer.
func writeJSONRPC(w *bufio.Writer, msg map[string]interface{}) {
	data, _ := json.Marshal(msg)
	// Content-Length header format used by gopls
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	_, _ = w.WriteString(header)
	_, _ = w.Write(data)
	_ = w.Flush()
}

// readJSONRPC reads a JSON-RPC message from the reader.
func readJSONRPC(r *bufio.Reader) (map[string]interface{}, error) {
	// Read Content-Length header
	header, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	var contentLen int
	fmt.Sscanf(strings.TrimSpace(header), "Content-Length: %d", &contentLen)

	// Skip the empty line after header
	_, err = r.ReadString('\n')
	if err != nil {
		return nil, err
	}

	// Read content
	content := make([]byte, contentLen)
	_, err = io.ReadFull(r, content)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(content, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// runGoCodeActions provides code actions for the current file using goimports.
func runGoCodeActions(input ToolInput) (ToolResult, error) {
	caps := Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true}

	// Check if goimports is available
	goimportsPath, err := exec.LookPath("goimports")
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: false},
			Error:        "goimports_not_available",
		}, nil
	}

	// Write content to a temp file for goimports to process
	tmpDir, err := os.MkdirTemp("", "sprout-go-codeaction-*")
	if err != nil {
		return ToolResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	baseName := filepath.Base(input.FilePath)
	if baseName == "" || baseName == "." {
		baseName = "main.go"
	}
	tmpFile := filepath.Join(tmpDir, baseName)

	if err := os.WriteFile(tmpFile, []byte(input.Content), 0600); err != nil {
		return ToolResult{}, err
	}

	// Run goimports to get formatted output with organized imports
	formatted, err := exec.Command(goimportsPath, tmpFile).Output()
	if err != nil {
		// goimports can fail on syntax errors; return no actions
		return ToolResult{Capabilities: caps, CodeActions: nil}, nil
	}

	formattedStr := string(formatted)
	if formattedStr == input.Content {
		return ToolResult{Capabilities: caps, CodeActions: nil}, nil
	}

	// Compute minimal edits between original and formatted
	edits := computeGoEdits(input.Content, formattedStr, input.FilePath)
	if len(edits) == 0 {
		return ToolResult{Capabilities: caps, CodeActions: nil}, nil
	}

	actions := []ToolCodeAction{
		{
			Title: "Organize Imports",
			Kind:  "source.organizeImports",
			Edits: edits,
		},
	}

	return ToolResult{Capabilities: caps, CodeActions: actions}, nil
}

// computeGoEdits produces a list of edits by comparing original and new text.
func computeGoEdits(original, modified, filePath string) []ToolCodeActionEdit {
	// Find common prefix
	prefixLen := 0
	for prefixLen < len(original) && prefixLen < len(modified) && original[prefixLen] == modified[prefixLen] {
		prefixLen++
	}

	// Find common suffix
	origSuffix := len(original)
	modSuffix := len(modified)
	for origSuffix > prefixLen && modSuffix > prefixLen && original[origSuffix-1] == modified[modSuffix-1] {
		origSuffix--
		modSuffix--
	}

	if prefixLen == origSuffix && prefixLen == modSuffix {
		return nil // no changes
	}

	return []ToolCodeActionEdit{
		{
			FilePath: filePath,
			From:     prefixLen,
			To:       origSuffix,
			NewText:  modified[prefixLen:modSuffix],
		},
	}
}

// runGoSignatureHelp retrieves signature help at a position using gopls.
func runGoSignatureHelp(input ToolInput) (ToolResult, error) {
	caps := Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: true, SignatureHelp: true}

	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: true, SignatureHelp: false},
			Error:        "gopls_not_available",
		}, nil
	}

	pos := input.Position
	if pos == nil {
		pos = &Position{Line: 1, Column: 1}
	}
	posArg := fmt.Sprintf("%s:%d:%d", input.FilePath, pos.Line, pos.Column)

	cmd := exec.Command(goplsPath, "signature", posArg)
	cmd.Dir = input.WorkspaceRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// gopls signature-help returns non-zero when no signature is available.
		return ToolResult{
			Capabilities:  caps,
			SignatureHelp: &ToolSignatureHelp{},
		}, nil
	}

	sigHelp := parseGoplsSignatureHelp(stdout.String(), input)
	return ToolResult{
		Capabilities:  caps,
		SignatureHelp: &sigHelp,
	}, nil
}

// parseGoplsSignatureHelp parses gopls signature-help output.
// Output format is plain text like:
//
//	func (t *T) MethodName(param1 type1, param2 type2) (result)
//	param1 type1, param2 type2
//
// Line 1 is the full signature. Line 2 (if present) is just the params.
// We parse it into our structured type. Active parameter is computed from
// the cursor position by counting commas at the same nesting depth.
func parseGoplsSignatureHelp(output string, input ToolInput) ToolSignatureHelp {
	text := strings.TrimSpace(output)
	if text == "" {
		return ToolSignatureHelp{}
	}

	lines := strings.Split(text, "\n")
	sigLabel := strings.TrimSpace(lines[0])
	if sigLabel == "" {
		return ToolSignatureHelp{}
	}

	// Extract parameters from the signature label.
	// Find the first '(' and its matching ')'.
	params := extractParamsFromSignature(sigLabel)

	// Compute active parameter from cursor position in the source content.
	activeParam := computeActiveParameter(input)

	return ToolSignatureHelp{
		Signatures: []ToolSignatureHelpSignature{
			{
				Label:      sigLabel,
				Parameters: params,
			},
		},
		ActiveSignature: 0,
		ActiveParameter: activeParam,
	}
}

// computeActiveParameter determines which parameter the cursor is on by
// counting commas at depth 0 between the opening paren and the cursor position.
func computeActiveParameter(input ToolInput) int {
	if input.Position == nil {
		return 0
	}

	lines := strings.Split(input.Content, "\n")
	if input.Position.Line < 1 || input.Position.Line > len(lines) {
		return 0
	}

	// Build the text up to the cursor position
	textUpToCursor := ""
	for i := 0; i < input.Position.Line-1; i++ {
		textUpToCursor += lines[i] + "\n"
	}
	lineIdx := input.Position.Line - 1
	if input.Position.Column > 0 {
		lineRunes := []rune(lines[lineIdx])
		col := input.Position.Column - 1
		if col <= len(lineRunes) {
			textUpToCursor += string(lineRunes[:col])
		} else {
			textUpToCursor += lines[lineIdx]
		}
	}

	// Walk backward from cursor to find the most recent unmatched '('
	depth := 0
	commaCount := 0
	for i := len(textUpToCursor) - 1; i >= 0; i-- {
		ch := textUpToCursor[i]
		if ch == ')' {
			depth++
		} else if ch == '(' {
			if depth == 0 {
				return commaCount
			}
			depth--
		} else if ch == ',' && depth == 0 {
			commaCount++
		}
	}
	return commaCount
}

// extractParamsFromSignature parses "func(params) result" or "func (recv) Name(params) result"
// into individual parameters. For Go methods, the first paren group is the receiver;
// we skip it and use the parameter paren group instead.
func extractParamsFromSignature(sig string) []ToolSignatureHelpParameter {
	// Find all top-level paren groups.
	start := strings.Index(sig, "(")
	if start < 0 {
		return nil
	}

	// Collect all top-level paren groups.
	type parenGroup struct {
		open, close int
	}
	var groups []parenGroup
	i := start
	for i < len(sig) {
		if sig[i] != '(' {
			i++
			continue
		}
		depth := 1
		j := i + 1
		for j < len(sig) && depth > 0 {
			switch sig[j] {
			case '(':
				depth++
			case ')':
				depth--
			}
			j++
		}
		groups = append(groups, parenGroup{open: i, close: j - 1})
		i = j
	}

	if len(groups) == 0 {
		return nil
	}

	// For Go methods like "func (t *T) Method(a int, b string) error",
	// the parameter list is the second group (first is receiver).
	// For plain functions like "func foo(a int, b string) error",
	// the parameter list is the first group.
	// Heuristic: if there are multiple groups, check if the first one
	// looks like a receiver (short, contains '*' or a single identifier).
	paramIdx := 0
	if len(groups) > 1 {
		// If the first group is followed by an identifier (method name),
		// it's a receiver. Otherwise it's the parameter list.
		afterFirst := sig[groups[0].close+1:]
		// Trim leading space
		afterFirst = strings.TrimLeft(afterFirst, " ")
		// If the next non-space char is an identifier or '*', it's a method
		// with a receiver — use the second group.
		if len(afterFirst) > 0 && (isIdentRune(rune(afterFirst[0])) || afterFirst[0] == '*') {
			paramIdx = 1
		}
	}

	if paramIdx >= len(groups) {
		return nil
	}

	paramStr := sig[groups[paramIdx].open+1 : groups[paramIdx].close]

	if paramStr == "" {
		return nil
	}

	// Split by comma, respecting nested parens/brackets.
	var params []ToolSignatureHelpParameter
	d := 0
	current := strings.Builder{}
	for _, ch := range paramStr {
		switch ch {
		case '(', '[', '{':
			d++
			current.WriteRune(ch)
		case ')', ']', '}':
			d--
			current.WriteRune(ch)
		case ',':
			if d == 0 {
				p := strings.TrimSpace(current.String())
				if p != "" {
					params = append(params, ToolSignatureHelpParameter{Label: p})
				}
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}
	p := strings.TrimSpace(current.String())
	if p != "" {
		params = append(params, ToolSignatureHelpParameter{Label: p})
	}

	return params
}
