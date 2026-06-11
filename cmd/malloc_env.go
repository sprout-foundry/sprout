//go:build !js

package cmd

import "os"

// mallocDebugEnvVars are macOS libmalloc "stack logging" debug switches. When
// any is set (e.g. left over from Xcode/Instruments or a debug session),
// libmalloc prints a noisy line to stderr such as:
//
//	sprout(12345) MallocStackLogging: could not tag MSL-related memory as
//	  no_footprint, so those pages will be included in process footprint - (null)
//
// The variable is inherited by every child process, and sprout spawns a lot of
// them (git, shell commands, MCP servers, the `automate` re-exec, …), so the
// line repeats for each one — turning a single stray env var into a wall of
// noise.
var mallocDebugEnvVars = []string{
	"MallocStackLogging",
	"MallocStackLoggingNoCompact",
	"MallocStackLoggingDirectory",
}

func init() { stripMallocDebugEnv() }

// stripMallocDebugEnv removes the macOS malloc stack-logging variables from this
// process's environment at startup so the subprocesses sprout spawns don't
// inherit them and re-emit the warning. It's a no-op off macOS / when nothing is
// set. sprout's OWN one-time line (if any) is already initialized by libmalloc
// before main() runs and can't be unset from here — unset the variable in your
// shell to silence that one. Set SPROUT_KEEP_MALLOC_DEBUG=1 to opt out (e.g. if
// you're intentionally profiling child processes).
func stripMallocDebugEnv() {
	if os.Getenv("SPROUT_KEEP_MALLOC_DEBUG") == "1" {
		return
	}
	for _, k := range mallocDebugEnvVars {
		if _, ok := os.LookupEnv(k); ok {
			_ = os.Unsetenv(k)
		}
	}
}
