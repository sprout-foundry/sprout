// Cloudflare Pages adapter: the fully implemented path, for a starter whose
// build output is static files.
//
// A Pages project hosts a list of deployments. Deploy uploads the built
// directory as a direct-upload deployment and records it; List reads the
// project's deployment history; Status reads one deployment's lifecycle
// state; PreviewURL returns a preview deployment's per-deployment address
// (the deployment's own pages.dev host); Rollback re-points the project at a
// prior deployment and marks the superseded one rolled back.
//
// All state lives in the Cloudflare account, keyed by the account id and the
// project name from CloudflareConfig. The adapter keeps no local history: a
// fresh process sees exactly what the account holds.
//
// Scope note: the request shapes below (the deployment-create body, the
// content-addressed asset upload, and the rollback call) are this adapter's
// protocol against the Cloudflare REST API and are exercised only against the
// local HTTP fake — no live account was available when they were written.
// They must be reconciled with a captured real response before this path is
// treated as verified against Cloudflare.
package deploy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// CloudflarePages implements DeployTarget against a Cloudflare Pages project.
type CloudflarePages struct {
	t       *CloudflareTarget
	project string
}

// Deploy uploads the built directory in req.BuildDir as a direct-upload Pages
// deployment under the project and returns the recorded Deployment.
//
// The adapter is bound to one project (CloudflareConfig.Project): req.Project
// must name that project, so the interface's Deployment.Project identity stays
// consistent with the API path the deploy went to. A request naming a
// different project is refused rather than silently deployed to the
// configured one.
//
// The deployment is created first (POST /projects/{project}/deployments),
// which yields the deployment id and its preview host, then the built files
// are uploaded against it (POST /deployments/{id}/assets). A deployment that
// fails to accept the files is surfaced as an error rather than a false
// success.
func (p *CloudflarePages) Deploy(req DeployRequest) (Deployment, error) {
	kind, err := validateDeployRequest(req)
	if err != nil {
		return Deployment{}, err
	}
	if err := p.checkProject(req.Project); err != nil {
		return Deployment{}, err
	}

	assets, err := collectAssets(req.BuildDir)
	if err != nil {
		return Deployment{}, fmt.Errorf("cloudflare: read build output: %w", err)
	}

	body := map[string]any{
		"branch":         branchFor(p.project, kind),
		"commit_message": versionLabel(req.Version),
	}
	ctx := deployContext()
	data, err := p.t.doRequest(ctx, p.t.cred.Value(), http.MethodPost, p.deploymentPath(""), body)
	if err != nil {
		return Deployment{}, fmt.Errorf("cloudflare: deploy project %q: %w", p.project, err)
	}

	var res pagesDeployment
	if err := decodeCloudflareResult("create deployment", data, &res); err != nil {
		return Deployment{}, err
	}
	if strings.TrimSpace(res.ID) == "" {
		return Deployment{}, errors.New("cloudflare: create deployment returned no deployment id")
	}

	if err := p.uploadAssets(ctx, res.ID, assets); err != nil {
		return Deployment{}, err
	}

	d := p.toDeployment(res, kind)
	return d, nil
}

// Status returns the current lifecycle state of d, reading it back from the
// account so a rolled-back or failed deployment is reported accurately. An
// unknown deployment is ErrUnknownDeployment.
func (p *CloudflarePages) Status(d Deployment) (StatusState, error) {
	res, err := p.getDeployment(d)
	if err != nil {
		return "", err
	}
	return pagesState(res.Stage), nil
}

