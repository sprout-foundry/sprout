package localmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// ModelStatus describes a model from the user's perspective — either a
// downloadable catalog entry or an installed model discovered on disk.
// Thresholds here are RAM-agnostic (the raw catalog values); classifying a
// model as suggested/stretch/blocked for a specific machine is done
// separately via llm.TieredCatalogForRAM, which needs actual RAM in hand.
type ModelStatus struct {
	Name      string `json:"name"`
	Dir       string `json:"dir"`
	HFRepo    string `json:"hf_repo"`
	HFInclude string `json:"hf_include,omitempty"`
	// MinRAM is the minimum RAM to select this model at all (with a warning
	// if below MinRAMSuggested). 0 for the entry-level model.
	MinRAM uint64 `json:"min_ram_gb"`
	// MinRAMSuggested is the RAM at which this model becomes the unwarned
	// default. Capped display-side for the top-of-line model, whose real
	// threshold is intentionally unbounded (it never becomes an unwarned
	// default — see catalog.go) rather than showing a nonsensical value.
	MinRAMSuggested uint64 `json:"min_ram_suggested_gb,omitempty"`
	Installed       bool   `json:"installed"`
	Size            int64  `json:"size_bytes"`
	IsTuned         bool   `json:"is_tuned"`
	ParamSize       string `json:"param_size"`
	QuantBits       string `json:"quant_bits"`
	ServerBackend   string `json:"server_backend,omitempty"`
}

// ProgressCallback is called during model download with bytes downloaded
// and total bytes (0 if unknown).
type ProgressCallback func(downloaded, total int64)

// TotalSystemRAM returns this machine's physical RAM in bytes, for callers
// outside this package that need it for RAM-tier gate checks (e.g. the
// /model CLI command, deciding whether to download a selection before
// LocalProvider.SetModel's own gate would reject it).
func TotalSystemRAM() uint64 {
	return tensorTotalSystemRAM()
}

// ListModels returns all models: installed variants discovered on disk
// (including sprout-tuned, different quant levels) plus downloadable
// catalog entries that aren't installed. Installed models are listed first,
// sorted by param size (largest first).
func ListModels() []ModelStatus {
	installed := llm.ListInstalledModels(DefaultModelsDir)
	var statuses []ModelStatus

	// dirBasenameToCatalogName maps a catalog entry's default install
	// directory (e.g. "qwen3.5-9b-4bit") back to its stable, user-facing
	// catalog Name ("qwen3.5-9b") — the ID /model and ResolveModelID
	// expect. An installed model discovered by directory scan otherwise
	// only carries its raw directory basename as Name, which is fine for
	// sprout-tuned variants (their directory names aren't catalog
	// entries), but for a PLAIN catalog download sitting under its
	// default directory, using the basename as Name silently orphans the
	// catalog Name: nothing in the returned list would carry it, so
	// ResolveModelID("qwen3.5-9b") and the /model listing's installed-status
	// check both fail even though the model is fully downloaded.
	dirBasenameToCatalogName := make(map[string]string, len(llm.ModelCatalog))
	for _, m := range llm.ModelCatalog {
		dirBasenameToCatalogName[m.Dir] = m.Name
	}

	seen := make(map[string]bool)

	for _, im := range installed {
		name := im.Name
		if !im.IsTuned {
			if catalogName, ok := dirBasenameToCatalogName[im.Name]; ok {
				name = catalogName
			}
		}
		statuses = append(statuses, ModelStatus{
			Name:      name,
			Dir:       im.Dir,
			Installed: true,
			Size:      im.SizeBytes,
			IsTuned:   im.IsTuned,
			ParamSize: im.ParamSize,
			QuantBits: im.QuantBits,
		})
		seen[name] = true
	}

	// Add downloadable catalog entries that aren't installed.
	for _, m := range llm.ModelCatalog {
		dir := filepath.Join(DefaultModelsDir, m.Dir)
		if seen[m.Name] {
			continue
		}
		installedOnDisk := false
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			installedOnDisk = hasModelWeights(dir)
		}
		// MinRAM reflects the "can select at all" threshold (MinRAMSelect) —
		// this field answers "how much RAM do I need to use this model",
		// which is what a download-decision UI needs. MinRAMSuggested is
		// capped at MinRAM for the top-of-line model instead of dividing its
		// real (intentionally unbounded — see catalog.go) threshold, which
		// would otherwise display a nonsensical GB figure.
		minRAMSuggestedGB := m.MinRAMSelect / (1024 * 1024 * 1024)
		if m.MinRAMSuggested != math.MaxUint64 {
			minRAMSuggestedGB = m.MinRAMSuggested / (1024 * 1024 * 1024)
		}
		statuses = append(statuses, ModelStatus{
			Name:            m.Name,
			Dir:             dir,
			HFRepo:          m.HFRepo,
			HFInclude:       m.HFInclude,
			MinRAM:          m.MinRAMSelect / (1024 * 1024 * 1024),
			MinRAMSuggested: minRAMSuggestedGB,
			Installed:       installedOnDisk,
			ParamSize:       llm.ExtractParamSize(m.Name),
			ServerBackend:   m.ServerBackend,
		})
		seen[m.Name] = true
	}

	return statuses
}

