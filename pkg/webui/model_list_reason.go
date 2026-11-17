//go:build !js

package webui

import (
	"errors"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// Machine-readable reasons for an empty model list. The WebUI pickers render
// copy for these (webui/src/services/api/types/common.ts documents the same
// vocabulary), so the daemon must emit them rather than a bare empty list —
// a provider whose endpoint cannot be reached and a provider whose key the
// endpoint rejected need opposite fixes, and "No models loaded" says neither.
const (
	modelReasonNoKey       = "no-key"
	modelReasonUnavailable = "unavailable"
)

// modelListFailure classifies a failed model listing into one of the reasons
// above, returning the reason and a detail string safe to show beside it.
//
// It returns an empty reason for a failure it cannot attribute. Those keep
// their HTTP error status and message: the reason field is for a listing the
// user can act on, and guessing wrong sends them to the wrong setting. In
// particular the credential store is never consulted — a provider unknown to
// the config reports "requires an API key" by default, so an absent key
// proves nothing, and an endpoint that answers 404 is not a key problem.
//
// What the endpoint said is therefore the only evidence used. HTTP-status
// failures arrive as a plain error carrying the status text
// (agenterrors.FormatHTTPResponseError), so they match on the message;
// transport failures are typed (CodeNetwork/CodeTimeout) after agenterrors.Wrap.
func modelListFailure(clientType api.ClientType, err error) (reason, detail string) {
	if err == nil {
		return "", ""
	}
	detail = err.Error()

	var typed *agenterrors.TypedError
	if errors.As(err, &typed) {
		switch typed.Code {
		case agenterrors.CodeNetwork, agenterrors.CodeTimeout:
			return modelReasonUnavailable, detail
		}
	}

	msg := strings.ToLower(detail)
	if rejectedUnauthenticated(msg) {
		return modelReasonNoKey, detail
	}
	if unreachable(msg) {
		return modelReasonUnavailable, detail
	}
	return "", detail
}

// rejectedUnauthenticated reports a request the endpoint refused for auth.
// It is the only evidence used for "no-key": the endpoint turned the request
// away, which is the credential's problem and fixable in the panel.
func rejectedUnauthenticated(msg string) bool {
	return strings.Contains(msg, "http 401") ||
		strings.Contains(msg, "http 403") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "invalid api key") ||
		strings.Contains(msg, "invalid_api_key")
}

// unreachable reports a request that never got a usable answer: the address
// did not resolve or connect, the connection dropped, or the call timed out.
// Behind a container's own loopback — where "localhost" is the container, not
// the host running the model server — this is the common case.
func unreachable(msg string) bool {
	for _, needle := range []string{
		"connection refused",
		"no such host",
		"dial tcp",
		"dial udp",
		"network is unreachable",
		"connection reset",
		"i/o timeout",
		"context deadline exceeded",
		"context canceled",
		"unexpected eof",
		"tls:",
		"x509",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}