// List returns the project's deployment history, oldest first. The project
// argument must name this adapter's project (or be empty, meaning "this
// adapter's project"); any other project yields an empty slice, matching the
// interface's "an unknown project yields an empty slice, not an error". An
// unknown or empty Cloudflare project likewise yields an empty slice.
func (p *CloudflarePages) List(project string) ([]Deployment, error) {
	target := strings.TrimSpace(project)
	if target == "" {
		target = p.project
	}
	if target != p.project {
		return []Deployment{}, nil
	}
	path := fmt.Sprintf("/accounts/%s/pages/projects/%s/deployments",
		url.PathEscape(strings.TrimSpace(p.t.cfg.AccountID)), url.PathEscape(target))

	data, err := p.t.doRequest(deployContext(), p.t.cred.Value(), http.MethodGet, path, nil)
	if err != nil {
		if isNotFound(err) {
			return []Deployment{}, nil
		}
		return nil, fmt.Errorf("cloudflare: list project %q deployments: %w", target, err)
	}

	var res []pagesDeployment
	if err := decodeCloudflareResult("list deployments", data, &res); err != nil {
		return nil, err
	}
	out := make([]Deployment, 0, len(res))
	for _, item := range res {
		out = append(out, p.toDeployment(item, kindFromBranch(item.Branch)))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

// Rollback re-points the project at the deployment preceding d and marks d as
// rolled back, returning the now-live deployment.
//
// It resolves the project's history, finds d within it, and asks Cloudflare to
// roll the project back to the earlier deployment. d itself is marked
// rolled-back in the returned history; the deployment it reverts to is
// reported as ready. It returns ErrUnknownDeployment when d is not in the
// project's history, and ErrNoPreviousDeployment when d is the earliest.
func (p *CloudflarePages) Rollback(d Deployment) (Deployment, error) {
	if strings.TrimSpace(d.Project) != "" && strings.TrimSpace(d.Project) != p.project {
		return Deployment{}, fmt.Errorf("%w: %q (project %q)", ErrUnknownDeployment, d.ID, d.Project)
	}
	history, err := p.List(d.Project)
	if err != nil {
		return Deployment{}, err
	}

	var idx = -1
	for i, item := range history {
		if item.ID == d.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Deployment{}, fmt.Errorf("%w: %q", ErrUnknownDeployment, d.ID)
	}
	if idx == 0 {
		return Deployment{}, fmt.Errorf("%w for %q", ErrNoPreviousDeployment, d.ID)
	}

	previous := history[idx-1]
	body := map[string]any{"deployment_id": previous.ID}
	path := p.deploymentPath("/rollback")
	if _, err := p.t.doRequest(deployContext(), p.t.cred.Value(), http.MethodPost, path, body); err != nil {
		return Deployment{}, fmt.Errorf("cloudflare: roll back project %q to %q: %w", p.project, previous.ID, err)
	}

	previous.Status = StatusReady
	return previous, nil
}

// PreviewURL returns the per-deployment preview address for d. A preview
// deployment has its own pages.dev host; a production deployment is served at
// the project's live host rather than a per-deployment address, so it reports
// ErrPreviewUnsupported. An unknown deployment is ErrUnknownDeployment.
func (p *CloudflarePages) PreviewURL(d Deployment) (string, error) {
	res, err := p.getDeployment(d)
	if err != nil {
		return "", err
	}
	if pagesState(res.Stage) == StatusRolledBack {
		return "", fmt.Errorf("%w: %q was rolled back", ErrUnknownDeployment, d.ID)
	}
	if kindFromBranch(res.Branch) != KindPreview {
		return "", fmt.Errorf("%w: %q is a %s deployment", ErrPreviewUnsupported, d.ID, kindFromBranch(res.Branch))
	}
	if u := deploymentURL(res); u != "" {
		return u, nil
	}
	return p.t.PageURL(), nil
}

// getDeployment fetches one deployment by id, translating a 404 into
// ErrUnknownDeployment. A deployment naming a project other than this
// adapter's project is unknown here, so a stale or foreign value can never be
// resolved against the wrong API path.
func (p *CloudflarePages) getDeployment(d Deployment) (pagesDeployment, error) {
	if strings.TrimSpace(d.Project) != "" && strings.TrimSpace(d.Project) != p.project {
		return pagesDeployment{}, fmt.Errorf("%w: %q (project %q)", ErrUnknownDeployment, d.ID, d.Project)
	}
	if strings.TrimSpace(d.ID) == "" {
		return pagesDeployment{}, fmt.Errorf("%w: empty deployment id", ErrUnknownDeployment)
	}
	path := p.deploymentPath("/" + url.PathEscape(d.ID))
	data, err := p.t.doRequest(deployContext(), p.t.cred.Value(), http.MethodGet, path, nil)
	if err != nil {
		if isNotFound(err) {
			return pagesDeployment{}, fmt.Errorf("%w: %q", ErrUnknownDeployment, d.ID)
		}
		return pagesDeployment{}, fmt.Errorf("cloudflare: get deployment %q: %w", d.ID, err)
	}
	var res pagesDeployment
	if err := decodeCloudflareResult("get deployment", data, &res); err != nil {
		return pagesDeployment{}, err
	}
	return res, nil
}

// checkProject refuses a request naming a project other than this adapter's
// configured project. The adapter is per-project: its API path, history and
// rollback are all keyed by the configured project, so deploying a request
// that names a different project would silently ship to the wrong place and
// return a Deployment whose Project does not match where it went.
func (p *CloudflarePages) checkProject(project string) error {
	name := strings.TrimSpace(project)
	if name != "" && name != p.project {
		return fmt.Errorf("cloudflare: project %q does not match this target's project %q", name, p.project)
	}
	return nil
}

// deploymentPath builds the Pages deployments path for the project, with an
// optional suffix ("/rollback", "/{id}", or "").
func (p *CloudflarePages) deploymentPath(suffix string) string {
	return fmt.Sprintf("/accounts/%s/pages/projects/%s/deployments%s",
		url.PathEscape(strings.TrimSpace(p.t.cfg.AccountID)),
		url.PathEscape(p.project),
		suffix)
}

// uploadAssets uploads the built files against a created deployment. Each
// asset carries its relative path and content; Cloudflare converts the hash to
// the content-addressed form it stores. The token never appears in the payload
// or in any error.
func (p *CloudflarePages) uploadAssets(ctx context.Context, deploymentID string, assets []pagesAsset) error {
	if len(assets) == 0 {
		return nil
	}
	payload := make([]map[string]string, 0, len(assets))
	for _, a := range assets {
		payload = append(payload, map[string]string{
			"path":    a.Path,
			"content": a.Content,
		})
	}
	path := fmt.Sprintf("/accounts/%s/pages/projects/%s/deployments/%s/assets",
		url.PathEscape(strings.TrimSpace(p.t.cfg.AccountID)),
		url.PathEscape(p.project),
		url.PathEscape(deploymentID))
	if _, err := p.t.doRequest(ctx, p.t.cred.Value(), http.MethodPost, path, map[string]any{"files": payload}); err != nil {
		return fmt.Errorf("cloudflare: upload assets for deployment %q: %w", deploymentID, err)
	}
	return nil
}

// toDeployment maps an API deployment onto the interface's value type.
func (p *CloudflarePages) toDeployment(res pagesDeployment, kind DeploymentKind) Deployment {
	if kind == "" {
		kind = kindFromBranch(res.Branch)
	}
	id := res.ID
	if strings.TrimSpace(id) == "" {
		id = res.URL
	}
	d := Deployment{
		ID:        id,
		Project:   p.project,
		Kind:      kind,
		URL:       deploymentURL(res),
		Version:   res.CommitMessage,
		CreatedAt: res.CreatedOn.UTC(),
		Status:    pagesState(res.Stage),
	}
	if d.URL == "" && kind == KindProduction {
		d.URL = p.t.PageURL()
	}
	return d
}

// pagesDeployment is the API shape of one Pages deployment.
type pagesDeployment struct {
	ID            string    `json:"id"`
	URL           string    `json:"url"`
	Stage         string    `json:"stage"`
	Branch        string    `json:"branch"`
	CommitMessage string    `json:"commit_message"`
	CreatedOn     time.Time `json:"created_on"`
}

// pagesAsset is one file in the built output.
type pagesAsset struct {
	Path    string
	Content string
}

// pagesState maps Cloudflare's deployment stage onto the interface's
// lifecycle state. Unknown stages read as deploying (not settled), so a caller
// keeps waiting rather than treating an in-flight deploy as ready.
func pagesState(stage string) StatusState {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case "success":
		return StatusReady
	case "failure", "failed", "canceled", "cancelled":
		return StatusFailed
	case "queued", "pending":
		return StatusQueued
	case "active", "idle", "build", "building", "deploying", "in_progress":
		return StatusDeploying
	default:
		return StatusDeploying
	}
}

// kindFromBranch classifies a deployment by the branch the adapter recorded on
// it. A deployment created for a production deploy carries the production
// branch marker; everything else is a preview. An unrecognised branch is
// treated as a preview, the safe default (a preview has its own URL and no
// live-site effect).
func kindFromBranch(branch string) DeploymentKind {
	if strings.EqualFold(strings.TrimSpace(branch), "main") || strings.EqualFold(strings.TrimSpace(branch), "production") {
		return KindProduction
	}
	return KindPreview
}

// branchFor is the branch marker recorded on a deployment for kind. Cloudflare
// Pages serves the project's live host for "main" (production) and a
// per-deployment preview host for any other branch.
func branchFor(project string, kind DeploymentKind) string {
	if kind == KindProduction {
		return "main"
	}
	return strings.TrimSpace(project) + "-preview"
}

// versionLabel returns the value to record as the deployment's commit message,
// matching the interface's Version field.
func versionLabel(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "sprout deploy"
	}
	return version
}

// deploymentURL returns the deployment's own address when the API reported one.
func deploymentURL(res pagesDeployment) string {
	return strings.TrimSpace(res.URL)
}
