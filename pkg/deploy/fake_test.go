package deploy

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fake is a convenience constructor with a filled-in request, so each test
// reads as the behaviour it exercises.
func req(project string, kind DeploymentKind) DeployRequest {
	return DeployRequest{Project: project, Kind: kind, BuildDir: "dist", Version: "1.0.0"}
}

// ---------------------------------------------------------------------------
// Round trip: deploy → list → status → preview URL → rollback
// ---------------------------------------------------------------------------

// TestFakeTarget_RoundTrip is the full lifecycle the acceptance criteria
// name: deploy, see it in history, read its status, resolve its preview URL,
// then roll back to the previous deployment.
func TestFakeTarget_RoundTrip(t *testing.T) {
	f := NewFake()

	first, err := f.Deploy(req("web-app", KindPreview))
	require.NoError(t, err)
	assert.NotEmpty(t, first.ID)
	assert.Equal(t, "web-app", first.Project)
	assert.Equal(t, KindPreview, first.Kind)
	assert.Equal(t, StatusReady, first.Status)
	assert.NotEmpty(t, first.URL)

	// List shows it.
	listed, err := f.List("web-app")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, first, listed[0])

	// Status returns it.
	state, err := f.Status(first)
	require.NoError(t, err)
	assert.Equal(t, StatusReady, state)

	// Preview URL resolves to the deployment's URL (a preview deployment).
	previewURL, err := f.PreviewURL(first)
	require.NoError(t, err)
	assert.Equal(t, first.URL, previewURL)

	// Deploy a second one, then roll back to the first.
	second, err := f.Deploy(req("web-app", KindPreview))
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)

	live, err := f.Rollback(second)
	require.NoError(t, err)
	assert.Equal(t, first.ID, live.ID, "rollback restores the previous deployment")

	// The rolled-back deployment is now marked, and the restored one ready.
	state, err = f.Status(second)
	require.NoError(t, err)
	assert.Equal(t, StatusRolledBack, state)

	state, err = f.Status(first)
	require.NoError(t, err)
	assert.Equal(t, StatusReady, state)

	// And the rollback is recorded in the call transcript.
	calls := f.Calls()
	var rollback *Call
	for i := range calls {
		if calls[i].Method == "Rollback" {
			rollback = &calls[i]
		}
	}
	require.NotNil(t, rollback, "rollback is recorded")
	assert.Equal(t, second.ID, rollback.DeploymentID)
}

// TestFakeTarget_RollbackIsRecorded asserts both halves of a rollback appear
// in the transcript: the rollback call that named the deployment, and the
// resulting state transitions observable through Status.
func TestFakeTarget_RollbackIsRecorded(t *testing.T) {
	f := NewFake()
	a, err := f.Deploy(req("site", KindProduction))
	require.NoError(t, err)
	b, err := f.Deploy(req("site", KindProduction))
	require.NoError(t, err)

	_, err = f.Rollback(b)
	require.NoError(t, err)

	// The restored deployment becomes the live one again.
	live, err := f.List("site")
	require.NoError(t, err)
	require.Len(t, live, 2)
	assert.Equal(t, StatusReady, live[0].Status, "restored deployment is live")
	assert.Equal(t, StatusRolledBack, live[1].Status, "rolled-back deployment is marked")
	assert.Equal(t, a.ID, live[0].ID)
}

// ---------------------------------------------------------------------------
// Rollback with no previous deployment
// ---------------------------------------------------------------------------

func TestFakeTarget_RollbackNoPreviousDeployment(t *testing.T) {
	f := NewFake()
	only, err := f.Deploy(req("site", KindPreview))
	require.NoError(t, err)

	_, err = f.Rollback(only)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoPreviousDeployment)
}

