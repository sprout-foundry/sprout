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
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/webcontent"
	"github.com/alantheprice/ledit/pkg/workspace"
)

// ProcessInstructionsWithWorkspace delegates to ProcessInstructions (legacy, no tag injection).
func ProcessInstructionsWithWorkspace(instructions string, cfg *config.Config) (string, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)
	logger.Logf("DEBUG: ProcessInstructionsWithWorkspace called with: %s", instructions)
	return ProcessInstructions(instructions, cfg)
}

// ProcessInstructions processes the tags found in the editor's context,
// interpreting them to perform various operations such as applying code changes,
// generating new content, or interacting with external tools.
func ProcessInstructions(instructions string, cfg *config.Config) (string, error) {
	// Note: Search grounding is now handled via explicit tool calls instead of #SG flags
	// This prevents accidental triggering by LLM responses and provides better control

	logger := utils.GetLogger(cfg.SkipPrompt)
	// Fast-path delete: Detect "Delete the file named 'X'" and perform locally
	if m := regexp.MustCompile(`(?i)delete the file named ['"]([^'"]+)['"]`).FindStringSubmatch(instructions); len(m) == 2 {
		target := m[1]
		// Remove from disk if present
		if err := os.Remove(target); err == nil {
			logger.Logf("Deleted file: %s", target)
		} else if !os.IsNotExist(err) {
			logger.Logf("Warning: could not delete %s: %v", target, err)
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
			if len(m) > 1 && m[1] != "" {
				// Use explicit quoted query if provided
				query = m[1]
			} else {
				// Fallback to using the main instruction text as query
				// Remove the #SG part and use the rest as query
				queryText := sgRe.ReplaceAllString(instructions, "")
				query = strings.TrimSpace(queryText)
				// Limit query length to avoid token limits
				if len(query) > 200 {
					query = query[:200]
				}
			}
			// Debug logging
			logger.Logf("DEBUG: Search grounding query: '%s'", query)
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

	// Strip deprecated workspace tags if provided to avoid accidental processing
	instructions = regexp.MustCompile(`(?i)\s*#(WS|WORKSPACE)\b`).ReplaceAllString(instructions, "")

	// Updated pattern to capture line ranges: #filename:start-end or #filename:start,end
	// Made more specific to avoid matching markdown headers by requiring at least one letter before any special chars
	filePattern := regexp.MustCompile(`\s+#([a-zA-Z][\w.-]*)(?::(\d+)[-,](\d+))?`)
	matches := filePattern.FindAllStringSubmatch(instructions, -1)
	logger.Logf("full instructions: %s", instructions)
	logger.Log("Found patterns:")
	logger.Logf(" %v", matches) // Logging the patterns found

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
				logger.Logf("Warning: Invalid start line number '%s' for %s, using full file", match[2], path)
				startLine = 0
			}
			if endLine, err = strconv.Atoi(match[3]); err != nil {
				logger.Logf("Warning: Invalid end line number '%s' for %s, using full file", match[3], path)
				endLine = 0
			}
		}

		logger.Logf("Processing path: %s", path) // Logging the path being processed
		if startLine > 0 && endLine > 0 {
			logger.Logf(" (lines %d-%d)", startLine, endLine)
		}

		if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
			content, err = webcontent.NewWebContentFetcher().FetchWebContent(path, cfg) // Pass cfg here
			if err != nil {
				logger.Log(prompts.URLFetchError(path, err))
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
				logger.Log(prompts.FileLoadError(path, err))
				continue
			}
		}

		// Replace the original pattern (including line range) with content
		originalPattern := match[0] // Full match including whitespace and line range
		instructions = strings.Replace(instructions, originalPattern, content, 1)
	}
	return instructions, nil
}
