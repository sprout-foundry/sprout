// Binding reconciliation for the Cloudflare Workers adapter: create what the
// project declares, reuse what already exists, and never go live with a
// binding missing.
//
// Each declared binding is resolved to a real account resource before the
// script is uploaded, so the uploaded script carries the ids it needs. The
// resolution is find-or-create: the per-project state file is consulted first
// (a re-run reuses what the first deploy created), then the account is queried
// by the stable resource name, and only then is a new resource created. The
// real D1 id is written back into wrangler.toml so local and deployed config
// agree.
//
// Scope note: the create/list request shapes and the script-upload metadata
// below are this adapter's protocol against the Cloudflare REST API and are
// exercised only against the local HTTP fake — no live account was available
// when they were written. They must be reconciled with a captured real
// response before this path is treated as verified against Cloudflare.

package deploy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// ErrBindingUnavailable is the typed error a deploy fails with when a declared
// binding cannot be created or attached. It is returned before the script is
// uploaded, so a deploy never goes live with bindings missing.
var ErrBindingUnavailable = errors.New("cloudflare: a declared binding could not be prepared")

// resolvedBindings is the outcome of binding reconciliation: the Cloudflare
// bindings to attach to the uploaded script, plus the D1 databases (by stable
// name) whose migrations must be applied before the script goes live.
type resolvedBindings struct {
	// Attach are the binding entries for the script upload metadata.
	Attach []workerBinding
	// D1 maps a stable resource name to its database id and migrations
	// directory, for the migration step.
	D1 []resolvedD1
}

// resolvedD1 is one resolved D1 database.
type resolvedD1 struct {
	StableName    string
	DatabaseID    string
	MigrationsDir string
}

// workerBinding is one binding entry in the script upload metadata. Its JSON
// shape matches the Cloudflare Workers script metadata (a flat object with a
// type discriminator and the resource id under a type-specific key).
type workerBinding struct {
	Type          string `json:"type"`
	Name          string `json:"name"`
	ID            string `json:"id,omitempty"`
	DatabaseID    string `json:"database_id,omitempty"`
	BucketName    string `json:"bucket_name,omitempty"`
	MigrationsDir string `json:"-"`
}

// reconcileBindings resolves every binding the wrangler config declares,
// writing created resources into state and the real D1 id back into
// wrangler.toml. It returns the bindings to attach or, on the first binding it
// cannot prepare, ErrBindingUnavailable naming it — in which case nothing has
// been uploaded.
func (w *CloudflareWorkers) reconcileBindings(ctx context.Context, root string, cfg *WranglerConfig) (resolvedBindings, error) {
	state, err := LoadDeployResourceState(root)
	if err != nil {
		return resolvedBindings{}, err
	}
	stateChanged := false

	var out resolvedBindings

	for i := range cfg.D1Databases {
		d1 := cfg.D1Databases[i]
		binding := strings.TrimSpace(d1.Binding)
		if binding == "" {
			return resolvedBindings{}, fmt.Errorf("%w: a d1_databases block has no binding name", ErrBindingUnavailable)
		}
		stable := stableResourceName(cfg.Name, "d1-"+binding)

		id := strings.TrimSpace(state.D1[stable])
		if id == "" {
			if !isPlaceholderD1ID(d1.DatabaseID) {
				// The file already carries a real id: adopt it.
				id = strings.TrimSpace(d1.DatabaseID)
			} else {
				// Find or create under the stable name, never the display
				// name: two projects declaring the same database_name must
				// not adopt each other's database.
				id, err = w.findOrCreateD1(ctx, stable)
				if err != nil {
					return resolvedBindings{}, fmt.Errorf("%w: d1 database %q: %w", ErrBindingUnavailable, binding, err)
				}
			}
			state.D1[stable] = id
			stateChanged = true
		}

		if isPlaceholderD1ID(d1.DatabaseID) || strings.TrimSpace(d1.DatabaseID) != id {
			if err := writeD1IDIntoWrangler(root, binding, id); err != nil {
				return resolvedBindings{}, fmt.Errorf("%w: write database_id for binding %q: %w", ErrBindingUnavailable, binding, err)
			}
		}

		out.Attach = append(out.Attach, workerBinding{Type: "d1", Name: binding, ID: id})
		out.D1 = append(out.D1, resolvedD1{
			StableName:    stable,
			DatabaseID:    id,
			MigrationsDir: strings.TrimSpace(d1.MigrationsDir),
		})
	}

	for i := range cfg.KVNamespaces {
		kv := cfg.KVNamespaces[i]
		binding := strings.TrimSpace(kv.Binding)
		if binding == "" {
			return resolvedBindings{}, fmt.Errorf("%w: a kv_namespaces block has no binding name", ErrBindingUnavailable)
		}
		stable := stableResourceName(cfg.Name, "kv-"+binding)
		id := strings.TrimSpace(state.KV[stable])
		if id == "" {
			if existing := strings.TrimSpace(kv.ID); isRealKVID(existing) {
				id = existing
			} else {
				id, err = w.findOrCreateKV(ctx, stable)
				if err != nil {
					return resolvedBindings{}, fmt.Errorf("%w: kv namespace %q: %w", ErrBindingUnavailable, binding, err)
				}
			}
			state.KV[stable] = id
			stateChanged = true
		}
		out.Attach = append(out.Attach, workerBinding{Type: "kv_namespace", Name: binding, ID: id})
	}

	for i := range cfg.R2Buckets {
		r2 := cfg.R2Buckets[i]
		binding := strings.TrimSpace(r2.Binding)
		if binding == "" {
			return resolvedBindings{}, fmt.Errorf("%w: a r2_buckets block has no binding name", ErrBindingUnavailable)
		}
		stable := stableResourceName(cfg.Name, "r2-"+binding)
		name := strings.TrimSpace(r2.BucketName)
		if name == "" {
			name = stable
		}
		bucket := strings.TrimSpace(state.R2[stable])
		if bucket == "" {
			bucket, err = w.findOrCreateR2(ctx, name)
			if err != nil {
				return resolvedBindings{}, fmt.Errorf("%w: r2 bucket %q: %w", ErrBindingUnavailable, binding, err)
			}
			state.R2[stable] = bucket
			stateChanged = true
		}
		out.Attach = append(out.Attach, workerBinding{Type: "r2_bucket", Name: binding, BucketName: bucket})
	}

	if stateChanged {
		if err := state.save(root); err != nil {
			return resolvedBindings{}, err
		}
	}
	return out, nil
}

