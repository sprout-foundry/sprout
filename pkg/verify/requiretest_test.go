package verify

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
)

// TestIsTestFilePath pins the name-convention classifier for test files: the
// recognitions and, just as important, the near-misses it must not claim.
func TestIsTestFilePath(t *testing.T) {
	yes := []string{
		"pkg/verify/requiretest_test.go",
		"src/components/Button.test.tsx",
		"src/components/Button.spec.ts",
		"webui/test/unit/foo.ts",
		"app/__tests__/foo.js",
		"lib/specs/thing.rb",
		"tests/integration/flow.py",
		"internal/pkg/foo_test.go",
		"foo_test.go",
		"test_foo.py",
		"spec_helper.rb",
		"Test.java",
		"path\\to\\foo_test.go",
		"pkg/test.go",
	}
	for _, p := range yes {
		assert.Truef(t, IsTestFilePath(p), "IsTestFilePath(%q) = false, want true", p)
	}

	no := []string{
		"",
		"   ",
		"pkg/verify/verify.go",
		"contest.go",
		"latest.ts",
		"src/attestation.ts",
		"test.json",
		"docs/testing.md",
		"makefile",
		"foo_test.txt",
	}
	for _, p := range no {
		assert.Falsef(t, IsTestFilePath(p), "IsTestFilePath(%q) = true, want false", p)
	}
}

// TestRequireTestCheck pins the pure gate: disabled and satisfied cases add no
// check, and only new behavior without a test produces a failing check.
func TestRequireTestCheck(t *testing.T) {
	cases := []struct {
		name     string
		in       RequireTestInput
		wantFail bool
	}{
		{
			name:     "disabled is a no-op even with new code and no test",
			in:       RequireTestInput{Enabled: false, ChangedApplicationPaths: []string{"app.go"}},
			wantFail: false,
		},
		{
			name:     "enabled with a plan test item passes",
			in:       RequireTestInput{Enabled: true, PlanHasTestItem: true, ChangedApplicationPaths: []string{"app.go"}},
			wantFail: false,
		},
		{
			name:     "enabled with a changed test file passes",
			in:       RequireTestInput{Enabled: true, ChangedApplicationPaths: []string{"app.go", "app_test.go"}},
			wantFail: false,
		},
		{
			name:     "enabled with new code and no test fails",
			in:       RequireTestInput{Enabled: true, ChangedApplicationPaths: []string{"app.go"}},
			wantFail: true,
		},
		{
			name:     "enabled with no changed application code is not new behavior",
			in:       RequireTestInput{Enabled: true, ChangedApplicationPaths: nil},
			wantFail: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := RequireTestCheck(tc.in)
			if !tc.wantFail {
				assert.Nil(t, check, "a satisfied or inapplicable requirement must add no check")
				return
			}
			require.NotNil(t, check, "new behavior without a test must produce a failing check")
			assert.Equal(t, RequireTestCheckKind, check.Kind)
			assert.False(t, check.Passed)
			assert.False(t, check.Skipped)
			assert.Equal(t, tc.in.ChangedApplicationPaths, check.Items)
			assert.Contains(t, check.Reason, "test")
		})
	}
}

// TestRunSnapshot_RequireTestDisabledUnchanged proves the default-off path:
// with the requirement disabled the run's checks are exactly the plan/baseline
// checks, no require_test check is appended, and a plan with no test item and
// a code change is never flagged.
func TestRunSnapshot_RequireTestDisabledUnchanged(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, manifestFull)
	p := newPlan(t)
	p.Acceptance = []plancontract.Acceptance{
		{ID: "a1", Scope: "s1", Check: "npm run build:ci", Kind: plancontract.KindBuild},
	}
	writePlanFile(t, root, p)

	exec := &fakeExecutor{}
	r := New()
	r.Exec = exec
	// RequireTest left as its zero value (disabled).
	r.RequireTest = RequireTestInput{ChangedApplicationPaths: []string{"app.go"}}

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)
	for _, c := range res.Checks {
		assert.NotEqual(t, RequireTestCheckKind, c.Kind, "disabled must add no require_test check")
	}
	assert.True(t, res.Passed(), "the disabled requirement must not change the run's outcome")
}

// TestRunSnapshot_RequireTestFailureFlagged proves the enabled gate through a
// full run: new behavior with neither a test item nor a test file appends a
// failing require_test check (failing the run), while adding a test file or a
// plan test item satisfies it.
func TestRunSnapshot_RequireTestFailureFlagged(t *testing.T) {
	t.Run("new code without a test fails the run", func(t *testing.T) {
		root := t.TempDir()
		writeManifestFile(t, root, manifestFull)

		exec := &fakeExecutor{}
		r := New()
		r.Exec = exec
		r.RequireTest = RequireTestInput{
			Enabled:                 true,
			ChangedApplicationPaths: []string{"app.go"},
		}

		res, err := r.Run(context.Background(), root)
		require.NoError(t, err)
		require.True(t, res.Failed(), "new behavior without a test must fail verification")
		last := res.Checks[len(res.Checks)-1]
		assert.Equal(t, RequireTestCheckKind, last.Kind)
		assert.Contains(t, res.Summary(), "require_test")
	})

	t.Run("a test file added in the turn passes", func(t *testing.T) {
		root := t.TempDir()
		writeManifestFile(t, root, manifestFull)

		exec := &fakeExecutor{}
		r := New()
		r.Exec = exec
		r.RequireTest = RequireTestInput{
			Enabled:                 true,
			ChangedApplicationPaths: []string{"app.go", "app_test.go"},
		}

		res, err := r.Run(context.Background(), root)
		require.NoError(t, err)
		assert.True(t, res.Passed())
		for _, c := range res.Checks {
			assert.NotEqual(t, RequireTestCheckKind, c.Kind)
		}
	})

	t.Run("a plan test item passes", func(t *testing.T) {
		root := t.TempDir()
		writeManifestFile(t, root, manifestFull)
		writePlanFile(t, root, newPlan(t))

		exec := &fakeExecutor{}
		r := New()
		r.Exec = exec
		r.RequireTest = RequireTestInput{
			Enabled:                 true,
			PlanHasTestItem:         true,
			ChangedApplicationPaths: []string{"app.go"},
		}

		res, err := r.Run(context.Background(), root)
		require.NoError(t, err)
		assert.True(t, res.Passed())
		for _, c := range res.Checks {
			assert.NotEqual(t, RequireTestCheckKind, c.Kind)
		}
	})

	t.Run("a docs-only turn is not flagged", func(t *testing.T) {
		root := t.TempDir()
		writeManifestFile(t, root, manifestFull)

		exec := &fakeExecutor{}
		r := New()
		r.Exec = exec
		r.RequireTest = RequireTestInput{
			Enabled:                 true,
			ChangedApplicationPaths: nil,
		}

		res, err := r.Run(context.Background(), root)
		require.NoError(t, err)
		assert.True(t, res.Passed())
		for _, c := range res.Checks {
			assert.NotEqual(t, RequireTestCheckKind, c.Kind)
		}
	})
}
