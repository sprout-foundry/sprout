package llm

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// CatalogModel describes a known local model for auto-selection. Dir is the
// model directory name relative to the models root; MinRAM is the minimum
// physical RAM (bytes) at which the model is the recommended choice.
type CatalogModel struct {
	Name   string // canonical name (e.g. "qwen3.5-4b")
	Dir    string // directory name under the models root
	MinRAM uint64 // minimum total RAM (bytes) for this to be the pick
}

// ModelCatalog lists the models the server knows how to serve, ordered by
// size (largest first). Selection picks the largest model whose MinRAM fits
// the machine. These map to mlx-community quantized releases; the 0.8B is the
// safe fallback on small machines, the 4B is the balanced choice for 16 GB
// class machines, and the 9B is only recommended with 32 GB+.
var ModelCatalog = []CatalogModel{
	{Name: "qwen3.5-9b", Dir: "qwen3.5-9b-4bit", MinRAM: 30 * 1024 * 1024 * 1024},
	{Name: "qwen3.5-4b", Dir: "qwen3.5-4b-4bit", MinRAM: 14 * 1024 * 1024 * 1024},
	{Name: "qwen3.5-0.8b", Dir: "qwen3.5-0.8b-4bit", MinRAM: 0},
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
