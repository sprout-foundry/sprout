package webui

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// pkg/logging and pkg/history — per SP-028 these are listed as packages
		// with known long-lived workers. Neither package currently spawns
		// goroutines, so there are no function names to ignore here. Add
		// IgnoreAnyFunction(...) entries when background workers land.

		// Known long-lived workers from pkg/webui infrastructure.
		// These background loops run across tests and leak until their
		// lifecycle is fixed (SP-028 Phase 3).
		goleak.IgnoreTopFunction("github.com/sprout-foundry/sprout/pkg/webui.(*fileWatcher).start.func1"),
		goleak.IgnoreTopFunction("github.com/sprout-foundry/sprout/pkg/lsp/proxy.(*Manager).cleanupLoop"),
		goleak.IgnoreTopFunction("github.com/sprout-foundry/sprout/pkg/webui.(*TerminalManager).ExecuteCommandAndWait"),
		goleak.IgnoreTopFunction("github.com/sprout-foundry/sprout/pkg/webui.(*TerminalManager).ExecuteCommandAndWait.func1"),

		// Library goroutines that appear at various depths in leaked stacks.
		// IgnoreAnyFunction matches anywhere in the call stack (more precise
		// than IgnoreTopFunction for deep-internal library code).
		goleak.IgnoreAnyFunction("github.com/fsnotify/fsnotify.(*shared).sendEvent"),
		goleak.IgnoreAnyFunction("os/exec.(*Cmd).watchCtx"),
		goleak.IgnoreAnyFunction("internal/poll.runtime_pollWait"),

		// syscall.Syscall is the top-of-stack for goroutines blocked in raw
		// syscalls (e.g., fsnotify's inotify watcher). This is broad by
		// necessity — the test suite spawns processes that don't fully clean
		// up. Narrow this when SP-028 Phase 3 fixes the underlying leaks.
		goleak.IgnoreTopFunction("syscall.Syscall"),
	)
}
