//go:build !js

package cmd

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// runAutomateList prints all discovered workflows.
func runAutomateList() error {
	dir := getAutomateDir()
	workflows, err := automate.Discover(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			console.GlyphWarning.Printf("No automate/ directory found at %s/", dir)
			return nil
		}
		return fmt.Errorf("failed to scan %s: %w", dir, err)
	}

	if len(workflows) == 0 {
		console.GlyphInfo.Printf("No workflows found in %s/", dir)
		return nil
	}

	fmt.Println()
	for _, wf := range workflows {
		desc := wf.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Printf("  %-30s %s\n", wf.Filename, desc)
	}
	fmt.Println()
	return nil
}

// listAvailableWorkflows shows available workflow names for the user.
func listAvailableWorkflows(dir string) error {
	fmt.Println("Available workflows:")
	workflows, err := automate.Discover(dir)
	if err != nil {
		return nil
	}
	for _, wf := range workflows {
		fmt.Printf("  %s\n", wf.Filename)
	}
	return nil
}
