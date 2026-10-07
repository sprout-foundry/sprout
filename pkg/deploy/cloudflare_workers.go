// Cloudflare Workers adapter: the server-side path, for a starter whose build
// output is a Worker script rather than static files.
//
// Workers has a fundamentally different shape from Pages: a Worker has one
// live script per name, not a list of deployments, so there is no per-
// deployment history, no per-deployment preview address, and no multi-step
// rollback in the account. This adapter therefore implements Deploy (upload
// the script read from a file in the build output), Status, and List (a
// single-entry history for the worker), and reports ErrPreviewUnsupported for
// PreviewURL and ErrNoPreviousDeployment for Rollback — each an accurate
// statement of the target's capability rather than an invented one.
//
// Deployment history with rollback for server-side starters is future work
// (Cloudflare Workers versions/deployments); until then a Worker deploy is a
// single live script and the interface's history-dependent methods say so.
package deploy

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CloudflareWorkers implements DeployTarget against a Cloudflare Worker.
type CloudflareWorkers struct {
	t      *CloudflareTarget
	worker string
}

// defaultWorkerEntryFile is the script file the adapter looks for in the build
// output when the request names no entry point. It matches the conventional
// Bundle build output (a bundled worker module).
const defaultWorkerEntryFile = "worker.js"

// Deploy uploads the Worker script from req.BuildDir and returns the recorded
// Deployment. The script file is defaultWorkerEntryFile (or "index.js" as a
// second convention); a build directory with neither is refused.
//
// A Worker has no per-deployment identity, so the returned Deployment's ID is
// the worker name and its URL is the standard workers.dev preview host. The
// deployment is immediately live for the worker name (Workers scripts are
// uploaded per name, not per branch).
func (w *CloudflareWorkers) Deploy(req DeployRequest) (Deployment, error) {
	if _, err := validateDeployRequest(req); err != nil {
		return Deployment{}, err
	}
	if name := strings.TrimSpace(req.Project); name != "" && name != w.worker {
		return Deployment{}, fmt.Errorf("cloudflare: project %q does not match this target's worker %q", name, w.worker)
	}

	script, err := readWorkerScript(req.BuildDir)
	if err != nil {
		return Deployment{}, err
	}

	path := fmt.Sprintf("/accounts/%s/workers/scripts/%s",
		url.PathEscape(strings.TrimSpace(w.t.cfg.AccountID)), url.PathEscape(w.worker))
	if _, err := w.t.doRawRequest(deployContext(), w.t.cred.Value(), http.MethodPut, path,
		strings.NewReader(script), "application/javascript+module"); err != nil {
		return Deployment{}, fmt.Errorf("cloudflare: upload worker %q: %w", w.worker, err)
	}

	return Deployment{
		ID:        w.worker,
		Project:   w.worker,
		Kind:      KindProduction,
		URL:       fmt.Sprintf("https://%s.%s.workers.dev", w.worker, strings.TrimSpace(w.t.cfg.AccountID)),
		Version:   versionLabel(req.Version),
		CreatedAt: time.Now().UTC(),
		Status:    StatusReady,
	}, nil
}

// Status reports the Worker as ready: a Workers script is live once uploaded.
// A deployment whose id does not name this worker is unknown.
func (w *CloudflareWorkers) Status(d Deployment) (StatusState, error) {
	if !w.matches(d) {
		return "", fmt.Errorf("%w: %q", ErrUnknownDeployment, d.ID)
	}
	return StatusReady, nil
}

// List returns the worker's single-entry history (oldest first) when the
// project names this worker; an unknown project yields an empty slice.
func (w *CloudflareWorkers) List(project string) ([]Deployment, error) {
	name := strings.TrimSpace(project)
	if name != "" && name != w.worker {
		return []Deployment{}, nil
	}
	return []Deployment{{
		ID:      w.worker,
		Project: w.worker,
		Kind:    KindProduction,
		URL:     fmt.Sprintf("https://%s.%s.workers.dev", w.worker, strings.TrimSpace(w.t.cfg.AccountID)),
		Status:  StatusReady,
	}}, nil
}

// PreviewURL reports ErrPreviewUnsupported: a Worker serves one live script
// and has no per-deployment preview address.
func (w *CloudflareWorkers) PreviewURL(d Deployment) (string, error) {
	if !w.matches(d) {
		return "", fmt.Errorf("%w: %q", ErrUnknownDeployment, d.ID)
	}
	return "", fmt.Errorf("%w: worker %q has no per-deployment previews", ErrPreviewUnsupported, w.worker)
}

// Rollback reports ErrNoPreviousDeployment: a Worker has a single live script,
// so there is no earlier deployment to restore. An unknown deployment is
// ErrUnknownDeployment.
func (w *CloudflareWorkers) Rollback(d Deployment) (Deployment, error) {
	if !w.matches(d) {
		return Deployment{}, fmt.Errorf("%w: %q", ErrUnknownDeployment, d.ID)
	}
	return Deployment{}, fmt.Errorf("%w for worker %q", ErrNoPreviousDeployment, w.worker)
}

// matches reports whether d names this worker (by id or project).
func (w *CloudflareWorkers) matches(d Deployment) bool {
	id := strings.TrimSpace(d.ID)
	return id != "" && (id == w.worker || strings.TrimSpace(d.Project) == w.worker)
}

// readWorkerScript locates and reads the worker script in the build output. It
// tries the conventional names before failing.
func readWorkerScript(buildDir string) (string, error) {
	for _, name := range []string{defaultWorkerEntryFile, "index.js"} {
		path := filepath.Join(buildDir, name)
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() {
			content, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("cloudflare: read worker script %s: %w", path, err)
			}
			return string(content), nil
		}
	}
	return "", fmt.Errorf("cloudflare: no worker script in %s (expected %s)", buildDir, defaultWorkerEntryFile)
}
