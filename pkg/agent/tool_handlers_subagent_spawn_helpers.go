// Subagent spawn helper functions (extracted from tool_handlers_subagent_spawn.go).
//
// These helpers operate on the textual output and effective provider/model
// resolution produced during subagent dispatch; they do not require the full
// *Agent receiver used by handleRun*Subagent and so live in a separate file
// for readability per SP-075 large-file decomposition guidance.

package agent

import (
	"fmt"
	"regexp"
	"strings"
)

// extractSubagentSummary parses stdout from a subagent execution to extract key information.
// Optimized to avoid regex compilation in loops and process only relevant lines.
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

// warnSubagentFallback logs a single async warning line when the effective
// provider/model for a subagent call came from the parent-agent fallback
// path (neither persona nor config specified explicit values).
//
// A "true" fallback is when BOTH the persona-supplied and config-supplied
// provider/model are empty, so the values were inherited from the parent's
// runtime configuration. Other code paths (persona-set or config-set) do
// not warn, since the user requested those values explicitly.
//
// Extracted from tool_handlers_subagent_spawn.go as part of SP-075's
// large-file decomposition.
func (a *Agent) warnSubagentFallback(scope, personaProvider, personaModel, configProvider, configModel, effectiveProvider, effectiveModel string) {
	personaProvider = strings.TrimSpace(personaProvider)
	personaModel = strings.TrimSpace(personaModel)
	configProvider = strings.TrimSpace(configProvider)
	configModel = strings.TrimSpace(configModel)
	effectiveProvider = strings.TrimSpace(effectiveProvider)
	effectiveModel = strings.TrimSpace(effectiveModel)

	// Only warn when effective values came from a TRUE fallback — i.e., both
	// the persona AND the config-level provider/model are absent. That means
	// the resolved values were inherited from the parent agent's runtime
	// provider/model, which is the only scenario worth surfacing to the user.
	usesProviderFallback := personaProvider == "" && configProvider == "" && effectiveProvider != ""
	usesModelFallback := personaModel == "" && configModel == "" && effectiveModel != ""
	if !usesProviderFallback && !usesModelFallback {
		return
	}

	provider := effectiveProvider
	if provider == "" {
		provider = "<system default>"
	}
	model := effectiveModel
	if model == "" {
		model = "<provider default>"
	}

	a.PrintLineAsync(fmt.Sprintf("[WARN] Subagent fallback active (%s): provider=%s model=%s", scope, provider, model))
}
