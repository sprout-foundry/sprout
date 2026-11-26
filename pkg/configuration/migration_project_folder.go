package configuration

import (
	"log"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// projectFolderStateRenames are the state files sprout kept under the earlier
// "workspace" word, and their names now.
var projectFolderStateRenames = [][2]string{
	{"recent_workspaces.json", "recent_project_folders.json"},
	{"workspace_consent.json", "project_folder_consent.json"},
	{"client_workspaces.json", "client_project_folders.json"},
	{"workspace.log", "project_folder.log"},
}

// RenameProjectFolderState moves each state file still under its earlier
// name to its current one. A file already under its current name wins and
// the earlier one is left alone, so running it again changes nothing.
func RenameProjectFolderState() {
	stateDir, err := envutil.StateDir()
	if err != nil {
		return
	}
	renameStateFiles(stateDir)
}

func renameStateFiles(stateDir string) {
	for _, r := range projectFolderStateRenames {
		earlier, current := filepath.Join(stateDir, r[0]), filepath.Join(stateDir, r[1])
		if fileExists(current) || !fileExists(earlier) {
			continue
		}
		if err := os.Rename(earlier, current); err != nil {
			log.Printf("[migration] warning: rename %s to %s: %v", r[0], r[1], err)
		}
	}
}
