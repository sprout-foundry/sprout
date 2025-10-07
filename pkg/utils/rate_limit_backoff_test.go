package utils

import (
    "errors"
    "net/http"
    "testing"
)

func TestRateLimitErrorQuotaMessage(t *testing.T) {
	rlb := NewRateLimitBackoff()
	err := errors.New("OpenAI API error: You exceeded your current quota, please check your plan and billing details.")

	if !rlb.IsRateLimitError(err, nil) {
		t.Fatalf("expected quota message to be treated as rate limit")
	}
}

func TestRateLimitErrorInsufficientQuota(t *testing.T) {
    rlb := NewRateLimitBackoff()
    err := errors.New("OpenAI API error: insufficient_quota")

    if !rlb.IsRateLimitError(err, nil) {
        t.Fatalf("expected insufficient_quota to be treated as rate limit")
    }
}

func TestRateLimitErrorStatus429Variants(t *testing.T) {
    rlb := NewRateLimitBackoff()
    cases := []string{
        "OpenRouter API error (status 429): Too many requests",
        "status 429",
        "HTTP 429 rate limit",
        "429",
    }
    for _, msg := range cases {
        if !rlb.IsRateLimitError(errors.New(msg), nil) {
            t.Fatalf("expected %q to be treated as rate limit", msg)
        }
    }
}

func TestRateLimitFromHTTPResponse(t *testing.T) {
    rlb := NewRateLimitBackoff()
    resp := &http.Response{StatusCode: 429}
    if !rlb.IsRateLimitError(nil, resp) {
        t.Fatalf("expected HTTP 429 response to be treated as rate limit")
    }
}

func TestNonRateLimitError(t *testing.T) {
    rlb := NewRateLimitBackoff()
    err := errors.New("upstream error 502")
    if rlb.IsRateLimitError(err, nil) {
        t.Fatalf("did not expect 502 to be treated as rate limit")
    }
}
