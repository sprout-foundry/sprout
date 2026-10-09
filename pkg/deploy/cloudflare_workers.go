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
// Deploy also brings the project's declared bindings with it. A Workers
// project declares its D1 databases, KV namespaces and R2 buckets in
// wrangler.toml; the adapter creates or reuses each one and attaches it to the
// uploaded script, and applies pending D1 migrations before the script goes
// live. The script upload — the step that makes the worker live — happens only
// after every binding is resolved, so a deploy never goes live with bindings
// missing.
//
// Deployment history with rollback for server-side starters is future work
// (Cloudflare Workers versions/deployments); until then a Worker deploy is a
// single live script and the interface's history-dependent methods say so.
package deploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
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
// Before the script is uploaded, every binding the project's wrangler.toml
// declares is created or reused and attached to the script (see
// reconcileBindings), and pending D1 migrations are applied. The upload — the
// step that makes the worker live — happens only once all of that has
// succeeded, so a deploy never goes live with a binding missing; a binding
// that cannot be prepared fails the deploy with ErrBindingUnavailable naming
// it.
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

	ctx := deployContext()

	// Resolve the declared bindings before anything is uploaded. A project
	// root is required to find wrangler.toml; without one the adapter cannot
	// know what to attach, so it refuses rather than shipping a script with no
	// bindings.
	root := strings.TrimSpace(req.Root)
	if root == "" {
		return Deployment{}, fmt.Errorf("%w: no project root supplied to read %s", ErrBindingUnavailable, WranglerFileName)
	}
	wrangler, err := LoadWranglerConfig(root)
	if err != nil {
		return Deployment{}, fmt.Errorf("%w: %w", ErrBindingUnavailable, err)
	}
	bindings, err := w.reconcileBindings(ctx, root, wrangler)
	if err != nil {
		return Deployment{}, err
	}

	// Apply pending D1 migrations before the script goes live, so the
	// database has the schema the code expects. A migration that cannot be
	// applied fails the deploy rather than going live against a partial
	// schema.
	for _, db := range bindings.D1 {
		dir := db.MigrationsDir
		if dir != "" && !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		if _, err := w.applyD1Migrations(ctx, db.DatabaseID, dir); err != nil {
			return Deployment{}, fmt.Errorf("%w: d1 migrations for %q: %w", ErrBindingUnavailable, db.StableName, err)
		}
	}

	// Only now upload the script, attaching every resolved binding. The upload
	// is the step that makes the worker live.
	body, contentType, err := workerUploadBody(script, wrangler.Main, bindings.Attach)
	if err != nil {
		return Deployment{}, err
	}
	path := fmt.Sprintf("/accounts/%s/workers/scripts/%s",
		url.PathEscape(strings.TrimSpace(w.t.cfg.AccountID)), url.PathEscape(w.worker))
	if _, err := w.t.doRawRequest(ctx, w.t.cred.Value(), http.MethodPut, path, body, contentType); err != nil {
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

// workerUploadBody builds the multipart body for a Worker script upload: a
// "metadata" part carrying the module entry point and the bindings to attach,
// and a "script" part carrying the script itself. Cloudflare reads the
// bindings from the metadata part, which is how a D1/KV/R2 binding reaches the
// deployed script. The token is not part of the body.
func workerUploadBody(script, main string, bindings []workerBinding) (io.Reader, string, error) {
	metadata, err := json.Marshal(map[string]any{
		"main_module": strings.TrimSpace(main),
		"bindings":    bindings,
	})
	if err != nil {
		return nil, "", fmt.Errorf("cloudflare: encode worker metadata: %w", err)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	metaHeader := textproto.MIMEHeader{}
	metaHeader.Set("Content-Disposition", `form-data; name="metadata"`)
	metaHeader.Set("Content-Type", "application/json")
	metaPart, err := mw.CreatePart(metaHeader)
	if err != nil {
		return nil, "", fmt.Errorf("cloudflare: build worker upload: %w", err)
	}
	if _, err := metaPart.Write(metadata); err != nil {
		return nil, "", fmt.Errorf("cloudflare: build worker upload: %w", err)
	}

	scriptPart, err := mw.CreateFormFile("script", defaultWorkerEntryFile)
	if err != nil {
		return nil, "", fmt.Errorf("cloudflare: build worker upload: %w", err)
	}
	if _, err := io.WriteString(scriptPart, script); err != nil {
		return nil, "", fmt.Errorf("cloudflare: build worker upload: %w", err)
	}

	if err := mw.Close(); err != nil {
		return nil, "", fmt.Errorf("cloudflare: build worker upload: %w", err)
	}
	return bytes.NewReader(buf.Bytes()), mw.FormDataContentType(), nil
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
