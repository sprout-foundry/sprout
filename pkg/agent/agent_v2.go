//go:build !agent2refactor

package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	pb "github.com/alantheprice/ledit/pkg/agent/playbooks"
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
	// Select a playbook when possible
	var planOps []struct{ file, instructions, desc string }
	if intentAnalysis != nil {
		if sel := pb.Select(userIntent, intentAnalysis.Category); sel != nil {
			logger.LogProcessStep("📘 Using playbook: " + sel.Name())
			spec := sel.BuildPlan(userIntent, intentAnalysis.EstimatedFiles)
			if spec != nil {
				// Convert spec ops → planOps, filter to real files only
				for _, op := range spec.Ops {
					// Skip directories; only operate on files
					if st, err := os.Stat(op.FilePath); err == nil && !st.IsDir() {
						planOps = append(planOps, struct{ file, instructions, desc string }{file: op.FilePath, instructions: op.Instructions, desc: op.Description})
					}
				}
			}
		}
	}
	// If no playbook produced ops, fall back to interactive planning (still no edits at this point)
	if len(planOps) == 0 {
		controlModel := cfg.OrchestrationModel
		if controlModel == "" {
			controlModel = cfg.EditingModel
		}
		msgs := []prompts.Message{{Role: "user", Content: fmt.Sprintf("Goal: %s", userIntent)}}
		// Best-effort: include small workspace synopsis if available
		if wfSnap, err := workspace.LoadWorkspaceFile(); err == nil {
			if prelude, err := workspace.GetFullWorkspaceSummary(wfSnap, cfg.CodeStyle, cfg, logger); err == nil && prelude != "" {
				const maxPrelude = 16000
				if len(prelude) > maxPrelude {
					prelude = prelude[:maxPrelude]
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
		// Let the interactive loop propose a plan; we will still gate edits below
		ch := func(reqs []llm.ContextRequest, cfg *config.Config) (string, error) { return "", nil }
		logger.LogProcessStep("📝 Creating plan via interactive controller (no edits)")
		if _, err := llm.CallLLMWithInteractiveContext(controlModel, msgs, "", cfg, 5*time.Minute, ch); err != nil {
			logger.LogError(fmt.Errorf("interactive planning failed: %w", err))
		}
		// As a minimal fallback, try to ground on EstimatedFiles if present
		if intentAnalysis != nil && len(intentAnalysis.EstimatedFiles) > 0 {
			for _, f := range intentAnalysis.EstimatedFiles {
				if st, err := os.Stat(f); err == nil && !st.IsDir() {
					planOps = append(planOps, struct{ file, instructions, desc string }{file: f, instructions: userIntent, desc: "Apply requested change"})
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
	applied := 0
	for _, op := range planOps {
		if err := preflightQuick(op.file); err != nil {
			logger.Logf("Skipping %s (preflight failed: %v)", op.file, err)
			continue
		}
		// For documentation files, first generate a claim→citation map by asking for minimal reads
		if strings.HasSuffix(strings.ToLower(op.file), ".md") || strings.Contains(strings.ToLower(op.file), "/docs/") {
			verifyInstr := "Before editing, enumerate outdated claims in this doc and provide a claim→citation map with file:line references from this repository only. Use workspace_context/read_file to fetch minimal evidence. If evidence is insufficient, request more files. Then propose the precise changes."
			// Use interactive controller for verification to allow tool calls
			verifyModel := cfg.OrchestrationModel
			if verifyModel == "" {
				verifyModel = cfg.EditingModel
			}
			msgs := []prompts.Message{
				{Role: "system", Content: "You must verify documentation claims using repository files only. No external web sources."},
				{Role: "user", Content: fmt.Sprintf("Doc: %s\nTask: %s\n%s", op.file, op.desc, verifyInstr)},
			}
			ch := func(reqs []llm.ContextRequest, cfg *config.Config) (string, error) { return "", nil }
			if _, err := llm.CallLLMWithInteractiveContext(verifyModel, msgs, op.file, cfg, 3*time.Minute, ch); err != nil {
				logger.Logf("Verification step failed for %s: %v", op.file, err)
				// Continue but keep edit conservative
			}
			// Always try deterministic proposer for docs
			if plan, perr := proposeDocReplacements(op.file, cfg, logger); perr == nil && len(plan.Edits) > 0 {
				if directApply {
					if aerr := applyDocReplacements(op.file, plan, logger); aerr == nil {
						applied++
						continue
					}
				}
			} else if perr != nil {
				logger.Logf("Doc proposer failed for %s: %v", op.file, perr)
			}
		}
		if _, err := editor.ProcessPartialEdit(op.file, op.instructions, cfg, logger); err != nil {
			logger.Logf("Partial edit failed for %s: %v; trying full-file edit", op.file, err)
			if _, err2 := editor.ProcessCodeGeneration(op.file, op.instructions, cfg, ""); err2 != nil {
				logger.Logf("Full-file edit failed for %s: %v", op.file, err2)
				continue
			}
		}
		applied++
	}
	if applied == 0 {
		return fmt.Errorf("plan produced no successful edits")
	}
	ui.Out().Print("✅ Agent v2 execution completed\n")
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

func proposeDocReplacements(path string, cfg *config.Config, logger *utils.Logger) (DocReplacementPlan, error) {
	var plan DocReplacementPlan
	model := cfg.OrchestrationModel
	if model == "" {
		model = cfg.EditingModel
	}
	sys := prompts.Message{Role: "system", Content: "You output only JSON. Propose minimal, grounded documentation replacements as {\"edits\":[{\"find\":...,\"replace\":...,\"where_hint\":...}]} based solely on repository evidence. No prose."}
	user := prompts.Message{Role: "user", Content: fmt.Sprintf("Doc file: %s\nReturn only JSON with replacements.", path)}
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
	return os.WriteFile(path, []byte(s), 0644)
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
