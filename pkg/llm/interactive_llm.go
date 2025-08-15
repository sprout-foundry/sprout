package llm

import (
	"bufio"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
)

// ContextHandler is a function type that defines how context requests are handled.
// It takes a slice of ContextRequest and returns a string response and an error.
type ContextHandler func([]ContextRequest, *config.Config) (string, error)

// ContextRequest represents a request for additional context from the LLM.
type ContextRequest struct {
	Type  string `json:"type"`
	Query string `json:"query"`
}

// ContextResponse represents the LLM's response containing context requests.
type ContextResponse struct {
	ContextRequests []ContextRequest `json:"context_requests"`
}

// CallLLMWithInteractiveContext handles interactive LLM calls, processing context requests, and retrying the LLM call.
// This now supports both legacy context handling and new tool calling
func CallLLMWithInteractiveContext(
	modelName string,
	initialMessages []prompts.Message,
	filename string,
	cfg *config.Config,
	timeout time.Duration,
	contextHandler ContextHandler, // This is the key: it takes a handler function
) (string, error) {
	// Create file detector for automatic file detection
	detector := NewFileDetector()

	// Analyze the user's message for mentioned files
	var userPrompt string
	for _, msg := range initialMessages {
		if msg.Role == "user" {
			userPrompt += fmt.Sprintf("%v ", msg.Content)
		}
	}

	mentionedFiles := detector.DetectMentionedFiles(userPrompt)

	// Enhance the system prompt with tool information
	var enhancedMessages []prompts.Message

	// Add tool information to the system message if it exists
	for i, msg := range initialMessages {
		if i == 0 && msg.Role == "system" {
			enhancedContent := fmt.Sprintf("%s\n\n%s", msg.Content, FormatToolsForPrompt())

			// Add file detection warning if files were mentioned
			if len(mentionedFiles) > 0 {
				fileWarning := GenerateFileReadPrompt(mentionedFiles)
				enhancedContent += fileWarning
			}

			enhancedMessages = append(enhancedMessages, prompts.Message{
				Role:    msg.Role,
				Content: enhancedContent,
			})
		} else {
			enhancedMessages = append(enhancedMessages, msg)
		}
	}

	// If no system message, add tools as first message
	if len(enhancedMessages) == 0 || enhancedMessages[0].Role != "system" {
		toolContent := FormatToolsForPrompt()

		// Add file detection warning if files were mentioned
		if len(mentionedFiles) > 0 {
			fileWarning := GenerateFileReadPrompt(mentionedFiles)
			toolContent += fileWarning
		}

		toolMessage := prompts.Message{
			Role:    "system",
			Content: toolContent,
		}
		enhancedMessages = append([]prompts.Message{toolMessage}, enhancedMessages...)
	}

	currentMessages := enhancedMessages

	// LLM prompt pinning: hash and print the system prompt for drift detection
	if len(enhancedMessages) > 0 && enhancedMessages[0].Role == "system" {
		contentStr, _ := enhancedMessages[0].Content.(string)
		h := sha1.Sum([]byte(contentStr))
		ui.Out().Printf("[tools] system_prompt_hash: %x\n", h)
		if rl := utils.GetRunLogger(); rl != nil {
			msgDump, _ := json.Marshal(enhancedMessages)
			rl.LogEvent("interactive_start", map[string]any{"model": modelName, "messages": string(msgDump)})
		}
	}
	// Limit the number of interactive turns. Prefer configured attempts when provided
	maxRetries := cfg.OrchestrationMaxAttempts
	if maxRetries <= 0 {
		maxRetries = 8
	}

	// Anti-loop and cap enforcement state
	workspaceContextCalls := 0
	workspaceRequests := map[string]bool{}
	shellCalls := 0
	executedShell := map[string]bool{}
	noProgressStreak := 0
	// Additional guardrails for speed
	maxWorkspaceContextCalls := 1
	maxReadFileCalls := 12

	// Observability and caching
	toolCounts := map[string]int{}
	blockedCounts := map[string]int{}
	cacheHits := 0
	readFileCache := map[string]string{}
	persisted := LoadEvidenceCache()
	var turnDurations []time.Duration

	// Context budgeting (character-based approximation for control turns)
	const turnBudgetChars = 8000
	usedBudgetChars := 0

	// Budgets: track run time, tokens, and approximate cost
	runStart := time.Now()
	approxTokensUsed := 0
	pricing := GetModelPricing(modelName)

	checkBudgets := func() (bool, string) {
		// Time budget
		if cfg.MaxRunSeconds > 0 {
			if time.Since(runStart) >= time.Duration(cfg.MaxRunSeconds)*time.Second {
				return true, "time"
			}
		}
		// Token budget (approximate: 4 chars per token)
		if cfg.MaxRunTokens > 0 {
			if approxTokensUsed >= cfg.MaxRunTokens {
				return true, "tokens"
			}
		}
		// Cost budget (rough approximation)
		if cfg.MaxRunCostUSD > 0 {
			avgPer1K := (pricing.InputCostPer1K + pricing.OutputCostPer1K) / 2.0
			estCost := float64(approxTokensUsed) / 1000.0 * avgPer1K
			if estCost >= cfg.MaxRunCostUSD {
				return true, "cost"
			}
		}
		// Predictive: if no progress in last 2 turns and remaining budget low, force next action to execute_edits/validate
		return false, ""
	}

	printSummary := func() {
		// Compact end-of-run summary
		// Approximate cost using configured model pricing
		approxCost := 0.0
		if approxTokensUsed > 0 {
			p := GetModelPricing(modelName)
			avgPer1K := (p.InputCostPer1K + p.OutputCostPer1K) / 2.0
			approxCost = float64(approxTokensUsed) / 1000.0 * avgPer1K
		}
		ui.Out().Printf("[tools] summary: turns=%d tools=%d blocks=%d cache_hits=%d approx_tokens=%d approx_cost=%.5f\n",
			len(turnDurations),
			func() int {
				c := 0
				for _, v := range toolCounts {
					c += v
				}
				return c
			}(),
			func() int {
				c := 0
				for _, v := range blockedCounts {
					c += v
				}
				return c
			}(),
			cacheHits,
			approxTokensUsed,
			approxCost,
		)
		if rl := utils.GetRunLogger(); rl != nil {
			rl.LogEvent("interactive_summary", map[string]any{"turns": len(turnDurations), "tools": toolCounts, "blocked": blockedCounts, "cache_hits": cacheHits, "approx_tokens": approxTokensUsed, "approx_cost": approxCost})
		}
	}

	// Planner/Executor/Evaluator state
	plannedAction := ""
	plannedTarget := ""
	plannedInstructions := ""
	plannedStopWhen := ""

	// Cache a plan JSON if model returns edits alongside tool_calls
	cachedPlanJSON := ""
	for i := 0; i < maxRetries; i++ {
		turnStart := time.Now()
		ui.Out().Printf("[tools] turn %d/%d\n", i+1, maxRetries)
		// Call the main LLM response function (with simple backoff on transient/provider errors)
		var response string
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			response, err = GetLLMResponseWithTools(modelName, currentMessages, "", cfg, timeout)
			if err == nil {
				break
			}
			em := strings.ToLower(err.Error())
			if strings.Contains(em, "429") || strings.Contains(em, "503") || strings.Contains(em, "timeout") || strings.Contains(em, "deadline") {
				backoff := time.Duration(500*(1<<attempt)) * time.Millisecond
				jitter := time.Duration(rand.Intn(250)) * time.Millisecond
				time.Sleep(backoff + jitter)
				continue
			}
			break
		}
		if err != nil {
			turnDurations = append(turnDurations, time.Since(turnStart))
			printSummary()
			return "", fmt.Errorf("LLM call failed: %w", err)
		}
		ui.Out().Print("[tools] model returned a response\n")
		if rl := utils.GetRunLogger(); rl != nil {
			lastMsg := ""
			if len(currentMessages) > 0 {
				last := currentMessages[len(currentMessages)-1]
				lastMsgBytes, _ := json.Marshal(last)
				lastMsg = string(lastMsgBytes)
			}
			rl.LogEvent("interactive_turn", map[string]any{"turn": i + 1, "model": modelName, "last_message": lastMsg, "raw_response": response})
		}

		// Update token approximation from response length
		approxTokensUsed = (usedBudgetChars + len(response)) / 4
		// Early stop if any budget exceeded
		if stop, reason := checkBudgets(); stop {
			printSummary()
			return fmt.Sprintf("stopped due to %s budget", reason), nil
		}

		// Check if the response contains tool calls (preferred method)
		if containsToolCall(response) {
			// If the response also includes an edits JSON, cache it for potential fallback
			for _, obj := range splitTopLevelJSONObjects(response) {
				if strings.Contains(obj, "\"edits\"") {
					// validate it is JSON
					var probe map[string]any
					if json.Unmarshal([]byte(obj), &probe) == nil {
						cachedPlanJSON = obj
						break
					}
				}
			}
			// Parse and execute tool calls
			toolCalls, err := parseToolCalls(response)
			if err != nil || len(toolCalls) == 0 {
				// Log the response that failed to parse for debugging
				ui.Out().Printf("Failed to parse tool calls from response (length %d chars): %.100s...\n", len(response), response)
				if rl := utils.GetRunLogger(); rl != nil {
					rl.LogEvent("tool_call_parse_error", map[string]any{"length": len(response), "raw_response": response})
				}
				_ = os.MkdirAll(".ledit/runlogs", 0755)
				fn := fmt.Sprintf(".ledit/runlogs/tool_call_parse_error_%d.json", time.Now().UnixNano())
				_ = os.WriteFile(fn, []byte(response), 0644)
				ui.Out().Printf("[tools] wrote raw tool_call response to %s\n", fn)
				return "", fmt.Errorf("failed to parse tool calls")
			}

			if len(toolCalls) > 0 {
				// Log parsed tool calls
				if rl := utils.GetRunLogger(); rl != nil {
					b, _ := json.Marshal(toolCalls)
					rl.LogEvent("tool_calls_parsed", map[string]any{"count": len(toolCalls), "tool_calls": string(b)})
				}
				// Execute tool calls using basic implementation
				var toolResults []string
				editedOrValidated := false
				shellCapTripped := false
				workspaceCapTripped := false

				// Optimization: if all tool calls are independent read_file, batch concurrently
				allRead := true
				for _, tc := range toolCalls {
					if strings.TrimSpace(tc.Function.Name) != "read_file" {
						allRead = false
						break
					}
				}
				if allRead && len(toolCalls) > 1 {
					results := make([]string, len(toolCalls))
					done := make(chan struct{})
					tasks := 0
					for i, tc := range toolCalls {
						var args map[string]interface{}
						_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
						fp, _ := args["file_path"].(string)
						if fp == "" {
							results[i] = "Tool read_file blocked: missing 'file_path'"
							continue
						}
						// Check persisted cache first
						if entry, ok := persisted.Get("read_file", fp); ok {
							if h, err := ComputeFileHash(fp); err == nil && entry.FileHash == h {
								results[i] = fmt.Sprintf("Tool read_file result (served from cache): %s", entry.Value)
								continue
							}
						}

						tasks++
						go func(idx int, path string) {
							b, err := os.ReadFile(path)
							if err != nil {
								results[idx] = fmt.Sprintf("Tool read_file failed (not_found): %v", err)
							} else {
								val := string(b)
								results[idx] = fmt.Sprintf("Tool read_file result: %s", val)
								if h, err := ComputeFileHash(path); err == nil {
									persisted.Put(EvidenceEntry{Tool: "read_file", Key: path, Value: val, FilePath: path, FileHash: h, Updated: NowUnix()})
									_ = persisted.Save()
								}
							}
							done <- struct{}{}
						}(i, fp)
					}
					for k := 0; k < tasks; k++ {
						<-done
					}
					// Append in order
					toolResults = append(toolResults, results...)
					toolResultMessage := prompts.Message{Role: "system", Content: fmt.Sprintf("Tool execution results:\n%s", strings.Join(toolResults, "\n"))}
					currentMessages = append(currentMessages, toolResultMessage)
					turnDurations = append(turnDurations, time.Since(turnStart))
					continue
				}
				for _, toolCall := range toolCalls {
					ui.Out().Printf("[tools] executing %s\n", toolCall.Function.Name)
					if rl := utils.GetRunLogger(); rl != nil {
						rl.LogEvent("tool_call", map[string]any{"name": toolCall.Function.Name, "args": toolCall.Function.Arguments})
					}
					// Pre-validate and enforce caps/dedupes
					var args map[string]interface{}
					_ = json.Unmarshal([]byte(toolCall.Function.Arguments), &args)

					name := strings.TrimSpace(toolCall.Function.Name)
					if name != "" {
						toolCounts[name]++
					}
					// Global caps to reduce slow looping
					if name == "read_file" && toolCounts["read_file"] > maxReadFileCalls {
						toolResults = append(toolResults, "Tool read_file blocked: usage cap reached")
						blockedCounts["read_file_cap"]++
						continue
					}

					// Enforce Planner→Executor→Evaluator protocol
					switch name {
					case "plan_step":
						// Require action and stop_when
						act, _ := args["action"].(string)
						stop, _ := args["stop_when"].(string)
						if strings.TrimSpace(act) == "" || strings.TrimSpace(stop) == "" {
							toolResults = append(toolResults, "Tool plan_step blocked: missing action or stop_when")
							blockedCounts["plan_invalid"]++
							continue
						}
						plannedAction = strings.TrimSpace(act)
						plannedTarget, _ = args["target_file"].(string)
						if s, ok := args["instructions"].(string); ok {
							plannedInstructions = s
						} else {
							plannedInstructions = ""
						}
						plannedStopWhen = strings.TrimSpace(stop)
						toolResults = append(toolResults, fmt.Sprintf("Planner accepted: action=%s target=%s stop_when=%s", plannedAction, plannedTarget, plannedStopWhen))
						// Do not execute anything for planning; proceed to next tool call
						continue
					case "execute_step":
						// Must have a planned action first
						if plannedAction == "" {
							toolResults = append(toolResults, "Executor blocked: no plan available; call plan_step first")
							blockedCounts["exec_no_plan"]++
							continue
						}
						// Action must match plan
						act, _ := args["action"].(string)
						if strings.TrimSpace(act) != plannedAction {
							toolResults = append(toolResults, fmt.Sprintf("Executor blocked: action %s does not match planned %s", strings.TrimSpace(act), plannedAction))
							blockedCounts["exec_mismatch"]++
							continue
						}
						// If target_file/instructions omitted, inherit from plan
						if _, ok := args["target_file"]; !ok && plannedTarget != "" {
							args["target_file"] = plannedTarget
						}
						if _, ok := args["instructions"]; !ok && plannedInstructions != "" {
							args["instructions"] = plannedInstructions
						}
						// Rebuild the execute_step call with merged args
						passArgsBytes, _ := json.Marshal(args)
						merged := ToolCall{Type: "function", Function: ToolCallFunction{Name: name, Arguments: string(passArgsBytes)}}
						// Delegate to executor which dispatches underlying action
						result, err := executeBasicToolCall(merged, cfg)
						if err != nil {
							toolResults = append(toolResults, fmt.Sprintf("Tool %s failed (%s): %s", merged.Function.Name, classifyError(err), sanitizeOutput(err.Error())))
							if rl := utils.GetRunLogger(); rl != nil {
								rl.LogEvent("tool_result", map[string]any{"name": merged.Function.Name, "status": "error", "error": err.Error()})
							}
						} else {
							const maxLen = 2000
							norm := sanitizeOutput(result)
							if len(norm) > maxLen {
								norm = norm[:maxLen] + "\n... [truncated]"
							}
							toolResults = append(toolResults, fmt.Sprintf("Tool %s result: %s", merged.Function.Name, norm))
							if rl := utils.GetRunLogger(); rl != nil {
								rl.LogEvent("tool_result", map[string]any{"name": merged.Function.Name, "status": "ok"})
							}
						}
						if name == "execute_step" {
							// Mark edited/validated if underlying action did so
							ua, _ := args["action"].(string)
							if ua == "micro_edit" || ua == "edit_file_section" || ua == "validate_file" {
								editedOrValidated = true
							}
						}
						continue
					case "evaluate_outcome":
						// Let evaluator pass through, but require status
						status, _ := args["status"].(string)
						if strings.TrimSpace(status) == "" {
							toolResults = append(toolResults, "Evaluator blocked: missing status")
							blockedCounts["eval_invalid"]++
							continue
						}
						// Accept evaluator output (no local computation yet)
						toolResults = append(toolResults, fmt.Sprintf("Evaluator status: %s", strings.TrimSpace(status)))
						// If completed, mark summary and short-circuit by returning a final response
						if strings.EqualFold(strings.TrimSpace(status), "completed") {
							turnDurations = append(turnDurations, time.Since(turnStart))
							printSummary()
							return "COMPLETED", nil
						}
						// If continue, clear planned step to request a new one next turn
						plannedAction = ""
						plannedTarget = ""
						plannedInstructions = ""
						plannedStopWhen = ""
						continue
					default:
						// Block direct use of write/exec tools when not via execute_step; allow read/discovery tools
						blockedUnderlying := map[string]bool{
							"micro_edit": true, "edit_file_section": true, "validate_file": true, "run_shell_command": true,
						}
						if blockedUnderlying[name] {
							toolResults = append(toolResults, "Tool blocked: use plan_step → execute_step → evaluate_outcome. Do not call underlying tools directly.")
							blockedCounts["direct_tool_blocked"]++
							continue
						}
					}

					// Workspace context caps and dedupe
					if name == "workspace_context" {
						action, _ := args["action"].(string)
						query, _ := args["query"].(string)
						key := strings.TrimSpace(action) + "::" + strings.TrimSpace(query)
						// Deterministic file targeting: if user mentioned concrete files, prefer read_file over workspace_context
						if len(mentionedFiles) > 0 {
							toolResults = append(toolResults, "Tool workspace_context blocked: explicit file(s) mentioned; use read_file instead")
							blockedCounts["ws_block_explicit_target"]++
							continue
						}
						// Persistent cache lookup
						if entry, ok := persisted.Get("workspace_context", key); ok {
							cacheHits++
							toolResults = append(toolResults, fmt.Sprintf("Tool workspace_context result (served from cache): %s", entry.Value))
							continue
						}
						if workspaceContextCalls >= maxWorkspaceContextCalls {
							toolResults = append(toolResults, "Tool workspace_context blocked: usage cap reached")
							blockedCounts["workspace_context_cap"]++
							workspaceCapTripped = true
							continue
						}
						if workspaceRequests[key] {
							toolResults = append(toolResults, "Tool workspace_context blocked: duplicate request. You already have this evidence.")
							blockedCounts["workspace_context_dup"]++
							continue
						}
						workspaceRequests[key] = true
						workspaceContextCalls++
					}

					// Shell caps and dedupe
					if name == "run_shell_command" {
						cmdStr, _ := args["command"].(string)
						trimmed := strings.TrimSpace(cmdStr)
						if trimmed == "" {
							toolResults = append(toolResults, "Tool run_shell_command blocked: missing 'command'")
							blockedCounts["shell_missing"]++
							continue
						}
						// Interceptors: reject unsafe patterns
						lower := strings.ToLower(trimmed)
						if strings.Contains(lower, "rm -rf") || strings.Contains(lower, "mkfs") || strings.Contains(lower, " :(){ :|:& };:") || strings.Contains(lower, "shutdown") || strings.Contains(lower, "reboot") || strings.Contains(lower, "sudo ") {
							toolResults = append(toolResults, "Tool run_shell_command blocked: unsafe pattern")
							blockedCounts["shell_unsafe"]++
							continue
						}
						// Persistent cache lookup
						if entry, ok := persisted.Get("run_shell_command", trimmed); ok {
							cacheHits++
							toolResults = append(toolResults, fmt.Sprintf("Tool run_shell_command result (served from cache): %s", sanitizeOutput(entry.Value)))
							continue
						}
						if executedShell[trimmed] {
							toolResults = append(toolResults, "Tool run_shell_command blocked: duplicate command. You already have this evidence.")
							blockedCounts["shell_dup"]++
							continue
						}
						if shellCalls >= 5 {
							toolResults = append(toolResults, "Tool run_shell_command blocked: shell usage cap reached")
							blockedCounts["shell_cap"]++
							shellCapTripped = true
							continue
						}
						executedShell[trimmed] = true
						shellCalls++
					}

					// Simple read_file cache with served-from-cache marker
					if name == "read_file" {
						if fp, ok := args["file_path"].(string); ok && fp != "" {
							// Persistent cache lookup with file hash guard
							if entry, ok := persisted.Get("read_file", fp); ok {
								if entry.FilePath == fp {
									if h, err := ComputeFileHash(fp); err == nil && h == entry.FileHash {
										cacheHits++
										toolResults = append(toolResults, fmt.Sprintf("Tool read_file result (served from cache): %s", entry.Value))
										continue
									}
								}
							}
							if cached, ok := readFileCache[fp]; ok {
								cacheHits++
								toolResults = append(toolResults, fmt.Sprintf("Tool read_file result (served from cache): %s", cached))
								continue
							}
						}
					}

					// Execute allowed tools (non-underlying helpers like preflight)
					result, err := executeBasicToolCall(toolCall, cfg)
					if err != nil {
						toolResults = append(toolResults, fmt.Sprintf("Tool %s failed (%s): %s", toolCall.Function.Name, classifyError(err), sanitizeOutput(err.Error())))
						if rl := utils.GetRunLogger(); rl != nil {
							rl.LogEvent("tool_result", map[string]any{"name": toolCall.Function.Name, "status": "error", "error": err.Error()})
						}
					} else {
						// Normalize/cap outputs with truncation markers
						const maxLen = 2000
						norm := sanitizeOutput(result)
						if len(norm) > maxLen {
							norm = norm[:maxLen] + "\n... [truncated]"
						}
						toolResults = append(toolResults, fmt.Sprintf("Tool %s result: %s", toolCall.Function.Name, norm))
						if rl := utils.GetRunLogger(); rl != nil {
							rl.LogEvent("tool_result", map[string]any{"name": toolCall.Function.Name, "status": "ok"})
						}
						// Populate cache for read_file
						if name == "read_file" {
							if fp, ok := args["file_path"].(string); ok && fp != "" {
								readFileCache[fp] = result
								if h, err := ComputeFileHash(fp); err == nil {
									persisted.Put(EvidenceEntry{Tool: "read_file", Key: fp, Value: result, FilePath: fp, FileHash: h, Updated: NowUnix()})
									_ = persisted.Save()
								}
							}
						}
						// Populate persistent caches for shell/workspace_context
						if name == "run_shell_command" {
							cmdStr, _ := args["command"].(string)
							trimmed := strings.TrimSpace(cmdStr)
							if trimmed != "" {
								persisted.Put(EvidenceEntry{Tool: "run_shell_command", Key: trimmed, Value: result, Updated: NowUnix()})
								_ = persisted.Save()
							}
						}
						if name == "workspace_context" {
							action, _ := args["action"].(string)
							query, _ := args["query"].(string)
							key := strings.TrimSpace(action) + "::" + strings.TrimSpace(query)
							persisted.Put(EvidenceEntry{Tool: "workspace_context", Key: key, Value: result, Updated: NowUnix()})
							_ = persisted.Save()
							// Auto-follow: if this was a search_keywords result with a top_file, read it now to provide evidence
							if strings.EqualFold(strings.TrimSpace(action), "search_keywords") {
								var sr map[string]any
								if json.Unmarshal([]byte(result), &sr) == nil {
									if tf, ok := sr["top_file"].(string); ok && tf != "" {
										// Respect read_file cap
										if toolCounts["read_file"] < maxReadFileCalls {
											// Avoid duplicate reads
											if _, ok := readFileCache[tf]; !ok {
												rfArgs := map[string]any{"file_path": tf}
												rfBytes, _ := json.Marshal(rfArgs)
												rfCall := ToolCall{Type: "function", Function: ToolCallFunction{Name: "read_file", Arguments: string(rfBytes)}}
												rfRes, rfErr := executeBasicToolCall(rfCall, cfg)
												if rfErr == nil {
													toolCounts["read_file"]++
													toolResults = append(toolResults, fmt.Sprintf("Tool read_file result: %s", rfRes))
													readFileCache[tf] = rfRes
													if h, err := ComputeFileHash(tf); err == nil {
														persisted.Put(EvidenceEntry{Tool: "read_file", Key: tf, Value: rfRes, FilePath: tf, FileHash: h, Updated: NowUnix()})
														_ = persisted.Save()
													}
												} else {
													toolResults = append(toolResults, fmt.Sprintf("Tool read_file failed: %v", rfErr))
												}
											}
										}
									}
								}
							}
						}

					}

					if name == "micro_edit" || name == "edit_file_section" || name == "validate_file" {
						editedOrValidated = true
					}
				}

				// Add tool results to messages and continue (apply budget compression if needed)
				combined := strings.Join(toolResults, "\n")
				usedBudgetChars += len(combined)
				approxTokensUsed = usedBudgetChars / 4
				if stop, reason := checkBudgets(); stop {
					toolResultMessage := prompts.Message{Role: "system", Content: fmt.Sprintf("Tool execution results (partial):\n%s", combined)}
					currentMessages = append(currentMessages, toolResultMessage)
					turnDurations = append(turnDurations, time.Since(turnStart))
					printSummary()
					return fmt.Sprintf("stopped due to %s budget", reason), nil
				}
				if usedBudgetChars > turnBudgetChars {
					// Compress by truncating middle to keep head and tail context
					head := combined
					tail := ""
					if len(combined) > 1200 {
						head = combined[:600]
						tail = combined[len(combined)-600:]
						combined = head + "\n... [compressed due to turn budget] ...\n" + tail
					} else {
						end := turnBudgetChars
						if end > len(combined) {
							end = len(combined)
						}
						combined = combined[:end] + "\n... [compressed due to turn budget]"
					}
					usedBudgetChars = turnBudgetChars
				}
				toolResultMessage := prompts.Message{Role: "system", Content: fmt.Sprintf("Tool execution results:\n%s", combined)}

				currentMessages = append(currentMessages, toolResultMessage)

				// Inject guidance when caps are tripped
				if shellCapTripped || workspaceCapTripped {
					currentMessages = append(currentMessages, prompts.Message{
						Role:    "system",
						Content: "Operational caps reached. Stop exploring. Choose a specific file, use read_file, apply micro_edit or edit_file_section, then validate_file.",
					})
				}

				// If very first turn yields only workspace_context without any read_file, synthesize a single read on README.md or top_file to provide evidence
				if i == 0 && !editedOrValidated && toolCounts["read_file"] == 0 {
					// Try README.md first
					candidate := "README.md"
					// If we saw a workspace_context result with top_file, use it
					if entry, ok := persisted.Get("workspace_context", "search_keywords::"); ok && strings.Contains(entry.Value, "top_file") {
						var sr map[string]any
						if json.Unmarshal([]byte(entry.Value), &sr) == nil {
							if tf, ok := sr["top_file"].(string); ok && tf != "" {
								candidate = tf
							}
						}
					}
					if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
						rfArgs := map[string]any{"file_path": candidate}
						rfBytes, _ := json.Marshal(rfArgs)
						rfCall := ToolCall{Type: "function", Function: ToolCallFunction{Name: "read_file", Arguments: string(rfBytes)}}
						if rfRes, rfErr := executeBasicToolCall(rfCall, cfg); rfErr == nil {
							toolCounts["read_file"]++
							toolResults = append(toolResults, fmt.Sprintf("Tool read_file result: %s", rfRes))
							readFileCache[candidate] = rfRes
						}
					}
				}

				// No-progress detector: if no edit/validate for 2 turns, force deterministic step
				if !editedOrValidated {
					noProgressStreak++
					if noProgressStreak >= 2 {
						currentMessages = append(currentMessages, prompts.Message{
							Role:    "system",
							Content: "Stop searching. Choose the top relevant file (e.g., README.md or the last search result), use read_file now, then produce a minimal JSON plan of edits. Do not call workspace_context again.",
						})
						noProgressStreak = 0
					}
				} else {
					noProgressStreak = 0
				}
				turnDurations = append(turnDurations, time.Since(turnStart))
				continue
			}
		}

		// Fallback to legacy context request handling
		if strings.Contains(response, "context_requests") {
			contextRequests, err := extractContextRequests(response)
			if err != nil {
				return "", fmt.Errorf("failed to extract context requests: %w", err)
			}

			if len(contextRequests) > 0 {
				// Handle the context requests using the provided handler
				contextContent, err := contextHandler(contextRequests, cfg)
				if err != nil {
					return "", fmt.Errorf("failed to handle context request: %w", err)
				}

				// Append the context content as a new message from the user
				currentMessages = append(currentMessages, prompts.Message{
					Role:    "user",
					Content: fmt.Sprintf("Context information:\n%s", contextContent),
				})
				// Continue the loop to send the updated messages to the LLM
				continue
			}
		}

		// No tool_calls and no actionable context requests: instruct model to emit plan/tool_calls and try again, including guidance to discover files
		currentMessages = append(currentMessages, prompts.Message{Role: "system", Content: "No tool_calls found. You must emit a PLAN followed by TOOL_CALLS. Use plan_step → execute_step → evaluate_outcome. Avoid prose. If no file is specified, first use workspace_context.search_keywords to find the most relevant file, then read_file it, then micro_edit or edit_file_section, then validate_file."})
		turnDurations = append(turnDurations, time.Since(turnStart))
		continue
	}

	printSummary()
	if strings.TrimSpace(cachedPlanJSON) != "" {
		return cachedPlanJSON, nil
	}
	return "", fmt.Errorf("max interactive LLM retries reached (%d)", maxRetries)
}

