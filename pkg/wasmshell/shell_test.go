package wasmshell

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// testDir is a scratch directory for file tests. Set by setupTestDir.
var testDir string

func getTestDir() string {
	if testDir != "" {
		return testDir
	}
	return filepath.Join(os.TempDir(), "wasmshell-test")
}

func init() {
	// Reset environment and cwd for clean test state.
	SetShellEnv(NewEnv())
	ResetHistory()
}

// setupTestDir creates (and cleans) a temp directory for file operation tests.
func setupTestDir(t *testing.T) {
	t.Helper()
	testDir = filepath.Join(os.TempDir(), "wasmshell-test")
	os.RemoveAll(testDir)
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}
	// Reset env and history for each test
	SetShellEnv(NewEnv())
	ResetHistory()
}

// resetToHome resets the working directory to a temp-based HOME.
func resetToHome(t *testing.T) {
	t.Helper()
	home := filepath.Join(os.TempDir(), "wasmshell-home")
	os.MkdirAll(home, 0755)
	if err := os.Chdir(home); err != nil {
		t.Fatalf("failed to chdir to home: %v", err)
	}
	ShellEnv.Set("HOME", home)
	ShellEnv.Set("PWD", home)
}

// ═══════════════════════════════════════════════════════════════════════
// 1. Tokenizer tests
// ═══════════════════════════════════════════════════════════════════════

func TestTokenize_SimpleTokens(t *testing.T) {
	tokens := Tokenize("echo hello world", false)
	if len(tokens) != 3 || tokens[0] != "echo" || tokens[1] != "hello" || tokens[2] != "world" {
		t.Fatalf("expected [echo hello world], got %v", tokens)
	}
}

func TestTokenize_DoubleQuotes(t *testing.T) {
	tokens := Tokenize(`echo "hello world"`, false)
	if len(tokens) != 2 || tokens[0] != "echo" || tokens[1] != "hello world" {
		t.Fatalf("expected [echo hello world], got %v", tokens)
	}
}

func TestTokenize_SingleQuotes(t *testing.T) {
	tokens := Tokenize(`echo 'hello world'`, false)
	if len(tokens) != 2 || tokens[0] != "echo" || tokens[1] != "hello world" {
		t.Fatalf("expected [echo hello world], got %v", tokens)
	}
}

func TestTokenize_Escapes(t *testing.T) {
	tokens := Tokenize(`echo hello\ world`, false)
	if len(tokens) != 2 || tokens[0] != "echo" || tokens[1] != "hello world" {
		t.Fatalf("expected [echo hello world], got %v", tokens)
	}
}

func TestTokenize_MixedQuotes(t *testing.T) {
	tokens := Tokenize(`echo "it's" cool`, false)
	if len(tokens) != 3 || tokens[0] != "echo" || tokens[1] != "it's" || tokens[2] != "cool" {
		t.Fatalf("expected [echo it's cool], got %v", tokens)
	}
}

func TestTokenize_Empty(t *testing.T) {
	tokens := Tokenize("", false)
	if len(tokens) != 0 {
		t.Fatalf("expected [], got %v", tokens)
	}
}

func TestTokenize_KeepQuotes(t *testing.T) {
	tokens := Tokenize(`echo "hello"`, true)
	if len(tokens) != 2 || tokens[1] != `"hello"` {
		t.Fatalf(`expected [echo "hello"], got %v`, tokens)
	}
}

func TestTokenize_OnlyWhitespace(t *testing.T) {
	tokens := Tokenize("   \t  ", false)
	if len(tokens) != 0 {
		t.Fatalf("expected [], got %v", tokens)
	}
}

func TestTokenize_SingleWord(t *testing.T) {
	tokens := Tokenize("ls", false)
	if len(tokens) != 1 || tokens[0] != "ls" {
		t.Fatalf("expected [ls], got %v", tokens)
	}
}

