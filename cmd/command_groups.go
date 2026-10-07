//go:build !js

package cmd

import "github.com/spf13/cobra"

var commandGroups = []struct {
	group    cobra.Group
	commands []string
}{
	{cobra.Group{ID: "core", Title: "Core Commands:"}, []string{"new", "agent", "plan", "commit", "review", "pr", "shell"}},
	{cobra.Group{ID: "sessions", Title: "Sessions & History:"}, []string{"history", "search", "export", "export-training", "log"}},
	{cobra.Group{ID: "automation", Title: "Automation:"}, []string{"automate", "shell-bg"}},
	{cobra.Group{ID: "deploy", Title: "Deploy:"}, []string{"deploy"}},
	{cobra.Group{ID: "benchmark", Title: "Benchmarking:"}, []string{"benchmark"}},
	{cobra.Group{ID: "config", Title: "Configuration:"}, []string{"config", "keys", "custom", "mcp", "lsp", "skill", "policy", "service", "runner"}},
	{cobra.Group{ID: "diagnostics", Title: "Diagnostics & Maintenance:"}, []string{"diag", "explain", "health", "audit", "api", "version", "upgrade", "help", "completion"}},
}

// plumbingCommands are invoked by tooling (sprout-foundry's workspace
// runner, the E2E harness), not typed by people. They stay callable under
// their existing names but are left out of help.
var plumbingCommands = []string{"txn-pull", "txn-push", "txn-status", "sync", "serve"}

func applyCommandGroups(root *cobra.Command) {
	for _, g := range commandGroups {
		if root.ContainsGroup(g.group.ID) {
			continue
		}
		group := g.group
		root.AddGroup(&group)
	}
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	byName := map[string]*cobra.Command{}
	for _, c := range root.Commands() {
		byName[c.Name()] = c
	}
	for _, g := range commandGroups {
		for _, name := range g.commands {
			if c, ok := byName[name]; ok {
				c.GroupID = g.group.ID
			}
		}
	}
	for _, name := range plumbingCommands {
		if c, ok := byName[name]; ok {
			c.Hidden = true
		}
	}
}
