package deploy

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// FakeTarget is an in-memory DeployTarget for tests. It records every call,
// hands out deterministic ids and URLs, keeps a per-project deployment
// history, and implements rollback by restoring the deployment that preceded
// the one being rolled back.
//
// It is an ordinary adapter: the interface it satisfies is the same one a
// real hosting adapter will satisfy, so tests written against FakeTarget
// describe the behaviour every target must provide. It makes no network
// calls and needs no credentials; it is safe for concurrent use.
//
// Determinism: ids are "<project>-<n>" with n counting 1,2,3… per target
// (not per project), and URLs are derived from the id, so a test can assert
// exact values without a clock or a rand source. CreatedAt advances one
// second per deploy from a fixed epoch so history ordering is stable and
// comparable.
type FakeTarget struct {
	mu sync.Mutex

	// calls records every method invocation, in order, for assertions.
	calls []Call

	// next is the ordinal handed to the next deploy, per target.
	next int

	// history maps a project name to its deployments, oldest first.
	history map[string][]Deployment
}

// Call records one method invocation on a FakeTarget. Method names the
// interface method and Args carries the salient input (the request's
// project, or the deployment id) for a test to assert on.
type Call struct {
	// Method is the interface method name: "Deploy", "Status", "List",
	// "Rollback", or "PreviewURL".
	Method string
	// Project is the project the call concerned, when it concerned one.
	Project string
	// DeploymentID is the deployment the call concerned, when it concerned
	// one.
	DeploymentID string
}

// fakeEpoch is the fixed base of the fake's synthetic clock, so CreatedAt
// values are deterministic across runs.
var fakeEpoch = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// NewFake returns an empty FakeTarget ready to deploy to.
func NewFake() *FakeTarget {
	return &FakeTarget{history: map[string][]Deployment{}}
}

// record appends a call to the transcript. The caller holds mu.
func (f *FakeTarget) record(c Call) {
	f.calls = append(f.calls, c)
}

// Calls returns a copy of the recorded call transcript, in order.
func (f *FakeTarget) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Call, len(f.calls))
	copy(out, f.calls)
	return out
}

// Deploy records the build described by req as a deployment, appends it to
// the project's history, and returns it. IDs and URLs are deterministic
// (see FakeTarget). A request missing a project or build directory is
// rejected.
func (f *FakeTarget) Deploy(req DeployRequest) (Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if strings.TrimSpace(req.Project) == "" {
		return Deployment{}, errors.New("deploy: project must not be empty")
	}
	if strings.TrimSpace(req.BuildDir) == "" {
		return Deployment{}, errors.New("deploy: build directory must not be empty")
	}

	kind := req.Kind
	if kind == "" {
		kind = KindPreview
	}
	if kind != KindPreview && kind != KindProduction {
		return Deployment{}, fmt.Errorf("deploy: unknown deployment kind %q", req.Kind)
	}

	f.next++
	id := fmt.Sprintf("%s-%d", req.Project, f.next)

	// Record the call only once the request is known valid, so the
	// transcript lists deploys the target actually accepted.
	f.record(Call{Method: "Deploy", Project: req.Project})

	d := Deployment{
		ID:        id,
		Project:   req.Project,
		Kind:      kind,
		URL:       f.urlFor(req.Project, kind, id),
		Version:   req.Version,
		CreatedAt: fakeEpoch.Add(time.Duration(f.next) * time.Second),
		Status:    StatusReady,
	}
	f.history[req.Project] = append(f.history[req.Project], d)
	return d, nil
}

// Status returns the current state of a known deployment, or
// ErrUnknownDeployment.
func (f *FakeTarget) Status(d Deployment) (StatusState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.record(Call{Method: "Status", Project: d.Project, DeploymentID: d.ID})

	current, ok := f.findLocked(d)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownDeployment, d.ID)
	}
	return current.Status, nil
}

// List returns the project's deployment history, oldest first. An unknown
// project yields an empty (non-nil) slice and no error.
func (f *FakeTarget) List(project string) ([]Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.record(Call{Method: "List", Project: project})

	history := f.history[project]
	out := make([]Deployment, len(history))
	copy(out, history)
	return out, nil
}

// Rollback reverts to the deployment immediately preceding d and marks d
// rolled back, returning the deployment that is now live. The pair of state
// changes is recorded, so a caller can see both the rolled-back deployment
// and the restored one.
//
// Rollback is a one-step rewind of the project's current head: it restores
// the deployment at position len(history)-2 and leaves everything older
// untouched. Rolling back a deployment that is not the current head is not
// a re-selection — it still restores that deployment's immediate
// predecessor, so a deployment that was already rolled back stays marked.
// See ErrNoPreviousDeployment and ErrUnknownDeployment.
func (f *FakeTarget) Rollback(d Deployment) (Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.record(Call{Method: "Rollback", Project: d.Project, DeploymentID: d.ID})

	current, ok := f.findLocked(d)
	if !ok {
		return Deployment{}, fmt.Errorf("%w: %q", ErrUnknownDeployment, d.ID)
	}

	history := f.history[current.Project]
	idx := indexOf(history, current.ID)
	if idx <= 0 {
		// idx == 0: nothing precedes it; idx < 0 should not happen for a
		// found deployment, but is treated the same defensively.
		return Deployment{}, fmt.Errorf("%w for %q", ErrNoPreviousDeployment, current.ID)
	}

	history[idx].Status = StatusRolledBack
	history[idx-1].Status = StatusReady
	f.history[current.Project] = history
	return history[idx-1], nil
}

// PreviewURL returns the per-deployment preview address for d. A deployment
// that is not a preview has no separate preview address, so this reports
// ErrPreviewUnsupported for it — matching the interface's "where the target
// supports per-deployment previews" and the preview/production split.
func (f *FakeTarget) PreviewURL(d Deployment) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.record(Call{Method: "PreviewURL", Project: d.Project, DeploymentID: d.ID})

	current, ok := f.findLocked(d)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownDeployment, d.ID)
	}
	if current.Kind != KindPreview {
		return "", fmt.Errorf("%w: %q is a %s deployment", ErrPreviewUnsupported, current.ID, current.Kind)
	}
	return current.URL, nil
}

// urlFor builds the deterministic URL for a deployment: previews get a
// per-deployment host carrying the id, production gets the project's live
// host.
func (f *FakeTarget) urlFor(project string, kind DeploymentKind, id string) string {
	if kind == KindProduction {
		return fmt.Sprintf("https://%s.example.test", project)
	}
	return fmt.Sprintf("https://%s.example.test", id)
}

// findLocked returns the stored deployment matching d by project and id.
// The caller holds mu.
func (f *FakeTarget) findLocked(d Deployment) (Deployment, bool) {
	for _, candidate := range f.history[d.Project] {
		if candidate.ID == d.ID {
			return candidate, true
		}
	}
	return Deployment{}, false
}

// indexOf returns the position of id in deployments, or -1.
func indexOf(deployments []Deployment, id string) int {
	for i, d := range deployments {
		if d.ID == id {
			return i
		}
	}
	return -1
}