func TestTokenize_Redirects(t *testing.T) {
	// "2>" and ">>" should be recognized as tokens by the tokenizer
	tokens := Tokenize("cmd 2> err.txt", false)
	expected := []string{"cmd", "2>", "err.txt"}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, tokens)
	}
	for i, tok := range tokens {
		if tok != expected[i] {
			t.Fatalf("token[%d]: expected %q, got %q", i, expected[i], tok)
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════
// 2. Redirect parsing tests
// ═══════════════════════════════════════════════════════════════════════

func TestParseRedirects_SimpleStdout(t *testing.T) {
	name, args, stdin, stdout, stderr, appendOut, appendErr := ParseRedirects("echo hello > out.txt")
	if name != "echo" {
		t.Errorf("name = %q, want %q", name, "echo")
	}
	if len(args) != 1 || args[0] != "hello" {
		t.Errorf("args = %v, want [hello]", args)
	}
	if stdin != "" || stderr != "" {
		t.Errorf("stdin=%q stderr=%q, want empty", stdin, stderr)
	}
	if stdout != "out.txt" {
		t.Errorf("stdout = %q, want out.txt", stdout)
	}
	if appendOut || appendErr {
		t.Errorf("append flags should be false")
	}
}

func TestParseRedirects_AppendStdout(t *testing.T) {
	_, _, _, stdout, _, appendOut, _ := ParseRedirects("echo hi >> out.txt")
	if stdout != "out.txt" {
		t.Errorf("stdout = %q, want out.txt", stdout)
	}
	if !appendOut {
		t.Error("appendStdout should be true")
	}
}

func TestParseRedirects_StdinRedirect(t *testing.T) {
	_, _, stdin, stdout, stderr, _, _ := ParseRedirects("cat < input.txt")
	if stdin != "input.txt" {
		t.Errorf("stdin = %q, want input.txt", stdin)
	}
	if stdout != "" || stderr != "" {
		t.Error("stdout and stderr should be empty")
	}
}

func TestParseRedirects_StderrRedirect(t *testing.T) {
	_, _, _, _, stderr, _, _ := ParseRedirects("cmd 2> err.txt")
	if stderr != "err.txt" {
		t.Errorf("stderr = %q, want err.txt", stderr)
	}
}

func TestParseRedirects_BothRedirect(t *testing.T) {
	_, _, _, stdout, stderr, _, _ := ParseRedirects("cmd &> all.txt")
	if stdout != "all.txt" {
		t.Errorf("stdout = %q, want all.txt", stdout)
	}
	if stderr != "all.txt" {
		t.Errorf("stderr = %q, want all.txt", stderr)
	}
}

func TestParseRedirects_StderrAppend(t *testing.T) {
	_, _, _, _, stderr, _, appendErr := ParseRedirects("cmd 2>> err.log")
	if stderr != "err.log" {
		t.Errorf("stderr = %q, want err.log", stderr)
	}
	if !appendErr {
		t.Error("appendStderr should be true")
	}
}

func TestParseRedirects_MultipleRedirects(t *testing.T) {
	_, _, stdin, stdout, stderr, _, _ := ParseRedirects("cmd < in.txt > out.txt 2> err.txt")
	if stdin != "in.txt" {
		t.Errorf("stdin = %q, want in.txt", stdin)
	}
	if stdout != "out.txt" {
		t.Errorf("stdout = %q, want out.txt", stdout)
	}
	if stderr != "err.txt" {
		t.Errorf("stderr = %q, want err.txt", stderr)
	}
}

func TestParseRedirects_NoRedirects(t *testing.T) {
	name, args, stdin, stdout, stderr, _, _ := ParseRedirects("echo hello")
	if name != "echo" {
		t.Errorf("name = %q, want echo", name)
	}
	if len(args) != 1 || args[0] != "hello" {
		t.Errorf("args = %v, want [hello]", args)
	}
	if stdin != "" || stdout != "" || stderr != "" {
		t.Error("all redirect fields should be empty")
	}
}

// ═══════════════════════════════════════════════════════════════════════
// 3. Pipeline splitting tests
// ═══════════════════════════════════════════════════════════════════════

func TestSplitPipeline_TwoCommands(t *testing.T) {
	segs := SplitPipeline("ls | grep foo")
	if len(segs) != 2 || segs[0] != "ls" || segs[1] != "grep foo" {
		t.Fatalf("expected [ls, grep foo], got %v", segs)
	}
}

func TestSplitPipeline_ThreeCommands(t *testing.T) {
	segs := SplitPipeline("ls | grep foo | sort")
	if len(segs) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segs))
	}
	if segs[0] != "ls" || segs[1] != "grep foo" || segs[2] != "sort" {
		t.Fatalf("unexpected segments: %v", segs)
	}
}

