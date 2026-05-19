package agent

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// extractSubagentSummary parses stdout from a subagent execution to extract key information
// Optimized to avoid regex compilation in loops and process only relevant lines
func extractSubagentSummary(stdout string) map[string]string {
	summary := make(map[string]string)

	// Pre-compile regex patterns once (outside the loop)
	passedRe := regexp.MustCompile(`(\d+)\s+passed`)
	failedRe := regexp.MustCompile(`(\d+)\s+failed`)
	todoRe := regexp.MustCompile(`(Added|Marked|Created|Updated|Completed|Removed)\s+(\d+)\s+todos?`)
	cmdRe := regexp.MustCompile(`(?:command|Running):\s+([^\n]+)`)

	// Compile metrics regex patterns once
	totalTokensRe := regexp.MustCompile(`total_tokens=(\d+)`)
	promptTokensRe := regexp.MustCompile(`prompt_tokens=(\d+)`)
	completionTokensRe := regexp.MustCompile(`completion_tokens=(\d+)`)
	totalCostRe := regexp.MustCompile(`total_cost=([\d.]+)`)
	cachedTokensRe := regexp.MustCompile(`cached_tokens=(\d+)`)

	lines := strings.Split(stdout, "\n")

	var fileChanges []string
	var buildStatus string
	var testStatus string
	var errors []string
	var todosCreated []string
	var commandsExecuted []string
	var testPassCount, testFailCount int

	// Process lines but limit to first 10,000 to avoid excessive processing
	maxLines := 10000
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	for _, line := range lines {
		// Skip empty lines early
		if line == "" {
			continue
		}

		// Only trim if needed (check if line has leading/trailing whitespace)
		trimmedLine := line
		if line[0] == ' ' || line[0] == '\t' || line[len(line)-1] == ' ' || line[len(line)-1] == '\t' {
			trimmedLine = strings.TrimSpace(line)
		}

		// Fast-path checks using byte operations for common prefixes
		if len(trimmedLine) > 0 {
			firstChar := trimmedLine[0]

			// Extract file operations (fast ASCII checks)
			switch firstChar {
			case 'C', 'c':
				if strings.HasPrefix(trimmedLine, "Created:") || strings.HasPrefix(trimmedLine, "Wrote") {
					file := strings.TrimSpace(trimmedLine[8:])
					if strings.HasPrefix(trimmedLine, "Created:") {
						file = strings.TrimSpace(trimmedLine[8:])
					} else if strings.HasPrefix(trimmedLine, "Wrote") {
						file = strings.TrimSpace(trimmedLine[6:])
					}
					fileChanges = append(fileChanges, "Created: "+file)
				}
			case 'M', 'm':
				if strings.HasPrefix(trimmedLine, "Modified:") {
					file := strings.TrimSpace(trimmedLine[9:])
					fileChanges = append(fileChanges, "Modified: "+file)
				}
			case 'D', 'd':
				if strings.HasPrefix(trimmedLine, "Deleted:") {
					file := strings.TrimSpace(trimmedLine[8:])
					fileChanges = append(fileChanges, "Deleted: "+file)
				}
			case 'U', 'u':
				if strings.HasPrefix(trimmedLine, "Updated:") {
					file := strings.TrimSpace(trimmedLine[8:])
					fileChanges = append(fileChanges, "Updated: "+file)
				}
			case 'E', 'e':
				if strings.HasPrefix(trimmedLine, "Error:") || strings.HasPrefix(trimmedLine, "error:") {
					errors = append(errors, trimmedLine)
				}
			case 'S', 's':
				if strings.HasPrefix(trimmedLine, "SUBAGENT_METRICS:") {
					// Parse the metrics using pre-compiled regex
					if matches := totalTokensRe.FindStringSubmatch(trimmedLine); len(matches) > 1 {
						summary["subagent_total_tokens"] = matches[1]
					}
					if matches := promptTokensRe.FindStringSubmatch(trimmedLine); len(matches) > 1 {
						summary["subagent_prompt_tokens"] = matches[1]
					}
					if matches := completionTokensRe.FindStringSubmatch(trimmedLine); len(matches) > 1 {
						summary["subagent_completion_tokens"] = matches[1]
					}
					if matches := totalCostRe.FindStringSubmatch(trimmedLine); len(matches) > 1 {
						summary["subagent_total_cost"] = matches[1]
					}
					if matches := cachedTokensRe.FindStringSubmatch(trimmedLine); len(matches) > 1 {
						summary["subagent_cached_tokens"] = matches[1]
					}
					continue // Skip further processing for metrics lines
				}
			}

			// Extract build status (only if line contains "Build:")
			if strings.Contains(trimmedLine, "Build:") {
				if strings.Contains(trimmedLine, "[OK] Passed") {
					buildStatus = "passed"
				} else if strings.Contains(trimmedLine, "[OK] Failed") || strings.Contains(trimmedLine, "[FAIL] Failed") {
					buildStatus = "failed"
				}
			}

			// Extract test status and counts
			if strings.Contains(trimmedLine, "Test:") || strings.Contains(trimmedLine, "Tests:") {
				if strings.Contains(trimmedLine, "[OK] Passed") {
					testStatus = "passed"
				} else if strings.Contains(trimmedLine, "[OK] Failed") || strings.Contains(trimmedLine, "[FAIL] Failed") {
					testStatus = "failed"
				}

				// Extract test counts using pre-compiled regex
				if matches := passedRe.FindStringSubmatch(trimmedLine); len(matches) > 1 {
					fmt.Sscanf(matches[1], "%d", &testPassCount)
				}
				if matches := failedRe.FindStringSubmatch(trimmedLine); len(matches) > 1 {
					fmt.Sscanf(matches[1], "%d", &testFailCount)
				}
			}

			// Extract todo list operations
			if strings.Contains(trimmedLine, "TodoWrite") || strings.Contains(trimmedLine, "todo list") {
				if matches := todoRe.FindStringSubmatch(trimmedLine); len(matches) > 2 {
					todosCreated = append(todosCreated, matches[0])
				}
			}

			// Extract shell commands executed
			if strings.Contains(trimmedLine, "$") || strings.Contains(trimmedLine, "shell_command") {
				if strings.HasPrefix(trimmedLine, "$") {
					cmd := strings.TrimSpace(trimmedLine[1:])
					if cmd != "" {
						commandsExecuted = append(commandsExecuted, cmd)
					}
				}
				if strings.Contains(trimmedLine, "Executing command") || strings.Contains(trimmedLine, "Running") {
					if matches := cmdRe.FindStringSubmatch(trimmedLine); len(matches) > 1 {
						commandsExecuted = append(commandsExecuted, strings.TrimSpace(matches[1]))
					}
				}
			}
		}
	}

	if len(fileChanges) > 0 {
		summary["files"] = strings.Join(fileChanges, "; ")
	}
	if buildStatus != "" {
		summary["build_status"] = buildStatus
	}
	if testStatus != "" {
		summary["test_status"] = testStatus
		if testPassCount > 0 || testFailCount > 0 {
			summary["test_counts"] = fmt.Sprintf("%d passed, %d failed", testPassCount, testFailCount)
		}
	}
	if len(errors) > 0 {
		summary["errors"] = strings.Join(errors, "; ")
	}
	if len(todosCreated) > 0 {
		summary["todos"] = strings.Join(todosCreated, "; ")
	}
	if len(commandsExecuted) > 0 {
		// Limit to first 10 commands to avoid overwhelming output
		if len(commandsExecuted) > 10 {
			commandsExecuted = commandsExecuted[:10]
			summary["commands"] = strings.Join(commandsExecuted, "; ") + "..."
		} else {
			summary["commands"] = strings.Join(commandsExecuted, "; ")
		}
	}

	return summary
}

