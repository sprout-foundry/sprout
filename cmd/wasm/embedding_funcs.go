//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// embedMgr is the singleton EmbeddingManager for the WASM process.
// We init it lazily on first use so the WASM module can start cheaply and
// only pay the index-load cost when the page actually calls into embedding.
//
// On WASM the manager uses the ONNX bridge (__sproutONNX) if available,
// falling back to a clear error if no ONNX bridge is installed.
var (
	embedMgrOnce sync.Once
	embedMgr     *embedding.EmbeddingManager
	embedMgrErr  error
)

func getEmbeddingManager() (*embedding.EmbeddingManager, error) {
	embedMgrOnce.Do(func() {
		cwd, err := os.Getwd()
		if err != nil {
			embedMgrErr = err
			return
		}
		// Use a relaxed default config — auto-build off, since the host page
		// drives indexing explicitly via buildSemanticIndex.
		cfg := &configuration.EmbeddingIndexConfig{
			Enabled:             true,
			AutoIndex:           false,
			SimilarityThreshold: 0.5,
			MaxResults:          10,
		}
		embedMgr = embedding.NewEmbeddingManager(cfg, cwd)
		// Eagerly Init so the JSONL store on MEMFS is created.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := embedMgr.Init(ctx); err != nil {
			embedMgrErr = err
		}
	})
	return embedMgr, embedMgrErr
}

// ─── JS Function Registry ────────────────────────────────────────

// embeddingJSFuncs returns the embedding/memory entries that main.go merges
// into the SproutWasm global. Kept in a separate function so main.go stays
// focused on the small shell-style API and the embedding additions don't
// fragment its readability.
func embeddingJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"buildSemanticIndex":  js.FuncOf(buildSemanticIndexFunc),
		"getSemanticStatus":   js.FuncOf(getSemanticStatusFunc),
		"searchSemantic":      js.FuncOf(searchSemanticFunc),
		"updateSemanticFile":  js.FuncOf(updateSemanticFileFunc),
		"listMemories":        js.FuncOf(listMemoriesFunc),
		"readMemory":          js.FuncOf(readMemoryFunc),
		"saveMemory":          js.FuncOf(saveMemoryFunc),
		"deleteMemory":        js.FuncOf(deleteMemoryFunc),
		"searchMemories":      js.FuncOf(searchMemoriesFunc),
	}
}

// ─── Helpers ─────────────────────────────────────────────────────

// asPromise wraps a Go function that does async work into a JS Promise. The
// browser side gets `await SproutWasm.searchSemantic(...)` semantics for free.
// Errors are surfaced as rejected promises; success results are passed to
// resolve() as native JS values (after running through marshalJS).
func asPromise(do func(ctx context.Context) (interface{}, error)) interface{} {
	return asPromiseWithTimeout(60*time.Second, do)
}

