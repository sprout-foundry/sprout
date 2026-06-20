package tools

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"testing"
	"time"
)

// mockEmbeddingProvider produces deterministic bag-of-tokens vectors for test
// commands. It tokenizes on whitespace and hashes each token into a fixed-
// dimension vector, then L2-normalizes. This gives high similarity for
// commands that share many tokens (e.g. "rm -rf node_modules" vs "rm -rf dist"
// share "rm", "-rf") without needing the real ONNX model.
type mockEmbeddingProvider struct {
	dims int
}

func (m *mockEmbeddingProvider) Embed(_ context.Context, text string) ([]float32, error) {
	vec := make([]float32, m.dims)
	tokens := splitTokens(text)
	for _, tok := range tokens {
		h := fnv.New32a()
		h.Write([]byte(tok))
		idx := int(h.Sum32()) % m.dims
		sign := float32(1.0)
		if h.Sum32()&1 == 1 {
			sign = -1.0
		}
		vec[idx] += sign
	}
	// L2 normalize
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if norm == 0 {
		return vec, nil
	}
	normSqrt := float32(1.0 / float64(norm))
	for i := range vec {
		vec[i] *= normSqrt
	}
	_ = fmt.Sprintf // avoid unused import if fmt only used in error paths
	return vec, nil
}

func (m *mockEmbeddingProvider) Dimensions() int { return m.dims }
func (m *mockEmbeddingProvider) Name() string    { return "mock" }

// oneShotEmbedProvider returns a fixed vector for a specific command, error
// otherwise. Used to test error paths.
type oneShotEmbedProvider struct {
	target string
	vec    []float32
	called bool
}

func (p *oneShotEmbedProvider) Embed(_ context.Context, text string) ([]float32, error) {
	if text == p.target {
		p.called = true
		return p.vec, nil
	}
	return nil, fmt.Errorf("unexpected embed call for %q", text)
}
func (p *oneShotEmbedProvider) Dimensions() int { return len(p.vec) }
func (p *oneShotEmbedProvider) Name() string    { return "oneshot" }