func TestFakeTarget_RollbackUnknownDeployment(t *testing.T) {
	f := NewFake()
	_, err := f.Rollback(Deployment{ID: "does-not-exist", Project: "site"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownDeployment)
}

// ---------------------------------------------------------------------------
// Preview vs production
// ---------------------------------------------------------------------------

// TestFakeTarget_ProductionVsPreviewAreDistinguishable asserts the two
// kinds are preserved through the round trip and get distinct URLs: a
// production deploy serves the project's live host, a preview deploy gets a
// per-deployment host.
func TestFakeTarget_ProductionVsPreviewAreDistinguishable(t *testing.T) {
	f := NewFake()

	prev, err := f.Deploy(req("web-app", KindPreview))
	require.NoError(t, err)
	prod, err := f.Deploy(req("web-app", KindProduction))
	require.NoError(t, err)

	assert.Equal(t, KindPreview, prev.Kind)
	assert.Equal(t, KindProduction, prod.Kind)
	assert.NotEqual(t, prev.URL, prod.URL, "preview and production serve different URLs")
	assert.Contains(t, prev.URL, prev.ID, "preview URL is per-deployment")

	// A production deployment has no separate preview address.
	_, err = f.PreviewURL(prod)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPreviewUnsupported)

	// The distinction survives a list round trip.
	listed, err := f.List("web-app")
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, KindPreview, listed[0].Kind)
	assert.Equal(t, KindProduction, listed[1].Kind)
}

