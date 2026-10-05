//go:build !js

// `sprout new` instantiates an embedded starter into a new project
// directory (SP-153 §153b, TODO 153.4). It is the CLI half of
// "sprout new --starter <id>"; the web UI new-project dialog lands in
// 153.7.
//
// The heavy lifting — resolving an embedded starter tree, copying it, and
// writing the project's .sprout/starter.json — lives in
// pkg/starters.Instantiate (TODO 153.3); this file only wires the flags and
// keeps the body in a small, testable function.
package cmd

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/starters"
)

// newStarterID is the --starter flag value for `sprout new`.
var newStarterID string

var newCmd = &cobra.Command{
	Use:   "new [dir]",
	Short: "Create a new project from a starter",
	Long: `Create a new project by instantiating an embedded starter (SP-153).

A starter is a versioned project tree that gives a new project a working
skeleton plus the .sprout/starter.json manifest that later tools read for
their build, test, dev and preview commands.

When no directory is given, one is created named after the starter id.

Examples:
  # Create ./fixture from the embedded 'fixture' starter
  sprout new --starter fixture

  # Create a directory of your own name
  sprout new --starter fixture my-app`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := ""
		if len(args) > 0 {
			dir = args[0]
		}
		return runNewProject(newStarterID, dir, cmd.OutOrStdout())
	},
}

// runNewProject is the body of `sprout new`: it instantiates the starter
// named by starterID into dir and prints a short confirmation to out.
//
//   - dir empty: the destination defaults to a directory named after the
//     starter id (e.g. a relative "fixture").
//   - starterID empty: the list of available starters is printed to out and
//     an error is returned — this command cannot create a project without a
//     starter (153.4).
//   - unknown starter id (starters.ErrUnknownStarter): the list of
//     available starters is printed to out and an error is returned, so the
//     caller can see what it can try.
//   - a non-empty destination is refused by pkg/starters (nothing is
//     written) and that error (starters.ErrNonEmptyDestination) is surfaced
//     here.
//
// The function is cobra-free — no flags, no command — so tests drive it
// directly with a buffer instead of the full cobra runtime.
func runNewProject(starterID, dir string, out io.Writer) error {
	if starterID == "" {
		if err := printAvailableStarters(out); err != nil {
			return err
		}
		return errors.New("a starter is required: pass --starter <id>")
	}

	if dir == "" {
		dir = starterID
	}

	if err := starters.Instantiate(starterID, dir); err != nil {
		if errors.Is(err, starters.ErrUnknownStarter) {
			// Show what is actually available, then surface the failure.
			if lerr := printAvailableStarters(out); lerr != nil {
				return lerr
			}
			return fmt.Errorf("unknown starter %q", starterID)
		}
		return err
	}

	version, err := starters.Version(starterID)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Created %s from starter %s (v%s)\n", dir, starterID, version)
	return err
}

// printAvailableStarters writes the user-facing starter catalogue (id and
// version, one per line, sorted by id) to out. It is the "here is what you
// can pick" help shown on both the no-starter and unknown-starter paths.
// Test-only trees (ListForUsers, e.g. the fixture) are withheld, since their
// commands cannot run in a real project.
func printAvailableStarters(out io.Writer) error {
	list, err := starters.ListForUsers()
	if err != nil {
		return fmt.Errorf("list available starters: %w", err)
	}
	if _, err := fmt.Fprintln(out, "Available starters:"); err != nil {
		return err
	}
	for _, s := range list {
		if _, err := fmt.Fprintf(out, "  %s (v%s)\n", s.ID, s.Version); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	newCmd.Flags().StringVar(&newStarterID, "starter", "", "Starter id to instantiate (run `sprout new` without it to list the available starters)")
	rootCmd.AddCommand(newCmd)
}
