package wasmshell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── Chain operators (&&, ||, ;) ─────────────────────────────────────────

func TestSplitChains_BasicOperators(t *testing.T) {
	segs := SplitChains("echo a && echo b || echo c ; echo d")
	if len(segs) != 4 {
		t.Fatalf("expected 4 segments, got %d: %+v", len(segs), segs)
	}
	wantOps := []string{"", "&&", "||", ";"}
	wantTexts := []string{"echo a", "echo b", "echo c", "echo d"}
	for i, seg := range segs {
		if seg.op != wantOps[i] {
			t.Errorf("segment %d op = %q, want %q", i, seg.op, wantOps[i])
		}
		if seg.text != wantTexts[i] {
			t.Errorf("segment %d text = %q, want %q", i, seg.text, wantTexts[i])
		}
	}
}

func TestSplitChains_QuotesSuppressOperators(t *testing.T) {
	segs := SplitChains(`echo "a && b"`)
	if len(segs) != 1 {
		t.Fatalf("quoted && must not split; got %d segments", len(segs))
	}
	if segs[0].text != `echo "a && b"` {
		t.Errorf("text = %q", segs[0].text)
	}
}

func TestParseAndExecute_ChainAnd(t *testing.T) {
	r := ParseAndExecute("echo one && echo two")
	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, stderr = %q", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "one") || !strings.Contains(r.Stdout, "two") {
		t.Errorf("stdout = %q, want both segments' output", r.Stdout)
	}
}

func TestParseAndExecute_ChainAndSkipsOnFailure(t *testing.T) {
	setupTestDir(t)
	r := ParseAndExecute("cat /definitely_missing_file && echo unreachable")
	if r.ExitCode == 0 {
		t.Error("exit should be non-zero (cat failed)")
	}
	if strings.Contains(r.Stdout, "unreachable") || strings.Contains(r.Stderr, "unreachable") {
		t.Error("second segment must not run after a failed &&")
	}
}

func TestParseAndExecute_ChainOrRunsFallback(t *testing.T) {
	setupTestDir(t)
	r := ParseAndExecute("cat /definitely_missing_file || echo fallback")
	if r.ExitCode != 0 {
		t.Fatalf("exit = %d — || fallback should set exit 0", r.ExitCode)
	}
	if !strings.Contains(r.Stdout, "fallback") {
		t.Errorf("stdout = %q, want fallback output", r.Stdout)
	}
}

func TestParseAndExecute_ChainSemicolonAlwaysRuns(t *testing.T) {
	setupTestDir(t)
	r := ParseAndExecute("cat /definitely_missing_file ; echo always")
	if !strings.Contains(r.Stdout, "always") {
		t.Errorf("stdout = %q — ; must run the next segment regardless", r.Stdout)
	}
	if r.ExitCode != 0 {
		t.Errorf("exit = %d — last command (echo) should be 0", r.ExitCode)
	}
}

func TestParseAndExecute_ChainWithPipes(t *testing.T) {
	r := ParseAndExecute("echo alpha && echo beta | grep beta")
	if !strings.Contains(r.Stdout, "alpha") || !strings.Contains(r.Stdout, "beta") {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

// ─── /dev/null and compound redirects ────────────────────────────────────

func TestParseAndExecute_StderrToDevNull(t *testing.T) {
	setupTestDir(t)
	r := ParseAndExecute("cat /definitely_missing_file 2>/dev/null")
	if r.ExitCode == 0 {
		t.Error("exit should be 1 (file missing)")
	}
	if r.Stderr != "" {
		t.Errorf("stderr = %q — 2>/dev/null must discard it", r.Stderr)
	}
}

func TestParseAndExecute_StdoutToDevNull(t *testing.T) {
	r := ParseAndExecute("echo secret >/dev/null")
	if r.ExitCode != 0 {
		t.Fatalf("exit = %d", r.ExitCode)
	}
	if r.Stdout != "" {
		t.Errorf("stdout = %q — >/dev/null must discard it", r.Stdout)
	}
}

func TestParseRedirects_FusedCompound(t *testing.T) {
	name, _, _, stdoutFile, _, _, _ := ParseRedirects("git log 2>/dev/null")
	if name != "git" {
		t.Errorf("name = %q", name)
	}
	if stdoutFile != "" {
		t.Errorf("stdoutFile = %q, want empty", stdoutFile)
	}
}

func TestParseRedirects_MergeErrIntoOut(t *testing.T) {
	_, _, _, _, stderrFile, _, _ := ParseRedirects("cmd 2>&1")
	if stderrFile != mergeErrIntoOutSentinel {
		t.Errorf("stderrFile = %q, want merge sentinel", stderrFile)
	}
}

func TestParseAndExecute_MergeErrIntoOut(t *testing.T) {
	setupTestDir(t)
	r := ParseAndExecute("cat /definitely_missing_file 2>&1")
	if r.ExitCode == 0 {
		t.Error("cat on missing file must exit 1")
	}
	if r.Stdout == "" {
		t.Error("2>&1 must merge the error message into stdout")
	}
	if r.Stderr != "" {
		t.Errorf("stderr = %q, want empty after merge", r.Stderr)
	}
}

// ─── grep upgrades ───────────────────────────────────────────────────────

func writeGrepFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("alpha\nbeta\ngamma\n"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("beta2\n"), 0644)
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "c.go"), []byte("delta alpha\n"), 0644)
	return dir
}

