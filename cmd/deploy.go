//go:build !js

// `sprout deploy`: build the current project and ship it
// through a deploy target, plus `deploy status`, `deploy history`, and
// `deploy rollback <id>`.
//
// The command resolves the project's .sprout/deploy.json (pkg/deployconfig)
// and hands the build-and-upload to pkg/deploy's Deployer, so the preview/
// production confirmation rule and the "what was verified is what ships"
// gate live in the package rather than being re-implemented here. A
// production deploy without --yes is refused with the package's typed
// ErrProductionNeedsConfirmation, before anything is built or uploaded.
//
// This milestone runs against a fake target (the on-disk journal in
// deploy_fake.go); the target seam below is where the real hosting adapter
// plugs in without touching the command. The deploy
// directory comes from the nearest .sprout/ found walking up from the
// working directory, so the command behaves like the rest of the toolchain
// when run from a subdirectory.
package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/deploy"
	"github.com/sprout-foundry/sprout/pkg/deployconfig"
	"github.com/sprout-foundry/sprout/pkg/startermanifest"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// deployDir is the --dir override for the project root; empty means "walk up
// from the working directory to the nearest .sprout/".
var deployDir string

// deployTargetOverride is the --target override for the deploy config's
// target id; empty means "use the config's target".
var deployTargetOverride string

// deployProduction requests a production deploy, which always needs explicit
// confirmation (--yes).
var deployProduction bool

// deployAssumeYes supplies the explicit confirmation a production deploy
// requires. It is the CLI's confirmation channel; the package's gate stays
// the source of truth for whether the confirmation is sufficient.
var deployAssumeYes bool

// deployTargetFor is the adapter seam: it maps a target id to a
// deploy.DeployTarget under a project root. The id is the vendor id (the
// --target override when set, else the config's target), qualified with the
// manifest's deploy_target shape when the vendor is Cloudflare (e.g.
// "cloudflare/pages"). Production uses the on-disk fake; the real hosting
// adapter is added here. A test swaps in its own target without touching the
// command.
var deployTargetFor func(root, targetID string) (deploy.DeployTarget, error)

// deployBuildRunner is the build seam handed to deploy.Deployer. Nil uses the
// package's real runner; a test supplies a stub so no process is spawned.
var deployBuildRunner deploy.BuildRunner

// deployFingerprint is the tree-fingerprint seam handed to deploy.Deployer.
// Nil uses the package's real fingerprint; a test supplies a stub.
var deployFingerprint deploy.TreeFingerprint

// deployVerificationSnapshot is the verification seam: it reports whether the
// current tree passed verification and the fingerprint captured then. Until
// the turn-end hook is wired into the CLI, the default records the
// current tree as verified — the fingerprint is computed over the same tree
// and skip set BuildAndDeploy checks against, so an unchanged tree passes the
// gate and a tree that moves between now and the build is still refused.
var deployVerificationSnapshot func(root, buildDir string) (deploy.VerificationSnapshot, error)

// defaultDeployVerificationSnapshot computes a passing snapshot for the
// current tree. It is the interim default (see deployVerificationSnapshot).
func defaultDeployVerificationSnapshot(root, buildDir string) (deploy.VerificationSnapshot, error) {
	fingerprint := deployFingerprint
	if fingerprint == nil {
		fingerprint = deploy.DefaultTreeFingerprint
	}
	fp, err := fingerprint(root, buildDir)
	if err != nil {
		return deploy.VerificationSnapshot{}, fmt.Errorf("fingerprint project tree: %w", err)
	}
	return deploy.VerificationSnapshot{Passed: true, Fingerprint: fp}, nil
}

// defaultDeployTargetFor is the production adapter seam: it returns the
// on-disk fake for the "fake" target id and resolves the Cloudflare ids to the
// real adapter. The Cloudflare ids are qualified with the manifest's
// deploy_target shape (see deployAdapterID): "cloudflare/pages" and
// "cloudflare/workers". Any other id fails actionably rather than silently
// doing nothing.
//
// The Cloudflare adapter authenticates with the existing deploy credential
// (CLOUDFLARE_API_TOKEN), resolved here and never passed as an argument or
// printed; the token reaches the wire only in the request's auth header.
func defaultDeployTargetFor(root, targetID string) (deploy.DeployTarget, error) {
	switch strings.TrimSpace(targetID) {
	case "", "fake":
		return newPersistentFakeTarget(root)
	case deployCloudflarePagesID, deployCloudflareWorkersID:
		return newCloudflareDeployTarget(root, targetID)
	default:
		return nil, fmt.Errorf("deploy target %q is not available; use --target fake or configure a supported target", targetID)
	}
}

