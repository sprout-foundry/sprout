package webcontent

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFetchDirectURL_404HTMLError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`<html><body><h1>Page Not Found</h1><p>The resource does not exist.</p></body></html>`))
	}))
	defer ts.Close()

	fetcher := NewWebContentFetcher()
	_, err := fetcher.fetchDirectURL(ts.URL)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404", "error should contain the status code")
	assert.Contains(t, err.Error(), "Server response:", "error should contain the server response label")
	assert.Contains(t, err.Error(), "Page Not Found", "error should contain extracted text from the HTML")
	assert.Contains(t, err.Error(), ts.URL, "error should contain the URL")
}

func TestFetchDirectURL_502HTMLError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`<html><body><h1>Bad Gateway</h1><p>The upstream server is unreachable.</p></body></html>`))
	}))
	defer ts.Close()

	fetcher := NewWebContentFetcher()
	_, err := fetcher.fetchDirectURL(ts.URL)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 502", "error should contain the status code")
	assert.Contains(t, err.Error(), "Server response:", "error should contain the server response label")
	assert.Contains(t, err.Error(), "Bad Gateway", "error should contain extracted text from the HTML")
}

func TestFetchDirectURL_404NonHTMLError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer ts.Close()

	fetcher := NewWebContentFetcher()
	_, err := fetcher.fetchDirectURL(ts.URL)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404", "error should contain the status code")
	assert.NotContains(t, err.Error(), "Server response:",
		"non-HTML error responses should NOT include the 'Server response:' label")
	assert.NotContains(t, err.Error(), "not found",
		"non-HTML error responses should NOT include the response body text")
}

func TestFetchDirectURL_200HTML(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><h1>Hello</h1><p>Welcome to the page.</p></body></html>`))
	}))
	defer ts.Close()

	fetcher := NewWebContentFetcher()
	content, err := fetcher.fetchDirectURL(ts.URL)

	assert.NoError(t, err)
	assert.Contains(t, content, "Hello", "successful response should contain the HTML text content")
	assert.Contains(t, content, "Welcome to the page.")
}
