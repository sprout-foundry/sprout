package agent

// image_payload_errors.go — recognising a malformed inline image payload.
//
// Attached images reach the provider as a base64 data URL. If the encoded
// bytes are not valid base64 (a truncated paste, a corrupted session record,
// a tool that produced a data URI with stray characters), the provider
// rejects the request with a 400 whose text mentions base64 — and the error
// then travels the generic provider-error path: wrapped as a network
// failure, retried, and finally reported as "Authentication failed … check
// your API key and configuration". Nothing about the API key is wrong; the
// request body is.
//
// Two things are needed, and they are different:
//
//   - Classification: the turn must fail fast with a message naming the
//     actual cause, instead of a retry loop that ends in an auth message.
//   - Recovery: the offending images must be dropped before the next
//     attempt, so a session that carries one bad attachment is not
//     permanently unusable. That is what stripInvalidImagePayloads does.

import (
	"encoding/base64"
	"fmt"
	"strings"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// base64RejectionMarkers are the substrings providers use when they refuse a
// request because an inline image payload is not valid base64. Matched
// case-insensitively against the error text.
var base64RejectionMarkers = []string{
	"non-base64 digit",
	"illegal base64",
	"invalid base64",
	"base64 decode",
	"base64 encoding",
	"corrupt base64",
}

// isImagePayloadError reports whether err is a provider rejection caused by a
// malformed inline image payload rather than a transport or credential
// problem. It is deliberately narrow: the marker must be present, so an
// ordinary 400 or a genuine auth failure is unaffected.
func isImagePayloadError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range base64RejectionMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// stripInvalidImagePayloads returns messages with every image whose base64
// payload does not decode removed, and a note appended to the affected
// message so the model knows content was withheld rather than never sent.
// Messages without images are returned unchanged (the slice is returned
// as-is when nothing was stripped).
//
// A URL-only image (no inline bytes) is left alone: there is nothing local
// to validate, and the provider fetches it.
func stripInvalidImagePayloads(messages []api.Message) []api.Message {
	out := make([]api.Message, len(messages))
	copy(out, messages)
	stripped := false

	for i := range out {
		if len(out[i].Images) == 0 {
			continue
		}
		kept := make([]api.ImageData, 0, len(out[i].Images))
		for _, img := range out[i].Images {
			if imagePayloadIsValid(img) {
				kept = append(kept, img)
			}
		}
		if len(kept) == len(out[i].Images) {
			continue
		}
		stripped = true
		out[i].Images = kept
		note := "\n\n[image content withheld: the attached image data was not valid base64 and could not be sent]"
		if !strings.Contains(out[i].Content, note) {
			out[i].Content += note
		}
	}

	if !stripped {
		return messages
	}
	return out
}

// messagesImageCount counts the image parts across messages.
func messagesImageCount(messages []api.Message) int {
	n := 0
	for i := range messages {
		n += len(messages[i].Images)
	}
	return n
}

// imagePayloadIsValid reports whether an image can be sent: either it has a
// URL, or its inline base64 decodes.
func imagePayloadIsValid(img api.ImageData) bool {
	if strings.TrimSpace(img.URL) != "" {
		return true
	}
	encoded := strings.TrimSpace(img.Base64)
	if encoded == "" {
		return false
	}
	// Data URLs are accepted in the Base64 field by some call sites; strip
	// the prefix before validating the payload.
	if idx := strings.Index(encoded, ";base64,"); idx >= 0 {
		encoded = encoded[idx+len(";base64,"):]
	}
	encoded = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t':
			return -1
		}
		return r
	}, encoded)

	_, err := base64.StdEncoding.DecodeString(encoded)
	return err == nil
}

// classifyImagePayloadError converts a provider rejection of a malformed
// image payload into a non-retryable ClientError that names the real cause,
// so the turn fails once with an actionable message instead of retrying and
// reporting an authentication failure. Returns nil when err is not an image
// payload rejection.
func classifyImagePayloadError(err error) error {
	if !isImagePayloadError(err) {
		return nil
	}
	return &core.ClientError{
		Wrapped: fmt.Errorf("the request carried an image whose base64 payload is malformed: %w", err),
	}
}