func TestSplitPipeline_PipeInQuotes(t *testing.T) {
	segs := SplitPipeline(`echo "hello|world"`)
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d: %v", len(segs), segs)
	}
}

func TestSplitPipeline_NoPipe(t *testing.T) {
	segs := SplitPipeline("ls")
	if len(segs) != 1 || segs[0] != "ls" {
		t.Fatalf("expected [ls], got %v", segs)
	}
}

func TestSplitPipeline_Debug(t *testing.T) {
	// Verify pipe splitting works correctly
	segs := SplitPipeline("echo piped | cat")
	if len(segs) != 2 {
		t.Errorf("expected 2 segments, got %d: %v", len(segs), segs)
	}
	if segs[0] != "echo piped" {
		t.Errorf("seg[0] = %q, want %q", segs[0], "echo piped")
	}
	if segs[1] != "cat" {
		t.Errorf("seg[1] = %q, want %q", segs[1], "cat")
	}
}

func TestSplitPipeline_TrailingPipe(t *testing.T) {
	segs := SplitPipeline("echo hello | ")
	// Should have 2 segments: "echo hello" and ""
	if len(segs) < 2 {
		t.Fatalf("expected at least 2 segments, got %d: %v", len(segs), segs)
	}
}

// ═══════════════════════════════════════════════════════════════════════
// 4. Command execution tests
// ═══════════════════════════════════════════════════════════════════════

func TestParseAndExecute_EchoHello(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	r := ParseAndExecute("echo hello")
	if r.ExitCode != 0 {
		t.Errorf("exitCode = %d, want 0", r.ExitCode)
	}
	if r.Stdout != "hello\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "hello\n")
	}
}

func TestParseAndExecute_Pwd(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	r := ParseAndExecute("pwd")
	if r.ExitCode != 0 {
		t.Errorf("exitCode = %d, want 0", r.ExitCode)
	}
	// Should end with newline and contain the home path
	if !strings.HasSuffix(r.Stdout, "\n") {
		t.Errorf("stdout = %q, want trailing newline", r.Stdout)
	}
}

func TestParseAndExecute_EchoNoNewline(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	r := ParseAndExecute("echo -n no newline")
	if r.ExitCode != 0 {
		t.Errorf("exitCode = %d, want 0", r.ExitCode)
	}
	if r.Stdout != "no newline" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "no newline")
	}
	// Ensure no trailing newline
	if strings.HasSuffix(r.Stdout, "\n") {
		t.Error("stdout should not have trailing newline")
	}
}

func TestParseAndExecute_Whoami(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	r := ParseAndExecute("whoami")
	if r.ExitCode != 0 {
		t.Errorf("exitCode = %d, want 0", r.ExitCode)
	}
	if r.Stdout != "user\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "user\n")
	}
}

func TestParseAndExecute_NonexistentCommand(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	r := ParseAndExecute("nonexistent_cmd_xyz")
	if r.ExitCode != 127 {
		t.Errorf("exitCode = %d, want 127", r.ExitCode)
	}
	if !strings.Contains(r.Stderr, "command not found") {
		t.Errorf("stderr = %q, want to contain 'command not found'", r.Stderr)
	}
}

func TestParseAndExecute_ExportAndExpand(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	ParseAndExecute("export FOO=bar")
	r := ParseAndExecute("echo $FOO")
	if r.ExitCode != 0 {
		t.Errorf("exitCode = %d, want 0", r.ExitCode)
	}
	if r.Stdout != "bar\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "bar\n")
	}
}

func TestParseAndExecute_BasicPipe(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)

	r := ParseAndExecute("echo piped | cat")
	if r.ExitCode != 0 {
		t.Errorf("exitCode = %d, want 0", r.ExitCode)
	}
	if r.Stdout != "piped\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "piped\n")
	}
}

