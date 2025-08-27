package llm

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/tools"
	"github.com/alantheprice/ledit/pkg/types"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
)

// ContextHandler is a function type that defines how context requests are handled.
// It takes a slice of ContextRequest and returns a string response and an error.
type ContextHandler func([]ContextRequest, *config.Config) (string, error)

// Global context handler for tool execution
var globalContextHandler ContextHandler

// SetGlobalContextHandler sets the global context handler for tool execution
func SetGlobalContextHandler(handler ContextHandler) {
	globalContextHandler = handler
}

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
	logger := utils.GetLogger(cfg.SkipPrompt)

	// Create file detector for automatic file detection
	detector := NewFileDetector()

	// Analyze the user's message for mentioned files
	var userPrompt string
	for _, msg := range initialMessages {
		if msg.Role == "user" {
			userPrompt += fmt.Sprintf("%v ", msg.Content)
		}
	}

	// Debug: Log function entry
	logger.Logf("DEBUG: CallLLMWithInteractiveContext called with model: %s", modelName)
	logger.Logf("DEBUG: User prompt: %s", userPrompt)
	logger.Logf("DEBUG: Initial messages count: %d", len(initialMessages))

	// Log initial messages
	for i, msg := range initialMessages {
		logger.Logf("DEBUG: Initial message %d: role=%s, content_type=%T", i, msg.Role, msg.Content)
	}

	mentionedFiles := detector.DetectMentionedFiles(userPrompt)

	// Enhance the system prompt with tool information
	var enhancedMessages []prompts.Message
	logger.Log("DEBUG: Starting message enhancement process")

	// Add tool information to the system message if it exists
	for i, msg := range initialMessages {
		if i == 0 && msg.Role == "system" {
			logger.Log("DEBUG: Enhancing system message with tools")
			originalContent := msg.Content
			toolInfo := FormatToolsForPrompt()
			enhancedContent := fmt.Sprintf("%s\n\n%s", originalContent, toolInfo)

			logger.Logf("DEBUG: Original system content length: %d", len(fmt.Sprintf("%v", originalContent)))
			logger.Logf("DEBUG: Tool info length: %d", len(toolInfo))
			logger.Logf("DEBUG: Enhanced content length: %d", len(enhancedContent))

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

	logger.Logf("DEBUG: Final enhanced messages count: %d", len(enhancedMessages))
	for i, msg := range enhancedMessages {
		logger.Logf("DEBUG: Enhanced message %d: role=%s, content_length=%d", i, msg.Role, len(fmt.Sprintf("%v", msg.Content)))
		// Check for detokenize in message content
		contentStr := fmt.Sprintf("%v", msg.Content)
		if strings.Contains(contentStr, "detokenize") {
			ui.Out().Printf("ERROR: Found 'detokenize' in enhanced message %d content!\n", i)
		}
	}

	// Limit the number of interactive turns. Prefer configured attempts when provided
	maxRetries := cfg.OrchestrationMaxAttempts
	if maxRetries <= 0 {
		maxRetries = 8
	}

	// Anti-loop and cap enforcement state
	shellCalls := 0
	totalToolCalls := 0
	// Set tool limit to 1 less than orchestration max attempts
	maxToolCalls := cfg.OrchestrationMaxAttempts - 1
	if maxToolCalls <= 0 {
		maxToolCalls = 7 // Default to 7 if config is not set or invalid
	}
	logger.Logf("DEBUG: Tool call limit set to %d (OrchestrationMaxAttempts: %d)", maxToolCalls, cfg.OrchestrationMaxAttempts)
	executedShell := map[string]bool{}
	noProgressStreak := 0
	// Additional guardrails for speed
	maxReadFileCalls := 12

	// Observability and caching
	toolCounts := map[string]int{}
	blockedCounts := map[string]int{}
	cacheHits := 0
	readFileCache := map[string]string{}
	persisted := LoadEvidenceCache()
	var turnDurations []time.Duration

	// Session tracking for duplicate request detection
	sessionTracker := tools.GetGlobalSessionTracker()
	sessionID := sessionTracker.StartSession()
	logger.Logf("DEBUG: Started session tracking with ID: %s", sessionID)

	// Clean up session when function exits
	defer func() {
		sessionTracker.EndSession(sessionID)
		logger.Logf("DEBUG: Ended session tracking for ID: %s", sessionID)
	}()

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

	// Artifact enforcement state
	expectPlanNext := false
	noPlanTurns := 0

	// Cache a plan JSON if model returns edits alongside tool_calls
	cachedPlanJSON := ""
	planJSONDetected := ""
	for i := 0; i < maxRetries; i++ {
		turnStart := time.Now()
		ui.Out().Printf("[tools] turn %d/%d\n", i+1, maxRetries)
		// Phase guidance: restrict allowed tools by phase to reduce loops
		phase := "plan"
		if plannedAction != "" {
			phase = "execute"
		}
		if phase == "plan" {
			currentMessages = append(currentMessages, prompts.Message{Role: "system", Content: "Phase=PLAN. Allowed tools: plan_step, read_file, run_shell_command. Do not call edit/validate/shell tools. Produce a plan next."})
		} else {
			currentMessages = append(currentMessages, prompts.Message{Role: "system", Content: "Phase=EXECUTE. Allowed tools: execute_step (with previously planned action), edit_file_section, validate_file, read_file, evaluate_outcome."})
		}
		// If we are expecting a plan now, push a strong system requirement
		if expectPlanNext {
			currentMessages = append(currentMessages, prompts.Message{Role: "system", Content: "You must now return ONLY the final JSON plan: {\"edits\":[{\"file\":...,\"instructions\":...}]}.\n\nDO NOT use tool_calls for this response. Provide only the JSON plan or a normal text response."})
		}

		// Call the main LLM response function (with simple backoff on transient/provider errors)
		var response string
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			// Use unified dispatcher policy; do not restrict here
			var allowed []string = nil
			logger.Logf("DEBUG: About to call GetLLMResponseWithToolsScoped with model: %s", modelName)
			logger.Logf("DEBUG: Current messages count: %d", len(currentMessages))
			logger.Logf("DEBUG: Allowed tools: %v", allowed)

			logger.Logf("DEBUG: First message role: %s", currentMessages[0].Role)
			logger.Logf("DEBUG: First message content preview: %s", fmt.Sprintf("%v", currentMessages[0].Content)[:100])

			response, _, err = GetLLMResponseWithToolsScoped(modelName, currentMessages, "", cfg, timeout, allowed)
			if err == nil {
				break
			}
			logger.Logf("DEBUG: Turn %d LLM call failed with error: %v", i+1, err)

			// Check if this is the detokenize error
			if strings.Contains(err.Error(), "detokenize") {
				ui.Out().Printf("ERROR: Caught detokenize error on turn %d! This is the source of the issue.\n", i+1)
				// Return the error immediately to surface the detokenize issue
				return "", fmt.Errorf("DeepInfra detokenize validation error: %w", err)
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
			// Debug: Log the actual error
			logger.Logf("DEBUG: Final error from interactive loop: %v", err)
			// Check if this was a detokenize error and surface it specifically
			if strings.Contains(err.Error(), "detokenize") {
				ui.Out().Printf("ERROR: Found detokenize error in final error handling\n")
				return "", fmt.Errorf("DeepInfra detokenize validation error: %w", err)
			}
			return "", fmt.Errorf("LLM call failed: %w", err)
		}
		ui.Out().Print("[tools] model returned a response\n")
		previewLen := 100
		if len(response) < previewLen {
			previewLen = len(response)
		}
		logger.Logf("DEBUG: Response preview: %s", response[:previewLen])

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
			// Parse tool calls first to count them
			toolCalls, err := parseToolCalls(response)
			if err != nil {
				logger.Logf("DEBUG: Failed to parse tool calls: %v", err)
				continue
			}
			totalToolCalls += len(toolCalls)

			// Check if we've exceeded the maximum tool calls limit
			if totalToolCalls > maxToolCalls {
				logger.Logf("DEBUG: Maximum tool calls (%d) exceeded, forcing final response", maxToolCalls)
				// Force a final response by adding a system message that disables tools
				currentMessages = append(currentMessages, prompts.Message{
					Role:    "system",
					Content: "Maximum tool calls reached. You must now provide your final response without using any more tools. Respond with your analysis and recommendations.",
				})
				// Clear allowed tools to prevent further tool calls
				// Note: allowed variable is defined inside the loop, so we need to modify it there
				logger.Log("DEBUG: Tool limit exceeded, tools will be disabled on next iteration")
				// Skip tool execution and go directly to next LLM call
				continue
			}

			// If the response also includes an edits JSON, cache it for potential fallback
			for _, obj := range utils.SplitTopLevelJSONObjects(response) {
				if strings.Contains(obj, "\"edits\"") {
					// validate it is JSON
					var probe map[string]any
					if json.Unmarshal([]byte(obj), &probe) == nil {
						cachedPlanJSON = obj
						planJSONDetected = obj
						break
					}
				}
			}
			// After executing tool calls we will require a plan on the next turn
			expectPlanNext = true
			// Parse and execute tool calls
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
				// Instead of aborting, instruct the model to emit a clean tool_calls JSON and retry this turn
				guidance := "Your previous response attempted tool calls but could not be parsed. Output ONLY a valid JSON object in this exact format: {\"tool_calls\":[{\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":{\"file_path\":\"path/to/file\"}}}]} — do not include fences or extra text."
				currentMessages = append(currentMessages, prompts.Message{Role: "system", Content: guidance})
				turnDurations = append(turnDurations, time.Since(turnStart))
				continue
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
						ctx := context.WithValue(context.Background(), "session_id", sessionID)
						result, err := ExecuteBasicToolCallWithContext(ctx, merged, cfg)
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
							if ua == "edit_file_section" || ua == "validate_file" {
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
						// Allow read/discovery tools always; for exec/write tools, prompt the user when not in allow list
						blockedUnderlying := map[string]bool{
							"edit_file_section": true, "validate_file": true,
						}
						// Special handling for run_shell_command: allow safe diagnostics or ask user approval
						if name == "run_shell_command" {
							cmdStr, _ := args["command"].(string)
							trimmed := strings.TrimSpace(cmdStr)
							lower := strings.ToLower(trimmed)
							if trimmed == "" || strings.Contains(lower, "rm -rf") || strings.Contains(lower, "mkfs") || strings.Contains(lower, " :(){ :|:& };:") || strings.Contains(lower, "shutdown") || strings.Contains(lower, "reboot") || strings.Contains(lower, "sudo ") {
								toolResults = append(toolResults, "Tool run_shell_command blocked: unsafe or empty command")
								blockedCounts["shell_unsafe"]++
								continue
							}
							allowPrefixes := []string{"ls", "grep", "cat", "wc", "head", "tail"}
							allowed := false
							for _, p := range allowPrefixes {
								if strings.HasPrefix(lower, p+" ") || lower == p {
									allowed = true
									break
								}
							}
							if !allowed {
								if cfg.SkipPrompt {
									toolResults = append(toolResults, "Tool run_shell_command blocked: not in allow list and --skip-prompt is set")
									blockedCounts["shell_not_allowed_skip"]++
									continue
								}
								ui.Out().Print(fmt.Sprintf("[tools] run_shell_command not in allow list. Approve to run? (y/n): %s\n", trimmed))
								var resp string
								fmt.Scanln(&resp)
								resp = strings.ToLower(strings.TrimSpace(resp))
								if resp != "y" && resp != "yes" {
									toolResults = append(toolResults, "Tool run_shell_command blocked by user")
									blockedCounts["shell_user_block"]++
									continue
								}
							}
							// Allowed or approved: fall through to execution
						}
						if blockedUnderlying[name] {
							toolResults = append(toolResults, "Tool blocked: use plan_step → execute_step → evaluate_outcome. Do not call underlying tools directly.")
							blockedCounts["direct_tool_blocked"]++
							currentMessages = append(currentMessages, prompts.Message{Role: "system", Content: "You attempted a write/exec tool in PLAN phase. Call plan_step to produce a plan, then execute_step with that action."})
							continue
						}
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
					ctx := context.WithValue(context.Background(), "session_id", sessionID)
					result, err := ExecuteBasicToolCallWithContext(ctx, toolCall, cfg)
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
						// Populate persistent caches for shell commands
						if name == "run_shell_command" {
							cmdStr, _ := args["command"].(string)
							trimmed := strings.TrimSpace(cmdStr)
							if trimmed != "" {
								persisted.Put(EvidenceEntry{Tool: "run_shell_command", Key: trimmed, Value: result, Updated: NowUnix()})
								_ = persisted.Save()
							}
						}

					}

					if name == "edit_file_section" || name == "validate_file" {
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
				if shellCapTripped {
					currentMessages = append(currentMessages, prompts.Message{
						Role:    "system",
						Content: "Operational caps reached. Stop exploring. Choose a specific file, use read_file, apply edit_file_section, then validate_file.",
					})
				}

				// If very first turn has no file reads, synthesize a single read on README.md to provide evidence
				if i == 0 && !editedOrValidated && toolCounts["read_file"] == 0 {
					// Try README.md first
					candidate := "README.md"
					if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
						rfArgs := map[string]any{"file_path": candidate}
						rfBytes, _ := json.Marshal(rfArgs)
						rfCall := ToolCall{Type: "function", Function: ToolCallFunction{Name: "read_file", Arguments: string(rfBytes)}}
						ctx := context.WithValue(context.Background(), "session_id", sessionID)
						if rfRes, rfErr := ExecuteBasicToolCallWithContext(ctx, rfCall, cfg); rfErr == nil {
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
							Content: "Stop searching. Choose the top relevant file (e.g., README.md or the last search result), use read_file now, then produce a minimal JSON plan of edits. Use run_shell_command (find, grep, ls) to explore the codebase if needed.",
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

		// If no tool_calls: check whether a plan JSON was produced
		if !containsToolCall(response) {
			editsFound := false
			for _, obj := range utils.SplitTopLevelJSONObjects(response) {
				if strings.Contains(obj, "\"edits\"") {
					var probe map[string]any
					if json.Unmarshal([]byte(obj), &probe) == nil {
						editsFound = true
						planJSONDetected = obj
						break
					}
				}
			}
			if expectPlanNext && !editsFound {
				noPlanTurns++
				// Nudge immediately on the first miss
				currentMessages = append(currentMessages, prompts.Message{Role: "system", Content: "Stop searching. Read the top relevant file if needed, then return ONLY the final JSON plan now."})
				if noPlanTurns >= 2 {
					// Synthesize a minimal plan from available evidence and return it
					// Choose a candidate file: prefer README.md, otherwise any cached read_file key
					candidate := "README.md"
					if fi, err := os.Stat(candidate); err != nil || fi.IsDir() {
						for k := range readFileCache {
							candidate = k
							break
						}
					}
					plan := map[string]any{"edits": []map[string]string{{"file": candidate, "instructions": "Verify and update outdated documentation to match current code behavior (minimal precise changes)."}}}
					b, _ := json.Marshal(plan)
					printSummary()
					return string(b), nil
				}
				// Continue loop to give model one more chance
				turnDurations = append(turnDurations, time.Since(turnStart))
				continue
			}
			// Plan found or not expected
			if editsFound {
				if rl := utils.GetRunLogger(); rl != nil {
					preview := planJSONDetected
					if len(preview) > 2000 {
						preview = preview[:2000] + "..."
					}
					rl.LogEvent("plan_json_detected", map[string]any{"turn": i + 1, "plan": preview})
				}
				expectPlanNext = false
				noPlanTurns = 0
			}
		}

		// No tool_calls and no actionable context requests: instruct model to emit plan/tool_calls and try again, including guidance to discover files
		currentMessages = append(currentMessages, prompts.Message{Role: "system", Content: "Your previous response did not contain valid tool_calls. You have two options:\n\n1. If you need more information: Output ONLY a JSON tool_calls object\n2. If you have enough information to complete the task: Provide your final response as normal text\n\nChoose ONE approach - either pure tool_calls JSON or pure text response. Do not mix them."})
		turnDurations = append(turnDurations, time.Since(turnStart))
		continue
	}

	printSummary()
	if strings.TrimSpace(cachedPlanJSON) != "" {
		if rl := utils.GetRunLogger(); rl != nil {
			preview := cachedPlanJSON
			if len(preview) > 2000 {
				preview = preview[:2000] + "..."
			}
			rl.LogEvent("plan_json_fallback", map[string]any{"plan": preview})
		}
		return cachedPlanJSON, nil
	}
	return "", fmt.Errorf("max interactive LLM retries reached (%d)", maxRetries)
}

// Helper functions for tool calling
func containsToolCall(response string) bool {
	// Loosened detection: accept tool_calls anywhere in the response.
	// Providers may prepend prose or use unlabeled fences.
	trimmed := strings.TrimSpace(response)

	// Fast-path: anywhere in the response
	if strings.Contains(response, `"tool_calls"`) {
		return true
	}

	// Starts with JSON containing tool_calls
	if strings.HasPrefix(trimmed, "{") && strings.Contains(response, `"tool_calls"`) {
		return true
	}

	// JSON code block
	if strings.Contains(response, "```json") {
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

	// Generic fenced block without language tag
	if strings.Contains(response, "```") {
		start := strings.Index(response, "```")
		if start >= 0 {
			start += 3
			end := strings.Index(response[start:], "```")
			if end > 0 {
				block := response[start : start+end]
				if strings.Contains(block, `"tool_calls"`) {
					return true
				}
			}
		}
	}

	// Debug logging for troubleshooting
	if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
		fmt.Printf("DEBUG: containsToolCall check failed for response: %s\n", response)
		fmt.Printf("DEBUG: trimmed: %s\n", trimmed)
		fmt.Printf("DEBUG: starts with {: %v\n", strings.HasPrefix(trimmed, "{"))
		fmt.Printf("DEBUG: contains tool_calls: %v\n", strings.Contains(response, `"tool_calls"`))
	}

	return false
}

func parseToolCalls(response string) ([]ToolCall, error) {
	// Tolerant parse: tool_calls may have arguments as string or object, may use 'parameters',
	// or may place {name, arguments} at the call level under 'arguments'.

	// Debug logging
	if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
		fmt.Printf("DEBUG: parseToolCalls called with response: %s\n", response)
	}

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
			// Some providers double-encode arguments as a JSON string.
			// Try to unescape and normalize to a canonical JSON object string.
			candidate := strings.TrimSpace(a)
			var tmp map[string]any
			if json.Unmarshal([]byte(candidate), &tmp) == nil {
				if b, err := json.Marshal(tmp); err == nil {
					argsStr = string(b)
				} else {
					argsStr = candidate
				}
			} else {
				argsStr = candidate
			}
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

	fixMalformedJSON := func(s string) string {
		// Handle common JSON formatting issues from LLMs

		// First, let's try a more sophisticated approach using a stack-based parser
		// to identify and fix structural issues

		// Count braces and brackets to understand the overall structure
		braceCount := strings.Count(s, "{")
		closeBraceCount := strings.Count(s, "}")
		bracketCount := strings.Count(s, "[")
		closeBracketCount := strings.Count(s, "]")

		// Simple fix: if we have missing braces/brackets, add them at the end
		if braceCount > closeBraceCount {
			missingBraces := braceCount - closeBraceCount
			for i := 0; i < missingBraces; i++ {
				s += "}"
			}
		}

		if bracketCount > closeBracketCount {
			missingBrackets := bracketCount - closeBracketCount
			for i := 0; i < missingBrackets; i++ {
				s += "]"
			}
		}

		// Handle the most common LLM JSON issues:

		// 1. Fix trailing commas before closing brackets/braces
		s = strings.ReplaceAll(s, ",}", "}")
		s = strings.ReplaceAll(s, ",]", "]")

		// 2. Fix missing commas between array elements
		// Look for patterns where objects/arrays are not properly separated
		s = strings.ReplaceAll(s, "}{", "},{")
		s = strings.ReplaceAll(s, "} {", "}, {")
		s = strings.ReplaceAll(s, "][", "],[")
		s = strings.ReplaceAll(s, "] [", "], [")

		// 3. Fix string concatenation issues
		s = strings.ReplaceAll(s, "\" \"", "")
		s = strings.ReplaceAll(s, "' '", "")

		// 4. Fix common LLM mistakes with quotes (but preserve valid escaping)
		s = strings.ReplaceAll(s, "\"{", "{")
		s = strings.ReplaceAll(s, "}\"", "}")
		s = strings.ReplaceAll(s, "\"[", "[")
		s = strings.ReplaceAll(s, "]\"", "]")

		// 5. Handle the specific issue where array elements are missing separators
		// Look for patterns where array elements are not properly separated
		if strings.Contains(s, "nil\"]") {
			s = strings.ReplaceAll(s, "nil\"]", "nil\",]")
		}
		if strings.Contains(s, "true\"]") {
			s = strings.ReplaceAll(s, "true\"]", "true\",]")
		}
		if strings.Contains(s, "false\"]") {
			s = strings.ReplaceAll(s, "false\"]", "false\",]")
		}

		return s
	}

	tryParse := func(s string) ([]ToolCall, bool) {
		// Clean up common JSON formatting issues
		cleaned := strings.TrimSpace(s)

		// Use a more sophisticated approach to fix JSON structure
		cleaned = fixMalformedJSON(cleaned)

		if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
			fmt.Printf("DEBUG: Attempting to parse cleaned JSON: %s\n", cleaned)
		}

		// Try the original cleaned JSON first
		var raw map[string]any
		if err := json.Unmarshal([]byte(cleaned), &raw); err == nil {
			if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
				fmt.Printf("DEBUG: JSON parsed successfully: %+v\n", raw)
			}
			if tcs, ok := raw["tool_calls"].([]any); ok && len(tcs) > 0 {
				if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
					fmt.Printf("DEBUG: Found tool_calls array with %d items\n", len(tcs))
				}
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
			} else if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
				fmt.Printf("DEBUG: tool_calls not found or empty in parsed JSON\n")
			}
		} else if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
			fmt.Printf("DEBUG: JSON parsing failed: %v\n", err)
			fmt.Printf("DEBUG: Attempted to parse: %s\n", cleaned)
		}

		// If the first attempt failed, try an incremental approach
		// This handles cases where we need to add more closing braces/brackets
		if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
			fmt.Printf("DEBUG: Trying incremental JSON fixing\n")
		}

		testJson := cleaned
		for i := 0; i < 5; i++ { // Try adding up to 5 extra braces/brackets
			var testRaw map[string]any
			if err := json.Unmarshal([]byte(testJson), &testRaw); err == nil {
				if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
					fmt.Printf("DEBUG: JSON parsed successfully after adding %d braces/brackets\n", i)
				}
				if tcs, ok := testRaw["tool_calls"].([]any); ok && len(tcs) > 0 {
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
				break // Success, stop trying
			} else {
				// Add one more closing brace/bracket
				testJson += "}"
				if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" && i < 2 {
					fmt.Printf("DEBUG: Attempt %d failed, adding another brace: %v\n", i+1, err)
				}
			}
		}

		return nil, false
	}

	// First, attempt to parse any fenced blocks (```json or generic ```)
	// Models often wrap the JSON in fences; extract and try those first.
	if strings.Contains(response, "```") {
		if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
			fmt.Printf("DEBUG: parseToolCalls - scanning fenced blocks for tool_calls JSON\n")
		}
		idx := 0
		for idx < len(response) {
			startFence := strings.Index(response[idx:], "```")
			if startFence == -1 {
				break
			}
			startFence += idx + 3
			endFence := strings.Index(response[startFence:], "```")
			if endFence == -1 {
				break
			}
			block := response[startFence : startFence+endFence]
			// Handle a leading language tag (e.g., "json\n")
			blockTrim := strings.TrimLeft(block, "\n\r\t ")
			if strings.HasPrefix(blockTrim, "json") {
				blockTrim = strings.TrimLeft(blockTrim[len("json"):], "\n\r\t ")
			}
			if strings.Contains(blockTrim, `"tool_calls"`) {
				if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
					fmt.Printf("DEBUG: parseToolCalls - found fenced block with tool_calls, attempting parse\n")
				}
				if tcs, ok := tryParse(blockTrim); ok {
					return tcs, nil
				}
			}
			idx = startFence + endFence + 3
		}
	}

	if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
		fmt.Printf("DEBUG: parseToolCalls - trying tryParse\n")
	}

	if tcs, ok := tryParse(response); ok {
		if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
			fmt.Printf("DEBUG: parseToolCalls - tryParse succeeded with %d tool calls\n", len(tcs))
		}
		return tcs, nil
	} else {
		if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
			fmt.Printf("DEBUG: parseToolCalls - tryParse failed, trying fallbacks\n")
		}
	}

	// Fallback: split multiple concatenated top-level JSON objects and try each
	if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
		fmt.Printf("DEBUG: parseToolCalls - trying SplitTopLevelJSONObjects fallback\n")
	}
	for _, obj := range utils.SplitTopLevelJSONObjects(response) {
		if tcs, ok := tryParse(obj); ok {
			return tcs, nil
		}
	}

	// Last-resort fallback: extract the tool_calls array manually and wrap it
	if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
		fmt.Printf("DEBUG: parseToolCalls - trying manual tool_calls extraction\n")
	}
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
	if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
		fmt.Printf("DEBUG: parseToolCalls - trying ToolMessage parsing\n")
	}
	var toolMessage ToolMessage
	if err := json.Unmarshal([]byte(response), &toolMessage); err == nil && len(toolMessage.ToolCalls) > 0 {
		return toolMessage.ToolCalls, nil
	}

	// Last resort: try splitting concatenated top-level objects
	if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
		fmt.Printf("DEBUG: parseToolCalls - trying concatenated objects\n")
	}
	for _, obj := range utils.SplitTopLevelJSONObjects(response) {
		var tm ToolMessage
		if err := json.Unmarshal([]byte(obj), &tm); err == nil && len(tm.ToolCalls) > 0 {
			return tm.ToolCalls, nil
		}
	}

	// Final fallback: try to extract tool calls using a very robust method
	// This handles cases where the JSON structure is severely broken but still contains recognizable tool call patterns
	if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
		fmt.Printf("DEBUG: parseToolCalls - trying robust extraction\n")
	}
	toolCalls := extractToolCallsRobust(response)
	if len(toolCalls) > 0 {
		if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
			fmt.Printf("DEBUG: parseToolCalls - robust extraction found %d tool calls\n", len(toolCalls))
		}
		return toolCalls, nil
	}

	return []ToolCall{}, fmt.Errorf("no tool calls found in response")
}

