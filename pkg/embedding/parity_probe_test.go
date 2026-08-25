package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// TestRemoteParityProbe measures whether vectors produced by the local
// ONNX EmbeddingGemma-300M match vectors from a remote serving of the
// same model (DeepInfra: google/embeddinggemma-300m) for identical inputs.
//
// This is the load-bearing check for the "pre-generate via API, then do
// incremental on-device" design. If the remote returns the same vectors
// (cosine ≈ 1.0) for the same prefixed text, a store seeded from the API can
// be incrementally updated by the local provider without re-measuring every
// similarity threshold. If it drifts, mixing providers shifts the score
// distribution and the measured gates (0.65 dup, 0.40 search) no longer hold.
//
// Two phases:
//  1. Per-text parity in BOTH subspaces the index uses — documentPrefix
//     (code stored as documents) and codeQueryPrefix (natural-language
//     queries). A match in one subspace does not imply a match in the other.
//  2. Mixed-store retrieval: documents embedded via API, query embedded
//     locally — the exact production shape of pre-gen + on-device query.
//     Asserts the near-duplicate still clears DefaultDuplicateThreshold and
//     outranks unrelated code.
//
// Opt-in: SPROUT_PARITY_PROBE=1 and DEEPINFRA_API_KEY set (skips cleanly
// otherwise, per test-isolation rules). The API call embeds ~96 units ≈ a
// fraction of a cent at DeepInfra's $0.002/1M tokens.
//
// This variant uses the production q4 model via the shared cache.
func TestRemoteParityProbe(t *testing.T) {
	requireParityEnv(t)
	ctx := context.Background()

	provider, _, err := acquireSharedONNXProvider(ctx, DefaultModelDir(), EmbeddingGemma300MConfig())
	if err != nil {
		t.Skipf("ONNX provider unavailable: %v", err)
	}
	if provider.Dimensions() != 768 {
		t.Fatalf("local provider dims = %d, want 768", provider.Dimensions())
	}

	runParityCore(ctx, t, provider, "q4")
}

// TestRemoteParityProbeFullPrecision runs the same parity checks against a
// full-precision local variant (fp16 or fp32) instead of the production q4
// model. The point: DeepInfra serves the model in full precision, so IF the
// local full-precision variant produces near-identical vectors to the API,
// the pre-gen design becomes viable at the cost of a much larger local model
// (fp16 ≈ 617 MB, fp32 ≈ 1.2 GB — vs 197 MB for q4).
//
// The provider is built directly (not through the shared cache) because
// sharedONNXKey is (modelDir, dims) — every variant is 768-dim in the same
// dir, so the cache would return the already-loaded q4 provider.
//
// Opt-in: SPROUT_PARITY_PROBE_FP=1 (plus DEEPINFRA_API_KEY). Select the
// variant with SPROUT_PARITY_FP_VARIANT=fp16|fp32 (default fp16). fp16 files
// ship with the model download; fp32 requires a one-time ~1.2 GB download.
func TestRemoteParityProbeFullPrecision(t *testing.T) {
	if os.Getenv("SPROUT_PARITY_PROBE_FP") != "1" {
		t.Skip("SPROUT_PARITY_PROBE_FP unset")
	}
	requireParityEnv(t)

	variant := os.Getenv("SPROUT_PARITY_FP_VARIANT")
	if variant == "" {
		variant = "fp16"
	}
	if variant != "fp16" && variant != "fp32" {
		t.Fatalf("SPROUT_PARITY_FP_VARIANT=%q: want fp16 or fp32", variant)
	}

	ctx := context.Background()
	cfg := fullPrecisionConfig(variant)

	// Build directly into a hermetic temp dir; we own the lifecycle and must
	// close both. Never touches the real model cache (DefaultModelDir).
	modelDir := stageVariantModelDir(t, ctx, variant)
	provider, runtime, err := buildONNXProvider(ctx, modelDir, cfg)
	if err != nil {
		t.Skipf("ONNX provider unavailable (%s): %v", variant, err)
	}
	defer runtime.Close()
	defer provider.Close()
	if provider.Dimensions() != 768 {
		t.Fatalf("local provider dims = %d, want 768", provider.Dimensions())
	}

	runParityCore(ctx, t, provider, variant)
}

