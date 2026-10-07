// Cloudflare deploy adapter: ship a project to Cloudflare Pages (static
// output) or Cloudflare Workers (server-side code).
//
// It implements the DeployTarget contract in deploy.go against Cloudflare's
// REST API. The adapter is deliberately split in two: Pages is the fully
// implemented path (deploy, history, status, per-deployment preview URLs, and
// rollback), while Workers covers the single-deployment shape a server-side
// starter needs. Everything flows through request, config, and client seams so
// tests drive a local HTTP fake with no account and no network.
package deploy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// CloudflareAPITokenEnvVar is the conventional embedding-environment variable
// for a Cloudflare API token. It is only a fallback hint for callers building
// a DeployCredentialConfig; the adapter never reads the environment itself —
// it receives an already-resolved Credential.
const CloudflareAPITokenEnvVar = "CLOUDFLARE_API_TOKEN"

// DefaultCloudflareAPIBaseURL is the production Cloudflare API root. The
// adapter appends the versioned path and resource segments to it.
const DefaultCloudflareAPIBaseURL = "https://api.cloudflare.com/client/v4"

// cloudflareDefaultTimeout bounds a single API call when the client does not
// set its own. It keeps a wedged connection from hanging a deploy forever.
const cloudflareDefaultTimeout = 30 * time.Second

// ErrCloudflareRequestFailed is the typed error every non-2xx Cloudflare API
// response is wrapped in. Callers detect it with errors.Is and read the HTTP
// status, the API error codes and the sanitized messages through the
// *CloudflareRequestError.
var ErrCloudflareRequestFailed = errors.New("cloudflare: API request failed")

// CloudflareError is one entry from a Cloudflare API error response.
type CloudflareError struct {
	Code    int
	Message string
}

// CloudflareRequestError describes a failed Cloudflare API call. StatusCode is
// the HTTP status, and Errors carries the API's structured errors when the
// response body was a Cloudflare envelope. It never holds the request's auth
// header or URL, so it is safe to format into a log line or an error.
type CloudflareRequestError struct {
	// Op names the call, e.g. "create deployment".
	Op string
	// StatusCode is the HTTP status of the response.
	StatusCode int
	// Errors are the API's structured errors, when present.
	Errors []CloudflareError
	// Message is a sanitized human-readable reason: Cloudflare's own error
	// messages with any credential-looking substring scrubbed, or a short
	// fallback when the body was not a Cloudflare envelope.
	Message string
}

// Error implements error.
func (e *CloudflareRequestError) Error() string {
	status := http.StatusText(e.StatusCode)
	if status == "" {
		status = "unknown status"
	}
	if e.Message == "" {
		return fmt.Sprintf("cloudflare: %s: %d %s", e.Op, e.StatusCode, status)
	}
	return fmt.Sprintf("cloudflare: %s: %d %s: %s", e.Op, e.StatusCode, status, e.Message)
}

// Unwrap exposes ErrCloudflareRequestFailed so errors.Is works uniformly.
func (e *CloudflareRequestError) Unwrap() error { return ErrCloudflareRequestFailed }

// CloudflareConfig names the account and project a Cloudflare adapter ships
// to. AccountID scopes Pages and Workers resources; Project is the Pages
// project (or the Worker name) the deployment belongs to and the key its
// history and rollback are registered under.
type CloudflareConfig struct {
	// AccountID is the Cloudflare account id. Required.
	AccountID string
	// Project is the Pages project name for Pages deploys, or the Worker
	// name for Workers deploys. Required.
	Project string
}

// CloudflareTarget implements DeployTarget against Cloudflare. It is the
// entry point that hosts the shared HTTP plumbing; the Pages and Workers
// adapters embed it and supply the resource shape.
//
// The token is held in an unexported Credential field: it is never a struct
// field a caller can read back, never part of any request value, and never
// formatted into an error or a log line. It reaches the wire only through the
// auth header the HTTP layer sets.
type CloudflareTarget struct {
	cfg  CloudflareConfig
	cred Credential
	base string
	http *http.Client
}

