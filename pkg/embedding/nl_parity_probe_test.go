package embedding

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestNLParityProbe compares Jina Code v2 vs Gemma 300M on natural-language
// semantic matching — the task the ConversationStore does when it searches for
// relevant past turns and memories.
//
// The probe embeds pairs of NL texts (no code) and measures cosine similarity
// bands across three tiers:
//
//	related:  same topic, same intent (should score high)
//	adjacent: same domain, different intent (should score medium-low)
//	unrelated: completely different topic (should score low)
//
// The gate for Jina consolidation: Jina's separation between "related" and
// "unrelated" must be clean enough to distinguish relevant turns from noise.
// Gemma's bands (measured separately on the conversation store) are:
// p25 0.40, median 0.50, p75 0.61, max 0.81. Jina must match or beat that
// separation to consolidate.
//
// Opt-in: SPROUT_NL_PARITY_EVAL=1. Requires both models on disk.

type nlPair struct {
	a, b     string
	category string // "related", "adjacent", "unrelated"
}

func conversationCorpus() []nlPair {
	return []nlPair{
		// Related — same topic, same intent. Should score high.
		{"We fixed the JWT authentication bug by adding an expiry check in the middleware.",
			"The auth issue was resolved by validating JWT expiration timestamps before processing requests.",
			"related"},
		{"I'm getting a segmentation fault when running the test suite on macOS.",
			"The tests crash with a segfault on macOS, likely a memory alignment issue in the ONNX runtime.",
			"related"},
		{"The webhook delivery is failing with a 500 error after the deploy.",
			"Webhook requests started returning 500 errors following the latest deployment.",
			"related"},
		{"Can we add pagination to the search results endpoint?",
			"We should implement pagination on the search API to handle large result sets.",
			"related"},
		{"The CI pipeline is timing out during the Docker build step.",
			"CI builds are exceeding the timeout limit when creating the Docker image.",
			"related"},
		{"I need to migrate the database schema to add a user preferences table.",
			"Let's add a user_preferences table to the database schema migration.",
			"related"},
		{"The memory usage keeps growing — looks like a goroutine leak in the event bus.",
			"There's a goroutine leak in the event bus causing steadily increasing memory consumption.",
			"related"},
		{"Rate limiting is rejecting legitimate requests during traffic spikes.",
			"The rate limiter is too aggressive and blocks valid users during peak load.",
			"related"},
		{"The config merge logic is overwriting workspace settings with global defaults.",
			"Configuration merging is incorrectly replacing workspace-specific values with global config.",
			"related"},
		{"WebSockets disconnect every 30 seconds in the daemon mode.",
			"The daemon's WebSocket connections are timing out after 30 seconds.",
			"related"},

		// Adjacent — same domain (dev/infra), different intent. Should score medium-low.
		{"We fixed the JWT authentication bug by adding an expiry check.",
			"The login page needs a remember-me checkbox for session persistence.",
			"adjacent"},
		{"The CI pipeline is timing out during the Docker build step.",
			"We should cache npm dependencies to speed up local development.",
			"adjacent"},
		{"Rate limiting is rejecting legitimate requests during traffic spikes.",
			"Let's add structured logging to track API response times.",
			"adjacent"},
		{"The config merge logic is overwriting workspace settings.",
			"I want to add a new provider for the OpenAI-compatible API.",
			"adjacent"},
		{"WebSockets disconnect every 30 seconds in daemon mode.",
			"The embedded model download should show a progress bar in the UI.",
			"adjacent"},
		{"I'm getting a segfault when running tests on macOS.",
			"Let's add unit tests for the new embedding provider.",
			"adjacent"},
		{"The memory usage keeps growing from a goroutine leak.",
			"We need to profile the ONNX inference path to find the CPU bottleneck.",
			"adjacent"},
		{"Can we add pagination to search results?",
			"The search index should be rebuilt when files are renamed via git.",
			"adjacent"},

		// Unrelated — completely different topics. Should score low.
		{"We fixed the JWT authentication bug by adding an expiry check.",
			"The weather forecast shows rain tomorrow afternoon.",
			"unrelated"},
		{"The CI pipeline is timing out during the Docker build.",
			"I bought new hiking boots for the trip next week.",
			"unrelated"},
		{"Rate limiting is rejecting legitimate requests.",
			"My favorite pizza place closed down last month.",
			"unrelated"},
		{"The config merge logic is overwriting workspace settings.",
			"The stock market dropped 3% after the earnings report.",
			"unrelated"},
		{"WebSockets disconnect every 30 seconds in daemon mode.",
			"We should plant tomatoes in the garden this spring.",
			"unrelated"},
		{"I'm getting a segfault when running tests on macOS.",
			"The concert was amazing — best live performance I've seen.",
			"unrelated"},
		{"The memory usage keeps growing from a goroutine leak.",
			"Remember to pick up milk and eggs from the grocery store.",
			"unrelated"},
		{"Can we add pagination to search results?",
			"The novel I'm reading has a surprising twist ending.",
			"unrelated"},
		{"The webhook delivery is failing with a 500 error.",
			"I've been learning Spanish for about three months now.",
			"unrelated"},
		{"The database schema needs a user preferences table.",
			"The flight to Tokyo was delayed by four hours.",
			"unrelated"},
	}
}

