//go:build darwin

package sandbox

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

type fixture struct {
	work, temp, outside, secretDir, allowedFile string
	policy                                      Policy
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	if c := Detect(); !c.Available {
		t.Skipf("seatbelt unavailable: %s", c.Detail)
	}
	f := fixture{work: realDir(t), temp: realDir(t), outside: realDir(t)}
	home := realDir(t)
	f.secretDir = filepath.Join(home, ".ssh")
	if err := os.Mkdir(f.secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.secretDir, "id_test"), []byte("synthetic-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.allowedFile = filepath.Join(home, "notes.txt")
	if err := os.WriteFile(f.allowedFile, []byte("public"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.policy = Policy{WorkDir: f.work, TempDir: f.temp, DenyRead: DefaultDenyRead(home)}
	return f
}

func runSandboxed(t *testing.T, p Policy, dir string, name string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd, err := CommandContext(ctx, p, name, args...)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TMPDIR="+p.TempDir, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	return out.String(), err
}

func TestDetectDarwin(t *testing.T) {
	c := Detect()
	if c.Name != "seatbelt" {
		t.Fatalf("Name = %q, want seatbelt", c.Name)
	}
	if !c.Available && c.Detail == "" {
		t.Fatal("unavailable capability must explain why")
	}
}

func TestSandboxWritesConfined(t *testing.T) {
	f := newFixture(t)
	if out, err := runSandboxed(t, f.policy, f.work, "/bin/sh", "-c", "echo ok > inside.txt && echo ok > \"$TMPDIR/t.txt\""); err != nil {
		t.Fatalf("write inside WorkDir/TempDir failed: %v\n%s", err, out)
	}
	target := filepath.Join(f.outside, "escape.txt")
	if out, err := runSandboxed(t, f.policy, f.work, "/bin/sh", "-c", "echo bad > \"$1\"", "sh", target); err == nil {
		t.Fatalf("write outside the policy succeeded\n%s", out)
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatal("file outside the policy was created")
	}
}

func TestSandboxDenyRead(t *testing.T) {
	f := newFixture(t)
	if out, err := runSandboxed(t, f.policy, f.work, "/bin/cat", filepath.Join(f.secretDir, "id_test")); err == nil {
		t.Fatalf("read of a denied path succeeded: %q", out)
	}
	if out, err := runSandboxed(t, f.policy, f.work, "/bin/ls", f.secretDir); err == nil {
		t.Fatalf("listing a denied dir succeeded: %q", out)
	}
	out, err := runSandboxed(t, f.policy, f.work, "/bin/cat", f.allowedFile)
	if err != nil || out != "public" {
		t.Fatalf("read of an allowed file: %v %q", err, out)
	}
}

func TestSandboxBlocksOutboundNetwork(t *testing.T) {
	if os.Getenv("SKIP_NETWORK_TESTS") != "" {
		t.Skip("SKIP_NETWORK_TESTS set")
	}
	f := newFixture(t)
	probe := []string{"-z", "-w", "2", "1.1.1.1", "443"}
	if err := exec.Command("/usr/bin/nc", probe...).Run(); err != nil {
		t.Skipf("no outbound network without the sandbox either: %v", err)
	}
	if out, err := runSandboxed(t, f.policy, f.work, "/usr/bin/nc", probe...); err == nil {
		t.Fatalf("outbound connect succeeded with AllowNetwork=false\n%s", out)
	}
	open := f.policy
	open.AllowNetwork = true
	if out, err := runSandboxed(t, open, f.work, "/usr/bin/nc", probe...); err != nil {
		t.Fatalf("outbound connect failed with AllowNetwork=true: %v\n%s", err, out)
	}
}

func TestSandboxAllowsLocalhostWithoutNetwork(t *testing.T) {
	f := newFixture(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	if out, err := runSandboxed(t, f.policy, f.work, "/usr/bin/nc", "-z", "-w", "2", "127.0.0.1", port); err != nil {
		t.Fatalf("localhost connect failed: %v\n%s", err, out)
	}
}

func TestSandboxEscapedWorkDirRuns(t *testing.T) {
	f := newFixture(t)
	work := filepath.Join(f.work, `we"ird\dir`)
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	p := f.policy
	p.WorkDir = work
	if out, err := runSandboxed(t, p, work, "/bin/sh", "-c", "echo ok > inside.txt"); err != nil {
		t.Fatalf("write inside an escaped WorkDir failed: %v\n%s", err, out)
	}
}

func TestSandboxGitWorks(t *testing.T) {
	f := newFixture(t)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	script := `set -e
"$1" init -q .
echo hello > README
"$1" add README
"$1" -c user.name=Sandbox -c user.email=sandbox@example.invalid -c commit.gpgsign=false commit -q -m init
"$1" log --oneline`
	if out, err := runSandboxed(t, f.policy, f.work, "/bin/sh", "-c", script, "sh", git); err != nil {
		t.Fatalf("git in sandbox: %v\n%s", err, out)
	}
}

func TestSandboxCompilerWorks(t *testing.T) {
	f := newFixture(t)
	if _, err := exec.Command("/usr/bin/xcrun", "--find", "clang").Output(); err != nil {
		t.Skip("no Xcode command line tools")
	}
	if out, err := runSandboxed(t, f.policy, f.work, "/usr/bin/xcrun", "--find", "clang"); err != nil {
		t.Fatalf("xcrun --find clang: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(f.work, "m.c"), []byte("int main(void){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runSandboxed(t, f.policy, f.work, "/usr/bin/cc", "m.c", "-o", "m"); err != nil {
		t.Fatalf("cc in sandbox: %v\n%s", err, out)
	}
	if out, err := runSandboxed(t, f.policy, f.work, "./m"); err != nil {
		t.Fatalf("running the built binary: %v\n%s", err, out)
	}
}

func TestCommandContextRejectsFlagAsName(t *testing.T) {
	newFixture(t)
	if _, err := CommandContext(context.Background(), Policy{WorkDir: t.TempDir()}, "-p"); err == nil {
		t.Fatal("expected an error for a command name starting with '-'")
	}
}
