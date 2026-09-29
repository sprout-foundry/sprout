package tools

import (
	"runtime"

	"github.com/sprout-foundry/sprout/pkg/utils/shellexec"
)

// shellPlatformNote tells the model which shell will interpret its command
// on Windows, where nothing else in the prompt reveals the host OS and the
// POSIX shell strips unquoted backslashes from paths like C:\Users.
func shellPlatformNote() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	if sh := shellexec.Path(); sh != "" {
		return " Host OS is Windows; commands run in a POSIX shell (" + sh + "), so use POSIX syntax and write Windows paths with forward slashes (C:/Users/me/file.txt) or single-quote them — the shell strips unquoted backslashes."
	}
	return " Host OS is Windows and no POSIX shell is installed; commands run in cmd.exe, so use cmd syntax (dir, type, copy) and Windows paths."
}
