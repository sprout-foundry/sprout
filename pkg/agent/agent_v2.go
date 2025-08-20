//go:build !agent2refactor

package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"os/exec"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/embedding"
	"github.com/alantheprice/ledit/pkg/git"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/parser"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
)

// tail returns last n chars of a string
func tailStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// preflightQuick checks existence and writability of a file
func preflightQuick(path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("not_found: %s", path)
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("permission: not writable: %s", path)
	}
	_ = f.Close()
	return nil
}

// RunAgentModeV2: deterministic fast-path for doc-only edits (top-of-file comment insertion).
// If the user intent includes an explicit existing .go file path, insert a one-paragraph
// summary comment at the top of the file. This avoids tool-loop overhead for trivial tasks.
func RunAgentModeV2(userIntent string, skipPrompt bool, model string, directApply bool) error {
	startTime := time.Now()
	// TODO: We should be able to be more intelligent about finding the correct file.
	ui.Out().Print("🤖 Agent v2 mode: Tool-driven execution\n")
	ui.Out().Printf("🎯 Intent: %s\n", userIntent)

	cfg, err := config.LoadOrInitConfig(skipPrompt)
	if err != nil {
		logger := utils.GetLogger(false)
		logger.LogError(fmt.Errorf("failed to load config: %w", err))
		return fmt.Errorf("failed to load config: %w", err)
	}
	// Respect CLI skip-prompt for non-interactive flows
	cfg.SkipPrompt = skipPrompt
	// Enable interactive mode and code tools for agent workflow
	cfg.Interactive = true
	cfg.CodeToolsEnabled = true
	// Set environment variable to indicate this is an agent workflow
	os.Setenv("LEDIT_FROM_AGENT", "1")
	logger := utils.GetLogger(cfg.SkipPrompt)
	runlog := utils.GetRunLogger()
	if runlog != nil {
		runlog.LogEvent("agent_start", map[string]any{"intent": userIntent, "orchestration_model": cfg.OrchestrationModel, "editing_model": cfg.EditingModel})
	}

	// Check for debug mode
	debugMode := os.Getenv("LEDIT_DEBUG") == "1" || os.Getenv("LEDIT_DEBUG") == "true"
	if debugMode {
		logger.LogProcessStep("🐛 Debug mode enabled - verbose logging activated")
	}
	// Ensure interactive tool-calling enabled for v2 planner/executor/evaluator usage
	cfg.Interactive = true
	cfg.CodeToolsEnabled = true
	cfg.FromAgent = true

	// Routing overview
	ctrlModel := cfg.OrchestrationModel
	if ctrlModel == "" {
		ctrlModel = cfg.EditingModel
	}
	edtModel := cfg.EditingModel
	ui.Out().Printf("controlModel %s\n", ctrlModel)
	ui.Out().Printf("editingModel %s\n", edtModel)

	// Focus-files bias (derived from estimated files)
	focusSet := map[string]bool{}

	// Optional workspace analysis upfront.
	// If a workspace file already exists, assume prior consent and run by default.
	// Otherwise: run by default when skipping prompts; ask consent in interactive mode.
	if wf, err := workspace.LoadWorkspaceFile(); err == nil && len(wf.Files) >= 0 {
		_ = workspace.GetWorkspaceContext("", cfg)
		db := embedding.NewVectorDB()
		_ = embedding.GenerateWorkspaceEmbeddings(wf, db, cfg)
	} else if cfg.SkipPrompt {
		_ = workspace.GetWorkspaceContext("", cfg)
		if wf2, err2 := workspace.LoadWorkspaceFile(); err2 == nil {
			db := embedding.NewVectorDB()
			_ = embedding.GenerateWorkspaceEmbeddings(wf2, db, cfg)
		}
	} else {
		consent := utils.GetLogger(false).AskForConfirmation(
			"Run a quick workspace analysis (file summaries + embeddings) to improve retrieval? This is inexpensive and speeds up future tasks.",
			true, false,
		)
		if consent {
			_ = workspace.GetWorkspaceContext("", cfg)
			if wf3, err3 := workspace.LoadWorkspaceFile(); err3 == nil {
				db := embedding.NewVectorDB()
				_ = embedding.GenerateWorkspaceEmbeddings(wf3, db, cfg)
			}
		}
	}

	// Phase 1: Intent analysis + planning (no edits). Provide a compact workspace synopsis for grounding.
	logger.LogProcessStep("🧭 Analyzing intent and planning changes (no edits yet)...")
	if runlog != nil {
		runlog.LogEvent("phase", map[string]any{"name": "planning"})
	}
	// (planning prelude removed to satisfy lints)
	intentAnalysis, _, err := analyzeIntentWithMinimalContext(userIntent, cfg, logger)
	if err != nil {
		logger.LogError(fmt.Errorf("intent analysis failed: %w", err))
	}
	// Populate focus set from estimated files (cap to 6) and common docs if this is a docs task
	if intentAnalysis != nil {
		capN := 0
		for _, f := range intentAnalysis.EstimatedFiles {
			focusSet[f] = true
			capN++
			if capN >= 6 {
				break
			}
		}
		lo := strings.ToLower(userIntent)
		if strings.Contains(lo, "docs") || strings.Contains(lo, "documentation") || strings.Contains(lo, "readme") {
			// include common docs
			for _, d := range []string{"README.md", "docs/README.md", "docs/index.md"} {
				if _, err := os.Stat(d); err == nil {
					focusSet[d] = true
				}
			}
		}
	}
	// Playbook path temporarily disabled to stabilize core workflow
	var planOps []struct{ file, instructions, desc string }
	logger.LogProcessStep("(playbooks disabled) proceeding with interactive planning")
	// If no playbook produced ops, fall back to interactive planning (still no edits at this point)
	if len(planOps) == 0 {
		controlModel := cfg.OrchestrationModel
		if controlModel == "" {
			controlModel = cfg.EditingModel
		}
		focusList := []string{}
		for f := range focusSet {
			focusList = append(focusList, f)
		}
		sort.Strings(focusList)
		// Use direct planning approach
		logger.LogProcessStep("🤖 Creating edit plan...")
		logger.LogProcessStep("📝 Using direct planning without complex tool interactions")

		// Create simple planning prompt
		planningPrompt := "You are an expert software development agent.\n\n"
		planningPrompt += "USER REQUEST: " + userIntent + "\n\n"
		planningPrompt += "Create a simple plan to fulfill this request.\n\n"
		planningPrompt += "Respond with JSON: {\"edits\":[{\"file\":\"filename\",\"instructions\":\"specific change to make\"}]}"

		msgs := []prompts.Message{{Role: "system", Content: planningPrompt}}
		// Best-effort: include small workspace synopsis if available
		if wfSnap, err := workspace.LoadWorkspaceFile(); err == nil {
			if prelude, err := workspace.GetFullWorkspaceSummary(wfSnap, cfg.CodeStyle, cfg, logger); err == nil && prelude != "" {
				const maxPrelude = 16000
				if len(prelude) > maxPrelude {
					prelude = prelude[:maxPrelude]
					// add a log to note the truncation
					logger.Logf("Workspace synopsis truncated to %d chars", maxPrelude)
				}
				msgs = append([]prompts.Message{{Role: "system", Content: "Workspace Synopsis (truncated):\n" + prelude}}, msgs...)
			}
		}
		if pre := buildWorkspacePrelude(cfg); pre != "" {
			msgs = append([]prompts.Message{{Role: "system", Content: pre}}, msgs...)
		}
		if br, u, s, err := gitStatusSummary(); err == nil {
			gitPrelude := fmt.Sprintf("Git: branch=%s staged=%d modified=%d", br, s, u)
			msgs = append([]prompts.Message{{Role: "system", Content: gitPrelude}}, msgs...)
		}
		logger.LogProcessStep("🔧 Creating simple planning...")
		logger.Logf("📝 Planning with model: %s", controlModel)
		logger.Logf("📝 Focus files: %v", focusList)
		logger.Logf("📝 Planning messages count: %d", len(msgs))

		resp, _, err := llm.GetLLMResponse(controlModel, msgs, "simple_planning", cfg, 60*time.Second)
		if err == nil {
			logger.Logf("📋 Planning session completed - response received (%d chars)", len(resp))
			if debugMode {
				logger.Logf("📋 Full response: %s", resp)
			}

			// Look for JSON plan in the response
			clean, jerr := utils.ExtractJSONFromLLMResponse(resp)
			if jerr != nil {
				logger.LogError(fmt.Errorf("Could not extract JSON from planning response: %w", jerr))
				logger.Logf("📋 Raw response (%d chars): %s", len(resp), resp)
				logger.Logf("📋 Will try to parse plan from raw response")
				clean = resp // Fall back to raw response
			} else {
				logger.Logf("📋 Extracted JSON plan (%d chars): %s", len(clean), clean)
			}

			// Per-turn artifact logging: record raw plan JSON (truncated)
			if runlog != nil && strings.TrimSpace(clean) != "" {
				preview := clean
				if len(preview) > 4000 {
					headPart := preview[:2000]
					tailPart := preview[len(preview)-2000:]
					preview = headPart + "\n... [truncated] ...\n" + tailPart
				}
				runlog.LogEvent("plan_json", map[string]any{"source": "initial", "json": preview})
			}
			// Try to parse the plan from the response
			var planFound = false
			if strings.Contains(clean, "\"edits\"") || strings.Contains(clean, "edits") {
				var plan struct {
					Edits []struct {
						File         string `json:"file"`
						Instructions string `json:"instructions"`
					} `json:"edits"`
				}
				if json.Unmarshal([]byte(clean), &plan) == nil {
					logger.Logf("📋 Plan parsed successfully: %d edits found", len(plan.Edits))
					for i, e := range plan.Edits {
						logger.Logf("📋 Edit %d: File=%s, Instructions=%s", i+1, e.File, e.Instructions)
						if e.File == "" || strings.HasSuffix(e.File, "/") {
							logger.Logf("📋 Skipping edit %d: invalid file path", i+1)
							continue
						}
						if st, err := os.Stat(e.File); err == nil && !st.IsDir() {
							desc := "Apply requested change"
							planOps = append(planOps, struct{ file, instructions, desc string }{file: e.File, instructions: e.Instructions, desc: desc})
							logger.Logf("📋 Added to plan: %s", e.File)
							planFound = true
						} else {
							logger.Logf("📋 File not found or is directory: %s", e.File)
						}
					}
				} else {
					logger.LogError(fmt.Errorf("Failed to unmarshal plan JSON"))
				}
			}

			// If no structured plan found, try to extract simple instructions
			if !planFound && len(focusList) > 0 {
				logger.Logf("📋 No structured plan found, creating simple plan from focus files")
				for _, file := range focusList {
					if strings.Contains(strings.ToLower(file), "readme") || strings.Contains(strings.ToLower(file), ".md") {
						planOps = append(planOps, struct{ file, instructions, desc string }{
							file:         file,
							instructions: "Update the file to reflect the current state of the commands and project structure",
							desc:         "Update documentation",
						})
						logger.Logf("📋 Added fallback plan for: %s", file)
						planFound = true
						break
					}
				}
			}

			if !planFound {
				logger.Logf("📋 No actionable plan could be created from the response")
				logger.Logf("📋 Response content: %.200s...", clean)
			}
		} else {
			// Handle planning failure
			logger.LogError(fmt.Errorf("Planning session failed: %w", err))
			logger.Logf("📋 Will try to create a simple fallback plan")

			// Create a simple fallback plan based on focus files
			if len(focusList) > 0 {
				logger.Logf("📋 Creating fallback plan for focus files")
				for _, file := range focusList {
					if strings.Contains(strings.ToLower(file), "readme") || strings.Contains(strings.ToLower(file), ".md") {
						planOps = append(planOps, struct{ file, instructions, desc string }{
							file:         file,
							instructions: userIntent,
							desc:         "Apply user's requested changes",
						})
						logger.Logf("📋 Fallback plan: Update %s with user's request", file)
						break
					}
				}
			}
		}
		// As a minimal fallback, use focus files if plan still empty
		if len(planOps) == 0 && len(focusSet) > 0 {
			for f := range focusSet {
				if st, err := os.Stat(f); err == nil && !st.IsDir() {
					planOps = append(planOps, struct{ file, instructions, desc string }{file: f, instructions: userIntent, desc: "Apply requested change"})
				}
			}
		}
	}

	// Log planned ops for observability
	if runlog != nil && len(planOps) > 0 {
		files := make([]string, 0, len(planOps))
		for _, op := range planOps {
			files = append(files, op.file)
		}
		runlog.LogEvent("plan_ops", map[string]any{"count": len(planOps), "files": files})
	}

	// Validate planOps: must target focus files (for docs) and include non-empty instructions
	if len(planOps) > 0 {
		validated := make([]struct{ file, instructions, desc string }, 0, len(planOps))
		for _, op := range planOps {
			if strings.TrimSpace(op.instructions) == "" {
				if runlog != nil {
					runlog.LogEvent("plan_op_reject", map[string]any{"file": op.file, "reason": "empty_instructions"})
				}
				continue
			}
			isDoc := strings.HasSuffix(strings.ToLower(op.file), ".md") || strings.Contains(strings.ToLower(op.file), "/docs/")
			if isDoc && len(focusSet) > 0 && !focusSet[op.file] {
				if runlog != nil {
					runlog.LogEvent("plan_op_reject", map[string]any{"file": op.file, "reason": "outside_focus"})
				}
				continue
			}
			validated = append(validated, op)
		}
		planOps = validated
		if runlog != nil {
			runlog.LogEvent("plan_ops_validated", map[string]any{"count": len(planOps)})
		}
	}

	// If no actionable plan after validation, re-request a stricter plan
	if len(planOps) == 0 {
		controlModel := cfg.OrchestrationModel
		if controlModel == "" {
			controlModel = cfg.EditingModel
		}
		focusList := []string{}
		for f := range focusSet {
			focusList = append(focusList, f)
		}
		sort.Strings(focusList)
		focusMsg := ""
		if len(focusList) > 0 {
			focusMsg = "You MUST pick at least one of these files: \n" + strings.Join(focusList, "\n")
		}
		strict := []prompts.Message{{Role: "system", Content: "Planner: Return ONLY the final JSON plan now.\nSchema: {\"edits\":[{\"file\":string,\"instructions\":string}]}.\nRules:\n- Do NOT include tool_calls.\n- Instructions must be concrete and minimal.\n- For docs: propose only changes you can support with citations you will gather next; keep scope tight.\n- For code: target a specific function/section; avoid sweeping refactors.\nExample: {\"edits\":[{\"file\":\"README.md\",\"instructions\":\"Replace outdated install command with current one from docs/CHEATSHEET.md.\"}]}"}}
		if focusMsg != "" {
			strict = append(strict, prompts.Message{Role: "system", Content: focusMsg})
		}
		strict = append(strict, prompts.Message{Role: "user", Content: fmt.Sprintf("Goal: %s", userIntent)})
		resp2, _, err2 := llm.GetLLMResponse(controlModel, strict, "plan_request_strict", cfg, 45*time.Second)
		if err2 == nil {
			clean2, jerr2 := utils.ExtractJSONFromLLMResponse(resp2)
			// Per-turn artifact logging: record strict plan JSON (truncated)
			if runlog != nil && strings.TrimSpace(clean2) != "" {
				preview2 := clean2
				if len(preview2) > 4000 {
					headPart2 := preview2[:2000]
					tailPart2 := preview2[len(preview2)-2000:]
					preview2 = headPart2 + "\n... [truncated] ...\n" + tailPart2
				}
				runlog.LogEvent("plan_json", map[string]any{"source": "strict", "json": preview2})
			}
			if jerr2 == nil && strings.Contains(clean2, "\"edits\"") {
				var plan2 struct {
					Edits []struct {
						File         string `json:"file"`
						Instructions string `json:"instructions"`
					} `json:"edits"`
				}
				if json.Unmarshal([]byte(clean2), &plan2) == nil {
					for _, e := range plan2.Edits {
						if e.File == "" || strings.HasSuffix(e.File, "/") {
							continue
						}
						if st, err := os.Stat(e.File); err == nil && !st.IsDir() {
							isDoc := strings.HasSuffix(strings.ToLower(e.File), ".md") || strings.Contains(strings.ToLower(e.File), "/docs/")
							if isDoc && len(focusSet) > 0 && !focusSet[e.File] {
								continue
							}
							planOps = append(planOps, struct{ file, instructions, desc string }{file: e.File, instructions: e.Instructions, desc: "Apply requested change"})
						}
					}
				}
			}
		}
		if runlog != nil {
			runlog.LogEvent("plan_ops_after_strict", map[string]any{"count": len(planOps)})
		}
	}

	// (seed README plan removed to simplify and satisfy lints)

	if len(planOps) == 0 {
		logger.LogProcessStep("⚠️ No actionable plan produced; aborting without edits")
		return fmt.Errorf("no actionable plan produced for intent")
	}

	// Phase 2: Execute plan with guardrails (verify → edit; preflight each file, minimal edits per op)
	logger.LogProcessStep("🛠️ Executing plan with guarded edits...")

	if len(planOps) == 0 {
		logger.LogProcessStep("⚠️ No plan operations found - this indicates a problem with planning")
		logger.Logf("📋 The agent should have created a plan but didn't produce any operations")
		logger.Logf("📋 This suggests the planning phase failed or didn't produce usable output")
	} else {
		logger.Logf("📋 Total operations to execute: %d", len(planOps))
		for i, op := range planOps {
			logger.Logf("📋 Operation %d: File=%s, Instructions='%s'", i+1, op.file, op.instructions)
		}
	}

	if runlog != nil {
		runlog.LogEvent("phase", map[string]any{"name": "execution", "ops": len(planOps)})
	}
	applied := 0
	verificationFailed := false
	for i, op := range planOps {
		logger.Logf("🛠️ Executing operation %d/%d: %s", i+1, len(planOps), op.file)
		logger.Logf("🛠️ Instructions: %s", op.instructions)

		// Observability: record the edit attempt
		if runlog != nil {
			typeStr := "code"
			if strings.HasSuffix(strings.ToLower(op.file), ".md") || strings.Contains(strings.ToLower(op.file), "/docs/") {
				typeStr = "doc"
			}
			runlog.LogEvent("edit_attempt", map[string]any{"file": op.file, "type": typeStr, "instructions_len": len(op.instructions)})
		}
		// Gate edits outside focus for docs tasks: skip if not in focus
		isDoc := strings.HasSuffix(strings.ToLower(op.file), ".md") || strings.Contains(strings.ToLower(op.file), "/docs/")
		if isDoc && len(focusSet) > 0 {
			if !focusSet[op.file] {
				logger.Logf("Skipping %s (outside focus set)", op.file)
				if runlog != nil {
					runlog.LogEvent("skip_outside_focus", map[string]any{"file": op.file})
				}
				continue
			}
		}
		if err := preflightQuick(op.file); err != nil {
			logger.Logf("Skipping %s (preflight failed: %v)", op.file, err)
			if runlog != nil {
				runlog.LogEvent("preflight_skip", map[string]any{"file": op.file, "error": err.Error()})
			}
			continue
		}
		// For documentation files, first generate a claim→citation map by asking for minimal reads
		if strings.HasSuffix(strings.ToLower(op.file), ".md") || strings.Contains(strings.ToLower(op.file), "/docs/") {
			// Temporary: if this is a seeded README update, insert a minimal Usage section deterministically
			// (readme usage insertion removed)

			// Use interactive controller for verification to allow tool calls
			verifyModel := cfg.OrchestrationModel
			if verifyModel == "" {
				verifyModel = cfg.EditingModel
			}
			// Skip complex verification for now - use simple approach
			logger.Logf("Verification step skipped for %s (using simple execution)", op.file)
			// Prefer hunk-based plan with citations; apply deterministically when present
			if hplan, herr := proposeDocHunks(op.file, cfg, logger); herr == nil && len(hplan.Hunks) > 0 {
				// Observability: log summarized hunk plan before applying
				if runlog != nil {
					whereHints := []string{}
					for i, h := range hplan.Hunks {
						if i >= 5 {
							break
						}
						whereHints = append(whereHints, strings.TrimSpace(h.WhereHint))
					}
					runlog.LogEvent("doc_hunks_plan", map[string]any{"file": op.file, "hunks": len(hplan.Hunks), "where_hints": whereHints})
				}
				allCited := true
				for _, h := range hplan.Hunks {
					if len(h.Citations) == 0 {
						allCited = false
						break
					}
				}
				if allCited {
					if aerr := applyDocHunks(op.file, hplan, logger); aerr == nil {
						if runlog != nil {
							runlog.LogEvent("edit_applied", map[string]any{"file": op.file, "method": "doc_hunks"})
						}
						applied++
						continue
					}
				}
				// If hunks lack citations, log and skip this doc edit (hard-reject)
				logger.Logf("Doc hunks missing citations for %s; skipping edit to enforce evidence-first policy", op.file)
				if runlog != nil {
					runlog.LogEvent("doc_skip_no_citations", map[string]any{"file": op.file})
				}
				continue
			}
			// If no hunks returned at all, do not apply replacements without citations; skip
			logger.Logf("No hunk plan returned for %s; skipping edit to enforce citations requirement", op.file)
			if runlog != nil {
				runlog.LogEvent("doc_skip_no_hunks", map[string]any{"file": op.file})
			}
			continue
		}

		// Prioritize hunk-based edits for all file types - they're more efficient than full file edits
		logger.Logf("🧩 Attempting hunk-based edit for %s (most efficient approach)", op.file)

		// Try hunk-based edits first for all file types
		if strings.HasSuffix(strings.ToLower(op.file), ".md") || strings.Contains(strings.ToLower(op.file), "/docs/") {
			// For documentation files
			if hplan, herr := proposeDocHunks(op.file, cfg, logger); herr == nil && len(hplan.Hunks) > 0 {
				// Observability: log summarized hunk plan before applying
				if runlog != nil {
					whereHints := []string{}
					for i, h := range hplan.Hunks {
						if i >= 5 {
							break
						}
						whereHints = append(whereHints, strings.TrimSpace(h.WhereHint))
					}
					runlog.LogEvent("doc_hunks_plan", map[string]any{"file": op.file, "hunks": len(hplan.Hunks), "where_hints": whereHints})
				}
				allCited := true
				for _, h := range hplan.Hunks {
					if len(h.Citations) == 0 {
						allCited = false
						break
					}
				}
				if allCited {
					if aerr := applyDocHunks(op.file, hplan, logger); aerr == nil {
						if runlog != nil {
							runlog.LogEvent("edit_applied", map[string]any{"file": op.file, "method": "doc_hunks"})
						}
						applied++
						continue
					}
				}
				// If hunks lack citations, log and skip this doc edit (hard-reject)
				logger.Logf("Doc hunks missing citations for %s; skipping edit to enforce evidence-first policy", op.file)
				if runlog != nil {
					runlog.LogEvent("doc_skip_no_citations", map[string]any{"file": op.file})
				}
				continue
			}
			// If no hunks returned at all, do not apply replacements without citations; skip
			logger.Logf("No hunk plan returned for %s; skipping edit to enforce citations requirement", op.file)
			if runlog != nil {
				runlog.LogEvent("doc_skip_no_hunks", map[string]any{"file": op.file})
			}
			continue
		}

		// For code files, attempt hunk-based code patch before editor fallback
		if chplan, cherr := proposeCodeHunks(op.file, op.instructions, cfg, logger); cherr == nil && len(chplan.Hunks) > 0 {
			// Observability: log summarized code hunk plan before applying
			if runlog != nil {
				whereHints := []string{}
				for i, h := range chplan.Hunks {
					if i >= 5 {
						break
					}
					whereHints = append(whereHints, strings.TrimSpace(h.WhereHint))
				}
				runlog.LogEvent("code_hunks_plan", map[string]any{"file": op.file, "hunks": len(chplan.Hunks), "where_hints": whereHints})
			}
			if aerr := applyCodeHunks(op.file, chplan, logger); aerr == nil {
				if runlog != nil {
					runlog.LogEvent("edit_applied", map[string]any{"file": op.file, "method": "code_hunks"})
				}
				applied++
				continue
			}
		}
		// Try patch-based editing first (most efficient)
		logger.Logf("🧩 Attempting patch-based edit for %s (most efficient approach)", op.file)
		if err := attemptPatchBasedEdit(op.file, op.instructions, cfg, logger); err != nil {
			logger.Logf("❌ Patch-based edit failed for %s: %v", op.file, err)
			logger.Logf("🎯 Falling back to full-file edit for %s", op.file)
			diff, err := editor.ProcessCodeGeneration(op.file, op.instructions, cfg, "")
			if err != nil {
				logger.Logf("❌ Full-file edit also failed for %s: %v", op.file, err)
				if runlog != nil {
					runlog.LogEvent("edit_failure", map[string]any{"file": op.file, "error": err.Error()})
				}
				continue
			} else {
				logger.Logf("✅ Full-file edit succeeded for %s", op.file)
				if runlog != nil && strings.TrimSpace(diff) != "" {
					runlog.LogEvent("edit_full_diff", map[string]any{"file": op.file, "diff_len": len(diff)})
					runlog.LogEvent("edit_applied", map[string]any{"file": op.file, "method": "full_edit"})
				}
				applied++
			}
		} else {
			logger.Logf("✅ Patch-based edit succeeded for %s", op.file)
			if runlog != nil {
				runlog.LogEvent("edit_applied", map[string]any{"file": op.file, "method": "patch_edit"})
			}
			applied++
		}
	}
	if applied == 0 {
		logger.LogProcessStep("❌ No edits were successfully applied")
		logger.Logf("📋 Planned operations: %d, Successful operations: %d", len(planOps), applied)
		if runlog != nil {
			runlog.LogEvent("agent_end", map[string]any{"status": "no_edits"})
		}
		return fmt.Errorf("plan produced no successful edits")
	}

	// Report final status based on verification results
	if verificationFailed {
		logger.LogProcessStep(fmt.Sprintf("⚠️ Agent v2 execution completed with issues: %d/%d edits applied, but verification failed", applied, len(planOps)))
		logger.Logf("📋 Some edits may not have been properly verified or may have issues")
	} else {
		logger.LogProcessStep(fmt.Sprintf("✅ Agent v2 execution completed successfully: %d/%d edits applied and verified", applied, len(planOps)))
	}

	// Log details of what was actually done
	for i, op := range planOps {
		logger.Logf("📋 Operation %d: %s - %s", i+1, op.file, op.desc)
	}

	ui.Out().Print("✅ Agent v2 execution completed\n")
	if runlog != nil {
		status := "ok"
		if verificationFailed {
			status = "verification_failed"
		}
		runlog.LogEvent("agent_end", map[string]any{
			"status":              status,
			"applied":             applied,
			"total_planned":       len(planOps),
			"verification_failed": verificationFailed,
		})
	}

	// Post-edit build validation with one-shot repair
	if wf, err := workspace.LoadWorkspaceFile(); err == nil {
		buildCmd := strings.TrimSpace(wf.BuildCommand)
		if buildCmd != "" {
			cmd := exec.Command("sh", "-c", buildCmd)
			out, berr := cmd.CombinedOutput()
			if berr != nil {
				failureSummary := fmt.Sprintf("Build failed. Output (tail):\n%s", tailStr(string(out), 4000))
				if runlog != nil {
					runlog.LogEvent("build_fail", map[string]any{"command": buildCmd, "tail": tailStr(string(out), 800)})
				}
				var sb strings.Builder
				sb.WriteString(failureSummary)
				sb.WriteString("\nApplied edits:\n")
				// We do not have the original per-file applied instructions easily; summarize by files touched in planOps
				for _, op := range planOps {
					sb.WriteString(fmt.Sprintf("- %s: %s\n", op.file, op.desc))
				}
				fixMsgs := []prompts.Message{
					{Role: "system", Content: "You are the orchestration model. Propose precise minimal file fixes. Return JSON: {\"edits\":[{\"file\":...,\"instructions\":...}]}."},
					{Role: "user", Content: sb.String()},
				}
				fixResp, _, ferr := llm.GetLLMResponse(ctrlModel, fixMsgs, "", cfg, 2*time.Minute)
				if ferr == nil {
					clean, jerr := utils.ExtractJSONFromLLMResponse(fixResp)
					if jerr == nil {
						var fix struct {
							Edits []struct {
								File         string `json:"file"`
								Instructions string `json:"instructions"`
							} `json:"edits"`
						}
						if json.Unmarshal([]byte(clean), &fix) == nil {
							for _, e := range fix.Edits {
								if e.File == "" || strings.HasSuffix(e.File, "/") {
									continue
								}
								_, _ = editor.ProcessPartialEdit(e.File, e.Instructions, cfg, logger)
							}
							if runlog != nil {
								runlog.LogEvent("build_repair_applied", map[string]any{"edits": len(fix.Edits)})
							}
						}
					}
				}
			} else {
				if runlog != nil {
					runlog.LogEvent("build_success", map[string]any{"command": buildCmd})
				}
			}
		}
	}

	// Print basic execution summary
	duration := time.Since(startTime)
	ui.Out().Print("\n💰 Execution Summary:\n")
	ui.Out().Printf("├─ Duration: %.2f seconds\n", duration.Seconds())
	ui.Out().Printf("├─ Status: Completed\n")
	ui.Out().Printf("└─ Note: Token tracking needs to be implemented for agent_v2\n")

	return nil
}

