package components

import (
	"context"
	"testing"

	"github.com/alantheprice/ledit/pkg/console"
)

func TestAgentConsoleInitClearsScrollbackBeforeScreen(t *testing.T) {
	mockTerminal := NewMockTerminal(80, 24)
	cfg := DefaultAgentConsoleConfig()
	ac := NewAgentConsole(nil, cfg)

	eventBus := console.NewEventBus(32)
	if err := eventBus.Start(); err != nil {
		t.Fatalf("failed to start event bus: %v", err)
	}
	t.Cleanup(func() {
		_ = eventBus.Stop()
	})

	deps := console.Dependencies{
		Terminal: mockTerminal,
		Layout:   ac.autoLayoutManager,
		Events:   eventBus,
		State:    console.NewStateManager(),
	}

	ctx := context.Background()
	if err := ac.Init(ctx, deps); err != nil {
		t.Fatalf("agent console init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = ac.Cleanup()
	})

	commands := mockTerminal.commands
	if !containsCommand(commands, "ClearScrollback()") {
		t.Fatalf("expected ClearScrollback() to be invoked during setup, got commands: %v", commands)
	}

	scrollIdx := commandIndexOf(commands, "ClearScrollback()")
	screenIdx := commandIndexOf(commands, "ClearScreen()")
	if screenIdx == -1 {
		t.Fatalf("expected ClearScreen() to be invoked during setup, got commands: %v", commands)
	}
	if scrollIdx > screenIdx {
		t.Fatalf("expected ClearScrollback() before ClearScreen(), got sequence: %v", commands)
	}
}

func containsCommand(commands []string, needle string) bool {
	return commandIndexOf(commands, needle) != -1
}

func commandIndexOf(commands []string, needle string) int {
	for i, cmd := range commands {
		if cmd == needle {
			return i
		}
	}
	return -1
}