// Helper functions for tool calling
func containsToolCall(response string) bool {
	// Check for explicit tool call JSON structures with proper context
	// Must be at the start of the response or in a JSON code block
	trimmed := strings.TrimSpace(response)

	// Check if response starts with JSON containing tool_calls
	if strings.HasPrefix(trimmed, "{") && strings.Contains(response, `"tool_calls"`) {
		return true
	}

	// Check for JSON code blocks that contain tool_calls
	if strings.Contains(response, "```json") {
		// Extract JSON blocks and check if they contain tool_calls
		start := strings.Index(response, "```json")
		if start >= 0 {
			start += 7
			end := strings.Index(response[start:], "```")
			if end > 0 {
				jsonContent := response[start : start+end]
				if strings.Contains(jsonContent, `"tool_calls"`) {
					return true
				}
			}
		}
	}

	return false
}

func parseToolCalls(response string) ([]ToolCall, error) {
	// Tolerant parse: tool_calls may have arguments as string or object, may use 'parameters',
	// or may place {name, arguments} at the call level under 'arguments'.
	normalize := func(m map[string]any) (ToolCall, bool) {
		id, _ := m["id"].(string)
		typ, _ := m["type"].(string)
		var fn map[string]any
		if x, ok := m["function"].(map[string]any); ok {
			fn = x
		} else if x, ok := m["arguments"].(map[string]any); ok { // Kimi variant
			fn = x
		}
		if fn == nil {
			return ToolCall{}, false
		}
		name, _ := fn["name"].(string)
		var rawArgs any
		if v, ok := fn["arguments"]; ok {
			rawArgs = v
		} else if v, ok := fn["parameters"]; ok {
			rawArgs = v
		}
		argsStr := "{}"
		switch a := rawArgs.(type) {
		case string:
			argsStr = a
		case map[string]any:
			if b, err := json.Marshal(a); err == nil {
				argsStr = string(b)
			}
		default:
			if b, err := json.Marshal(a); err == nil {
				argsStr = string(b)
			}
		}
		return ToolCall{ID: id, Type: typ, Function: ToolCallFunction{Name: name, Arguments: argsStr}}, true
	}

	tryParse := func(s string) ([]ToolCall, bool) {
		var raw map[string]any
		if err := json.Unmarshal([]byte(s), &raw); err == nil {
			if tcs, ok := raw["tool_calls"].([]any); ok && len(tcs) > 0 {
				var toolCalls []ToolCall
				for _, it := range tcs {
					m, _ := it.(map[string]any)
					if tc, ok := normalize(m); ok {
						toolCalls = append(toolCalls, tc)
					}
				}
				if len(toolCalls) > 0 {
					return toolCalls, true
				}
			}
		}
		return nil, false
	}

	if tcs, ok := tryParse(response); ok {
		return tcs, nil
	}

	// Fallback: split multiple concatenated top-level JSON objects and try each
	for _, obj := range splitTopLevelJSONObjects(response) {
		if tcs, ok := tryParse(obj); ok {
			return tcs, nil
		}
	}

	// Last-resort fallback: extract the tool_calls array manually and wrap it
	if idx := strings.Index(response, "\"tool_calls\""); idx != -1 {
		// Find the first '[' after "tool_calls"
		arrStart := strings.Index(response[idx:], "[")
		if arrStart != -1 {
			arrStart += idx
			// Scan forward to find matching ']' accounting for nested braces/brackets in arguments
			depth := 0
			for i := arrStart; i < len(response); i++ {
				ch := response[i]
				if ch == '[' {
					depth++
				} else if ch == ']' {
					depth--
					if depth == 0 {
						arr := strings.TrimSpace(response[arrStart : i+1])
						wrapper := "{\"tool_calls\": " + arr + "}"
						if tcs, ok := tryParse(wrapper); ok {
							return tcs, nil
						}
						break
					}
				}
			}
		}
	}

	// Try to parse the response as a full tool message (with role)
	var toolMessage ToolMessage
	if err := json.Unmarshal([]byte(response), &toolMessage); err == nil && len(toolMessage.ToolCalls) > 0 {
		return toolMessage.ToolCalls, nil
	}

	// Last resort: try splitting concatenated top-level objects
	for _, obj := range splitTopLevelJSONObjects(response) {
		var tm ToolMessage
		if err := json.Unmarshal([]byte(obj), &tm); err == nil && len(tm.ToolCalls) > 0 {
			return tm.ToolCalls, nil
		}
	}

	return []ToolCall{}, fmt.Errorf("no tool calls found in response")
}