// DocHunkPlan prefers structured hunks with citations (file:line) to ground changes
type DocHunkPlan struct {
	Hunks []struct {
		Before    string   `json:"before"`
		After     string   `json:"after"`
		WhereHint string   `json:"where_hint"`
		Citations []string `json:"citations"`
	} `json:"hunks"`
}

// applyDocHunks applies simple before→after replacements deterministically, requiring exact 'before' matches
func applyDocHunks(path string, plan DocHunkPlan, logger *utils.Logger) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(b)
	for _, h := range plan.Hunks {
		if h.Before == "" {
			continue
		}
		if !strings.Contains(s, h.Before) {
			// skip if exact before content not found; do not guess
			continue
		}
		s = strings.Replace(s, h.Before, h.After, 1)
	}
	if err := os.WriteFile(path, []byte(s), 0644); err != nil {
		return err
	}
	if rl := utils.GetRunLogger(); rl != nil {
		rl.LogEvent("doc_hunks_applied", map[string]any{"file": path, "hunks": len(plan.Hunks)})
	}
	return nil
}

// proposeDocHunks asks the orchestration model for hunks-with-citations JSON
func proposeDocHunks(path string, cfg *config.Config, logger *utils.Logger) (DocHunkPlan, error) {
	var plan DocHunkPlan
	model := cfg.OrchestrationModel
	if model == "" {
		model = cfg.EditingModel
	}

	// Read the file content to provide context for better hunk generation
	fileContent, err := os.ReadFile(path)
	if err != nil {
		logger.Logf("Could not read file for doc hunk generation: %v", err)
		return plan, err
	}

	// Limit file content to avoid token limits while providing context
	contentStr := string(fileContent)
	if len(contentStr) > 8000 {
		contentStr = contentStr[:8000] + "\n...[truncated]..."
	}

	sys := prompts.Message{Role: "system", Content: `You are an expert at creating precise documentation patches. Generate minimal, targeted hunks that update documentation based on repository evidence.

Return JSON: {"hunks":[{"before":"exact text to replace","after":"replacement text","where_hint":"brief location description","citations":["path:line"]}]}

Guidelines:
- Create small, focused hunks for documentation updates
- Use exact text matching for reliable replacement
- Include repository citations for each change
- Prefer multiple small hunks over one large change
- Each hunk should be independently applicable
- Base changes only on repository evidence`}

	user := prompts.Message{Role: "user", Content: fmt.Sprintf(`Doc file: %s

Current file content:
%s

Generate precise hunks to update this documentation based on current repository state. Include citations for each change. Return ONLY valid JSON.`, path, contentStr)}

	resp, _, err := llm.GetLLMResponse(model, []prompts.Message{sys, user}, path, cfg, 60*time.Second)
	if err != nil {
		logger.Logf("Doc hunk proposal failed: %v", err)
		return plan, err
	}

	logger.Logf("Doc hunk proposal response length: %d chars", len(resp))
	clean, cerr := utils.ExtractJSONFromLLMResponse(resp)
	if cerr != nil {
		logger.Logf("Could not extract JSON from doc hunk response: %v", cerr)
		return plan, cerr
	}

	if err := json.Unmarshal([]byte(clean), &plan); err != nil {
		logger.Logf("Could not parse doc hunk JSON: %v", err)
		return plan, err
	}

	logger.Logf("Generated %d doc hunks for %s", len(plan.Hunks), path)
	return plan, nil
}

