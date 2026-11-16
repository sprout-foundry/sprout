package gpulink

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/runner"
)

func TestStartLink_SendsTheEngine(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gpus/device" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"device_code":"d","user_code":"CDFG-HJKM","verification_uri":"https://x/gpus/link","interval":5,"expires_in":600}`))
	}))
	defer srv.Close()
	start, err := NewClient(srv.URL).StartLink(context.Background(), "office-4090", "linux", "amd64", "127.0.0.1:11434")
	if err != nil {
		t.Fatal(err)
	}
	if start.UserCode != "CDFG-HJKM" || got["engine"] != "127.0.0.1:11434" || got["name"] != "office-4090" {
		t.Fatalf("start = %+v, sent %v", start, got)
	}
}

func TestStartFleetLink_UsesTheFleetEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"device_code":"d","user_code":"CDFG-HJKM","verification_uri":"https://x/fleet/link","interval":5,"expires_in":600}`))
	}))
	defer srv.Close()
	start, err := NewClient(srv.URL).StartFleetLink(context.Background(), "rig-1", "linux", "amd64", "127.0.0.1:8006")
	if err != nil || start.UserCode != "CDFG-HJKM" {
		t.Fatalf("start = %+v, %v", start, err)
	}
	if gotPath != "/fleet/gpus/device" {
		t.Fatalf("path = %q, want /fleet/gpus/device", gotPath)
	}
}

func TestPollLink_Outcomes(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   error
	}{
		{http.StatusPreconditionRequired, `{"error":"authorization_pending"}`, runner.ErrAuthorizationPending},
		{http.StatusTooManyRequests, `{"error":"slow_down"}`, runner.ErrSlowDown},
		{http.StatusForbidden, `{"error":"access_denied"}`, runner.ErrAccessDenied},
		{http.StatusGone, `{"error":"expired_token"}`, runner.ErrExpiredToken},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		_, err := NewClient(srv.URL).PollLink(context.Background(), "d")
		srv.Close()
		if !errors.Is(err, tc.want) {
			t.Errorf("%d: err = %v, want %v", tc.status, err, tc.want)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"link_id":"l1","api_key":"sfg_x"}`))
	}))
	defer srv.Close()
	creds, err := NewClient(srv.URL).PollLink(context.Background(), "d")
	if err != nil || creds.LinkID != "l1" || creds.APIKey != "sfg_x" {
		t.Fatalf("creds = %+v, %v", creds, err)
	}
}

func TestEngineModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"a"},{"id":"b"}]}`))
	}))
	defer srv.Close()
	addr := srv.Listener.Addr().String()
	models, err := EngineModels(context.Background(), addr, "k")
	if err != nil || len(models) != 2 || models[0] != "a" {
		t.Fatalf("models = %v, %v", models, err)
	}
	if _, err := EngineModels(context.Background(), addr, ""); err == nil {
		t.Fatal("an engine answering 401 must be an error")
	}
}