func executeBasicToolCall(toolCall ToolCall, cfg *config.Config) (string, error) {
	// Parse the arguments - they might be a JSON string or already parsed object
	var args map[string]interface{}

	// Prefer Arguments if present; fallback to Parameters
	argSource := toolCall.Function.Arguments
	if strings.TrimSpace(argSource) == "" && len(toolCall.Function.Parameters) > 0 {
		argSource = string(toolCall.Function.Parameters)
	}

	// First try to unmarshal as JSON string
	if err := json.Unmarshal([]byte(argSource), &args); err != nil {
		return "", fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	switch toolCall.Function.Name {
	case "read_file":
		if filePath, ok := args["file_path"].(string); ok {
			// Use the filesystem package to read the file
			content, err := os.ReadFile(filePath)
			if err != nil {
				return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
			}
			return string(content), nil
		}
		return "", fmt.Errorf("read_file requires 'file_path' parameter")

	case "ask_user":
		if question, ok := args["question"].(string); ok {
			if cfg.SkipPrompt {
				return "User interaction skipped in non-interactive mode", nil
			}
			ui.Out().Printf("\n🤖 Question: %s\n", question)
			ui.Out().Print("Your answer: ")
			reader := bufio.NewReader(os.Stdin)
			answer, err := reader.ReadString('\n')
			if err != nil {
				return "", fmt.Errorf("failed to read user input: %w", err)
			}
			return strings.TrimSpace(answer), nil
		}
		return "", fmt.Errorf("ask_user requires 'question' parameter")

	case "run_shell_command":
		if command, ok := args["command"].(string); ok {
			cmd := exec.Command("sh", "-c", command)
			output, err := cmd.CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("command failed: %w\nOutput: %s", err, string(output))
			}
			return string(output), nil
		}
		return "", fmt.Errorf("run_shell_command requires 'command' parameter")

	case "workspace_context":
		action, _ := args["action"].(string)
		switch action {
		case "search_keywords":
			query, _ := args["query"].(string)
			if strings.TrimSpace(query) == "" {
				return "", fmt.Errorf("invalid: search_keywords requires 'query'")
			}
			// Walk workspace for likely text/code files and collect matches deterministically
			var matches []string
			_ = filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() {
					name := d.Name()
					if name == ".git" || name == "node_modules" || name == "vendor" || strings.HasPrefix(name, ".") {
						return filepath.SkipDir
					}
					return nil
				}
				if !isLikelyTextOrCode(path) {
					return nil
				}
				b, err := os.ReadFile(path)
				if err == nil && strings.Contains(string(b), query) {
					matches = append(matches, filepath.Clean(path))
				}
				return nil
			})
			if len(matches) == 0 {
				return "{}", nil
			}
			sort.Strings(matches)
			top := matches[0]
			res := map[string]any{"top_file": top, "matches": matches}
			bytes, _ := json.Marshal(res)
			return string(bytes), nil
		case "load_tree":
			var files []string
			_ = filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() {
					name := d.Name()
					if name == ".git" || name == "node_modules" || name == "vendor" || strings.HasPrefix(name, ".") {
						return filepath.SkipDir
					}
					return nil
				}
				if isLikelyTextOrCode(path) {
					files = append(files, filepath.Clean(path))
				}
				return nil
			})
			sort.Strings(files)
			if len(files) > 200 {
				files = files[:200]
			}
			bytes, _ := json.Marshal(map[string]any{"files": files})
			return string(bytes), nil
		case "load_summary":
			return "{\"status\":\"not_implemented\"}", nil
		case "search_embeddings":
			return "{\"status\":\"not_implemented\"}", nil
		default:
			return "", fmt.Errorf("invalid: unknown workspace_context action")
		}

	case "plan_step":
		// Echo back the plan to aid determinism; planner logic is model-driven
		return toolCall.Function.Arguments, nil

	case "execute_step":
		// Dispatch a single step to the corresponding existing tool
		var args map[string]interface{}
		_ = json.Unmarshal([]byte(toolCall.Function.Arguments), &args)
		action, _ := args["action"].(string)
		if strings.TrimSpace(action) == "" {
			return "", fmt.Errorf("invalid_args: execute_step requires action")
		}
		// Build a synthetic ToolCall for the underlying action, passing through other args
		passArgsBytes, _ := json.Marshal(args)
		return executeBasicToolCall(ToolCall{Type: "function", Function: ToolCallFunction{Name: action, Arguments: string(passArgsBytes)}}, cfg)

	case "evaluate_outcome":
		// Pass-through evaluator outcome; encourages the loop to stop or continue
		return toolCall.Function.Arguments, nil

	case "preflight":
		// Check optional file existence/writability and basic git cleanliness
		if fp, ok := args["file_path"].(string); ok && fp != "" {
			if _, err := os.Stat(fp); err != nil {
				return "", fmt.Errorf("not_found: %s", fp)
			}
			if f, err := os.OpenFile(fp, os.O_WRONLY, 0); err == nil {
				f.Close()
			} else {
				return "", fmt.Errorf("permission: not writable: %s", fp)
			}
		}
		// Git status (best-effort)
		if _, err := exec.LookPath("git"); err == nil {
			cmd := exec.Command("git", "status", "--porcelain")
			out, _ := cmd.CombinedOutput()
			return fmt.Sprintf("preflight ok; git status: %s", strings.TrimSpace(string(out))), nil
		}
		return "preflight ok; git not available", nil

	case "search_web":
		if !cfg.UseSearchGrounding {
			return "", fmt.Errorf("web search disabled by configuration")
		}
		if query, ok := args["query"].(string); ok {
			return fmt.Sprintf("Web search for '%s' - not implemented in this build", query), nil
		}
		return "", fmt.Errorf("search_web requires 'query' parameter")

	default:
		return "", fmt.Errorf("unknown tool: %s", toolCall.Function.Name)
	}
}

