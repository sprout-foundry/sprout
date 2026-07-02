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
