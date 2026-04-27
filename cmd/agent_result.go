package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// AgentResult is the structured output produced when --output-format=json is used.
// It captures everything a SaaS wrapper (e.g. Sprout Foundry) needs from a
// non-interactive sprout run.
type AgentResult struct {
	Status        string             `json:"status"`                   // "success" or "error"
	Error         string             `json:"error,omitempty"`          // error message if status=="error"
	Query         string             `json:"query"`                    // the original prompt
	FilesModified []string           `json:"files_modified,omitempty"` // files changed during execution
	GitDiff       string             `json:"git_diff,omitempty"`       // unified diff of all changes
	Metrics       AgentResultMetrics `json:"metrics"`
}

// AgentResultMetrics holds execution metrics for structured output.
type AgentResultMetrics struct {
	ElapsedSeconds float64 `json:"elapsed_seconds"`
	TokensIn       int     `json:"tokens_in"`  // Total prompt/input tokens
	TokensOut      int     `json:"tokens_out"` // Total completion/output tokens
	LLMCalls       int     `json:"llm_calls"`  // Number of LLM API calls made
	Provider       string  `json:"provider"`   // LLM provider name (e.g., "openai", "anthropic")
	Model          string  `json:"model"`      // Model identifier (e.g., "gpt-4o")
}

// outputFormatJSON is the flag value for JSON output mode.
var outputFormatJSON bool

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
	}

	if runErr != nil {
		result.Status = "error"
		result.Error = runErr.Error()
	} else {
		result.Status = "success"
	}

	// Collect git diff (best-effort)
	var diffOutput string
	if diff, err := exec.Command("git", "diff", "HEAD").Output(); err == nil {
		trimmed := strings.TrimSpace(string(diff))
		if trimmed != "" {
			diffOutput = trimmed
		}
	} else {
		// No HEAD ref - try combining unstaged and staged diffs
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
	if diffOutput != "" {
		result.GitDiff = diffOutput
	}

	// Collect modified files (best-effort)
	seen := make(map[string]bool)
	if out, err := exec.Command("git", "diff", "--name-only", "HEAD").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, l := range lines {
			if l = strings.TrimSpace(l); l != "" && !seen[l] {
				seen[l] = true
				result.FilesModified = append(result.FilesModified, l)
			}
		}
	} else {
		// No HEAD ref - try combining unstaged and staged file lists
		for _, cmd := range []*exec.Cmd{
			exec.Command("git", "diff", "--name-only"),
			exec.Command("git", "diff", "--cached", "--name-only"),
		} {
			if out, err := cmd.Output(); err == nil {
				lines := strings.Split(strings.TrimSpace(string(out)), "\n")
				for _, l := range lines {
					if l = strings.TrimSpace(l); l != "" && !seen[l] {
						seen[l] = true
						result.FilesModified = append(result.FilesModified, l)
					}
				}
			}
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "[WARN] Failed to encode JSON result: %v\n", err)
	}
}