// extractToolCallsRobust provides a very robust fallback for extracting tool calls
// from malformed JSON responses. This handles cases where the JSON structure is
// severely broken but still contains recognizable tool call patterns.
func extractToolCallsRobust(response string) []ToolCall {
	var toolCalls []ToolCall

	if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
		fmt.Printf("DEBUG: extractToolCallsRobust called with response length: %d\n", len(response))
	}

	// Look for the tool_calls array pattern
	toolCallsIdx := strings.Index(response, `"tool_calls"`)
	if toolCallsIdx == -1 {
		if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
			fmt.Printf("DEBUG: extractToolCallsRobust - no tool_calls found\n")
		}
		return nil
	}

	if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
		fmt.Printf("DEBUG: extractToolCallsRobust - found tool_calls at index %d\n", toolCallsIdx)
	}

	// Find the array start
	arrayStartIdx := strings.Index(response[toolCallsIdx:], "[")
	if arrayStartIdx == -1 {
		if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
			fmt.Printf("DEBUG: extractToolCallsRobust - no array start found\n")
		}
		return nil
	}
	arrayStartIdx += toolCallsIdx

	if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
		fmt.Printf("DEBUG: extractToolCallsRobust - array starts at index %d\n", arrayStartIdx)
	}

	// Find the array end by looking for the matching closing bracket
	arrayEndIdx := -1
	bracketLevel := 0

	for i := arrayStartIdx; i < len(response); i++ {
		switch response[i] {
		case '[':
			bracketLevel++
		case ']':
			bracketLevel--
			if bracketLevel == 0 {
				arrayEndIdx = i
				break
			}
		}
	}

	if arrayEndIdx == -1 {
		if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
			fmt.Printf("DEBUG: extractToolCallsRobust - no matching array end found\n")
		}
		return nil
	}

	// Extract the array content without outer brackets
	arrayContent := response[arrayStartIdx+1 : arrayEndIdx]

	if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
		fmt.Printf("DEBUG: extractToolCallsRobust - extracted array content: %s\n", arrayContent[:min(100, len(arrayContent))])
	}

	if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
		fmt.Printf("DEBUG: extractToolCallsRobust - array content length: %d\n", len(arrayContent))
		fmt.Printf("DEBUG: extractToolCallsRobust - array content: %s\n", arrayContent[:min(100, len(arrayContent))])
	}

	// Try a different approach: look for complete tool call patterns
	// Since the JSON is malformed, we'll look for the specific pattern:
	// {"id": "...", "type": "...", "function": {"name": "...", "arguments": ...}}

	// Find the first tool call by looking for the id field
	if idIdx := strings.Index(arrayContent, `"id"`); idIdx != -1 {
		// Look for the start of this object (should be at the beginning)
		objStart := 0
		objEnd := len(arrayContent)

		// Try to find a reasonable end point by looking for patterns that indicate the end
		// Look for the closing brace that would match the opening one
		braceLevel := 0
		for i := objStart; i < len(arrayContent) && i < 2000; i++ { // Limit search to avoid infinite loop
			switch arrayContent[i] {
			case '{':
				braceLevel++
			case '}':
				braceLevel--
				if braceLevel == 0 {
					objEnd = i + 1
					break
				}
			}
		}

		if objEnd > objStart {
			objStr := arrayContent[objStart:objEnd]
			if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
				fmt.Printf("DEBUG: extractToolCallsRobust - trying to parse object: %s\n", objStr[:min(200, len(objStr))])
			}

			// Try to parse this as a tool call
			if tc := parseSingleToolCall(objStr); tc != nil {
				toolCalls = append(toolCalls, *tc)
				if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
					fmt.Printf("DEBUG: extractToolCallsRobust - successfully parsed tool call\n")
				}
			} else {
				if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
					fmt.Printf("DEBUG: extractToolCallsRobust - failed to parse tool call\n")
				}
			}
		}
	}

	if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
		fmt.Printf("DEBUG: extractToolCallsRobust - parsed %d tool calls\n", len(toolCalls))
	}

	return toolCalls
}

