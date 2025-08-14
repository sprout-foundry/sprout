package embedding

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/workspaceinfo"
)

func TestGenerateWorkspaceEmbeddings_AddUpdateRemoveFlow(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	_ = os.Chdir(dir)

	// Create two files
	if err := os.WriteFile("a.go", []byte("package a\n\nfunc A(){}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("b.go", []byte("package b\n\nfunc B(){}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	wf := workspaceinfo.WorkspaceFile{Files: map[string]workspaceinfo.WorkspaceFileInfo{
		filepath.ToSlash(filepath.Join(dir, "a.go")): {Summary: "A", Exports: "A()", TokenCount: 10},
		filepath.ToSlash(filepath.Join(dir, "b.go")): {Summary: "B", Exports: "B()", TokenCount: 12},
	}}

	db := NewVectorDB()
	cfg := &config.Config{EmbeddingModel: "test:dummy"}

	// Initial generation
	if err := GenerateWorkspaceEmbeddings(wf, db, cfg); err != nil {
		t.Fatalf("gen1: %v", err)
	}
	// Ensure files on disk
	for _, f := range []string{"a.go", "b.go"} {
		id := "file:" + filepath.ToSlash(filepath.Join(dir, f))
		p := GetEmbeddingFilePath(id)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("missing embedding for %s: %v", f, err)
		}
	}

	// Remove b.go from workspace, update a.go timestamp to trigger up-to-date logic
	wf2 := workspaceinfo.WorkspaceFile{Files: map[string]workspaceinfo.WorkspaceFileInfo{
		filepath.ToSlash(filepath.Join(dir, "a.go")): {Summary: "A2", Exports: "A()", TokenCount: 10},
	}}
	if err := GenerateWorkspaceEmbeddings(wf2, db, cfg); err != nil {
		t.Fatalf("gen2: %v", err)
	}

	// b.go embedding should be removed
	bid := "file:" + filepath.ToSlash(filepath.Join(dir, "b.go"))
	if _, err := os.Stat(GetEmbeddingFilePath(bid)); !os.IsNotExist(err) {
		t.Fatalf("expected b.go embedding removed, err=%v", err)
	}
}

func TestVectorDB_Search(t *testing.T) {
	db := NewVectorDB()
	e1 := &CodeEmbedding{ID: "file:/x/a.go", Path: "/x/a.go", Name: "a.go", Vector: []float64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}}
	e2 := &CodeEmbedding{ID: "file:/x/b.go", Path: "/x/b.go", Name: "b.go", Vector: []float64{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}}
	db.Add(e1)
	db.Add(e2)
	// Query vector near e1
	res, scores, err := db.Search([]float64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, 1)
	if err != nil || len(res) != 1 || res[0].ID != e1.ID || scores[0] <= 0.0 {
		t.Fatalf("unexpected search result: %v %v %v", res, scores, err)
	}
}

func TestSearchRelevantFiles_UsesTestProvider(t *testing.T) {
	db := NewVectorDB()
	e1 := &CodeEmbedding{ID: "file:/x/a.go", Path: "/x/a.go", Name: "a.go", Vector: make([]float64, 16)}
	e2 := &CodeEmbedding{ID: "file:/x/b.go", Path: "/x/b.go", Name: "b.go", Vector: make([]float64, 16)}
	e1.Vector[1] = 10 // bucket for 'a' (97 % 16 == 1)
	e2.Vector[2] = 10 // bucket for 'b' (98 % 16 == 2)
	db.Add(e1)
	db.Add(e2)
	cfg := &config.Config{EmbeddingModel: "test:dummy"}
	embs, scores, err := SearchRelevantFiles("aaa", db, 1, cfg)
	if err != nil {
		t.Fatalf("SearchRelevantFiles error: %v", err)
	}
	if len(embs) != 1 || embs[0].ID != e1.ID || scores[0] <= 0 {
		t.Fatalf("unexpected relevant file result: %v %v", embs, scores)
	}
}
