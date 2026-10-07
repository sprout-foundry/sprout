package timeline

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/deploy"
	"github.com/sprout-foundry/sprout/pkg/history"
)

func TestSummarizeRevision_Templates(t *testing.T) {
	tests := []struct {
		name      string
		group     *history.RevisionGroup
		wantText  string
		wantFiles []string
		wantIns   int
		wantDel   int
	}{
		{
			name:     "nil group",
			group:    nil,
			wantText: noChangesSummary,
		},
		{
			name:     "no changes",
			group:    &history.RevisionGroup{RevisionID: "r"},
			wantText: noChangesSummary,
		},
		{
			name: "single file insertion",
			group: &history.RevisionGroup{RevisionID: "r", Changes: []history.ChangeLog{
				{Filename: "a.go", Status: activeStatus, OriginalCode: "x\n", NewCode: "x\ny\n"},
			}},
			wantText:  "changed 1 file: a.go (+1/-0)",
			wantFiles: []string{"a.go"},
			wantIns:   1,
		},
		{
			name: "deletion only",
			group: &history.RevisionGroup{RevisionID: "r", Changes: []history.ChangeLog{
				{Filename: "a.go", Status: activeStatus, OriginalCode: "x\ny\n", NewCode: "x\n"},
			}},
			wantText:  "changed 1 file: a.go (+0/-1)",
			wantFiles: []string{"a.go"},
			wantDel:   1,
		},
		{
			name: "identical content counts zero",
			group: &history.RevisionGroup{RevisionID: "r", Changes: []history.ChangeLog{
				{Filename: "a.go", Status: activeStatus, OriginalCode: "x\n", NewCode: "x\n"},
			}},
			wantText:  "changed 1 file: a.go",
			wantFiles: []string{"a.go"},
		},
		{
			name: "internal edit counts only the middle",
			group: &history.RevisionGroup{RevisionID: "r", Changes: []history.ChangeLog{
				{Filename: "a.go", Status: activeStatus, OriginalCode: "head\nold\nold2\ntail\n", NewCode: "head\nnew\ntail\n"},
			}},
			wantText:  "changed 1 file: a.go (+1/-2)",
			wantFiles: []string{"a.go"},
			wantIns:   1,
			wantDel:   2,
		},
		{
			name: "duplicate filenames deduped and sorted",
			group: &history.RevisionGroup{RevisionID: "r", Changes: []history.ChangeLog{
				{Filename: "b.go", Status: activeStatus, OriginalCode: "x\n", NewCode: "x\ny\n"},
				{Filename: "a.go", Status: activeStatus, OriginalCode: "x\n", NewCode: "x\ny\n"},
				{Filename: "b.go", Status: activeStatus, OriginalCode: "x\n", NewCode: "x\nz\n"},
			}},
			wantText:  "changed 2 files: a.go, b.go (+3/-0)",
			wantFiles: []string{"a.go", "b.go"},
			wantIns:   3,
		},
		{
			name: "empty filename skipped",
			group: &history.RevisionGroup{RevisionID: "r", Changes: []history.ChangeLog{
				{Filename: "", Status: activeStatus, OriginalCode: "x\n", NewCode: "y\n"},
			}},
			wantText: noChangesSummary,
		},
		{
			name: "reverted change excluded",
			group: &history.RevisionGroup{RevisionID: "r", Changes: []history.ChangeLog{
				{Filename: "a.go", Status: "active", OriginalCode: "x\n", NewCode: "x\ny\n"},
				{Filename: "b.go", Status: "reverted", OriginalCode: "x\n", NewCode: "x\ny\n"},
			}},
			wantText:  "changed 1 file: a.go (+1/-0)",
			wantFiles: []string{"a.go"},
			wantIns:   1,
		},
		{
			name: "more than three files collapse the remainder",
			group: &history.RevisionGroup{RevisionID: "r", Changes: []history.ChangeLog{
				{Filename: "d.go", Status: activeStatus, OriginalCode: "x\n", NewCode: "x\ny\n"},
				{Filename: "c.go", Status: activeStatus, OriginalCode: "x\n", NewCode: "x\ny\n"},
				{Filename: "b.go", Status: activeStatus, OriginalCode: "x\n", NewCode: "x\ny\n"},
				{Filename: "a.go", Status: activeStatus, OriginalCode: "x\n", NewCode: "x\ny\n"},
				{Filename: "e.go", Status: activeStatus, OriginalCode: "x\n", NewCode: "x\ny\n"},
			}},
			wantText:  "changed 5 files: a.go, b.go, c.go, +2 more (+5/-0)",
			wantFiles: []string{"a.go", "b.go", "c.go", "d.go", "e.go"},
			wantIns:   5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SummarizeRevision(tt.group)
			if got.Text != tt.wantText {
				t.Errorf("text = %q, want %q", got.Text, tt.wantText)
			}
			if !equalStrings(got.Files, tt.wantFiles) {
				t.Errorf("files = %v, want %v", got.Files, tt.wantFiles)
			}
			if got.FilesTouched != len(tt.wantFiles) {
				t.Errorf("files touched = %d, want %d", got.FilesTouched, len(tt.wantFiles))
			}
			if got.Insertions != tt.wantIns {
				t.Errorf("insertions = %d, want %d", got.Insertions, tt.wantIns)
			}
			if got.Deletions != tt.wantDel {
				t.Errorf("deletions = %d, want %d", got.Deletions, tt.wantDel)
			}
		})
	}
}

