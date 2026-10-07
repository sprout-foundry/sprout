package tools

// deploy_status and deploy: the agent-facing face of deploys.
//
// deploy_status is read-only: it reports the latest deployment the project's
// target has recorded for it. deploy ships the current project through the
// same deploy.Deployer the CLI uses (cmd/deploy.go), so the preview/
// production confirmation rule and the "what was verified is what ships"
// gate are enforced in pkg/deploy rather than re-implemented here.
//
// The tool adds two rules on top of the package:
//
//   - Verification. When verification is enabled for the session,
//     deploy refuses unless the agent's latest verification result passed.
//     The result is read through the ToolFuncSet seam wired by pkg/agent; a
//     session with verification enabled but no wired result fails closed —
//     the tool never guesses "passed".
//   - Production confirmation. A production deploy always requires explicit
//     user confirmation, and the agent cannot mint one: the tool routes
//     through the interactive approval gate (ToolEnv.ApprovalManager) and
//     refuses when no approval surface exists. There is deliberately no
//     "confirm" argument — a model-supplied boolean would be self-
//     confirmation.
//
// Deploy outcomes (success and refusal) publish a progress event on
// the shared event bus, so a consumer sees the ship step in the run's
// progress stream.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/deploy"
	"github.com/sprout-foundry/sprout/pkg/deployconfig"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/history"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// deployTargetFor resolves a deploy target id to a deploy.DeployTarget under
// a project root. The id is the vendor id (the tool's target argument when
// set, else the config's target), qualified with the manifest's deploy_target
// shape when the vendor is Cloudflare (e.g. "cloudflare/pages"). It is the
// adapter seam, mirroring cmd/deploy.go's deployTargetFor: the real hosting
// adapter plugs in here later, and a test swaps in its own target without
// constructing an agent. Guarded by ToolFuncMu like the other package-level
// seams.
var deployTargetFor func(root, targetID string) (deploy.DeployTarget, error)

// deployBuildRunner is the build seam handed to deploy.Deployer. Nil uses the
// package's real runner; a test supplies a stub so no process is spawned.
var deployBuildRunner deploy.BuildRunner

// deployTreeFingerprint is the tree-fingerprint seam handed to
// deploy.Deployer. Nil uses the package's real fingerprint; a test supplies a
// stub.
var deployTreeFingerprint deploy.TreeFingerprint

// deployFakeTargets caches the interim in-process fake target per
// (root, target id), so a deploy made by the deploy tool is visible to a
// later deploy_status call in the same session. The real adapter replaces the
// resolver entirely; this cache only serves the default.
var deployFakeTargets = struct {
	mu    sync.Mutex
	byKey map[string]*deploy.FakeTarget
}{byKey: map[string]*deploy.FakeTarget{}}

// defaultDeployTargetFor is the interim adapter for the "fake" target id (and
// an empty id): an in-process deploy.FakeTarget, cached per root so history
// survives between tool calls. Every other id — including the
// manifest-qualified "cloudflare/pages" and "cloudflare/workers" the real
// adapter will resolve in a later milestone — fails actionably rather than
// silently doing nothing, mirroring the CLI's default.
func defaultDeployTargetFor(root, targetID string) (deploy.DeployTarget, error) {
	switch strings.TrimSpace(targetID) {
	case "", "fake":
		key := root + "\x00" + strings.TrimSpace(targetID)
		deployFakeTargets.mu.Lock()
		defer deployFakeTargets.mu.Unlock()
		t, ok := deployFakeTargets.byKey[key]
		if !ok {
			t = deploy.NewFake()
			deployFakeTargets.byKey[key] = t
		}
		return t, nil
	default:
		return nil, fmt.Errorf("deploy target %q is not available in this session; use target \"fake\" (the real hosting adapter lands in a later milestone)", targetID)
	}
}

// resolveDeployTargetFor returns the resolver to use, preferring the test/
// adapter seam over the interim default.
func resolveDeployTargetFor() func(root, targetID string) (deploy.DeployTarget, error) {
	ToolFuncMu.RLock()
	forTarget := deployTargetFor
	ToolFuncMu.RUnlock()
	if forTarget == nil {
		return defaultDeployTargetFor
	}
	return forTarget
}

