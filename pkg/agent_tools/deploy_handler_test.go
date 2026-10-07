package tools

// Tests for the deploy_status and deploy agent tools. They
// exercise the handlers with injected seams only — a fake deploy target, a
// stub build runner, a stub tree fingerprint, and a stub verification
// result — so no process is spawned and no network is touched.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/deploy"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/startermanifest"
)

// newDeployProject creates a temporary project with a starter manifest (a
// build command and dist build output) and a deploy config naming the fake
// target, plus the build output directory. It returns the project root.
func newDeployProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	sproutDir := filepath.Join(root, startermanifest.SproutDir)
	require.NoError(t, os.MkdirAll(sproutDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "dist"), 0o755))

	manifest := `{"starter":{"id":"web-app","version":"1.2.0"},"build":"echo build",` +
		`"build_output":"dist"}`
	require.NoError(t, os.WriteFile(filepath.Join(sproutDir, startermanifest.StarterJSONName),
		[]byte(manifest), 0o644))

	cfg := `{"target":"fake","project":"myapp"}`
	require.NoError(t, os.WriteFile(filepath.Join(sproutDir, "deploy.json"),
		[]byte(cfg), 0o644))
	return root
}

// installDeploySeams swaps the package-level deploy seams for stubs and
// restores them when the test finishes. The fingerprint is constant so the
// pre- and post-build checks both match the snapshot; the build runner is a
// no-op.
func installDeploySeams(t *testing.T, target deploy.DeployTarget) {
	t.Helper()
	ToolFuncMu.Lock()
	prevTarget, prevRun, prevFP := deployTargetFor, deployBuildRunner, deployTreeFingerprint
	deployTargetFor = func(root, targetID string) (deploy.DeployTarget, error) { return target, nil }
	deployBuildRunner = func(ctx context.Context, root, command string) error { return nil }
	deployTreeFingerprint = func(root string, skip ...string) (string, error) { return "fp-1", nil }
	ToolFuncMu.Unlock()

	t.Cleanup(func() {
		ToolFuncMu.Lock()
		deployTargetFor, deployBuildRunner, deployTreeFingerprint = prevTarget, prevRun, prevFP
		ToolFuncMu.Unlock()
	})
}

// verificationManager returns a ConfigManager with SP-149 verification
// enabled (or disabled) for the deploy verification gate.
func verificationManager(enabled bool) *configuration.Manager {
	cfg := configuration.NewConfig()
	cfg.Verification = &configuration.VerificationConfig{Enabled: enabled}
	return configuration.NewManagerWithConfig(cfg, nil)
}

// stubApprovalManager is an ApprovalManager stub recording calls and
// returning a fixed verdict.
type stubApprovalManager struct {
	approved bool
	reason   string
	calls    int
	prompts  []string
}

func (s *stubApprovalManager) RequestApproval(requestID, toolName, riskLevel, prompt string, extras map[string]string) ApprovalResult {
	s.calls++
	s.prompts = append(s.prompts, prompt)
	return ApprovalResult{Approved: s.approved, Reason: s.reason}
}

// deployToolEnv builds the ToolEnv the tools run under, with the project
// root as the workspace and optional overrides.
func deployToolEnv(root string, opts ...func(*ToolEnv)) ToolEnv {
	env := ToolEnv{WorkspaceRoot: root}
	for _, opt := range opts {
		opt(&env)
	}
	return env
}

func TestDeployTools_RegisteredWithValidDefinitions(t *testing.T) {
	reg := GetNewToolRegistry()
	for _, name := range []string{"deploy", "deploy_status"} {
		h, ok := reg.Lookup(name)
		require.True(t, ok, "tool %q must be registered", name)
		def := h.Definition()
		assert.Equal(t, name, def.Name)
		assert.NotEmpty(t, def.Description)
	}
}

func TestDeployTools_DefinitionNamesMatch(t *testing.T) {
	assert.Equal(t, "deploy", (&deployHandler{}).Name())
	assert.Equal(t, "deploy_status", (&deployStatusHandler{}).Name())
	// The deploy tool is interactive (it may prompt for production);
	// deploy_status is read-only and safe to parallelize.
	assert.True(t, (&deployHandler{}).Interactive())
	assert.True(t, (&deployStatusHandler{}).SafeForParallel())
	assert.False(t, (&deployHandler{}).SafeForParallel())
}

