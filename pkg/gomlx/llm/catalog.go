package llm

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const gb = 1024 * 1024 * 1024

// CatalogModel describes a known local model for auto-selection. Dir is the
// model directory name relative to the models root; HFRepo is the
// HuggingFace repo to download it from.
//
// Two RAM thresholds place a model on the suggested/stretch/blocked matrix:
//   - MinRAMSelect: the minimum RAM at which a user may choose this model at
//     all (with a risk warning if below MinRAMSuggested). Below this, the
//     model is not selectable — it would not fit even with an OOM warning.
//   - MinRAMSuggested: the RAM at which this model becomes the safe default
//     (no warning). A value of math.MaxUint64 means the model never becomes
//     the unwarned default — it's always an explicit, warned choice (used
//     for the top-of-line model).
//
// ModelCatalog is ordered smallest to largest; for any given RAM budget the
// "suggested" pick is the largest entry whose MinRAMSuggested fits, and the
// "stretch" pick is the very next entry up, if its MinRAMSelect also fits —
// see TieredCatalogForRAM.
type CatalogModel struct {
	Name            string // canonical name (e.g. "qwen3.5-4b") — the stable ID used for selection
	Dir             string // directory name under the models root
	HFRepo          string // HuggingFace repo (mlx-community layout)
	HFInclude       string // glob pattern for hf download --include (e.g. "5bit/*"); empty downloads everything
	MinRAMSelect    uint64 // minimum total RAM (bytes) at which this model is selectable at all
	MinRAMSuggested uint64 // minimum total RAM (bytes) at which this model becomes the unwarned default
	ServerBackend   string // "gomlx" (Go native) or "mlx_lm" (Python mlx_lm.server); empty defaults to "gomlx"
}

// ModelCatalog lists downloadable models, ordered smallest to largest.
// Quantization defaults to Q4 ("4bit") across the board for now — Q5 is the
// long-term target once a clean export of the sprout-tuned models exists.
var ModelCatalog = []CatalogModel{
	{
		Name:            "gemma4-e2b",
		Dir:             "gemma-4-e2b-it-4bit",
		HFRepo:          "mlx-community/gemma-4-e2b-it-4bit",
		MinRAMSelect:    0,
		MinRAMSuggested: 0,
	},
	{
		Name:            "qwen3.5-4b",
		Dir:             "qwen3.5-4b-4bit",
		HFRepo:          "mlx-community/Qwen3.5-4B-4bit",
		MinRAMSelect:    8 * gb,
		MinRAMSuggested: 16 * gb,
	},
	{
		Name:            "qwen3.5-9b",
		Dir:             "qwen3.5-9b-4bit",
		HFRepo:          "mlx-community/Qwen3.5-9B-MLX-4bit",
		MinRAMSelect:    16 * gb,
		MinRAMSuggested: 24 * gb,
	},
	{
		Name: "qwen3.6-35b-a3b",
		Dir:  "qwen3.6-35b-a3b-4bit",
		// MoE: ~35B total params, ~3B active per token. Full expert set must
		// still be resident, so the memory cost tracks total params, not
		// active params.
		HFRepo:          "mlx-community/Qwen3.6-35B-A3B-4bit",
		MinRAMSelect:    32 * gb,
		MinRAMSuggested: math.MaxUint64, // top-of-line: always an explicit, warned pick
	},
}

// TierStatus classifies a catalog model against a specific RAM budget.
type TierStatus int

const (
	// TierBlocked models are too big to select even with a warning.
	TierBlocked TierStatus = iota
	// TierEligible models are smaller than the suggested tier — a safe
	// downgrade, selectable with no warning (just not the default).
	TierEligible
	// TierStretch models fit but risk OOM — selectable with a warning.
	// Exactly one tier above suggested, never more.
	TierStretch
	// TierSuggested is the safe default for this RAM budget.
	TierSuggested
)

func (s TierStatus) String() string {
	switch s {
	case TierSuggested:
		return "suggested"
	case TierStretch:
		return "stretch"
	case TierEligible:
		return "eligible"
	default:
		return "blocked"
	}
}

// TieredModel pairs a catalog entry with its classification for a specific
// RAM budget.
type TieredModel struct {
	Model  CatalogModel
	Status TierStatus
}

// sortedCatalog returns ModelCatalog sorted by MinRAMSuggested ascending
// (MaxUint64 entries — never-suggested, top-of-line models — sort last).
// This defines the tier progression TieredCatalogForRAM walks: the order a
// model becomes the suggested default is the same order it can ever be a
// stretch pick for the tier below it.
func sortedCatalog() []CatalogModel {
	sorted := make([]CatalogModel, len(ModelCatalog))
	copy(sorted, ModelCatalog)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].MinRAMSuggested < sorted[j].MinRAMSuggested })
	return sorted
}

// TieredCatalogForRAM classifies every catalog model against ramBytes:
// exactly one is TierSuggested (the safe default); everything smaller is
// TierEligible (a safe downgrade — "their machine or lower" is always
// selectable); at most one adjacent entry above suggested is TierStretch
// (selectable with a warning); anything further up is TierBlocked. All
// entries are returned — callers that want to show the full catalog (with
// blocked entries visible-but-unselectable) get that for free; callers that
// only want the pickable set can filter on Status != TierBlocked.
func TieredCatalogForRAM(ramBytes uint64) []TieredModel {
	sorted := sortedCatalog()

	suggestedIdx := 0
	for i, m := range sorted {
		if ramBytes >= m.MinRAMSuggested {
			suggestedIdx = i
		}
	}

	out := make([]TieredModel, len(sorted))
	for i, m := range sorted {
		status := TierBlocked
		switch {
		case i == suggestedIdx:
			status = TierSuggested
		case i < suggestedIdx:
			status = TierEligible
		case i == suggestedIdx+1 && ramBytes >= m.MinRAMSelect:
			status = TierStretch
		}
		out[i] = TieredModel{Model: m, Status: status}
	}
	return out
}

