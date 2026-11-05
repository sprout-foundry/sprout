//go:build !js

package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/injection"
)

// trustCmd is the top-level `sprout trust` command (SP-169, phasing step 2).
//
// Which content is third-party is a user decision, not something to infer: a
// cloned repository may be the user's own work, and mislabelling our own code
// as untrusted makes the model distrust its own output. So the answer is asked
// once, persisted per machine and per repository (keyed by absolute workspace
// root), and is inspectable and revocable here — a repository can change hands,
// and a dependency can be adopted and edited.
var trustCmd = &cobra.Command{
	Use:   "trust",
	Short: "Manage third-party content classifications",
	Long: `Manage which content roots in this repository are treated as third-party.

Sprout frames content it did not write as data rather than instructions, so a
vendored file or a fetched page that says "ignore the previous instructions"
is less likely to be followed. Known dependency trees (node_modules, vendor,
.venv, site-packages, .git, ...) are recognised without asking; anything else
is asked once and remembered here, per machine and per repository.

Examples:
  sprout trust list
  sprout trust set deps/acme third-party
  sprout trust set our-fork ours
  sprout trust forget deps/acme`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var trustListCmd = &cobra.Command{
	Use:   "list",
	Short: "List this repository's recorded trust decisions",
	Long:  `Show the paths classified as third-party or ours for the current repository.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := trustWorkspaceRoot()
		if err != nil {
			return err
		}
		return runTrustList(os.Stdout, root)
	},
}

var trustSetCmd = &cobra.Command{
	Use:   "set <path> third-party|ours",
	Short: "Record a trust decision for a path in this repository",
	Long: `Record whether <path> (relative to the repository root) is third-party
content — code you did not write — or ours.

The answer is persisted per machine and per repository, so the question is not
asked again there.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		class, err := parseTrustClass(args[1])
		if err != nil {
			return usageErrorf(cmd, "%v", err)
		}
		root, err := trustWorkspaceRoot()
		if err != nil {
			return err
		}
		mgr, err := configuration.NewManagerSilent()
		if err != nil {
			return fmt.Errorf("opening config: %w", err)
		}
		return runTrustSet(os.Stdout, mgr, root, args[0], class)
	},
}

