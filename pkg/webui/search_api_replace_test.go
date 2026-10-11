//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// replaceRequest posts a replace request for test-client and returns the
// status, the decoded response and the file's content afterwards.
func replaceRequest(t *testing.T, content string, req ReplaceRequest) (int, ReplaceResponse, string) {
	t.Helper()
	ws, _ := newTestWebServer(t)
	root := ws.getWorkspaceRootForRequest(clientRequest())
	path := filepath.Join(root, "src", "a.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if req.Files == nil {
		req.Files = []string{"src/a.txt"}
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/search/replace", strings.NewReader(string(body)))
	r.Header.Set(webClientIDHeader, "test-client")
	w := httptest.NewRecorder()
	ws.handleAPIQuerySearchReplace(w, r)

	var resp ReplaceResponse
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v (body %s)", err, w.Body.String())
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return w.Code, resp, string(after)
}

func clientRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(webClientIDHeader, "test-client")
	return r
}

func TestSearchReplace_Options(t *testing.T) {
	cases := []struct {
		name    string
		content string
		req     ReplaceRequest
		want    string
		total   int
	}{
		{"literal query does not match as a regex", "a.b axb a.b\nAXB", ReplaceRequest{Search: "a.b", Replace: "X", CaseSensitive: true}, "X axb X\nAXB", 2},
		{"case-insensitive by default", "Foo foo FOO", ReplaceRequest{Search: "foo", Replace: "x"}, "x x x", 3},
		{"case-sensitive", "Foo foo FOO", ReplaceRequest{Search: "foo", Replace: "x", CaseSensitive: true}, "Foo x FOO", 1},
		{"whole word", "cat concat cat_x cat.", ReplaceRequest{Search: "cat", Replace: "dog", WholeWord: true}, "dog concat cat_x dog.", 2},
		{"whole word bounds a whole regex alternation", "catalog hotdog cat", ReplaceRequest{Search: "cat|dog", Replace: "pet", Regex: true, WholeWord: true}, "catalog hotdog pet", 1},
		{"regex", "a1 a22 b3", ReplaceRequest{Search: `a\d+`, Replace: "N", Regex: true}, "N N b3", 2},
		{"leading-dash query", "run --force -f", ReplaceRequest{Search: "--force", Replace: "--yes"}, "run --yes -f", 1},
		{"replacement is literal", "foo(bar)", ReplaceRequest{Search: `(\w+)\((\w+)\)`, Replace: `$2 $1 $& \1 &`, Regex: true}, `$2 $1 $& \1 &`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, resp, after := replaceRequest(t, tc.content, tc.req)
			if code != http.StatusOK {
				t.Fatalf("status = %d", code)
			}
			if after != tc.want {
				t.Errorf("file = %q, want %q", after, tc.want)
			}
			if resp.TotalChanges != tc.total {
				t.Errorf("total_changes = %d, want %d", resp.TotalChanges, tc.total)
			}
		})
	}
}

func TestSearchReplace_ResponseShapeAndPreview(t *testing.T) {
	code, resp, after := replaceRequest(t, "x needle needle\nnone", ReplaceRequest{Search: "needle", Replace: "pin", Preview: true})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if after != "x needle needle\nnone" {
		t.Errorf("preview wrote the file: %q", after)
	}
	if len(resp.Changes) != 1 || resp.Changes[0].File != "src/a.txt" || resp.Changes[0].ChangedLines != 1 {
		t.Fatalf("changes = %+v", resp.Changes)
	}
	want := []ReplaceMatch{
		{LineNumber: 1, OldLine: "x needle needle", NewLine: "x pin pin", ColumnStart: 3, ColumnEnd: 9},
		{LineNumber: 1, OldLine: "x needle needle", NewLine: "x pin pin", ColumnStart: 10, ColumnEnd: 16},
	}
	got := resp.Changes[0].Matches
	if len(got) != len(want) {
		t.Fatalf("matches = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("match %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if resp.TotalChanges != 2 || !resp.Preview {
		t.Errorf("total_changes = %d preview = %v", resp.TotalChanges, resp.Preview)
	}
}

func TestSearchReplace_RefusesPathOutsideWorkspace(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("needle"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, _ := replaceRequest(t, "needle", ReplaceRequest{Search: "needle", Replace: "pin", Files: []string{"src/a.txt", outside}})
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
	if data, _ := os.ReadFile(outside); string(data) != "needle" {
		t.Errorf("outside file rewritten: %q", data)
	}
}
