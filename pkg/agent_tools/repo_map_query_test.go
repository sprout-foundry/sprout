package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// A query searches every file, not only the ones the per-directory caps keep:
// a file far past the cap in a large directory is still found.
func TestGenerateRepoMapQueryBeforeCaps(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{}
	for i := 0; i < repoMapPerDirCap+40; i++ {
		files[fmt.Sprintf("internal/api/a%03d.go", i)] = fmt.Sprintf("package api\n\nfunc Handler%d() {}\n", i)
	}
	files["internal/api/zz_queue.go"] = "package api\n\nfunc ClaimNextQueuedTask() {}\n"
	createTestFiles(t, dir, files)

	out, err := GenerateRepoMap(context.Background(), dir, 3, "queue")
	requireErr(t, err, "repo map")
	if !strings.Contains(out, "zz_queue.go") {
		t.Fatalf("query missed the file past the cap:\n%s", out)
	}
	if strings.Contains(out, "a001.go") {
		t.Fatalf("query kept files that do not match:\n%s", out)
	}
}

// A query of several words matches a name holding all of them in any form:
// "task queue" finds TaskQueue, task_queue and task-queue.
func TestGenerateRepoMapQueryWords(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"store/task_queue.go": "package store\n\nfunc Claim() {}\n",
		"api/handlers.go":     "package api\n\ntype TaskQueue struct{}\n",
		"web/task-queue.ts":   "export function claim() {}\n",
		"api/other.go":        "package api\n\nfunc Task() {}\n",
	})

	out, err := GenerateRepoMap(context.Background(), dir, 3, "task queue")
	requireErr(t, err, "repo map")
	for _, want := range []string{"store/task_queue.go", "api/handlers.go", "web/task-queue.ts"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "api/other.go") {
		t.Errorf("matched a file with only one of the words:\n%s", out)
	}
}

// A file that declares a match ranks before one that only mentions it.
func TestFilterFilesByQueryContentRanksDeclarations(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"a/uses.go":    "package a\n\nfunc run() { _ = MarkDone }\n",
		"b/declare.go": "package b\n\nfunc MarkDone() {}\n",
	})
	var entries []fileEntry
	for _, rel := range []string{"a/uses.go", "b/declare.go"} {
		entries = append(entries, fileEntry{absPath: dir + "/" + rel, relPath: rel, ext: ".go"})
	}
	got := filterFilesByQueryContent(entries, "mark done")
	if len(got) != 2 || got[0].relPath != "b/declare.go" {
		t.Fatalf("got %v, want the declaring file first", got)
	}
}

// A large top-level directory is capped per package, and the inclusion order
// visits every group in turn, so one big package cannot fill the map.
func TestBuildInclusionOrderGroupsLargeDirs(t *testing.T) {
	var files []fileEntry
	add := func(rel string) { files = append(files, fileEntry{relPath: rel, ext: ".go"}) }
	for i := 0; i < repoMapPerDirCap+10; i++ {
		add(fmt.Sprintf("internal/store/s%03d.go", i))
	}
	add("internal/api/handler.go")
	add("cmd/main.go")

	groupOf := capGroups(files)
	if g := groupOf("internal/api/handler.go"); g != "internal/api" {
		t.Fatalf("large dir group = %q, want internal/api", g)
	}
	if g := groupOf("cmd/main.go"); g != "cmd" {
		t.Fatalf("small dir group = %q, want cmd", g)
	}

	ordered, _, _ := buildInclusionOrder(files)
	first := map[string]bool{}
	for _, f := range ordered[:3] {
		first[groupOf(f.relPath)] = true
	}
	if !first["internal/api"] || !first["cmd"] {
		t.Fatalf("first three files did not cover every group: %v", ordered[:3])
	}
}
