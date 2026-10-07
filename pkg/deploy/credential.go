package deploy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// CredentialSource names where a resolved deploy credential came from, for
// diagnostics and logs. It is deliberately a small closed set so a caller
// (and a test) can distinguish "the user stored it" from "the embedding
// environment supplied it" without ever seeing the secret itself.
type CredentialSource string

const (
	// SourceStore means the secret came from the existing credential store
	// (the OS keyring or the encrypted file store). This is the preferred
	// source.
	SourceStore CredentialSource = "store"
	// SourceEnvironment means the secret came from an embedding-supplied
	// environment variable, used only when the store had none.
	SourceEnvironment CredentialSource = "environment"
)

// String implements fmt.Stringer.
func (s CredentialSource) String() string { return string(s) }

// DeployCredentialConfig names the credential a deploy target needs. Callers
// build it from the deploy config (target, project) and the target adapter's
// declared token variable.
//
// It carries no secret: only the provider key used to look a credential up in
// the store, and the environment variable name an embedding environment may
// supply as a fallback. The resolved secret is returned to the caller
// separately, never stored on this struct.
type DeployCredentialConfig struct {
	// Target is the deploy target adapter id from .sprout/deploy.json (e.g.
	// "cloudflare"). It is also the provider key used in the credential store.
	// Required.
	Target string
	// Project is the project name on the target. Optional; it is carried so a
	// caller can key a target-specific credential without inventing a parallel
	// lookup.
	Project string
	// EnvVar is the embedding-environment variable that may supply the token
	// as a fallback (e.g. "CLOUDFLARE_API_TOKEN"). Empty means "no
	// environment fallback": the resolver then only consults the store.
	EnvVar string
}

// ErrMissingDeployCredential is returned by ResolveCredential when neither
// the credential store nor the embedding environment has a token for the
// target. It is a typed, distinguishable "missing credential" error: callers
// detect it with errors.Is and can render actionable guidance without the
// resolver leaking any value.
var ErrMissingDeployCredential = errors.New("missing deploy credential")

// Credential is a resolved deploy credential: the secret value plus the
// source it came from. The value is returned to the caller (the runtime or
// the target adapter); it must never be attached to DeployRequest,
// Deployment, or any other value that crosses to a model or a log.
//
// The secret is held in an unexported field and the type marshals to JSON
// without it, so a Credential — even embedded in a larger struct — can never
// be serialized into a payload sent to a model, and an accidental %v/%#v
// renders a masked form. Reaching the secret requires the explicit Value()
// accessor, which keeps every use site deliberate.
type Credential struct {
	value  string
	source CredentialSource
}

// newCredential builds a Credential from a resolved value and source. It is
// unexported so a caller cannot fabricate one with an arbitrary value, and so
// the only way to obtain a Credential is through ResolveCredential.
func newCredential(value string, source CredentialSource) Credential {
	return Credential{value: value, source: source}
}

// Value returns the secret token. Sensitive: the caller must not log it or
// serialize it; it exists for the adapter that needs to authenticate.
func (c Credential) Value() string { return c.value }

// Source reports where the credential came from (store or environment).
func (c Credential) Source() CredentialSource { return c.source }

// String returns a safe, opaque description of the credential with the value
// masked, so an accidental %v or %s of a Credential never reveals the secret.
func (c Credential) String() string {
	return fmt.Sprintf("deploy.Credential{Source: %q, Value: %q}", c.source, credentials.MaskValue(c.value))
}

// GoString implements fmt.GoStringer so a %#v of a Credential is masked too.
func (c Credential) GoString() string { return c.String() }

// MarshalJSON serializes a Credential without its secret: only the source is
// emitted. This is the structural guarantee behind "never in model context":
// even if a Credential is embedded in an event, request, or tool payload, the
// secret cannot ride along in its JSON.
func (c Credential) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Source CredentialSource `json:"source"`
	}{Source: c.source})
}

// ResolveCredential obtains the deploy credential described by cfg, preferring
// the existing credential store and falling back to the embedding-supplied
// environment variable only when the store has none.
//
// Resolution order (tokens live in the existing credential store — keyring or
// encrypted file — or are supplied by the embedding environment):
//
//  1. Credential store: the OS keyring or the encrypted file store. This is
//     the preferred source, so a token a user stored with `sprout keys set`
//     always wins over an ambient environment value.
//  2. Embedding environment: the variable named by cfg.EnvVar, when set and
//     non-blank. Used only when the store had nothing.
//
// It returns the secret plus its source, or ErrMissingDeployCredential
// (wrapped with the target name) when neither source has one. The returned
// value is for the caller alone: it is never attached to a DeployRequest, a
// Deployment, or a DeployConfig.
func ResolveCredential(cfg DeployCredentialConfig) (Credential, error) {
	target := strings.TrimSpace(cfg.Target)
	if target == "" {
		return Credential{}, fmt.Errorf("deploy: %w: no target configured", ErrMissingDeployCredential)
	}

	// 1. Preferred: the existing credential store (keyring or encrypted
	// file). storeCredential reads only the store, deliberately never the
	// environment, so an ambient variable can never shadow a stored token.
	if value, source, err := storeCredential(target); err != nil {
		return Credential{}, err
	} else if value != "" {
		return newCredential(value, source), nil
	}

	// 2. Fallback: the embedding-supplied environment variable, when the
	// caller named one and it is set.
	if envVar := strings.TrimSpace(cfg.EnvVar); envVar != "" {
		if value := strings.TrimSpace(os.Getenv(envVar)); value != "" {
			return newCredential(value, SourceEnvironment), nil
		}
	}

	return Credential{}, fmt.Errorf("deploy: %w for target %q (store it with `sprout keys set %s` or set %s)",
		ErrMissingDeployCredential, target, target, envVarHint(cfg.EnvVar))
}

// storeCredential reads the target's credential from the credential store
// only, without consulting the environment. It returns an empty value (and
// nil error) when the store holds nothing, so the caller can fall back.
func storeCredential(target string) (string, CredentialSource, error) {
	pool, err := credentials.LoadKeyPool(target)
	if err != nil {
		return "", "", fmt.Errorf("deploy: read credential store for %q: %w", target, err)
	}
	if pool == nil || pool.Pool == nil || len(pool.Pool.Keys) == 0 {
		return "", "", nil
	}
	value := strings.TrimSpace(pool.Pool.Keys[0])
	if value == "" {
		return "", "", nil
	}
	return value, SourceStore, nil
}

// envVarHint returns the environment variable name to name in guidance, or a
// generic phrase when the caller supplied none.
func envVarHint(envVar string) string {
	if strings.TrimSpace(envVar) == "" {
		return "the target's token environment variable"
	}
	return strings.TrimSpace(envVar)
}
