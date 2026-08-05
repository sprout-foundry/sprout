package hnsw

import (
	"math"
	"math/rand"
	"sort"
	"testing"
)

// TestGraphRecallAgainstExhaustive measures this implementation's recall
// against exhaustive nearest-neighbour search on synthetic vectors.
//
// It exists to answer one question: when sprout's semantic search returns bad
// results, is that sprout using the library wrongly, or the library? Nothing
// here touches sprout — it is textbook usage (NewGraph defaults, Add, Search)
// on random unit vectors with ground truth computed by brute force.
//
// A production HNSW at M=16 is expected to retain ~90%+ of exhaustive recall@10.
func TestGraphRecallAgainstExhaustive(t *testing.T) {
	const (
		n    = 5000
		dims = 128
		k    = 10
	)
	rng := rand.New(rand.NewSource(7))

	vecs := make([][]float32, n)
	for i := range vecs {
		v := make([]float32, dims)
		var norm float64
		for d := range v {
			v[d] = float32(rng.NormFloat64())
			norm += float64(v[d]) * float64(v[d])
		}
		inv := float32(1 / math.Sqrt(norm))
		for d := range v {
			v[d] *= inv
		}
		vecs[i] = v
	}

	g := NewGraph[int]()
	g.M = 16
	g.Ml = 0.25
	g.Distance = CosineDistance
	for i, v := range vecs {
		g.Add(MakeNode(i, v))
	}
	if g.Len() != n {
		t.Fatalf("graph holds %d nodes, want %d", g.Len(), n)
	}

	for _, efSearch := range []int{20, 50, 200, 800} {
		g.EfSearch = efSearch

		var totalRecall float64
		const queries = 50
		for q := 0; q < queries; q++ {
			target := vecs[rng.Intn(n)]

			// Ground truth: exhaustive top-k.
			type sc struct {
				i int
				d float32
			}
			scored := make([]sc, n)
			for i, v := range vecs {
				scored[i] = sc{i, CosineDistance(v, target)}
			}
			sort.Slice(scored, func(a, b int) bool { return scored[a].d < scored[b].d })
			want := map[int]bool{}
			for _, s := range scored[:k] {
				want[s.i] = true
			}

			got := g.Search(target, k)
			hit := 0
			for _, node := range got {
				if want[node.Key] {
					hit++
				}
			}
			totalRecall += float64(hit) / float64(k)
		}
		recall := totalRecall / float64(queries)
		t.Logf("EfSearch=%-4d recall@%d = %.1f%%", efSearch, k, recall*100)

		if efSearch >= 200 && recall < 0.80 {
			t.Errorf("recall@%d is %.1f%% at EfSearch=%d on synthetic data with textbook usage — "+
				"the ANN implementation itself is losing results, independent of how sprout calls it",
				k, recall*100, efSearch)
		}
	}
}