// resolveDeployRunnerAndFingerprint returns the build runner and tree
// fingerprint, falling back to the pkg/deploy defaults.
func resolveDeployRunnerAndFingerprint() (deploy.BuildRunner, deploy.TreeFingerprint) {
	ToolFuncMu.RLock()
	run := deployBuildRunner
	fingerprint := deployTreeFingerprint
	ToolFuncMu.RUnlock()
	if run == nil {
		run = deploy.DefaultBuildRunner
	}
	if fingerprint == nil {
		fingerprint = deploy.DefaultTreeFingerprint
	}
	return run, fingerprint
}

// verificationEnabled reports whether the verification run is enabled
// for this session. A nil ConfigManager (standalone tool runs, tests) reads
// as disabled, matching the CLI default.
func verificationEnabled(env ToolEnv) bool {
	if env.ConfigManager == nil {
		return false
	}
	cfg := env.ConfigManager.GetConfig()
	return cfg.VerificationEnabled()
}

// deployVerificationGate decides whether a deploy may proceed past the
// verification gate, and returns the snapshot deploy.BuildAndDeploy checks.
//
//   - When verification is disabled for the session (the default), there is
//     no result to gate on — verification is off — so the gate is
//     open, mirroring the CLI's default snapshot: the snapshot records the
//     current tree's fingerprint and passed=true, and BuildAndDeploy's own
//     fingerprint check still guards the build window. The passing flag is
//     asserted here precisely because verification was not run; enabling
//     verification is what turns this into an observed pass.
//   - When verification is enabled, the agent's latest result is required.
//     The result is read through the ToolFuncSet seam; a session with no
//     wired seam, no result, or a failed result is refused with a plain
//     error (fail closed).
//
// The fingerprint is computed now, over the same tree and with the same skip
// set BuildAndDeploy re-checks against. It therefore guards the verify→build
// window; the passing result itself is the gate for the earlier window.
func deployVerificationGate(env ToolEnv, root, buildDir string) (deploy.VerificationSnapshot, error) {
	_, fingerprint := resolveDeployRunnerAndFingerprint()

	if verificationEnabled(env) {
		verifyFn := env.ResolveToolFuncs().DeployVerification
		if verifyFn == nil {
			return deploy.VerificationSnapshot{}, errors.New("verification is enabled but no verification result is available to the deploy tool; deploy refused")
		}
		passed, haveResult := verifyFn()
		if !haveResult {
			return deploy.VerificationSnapshot{}, errors.New("verification has not run for the current work; deploy refused until it passes")
		}
		if !passed {
			return deploy.VerificationSnapshot{}, errors.New("the latest verification did not pass; fix the failing checks and re-verify before deploying")
		}
	}

	fp, err := fingerprint(root, buildDir)
	if err != nil {
		return deploy.VerificationSnapshot{}, fmt.Errorf("fingerprint project tree: %w", err)
	}
	return deploy.VerificationSnapshot{Passed: true, Fingerprint: fp}, nil
}

// deploySubject is the resolved context both tools operate on: the project
// root, the deploy config, the resolved build shape, and the target adapter.
type deploySubject struct {
	root     string
	cfg      *deployconfig.DeployConfig
	resolved deployconfig.Resolved
	target   deploy.DeployTarget
}

// deployProjectRoot resolves the project root the tools operate on:
// env.WorkspaceRoot when set, or an explicit project_dir argument resolved
// against it. It refuses an absolute or escaping project_dir, so a tool
// argument cannot reach outside the workspace.
func deployProjectRoot(env ToolEnv, args map[string]any) (string, error) {
	base := strings.TrimSpace(env.WorkspaceRoot)
	if base == "" {
		return "", errors.New("no workspace root is available to resolve the project; run inside a workspace")
	}
	dir, _ := extractString(args, "project_dir")
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return filepath.Clean(base), nil
	}
	if filepath.IsAbs(dir) {
		return "", fmt.Errorf("project_dir %q must be a relative path", dir)
	}
	cleaned := filepath.Clean(dir)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("project_dir %q must stay under the workspace root", dir)
	}
	return filepath.Join(filepath.Clean(base), cleaned), nil
}