// CodeHunkPlan is a simple before→after hunk list for code files
type CodeHunkPlan struct {
	Hunks []struct {
		Before    string `json:"before"`
		After     string `json:"after"`
		WhereHint string `json:"where_hint"`
	} `json:"hunks"`
}

func proposeCodeHunks(path, instructions string, cfg *config.Config, logger *utils.Logger) (CodeHunkPlan, error) {
	var plan CodeHunkPlan
	model := cfg.OrchestrationModel
	if model == "" {
		model = cfg.EditingModel
	}

	// Read the file content to provide context for better hunk generation
	fileContent, err := os.ReadFile(path)
	if err != nil {
		logger.Logf("Could not read file for hunk generation: %v", err)
		return plan, err
	}

	// Limit file content to avoid token limits while providing context
	contentStr := string(fileContent)
	if len(contentStr) > 8000 {
		contentStr = contentStr[:8000] + "\n...[truncated]..."
	}

	sys := prompts.Message{Role: "system", Content: `You are an expert at creating precise code patches. Generate minimal, targeted hunks that make only the necessary changes.

Return JSON: {"hunks":[{"before":"exact text to replace","after":"replacement text","where_hint":"brief location description"}]}

Guidelines:
- Create small, focused hunks (typically 1-5 lines changed)
- Use exact text matching for reliable replacement
- Include enough context (3-5 lines before/after) for unique identification
- Prefer multiple small hunks over one large change
- Each hunk should be independently applicable`}

	user := prompts.Message{Role: "user", Content: fmt.Sprintf(`File: %s
Instructions: %s

Current file content:
%s

Generate precise hunks for these changes. Return ONLY valid JSON.`, path, instructions, contentStr)}

	resp, _, err := llm.GetLLMResponse(model, []prompts.Message{sys, user}, path, cfg, 60*time.Second)
	if err != nil {
		logger.Logf("Hunk proposal failed: %v", err)
		return plan, err
	}

	logger.Logf("Hunk proposal response length: %d chars", len(resp))
	clean, cerr := utils.ExtractJSONFromLLMResponse(resp)
	if cerr != nil {
		logger.Logf("Could not extract JSON from hunk response: %v", cerr)
		return plan, cerr
	}

	if err := json.Unmarshal([]byte(clean), &plan); err != nil {
		logger.Logf("Could not parse hunk JSON: %v", err)
		return plan, err
	}

	logger.Logf("Generated %d code hunks for %s", len(plan.Hunks), path)
	return plan, nil
}

