package deploy

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cfSelectCred is a resolved-looking credential for the selector tests: the
// constructors only check it is non-blank, so no store or environment is
// touched.
func cfSelectCred() Credential { return Credential{value: "select-test-token"} }

func TestNormalizeDeployTarget(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty means pages", input: "", want: DeployTargetPages},
		{name: "whitespace is not defaulted", input: "   ", want: "   "},
		{name: "pages is kept", input: "pages", want: DeployTargetPages},
		{name: "workers is kept", input: "workers", want: DeployTargetWorkers},
		{name: "unknown is returned unchanged", input: "foo", want: "foo"},
		{name: "padded value is returned unchanged", input: "pages ", want: "pages "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, NormalizeDeployTarget(tc.input))
		})
	}
}

// TestCloudflareTargetFor_SelectsAdapter pins the rule: the deploy target
// string picks the Pages or Workers adapter, an absent value defaults to
// Pages, and anything else is an actionable error.
func TestCloudflareTargetFor_SelectsAdapter(t *testing.T) {
	cfg := CloudflareConfig{AccountID: "acct-1", Project: "my-site"}

	cases := []struct {
		name       string
		deployTgt  string
		wantPages  bool
		wantWorker bool
	}{
		{name: "pages selects Pages", deployTgt: DeployTargetPages, wantPages: true},
		{name: "empty defaults to Pages", deployTgt: "", wantPages: true},
		{name: "workers selects Workers", deployTgt: DeployTargetWorkers, wantWorker: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CloudflareTargetFor(tc.deployTgt, cfg, cfSelectCred(), "https://api.example.invalid", nil)
			require.NoError(t, err)
			if tc.wantPages {
				_, ok := got.(*CloudflarePages)
				assert.True(t, ok, "got %T, want *CloudflarePages", got)
			}
			if tc.wantWorker {
				_, ok := got.(*CloudflareWorkers)
				assert.True(t, ok, "got %T, want *CloudflareWorkers", got)
			}
		})
	}
}

// TestCloudflareTargetFor_UnknownIsActionableError pins that an unrecognised
// deploy target is refused with a message naming the accepted values, rather
// than silently defaulting.
func TestCloudflareTargetFor_UnknownIsActionableError(t *testing.T) {
	cfg := CloudflareConfig{AccountID: "acct-1", Project: "my-site"}
	for _, bad := range []string{"foo", "  ", "pages "} {
		got, err := CloudflareTargetFor(bad, cfg, cfSelectCred(), "https://api.example.invalid", nil)
		require.Error(t, err, "deploy target %q must be refused", bad)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "unknown deploy target")
		assert.Contains(t, err.Error(), DeployTargetPages)
		assert.Contains(t, err.Error(), DeployTargetWorkers)
	}
}

// TestCloudflareTargetFor_PropagatesValidation pins that the selector forwards
// the constructor's validation: a config missing its account or project is
// refused regardless of the shape.
func TestCloudflareTargetFor_PropagatesValidation(t *testing.T) {
	for _, deployTgt := range []string{DeployTargetPages, DeployTargetWorkers} {
		_, err := CloudflareTargetFor(deployTgt, CloudflareConfig{AccountID: "", Project: "p"}, cfSelectCred(), "https://api.example.invalid", nil)
		require.Error(t, err, "target %q must refuse an empty account", deployTgt)
		assert.True(t, strings.Contains(err.Error(), "account"), "got: %v", err)
	}
}
