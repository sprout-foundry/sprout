package platformauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/runner"
)

// fakePlatform answers the CLI endpoints for one token.
func fakePlatform(t *testing.T, token string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authed := r.Header.Get("Authorization") == "Bearer "+token
		switch r.URL.Path {
		case "/cli/device":
			_, _ = w.Write([]byte(`{"device_code":"d","user_code":"CDFG-HJKM","verification_uri":"https://x/cli/link","interval":5,"expires_in":600}`))
		case "/runners/device/poll":
			_, _ = w.Write([]byte(`{"access_token":"` + token + `","account_email":"me@example.com"}`))
		case "/cli/whoami":
			if !authed {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"user_id":"u1","email":"me@example.com","plan":"pro"}`))
		case "/cli/runners", "/cli/gpus":
			if !authed {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var req LinkRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Name == "taken" {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":"You already have a runner named \"taken\".","code":"runner_name_taken"}`))
				return
			}
			_, _ = w.Write([]byte(`{"runner_id":"r1","link_id":"l1","api_key":"key","account_email":"me@example.com"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLoginFlowAndLinking(t *testing.T) {
	srv := fakePlatform(t, "spt_tok")
	ctx := context.Background()
	anon := NewClient(srv.URL, "")
	start, err := anon.StartLogin(ctx, "mac", "darwin", "arm64")
	if err != nil || start.UserCode != "CDFG-HJKM" {
		t.Fatalf("start = %+v, %v", start, err)
	}
	granted, err := anon.PollLogin(ctx, start.DeviceCode)
	if err != nil || granted.AccessToken != "spt_tok" || granted.AccountEmail != "me@example.com" {
		t.Fatalf("granted = %+v, %v", granted, err)
	}

	signed := NewClient(srv.URL, granted.AccessToken)
	account, err := signed.WhoAmI(ctx)
	if err != nil || account.Email != "me@example.com" || account.Plan != "pro" {
		t.Fatalf("whoami = %+v, %v", account, err)
	}
	if _, err := anon.WhoAmI(ctx); !errors.Is(err, ErrNotSignedIn) {
		t.Fatalf("whoami without a token = %v, want ErrNotSignedIn", err)
	}
	linked, err := signed.LinkRunner(ctx, LinkRequest{Name: "mac"})
	if err != nil || linked.RunnerID != "r1" || linked.APIKey != "key" || linked.AccountEmail != "me@example.com" {
		t.Fatalf("link runner = %+v, %v", linked, err)
	}
	if _, err := signed.LinkGPU(ctx, LinkRequest{Name: "taken"}); err == nil || err.Error() != `You already have a runner named "taken".` {
		t.Fatalf("a refusal carries the platform's message; got %v", err)
	}
}

func TestPollLoginOutcomes(t *testing.T) {
	for status, want := range map[int]error{
		http.StatusPreconditionRequired: runner.ErrAuthorizationPending,
		http.StatusTooManyRequests:      runner.ErrSlowDown,
		http.StatusForbidden:            runner.ErrAccessDenied,
		http.StatusGone:                 runner.ErrExpiredToken,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		_, err := NewClient(srv.URL, "").PollLogin(context.Background(), "d")
		srv.Close()
		if !errors.Is(err, want) {
			t.Errorf("%d: %v, want %v", status, err, want)
		}
	}
}

func TestSaveLoadRemove(t *testing.T) {
	t.Setenv("SPROUT_CONFIG_DIR", t.TempDir())
	t.Setenv("SPROUT_CREDENTIAL_BACKEND", "file")
	if _, _, ok := Load(); ok {
		t.Fatal("signed in before login")
	}
	if _, err := Save(&Login{PlatformURL: "https://app.example.com/", Email: "me@example.com"}, "spt_tok"); err != nil {
		t.Fatal(err)
	}
	l, token, ok := SignedInTo("https://app.example.com")
	if !ok || token != "spt_tok" || l.Email != "me@example.com" {
		t.Fatalf("loaded = %+v %q %v", l, token, ok)
	}
	if _, _, ok := SignedInTo("https://other.example.com"); ok {
		t.Fatal("a sign-in to one platform must not apply to another")
	}
	if err := Remove(); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := Load(); ok {
		t.Fatal("still signed in after Remove")
	}
}