// isLikelyTextOrCode returns true for typical text/code files
func isLikelyTextOrCode(path string) bool {
	lower := strings.ToLower(path)
	// Common source and text extensions
	exts := []string{".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".java", ".rb", ".rs", ".c", ".cc", ".cpp", ".h", ".hpp", ".cs", ".php", ".kt", ".m", ".mm", ".swift", ".scala", ".sql", ".sh", ".bash", ".zsh", ".fish", ".yaml", ".yml", ".json", ".toml", ".ini", ".md", ".txt"}
	for _, e := range exts {
		if strings.HasSuffix(lower, e) {
			return true
		}
	}
	return false
}

// sanitizeOutput redacts possible secrets from logs
func sanitizeOutput(s string) string {
	// Basic redactions; extend as needed
	redactions := []string{"AWS_SECRET", "AWS_ACCESS_KEY", "OPENAI_API_KEY", "DEEPINFRA_API_KEY"}
	out := s
	for _, k := range redactions {
		if strings.Contains(out, k) {
			out = strings.ReplaceAll(out, k, "<REDACTED>")
		}
	}
	return out
}

// classifyError places errors into a coarse taxonomy for routing/analysis
func classifyError(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "permission") || strings.Contains(msg, "denied"):
		return "permission"
	case strings.Contains(msg, "not found") || strings.Contains(msg, "no such file"):
		return "not_found"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline"):
		return "transient"
	case strings.Contains(msg, "invalid") || strings.Contains(msg, "bad request"):
		return "invalid_args"
	default:
		return "unknown"
	}
}

