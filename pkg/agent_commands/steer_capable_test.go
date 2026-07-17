package commands

import (
	"sync"
	"testing"
)

// TestSteerCapable_SafeCommands verifies that commands implementing
// SteerCapable with SafeDuringSteer() == true are classified correctly.
func TestSteerCapable_SafeCommands(t *testing.T) {
	registry := DefaultRegistry()

	safeCommands := []string{
		"info",
		"codegraph",
		"model",
		"provider",
		"help",
		"status",
		"changes",
		"log",
		"mcp",
		"risk-profile",
		"max-context",
		"skill",
		"recall",
		"search",
		"transcript",
		"verbose",
		"tools",
		"keys",
		"custom",
		"persona",          // safe during steer
		"subagent-persona", // alias for /persona - safe
		"subagent-personas",// alias for /persona - safe
	}

	for _, name := range safeCommands {
		t.Run(name, func(t *testing.T) {
			cmd, ok := registry.GetCommand(name)
			if !ok {
				t.Fatalf("command %q not found in registry", name)
			}

			sc, ok := cmd.(SteerCapable)
			if !ok {
				t.Fatalf("command %q does not implement SteerCapable", name)
			}

			if !sc.SafeDuringSteer() {
				t.Errorf("command %q should be safe during steer, but SafeDuringSteer() returned false", name)
			}
		})
	}
}

// TestSteerCapable_UnsafeCommands verifies that commands implementing
// SteerCapable with SafeDuringSteer() == false are classified correctly.
// Commands that don't implement SteerCapable are treated as unsafe.
func TestSteerCapable_UnsafeCommands(t *testing.T) {
	registry := DefaultRegistry()

	// Commands that explicitly implement SteerCapable with SafeDuringSteer() == false
	unsafeCommands := []string{
		"setup",
		"settings",
		"compact",
		"sessions",
		"commit",
		"clear",
		"exit",
		"init",
		"shell",
		"review",
		"review-deep",
		"rollback",
		"rewind",
		"index",
		"fork",
	}

	for _, name := range unsafeCommands {
		t.Run(name, func(t *testing.T) {
			cmd, ok := registry.GetCommand(name)
			if !ok {
				t.Fatalf("command %q not found in registry", name)
			}

			sc, ok := cmd.(SteerCapable)
			if !ok {
				t.Fatalf("command %q does not implement SteerCapable", name)
			}

			if sc.SafeDuringSteer() {
				t.Errorf("command %q should NOT be safe during steer, but SafeDuringSteer() returned true", name)
			}
		})
	}
}

// TestSteerCapable_CommandsWithoutInterfaceAreUnsafe verifies that commands
// which don't implement SteerCapable are treated as unsafe.
func TestSteerCapable_CommandsWithoutInterfaceAreUnsafe(t *testing.T) {
	registry := DefaultRegistry()

	// These commands don't implement SteerCapable and should be treated as unsafe
	commandsWithoutInterface := []string{
		"exec",  // shell execution - unsafe
		"edit",  // opens editor - unsafe
		"usage", // uses agent state - treat as unsafe for now
	}

	for _, name := range commandsWithoutInterface {
		t.Run(name, func(t *testing.T) {
			cmd, ok := registry.GetCommand(name)
			if !ok {
				t.Fatalf("command %q not found in registry", name)
			}

			// Commands without SteerCapable are treated as unsafe
			if sc, ok := cmd.(SteerCapable); ok {
				if sc.SafeDuringSteer() {
					t.Errorf("command %q should NOT be safe during steer, but SafeDuringSteer() returned true", name)
				}
				return
			}
			// No SteerCapable interface - this is the expected path for these commands
		})
	}
}

