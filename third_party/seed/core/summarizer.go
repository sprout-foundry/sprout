package core

import "context"

// SummarizerHint conveys optional shaping parameters from seed's compaction
// pipeline to a consumer-supplied LLMSummarizer. All fields are advisory: a
// summarizer that ignores hints still satisfies the contract.
type SummarizerHint struct {
	// DetailLevel is one of "brief", "summary", "detailed", or "" (default).
	// Layered compaction uses these to graduate detail across age bands of the
	// conversation: brief for oldest content, detailed for newest middle.
	DetailLevel string

	// MaxWords caps the requested length of the returned summary. Zero means
	// "use the summarizer's default budget".
	MaxWords int
}

// LLMSummarizer is the seed extension point for LLM-based structural
// compaction. Consumers (like sprout) provide an implementation that calls
// their LLM to compress a window of older conversation history into a single
// summary string.
//
// The function receives the messages to summarize (read-only — implementations
// must not mutate the slice) and a SummarizerHint shaping the response.
// It returns the raw summary text — seed wraps it into the standard
// "Compacted earlier conversation state:" header before splicing into history.
//
// A nil summarizer disables LLM-based structural compaction; seed then falls
// back to its rule-based compaction (drop oldest turns + emergency truncate).
//
// Implementations should:
//   - Honor ctx cancellation. Long-running summary calls block the chat loop.
//   - Return ("", nil) if the summary would be empty or low-value; seed treats
//     this as "skip LLM summary, fall through to structural compaction".
//   - Return (s, err) with err non-nil if the underlying call failed; seed
//     logs and falls back to rule-based compaction.
type LLMSummarizer func(ctx context.Context, messages []Message, hint SummarizerHint) (string, error)

// ArtifactBlockFunc renders a deterministic record of the durable artifacts
// (files touched) in a message span. Structural compaction calls it once per
// pass with the whole summarized span and appends the result to the final
// summary body, so the record appears exactly once no matter how many
// summarizer calls built that body. Return "" for a span with nothing to
// record.
//
// The distinction matters: a summarizer that appends its own artifact block is
// invoked once per layer by the layered pass, and the merged body would carry
// one block per layer. A hook here runs after the merge.
//
// Deduping across carried blocks is the consumer's job: a file edited both
// before and after a compaction appears in the carried summary's record and
// again in the new one, and seed writes both verbatim.
type ArtifactBlockFunc func(messages []Message) string

// StructuralOptions carries optional consumer hooks for one structural
// compaction pass. The zero value preserves the historical behavior.
type StructuralOptions struct {
	// ArtifactBlock, when non-nil, appends a deterministic artifact record to
	// the summary body, once per pass.
	ArtifactBlock ArtifactBlockFunc
	// RecentTailTokens, when > 0, sizes the retained tail by estimated tokens
	// instead of StructuralRecentToKeep messages. A message count cannot
	// express a context budget: 24 messages can be 2K tokens of chat or 200K
	// of tool output. Zero keeps the historical count-based boundary.
	RecentTailTokens int
	// EstimateFn prices the tail when RecentTailTokens > 0. nil → roughTokens.
	EstimateFn func([]Message) int
}

// withArtifactBlock appends the artifact record for span to body. Returns body
// unchanged when the hook is nil, the span has no artifacts, or body is empty.
//
// The empty-body check is a backstop: both callers already return "no change"
// on an empty or whitespace-only summary before reaching here, so the
// fall-through-to-rule-based-compaction contract is enforced there. It stays
// because a manifest-only body would be treated as a usable summary and
// spliced in place of the narrative, which is worth guarding against twice.
func (o StructuralOptions) withArtifactBlock(body string, span []Message) string {
	if o.ArtifactBlock == nil || len(span) == 0 {
		return body
	}
	if body == "" {
		return body
	}
	block := o.ArtifactBlock(span)
	if block == "" {
		return body
	}
	return body + "\n\n" + block
}
