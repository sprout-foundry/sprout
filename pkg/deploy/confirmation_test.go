package deploy

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// productionReq is a production build request over the same shapes the
// preview tests use, so the confirmation tests read as the rule they exercise.
func productionReq(root string) BuildRequest {
	req := buildReq(root)
	req.Kind = KindProduction
	return req
}

// granted is an explicit confirmation, as a CLI prompt or Ship mode action
// would supply.
func granted(by string) Confirmation {
	return Confirmation{Confirmed: true, ApprovedBy: by, Note: "confirmed in test"}
}

// ---------------------------------------------------------------------------
// Preview: automatic, no confirmation needed
// ---------------------------------------------------------------------------

// TestBuildAndDeploy_PreviewRunsAutomaticallyWithoutConfirmation asserts the
// preview half of the rule: once verification passes, a preview deploy runs
// with no confirmation at all (the zero Confirmation), and neither the build
// nor the target is blocked on it.
func TestBuildAndDeploy_PreviewRunsAutomaticallyWithoutConfirmation(t *testing.T) {
	root := t.TempDir()
	req := buildReq(root) // KindPreview
	require.Equal(t, KindPreview, req.Kind)

	runner := &stubRunner{}
	fp := &stubFingerprint{values: []string{"same", "same"}}
	target := NewFake()
	d := &Deployer{Target: target, Run: runner.run, Fingerprint: fp.fingerprint}

	got, err := d.BuildAndDeploy(context.Background(), req, passingSnapshot("same"), Confirmation{})
	require.NoError(t, err, "a preview deploy needs no confirmation")

	assert.Equal(t, 1, runner.calls, "the preview built with no confirmation")
	assert.Equal(t, 2, fp.calls, "the preview ran the full gate: fingerprint checked before and after the build")
	assert.Equal(t, KindPreview, got.Kind)
	require.Len(t, target.Calls(), 1, "the preview uploaded")
	assert.Equal(t, "Deploy", target.Calls()[0].Method)
}

// TestBuildAndDeploy_EmptyKindTreatedAsPreview asserts an empty Kind behaves
// like a preview: the gate refuses only an explicit KindProduction, matching
// the adapters' "empty is treated as preview" rule, so a caller that omits the
// kind is never asked for a production confirmation.
func TestBuildAndDeploy_EmptyKindTreatedAsPreview(t *testing.T) {
	root := t.TempDir()
	req := buildReq(root)
	req.Kind = ""

	runner := &stubRunner{}
	fp := &stubFingerprint{values: []string{"same", "same"}}
	target := NewFake()
	d := &Deployer{Target: target, Run: runner.run, Fingerprint: fp.fingerprint}

	_, err := d.BuildAndDeploy(context.Background(), req, passingSnapshot("same"), Confirmation{})
	require.NoError(t, err, "an unset kind is a preview and needs no confirmation")
	assert.Equal(t, 1, runner.calls)
	require.Len(t, target.Calls(), 1)
}

// ---------------------------------------------------------------------------
// Production: explicit confirmation required
// ---------------------------------------------------------------------------

// TestBuildAndDeploy_ProductionWithConfirmationProceeds asserts the granted
// half of the rule: a production deploy with an explicit confirmation builds
// and uploads, and the target records it as a production deployment.
func TestBuildAndDeploy_ProductionWithConfirmationProceeds(t *testing.T) {
	root := t.TempDir()
	req := productionReq(root)
	runner := &stubRunner{}
	fp := &stubFingerprint{values: []string{"same", "same"}}
	target := NewFake()
	d := &Deployer{Target: target, Run: runner.run, Fingerprint: fp.fingerprint}

	got, err := d.BuildAndDeploy(context.Background(), req, passingSnapshot("same"), granted("cli"))
	require.NoError(t, err, "an explicitly confirmed production deploy proceeds")

	assert.Equal(t, 1, runner.calls, "the confirmed production deploy built")
	assert.Equal(t, KindProduction, got.Kind)
	require.Len(t, target.Calls(), 1, "the confirmed production deploy uploaded")
	assert.Equal(t, "Deploy", target.Calls()[0].Method)
}

