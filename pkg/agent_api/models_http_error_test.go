package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestFormatHTTPResponseError_SummarizesCloudflareHTML(t *testing.T) {
	headers := http.Header{}
	headers.Set("Content-Type", "text/html; charset=utf-8")
	body := []byte(`<!DOCTYPE html>
<html>
<head><title>local-aprice.dev | 524: A timeout occurred</title></head>
<body>
<div>Cloudflare</div>
<div>Error code 524</div>
</body>
</html>`)

	err := FormatHTTPResponseError(524, headers, body)
	if err == nil {
		t.Fatal("expected error")
	}
	got := err.Error()
	if !strings.Contains(got, "HTTP 524: upstream timeout (Cloudflare 524 HTML error page)") {
		t.Fatalf("unexpected error: %s", got)
	}
	if strings.Contains(strings.ToLower(got), "<!doctype html") || strings.Contains(got, "<html") {
		t.Fatalf("expected HTML body to be suppressed, got: %s", got)
	}
}

func TestFormatHTTPResponseError_ExtractsJSONMessage(t *testing.T) {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	body := []byte(`{"error":{"message":"model not available for this account"}}`)

	err := FormatHTTPResponseError(http.StatusBadRequest, headers, body)
	if err == nil {
		t.Fatal("expected error")
	}
	got := err.Error()
	if got != "HTTP 400: model not available for this account" {
		t.Fatalf("unexpected error: %s", got)
	}
}

func TestFormatHTTPResponseError_TruncatesPlainText(t *testing.T) {
	headers := http.Header{}
	headers.Set("Content-Type", "text/plain; charset=utf-8")
	body := []byte(strings.Repeat("backend overload ", 40))

	err := FormatHTTPResponseError(http.StatusBadGateway, headers, body)
	if err == nil {
		t.Fatal("expected error")
	}
	got := err.Error()
	if !strings.HasPrefix(got, "HTTP 502: ") {
		t.Fatalf("unexpected error prefix: %s", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected truncated error to end with ellipsis, got: %s", got)
	}
	if len(got) > len("HTTP 502: ")+maxHTTPErrorBodyPreview+10 {
		t.Fatalf("expected truncated error, got len=%d: %s", len(got), got)
	}
}
