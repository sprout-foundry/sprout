package utils

import (
	"errors"
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
