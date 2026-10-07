package deploy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// sentinelToken is a deliberately long, high-entropy, unique string used in
// the absence tests. Its awkward shape (mixed case, digits, dashes, a very
// distinctive "sprout-sentinel" marker) makes an accidental leak easy to
// detect with a plain substring search, while still being long enough to
// exercise value-based redaction.
const sentinelToken = "sprout-sentinel-DEPLOY-token-9f8e7d6c5b4a3f2e1d0c-do-not-log"

// isolateCredentialStore points the credential store at a throwaway config
// directory so the test never reads or writes the developer's real store.
// In a test binary the file backend is chosen, so SetToActiveBackend writes
// into this isolated directory.
func isolateCredentialStore(t *testing.T) {
	t.Helper()
	// Clear any ambient credential that could shadow the store or the
	// fallback variable, then point the store at a temp dir.
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	dir := filepath.Join(t.TempDir(), "config")
	t.Setenv("SPROUT_CONFIG_DIR", dir)
	credentials.ResetStorageBackend()
	t.Cleanup(credentials.ResetStorageBackend)
}

// ---------------------------------------------------------------------------
// Resolution: store preferred, environment fallback, missing sentinel error
// ---------------------------------------------------------------------------

func TestResolveCredential_PrefersStoreOverEnvironment(t *testing.T) {
	isolateCredentialStore(t)
	require.NoError(t, credentials.SetToActiveBackend("cloudflare", sentinelToken))
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-token-that-must-lose")

	cred, err := ResolveCredential(DeployCredentialConfig{
		Target:  "cloudflare",
		Project: "my-site",
		EnvVar:  "CLOUDFLARE_API_TOKEN",
	})
	require.NoError(t, err)
	assert.Equal(t, sentinelToken, cred.Value(), "the stored token must win over the environment")
	assert.Equal(t, SourceStore, cred.Source())
}

func TestResolveCredential_FallsBackToEnvironment(t *testing.T) {
	isolateCredentialStore(t)
	// Nothing in the store for "cloudflare".
	t.Setenv("CLOUDFLARE_API_TOKEN", sentinelToken)

	cred, err := ResolveCredential(DeployCredentialConfig{
		Target: "cloudflare",
		EnvVar: "CLOUDFLARE_API_TOKEN",
	})
	require.NoError(t, err)
	assert.Equal(t, sentinelToken, cred.Value())
	assert.Equal(t, SourceEnvironment, cred.Source())
}

func TestResolveCredential_NamedEnvVarWinsOverImplicitProviderEnv(t *testing.T) {
	isolateCredentialStore(t)
	// The provider's conventional env var holds a different value; the
	// caller-named embedding variable must be the one consulted.
	t.Setenv("CLOUDFLARE_API_TOKEN", "implicit-provider-value")
	t.Setenv("SPROUT_DEPLOY_TOKEN", sentinelToken)

	cred, err := ResolveCredential(DeployCredentialConfig{
		Target: "cloudflare",
		EnvVar: "SPROUT_DEPLOY_TOKEN",
	})
	require.NoError(t, err)
	assert.Equal(t, sentinelToken, cred.Value())
	assert.Equal(t, SourceEnvironment, cred.Source())
}

func TestResolveCredential_MissingIsTypedSentinel(t *testing.T) {
	isolateCredentialStore(t)
	cred, err := ResolveCredential(DeployCredentialConfig{
		Target:  "cloudflare",
		Project: "my-site",
		EnvVar:  "CLOUDFLARE_API_TOKEN",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingDeployCredential), "must be the typed missing-credential error")
	assert.Empty(t, cred.Value())
	// The message names the target and actionable sources, but never a value.
	assert.Contains(t, err.Error(), "cloudflare")
	assert.NotContains(t, err.Error(), sentinelToken)
}

func TestResolveCredential_BlankTargetIsMissing(t *testing.T) {
	isolateCredentialStore(t)
	t.Setenv("CLOUDFLARE_API_TOKEN", sentinelToken)

	_, err := ResolveCredential(DeployCredentialConfig{Target: "  ", EnvVar: "CLOUDFLARE_API_TOKEN"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingDeployCredential))
}

