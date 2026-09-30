//go:build windows

package changes

import "testing"

func TestResolveAbsPath_RootedPathTakesWorkspaceDrive(t *testing.T) {
	ct := &ChangeTracker{view: stubAgentView{workspaceRoot: `D:\work\repo`}}
	if got := ct.resolveAbsPath("/etc/shadow"); got != `D:\etc\shadow` {
		t.Fatalf("resolveAbsPath(/etc/shadow) = %q, want %q", got, `D:\etc\shadow`)
	}
	if !ct.IsOutsideWorkspace(`D:\etc\shadow`) {
		t.Fatal(`D:\etc\shadow must be outside D:\work\repo`)
	}
}