// TestDeployStatus_ReportsLatest deploys once (through the tool) and then
// reads it back through deploy_status.
func TestDeployStatus_ReportsLatest(t *testing.T) {
	root := newDeployProject(t)
	target := deploy.NewFake()
	installDeploySeams(t, target)
	env := deployToolEnv(root)

	_, err := (&deployHandler{}).Execute(context.Background(), env, map[string]any{})
	require.NoError(t, err)

	res, err := (&deployStatusHandler{}).Execute(context.Background(), env, map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError, "output: %s", res.Output)
	assert.Contains(t, res.Output, "myapp-1")
	assert.Contains(t, res.Output, "preview")
	assert.Contains(t, res.Output, "ready")
}

func TestDeployStatus_NoDeployments(t *testing.T) {
	root := newDeployProject(t)
	installDeploySeams(t, deploy.NewFake())

	res, err := (&deployStatusHandler{}).Execute(context.Background(), deployToolEnv(root), map[string]any{})
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Contains(t, res.Output, "No deployments")
}

func TestDeployStatus_MissingConfig(t *testing.T) {
	root := t.TempDir()
	installDeploySeams(t, deploy.NewFake())

	res, err := (&deployStatusHandler{}).Execute(context.Background(), deployToolEnv(root), map[string]any{})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Output, "no deploy config")
}

// TestDeploy_PreviewDeploysAfterVerificationDisabled: with verification
// disabled (the default), a preview deploy proceeds and the target receives
// the upload.
func TestDeploy_PreviewDeploysAfterVerificationDisabled(t *testing.T) {
	root := newDeployProject(t)
	target := deploy.NewFake()
	installDeploySeams(t, target)

	res, err := (&deployHandler{}).Execute(context.Background(), deployToolEnv(root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError, "output: %s", res.Output)
	assert.Contains(t, res.Output, "Deployed preview")

	calls := target.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "Deploy", calls[0].Method)
	assert.Equal(t, "myapp", calls[0].Project)
}

// TestDeploy_VerificationFailureRefuses: verification enabled and the latest
// result failed → refuse before the target is called.
func TestDeploy_VerificationFailureRefuses(t *testing.T) {
	root := newDeployProject(t)
	target := deploy.NewFake()
	installDeploySeams(t, target)

	env := deployToolEnv(root,
		func(e *ToolEnv) { e.ConfigManager = verificationManager(true) },
		func(e *ToolEnv) {
			e.ToolFuncs = &ToolFuncSet{DeployVerification: func() (bool, bool) { return false, true }}
		},
	)

	res, err := (&deployHandler{}).Execute(context.Background(), env, map[string]any{})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Output, "verification did not pass")
	assert.Empty(t, target.Calls(), "the target must not be called when verification fails")
}

// TestDeploy_VerificationEnabledNoResultFailsClosed: verification enabled but
// no result wired → refuse (fail closed), never deploy on a guess.
func TestDeploy_VerificationEnabledNoResultFailsClosed(t *testing.T) {
	root := newDeployProject(t)
	target := deploy.NewFake()
	installDeploySeams(t, target)

	t.Run("no seam wired", func(t *testing.T) {
		env := deployToolEnv(root, func(e *ToolEnv) { e.ConfigManager = verificationManager(true) })
		res, err := (&deployHandler{}).Execute(context.Background(), env, map[string]any{})
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Contains(t, res.Output, "no verification result is available")
		assert.Empty(t, target.Calls())
	})

	t.Run("seam reports no result", func(t *testing.T) {
		env := deployToolEnv(root,
			func(e *ToolEnv) { e.ConfigManager = verificationManager(true) },
			func(e *ToolEnv) {
				e.ToolFuncs = &ToolFuncSet{DeployVerification: func() (bool, bool) { return false, false }}
			},
		)
		res, err := (&deployHandler{}).Execute(context.Background(), env, map[string]any{})
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Contains(t, res.Output, "verification has not run")
		assert.Empty(t, target.Calls())
	})
}

// TestDeploy_VerificationPassedDeploys: verification enabled and passing →
// the deploy proceeds.
func TestDeploy_VerificationPassedDeploys(t *testing.T) {
	root := newDeployProject(t)
	target := deploy.NewFake()
	installDeploySeams(t, target)

	env := deployToolEnv(root,
		func(e *ToolEnv) { e.ConfigManager = verificationManager(true) },
		func(e *ToolEnv) {
			e.ToolFuncs = &ToolFuncSet{DeployVerification: func() (bool, bool) { return true, true }}
		},
	)

	res, err := (&deployHandler{}).Execute(context.Background(), env, map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError, "output: %s", res.Output)
	assert.Len(t, target.Calls(), 1)
}

