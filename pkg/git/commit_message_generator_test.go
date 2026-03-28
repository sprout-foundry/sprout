package git

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestActionFromStatus(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"A", "Adds"},
		{"D", "Deletes"},
		{"R", "Renames"},
		{"M", "Updates"},
		{"M ", "Updates"},
		{"", "Updates"},
		{"?", "Updates"},
	}
	for _, tt := range tests {
		t.Run("status_"+tt.status, func(t *testing.T) {
			assert.Equal(t, tt.want, actionFromStatus(tt.status))
		})
	}
}

func TestFileActionsSummary_Unmixed(t *testing.T) {
	tests := []struct {
		name    string
		changes []CommitFileChange
		want    string
	}{
		{
			name: "all additions",
			changes: []CommitFileChange{
				{Status: "A", Path: "foo.go"},
				{Status: "A", Path: "bar.go"},
				{Status: "A", Path: "baz.go"},
			},
			want: "Adds 3 files",
		},
		{
			name: "all deletions",
			changes: []CommitFileChange{
				{Status: "D", Path: "old.go"},
				{Status: "D", Path: "stale.go"},
			},
			want: "Deletes 2 files",
		},
		{
			name: "all renames",
			changes: []CommitFileChange{
				{Status: "R", Path: "a.go"},
			},
			want: "Renames a.go",
		},
		{
			name: "all modifications",
			changes: []CommitFileChange{
				{Status: "M", Path: "main.go"},
				{Status: "M", Path: "util.go"},
			},
			want: "Updates 2 files",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := computeFileActionsSummary(tt.changes)
			assert.Equal(t, tt.want, summary)
		})
	}
}

func TestFileActionsSummary_Mixed(t *testing.T) {
	tests := []struct {
		name    string
		changes []CommitFileChange
		want    string
	}{
		{
			name: "adds and modifications",
			changes: []CommitFileChange{
				{Status: "A", Path: "new.go"},
				{Status: "M", Path: "existing.go"},
				{Status: "M", Path: "other.go"},
			},
			want: "Updates 3 files",
		},
		{
			name: "adds and deletes",
			changes: []CommitFileChange{
				{Status: "A", Path: "new.go"},
				{Status: "D", Path: "old.go"},
				{Status: "A", Path: "another.go"},
				{Status: "D", Path: "gone.go"},
			},
			want: "Updates 4 files",
		},
		{
			name: "all three types mixed",
			changes: []CommitFileChange{
				{Status: "A", Path: "new.go"},
				{Status: "M", Path: "mod.go"},
				{Status: "D", Path: "del.go"},
				{Status: "A", Path: "new2.go"},
				{Status: "M", Path: "mod2.go"},
			},
			want: "Updates 5 files",
		},
		{
			name: "deletes and modifications",
			changes: []CommitFileChange{
				{Status: "M", Path: "updated.go"},
				{Status: "D", Path: "removed.go"},
			},
			want: "Updates 2 files",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := computeFileActionsSummary(tt.changes)
			assert.Equal(t, tt.want, summary)
		})
	}
}

// computeFileActionsSummary extracts the summary logic so it can be tested without needing an LLM client.
func computeFileActionsSummary(fileChanges []CommitFileChange) string {
	primaryAction := "Updates"
	fileActions := make([]string, 0, len(fileChanges))
	actionCounts := make(map[string]int)
	for _, change := range fileChanges {
		action := actionFromStatus(change.Status)
		actionCounts[action]++
		if len(change.Path) > 0 {
			fileActions = append(fileActions, action+" "+change.Path)
		}
	}
	if len(actionCounts) == 1 {
		for action := range actionCounts {
			primaryAction = action
			break
		}
	}
	fileActionsSummary := primaryAction + " " + fmt.Sprintf("%d", len(fileChanges)) + " files"
	if len(fileActions) == 1 {
		fileActionsSummary = fileActions[0]
	}
	return fileActionsSummary
}
