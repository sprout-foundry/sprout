package configuration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenameStateFilesMovesEarlierNamesOnce(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return ""
		}
		return string(b)
	}
	write("recent_workspaces.json", "recent")
	write("workspace_consent.json", "old consent")
	write("project_folder_consent.json", "new consent")

	renameStateFiles(dir)
	renameStateFiles(dir)

	if read("recent_project_folders.json") != "recent" || read("recent_workspaces.json") != "" {
		t.Errorf("recent_workspaces.json was not moved")
	}
	if read("project_folder_consent.json") != "new consent" || read("workspace_consent.json") != "old consent" {
		t.Errorf("an existing current file must win and leave the earlier one alone")
	}
}