// TestDeploy_ProductionWithoutApprovalRefused: no approval surface → refuse
// and do not deploy.
func TestDeploy_ProductionWithoutApprovalRefused(t *testing.T) {
	root := newDeployProject(t)
	target := deploy.NewFake()
	installDeploySeams(t, target)

	res, err := (&deployHandler{}).Execute(context.Background(), deployToolEnv(root),
		map[string]any{"production": true})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Output, "requires explicit user approval")
	assert.Empty(t, target.Calls(), "an unconfirmed production deploy must not reach the target")
}

// TestDeploy_ProductionApprovalDeniedRefused: the approval prompt is shown but
// denied → refuse.
func TestDeploy_ProductionApprovalDeniedRefused(t *testing.T) {
	root := newDeployProject(t)
	target := deploy.NewFake()
	installDeploySeams(t, target)

	approval := &stubApprovalManager{approved: false, reason: "rejected"}
	env := deployToolEnv(root, func(e *ToolEnv) { e.ApprovalManager = approval })

	res, err := (&deployHandler{}).Execute(context.Background(), env, map[string]any{"production": true})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Output, "not approved")
	assert.Equal(t, 1, approval.calls, "the tool must ask for approval")
	assert.Empty(t, target.Calls())
}

// TestDeploy_ProductionApprovedDeploys: an approved prompt yields a granted
// Confirmation and the production deploy proceeds.
func TestDeploy_ProductionApprovedDeploys(t *testing.T) {
	root := newDeployProject(t)
	target := deploy.NewFake()
	installDeploySeams(t, target)

	approval := &stubApprovalManager{approved: true}
	env := deployToolEnv(root, func(e *ToolEnv) { e.ApprovalManager = approval })

	res, err := (&deployHandler{}).Execute(context.Background(), env, map[string]any{"production": true})
	require.NoError(t, err)
	require.False(t, res.IsError, "output: %s", res.Output)
	assert.Contains(t, res.Output, "Deployed production")
	assert.Equal(t, 1, approval.calls)
	require.Len(t, target.Calls(), 1)
}

// TestDeploy_ModelCannotSelfConfirmProduction pins that there is no
// model-supplied confirmation argument: a "confirm"/"confirmed" argument is
// ignored and production is still refused without the approval gate.
func TestDeploy_ModelCannotSelfConfirmProduction(t *testing.T) {
	root := newDeployProject(t)
	target := deploy.NewFake()
	installDeploySeams(t, target)

	for _, arg := range []string{"confirm", "confirmed", "yes"} {
		res, err := (&deployHandler{}).Execute(context.Background(), deployToolEnv(root),
			map[string]any{"production": true, arg: true})
		require.NoError(t, err)
		assert.True(t, res.IsError, "argument %q must not self-confirm a production deploy", arg)
	}
	assert.Empty(t, target.Calls())
}

// TestDeploy_ProgressEventOnSuccess: a successful deploy publishes a progress
// progress event carrying the outcome.
func TestDeploy_ProgressEventOnSuccess(t *testing.T) {
	root := newDeployProject(t)
	installDeploySeams(t, deploy.NewFake())

	bus := events.NewEventBus()
	sub := bus.Subscribe("deploy-success")

	env := deployToolEnv(root, func(e *ToolEnv) { e.EventBus = bus })
	res, err := (&deployHandler{}).Execute(context.Background(), env, map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)

	ev := nextEvent(t, sub)
	require.NotNil(t, ev)
	assert.Equal(t, events.EventTypeProgressMilestone, ev.Type)
	data, ok := ev.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "success", data["outcome"])
	assert.Equal(t, events.MilestonePhaseFinished, data["phase"])
}

// TestDeploy_ProgressEventOnRefusal: a refused deploy (unconfirmed
// production) still publishes a progress event.
func TestDeploy_ProgressEventOnRefusal(t *testing.T) {
	root := newDeployProject(t)
	installDeploySeams(t, deploy.NewFake())

	bus := events.NewEventBus()
	sub := bus.Subscribe("deploy-refusal")

	env := deployToolEnv(root, func(e *ToolEnv) { e.EventBus = bus })
	res, err := (&deployHandler{}).Execute(context.Background(), env, map[string]any{"production": true})
	require.NoError(t, err)
	require.True(t, res.IsError)

	ev := nextEvent(t, sub)
	require.NotNil(t, ev)
	assert.Equal(t, events.EventTypeProgressMilestone, ev.Type)
	data := ev.Data.(map[string]any)
	assert.Equal(t, "refused", data["outcome"])
}

// nextEvent waits briefly for the next event on sub, returning nil when none
// arrives.
func nextEvent(t *testing.T, sub <-chan events.UIEvent) *events.UIEvent {
	t.Helper()
	select {
	case ev := <-sub:
		return &ev
	case <-time.After(2 * time.Second):
		return nil
	}
}