// asPromiseWithTimeout is asPromise with an explicit timeout — use for
// long-running calls (chat completions, agent loops) that the default
// 60s ceiling on asPromise would prematurely cancel. Pass 0 to disable
// the timeout entirely (caller is responsible for cancellation).
func asPromiseWithTimeout(timeout time.Duration, do func(ctx context.Context) (interface{}, error)) interface{} {
	promiseCtor := js.Global().Get("Promise")
	if promiseCtor.IsUndefined() {
		// No Promise constructor available — fall through to a synchronous
		// call. This path only happens in non-browser hosts.
		result, err := do(context.Background())
		if err != nil {
			return map[string]interface{}{"error": err.Error()}
		}
		return marshalJS(result)
	}
	return promiseCtor.New(js.FuncOf(func(_ js.Value, pargs []js.Value) interface{} {
		resolve, reject := pargs[0], pargs[1]
		go func() {
			var ctx context.Context
			var cancel context.CancelFunc
			if timeout > 0 {
				ctx, cancel = context.WithTimeout(context.Background(), timeout)
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			result, err := do(ctx)
			if err != nil {
				reject.Invoke(js.ValueOf(err.Error()))
				return
			}
			resolve.Invoke(marshalJS(result))
		}()
		return nil
	}))
}

// marshalJS converts a Go value into a js.Value the browser can consume.
// We go through JSON because the values we return are already simple (no
// channels, funcs, or pointers) and round-tripping through JSON gives us
// guaranteed structural identity with the browser side.
func marshalJS(v interface{}) js.Value {
	if v == nil {
		return js.Null()
	}
	data, err := json.Marshal(v)
	if err != nil {
		return js.ValueOf(fmt.Sprintf("marshal error: %v", err))
	}
	return js.Global().Get("JSON").Call("parse", string(data))
}

// argString reads a positional string argument from the JS call site, with
// a default for missing/non-string slots. Keeps callsite parsing terse.
func argString(args []js.Value, idx int, def string) string {
	if idx >= len(args) || args[idx].IsUndefined() || args[idx].IsNull() {
		return def
	}
	if args[idx].Type() != js.TypeString {
		return def
	}
	return args[idx].String()
}

func argInt(args []js.Value, idx int, def int) int {
	if idx >= len(args) || args[idx].IsUndefined() || args[idx].IsNull() {
		return def
	}
	if args[idx].Type() != js.TypeNumber {
		return def
	}
	return args[idx].Int()
}

func argFloat32(args []js.Value, idx int, def float32) float32 {
	if idx >= len(args) || args[idx].IsUndefined() || args[idx].IsNull() {
		return def
	}
	if args[idx].Type() != js.TypeNumber {
		return def
	}
	return float32(args[idx].Float())
}

// ─── Semantic Search ─────────────────────────────────────────────

func buildSemanticIndexFunc(_ js.Value, _ []js.Value) interface{} {
	return asPromise(func(ctx context.Context) (interface{}, error) {
		mgr, err := getEmbeddingManager()
		if err != nil {
			return nil, err
		}
		stats, err := mgr.BuildIndex(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"filesProcessed": stats.FilesProcessed,
			"unitsExtracted": stats.UnitsExtracted,
			"unitsEmbedded":  stats.UnitsEmbedded,
			"durationMs":     stats.Duration.Milliseconds(),
		}, nil
	})
}

func getSemanticStatusFunc(_ js.Value, _ []js.Value) interface{} {
	return asPromise(func(_ context.Context) (interface{}, error) {
		mgr, err := getEmbeddingManager()
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"initialized": mgr.IsInitialized(),
			"building":    mgr.IsBuilding(),
			"indexSize":   mgr.IndexSize(),
		}, nil
	})
}

func searchSemanticFunc(_ js.Value, args []js.Value) interface{} {
	query := argString(args, 0, "")
	topK := argInt(args, 1, 5)
	threshold := argFloat32(args, 2, 0.0)
	return asPromise(func(ctx context.Context) (interface{}, error) {
		if query == "" {
			return nil, fmt.Errorf("query is required")
		}
		mgr, err := getEmbeddingManager()
		if err != nil {
			return nil, err
		}
		results, err := mgr.QuerySimilar(ctx, query, topK, threshold)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]interface{}, len(results))
		for i, r := range results {
			out[i] = map[string]interface{}{
				"id":         r.Record.ID,
				"file":       r.Record.File,
				"name":       r.Record.Name,
				"type":       r.Record.Type,
				"signature":  r.Record.Signature,
				"startLine":  r.Record.StartLine,
				"endLine":    r.Record.EndLine,
				"similarity": r.Similarity,
			}
		}
		return out, nil
	})
}

func updateSemanticFileFunc(_ js.Value, args []js.Value) interface{} {
	filePath := argString(args, 0, "")
	return asPromise(func(ctx context.Context) (interface{}, error) {
		if filePath == "" {
			return nil, fmt.Errorf("filePath is required")
		}
		mgr, err := getEmbeddingManager()
		if err != nil {
			return nil, err
		}
		if err := mgr.UpdateFile(ctx, filePath); err != nil {
			return nil, err
		}
		return map[string]interface{}{"ok": true}, nil
	})
}

// ─── Memory CRUD ─────────────────────────────────────────────────