// TestBuildAndDeploy_ProductionWithoutConfirmationRefused is the item's
// required test: a production deploy with no confirmation is refused with the
// typed error, and — because the gate is first — the build runner and the
// target are NEVER called.
func TestBuildAndDeploy_ProductionWithoutConfirmationRefused(t *testing.T) {
	root := t.TempDir()
	runner := &stubRunner{}
	fp := &stubFingerprint{values: []string{"same", "same"}}
	target := NewFake()
	d := &Deployer{Target: target, Run: runner.run, Fingerprint: fp.fingerprint}

	_, err := d.BuildAndDeploy(context.Background(), productionReq(root), passingSnapshot("same"), Confirmation{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProductionNeedsConfirmation)

	assert.Zero(t, runner.calls, "the build never runs for an unconfirmed production deploy")
	assert.Zero(t, fp.calls, "the confirmation gate runs before the fingerprint is read")
	assert.Empty(t, target.Calls(), "nothing is uploaded for an unconfirmed production deploy")
}

// TestBuildAndDeploy_ProductionValidButUnconfirmedStillRefused is the
// rule-breaking test: the request is otherwise perfect — a passing
// verification over an unchanged tree, a valid fingerprint, a build command —
// and the confirmation is present but with Confirmed left false (a partial,
// rejected value carrying only an approver). The deploy must still be refused
// before the build or the target is touched, so the rule cannot be broken by a
// plausible-looking confirmation.
func TestBuildAndDeploy_ProductionValidButUnconfirmedStillRefused(t *testing.T) {
	root := t.TempDir()
	req := productionReq(root)
	runner := &stubRunner{}
	// Two identical values: if the gate bled past the confirmation check, the
	// fingerprint gate would pass and the build would run — which is exactly
	// what must not happen.
	fp := &stubFingerprint{values: []string{"same", "same"}}
	target := NewFake()
	d := &Deployer{Target: target, Run: runner.run, Fingerprint: fp.fingerprint}

	partial := Confirmation{Confirmed: false, ApprovedBy: "cli", Note: "not actually approved"}
	_, err := d.BuildAndDeploy(context.Background(), req, passingSnapshot("same"), partial)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProductionNeedsConfirmation)

	assert.Zero(t, runner.calls, "a valid-but-unconfirmed production deploy still never builds")
	assert.Zero(t, fp.calls, "the gate refuses before any fingerprinting")
	assert.Empty(t, target.Calls(), "and never touches the target")
}

// TestBuildAndDeploy_ZeroConfirmationRefusesProduction asserts the gate fails
// closed: the zero value of Confirmation is the default a caller gets from an
// unset field, and it must not be read as confirmed.
func TestBuildAndDeploy_ZeroConfirmationRefusesProduction(t *testing.T) {
	var zero Confirmation
	assert.False(t, zero.Grants(), "the zero Confirmation does not grant")

	root := t.TempDir()
	runner := &stubRunner{}
	fp := &stubFingerprint{values: []string{"same", "same"}}
	target := NewFake()
	d := &Deployer{Target: target, Run: runner.run, Fingerprint: fp.fingerprint}

	_, err := d.BuildAndDeploy(context.Background(), productionReq(root), passingSnapshot("same"), zero)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProductionNeedsConfirmation)
	assert.Zero(t, runner.calls)
	assert.Zero(t, fp.calls)
	assert.Empty(t, target.Calls())
}

// TestBuildAndDeploy_ProductionUnconfirmedRefusedWithRealFingerprint wires the
// real DefaultTreeFingerprint so the refused production path is exercised
// against a genuinely mappable tree: it still does nothing, proving the gate
// is not merely absent real state.
func TestBuildAndDeploy_ProductionUnconfirmedRefusedWithRealFingerprint(t *testing.T) {
	root := t.TempDir()
	req := productionReq(root)
	req.BuildDir = filepath.Join(root, "dist")

	snap, err := DefaultTreeFingerprint(root, req.BuildDir)
	require.NoError(t, err)

	runner := &stubRunner{}
	target := NewFake()
	d := &Deployer{Target: target, Run: runner.run, Fingerprint: DefaultTreeFingerprint}

	_, err = d.BuildAndDeploy(context.Background(), req, passingSnapshot(snap), Confirmation{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProductionNeedsConfirmation)
	assert.Zero(t, runner.calls, "no build for an unconfirmed production deploy")
	assert.Empty(t, target.Calls(), "no upload for an unconfirmed production deploy")
}

// ---------------------------------------------------------------------------
// Confirmation value semantics
// ---------------------------------------------------------------------------

// TestConfirmation_GrantsOnlyWhenConfirmed pins the predicate the gate
// consults: only an explicitly confirmed value grants; the zero value and any
// partial value do not.
func TestConfirmation_GrantsOnlyWhenConfirmed(t *testing.T) {
	cases := []struct {
		name string
		c    Confirmation
		want bool
	}{
		{"zero value", Confirmation{}, false},
		{"approver only", Confirmation{ApprovedBy: "cli"}, false},
		{"note only", Confirmation{Note: "approved?"}, false},
		{"confirmed", Confirmation{Confirmed: true}, true},
		{"confirmed with provenance", Confirmation{Confirmed: true, ApprovedBy: "ship-mode"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.c.Grants())
		})
	}
}

// TestConfirmation_DescribeApproval asserts the audit rendering names the
// approver when present and otherwise returns a non-empty generic marker, so
// a granted confirmation can always be attributed in a log line.
func TestConfirmation_DescribeApproval(t *testing.T) {
	assert.Equal(t, "cli", granted("cli").DescribeApproval())
	assert.Equal(t, "user", Confirmation{Confirmed: true}.DescribeApproval())
	assert.Equal(t, "user", Confirmation{Confirmed: true, ApprovedBy: "   "}.DescribeApproval())
}

// TestConfirmProduction_Gate pins the gate function directly: previews (and an
// empty kind) pass with no confirmation; production passes only when the
// confirmation grants.
func TestConfirmProduction_Gate(t *testing.T) {
	require.NoError(t, confirmProduction(KindPreview, Confirmation{}))
	require.NoError(t, confirmProduction("", Confirmation{}))
	assert.ErrorIs(t, confirmProduction(KindProduction, Confirmation{}), ErrProductionNeedsConfirmation)
	require.NoError(t, confirmProduction(KindProduction, granted("cli")))
}
