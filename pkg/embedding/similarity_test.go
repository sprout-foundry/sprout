package embedding

import (
	"math"
	"testing"
	"time"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 2, 3},
			b:        []float32{1, 2, 3},
			expected: 1.0,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{0, 1, 0},
			expected: 0.0,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1, 2, 3},
			b:        []float32{-1, -2, -3},
			expected: -1.0,
		},
		{
			name:     "different length vectors",
			a:        []float32{1, 2, 3},
			b:        []float32{1, 2},
			expected: 0.0,
		},
		{
			name:     "empty vectors",
			a:        []float32{},
			b:        []float32{},
			expected: 0.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := CosineSimilarity(tc.a, tc.b)
			// Use approximate comparison for float32
			if math.Abs(float64(result-tc.expected)) > 1e-6 {
				t.Errorf("expected %.6f, got %.6f", tc.expected, result)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		name  string
		input []float32
		check func(t *testing.T, result []float32)
	}{
		{
			name:  "unit vector stays same",
			input: []float32{1, 0, 0},
			check: func(t *testing.T, result []float32) {
				if len(result) != 3 {
					t.Fatalf("expected length 3, got %d", len(result))
				}
				if result[0] != 1.0 || result[1] != 0.0 || result[2] != 0.0 {
					t.Errorf("expected [1, 0, 0], got %v", result)
				}
			},
		},
		{
			name:  "zero vector returns nil",
			input: []float32{0, 0, 0},
			check: func(t *testing.T, result []float32) {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
			},
		},
		{
			name:  "empty vector returns nil",
			input: []float32{},
			check: func(t *testing.T, result []float32) {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
			},
		},
		{
			name:  "non-trivial normalization",
			input: []float32{3, 4},
			check: func(t *testing.T, result []float32) {
				if len(result) != 2 {
					t.Fatalf("expected length 2, got %d", len(result))
				}
				// [3,4] normalized → [0.6, 0.8]
				if math.Abs(float64(result[0]-0.6)) > 1e-6 || math.Abs(float64(result[1]-0.8)) > 1e-6 {
					t.Errorf("expected [0.6, 0.8], got %v", result)
				}
				// Verify unit length
				norm := math.Sqrt(float64(result[0]*result[0] + result[1]*result[1]))
				if math.Abs(norm-1.0) > 1e-6 {
					t.Errorf("normalized vector has norm %.6f, expected 1.0", norm)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := Normalize(tc.input)
			tc.check(t, result)
		})
	}
}

func TestTopK(t *testing.T) {
	// Create 10 candidate vectors with known similarity to a query.
	query := []float32{1, 0, 0}
	candidates := make([]VectorRecord, 10)
	for i := range candidates {
		vec := make([]float32, 3)
		// Each candidate has decreasing similarity: candidate 0 is identical to query.
		vec[0] = float32(9-i) / 10.0
		vec[1] = float32(i) / 10.0
		vec[2] = 0
		candidates[i] = VectorRecord{
			ID:        "id",
			File:      "file.go",
			Name:      "func" + string(rune('0'+i)),
			Embedding: vec,
		}
	}

	t.Run("basic top-3", func(t *testing.T) {
		results := TopK(query, candidates, 3, 0.0)
		if len(results) != 3 {
			t.Fatalf("expected 3 results, got %d", len(results))
		}
		// Results should be sorted by descending similarity.
		for i := 1; i < len(results); i++ {
			if results[i].Similarity > results[i-1].Similarity {
				t.Errorf("results not sorted: result[%d].sim=%.4f > result[%d].sim=%.4f",
					i, results[i].Similarity, i-1, results[i-1].Similarity)
			}
		}
		// First result should have the highest similarity.
		if results[0].Record.Name != "func0" {
			t.Errorf("expected first result to be func0, got %s", results[0].Record.Name)
		}
	})

	t.Run("threshold filtering", func(t *testing.T) {
		results := TopK(query, candidates, 10, 0.5)
		// Only candidates with cosine similarity >= 0.5 should be included.
		for _, r := range results {
			if r.Similarity < 0.5 {
				t.Errorf("result below threshold: %.4f", r.Similarity)
			}
		}
	})

	t.Run("k=0 returns all above threshold", func(t *testing.T) {
		results := TopK(query, candidates, 0, 0.0)
		// With k=0 and threshold=0, all candidates pass.
		if len(results) != len(candidates) {
			t.Errorf("expected %d results, got %d", len(candidates), len(results))
		}
	})

	t.Run("empty candidates", func(t *testing.T) {
		results := TopK(query, []VectorRecord{}, 5, 0.0)
		if len(results) != 0 {
			t.Errorf("expected 0 results, got %d", len(results))
		}
	})
}

func TestScoreWithDecay(t *testing.T) {
	tolerance := 1e-4

	t.Run("recent record (0 days old)", func(t *testing.T) {
		now := time.Now()
		timestamp := now
		similarity := 0.9
		result := ScoreWithDecay(similarity, timestamp, now)
		expected := 0.9 // decay ≈ 1.0
		if math.Abs(result-expected) > tolerance {
			t.Errorf("expected %.6f, got %.6f", expected, result)
		}
	})

	t.Run("30-day half-life", func(t *testing.T) {
		now := time.Now()
		timestamp := now.Add(-30 * 24 * time.Hour)
		similarity := 1.0
		result := ScoreWithDecay(similarity, timestamp, now)
		expected := 0.5 // decay = 0.5^(30/30) = 0.5
		if math.Abs(result-expected) > tolerance {
			t.Errorf("expected %.6f, got %.6f", expected, result)
		}
	})

	t.Run("60-day decay", func(t *testing.T) {
		now := time.Now()
		timestamp := now.Add(-60 * 24 * time.Hour)
		similarity := 1.0
		result := ScoreWithDecay(similarity, timestamp, now)
		expected := 0.25 // decay = 0.5^(60/30) = 0.25
		if math.Abs(result-expected) > tolerance {
			t.Errorf("expected %.6f, got %.6f", expected, result)
		}
	})

	t.Run("90-day decay", func(t *testing.T) {
		now := time.Now()
		timestamp := now.Add(-90 * 24 * time.Hour)
		similarity := 1.0
		result := ScoreWithDecay(similarity, timestamp, now)
		expected := 0.125 // decay = 0.5^(90/30) = 0.125
		if math.Abs(result-expected) > tolerance {
			t.Errorf("expected %.6f, got %.6f", expected, result)
		}
	})

	t.Run("zero similarity", func(t *testing.T) {
		now := time.Now()
		timestamp := now.Add(-30 * 24 * time.Hour)
		similarity := 0.0
		result := ScoreWithDecay(similarity, timestamp, now)
		expected := 0.0 // 0 * decay = 0
		if result != expected {
			t.Errorf("expected %.6f, got %.6f", expected, result)
		}
	})

	t.Run("negative similarity", func(t *testing.T) {
		now := time.Now()
		timestamp := now.Add(-30 * 24 * time.Hour)
		similarity := -0.5
		result := ScoreWithDecay(similarity, timestamp, now)
		expected := -0.25 // -0.5 * 0.5 = -0.25
		if math.Abs(result-expected) > tolerance {
			t.Errorf("expected %.6f, got %.6f", expected, result)
		}
	})

	t.Run("future timestamp", func(t *testing.T) {
		now := time.Now()
		timestamp := now.Add(30 * 24 * time.Hour) // 30 days in the future
		similarity := 1.0
		result := ScoreWithDecay(similarity, timestamp, now)
		expected := 2.0 // decay = 0.5^(-30/30) = 2.0
		if math.Abs(result-expected) > tolerance {
			t.Errorf("expected %.6f, got %.6f", expected, result)
		}
	})

	t.Run("old record still contributes", func(t *testing.T) {
		now := time.Now()
		timestamp := now.Add(-365 * 24 * time.Hour)
		similarity := 1.0
		result := ScoreWithDecay(similarity, timestamp, now)
		expected := math.Pow(0.5, 365.0/30.0) // ≈ 1.5e-4
		if math.Abs(result-expected) > tolerance {
			t.Errorf("expected %.6f, got %.6f", expected, result)
		}
		if result <= 0 {
			t.Errorf("expected non-zero result for old record, got %.6f", result)
		}
	})

	t.Run("combined effect", func(t *testing.T) {
		now := time.Now()
		timestamp := now.Add(-30 * 24 * time.Hour)
		similarity := 0.8
		result := ScoreWithDecay(similarity, timestamp, now)
		expected := 0.4 // 0.8 * 0.5 = 0.4
		if math.Abs(result-expected) > tolerance {
			t.Errorf("expected %.6f, got %.6f", expected, result)
		}
	})

	t.Run("180-day heavy deprioritization", func(t *testing.T) {
		now := time.Now()
		timestamp := now.Add(-180 * 24 * time.Hour)
		similarity := 1.0
		result := ScoreWithDecay(similarity, timestamp, now)
		expected := math.Pow(0.5, 180.0/30.0) // 0.5^6 = 0.015625
		if math.Abs(result-expected) > tolerance {
			t.Errorf("expected %.6f, got %.6f", expected, result)
		}
		if result > 0.02 {
			t.Errorf("180-day-old record should be heavily deprioritized, got %.6f", result)
		}
	})
}