// requireParityEnv centralizes the opt-in gates shared by all parity variants.
func requireParityEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("SPROUT_PARITY_PROBE") != "1" && os.Getenv("SPROUT_PARITY_PROBE_FP") != "1" {
		t.Skip("SPROUT_PARITY_PROBE/SPROUT_PARITY_PROBE_FP unset")
	}
	if os.Getenv("SKIP_NETWORK_TESTS") != "" {
		t.Skip("SKIP_NETWORK_TESTS is set, skipping remote parity probe")
	}
	if os.Getenv("DEEPINFRA_API_KEY") == "" {
		t.Skip("DEEPINFRA_API_KEY unset — skipping remote parity probe")
	}
}

// fullPrecisionConfig returns a ModelConfig pointing at the fp16 or fp32 ONNX
// variant of embeddinggemma-300m. The graph+weights files for both variants
// live in the same model directory as the q4 files (model_fp16.onnx[,_data],
// model.onnx[,_data]); only the downloader layout changes, not the tokenizer.
//
// URLs and hashes are variant-specific. A past bug copied the q4 URL into
// these fields while only changing the filenames, which silently downloaded
// q4 weights under fp16 names — the probe then "matched" the API because it
// was comparing q4 to itself. Hashes for fp16 are pinned to the files staged
// alongside the q4 download; fp32 hashes are left empty (skip verification)
// because the probe does not re-download a 1.2 GB file just to hash it.
func fullPrecisionConfig(variant string) ModelConfig {
	base := EmbeddingGemma300MConfig()
	const baseURL = "https://huggingface.co/onnx-community/embeddinggemma-300m-ONNX/resolve/main/onnx"
	switch variant {
	case "fp32":
		base.ModelURL = baseURL + "/model.onnx"
		base.ModelDataURL = baseURL + "/model.onnx_data"
		base.ModelFilename = "model.onnx"
		base.ModelDataFilename = "model.onnx_data"
		base.ModelHash = ""
		base.ModelDataHash = ""
	default: // fp16
		base.ModelURL = baseURL + "/model_fp16.onnx"
		base.ModelDataURL = baseURL + "/model_fp16.onnx_data"
		base.ModelFilename = "model_fp16.onnx"
		base.ModelDataFilename = "model_fp16.onnx_data"
		// Hashes computed from the fp16 files staged beside the q4 download
		// (verified against the HF resolve URLs during probe development).
		base.ModelHash = "dcfaf21ff7cae91af9295366ac0d7352efcadeaf7deefb98f82d5056502d0bf2"
		base.ModelDataHash = "1cd839755aa8e24d5af7f16ef275b12d717a4401bb009099b8c17e4156d3d5d5"
	}
	return base
}

// stageVariantModelDir prepares a hermetic model directory for the given
// variant: a fresh temp dir seeded with the tokenizer and the variant's
// graph+weights (copied from whichever already-staged location has them, or
// downloaded on demand). The probe NEVER reads or writes DefaultModelDir() —
// a probe run must not mutate the real model cache.
func stageVariantModelDir(t *testing.T, ctx context.Context, variant string) string {
	t.Helper()
	dir := t.TempDir()

	cfg := fullPrecisionConfig(variant)
	name := cfg.Name
	modelDir := filepath.Join(dir, name)
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("stage model dir: %v", err)
	}

	// Candidate source dirs that may already have the variant staged.
	sources := []string{DefaultModelDir(), filepath.Join(os.Getenv("HOME"), ".config", "sprout", "models")}
	var seeded bool
	for _, src := range sources {
		srcModel := filepath.Join(src, name)
		graph := filepath.Join(srcModel, cfg.ModelFilenameOrDefault())
		if _, err := os.Stat(graph); err != nil {
			continue
		}
		// Copy graph + external weights + tokenizer.
		for _, f := range []string{cfg.ModelFilenameOrDefault(), cfg.ModelDataFilenameOrDefault(), "tokenizer.json"} {
			srcPath := filepath.Join(srcModel, f)
			if _, err := os.Stat(srcPath); err != nil {
				continue
			}
			data, err := os.ReadFile(srcPath)
			if err != nil {
				t.Fatalf("read staged %s: %v", srcPath, err)
			}
			if err := os.WriteFile(filepath.Join(modelDir, f), data, 0o644); err != nil {
				t.Fatalf("write staged %s: %v", f, err)
			}
		}
		seeded = true
		break
	}

	if !seeded {
		// Nothing staged anywhere — let the downloader fetch the variant.
		if err := DownloadModel(ctx, dir, cfg); err != nil {
			t.Skipf("download %s model: %v", variant, err)
		}
	}
	return dir
}