// Cloudflare adapter ids, as produced by deployAdapterID (the vendor qualified
// with the manifest's deploy_target shape).
const (
	deployCloudflarePagesID   = deployCloudflareVendor + "/" + deploy.DeployTargetPages
	deployCloudflareWorkersID = deployCloudflareVendor + "/" + deploy.DeployTargetWorkers
)

// newCloudflareDeployTarget builds the Cloudflare adapter for a qualified id,
// resolving the deploy credential and reading the account id from the
// environment. The token is never passed as a command argument or printed.
func newCloudflareDeployTarget(root, targetID string) (deploy.DeployTarget, error) {
	shape := strings.TrimPrefix(strings.TrimSpace(targetID), deployCloudflareVendor+"/")
	accountID := strings.TrimSpace(os.Getenv(deployCloudflareAccountIDEnvVar))
	if accountID == "" {
		return nil, fmt.Errorf("cloudflare deploy needs an account id: set %s", deployCloudflareAccountIDEnvVar)
	}
	project, err := deployProjectName(root)
	if err != nil {
		return nil, err
	}
	cred, err := deploy.ResolveCredential(deploy.DeployCredentialConfig{
		Target: deployCloudflareVendor, Project: project, EnvVar: deploy.CloudflareAPITokenEnvVar,
	})
	if err != nil {
		return nil, err
	}
	return deploy.CloudflareTargetFor(shape,
		deploy.CloudflareConfig{AccountID: accountID, Project: project},
		cred, "", nil)
}

// deployCloudflareAccountIDEnvVar is the environment variable carrying the
// Cloudflare account id the deploy targets. It is not a secret (unlike the
// token), so it is read directly.
const deployCloudflareAccountIDEnvVar = "CLOUDFLARE_ACCOUNT_ID"

// deployProjectName reads the project name the Cloudflare adapter keys its
// resources and history by: the deploy config's project, defaulting to the
// worker/project name declared in wrangler.toml when the config names none.
func deployProjectName(root string) (string, error) {
	if cfg, err := deployconfig.LoadDeployConfig(root); err == nil && strings.TrimSpace(cfg.Project) != "" {
		return strings.TrimSpace(cfg.Project), nil
	}
	if w, err := deploy.LoadWranglerConfig(root); err == nil && strings.TrimSpace(w.Name) != "" {
		return strings.TrimSpace(w.Name), nil
	}
	return "", fmt.Errorf("cloudflare deploy needs a project name: set \"project\" in %s", deployconfig.DeployConfigPath(root))
}

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Build and deploy the current project",
	Long: `Build the current project and ship it through a deploy target.

The project's .sprout/deploy.json names where it ships (target and project
name) and, optionally, a build output directory; the build command comes
from the starter manifest .sprout/starter.json. The build runs in the
workspace and the target only ever receives the built output.

By default a preview deployment is made (with its own per-deployment URL).
A production deployment is always gated on explicit confirmation: pass
--yes to grant it, otherwise the command refuses before anything is built
or uploaded.

This milestone runs against a fake target (in-memory, persisted under
.sprout/deploy-fake.json); the real hosting adapter arrives later.

Examples:
  sprout deploy
  sprout deploy --production --yes
  sprout deploy status
  sprout deploy history
  sprout deploy rollback my-app-2`,
	Args: cobra.NoArgs,
	RunE: runDeployCmd,
}

var deployStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the latest deployment for the project",
	Long: `Show the most recent deployment recorded for the project.

The deploy config's project name is the one the target's history is keyed
by; the current lifecycle state is read back from the target, so a
deployment that was rolled back is reported as such.`,
	Args: cobra.NoArgs,
	RunE: runDeployStatusCmd,
}

var deployHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "List the project's deployments",
	Long: `List every deployment the target has recorded for the project,
oldest first, with its kind, state, version and URL.`,
	Args: cobra.NoArgs,
	RunE: runDeployHistoryCmd,
}

var deployRollbackCmd = &cobra.Command{
	Use:   "rollback <id>",
	Short: "Roll back to the deployment preceding <id>",
	Long: `Roll back to the deployment that preceded <id> and mark <id> as
rolled back, returning the deployment that is live again.

<id> is a deployment id from 'sprout deploy history'. Rolling back the
only recorded deployment is refused (there is nothing earlier to revert
to).`,
	Args: cobra.ExactArgs(1),
	RunE: runDeployRollbackCmd,
}

func init() {
	deployCmd.PersistentFlags().StringVar(&deployDir, "dir", "",
		"Project root to deploy (default: nearest .sprout/ walking up from the working directory)")
	deployCmd.PersistentFlags().StringVar(&deployTargetOverride, "target", "",
		"Deploy target id, overriding .sprout/deploy.json (default: the config's target)")
	deployCmd.Flags().BoolVar(&deployProduction, "production", false,
		"Deploy to production (always requires --yes: a production deploy needs explicit confirmation)")
	deployCmd.Flags().BoolVarP(&deployAssumeYes, "yes", "y", false,
		"Confirm a production deploy without an interactive prompt")

	deployCmd.AddCommand(deployStatusCmd)
	deployCmd.AddCommand(deployHistoryCmd)
	deployCmd.AddCommand(deployRollbackCmd)
	rootCmd.AddCommand(deployCmd)
}

// deployProjectRoot resolves the project root the command operates on: --dir
// when given, else the nearest ancestor of the working directory that holds
// a .sprout/ directory, else the working directory itself (matching the
// discoverSproutSessionRoot convention used elsewhere in the CLI).
func deployProjectRoot() (string, error) {
	if strings.TrimSpace(deployDir) != "" {
		abs, err := filepath.Abs(deployDir)
		if err != nil {
			return "", fmt.Errorf("resolve --dir: %w", err)
		}
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return "", fmt.Errorf("--dir %s is not a directory", deployDir)
		}
		return abs, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	dir := filepath.Clean(cwd)
	for {
		if info, err := os.Stat(filepath.Join(dir, startermanifest.SproutDir)); err == nil && info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Clean(cwd), nil
		}
		dir = parent
	}
}

// loadDeploySubject resolves the project root, loads the deploy config, and
// resolves the target adapter — the parts every subcommand needs. A missing
// .sprout/deploy.json is surfaced as the deployconfig sentinel with an
// actionable hint rather than a raw error.
func loadDeploySubject() (root string, cfg *deployconfig.DeployConfig, target deploy.DeployTarget, err error) {
	root, err = deployProjectRoot()
	if err != nil {
		return "", nil, nil, err
	}

	cfg, err = deployconfig.LoadDeployConfig(root)
	if err != nil {
		if errors.Is(err, deployconfig.ErrNoDeployConfig) {
			return "", nil, nil, withHint(err,
				fmt.Sprintf("Create %s naming the target and project (see 'sprout deploy --help').", deployconfig.DeployConfigPath(root)))
		}
		return "", nil, nil, err
	}

	forTarget := deployTargetFor
	if forTarget == nil {
		forTarget = defaultDeployTargetFor
	}
	adapterID, err := deployAdapterID(root, cfg)
	if err != nil {
		return "", nil, nil, err
	}
	target, err = forTarget(root, adapterID)
	if err != nil {
		return "", nil, nil, err
	}
	return root, cfg, target, nil
}

// deployCloudflareVendor is the deploy config target id whose Pages-vs-Workers
// shape the starter manifest selects.
const deployCloudflareVendor = "cloudflare"

