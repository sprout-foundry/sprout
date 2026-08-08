package llm

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// CatalogModel describes a known local model for auto-selection. Dir is the
// model directory name relative to the models root; HFRepo is the
// HuggingFace repo to download it from; MinRAM is the minimum physical RAM
// (bytes) at which the model is the recommended choice.
type CatalogModel struct {
	Name    string // canonical name (e.g. "qwen3.5-4b")
	Dir     string // directory name under the models root
	HFRepo  string // HuggingFace repo (mlx-community layout)
	MinRAM  uint64 // minimum total RAM (bytes) for this to be the pick
}

// ModelCatalog lists the models the server knows how to serve, ordered by
// size (largest first). Selection picks the largest model whose MinRAM fits
// the machine. These map to mlx-community quantized releases; the 0.8B is the
// safe fallback on small machines, the 4B is the balanced choice for 16 GB
// class machines, and the 9B is only recommended with 32 GB+.
var ModelCatalog = []CatalogModel{
	{Name: "qwen3.5-9b", Dir: "qwen3.5-9b-4bit", HFRepo: "mlx-community/Qwen3.5-9B-4bit", MinRAM: 30 * 1024 * 1024 * 1024},
	{Name: "qwen3.5-4b", Dir: "qwen3.5-4b-4bit", HFRepo: "mlx-community/Qwen3.5-4B-4bit", MinRAM: 14 * 1024 * 1024 * 1024},
	{Name: "qwen3.5-0.8b", Dir: "qwen3.5-0.8b-4bit", HFRepo: "mlx-community/Qwen3.5-0.8B-4bit", MinRAM: 0},
}

// RecommendModelForRAM picks the best model for the machine purely by RAM,
// ignoring what's installed. Use this to tell a user what to download.
// Returns nil if the catalog is empty.
func RecommendModelForRAM(ramBytes uint64) *CatalogModel {
	sorted := make([]CatalogModel, len(ModelCatalog))
	copy(sorted, ModelCatalog)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].MinRAM > sorted[j].MinRAM })
	for i := range sorted {
		if ramBytes >= sorted[i].MinRAM {
			return &sorted[i]
		}
	}
	return nil // should be unreachable — 0.8B has MinRAM 0
}

// SelectModelForRAM picks the best model from ModelCatalog for the given
// physical RAM (bytes). Returns the catalog entry (nil if the catalog is
// empty). The model must actually exist on disk — a catalog entry whose dir
// is missing is skipped so a partial install degrades gracefully to the
// next-smallest model instead of failing at load time.
func SelectModelForRAM(modelsRoot string, ramBytes uint64) (*CatalogModel, error) {
	// Largest first.
	sorted := make([]CatalogModel, len(ModelCatalog))
	copy(sorted, ModelCatalog)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].MinRAM > sorted[j].MinRAM })

	for _, m := range sorted {
		if ramBytes < m.MinRAM {
			continue // too small for this model
		}
		dir := filepath.Join(modelsRoot, m.Dir)
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue // not installed — try next smaller
		}
		// Gate the actual weights like a direct load would.
		if err := ModelMemoryGate(dir); err != nil {
			continue // weights don't fit — try next smaller
		}
		m.Dir = dir // resolve to the absolute path for the caller
		return &m, nil
	}
	return nil, fmt.Errorf("no model from catalog fits %d bytes RAM in %s (install a model or pass -model explicitly)", ramBytes, modelsRoot)
}