func listMemoriesFunc(_ js.Value, _ []js.Value) interface{} {
	return asPromise(func(_ context.Context) (interface{}, error) {
		mems, err := agent.LoadAllMemories()
		if err != nil {
			return nil, err
		}
		out := make([]map[string]interface{}, len(mems))
		for i, m := range mems {
			out[i] = map[string]interface{}{
				"name":    m.Name,
				"path":    m.Path,
				"content": m.Content,
			}
		}
		return out, nil
	})
}

func readMemoryFunc(_ js.Value, args []js.Value) interface{} {
	name := argString(args, 0, "")
	return asPromise(func(_ context.Context) (interface{}, error) {
		if name == "" {
			return nil, fmt.Errorf("name is required")
		}
		content, err := agent.LoadMemoryContent(name)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"name": name, "content": content}, nil
	})
}

func saveMemoryFunc(_ js.Value, args []js.Value) interface{} {
	name := argString(args, 0, "")
	content := argString(args, 1, "")
	return asPromise(func(ctx context.Context) (interface{}, error) {
		if name == "" {
			return nil, fmt.Errorf("name is required")
		}
		if err := saveMemoryToDisk(name, content); err != nil {
			return nil, err
		}
		// Embed in background — same pattern the native handler uses.
		mgr, _ := getEmbeddingManager()
		if mgr != nil {
			_ = agent.EmbedMemory(ctx, mgr, name, content)
		}
		return map[string]interface{}{"ok": true, "name": name}, nil
	})
}

func deleteMemoryFunc(_ js.Value, args []js.Value) interface{} {
	name := argString(args, 0, "")
	return asPromise(func(_ context.Context) (interface{}, error) {
		if name == "" {
			return nil, fmt.Errorf("name is required")
		}
		if err := deleteMemoryFromDisk(name); err != nil {
			return nil, err
		}
		mgr, _ := getEmbeddingManager()
		if mgr != nil {
			_ = agent.DeleteMemoryEmbedding(mgr, name)
		}
		return map[string]interface{}{"ok": true}, nil
	})
}

func searchMemoriesFunc(_ js.Value, args []js.Value) interface{} {
	query := argString(args, 0, "")
	topK := argInt(args, 1, 5)
	threshold := argFloat32(args, 2, 0.5)
	return asPromise(func(ctx context.Context) (interface{}, error) {
		if query == "" {
			return nil, fmt.Errorf("query is required")
		}
		mgr, err := getEmbeddingManager()
		if err != nil {
			return nil, err
		}
		store, err := mgr.GetConversationStore(ctx)
		if err != nil {
			return nil, err
		}
		results, err := store.QueryMemories(ctx, query, topK, threshold)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]interface{}, len(results))
		for i, r := range results {
			preview, _ := r.Record.Metadata["content_preview"].(string)
			out[i] = map[string]interface{}{
				"name":       r.Record.Name,
				"similarity": r.Similarity,
				"preview":    preview,
			}
		}
		return out, nil
	})
}

// ─── Memory disk helpers ────────────────────────────────────────
// agent.LoadAllMemories reads from ~/.config/sprout/memories. We mirror the
// write side here rather than reaching into agent's unexported helpers; the
// shape is small and stable enough that duplication is cleaner than carving
// out a new agent-package API just for WASM.

func memoryDir() string {
	configDir, err := configuration.GetConfigDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(configDir, "memories")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func saveMemoryToDisk(name, content string) error {
	dir := memoryDir()
	if dir == "" {
		return fmt.Errorf("memory directory unavailable")
	}
	// Sanitize: memory names map 1:1 to filenames, so any directory traversal
	// attempt must be rejected before we touch disk.
	if strings.ContainsAny(name, "/\\") || name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid memory name %q", name)
	}
	path := filepath.Join(dir, name+".md")
	return os.WriteFile(path, []byte(content), 0o644)
}

func deleteMemoryFromDisk(name string) error {
	dir := memoryDir()
	if dir == "" {
		return fmt.Errorf("memory directory unavailable")
	}
	if strings.ContainsAny(name, "/\\") || name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid memory name %q", name)
	}
	path := filepath.Join(dir, name+".md")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
