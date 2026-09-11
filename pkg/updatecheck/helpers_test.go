package updatecheck

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// withTempStateDir points envutil.StateDir at a throwaway directory for
// the duration of the test so tests never touch the real ~/.local/state.
func withTempStateDir(t *testing.T) {
	t.Helper()
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())
}

// withTestReleasesURL points the release fetch at an httptest server
// returning body for the duration of the test.
func withTestReleasesURL(t *testing.T, body string) func() {
	t.Helper()
	return withTestReleasesURLStatus(t, body, http.StatusOK)
}

func withTestReleasesURLStatus(t *testing.T, body string, status int) func() {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	old := releasesURL
	releasesURL = srv.URL
	return func() {
		releasesURL = old
		srv.Close()
	}
}