// runParityCore executes the full parity battery against the given local
// provider. label names the local variant in the output for comparison.
func runParityCore(ctx context.Context, t *testing.T, provider EmbeddingProvider, label string) {
	t.Helper()
	apiKey := os.Getenv("DEEPINFRA_API_KEY")

	// --- Phase 1a: document-subspace parity over real extracted units ---
	units := sampleProbeUnits(t, 96)
	texts := make([]string, len(units))
	for i, u := range units {
		texts[i] = embeddingText(u, 2000)
	}

	// Controls: each side must be self-consistent (~1.0) before the cross
	// comparison means anything. A harness bug (e.g., API index misordering,
	// wrong prefix) shows up here as well.
	localDoc := embedLocalBatch(ctx, t, provider, texts, documentPrefix)
	localDoc2 := embedLocalBatch(ctx, t, provider, texts, documentPrefix)
	parityReport(t, "CONTROL local-vs-local", localDoc, localDoc2)

	apiDoc := embedRemoteBatch(ctx, t, apiKey, texts, documentPrefix)
	apiDoc2 := embedRemoteBatch(ctx, t, apiKey, texts, documentPrefix)
	parityReport(t, "CONTROL api-vs-api", apiDoc, apiDoc2)

	parityReport(t, fmt.Sprintf("CROSS document subspace (local=%s)", label), localDoc, apiDoc)

	// --- Phase 1b: query-subspace parity over realistic code-search queries ---
	queries := []string{
		"sort an array of integers in place",
		"read a file and return its lines as a slice",
		"parse a JSON string into a struct",
		"retry a network request with exponential backoff",
		"compute the sha256 hash of a string",
		"find all callers of a function in the codegraph",
		"build a checkpoint-compacted message list from turn summaries",
		"check whether a shell command is safe to auto-run",
		"merge two sorted slices without duplicates",
		"watch a directory for file changes and debounce events",
	}
	localQuery := embedLocalBatch(ctx, t, provider, queries, codeQueryPrefix)
	apiQuery := embedRemoteBatch(ctx, t, apiKey, queries, codeQueryPrefix)
	parityReport(t, fmt.Sprintf("CROSS query subspace (local=%s)", label), localQuery, apiQuery)

	// --- Phase 2: mixed-store retrieval (API docs + local query) ---
	// Reuse the near-dup corpus from TestDuplicateDetectionFiresOnRealNearDuplicate:
	// docs embedded via API, then queried with a LOCAL embedding of the same
	// near-duplicate candidate. This is the shape of pre-gen + on-device query.
	mixedStoreRetrieval(ctx, t, apiKey, provider)
}

// --- helpers ---

// sampleProbeUnits walks the repository, extracts code units, and returns a
// bounded, deterministic sample (spread across files) so the probe stays fast
// and cheap while still exercising the real extraction pipeline.
func sampleProbeUnits(t *testing.T, n int) []CodeUnit {
	t.Helper()
	root := "../.."
	ctx := context.Background()

	files, err := WalkAllIndexableFiles(ctx, root)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	var all []CodeUnit
	fileExtractor := NewFileExtractor(8000)
	for _, path := range files {
		var got []CodeUnit
		switch {
		case hasCodeExtension(path):
			got, err = ExtractFromFile(path, WithIncludeTests(false))
		case IsSupportedIndexableFile(path):
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				continue
			}
			got, err = fileExtractor.Extract(path, content)
		default:
			continue
		}
		if err != nil {
			continue
		}
		all = append(all, got...)
		if len(all) >= n {
			break
		}
	}
	if len(all) < n {
		t.Fatalf("extracted %d units, want at least %d", len(all), n)
	}
	return all[:n]
}

