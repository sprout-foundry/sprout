package personas

import (
	"strings"
	"testing"
	"testing/fstest"
)

// loadDefinitionsFromFS is the testable seam introduced to drive the conflict
// detection paths in loadEmbeddedDefinitions without colliding with the real
// embedded catalog. Each test below builds an in-memory fs.FS with the
// configs/ directory layout the loader expects.

func TestLoadDefinitionsFromFS_DuplicateID(t *testing.T) {
	fsys := fstest.MapFS{
		"configs/a.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "duplicated", "name": "First"}
			]
		}`)},
		"configs/b.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "duplicated", "name": "Second"}
			]
		}`)},
	}

	_, err := loadDefinitionsFromFS(fsys, "configs")
	if err == nil {
		t.Fatal("expected error for duplicate persona id across files, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate persona id") {
		t.Errorf("error should mention duplicate persona id, got: %v", err)
	}
	if !strings.Contains(err.Error(), "duplicated") {
		t.Errorf("error should name the conflicting id, got: %v", err)
	}
}

func TestLoadDefinitionsFromFS_AliasShadowsExistingID(t *testing.T) {
	// File a.json declares "coder". File b.json declares "researcher" with
	// alias "coder" — that alias shadows the existing ID, which must error.
	fsys := fstest.MapFS{
		"configs/a.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "coder", "name": "Coder"}
			]
		}`)},
		"configs/b.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "researcher", "name": "Researcher", "aliases": ["coder"]}
			]
		}`)},
	}

	_, err := loadDefinitionsFromFS(fsys, "configs")
	if err == nil {
		t.Fatal("expected error for alias shadowing existing persona id, got nil")
	}
	if !strings.Contains(err.Error(), "shadows persona id") {
		t.Errorf("error should mention id shadowing, got: %v", err)
	}
}

func TestLoadDefinitionsFromFS_IDShadowsExistingAlias(t *testing.T) {
	// Inverse case: alias declared first, then a later persona's id collides
	// with that alias.
	fsys := fstest.MapFS{
		"configs/a.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "researcher", "name": "Researcher", "aliases": ["digger"]}
			]
		}`)},
		"configs/b.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "digger", "name": "Digger"}
			]
		}`)},
	}

	_, err := loadDefinitionsFromFS(fsys, "configs")
	if err == nil {
		t.Fatal("expected error for persona id shadowed by prior alias, got nil")
	}
	if !strings.Contains(err.Error(), "shadowed by alias") {
		t.Errorf("error should mention alias shadowing, got: %v", err)
	}
}

func TestLoadDefinitionsFromFS_DuplicateAlias(t *testing.T) {
	fsys := fstest.MapFS{
		"configs/a.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "first", "name": "First", "aliases": ["shared"]}
			]
		}`)},
		"configs/b.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "second", "name": "Second", "aliases": ["shared"]}
			]
		}`)},
	}

	_, err := loadDefinitionsFromFS(fsys, "configs")
	if err == nil {
		t.Fatal("expected error for duplicate alias across files, got nil")
	}
	if !strings.Contains(err.Error(), "alias") {
		t.Errorf("error should mention alias conflict, got: %v", err)
	}
}

func TestLoadDefinitionsFromFS_EmptyIDRejected(t *testing.T) {
	fsys := fstest.MapFS{
		"configs/a.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "", "name": "Nameless"}
			]
		}`)},
	}

	_, err := loadDefinitionsFromFS(fsys, "configs")
	if err == nil {
		t.Fatal("expected error for empty persona id, got nil")
	}
	if !strings.Contains(err.Error(), "empty id") {
		t.Errorf("error should mention empty id, got: %v", err)
	}
}

func TestLoadDefinitionsFromFS_SelfAliasIsHarmless(t *testing.T) {
	// A persona that lists its own id in aliases should load without error
	// (the loader treats self-aliases as a no-op).
	fsys := fstest.MapFS{
		"configs/a.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "coder", "name": "Coder", "aliases": ["coder", "dev"]}
			]
		}`)},
	}

	defs, err := loadDefinitionsFromFS(fsys, "configs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := defs["coder"]; !ok {
		t.Errorf("expected coder persona to load despite self-alias")
	}
}

func TestLoadDefinitionsFromFS_CleanCatalogLoadsAllPersonas(t *testing.T) {
	fsys := fstest.MapFS{
		"configs/a.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "coder", "name": "Coder"},
				{"id": "tester", "name": "Tester"}
			]
		}`)},
		"configs/b.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "researcher", "name": "Researcher", "aliases": ["analyst"]}
			]
		}`)},
	}

	defs, err := loadDefinitionsFromFS(fsys, "configs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"coder", "tester", "researcher"} {
		if _, ok := defs[want]; !ok {
			t.Errorf("expected persona %q to load", want)
		}
	}
}