// loadDeploySubject resolves the project root, loads the deploy config, and
// resolves the target adapter. A missing .sprout/deploy.json surfaces as the
// deployconfig sentinel with an actionable message.
func loadDeploySubject(env ToolEnv, args map[string]any) (deploySubject, error) {
	root, err := deployProjectRoot(env, args)
	if err != nil {
		return deploySubject{}, err
	}

	cfg, err := deployconfig.LoadDeployConfig(root)
	if err != nil {
		if errors.Is(err, deployconfig.ErrNoDeployConfig) {
			return deploySubject{}, fmt.Errorf("this project has no deploy config (%s); create it naming the deploy target and project", deployconfig.DeployConfigPath(root))
		}
		return deploySubject{}, err
	}

	resolved, err := deployconfig.Resolve(cfg, root)
	if err != nil {
		return deploySubject{}, err
	}

	targetID, _ := extractString(args, "target")
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		targetID = strings.TrimSpace(cfg.Target)
	}
	// resolved.Target is the vendor id shown to the user (e.g. in the
	// production approval prompt), not the manifest-qualified adapter id: a
	// target argument is the vendor (e.g. "cloudflare" or "fake").
	resolved.Target = targetID

	adapterID, err := defaultAdapterID(root, targetID)
	if err != nil {
		return deploySubject{}, err
	}

	target, err := resolveDeployTargetFor()(root, adapterID)
	if err != nil {
		return deploySubject{}, err
	}

	return deploySubject{root: root, cfg: cfg, resolved: resolved, target: target}, nil
}

// deployCloudflareVendor is the deploy config target id whose Pages-vs-Workers
// shape the starter manifest selects.
const deployCloudflareVendor = "cloudflare"

// defaultAdapterID is the adapter id handed to the target seam: the vendor id
// (the tool's target argument when set, else the config's target) qualified
// with the manifest's deploy_target shape when that vendor is Cloudflare.
//
// Resolution order (an explicit target argument overrides everything):
//
//   - target argument set: it names the adapter directly (e.g. "fake"). It
//     wins over the config; within the Cloudflare vendor the manifest still
//     picks the shape because the vendor is qualified below.
//   - otherwise the config's target names the vendor.
//   - when the vendor is "cloudflare", the starter manifest's deploy_target
//     picks the shape (default "pages").
//
// A missing starter manifest means the default shape (pages), mirroring the
// CLI: a project can hand-author its deploy config without one.
func defaultAdapterID(root, vendor string) (string, error) {
	vendor = strings.TrimSpace(vendor)
	if vendor != deployCloudflareVendor {
		return vendor, nil
	}
	m, err := starterstore.LoadStarterManifest(root)
	if err != nil {
		if errors.Is(err, starterstore.ErrNoManifest) {
			return vendor + "/" + deploy.DeployTargetPages, nil
		}
		return "", err
	}
	return vendor + "/" + deploy.NormalizeDeployTarget(m.DeployTarget), nil
}

// emitDeployProgress publishes a progress event for a deploy outcome
// (success or refusal) on the shared bus. It reuses the progress_milestone
// event type — a ship step is a milestone of the run — and adds an "outcome"
// key ("success" | "refused") plus a bounded detail string. A nil bus is a
// no-op: the tool's model-visible result never depends on the event.
func emitDeployProgress(env ToolEnv, kind deploy.DeploymentKind, outcome, detail string) {
	if env.EventBus == nil {
		return
	}
	if kind == "" {
		kind = deploy.KindPreview
	}
	payload := map[string]interface{}{
		"run_id":      env.ChatID,
		"phase":       events.MilestonePhaseFinished,
		"scope_title": fmt.Sprintf("Deploy (%s)", kind),
		"outcome":     outcome,
	}
	if detail != "" {
		payload["detail"] = detail
	}
	env.EventBus.Publish(events.EventTypeProgressMilestone, payload)
}

// ---------------------------------------------------------------------------
// deploy_status
// ---------------------------------------------------------------------------

type deployStatusHandler struct{}

func (h *deployStatusHandler) Name() string { return "deploy_status" }

