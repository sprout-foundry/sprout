package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseDiffForContent(t *testing.T) {
	tests := []struct {
		name           string
		diffOutput     string
		filename       string
		wantOldContent string
		wantNewContent string
	}{
		{
			name: "addition followed by context",
			diffOutput: `diff --git a/file.go b/file.go
index 123..456 789
--- a/file.go
+++ b/file.go
@@ -1,3 +1,4 @@
 line 1
+new line 2
 line 3`,
			filename:       "file.go",
			wantOldContent: "\n",
			wantNewContent: "+new line 2\nline 3\n",
		},
		{
			name: "deletion followed by context",
			diffOutput: `diff --git a/file.go b/file.go
index 123..456 789
--- a/file.go
+++ b/file.go
@@ -1,3 +1,2 @@
 line 1
-old line 2
 line 3`,
			filename:       "file.go",
			wantOldContent: "-old line 2\nline 3\n",
			wantNewContent: "\n",
		},
		{
			name: "deletion then addition",
			diffOutput: `diff --git a/file.go b/file.go
index 123..456 789
--- a/file.go
+++ b/file.go
@@ -1,3 +1,3 @@
 line 1
-old line 2
+new line 2
 line 3`,
			filename:       "file.go",
			wantOldContent: "-old line 2\n",
			wantNewContent: "+new line 2\nline 3\n",
		},
		{
			name:           "empty diff string",
			diffOutput:     "",
			filename:       "file.go",
			wantOldContent: "\n",
			wantNewContent: "\n",
		},
		{
			name: "only context lines produces empty sections",
			diffOutput: `diff --git a/file.go b/file.go
index 123..456 789
--- a/file.go
+++ b/file.go
@@ -1,3 +1,3 @@
 line 1
 line 2
 line 3`,
			filename:       "file.go",
			wantOldContent: "\n",
			wantNewContent: "\n",
		},
		{
			name: "multiple additions in sequence",
			diffOutput: `diff --git a/file.go b/file.go
index 123..456 789
--- a/file.go
+++ b/file.go
@@ -1,2 +1,5 @@
 line 1
+new line 2
+new line 3
+new line 4
 line 5`,
			filename:       "file.go",
			wantOldContent: "\n",
			wantNewContent: "+new line 2\n+new line 3\n+new line 4\nline 5\n",
		},
		{
			name: "multiple deletions in sequence",
			diffOutput: `diff --git a/file.go b/file.go
index 123..456 789
--- a/file.go
+++ b/file.go
@@ -1,5 +1,2 @@
 line 1
-old line 2
-old line 3
-old line 4
 line 5`,
			filename:       "file.go",
			wantOldContent: "-old line 2\n-old line 3\n-old line 4\nline 5\n",
			wantNewContent: "\n",
		},
		{
			name: "two files collects from all",
			diffOutput: `diff --git a/other.go b/other.go
index 123..456 789
--- a/other.go
+++ b/other.go
@@ -1,1 +1,1 @@
-old1
+new1
diff --git a/target.go b/target.go
index 789..012 345
--- a/target.go
+++ b/target.go
@@ -1,1 +1,1 @@
-old2
+new2`,
			filename:       "target.go",
			wantOldContent: "-old1\n-old2\n",
			wantNewContent: "+new1\n+new2\n",
		},
		{
			name: "filename parameter resets sections when matched",
			diffOutput: `diff --git a/file.go b/file.go
--- a/file.go
+++ b/file.go
-old line
+new line`,
			filename:       "file.go",
			wantOldContent: "-old line\n",
			wantNewContent: "+new line\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOld, gotNew := parseDiffForContent(tt.diffOutput, tt.filename)
			assert.Equal(t, tt.wantOldContent, gotOld, "old content mismatch")
			assert.Equal(t, tt.wantNewContent, gotNew, "new content mismatch")
		})
	}
}