// findOrCreateD1 returns the id of the D1 database named name, creating it
// when the account has none. The name is the stable per-worker resource name,
// so a state-less rerun cannot adopt another project's database.
func (w *CloudflareWorkers) findOrCreateD1(ctx context.Context, name string) (string, error) {
	if id, ok, err := w.findD1ByName(ctx, name); err != nil {
		return "", err
	} else if ok {
		return id, nil
	}
	path := fmt.Sprintf("/accounts/%s/d1/database", url.PathEscape(strings.TrimSpace(w.t.cfg.AccountID)))
	data, err := w.t.doRequest(ctx, w.t.cred.Value(), http.MethodPost, path, map[string]any{"name": name})
	if err != nil {
		return "", err
	}
	var res struct {
		UUID string `json:"uuid"`
		ID   string `json:"id"`
	}
	if err := decodeCloudflareResult("create d1 database", data, &res); err != nil {
		return "", err
	}
	id := strings.TrimSpace(res.UUID)
	if id == "" {
		id = strings.TrimSpace(res.ID)
	}
	if id == "" {
		return "", fmt.Errorf("create d1 database returned no id for %q", name)
	}
	return id, nil
}

// findD1ByName looks the database up in the account by name, returning its id
// when the account already holds one.
func (w *CloudflareWorkers) findD1ByName(ctx context.Context, name string) (string, bool, error) {
	path := fmt.Sprintf("/accounts/%s/d1/database", url.PathEscape(strings.TrimSpace(w.t.cfg.AccountID)))
	data, err := w.t.doRequest(ctx, w.t.cred.Value(), http.MethodGet, path, nil)
	if err != nil {
		if isNotFound(err) {
			return "", false, nil
		}
		return "", false, err
	}
	var res []struct {
		UUID string `json:"uuid"`
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := decodeCloudflareResult("list d1 databases", data, &res); err != nil {
		return "", false, err
	}
	for _, db := range res {
		if strings.EqualFold(strings.TrimSpace(db.Name), name) {
			id := strings.TrimSpace(db.UUID)
			if id == "" {
				id = strings.TrimSpace(db.ID)
			}
			if id != "" {
				return id, true, nil
			}
		}
	}
	return "", false, nil
}

// findOrCreateKV returns the id of the KV namespace named name, creating it
// when the account has none.
func (w *CloudflareWorkers) findOrCreateKV(ctx context.Context, name string) (string, error) {
	if id, ok, err := w.findKVByName(ctx, name); err != nil {
		return "", err
	} else if ok {
		return id, nil
	}
	path := fmt.Sprintf("/accounts/%s/storage/kv/namespaces", url.PathEscape(strings.TrimSpace(w.t.cfg.AccountID)))
	data, err := w.t.doRequest(ctx, w.t.cred.Value(), http.MethodPost, path, map[string]any{"title": name})
	if err != nil {
		return "", err
	}
	var res struct {
		ID string `json:"id"`
	}
	if err := decodeCloudflareResult("create kv namespace", data, &res); err != nil {
		return "", err
	}
	if strings.TrimSpace(res.ID) == "" {
		return "", fmt.Errorf("create kv namespace returned no id for %q", name)
	}
	return strings.TrimSpace(res.ID), nil
}

// findKVByName looks the namespace up in the account by title.
func (w *CloudflareWorkers) findKVByName(ctx context.Context, name string) (string, bool, error) {
	path := fmt.Sprintf("/accounts/%s/storage/kv/namespaces", url.PathEscape(strings.TrimSpace(w.t.cfg.AccountID)))
	data, err := w.t.doRequest(ctx, w.t.cred.Value(), http.MethodGet, path, nil)
	if err != nil {
		if isNotFound(err) {
			return "", false, nil
		}
		return "", false, err
	}
	var res []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := decodeCloudflareResult("list kv namespaces", data, &res); err != nil {
		return "", false, err
	}
	for _, ns := range res {
		if strings.EqualFold(strings.TrimSpace(ns.Title), name) && strings.TrimSpace(ns.ID) != "" {
			return strings.TrimSpace(ns.ID), true, nil
		}
	}
	return "", false, nil
}

// findOrCreateR2 returns the name of the R2 bucket, creating it when the
// account has none.
func (w *CloudflareWorkers) findOrCreateR2(ctx context.Context, name string) (string, error) {
	if ok, err := w.findR2ByName(ctx, name); err != nil {
		return "", err
	} else if ok {
		return name, nil
	}
	path := fmt.Sprintf("/accounts/%s/r2/buckets", url.PathEscape(strings.TrimSpace(w.t.cfg.AccountID)))
	if _, err := w.t.doRequest(ctx, w.t.cred.Value(), http.MethodPost, path, map[string]any{"name": name}); err != nil {
		return "", err
	}
	return name, nil
}

// findR2ByName reports whether the account already holds the bucket.
func (w *CloudflareWorkers) findR2ByName(ctx context.Context, name string) (bool, error) {
	path := fmt.Sprintf("/accounts/%s/r2/buckets", url.PathEscape(strings.TrimSpace(w.t.cfg.AccountID)))
	data, err := w.t.doRequest(ctx, w.t.cred.Value(), http.MethodGet, path, nil)
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	var res struct {
		Buckets []struct {
			Name string `json:"name"`
		} `json:"buckets"`
	}
	if err := decodeCloudflareResult("list r2 buckets", data, &res); err != nil {
		return false, err
	}
	for _, b := range res.Buckets {
		if strings.EqualFold(strings.TrimSpace(b.Name), name) {
			return true, nil
		}
	}
	return false, nil
}

// writeD1IDIntoWrangler rewrites the database_id of the [[d1_databases]] block
// whose binding is `binding` in the project's wrangler.toml, so local and
// deployed config agree. It edits only that block's database_id line, leaving
// the rest of the file (comments included) untouched.
func writeD1IDIntoWrangler(root, binding, id string) error {
	root = strings.TrimSpace(root)
	// G703: read and write through an os.Root scoped to the project root, so
	// a symlink swapped in cannot redirect the write outside the project.
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = rootFS.Close() }()

	data, err := rootFS.ReadFile(WranglerFileName)
	if err != nil {
		return err
	}
	updated, ok := replaceD1DatabaseID(string(data), binding, id)
	if !ok {
		return fmt.Errorf("no d1_databases block with binding %q in %s", binding, WranglerFileName)
	}
	if updated == string(data) {
		return nil
	}
	return rootFS.WriteFile(WranglerFileName, []byte(updated), 0o644)
}

