package deploy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cfSentinelToken is a deliberately distinctive token used across the
// Cloudflare tests: an accidental leak into an error, a header dump, or a
// request payload is caught by a plain substring search.
const cfSentinelToken = "sprout-sentinel-CLOUDFLARE-token-9f8e7d6c5b4a3f2e"

// cloudflareFake is an httptest-backed stand-in for the Cloudflare API: an
// in-memory Pages project with deployments, and a Worker script. It records
// every request (method, path, auth header, body) so a test can assert the
// adapter spoke the right protocol and never leaked the token.
type cloudflareFake struct {
	mu sync.Mutex

	accountID string
	project   string
	worker    string

	deployments []fakePagesDeployment
	nextID      int

	requests []fakeRequest

	// failure, when set, makes the next matching request return this response.
	failStatus int
	failBody   string
	failPath   string
}

type fakePagesDeployment struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Stage     string    `json:"stage"`
	Branch    string    `json:"branch"`
	CommitMsg string    `json:"commit_message"`
	CreatedOn time.Time `json:"created_on"`
}

type fakeRequest struct {
	Method  string
	Path    string
	Auth    string
	Body    string
	TokenIn bool
}

// newCloudflareFake builds the fake and returns it with its test server. The
// server is closed automatically when the test ends.
func newCloudflareFake(t *testing.T, accountID, project string) (*cloudflareFake, *httptest.Server) {
	t.Helper()
	f := &cloudflareFake{accountID: accountID, project: project, worker: project}
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *cloudflareFake) handle(w http.ResponseWriter, r *http.Request) {
	// Read the body before recording, so a handler downstream can still
	// decode it (record consumes the stream otherwise).
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, fakeRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Auth:   r.Header.Get("Authorization"),
		Body:   string(body),
	})
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	if f.failStatus != 0 && (f.failPath == "" || strings.Contains(r.URL.Path, f.failPath)) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.failStatus)
		io.WriteString(w, f.failBody)
		return
	}

	path := r.URL.Path
	base := "/accounts/" + f.accountID

	switch {
	case r.Method == http.MethodPost && path == base+"/pages/projects/"+f.project+"/deployments":
		f.createDeployment(w)
	case r.Method == http.MethodGet && path == base+"/pages/projects/"+f.project+"/deployments":
		f.listDeployments(w)
	case r.Method == http.MethodPost && strings.HasPrefix(path, base+"/pages/projects/"+f.project+"/deployments/") && strings.HasSuffix(path, "/assets"):
		writeCFOK(w, map[string]any{})
	case r.Method == http.MethodPost && path == base+"/pages/projects/"+f.project+"/deployments/rollback":
		f.rollback(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(path, base+"/pages/projects/"+f.project+"/deployments/"):
		f.getDeployment(w, strings.TrimPrefix(path, base+"/pages/projects/"+f.project+"/deployments/"))
	case r.Method == http.MethodPut && path == base+"/workers/scripts/"+f.worker:
		writeCFOK(w, map[string]any{"id": f.worker})
	default:
		w.WriteHeader(http.StatusNotFound)
		writeCFErr(w, 404, "not found")
	}
}

func (f *cloudflareFake) createDeployment(w http.ResponseWriter) {
	f.nextID++
	id := "dep-" + itoa(f.nextID)
	d := fakePagesDeployment{
		ID:        id,
		URL:       "https://" + id + "." + f.project + ".pages.dev",
		Stage:     "success",
		Branch:    f.project + "-preview",
		CommitMsg: "1.0.0",
		CreatedOn: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(f.nextID) * time.Hour),
	}
	f.deployments = append(f.deployments, d)
	writeCFOK(w, d)
}

func (f *cloudflareFake) listDeployments(w http.ResponseWriter) {
	// Always return a JSON array, even when empty: the adapter decodes a list
	// and a null result would be ambiguous.
	if f.deployments == nil {
		writeCFOK(w, []fakePagesDeployment{})
		return
	}
	writeCFOK(w, f.deployments)
}

