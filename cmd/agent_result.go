//go:build !js

package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// AgentResult is the structured output produced when --output-format=json is used.
// It captures everything a SaaS wrapper needs from a non-interactive sprout run.
type AgentResult struct {
	Status         string             `json:"status"`                     // "success", "error", "needs_input", or "handed_off"
	Error          string             `json:"error,omitempty"`            // error message if status=="error"
	Query          string             `json:"query"`                      // the original prompt
	Response       string             `json:"response,omitempty"`         // final assistant response text (success runs with an in-process agent)
	FilesModified  []string           `json:"files_modified,omitempty"`   // files changed during execution
	GitDiff        string             `json:"git_diff,omitempty"`         // unified diff of all changes
	DiffOmitted    []string           `json:"diff_omitted,omitempty"`     // files left out of GitDiff to keep it under maxDiffBytes
	PullRequestURL string             `json:"pull_request_url,omitempty"` // URL of PR created during execution
	Question       *AgentQuestion     `json:"question,omitempty"`         // the question that ended a "needs_input" run
	Handoff        string             `json:"handoff,omitempty"`          // a "handed_off" run's note for the next run
	Metrics        AgentResultMetrics `json:"metrics"`
}

// AgentQuestion is the question a run ended on (ask_user_ends_run): the host
// asks the person and continues the work with their answer.
type AgentQuestion struct {
	Question string   `json:"question"`
	Header   string   `json:"header,omitempty"`
	Options  []string `json:"options,omitempty"`
	Default  string   `json:"default,omitempty"`
}

// AgentResultMetrics holds execution metrics for structured output.
type AgentResultMetrics struct {
	ElapsedSeconds float64 `json:"elapsed_seconds"`
	TokensIn       int     `json:"tokens_in"`  // Total prompt/input tokens
	TokensOut      int     `json:"tokens_out"` // Total completion/output tokens
	LLMCalls       int     `json:"llm_calls"`  // Number of LLM API calls made
	Cost           float64 `json:"cost"`       // Total estimated USD cost
	Provider       string  `json:"provider"`   // LLM provider name (e.g., "openai", "anthropic")
	Model          string  `json:"model"`      // Model identifier (e.g., "gpt-4o")

	// Security telemetry — track post-caution LLM behavior so external tools
	// can measure SECURITY_CAUTION_REQUIRED signal effectiveness.
	SecurityCautionsIssued      int64 `json:"security_cautions_issued"`       // Times a SECURITY_CAUTION_REQUIRED was produced
	SecurityRetriesAfterCaution int64 `json:"security_retries_after_caution"` // Times the LLM retried the same blocked op after a caution
	SecurityLoopsDetected       int64 `json:"security_loops_detected"`        // Times loop-detection fired (3+ identical blocks)
}

// outputFormatJSON is the flag value for JSON output mode.
var outputFormatJSON bool

// outputPath, when set, redirects the structured JSON result to this file
// instead of stdout. It only takes effect when outputFormatJSON is also
// true. Keeping stdout free lets logs flow through the Fly machine log
// fallback path without being interleaved with the JSON payload.
var outputPath string

// maxDiffBytes is the maximum size of git diff output to include.
const maxDiffBytes = 1 << 20 // 1MB

func init() {
	agentCmd.Flags().BoolVar(&outputFormatJSON, "json", false, "Print a structured JSON result to stdout after execution (for CI/SaaS integration)")
	agentCmd.Flags().StringVarP(&outputPath, "output", "o", "", "Write the structured JSON result to this file instead of stdout (requires --json)")
	boolFlagAlias(agentCmd.Flags(), &outputFormatJSON, "output-json", "json", aliasSilent)
	stringFlagAlias(agentCmd.Flags(), &outputPath, "output-path", "output", aliasSilent)
	agentCmd.Flags().StringVar(&progressEventsTarget, "progress-events", "", "Emit one-line progress milestones to stderr, stdout, or a file path (e.g. --progress-events stderr)")
}