func (h *deployStatusHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "deploy_status",
		Description: "Report the latest deployment recorded for the current project (read-only): " +
			"its id, kind (preview or production), lifecycle state, version, and URL. " +
			"Use it to check what is currently shipped before deploying or rolling back.",
		Required: []string{},
		Parameters: []ParameterDef{
			{Name: "project_dir", Type: "string",
				Description: "Project subdirectory within the workspace to read (relative path). Defaults to the workspace root."},
			{Name: "target", Type: "string",
				Description: "Deploy target id, overriding the deploy config's target. Defaults to the config's target."},
		},
	}
}

func (h *deployStatusHandler) Validate(args map[string]any) error { return nil }

func (h *deployStatusHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	subject, err := loadDeploySubject(env, args)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("deploy_status: %v", err), IsError: true}, nil
	}

	history, err := subject.target.List(subject.cfg.Project)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("deploy_status: list deployments: %v", err), IsError: true}, nil
	}
	if len(history) == 0 {
		return ToolResult{Output: fmt.Sprintf("No deployments recorded for project %q yet.", subject.cfg.Project)}, nil
	}

	latest := history[len(history)-1]
	if state, serr := subject.target.Status(latest); serr == nil {
		latest.Status = state
	}

	version := latest.Version
	if version == "" {
		version = "-"
	}
	out := fmt.Sprintf("Latest deployment for project %q:\n  %s  %s  %s  version=%s  %s",
		subject.cfg.Project, latest.ID, latest.Kind, latest.Status, version, latest.URL)
	return ToolResult{Output: out}, nil
}

func (h *deployStatusHandler) Aliases() []string      { return nil }
func (h *deployStatusHandler) Timeout() time.Duration { return 30 * time.Second }
func (h *deployStatusHandler) MaxResultSize() int     { return 2048 }
func (h *deployStatusHandler) SafeForParallel() bool  { return true }
func (h *deployStatusHandler) Interactive() bool      { return false }

// ---------------------------------------------------------------------------
// deploy
// ---------------------------------------------------------------------------

type deployHandler struct{}

func (h *deployHandler) Name() string { return "deploy" }

func (h *deployHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "deploy",
		Description: "Build the current project and ship it through its deploy target. " +
			"By default this makes a preview deployment (with its own preview URL), which proceeds " +
			"after verification passes. A production deployment requires explicit user approval: " +
			"set production=true and the user is prompted to approve; without approval the deploy is refused. " +
			"Refuses when verification for the current work has not passed (when verification is enabled).",
		Required: []string{},
		Parameters: []ParameterDef{
			{Name: "production", Type: "boolean",
				Description: "Deploy to production (the live site). Always requires explicit user approval; unapproved production deploys are refused. Defaults to false (preview)."},
			{Name: "project_dir", Type: "string",
				Description: "Project subdirectory within the workspace to deploy (relative path). Defaults to the workspace root."},
			{Name: "target", Type: "string",
				Description: "Deploy target id, overriding the deploy config's target. Defaults to the config's target."},
			{Name: "version", Type: "string",
				Description: "Version string to record on the deployment. Defaults to the starter manifest's version."},
		},
	}
}

func (h *deployHandler) Validate(args map[string]any) error { return nil }

