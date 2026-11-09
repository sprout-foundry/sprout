//go:build !js && !windows

package appcheck

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// serveApp stages the build output in a scratch directory (so the check never
// touches the project's local data), starts the server on a free port, and
// waits until it answers. The returned stop function always shuts the server
// down and removes the scratch directory.
func serveApp(ctx context.Context, opts serveOptions) (*hostedApp, func(), error) {
	hosted, err := os.MkdirTemp("", "app-check-")
	if err != nil {
		return nil, nil, fmt.Errorf("create scratch directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(hosted) }

	site := filepath.Join(hosted, "site")
	if err := os.MkdirAll(site, 0o755); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("create site directory: %w", err)
	}
	if err := copySite(opts.Dist, site); err != nil {
		cleanup()
		return nil, nil, err
	}

	toml := readToml(opts.Root)
	workerPath := filepath.Join(opts.Dist, "worker.js")
	workerInfo, workerErr := os.Stat(workerPath)
	hasWorker := workerErr == nil && !workerInfo.IsDir() && tomlHasMainRe.MatchString(toml)

	port, err := freePort()
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	app := &hostedApp{BaseURL: fmt.Sprintf("http://127.0.0.1:%d", port), HasServer: hasWorker}

	if hasWorker && opts.Wrangler != "" {
		stop, err := startWrangler(ctx, opts, hosted, toml, port, app)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		return app, func() { stop(); cleanup() }, nil
	}

	server := staticServer(site, notFoundHandling(toml, site), port)
	if err := waitFor(app.BaseURL+"/", serverStartTimeout); err != nil {
		_ = server.Close()
		cleanup()
		return nil, nil, fmt.Errorf("the static server did not start: %w", err)
	}
	return app, func() {
		_ = server.Close()
		cleanup()
	}, nil
}

// startWrangler applies the database migrations to a scratch state, then
// starts wrangler dev on the platform's settings. A failed migration is
// recorded in the log (the report turns it into a finding), not returned:
// the app is still worth using without its tables.
func startWrangler(ctx context.Context, opts serveOptions, hosted, toml string, port int, app *hostedApp) (func(), error) {
	if err := os.WriteFile(filepath.Join(hosted, "worker.js"), mustRead(filepath.Join(opts.Dist, "worker.js")), 0o644); err != nil {
		return nil, fmt.Errorf("stage worker: %w", err)
	}
	if err := os.WriteFile(filepath.Join(hosted, "wrangler.toml"), []byte(hostedToml(toml)), 0o644); err != nil {
		return nil, fmt.Errorf("stage wrangler.toml: %w", err)
	}
	state := filepath.Join(hosted, "state")
	migrations := filepath.Join(hosted, "migrations")
	distMigrations := filepath.Join(opts.Dist, "_migrations")
	if info, err := os.Stat(distMigrations); err == nil && info.IsDir() {
		if err := os.CopyFS(migrations, os.DirFS(distMigrations)); err != nil { //nolint:gosec // G305: the project's own build output
			return nil, fmt.Errorf("stage migrations: %w", err)
		}
	}

	app.log = &logSink{}
	for _, m := range tomlDatabaseRe.FindAllStringSubmatch(toml, -1) {
		name := m[1]
		if err := os.MkdirAll(migrations, 0o755); err != nil {
			return nil, fmt.Errorf("stage migrations: %w", err)
		}
		migrate := exec.CommandContext(ctx, opts.Wrangler, //nolint:gosec // G204: the project's own wrangler
			"d1", "migrations", "apply", name, "--local", "--persist-to", state)
		migrate.Dir = hosted
		migrate.Env = wranglerEnv()
		migrate.Stdin = strings.NewReader("y\n")
		if out, err := migrate.CombinedOutput(); err != nil {
			app.log.append(fmt.Sprintf("migrations for %s failed:\n%s%s\n", name, out, err))
		}
	}

	proc, err := startHostProcess(opts.Wrangler, []string{
		"dev", "--local", "--port", strconv.Itoa(port), "--ip", "127.0.0.1", "--persist-to", state,
	}, hosted, wranglerEnv(), app.log)
	if err != nil {
		return nil, fmt.Errorf("start wrangler: %w", err)
	}
	stop := func() {
		proc.stop(stopTimeout)
		proc.wait()
	}
	if err := waitFor(app.BaseURL+"/", serverStartTimeout); err != nil {
		output := app.Log()
		stop()
		if output == "" {
			return nil, fmt.Errorf("wrangler dev did not start: %w", err)
		}
		return nil, fmt.Errorf("wrangler dev did not start:\n%s", tail(output, 1500))
	}
	return stop, nil
}
