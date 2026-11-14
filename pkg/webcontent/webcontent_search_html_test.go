package webcontent

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

const duckDuckGoHTMLFixture = `<html><body>
<div class="result results_links web-result">
  <h2 class="result__title"><a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Ffirecracker%2Dmicrovm.github.io%2F&amp;rut=x">Firecracker <b>microVM</b></a></h2>
  <a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Ffirecracker%2Dmicrovm.github.io%2F">Secure and fast <b>microVMs</b> for serverless computing.</a>
</div>
<div class="result results_links web-result">
  <h2 class="result__title"><a class="result__a" href="https://github.com/firecracker-microvm/firecracker">firecracker-microvm/firecracker</a></h2>
</div>
<div class="result"><a class="result__a" href="javascript:void(0)">not a page</a></div>
</body></html>`

func TestParseDuckDuckGoHTML(t *testing.T) {
	got := parseDuckDuckGoHTML(duckDuckGoHTMLFixture)
	if len(got) != 2 {
		t.Fatalf("got %d results: %+v", len(got), got)
	}
	if got[0].Title != "Firecracker microVM" || got[0].URL != "https://firecracker-microvm.github.io/" || got[0].Description != "Secure and fast microVMs for serverless computing." {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].URL != "https://github.com/firecracker-microvm/firecracker" || got[1].Description != "" {
		t.Errorf("second = %+v", got[1])
	}
}

// The search posts the query to the HTML page, and a page with no results
// answers an empty list, not a placeholder.
func TestPerformDuckDuckGoSearchPostsAndParses(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotQuery = r.PostForm.Get("q")
		if gotQuery == "nothing" {
			_, _ = w.Write([]byte("<html><body>No results.</body></html>"))
			return
		}
		_, _ = w.Write([]byte(duckDuckGoHTMLFixture))
	}))
	defer srv.Close()
	old := duckDuckGoHTMLURL
	duckDuckGoHTMLURL = srv.URL
	defer func() { duckDuckGoHTMLURL = old }()

	logger := utils.GetLogger(false)
	results, err := performDuckDuckGoSearch("firecracker microvm", logger)
	if err != nil || len(results) != 2 || gotQuery != "firecracker microvm" {
		t.Fatalf("results=%d err=%v query=%q", len(results), err, gotQuery)
	}
	results, err = performDuckDuckGoSearch("nothing", logger)
	if err != nil || len(results) != 0 {
		t.Fatalf("no results: %+v %v", results, err)
	}
}
