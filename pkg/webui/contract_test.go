//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestHandleAPIBootstrap_ReportsContractVersion pins the bootstrap wire
// field: the daemon's /api/bootstrap response must report ContractVersion
// (the single Go source of truth), which the Web UI uses to negotiate
// against its own build-time pin.
func TestHandleAPIBootstrap_ReportsContractVersion(t *testing.T) {
	server := newSyncTestWebServer(t, t.TempDir())
	request := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)

	rec := httptest.NewRecorder()
	server.handleAPIBootstrap(rec, request)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("bootstrap body not JSON: %v", err)
	}
	if got := payload["contractVersion"]; got != ContractVersion {
		t.Errorf("contractVersion = %v, want %q", got, ContractVersion)
	}
}

// TestContractVersionMatchesHumaDoc pins the Huma-generated OpenAPI document's
// info.version to the constant, so the generated contract (docs/api/openapi.yaml,
// whose info section the Huma doc supplies the version through) cannot drift
// from the constant the bootstrap response reports.
func TestContractVersionMatchesHumaDoc(t *testing.T) {
	doc, err := HumaOpenAPIDoc()
	if err != nil {
		t.Fatalf("HumaOpenAPIDoc: %v", err)
	}
	info, ok := doc["info"].(map[string]any)
	if !ok {
		t.Fatalf("Huma doc has no info section: %v", doc["info"])
	}
	if got := info["version"]; got != ContractVersion {
		t.Errorf("Huma doc info.version = %v, want %q", got, ContractVersion)
	}
}

// TestContractVersionPinnedToSeed parses the hand-written seed (the source
// cmd/genapi copies the info section from) and pins its info.version to the
// constant. The seed is the one place the contract version is written by hand;
// this test is the guard that keeps it in lockstep with the Go side.
func TestContractVersionPinnedToSeed(t *testing.T) {
	root := repoRootFromWorkingDir(t)
	seed, err := os.ReadFile(filepath.Join(root, "docs", "api", "openapi.base.yaml")) // #nosec G304 -- repo-root-joined path
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	var doc struct {
		Info struct {
			Version string `yaml:"version"`
		} `yaml:"info"`
	}
	if err := yaml.Unmarshal(seed, &doc); err != nil {
		t.Fatalf("parse seed: %v", err)
	}
	if doc.Info.Version != ContractVersion {
		t.Errorf("seed info.version = %q, want %q (bump both together)", doc.Info.Version, ContractVersion)
	}
}

// TestContractVersionPinnedToWebUI reads the Web UI's build-time pin and
// verifies it equals the Go constant. This closes the loop on the contract
// version: seed (hand-written) ↔ Huma doc ↔ bootstrap field ↔ Web UI pin are
// all checked against the same value.
func TestContractVersionPinnedToWebUI(t *testing.T) {
	root := repoRootFromWorkingDir(t)
	src, err := os.ReadFile(filepath.Join(root, "webui", "src", "config", "contractCompat.tsx")) // #nosec G304 -- repo-root-joined path
	if err != nil {
		t.Fatalf("read the Web UI contract pin: %v", err)
	}
	re := regexp.MustCompile(`CONTRACT_VERSION\s*=\s*'([0-9.]+)'`)
	matches := re.FindAllSubmatch(src, -1)
	if len(matches) != 1 {
		t.Fatalf("expected exactly one CONTRACT_VERSION = '<semver>' constant in the Web UI pin, found %d", len(matches))
	}
	if got := string(matches[0][1]); got != ContractVersion {
		t.Errorf("Web UI CONTRACT_VERSION = %q, want %q (bump pkg/webui/contract.go and webui/src/config/contractCompat.tsx together)", got, ContractVersion)
	}
}
