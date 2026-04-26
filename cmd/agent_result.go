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
	if diff, err := exec.Command("git", "diff", "HEAD").Output(); err == nil {
		trimmed := strings.TrimSpace(string(diff))
		if trimmed != "" {
			result.GitDiff = trimmed
		}
	}

	// Collect modified files (best-effort)
	if out, err := exec.Command("git", "diff", "--name-only", "HEAD").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, l := range lines {
			if l = strings.TrimSpace(l); l != "" {
				result.FilesModified = append(result.FilesModified, l)
			}
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "[WARN] Failed to encode JSON result: %v\n", err)
	}
}