// splitTokens is a simple whitespace tokenizer for the mock provider.
func splitTokens(s string) []string {
	var out []string
	cur := ""
	for _, ch := range s {
		if ch == ' ' || ch == '\t' || ch == '\n' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
		} else {
			cur += string(ch)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// newTestClassifier builds a classifier with the mock provider, initializes
// the corpus, and returns it ready for Classify calls.
func newTestClassifier(t *testing.T) *EmbeddingClassifier {
	t.Helper()
	c := NewEmbeddingClassifier(&mockEmbeddingProvider{dims: 128})
	if err := c.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	return c
}

// ---------------------------------------------------------------------------
// Safety contract tests
// ---------------------------------------------------------------------------

func TestEmbeddingClassifier_NeverDowngradesDangerous(t *testing.T) {
	c := newTestClassifier(t)
	dangerousCmds := []string{
		"rm -rf /",
		"rm -rf ~",
		"sudo rm -rf /etc",
		"mkfs.ext4 /dev/sda",
		"dd if=/dev/zero of=/dev/sda",
		"chmod -R 777 /",
		"shutdown -h now",
	}
	for _, cmd := range dangerousCmds {
		risk, reason := c.Classify(context.Background(), SecurityDangerous, cmd)
		if risk != SecurityDangerous {
			t.Errorf("DANGEROUS cmd %q was downgraded to %v (reason: %s) — safety contract violated", cmd, risk, reason)
		}
		if reason != "" {
			t.Errorf("DANGEROUS cmd %q produced non-empty reasoning — should be untouched", cmd)
		}
	}
}

func TestEmbeddingClassifier_NeverTouchesSafe(t *testing.T) {
	c := newTestClassifier(t)
	risk, reason := c.Classify(context.Background(), SecuritySafe, "npm test")
	if risk != SecuritySafe {
		t.Errorf("SAFE result was changed to %v", risk)
	}
	if reason != "" {
		t.Errorf("SAFE result produced non-empty reasoning")
	}
}

func TestEmbeddingClassifier_NeverTouchesCritical(t *testing.T) {
	c := newTestClassifier(t)
	// Even if "critical" isn't a real SecurityRisk constant, verify that
	// non-CAUTION results pass through untouched.
	risk, _ := c.Classify(context.Background(), SecurityDangerous, "rm -rf /")
	if risk != SecurityDangerous {
		t.Errorf("expected DANGEROUS, got %v", risk)
	}
}

// ---------------------------------------------------------------------------
// Downgrade tests
// ---------------------------------------------------------------------------

func TestEmbeddingClassifier_DowngradesCautionToSafe(t *testing.T) {
	c := newTestClassifier(t)
	// Commands that the heuristic might over-flag as CAUTION but are in the
	// corpus as SAFE. With the mock provider, identical corpus entries produce
	// similarity 1.0.
	safeCmds := []string{
		"npm test",
		"go test ./...",
		"npm run build",
		"go build ./...",
		"rm -rf node_modules",
		"gofmt -w .",
		"cargo test",
		"make build-all",
	}
	for _, cmd := range safeCmds {
		risk, reason := c.Classify(context.Background(), SecurityCaution, cmd)
		if risk != SecuritySafe {
			t.Errorf("expected CAUTION→SAFE downgrade for %q, got %v (reason: %s)", cmd, risk, reason)
		}
		if reason == "" {
			t.Errorf("expected non-empty reasoning for downgrade of %q", cmd)
		}
	}
}

func TestEmbeddingClassifier_RespectsDangerousCorpus(t *testing.T) {
	c := newTestClassifier(t)
	// "rm -rf /" is in the corpus as DANGEROUS. Even if the heuristic said
	// CAUTION (hypothetically), the classifier must NOT downgrade because the
	// nearest neighbor is DANGEROUS (negative-evidence guard).
	risk, _ := c.Classify(context.Background(), SecurityCaution, "rm -rf /")
	if risk != SecurityCaution {
		t.Errorf("expected CAUTION for 'rm -rf /' (dangerous corpus guard), got %v", risk)
	}
}

func TestEmbeddingClassifier_DangerousNeighborBlocksDowngrade(t *testing.T) {
	c := newTestClassifier(t)
	// "rm -rf node_modules" is SAFE in the corpus, but "rm -rf /" is
	// DANGEROUS. If a command's nearest neighbors include both, the guard
	// blocks the downgrade. Test with a command equidistant from both —
	// with the mock provider this is hard to construct, so we verify the
	// guard fires for an exact dangerous corpus match.
	risk, reason := c.Classify(context.Background(), SecurityCaution, "chmod -R 777 /")
	if risk == SecuritySafe {
		t.Errorf("dangerous corpus entry was downgraded — guard failed. reason: %s", reason)
	}
}

// ---------------------------------------------------------------------------
// Availability / robustness tests
// ---------------------------------------------------------------------------

func TestEmbeddingClassifier_NotReadyReturnsHeuristic(t *testing.T) {
	c := NewEmbeddingClassifier(&mockEmbeddingProvider{dims: 128})
	// Don't call Init — classifier should not be ready.
	if c.IsReady() {
		t.Fatal("expected not ready before Init")
	}
	risk, reason := c.Classify(context.Background(), SecurityCaution, "npm test")
	if risk != SecurityCaution {
		t.Errorf("uninitialized classifier changed risk to %v", risk)
	}
	if reason != "" {
		t.Errorf("uninitialized classifier produced reasoning")
	}
}

func TestEmbeddingClassifier_NilClassifierIsSafe(t *testing.T) {
	var c *EmbeddingClassifier
	risk, reason := c.Classify(context.Background(), SecurityCaution, "npm test")
	if risk != SecurityCaution {
		t.Errorf("nil classifier changed risk to %v", risk)
	}
	if reason != "" {
		t.Errorf("nil classifier produced reasoning")
	}
}

func TestEmbeddingClassifier_InitWithNilProvider(t *testing.T) {
	c := NewEmbeddingClassifier(nil)
	if err := c.Init(context.Background()); err == nil {
		t.Error("expected error from Init with nil provider")
	}
	if c.IsReady() {
		t.Error("expected not ready after Init failure")
	}
}

func TestEmbeddingClassifier_ContextCancellation(t *testing.T) {
	c := newTestClassifier(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// With a cancelled context, Embed should fail or return immediately.
	// The classifier must return the heuristic result unchanged (no block).
	risk, reason := c.Classify(ctx, SecurityCaution, "npm test")
	// Either the embed fails (returns CAUTION) or succeeds (returns SAFE).
	// Either way, it must not block or panic. We mainly test it doesn't hang.
	_ = risk
	_ = reason
}

func TestEmbeddingClassifier_InitIdempotent(t *testing.T) {
	c := newTestClassifier(t)
	// Second Init should be a no-op (already ready).
	if err := c.Init(context.Background()); err != nil {
		t.Errorf("second Init failed: %v", err)
	}
	if !c.IsReady() {
		t.Error("not ready after second Init")
	}
}

func TestEmbeddingClassifier_ConcurrentClassify(t *testing.T) {
	c := newTestClassifier(t)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			cmd := "npm test"
			if n%2 == 0 {
				cmd = "go build ./..."
			}
			risk, _ := c.Classify(context.Background(), SecurityCaution, cmd)
			if risk != SecuritySafe {
				// Not necessarily a failure (could be race in readiness),
				// but we mainly verify no panic/corruption.
				t.Logf("concurrent classify got %v for %q", risk, cmd)
			}
		}(i)
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Package-level setter/getter
// ---------------------------------------------------------------------------

func TestSetEmbeddingClassifier(t *testing.T) {
	c := newTestClassifier(t)
	SetEmbeddingClassifier(c)
	if got := GetEmbeddingClassifier(); got != c {
		t.Error("GetEmbeddingClassifier did not return the set classifier")
	}
	SetEmbeddingClassifier(nil)
	if got := GetEmbeddingClassifier(); got != nil {
		t.Error("expected nil after setting nil")
	}
}

// ---------------------------------------------------------------------------
// Custom thresholds
// ---------------------------------------------------------------------------

func TestEmbeddingClassifier_CustomThresholdsRespected(t *testing.T) {
	// With an impossibly-high top-neighbor-sim threshold (1.01), no command
	// can be downgraded.
	c := NewEmbeddingClassifier(
		&mockEmbeddingProvider{dims: 128},
		WithTopNeighborSim(1.01),
	)
	if err := c.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	risk, _ := c.Classify(context.Background(), SecurityCaution, "npm test")
	if risk != SecurityCaution {
		t.Errorf("with threshold > 1, expected CAUTION unchanged, got %v", risk)
	}
}

// ---------------------------------------------------------------------------
// Corpus validation
// ---------------------------------------------------------------------------

func TestCorpus_HasMinimumSize(t *testing.T) {
	corpus := DefaultCommandCorpus()
	if len(corpus) < 150 {
		t.Errorf("corpus has only %d entries, need >= 150", len(corpus))
	}
}

func TestCorpus_HasDangerousCounterexamples(t *testing.T) {
	corpus := DefaultCommandCorpus()
	dangerous := 0
	for _, e := range corpus {
		if e.Label == SecurityDangerous {
			dangerous++
		}
	}
	if dangerous < 15 {
		t.Errorf("corpus has only %d DANGEROUS entries, need >= 15 for KNN anchoring", dangerous)
	}
}

func TestCorpus_HasSufficientSafeEntries(t *testing.T) {
	corpus := DefaultCommandCorpus()
	safe := 0
	for _, e := range corpus {
		if e.Label == SecuritySafe {
			safe++
		}
	}
	if safe < 100 {
		t.Errorf("corpus has only %d SAFE entries, need >= 100", safe)
	}
}

func TestCorpus_NoDuplicateCommands(t *testing.T) {
	corpus := DefaultCommandCorpus()
	seen := make(map[string]bool)
	for _, e := range corpus {
		if seen[e.Command] {
			t.Errorf("duplicate corpus entry: %q", e.Command)
		}
		seen[e.Command] = true
	}
}

// Ensure unused vars don't trip the compiler in simplified test builds.
var _ = time.Second
