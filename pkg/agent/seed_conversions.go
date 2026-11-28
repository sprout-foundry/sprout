// Package agent provides the seed integration layer.
//
// seed_conversions.go — identity conversion helpers between seed/core types
// and sprout api types (now aliases), plus error wrapping utilities.

package agent

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// ---------------------------------------------------------------------------
// Conversion helpers — identity functions (types are now aliases)
// ---------------------------------------------------------------------------

// seedRequestToSprout returns the request unchanged since
// api.ChatRequest and core.ChatRequest are the same type.
func seedRequestToSprout(req *core.ChatRequest) *core.ChatRequest {
	return req
}

// sproutResponseToSeed returns the response unchanged since
// api.ChatResponse and core.ChatResponse are the same type.
func sproutResponseToSeed(resp *api.ChatResponse) *core.ChatResponse {
	return resp
}

// ---------------------------------------------------------------------------
// Error wrapping (mimics old ErrorHandler.HandleAPIFailure)
// ---------------------------------------------------------------------------

var (
	// The "chat failed: " prefix some seed runtimes prepend.
	chatFailedPrefixRe = regexp.MustCompile(`^chat failed: `)
	// The "HTTP <code>: " prefix that formatProviderHTTPError prepends to a
	// response body — bookkeeping noise for the user, not provider text.
	httpStatusPrefixRe = regexp.MustCompile(`(?i)\bHTTP \d{3}: `)
	// Wrapper prefixes a typed error chain adds around its cause: seed core's
	// typed prefixes ("transient error during chat (provider):", "client
	// error (provider):", ...), the legacy AgentError bracket tag, the
	// TypedError bracket tag, and the generic provider's network-wrapper
	// phrases. All are stripped so the provider's own text survives.
	chainWrapperRe = regexp.MustCompile(`(?i)(?:chat failed|transient error(?: during [a-z_]+)?|authentication failed|rate limit exceeded|client error|context window exceeded|tool call threading error|chat request failed(?: \(streaming\))?|HTTP request failed)(?: \([^)]*\))?(?:: )|\[(?:network|validation|not_found|permission|timeout|config|agent|tool|approval|unknown|transient|ratelimited|provider|security|invalidinput|context|permanent|chat)\] |\[(?:Transient|RateLimited|Provider|Security|InvalidInput|Context|Permanent)\] `)
	// Status-code of an HTTP error inside an error message.
	statusFromMsgRe = regexp.MustCompile(`(?i)HTTP (\d{3})`)
)

// typedStatusCodes are the TypedError codes whose Status is a meaningful
// HTTP status. Config/agent/tool/approval/unknown codes all fall back to 500
// in StatusFor — a bogus HTTP code for a local failure — so the typed Status
// is only trusted for the codes that carry a real status.
var typedStatusCodes = map[agenterrors.ErrorCode]bool{
	agenterrors.CodeNetwork:    true,
	agenterrors.CodeTimeout:    true,
	agenterrors.CodeValidation: true,
	agenterrors.CodeNotFound:   true,
	agenterrors.CodePermission: true,
}

// providerHTTPStatus reports the HTTP status code relevant to err's text —
// preferring the TypedError's own Status field when it carries a real HTTP
// status — or 0 when none is present.
func providerHTTPStatus(err error, msg string) int {
	var typed *agenterrors.TypedError
	if errors.As(err, &typed) && typed.Status > 0 && typedStatusCodes[typed.Code] {
		return typed.Status
	}
	if m := statusFromMsgRe.FindStringSubmatch(msg); len(m) > 1 {
		if code, err := strconv.Atoi(m[1]); err == nil {
			return code
		}
	}
	return 0
}

// stripChainPrefixes removes the tool-chain wrappers — seed core's typed
// prefixes, the bracket tags, the generic provider's network-wrapper phrases
// and the redundant "HTTP <code>" prefix — leaving the provider's own text.
func stripChainPrefixes(msg string) string {
	prev := ""
	cur := strings.TrimSpace(chatFailedPrefixRe.ReplaceAllString(msg, ""))
	for prev != cur {
		prev = cur
		cur = strings.TrimSpace(httpStatusPrefixRe.ReplaceAllString(cur, ""))
		cur = strings.TrimSpace(chainWrapperRe.ReplaceAllString(cur, ""))
	}
	return cur
}

// textContainsAny reports whether the lower-cased message contains any of the
// lower-cased substrings.
func textContainsAny(lowerMsg string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(lowerMsg, sub) {
			return true
		}
	}
	return false
}

// wrapError converts a non-retryable error into a user-friendly string.
// This is called when seed.Run() returns an error that should be shown
// to the user rather than propagated as a Go error.
func wrapError(err error) string {
	if err == nil {
		return ""
	}
	raw := strings.TrimSpace(err.Error())

	status := providerHTTPStatus(err, raw)
	text := stripChainPrefixes(raw)
	if strings.TrimSpace(text) == "" {
		text = raw
	}
	lower := strings.ToLower(text)

	hasTimeout := textContainsAny(lower, "timeout", "timed out", "deadline exceeded", "etimedout")
	hasConnectionReset := textContainsAny(lower, "connection reset", "connection refused", "connection aborted", "connection lost", "econnreset", "econnrefused")

	// 1) Server errors — transient typed errors, 5xx statuses, connection
	// resets and timeouts. The message leads with the provider's own text.
	if status == 500 || status == 502 || status == 503 || status == 504 ||
		agenterrors.IsTransient(err) || hasConnectionReset || hasTimeout {
		if status > 0 {
			return fmt.Sprintf("Provider server error (HTTP %d): %s", status, text)
		}
		return fmt.Sprintf("Provider server error: %s", text)
	}

	// 2) Rate limits — typed rate-limited errors or 429 / rate-limit wording.
	if agenterrors.IsRateLimited(err) || status == 429 ||
		textContainsAny(lower, "rate limit", "rate_limit", "too many requests", "quota exceeded") {
		return fmt.Sprintf("Rate limit exceeded: %s. Please wait before making more requests.", text)
	}

	// 3) Authentication — only an HTTP 401/403 or a body naming an invalid or
	// missing key. Everything else is not an auth failure.
	if status == 401 || status == 403 ||
		textContainsAny(lower, "authentication", "unauthorized", "invalid api key", "invalid_api_key",
			"api key is not valid", "api key invalid", "api key rejected",
			"missing api key", "no api key", "api key not provided",
			"invalid credentials", "missing credentials") {
		return fmt.Sprintf("Authentication failed: %s. Please check your API key and configuration.", text)
	}

	// 4) Rejected requests — 400/404/422 are the provider's own refusal, not
	// an auth problem, and not retryable.
	if status == 400 || status == 404 || status == 422 {
		return fmt.Sprintf("The provider rejected the request: %s", text)
	}

	// 5) Anything else — report the error as-is.
	return fmt.Sprintf("Provider error: %s", text)
}
