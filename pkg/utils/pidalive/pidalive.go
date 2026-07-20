// Package pidalive provides a single canonical answer to "is this PID alive?"
// for all of sprout. Replaces the per-package duplicate helpers that used
// os.FindProcess (which returns a non-nil handle even for dead PIDs on
// Windows, so a recycled PID would falsely appear alive).
//
// Build tags in pidalive_windows.go and pidalive_unix.go route to the
// platform-specific implementation. The Unix version uses syscall.Kill(pid, 0)
// which is the correct cross-platform check on POSIX. The Windows version
// uses OpenProcess + GetExitCodeProcess via golang.org/x/sys/windows so it
// correctly distinguishes running PIDs from recycled/reused PIDs.
//
// Tier 1 of SP-112 (platform parity).
package pidalive