// parseSingleToolCall attempts to parse a single tool call from a string
func parseSingleToolCall(objStr string) *ToolCall {
	if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
		fmt.Printf("DEBUG: parseSingleToolCall called with: %s\n", objStr[:min(100, len(objStr))])
	}

	// Try to extract key components using string parsing
	id := extractStringValue(objStr, `"id"`, `"`)
	typ := extractStringValue(objStr, `"type"`, `"`)
	name := extractStringValue(objStr, `"name"`, `"`)

	if os.Getenv("LEDIT_DEBUG_TOOL_CALLS") == "1" {
		fmt.Printf("DEBUG: parseSingleToolCall - extracted id='%s', type='%s', name='%s'\n", id, typ, name)
	}

	// For arguments, it might be an object or a string
	var args string
	if argsStart := strings.Index(objStr, `"arguments"`); argsStart != -1 {
		argsStart += 12 // length of "arguments":

		// Find the arguments value (could be object or string)
		if objStr[argsStart] == '{' {
			// Object - find matching closing brace
			braceLevel := 1
			for i := argsStart + 1; i < len(objStr); i++ {
				switch objStr[i] {
				case '{':
					braceLevel++
				case '}':
					braceLevel--
					if braceLevel == 0 {
						args = objStr[argsStart : i+1]
						break
					}
				}
			}
		} else if objStr[argsStart] == '"' {
			// String - find closing quote
			for i := argsStart + 1; i < len(objStr); i++ {
				if objStr[i] == '"' && objStr[i-1] != '\\' {
					args = objStr[argsStart : i+1]
					break
				}
			}
		}
	}

	if id != "" && typ != "" && name != "" {
		return &ToolCall{
			ID:   id,
			Type: typ,
			Function: ToolCallFunction{
				Name:      name,
				Arguments: args,
			},
		}
	}

	return nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// extractStringValue extracts a string value from JSON-like text
