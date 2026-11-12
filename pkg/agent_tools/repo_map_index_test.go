//go:build !js

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/codegraph"
)

// indexedRepo makes a git checkout holding one Go file, last changed an hour
// ago, and indexes it with the given symbols (not the file's own), so a test
// can tell symbols served from the index from symbols parsed from disk.
func indexedRepo(t *testing.T, indexed ...codegraph.Symbol) (root, file string) {
	t.Helper()
	root = t.TempDir()
	initGitRepo(t, root)
	file = filepath.Join(root, "svc", "queue.go")
	createTestFiles(t, root, map[string]string{"svc/queue.go": "package svc\n\nfunc OnDisk() {}\n"})
	hourAgo := time.Now().Add(-time.Hour)
	requireErr(t, os.Chtimes(file, hourAgo, hourAgo), "age the file")

	store, err := codegraph.CreateForDir(root)
	requireErr(t, err, "create the index")
	defer store.Close()
	for i := range indexed {
		indexed[i].FilePath = "svc/queue.go"
	}
	requireErr(t, store.IndexFile(context.Background(), "svc/queue.go", indexed, nil), "index the file")
	return root, file
}

func TestGenerateRepoMapServesSymbolsFromTheIndex(t *testing.T) {
	root, file := indexedRepo(t, codegraph.Symbol{QualifiedName: "svc.FromIndex", DisplayName: "FromIndex", Line: 7, Kind: "func", Language: "go"})

	out, err := GenerateRepoMap(context.Background(), root, 3, "")
	requireErr(t, err, "repo map")
	if !strings.Contains(out, "func FromIndex:7") || strings.Contains(out, "OnDisk") {
		t.Fatalf("symbols did not come from the index:\n%s", out)
	}

	// Once the file changes, its symbols are parsed from disk again.
	requireErr(t, os.Chtimes(file, time.Now(), time.Now()), "touch the file")
	out, err = GenerateRepoMap(context.Background(), root, 3, "")
	requireErr(t, err, "repo map")
	if !strings.Contains(out, "func OnDisk") || strings.Contains(out, "FromIndex") {
		t.Fatalf("a changed file was served from the index:\n%s", out)
	}
}

func TestGenerateRepoMapQueryMatchesIndexedDeclarations(t *testing.T) {
	root, _ := indexedRepo(t, codegraph.Symbol{QualifiedName: "svc.ClaimNext", DisplayName: "ClaimNext", Line: 3, Kind: "func", Language: "go"})

	out, err := GenerateRepoMap(context.Background(), root, 3, "claim next")
	requireErr(t, err, "repo map")
	if !strings.Contains(out, "svc/queue.go") || !strings.Contains(out, "func ClaimNext:3") {
		t.Fatalf("the query missed the indexed declaration:\n%s", out)
	}
}

func TestCodeIndexDatabaseLivesInTheGitDir(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	gotRoot, dbPath, err := codegraph.DBPathFor(filepath.Join(root))
	requireErr(t, err, "db path")
	wantRoot, _ := filepath.EvalSymlinks(root)
	if gotRoot != wantRoot {
		t.Fatalf("root = %s, want %s", gotRoot, wantRoot)
	}
	if !strings.HasPrefix(dbPath, filepath.Join(wantRoot, ".git")+string(filepath.Separator)) {
		t.Fatalf("db path %s is outside the git dir", dbPath)
	}
}

func TestTakeLock(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "codegraph.db.lock")
	requireErr(t, takeLock(lock), "first take")
	if err := takeLock(lock); err != errCodeIndexBusy {
		t.Fatalf("second take = %v, want busy", err)
	}
	old := time.Now().Add(-codeIndexStaleLock - time.Minute)
	requireErr(t, os.Chtimes(lock, old, old), "age the lock")
	requireErr(t, takeLock(lock), "take over an abandoned lock")
}
