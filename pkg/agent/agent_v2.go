package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
)

// RunAgentModeV2 is a tool-driven agent that stays in-flow using tool calls until done
func RunAgentModeV2(userIntent string, skipPrompt bool, model string) error {
	fmt.Printf("🤖 Agent v2 mode: Tool-driven execution\n")

	utils.LogUserPrompt(userIntent)

	cfg, err := config.LoadOrInitConfig(skipPrompt)
	if err != nil {
		logger := utils.GetLogger(false)
		logger.LogError(fmt.Errorf("failed to load config: %w", err))
		return fmt.Errorf("failed to load config: %w", err)
	}
	if model != "" {
		cfg.OrchestrationModel = model
	}
	// Force non-interactive for v2 to prevent hidden prompts
	cfg.SkipPrompt = true
	_ = llm.InitPricingTable()

	fmt.Printf("🎯 Intent: %s\n", userIntent)
	logger := utils.GetLogger(cfg.SkipPrompt)

	overallStart := time.Now()
	tokenUsage, err := ExecuteV2(userIntent, cfg, logger)
	if err != nil {
		return err
	}
	overallDuration := time.Since(overallStart)
	PrintTokenUsageSummary(tokenUsage, overallDuration, cfg)
	fmt.Printf("✅ Agent v2 execution completed\n")
	return nil
}