func TestSummarizeDeploy_Templates(t *testing.T) {
	tests := []struct {
		name string
		d    deploy.Deployment
		want string
	}{
		{
			name: "production ready",
			d:    deploy.Deployment{Kind: deploy.KindProduction, Version: "v3", Status: deploy.StatusReady},
			want: "deployed v3 to production",
		},
		{
			name: "production with no version",
			d:    deploy.Deployment{Kind: deploy.KindProduction, Status: deploy.StatusReady},
			want: "deployed to production",
		},
		{
			name: "preview with id",
			d:    deploy.Deployment{ID: "proj-7", Kind: deploy.KindPreview, Status: deploy.StatusReady},
			want: "preview deploy proj-7",
		},
		{
			name: "preview with version and id",
			d:    deploy.Deployment{ID: "proj-7", Kind: deploy.KindPreview, Version: "v2", Status: deploy.StatusDeploying},
			want: "preview deploy v2 proj-7 (deploying)",
		},
		{
			name: "failed production",
			d:    deploy.Deployment{Kind: deploy.KindProduction, Version: "v9", Status: deploy.StatusFailed},
			want: "deployed v9 to production (failed)",
		},
		{
			name: "rolled back",
			d:    deploy.Deployment{Kind: deploy.KindProduction, Version: "v1", Status: deploy.StatusRolledBack},
			want: "deployed v1 to production (rolled back)",
		},
		{
			name: "queued",
			d:    deploy.Deployment{Kind: deploy.KindPreview, ID: "p-1", Status: deploy.StatusQueued},
			want: "preview deploy p-1 (queued)",
		},
		{
			name: "unknown status surfaced",
			d:    deploy.Deployment{Kind: deploy.KindProduction, Version: "v5", Status: deploy.StatusState("weird")},
			want: "deployed v5 to production (weird)",
		},
		{
			name: "unknown kind surfaced",
			d:    deploy.Deployment{Kind: deploy.DeploymentKind("staging"), Version: "v5", Status: deploy.StatusReady},
			want: "preview deploy v5 to staging",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SummarizeDeploy(tt.d); got != tt.want {
				t.Errorf("summary = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a\n", []string{"a"}},
		{"a\nb", []string{"a", "b"}},
		{"a\nb\n", []string{"a", "b"}},
		{"\n", []string{""}},
	}
	for _, tt := range tests {
		if got := splitLines(tt.in); !equalStrings(got, tt.want) {
			t.Errorf("splitLines(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestEntry_Malformed(t *testing.T) {
	var empty Entry
	if empty.Kind() != "" {
		t.Errorf("empty entry kind = %q, want empty", empty.Kind())
	}
	if empty.Summary() != "" {
		t.Errorf("empty entry summary = %q, want empty", empty.Summary())
	}
	if empty.ScopeIDs() != nil {
		t.Errorf("empty entry scope IDs = %v, want nil", empty.ScopeIDs())
	}

	both := Entry{ChangeSet: &ChangeSet{Summary: "s"}, Deploy: &Deploy{Summary: "d"}}
	if both.Kind() != "" {
		t.Errorf("both-payloads entry kind = %q, want empty", both.Kind())
	}
}