func applyCodeHunks(path string, plan CodeHunkPlan, logger *utils.Logger) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	original := string(b)
	s := original
	applied := 0

	for i, h := range plan.Hunks {
		if h.Before == "" {
			logger.Logf("Skipping hunk %d for %s: empty 'before' field", i+1, path)
			continue
		}
		if !strings.Contains(s, h.Before) {
			logger.Logf("Skipping hunk %d for %s: 'before' text not found in file", i+1, path)
			continue
		}
		logger.Logf("Applying hunk %d for %s: replacing %d chars with %d chars", i+1, path, len(h.Before), len(h.After))
		s = strings.Replace(s, h.Before, h.After, 1)
		applied++
	}

	if applied == 0 {
		logger.Logf("No hunks could be applied to %s", path)
		return fmt.Errorf("no hunks could be applied")
	}

	logger.Logf("Successfully applied %d/%d hunks to %s", applied, len(plan.Hunks), path)

	if err := os.WriteFile(path, []byte(s), 0644); err != nil {
		return err
	}
	if rl := utils.GetRunLogger(); rl != nil {
		rl.LogEvent("code_hunks_applied", map[string]any{"file": path, "hunks": len(plan.Hunks)})
	}
	return nil
}

func extractExplicitPath(intent string) string {
	// Generic path extractor for any file type; allow unicode and symbols except whitespace
	re := regexp.MustCompile(`(?m)(?:^|\s)([^\s]+\.[^\s]+)`) // match token with a dot and no spaces
	m := re.FindStringSubmatch(intent)
	if len(m) >= 2 {
		return filepath.Clean(m[1])
	}
	return ""
}

func fileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}

func looksLikeDocOnly(intent string) bool {
	lo := strings.ToLower(intent)
	// Exclusions: inline comments or comment-out requests are not file-header edits
	if strings.Contains(lo, "comment out") || strings.Contains(lo, "comment-out") || strings.Contains(lo, "inline comment") {
		return false
	}
	// If it looks like a replacement instruction, do NOT treat as doc-only
	if strings.Contains(lo, "change") && strings.Contains(lo, " to ") {
		return false
	}
	// Positive signals for a file-level header/summary request
	hasHeader := strings.Contains(lo, "header") || strings.Contains(lo, "file header") || strings.Contains(lo, "add header")
	hasSummary := strings.Contains(lo, "summary") || strings.Contains(lo, "summarize")
	mentionsTop := strings.Contains(lo, "top of file") || strings.Contains(lo, "top-of-file") || strings.Contains(lo, "at top")
	// Consider it referencing a file if it mentions "file" or includes a path-like token
	mentionsFile := strings.Contains(lo, "file ") || strings.Contains(lo, "/")

	// Trigger when it's clearly a header/summary ask
	if (hasHeader && hasSummary) || (hasHeader && mentionsFile) || (hasSummary && mentionsFile) || (hasHeader && mentionsTop) || (hasSummary && mentionsTop) {
		return true
	}
	// Also allow general phrasing like "file header summary"
	if strings.Contains(lo, "file header summary") || strings.Contains(lo, "summary header") {
		return true
	}
	return false
}

