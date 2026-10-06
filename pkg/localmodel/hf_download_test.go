package localmodel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sinter/llm/catalog"
)

const fakeRepo = "org/fake-model"

type fakeHub struct {
	t         *testing.T
	files     map[string][]byte
	sizes     map[string]int64 // overrides listed size
	noRange   bool
	pageSize  int
	block     map[string]int // path -> bytes to send before stalling
	stalled   chan string
	mu        sync.Mutex
	ranges    map[string][]string
	served    map[string]int64
	authSeen  []string
	listCalls int
}

func newFakeHub(t *testing.T, files map[string][]byte) (*fakeHub, *httptest.Server) {
	h := &fakeHub{
		t: t, files: files, sizes: map[string]int64{}, pageSize: 2,
		block: map[string]int{}, stalled: make(chan string, 4),
		ranges: map[string][]string{}, served: map[string]int64{},
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	oldBase := hfBaseURL
	hfBaseURL = srv.URL
	t.Cleanup(func() { hfBaseURL = oldBase })
	oldBackoff := hfRetryBackoff
	hfRetryBackoff = time.Millisecond
	t.Cleanup(func() { hfRetryBackoff = oldBackoff })
	return h, srv
}

func (h *fakeHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.authSeen = append(h.authSeen, r.Header.Get("Authorization"))
	h.mu.Unlock()
	treePrefix := "/api/models/" + fakeRepo + "/tree/main"
	resolvePrefix := "/" + fakeRepo + "/resolve/main/"
	switch {
	case r.URL.Path == treePrefix:
		h.serveTree(w, r)
	case strings.HasPrefix(r.URL.Path, resolvePrefix):
		http.Redirect(w, r, "/cdn/"+strings.TrimPrefix(r.URL.Path, resolvePrefix), http.StatusFound) //nolint:gosec // G710: fake CDN redirect within the test server
	case strings.HasPrefix(r.URL.Path, "/cdn/"):
		h.serveFile(w, r, strings.TrimPrefix(r.URL.Path, "/cdn/"))
	default:
		http.NotFound(w, r)
	}
}

func (h *fakeHub) serveTree(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.listCalls++
	h.mu.Unlock()
	if r.URL.Query().Get("recursive") != "true" {
		h.t.Errorf("tree listing must be recursive")
	}
	paths := make([]string, 0, len(h.files))
	for p := range h.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	start, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
	end := min(start+h.pageSize, len(paths))
	var entries []map[string]any
	if start == 0 {
		entries = append(entries, map[string]any{"type": "directory", "path": "sub"})
	}
	for _, p := range paths[start:end] {
		size, ok := h.sizes[p]
		if !ok {
			size = int64(len(h.files[p]))
		}
		e := map[string]any{"type": "file", "path": p, "size": 135}
		e["lfs"] = map[string]any{"size": size}
		entries = append(entries, e)
	}
	if end < len(paths) {
		w.Header().Set("Link", fmt.Sprintf(`<%s?recursive=true&cursor=%d>; rel="next"`, r.URL.Path, end))
	}
	_ = json.NewEncoder(w).Encode(entries)
}

func (h *fakeHub) serveFile(w http.ResponseWriter, r *http.Request, p string) {
	data, ok := h.files[p]
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.mu.Lock()
	h.ranges[p] = append(h.ranges[p], r.Header.Get("Range"))
	h.mu.Unlock()
	if n, ok := h.block[p]; ok {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data[:n])
		w.(http.Flusher).Flush()
		h.stalled <- p
		<-r.Context().Done()
		return
	}
	if h.noRange {
		r.Header.Del("Range")
	}
	cw := &countingWriter{ResponseWriter: w}
	http.ServeContent(cw, r, p, time.Time{}, bytes.NewReader(data))
	h.mu.Lock()
	h.served[p] += cw.n
	h.mu.Unlock()
}

type countingWriter struct {
	http.ResponseWriter
	n int64
}