func cosineSim(a, b []float32) float32 {
	var dot, na, nb float32
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na < 1e-9 || nb < 1e-9 {
		return 0
	}
	return dot / float32(math.Sqrt(float64(na*nb)))
}

func TestNLParityProbe(t *testing.T) {
	if os.Getenv("SPROUT_NL_PARITY_EVAL") != "1" {
		t.Skip("SPROUT_NL_PARITY_EVAL unset")
	}

	ctx := context.Background()
	modelDir := DefaultModelDir()
	corpus := conversationCorpus()

	// --- Load Gemma ---
	gemmaModelPath := filepath.Join(modelDir, "embeddinggemma-300m", EmbeddingGemma300MConfig().ModelFilenameOrDefault())
	gemmaTokenizerPath := filepath.Join(modelDir, "embeddinggemma-300m", "tokenizer.json")
	if _, err := os.Stat(gemmaModelPath); err != nil {
		t.Skipf("Gemma model not staged at %s", gemmaModelPath)
	}
	gemmaRuntime, err := NewONNXRuntimeWithDir(modelDir)
	if err != nil {
		t.Skipf("ONNX runtime unavailable: %v", err)
	}
	defer gemmaRuntime.Close()
	gemmaProvider, err := NewONNXEmbeddingProvider(ctx, gemmaRuntime, gemmaModelPath, gemmaTokenizerPath,
		EmbeddingGemma300MConfig().Dims, EmbeddingGemma300MConfig().FullDims)
	if err != nil {
		t.Fatalf("create Gemma provider: %v", err)
	}
	defer gemmaProvider.Close()

	// --- Load Jina ---
	jinaModelPath := filepath.Join(modelDir, "jina-code-v2", JinaCodeV2Config().ModelFilenameOrDefault())
	jinaTokenizerPath := filepath.Join(modelDir, "jina-code-v2", "tokenizer.json")
	if _, err := os.Stat(jinaModelPath); err != nil {
		t.Skipf("Jina model not staged at %s", jinaModelPath)
	}
	jinaRuntime, err := NewONNXRuntimeWithDir(modelDir)
	if err != nil {
		t.Skipf("ONNX runtime unavailable: %v", err)
	}
	defer jinaRuntime.Close()
	jinaProvider, err := NewJinaONNXEmbeddingProvider(ctx, jinaRuntime, jinaModelPath, jinaTokenizerPath)
	if err != nil {
		t.Fatalf("create Jina provider: %v", err)
	}
	defer jinaProvider.Close()

	// --- Embed all texts with both models ---
	// Gemma embeds conversation turns with NO prefix (matching ConversationStore behavior).
	// Jina ignores prefixes (symmetric), so prefix doesn't matter.
	providers := map[string]EmbeddingProvider{
		"Gemma": gemmaProvider,
		"Jina":  jinaProvider,
	}

	for name, provider := range providers {
		t.Run(name, func(t *testing.T) {
			scores := map[string][]float32{} // category → similarities

			for _, p := range corpus {
				// Embed both sides with NO prefix (conversation store behavior).
				vecA, err := provider.Embed(ctx, p.a)
				if err != nil {
					t.Fatalf("embed a: %v", err)
				}
				vecB, err := provider.Embed(ctx, p.b)
				if err != nil {
					t.Fatalf("embed b: %v", err)
				}
				sim := cosineSim(vecA, vecB)
				scores[p.category] = append(scores[p.category], sim)
			}

			// Report bands per category.
			for _, cat := range []string{"related", "adjacent", "unrelated"} {
				s := scores[cat]
				sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
				p25 := percentile(s, 0.25)
				p50 := percentile(s, 0.50)
				p75 := percentile(s, 0.75)
				min := s[0]
				max := s[len(s)-1]
				t.Logf("  %-10s n=%2d  min=%.3f  p25=%.3f  p50=%.3f  p75=%.3f  max=%.3f",
					cat, len(s), min, p25, p50, p75, max)
			}

			// Compute separation: median(related) - median(unrelated).
			medRel := percentile(scores["related"], 0.50)
			medUnrel := percentile(scores["unrelated"], 0.50)
			medAdj := percentile(scores["adjacent"], 0.50)
			sep := medRel - medUnrel

			t.Logf("  ── Separation: related(p50)=%.3f − unrelated(p50)=%.3f = %.3f", medRel, medUnrel, sep)
			t.Logf("  ── Adjacent sits at p50=%.3f (gap to related: %.3f, gap to unrelated: %.3f)",
				medAdj, medRel-medAdj, medAdj-medUnrel)

			// Verdict
			if sep >= 0.25 && medRel-medAdj >= 0.05 {
				t.Logf("  ── VERDICT: GOOD separation — safe for conversation recall")
			} else if sep >= 0.15 {
				t.Logf("  ── VERDICT: MARGINAL — workable but tighter than ideal")
			} else {
				t.Logf("  ── VERDICT: POOR — related and unrelated overlap, not suitable for recall")
			}
		})
	}
}

func percentile(sorted []float32, p float64) float32 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Floor(float64(len(sorted)) * p))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
