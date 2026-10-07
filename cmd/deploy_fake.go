//go:build !js

// persistent fake deploy target: pkg/deploy.FakeTarget's semantics with an
// on-disk journal, so a deployment recorded by one `sprout deploy` invocation
// is visible to the next (`deploy status`, `deploy history`, `deploy
// rollback`). The in-memory FakeTarget cannot do that — a CLI process ends
// between invocations — so this adapter replays its journal into a fresh
// FakeTarget on load, reusing the fake's own deploy/rollback logic rather
// than reimplementing it.
//
// It is the interim target the CLI runs against until the real hosting
// adapter arrives; the target seam in deploy.go is the single place that
// chooses between them.
package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/deploy"
	"github.com/sprout-foundry/sprout/pkg/startermanifest"
)

// deployFakeJournalName is the journal file, relative to the .sprout state
// directory. The journal is the exact sequence of accepted operations, which
// is what makes replay deterministic: FakeTarget's ids and timestamps depend
// only on the order of accepted deploys.
const deployFakeJournalName = "deploy-fake.json"

// fakeJournalEntry is one recorded operation. A deploy carries the request
// that was accepted; a rollback carries the deployment that was rolled back
// (by project and id) — the same pair a caller passes to Rollback.
type fakeJournalEntry struct {
	Op       string                `json:"op"`
	Request  *deploy.DeployRequest `json:"request,omitempty"`
	Project  string                `json:"project,omitempty"`
	DeployID string                `json:"deploy_id,omitempty"`
}

// persistentFakeTarget implements deploy.DeployTarget by replaying a journal
// of accepted operations into an in-memory deploy.FakeTarget. Reads
// (Status/List/PreviewURL) go straight to the replayed fake; mutations append
// to the journal and persist it after the fake has accepted them, so a failed
// operation never leaves a journal entry behind.
type persistentFakeTarget struct {
	inner   *deploy.FakeTarget
	journal []fakeJournalEntry
	path    string
}

// newPersistentFakeTarget loads the journal at .sprout/deploy-fake.json under
// root (an absent journal starts empty) and replays it. A journal that cannot
// be read or decoded is an error: a corrupt state file must not be silently
// treated as "no history".
func newPersistentFakeTarget(root string) (*persistentFakeTarget, error) {
	t := &persistentFakeTarget{
		inner: deploy.NewFake(),
		path:  filepath.Join(root, startermanifest.SproutDir, deployFakeJournalName),
	}
	data, err := os.ReadFile(t.path)
	if err != nil {
		if os.IsNotExist(err) {
			return t, nil
		}
		return nil, fmt.Errorf("read deploy state %s: %w", t.path, err)
	}
	if err := json.Unmarshal(data, &t.journal); err != nil {
		return nil, fmt.Errorf("decode deploy state %s: %w", t.path, err)
	}
	if err := t.replay(); err != nil {
		return nil, fmt.Errorf("restore deploy state %s: %w", t.path, err)
	}
	return t, nil
}

// replay reconstructs the in-memory fake from the journal, in order, so the
// ids and lifecycle states match what the recorded operations produced.
func (t *persistentFakeTarget) replay() error {
	for _, e := range t.journal {
		switch e.Op {
		case "deploy":
			if e.Request == nil {
				return errors.New("journal: deploy entry has no request")
			}
			if _, err := t.inner.Deploy(*e.Request); err != nil {
				return err
			}
		case "rollback":
			if _, err := t.inner.Rollback(deploy.Deployment{Project: e.Project, ID: e.DeployID}); err != nil {
				return err
			}
		default:
			return fmt.Errorf("journal: unknown op %q", e.Op)
		}
	}
	return nil
}

// persist writes the journal back to disk atomically: a temp file in the same
// directory is renamed over the state file, so a crash mid-write never leaves
// a torn journal. It is called only after the fake has accepted a mutation, so
// the file never claims an operation that did not happen. (Cross-process
// concurrency is out of scope for the interim fake; a real adapter owns its
// own remote state.)
func (t *persistentFakeTarget) persist() error {
	data, err := json.MarshalIndent(t.journal, "", "  ")
	if err != nil {
		return fmt.Errorf("encode deploy state: %w", err)
	}
	dir := filepath.Dir(t.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create deploy state dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, deployFakeJournalName+".tmp-*")
	if err != nil {
		return fmt.Errorf("create deploy state temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write deploy state %s: %w", t.path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write deploy state %s: %w", t.path, err)
	}
	if err := os.Rename(tmpName, t.path); err != nil {
		return fmt.Errorf("write deploy state %s: %w", t.path, err)
	}
	return nil
}

// Deploy records the request through the in-memory fake and, only when it is
// accepted, appends it to the journal and persists.
func (t *persistentFakeTarget) Deploy(req deploy.DeployRequest) (deploy.Deployment, error) {
	d, err := t.inner.Deploy(req)
	if err != nil {
		return deploy.Deployment{}, err
	}
	reqCopy := req
	t.journal = append(t.journal, fakeJournalEntry{Op: "deploy", Request: &reqCopy})
	if err := t.persist(); err != nil {
		return deploy.Deployment{}, err
	}
	return d, nil
}

// Status delegates to the replayed fake.
func (t *persistentFakeTarget) Status(d deploy.Deployment) (deploy.StatusState, error) {
	return t.inner.Status(d)
}

// List delegates to the replayed fake.
func (t *persistentFakeTarget) List(project string) ([]deploy.Deployment, error) {
	return t.inner.List(project)
}

// Rollback rolls back through the in-memory fake and, only when it succeeds,
// appends the operation to the journal and persists.
func (t *persistentFakeTarget) Rollback(d deploy.Deployment) (deploy.Deployment, error) {
	restored, err := t.inner.Rollback(d)
	if err != nil {
		return deploy.Deployment{}, err
	}
	t.journal = append(t.journal, fakeJournalEntry{Op: "rollback", Project: d.Project, DeployID: d.ID})
	if err := t.persist(); err != nil {
		return deploy.Deployment{}, err
	}
	return restored, nil
}

// PreviewURL delegates to the replayed fake.
func (t *persistentFakeTarget) PreviewURL(d deploy.Deployment) (string, error) {
	return t.inner.PreviewURL(d)
}