var trustForgetCmd = &cobra.Command{
	Use:   "forget <path>",
	Short: "Remove a recorded trust decision",
	Long:  `Remove the recorded classification for <path> in this repository.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := trustWorkspaceRoot()
		if err != nil {
			return err
		}
		mgr, err := configuration.NewManagerSilent()
		if err != nil {
			return fmt.Errorf("opening config: %w", err)
		}
		return runTrustForget(os.Stdout, mgr, root, args[0])
	},
}

// trustWorkspaceRoot resolves the repository the decisions are recorded
// against. The store is keyed by the absolute workspace root, so this must be
// the repo directory itself — not the .sprout config directory that
// detectGitRepo reports (it returns the config dir so callers can point
// SPROUT_CONFIG at it). Taking that value verbatim would key every decision on
// a path that moves with the config layout.
//
// Falls back to the working directory outside a repo, matching how the rest of
// the CLI decides what "here" means.
func trustWorkspaceRoot() (string, error) {
	if autoDetectedWorkspaceDir != "" {
		// autoDetectedWorkspaceDir is <repo>/.sprout; the workspace root is
		// its parent. Guard the shape so an unexpected value cannot silently
		// become a key.
		if filepath.Base(autoDetectedWorkspaceDir) == ".sprout" {
			return filepath.Dir(autoDetectedWorkspaceDir), nil
		}
		return autoDetectedWorkspaceDir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolving working directory: %w", err)
	}
	if isolatedDir, found := detectGitRepo(cwd); found {
		return filepath.Dir(isolatedDir), nil
	}
	return cwd, nil
}

// parseTrustClass maps the CLI spelling to a TrustClass. Only the two decided
// states are accepted: "unknown" is what absence means, so it is not a value
// the user sets.
func parseTrustClass(s string) (injection.TrustClass, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "third-party", "third_party", "thirdparty":
		return injection.TrustThirdParty, nil
	case "ours", "first-party", "first_party", "firstparty":
		return injection.TrustFirstParty, nil
	default:
		return injection.TrustUnknown, fmt.Errorf("unknown classification %q: use \"third-party\" or \"ours\"", s)
	}
}

// runTrustList prints this repository's recorded decisions, sorted for stable
// output. An empty store prints "no decisions recorded" rather than nothing,
// so the command is never silently ambiguous.
func runTrustList(w io.Writer, workspaceRoot string) error {
	cfg, err := configuration.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	decisions := cfg.TrustedContentRoots[workspaceRoot]

	console.Heading(w, fmt.Sprintf("Trust decisions for %s", workspaceRoot))
	if len(decisions) == 0 {
		fmt.Fprintln(w, "no decisions recorded")
		return nil
	}
	paths := make([]string, 0, len(decisions))
	for p := range decisions {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		class := injection.TrustFirstParty
		if decisions[p] {
			class = injection.TrustThirdParty
		}
		fmt.Fprintf(w, "  %s  %s\n", p, class)
	}
	return nil
}

// runTrustSet records a decision for workspaceRoot and saves it through the
// config manager (the PersistShellCommandPattern precedent). Re-recording the
// same decision is a no-op that still saves, so the file's mtime updates.
func runTrustSet(w io.Writer, mgr *configuration.Manager, workspaceRoot, path string, class injection.TrustClass) error {
	if mgr == nil {
		return fmt.Errorf("no config manager — cannot persist trust decision")
	}
	if class != injection.TrustThirdParty && class != injection.TrustFirstParty {
		return fmt.Errorf("cannot record classification %v for %q", class, path)
	}
	rel := injection.NormalizeRelPath(path)
	thirdParty := class == injection.TrustThirdParty

	if err := mgr.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.TrustedContentRoots == nil {
			cfg.TrustedContentRoots = make(map[string]map[string]bool)
		}
		repo := cfg.TrustedContentRoots[workspaceRoot]
		if repo == nil {
			repo = make(map[string]bool)
			cfg.TrustedContentRoots[workspaceRoot] = repo
		}
		repo[rel] = thirdParty
		return nil
	}); err != nil {
		return fmt.Errorf("saving trust decision: %w", err)
	}

	// The recorded decision may be shadowed by a known marker, which outranks
	// it. Say so rather than letting the user believe the answer took effect.
	if class == injection.TrustFirstParty && injection.ClassifyPath(rel, map[string]bool{rel: false}) == injection.TrustThirdParty {
		console.GlyphWarning.Fprintf(w, "%s is inside a known dependency tree; it stays third-party regardless of this decision", rel)
	}
	console.GlyphSuccess.Fprintf(w, "%s = %s\n", rel, class)
	return nil
}

// runTrustForget removes one recorded decision for workspaceRoot. Forgetting a
// path that was never recorded is not an error — the postcondition the user
// asked for ("no decision here") already holds — but it is reported plainly so
// a typo is visible rather than silent.
func runTrustForget(w io.Writer, mgr *configuration.Manager, workspaceRoot, path string) error {
	if mgr == nil {
		return fmt.Errorf("no config manager — cannot persist trust decision")
	}
	rel := injection.NormalizeRelPath(path)
	removed := false

	if err := mgr.UpdateConfig(func(cfg *configuration.Config) error {
		repo := cfg.TrustedContentRoots[workspaceRoot]
		if repo == nil {
			return nil
		}
		if _, ok := repo[rel]; !ok {
			return nil
		}
		delete(repo, rel)
		removed = true
		if len(repo) == 0 {
			delete(cfg.TrustedContentRoots, workspaceRoot)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("saving trust decision: %w", err)
	}

	if !removed {
		fmt.Fprintf(w, "no decision recorded for %s\n", rel)
		return nil
	}
	console.GlyphSuccess.Fprintf(w, "forgot %s\n", rel)
	return nil
}

func init() {
	trustCmd.AddCommand(trustListCmd, trustSetCmd, trustForgetCmd)
}
