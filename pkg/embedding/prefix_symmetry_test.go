package embedding

import (
	"context"
	"os"
	"testing"
)

// EmbeddingGemma uses asymmetric task prefixes: a query and the document it
// should match are deliberately embedded into different subspaces, so a
// query↔document cosine of ~0.5 is a strong match while document↔document
// similarity for near-identical text runs much higher.
//
// CheckDuplicates compares CODE against CODE, but routes through QuerySimilar,
// which applies queryPrefix. It therefore measures a code↔document score in the
// asymmetric regime and then gates it at 0.85-0.90 — a bar the symmetric regime
// reaches and the asymmetric one does not. This test measures both regimes so
// the threshold question can be settled with numbers.
//
// Opt-in: SPROUT_RETRIEVAL_EVAL=1.
func TestPrefixSymmetryAffectsDuplicateThresholds(t *testing.T) {
	if os.Getenv("SPROUT_RETRIEVAL_EVAL") != "1" {
		t.Skip("SPROUT_RETRIEVAL_EVAL unset")
	}

	ctx := context.Background()
	provider, _, err := acquireSharedONNXProvider(ctx, DefaultModelDir(), EmbeddingGemma300MConfig())
	if err != nil {
		t.Skipf("provider unavailable: %v", err)
	}

	// A realistic near-duplicate pair: same logic, renamed identifiers.
	original := `func SumInts(values []int) int {
	total := 0
	for _, v := range values {
		total += v
	}
	return total
}`
	nearDup := `func AddNumbers(nums []int) int {
	sum := 0
	for _, n := range nums {
		sum += n
	}
	return sum
}`
	unrelated := `func ParseTimestamp(raw string) (time.Time, error) {
	return time.Parse(time.RFC3339, raw)
}`
	// Same domain, different job: what "related code" injection should surface
	// but duplicate detection should not flag.
	related := `func MaxInt(values []int) int {
	best := values[0]
	for _, v := range values[1:] {
		if v > best {
			best = v
		}
	}
	return best
}`

	embed := func(text, prefix string) []float32 {
		t.Helper()
		v, err := provider.EmbedWithPrefix(ctx, text, prefix)
		if err != nil {
			t.Fatalf("embed: %v", err)
		}
		return v
	}

	// Symmetric: both sides as documents — what duplicate detection should do.
	symDup := CosineSimilarity(embed(original, documentPrefix), embed(nearDup, documentPrefix))
	symUnrel := CosineSimilarity(embed(original, documentPrefix), embed(unrelated, documentPrefix))
	symRelated := CosineSimilarity(embed(original, documentPrefix), embed(related, documentPrefix))

	// Asymmetric: candidate as query vs indexed document — what CheckDuplicates
	// actually does today via QuerySimilar.
	asymDup := CosineSimilarity(embed(nearDup, queryPrefix), embed(original, documentPrefix))
	asymUnrel := CosineSimilarity(embed(unrelated, queryPrefix), embed(original, documentPrefix))

	t.Log("")
	t.Logf("document↔document (symmetric, correct for dup detection):")
	t.Logf("    near-duplicate : %.3f", symDup)
	t.Logf("    related        : %.3f", symRelated)
	t.Logf("    unrelated      : %.3f", symUnrel)
	t.Logf("query↔document (asymmetric, what CheckDuplicates does today):")
	t.Logf("    near-duplicate : %.3f", asymDup)
	t.Logf("    unrelated      : %.3f", asymUnrel)
	t.Log("")
	t.Logf("shipping thresholds: CheckDuplicates=%.2f  file-dup-check=%.2f", 0.90, 0.85)

	if symDup <= asymDup {
		t.Logf("NOTE: symmetric did not exceed asymmetric; prefix is not the dominant factor here")
	}
	// Separation is what matters for a threshold to exist at all.
	if symDup <= symUnrel {
		t.Errorf("symmetric embedding cannot separate a near-duplicate (%.3f) from unrelated code (%.3f)", symDup, symUnrel)
	}
}
