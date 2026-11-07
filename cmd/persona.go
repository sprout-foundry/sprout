//go:build !js

package cmd

// persona.go — `sprout persona` (SP-175).
//
// Core personas are immutable; a custom persona is a file the creator owns.
// This command checks those files without starting an agent, so a creator (or
// a repository's CI) can validate a persona before relying on it. With no
// path it validates the scopes that apply to the current directory: the user
// scope and, when there is a workspace, the project scope.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

var personaCmd = &cobra.Command{
	Use:   "persona",
	Short: "Inspect and validate custom personas",
	Long: `Inspect and validate custom personas.

Core personas ship with Sprout and cannot be changed. A custom persona is a
file you own: <config dir>/personas/*.json (user scope) or
<workspace>/.sprout/personas/*.json (project scope, which a repository
provides and you accept). Everything about a custom persona is yours to set —
its prompt, its tools, its skills, its MCP servers, where it may write.

Examples:
  sprout persona validate
  sprout persona validate ~/.config/sprout/personas/reviewer.json
  sprout persona dirs`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var personaValidateCmd = &cobra.Command{
	Use:   "validate [path]",
	Short: "Validate custom persona definitions",
	Long: `Validate custom persona definitions.

With no path, validates the user-scope persona directory and the current
workspace's project-scope directory. With a path, validates that file (or
every .json file in that directory).

A persona that fails to load is reported and skipped; the others are still
usable. Unknown tool names are warnings, not errors: a persona may name a host
tool the current host does not register.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return runPersonaValidatePath(os.Stdout, args[0])
		}
		return runPersonaValidateScopes(os.Stdout)
	},
}

var personaDirsCmd = &cobra.Command{
	Use:   "dirs",
	Short: "Show the persona directories in use",
	Long:  `Show where Sprout reads custom persona files from.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPersonaDirs(os.Stdout)
	},
}

func init() {
	personaCmd.AddCommand(personaValidateCmd, personaDirsCmd)
}

// runPersonaDirs prints the scopes and whether each exists.
func runPersonaDirs(w io.Writer) error {
	userDir, err := configuration.UserPersonasDir()
	if err != nil {
		return fmt.Errorf("resolve user persona directory: %w", err)
	}
	_, _ = fmt.Fprintf(w, "User scope:    %s\n", dirState(userDir))
	if workspaceDir, ok := currentWorkspaceConfigDir(); ok {
		_, _ = fmt.Fprintf(w, "Project scope: %s\n", dirState(configuration.ProjectPersonasDir(workspaceDir)))
	} else {
		_, _ = fmt.Fprintln(w, "Project scope: (no workspace here)")
	}
	return nil
}

func dirState(dir string) string {
	if _, err := os.Stat(dir); err != nil {
		return dir + " (does not exist)"
	}
	return dir
}

// runPersonaValidateScopes validates the user and project scopes.
func runPersonaValidateScopes(w io.Writer) error {
	core, err := personas.DefaultDefinitions()
	if err != nil {
		return fmt.Errorf("load core persona catalog: %w", err)
	}

	userDir, err := configuration.UserPersonasDir()
	if err != nil {
		return fmt.Errorf("resolve user persona directory: %w", err)
	}
	projectDir := ""
	if dir, ok := currentWorkspaceConfigDir(); ok {
		projectDir = configuration.ProjectPersonasDir(dir)
	}

	// Acceptance does not gate validation: the question here is whether the
	// definitions are well formed, not whether they are trusted yet. A
	// project persona that is merely unaccepted is reported as such, so CI
	// can tell "no personas" from "personas waiting for acceptance".
	result := personas.LoadCustomPersonas(core, personas.LoadOptions{
		UserDir:    userDir,
		ProjectDir: projectDir,
	})

	return reportPersonaLoad(w, result)
}

// runPersonaValidatePath validates one file or every .json file in a
// directory.
func runPersonaValidatePath(w io.Writer, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if !info.IsDir() {
		return reportPersonaLoad(w, personas.LoadCustomPersonasFromFile(path))
	}
	return reportPersonaLoad(w, personas.LoadCustomPersonasFromDir(path))
}

// reportPersonaLoad prints the findings and returns an error when any
// definition failed to load, so the command's exit code is usable in CI.
func reportPersonaLoad(w io.Writer, result personas.LoadResult) error {
	for _, problem := range result.Problems {
		if problem.Kind == personas.ProblemError {
			console.GlyphError.Fprintln(w, problem.String())
			continue
		}
		console.GlyphWarning.Fprintln(w, problem.String())
	}

	for _, pending := range result.PendingProject {
		console.GlyphInfo.Fprintf(w, "Not accepted: %s (%s) — run `/persona accept %s`",
			pending.ID, pending.File, pending.ID)
	}

	ids := make([]string, 0, len(result.Personas))
	for _, persona := range result.Personas {
		ids = append(ids, persona.ID)
	}
	if len(ids) > 0 {
		console.GlyphSuccess.Fprintf(w, "Valid: %s", strings.Join(ids, ", "))
	}

	failed := 0
	for _, problem := range result.Problems {
		if problem.Kind == personas.ProblemError {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d persona definition(s) failed to load", failed)
	}
	if len(ids) == 0 && len(result.Problems) == 0 && len(result.PendingProject) == 0 {
		console.GlyphInfo.Fprintln(w, "No custom personas found.")
	}
	return nil
}

// currentWorkspaceConfigDir returns `<cwd>/.sprout` when it exists, so the
// project scope matches what a workspace-scoped manager would load.
func currentWorkspaceConfigDir() (string, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	dir := filepath.Join(cwd, configuration.ConfigDirName)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", false
	}
	return dir, true
}