func (h *deployHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	subject, err := loadDeploySubject(env, args)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("deploy: %v", err), IsError: true}, nil
	}

	kind := deploy.KindPreview
	if getBoolArg(args, "production") {
		kind = deploy.KindProduction
	}

	buildCmd, version, err := deployStarterBuild(subject.root)
	if err != nil {
		emitDeployProgress(env, kind, "refused", err.Error())
		return ToolResult{Output: fmt.Sprintf("deploy: %v", err), IsError: true}, nil
	}
	if v, _ := extractString(args, "version"); strings.TrimSpace(v) != "" {
		version = strings.TrimSpace(v)
	}

	// Production confirmation: the agent cannot mint a Confirmation on its
	// own. It must come from the interactive approval gate, and a session
	// with no approval surface refuses (fail closed).
	confirm, refusal := deployConfirmation(env, kind, subject)
	if refusal != "" {
		emitDeployProgress(env, kind, "refused", refusal)
		return ToolResult{Output: fmt.Sprintf("deploy: %s", refusal), IsError: true}, nil
	}

	snap, err := deployVerificationGate(env, subject.root, subject.resolved.BuildDir)
	if err != nil {
		emitDeployProgress(env, kind, "refused", err.Error())
		return ToolResult{Output: fmt.Sprintf("deploy: %v", err), IsError: true}, nil
	}

	run, fingerprint := resolveDeployRunnerAndFingerprint()
	deployer := &deploy.Deployer{
		Target:      subject.target,
		Run:         run,
		Fingerprint: fingerprint,
	}

	req := deploy.BuildRequest{
		Root:     subject.root,
		Command:  buildCmd,
		BuildDir: subject.resolved.BuildDir,
		Project:  subject.resolved.Project,
		Kind:     kind,
		Version:  version,
	}

	d, err := deployer.BuildAndDeploy(ctx, req, snap, confirm)
	if err != nil {
		msg := err.Error()
		if errors.Is(err, deploy.ErrProductionNeedsConfirmation) {
			msg = "a production deploy needs explicit user approval"
		}
		emitDeployProgress(env, kind, "refused", msg)
		return ToolResult{Output: fmt.Sprintf("deploy: %s", msg), IsError: true}, nil
	}

	emitDeployProgress(env, kind, "success", fmt.Sprintf("%s deployed to %s", d.ID, d.URL))

	// A completed deploy leaves a checkpoint of the state that shipped, so
	// the timeline carries a marker for what was deployed and, when the
	// history store holds the revision, a restorable one. The checkpoint
	// records the revision current in the process history store — for the
	// agent that is the workspace's revision, i.e. the tree the deploy
	// built — rather than d.Version, which is the opaque starter/plan
	// version and not a history revision a restore could act on. A capture
	// failure never changes the deploy result: the deploy succeeded, and
	// the checkpoint is a convenience over it.
	_, _ = history.CreateCheckpointInWorkspace(subject.root, history.CheckpointDeploy,
		fmt.Sprintf("deployed %s %s", d.Kind, d.ID), nil)

	return ToolResult{Output: fmt.Sprintf("Deployed %s %s to %s", d.Kind, d.ID, d.URL)}, nil
}

func (h *deployHandler) Aliases() []string      { return nil }
func (h *deployHandler) Timeout() time.Duration { return 0 }
func (h *deployHandler) MaxResultSize() int     { return 4096 }
func (h *deployHandler) SafeForParallel() bool  { return false }
func (h *deployHandler) Interactive() bool      { return true }

// deployConfirmation resolves the confirmation a deploy needs. A preview
// deploy needs none (the zero Confirmation is returned). A production deploy
// is routed through the interactive approval gate: an approved prompt yields
// a granted Confirmation, and a denied prompt, a missing approval surface, or
// an error returns a non-empty refusal reason. It never accepts a
// model-supplied confirmation — the approval gate is the only source.
func deployConfirmation(env ToolEnv, kind deploy.DeploymentKind, subject deploySubject) (deploy.Confirmation, string) {
	if kind != deploy.KindProduction {
		return deploy.Confirmation{}, ""
	}
	if env.ApprovalManager == nil {
		return deploy.Confirmation{}, "a production deploy requires explicit user approval, but no approval prompt is available in this session"
	}
	result := env.ApprovalManager.RequestApproval(
		"deploy-production",
		"deploy",
		"critical",
		fmt.Sprintf("Deploy project %q to PRODUCTION (live site) via target %q?",
			subject.resolved.Project, subject.resolved.Target),
		nil,
	)
	if !result.Approved {
		reason := result.Reason
		if strings.TrimSpace(reason) == "" {
			reason = "denied"
		}
		return deploy.Confirmation{}, fmt.Sprintf("production deploy was not approved (%s)", reason)
	}
	return deploy.Confirmation{
		Confirmed:  true,
		ApprovedBy: "tool-approval:deploy",
		Note:       "interactive approval gate",
	}, ""
}

// deployStarterBuild reads the project's starter manifest for the build
// command and version the deploy needs, mirroring cmd/deploy.go's helper. A
// manifest with no build command is refused: there is nothing to upload.
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