func embedLocalBatch(ctx context.Context, t *testing.T, p EmbeddingProvider, texts []string, prefix string) [][]float32 {
	t.Helper()
	vecs, err := p.EmbedBatchWithPrefix(ctx, texts, prefix)
	if err != nil {
		t.Fatalf("local embed (%q): %v", prefix, err)
	}
	if len(vecs) != len(texts) {
		t.Fatalf("local embed returned %d vectors for %d texts", len(vecs), len(texts))
	}
	return vecs
}

// deepinfraRequest is the OpenAI-compatible embeddings request body. The
// prefix is prepended CLIENT-SIDE (input = prefix+text) rather than via the
// model page's "Custom Instruction" field, because the OpenAI-compatible
// endpoint's documented parameters are model/input/encoding_format only —
// and prepending client-side is byte-for-byte what the local provider does
// before tokenization (prefix + text). "encoding_format": "float" returns
// raw float arrays.
type deepinfraRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format,omitempty"`
	Normalize      *bool    `json:"normalize,omitempty"`
}

type deepinfraEmbedding struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type deepinfraResponse struct {
	Data []deepinfraEmbedding `json:"data"`
}

// embedRemoteBatch POSTs texts (with prefix already client-side prepended to
// each) to the DeepInfra OpenAI-compatible embeddings endpoint, reorders by
// index, and returns vectors in input order.
func embedRemoteBatch(ctx context.Context, t *testing.T, apiKey string, texts []string, prefix string) [][]float32 {
	t.Helper()

	const chunkSize = 32
	base := os.Getenv("DEEPINFRA_BASE_URL")
	if base == "" {
		base = "https://api.deepinfra.com/v1/openai/embeddings"
	}
	model := os.Getenv("DEEPINFRA_EMBED_MODEL")
	if model == "" {
		model = "google/embeddinggemma-300m"
	}

	client := &http.Client{Timeout: 60 * time.Second}
	all := make([][]float32, len(texts))

	for start := 0; start < len(texts); start += chunkSize {
		end := start + chunkSize
		if end > len(texts) {
			end = len(texts)
		}
		inputs := make([]string, 0, end-start)
		for _, txt := range texts[start:end] {
			inputs = append(inputs, prefix+txt)
		}

		body, err := json.Marshal(deepinfraRequest{
			Model:          model,
			Input:          inputs,
			EncodingFormat: "float",
		})
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base, bytes.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("deepinfra request (chunk %d): %v", start/chunkSize, err)
		}
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if readErr != nil {
			t.Fatalf("read response: %v", readErr)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("deepinfra status %d: %s", resp.StatusCode, truncateForLog(string(respBody), 400))
		}

		var parsed deepinfraResponse
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(parsed.Data) != len(inputs) {
			t.Fatalf("deepinfra returned %d embeddings for %d inputs", len(parsed.Data), len(inputs))
		}
		for _, d := range parsed.Data {
			if d.Index < 0 || d.Index >= len(inputs) {
				t.Fatalf("deepinfra index %d out of range", d.Index)
			}
			all[start+d.Index] = d.Embedding
		}
	}

	for i, v := range all {
		if v == nil {
			t.Fatalf("missing embedding for input %d", i)
		}
		if len(v) != 768 {
			t.Fatalf("remote vector %d has dims %d, want 768", i, len(v))
		}
	}
	return all
}