func TestCredential_StringMasksValue(t *testing.T) {
	c := newCredential(sentinelToken, SourceStore)
	assert.NotContains(t, c.String(), sentinelToken, "String() must mask the secret")
	assert.NotContains(t, c.GoString(), sentinelToken, "GoString() must mask the secret")
}

// TestCredential_JSONNeverCarriesSecret pins the structural guarantee: a
// Credential marshals to JSON without its secret, so even if one is embedded
// in an event, request, or tool payload it cannot ride along to a model.
func TestCredential_JSONNeverCarriesSecret(t *testing.T) {
	c := newCredential(sentinelToken, SourceStore)

	assert.NotContains(t, mustJSON(t, c), sentinelToken, "a serialized Credential must not carry the secret")

	// Embedding a Credential in a larger payload must not leak it either.
	wrapper := struct {
		Event      string     `json:"event"`
		Credential Credential `json:"credential"`
	}{Event: "deploy.started", Credential: c}
	assert.NotContains(t, mustJSON(t, wrapper), sentinelToken, "an embedded Credential must not carry the secret")

	// The source survives serialization so a log can still say where it came
	// from without knowing the value.
	assert.Contains(t, mustJSON(t, c), "store")
}

// ---------------------------------------------------------------------------
// Absence: the secret never lands in a serialized or loggable artifact
// ---------------------------------------------------------------------------

// mustJSON marshals v and returns the JSON string, failing the test on error.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// TestNoTokenInSerializedDeployTypes is the core absence assertion: a
// resolved token must not appear in the JSON of the deploy request or the
// recorded deployment, and neither type may carry a credential-ish field.
func TestNoTokenInSerializedDeployTypes(t *testing.T) {
	isolateCredentialStore(t)
	require.NoError(t, credentials.SetToActiveBackend("cloudflare", sentinelToken))

	cred, err := ResolveCredential(DeployCredentialConfig{Target: "cloudflare", EnvVar: "CLOUDFLARE_API_TOKEN"})
	require.NoError(t, err)
	require.Equal(t, sentinelToken, cred.Value())

	// Build the deploy values a deploy would actually carry. Neither takes
	// the credential — the adapter receives it out of band.
	d := Deployment{ID: "my-site-1", Project: "my-site", Kind: KindPreview, URL: "https://my-site-1.example.test", Version: "1.0.0", Status: StatusReady}
	r := DeployRequest{Project: "my-site", Kind: KindPreview, BuildDir: "dist", Version: "1.0.0"}

	for name, v := range map[string]any{
		"Deployment":    d,
		"DeployRequest": r,
	} {
		assert.NotContains(t, mustJSON(t, v), sentinelToken, "%s JSON must not carry the token", name)
	}

	// Guard against a token field being added to the request type at all:
	// the marshalled keys must not name a credential-ish field.
	lower := strings.ToLower(mustJSON(t, r))
	assert.NotContains(t, lower, "token")
	assert.NotContains(t, lower, "api_key")
	assert.NotContains(t, lower, "apikey")
	assert.NotContains(t, lower, "secret")
}