func (f *cloudflareFake) getDeployment(w http.ResponseWriter, id string) {
	for _, d := range f.deployments {
		if d.ID == id {
			writeCFOK(w, d)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
	writeCFErr(w, 404, "deployment not found")
}

func (f *cloudflareFake) rollback(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeploymentID string `json:"deployment_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	for _, d := range f.deployments {
		if d.ID == body.DeploymentID {
			writeCFOK(w, d)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
	writeCFErr(w, 404, "deployment not found")
}

// fail configures the fake to return status/body for the next request whose
// path contains pathContains.
func (f *cloudflareFake) fail(status int, body, pathContains string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failStatus = status
	f.failBody = body
	f.failPath = pathContains
}

func (f *cloudflareFake) requestsSnapshot() []fakeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

func writeCFOK(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"errors":  []any{},
		"result":  result,
	})
}

func writeCFErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"errors":  []map[string]any{{"code": code, "message": msg}},
		"result":  nil,
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// writeBuildOutput writes files into a temp build directory.
func writeBuildOutput(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return dir
}

// newPagesTarget wires a Pages adapter to the fake server.
func newPagesTarget(t *testing.T, f *cloudflareFake, srv *httptest.Server, token string) *CloudflarePages {
	t.Helper()
	target, err := NewCloudflarePagesTarget(
		CloudflareConfig{AccountID: f.accountID, Project: f.project},
		Credential{value: token},
		srv.URL,
		srv.Client(),
	)
	require.NoError(t, err)
	return target
}

// ---------------------------------------------------------------------------
// Interface conformance
// ---------------------------------------------------------------------------

func TestCloudflareTargetsImplementDeployTarget(t *testing.T) {
	var _ DeployTarget = (*CloudflarePages)(nil)
	var _ DeployTarget = (*CloudflareWorkers)(nil)
}

// ---------------------------------------------------------------------------
// Round trip: deploy → list → status → preview URL → rollback
// ---------------------------------------------------------------------------

// TestCloudflarePages_RoundTrip is the lifecycle the acceptance criteria name,
// driven entirely against the httptest fake: deploy, see it in history, read
// its status, resolve its preview URL, then roll back to the previous
// deployment.
func TestCloudflarePages_RoundTrip(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-site")
	target := newPagesTarget(t, f, srv, cfSentinelToken)

	buildDir := writeBuildOutput(t, map[string]string{"index.html": "<h1>hi</h1>", "app.js": "console.log(1)"})

	first, err := target.Deploy(DeployRequest{Project: "my-site", Kind: KindPreview, BuildDir: buildDir, Version: "1.0.0"})
	require.NoError(t, err)
	assert.Equal(t, "dep-1", first.ID)
	assert.Equal(t, "my-site", first.Project)
	assert.Equal(t, KindPreview, first.Kind)
	assert.Equal(t, StatusReady, first.Status)
	assert.Equal(t, "https://dep-1.my-site.pages.dev", first.URL)
	assert.Equal(t, "1.0.0", first.Version)

	listed, err := target.List("my-site")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, first.ID, listed[0].ID)
	assert.Equal(t, first.URL, listed[0].URL)

	state, err := target.Status(first)
	require.NoError(t, err)
	assert.Equal(t, StatusReady, state)

	previewURL, err := target.PreviewURL(first)
	require.NoError(t, err)
	assert.Equal(t, "https://dep-1.my-site.pages.dev", previewURL)

	// A second deployment, then roll back to the first.
	second, err := target.Deploy(DeployRequest{Project: "my-site", Kind: KindPreview, BuildDir: buildDir, Version: "1.1.0"})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)

	live, err := target.Rollback(second)
	require.NoError(t, err)
	assert.Equal(t, first.ID, live.ID, "rollback restores the previous deployment")
	assert.Equal(t, StatusReady, live.Status)

	// The rollback call carried the earlier deployment id.
	var rollbackReq *fakeRequest
	for i := range f.requestsSnapshot() {
		r := f.requestsSnapshot()[i]
		if strings.HasSuffix(r.Path, "/rollback") {
			rollbackReq = &r
		}
	}
	require.NotNil(t, rollbackReq, "a rollback request was sent")
	assert.Contains(t, rollbackReq.Body, first.ID)
}

// TestCloudflarePages_ListOldestFirstAndOrdered asserts history is returned
// oldest first regardless of the order the API lists it in.
func TestCloudflarePages_ListOldestFirstAndOrdered(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-site")
	target := newPagesTarget(t, f, srv, cfSentinelToken)
	buildDir := writeBuildOutput(t, map[string]string{"index.html": "x"})

	for i := 0; i < 3; i++ {
		_, err := target.Deploy(DeployRequest{Project: "my-site", BuildDir: buildDir})
		require.NoError(t, err)
	}

	// Reverse the order the API serves so the adapter must sort.
	f.mu.Lock()
	for i, j := 0, len(f.deployments)-1; i < j; i, j = i+1, j-1 {
		f.deployments[i], f.deployments[j] = f.deployments[j], f.deployments[i]
	}
	f.mu.Unlock()

	history, err := target.List("my-site")
	require.NoError(t, err)
	require.Len(t, history, 3)
	assert.Equal(t, "dep-1", history[0].ID)
	assert.Equal(t, "dep-3", history[2].ID)
	for i := 1; i < len(history); i++ {
		assert.False(t, history[i].CreatedAt.Before(history[i-1].CreatedAt), "oldest first")
	}
}

func TestCloudflarePages_ListUnknownProjectIsEmpty(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-site")
	target := newPagesTarget(t, f, srv, cfSentinelToken)
	got, err := target.List("never-deployed")
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NotNil(t, got)
}

// TestCloudflarePages_ProjectBindingIsEnforced pins that the adapter is
// per-project: a request naming a different project is refused rather than
// silently deployed to the configured one, and a deployment carrying a foreign
// project is unknown to Status/Rollback/PreviewURL.
func TestCloudflarePages_ProjectBindingIsEnforced(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-site")
	target := newPagesTarget(t, f, srv, cfSentinelToken)
	buildDir := writeBuildOutput(t, map[string]string{"index.html": "x"})

	_, err := target.Deploy(DeployRequest{Project: "other-site", BuildDir: buildDir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")
	assert.Empty(t, f.requestsSnapshot(), "a mismatched project sends no request")

	foreign := Deployment{ID: "dep-1", Project: "other-site"}
	_, err = target.Status(foreign)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownDeployment)

	_, err = target.PreviewURL(foreign)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownDeployment)

	_, err = target.Rollback(foreign)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownDeployment)
}

// ---------------------------------------------------------------------------
// Preview vs production
// ---------------------------------------------------------------------------

func TestCloudflarePages_ProductionHasNoPreviewURL(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-site")
	target := newPagesTarget(t, f, srv, cfSentinelToken)
	buildDir := writeBuildOutput(t, map[string]string{"index.html": "x"})

	// Seed a production deployment directly so its branch marks it production.
	f.mu.Lock()
	f.nextID++
	f.deployments = append(f.deployments, fakePagesDeployment{
		ID: "dep-prod", URL: "https://my-site.pages.dev", Stage: "success",
		Branch: "main", CreatedOn: time.Now().UTC(),
	})
	f.mu.Unlock()

	prod := Deployment{ID: "dep-prod", Project: "my-site", Kind: KindProduction}
	_, err := target.PreviewURL(prod)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPreviewUnsupported)

	// Deploying with KindProduction records the production branch.
	d, err := target.Deploy(DeployRequest{Project: "my-site", Kind: KindProduction, BuildDir: buildDir})
	require.NoError(t, err)
	assert.Equal(t, KindProduction, d.Kind)
}

// ---------------------------------------------------------------------------
// Unknown / error paths
// ---------------------------------------------------------------------------

func TestCloudflarePages_UnknownDeployment(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-site")
	target := newPagesTarget(t, f, srv, cfSentinelToken)

	ghost := Deployment{ID: "ghost", Project: "my-site"}
	_, err := target.Status(ghost)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownDeployment)

	_, err = target.PreviewURL(ghost)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownDeployment)
}

func TestCloudflarePages_RollbackNoPreviousDeployment(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-site")
	target := newPagesTarget(t, f, srv, cfSentinelToken)
	buildDir := writeBuildOutput(t, map[string]string{"index.html": "x"})

	only, err := target.Deploy(DeployRequest{Project: "my-site", BuildDir: buildDir})
	require.NoError(t, err)

	_, err = target.Rollback(only)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoPreviousDeployment)
}

func TestCloudflarePages_RollbackUnknownDeployment(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-site")
	target := newPagesTarget(t, f, srv, cfSentinelToken)

	_, err := target.Rollback(Deployment{ID: "ghost", Project: "my-site"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownDeployment)
}

// TestCloudflarePages_NonSuccessResponseIsTyped pins the requirement that a
// non-2xx API response surfaces a typed, wrapped error carrying the status.
func TestCloudflarePages_NonSuccessResponseIsTyped(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-site")
	target := newPagesTarget(t, f, srv, cfSentinelToken)
	buildDir := writeBuildOutput(t, map[string]string{"index.html": "x"})

	f.fail(http.StatusForbidden, `{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`, "/deployments")

	_, err := target.Deploy(DeployRequest{Project: "my-site", BuildDir: buildDir})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCloudflareRequestFailed)

	var reqErr *CloudflareRequestError
	require.ErrorAs(t, err, &reqErr)
	assert.Equal(t, http.StatusForbidden, reqErr.StatusCode)
	require.Len(t, reqErr.Errors, 1)
	assert.Equal(t, 10000, reqErr.Errors[0].Code)
	assert.Contains(t, reqErr.Error(), "403")
	assert.Contains(t, reqErr.Message, "Authentication error")
}

// ---------------------------------------------------------------------------
// The token never leaks
// ---------------------------------------------------------------------------

// TestCloudflarePages_TokenNeverLeaks is the absence assertion: the token
// reaches the wire only in the Authorization header, and never appears in a
// returned deployment, an error, or a request body.
func TestCloudflarePages_TokenNeverLeaks(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-site")
	target := newPagesTarget(t, f, srv, cfSentinelToken)
	buildDir := writeBuildOutput(t, map[string]string{"index.html": "hello"})

	d, err := target.Deploy(DeployRequest{Project: "my-site", BuildDir: buildDir, Version: "1.2.3"})
	require.NoError(t, err)

	// The auth header carries the token; no request body does.
	reqs := f.requestsSnapshot()
	require.NotEmpty(t, reqs)
	sawAuth := false
	for _, r := range reqs {
		assert.NotContains(t, r.Body, cfSentinelToken, "token must not appear in a request body")
		assert.NotContains(t, r.Path, cfSentinelToken, "token must not appear in the request path")
		if strings.Contains(r.Auth, cfSentinelToken) {
			sawAuth = true
		}
	}
	assert.True(t, sawAuth, "the token is sent in the Authorization header")

	// The returned deployment must not carry the token.
	b, err := json.Marshal(d)
	require.NoError(t, err)
	assert.NotContains(t, string(b), cfSentinelToken)

	// A non-2xx error whose body echoes the token is sanitized.
	f.fail(http.StatusBadRequest, `{"success":false,"errors":[{"code":1,"message":"bad token `+cfSentinelToken+`"}]}`, "/deployments")
	_, err = target.Deploy(DeployRequest{Project: "my-site", BuildDir: buildDir})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), cfSentinelToken, "an error must never carry the token")
	assert.Contains(t, err.Error(), "[REDACTED]")

	// An error body that echoes the Authorization header is scrubbed.
	f.fail(http.StatusUnauthorized, `{"success":false,"errors":[{"code":2,"message":"authorization: Bearer `+cfSentinelToken+` is invalid"}]}`, "/deployments")
	_, err = target.Deploy(DeployRequest{Project: "my-site", BuildDir: buildDir})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), cfSentinelToken)
}

// ---------------------------------------------------------------------------
// Construction and validation
// ---------------------------------------------------------------------------

func TestNewCloudflarePagesTarget_Validation(t *testing.T) {
	_, srv := newCloudflareFake(t, "acct-1", "my-site")
	cred := Credential{value: cfSentinelToken}

	_, err := NewCloudflarePagesTarget(CloudflareConfig{AccountID: "", Project: "p"}, cred, srv.URL, srv.Client())
	require.Error(t, err)

	_, err = NewCloudflarePagesTarget(CloudflareConfig{AccountID: "a", Project: ""}, cred, srv.URL, srv.Client())
	require.Error(t, err)

	_, err = NewCloudflarePagesTarget(CloudflareConfig{AccountID: "a", Project: "p"}, Credential{}, srv.URL, srv.Client())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")

	_, err = NewCloudflarePagesTarget(CloudflareConfig{AccountID: "a", Project: "p"}, cred, "not-a-url", srv.Client())
	require.Error(t, err)

}

func TestNewCloudflarePagesTarget_DefaultsBaseURLAndClient(t *testing.T) {
	target, err := NewCloudflarePagesTarget(
		CloudflareConfig{AccountID: "acct", Project: "site"},
		Credential{value: cfSentinelToken},
		"", nil,
	)
	require.NoError(t, err)
	assert.Equal(t, DefaultCloudflareAPIBaseURL, target.t.base)
	require.NotNil(t, target.t.http)
	assert.NotZero(t, target.t.http.Timeout)
}

func TestCloudflareDeploy_RejectsInvalidRequests(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-site")
	target := newPagesTarget(t, f, srv, cfSentinelToken)
	buildDir := writeBuildOutput(t, map[string]string{"index.html": "x"})

	cases := []struct {
		name string
		req  DeployRequest
	}{
		{"empty project", DeployRequest{BuildDir: buildDir}},
		{"empty build dir", DeployRequest{Project: "my-site"}},
		{"unknown kind", DeployRequest{Project: "my-site", BuildDir: buildDir, Kind: "staging"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := target.Deploy(tc.req)
			require.Error(t, err)
			assert.Empty(t, f.requestsSnapshot(), "a rejected deploy sends no request")
		})
	}
}

func TestCloudflareDeploy_MissingBuildDirIsRefused(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-site")
	target := newPagesTarget(t, f, srv, cfSentinelToken)

	_, err := target.Deploy(DeployRequest{Project: "my-site", BuildDir: filepath.Join(t.TempDir(), "does-not-exist")})
	require.Error(t, err)
	assert.Empty(t, f.requestsSnapshot(), "a missing build directory sends no request")
}

func TestCollectAssets_EmptyDirectoryIsRefused(t *testing.T) {
	_, err := collectAssets(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no files")
}

// ---------------------------------------------------------------------------
// Workers
// ---------------------------------------------------------------------------

func TestCloudflareWorkers_DeployAndStatus(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-api")
	target, err := NewCloudflareWorkersTarget(
		CloudflareConfig{AccountID: "acct-1", Project: "my-api"},
		Credential{value: cfSentinelToken},
		srv.URL, srv.Client(),
	)
	require.NoError(t, err)

	buildDir := writeBuildOutput(t, map[string]string{"worker.js": "export default { fetch() {} }"})
	d, err := target.Deploy(DeployRequest{Project: "my-api", BuildDir: buildDir, Version: "2.0.0"})
	require.NoError(t, err)
	assert.Equal(t, "my-api", d.ID)
	assert.Contains(t, d.URL, "my-api.acct-1.workers.dev")
	assert.Equal(t, StatusReady, d.Status)

	state, err := target.Status(d)
	require.NoError(t, err)
	assert.Equal(t, StatusReady, state)

	history, err := target.List("my-api")
	require.NoError(t, err)
	require.Len(t, history, 1)

	// The script was uploaded with the token only in the header.
	reqs := f.requestsSnapshot()
	found := false
	for _, r := range reqs {
		if strings.Contains(r.Path, "/workers/scripts/") {
			found = true
			assert.Contains(t, r.Body, "export default")
			assert.NotContains(t, r.Body, cfSentinelToken)
		}
	}
	assert.True(t, found, "the worker script was uploaded")
}

func TestCloudflareWorkers_HasNoPreviewsOrRollback(t *testing.T) {
	_, srv := newCloudflareFake(t, "acct-1", "my-api")
	target, err := NewCloudflareWorkersTarget(
		CloudflareConfig{AccountID: "acct-1", Project: "my-api"},
		Credential{value: cfSentinelToken},
		srv.URL, srv.Client(),
	)
	require.NoError(t, err)

	d := Deployment{ID: "my-api", Project: "my-api"}
	_, err = target.PreviewURL(d)
	assert.ErrorIs(t, err, ErrPreviewUnsupported)

	_, err = target.Rollback(d)
	assert.ErrorIs(t, err, ErrNoPreviousDeployment)

	_, err = target.Status(Deployment{ID: "other", Project: "other"})
	assert.ErrorIs(t, err, ErrUnknownDeployment)
}

func TestCloudflareWorkers_MissingScriptIsRefused(t *testing.T) {
	_, srv := newCloudflareFake(t, "acct-1", "my-api")
	target, err := NewCloudflareWorkersTarget(
		CloudflareConfig{AccountID: "acct-1", Project: "my-api"},
		Credential{value: cfSentinelToken},
		srv.URL, srv.Client(),
	)
	require.NoError(t, err)

	buildDir := writeBuildOutput(t, map[string]string{"index.html": "<h1>not a worker</h1>"})
	_, err = target.Deploy(DeployRequest{Project: "my-api", BuildDir: buildDir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "worker.js")
}

func TestCloudflareWorkers_ProjectBindingIsEnforced(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-api")
	target, err := NewCloudflareWorkersTarget(
		CloudflareConfig{AccountID: "acct-1", Project: "my-api"},
		Credential{value: cfSentinelToken},
		srv.URL, srv.Client(),
	)
	require.NoError(t, err)

	buildDir := writeBuildOutput(t, map[string]string{"worker.js": "export default {}"})
	_, err = target.Deploy(DeployRequest{Project: "other", BuildDir: buildDir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")
	assert.Empty(t, f.requestsSnapshot(), "a mismatched worker name sends no request")
}

func TestCloudflareWorkers_NonSuccessResponseIsTyped(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-api")
	target, err := NewCloudflareWorkersTarget(
		CloudflareConfig{AccountID: "acct-1", Project: "my-api"},
		Credential{value: cfSentinelToken},
		srv.URL, srv.Client(),
	)
	require.NoError(t, err)

	f.fail(http.StatusForbidden, `{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`, "/workers/scripts/")

	buildDir := writeBuildOutput(t, map[string]string{"worker.js": "export default {}"})
	_, err = target.Deploy(DeployRequest{Project: "my-api", BuildDir: buildDir})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCloudflareRequestFailed)

	var reqErr *CloudflareRequestError
	require.ErrorAs(t, err, &reqErr)
	assert.Equal(t, http.StatusForbidden, reqErr.StatusCode)
	assert.NotContains(t, err.Error(), cfSentinelToken)
}

// TestCloudflarePages_NonEnvelopeBodyIsHandled pins that a non-2xx response
// whose body is not a Cloudflare envelope still yields the typed error (and
// never embeds the raw body).
func TestCloudflarePages_NonEnvelopeBodyIsHandled(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-site")
	target := newPagesTarget(t, f, srv, cfSentinelToken)
	buildDir := writeBuildOutput(t, map[string]string{"index.html": "x"})

	f.fail(http.StatusBadGateway, `<html>502 Bad Gateway</html>`, "/deployments")

	_, err := target.Deploy(DeployRequest{Project: "my-site", BuildDir: buildDir})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCloudflareRequestFailed)
	var reqErr *CloudflareRequestError
	require.ErrorAs(t, err, &reqErr)
	assert.Equal(t, http.StatusBadGateway, reqErr.StatusCode)
	assert.NotContains(t, err.Error(), "<html>")
}

// TestCloudflarePages_ConcurrentDeploys asserts the adapter is safe for
// concurrent use (the interface requires it) and that each deploy still
// records a distinct deployment.
func TestCloudflarePages_ConcurrentDeploys(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-site")
	target := newPagesTarget(t, f, srv, cfSentinelToken)
	buildDir := writeBuildOutput(t, map[string]string{"index.html": "x"})

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err := target.Deploy(DeployRequest{Project: "my-site", BuildDir: buildDir})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	history, err := target.List("my-site")
	require.NoError(t, err)
	assert.Len(t, history, n)
	seen := map[string]bool{}
	for _, d := range history {
		require.False(t, seen[d.ID], "ids are unique")
		seen[d.ID] = true
	}
}
