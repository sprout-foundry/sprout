//go:build !js

package webui

import (
	"errors"
	"fmt"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

func TestModelListFailure_TransportFailuresAreUnavailable(t *testing.T) {
	// The classification must not depend on the credential store: a provider
	// that needs a key and has none also fails on an unreachable endpoint,
	// and the endpoint is the thing to fix first.
	cases := []struct {
		name string
		err  error
	}{
		{"typed network", agenterrors.NewNetwork("dial failed", errors.New("connection refused"))},
		{"typed timeout", agenterrors.NewTimeout("list models", 0)},
		{"wrapped network", agenterrors.Wrap(agenterrors.NewNetwork("dial failed", nil), "extra context")},
		{"refused", errors.New("failed to fetch models from acme: dial tcp 127.0.0.1:8000: connect: connection refused")},
		{"dns", errors.New("failed to fetch models from acme: dial tcp: lookup acme.example: no such host")},
		{"timeout text", errors.New("failed to fetch models from acme: context deadline exceeded")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, detail := modelListFailure("acme-gw", tc.err)
			if reason != modelReasonUnavailable {
				t.Errorf("reason = %q, want %q (err: %v)", reason, modelReasonUnavailable, tc.err)
			}
			if detail != tc.err.Error() {
				t.Errorf("detail = %q, want the error text %q", detail, tc.err.Error())
			}
		})
	}
}

func TestModelListFailure_AuthFailuresAreNoKey(t *testing.T) {
	for _, msg := range []string{
		"failed to fetch models from acme: HTTP 401: Unauthorized",
		"failed to fetch models from acme: HTTP 403: Forbidden",
		"failed to fetch models from acme: HTTP 401: invalid_api_key",
	} {
		t.Run(msg, func(t *testing.T) {
			reason, detail := modelListFailure("acme-gw", errors.New(msg))
			if reason != modelReasonNoKey {
				t.Errorf("reason = %q, want %q", reason, modelReasonNoKey)
			}
			if detail == "" {
				t.Error("detail must carry the endpoint's own message")
			}
		})
	}
}

func TestModelListFailure_UnclassifiedKeepsHTTPError(t *testing.T) {
	// Anything else stays an HTTP 500 with its message: inventing a reason
	// the user can act on would send them to the wrong setting.
	cases := []error{
		errors.New("failed to fetch models from acme: HTTP 404: not found"),
		errors.New("failed to fetch models from acme: HTTP 500: upstream error"),
		fmt.Errorf("provider acme-gw returned no models from its models endpoint"),
	}
	for _, err := range cases {
		t.Run(err.Error(), func(t *testing.T) {
			reason, _ := modelListFailure("acme-gw", err)
			if reason != "" {
				t.Errorf("reason = %q, want empty (leave it a server error)", reason)
			}
		})
	}
}

func TestModelListFailure_NilError(t *testing.T) {
	reason, detail := modelListFailure("acme-gw", nil)
	if reason != "" || detail != "" {
		t.Errorf("nil error gave reason %q detail %q, want both empty", reason, detail)
	}
}

func TestModelListFailure_AmbiguousFailureIsNotBlamedOnCredentials(t *testing.T) {
	// A provider unknown to the config reports "requires an API key" by
	// default, so an absent key proves nothing. Guessing "no-key" here would
	// send the user to the credential field for a provider whose listing
	// failed for some other reason.
	err := errors.New("acme-gw listing went wrong in a way nothing classifies")

	for _, provider := range []api.ClientType{api.OllamaLocalClientType, "acme-gw"} {
		if reason, _ := modelListFailure(provider, err); reason != "" {
			t.Errorf("%s: reason = %q, want empty", provider, reason)
		}
	}
}
