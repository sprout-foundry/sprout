// HTTP plumbing shared by the Cloudflare adapters: the request/response
// envelope, the auth header, and the typed error every non-2xx response
// becomes.
//
// The token is threaded through as an explicit argument — never stored on a
// request struct — so it exists only in the call stack and in the auth header
// that doRequest sets. A failed response's body is sanitized before it reaches
// an error: Cloudflare's own messages are scrubbed of any credential-looking
// substring, so a token echoed back by the API can never land in a log line.

package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/redact"
)

// cloudflareEnvelope is the common Cloudflare API response wrapper. The
// adapters decode this to classify a non-2xx response, then decode Result into
// their own shape.
type cloudflareEnvelope struct {
	Success  bool              `json:"success"`
	Errors   []cloudflareError `json:"errors"`
	Messages []string          `json:"messages"`
	Result   json.RawMessage   `json:"result"`
}

// cloudflareError is one structured error entry from the envelope.
type cloudflareError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// doRequest issues one authenticated Cloudflare API call and returns the raw
// response body of a successful (2xx) call. Any other outcome — a transport
// error, a non-2xx status, or an unreadable body — is returned as an error;
// a non-2xx status is a *CloudflareRequestError wrapping
// ErrCloudflareRequestFailed.
//
// token is passed explicitly so it is never persisted on a value that could be
// serialized or logged. The request's auth header is set here and nowhere
// else; nothing in this function or its error path writes the token to an
// error, a message, or stdout.
func (t *CloudflareTarget) doRequest(ctx context.Context, token, method, path string, body any) ([]byte, error) {
	var reader io.Reader
	contentType := ""
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("cloudflare: encode request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
		contentType = "application/json"
	}
	return t.doRawRequest(ctx, token, method, path, reader, contentType)
}

// doRawRequest is doRequest with a caller-supplied body reader and content
// type. It exists so a caller with a non-JSON body (a Worker script) still
// goes through the single place that sets the auth header and classifies a
// non-2xx response. When body is nil the request is sent with no body.
func (t *CloudflareTarget) doRawRequest(ctx context.Context, token, method, path string, body io.Reader, contentType string) ([]byte, error) {
	url := t.base + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := t.http.Do(req)
	if err != nil {
		// A transport error may quote the URL (which never carries the token)
		// but never the header, so the wrapped message is safe to surface.
		return nil, fmt.Errorf("cloudflare: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: read response for %s %s: %w", method, path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newCloudflareRequestError(method, path, resp.StatusCode, data, token)
	}
	return data, nil
}

// newCloudflareRequestError builds the typed error for a non-2xx response,
// salvaging the API's structured errors and sanitizing every message. When the
// body is not a Cloudflare envelope, a short fallback message is used; the raw
// body is never embedded, so arbitrary server output cannot smuggle a secret
// into an error.
//
// token is passed so the API's own messages can be scrubbed of the exact
// credential value: even if Cloudflare echoes the token back, it is removed
// before the error leaves this function. The token is not stored on the
// returned error.
func newCloudflareRequestError(method, path string, status int, data []byte, token string) error {
	if len(data) >= maxCloudflareErrorBody {
		data = data[:maxCloudflareErrorBody]
	}
	out := &CloudflareRequestError{
		Op:         operationName(method, path),
		StatusCode: status,
	}
	if len(data) == 0 {
		return out
	}

	var env cloudflareEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		out.Message = "unexpected response body (not a Cloudflare error envelope)"
		return out
	}
	for _, e := range env.Errors {
		out.Errors = append(out.Errors, CloudflareError{Code: e.Code, Message: sanitizeCloudflareMessage(e.Message, token)})
	}
	for _, m := range env.Messages {
		if s := sanitizeCloudflareMessage(m, token); s != "" {
			out.Message = s
			break
		}
	}
	if out.Message == "" && len(out.Errors) > 0 {
		out.Message = out.Errors[0].Message
	}
	return out
}

