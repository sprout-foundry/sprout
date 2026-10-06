package tools

// design_sync_helpers.go — helpers for the design-sync handler: the
// design-file reader, sync-mode normalisation, touched-file resolution
// and arg/path parsing, input loading, and the design-file probe. Split
// out of design_sync_handler.go.
import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// syncDesignFileReader returns the workspace-confined design-file reader the
// pure apply core uses for current bytes. A design/ path is read from disk; a
// path outside design/ (the plan never emits one, but the reader stays
// defensive) is treated as absent, so no non-design bytes can ever reach a
// plan. A missing file reports (nil,false) — a create.
func syncDesignFileReader(root string) design.SyncFileReader {
	return func(rel string) ([]byte, bool) {
		if !designSyncDesignFile(rel) {
			return nil, false
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			return nil, false
		}
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			return nil, false
		}
		return data, true
	}
}

// normaliseSyncMode trims and lowercases the mode argument, defaulting to
// analyze (§5b: "mode (analyze | apply, default analyze)").
func normaliseSyncMode(raw string) string {
	m := strings.ToLower(strings.TrimSpace(raw))
	if m == "" {
		return SyncModeAnalyze
	}
	return m
}

// resolveSyncTouched determines the touched-file set and its provenance. An
// explicit `files` argument (string or list) overrides the default; otherwise
// the turn's ChangeTracker set is read through the sanctioned
// env.ResolveToolFuncs().ListChanges seam — string output, parsed (there is no
// typed ChangeTracker accessor; §5b names this seam explicitly).
func resolveSyncTouched(ctx context.Context, env ToolEnv, args map[string]any) (paths []string, source string, err error) {
	if v, exists := lookupKey(args, "files"); exists && v != nil {
		explicit := syncArgPaths(v)
		if len(explicit) > 0 {
			return explicit, "argument", nil
		}
		// An explicitly-passed but empty `files` is a usage error: the caller
		// asked for a specific set and named none.
		if _, ok := v.(string); ok && strings.TrimSpace(syncArgString(v)) == "" {
			return nil, "argument", fmt.Errorf("`files` was passed but names no paths")
		}
	}

	fn := env.ResolveToolFuncs().ListChanges
	if fn == nil {
		return nil, "changes", nil
	}
	raw, callErr := fn(ctx, map[string]any{})
	if callErr != nil {
		return nil, "changes", fmt.Errorf("listing the turn's changed files: %w", callErr)
	}
	return parseListChangesPaths(raw), "changes", nil
}

// syncArgPaths extracts a path list from a `files` argument value: a
// comma/newline-separated string, or a []any/[]string list. Entries are
// trimmed; empties dropped; duplicates removed; the result is sorted.
func syncArgPaths(v any) []string {
	var raw []string
	switch val := v.(type) {
	case string:
		raw = splitPathList(val)
	case []string:
		raw = append(raw, val...)
	case []any:
		for _, item := range val {
			if s, ok := item.(string); ok {
				raw = append(raw, s)
			}
		}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// syncArgString renders a string arg for emptiness checks.
func syncArgString(v any) string {
	s, _ := v.(string)
	return s
}

// splitPathList splits a comma-, newline-, or semicolon-separated path list,
// trimmed and with empties dropped.
func splitPathList(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '\n' || r == ';'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if t := strings.TrimSpace(f); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// loadSyncInputs reads each touched path's bytes, returning the analysis inputs
// plus the paths that could not be read. A path is skipped when it is empty, a
// directory, missing, or resolves outside the workspace root (Gate-1 is the
// enforcement point; this is the read-side guard so an out-of-workspace path
// can never leak bytes into the report).
func loadSyncInputs(root string, touched []string) (inputs []design.SyncFileInput, skipped []string) {
	inputs = make([]design.SyncFileInput, 0, len(touched))
	for _, p := range touched {
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, filepath.FromSlash(p))
		}
		rel, relErr := filepath.Rel(root, abs)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			skipped = append(skipped, p)
			continue
		}
		info, statErr := os.Stat(abs)
		if statErr != nil || info.IsDir() {
			skipped = append(skipped, p)
			continue
		}
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			skipped = append(skipped, p)
			continue
		}
		inputs = append(inputs, design.SyncFileInput{Path: filepath.ToSlash(rel), Content: data})
	}
	sort.Strings(skipped)
	return inputs, skipped
}

// designSyncDesignFile guards the report's §5e invariant at the handler
// boundary: every design file a delta names is inside design/. It is used by
// the tests and by item 5.4's apply path; analyze mode already guarantees it by
// construction (the pure analysis only ever emits design/-prefixed paths).
func designSyncDesignFile(p string) bool {
	clean := strings.TrimSuffix(path.Clean(filepath.ToSlash(p)), "/")
	return clean == design.DirName || strings.HasPrefix(clean, design.DirName+"/")
}