func extractStringValue(text, key, quote string) string {
	keyIdx := strings.Index(text, key)
	if keyIdx == -1 {
		return ""
	}

	// Find the colon after the key
	colonIdx := strings.Index(text[keyIdx:], ":")
	if colonIdx == -1 {
		return ""
	}
	colonIdx += keyIdx

	// Skip whitespace after colon
	valueStart := colonIdx + 1
	for valueStart < len(text) && (text[valueStart] == ' ' || text[valueStart] == '\t' || text[valueStart] == '\n') {
		valueStart++
	}

	if valueStart >= len(text) || text[valueStart] != quote[0] {
		return ""
	}

	valueStart++ // Skip the opening quote
	valueEnd := valueStart

	for i := valueStart; i < len(text); i++ {
		if text[i] == '"' && (i == 0 || text[i-1] != '\\') {
			valueEnd = i
			break
		}
	}

	if valueEnd > valueStart {
		return text[valueStart:valueEnd]
	}

	return ""
}

func ExecuteBasicToolCall(toolCall ToolCall, cfg *config.Config) (string, error) {
	return ExecuteBasicToolCallWithContext(context.Background(), toolCall, cfg)
}

func ExecuteBasicToolCallWithContext(ctx context.Context, toolCall ToolCall, cfg *config.Config) (string, error) {
	// Convert to types.ToolCall for the unified executor
	typesToolCall := convertToTypesToolCall(toolCall)

	// Use the new unified tool executor
	result, err := tools.ExecuteToolCall(ctx, typesToolCall)
	if err != nil {
		return "", err
	}

	if !result.Success {
		return "", fmt.Errorf("tool execution failed: %v", strings.Join(result.Errors, "; "))
	}

	// Convert result to string format expected by existing code
	if output, ok := result.Output.(string); ok {
		return output, nil
	}

	// Fallback for non-string outputs
	return fmt.Sprintf("%v", result.Output), nil
}

// convertToTypesToolCall converts LLM ToolCall to types.ToolCall
func convertToTypesToolCall(toolCall ToolCall) types.ToolCall {
	return types.ToolCall{
		ID:   toolCall.ID,
		Type: toolCall.Type,
		Function: types.ToolCallFunction{
			Name:      toolCall.Function.Name,
			Arguments: toolCall.Function.Arguments,
		},
	}
}
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
