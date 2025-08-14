package editor

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/webcontent"
	"github.com/alantheprice/ledit/pkg/workspace"
)

// ProcessInstructionsWithWorkspace appends the workspace tag and delegates to ProcessInstructions.
func ProcessInstructionsWithWorkspace(instructions string, cfg *config.Config) (string, error) {
	// Replace any existing #WS or #WORKSPACE tags with a single #WS tag
	re := regexp.MustCompile(`(?i)\s*#(WS|WORKSPACE)\s*$`)
	instructions = re.ReplaceAllString(instructions, "") + " #WS"

	return ProcessInstructions(instructions, cfg)
}

// ProcessInstructions processes the tags found in the editor's context,
// interpreting them to perform various operations such as applying code changes,
// generating new content, or interacting with external tools.
func ProcessInstructions(instructions string, cfg *config.Config) (string, error) {
	// Note: Search grounding is now handled via explicit tool calls instead of #SG flags
	// This prevents accidental triggering by LLM responses and provides better control

	// Fast-path delete: Detect "Delete the file named 'X'" and perform locally
	if m := regexp.MustCompile(`(?i)delete the file named ['"]([^'"]+)['"]`).FindStringSubmatch(instructions); len(m) == 2 {
		target := m[1]
		// Remove from disk if present
		if err := os.Remove(target); err == nil {
			ui.Out().Printf("Deleted file: %s\n", target)
		} else if !os.IsNotExist(err) {
			ui.Out().Printf("Warning: could not delete %s: %v\n", target, err)
		}
		// Remove from workspace.json if present
		ws, err := workspace.LoadWorkspaceFile()
		if err == nil {
			if _, ok := ws.Files[target]; ok {
				delete(ws.Files, target)
				_ = os.MkdirAll(filepath.Dir(workspace.DefaultWorkspaceFilePath), os.ModePerm)
				_ = workspace.SaveWorkspace(ws)
			}
		}
		// Done – no LLM call needed
		return "", nil
	}

	// Handle optional search grounding when flag is enabled
	if cfg.UseSearchGrounding {
		// Extract optional quoted query after #SG
		sgRe := regexp.MustCompile(`(?i)#SG(?:\s+"([^"]*)")?`)
		if m := sgRe.FindStringSubmatch(instructions); m != nil {
			query := ""
			if len(m) > 1 {
				query = m[1]
			}
			// Log initiation happens inside FetchContextFromSearch
			ctx, err := webcontent.FetchContextFromSearch(query, cfg)
			if err == nil && ctx != "" {
				instructions = ctx + "\n\n" + instructions
			}
		}
		// Strip all #SG tokens from instructions
		instructions = sgRe.ReplaceAllString(instructions, "")
	} else {
		// If not using search grounding, strip #SG to avoid mis-parsing as a file tag
		instructions = regexp.MustCompile(`(?i)#SG(?:\s+"([^"]*)")?`).ReplaceAllString(instructions, "")
	}

	// Updated pattern to capture line ranges: #filename:start-end or #filename:start,end
	filePattern := regexp.MustCompile(`\s+#(\S+)(?::(\d+)[-,](\d+))?`)
	matches := filePattern.FindAllStringSubmatch(instructions, -1)
	ui.Out().Printf("full instructions: %s\n", instructions)
	ui.Out().Print("Found patterns:")
	ui.Out().Printf(" %v\n", matches) // Logging the patterns found

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		path := match[1]
		var startLine, endLine int
		var err error
		var content string

		// Parse line range if provided
		if len(match) >= 4 && match[2] != "" && match[3] != "" {
			if startLine, err = strconv.Atoi(match[2]); err != nil {
				ui.Out().Printf("Warning: Invalid start line number '%s' for %s, using full file\n", match[2], path)
				startLine = 0
			}
			if endLine, err = strconv.Atoi(match[3]); err != nil {
				ui.Out().Printf("Warning: Invalid end line number '%s' for %s, using full file\n", match[3], path)
				endLine = 0
			}
		}

		ui.Out().Printf("Processing path: %s", path) // Logging the path being processed
		if startLine > 0 && endLine > 0 {
			ui.Out().Printf(" (lines %d-%d)", startLine, endLine)
		}
		ui.Out().Print("\n")

		if path == "WORKSPACE" || path == "WS" {
			ui.Out().Print(prompts.LoadingWorkspaceData() + "\n")
			content = workspace.GetWorkspaceContext(instructions, cfg)
		} else if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
			content, err = webcontent.NewWebContentFetcher().FetchWebContent(path, cfg) // Pass cfg here
			if err != nil {
				ui.Out().Print(prompts.URLFetchError(path, err))
				continue
			}
		} else {
			// Use partial loading if line range is specified
			if startLine > 0 && endLine > 0 {
				content, err = filesystem.LoadFileContentWithRange(path, startLine, endLine)
			} else {
				content, err = filesystem.LoadFileContent(path)
			}
			if err != nil {
				ui.Out().Print(prompts.FileLoadError(path, err))
				continue
			}
		}

		// Replace the original pattern (including line range) with content
		originalPattern := match[0] // Full match including whitespace and line range
		instructions = strings.Replace(instructions, originalPattern, content, 1)
	}
	return instructions, nil
}
