// Package deploy defines the deploy-target interface and the in-memory
// fake adapter used by tests: deploy, status, history (list), rollback, and
// per-deployment preview URLs.
//
// The package is a pure contract: it holds the DeployTarget interface, the
// value types that cross it (Deployment, DeployRequest, DeploymentKind), and
// the build-and-upload orchestrator (build.go). It reaches for nothing outside
// the standard library and the shell-exec helper the build runner needs, so
// real adapters and consumers (CLI, agent tools, Ship mode) can depend on it
// without dragging in a network client. An adapter is a small package that
// implements the interface; the fake (fake.go) is one such adapter, for tests.
//
// A build is described by DeployRequest. The build runs in the workspace
// (from the starter manifest's build command) and the adapter uploads the
// resulting output — it never rebuilds on the target. The request carries the
// build output directory. The build-and-upload orchestration lives in
// build.go; the first gate it applies is preview vs production — a production
// deploy is refused unless the caller supplies an explicit Confirmation
// (confirmation.go), while a preview may run automatically — followed by the
// "what was verified is what ships" verification gate; the interface and
// value types it drives live here.
package deploy

import (
	"errors"
	"time"
)

// ErrNoPreviousDeployment is returned by Rollback when the target has no
// earlier deployment to revert to: the deployment being rolled back is the
// only one recorded (or the list is empty).
var ErrNoPreviousDeployment = errors.New("no previous deployment to roll back to")

// ErrUnknownDeployment is returned by Status, Rollback, and PreviewURL when
// the deployment is not one the target knows about (an unknown id, or a
// Deployment that was never deployed by this target).
var ErrUnknownDeployment = errors.New("unknown deployment")

// ErrPreviewUnsupported is returned by PreviewURL when the target does not
// offer a per-deployment preview URL (a target that supports per-deployment
// previews is one kind of deploy target; a target that only ever serves
// production returns this instead of inventing a URL).
var ErrPreviewUnsupported = errors.New("target does not support preview URLs")

// DeploymentKind distinguishes the two deploy shapes: a preview deployment
// (safe to run automatically once verification passes, with its own
// per-deployment URL) and a production deployment (the live site, which
// always needs explicit user confirmation).
type DeploymentKind string

const (
	// KindPreview is a per-deployment preview. Preview deploys may run
	// automatically once verification passes.
	KindPreview DeploymentKind = "preview"
	// KindProduction is the live production deployment. Production deploys
	// always require explicit user confirmation.
	KindProduction DeploymentKind = "production"
)

// Deployment is one recorded deploy of a project to a target: the identity
// (ID), what was deployed (Project, Version), where it landed (URL), the
// deploy shape (Kind), and its lifecycle state (Status).
//
// A Deployment is an immutable value: adapters return copies and never
// hand out a reference callers can mutate. Rollback and PreviewURL take a
// Deployment and locate it by its ID (and Project), so a stale copy is
// still recognised as long as the id matches.
type Deployment struct {
	// ID is the target-assigned deployment identifier. Unique within a
	// target and stable for the life of the deployment.
	ID string
	// Project is the project name the deployment belongs to. A target may
	// serve several projects; history is registered per project.
	Project string
	// Kind is whether this is a preview or a production deployment.
	Kind DeploymentKind
	// URL is the address the deployment is served at. For a preview
	// deployment this is the per-deployment preview URL.
	URL string
	// Version identifies what was built: the starter/plan version the
	// deployment was produced from. Opaque to this package.
	Version string
	// CreatedAt is when the deployment was recorded, in UTC.
	CreatedAt time.Time
	// Status is the deployment's lifecycle state (see StatusState).
	Status StatusState
}

// StatusState is the lifecycle state of a deployment, as reported by
// Status and carried on Deployment.Status.
type StatusState string

const (
	// StatusQueued means the deploy has been accepted but is not yet live.
	StatusQueued StatusState = "queued"
	// StatusDeploying means the deploy is in progress.
	StatusDeploying StatusState = "deploying"
	// StatusReady means the deploy is live and serving.
	StatusReady StatusState = "ready"
	// StatusFailed means the deploy did not complete.
	StatusFailed StatusState = "failed"
	// StatusRolledBack means the deploy was superseded by a rollback.
	StatusRolledBack StatusState = "rolled_back"
)

// DeployRequest is the input to Deploy: what to ship and where it came from.
//
// BuildDir is the directory the build produced its deployable output in,
// taken from the starter manifest's build_output. The adapter uploads that
// output; it never rebuilds. The field is deliberately a plain path string —
// resolving it against the manifest and the project root, and enforcing the
// "what was verified is what ships" gate, is later work.
type DeployRequest struct {
	// Project is the project name to deploy. Required.
	Project string
	// Kind is preview or production. Empty is treated as KindPreview by
	// adapters that make the distinction.
	Kind DeploymentKind
	// BuildDir is the directory holding the build output to upload, from
	// the starter manifest's build_output. Required.
	BuildDir string
	// Root is the project root the build ran in. Optional; an adapter that
	// needs a project file (a Workers adapter reading wrangler.toml) uses it,
	// and an adapter that does not ignores it.
	Root string
	// Version identifies the build being shipped (e.g. the starter version
	// or plan revision). Optional; adapters may fill a default.
	Version string
}

// DeployTarget is a destination a project can be shipped to. Adapters are
// small packages that implement it — the fake adapter (FakeTarget) is one,
// a real hosting adapter another.
//
// The interface is shaped for later items to build on: Deploy records a
// deployment and returns it; Status and List read the history; Rollback
// reverts to the previous deployment; PreviewURL resolves a per-deployment
// preview address where the target supports one. Implementations must be
// safe for concurrent use.
type DeployTarget interface {
	// Deploy ships the build described by req and returns the recorded
	// Deployment. It does not rebuild: it uploads the output in
	// req.BuildDir.
	Deploy(req DeployRequest) (Deployment, error)

	// Status returns the current state of a deployment previously returned
	// by Deploy. It returns ErrUnknownDeployment for a deployment this
	// target does not know.
	Status(d Deployment) (StatusState, error)

	// List returns the target's deployment history for a project, oldest
	// first. An unknown project yields an empty slice, not an error.
	List(project string) ([]Deployment, error)

	// Rollback reverts to the deployment preceding d and marks d as rolled
	// back, returning the now-live deployment. It returns
	// ErrNoPreviousDeployment when there is nothing earlier to revert to,
	// and ErrUnknownDeployment when d is unknown.
	Rollback(d Deployment) (Deployment, error)

	// PreviewURL returns the per-deployment preview address for d. It
	// returns ErrPreviewUnsupported when the target has no per-deployment
	// previews, and ErrUnknownDeployment when d is unknown.
	PreviewURL(d Deployment) (string, error)
}