func (c *countingWriter) Write(b []byte) (int, error) {
	n, err := c.ResponseWriter.Write(b)
	c.n += int64(n)
	return n, err
}

func validConfig() []byte {
	return []byte(`{"num_attention_heads": 8, "hidden_size": 512}`)
}

func modelFiles() map[string][]byte {
	return map[string][]byte{
		"config.json":                      validConfig(),
		"tokenizer.json":                   bytes.Repeat([]byte("t"), 3000),
		"model-00001-of-00002.safetensors": bytes.Repeat([]byte("a"), 400_000),
		"model-00002-of-00002.safetensors": bytes.Repeat([]byte("b"), 300_000),
		"model.safetensors.index.json":     []byte(`{"weight_map":{}}`),
		"sub/extra.json":                   []byte(`{}`),
	}
}

func assertTree(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for p, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: content mismatch (got %d bytes, want %d)", p, len(got), len(want))
		}
	}
	_ = filepath.WalkDir(dir, func(p string, _ os.DirEntry, _ error) error {
		if strings.HasSuffix(p, partSuffix) {
			t.Errorf("leftover partial file %s", p)
		}
		return nil
	})
}

func TestEnsureModelDownloadsPaginatedRepo(t *testing.T) {
	files := modelFiles()
	hub, _ := newFakeHub(t, files)
	t.Setenv("HF_TOKEN", "secret-token")
	dest := filepath.Join(t.TempDir(), "fake-model")

	var mu sync.Mutex
	var calls [][2]int64
	got, err := EnsureModel(context.Background(), ModelStatus{Dir: dest, HFRepo: fakeRepo}, func(d, total int64) {
		mu.Lock()
		calls = append(calls, [2]int64{d, total})
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("EnsureModel: %v", err)
	}
	if got != dest {
		t.Errorf("dest = %q, want %q", got, dest)
	}
	assertTree(t, dest, files)
	if hub.listCalls < 3 {
		t.Errorf("expected Link pagination to fetch 3 pages, got %d", hub.listCalls)
	}
	if !hasModelWeights(dest) || !validModelConfig(dest) {
		t.Error("completed download must be detected as installed")
	}
	for _, a := range hub.authSeen {
		if a != "Bearer secret-token" {
			t.Errorf("Authorization = %q, want bearer token", a)
		}
	}

	var want int64
	for _, b := range files {
		want += int64(len(b))
	}
	if len(calls) == 0 {
		t.Fatal("no progress reported")
	}
	last := calls[len(calls)-1]
	if last[0] != want || last[1] != want {
		t.Errorf("final progress = %v, want (%d, %d)", last, want, want)
	}
	for i, c := range calls {
		if c[1] != want {
			t.Errorf("call %d total = %d, want %d", i, c[1], want)
		}
		if i > 0 && c[0] < calls[i-1][0] {
			t.Errorf("progress went backwards: %v", calls)
		}
	}
	if len(calls) > 10 {
		t.Errorf("progress should be rate-limited, got %d callbacks", len(calls))
	}
}

func TestEnsureModelIncludeLandsUnderDest(t *testing.T) {
	files := map[string][]byte{
		"5bit/config.json":       validConfig(),
		"5bit/model.safetensors": bytes.Repeat([]byte("w"), 1000),
		"4bit/model.safetensors": bytes.Repeat([]byte("x"), 1000),
		"README.md":              []byte("readme"),
	}
	newFakeHub(t, files)
	root := t.TempDir()
	dest := filepath.Join(root, "5bit")

	if _, err := EnsureModel(context.Background(), ModelStatus{Dir: dest, HFRepo: fakeRepo, HFInclude: "5bit/*"}, nil); err != nil {
		t.Fatalf("EnsureModel: %v", err)
	}
	if !hasModelWeights(dest) || !validModelConfig(dest) {
		t.Fatal("included subdir must land under dest")
	}
	for _, p := range []string{"4bit", "README.md"} {
		if _, err := os.Stat(filepath.Join(root, p)); !os.IsNotExist(err) {
			t.Errorf("%s should have been filtered out", p)
		}
	}
}

func TestFilterHFFilesMatchesFnmatchSemantics(t *testing.T) {
	files := []hfFile{
		{Path: "config.json"}, {Path: "sub/a.json"}, {Path: "5bit/model.safetensors"},
		{Path: "5bit/deep/x.bin"}, {Path: "model-1.safetensors"}, {Path: "../escape.json"},
	}
	cases := map[string][]string{
		"":                    {"config.json", "sub/a.json", "5bit/model.safetensors", "5bit/deep/x.bin", "model-1.safetensors"},
		"*.json":              {"config.json", "sub/a.json"},
		"5bit/*":              {"5bit/model.safetensors", "5bit/deep/x.bin"},
		"5bit/":               {"5bit/model.safetensors", "5bit/deep/x.bin"},
		"model-?.safetensors": {"model-1.safetensors"},
		"[!c]*.json":          {"sub/a.json"},
	}
	for pattern, want := range cases {
		got, err := filterHFFiles(files, pattern)
		if err != nil {
			t.Fatalf("%q: %v", pattern, err)
		}
		var paths []string
		for _, f := range got {
			paths = append(paths, f.Path)
		}
		if strings.Join(paths, ",") != strings.Join(want, ",") {
			t.Errorf("include %q = %v, want %v", pattern, paths, want)
		}
	}
}

func TestEnsureModelNoMatchingFiles(t *testing.T) {
	newFakeHub(t, modelFiles())
	_, err := EnsureModel(context.Background(), ModelStatus{Dir: filepath.Join(t.TempDir(), "m"), HFRepo: fakeRepo, HFInclude: "nothing/*"}, nil)
	if err == nil || !strings.Contains(err.Error(), "no files") {
		t.Fatalf("expected no-match error, got %v", err)
	}
}

func TestEnsureModelResumesPartialWithRange(t *testing.T) {
	files := modelFiles()
	hub, _ := newFakeHub(t, files)
	dest := filepath.Join(t.TempDir(), "m")
	shard := "model-00001-of-00002.safetensors"
	if err := writeFile(filepath.Join(dest, shard+partSuffix), files[shard][:100_000]); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureModel(context.Background(), ModelStatus{Dir: dest, HFRepo: fakeRepo}, nil); err != nil {
		t.Fatalf("EnsureModel: %v", err)
	}
	assertTree(t, dest, files)
	if r := hub.ranges[shard]; len(r) != 1 || r[0] != "bytes=100000-" {
		t.Errorf("Range headers = %v, want [bytes=100000-]", r)
	}
	if got := hub.served[shard]; got != 300_000 {
		t.Errorf("served %d bytes for resumed shard, want 300000", got)
	}
}

func TestEnsureModelRestartsWhenRangeUnsupported(t *testing.T) {
	files := modelFiles()
	hub, _ := newFakeHub(t, files)
	hub.noRange = true
	dest := filepath.Join(t.TempDir(), "m")
	shard := "model-00001-of-00002.safetensors"
	if err := writeFile(filepath.Join(dest, shard+partSuffix), bytes.Repeat([]byte("z"), 1000)); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureModel(context.Background(), ModelStatus{Dir: dest, HFRepo: fakeRepo}, nil); err != nil {
		t.Fatalf("EnsureModel: %v", err)
	}
	assertTree(t, dest, files)
}

func TestEnsureModelRestartsOnSizeMismatch(t *testing.T) {
	files := modelFiles()
	hub, _ := newFakeHub(t, files)
	dest := filepath.Join(t.TempDir(), "m")
	shardA := "model-00001-of-00002.safetensors"
	shardB := "model-00002-of-00002.safetensors"
	if err := writeFile(filepath.Join(dest, shardA+partSuffix), bytes.Repeat([]byte("z"), 500_000)); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(dest, shardB), []byte("truncated")); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(dest, "tokenizer.json"), files["tokenizer.json"]); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureModel(context.Background(), ModelStatus{Dir: dest, HFRepo: fakeRepo}, nil); err != nil {
		t.Fatalf("EnsureModel: %v", err)
	}
	assertTree(t, dest, files)
	if r := hub.ranges[shardA]; len(r) != 1 || r[0] != "" {
		t.Errorf("oversized partial must restart without Range, got %v", r)
	}
	if _, ok := hub.ranges["tokenizer.json"]; ok {
		t.Error("complete file with the right size must be skipped")
	}
}

