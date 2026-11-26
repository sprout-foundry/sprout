package utils

import "os"

// ProjectLogPath is the project folder's log, relative to the project folder.
const ProjectLogPath = ".sprout/project.log"

const earlierProjectLogPath = ".sprout/workspace.log"

// AdoptProjectLog renames the log an earlier version kept as
// .sprout/workspace.log, unless the current log already exists.
func AdoptProjectLog() {
	if _, err := os.Stat(ProjectLogPath); err == nil {
		return
	}
	if _, err := os.Stat(earlierProjectLogPath); err == nil {
		_ = os.Rename(earlierProjectLogPath, ProjectLogPath)
	}
}
