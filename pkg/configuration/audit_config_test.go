package configuration

import (
	"encoding/json"
	"testing"
)

// TestAuditConfigRoundTrip pins the audit section's JSON shape, which the
// host contract documents (docs/integration/host-contract.md).
func TestAuditConfigRoundTrip(t *testing.T) {
	raw := `{
		"audit": {
			"endpoint": "https://host.example.com/api/agent-audit",
			"batch_size": 16,
			"flush_interval_seconds": 2
		}
	}`

	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if cfg.Audit == nil {
		t.Fatal("expected audit section to be parsed")
	}
	if cfg.Audit.Endpoint != "https://host.example.com/api/agent-audit" {
		t.Errorf("endpoint = %q", cfg.Audit.Endpoint)
	}
	if cfg.Audit.BatchSize != 16 {
		t.Errorf("batch_size = %d, want 16", cfg.Audit.BatchSize)
	}
	if cfg.Audit.FlushIntervalSeconds != 2 {
		t.Errorf("flush_interval_seconds = %d, want 2", cfg.Audit.FlushIntervalSeconds)
	}
}

// TestAuditConfigAbsentIsNil pins that a config without an audit section
// leaves the field nil (local-only auditing).
func TestAuditConfigAbsentIsNil(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if cfg.Audit != nil {
		t.Errorf("expected nil audit section, got %+v", cfg.Audit)
	}
}
