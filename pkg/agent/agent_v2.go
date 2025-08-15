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
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
)

// tail returns last n chars of a string
func tail(s string, n int) string {
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
	logger := utils.GetLogger(cfg.SkipPrompt)
	runlog := utils.GetRunLogger()
	if runlog != nil {
		runlog.LogEvent("agent_start", map[string]any{"intent": userIntent, "orchestration_model": cfg.OrchestrationModel, "editing_model": cfg.EditingModel})
	}
	// Ensure interactive tool-calling enabled for v2 planner/executor/evaluator usage
	cfg.Interactive = true
	cfg.CodeToolsEnabled = true

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
	if wfSnap, err := workspace.LoadWorkspaceFile(); err == nil {
		if prelude, err := workspace.GetFullWorkspaceSummary(wfSnap, cfg.CodeStyle, cfg, logger); err == nil && prelude != "" {
			// Truncate to a safe size to avoid prompt bloat
			const maxPrelude = 16000
			if len(prelude) > maxPrelude {
				prelude = prelude[:maxPrelude]
			}
			// Log and surface as a planning prelude
			logger.LogProcessStep("Including workspace synopsis in planning prelude")
			// Attach as a system message in the interactive planning call below by augmenting msgs
		}
	}
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
		focusMsg := ""
		if len(focusList) > 0 {
			focusMsg = "Focus files (prefer these):\n" + strings.Join(focusList, "\n")
		}
		// Ask directly for the final JSON plan (no tool_calls) to avoid planner loops
		msgs := []prompts.Message{{Role: "system", Content: "Planner: Return ONLY the final JSON plan now: {\"edits\":[{\"file\":...,\"instructions\":...}]}. Do NOT use tool_calls in this message."}}
		if focusMsg != "" {
			msgs = append(msgs, prompts.Message{Role: "system", Content: focusMsg})
		}
		msgs = append(msgs, prompts.Message{Role: "user", Content: fmt.Sprintf("Goal: %s", userIntent)})
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
		logger.LogProcessStep("📝 Requesting final plan (no tool_calls)")
		resp, _, err := llm.GetLLMResponse(controlModel, msgs, "plan_request", cfg, 60*time.Second)
		if err == nil {
			clean, jerr := utils.ExtractJSONFromLLMResponse(resp)
			if jerr == nil && strings.Contains(clean, "\"edits\"") {
				var plan struct {
					Edits []struct {
						File, Instructions string `json:"file","instructions"`
					} `json:"edits"`
				}
				if json.Unmarshal([]byte(clean), &plan) == nil {
					for _, e := range plan.Edits {
						if e.File == "" || strings.HasSuffix(e.File, "/") {
							continue
						}
						if st, err := os.Stat(e.File); err == nil && !st.IsDir() {
							desc := "Apply requested change"
							planOps = append(planOps, struct{ file, instructions, desc string }{file: e.File, instructions: e.Instructions, desc: desc})
						}
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
		strict := []prompts.Message{{Role: "system", Content: "Planner: Return ONLY the final JSON plan now: {\"edits\":[{\"file\":...,\"instructions\":...}]} (no tool_calls). Instructions must be concrete and minimal. For docs, only propose edits you can support with citations; for code, target a specific function/section."}}
		if focusMsg != "" {
			strict = append(strict, prompts.Message{Role: "system", Content: focusMsg})
		}
		strict = append(strict, prompts.Message{Role: "user", Content: fmt.Sprintf("Goal: %s", userIntent)})
		resp2, _, err2 := llm.GetLLMResponse(controlModel, strict, "plan_request_strict", cfg, 45*time.Second)
		if err2 == nil {
			clean2, jerr2 := utils.ExtractJSONFromLLMResponse(resp2)
			if jerr2 == nil && strings.Contains(clean2, "\"edits\"") {
				var plan2 struct {
					Edits []struct {
						File, Instructions string `json:"file","instructions"`
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

	// If this is a docs/README intent and still no actionable doc edit, seed a README plan
	if len(planOps) == 0 {
		lo := strings.ToLower(userIntent)
		if strings.Contains(lo, "readme") || strings.Contains(lo, "docs") || strings.Contains(lo, "documentation") {
			candidates := []string{"README.md", "docs/README.md", "docs/index.md"}
			var chosen string
			for _, c := range candidates {
				if st, err := os.Stat(c); err == nil && !st.IsDir() {
					chosen = c
					break
				}
			}
			if chosen != "" {
				instr := "Update README to include accurate usage for `ledit agent`, with minimal, precise changes grounded in current CLI."
				planOps = append(planOps, struct{ file, instructions, desc string }{file: chosen, instructions: instr, desc: "Seed README update"})
				if runlog != nil {
					runlog.LogEvent("plan_seed_readme", map[string]any{"file": chosen})
				}
			}
		}
	}

	if len(planOps) == 0 {
		logger.LogProcessStep("⚠️ No actionable plan produced; aborting without edits")
		return fmt.Errorf("no actionable plan produced for intent")
	}

	// Phase 2: Execute plan with guardrails (verify → edit; preflight each file, minimal edits per op)
	logger.LogProcessStep("🛠️ Executing plan with guarded edits...")
	if runlog != nil {
		runlog.LogEvent("phase", map[string]any{"name": "execution", "ops": len(planOps)})
	}
	applied := 0
	for _, op := range planOps {
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
			if strings.Contains(strings.ToLower(op.desc), "seed readme update") {
				if err := ensureReadmeUsage(op.file, logger); err == nil {
					applied++
					if runlog != nil {
						runlog.LogEvent("readme_usage_inserted", map[string]any{"file": op.file})
					}
					continue
				}
			}
			verifyInstr := "Before editing, enumerate outdated claims in this doc and provide a claim→citation map with file:line references from this repository only. Use workspace_context/read_file to fetch minimal evidence. If evidence is insufficient, request more files. Then propose the precise changes."
			// Use interactive controller for verification to allow tool calls
			verifyModel := cfg.OrchestrationModel
			if verifyModel == "" {
				verifyModel = cfg.EditingModel
			}
			msgs := []prompts.Message{
				{Role: "system", Content: "You must verify documentation claims using repository files only. No external web sources. Produce a claim→citation map (file:line) before proposing changes."},
				{Role: "user", Content: fmt.Sprintf("Doc: %s\nTask: %s\n%s", op.file, op.desc, verifyInstr)},
			}
			ch := func(reqs []llm.ContextRequest, cfg *config.Config) (string, error) { return "", nil }
			if _, err := llm.CallLLMWithInteractiveContext(verifyModel, msgs, op.file, cfg, 3*time.Minute, ch); err != nil {
				logger.Logf("Verification step failed for %s: %v", op.file, err)
				// Continue but keep edit conservative
			}
			// Prefer hunk-based plan with citations; apply deterministically when present
			if hplan, herr := proposeDocHunks(op.file, cfg, logger); herr == nil && len(hplan.Hunks) > 0 {
				allCited := true
				for _, h := range hplan.Hunks {
					if len(h.Citations) == 0 {
						allCited = false
						break
					}
				}
				if allCited {
					if aerr := applyDocHunks(op.file, hplan, logger); aerr == nil {
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

		// Non-doc files: attempt hunk-based code patch before editor fallback
		if !strings.HasSuffix(strings.ToLower(op.file), ".md") && !strings.Contains(strings.ToLower(op.file), "/docs/") {
			if chplan, cherr := proposeCodeHunks(op.file, op.instructions, cfg, logger); cherr == nil && len(chplan.Hunks) > 0 {
				if aerr := applyCodeHunks(op.file, chplan, logger); aerr == nil {
					applied++
					continue
				}
			}
		}
		diff, err := editor.ProcessPartialEdit(op.file, op.instructions, cfg, logger)
		if runlog != nil && strings.TrimSpace(diff) != "" {
			runlog.LogEvent("edit_partial_diff", map[string]any{"file": op.file, "diff_len": len(diff)})
		}
		if err != nil {
			logger.Logf("Partial edit failed for %s: %v; trying full-file edit", op.file, err)
			diff2, err2 := editor.ProcessCodeGeneration(op.file, op.instructions, cfg, "")
			if runlog != nil && strings.TrimSpace(diff2) != "" {
				runlog.LogEvent("edit_full_diff", map[string]any{"file": op.file, "diff_len": len(diff2)})
			}
			if err2 != nil {
				logger.Logf("Full-file edit failed for %s: %v", op.file, err2)
				if runlog != nil {
					runlog.LogEvent("edit_failure", map[string]any{"file": op.file, "error": err2.Error()})
				}
				continue
			}
		}
		applied++
	}
	if applied == 0 {
		if runlog != nil {
			runlog.LogEvent("agent_end", map[string]any{"status": "no_edits"})
		}
		return fmt.Errorf("plan produced no successful edits")
	}
	ui.Out().Print("✅ Agent v2 execution completed\n")
	if runlog != nil {
		runlog.LogEvent("agent_end", map[string]any{"status": "ok", "applied": applied})
	}

	// Post-edit build validation with one-shot repair
	if wf, err := workspace.LoadWorkspaceFile(); err == nil {
		buildCmd := strings.TrimSpace(wf.BuildCommand)
		if buildCmd != "" {
			cmd := exec.Command("sh", "-c", buildCmd)
			out, berr := cmd.CombinedOutput()
			if berr != nil {
				failureSummary := fmt.Sprintf("Build failed. Output (tail):\n%s", tail(string(out), 4000))
				if runlog != nil {
					runlog.LogEvent("build_fail", map[string]any{"command": buildCmd, "tail": tail(string(out), 800)})
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
								File, Instructions string `json:"file","instructions"`
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
	return nil
}

// proposeDocReplacements asks the orchestration model to return a deterministic JSON of find/replace edits.
type DocReplacementPlan struct {
	Edits []struct {
		Find      string `json:"find"`
		Replace   string `json:"replace"`
		WhereHint string `json:"where_hint"`
	} `json:"edits"`
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

func proposeDocReplacements(path string, cfg *config.Config, logger *utils.Logger) (DocReplacementPlan, error) {
	var plan DocReplacementPlan
	model := cfg.OrchestrationModel
	if model == "" {
		model = cfg.EditingModel
	}
	// Prefer hunk plan with citations; allow fallback to replacements
	sys := prompts.Message{Role: "system", Content: "You output only JSON. Prefer returning a hunk plan with citations: {\"hunks\":[{\"before\":...,\"after\":...,\"where_hint\":...,\"citations\":[\"path:line\", ...]}...]}. If you cannot produce hunks, return replacements as {\"edits\":[{\"find\":...,\"replace\":...,\"where_hint\":...}]}. Base changes solely on repository evidence. No prose."}
	user := prompts.Message{Role: "user", Content: fmt.Sprintf("Doc file: %s\nReturn ONLY JSON: hunks with citations preferred; otherwise replacements.", path)}
	resp, _, err := llm.GetLLMResponse(model, []prompts.Message{sys, user}, path, cfg, 45*time.Second)
	if err != nil {
		return plan, err
	}
	clean, cerr := utils.ExtractJSONFromLLMResponse(resp)
	if cerr != nil {
		return plan, cerr
	}
	// Try to parse hunk plan first; if valid and has citations, apply hunks instead of find/replace
	var hunkPlan DocHunkPlan
	if err := json.Unmarshal([]byte(clean), &hunkPlan); err == nil && len(hunkPlan.Hunks) > 0 {
		// Validate citations exist for each hunk
		allHaveCitations := true
		for _, h := range hunkPlan.Hunks {
			if len(h.Citations) == 0 {
				allHaveCitations = false
				break
			}
		}
		if allHaveCitations {
			if aerr := applyDocHunks(path, hunkPlan, utils.GetLogger(cfg.SkipPrompt)); aerr == nil {
				// Return empty plan to signal that hunks were applied successfully
				return DocReplacementPlan{}, nil
			}
			// if hunk apply fails, fall through to try replacements
		}
	}
	// Fallback: replacements
	if err := json.Unmarshal([]byte(clean), &plan); err != nil {
		return plan, err
	}
	return plan, nil
}

func applyDocReplacements(path string, plan DocReplacementPlan, logger *utils.Logger) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(b)
	for _, e := range plan.Edits {
		if e.Find == "" {
			continue
		}
		if !strings.Contains(s, e.Find) {
			continue
		}
		s = strings.Replace(s, e.Find, e.Replace, 1)
	}
	if err := os.WriteFile(path, []byte(s), 0644); err != nil {
		return err
	}
	if rl := utils.GetRunLogger(); rl != nil {
		rl.LogEvent("doc_replacements_applied", map[string]any{"file": path, "edits": len(plan.Edits)})
	}
	return nil
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
	sys := prompts.Message{Role: "system", Content: "You output only JSON.\nSchema: {\"hunks\":[{\"before\":string,\"after\":string,\"where_hint\":string,\"citations\":[\"path:line\"]}]}\nExample: {\"hunks\":[{\"before\":\"Old sentence.\",\"after\":\"New sentence.\",\"where_hint\":\"README intro\",\"citations\":[\"pkg/agent/agent_v2.go:120\"]}]}\nBase solely on repository evidence. No prose."}
	user := prompts.Message{Role: "user", Content: fmt.Sprintf("Doc file: %s\nReturn ONLY JSON: hunks-with-citations.", path)}
	resp, _, err := llm.GetLLMResponse(model, []prompts.Message{sys, user}, path, cfg, 45*time.Second)
	if err != nil {
		return plan, err
	}
	clean, cerr := utils.ExtractJSONFromLLMResponse(resp)
	if cerr != nil {
		return plan, cerr
	}
	if err := json.Unmarshal([]byte(clean), &plan); err != nil {
		return plan, err
	}
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
	sys := prompts.Message{Role: "system", Content: "You output only JSON. Return code hunks as {\"hunks\":[{\"before\":...,\"after\":...,\"where_hint\":...}]}. Base solely on the provided file and instructions. No prose."}
	user := prompts.Message{Role: "user", Content: fmt.Sprintf("Code file: %s\nInstructions: %s\nReturn ONLY JSON hunks.", path, instructions)}
	resp, _, err := llm.GetLLMResponse(model, []prompts.Message{sys, user}, path, cfg, 45*time.Second)
	if err != nil {
		return plan, err
	}
	clean, cerr := utils.ExtractJSONFromLLMResponse(resp)
	if cerr != nil {
		return plan, cerr
	}
	if err := json.Unmarshal([]byte(clean), &plan); err != nil {
		return plan, err
	}
	return plan, nil
}

func applyCodeHunks(path string, plan CodeHunkPlan, logger *utils.Logger) error {
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
			continue
		}
		s = strings.Replace(s, h.Before, h.After, 1)
	}
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

func buildSummaryParagraph(path string, cfg *config.Config) string {
	// Read file and send a concise summarization request to the summary/orchestration model
	base := filepath.Base(path)
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("%s: summary header.", base)
	}
	content := string(b)
	// Trim content to a safe size to control costs
	const maxChars = 16000
	if len(content) > maxChars {
		head := content[:8000]
		tail := content[len(content)-7000:]
		content = head + "\n... [truncated for summary]\n" + tail
	}
	// Choose a small/fast control model for summarization
	model := cfg.SummaryModel
	if model == "" {
		model = cfg.OrchestrationModel
	}
	if model == "" {
		model = cfg.EditingModel
	}
	sys := prompts.Message{Role: "system", Content: "You are a senior engineer. Produce a concise 1-2 sentence file header summarizing purpose and key responsibilities. No code fences, no markdown, no quotes. Keep under 220 characters."}
	user := prompts.Message{Role: "user", Content: fmt.Sprintf("File: %s\nPlease summarize this file succinctly for a header comment.\n\nBEGIN FILE CONTENT\n%s\nEND FILE CONTENT", base, content)}
	resp, _, err := llm.GetLLMResponse(model, []prompts.Message{sys, user}, path, cfg, 45*time.Second)
	if err != nil || strings.TrimSpace(resp) == "" {
		// Fallback generic summary on failure
		ext := filepath.Ext(path)
		dir := filepath.Base(filepath.Dir(path))
		return fmt.Sprintf("%s (%s) belongs to %s. This header summarizes the file’s purpose and typical usage.", base, ext, dir)
	}
	// Normalize whitespace and trim
	s := strings.TrimSpace(resp)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 260 {
		s = s[:260]
	}
	return s
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
	re := regexp.MustCompile(`(?i)change\s+['\"]([^'\"]+)['\"]\s+to\s+['\"]([^'\"]+)['\"]`)
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

// discoverLikelyTargetFile uses simple keyword search across workspace to find a likely file to edit
func discoverLikelyTargetFile(intent, root string) string {
	// Generic: derive keywords from intent only; no language/library-specific boosts
	lo := strings.ToLower(intent)
	words := strings.Fields(lo)
	var keywords []string
	for _, w := range words {
		w = strings.Trim(w, ":,.;!?")
		if len(w) >= 3 {
			keywords = append(keywords, w)
		}
	}
	if len(keywords) == 0 {
		return ""
	}
	info := &WorkspaceInfo{ProjectType: "other"}
	found := findFilesUsingShellCommands(strings.Join(keywords, " "), info, utils.GetLogger(true))
	if len(found) == 0 {
		return ""
	}
	// Deterministic choice: smallest path lexicographically
	best := found[0]
	for _, f := range found[1:] {
		if f < best {
			best = f
		}
	}
	return best
}

// replaceFirstInFile performs a literal first occurrence replacement in a file's content
func replaceFirstInFile(path, oldText, newText string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(b)
	idx := strings.Index(content, oldText)
	if idx == -1 {
		return fmt.Errorf("old text not found")
	}
	updated := content[:idx] + newText + content[idx+len(oldText):]
	return os.WriteFile(path, []byte(updated), 0644)
}

// interpretEscapes converts common escape sequences like \n, \t into real characters
func interpretEscapes(s string) string {
	// Only handle the most common sequences used in tests
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\t", "\t")
	s = strings.ReplaceAll(s, "\\r", "\r")
	return s
}

// removed Go-specific deterministic helpers to keep v2 language-agnostic

// ensureReadmeUsage appends a minimal Usage section for `ledit agent` if missing
func ensureReadmeUsage(path string, logger *utils.Logger) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(b)
	if strings.Contains(strings.ToLower(s), "ledit agent") {
		return fmt.Errorf("usage already present")
	}
	snippet := "\n## Usage\n\nRun:\n\n```bash\nledit agent \"Your intent here\"\n```\n"
	s = s + snippet
	return os.WriteFile(path, []byte(s), 0644)
}