// SuggestedForRAM returns the safe-default catalog model for ramBytes.
func SuggestedForRAM(ramBytes uint64) CatalogModel {
	for _, tm := range TieredCatalogForRAM(ramBytes) {
		if tm.Status == TierSuggested {
			return tm.Model
		}
	}
	// Unreachable: the smallest catalog entry always has MinRAMSuggested 0.
	return ModelCatalog[0]
}

// SelectableForRAM reports whether a named catalog model (by CatalogModel.Name)
// can be selected at ramBytes, and its tier status. Used to gate `/model`
// selection: TierBlocked should be refused (subject to the
// SPROUT_ALLOW_OVERWEIGHT escape hatch used elsewhere in this package for
// the same purpose), TierStretch should warn but proceed.
func SelectableForRAM(name string, ramBytes uint64) (TierStatus, bool) {
	for _, tm := range TieredCatalogForRAM(ramBytes) {
		if strings.EqualFold(tm.Model.Name, name) {
			return tm.Status, true
		}
	}
	return TierBlocked, false
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
func RecommendModelForRAM(ramBytes uint64) *CatalogModel {
	m := SuggestedForRAM(ramBytes)
	return &m
}

// SelectModelForRAM picks the best model from ModelCatalog for the given
// physical RAM (bytes) that is actually installed and fits, preferring
// sprout-tuned variants. Returns the catalog entry resolved to an absolute
// directory (nil if nothing installed fits).
//
// Candidate order: the suggested (safe) tier first, then progressively
// smaller tiers, and — only if nothing at or below the suggested tier is
// installed at all — the one stretch tier up, never further. This mirrors
// bestInstalledModel's philosophy elsewhere in this package (the
// recommendation is a suggestion, not a hard gate: a machine with only a
// larger tuned model installed should still run it rather than refuse to
// start), while still never auto-selecting a genuinely blocked model.
// Interactive selection of a stretch model with an explicit warning is a
// separate, user-initiated path (see SelectableForRAM) — this is only the
// unattended "what loads with no explicit choice" path.
//
// A catalog entry whose dir is missing is skipped so a partial install
// degrades gracefully to the next candidate instead of failing at load time.
func SelectModelForRAM(modelsRoot string, ramBytes uint64) (*CatalogModel, error) {
	tiered := TieredCatalogForRAM(ramBytes)
	suggestedIdx := 0
	for i, tm := range tiered {
		if tm.Status == TierSuggested {
			suggestedIdx = i
		}
	}

	order := make([]int, 0, suggestedIdx+2)
	for i := suggestedIdx; i >= 0; i-- {
		order = append(order, i)
	}
	if suggestedIdx+1 < len(tiered) {
		order = append(order, suggestedIdx+1)
	}

	for _, i := range order {
		m := tiered[i].Model
		if tuned := findTunedVariantForModel(modelsRoot, m); tuned != nil {
			return tuned, nil
		}
		dir := filepath.Join(modelsRoot, m.Dir)
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue // not installed — try next candidate
		}
		if err := ModelMemoryGate(dir); err != nil {
			continue // weights don't fit — try next candidate
		}
		m.Dir = dir // resolve to the absolute path for the caller
		return &m, nil
	}
	return nil, fmt.Errorf("no model from catalog fits %d bytes RAM in %s (install a model or pass -model explicitly)", ramBytes, modelsRoot)
}

// findTunedVariantForModel looks for a sprout-tuned variant installed on
// disk matching m's parameter size, preferred over m's own plain catalog
// dir when present (mlx-q5 > q5 > q8 > unquantized — see preferTunedQuant).
func findTunedVariantForModel(modelsRoot string, m CatalogModel) *CatalogModel {
	targetParams := extractParamSize(m.Name)
	if targetParams == "" {
		return nil
	}
	installed := ListInstalledModels(modelsRoot)
	var bestTuned *InstalledModel
	for i := range installed {
		im := &installed[i]
		if !im.IsTuned || im.ParamSize != targetParams {
			continue
		}
		if err := ModelMemoryGate(im.Dir); err != nil {
			continue
		}
		if bestTuned == nil || preferTunedQuant(im, bestTuned) {
			bestTuned = im
		}
	}
	if bestTuned == nil {
		return nil
	}
	return &CatalogModel{
		Name:            bestTuned.Name,
		Dir:             bestTuned.Dir,
		MinRAMSelect:    m.MinRAMSelect,
		MinRAMSuggested: m.MinRAMSuggested,
	}
}

// preferTunedQuant returns true if a is a better quantization than b for
// local inference. Prefers mlx-q5 (balanced speed/quality) over q5 and q8.
func preferTunedQuant(a, b *InstalledModel) bool {
	score := func(q string) int {
		switch q {
		case "mlx-q5":
			return 3
		case "q5", "-q5":
			return 2
		case "q8", "-q8":
			return 1
		default:
			return 0
		}
	}
	return score(a.QuantBits) > score(b.QuantBits)
}
