package agent

import (
	"math"
	"testing"
)

// TestCosineSimilarity_Basic exercises the math on hand-picked vectors
// (orthogonal, parallel, anti-parallel) so a regression in the formula
// surfaces immediately.
func TestCosineSimilarity_Basic(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float64
	}{
		{"parallel", []float32{1, 0, 0}, []float32{2, 0, 0}, 1.0},
		{"orthogonal", []float32{1, 0, 0}, []float32{0, 1, 0}, 0.0},
		{"anti-parallel", []float32{1, 0, 0}, []float32{-1, 0, 0}, -1.0},
		{"zero a", []float32{0, 0, 0}, []float32{1, 0, 0}, 0.0},
		{"zero b", []float32{1, 0, 0}, []float32{0, 0, 0}, 0.0},
		{"length mismatch", []float32{1, 0}, []float32{1, 0, 0}, 0.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cosineSimilarity(tc.a, tc.b)
			if math.Abs(got-tc.want) > 1e-6 {
				t.Errorf("cosine(%v,%v)=%v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestLargestSimilarityDrop_NoDrop returns (0, 0) when all pairwise
// similarities are equal — there's no boundary to detect, so the worker
// falls back to the default range.
func TestLargestSimilarityDrop_NoDrop(t *testing.T) {
	vectors := [][]float32{
		{1, 0, 0}, {1, 0, 0}, {1, 0, 0}, {1, 0, 0},
	}
	idx, drop := largestSimilarityDrop(vectors)
	if drop != 0 || idx != 0 {
		t.Fatalf("expected (0,0), got (%d, %f)", idx, drop)
	}
}

// TestLargestSimilarityDrop_DetectsBoundary builds a window where the
// first half cluster on one direction and the second half cluster on
// an orthogonal direction. The drop in pairwise similarity happens at
// the cluster boundary and should be detected at that index.
func TestLargestSimilarityDrop_DetectsBoundary(t *testing.T) {
	// Six vectors: first three are along x-axis (high pairwise similarity);
	// last three are along y-axis. The drop should be between idx 2 and 3.
	vectors := [][]float32{
		{1, 0, 0},
		{0.95, 0.05, 0},
		{0.9, 0.05, 0},
		{0.05, 0.9, 0},
		{0, 1, 0},
		{0.05, 0.95, 0},
	}
	idx, drop := largestSimilarityDrop(vectors)
	// Pairwise similarities (between consecutive items):
	//   0-1: high, 1-2: high, 2-3: low, 3-4: high, 4-5: high
	// Largest drop is between sims[1] (~1.0) and sims[2] (~low) — idx 2.
	if idx != 2 {
		t.Errorf("boundary index = %d, want 2", idx)
	}
	if drop < 0.5 {
		t.Errorf("drop = %f, want substantial drop (>0.5)", drop)
	}
}

// TestLargestSimilarityDrop_TooFewVectors handles the degenerate case
// where there aren't enough vectors to even compute a pair of consecutive
// similarities. Returns the no-boundary sentinel.
func TestLargestSimilarityDrop_TooFewVectors(t *testing.T) {
	cases := [][][]float32{
		nil,
		{},
		{{1, 0}},
		{{1, 0}, {0, 1}},
	}
	for i, vectors := range cases {
		idx, drop := largestSimilarityDrop(vectors)
		if idx != 0 || drop != 0 {
			t.Errorf("case %d: expected (0,0), got (%d, %f)", i, idx, drop)
		}
	}
}