// ExecuteV2 runs a single chat with tool-calls to drive the task to completion
func ExecuteV2(userIntent string, cfg *config.Config, logger *utils.Logger) (*AgentTokenUsage, error) {
	logger.LogProcessStep("🚀 Starting agent v2 tool-driven execution...")

	context := &AgentContext{
		UserIntent:         userIntent,
		ExecutedOperations: []string{},
		Errors:             []string{},
		ValidationResults:  []string{},
		IterationCount:     0,
		MaxIterations:      15, // tighter cap on tool cycles
		StartTime:          time.Now(),
		TokenUsage:         &AgentTokenUsage{},
		Config:             cfg,
		Logger:             logger,
	}

	policy := buildAgentV2Policy()
	messages := []prompts.Message{
		{Role: "system", Content: policy},
		{Role: "user", Content: fmt.Sprintf("Goal: %s\nYou have tools. Use them as needed and stop when validated. Prioritize: (1) read_file of the most relevant file, (2) micro_edit if change is tiny else edit_file_section, (3) validate.", userIntent)},
	}

	// Prefer a smaller/cheaper control model if available
	model := cfg.SummaryModel
	if strings.TrimSpace(model) == "" {
		model = cfg.OrchestrationModel
	}
	if strings.TrimSpace(model) == "" {
		model = cfg.EditingModel
	}
	logger.LogProcessStep(fmt.Sprintf("v2: using orchestration model: %s", model))

	editedSomething := false
	validated := false
	noopShellCount := 0
	totalShellCalls := 0
	executedShell := make(map[string]bool)
	// readFilesRead reserved for future caching of file content; removing unused var for now
	// Debug counters
	totalTurns := 0
	totalTools := 0
	blockedDupShell := 0
	blockedNoopShell := 0
	blockedWSCap := 0
	blockedWSDup := 0
	invalidToolCalls := 0
	workspaceContextCalls := 0
	workspaceRequests := make(map[string]bool)
	noProgressStreak := 0
	const perTurnTimeout = 60 * time.Second
	const overallTimeout = 5 * time.Minute
	overallStart := time.Now()
	for context.IterationCount = 0; context.IterationCount < context.MaxIterations; context.IterationCount++ {
		totalTurns++
		logger.LogProcessStep(fmt.Sprintf("v2: turn %d/%d - calling LLM", context.IterationCount+1, context.MaxIterations))
		if time.Since(overallStart) > overallTimeout {
			logger.LogProcessStep("v2: overall timeout reached; stopping")
			break
		}
		// Call model
		response, usage, err := llm.GetLLMResponse(model, messages, "", cfg, perTurnTimeout)
		if err != nil {
			return context.TokenUsage, fmt.Errorf("agent v2 LLM call failed: %w", err)
		}
		logger.LogProcessStep("v2: LLM call completed")
		// Token accounting (approx)
		if usage != nil {
			context.TokenUsage.ProgressEvaluation += usage.TotalTokens
			context.TokenUsage.ProgressSplit.Prompt += usage.PromptTokens
			context.TokenUsage.ProgressSplit.Completion += usage.CompletionTokens
			logger.Logf("v2: DEBUG turn tokens: prompt=%d completion=%d total=%d", usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
		}

		// Parse tool calls
		toolCalls, _ := llm.ParseToolCalls(response)
		// Debug: show parsed tool names (truncated)
		if len(toolCalls) > 0 {
			var names []string
			for _, t := range toolCalls {
				names = append(names, strings.TrimSpace(t.Function.Name))
			}
			logger.Logf("v2: DEBUG parsed tool_calls: %v", names)
		} else {
			// Help debugging non-compliant responses without flooding logs
			hasMarker := strings.Contains(response, "tool_calls")
			snippet := response
			if len(snippet) > 240 {
				snippet = snippet[:240] + "..."
			}
			logger.Logf("v2: DEBUG no tool_calls parsed (contains marker=%t). Snippet: %s", hasMarker, snippet)
		}
		if len(toolCalls) == 0 {
			// Deterministic bootstrap on first turn
			if context.IterationCount == 0 {
				logger.LogProcessStep("v2: no tool calls; bootstrapping with analyze_intent")
				synthetic := llm.ToolCall{Function: llm.ToolCallFunction{Name: "analyze_intent", Arguments: "{}"}}
				res, err := executeV2ToolCall(synthetic, context)
				if err != nil {
					messages = append(messages, prompts.Message{Role: "system", Content: fmt.Sprintf("Tool analyze_intent failed: %v", err)})
				} else {
					messages = append(messages, prompts.Message{Role: "system", Content: fmt.Sprintf("Tool analyze_intent result: %s", res)})
				}
				continue
			}
			// Nudge the model to use tools until validated
			messages = append(messages, prompts.Message{Role: "system", Content: "No tool calls found. Use tools to make progress and do not provide a final answer until validation passes."})
			continue
		}

		// Execute tool calls sequentially, append results
		var results []string
		for _, tc := range toolCalls {
			// Parse args (if any)
			var args map[string]any
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			name := strings.TrimSpace(tc.Function.Name)
			if name == "" {
				// Strict schema enforcement
				messages = append(messages, prompts.Message{Role: "system", Content: "Invalid tool call: missing function name. Use the exact tool_calls JSON schema."})
				messages = append(messages, prompts.Message{Role: "system", Content: llm.FormatToolsForPrompt()})
				invalidToolCalls++
				continue
			}

			// Validate required args per tool BEFORE logging execution intent
			switch name {
			case "read_file":
				if fp, _ := args["file_path"].(string); strings.TrimSpace(fp) == "" {
					results = append(results, "Tool read_file blocked: missing 'file_path'")
					messages = append(messages, prompts.Message{Role: "system", Content: "Invalid read_file call: 'file_path' is required. Re-emit a valid tool_calls JSON."})
					invalidToolCalls++
					continue
				}
			case "run_shell_command":
				if cmd, _ := args["command"].(string); strings.TrimSpace(cmd) == "" {
					results = append(results, "Tool run_shell_command blocked: missing 'command'")
					messages = append(messages, prompts.Message{Role: "system", Content: "Invalid run_shell_command: 'command' is required. Re-emit with a concrete command or choose a different tool."})
					invalidToolCalls++
					continue
				}
				// Enforce overall shell cap (aggressive)
				if totalShellCalls >= 5 {
					results = append(results, "Tool run_shell_command blocked: shell usage cap reached")
					messages = append(messages, prompts.Message{Role: "system", Content: "Shell usage cap reached. Proceed to read_file on a specific file, then micro_edit or edit_file_section, then validate."})
					continue
				}
			case "workspace_context":
				if act, _ := args["action"].(string); strings.TrimSpace(act) == "" {
					results = append(results, "Tool workspace_context blocked: missing 'action'")
					messages = append(messages, prompts.Message{Role: "system", Content: "Invalid workspace_context: 'action' is required. Use search_embeddings, search_keywords, load_tree, or load_summary."})
					invalidToolCalls++
					continue
				}
			case "full_edit", "edit_file_section":
				fp, _ := args["file_path"].(string)
				ins, _ := args["instructions"].(string)
				if strings.TrimSpace(fp) == "" || strings.TrimSpace(ins) == "" {
					results = append(results, fmt.Sprintf("Tool %s blocked: missing 'file_path' or 'instructions'", name))
					messages = append(messages, prompts.Message{Role: "system", Content: fmt.Sprintf("Invalid %s: 'file_path' and 'instructions' are required. Re-emit a valid tool_calls JSON.", name)})
					invalidToolCalls++
					continue
				}
			}

			// Announce execution intent only after validation
			logger.LogProcessStep(fmt.Sprintf("v2: %s", describeToolCall(name, args)))

			// Prevent exact duplicate shell commands and throttle no-op shell usage
			if name == "run_shell_command" {
				cmdStr, _ := args["command"].(string)
				trimmed := strings.TrimSpace(cmdStr)
				// Block exact duplicate commands
				if trimmed != "" && executedShell[trimmed] {
					dupMsg := fmt.Sprintf("Tool %s blocked: duplicate command '%s' (already executed)", name, trimmed)
					results = append(results, dupMsg)
					messages = append(messages, prompts.Message{Role: "system", Content: fmt.Sprintf("ERROR: Duplicate shell command requested: '%s'. You already have this information. Do not repeat shell calls; proceed using previously returned output.", trimmed)})
					logger.LogProcessStep(dupMsg)
					blockedDupShell++
					continue
				}
				executedShell[trimmed] = true
				if strings.HasPrefix(trimmed, "ls") || trimmed == "pwd" {
					totalShellCalls++
					noopShellCount++
					if noopShellCount > 3 {
						results = append(results, fmt.Sprintf("Tool %s blocked: repetitive no-op '%s'", tc.Function.Name, trimmed))
						// Generic nudge toward productive next steps
						messages = append(messages, prompts.Message{Role: "system", Content: "Stop listing directories. Use read_file on relevant files, propose a precise edit with full_edit, then run validate."})
						blockedNoopShell++
						continue
					}
				}
				// Count towards shell cap after validation
				totalShellCalls++
			}

			// Cap workspace_context usage and block duplicate requests
			if name == "workspace_context" {
				action, _ := args["action"].(string)
				query, _ := args["query"].(string)
				key := strings.TrimSpace(action) + "::" + strings.TrimSpace(query)
				if workspaceContextCalls >= 2 {
					results = append(results, "Tool workspace_context blocked: usage cap reached")
					messages = append(messages, prompts.Message{Role: "system", Content: "Workspace context already gathered. Proceed to read_file on specific files, edit, and validate."})
					logger.LogProcessStep("v2: workspace_context blocked (cap reached)")
					blockedWSCap++
					continue
				}
				if workspaceRequests[key] {
					results = append(results, fmt.Sprintf("Tool workspace_context blocked: duplicate request (%s)", key))
					logger.LogProcessStep("v2: workspace_context blocked (duplicate request)")
					blockedWSDup++
					continue
				}
				workspaceRequests[key] = true
				workspaceContextCalls++
			}

			res, execErr := executeV2ToolCall(tc, context)
			if execErr != nil {
				results = append(results, fmt.Sprintf("Tool %s failed: %v", tc.Function.Name, execErr))
				context.Errors = append(context.Errors, execErr.Error())
			} else {
				results = append(results, fmt.Sprintf("Tool %s result: %s", tc.Function.Name, res))
			}
			// Track simple success signals
			switch name {
			case "full_edit":
				editedSomething = true
			case "validate":
				validated = !context.ValidationFailed
			}
			// Log completion or failure for visibility
			if execErr != nil {
				logger.LogProcessStep(fmt.Sprintf("v2: failed tool %s", name))
			} else {
				logger.LogProcessStep(fmt.Sprintf("v2: completed tool %s", name))
			}
			totalTools++
			// Hard stop if too many shell calls overall
			if totalShellCalls > 15 {
				messages = append(messages, prompts.Message{Role: "system", Content: "Too many shell listings. Proceed to read_file, then full_edit, then validate."})
			}
		}
		messages = append(messages, prompts.Message{Role: "system", Content: fmt.Sprintf("Tool results:\n%s", strings.Join(results, "\n"))})

		// Stuck detection: if no progress (no edit or validate) for consecutive turns, force deterministic next steps
		if !editedSomething && !validated {
			noProgressStreak++
			if noProgressStreak >= 2 {
				messages = append(messages, prompts.Message{Role: "system", Content: "You are stuck. Stop exploring. Choose a specific file, use read_file to load it, propose a minimal change with micro_edit or edit_file_section, then run validate."})
				noProgressStreak = 0
				logger.LogProcessStep("v2: DEBUG stuck-detector fired; injected corrective instruction")
			}
		} else {
			noProgressStreak = 0
		}
	}

	// If we edited but never validated, attempt a final validation pass
	if editedSomething && !validated {
		logger.LogProcessStep("v2: running final validation pass")
		_ = executeValidation(context)
	}

	// Debug summary
	logger.LogProcessStep(fmt.Sprintf("v2: DEBUG run summary: turns=%d tools=%d dupShellBlocked=%d noopShellBlocked=%d wsCapBlocked=%d wsDupBlocked=%d invalidTools=%d", totalTurns, totalTools, blockedDupShell, blockedNoopShell, blockedWSCap, blockedWSDup, invalidToolCalls))

	return context.TokenUsage, nil
}

func buildAgentV2Policy() string {
	return `You are an autonomous software engineering agent. You accomplish concrete dev tasks using a constrained toolbelt.

OBJECTIVE-DRIVEN WORKFLOW (follow in order, skip when satisfied):
1) Identify target file(s).
   - If a file is explicitly mentioned and exists, prefer it.
   - Otherwise, call workspace_context (embeddings or keywords) at most 2 total times to select a single best candidate.
2) Inspect only what you need.
   - Use read_file to load the selected file. Do not guess contents.
3) Make the smallest viable change.
   - If the change is tiny (comments/imports/literals, ≤50 changed lines), use micro_edit.
   - Otherwise use edit_file_section for targeted changes.
   - Prefer precise, minimal instructions; avoid refactors unless requested.
4) Validate.
   - Run validate after editing. If validation fails, adjust and retry once.
5) Stop immediately when the goal is achieved and validated (or clearly doc-only).

SAFETY AND CONSTRAINTS:
- STRICT FORMAT: While working, respond ONLY with JSON tool_calls (no prose). Invalid JSON or missing function name is rejected.
- Do not repeat the exact same shell command; use prior outputs instead.
- Avoid repetitive no-op shell commands like "ls"/"pwd" (max 2 allowed).
- Do not call workspace_context more than 2 times total per run; do not duplicate the same request.
- Keep decisions short, deterministic, and actionable. Prefer doing over planning.
- Use as few tools as necessary; avoid global searches when a file is known.

HEURISTICS TO AVOID LOOPS:
- If you haven't edited or validated after 3 turns, stop exploring. Choose a file, read it, apply micro_edit or edit_file_section, then validate.
- Do not ask the user unless truly blocked; prefer reading files and validating.

OUTPUT POLICY:
- While working, emit ONLY the strict tool_calls JSON block.
- Do not batch dependent calls in one turn (e.g., do not edit before you’ve read the file you need). Independent calls can be batched.
- Provide a concise final summary only after validation passes.
` + "\n\n" + llm.FormatToolsForPrompt()
}

func executeV2ToolCall(tc llm.ToolCall, context *AgentContext) (string, error) {
	// Arguments are JSON encoded in tc.Function.Arguments
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return "", fmt.Errorf("invalid tool args: %w", err)
	}
	name := strings.TrimSpace(tc.Function.Name)
	switch name {
	case "analyze_intent":
		ia, tokens, err := analyzeIntentWithMinimalContext(context.UserIntent, context.Config, context.Logger)
		if err != nil {
			return "", err
		}
		context.IntentAnalysis = ia
		context.TokenUsage.IntentAnalysis += tokens
		context.TokenUsage.IntentSplit.Prompt += tokens // approx
		return fmt.Sprintf("intent: category=%s complexity=%s files=%d", ia.Category, ia.Complexity, len(ia.EstimatedFiles)), nil

	case "create_plan":
		if context.IntentAnalysis == nil {
			return "", fmt.Errorf("no intent analysis available")
		}
		plan, tokens, err := createDetailedEditPlan(context.UserIntent, context.IntentAnalysis, context.Config, context.Logger)
		if err != nil {
			return "", err
		}
		context.CurrentPlan = plan
		context.TokenUsage.Planning += tokens
		context.TokenUsage.PlanningSplit.Prompt += tokens // approx
		return fmt.Sprintf("plan: files=%d ops=%d", len(plan.FilesToEdit), len(plan.EditOperations)), nil

	case "full_edit":
		filePath, _ := args["file_path"].(string)
		instr, _ := args["instructions"].(string)
		if strings.TrimSpace(filePath) == "" || strings.TrimSpace(instr) == "" {
			return "", fmt.Errorf("full_edit requires file_path and instructions")
		}
		// Count prompt tokens
		pTokens := utils.EstimateTokens(instr)
		context.TokenUsage.CodeGeneration += pTokens
		context.TokenUsage.CodegenSplit.Prompt += pTokens
		diff, err := editor.ProcessCodeGeneration(filePath, instr, context.Config, "")
		if err != nil {
			return "", err
		}
		cTokens := utils.EstimateTokens(diff)
		context.TokenUsage.CodeGeneration += cTokens
		context.TokenUsage.CodegenSplit.Completion += cTokens
		context.ExecutedOperations = append(context.ExecutedOperations, fmt.Sprintf("Edited %s", filePath))
		return fmt.Sprintf("edited %s (diff tokens ~%d)", filePath, cTokens), nil

	case "micro_edit":
		// Optional args: file_path, instructions. If missing, fall back to executeMicroEdit
		filePath, _ := args["file_path"].(string)
		instr, _ := args["instructions"].(string)
		if strings.TrimSpace(filePath) == "" || strings.TrimSpace(instr) == "" {
			if err := executeMicroEdit(context); err != nil {
				return "", err
			}
			return "micro_edit applied", nil
		}
		// With explicit target and instructions, perform partial edit directly
		pTokens := utils.EstimateTokens(instr)
		context.TokenUsage.CodeGeneration += pTokens
		context.TokenUsage.CodegenSplit.Prompt += pTokens
		diff, err := editor.ProcessPartialEdit(filePath, instr, context.Config, context.Logger)
		if err != nil {
			return "", err
		}
		cTokens := utils.EstimateTokens(diff)
		context.TokenUsage.CodeGeneration += cTokens
		context.TokenUsage.CodegenSplit.Completion += cTokens
		context.ExecutedOperations = append(context.ExecutedOperations, fmt.Sprintf("micro_edit applied to %s", filePath))
		return fmt.Sprintf("micro_edit applied to %s (diff tokens ~%d)", filePath, cTokens), nil

	case "edit_file_section":
		// Similar to micro_edit; 'target_section' is advisory and should be reflected in instructions
		filePath, _ := args["file_path"].(string)
		instr, _ := args["instructions"].(string)
		if strings.TrimSpace(filePath) == "" || strings.TrimSpace(instr) == "" {
			return "", fmt.Errorf("edit_file_section requires file_path and instructions")
		}
		pTokens := utils.EstimateTokens(instr)
		context.TokenUsage.CodeGeneration += pTokens
		context.TokenUsage.CodegenSplit.Prompt += pTokens
		diff, err := editor.ProcessPartialEdit(filePath, instr, context.Config, context.Logger)
		if err != nil {
			return "", err
		}
		cTokens := utils.EstimateTokens(diff)
		context.TokenUsage.CodeGeneration += cTokens
		context.TokenUsage.CodegenSplit.Completion += cTokens
		context.ExecutedOperations = append(context.ExecutedOperations, fmt.Sprintf("edit_file_section applied to %s", filePath))
		return fmt.Sprintf("edit_file_section applied to %s (diff tokens ~%d)", filePath, cTokens), nil

	case "validate":
		if err := executeValidation(context); err != nil {
			return "", err
		}
		status := "passed"
		if context.ValidationFailed {
			status = "failed"
		}
		return fmt.Sprintf("validation %s", status), nil

	case "run_shell_command":
		command, _ := args["command"].(string)
		if strings.TrimSpace(command) == "" {
			return "", fmt.Errorf("run_shell_command requires command")
		}
		if err := executeShellCommands(context, []string{command}); err != nil {
			return "", err
		}
		return "command executed", nil

	case "read_file":
		filePath, _ := args["file_path"].(string)
		if strings.TrimSpace(filePath) == "" {
			return "", fmt.Errorf("read_file requires file_path")
		}
		b, err := os.ReadFile(filePath)
		if err != nil {
			return "", err
		}
		return string(b), nil

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// describeToolCall builds a human-friendly description of a tool's intent
func describeToolCall(name string, args map[string]any) string {
	switch name {
	case "read_file":
		if fp, ok := args["file_path"].(string); ok && strings.TrimSpace(fp) != "" {
			return fmt.Sprintf("Examining %s", fp)
		}
		return "Reading file"
	case "full_edit":
		if fp, ok := args["file_path"].(string); ok && strings.TrimSpace(fp) != "" {
			return fmt.Sprintf("Editing (full file) %s", fp)
		}
		return "Editing file (full)"
	case "micro_edit":
		if fp, ok := args["file_path"].(string); ok && strings.TrimSpace(fp) != "" {
			return fmt.Sprintf("Applying micro edit to %s", fp)
		}
		return "Applying micro edit"
	case "edit_file_section":
		if fp, ok := args["file_path"].(string); ok && strings.TrimSpace(fp) != "" {
			return fmt.Sprintf("Editing section in %s", fp)
		}
		return "Editing file section"
	case "validate":
		return "Validating changes"
	case "analyze_intent":
		return "Analyzing intent"
	case "create_plan":
		return "Creating execution plan"
	case "workspace_context":
		action, _ := args["action"].(string)
		query, _ := args["query"].(string)
		action = strings.TrimSpace(action)
		switch action {
		case "search_embeddings":
			if query != "" {
				return fmt.Sprintf("Searching workspace (embeddings): %s", query)
			}
			return "Searching workspace (embeddings)"
		case "search_keywords":
			if query != "" {
				return fmt.Sprintf("Searching workspace (keywords): %s", query)
			}
			return "Searching workspace (keywords)"
		case "load_tree":
			return "Loading workspace tree"
		case "load_summary":
			return "Loading workspace summary"
		default:
			return "Accessing workspace context"
		}
	case "run_shell_command":
		if cmd, ok := args["command"].(string); ok && strings.TrimSpace(cmd) != "" {
			return fmt.Sprintf("Running shell: %s", strings.TrimSpace(cmd))
		}
		return "Running shell command"
	default:
		return fmt.Sprintf("Executing %s", name)
	}
}