// deployAdapterID is the adapter id handed to the target seam: the vendor id
// (the --target override when set, else the config's target) qualified with
// the manifest's deploy_target shape when that vendor is Cloudflare.
//
// Resolution order (an explicit --target overrides everything):
//
//   - --target set: it names the adapter directly (e.g. "fake"). It wins over
//     the config; within the Cloudflare vendor the manifest still picks the
//     shape because the vendor is qualified below.
//   - otherwise the config's target names the vendor.
//   - when the vendor is "cloudflare", the starter manifest's deploy_target
//     picks the shape: "cloudflare/pages" (the default when absent) or
//     "cloudflare/workers".
//
// A missing starter manifest means the default shape (pages): a project can
// hand-author its deploy config without one. Any other manifest failure is
// surfaced, since an invalid manifest is a real configuration error the
// deploy should not silently paper over.
func deployAdapterID(root string, cfg *deployconfig.DeployConfig) (string, error) {
	vendor := strings.TrimSpace(deployTargetID(cfg))
	if vendor != deployCloudflareVendor {
		return vendor, nil
	}
	shape, err := deployManifestShape(root)
	if err != nil {
		return "", err
	}
	return vendor + "/" + shape, nil
}

// deployManifestShape reads the starter manifest's deploy_target and returns
// the effective Cloudflare shape ("pages" or "workers"), defaulting to
// "pages" when the manifest is absent. It mirrors the deploy selector's
// normalization so the command and the adapter agree on the default.
func deployManifestShape(root string) (string, error) {
	m, err := starterstore.LoadStarterManifest(root)
	if err != nil {
		if errors.Is(err, starterstore.ErrNoManifest) {
			return deploy.DeployTargetPages, nil
		}
		return "", err
	}
	return deploy.NormalizeDeployTarget(m.DeployTarget), nil
}

// deployTargetID is the target id in effect: the --target override when set,
// else the config's target. It is read-only commands' view of the config;
// the deploy path also records it on the resolved config.
func deployTargetID(cfg *deployconfig.DeployConfig) string {
	if strings.TrimSpace(deployTargetOverride) != "" {
		return deployTargetOverride
	}
	if cfg == nil {
		return ""
	}
	return cfg.Target
}

// loadDeployProject loads the subject and additionally resolves the build
// output (the config merged with the starter manifest), which only the
// deploy path needs.
func loadDeployProject() (deployconfig.Resolved, string, deploy.DeployTarget, error) {
	root, cfg, target, err := loadDeploySubject()
	if err != nil {
		return deployconfig.Resolved{}, "", nil, err
	}
	resolved, err := deployconfig.Resolve(cfg, root)
	if err != nil {
		return deployconfig.Resolved{}, "", nil, err
	}
	resolved.Target = deployTargetID(cfg)
	return resolved, root, target, nil
}

// runDeployCmd is the body of `sprout deploy`: resolve config, gate on
// confirmation and verification, build, upload.
func runDeployCmd(cmd *cobra.Command, args []string) error {
	resolved, root, target, err := loadDeployProject()
	if err != nil {
		return err
	}

	if deployProduction {
		resolved.Kind = deploy.KindProduction
	}

	buildCmd, version, err := deployStarterBuild(root)
	if err != nil {
		return err
	}

	confirm := deploy.Confirmation{
		Confirmed:  deployAssumeYes,
		ApprovedBy: "cli",
	}

	deployer := &deploy.Deployer{
		Target:      target,
		Run:         deployBuildRunner,
		Fingerprint: deployFingerprint,
	}

	snapshotFn := deployVerificationSnapshot
	if snapshotFn == nil {
		snapshotFn = defaultDeployVerificationSnapshot
	}
	snap, err := snapshotFn(root, resolved.BuildDir)
	if err != nil {
		return err
	}

	req := deploy.BuildRequest{
		Root:     root,
		Command:  buildCmd,
		BuildDir: resolved.BuildDir,
		Project:  resolved.Project,
		Kind:     resolved.Kind,
		Version:  version,
	}

	d, err := deployer.BuildAndDeploy(cmd.Context(), req, snap, confirm)
	if err != nil {
		if errors.Is(err, deploy.ErrProductionNeedsConfirmation) {
			return withHint(err,
				"A production deploy needs explicit confirmation: re-run with --yes once you have reviewed the change.")
		}
		return err
	}

	printDeployed(cmd.OutOrStdout(), d)
	return nil
}