func TestEnsureModelShortBodyFailsWithoutFinalFile(t *testing.T) {
	files := modelFiles()
	hub, _ := newFakeHub(t, files)
	shard := "model-00002-of-00002.safetensors"
	hub.sizes[shard] = 999_999
	dest := filepath.Join(t.TempDir(), "m")

	_, err := EnsureModel(context.Background(), ModelStatus{Dir: dest, HFRepo: fakeRepo}, nil)
	if err == nil || !strings.Contains(err.Error(), "size mismatch") {
		t.Fatalf("expected size mismatch error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, shard)); !os.IsNotExist(err) {
		t.Error("mismatched file must not be finalized")
	}
	if hasModelWeights(dest) {
		t.Error("failed download must not look installed")
	}
}

func TestEnsureModelCancelLeavesOnlyPartsThenResumes(t *testing.T) {
	files := modelFiles()
	hub, _ := newFakeHub(t, files)
	shard := "model-00002-of-00002.safetensors"
	hub.block[shard] = 120_000
	dest := filepath.Join(t.TempDir(), "m")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		_, err := EnsureModel(ctx, ModelStatus{Dir: dest, HFRepo: fakeRepo}, nil)
		errc <- err
	}()

	select {
	case <-hub.stalled:
	case <-time.After(10 * time.Second):
		t.Fatal("download never reached the stalled shard")
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if sz, _ := fileSize(filepath.Join(dest, shard+partSuffix)); sz == 120_000 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("partial shard never reached the stalled offset")
		}
	}
	if hasModelWeights(dest) {
		t.Error("model must not look installed while a shard is incomplete")
	}
	cancel()
	var err error
	select {
	case err = <-errc:
	case <-time.After(10 * time.Second):
		t.Fatal("EnsureModel did not return after cancel")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	for p := range files {
		if _, err := os.Stat(filepath.Join(dest, filepath.FromSlash(p))); !os.IsNotExist(err) {
			t.Errorf("final file %s exists after cancel", p)
		}
	}
	if hasModelWeights(dest) || len(catalog.ListInstalledModels(filepath.Dir(dest))) != 0 {
		t.Error("cancelled download must not be detected as installed")
	}
	if sz, _ := fileSize(filepath.Join(dest, shard+partSuffix)); sz != 120_000 {
		t.Errorf("partial size = %d, want 120000", sz)
	}

	delete(hub.block, shard)
	if _, err := EnsureModel(context.Background(), ModelStatus{Dir: dest, HFRepo: fakeRepo}, nil); err != nil {
		t.Fatalf("resume: %v", err)
	}
	assertTree(t, dest, files)
	if r := hub.ranges[shard]; r[len(r)-1] != "bytes=120000-" {
		t.Errorf("retry must resume with Range, got %v", r)
	}
}

func TestEnsureModelGatedRepoMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	old := hfBaseURL
	hfBaseURL = srv.URL
	defer func() { hfBaseURL = old }()

	_, err := EnsureModel(context.Background(), ModelStatus{Dir: filepath.Join(t.TempDir(), "m"), HFRepo: fakeRepo}, nil)
	if err == nil || !strings.Contains(err.Error(), "HF_TOKEN") {
		t.Fatalf("expected HF_TOKEN hint, got %v", err)
	}
}
