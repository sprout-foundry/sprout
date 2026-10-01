//go:build !js

package webui

// terminal_shell_discovery.go — available-shell discovery: the
// knownUnixShells list, AvailableShells, and the Unix/Windows PATH
// scanners. Split out of terminal_types.go.
import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// knownUnixShells is the ordered list of shells to scan for on Unix systems.
var knownUnixShells = []string{
	"bash", "zsh", "fish", "sh", "dash", "ash", "ksh", "csh", "tcsh",
}

// AvailableShells returns a list of shells found on the system.
// On Unix, it scans for common shells plus the user's $SHELL.
// On Windows, it returns cmd.exe and PowerShell if found.
func (tm *TerminalManager) AvailableShells() []ShellInfo {
	if runtime.GOOS == "windows" {
		return tm.availableWindowsShells()
	}
	return tm.availableUnixShells()
}

// availableUnixShells scans PATH for common Unix shells.
func (tm *TerminalManager) availableUnixShells() []ShellInfo {
	userShell := os.Getenv("SHELL")
	// Normalize $SHELL to a basename so comparisons work regardless of
	// whether $SHELL is "/bin/bash" or simply "bash".
	defaultShell := ""
	if userShell != "" {
		defaultShell = filepath.Base(userShell)
	}
	if defaultShell == "" {
		for _, name := range knownUnixShells {
			if shellExists(name) {
				defaultShell = name
				break
			}
		}
	}

	seen := make(map[string]bool)
	var shells []ShellInfo

	for _, name := range knownUnixShells {
		if seen[name] {
			continue
		}
		path := shellResolvePath(name)
		if path == "" {
			continue
		}
		seen[name] = true
		shells = append(shells, ShellInfo{
			Name:    name,
			Path:    path,
			Default: name == defaultShell,
		})
	}

	// Include user's $SHELL if not already in the list.
	if userShell != "" {
		userShellBase := filepath.Base(userShell)
		if !seen[userShellBase] {
			path := shellResolvePath(userShell)
			if path != "" {
				shells = append(shells, ShellInfo{
					Name:    userShellBase,
					Path:    path,
					Default: true,
				})
			}
		}
	}

	return shells
}

// availableWindowsShells returns available shells on Windows.
func (tm *TerminalManager) availableWindowsShells() []ShellInfo {
	var shells []ShellInfo

	if path, err := exec.LookPath("cmd.exe"); err == nil {
		shells = append(shells, ShellInfo{Name: "cmd.exe", Path: path, Default: true})
	}
	if path, err := exec.LookPath("powershell.exe"); err == nil {
		shells = append(shells, ShellInfo{Name: "powershell.exe", Path: path, Default: false})
	}

	return shells
}
