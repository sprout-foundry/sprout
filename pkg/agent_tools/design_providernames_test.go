package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// readRepoFile reads a repository-relative file (slash-separated).
func readRepoFile(root, rel string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// formatProprietaryHit renders one failure line in SP-137's grep style.
func formatProprietaryHit(file string, line int, name, text string) string {
	return fmt.Sprintf("%s:%d references proprietary name %q: %s", file, line, name, strings.TrimSpace(text))
}

// TestDesignTierNoProprietaryNames is the pkg/agent_tools half of SP-140-1's
// Acceptance-Criterion grep, mirroring SP-137's TestVisionTierNoProviderNames
// (which lives beside the code it guards). It scans the design_validate
// ToolHandler — the one design-tier Go file that lives in this package — for
// the hardcoded proprietary product names (figma, penpot, sketch, illustrator,
// adobe, photoshop) and fails naming the exact file and line.
//
// The pkg/design half of the tier is covered by
// design.TestDesignTierNoProprietaryNames, and the shared vocabulary and walk
// live in design.ProprietaryNames / design.ScanProviderNames so both halves use
// one list. docs, specs, and fixtures are out of scope.
func TestDesignTierNoProprietaryNames(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err, "resolve repository root")

	const rel = "pkg/agent_tools/design_validate_handler.go"
	data, err := readRepoFile(root, rel)
	require.NoError(t, err, "design_validate handler must be present")

	var problems []string
	for i, line := range strings.Split(data, "\n") {
		lower := strings.ToLower(line)
		for _, name := range design.ProprietaryNames {
			if strings.Contains(lower, name) {
				problems = append(problems, formatProprietaryHit(rel, i+1, name, line))
				break
			}
		}
	}
	assert.Empty(t, problems,
		"design_validate handler must not name a proprietary design tool (SP-140-1 AC)")
}

// TestDesignTierSharedScannerScopesTheHandler pins that the shared scanner's Go
// tier — not merely the direct file read above — reaches
// design_validate_handler.go: on the real repository the shared scanner reports
// no hits, and part of what it inspected was that handler. pkg/design owns the
// seeded-injection proof that the handler is in the walk scope
// (TestDesignTierSharedScannerCoversHandler); this test pins the end-to-end
// clean result through the same shared vocabulary.
func TestDesignTierSharedScannerScopesTheHandler(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	// The handler must be a real file the shared scanner will visit.
	assert.True(t, design.FSScanner{}.HasAnyFile(
		filepath.Join(root, "pkg", "agent_tools", "design_validate_handler.go")),
		"the design_validate handler must exist for the shared scanner to scope")

	findings, present, err := design.ScanProviderNames(root, "go", design.FSScanner{})
	require.NoError(t, err)
	require.True(t, present, "pkg/design must exist for the shared scanner")
	assert.Empty(t, findings,
		"the shared scanner must find no proprietary name in the design-tier Go files")
}
