//go:build !js

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/health"
)

// healthDepsTimeout bounds the live dependency lookup. Module discovery can
// touch the network (the module proxy), so it gets its own ceiling rather than
// the command's overall context.
const healthDepsTimeout = 60 * time.Second

// healthProxyChecker resolves the latest available version of each module by
// asking the Go toolchain (`go list -m -u`), which consults the module proxy
// the project is already configured to use. It is the live implementation of
// the dependency seam: pointing it at the toolchain keeps the health check's
// notion of "outdated" the same one `go get` acts on, and the network-free
// tests inject a fixture checker instead.
type healthProxyChecker struct{}

// LatestVersions runs `go list -m -u -json all` in root and returns module
// path -> latest version for every module the toolchain reports an update for.
// A module with no update is absent from the map. When the toolchain is absent
// or the lookup fails, the error is returned so the report records it as a
// run-level note rather than silently claiming everything is current.
func (healthProxyChecker) LatestVersions(ctx context.Context, root string) (map[string]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, healthDepsTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "list", "-m", "-u", "-json", "all") //nolint:gosec // the arguments are fixed; running the module toolchain is the mechanism by design
	cmd.Dir = root
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("go list -m -u: %s", msg)
	}

	return parseGoListUpdates(out.Bytes())
}

// goListModule is the subset of `go list -m -u -json` output the checker reads.
type goListModule struct {
	Path    string
	Version string
	Update  *struct {
		Path    string
		Version string
	}
}

// parseGoListUpdates decodes the concatenated JSON objects `go list -m -json
// all` emits and returns module path -> latest version for each module that
// carries an Update. Modules without an update are skipped.
func parseGoListUpdates(data []byte) (map[string]string, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	out := map[string]string{}
	for dec.More() {
		var m goListModule
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("parse go list output: %w", err)
		}
		if m.Update != nil && m.Update.Version != "" && m.Path != "" {
			out[m.Path] = m.Update.Version
		}
	}
	return out, nil
}

// compile-time assertion that the live checker satisfies the seam.
var _ health.DependencyChecker = healthProxyChecker{}
