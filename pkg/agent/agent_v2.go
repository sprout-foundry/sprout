package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
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
func RunAgentModeV2(userIntent string, skipPrompt bool, model string) error {
	// TODO: We should be able to be more intelligent about finding the correct file.
	fmt.Printf("🤖 Agent v2 mode: Tool-driven execution\n")
	fmt.Printf("🎯 Intent: %s\n", userIntent)

	cfg, err := config.LoadOrInitConfig(skipPrompt)
	if err != nil {
		logger := utils.GetLogger(false)
		logger.LogError(fmt.Errorf("failed to load config: %w", err))
		return fmt.Errorf("failed to load config: %w", err)
	}
	logger := utils.GetLogger(cfg.SkipPrompt)
	// Ensure interactive tool-calling enabled for v2 planner/executor/evaluator usage
	cfg.Interactive = true
	cfg.CodeToolsEnabled = true

	// TODO: We should be able to be more intelligent about finding the correct file. if the user mentions a file, but doesn't include a path, we should be able to grep to find the likely file and use file contents to validate that it could be referenced file.
	target := extractExplicitPath(userIntent)
	if target == "" || !fileExists(target) {
		// No explicit file path: fall back to planner→executor→evaluator interactive flow
		// Route control/editing models based on task type (rough heuristic) and small size
		category := "code"
		lo := strings.ToLower(userIntent)
		if strings.Contains(lo, "comment") || strings.Contains(lo, "docs") || strings.Contains(lo, "summary") || strings.Contains(lo, "header") {
			category = "docs"
		}
		controlModel, _, _ := llm.RouteModels(cfg, category, len(userIntent))
		msgs := []prompts.Message{{Role: "user", Content: fmt.Sprintf("Goal: %s", userIntent)}}
		// Minimal legacy context handler (unused in tool-first flow)
		ch := func(reqs []llm.ContextRequest, cfg *config.Config) (string, error) {
			return "", nil
		}
		logger.LogProcessStep("v2: invoking interactive planner→executor→evaluator loop")
		if _, err := llm.CallLLMWithInteractiveContext(controlModel, msgs, "", cfg, 6*time.Minute, ch); err != nil {
			logger.LogError(fmt.Errorf("interactive v2 flow failed: %w", err))
			return err
		}
		fmt.Printf("✅ Agent v2 execution completed\n")
		return nil
	}

	// Preflight: ensure file exists and is writable
	if err := preflightQuick(target); err != nil {
		logger.LogError(fmt.Errorf("preflight failed: %w", err))
		return err
	}

	// Deterministic micro edit: support pattern "change 'old' to 'new'" (executes via partial edit path)
	if oldText, newText, ok := parseSimpleReplacement(userIntent); ok {
		// Construct minimal instructions for partial edit
		instr := fmt.Sprintf("Replace the first occurrence of '%s' with '%s'. Do not modify anything else.", oldText, newText)
		if _, err := editor.ProcessPartialEdit(target, instr, cfg, logger); err != nil {
			return fmt.Errorf("micro_edit failed: %w", err)
		}
		logger.LogProcessStep(fmt.Sprintf("v2: micro_edit applied to %s (replaced '%s' → '%s')", target, oldText, newText))
		// Validate with a quick build
		cmd := exec.Command("go", "build", "./...")
		out, err := cmd.CombinedOutput()
		if err != nil {
			logger.LogProcessStep("❌ Validation failed after micro_edit")
			return fmt.Errorf("build failed: %v\nOutput: %s", err, string(out))
		}
		logger.LogProcessStep("✅ Validation passed after micro_edit")
		fmt.Printf("✅ Agent v2 execution completed\n")
		return nil
	}

	// If intent looks like a doc-only header/summary request, add header via LLM (only when no replacement pattern found)
	if looksLikeDocOnly(userIntent) && !strings.Contains(strings.ToLower(userIntent), "change ") {
		// Idempotence: skip if file already starts with a comment block
		if fileStartsWithComment(target) {
			logger.LogProcessStep("v2: header already present; skipping doc insertion")
			fmt.Printf("✅ Agent v2 execution completed\n")
			return nil
		}
		logger.LogProcessStep("v2: generating file header summary via LLM")
		header := buildSummaryParagraph(target, cfg)
		if err := insertTopComment(target, header); err != nil {
			logger.LogError(fmt.Errorf("failed to insert comment in %s: %w", target, err))
			return err
		}
		logger.LogProcessStep(fmt.Sprintf("v2: added summary comment to %s", target))
		fmt.Printf("✅ Agent v2 execution completed\n")
		return nil
	}

	// Fallback: run planner→executor→evaluator interactive flow for general tasks
	category2 := "code"
	lo2 := strings.ToLower(userIntent)
	if strings.Contains(lo2, "comment") || strings.Contains(lo2, "docs") || strings.Contains(lo2, "summary") || strings.Contains(lo2, "header") {
		category2 = "docs"
	}
	controlModel, _, _ := llm.RouteModels(cfg, category2, len(userIntent))
	msgs := []prompts.Message{{Role: "user", Content: fmt.Sprintf("Goal: %s", userIntent)}}
	ch := func(reqs []llm.ContextRequest, cfg *config.Config) (string, error) { return "", nil }
	logger.LogProcessStep("v2: invoking interactive planner→executor→evaluator loop (fallback)")
	if _, err := llm.CallLLMWithInteractiveContext(controlModel, msgs, target, cfg, 6*time.Minute, ch); err != nil {
		logger.LogError(fmt.Errorf("interactive v2 flow failed: %w", err))
		return err
	}
	fmt.Printf("✅ Agent v2 execution completed\n")
	return nil
}

func extractExplicitPath(intent string) string {
	// Generic path extractor for any file type; not Go-specific
	re := regexp.MustCompile(`(?m)(?:^|\s)([\w./-]+\.[\w]+)`) // crude local path matcher with extension
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
