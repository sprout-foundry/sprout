//go:build !js

package automate

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestSession(t *testing.T, dir, id string, info *AutomateSessionInfo) {
	t.Helper()
	if err := WriteSessionFile(dir, id, info); err != nil {
		t.Fatalf("WriteSessionFile: %v", err)
	}
}

func TestFinalizeSessionFile_Success(t *testing.T) {
	dir := t.TempDir()
	writeTestSession(t, dir, "s1", &AutomateSessionInfo{Workflow: "wf.json", PID: 42, StartedAt: time.Now(), Kind: "automate"})

	if err := FinalizeSessionFile(dir, "s1", 0); err != nil {
		t.Fatalf("FinalizeSessionFile: %v", err)
	}

	info, err := ReadSessionFile(dir, "s1")
	if err != nil {
		t.Fatalf("ReadSessionFile: %v", err)
	}
	if info.Status != "success" {
		t.Errorf("Status = %q, want success", info.Status)
	}
	if info.EndedAt == nil {
		t.Error("EndedAt not set")
	}
	if info.ExitCode == nil || *info.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", info.ExitCode)
	}
	if info.PID != 0 {
		t.Errorf("PID = %d, want 0 (zeroed to avoid recycled-PID matches)", info.PID)
	}
}

func TestFinalizeSessionFile_ErrorExit(t *testing.T) {
	dir := t.TempDir()
	writeTestSession(t, dir, "s2", &AutomateSessionInfo{Workflow: "wf.json", PID: 42, StartedAt: time.Now(), Kind: "automate"})

	if err := FinalizeSessionFile(dir, "s2", -1); err != nil {
		t.Fatalf("FinalizeSessionFile: %v", err)
	}

	info, _ := ReadSessionFile(dir, "s2")
	if info.Status != "error" {
		t.Errorf("Status = %q, want error", info.Status)
	}
	if info.ExitCode == nil || *info.ExitCode != -1 {
		t.Errorf("ExitCode = %v, want -1 (signal kill)", info.ExitCode)
	}
}

func TestFinalizeSessionFile_MissingSession(t *testing.T) {
	dir := t.TempDir()
	if err := FinalizeSessionFile(dir, "nope", 0); err == nil {
		t.Error("expected error for missing session file")
	}
}

func TestFinalizeSessionFileByPath_Success(t *testing.T) {
	dir := t.TempDir()
	writeTestSession(t, dir, "p1", &AutomateSessionInfo{Workflow: "wf.json", PID: 4242, StartedAt: time.Now(), Kind: "automate"})

	if err := FinalizeSessionFileByPath(filepath.Join(dir, "automate", "p1.json"), 0); err != nil {
		t.Fatalf("FinalizeSessionFileByPath: %v", err)
	}

	info, err := ReadSessionFile(dir, "p1")
	if err != nil {
		t.Fatalf("ReadSessionFile: %v", err)
	}
	if info.Status != "success" {
		t.Errorf("Status = %q, want success", info.Status)
	}
	if info.EndedAt == nil {
		t.Error("EndedAt not set")
	}
	if info.ExitCode == nil || *info.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", info.ExitCode)
	}
	if info.PID != 0 {
		t.Errorf("PID = %d, want 0 (zeroed to avoid recycled-PID matches)", info.PID)
	}
}

func TestFinalizeSessionFileByPath_ErrorExit(t *testing.T) {
	dir := t.TempDir()
	writeTestSession(t, dir, "p2", &AutomateSessionInfo{Workflow: "wf.json", PID: 4242, StartedAt: time.Now(), Kind: "automate"})

	if err := FinalizeSessionFileByPath(filepath.Join(dir, "automate", "p2.json"), 1); err != nil {
		t.Fatalf("FinalizeSessionFileByPath: %v", err)
	}

	info, _ := ReadSessionFile(dir, "p2")
	if info.Status != "error" {
		t.Errorf("Status = %q, want error", info.Status)
	}
	if info.ExitCode == nil || *info.ExitCode != 1 {
		t.Errorf("ExitCode = %v, want 1", info.ExitCode)
	}
	if info.PID != 0 {
		t.Errorf("PID = %d, want 0", info.PID)
	}
}

func TestFinalizeSessionFileByPath_MissingFile(t *testing.T) {
	dir := t.TempDir()
	if err := FinalizeSessionFileByPath(filepath.Join(dir, "automate", "absent.json"), 0); err == nil {
		t.Error("expected error for missing session file")
	}
}