// replaceD1DatabaseID returns content with the database_id line of the
// [[d1_databases]] block whose binding is `binding` set to id. It reports
// whether such a block was found.
func replaceD1DatabaseID(content, binding, id string) (string, bool) {
	lines := strings.Split(content, "\n")
	inBlock := false
	matched := false
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[[") {
			// Entering a new array-of-tables block: only d1_databases blocks
			// matter, and a new one resets the binding match.
			inBlock = strings.HasPrefix(trimmed, "[[d1_databases]]")
			matched = false
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			// A plain table header ends the current array-of-tables block.
			inBlock = false
			matched = false
			continue
		}
		if !inBlock {
			continue
		}
		key, value, ok := splitTomlAssignment(trimmed)
		if !ok {
			continue
		}
		if key == "binding" && unquoteTomlString(value) == binding {
			matched = true
			found = true
		}
		if key == "database_id" && matched {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + "database_id = " + quoteTomlString(id)
		}
	}
	return strings.Join(lines, "\n"), found
}

// splitTomlAssignment splits a TOML key = value line into its key and raw
// value, reporting false for a line that is not an assignment.
func splitTomlAssignment(line string) (key, value string, ok bool) {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	value = strings.TrimSpace(line[idx+1:])
	if key == "" {
		return "", "", false
	}
	// Drop a trailing comment outside a quoted value.
	if !strings.HasPrefix(value, `"`) && !strings.HasPrefix(value, `'`) {
		if c := strings.Index(value, "#"); c >= 0 {
			value = strings.TrimSpace(value[:c])
		}
	}
	return key, value, true
}

// unquoteTomlString removes surrounding quotes from a TOML string value.
func unquoteTomlString(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

// quoteTomlString renders s as a double-quoted TOML string.
func quoteTomlString(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