// opNames maps a request path suffix to a short operation name for error
// messages, so an error reads "cloudflare: create deployment: 403 …" rather
// than echoing a full URL.
var opNames = []struct {
	suffix string
	name   string
}{
	{"/deployments", "deployment"},
	{"/deployments/", "project deployment"},
	{"/projects/", "project"},
	{"/projects", "projects"},
	{"/scripts/", "worker script"},
	{"/assets-upload", "asset upload"},
}

// operationName derives a short, human-readable name for a call from its
// method and path, preferring a known resource suffix.
func operationName(method, path string) string {
	verb := strings.ToLower(method)
	resource := "request"
	for _, entry := range opNames {
		if strings.Contains(path, entry.suffix) {
			resource = entry.name
			break
		}
	}
	switch method {
	case http.MethodGet:
		return "get " + resource
	case http.MethodPost:
		return "create " + resource
	case http.MethodPatch, http.MethodPut:
		return "update " + resource
	case http.MethodDelete:
		return "delete " + resource
	default:
		return verb + " " + resource
	}
}

// maxCloudflareErrorBody bounds how much of an error body is parsed, so a
// runaway response cannot be pulled wholesale into memory.
const maxCloudflareErrorBody = 64 << 10

// sanitizeCloudflareMessage removes credential-looking substrings from a
// message returned by the API. The exact token value is removed first, so a
// message that echoes the credential is scrubbed regardless of its format;
// an echoed Authorization header is then removed wholesale; finally the
// value-based scanner (which only trips on known secret shapes) runs as a
// backstop. The result is always bounded in length.
func sanitizeCloudflareMessage(message, token string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	if token = strings.TrimSpace(token); token != "" {
		message = strings.ReplaceAll(message, token, "[REDACTED]")
	}
	message = scrubAuthorizationHeader(message)
	message = redact.String(message)
	if len(message) > 512 {
		message = message[:512] + "…"
	}
	return message
}

// scrubAuthorizationHeader removes the value of an Authorization header if the
// API echoed the request back in its message. A bearer token must never reach
// an error string, even when it is the API that repeated it.
func scrubAuthorizationHeader(message string) string {
	const marker = "authorization"
	lower := strings.ToLower(message)
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return message
	}
	tail := message[idx+len(marker):]
	sep := strings.IndexAny(tail, ":=")
	if sep < 0 {
		return message
	}
	valueStart := idx + len(marker) + sep + 1
	for valueStart < len(message) && (message[valueStart] == ' ' || message[valueStart] == '\t') {
		valueStart++
	}
	valueEnd := valueStart
	for valueEnd < len(message) && message[valueEnd] != '\n' && message[valueEnd] != '\r' && message[valueEnd] != '"' {
		valueEnd++
	}
	if valueEnd <= valueStart {
		return message
	}
	return message[:valueStart] + "[REDACTED]" + message[valueEnd:]
}

// decodeCloudflareResult unwraps the Cloudflare response envelope and decodes
// its Result field into out. Every successful Cloudflare response — single
// objects and lists alike — is wrapped in an envelope, so this is the single
// place that knows the shape.
func decodeCloudflareResult(op string, data []byte, out any) error {
	var env cloudflareEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return errCloudflareMalformed(op, err)
	}
	if len(env.Result) == 0 || string(env.Result) == "null" {
		return fmt.Errorf("cloudflare: %s response carried no result", op)
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return errCloudflareMalformed(op, err)
	}
	return nil
}

// errCloudflareMalformed wraps a body that could not be decoded into the
// expected shape for op.
func errCloudflareMalformed(op string, err error) error {
	return fmt.Errorf("cloudflare: decode %s response: %w", op, err)
}

// isNotFound reports whether err is a Cloudflare "not found" response.
func isNotFound(err error) bool {
	var reqErr *CloudflareRequestError
	if errors.As(err, &reqErr) {
		return reqErr.StatusCode == http.StatusNotFound
	}
	return false
}
