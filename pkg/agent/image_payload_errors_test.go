package agent

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func validBase64Image() string {
	// 1x1 PNG.
	return base64.StdEncoding.EncodeToString([]byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	})
}

func TestIsImagePayloadError(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want bool
	}{
		{"provider base64 rejection", "[Provider] Non-base64 digit found", true},
		{"go decode error", "illegal base64 data at input byte 12", true},
		{"invalid base64 phrasing", "invalid base64 in image_url", true},
		{"auth failure", "HTTP 401: invalid api key", false},
		{"rate limit", "HTTP 429: rate limit exceeded", false},
		{"transport", "connection reset by peer", false},
		{"context overflow", "maximum context length is 128000 tokens", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isImagePayloadError(errors.New(tc.msg)); got != tc.want {
				t.Errorf("isImagePayloadError(%q) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
	if isImagePayloadError(nil) {
		t.Error("nil error must not be an image payload error")
	}
}

func TestStripInvalidImagePayloads(t *testing.T) {
	valid := validBase64Image()

	t.Run("drops the undecodable image and notes it", func(t *testing.T) {
		messages := []api.Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "look", Images: []api.ImageData{
				{Base64: valid, Type: "image/png"},
				{Base64: "!!!not-base64!!!", Type: "image/png"},
			}},
		}
		got := stripInvalidImagePayloads(messages)
		if len(got) != len(messages) {
			t.Fatalf("message count changed: %d -> %d", len(messages), len(got))
		}
		if n := len(got[1].Images); n != 1 {
			t.Fatalf("expected 1 surviving image, got %d", n)
		}
		if got[1].Images[0].Base64 != valid {
			t.Error("the valid image was not the one kept")
		}
		if !strings.Contains(got[1].Content, "not valid base64") {
			t.Errorf("expected a withholding note, got %q", got[1].Content)
		}
		if messages[1].Content != "look" {
			t.Error("input messages must not be mutated")
		}
	})

	t.Run("leaves a clean message list untouched", func(t *testing.T) {
		messages := []api.Message{
			{Role: "user", Content: "look", Images: []api.ImageData{{Base64: valid, Type: "image/png"}}},
		}
		got := stripInvalidImagePayloads(messages)
		if len(got[0].Images) != 1 || got[0].Content != "look" {
			t.Errorf("clean messages changed: %+v", got)
		}
	})

	t.Run("keeps URL-only images", func(t *testing.T) {
		messages := []api.Message{
			{Role: "user", Content: "look", Images: []api.ImageData{{URL: "https://example.com/a.png"}}},
		}
		got := stripInvalidImagePayloads(messages)
		if len(got[0].Images) != 1 {
			t.Error("a URL-only image has nothing local to validate and must survive")
		}
	})

	t.Run("accepts a data URL in the Base64 field", func(t *testing.T) {
		messages := []api.Message{
			{Role: "user", Content: "look", Images: []api.ImageData{
				{Base64: "data:image/png;base64," + valid, Type: "image/png"},
			}},
		}
		got := stripInvalidImagePayloads(messages)
		if len(got[0].Images) != 1 {
			t.Error("a valid data URL must survive")
		}
	})

	t.Run("tolerates whitespace inside the payload", func(t *testing.T) {
		wrapped := valid[:4] + "\n" + valid[4:]
		messages := []api.Message{
			{Role: "user", Content: "look", Images: []api.ImageData{{Base64: wrapped, Type: "image/png"}}},
		}
		got := stripInvalidImagePayloads(messages)
		if len(got[0].Images) != 1 {
			t.Error("line-wrapped base64 decodes once whitespace is removed and must survive")
		}
	})
}

func TestClassifyImagePayloadError(t *testing.T) {
	err := classifyImagePayloadError(errors.New("[Provider] Non-base64 digit found"))
	if err == nil {
		t.Fatal("expected a classified error")
	}
	if !core.IsClientError(err) {
		t.Errorf("expected a non-retryable ClientError, got %T", err)
	}
	if core.IsAuthError(err) {
		t.Error("an image payload rejection must not read as an auth failure")
	}
	if !strings.Contains(err.Error(), "base64") {
		t.Errorf("the message must name the cause, got %q", err.Error())
	}

	if got := classifyImagePayloadError(errors.New("HTTP 401: invalid api key")); got != nil {
		t.Errorf("non-image errors must not be classified, got %v", got)
	}
}

func TestWrapError_ImagePayloadIsNotAnAuthFailure(t *testing.T) {
	classified := core.ClassifyError(classifyImagePayloadError(errors.New("Non-base64 digit found")), "deepseek")
	got := wrapError(classified)
	if strings.Contains(got, "Authentication failed") {
		t.Errorf("a malformed image payload must not be reported as an auth failure: %s", got)
	}
	if !strings.Contains(got, "base64") {
		t.Errorf("the message must name the cause: %s", got)
	}
}

func TestMessagesImageCount(t *testing.T) {
	messages := []api.Message{
		{Role: "system"},
		{Role: "user", Images: []api.ImageData{{Base64: "a"}, {Base64: "b"}}},
		{Role: "assistant", Images: []api.ImageData{{URL: "u"}}},
	}
	if got := messagesImageCount(messages); got != 3 {
		t.Errorf("messagesImageCount = %d, want 3", got)
	}
}