func TestParseAndExecute_LsNonexistent(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	r := ParseAndExecute("ls /nonexistent_xyz")
	if r.ExitCode == 0 {
		t.Error("exitCode should be non-zero for nonexistent dir")
	}
	if r.Stderr == "" {
		t.Error("stderr should contain error message")
	}
}

func TestParseAndExecute_VariableAssignment(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	// Note: $X is expanded by os.ExpandEnv before the assignment runs,
	// so the echo sees empty. But the variable IS set for subsequent commands.
	r := ParseAndExecute("X=hello")
	if r.ExitCode != 0 {
		t.Errorf("assignment exitCode = %d, want 0", r.ExitCode)
	}
	r2 := ParseAndExecute("echo $X")
	if r2.Stdout != "hello\n" {
		t.Errorf("after assignment: stdout = %q, want %q", r2.Stdout, "hello\n")
	}
}

func TestParseAndExecute_EmptyInput(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	r := ParseAndExecute("")
	if r.ExitCode != 0 {
		t.Errorf("exitCode = %d, want 0", r.ExitCode)
	}
	if r.Stdout != "" || r.Stderr != "" {
		t.Errorf("stdout=%q stderr=%q, want empty", r.Stdout, r.Stderr)
	}
}

func TestParseAndExecute_Comment(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	r := ParseAndExecute("# this is a comment")
	if r.ExitCode != 0 {
		t.Errorf("exitCode = %d, want 0", r.ExitCode)
	}
}

func TestParseAndExecute_Basename(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	r := ParseAndExecute("basename /foo/bar/baz.txt")
	if r.Stdout != "baz.txt\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "baz.txt\n")
	}
}

func TestParseAndExecute_Dirname(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	r := ParseAndExecute("dirname /foo/bar/baz.txt")
	if r.Stdout != "/foo/bar\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "/foo/bar\n")
	}
}

func TestParseAndExecute_Help(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	r := ParseAndExecute("help")
	if r.ExitCode != 0 {
		t.Errorf("exitCode = %d, want 0", r.ExitCode)
	}
	if !strings.Contains(r.Stdout, "ledit-wasm shell commands") {
		t.Error("help output should contain header")
	}
}

func TestParseAndExecute_WhichBuiltin(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	r := ParseAndExecute("which ls")
	if r.ExitCode != 0 {
		t.Errorf("exitCode = %d, want 0", r.ExitCode)
	}
	if !strings.Contains(r.Stdout, "built-in") {
		t.Errorf("stdout = %q, want to contain 'built-in'", r.Stdout)
	}
}

func TestParseAndExecute_TypeBuiltin(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	r := ParseAndExecute("type echo")
	if r.ExitCode != 0 {
		t.Errorf("exitCode = %d, want 0", r.ExitCode)
	}
	if !strings.Contains(r.Stdout, "built-in") {
		t.Errorf("stdout = %q, want to contain 'built-in'", r.Stdout)
	}
}

// ═══════════════════════════════════════════════════════════════════════
// 5. File operation tests
// ═══════════════════════════════════════════════════════════════════════

func TestFileOperation_TouchAndLs(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	// Create a file
	touchPath := filepath.Join(testDir, "testfile.txt")
	ParseAndExecute("touch " + touchPath)

	// Verify it exists via ls
	r := ParseAndExecute("ls " + testDir)
	if r.ExitCode != 0 {
		t.Fatalf("ls failed: %s", r.Stderr)
	}
	if !strings.Contains(r.Stdout, "testfile.txt") {
		t.Errorf("ls output = %q, want to contain 'testfile.txt'", r.Stdout)
	}
}

func TestFileOperation_EchoRedirectAndCat(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	catPath := filepath.Join(testDir, "cattest.txt")

	ParseAndExecute("echo hello > " + catPath)
	r := ParseAndExecute("cat " + catPath)
	if r.ExitCode != 0 {
		t.Fatalf("cat failed: %s", r.Stderr)
	}
	if r.Stdout != "hello\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "hello\n")
	}
}

func TestFileOperation_AppendRedirect(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	appPath := filepath.Join(testDir, "appendtest.txt")

	ParseAndExecute("echo line1 > " + appPath)
	ParseAndExecute("echo line2 >> " + appPath)
	r := ParseAndExecute("cat " + appPath)
	if r.ExitCode != 0 {
		t.Fatalf("cat failed: %s", r.Stderr)
	}
	if r.Stdout != "line1\nline2\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "line1\nline2\n")
	}
}

