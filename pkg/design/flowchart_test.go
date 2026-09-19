package design

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseFlowchart verifies node/edge extraction for the flowchart subset.
func TestParseFlowchart(t *testing.T) {
	content := "flowchart LR\n  A[Start] --> B\n  B --> C([End])\n"
	fc := ParseFlowchart(content)

	assert.Equal(t, 1, fc.Declarations)
	assert.Equal(t, "LR", fc.Direction)
	assert.Equal(t, []string{"A", "B", "C"}, fc.NodeOrder)
	require.Len(t, fc.Nodes, 3)
	assert.Equal(t, "Start", fc.Nodes["A"].Label)

	require.Len(t, fc.Edges, 2)
	assert.Equal(t, "A", fc.Edges[0].Source)
	assert.Equal(t, "B", fc.Edges[0].Target)
	assert.True(t, fc.Edges[0].HasArrow)
	assert.Equal(t, "B", fc.Edges[1].Source)
	assert.Equal(t, "C", fc.Edges[1].Target)
}

func TestParseFlowchartChained(t *testing.T) {
	fc := ParseFlowchart("flowchart TD\n  A --> B --> C\n")
	assert.Equal(t, []string{"A", "B", "C"}, fc.NodeOrder)
	require.Len(t, fc.Edges, 2)
	assert.Equal(t, "A", fc.Edges[0].Source)
	assert.Equal(t, "B", fc.Edges[0].Target)
	assert.Equal(t, "B", fc.Edges[1].Source)
	assert.Equal(t, "C", fc.Edges[1].Target)
}

func TestParseFlowchartIgnoresNoise(t *testing.T) {
	// Comments, styling keywords, and subgraph/end lines are skipped; node
	// and edge lines inside a subgraph are still extracted.
	content := "flowchart TD\n  %% a comment\n  subgraph \"Group\"\n    A --> B\n    classDef x fill:red\n    class A x\n  end\n  style B stroke:blue\n"
	fc := ParseFlowchart(content)
	assert.Equal(t, []string{"A", "B"}, fc.NodeOrder)
	require.Len(t, fc.Edges, 1)
	assert.Empty(t, fc.BadLines)
}

func TestParseFlowchartDottedAndUndirected(t *testing.T) {
	fc := ParseFlowchart("flowchart LR\n  A -.-> B\n  B --- C\n")
	require.Len(t, fc.Edges, 2)
	assert.True(t, fc.Edges[0].HasArrow)
	assert.False(t, fc.Edges[1].HasArrow)
}

func TestParseFlowchartLabelContainingArrow(t *testing.T) {
	// An arrow inside a node label must not be treated as a connect operator.
	fc := ParseFlowchart("flowchart LR\n  A[Step --> Process] --> B\n")
	assert.Equal(t, []string{"A", "B"}, fc.NodeOrder)
	require.Len(t, fc.Edges, 1)
	assert.Equal(t, "A", fc.Edges[0].Source)
	assert.Equal(t, "B", fc.Edges[0].Target)
	assert.Equal(t, "Step --> Process", fc.Nodes["A"].Label)
}

// TestValidateFlows covers the flow validation rules at the right severity.
func TestValidateFlows(t *testing.T) {
	t.Run("valid-screen-flow", func(t *testing.T) {
		content := "flowchart LR\n  login --> home\n  home --> dashboard\n"
		findings := ValidateFlows("design/flows/main.mmd", []byte(content), []string{"login", "home", "dashboard"})
		requireNoWireframeFindings(t, findings)
	})

	t.Run("missing-declaration", func(t *testing.T) {
		content := "login --> home\n"
		findings := ValidateFlows("design/flows/main.mmd", []byte(content), []string{"login", "home"})
		assert.Equal(t, 1, findingRules(findings)[ruleFlowchartDeclaration])
	})

	t.Run("multiple-declarations", func(t *testing.T) {
		content := "flowchart LR\n  login --> home\ngraph TD\n  home --> back\n"
		findings := ValidateFlows("design/flows/main.mmd", []byte(content), []string{"login", "home", "back"})
		assert.Equal(t, 1, findingRules(findings)[ruleFlowchartDeclaration])
	})

	t.Run("dangling-node-stem", func(t *testing.T) {
		// login is a known stem (so this is a screen flow); "missing" is not.
		content := "flowchart LR\n  login --> missing\n"
		findings := ValidateFlows("design/flows/main.mmd", []byte(content), []string{"login"})
		assert.Equal(t, 1, findingRules(findings)[ruleFlowchartNodeStem])
		for _, f := range findings {
			if f.Rule == ruleFlowchartNodeStem {
				assert.Contains(t, f.Message, "missing")
				assert.Equal(t, SeverityError, f.Severity)
			}
		}
	})

	t.Run("process-flow-untouched", func(t *testing.T) {
		// No node id matches a wireframe stem, so the flow is a process/user
		// flow and the node-stem rule does not cross-check it.
		content := "flowchart TD\n  start --> process --> done\n"
		findings := ValidateFlows("design/flows/pipeline.mmd", []byte(content), []string{"login"})
		assert.Equal(t, 0, findingRules(findings)[ruleFlowchartNodeStem])
		requireNoWireframeFindings(t, findings)
	})

	t.Run("unparseable-line", func(t *testing.T) {
		content := "flowchart LR\n  login -->\n"
		findings := ValidateFlows("design/flows/main.mmd", []byte(content), []string{"login"})
		assert.Equal(t, 1, findingRules(findings)[ruleFlowchartSyntax])
	})

	t.Run("bare-junk-line", func(t *testing.T) {
		content := "flowchart LR\n  login --> home\n  ???\n"
		findings := ValidateFlows("design/flows/main.mmd", []byte(content), []string{"login", "home"})
		assert.Equal(t, 1, findingRules(findings)[ruleFlowchartSyntax])
	})
}

// TestValidateFlowsDir exercises the dir-level entry point: cross-file stem
// resolution and graceful handling of a missing/empty directory.
func TestValidateFlowsDir(t *testing.T) {
	t.Run("resolves-stems", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "flows"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "wireframes"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "flows", "main.mmd"),
			[]byte("flowchart LR\n  login --> home\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "wireframes", "login.svg"),
			[]byte("<svg viewBox=\"0 0 1 1\"></svg>"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "wireframes", "home.svg"),
			[]byte("<svg viewBox=\"0 0 1 1\"></svg>"), 0o644))

		findings, err := ValidateFlowsDir(root)
		require.NoError(t, err)
		requireNoWireframeFindings(t, findings)
	})

	t.Run("dangling-stem-across-files", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "flows"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "wireframes"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "flows", "main.mmd"),
			[]byte("flowchart LR\n  login --> nowhere\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "wireframes", "login.svg"),
			[]byte("<svg viewBox=\"0 0 1 1\"></svg>"), 0o644))

		findings, err := ValidateFlowsDir(root)
		require.NoError(t, err)
		assert.Equal(t, 1, findingRules(findings)[ruleFlowchartNodeStem])
	})

	t.Run("missing-dir", func(t *testing.T) {
		root := t.TempDir()
		findings, err := ValidateFlowsDir(root)
		require.NoError(t, err)
		require.NotNil(t, findings)
		assert.Empty(t, findings)
	})
}