func (a *Agent) warnSubagentFallback(scope, configuredProvider, configuredModel, effectiveProvider, effectiveModel string) {
	usesProviderFallback := configuredProvider == "" && strings.TrimSpace(effectiveProvider) != ""
	usesModelFallback := configuredModel == "" && strings.TrimSpace(effectiveModel) != ""
	if !usesProviderFallback && !usesModelFallback {
		return
	}

	provider := strings.TrimSpace(effectiveProvider)
	if provider == "" {
		provider = "<system default>"
	}
	model := strings.TrimSpace(effectiveModel)
	if model == "" {
		model = "<provider default>"
	}

	a.PrintLineAsync(fmt.Sprintf("[WARN] Subagent fallback active (%s): provider=%s model=%s", scope, provider, model))
}

// Helper functions for subagent handlers

// truncateString truncates a string to a maximum length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// stripAnsiCodes removes ANSI escape codes from a string
func stripAnsiCodes(s string) string {
	// ANSI escape code regex pattern
	ansiEscape := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	return ansiEscape.ReplaceAllString(s, "")
}

// isPathInWorkspace checks if a path is within the workspace directory
func isPathInWorkspace(path, workspaceDir string) bool {
	if path == workspaceDir {
		return true
	}
	return strings.HasPrefix(path, workspaceDir+string(filepath.Separator))
}

// isPathInTmp checks if a path is in /tmp/ for temporary file access
func isPathInTmp(path string) bool {
	// Check for /tmp/ or /var/folders/.../T/ (macOS temp dir) or any path containing tmp
	return strings.Contains(path, "/tmp/") ||
		strings.Contains(path, "/var/folders/.../T/") ||
		strings.Contains(strings.ToLower(path), "/tmp/")
}

// commonParent finds the common parent directory of multiple paths
func commonParent(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	if len(paths) == 1 {
		return filepath.Dir(paths[0])
	}
	result := paths[0]
	for _, p := range paths[1:] {
		for !strings.HasPrefix(p+string(filepath.Separator), result+string(filepath.Separator)) && p != result {
			result = filepath.Dir(result)
			if result == "/" || result == "." {
				return result
			}
		}
	}
	return result
}