// emitJSONResult writes the AgentResult as indented JSON to stdout.
// It collects git diff and modified files from the workspace.
//
// Thread safety note: Metrics are read from the agent after it has finished
// executing (sequential access). The non-interactive --output-json mode guarantees
// the agent is idle when this function runs, so no mutex is needed for the
// promptTokens/completionTokens/llmCallCount reads. Do NOT call this from a
// goroutine that may run concurrently with agent execution.
func emitJSONResult(query string, startTime time.Time, runErr error, a *agent.Agent) {
	result := AgentResult{
		Query: query,
		Metrics: AgentResultMetrics{
			ElapsedSeconds: time.Since(startTime).Seconds(),
		},
	}

	// Populate metrics from the agent if available
	if a != nil {
		result.Metrics.TokensIn = a.GetPromptTokens()
		result.Metrics.TokensOut = a.GetCompletionTokens()
		result.Metrics.LLMCalls = a.GetLLMCallCount()
		result.Metrics.Cost = a.GetTotalCost()
		result.Metrics.Provider = a.GetProvider()
		result.Metrics.Model = a.GetModel()
		// Security telemetry
		result.Metrics.SecurityCautionsIssued = a.GetSecurityCautionsIssued()
		result.Metrics.SecurityRetriesAfterCaution = a.GetSecurityRetriesAfterCaution()
		result.Metrics.SecurityLoopsDetected = a.GetSecurityLoopsDetected()
	}

	if q := a.PendingQuestion(); q != nil {
		// The run stopped to ask: not a failure, and its work so far is kept.
		result.Status = "needs_input"
		result.Question = &AgentQuestion{Question: q.Question, Header: q.Header, Default: q.Default}
		for _, o := range q.Options {
			result.Question.Options = append(result.Question.Options, o.Label)
		}
	} else if runErr != nil {
		result.Status = "error"
		result.Error = runErr.Error()
	} else {
		result.Status = "success"
		// Only on success: on an error run the last assistant message may
		// belong to an earlier turn in a resumed session, which is not this
		// query's response.
		if a != nil {
			result.Response = lastAssistantResponse(a)
		}
		// A run told it was near its deadline that left a handoff note
		// stopped part way: the host continues it in a new run.
		if note := handoffNote("."); note != "" {
			result.Status = "handed_off"
			result.Handoff = note
		}
	}

	// Collect git diff (best-effort)
	// The pieces are joined byte for byte: trimming a piece would drop a
	// hunk's trailing blank context line (" "), leaving the hunk shorter
	// than its header says and the patch unappliable.
	var diffOutput string
	noHEAD := false
	if diff, err := exec.Command("git", "diff", "HEAD").Output(); err == nil {
		diffOutput = appendDiffPiece(diffOutput, string(diff))
	} else {
		// No HEAD ref - try combining unstaged and staged diffs
		noHEAD = true
		if unstaged, err := exec.Command("git", "diff").Output(); err == nil {
			diffOutput = appendDiffPiece(diffOutput, string(unstaged))
		}
		if staged, err := exec.Command("git", "diff", "--cached").Output(); err == nil {
			diffOutput = appendDiffPiece(diffOutput, string(staged))
		}
	}

	// Include diffs for untracked files (new files not yet staged, not gitignored).
	// git diff --no-index exits with code 1 when files differ (normal case), so we
	// must read output even when err != nil as long as the exit code is 1.
	// We capture the untracked file list here to reuse later for FilesModified population.
	var untrackedFiles []string
	if untracked, err := exec.Command("git", "ls-files", "--others", "--exclude-standard").Output(); err == nil {
		for _, f := range strings.Split(strings.TrimSpace(string(untracked)), "\n") {
			if f = strings.TrimSpace(f); f != "" {
				untrackedFiles = append(untrackedFiles, f)
				cmd := exec.Command("git", "diff", "--no-index", os.DevNull, f) //nolint:gosec // G204: f is a repo-relative path from git ls-files --others
				d, err := cmd.Output()
				if err != nil {
					// Exit code 1 = files differ (normal); accept output.
					// Exit code 2+ = genuine error; skip.
					if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
						continue
					}
				}
				diffOutput = appendDiffPiece(diffOutput, string(d))
			}
		}
	}

	// Keep the diff under maxDiffBytes by leaving out whole files, never
	// part of one: a cut file cannot be applied, and everything after the
	// cut used to be lost. The files left out are named in the result.
	diffOutput, result.DiffOmitted = capDiff(diffOutput, maxDiffBytes)

	if diffOutput != "" {
		result.GitDiff = diffOutput
	}

	// Collect modified files (best-effort)
	seen := make(map[string]bool)
	if !noHEAD {
		if out, err := exec.Command("git", "diff", "--name-only", "HEAD").Output(); err == nil {
			for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if l = strings.TrimSpace(l); l != "" && !seen[l] {
					seen[l] = true
					result.FilesModified = append(result.FilesModified, l)
				}
			}
		}
	} else {
		// No HEAD ref - combine unstaged and staged file lists
		for _, cmd := range []*exec.Cmd{
			exec.Command("git", "diff", "--name-only"),
			exec.Command("git", "diff", "--cached", "--name-only"),
		} {
			if out, err := cmd.Output(); err == nil {
				for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
					if l = strings.TrimSpace(l); l != "" && !seen[l] {
						seen[l] = true
						result.FilesModified = append(result.FilesModified, l)
					}
				}
			}
		}
	}

	// Include untracked new files (not yet staged, not gitignored).
	// Reuse the file list captured earlier during diff generation.
	for _, l := range untrackedFiles {
		if l = strings.TrimSpace(l); l != "" && !seen[l] {
			seen[l] = true
			result.FilesModified = append(result.FilesModified, l)
		}
	}

	// Determine the output destination. When --output-path is set, write the
	// JSON to that file so stdout stays free for logs (important for the Fly
	// machine log fallback path). On file-write failure, fall back to stdout
	// so the structured result is never silently lost.
	var out *os.File = os.Stdout
	if outputPath != "" {
		f, err := os.Create(outputPath)
		if err != nil {
			console.GlyphWarning.Fprintf(os.Stderr, "Failed to open output path %q for JSON result: %v; falling back to stdout", outputPath, err)
		} else {
			out = f
			defer func() {
				if cerr := out.Close(); cerr != nil {
					console.GlyphWarning.Fprintf(os.Stderr, "Failed to close output path %q: %v", outputPath, cerr)
				}
			}()
		}
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		console.GlyphWarning.Fprintf(os.Stderr, "Failed to encode JSON result: %v", err)
		// If we were writing to a file and the encode failed (disk full,
		// broken pipe mid-write), the file is likely partial/empty. Fall
		// back to stdout so the structured result is never silently lost.
		if out != os.Stdout {
			stdoutEnc := json.NewEncoder(os.Stdout)
			stdoutEnc.SetIndent("", "  ")
			_ = stdoutEnc.Encode(result) // best-effort; nothing more we can do
		}
	}
}