// TestSteerCapable_AllRegisteredCommandsHaveInterface verifies that most
// registered commands implement SteerCapable. Some commands (exec, edit,
// subagent-persona*, usage) intentionally don't implement the interface
// and are treated as unsafe.
func TestSteerCapable_AllRegisteredCommandsHaveInterface(t *testing.T) {
	registry := DefaultRegistry()
	commands := registry.ListCommands()

	// Commands that intentionally don't implement SteerCapable
	knownMissing := map[string]bool{
		"exec":  true,
		"edit":  true,
		"usage": true,
	}

	missing := []string{}
	for _, cmd := range commands {
		if _, ok := cmd.(SteerCapable); !ok && !knownMissing[cmd.Name()] {
			missing = append(missing, cmd.Name())
		}
	}

	if len(missing) > 0 {
		t.Errorf("the following commands do not implement SteerCapable: %v", missing)
	}
}

// TestDefaultRegistry_Singleton verifies that DefaultRegistry returns the
// same instance on repeated calls (sync.Once semantics).
func TestDefaultRegistry_Singleton(t *testing.T) {
	// Note: We cannot directly reset sync.Once in tests, so we create fresh
	// registries to verify the behavior of NewCommandRegistry and verify
	// that DefaultRegistry itself returns a singleton by calling it multiple
	// times and checking the pointer.

	// Call DefaultRegistry multiple times and verify they return the same pointer.
	r1 := DefaultRegistry()
	r2 := DefaultRegistry()
	r3 := DefaultRegistry()

	if r1 != r2 {
		t.Error("DefaultRegistry() calls returned different instances")
	}
	if r2 != r3 {
		t.Error("DefaultRegistry() calls returned different instances")
	}
}

// TestDefaultRegistry_InitializesAllCommands verifies that the default
// registry has all expected commands registered.
func TestDefaultRegistry_InitializesAllCommands(t *testing.T) {
	registry := DefaultRegistry()

	expectedCommands := []string{
		// Read-only / config commands (safe)
		"info", "codegraph", "model", "provider", "help", "status",
		"changes", "log", "mcp", "risk-profile", "max-context", "skill",
		"recall", "search", "transcript", "verbose", "tools", "keys", "custom",
		// Mutating commands (unsafe)
		"setup", "settings", "compact", "sessions", "commit", "clear", "exit",
		"init", "shell", "exec", "edit", "review", "review-deep", "rollback",
		"rewind", "index", "fork",
		// Subagent commands
		"persona", "subagent-provider", "subagent-model",
		"subagent-persona", "subagent-personas",
		// Aliases that should resolve
		"m", "p", "x", "q", "?", "h", "stats", "c", "s", "i", "e", "r",
		"cl", "cp", "st", "rb", "rw", "ch", "cg",
	}

	registered := make(map[string]bool)
	aliases := make(map[string]bool)

	// Check all registered commands
	for _, cmd := range registry.ListCommands() {
		registered[cmd.Name()] = true
	}

	// Check aliases
	aliasesOf := map[string][]string{
		"model":       {"m"},
		"provider":     {"p"},
		"exit":        {"x", "q"},
		"help":        {"?", "h"},
		"usage":       {"stats"},
		"commit":      {"c"},
		"search":      {"s"},
		"index":       {"i"},
		"edit":        {"e"},
		"review":      {"r"},
		"clear":       {"cl", "new"},
		"compact":     {"cp"},
		"status":      {"st"},
		"rollback":    {"rb"},
		"rewind":      {"rw"},
		"changes":     {"ch"},
		"codegraph":   {"cg"},
		"sessions":    {"resume"},
		"keys":        {"key"},
	}
	for canonical, als := range aliasesOf {
		if !registered[canonical] {
			continue // skip if canonical command is missing
		}
		for _, a := range als {
			aliases[a] = true
		}
	}

	missing := []string{}
	for _, name := range expectedCommands {
		if registered[name] || aliases[name] {
			continue
		}
		missing = append(missing, name)
	}

	if len(missing) > 0 {
		t.Errorf("expected commands not found in registry: %v", missing)
	}
}

// TestSteerCapable_ConcurrentAccess verifies that the SteerCapable interface
// is safe to call concurrently.
func TestSteerCapable_ConcurrentAccess(t *testing.T) {
	registry := DefaultRegistry()
	commands := registry.ListCommands()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, cmd := range commands {
				if sc, ok := cmd.(SteerCapable); ok {
					_ = sc.SafeDuringSteer()
				}
			}
		}()
	}
	wg.Wait()
}
