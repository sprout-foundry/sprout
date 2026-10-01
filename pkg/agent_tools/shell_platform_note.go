package tools

import (
	"os"
	"path/filepath"
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
		return " Host OS is Windows; commands run in a POSIX shell (" + sh + "), so use POSIX syntax and write Windows paths with forward slashes (C:/Users/me/file.txt) or single-quote them — the shell strips unquoted backslashes." +
			" The shell and the file tools share /tmp (" + filepath.ToSlash(os.TempDir()) + "), and path arguments passed to native programs are converted, but a '/tmp' path written inside a script or JSON that node, python or other native programs read resolves to C:/tmp instead — embed the real path from `cygpath -m /tmp/<file>` or use a workspace-relative path."
	}
	return " Host OS is Windows and no POSIX shell is installed; commands run in cmd.exe, so use cmd syntax (dir, type, copy) and Windows paths."
}