// lastAssistantResponse returns the content of the most recent assistant
// message with non-empty content, walking the conversation history in
// reverse. Tool-only assistant messages (empty content + ToolCalls) are
// skipped so the envelope carries the user-visible final answer, not an
// intermediate tool-call turn.
func lastAssistantResponse(a *agent.Agent) string {
	msgs := a.GetMessages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && strings.TrimSpace(msgs[i].Content) != "" {
			return msgs[i].Content
		}
	}
	return ""
}

// appendDiffPiece appends one git diff's output to a combined diff, unchanged
// apart from ending it with a newline. An empty (or whitespace-only) piece adds
// nothing. The piece is never trimmed: a hunk's last line can be a blank
// context line (a single space), which a trim would remove.
func appendDiffPiece(combined, piece string) string {
	if strings.TrimSpace(piece) == "" {
		return combined
	}
	if !strings.HasSuffix(piece, "\n") {
		piece += "\n"
	}
	return combined + piece
}

// capDiff keeps diff within max bytes by leaving out whole file sections.
// When the diff fits it is returned as it is. Otherwise the smallest files
// are kept first (a source change is small; a rebuilt bundle, vendored code
// or a large deletion is what pushes a diff over), in their original order,
// and the paths of the files left out are returned. A section is never cut.
//
// At least one file section is always kept, even when it alone is over max:
// a diff with no file in it reads as "nothing changed", which is worse than
// one oversized file the caller can see and handle.
func capDiff(diff string, max int) (string, []string) {
	if len(diff) <= max {
		return diff, nil
	}
	sections := splitDiffSections(diff)
	order := make([]int, len(sections))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return len(sections[order[a]]) < len(sections[order[b]]) })
	keep := make([]bool, len(sections))
	total := 0
	for _, i := range order {
		if total+len(sections[i]) <= max {
			keep[i] = true
			total += len(sections[i])
		}
	}
	if total == 0 {
		keep[order[0]] = true
	}
	var b strings.Builder
	var omitted []string
	for i, sec := range sections {
		if keep[i] {
			b.WriteString(sec)
		} else {
			omitted = append(omitted, diffSectionPath(sec))
		}
	}
	return b.String(), omitted
}

