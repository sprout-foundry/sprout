package agent

import (
	"sync"

	"github.com/sprout-foundry/seed/core"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ConversationOptimizer is sprout's thin wrapper around seed's
// core.ConversationOptimizer. The dedup + observation-masking
// implementation moved into seed so other consumers benefit; this wrapper
// preserves sprout's historical method surface (InvalidateFile,
// GetOptimizationStats, SetLLMClient) for callers that haven't migrated.
//
// The LLM-based structural compaction that used to live here is now wired
// through seed's chat loop via Options.LLMSummarizer — see
// newLLMSummarizer in llm_summarizer.go and the construction site in
// seed_integration.go. SetLLMClient is consequently a no-op here.
type ConversationOptimizer struct {
	mu       sync.Mutex
	inner    *core.ConversationOptimizer
	enabled  bool
}

// NewConversationOptimizer constructs the wrapper. The debug flag is kept
// for backward-compatible call sites but is unused now that seed's
// optimizer emits via the EventPublisher instead.
func NewConversationOptimizer(enabled, debug bool) *ConversationOptimizer {
	_ = debug
	return &ConversationOptimizer{
		inner: core.NewConversationOptimizer(core.ConversationOptimizerOptions{
			Enabled:     enabled,
			KnownToolFn: sproutKnownToolFn,
		}),
		enabled: enabled,
	}
}

// sproutKnownToolFn maps sprout's tool names to seed's optimizer categories
// so the file-read and shell-command dedup paths fire for the right tools.
func sproutKnownToolFn(name string) core.ToolCategory {
	switch name {
	case "read_file":
		return core.ToolCategoryFileRead
	case "shell_command":
		return core.ToolCategoryShellCommand
	}
	return core.ToolCategoryUnknown
}

// OptimizeConversation delegates to the seed optimizer. Returns the input
// unchanged when the optimizer is disabled.
func (co *ConversationOptimizer) OptimizeConversation(messages []api.Message) []api.Message {
	if co == nil || co.inner == nil {
		return messages
	}
	return co.inner.OptimizeConversation(messages)
}

// CompactConversation is retained for backward compatibility with callers
// that haven't migrated. Structural compaction now runs inside seed's chat
// loop via core.CompactWithLLMSummary, configured at seed-Agent construction.
// Calling this here is a no-op; the live request path no longer routes
// through this method.
func (co *ConversationOptimizer) CompactConversation(messages []api.Message) []api.Message {
	return messages
}

// InvalidateFile was used by sprout's per-file dedup cache. Seed's optimizer
// is stateless across calls (it scans the message list fresh each time), so
// there is no cache to invalidate. Kept as a no-op for caller compatibility.
func (co *ConversationOptimizer) InvalidateFile(filePath string) {
	_ = filePath
}

// SetLLMClient is now a no-op. The LLM summary path is wired via seed
// Options.LLMSummarizer at seed-Agent construction (seed_integration.go).
func (co *ConversationOptimizer) SetLLMClient(client api.ClientInterface, provider string, printLine func(string)) {
	_ = client
	_ = provider
	_ = printLine
}

// IsEnabled reports whether the optimizer was constructed enabled.
func (co *ConversationOptimizer) IsEnabled() bool {
	if co == nil {
		return false
	}
	co.mu.Lock()
	defer co.mu.Unlock()
	return co.enabled
}

// SetEnabled toggles the optimizer at runtime. Since seed's optimizer
// captures Enabled at construction, this rebuilds the inner instance.
func (co *ConversationOptimizer) SetEnabled(enabled bool) {
	if co == nil {
		return
	}
	co.mu.Lock()
	defer co.mu.Unlock()
	if co.enabled == enabled {
		return
	}
	co.enabled = enabled
	co.inner = core.NewConversationOptimizer(core.ConversationOptimizerOptions{
		Enabled:     enabled,
		KnownToolFn: sproutKnownToolFn,
	})
}

// Reset clears optimizer state. Seed's optimizer is stateless across calls
// so this is a no-op; preserved for caller compatibility.
func (co *ConversationOptimizer) Reset() {}

// GetOptimizationStats returns a small status map. The per-file and
// per-command tracking counts are no longer maintained (seed scans
// fresh each call); the map shape stays so UI consumers continue to work.
func (co *ConversationOptimizer) GetOptimizationStats() map[string]interface{} {
	enabled := false
	if co != nil {
		enabled = co.IsEnabled()
	}
	return map[string]interface{}{
		"enabled":          enabled,
		"tracked_files":    0,
		"tracked_commands": 0,
		"file_paths":       []string{},
		"shell_commands":   []string{},
	}
}

// Inner exposes the wrapped seed optimizer for direct use when constructing
// seed-Agent options (see seed_integration.go).
func (co *ConversationOptimizer) Inner() *core.ConversationOptimizer {
	if co == nil {
		return nil
	}
	return co.inner
}