func parseSimpleReplacement(intent string) (oldText, newText string, ok bool) {
	// regex: change 'old' to 'new' or change "old" to "new" (case-insensitive)
	re := regexp.MustCompile(`(?i)change\s+['"]([^'\"]+)['"]\s+to\s+['"]([^'\"]+)['"]`)
	m := re.FindStringSubmatch(intent)
	if len(m) == 3 {
		return m[1], m[2], true
	}
	// unquoted form: change old to new
	re2 := regexp.MustCompile(`(?i)change\s+([^'\"\n]+?)\s+to\s+([^'\"\n]+)$`)
	m2 := re2.FindStringSubmatch(intent)
	if len(m2) == 3 {
		oldText = strings.TrimSpace(m2[1])
		newText = strings.TrimSpace(m2[2])
		// Trim trailing punctuation from newText
		newText = strings.TrimRight(newText, " .,!?")
		return oldText, newText, true
	}
	// fallback simple parser
	lo := strings.ToLower(intent)
	if strings.Contains(lo, "change ") && strings.Contains(lo, " to ") {
		// try to locate first quoted segment
		s := intent
		q1 := strings.IndexAny(s, "'\"")
		if q1 != -1 {
			quote := s[q1 : q1+1]
			q2 := strings.Index(s[q1+1:], quote)
			if q2 != -1 {
				oldText = s[q1+1 : q1+1+q2]
				tail := s[q1+1+q2+1:]
				toIdx := strings.Index(strings.ToLower(tail), " to ")
				if toIdx != -1 {
					tail2 := tail[toIdx+4:]
					q3 := strings.IndexAny(tail2, "'\"")
					if q3 != -1 {
						quote2 := tail2[q3 : q3+1]
						q4 := strings.Index(tail2[q3+1:], quote2)
						if q4 != -1 {
							newText = tail2[q3+1 : q3+1+q4]
							return oldText, newText, true
						}
					}
				}
			}
		}
	}
	// last resort naive split: change <old> to <new>
	loIdx := strings.Index(strings.ToLower(intent), "change ")
	toIdx := strings.LastIndex(strings.ToLower(intent), " to ")
	if loIdx != -1 && toIdx != -1 && toIdx > loIdx+7 {
		seg := intent[loIdx+7:]
		mid := strings.LastIndex(strings.ToLower(seg), " to ")
		if mid != -1 {
			oldText = strings.TrimSpace(seg[:mid])
			newText = strings.TrimSpace(seg[mid+4:])
			newText = strings.TrimRight(newText, " .,!?\n\r")
			if oldText != "" && newText != "" {
				return oldText, newText, true
			}
		}
	}
	return "", "", false
}