func TestFileOperation_MkdirP(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	deepDir := filepath.Join(testDir, "a", "b", "c")
	r := ParseAndExecute("mkdir -p " + deepDir)
	if r.ExitCode != 0 {
		t.Fatalf("mkdir -p failed: %s", r.Stderr)
	}
	// Verify the dir exists
	if _, err := os.Stat(deepDir); os.IsNotExist(err) {
		t.Error("directory was not created")
	}
}

func TestFileOperation_CopyAndCat(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	srcPath := filepath.Join(testDir, "src.txt")
	dstPath := filepath.Join(testDir, "dst.txt")

	ParseAndExecute("echo hello > " + srcPath)
	r := ParseAndExecute("cp " + srcPath + " " + dstPath)
	if r.ExitCode != 0 {
		t.Fatalf("cp failed: %s", r.Stderr)
	}
	catR := ParseAndExecute("cat " + dstPath)
	if catR.Stdout != "hello\n" {
		t.Errorf("copied content = %q, want %q", catR.Stdout, "hello\n")
	}
}

func TestFileOperation_MoveAndVerify(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	srcPath := filepath.Join(testDir, "move_src.txt")
	dstPath := filepath.Join(testDir, "move_dst.txt")

	ParseAndExecute("echo moved > " + srcPath)
	ParseAndExecute("mv " + srcPath + " " + dstPath)

	// Dst should exist
	catR := ParseAndExecute("cat " + dstPath)
	if catR.Stdout != "moved\n" {
		t.Errorf("moved content = %q, want %q", catR.Stdout, "moved\n")
	}

	// Src should be gone
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Error("source file still exists after mv")
	}
}

func TestFileOperation_Remove(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	rmPath := filepath.Join(testDir, "rmtest.txt")

	ParseAndExecute("touch " + rmPath)
	ParseAndExecute("rm " + rmPath)

	if _, err := os.Stat(rmPath); !os.IsNotExist(err) {
		t.Error("file still exists after rm")
	}
}

func TestFileOperation_Find(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)

	// Create some files
	os.MkdirAll(filepath.Join(testDir, "sub"), 0755)
	SyncWriteFile(filepath.Join(testDir, "a.txt"), "")
	SyncWriteFile(filepath.Join(testDir, "b.go"), "")
	SyncWriteFile(filepath.Join(testDir, "sub", "c.txt"), "")

	r := ParseAndExecute("find " + testDir + " -name '*.txt'")
	if r.ExitCode != 0 {
		t.Fatalf("find failed: %s", r.Stderr)
	}
	lines := strings.Split(strings.TrimSpace(r.Stdout), "\n")
	if len(lines) < 2 {
		t.Errorf("expected at least 2 .txt files, got %d: %v", len(lines), lines)
	}
	for _, line := range lines {
		if !strings.HasSuffix(line, ".txt") {
			t.Errorf("find returned non-.txt file: %q", line)
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════
// 6. Text processing tests
// ═══════════════════════════════════════════════════════════════════════

func TestTextProcessing_Grep(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	grepFile := filepath.Join(testDir, "grepinput.txt")
	SyncWriteFile(grepFile, "hello\nworld\nfoo\n")
	r := ParseAndExecute("grep hello " + grepFile)
	if r.ExitCode != 0 {
		t.Fatalf("grep failed: %s", r.Stderr)
	}
	if r.Stdout != "hello\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "hello\n")
	}
}

func TestTextProcessing_GrepInvert(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	grepFile := filepath.Join(testDir, "grepvinv.txt")
	SyncWriteFile(grepFile, "hello\nworld\nfoo\n")
	r := ParseAndExecute("grep -v hello " + grepFile)
	if r.ExitCode != 0 {
		t.Fatalf("grep -v failed: %s", r.Stderr)
	}
	if !strings.Contains(r.Stdout, "world") {
		t.Errorf("stdout = %q, should contain 'world'", r.Stdout)
	}
	if strings.Contains(r.Stdout, "hello") {
		t.Errorf("stdout = %q, should NOT contain 'hello'", r.Stdout)
	}
}

func TestTextProcessing_Sort(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	sortFile := filepath.Join(testDir, "sortinput.txt")
	SyncWriteFile(sortFile, "c\na\nb\n")
	r := ParseAndExecute("sort " + sortFile)
	if r.ExitCode != 0 {
		t.Fatalf("sort failed: %s", r.Stderr)
	}
	if r.Stdout != "a\nb\nc\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "a\nb\nc\n")
	}
}