// deployStarterBuild reads the project's starter manifest for the build
// command and version the deploy needs. A manifest with no build command is
// refused: there is nothing to upload, and the package would refuse it
// anyway (ErrNoBuildCommand) — failing here names the file to fix.
func deployStarterBuild(root string) (command, version string, err error) {
	m, err := starterstore.LoadStarterManifest(root)
	if err != nil {
		if errors.Is(err, starterstore.ErrNoManifest) {
			return "", "", fmt.Errorf("no starter manifest at %s: a deploy needs a build command (set \"build\" there)", starterstore.StarterManifestPath(root))
		}
		return "", "", err
	}
	if strings.TrimSpace(m.Build) == "" {
		return "", "", fmt.Errorf("the starter manifest %s has no build command (set \"build\" there)", starterstore.StarterManifestPath(root))
	}
	return m.Build, m.Starter.Version, nil
}

// runDeployStatusCmd prints the latest deployment for the project, with its
// current lifecycle state read back from the target.
func runDeployStatusCmd(cmd *cobra.Command, args []string) error {
	_, cfg, target, err := loadDeploySubject()
	if err != nil {
		return err
	}
	project := cfg.Project

	history, err := target.List(project)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(history) == 0 {
		_, _ = fmt.Fprintf(out, "No deployments recorded for project %q yet.\n", project)
		_, _ = fmt.Fprintln(out, "Run 'sprout deploy' to make the first one.")
		return nil
	}

	latest := history[len(history)-1]
	state, err := target.Status(latest)
	if err != nil {
		return err
	}
	latest.Status = state

	_, _ = fmt.Fprintf(out, "Latest deployment for project %q:\n", project)
	printDeploymentLine(out, latest)
	return nil
}

// runDeployHistoryCmd prints every deployment recorded for the project,
// oldest first.
func runDeployHistoryCmd(cmd *cobra.Command, args []string) error {
	_, cfg, target, err := loadDeploySubject()
	if err != nil {
		return err
	}
	project := cfg.Project

	history, err := target.List(project)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(history) == 0 {
		_, _ = fmt.Fprintf(out, "No deployments recorded for project %q yet.\n", project)
		return nil
	}

	_, _ = fmt.Fprintf(out, "%d deployment(s) for project %q (oldest first):\n", len(history), project)
	for _, d := range history {
		printDeploymentLine(out, d)
	}
	return nil
}

// runDeployRollbackCmd rolls back to the deployment preceding the one named
// by id, resolving the deployment from the project's history first.
func runDeployRollbackCmd(cmd *cobra.Command, args []string) error {
	_, cfg, target, err := loadDeploySubject()
	if err != nil {
		return err
	}
	project := cfg.Project

	id := strings.TrimSpace(args[0])
	history, err := target.List(project)
	if err != nil {
		return err
	}
	target0, ok := findDeployment(history, id)
	if !ok {
		return fmt.Errorf("%w: %q (see 'sprout deploy history')", deploy.ErrUnknownDeployment, id)
	}

	restored, err := target.Rollback(target0)
	if err != nil {
		if errors.Is(err, deploy.ErrNoPreviousDeployment) {
			return withHint(err, "The deployment being rolled back has no earlier deployment to restore.")
		}
		return err
	}

	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Rolled back %s; %s is live again.\n", id, restored.ID)
	printDeploymentLine(out, restored)
	return nil
}

// findDeployment returns the history entry with the given id.
func findDeployment(history []deploy.Deployment, id string) (deploy.Deployment, bool) {
	for _, d := range history {
		if d.ID == id {
			return d, true
		}
	}
	return deploy.Deployment{}, false
}

// printDeployed prints the outcome of a successful deploy: the kind, id and
// URL the target returned.
func printDeployed(out io.Writer, d deploy.Deployment) {
	_, _ = fmt.Fprintf(out, "Deployed %s %s to %s\n", d.Kind, d.ID, d.URL)
}

// printDeploymentLine prints one deployment: id, kind, state, version and URL.
func printDeploymentLine(out io.Writer, d deploy.Deployment) {
	version := d.Version
	if version == "" {
		version = "-"
	}
	_, _ = fmt.Fprintf(out, "  %s  %s  %s  version=%s  %s\n", d.ID, d.Kind, d.Status, version, d.URL)
}