// TestFakeTarget_PreviewURLUnknownDeployment covers the unknown-deployment
// path of PreviewURL, distinct from the unsupported-kind path.
func TestFakeTarget_PreviewURLUnknownDeployment(t *testing.T) {
	f := NewFake()
	_, err := f.PreviewURL(Deployment{ID: "ghost", Project: "web-app"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownDeployment)
}

// ---------------------------------------------------------------------------
// List: stability and ordering
// ---------------------------------------------------------------------------

// TestFakeTarget_ListOrderedAndStable asserts history is oldest-first and
// that repeated reads return the same order (deterministic ids and times).
func TestFakeTarget_ListOrderedAndStable(t *testing.T) {
	f := NewFake()
	var ids []string
	for i := 0; i < 3; i++ {
		d, err := f.Deploy(req("web-app", KindPreview))
		require.NoError(t, err)
		ids = append(ids, d.ID)
	}

	first, err := f.List("web-app")
	require.NoError(t, err)
	second, err := f.List("web-app")
	require.NoError(t, err)

	require.Len(t, first, 3)
	assert.Equal(t, ids, []string{first[0].ID, first[1].ID, first[2].ID},
		"history is oldest first")
	assert.Equal(t, first, second, "repeated List calls are stable")

	// CreatedAt is strictly increasing, so ordering is well-defined.
	for i := 1; i < len(first); i++ {
		assert.True(t, first[i].CreatedAt.After(first[i-1].CreatedAt),
			"created times increase with order")
	}
}

func TestFakeTarget_ListUnknownProjectIsEmpty(t *testing.T) {
	f := NewFake()
	got, err := f.List("never-deployed")
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NotNil(t, got, "unknown project yields an empty, non-nil slice")
}

// ---------------------------------------------------------------------------
// Determinism and histories per project
// ---------------------------------------------------------------------------

func TestFakeTarget_DeterministicIDsAndURLs(t *testing.T) {
	f := NewFake()
	a, err := f.Deploy(req("alpha", KindPreview))
	require.NoError(t, err)
	b, err := f.Deploy(req("beta", KindPreview))
	require.NoError(t, err)

	assert.Equal(t, "alpha-1", a.ID)
	assert.Equal(t, "beta-2", b.ID, "ids count per target, not per project")
	assert.Equal(t, "https://alpha-1.example.test", a.URL)

	// Fixed epoch base: the ordinal-th deploy lands n seconds after it.
	assert.Equal(t, fakeEpoch.Add(1*time.Second), a.CreatedAt)
	assert.Equal(t, fakeEpoch.Add(2*time.Second), b.CreatedAt)
}

func TestFakeTarget_HistoriesArePerProject(t *testing.T) {
	f := NewFake()
	_, err := f.Deploy(req("alpha", KindPreview))
	require.NoError(t, err)
	_, err = f.Deploy(req("beta", KindProduction))
	require.NoError(t, err)

	alpha, err := f.List("alpha")
	require.NoError(t, err)
	beta, err := f.List("beta")
	require.NoError(t, err)
	assert.Len(t, alpha, 1)
	assert.Len(t, beta, 1)
	assert.Equal(t, "alpha", alpha[0].Project)
	assert.Equal(t, "beta", beta[0].Project)
}

// ---------------------------------------------------------------------------
// Input validation
// ---------------------------------------------------------------------------

func TestFakeTarget_DeployRejectsInvalidRequests(t *testing.T) {
	cases := []struct {
		name string
		req  DeployRequest
	}{
		{"empty project", DeployRequest{BuildDir: "dist"}},
		{"blank project", DeployRequest{Project: "  ", BuildDir: "dist"}},
		{"empty build dir", DeployRequest{Project: "web-app"}},
		{"blank build dir", DeployRequest{Project: "web-app", BuildDir: " "}},
		{"unknown kind", DeployRequest{Project: "web-app", BuildDir: "dist", Kind: "staging"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := NewFake()
			_, err := f.Deploy(tc.req)
			require.Error(t, err)
			for _, c := range f.Calls() {
				assert.NotEqual(t, "Deploy", c.Method, "a rejected deploy is not recorded as a call")
			}
			got, err := f.List(tc.req.Project)
			require.NoError(t, err)
			assert.Empty(t, got, "a rejected deploy records nothing")
		})
	}
}

func TestFakeTarget_DeployDefaultsKindToPreview(t *testing.T) {
	f := NewFake()
	d, err := f.Deploy(DeployRequest{Project: "web-app", BuildDir: "dist"})
	require.NoError(t, err)
	assert.Equal(t, KindPreview, d.Kind)
}

// ---------------------------------------------------------------------------
// Call recording
// ---------------------------------------------------------------------------

func TestFakeTarget_RecordsCallsInOrder(t *testing.T) {
	f := NewFake()
	d, err := f.Deploy(req("web-app", KindPreview))
	require.NoError(t, err)
	_, err = f.Status(d)
	require.NoError(t, err)
	_, err = f.List("web-app")
	require.NoError(t, err)
	_, err = f.PreviewURL(d)
	require.NoError(t, err)

	got := f.Calls()
	require.Len(t, got, 4)
	assert.Equal(t, []string{"Deploy", "Status", "List", "PreviewURL"},
		[]string{got[0].Method, got[1].Method, got[2].Method, got[3].Method})
	assert.Equal(t, d.ID, got[1].DeploymentID)

	// Calls returns a copy: mutating it must not alter the transcript.
	got[0].Method = "tampered"
	assert.Equal(t, []string{"Deploy", "Status", "List", "PreviewURL"},
		[]string{f.Calls()[0].Method, f.Calls()[1].Method, f.Calls()[2].Method, f.Calls()[3].Method})
}

// ---------------------------------------------------------------------------
// Interface conformance and concurrency
// ---------------------------------------------------------------------------

// TestFakeTargetImplementsDeployTarget is the compile-time contract check,
// written as a test so a change to the interface fails this package.
func TestFakeTargetImplementsDeployTarget(t *testing.T) {
	var _ DeployTarget = NewFake()
}

func TestFakeTarget_ConcurrentDeploys(t *testing.T) {
	f := NewFake()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err := f.Deploy(req("web-app", KindPreview))
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	got, err := f.List("web-app")
	require.NoError(t, err)
	assert.Len(t, got, n)
	seen := map[string]bool{}
	for _, d := range got {
		require.False(t, seen[d.ID], "ids are unique under concurrency")
		seen[d.ID] = true
	}
}

// TestFakeTarget_StatusUnknownDeployment covers Status's unknown path.
func TestFakeTarget_StatusUnknownDeployment(t *testing.T) {
	f := NewFake()
	_, err := f.Status(Deployment{ID: "ghost", Project: "web-app"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownDeployment))
}