// TestFinalizeSessionFileByPath_MatchesDirIDForm pins that both entry points
// share one record-mutation path: FinalizeSessionFile delegates to the
// path-based form, so a detached child and an attached launcher produce
// byte-equivalent end states for the same exit code.
func TestFinalizeSessionFileByPath_MatchesDirIDForm(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"via-dir", "via-path"} {
		writeTestSession(t, dir, id, &AutomateSessionInfo{Workflow: "wf.json", PID: 7, StartedAt: time.Now().Add(-time.Minute), Kind: "automate", OutputFilePath: "/tmp/x.log"})
	}

	if err := FinalizeSessionFile(dir, "via-dir", 1); err != nil {
		t.Fatalf("FinalizeSessionFile: %v", err)
	}
	if err := FinalizeSessionFileByPath(filepath.Join(dir, "automate", "via-path.json"), 1); err != nil {
		t.Fatalf("FinalizeSessionFileByPath: %v", err)
	}

	viaDir, err := ReadSessionFile(dir, "via-dir")
	if err != nil {
		t.Fatalf("read via-dir: %v", err)
	}
	viaPath, err := ReadSessionFile(dir, "via-path")
	if err != nil {
		t.Fatalf("read via-path: %v", err)
	}
	if viaDir.Status != viaPath.Status || viaDir.Status != "error" {
		t.Errorf("Status mismatch: dir=%q path=%q", viaDir.Status, viaPath.Status)
	}
	if viaDir.ExitCode == nil || viaPath.ExitCode == nil || *viaDir.ExitCode != *viaPath.ExitCode {
		t.Errorf("ExitCode mismatch: dir=%v path=%v", viaDir.ExitCode, viaPath.ExitCode)
	}
	if viaDir.PID != viaPath.PID || viaDir.PID != 0 {
		t.Errorf("PID mismatch: dir=%d path=%d", viaDir.PID, viaPath.PID)
	}
	if viaDir.EndedAt == nil || viaPath.EndedAt == nil {
		t.Error("both forms must set EndedAt")
	}
	if viaDir.Workflow != viaPath.Workflow || viaDir.OutputFilePath != viaPath.OutputFilePath {
		t.Error("finalization must not disturb the launch-time fields")
	}
}

func TestWriteSessionFile_RunningStatusDefault(t *testing.T) {
	dir := t.TempDir()
	writeTestSession(t, dir, "s3", &AutomateSessionInfo{Workflow: "wf.json", PID: 1, StartedAt: time.Now(), Kind: "automate"})

	info, _ := ReadSessionFile(dir, "s3")
	if info.Status != "running" {
		t.Errorf("Status = %q, want running default", info.Status)
	}
}

func TestSweepStaleSessions_KeepsRecentAndFinalized(t *testing.T) {
	dir := t.TempDir()

	// Finalized 1h ago — kept (inside 7d retention).
	writeTestSession(t, dir, "recent-dead", &AutomateSessionInfo{Workflow: "a.json", PID: 0, StartedAt: time.Now().Add(-2 * time.Hour), Kind: "automate"})
	if err := FinalizeSessionFile(dir, "recent-dead", 3); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	// Finalized 8d ago — swept.
	old := time.Now().Add(-8 * 24 * time.Hour)
	writeTestSession(t, dir, "old-dead", &AutomateSessionInfo{Workflow: "a.json", PID: 0, StartedAt: old, Kind: "automate", EndedAt: &old, Status: "error"})

	// Legacy (no end) started 1h ago, PID dead — kept.
	writeTestSession(t, dir, "legacy-recent", &AutomateSessionInfo{Workflow: "a.json", PID: 999999, StartedAt: time.Now().Add(-1 * time.Hour), Kind: "automate"})

	// Legacy started 8d ago, PID dead — swept.
	writeTestSession(t, dir, "legacy-old", &AutomateSessionInfo{Workflow: "a.json", PID: 999999, StartedAt: old, Kind: "automate", Status: "exited"})

	removed, err := SweepStaleSessions(dir)
	if err != nil {
		t.Fatalf("SweepStaleSessions: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	for _, id := range []string{"recent-dead", "legacy-recent"} {
		if _, err := ReadSessionFile(dir, id); err != nil {
			t.Errorf("session %s should have been kept: %v", id, err)
		}
	}
	for _, id := range []string{"old-dead", "legacy-old"} {
		if _, err := ReadSessionFile(dir, id); err == nil {
			t.Errorf("session %s should have been swept", id)
		}
	}
}

func TestCheckMemoryFloor_DisabledViaEnv(t *testing.T) {
	t.Setenv("SPROUT_AUTOMATE_MIN_MEM_MB", "0")
	if err := CheckMemoryFloor(); err != nil {
		t.Fatalf("disabled floor must never block: %v", err)
	}
}

func TestCheckMemoryFloor_AbsurdFloorBlocks(t *testing.T) {
	t.Setenv("SPROUT_AUTOMATE_MIN_MEM_MB", "99999999")
	err := CheckMemoryFloor()
	if err == nil {
		t.Skip("platform has no memory reader — block path untestable here")
	}
	if got := err.Error(); !strings.Contains(got, "SPROUT_AUTOMATE_MIN_MEM_MB") || !strings.Contains(got, "below") {
		t.Errorf("error should name the override env and the floor, got: %s", got)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{512, "512 bytes"},
		{5 << 20, "5 MB"},
		{1536 << 20, "1.5 GB"},
	}
	for _, c := range cases {
		if got := formatBytes(c.in); got != c.want {
			t.Errorf("formatBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