// splitDiffSections splits a unified diff into one section per file, each
// starting at its "diff --git " line. Text before the first header is kept as
// its own section so it is never silently dropped or merged into a file it
// does not belong to.
func splitDiffSections(diff string) []string {
	var sections []string
	start := 0
	if !strings.HasPrefix(diff, "diff --git ") {
		next := strings.Index(diff, "\ndiff --git ")
		if next < 0 {
			return []string{diff}
		}
		sections = append(sections, diff[:next+1])
		start = next + 1
	}
	for start < len(diff) {
		next := strings.Index(diff[start+len("diff --git "):], "\ndiff --git ")
		if next < 0 {
			break
		}
		cut := start + len("diff --git ") + next + 1
		sections = append(sections, diff[start:cut])
		start = cut
	}
	return append(sections, diff[start:])
}

// diffSectionPath is the path a file section changes.
//
// The "diff --git" header is ambiguous for a path holding a space: git emits
// "diff --git a/x b/f.txt b/x b/f.txt" with no quoting, so neither side can
// be told from the text alone. The "+++"/"---" lines carry the path
// unambiguously (quoted only when it holds a space, a quote or a non-ASCII
// byte), so they are the source: "+++ b/<path>" names the file, and for a
// deletion ("+++ /dev/null") "--- a/<path>" does.
func diffSectionPath(section string) string {
	var fromMinus, fromPlus string
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "@@") {
			// The hunk body has started; the header lines are behind us.
			break
		}
		if strings.HasPrefix(line, "+++ ") {
			fromPlus = headerLinePath(line[len("+++ "):])
		} else if strings.HasPrefix(line, "--- ") {
			fromMinus = headerLinePath(line[len("--- "):])
		}
	}
	if fromPlus != "" {
		return fromPlus
	}
	return fromMinus
}

// headerLinePath is the path on a "---"/"+++" line: unquoted when git quoted
// it, without the a/ or b/ prefix, and without the tab git appends when the
// path holds a space. /dev/null (a creation or a deletion) is not a path.
func headerLinePath(s string) string {
	if i := strings.IndexByte(s, '\t'); i >= 0 {
		s = s[:i]
	}
	if strings.HasPrefix(s, `"`) {
		if p, err := strconv.Unquote(s); err == nil {
			s = p
		} else {
			s = strings.Trim(s, `"`)
		}
	}
	if s == "/dev/null" || s == "" {
		return ""
	}
	s = strings.TrimPrefix(s, "a/")
	return strings.TrimPrefix(s, "b/")
}
