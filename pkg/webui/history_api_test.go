package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/history"
)

func TestHandleAPIHistoryChangelogMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/history/changelog", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIHistoryChangelog(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIHistoryRollbackMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/history/rollback", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIHistoryRollback(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIHistoryChangesMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/history/changes", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIHistoryChanges(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIHistoryRevisionMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/history/revision?revision_id=test", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIHistoryRevision(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIHistoryRevisionMissingRevisionID(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/history/revision", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIHistoryRevision(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIHistoryRollbackMissingRevisionID(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/history/rollback", strings.NewReader(`{"revision_id":""}`))
	rec := httptest.NewRecorder()
	ws.handleAPIHistoryRollback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRelativeFilePath(t *testing.T) {
	ws := &ReactWebServer{workspaceRoot: "/home/user/project"}

	tests := []struct {
		path string
		want string
	}{
		{"/home/user/project/src/main.go", "src/main.go"},
		{"/home/user/project", "."},
		{"/other/path/file.txt", "/other/path/file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := ws.relativeFilePath(ws.workspaceRoot, tt.path)
			if got != tt.want {
				t.Errorf("relativeFilePath(%q, %q) = %q, want %q", ws.workspaceRoot, tt.path, got, tt.want)
			}
		})
	}
}

func TestBuildChangelogEntryCreatedFile(t *testing.T) {
	ws := &ReactWebServer{workspaceRoot: "/home/user/project"}

	group := history.RevisionGroup{
		RevisionID:   "rev-1",
		Timestamp:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Instructions: "Initial commit",
		Changes: []history.ChangeLog{
			{
				Filename:     "/home/user/project/src/main.go",
				OriginalCode: "",
				NewCode:      "package main\nfunc main() {}",
			},
		},
	}

	entry := ws.buildChangelogEntry(ws.workspaceRoot, group)
	if entry.RevisionID != "rev-1" {
		t.Errorf("expected revision_id rev-1, got %q", entry.RevisionID)
	}
	if len(entry.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entry.Files))
	}
	if entry.Files[0].Operation != "created" {
		t.Errorf("expected operation created, got %q", entry.Files[0].Operation)
	}
	if entry.Files[0].Path != "src/main.go" {
		t.Errorf("expected path src/main.go, got %q", entry.Files[0].Path)
	}
}

func TestBuildChangelogEntryDeletedFile(t *testing.T) {
	ws := &ReactWebServer{workspaceRoot: "/home/user/project"}

	group := history.RevisionGroup{
		RevisionID:   "rev-2",
		Timestamp:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Instructions: "",
		Changes: []history.ChangeLog{
			{
				Filename:     "/home/user/project/old.txt",
				OriginalCode: "line1\nline2",
				NewCode:      "",
			},
		},
	}

	entry := ws.buildChangelogEntry(ws.workspaceRoot, group)
	if entry.Files[0].Operation != "deleted" {
		t.Errorf("expected operation deleted, got %q", entry.Files[0].Operation)
	}
}

func TestBuildChangelogEntryEditedFile(t *testing.T) {
	ws := &ReactWebServer{workspaceRoot: "/home/user/project"}

	group := history.RevisionGroup{
		RevisionID:   "rev-3",
		Timestamp:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Instructions: "",
		Changes: []history.ChangeLog{
			{
				Filename:     "/home/user/project/edit.go",
				OriginalCode: "old code",
				NewCode:      "new code",
			},
		},
	}

	entry := ws.buildChangelogEntry(ws.workspaceRoot, group)
	if entry.Files[0].Operation != "edited" {
		t.Errorf("expected operation edited, got %q", entry.Files[0].Operation)
	}
}

func TestConvertRevisionGroupsLimits(t *testing.T) {
	ws := &ReactWebServer{workspaceRoot: "/home/user/project"}

	groups := make([]history.RevisionGroup, 0)
	for i := 0; i < 150; i++ {
		groups = append(groups, history.RevisionGroup{
			RevisionID:   "rev-100",
			Timestamp:    time.Now(),
			Instructions: "test",
		})
	}

	result := ws.convertRevisionGroups(ws.workspaceRoot, groups, maxRevisions)
	if len(result) != maxRevisions {
		t.Errorf("expected %d entries after limit, got %d", maxRevisions, len(result))
	}
}