func extractContextRequests(response string) ([]ContextRequest, error) {
	// Try to find JSON in the response
	var contextResp ContextResponse

	// First try parsing the whole response as JSON
	if err := json.Unmarshal([]byte(response), &contextResp); err == nil {
		return contextResp.ContextRequests, nil
	}

	// Look for JSON blocks
	if strings.Contains(response, "```json") {
		start := strings.Index(response, "```json") + 7
		end := strings.Index(response[start:], "```")
		if end > 0 {
			jsonStr := strings.TrimSpace(response[start : start+end])
			if err := json.Unmarshal([]byte(jsonStr), &contextResp); err == nil {
				return contextResp.ContextRequests, nil
			}
		}
	}

	// Look for bare JSON
	if strings.Contains(response, "context_requests") {
		// Try to extract JSON object containing context_requests
		start := strings.Index(response, "{")
		if start >= 0 {
			// Find the matching closing brace
			depth := 0
			for i := start; i < len(response); i++ {
				if response[i] == '{' {
					depth++
				} else if response[i] == '}' {
					depth--
					if depth == 0 {
						jsonStr := response[start : i+1]
						if err := json.Unmarshal([]byte(jsonStr), &contextResp); err == nil {
							return contextResp.ContextRequests, nil
						}
						break
					}
				}
			}
		}
	}

	return []ContextRequest{}, nil
}

// splitTopLevelJSONObjects splits a string that may contain multiple concatenated
// top-level JSON objects and returns each object string.
func splitTopLevelJSONObjects(s string) []string {
	var parts []string
	inStr := false
	esc := false
	depth := 0
	start := -1
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if ch == '\\' {
				esc = true
				continue
			}
			if ch == '"' {
				inStr = false
			}
			continue
		}
		if ch == '"' {
			inStr = true
			continue
		}
		if ch == '{' {
			if depth == 0 {
				start = i
			}
			depth++
			continue
		}
		if ch == '}' {
			if depth > 0 {
				depth--
			}
			if depth == 0 && start != -1 {
				parts = append(parts, strings.TrimSpace(s[start:i+1]))
				start = -1
			}
			continue
		}
	}
	return parts
}