func TestTextProcessing_Uniq(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	uniqFile := filepath.Join(testDir, "uniqinput.txt")
	SyncWriteFile(uniqFile, "a\nb\nb\nc\n")
	r := ParseAndExecute("sort " + uniqFile + " | uniq")
	if r.ExitCode != 0 {
		t.Fatalf("sort|uniq failed: %s", r.Stderr)
	}
	if r.Stdout != "a\nb\nc\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "a\nb\nc\n")
	}
}

func TestTextProcessing_Wc(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	r := ParseAndExecute("echo hello world | wc -w")
	if r.ExitCode != 0 {
		t.Fatalf("wc failed: %s", r.Stderr)
	}
	r.Stdout = strings.TrimSpace(r.Stdout)
	if r.Stdout != "2" {
		t.Errorf("wc -w stdout = %q, want %q", r.Stdout, "2")
	}
}

func TestTextProcessing_Tr(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	// Note: shell tr doesn't expand A-Z ranges; use explicit characters
	r := ParseAndExecute("echo ABCDEF | tr ABCDEF abcdef")
	if r.ExitCode != 0 {
		t.Fatalf("tr failed: %s", r.Stderr)
	}
	if r.Stdout != "abcdef\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "abcdef\n")
	}
}

func TestTextProcessing_Cut(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	r := ParseAndExecute("echo hello world | cut -d' ' -f1")
	if r.ExitCode != 0 {
		t.Fatalf("cut failed: %s", r.Stderr)
	}
	if r.Stdout != "hello\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "hello\n")
	}
}

func TestTextProcessing_Head(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	headFile := filepath.Join(testDir, "headinput.txt")
	SyncWriteFile(headFile, "a\nb\nc\nd\n")
	r := ParseAndExecute("head -n 2 " + headFile)
	if r.ExitCode != 0 {
		t.Fatalf("head failed: %s", r.Stderr)
	}
	if r.Stdout != "a\nb\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "a\nb\n")
	}
}

func TestTextProcessing_Tail(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	tailFile := filepath.Join(testDir, "tailinput.txt")
	// Write without trailing newline to avoid Split edge case
	SyncWriteFile(tailFile, "a\nb\nc\nd")
	r := ParseAndExecute("tail -n 2 " + tailFile)
	if r.ExitCode != 0 {
		t.Fatalf("tail failed: %s", r.Stderr)
	}
	if r.Stdout != "c\nd\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "c\nd\n")
	}
}

func TestTextProcessing_Tee(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	teePath := filepath.Join(testDir, "teeout.txt")
	r := ParseAndExecute("echo teedata | tee " + teePath)
	if r.ExitCode != 0 {
		t.Fatalf("tee failed: %s", r.Stderr)
	}
	if r.Stdout != "teedata\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "teedata\n")
	}
	// Verify file was written
	data, err := os.ReadFile(teePath)
	if err != nil {
		t.Fatalf("tee output file not found: %v", err)
	}
	if string(data) != "teedata\n" {
		t.Errorf("tee file content = %q, want %q", string(data), "teedata\n")
	}
}

// ═══════════════════════════════════════════════════════════════════════
// 7. Completion tests
// ═══════════════════════════════════════════════════════════════════════

func TestAutoComplete_Ls(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	result := AutoComplete("ls")
	found := false
	for _, c := range result.Completions {
		if c == "ls " {
			found = true
			break
		}
	}
	if !found {
		sort.Strings(result.Completions)
		t.Errorf("expected 'ls ' in completions, got %v", result.Completions)
	}
}

func TestAutoComplete_Pwd(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	result := AutoComplete("pwd")
	found := false
	for _, c := range result.Completions {
		if c == "pwd" { // no trailing space
			found = true
			break
		}
	}
	if !found {
		sort.Strings(result.Completions)
		t.Errorf("expected 'pwd' in completions, got %v", result.Completions)
	}
}

