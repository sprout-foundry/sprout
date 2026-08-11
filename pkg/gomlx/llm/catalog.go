package llm

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CatalogModel describes a known local model for auto-selection. Dir is the
// model directory name relative to the models root; HFRepo is the
// HuggingFace repo to download it from; MinRAM is the minimum physical RAM
// (bytes) at which the model is the recommended choice.
type CatalogModel struct {
	Name          string // canonical name (e.g. "qwen3.5-4b")
	Dir           string // directory name under the models root
	HFRepo        string // HuggingFace repo (mlx-community layout)
	HFInclude     string // glob pattern for hf download --include (e.g. "5bit/*"); empty downloads everything
	MinRAM        uint64 // minimum total RAM (bytes) for this to be the pick
	ServerBackend string // "gomlx" (Go native) or "mlx_lm" (Python mlx_lm.server); empty defaults to "gomlx"
}

// ModelCatalog lists downloadable models, ordered from largest to smallest
// by recommended RAM. SelectModelForRAM picks the first that fits.
var ModelCatalog = []CatalogModel{
	{
		Name:      "lfm2.5-2.6b",
		Dir:       "lfm2.5-2.6b-mlx/5bit",
		HFRepo:    "LiquidAI/LFM2.5-2.6B-MLX",
		HFInclude: "5bit/*",
		MinRAM:    8 * 1024 * 1024 * 1024, // 8GB
	},
	{
		Name:   "gemma4-e2b",
		Dir:    "gemma-4-e2b-it-4bit",
		HFRepo: "mlx-community/gemma-4-e2b-it-4bit",
		MinRAM: 0,
	},
}

// InstalledModel describes a model directory found on disk. Unlike
// CatalogModel (downloadable defaults), InstalledModel is discovered at
// runtime by scanning the models directory — it captures sprout-tuned
// variants, different quantization levels, and manually-added models.
type InstalledModel struct {
	Name      string // human-readable label (directory name)
	Dir       string // absolute path to the model directory
	SizeBytes int64  // total weight files size on disk
	IsTuned   bool   // sprout-tuned variant (has "sprout-tuned" in name)
	ParamSize string // rough param count: "0.8b", "4b", "9b", etc.
	QuantBits string // quantization hint extracted from name ("4bit", "q5", "q8")
}

// ListInstalledModels scans modelsRoot for directories containing model
// weights (safetensors or gguf). Returns all found models, sorted by size
// (largest params first, then by name).
func ListInstalledModels(modelsRoot string) []InstalledModel {
	entries, err := os.ReadDir(modelsRoot)
	if err != nil {
		return nil
	}
	var models []InstalledModel
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(modelsRoot, e.Name())
		if !hasWeights(dir) {
			continue
		}
		im := InstalledModel{
			Name:      e.Name(),
			Dir:       dir,
			SizeBytes: dirSize(dir),
			IsTuned:   strings.Contains(e.Name(), "sprout-tuned"),
			ParamSize: extractParamSize(e.Name()),
			QuantBits: extractQuantBits(e.Name()),
		}
		models = append(models, im)
	}
	sort.Slice(models, func(i, j int) bool {
		pi, pj := paramOrder(models[i].ParamSize), paramOrder(models[j].ParamSize)
		if pi != pj {
			return pi > pj // larger params first
		}
		return models[i].Name < models[j].Name
	})
	return models
}

// HasWeights checks if a directory contains model weight files.
func HasWeights(dir string) bool {
	return hasWeights(dir)
}

// ExtractParamSize extracts a parameter-size string like "4b" from a model name.
func ExtractParamSize(name string) string {
	return extractParamSize(name)
}

func hasWeights(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".safetensors") ||
			strings.HasSuffix(name, ".gguf") ||
			name == "model.safetensors.index.json" {
			return true
		}
	}
	return false
}

func dirSize(dir string) int64 {
	var size int64
	filepath.Walk(dir, func(_ string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

func extractParamSize(name string) string {
	name = strings.ToLower(name)
	// Match patterns like "0.8b", "1.7b", "4b", "9b", "35b-a3b", "0.6b"
	// Scan for a number (optionally with a decimal point) followed by "b".
	for i := 0; i < len(name); i++ {
		if name[i] >= '0' && name[i] <= '9' {
			j := i
			for j < len(name) && ((name[j] >= '0' && name[j] <= '9') || name[j] == '.') {
				j++
			}
			if j > i && j < len(name) && name[j] == 'b' {
				return name[i : j+1]
			}
		}
	}
	return ""
}

func extractQuantBits(name string) string {
	name = strings.ToLower(name)
	for _, pattern := range []string{"mlx-q5", "-q5", "5bit", "mlx-q8", "-q8", "8bit", "4bit", "mlx-q4", "-q4"} {
		if strings.Contains(name, pattern) {
			return pattern
		}
	}
	if strings.Contains(name, "f16") || strings.Contains(name, "bf16") {
		return "f16"
	}
	return ""
}

func paramOrder(s string) int {
	if s == "" {
		return 0
	}
	var n float64
	fmt.Sscanf(s, "%f", &n)
	return int(n * 100)
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
// next-smaller model instead of failing at load time.
func SelectModelForRAM(modelsRoot string, ramBytes uint64) (*CatalogModel, error) {
	// Prefer sprout-tuned variants of the recommended model if installed.
	if tuned := findTunedForRAM(modelsRoot, ramBytes); tuned != nil {
		return tuned, nil
	}

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

// findTunedForRAM looks for sprout-tuned variants installed on disk that
// match the recommended model size for the given RAM. Tuned models are
// preferred over generic mlx-community ones when available.
func findTunedForRAM(modelsRoot string, ramBytes uint64) *CatalogModel {
	rec := RecommendModelForRAM(ramBytes)
	if rec == nil {
		return nil
	}
	installed := ListInstalledModels(modelsRoot)
	for _, im := range installed {
		if !im.IsTuned {
			continue
		}
		// Match by param size (e.g. "4b" in "qwen35-4b-sprout-tuned-q8").
		if im.ParamSize != "" && rec.Name != "" {
			recParams := extractParamSize(rec.Name)
			if recParams != "" && im.ParamSize == recParams {
				if err := ModelMemoryGate(im.Dir); err == nil {
					return &CatalogModel{
						Name:   im.Name,
						Dir:    im.Dir,
						HFRepo: "",
						MinRAM: rec.MinRAM,
					}
				}
			}
		}
	}
	return nil
}
