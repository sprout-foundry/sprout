package approvals

// security_analyzer_chain.go — the chain-decomposition helpers, split out of
// security_analyzer.go (now in this package). Chain parses a shell command into subcommands +
// operators; ParseChain / tokenizeChain are the quote-aware tokenizer;
// ChainCacheKey / NormalizeChain produce normalized cache keys so equivalent
// chains collide.
import (
	"strings"

	agenttools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// ─── Chain types ───────────────────────────────────────────────────────────

// Chain is a top-level decomposition of a shell command.
// For unchained input, Subcommands has length 1. Operators carries the chain
// operator between each adjacent pair of subcommands.
type Chain struct {
	Original    string
	Operators   []string // len(Subcommands)-1
	Subcommands []string // len >= 1 (split via SplitChainedCommand)
}

// ParseChain splits a shell command string into a Chain value, delegating to SplitChainedCommand.
func ParseChain(s string) Chain {
	s = strings.TrimSpace(s)
	parts := agenttools.SplitChainedCommand(s)
	return Chain{
		Original:    s,
		Operators:   nil, // reconstruction deferred
		Subcommands: parts,
	}
}

// tokenizeChain walks the input string and emits a normalized sequence of
// subcommands and operators. It is quote-aware: operators inside single or
// double quotes are not treated as chain operators.
//
// Example outputs:
//
//	"a && b"     → ["a", "AND", "b"]
//	"a || b"     → ["a", "OR", "b"]
//	"a | b"      → ["a", "PIPE", "b"]
//	"a ; b"      → ["a", "SEQ", "b"]
//	"a && b || c | d" → ["a", "AND", "b", "OR", "c", "PIPE", "d"]
func tokenizeChain(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	var tokens []string
	var current strings.Builder
	inSingleQuote := false
	inDoubleQuote := false
	i := 0

	// Helper to flush the current subcommand token (with whitespace normalization)
	flush := func() {
		s := current.String()
		// Collapse internal whitespace
		fields := strings.Fields(s)
		if len(fields) > 0 {
			tokens = append(tokens, strings.Join(fields, " "))
		}
		current.Reset()
	}

	for i < len(input) {
		ch := input[i]

		// Handle quote state
		if ch == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
			current.WriteByte(ch)
			i++
			continue
		}
		if ch == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			current.WriteByte(ch)
			i++
			continue
		}

		// If we're inside quotes, just add the character
		if inSingleQuote || inDoubleQuote {
			current.WriteByte(ch)
			i++
			continue
		}

		// Check for operators at current position
		remaining := input[i:]
		if strings.HasPrefix(remaining, "&&") {
			flush()
			tokens = append(tokens, "AND")
			i += 2
			// Skip trailing whitespace after operator
			for i < len(input) && (input[i] == ' ' || input[i] == '\t') {
				i++
			}
			continue
		}
		if strings.HasPrefix(remaining, "||") {
			flush()
			tokens = append(tokens, "OR")
			i += 2
			// Skip trailing whitespace after operator
			for i < len(input) && (input[i] == ' ' || input[i] == '\t') {
				i++
			}
			continue
		}
		if strings.HasPrefix(remaining, "|") {
			flush()
			tokens = append(tokens, "PIPE")
			i++
			// Skip trailing whitespace after operator
			for i < len(input) && (input[i] == ' ' || input[i] == '\t') {
				i++
			}
			continue
		}
		if strings.HasPrefix(remaining, ";") {
			flush()
			tokens = append(tokens, "SEQ")
			i++
			// Skip trailing whitespace after operator
			for i < len(input) && (input[i] == ' ' || input[i] == '\t') {
				i++
			}
			continue
		}

		// Regular character - accumulate
		current.WriteByte(ch)
		i++
	}

	// Flush any remaining subcommand
	flush()

	return tokens
}

// chainTokensToCacheKey converts a tokenized chain to a normalized cache key.
// The prefix is prepended by the caller.
func chainTokensToCacheKey(tokens []string) string {
	return strings.Join(tokens, " | ")
}

// ChainCacheKey returns the cache key for storing/retrieving analyses
// of a shell chain. The key is normalized so that equivalent chains
// (modulo whitespace and outer trimming) collide, but distinct
// operators keep distinct keys.
func ChainCacheKey(input string) string {
	tokens := tokenizeChain(input)
	if len(tokens) == 0 {
		return "sp-124b:v1:"
	}
	return "sp-124b:v1:" + chainTokensToCacheKey(tokens)
}

// NormalizeChain returns a normalized cache key for a chain.
// It walks chain.Original to recover operators and produces distinct keys
// for "a && b" and "a || b". Chains with identical subcommands and operators
// normalize to the same key regardless of internal whitespace.
func NormalizeChain(chain Chain) string {
	if chain.Original == "" {
		return ""
	}
	tokens := tokenizeChain(chain.Original)
	if len(tokens) == 0 {
		return ""
	}
	return chainTokensToCacheKey(tokens)
}