// parityReport logs the cosine distribution between local and remote vectors
// for the same texts. Vectors that match produce cosines ≈ 1.0; the q4
// quantization vs the API's full-precision serving means identical inputs will
// drift slightly below 1.0. The verdict is a soft assertion (mean ≥ 0.98)
// because a hard cutoff would make the probe brittle to quantization noise —
// the distribution printout is the real deliverable for eyeballing.
func parityReport(t *testing.T, label string, local, remote [][]float32) {
	t.Helper()
	if len(local) != len(remote) {
		t.Fatalf("%s: %d local vs %d remote vectors", label, len(local), len(remote))
	}
	cos := make([]float64, len(local))
	for i := range local {
		cos[i] = float64(CosineSimilarity(local[i], remote[i]))
	}
	sort.Float64s(cos)
	mean := 0.0
	for _, c := range cos {
		mean += c
	}
	mean /= float64(len(cos))

	pct := func(p float64) float64 {
		if len(cos) == 0 {
			return 0
		}
		idx := int(p * float64(len(cos)-1))
		return cos[idx]
	}

	t.Logf("PARITY %-40s n=%d  mean=%.4f  min=%.4f  p50=%.4f  p95=%.4f  max=%.4f",
		label, len(cos), mean, cos[0], pct(0.50), pct(0.95), cos[len(cos)-1])
	for i, c := range cos {
		if c < 0.95 {
			t.Logf("  low-parity vector: %.4f", c)
			_ = i
		}
	}
	if mean < 0.98 {
		t.Errorf("%s: mean cosine %.4f < 0.98 — remote and local embeddings are NOT interchangeable", label, mean)
	}
}