func TestGrep_Recursive(t *testing.T) {
	dir := writeGrepFixture(t)
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	r := ParseAndExecute("grep -r alpha")
	if r.ExitCode != 0 {
		t.Fatalf("exit = %d stderr = %q", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "a.go") || !strings.Contains(r.Stdout, "sub/c.go") {
		t.Errorf("stdout = %q — recursive hits must carry file prefixes", r.Stdout)
	}
}

func TestGrep_RecursiveInclude(t *testing.T) {
	dir := writeGrepFixture(t)
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	r := ParseAndExecute(`grep -r --include=*.go alpha`)
	if !strings.Contains(r.Stdout, "a.go") {
		t.Errorf("stdout = %q — .go hit missing", r.Stdout)
	}
	if strings.Contains(r.Stdout, "b.txt") {
		t.Errorf("stdout = %q — include filter must exclude .txt", r.Stdout)
	}
}

func TestGrep_FlagCluster(t *testing.T) {
	dir := writeGrepFixture(t)
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	r := ParseAndExecute("grep -rn alpha")
	if !strings.Contains(r.Stdout, "a.go:1:alpha") {
		t.Errorf("stdout = %q — -rn must show file:line:match", r.Stdout)
	}
}

func TestGrep_OnlyMatching(t *testing.T) {
	r := ParseAndExecute("echo one two three | grep -o two")
	if r.Stdout != "two\n" {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

func TestGrep_ExtendedRegex(t *testing.T) {
	r := ParseAndExecute("echo cat dog | grep -E 'cat|dog'")
	if r.ExitCode != 0 || r.Stdout == "" {
		t.Errorf("exit = %d stdout = %q", r.ExitCode, r.Stdout)
	}
}

func TestGrep_NoMatchExitCode(t *testing.T) {
	r := ParseAndExecute("echo hello | grep zebra")
	if r.ExitCode != 1 {
		t.Errorf("exit = %d, want 1 for no matches", r.ExitCode)
	}
}

// ─── find upgrades ───────────────────────────────────────────────────────

func TestFind_MaxDepth(t *testing.T) {
	dir := writeGrepFixture(t)
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	r := ParseAndExecute("find . -maxdepth 1 -type f")
	if !strings.Contains(r.Stdout, "a.go") {
		t.Errorf("stdout = %q", r.Stdout)
	}
	if strings.Contains(r.Stdout, "c.go") {
		t.Errorf("stdout = %q — maxdepth 1 must exclude sub/", r.Stdout)
	}
}

func TestFind_OrAlternation(t *testing.T) {
	dir := writeGrepFixture(t)
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	r := ParseAndExecute(`find . -name "*.txt" -o -name "c.go"`)
	if !strings.Contains(r.Stdout, "b.txt") || !strings.Contains(r.Stdout, "c.go") {
		t.Errorf("stdout = %q — -o must union both names", r.Stdout)
	}
}

func TestFind_Not(t *testing.T) {
	dir := writeGrepFixture(t)
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	r := ParseAndExecute(`find . -name "*.go" -not -name "c.go"`)
	if !strings.Contains(r.Stdout, "a.go") {
		t.Errorf("stdout = %q", r.Stdout)
	}
	if strings.Contains(r.Stdout, "c.go") {
		t.Errorf("stdout = %q — -not must exclude c.go", r.Stdout)
	}
}

func TestFind_UnsupportedPredicateEscalates(t *testing.T) {
	r := ParseAndExecute("find . -exec wc -l {} \\;")
	if r.ExitCode != 127 {
		t.Errorf("exit = %d, want 127 (escalate to container)", r.ExitCode)
	}
}

// ─── ls upgrades ─────────────────────────────────────────────────────────

func TestLs_FlagClusters(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	os.WriteFile("f1.txt", []byte("x"), 0644)
	os.WriteFile("f2.txt", []byte("x"), 0644)

	for _, flag := range []string{"-la", "-lt", "-lat", "-ld", "-laR"} {
		r := ParseAndExecute("ls " + flag)
		if r.ExitCode != 0 {
			t.Errorf("ls %s exit = %d stderr = %q", flag, r.ExitCode, r.Stderr)
		}
	}
}

func TestLs_FileArgument(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	os.WriteFile("f1.txt", []byte("x"), 0644)

	r := ParseAndExecute("ls f1.txt")
	if r.ExitCode != 0 || !strings.Contains(r.Stdout, "f1.txt") {
		t.Errorf("exit = %d stdout = %q", r.ExitCode, r.Stdout)
	}
}

func TestLs_DirOnly(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)

	r := ParseAndExecute("ls -d .")
	if r.ExitCode != 0 {
		t.Fatalf("exit = %d stderr = %q", r.ExitCode, r.Stderr)
	}
	if strings.Count(r.Stdout, "\n") != 1 {
		t.Errorf("stdout = %q — ls -d lists one entry", r.Stdout)
	}
}

// ─── cat -n, wc -c, head/tail upgrades ──────────────────────────────────

func TestCat_NumberLines(t *testing.T) {
	r := ParseAndExecute("echo hello | cat -n")
	if !strings.Contains(r.Stdout, "1") || !strings.Contains(r.Stdout, "hello") {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

func TestWc_BytesVsChars(t *testing.T) {
	r := ParseAndExecute("echo hello | wc -c")
	if strings.TrimSpace(r.Stdout) != "6" {
		t.Errorf("wc -c stdout = %q, want 6 (5 chars + newline)", r.Stdout)
	}
}

func TestWc_MultiFile(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	os.WriteFile("one.txt", []byte("a\n"), 0644)
	os.WriteFile("two.txt", []byte("b c\n"), 0644)

	r := ParseAndExecute("wc -l one.txt two.txt")
	if !strings.Contains(r.Stdout, "one.txt") || !strings.Contains(r.Stdout, "two.txt") {
		t.Errorf("stdout = %q — multi-file wc must name each file", r.Stdout)
	}
}

func TestHead_ByteMode(t *testing.T) {
	r := ParseAndExecute("echo abcdefgh | head -c 3")
	if r.Stdout != "abc" {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

func TestTail_FromLine(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	os.WriteFile("lines.txt", []byte("1\n2\n3\n4\n5\n"), 0644)

	r := ParseAndExecute("tail -n +3 lines.txt")
	want := "3\n4\n5\n"
	if r.Stdout != want {
		t.Errorf("stdout = %q, want %q", r.Stdout, want)
	}
}

func TestTail_MultiFile(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	os.WriteFile("one.txt", []byte("a\n"), 0644)
	os.WriteFile("two.txt", []byte("b\n"), 0644)

	r := ParseAndExecute("tail -n 1 one.txt two.txt")
	if !strings.Contains(r.Stdout, "==> one.txt <==") || !strings.Contains(r.Stdout, "==> two.txt <==") {
		t.Errorf("stdout = %q — multi-file tail must print headers", r.Stdout)
	}
}

// ─── git command ─────────────────────────────────────────────────────────

func TestGit_WriteSubcommandEscalates(t *testing.T) {
	for _, sub := range []string{"add", "commit", "push", "pull", "checkout", "reset", "rm", "stash"} {
		r := ParseAndExecute("git " + sub + " .")
		if r.ExitCode != 127 {
			t.Errorf("git %s exit = %d, want 127 (write subcommands escalate)", sub, r.ExitCode)
		}
	}
}

func TestGit_ReadOnlySubcommandWithoutExecutor(t *testing.T) {
	RegisterGitExecutor(nil)
	defer RegisterGitExecutor(nil)

	r := ParseAndExecute("git status")
	if r.ExitCode != 127 {
		t.Errorf("exit = %d, want 127 when no git executor is installed", r.ExitCode)
	}
}

func TestGit_ReadOnlySubcommandWithExecutor(t *testing.T) {
	called := ""
	RegisterGitExecutor(func(subcommand string, args []string) CmdResult {
		called = subcommand
		return CmdResult{"M file.txt\n", "", 0}
	})
	defer RegisterGitExecutor(nil)

	r := ParseAndExecute("git status")
	if r.ExitCode != 0 {
		t.Fatalf("exit = %d stderr = %q", r.ExitCode, r.Stderr)
	}
	if called != "status" {
		t.Errorf("executor saw subcommand %q", called)
	}
	if r.Stdout != "M file.txt\n" {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

func TestGit_GlobalFlagC(t *testing.T) {
	seenSub := ""
	seenArgs := []string{}
	RegisterGitExecutor(func(subcommand string, args []string) CmdResult {
		seenSub = subcommand
		seenArgs = args
		return CmdResult{"", "", 0}
	})
	defer RegisterGitExecutor(nil)

	ParseAndExecute("git -C /repo status")
	if seenSub != "status" {
		t.Errorf("executor saw subcommand %q, want 'status' with -C peeled", seenSub)
	}
	if len(seenArgs) != 0 {
		t.Errorf("executor saw args %v, want none", seenArgs)
	}
}

func TestGit_InChain(t *testing.T) {
	RegisterGitExecutor(func(subcommand string, args []string) CmdResult {
		return CmdResult{"ok\n", "", 0}
	})
	defer RegisterGitExecutor(nil)

	r := ParseAndExecute("cd . && git status | grep ok")
	if r.ExitCode != 0 {
		t.Fatalf("exit = %d stderr = %q", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "ok") {
		t.Errorf("stdout = %q", r.Stdout)
	}
}