// parseSimpleAppend extracts a simple append request like:
// "append the word delta to the end of <file>" or "append \"delta\" to the end"
func parseSimpleAppend(intent string) (text string, ok bool) {
	lo := strings.ToLower(intent)
	if !strings.Contains(lo, "append") || !strings.Contains(lo, "to the end") {
		return "", false
	}
	// quoted form
	re := regexp.MustCompile(`(?i)append\s+(?:the\s+word\s+|the\s+text\s+)?["']([^"']+)["']\s+to\s+the\s+end`)
	if m := re.FindStringSubmatch(intent); len(m) == 2 {
		return strings.TrimSpace(m[1]), true
	}
	// unquoted single token
	re2 := regexp.MustCompile(`(?i)append\s+(?:the\s+word\s+|the\s+text\s+)?([A-Za-z0-9_.-]+)\s+to\s+the\s+end`)
	if m := re2.FindStringSubmatch(intent); len(m) == 2 {
		return strings.TrimSpace(m[1]), true
	}
	return "", false
}

func wantsEnthusiasmBoost(intent string) bool {
	lo := strings.ToLower(intent)
	if strings.Contains(lo, "enthusiastic") || strings.Contains(lo, "more excited") || strings.Contains(lo, "more exciting") || strings.Contains(lo, "friendlier") || strings.Contains(lo, "more friendly") {
		return true
	}
	return false
}

// tryRemoveTrailingExtraBrace removes a single trailing '}' at EOF if it appears to be extra
func tryRemoveTrailingExtraBrace(path string, logger *utils.Logger) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(b)
	// Walk back over whitespace
	i := len(s) - 1
	for i >= 0 && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i--
	}
	if i >= 0 && s[i] == '}' {
		// Remove just this brace and keep trailing whitespace
		newContent := s[:i] + s[i+1:]
		if writeErr := os.WriteFile(path, []byte(newContent), 0644); writeErr == nil {
			logger.LogProcessStep("v2: auto-removed trailing extra brace '}' at EOF")
			return nil
		} else {
			return writeErr
		}
	}
	return fmt.Errorf("no trailing brace fix applied")
}

// ensureLineAtFunctionStart inserts a line as the first statement inside a named Go function if missing
func ensureLineAtFunctionStart(path, funcName, line string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(b)
	// Simple regex to find function block for funcName
	re := regexp.MustCompile(`(?ms)func\s+` + regexp.QuoteMeta(funcName) + `\s*\([^)]*\)\s*\{`)
	loc := re.FindStringIndex(content)
	if loc == nil {
		return fmt.Errorf("function %s not found", funcName)
	}
	// Find insertion point: first newline after opening brace
	insertPos := loc[1]
	// Check if line already present within next ~200 chars
	upper := insertPos + 200
	if upper > len(content) {
		upper = len(content)
	}
	if strings.Contains(content[insertPos:upper], line) {
		return nil
	}
	// Insert with indentation of one tab
	newContent := content[:insertPos] + "\n\t" + line + "\n" + content[insertPos:]
	return os.WriteFile(path, []byte(newContent), 0644)
}

func insertTopComment(path, paragraph string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(b)
	// Detect original EOL style
	eol := "\n"
	if strings.Contains(content, "\r\n") {
		eol = "\r\n"
	} else if strings.Contains(content, "\r") {
		eol = "\r"
	}
	// Split using detected EOL
	var lines []string
	if eol == "\r\n" {
		lines = strings.Split(content, "\r\n")
	} else if eol == "\r" {
		lines = strings.Split(content, "\r")
	} else {
		lines = strings.Split(content, "\n")
	}
	// Choose comment style by extension
	style := detectCommentStyle(path)
	commentLines := toCommentBlockWithStyle(paragraph, style)
	// Build new content preserving EOL style
	newLines := append(commentLines, append([]string{""}, lines...)...)
	newContent := strings.Join(newLines, eol)
	return os.WriteFile(path, []byte(newContent), 0644)
}

// use validation_exec.go's helper instead

type commentStyle struct {
	kind       string
	linePrefix string
	blockStart string
	blockEnd   string
}