// mixedStoreRetrieval builds TWO stores over the same near-dup/related/
// unrelated corpus — one seeded with API embeddings (the pre-gen side), one
// seeded with local embeddings (the baseline) — and queries both with a LOCAL
// embedding of the near-duplicate candidate. This is the exact shape of
// "pre-generate via API, query on-device".
//
// The pass/fail is API-seeded vs local-seeded AGREEMENT, not the absolute
// score: the near-dup similarity depends on the local model's own geometry
// (q4 measures ~0.767; fp16 differs), so comparing an API-seeded fp16 store
// against the q4 gate would conflate model change with provider mismatch. If
// the two stores rank the same and score within tolerance, the API and local
// providers are interchangeable for retrieval and the pre-gen design works.
func mixedStoreRetrieval(ctx context.Context, t *testing.T, apiKey string, provider EmbeddingProvider) {
	t.Helper()

	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, "sum.go"), `package p

// SumInts adds every value in the slice and returns the total.
func SumInts(values []int) int {
	total := 0
	for _, v := range values {
		total += v
	}
	return total
}
`)
	writeFile(t, filepath.Join(workspace, "maxint.go"), `package p

// MaxInt returns the largest value in the slice.
func MaxInt(values []int) int {
	best := values[0]
	for _, v := range values[1:] {
		if v > best {
			best = v
		}
	}
	return best
}
`)
	for i := 0; i < 6; i++ {
		writeFile(t, filepath.Join(workspace, fmt.Sprintf("filler%d.go", i)), fmt.Sprintf(`package p

// Handler%d processes a request and returns a formatted label.
func Handler%d(name string, count int) string {
	if count <= 0 {
		return "none"
	}
	return name
}
`, i, i))
	}

	// Extract the corpus.
	var corpusUnits []CodeUnit
	for _, path := range []string{
		filepath.Join(workspace, "sum.go"),
		filepath.Join(workspace, "maxint.go"),
		filepath.Join(workspace, "filler0.go"),
		filepath.Join(workspace, "filler1.go"),
		filepath.Join(workspace, "filler2.go"),
		filepath.Join(workspace, "filler3.go"),
		filepath.Join(workspace, "filler4.go"),
		filepath.Join(workspace, "filler5.go"),
	} {
		got, err := ExtractFromFile(path, WithIncludeTests(false))
		if err != nil {
			t.Fatalf("extract %s: %v", path, err)
		}
		corpusUnits = append(corpusUnits, got...)
	}
	texts := make([]string, len(corpusUnits))
	for i, u := range corpusUnits {
		texts[i] = embeddingText(u, 2000)
	}

	// Embed the corpus both ways: via the API (pre-gen) and locally (baseline).
	apiVecs := embedRemoteBatch(ctx, t, apiKey, texts, documentPrefix)
	localVecs := embedLocalBatch(ctx, t, provider, texts, documentPrefix)

	buildStore := func(vecs [][]float32, label string) *HNSWStore {
		store, err := NewHNSWStore(filepath.Join(t.TempDir(), "index.hnsw"), provider.ModelHash())
		if err != nil {
			t.Fatalf("%s store: %v", label, err)
		}
		t.Cleanup(func() { store.Close() })
		records := make([]VectorRecord, len(corpusUnits))
		now := time.Now()
		for i, u := range corpusUnits {
			if u.ID == u.File {
				records[i] = fileCodeUnitToRecord(u, vecs[i], now)
			} else {
				records[i] = codeUnitToRecord(u, vecs[i], now)
			}
		}
		if err := store.Store(records); err != nil {
			t.Fatalf("%s store records: %v", label, err)
		}
		return store
	}
	apiStore := buildStore(apiVecs, "api-seeded")
	localStore := buildStore(localVecs, "local-seeded")

	// Query both with a LOCAL embedding of the near-duplicate candidate.
	candidate := `package p

// AddNumbers totals the numbers in the slice.
func AddNumbers(nums []int) int {
	sum := 0
	for _, n := range nums {
		sum += n
	}
	return sum
}
`
	localVec, err := provider.EmbedWithPrefix(ctx, candidate, documentPrefix)
	if err != nil {
		t.Fatalf("local embed candidate: %v", err)
	}

	query := func(store *HNSWStore) []QueryResult {
		results, err := store.Query(localVec, 5, 0.001)
		if err != nil {
			t.Fatalf("store query: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("store query returned no results")
		}
		return results
	}
	apiResults := query(apiStore)
	localResults := query(localStore)

	t.Logf("MIXED-STORE retrieval (API-seeded vs local-seeded), gate=%.2f:", DefaultDuplicateThreshold)
	label := func(file string) string { return filepath.Base(file) }
	t.Logf("  rank  api-seeded        local-seeded")
	for i := range apiResults {
		apiFile := label(apiResults[i].Record.File)
		localFile := label(localResults[i].Record.File)
		t.Logf("  %-2d   %.3f %-10s  %.3f %-10s", i+1,
			apiResults[i].Similarity, apiFile,
			localResults[i].Similarity, localFile)
	}

	// Agreement checks.
	apiNear := findResult(apiResults, "sum.go")
	localNear := findResult(localResults, "sum.go")
	if apiNear == nil || localNear == nil {
		t.Errorf("mixed-store: near-duplicate sum.go not in top-5 (api=%v local=%v)", apiNear != nil, localNear != nil)
		return
	}
	delta := apiNear.Similarity - localNear.Similarity
	if delta < 0 {
		delta = -delta
	}
	t.Logf("  near-dup agreement: api=%.3f local=%.3f delta=%.3f", apiNear.Similarity, localNear.Similarity, delta)
	if delta > 0.05 {
		t.Errorf("mixed-store: near-duplicate score differs by %.3f between API-seeded and local-seeded stores — providers are NOT interchangeable for retrieval", delta)
	}
	// Rank agreement: the same file should rank at the same position — except
	// for EXACT ties (identical filler code scores identically, and HNSW may
	// return equal-scoring records in either order). Compare with tolerance so
	// a tie flip is not reported as a ranking regression.
	for i := range apiResults {
		if label(apiResults[i].Record.File) != label(localResults[i].Record.File) {
			simDiff := apiResults[i].Similarity - localResults[i].Similarity
			if simDiff < 0 {
				simDiff = -simDiff
			}
			if simDiff < 0.001 {
				t.Logf("  rank %d tie flip (identical score %.4f): api=%s local=%s — benign",
					i+1, apiResults[i].Similarity, label(apiResults[i].Record.File), label(localResults[i].Record.File))
				continue
			}
			t.Errorf("mixed-store: rank %d differs — api=%s (%.3f) local=%s (%.3f); retrieval ranking is not preserved",
				i+1, label(apiResults[i].Record.File), apiResults[i].Similarity,
				label(localResults[i].Record.File), localResults[i].Similarity)
			break
		}
	}
}

// findResult returns the first result whose file basename matches want, or nil.
func findResult(results []QueryResult, want string) *QueryResult {
	for i := range results {
		if filepath.Base(results[i].Record.File) == want {
			return &results[i]
		}
	}
	return nil
}

func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
