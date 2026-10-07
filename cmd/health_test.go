//go:build !js

package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/health"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

func TestHealthCmd_Registered(t *testing.T) {
	if healthCmd == nil {
		t.Fatal("healthCmd should not be nil")
	}
	if healthCmd.Use == "" || healthCmd.Short == "" {
		t.Error("healthCmd must have Use and Short")
	}
	for _, flag := range []string{"dir", "max-file-lines", "max-complexity", "no-checks", "json"} {
		if healthCmd.Flags().Lookup(flag) == nil {
			t.Errorf("healthCmd should have --%s", flag)
		}
	}
}

// writeHealthFile writes content at root/rel, creating parent dirs.
func writeHealthFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// healthFixture builds a fixture project: an over-long file, an over-complex
// function, and a starter manifest whose build command passes and whose test
// command fails.
func healthFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	var big strings.Builder
	for i := 0; i < 60; i++ {
		big.WriteString("// filler line\n")
	}
	writeHealthFile(t, root, "big.go", big.String())

	writeHealthFile(t, root, "hot.go", `package x

func Hot(xs []int) int {
	n := 0
	for _, x := range xs {
		if x > 0 {
			if x%2 == 0 {
				n++
			}
		}
	}
	return n
}
`)

	manifest := `{
  "starter": {"id": "fixture", "version": "0.1.0"},
  "build": "true",
  "test": "false"
}`
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".sprout"), 0o755))
	require.NoError(t, os.WriteFile(starterstore.StarterManifestPath(root), []byte(manifest), 0o644))

	return root
}

func newHealthTestCmd(t *testing.T) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	cmd.SetContext(t.Context())
	return cmd, &buf
}

// TestRunHealth_TextFindings drives the command against the fixture and
// asserts the size, complexity, and failing-check findings all surface with
// their proposed fixes.
func TestRunHealth_TextFindings(t *testing.T) {
	root := healthFixture(t)
	cmd, buf := newHealthTestCmd(t)

	err := runHealth(cmd, root, healthFlags{maxFileLines: 10, maxComplexity: 3}, buf)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "big.go")
	assert.Contains(t, out, "split big.go")
	assert.Contains(t, out, "proposed fix")
	assert.Contains(t, out, "Hot")
	// The failing test check surfaces, the passing build does not.
	assert.Contains(t, out, "test check failed")
	assert.NotContains(t, out, "build check failed")
	assert.Contains(t, out, "Checks:")
	assert.Contains(t, out, "test: failed")
}

// TestRunHealth_JSONFindings drives the JSON path and asserts the structured
// finding model — a kind, severity, and a proposed fix per finding.
func TestRunHealth_JSONFindings(t *testing.T) {
	root := healthFixture(t)
	cmd, buf := newHealthTestCmd(t)

	err := runHealth(cmd, root, healthFlags{maxFileLines: 10, maxComplexity: 3, jsonOut: true}, buf)
	require.NoError(t, err)

	var report health.Report
	require.NoError(t, json.Unmarshal(buf.Bytes(), &report))
	assert.Equal(t, root, report.Root)
	assert.Positive(t, report.FilesScanned)

	kinds := map[health.FindingKind]int{}
	for _, f := range report.Findings {
		kinds[f.Kind]++
		assert.NotEmpty(t, f.Target, "every finding names a target")
		assert.NotNil(t, f.Fix, "every finding proposes a fix")
	}
	assert.Positive(t, kinds[health.KindFileSize])
	assert.Positive(t, kinds[health.KindComplexity])
	assert.Equal(t, 1, kinds[health.KindCheck])
}

// TestRunHealth_NoChecksSkippedButCleanTree pins the --no-checks path: no
// build/test run and no check findings, on a tree with nothing to fix.
func TestRunHealth_NoChecksSkippedButCleanTree(t *testing.T) {
	root := t.TempDir()
	writeHealthFile(t, root, "small.go", "package x\n\nfunc A() int { return 1 }\n")
	cmd, buf := newHealthTestCmd(t)

	err := runHealth(cmd, root, healthFlags{noChecks: true}, buf)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "No health findings")
	assert.NotContains(t, out, "Checks:")
}

// TestRunHealth_AbsoluteRoot pins that the reported root is resolved to an
// absolute path even when the caller passes a relative one.
func TestRunHealth_AbsoluteRoot(t *testing.T) {
	root := t.TempDir()
	writeHealthFile(t, root, "big.go", strings.Repeat("// x\n", 40))
	cmd, buf := newHealthTestCmd(t)

	err := runHealth(cmd, root, healthFlags{maxFileLines: 5, noChecks: true, jsonOut: true}, buf)
	require.NoError(t, err)

	var report health.Report
	require.NoError(t, json.Unmarshal(buf.Bytes(), &report))
	assert.Equal(t, root, report.Root, "the reported root is absolute")
}
