package utils

import (
	"os"
	"testing"
)

func TestAdoptProjectLogRenamesTheEarlierLog(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(".sprout", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(earlierProjectLogPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	AdoptProjectLog()
	if b, err := os.ReadFile(ProjectLogPath); err != nil || string(b) != "old" {
		t.Fatalf("project.log = %q, %v", b, err)
	}
	if _, err := os.Stat(earlierProjectLogPath); !os.IsNotExist(err) {
		t.Fatal("workspace.log is still there")
	}
}