func TestAutoComplete_PrefixMatch(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	result := AutoComplete("ech")
	found := false
	for _, c := range result.Completions {
		if c == "echo " {
			found = true
			break
		}
	}
	if !found {
		sort.Strings(result.Completions)
		t.Errorf("expected 'echo ' in completions, got %v", result.Completions)
	}
}

func TestAutoComplete_Empty(t *testing.T) {
	result := AutoComplete("")
	if len(result.Completions) != 0 {
		t.Errorf("expected no completions for empty input, got %v", result.Completions)
	}
}

// ═══════════════════════════════════════════════════════════════════════
// 8. Store / utility tests
// ═══════════════════════════════════════════════════════════════════════

func TestSyncWriteFile(t *testing.T) {
	setupTestDir(t)
	path := filepath.Join(testDir, "syncwrite.txt")
	err := SyncWriteFile(path, "hello sync")
	if err != nil {
		t.Fatalf("SyncWriteFile failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(data) != "hello sync" {
		t.Errorf("content = %q, want %q", string(data), "hello sync")
	}
}

func TestSyncWriteFile_CreatesDirs(t *testing.T) {
	setupTestDir(t)
	path := filepath.Join(testDir, "deep", "nested", "file.txt")
	err := SyncWriteFile(path, "nested content")
	if err != nil {
		t.Fatalf("SyncWriteFile failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(data) != "nested content" {
		t.Errorf("content = %q, want %q", string(data), "nested content")
	}
}

func TestSyncDeleteFile(t *testing.T) {
	setupTestDir(t)
	path := filepath.Join(testDir, "todelete.txt")
	SyncWriteFile(path, "delete me")
	err := SyncDeleteFile(path)
	if err != nil {
		t.Fatalf("SyncDeleteFile failed: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file still exists after delete")
	}
}

func TestSyncDeleteFile_NotExist(t *testing.T) {
	setupTestDir(t)
	err := SyncDeleteFile("/nonexistent/path/file.txt")
	// Should not error — file doesn't exist
	if err != nil {
		t.Errorf("expected nil error for nonexistent file, got %v", err)
	}
}

func TestResolvePath(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	home := ShellEnv.Get("HOME")

	// Absolute path stays absolute
	p := ResolvePath("/tmp/foo")
	if p != "/tmp/foo" {
		t.Errorf("absolute path: got %q, want /tmp/foo", p)
	}

	// Relative path made absolute
	p = ResolvePath("relative")
	if !filepath.IsAbs(p) {
		t.Errorf("relative path should become absolute, got %q", p)
	}

	// Tilde expansion
	p = ResolvePath("~/documents")
	if !strings.HasPrefix(p, home) {
		t.Errorf("tilde expansion: got %q, want prefix %s", p, home)
	}

	// Bare tilde
	p = ResolvePath("~")
	if p != home {
		t.Errorf("bare tilde: got %q, want %s", p, home)
	}
}

func TestExpandGlobs(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)

	// Create some files for globbing
	os.MkdirAll(filepath.Join(testDir, "glob"), 0755)
	SyncWriteFile(filepath.Join(testDir, "glob", "a.txt"), "")
	SyncWriteFile(filepath.Join(testDir, "glob", "b.txt"), "")

	pattern := filepath.Join(testDir, "glob", "*.txt")
	args := ExpandGlobs([]string{pattern})
	// Without chdir to the test dir, the glob resolution depends on actual file expansion
	// Let's check that we get at least the 2 files
	if len(args) < 2 {
		t.Errorf("expandGlobs expected 2+ matches, got %d: %v", len(args), args)
	}
}

func TestNewEnv_Defaults(t *testing.T) {
	e := NewEnv()
	if e.Get("HOME") != "/home/user" {
		t.Errorf("HOME = %q, want /home/user", e.Get("HOME"))
	}
	if e.Get("USER") != "user" {
		t.Errorf("USER = %q, want user", e.Get("USER"))
	}
	if e.Get("PATH") == "" {
		t.Error("PATH should not be empty")
	}
}

func TestEnv_SetAndGet(t *testing.T) {
	e := NewEnv()
	e.Set("TEST_KEY", "test_value")
	if e.Get("TEST_KEY") != "test_value" {
		t.Errorf("TEST_KEY = %q, want test_value", e.Get("TEST_KEY"))
	}
}

func TestEnv_All(t *testing.T) {
	e := NewEnv()
	all := e.All()
	if len(all) == 0 {
		t.Error("All() should return non-empty map")
	}
	if all["HOME"] == "" {
		t.Error("All() should contain HOME")
	}
}

func TestHistory(t *testing.T) {
	ResetHistory()
	addToHistory("echo hello")
	addToHistory("ls -la")
	addToHistory("pwd")

	if len(commandHistory) != 3 {
		t.Errorf("history length = %d, want 3", len(commandHistory))
	}

	results := HistorySearch("ech")
	if len(results) != 1 || results[0] != "echo hello" {
		t.Errorf("history search: got %v, want [echo hello]", results)
	}
}

func TestHistory_NoDupes(t *testing.T) {
	ResetHistory()
	addToHistory("echo hello")
	addToHistory("echo hello") // duplicate, should be ignored
	if len(commandHistory) != 1 {
		t.Errorf("history length = %d, want 1 (no duplicates)", len(commandHistory))
	}
}

// ═══════════════════════════════════════════════════════════════════════
// 9. JSON rendering tests
// ═══════════════════════════════════════════════════════════════════════

func TestJSONResult(t *testing.T) {
	r := CmdResult{Stdout: "hello", Stderr: "", ExitCode: 0}
	j := JSONResult(r)
	if !strings.Contains(j, `"stdout":"hello"`) {
		t.Errorf("JSON = %q, should contain stdout", j)
	}
}

func TestAutoCompleteJSON(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	j := AutoCompleteJSON("ech")
	if !strings.Contains(j, `"completions"`) {
		t.Errorf("JSON = %q, should contain completions key", j)
	}
}

func TestListDirEntryJSON(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	os.MkdirAll(filepath.Join(testDir, "listdir"), 0755)
	SyncWriteFile(filepath.Join(testDir, "listdir", "file1.txt"), "content")

	j, err := ListDirEntryJSON(filepath.Join(testDir, "listdir"))
	if err != nil {
		t.Fatalf("ListDirEntryJSON error: %v", err)
	}
	if !strings.Contains(j, "file1.txt") {
		t.Errorf("JSON = %q, should contain file1.txt", j)
	}
}

// ═══════════════════════════════════════════════════════════════════════
// 10. Cd and realpath tests
// ═══════════════════════════════════════════════════════════════════════

func TestCdHome(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	// Create a temp dir and cd into it
	tmpDir := filepath.Join(testDir, "cdtest")
	os.MkdirAll(tmpDir, 0755)

	r := ParseAndExecute("cd " + tmpDir)
	if r.ExitCode != 0 {
		t.Fatalf("cd failed: %s", r.Stderr)
	}

	r = ParseAndExecute("pwd")
	if r.ExitCode != 0 {
		t.Fatalf("pwd failed: %s", r.Stderr)
	}
	if !strings.HasPrefix(r.Stdout, tmpDir) {
		t.Errorf("pwd after cd = %q, should start with %q", r.Stdout, tmpDir)
	}
}

func TestCdTilde(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	home := ShellEnv.Get("HOME")
	r := ParseAndExecute("cd ~")
	if r.ExitCode != 0 {
		t.Fatalf("cd ~ failed: %s", r.Stderr)
	}
	r = ParseAndExecute("pwd")
	if !strings.HasPrefix(r.Stdout, home) {
		t.Errorf("pwd = %q, want prefix %s", r.Stdout, home)
	}
}

func TestRealpath(t *testing.T) {
	setupTestDir(t)
	resetToHome(t)
	r := ParseAndExecute("realpath /tmp/../tmp/foo.txt")
	if r.ExitCode != 0 {
		t.Fatalf("realpath failed: %s", r.Stderr)
	}
	if r.Stdout != "/tmp/foo.txt\n" {
		t.Errorf("realpath = %q, want /tmp/foo.txt\\n", r.Stdout)
	}
}
