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
		controlModel := cfg.SummaryModel
		if controlModel == "" {
			controlModel = cfg.OrchestrationModel
		}
		if controlModel == "" {
			controlModel = model
		}
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

	// If intent looks like a doc-only comment change, add header (only when no replacement pattern found)
	if looksLikeDocOnly(userIntent) && !strings.Contains(strings.ToLower(userIntent), "change ") {
		// Idempotence: skip if file already starts with a comment block
		if goFileStartsWithComment(target) {
			logger.LogProcessStep("v2: header already present; skipping doc insertion")
			fmt.Printf("✅ Agent v2 execution completed\n")
			return nil
		}
		header := buildSummaryParagraph(target)
		if err := insertTopComment(target, header); err != nil {
			logger.LogError(fmt.Errorf("failed to insert comment in %s: %w", target, err))
			return err
		}
		logger.LogProcessStep(fmt.Sprintf("v2: added summary comment to %s", target))
		fmt.Printf("✅ Agent v2 execution completed\n")
		return nil
	}

	// Fallback: run planner→executor→evaluator interactive flow for general tasks
	controlModel := cfg.SummaryModel
	if controlModel == "" {
		controlModel = cfg.OrchestrationModel
	}
	if controlModel == "" {
		controlModel = model
	}
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

func buildSummaryParagraph(path string) string {
	// TODO: This should use an llm t summarize. The summarize model would be a good candidate.
	base := filepath.Base(path)
	dir := filepath.Dir(path)
	return fmt.Sprintf("%s provides utilities for the %s package. It contains functions and helpers used across the codebase. This comment summarizes the file’s purpose and typical usage.", base, dir)
}

func looksLikeDocOnly(intent string) bool {
	lo := strings.ToLower(intent)
	// If it looks like a replacement instruction, do NOT treat as doc-only
	if strings.Contains(lo, "change") && strings.Contains(lo, " to ") {
		return false
	}
	return strings.Contains(lo, "comment") || strings.Contains(lo, "doc") || strings.Contains(lo, "summary") || strings.Contains(lo, "header")
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
	lines := strings.Split(content, "\n")
	commentLines := toCommentBlock(paragraph)
	newContent := strings.Join(append(commentLines, append([]string{""}, lines...)...), "\n")
	return os.WriteFile(path, []byte(newContent), 0644)
}

// use validation_exec.go's helper instead

func toCommentBlock(paragraph string) []string {
	wrapped := wrapText(paragraph, 100)
	res := make([]string, 0, len(wrapped))
	for _, line := range wrapped {
		res = append(res, "// "+strings.TrimRight(line, " "))
	}
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
