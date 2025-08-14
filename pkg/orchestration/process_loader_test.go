package orchestration

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return p
}

func TestProcessLoader_ValidSimple(t *testing.T) {
	dir := t.TempDir()
	content := `{
        "version": "1.0",
        "goal": "Test",
        "agents": [
            {"id":"a1","name":"A1","persona":"dev","description":"desc"}
        ],
        "steps": [
            {"id":"s1","name":"S1","description":"d","agent_id":"a1"}
        ]
    }`
	path := writeTempFile(t, dir, "p.json", content)
	l := NewProcessLoader()
	pf, err := l.LoadProcessFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pf.Goal != "Test" {
		t.Fatalf("unexpected goal: %s", pf.Goal)
	}
}

func TestProcessLoader_MissingAgentRef(t *testing.T) {
	dir := t.TempDir()
	content := `{
        "version": "1.0",
        "goal": "Test",
        "agents": [
            {"id":"a1","name":"A1","persona":"dev","description":"desc"}
        ],
        "steps": [
            {"id":"s1","name":"S1","description":"d","agent_id":"a2"}
        ]
    }`
	path := writeTempFile(t, dir, "p.json", content)
	l := NewProcessLoader()
	if _, err := l.LoadProcessFile(path); err == nil {
		t.Fatalf("expected error for missing agent reference")
	}
}

func TestProcessLoader_MissingStepDependency(t *testing.T) {
	dir := t.TempDir()
	content := `{
        "version": "1.0",
        "goal": "Test",
        "agents": [
            {"id":"a1","name":"A1","persona":"dev","description":"desc"}
        ],
        "steps": [
            {"id":"s1","name":"S1","description":"d","agent_id":"a1","depends_on":["sX"]}
        ]
    }`
	path := writeTempFile(t, dir, "p.json", content)
	l := NewProcessLoader()
	if _, err := l.LoadProcessFile(path); err == nil {
		t.Fatalf("expected error for missing depends_on step id")
	}
}

func TestProcessLoader_CircularDependency(t *testing.T) {
	dir := t.TempDir()
	content := `{
        "version": "1.0",
        "goal": "Test",
        "agents": [
            {"id":"a1","name":"A1","persona":"dev","description":"desc"}
        ],
        "steps": [
            {"id":"s1","name":"S1","description":"d","agent_id":"a1","depends_on":["s2"]},
            {"id":"s2","name":"S2","description":"d","agent_id":"a1","depends_on":["s1"]}
        ]
    }`
	path := writeTempFile(t, dir, "p.json", content)
	l := NewProcessLoader()
	if _, err := l.LoadProcessFile(path); err == nil {
		t.Fatalf("expected error for circular dependency")
	}
}

// Ensure defaults are set
func TestProcessLoader_DefaultsApplied(t *testing.T) {
	dir := t.TempDir()
	content := `{
        "goal": "Defaults",
        "agents": [
            {"id":"a1","name":"A1","persona":"dev","description":"desc"}
        ],
        "steps": [
            {"id":"s1","name":"S1","description":"d","agent_id":"a1"}
        ]
    }`
	path := writeTempFile(t, dir, "p.json", content)
	l := NewProcessLoader()
	pf, err := l.LoadProcessFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pf.Version == "" {
		t.Fatalf("expected version to be set by defaults")
	}
	if pf.Settings == nil {
		t.Fatalf("expected settings defaults")
	}
}
