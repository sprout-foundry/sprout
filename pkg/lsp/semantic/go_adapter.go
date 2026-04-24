package semantic

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
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
	default:
		return ToolResult{Capabilities: Capabilities{}}, nil
	}
}

// runGoDiagnostics reports syntax errors (via gofmt -e) and vet issues
// (via go vet) by writing the current file content to a temp package.
func runGoDiagnostics(input ToolInput) (ToolResult, error) {
	tmpDir, err := os.MkdirTemp("", "ledit-go-diag-*")
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
	const goMod = "module ledit_temp\n\ngo 1.21\n"
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
	tmpDir, err := os.MkdirTemp("", "ledit-go-codeaction-*")
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