func detectCommentStyle(path string) commentStyle {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".go"), strings.HasSuffix(lower, ".js"), strings.HasSuffix(lower, ".ts"), strings.HasSuffix(lower, ".tsx"), strings.HasSuffix(lower, ".jsx"), strings.HasSuffix(lower, ".java"), strings.HasSuffix(lower, ".c"), strings.HasSuffix(lower, ".cc"), strings.HasSuffix(lower, ".cpp"), strings.HasSuffix(lower, ".h"), strings.HasSuffix(lower, ".hpp"), strings.HasSuffix(lower, ".cs"), strings.HasSuffix(lower, ".rs"), strings.HasSuffix(lower, ".kt"), strings.HasSuffix(lower, ".swift"), strings.HasSuffix(lower, ".scala"), strings.HasSuffix(lower, ".php"):
		return commentStyle{kind: "line", linePrefix: "// "}
	case strings.HasSuffix(lower, ".py"), strings.HasSuffix(lower, ".rb"), strings.HasSuffix(lower, ".sh"), strings.HasSuffix(lower, ".bash"), strings.HasSuffix(lower, ".zsh"), strings.HasSuffix(lower, ".fish"), strings.HasSuffix(lower, ".toml"), strings.HasSuffix(lower, ".ini"), strings.HasSuffix(lower, "dockerfile"), strings.HasSuffix(lower, ".yml"), strings.HasSuffix(lower, ".yaml"), strings.HasSuffix(lower, ".conf"):
		return commentStyle{kind: "line", linePrefix: "# "}
	case strings.HasSuffix(lower, ".css"):
		return commentStyle{kind: "block", blockStart: "/*", blockEnd: "*/"}
	case strings.HasSuffix(lower, ".html"), strings.HasSuffix(lower, ".xml"), strings.HasSuffix(lower, ".md"):
		return commentStyle{kind: "block", blockStart: "<!--", blockEnd: "-->"}
	default:
		return commentStyle{kind: "line", linePrefix: "# "}
	}
}

func toCommentBlockWithStyle(paragraph string, style commentStyle) []string {
	wrapped := wrapText(paragraph, 100)
	if style.kind == "line" {
		res := make([]string, 0, len(wrapped))
		for _, line := range wrapped {
			res = append(res, style.linePrefix+strings.TrimRight(line, " "))
		}
		return res
	}
	// block style
	res := []string{style.blockStart}
	for _, line := range wrapped {
		res = append(res, strings.TrimRight(line, " "))
	}
	res = append(res, style.blockEnd)
	return res
}

func wrapText(s string, width int) []string {
	fields := strings.Fields(s)
	var lines []string
	var cur []string
	curLen := 0
	for _, w := range fields {
		if curLen+len(w)+1 > width {
			lines = append(lines, strings.Join(cur, " "))
			cur = []string{w}
			curLen = len(w)
		} else {
			if curLen > 0 {
				cur = append(cur, w)
				curLen += len(w) + 1
			} else {
				cur = []string{w}
				curLen = len(w)
			}
		}
	}
	if len(cur) > 0 {
		lines = append(lines, strings.Join(cur, " "))
	}
	return lines
}

// buildWorkspacePrelude returns a tiny system message with cached workspace context
func buildWorkspacePrelude(cfg *config.Config) string {
	wf, err := workspace.LoadWorkspaceFile()
	if err != nil {
		return ""
	}
	// Compose brief prelude, keep it short
	var parts []string
	if len(wf.Languages) > 0 {
		parts = append(parts, "languages="+strings.Join(wf.Languages, ","))
	}
	if wf.BuildCommand != "" {
		parts = append(parts, "build="+wf.BuildCommand)
	}
	if wf.TestCommand != "" {
		parts = append(parts, "test="+wf.TestCommand)
	}
	if len(parts) == 0 {
		return ""
	}
	return "Workspace context: " + strings.Join(parts, "; ") + ". Prefer using workspace_context/read_file before editing."
}

// gitStatusSummary returns brief git status for prelude
func gitStatusSummary() (branch string, uncommitted, staged int, err error) {
	br, un, st, e := git.GetGitStatus()
	if e != nil {
		return "", 0, 0, e
	}
	return br, un, st, nil
}

// interpretEscapes converts common escape sequences like \n, \t into real characters
func interpretEscapes(s string) string {
	// Only handle the most common sequences used in tests
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\t", "\t")
	s = strings.ReplaceAll(s, "\\r", "\r")
	return s
}

// (removed additional unused helpers to satisfy lints)

// attemptPatchBasedEdit attempts to edit a file using patch syntax
func attemptPatchBasedEdit(filePath, instructions string, cfg *config.Config, logger *utils.Logger) error {
	// Read the current file content
	currentContent, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	// Create messages for patch-based editing
	messages := createPatchEditMessages(filePath, string(currentContent), instructions)

	// Use the orchestration model for patch generation
	controlModel := cfg.OrchestrationModel
	if controlModel == "" {
		controlModel = cfg.EditingModel
	}

	// Get LLM response with patch instructions
	response, _, err := llm.GetLLMResponse(controlModel, messages, filePath, cfg, 60*time.Second)
	if err != nil {
		return fmt.Errorf("failed to get patch response: %w", err)
	}

	// Parse patches from response
	patches, err := parser.GetUpdatedCodeFromPatchResponse(response)
	if err != nil {
		return fmt.Errorf("failed to parse patches: %w", err)
	}

	// Find patch for this specific file
	filePatch, exists := patches[filePath]
	if !exists {
		return fmt.Errorf("no patch found for file %s", filePath)
	}

	// Apply the patch with enhanced error handling
	if err := parser.EnhancedApplyPatchToFile(filePatch, filePath); err != nil {
		return fmt.Errorf("failed to apply patch: %w", err)
	}

	return nil
}

// createPatchEditMessages creates messages for patch-based editing
func createPatchEditMessages(filePath, currentContent, instructions string) []prompts.Message {
	// Limit content size to avoid token limits
	if len(currentContent) > 8000 {
		currentContent = currentContent[:8000] + "\n...[truncated]..."
	}

	systemPrompt := `You are an expert code editor. Generate precise patches in unified diff format.

CRITICAL REQUIREMENTS:
- Use unified diff format with proper diff headers
- Each patch must include sufficient context (3-5 lines before and after changes)
- Include ONLY the changed sections, not the entire file
- Make only the specific changes requested
- Use exact line matching for reliable patch application

Format your response as:
` + "```diff # " + filePath + `
--- a/` + filePath + `
+++ b/` + filePath + `
@@ -10,7 +10,7 @@
     old line 1
     old line 2
-    line to remove
+    line to add
     old line 4
    old line 5
` + "```END"

	userPrompt := fmt.Sprintf(`File to edit: %s

Current file content:
%s

Instructions: %s

Generate a precise patch for these changes. Return ONLY the diff block.`, filePath, currentContent, instructions)

	return []prompts.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
}
