package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A semantic match must WIDEN the map, never narrow it. The substring filter is
// precise but literal; semantic recall is conceptual but approximate. Dropping
// either shrinks what the agent is shown before it decides what to read.
func TestRepoMapUnionsSemanticAndSubstringMatches(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Name contains "server" — reachable by substring.
	write("server.go", "package a\n\nfunc StartServer() error { return nil }\n")
	// Nothing in the path or symbol says "server" — reachable only semantically.
	write("listener.go", "package a\n\nfunc BindAndAccept(addr string) error { return nil }\n")
	// Neither — must stay out.
	write("unrelated.go", "package a\n\nfunc FormatCurrency(v float64) string { return \"\" }\n")

	ctx := context.Background()

	// Substring only: finds server.go, misses listener.go.
	plain, err := GenerateRepoMapWithSemanticMatches(ctx, root, depthFullSymbols, "server", nil)
	if err != nil {
		t.Fatalf("plain: %v", err)
	}
	if !strings.Contains(plain, "server.go") {
		t.Errorf("substring match lost server.go:\n%s", plain)
	}
	if strings.Contains(plain, "listener.go") {
		t.Errorf("listener.go has no literal match; substring-only should not surface it:\n%s", plain)
	}

	// With a semantic hit on listener.go, both appear and unrelated.go does not.
	withSem, err := GenerateRepoMapWithSemanticMatches(ctx, root, depthFullSymbols, "server",
		map[string]bool{"listener.go": true})
	if err != nil {
		t.Fatalf("semantic: %v", err)
	}
	if !strings.Contains(withSem, "listener.go") {
		t.Errorf("semantic match did not widen the map to listener.go:\n%s", withSem)
	}
	if !strings.Contains(withSem, "server.go") {
		t.Errorf("semantic match narrowed the map — server.go was dropped:\n%s", withSem)
	}
	if strings.Contains(withSem, "unrelated.go") {
		t.Errorf("unrelated.go matched neither filter but appeared:\n%s", withSem)
	}
}

// A semantically-matched file keeps all its symbols. Filtering them by the same
// literal string would discard exactly what the semantic search just found —
// the caller asked conceptually precisely because they do not know the name.
func TestRepoMapKeepsSymbolsOfSemanticallyMatchedFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "listener.go"),
		[]byte("package a\n\nfunc BindAndAccept(addr string) error { return nil }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := GenerateRepoMapWithSemanticMatches(context.Background(), root, depthFullSymbols,
		"incoming connections", map[string]bool{"listener.go": true})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(out, "BindAndAccept") {
		t.Errorf("symbols of a semantically-matched file were filtered away by the literal query:\n%s", out)
	}
}

// No index, no semantic matches: behaviour must be exactly the old substring
// filter, since the repo map is useful without embeddings enabled.
func TestRepoMapWithoutSemanticMatchesIsUnchanged(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "api_server.go"),
		[]byte("package a\n\nfunc Serve() error { return nil }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	a, err := GenerateRepoMap(ctx, root, depthFullSymbols, "server")
	if err != nil {
		t.Fatalf("GenerateRepoMap: %v", err)
	}
	b, err := GenerateRepoMapWithSemanticMatches(ctx, root, depthFullSymbols, "server", nil)
	if err != nil {
		t.Fatalf("WithSemanticMatches: %v", err)
	}
	if a != b {
		t.Errorf("nil semantic set changed the output:\n--- GenerateRepoMap ---\n%s\n--- WithSemanticMatches ---\n%s", a, b)
	}
}
