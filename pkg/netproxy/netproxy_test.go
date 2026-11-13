package netproxy

import (
	"net/http"
	"net/url"
	"testing"
)

// Requests go through the proxy except to loopback hosts, and nothing is
// changed when no proxy is set.
func TestApply(t *testing.T) {
	t.Setenv(Env, "")
	if Apply() {
		t.Fatal("applied with no proxy set")
	}
	t.Setenv(Env, "http://127.0.0.1:8082")
	tr := http.DefaultTransport.(*http.Transport)
	old := tr.Proxy
	t.Cleanup(func() { tr.Proxy = old })
	if !Apply() {
		t.Fatal("not applied")
	}
	for host, want := range map[string]bool{"example.com": true, "127.0.0.1": false, "localhost": false, "[::1]": false} {
		req := &http.Request{URL: &url.URL{Scheme: "https", Host: host}}
		got, err := tr.Proxy(req)
		if err != nil {
			t.Fatal(err)
		}
		if (got != nil) != want {
			t.Errorf("%s: proxied=%v, want %v", host, got != nil, want)
		}
	}
}