// TestNoTokenInRedactedLogPayload asserts the token does not survive the
// existing redaction helpers (the ones a log/telemetry path uses): neither a
// key-aware redacted env map nor a redacted JSON payload may contain it.
func TestNoTokenInRedactedLogPayload(t *testing.T) {
	isolateCredentialStore(t)
	require.NoError(t, credentials.SetToActiveBackend("cloudflare", sentinelToken))

	cred, err := ResolveCredential(DeployCredentialConfig{Target: "cloudflare", EnvVar: "CLOUDFLARE_API_TOKEN"})
	require.NoError(t, err)
	require.Equal(t, sentinelToken, cred.Value())

	// A deploy-related log payload that (incorrectly, but plausibly) carries
	// the token under a sensitive-looking key. The redactor must scrub it.
	logMap := map[string]string{
		"target":               "cloudflare",
		"project":              "my-site",
		"CLOUDFLARE_API_TOKEN": cred.Value(),
	}
	redacted := credentials.RedactMap(logMap)
	for k, v := range redacted {
		assert.NotContains(t, v, sentinelToken, "RedactMap output for %q must not carry the token", k)
	}

	// Key-aware env redaction zeroes sensitive keys entirely.
	envRedacted := credentials.RedactEnvMap(map[string]string{"CLOUDFLARE_API_TOKEN": cred.Value()})
	assert.Equal(t, "[REDACTED]", envRedacted["CLOUDFLARE_API_TOKEN"])
	assert.NotContains(t, envRedacted["CLOUDFLARE_API_TOKEN"], sentinelToken)

	// A JSON log payload through RedactJSONBytes: a sensitive key is replaced
	// wholesale, so the token put under a credential-named field is scrubbed.
	// (A token embedded in an arbitrary free-text field of a log payload is
	// the caller's responsibility never to write — the value-based scanner
	// only catches secrets matching known formats, so the deploy path must
	// not build log payloads carrying the raw value.)
	payload := map[string]any{
		"event":  "deploy.started",
		"target": "cloudflare",
		"token":  cred.Value(),
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	redactedJSON, err := credentials.RedactJSONBytes(raw)
	require.NoError(t, err)
	assert.NotContains(t, string(redactedJSON), sentinelToken, "RedactJSONBytes output must not carry the token")
}

// TestNoTokenInDeployTranscript asserts the token never reaches a deploy
// target's recorded transcript. The credential is resolved and used by the
// caller "around" the adapter; the DeployRequest handed to the target carries
// no secret, so the transcript cannot contain it.
func TestNoTokenInDeployTranscript(t *testing.T) {
	isolateCredentialStore(t)
	require.NoError(t, credentials.SetToActiveBackend("cloudflare", sentinelToken))

	cred, err := ResolveCredential(DeployCredentialConfig{Target: "cloudflare", EnvVar: "CLOUDFLARE_API_TOKEN"})
	require.NoError(t, err)
	require.Equal(t, sentinelToken, cred.Value())

	target := NewFake()
	request := DeployRequest{Project: "my-site", Kind: KindPreview, BuildDir: "dist"}
	_, err = target.Deploy(request)
	require.NoError(t, err)

	// The whole transcript, as a caller would serialize it, must be clean.
	transcriptJSON := mustJSON(t, target.Calls())
	assert.NotContains(t, transcriptJSON, sentinelToken)

	// And so must the request the caller built: the token is held by the
	// caller/adapter, never attached to the request.
	assert.NotContains(t, mustJSON(t, request), sentinelToken)

	// The resolved credential itself masks when rendered.
	assert.NotContains(t, cred.String(), sentinelToken)
}

// TestSentinelTokenAbsentFromEveryArtifact is the "try to break the rule"
// test: a unique, deliberately long sentinel token is placed in the
// environment, resolved, and then searched for across every serializable or
// loggable artifact a deploy could touch.
func TestSentinelTokenAbsentFromEveryArtifact(t *testing.T) {
	const envVar = "SPROUT_DEPLOY_SENTINEL_TOKEN"
	isolateCredentialStore(t)
	t.Setenv(envVar, sentinelToken)

	cred, err := ResolveCredential(DeployCredentialConfig{Target: "cloudflare", Project: "my-site", EnvVar: envVar})
	require.NoError(t, err)
	require.Equal(t, sentinelToken, cred.Value())

	target := NewFake()
	request := DeployRequest{Project: "my-site", Kind: KindPreview, BuildDir: "dist", Version: "1.0.0"}
	deployment, err := target.Deploy(request)
	require.NoError(t, err)

	artifacts := map[string]string{
		"DeployRequest":   mustJSON(t, request),
		"Deployment":      mustJSON(t, deployment),
		"transcript":      mustJSON(t, target.Calls()),
		"Credential":      cred.String(),
		"CredentialGoStr": cred.GoString(),
		"CredentialJSON":  mustJSON(t, cred),
		"logMap":          strings.Join(mapValues(credentials.RedactMap(map[string]string{"token": cred.Value()})), "\n"),
		"error":           func() string { _, e := ResolveCredential(DeployCredentialConfig{Target: "none"}); return e.Error() }(),
	}
	for name, artifact := range artifacts {
		assert.NotContains(t, artifact, sentinelToken, "artifact %q must not contain the sentinel token", name)
	}

	// Sanity: the token really was in the environment, so this is a genuine
	// absence test rather than a no-op.
	assert.Equal(t, sentinelToken, os.Getenv(envVar))
}

// mapValues returns the values of m as a slice (order-independent).
func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