// NewCloudflarePagesTarget builds a Pages adapter for cfg, authenticating
// with cred. baseURL is the API root (default DefaultCloudflareAPIBaseURL
// when empty), and client is the HTTP client (default a client with a bounded
// timeout when nil). Tests point baseURL at an httptest server and pass that
// server's client, so no network is touched.
//
// cred is the resolved credential from ResolveCredential — the adapter never
// reads the environment, so the token is always obtained out of band and can
// never reach model context, tool arguments, or logs.
func NewCloudflarePagesTarget(cfg CloudflareConfig, cred Credential, baseURL string, client *http.Client) (*CloudflarePages, error) {
	base, err := normalizeCloudflareBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if err := validateCloudflareConfig(cfg); err != nil {
		return nil, err
	}
	if err := validateCloudflareCredential(cred); err != nil {
		return nil, err
	}
	t := &CloudflareTarget{
		cfg:  cfg,
		cred: cred,
		base: base,
		http: cloudflareHTTPClient(client),
	}
	return &CloudflarePages{t: t, project: strings.TrimSpace(cfg.Project)}, nil
}

// NewCloudflareWorkersTarget builds a Workers adapter for cfg. It takes the
// same seams as the Pages constructor.
func NewCloudflareWorkersTarget(cfg CloudflareConfig, cred Credential, baseURL string, client *http.Client) (*CloudflareWorkers, error) {
	base, err := normalizeCloudflareBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if err := validateCloudflareConfig(cfg); err != nil {
		return nil, err
	}
	if err := validateCloudflareCredential(cred); err != nil {
		return nil, err
	}
	t := &CloudflareTarget{
		cfg:  cfg,
		cred: cred,
		base: base,
		http: cloudflareHTTPClient(client),
	}
	return &CloudflareWorkers{t: t, worker: strings.TrimSpace(cfg.Project)}, nil
}

// PageURL is the fallback address a Pages deploy is served at when the API
// response does not carry a deployment-specific URL: the project's stable
// host on the pages.dev domain.
func (t *CloudflareTarget) PageURL() string {
	return fmt.Sprintf("https://%s.pages.dev", t.cfg.Project)
}

// normalizeCloudflareBaseURL trims a trailing slash and validates that the
// base is an absolute http(s) URL. An empty base selects the production API.
func normalizeCloudflareBaseURL(base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return DefaultCloudflareAPIBaseURL, nil
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return "", fmt.Errorf("cloudflare: API base URL %q must be an http(s) URL", base)
	}
	return strings.TrimRight(base, "/"), nil
}

// cloudflareHTTPClient returns client, or a client with a bounded timeout when
// none was supplied.
func cloudflareHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: cloudflareDefaultTimeout}
}

// validateCloudflareConfig rejects a config missing its account or project.
func validateCloudflareConfig(cfg CloudflareConfig) error {
	if strings.TrimSpace(cfg.AccountID) == "" {
		return errors.New("cloudflare: account id must not be empty")
	}
	if strings.TrimSpace(cfg.Project) == "" {
		return errors.New("cloudflare: project must not be empty")
	}
	return nil
}

// validateCloudflareCredential refuses an empty token. An unauthenticated
// deploy would fail at the API anyway; failing here names the real problem
// without ever revealing a value.
func validateCloudflareCredential(cred Credential) error {
	if strings.TrimSpace(cred.Value()) == "" {
		return errors.New("cloudflare: API token is empty (resolve a credential for the target first)")
	}
	return nil
}

// validateDeployRequest applies the shared request invariants: a project and a
// build directory are required, and the kind must be preview or production.
func validateDeployRequest(req DeployRequest) (DeploymentKind, error) {
	if strings.TrimSpace(req.Project) == "" {
		return "", errors.New("cloudflare: project must not be empty")
	}
	if strings.TrimSpace(req.BuildDir) == "" {
		return "", errors.New("cloudflare: build directory must not be empty")
	}
	kind := req.Kind
	if kind == "" {
		kind = KindPreview
	}
	if kind != KindPreview && kind != KindProduction {
		return "", fmt.Errorf("cloudflare: unknown deployment kind %q", req.Kind)
	}
	return kind, nil
}

// deployContext is the context a Deploy call runs with: a values-only parent
// is used when the caller supplies none, so an adapter built for a future
// context-carrying deploy still makes a well-formed request.
func deployContext() context.Context { return context.Background() }
