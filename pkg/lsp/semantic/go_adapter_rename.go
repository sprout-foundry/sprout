package semantic

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

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
