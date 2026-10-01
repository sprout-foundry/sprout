package agent

import (
	"context"

	"github.com/sprout-foundry/sprout/pkg/agent/approvals"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenttools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// SP-141 phase 3 (increment 1): the LLM security-analyzer cluster
// (SecurityAnalysis, chain parsing, cache, LLM entrypoints) now lives in
// pkg/agent/approvals behind an injected client + model. These type aliases
// and forwarding functions keep every in-package and external call site
// compiling unchanged; the *Agent-based signatures resolve the client
// (via getClient, under the client read lock) and the model (via GetModel,
// which includes the session-model override) and forward to approvals.
//
// The one behavioral nuance vs. the pre-move code: a nil *Agent used to
// fail fast with an "nil agent" (InvalidInput) error at every entrypoint.
// It now degrades to a nil client, so AnalyzeChain/AnalyzeShellCommand on a
// short chain return the "no client configured" (Config) error, while
// AnalyzeChainFallback (and the long-chain path, which dispatches to it
// before the client check) still returns a synthesized best-effort entry
// with no error. The only production caller (approval_broker) always
// passes a non-nil receiver, so this is unreachable in the live path; it is
// pinned by TestAnalyzeChainFallback_NilClient_ReturnsSynthesized.

type SecurityAnalysis = approvals.SecurityAnalysis
type SecurityAnalysisCache = approvals.SecurityAnalysisCache
type Chain = approvals.Chain

const MaxChainSubcommandsForBatchPrompt = approvals.MaxChainSubcommandsForBatchPrompt

func NewSecurityAnalysisCache() *SecurityAnalysisCache {
	return approvals.NewSecurityAnalysisCache()
}

func ParseChain(s string) Chain {
	return approvals.ParseChain(s)
}

func ChainCacheKey(input string) string {
	return approvals.ChainCacheKey(input)
}

func NormalizeChain(chain Chain) string {
	return approvals.NormalizeChain(chain)
}

// AnalyzeChain analyzes a command chain using the agent's LLM.
func AnalyzeChain(ctx context.Context, a *Agent, chain Chain, classifications []agenttools.ChainedClassification, cwd string) (*SecurityAnalysis, error) {
	var client api.ClientInterface
	var model string
	if a != nil {
		client = a.getClient()
		model = a.GetModel()
	}
	return approvals.AnalyzeChain(ctx, client, model, chain, classifications, cwd)
}

// AnalyzeChainFallback analyzes long chains per-subcommand and synthesizes.
func AnalyzeChainFallback(ctx context.Context, a *Agent, chain Chain, classifications []agenttools.ChainedClassification, cwd string) (*SecurityAnalysis, error) {
	var client api.ClientInterface
	var model string
	if a != nil {
		client = a.getClient()
		model = a.GetModel()
	}
	return approvals.AnalyzeChainFallback(ctx, client, model, chain, classifications, cwd)
}

// AnalyzeShellCommand sends a shell command to the agent's LLM for
// plain-language analysis.
func AnalyzeShellCommand(ctx context.Context, a *Agent, command, cwd string) (*SecurityAnalysis, error) {
	var client api.ClientInterface
	var model string
	if a != nil {
		client = a.getClient()
		model = a.GetModel()
	}
	return approvals.AnalyzeShellCommand(ctx, client, model, command, cwd)
}
