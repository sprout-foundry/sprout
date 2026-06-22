//go:build !js

package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// AgentResult is the structured output produced when --output-format=json is used.
// It captures everything a SaaS wrapper (e.g. Sprout Foundry) needs from a
// non-interactive sprout run.
type AgentResult struct {
	Status         string             `json:"status"`                     // "success" or "error"
	Error          string             `json:"error,omitempty"`            // error message if status=="error"
	Query          string             `json:"query"`                      // the original prompt
	FilesModified  []string           `json:"files_modified,omitempty"`   // files changed during execution
	GitDiff        string             `json:"git_diff,omitempty"`         // unified diff of all changes
	PullRequestURL string             `json:"pull_request_url,omitempty"` // URL of PR created during execution
	Metrics        AgentResultMetrics `json:"metrics"`
}

// AgentResultMetrics holds execution metrics for structured output.
type AgentResultMetrics struct {
	ElapsedSeconds float64 `json:"elapsed_seconds"`
	TokensIn       int     `json:"tokens_in"`  // Total prompt/input tokens
	TokensOut      int     `json:"tokens_out"` // Total completion/output tokens
	LLMCalls       int     `json:"llm_calls"`  // Number of LLM API calls made
	Provider       string  `json:"provider"`   // LLM provider name (e.g., "openai", "anthropic")
	Model          string  `json:"model"`      // Model identifier (e.g., "gpt-4o")

	// Security telemetry — track post-caution LLM behavior so external tools
	// can measure SECURITY_CAUTION_REQUIRED signal effectiveness.
	SecurityCautionsIssued      int64 `json:"security_cautions_issued"`      // Times a SECURITY_CAUTION_REQUIRED was produced
	SecurityRetriesAfterCaution int64 `json:"security_retries_after_caution"` // Times the LLM retried the same blocked op after a caution
	SecurityLoopsDetected       int64 `json:"security_loops_detected"`       // Times loop-detection fired (3+ identical blocks)
}

// outputFormatJSON is the flag value for JSON output mode.
var outputFormatJSON bool

// maxDiffBytes is the maximum size of git diff output to include.
const maxDiffBytes = 1 << 20 // 1MB

func init() {
	agentCmd.Flags().BoolVar(&outputFormatJSON, "output-json", false, "Output structured JSON result to stdout after execution (for CI/SaaS integration)")
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
		result.Metrics.Provider = a.GetProvider()
		result.Metrics.Model = a.GetModel()
		// Security telemetry
		result.Metrics.SecurityCautionsIssued = a.GetSecurityCautionsIssued()
		result.Metrics.SecurityRetriesAfterCaution = a.GetSecurityRetriesAfterCaution()
		result.Metrics.SecurityLoopsDetected = a.GetSecurityLoopsDetected()
	}

	if runErr != nil {
		result.Status = "error"
		result.Error = runErr.Error()
	} else {
		result.Status = "success"
	}

	// Collect git diff (best-effort)
	var diffOutput string
	noHEAD := false
	if diff, err := exec.Command("git", "diff", "HEAD").Output(); err == nil {
		trimmed := strings.TrimSpace(string(diff))
		if trimmed != "" {
			diffOutput = trimmed
		}
	} else {
		// No HEAD ref - try combining unstaged and staged diffs
		noHEAD = true
		var parts []string
		if unstaged, err := exec.Command("git", "diff").Output(); err == nil {
			if trimmed := strings.TrimSpace(string(unstaged)); trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
		if staged, err := exec.Command("git", "diff", "--cached").Output(); err == nil {
			if trimmed := strings.TrimSpace(string(staged)); trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
		if len(parts) > 0 {
			diffOutput = strings.Join(parts, "\n")
		}
	}

	// Include diffs for untracked files (new files not yet staged, not gitignored).
	// git diff --no-index exits with code 1 when files differ (normal case), so we
	// must read output even when err != nil as long as the exit code is 1.
	// We capture the untracked file list here to reuse later for FilesModified population.
	var untrackedFiles []string
	if untracked, err := exec.Command("git", "ls-files", "--others", "--exclude-standard").Output(); err == nil {
		var untrackedParts []string
		for _, f := range strings.Split(strings.TrimSpace(string(untracked)), "\n") {
			if f = strings.TrimSpace(f); f != "" {
				untrackedFiles = append(untrackedFiles, f)
				cmd := exec.Command("git", "diff", "--no-index", "/dev/null", f)
				d, err := cmd.Output()
				if err != nil {
					// Exit code 1 = files differ (normal); accept output.
					// Exit code 2+ = genuine error; skip.
					if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
						continue
					}
				}
				if trimmed := strings.TrimSpace(string(d)); trimmed != "" {
					untrackedParts = append(untrackedParts, trimmed)
				}
			}
		}
		if len(untrackedParts) > 0 {
			if diffOutput != "" {
				diffOutput += "\n"
			}
			diffOutput += strings.Join(untrackedParts, "\n")
		}
	}

	// Truncate diff output if it exceeds maxDiffBytes
	if len(diffOutput) > maxDiffBytes {
		diffOutput = diffOutput[:maxDiffBytes] + "\n\n... [diff truncated at 1MB]"
	}

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

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		console.GlyphWarning.Fprintf(os.Stderr, "Failed to encode JSON result: %v", err)
	}
}
