package embedding

import (
	"math"
	"slices"
)

// CosineSimilarity computes the cosine similarity between two vectors.
// It returns a value in [-1, 1], where 1 means identical direction.
// Vectors of different lengths return 0.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	if len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	normA = math.Sqrt(normA)
	normB = math.Sqrt(normB)

	if normA == 0 || normB == 0 {
		return 0
	}

	return float32(dot / (normA * normB))
}

// Normalize returns a new vector that has been normalized to unit length.
// If the input vector is zero-length or empty, it returns an empty slice.
func Normalize(v []float32) []float32 {
	if len(v) == 0 {
		return nil
	}

	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}

	norm = math.Sqrt(norm)
	if norm == 0 {
		return nil
	}

	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / norm)
	}
	return out
}

// TopK returns the top-K VectorRecord matches for a query vector from
// the given candidates, filtering out results below threshold.
// Results are sorted by descending similarity.
// If k <= 0, all results above threshold are returned.
func TopK(query []float32, candidates []VectorRecord, k int, threshold float32) []QueryResult {
	var scored []QueryResult
	for i := range candidates {
		s := CosineSimilarity(query, candidates[i].Embedding)
		if s >= threshold {
			scored = append(scored, QueryResult{
				Record:     candidates[i],
				Similarity: s,
			})
		}
	}

	slices.SortFunc(scored, func(a, b QueryResult) int {
		if a.Similarity > b.Similarity {
			return -1
		}
		if a.Similarity < b.Similarity {
			return 1
		}
		return 0
	})

	if k > 0 && k < len(scored) {
		scored = scored[:k]
	}

	return scored
}