// RecommendedModel returns the best model for the machine's RAM that is
// already installed, preferring sprout-tuned variants. Delegates to
// llm.SelectModelForRAM so this shares the same RAM-gate and quant
// preference (mlx-q5 > q5 > q8 > unquantized) logic as onboarding and
// the standalone server — see preferTunedQuant.
func RecommendedModel(ramBytes uint64) *ModelStatus {
	picked, err := llm.SelectModelForRAM(DefaultModelsDir, ramBytes)
	if err != nil || picked == nil {
		return nil
	}
	return &ModelStatus{
		Name:          picked.Name,
		Dir:           picked.Dir,
		HFRepo:        picked.HFRepo,
		Installed:     true,
		ParamSize:     llm.ExtractParamSize(picked.Name),
		ServerBackend: picked.ServerBackend,
	}
}

// quantScore ranks quantization preference for tuned-variant selection:
// mlx-q5 (balanced speed/quality) > q5 > q8 > unquantized/other. Mirrors
// llm's unexported preferTunedQuant scoring (kept local — it's a 4-line
// switch, not worth plumbing across the package boundary).
func quantScore(q string) int {
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

// ResolveModelID finds the ModelStatus for a stable model ID — either a
// catalog tier Name (e.g. "qwen3.5-9b", preferring an installed sprout-tuned
// variant of the same size, matching SelectModelForRAM's own preference) or
// an installed directory's exact basename. Returns an error if unknown.
func ResolveModelID(id string) (*ModelStatus, error) {
	statuses := ListModels()

	for _, cm := range llm.ModelCatalog {
		if !strings.EqualFold(cm.Name, id) {
			continue
		}
		targetSize := llm.ExtractParamSize(cm.Name)
		var best *ModelStatus
		for i := range statuses {
			s := &statuses[i]
			if s.Installed && s.IsTuned && targetSize != "" && s.ParamSize == targetSize {
				if best == nil || quantScore(s.QuantBits) > quantScore(best.QuantBits) {
					best = s
				}
			}
		}
		if best != nil {
			return best, nil
		}
		break // no tuned variant — fall through to the plain catalog entry below
	}

	for i := range statuses {
		if strings.EqualFold(statuses[i].Name, id) || strings.EqualFold(filepath.Base(statuses[i].Dir), id) {
			return &statuses[i], nil
		}
	}
	return nil, fmt.Errorf("unknown local model %q", id)
}

// TieredModelInfos builds the RAM-tier catalog matrix as api.ModelInfo
// entries for a given RAM budget — see llm.TieredCatalogForRAM. Every tier
// is included (so /model shows the full roadmap, not just what's pickable
// today), but only the suggested and stretch tiers carry EligibleRoles;
// blocked tiers get an explanatory Description and no eligible/recommended
// roles, matching existing picker conventions for "not really pickable".
// Actual selection is enforced separately in LocalProvider.SetModel — this
// only informs the picker.
func TieredModelInfos(ram uint64) []api.ModelInfo {
	tiered := llm.TieredCatalogForRAM(ram)
	infos := make([]api.ModelInfo, 0, len(tiered))
	ramGB := float64(ram) / (1024 * 1024 * 1024)

	for _, tm := range tiered {
		status, _ := ResolveModelID(tm.Model.Name)
		installed := status != nil && status.Installed

		info := api.ModelInfo{ID: tm.Model.Name, Name: tm.Model.Name}
		// A tier whose ResolveModelID pick is an installed sprout-tuned
		// variant will load those weights, not the plain catalog download —
		// without this annotation the tuned build hides behind the bare
		// catalog name with no visible trace in the picker.
		if installed && status.IsTuned {
			info.Name = tunedLabel(tm.Model.Name, status.QuantBits)
			info.Tags = append(info.Tags, "sprout-tuned")
		}
		selectGB := float64(tm.Model.MinRAMSelect) / (1024 * 1024 * 1024)

		switch tm.Status {
		case llm.TierSuggested:
			info.Description = fmt.Sprintf("Suggested for this machine (%.0f GB RAM)", ramGB)
			info.EligibleRoles = []string{"primary", "subagent"}
			info.RecommendedRoles = []string{"primary", "subagent"}
		case llm.TierEligible:
			info.Description = "Smaller than the suggested model for this machine — a safe, lighter-weight choice"
			info.EligibleRoles = []string{"primary", "subagent"}
		case llm.TierStretch:
			if tm.Model.MinRAMSuggested == math.MaxUint64 {
				info.Description = fmt.Sprintf("Fits, but risks running out of memory — top-of-line model, always a manual choice (this machine has %.0f GB)", ramGB)
			} else {
				suggestGB := float64(tm.Model.MinRAMSuggested) / (1024 * 1024 * 1024)
				info.Description = fmt.Sprintf("Fits, but risks running out of memory — %.0f GB+ recommended, this machine has %.0f GB", suggestGB, ramGB)
			}
			info.EligibleRoles = []string{"primary", "subagent"}
			info.Warnings = []string{"Risk of running out of memory on this machine"}
		default:
			info.Description = fmt.Sprintf("Requires %.0f GB+ RAM — this machine has %.0f GB", selectGB, ramGB)
		}
		if !installed {
			info.Tags = append(info.Tags, "not downloaded")
		}
		infos = append(infos, info)
	}

	// Installed sprout-tuned builds beyond the catalog tiers (a tuned 9B/12B
	// with no matching catalog entry would otherwise be invisible to every
	// model picker — resolvable only by exact directory basename). Append
	// them as explicit rows so /model and the WebUI picker can select them
	// directly; a tier whose catalog entry already resolves to this same
	// directory (ResolveModelID's tuned preference) is skipped to avoid
	// duplicate rows for one set of weights.
	resolved := make(map[string]bool, len(infos))
	for i := range infos {
		if status, err := ResolveModelID(infos[i].ID); err == nil {
			resolved[status.Dir] = true
		}
	}
	for _, s := range ListModels() {
		if !s.Installed || !s.IsTuned || resolved[s.Dir] {
			continue
		}
		warn := ""
		if s.Size*2 > int64(ram) {
			warn = fmt.Sprintf("Weights %.1f GB exceed half this machine's RAM (%.0f GB) — needs SPROUT_ALLOW_OVERWEIGHT=1 and risks swapping",
				float64(s.Size)/1073741824, ramGB)
		}
		infos = append(infos, api.ModelInfo{
			ID:   filepath.Base(s.Dir),
			Name: tunedLabel(s.Name, s.QuantBits),
			Description: fmt.Sprintf("Sprout-tuned build installed on this machine — %s, %.1f GB%s",
				s.ParamSize, float64(s.Size)/1073741824, ternaryStr(warn != "", " — overweight", "")),
			EligibleRoles: []string{"primary", "subagent"},
			Warnings:      ternaryWarn(warn),
			Tags:          []string{"local", "sprout-tuned"},
		})
	}
	return infos
}

func ternaryStr(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// tunedLabel renders a picker label for a model backed by an installed
// sprout-tuned variant, e.g. "qwen3.5-9b (sprout-tuned mlx-q5)". Shared by
// tier rows whose ResolveModelID pick resolves to a tuned variant and the
// explicit beyond-tier rows in TieredModelInfos. Quant is the variant's
// extractQuantBits hint ("" → no quant suffix, dropping the stray trailing
// space the old direct Sprintf produced: "(sprout-tuned )").
func tunedLabel(catalogName, quant string) string {
	if quant == "" {
		return fmt.Sprintf("%s (sprout-tuned)", catalogName)
	}
	return fmt.Sprintf("%s (sprout-tuned %s)", catalogName, quant)
}

func ternaryWarn(w string) []string {
	if w == "" {
		return nil
	}
	return []string{w}
}

// EnsureModel downloads a model if it's not already installed.
func EnsureModel(ctx context.Context, status ModelStatus, progressFn ProgressCallback) (string, error) {
	if status.Installed {
		return status.Dir, nil
	}

	bin := "hf"
	if _, err := exec.LookPath("hf"); err != nil {
		bin = "huggingface-cli"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return "", fmt.Errorf("huggingface CLI not found — install with: pip install -U huggingface_hub")
	}

	dest := status.Dir
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("create models dir: %w", err)
	}

	// When HFInclude is set, files are downloaded with their repo path
	// prefix preserved. Download to the parent of dest so the include
	// subdir lands correctly under dest.
	localDir := dest
	if status.HFInclude != "" {
		localDir = filepath.Dir(dest)
	}
	args := []string{"download", status.HFRepo}
	if status.HFInclude != "" {
		args = append(args, "--include", status.HFInclude)
	}
	args = append(args, "--local-dir", localDir)
	cmd := exec.CommandContext(ctx, bin, args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("pipe stderr: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("pipe stdout: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start download: %w", err)
	}

	// Drain stdout/stderr so the subprocess never blocks on a full pipe
	// buffer — hf download writes nothing to either when piped (see
	// pollDownloadProgress's doc comment), but the pipes still need a
	// reader.
	go io.Copy(io.Discard, stderr)
	go io.Copy(io.Discard, stdout)

	stopPoll := make(chan struct{})
	pollDone := make(chan struct{})
	go func() {
		defer close(pollDone)
		pollDownloadProgress(localDir, stopPoll, progressFn)
	}()

	waitErr := cmd.Wait()
	close(stopPoll)
	<-pollDone
	if waitErr != nil {
		return "", fmt.Errorf("download failed: %w", waitErr)
	}

	patchTokenizerConfig(dest)

	return dest, nil
}

// patchTokenizerConfig patches tokenizer_config.json for models whose chat
// template tool-call format isn't auto-detected by mlx_lm's inference logic.
// Currently handles LFM2 models (need tool_parser_type=pythonic).
func patchTokenizerConfig(modelDir string) {
	configPath := filepath.Join(modelDir, "config.json")
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return
	}

	var cfg struct {
		ModelType string `json:"model_type"`
	}
	if json.Unmarshal(configData, &cfg) != nil {
		return
	}

	var toolParser string
	switch cfg.ModelType {
	case "lfm2":
		toolParser = "pythonic"
	default:
		return
	}

	tokPath := filepath.Join(modelDir, "tokenizer_config.json")
	tokData, err := os.ReadFile(tokPath)
	if err != nil {
		return
	}
	var tokConfig map[string]interface{}
	if json.Unmarshal(tokData, &tokConfig) != nil {
		return
	}
	if existing, ok := tokConfig["tool_parser_type"].(string); ok && existing == toolParser {
		return
	}
	tokConfig["tool_parser_type"] = toolParser
	patched, err := json.MarshalIndent(tokConfig, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(tokPath, patched, 0o644)
}

// pollDownloadProgress reports download progress by periodically measuring
// how many bytes have landed in dest, until stop is closed.
//
// The obvious approach — scan the hf/huggingface-cli subprocess's stdout
// and stderr for a percentage — doesn't work: confirmed empirically (piped
// hf download, --format human/json/default all tried) that it writes
// ZERO bytes to either stream when not connected to a real terminal,
// regardless of --format. Its progress bars are tqdm-based and gated on
// isatty(), which a Go exec.Cmd pipe never satisfies. So the total is
// unknown up front (fn is called with total=0 — callers show bytes
// downloaded so far rather than a percentage) and progress is inferred
// from disk instead, which works regardless of the download tool's own
// TTY detection.
func pollDownloadProgress(dest string, stop <-chan struct{}, fn ProgressCallback) {
	if fn == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			fn(dirSizeBytes(dest), 0)
			return
		case <-ticker.C:
			fn(dirSizeBytes(dest), 0)
		}
	}
}

func dirSizeBytes(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, statErr := d.Info(); statErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func hasModelWeights(dir string) bool {
	return llm.HasWeights(dir)
}
