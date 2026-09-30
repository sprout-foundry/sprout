package wasmshell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shellCase is one script with its expected stdout and exit code.
type shellCase struct {
	name   string
	script string
	stdout string
	exit   int
}

// inScratchDir runs the test in a fresh working directory with a clean
// shell environment.
func inScratchDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	SetShellEnv(NewEnv())
	ShellEnv.Set("HOME", dir)
	ShellEnv.Set("PWD", dir)
	functions = map[string]command{}
	lastExitCode = 0
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runShellCases(t *testing.T, files map[string]string, cases []shellCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inScratchDir(t, files)
			r := ParseAndExecute(tc.script)
			if r.Stdout != tc.stdout || r.ExitCode != tc.exit {
				t.Errorf("script:\n%s\nstdout = %q (exit %d), want %q (exit %d)\nstderr = %q",
					tc.script, r.Stdout, r.ExitCode, tc.stdout, tc.exit, r.Stderr)
			}
			assertOrderedMatchesStreams(t, r)
		})
	}
}

func TestShellLanguage(t *testing.T) {
	runShellCases(t, nil, []shellCase{
		{"newline separates commands", "echo a\necho b", "a\nb\n", 0},
		{"comments", "echo a # trailing\n# whole line\necho b", "a\nb\n", 0},
		{"single quotes are literal", `echo '$HOME'`, "$HOME\n", 0},
		{"assignment then use", "X=1; echo $X", "1\n", 0},
		{"export then use in chain", "export Y=2 && echo $Y", "2\n", 0},
		{"exit status", "false; echo $?", "1\n", 0},
		{"negation", "! false && echo ok", "ok\n", 0},
		{"brace expansion defaults", `echo ${UNSET:-dflt} ${UNSET-x}`, "dflt x\n", 0},
		{"assign default", `echo ${V:=set}; echo $V`, "set\nset\n", 0},
		{"length", `S=hello; echo ${#S}`, "5\n", 0},
		{"prefix and suffix trim", `F=dir/sub/file.tar.gz; echo ${F##*/} ${F%%.*} ${F#*/} ${F%.*}`, "file.tar.gz dir/sub/file sub/file.tar.gz dir/sub/file.tar\n", 0},
		{"replace", `P=a-b-c; echo ${P/-/_} ${P//-/_}`, "a_b-c a_b_c\n", 0},
		{"substring and case", `S=abcdef; echo ${S:1:3} ${S: -2} ${S^^}`, "bcd ef ABCDEF\n", 0},
		{"command substitution", `echo "got $(echo hi)" and ` + "`echo ho`", "got hi and ho\n", 0},
		{"nested substitution", `echo $(echo $(echo deep))`, "deep\n", 0},
		{"arithmetic", `i=3; echo $((i * 2 + 1)) $((10 / 3)) $((2 ** 8)) $((7 % 3))`, "7 3 256 1\n", 0},
		{"arithmetic command", `i=0; ((i++)); ((i += 5)); echo $i`, "6\n", 0},
		{"let", `let x=4*5; echo $x`, "20\n", 0},
		{"if elif else", "x=2\nif [ $x -eq 1 ]; then echo one\nelif [ $x -eq 2 ]; then echo two\nelse echo other\nfi", "two\n", 0},
		{"for loop", `for f in a b c; do echo -n $f; done; echo`, "abc\n", 0},
		{"c-style for", `for ((i=0; i<3; i++)); do echo $i; done`, "0\n1\n2\n", 0},
		{"while with counter", `n=0; while [ $n -lt 3 ]; do n=$((n+1)); done; echo $n`, "3\n", 0},
		{"until", `n=0; until [ $n -ge 2 ]; do n=$((n+1)); done; echo $n`, "2\n", 0},
		{"break and continue", `for i in 1 2 3 4 5; do [ $i = 2 ] && continue; [ $i = 4 ] && break; echo $i; done`, "1\n3\n", 0},
		{"case", `for f in a.go b.ts c.md; do case $f in *.go) echo go;; *.ts|*.js) echo web;; *) echo other;; esac; done`, "go\nweb\nother\n", 0},
		{"function with args and return", `greet() { echo "hi $1 ($#)"; return 3; }; greet bob x; echo $?`, "hi bob (2)\n3\n", 0},
		{"function keyword and local", `function f { local v=inner; echo $v; }; v=outer; f; echo $v`, "inner\nouter\n", 0},
		{"subshell isolates", `X=1; (X=2; cd /; echo $X); echo $X`, "2\n1\n", 0},
		{"brace group redirect", `{ echo a; echo b; } | wc -l | tr -d ' '`, "2\n", 0},
		{"loop piped", `for i in 3 1 2; do echo $i; done | sort`, "1\n2\n3\n", 0},
		{"or group", `false || { echo recovered; }`, "recovered\n", 0},
		{"set -e stops", "set -e\necho a\nfalse\necho b", "a\n", 1},
		{"set -e ignores conditions", "set -e\nif false; then :; fi\nfalse || true\necho done", "done\n", 0},
		{"pipefail", "set -o pipefail; false | true; echo $?", "1\n", 0},
		{"exit stops script", "echo a; exit 4; echo b", "a\n", 4},
		{"quoted $@", `set -- "a b" c; for x in "$@"; do echo "[$x]"; done`, "[a b]\n[c]\n", 0},
		{"unquoted splitting", `V="1 2 3"; for x in $V; do echo $x; done`, "1\n2\n3\n", 0},
		{"quoted keeps spaces", `V="1  2"; echo "$V"`, "1  2\n", 0},
		{"empty unquoted var vanishes", `E=; set -- a $E b; echo $#`, "2\n", 0},
		{"double bracket", `f=main.go; [[ $f == *.go && -n $f ]] && echo yes`, "yes\n", 0},
		{"double bracket regex", `[[ v1.22 =~ ^v[0-9]+\.[0-9]+$ ]] && echo match`, "match\n", 0},
		{"ansi c quoting", `printf '%s' $'a\tb'`, "a\tb", 0},
		{"line continuation", "echo a \\\n  b", "a b\n", 0},
		{"eval", `cmd="echo evaluated"; eval $cmd`, "evaluated\n", 0},
		{"read from here-string", `read a b <<< "one two three"; echo "$a|$b"`, "one|two three\n", 0},
		{"while read loop", "printf 'x\\ny\\n' | while read line; do echo \"<$line>\"; done", "<x>\n<y>\n", 0},
		{"sh -c with args", `sh -c 'echo $0 $1' name arg`, "name arg\n", 0},
		{"xtrace goes to stderr", "set -x; echo hi", "hi\n", 0},
		{"brace expansion", "echo a{b,c}d {1..3} {x..z} f{,.bak}", "abd acd 1 2 3 x y z f f.bak\n", 0},
		{"brace in mkdir", "mkdir -p p/{a,b} && ls p", "a\nb\n", 0},
		{"quoted braces stay", `echo "{a,b}" ${HOME:+{x,y}}`, "{a,b} {x,y}\n", 0},
		{"arithmetic bases", "echo $((16#ff)) $((0x10)) $((010))", "255 16 8\n", 0},
		{"syntax error", "if true; then echo", "", 2},
		{"unsupported syntax escalates", "a=(1 2 3)", "", 127},
	})
}

func TestShellHeredocs(t *testing.T) {
	runShellCases(t, nil, []shellCase{
		{"heredoc to file", "cat > f.txt <<EOF\nline $((1+1))\nEOF\ncat f.txt", "line 2\n", 0},
		{"quoted heredoc is literal", "cat <<'EOF'\n$HOME `x`\nEOF", "$HOME `x`\n", 0},
		{"dash heredoc strips tabs", "cat <<-END\n\tindented\n\tEND\necho after", "indented\nafter\n", 0},
		{"heredoc then more commands", "cat <<EOF | tr a-z A-Z\nabc\nEOF\necho next", "ABC\nnext\n", 0},
		{"two heredocs", "cat <<A; cat <<B\none\nA\ntwo\nB", "one\ntwo\n", 0},
	})
}

func TestShellRedirections(t *testing.T) {
	runShellCases(t, map[string]string{"in.txt": "from file\n"}, []shellCase{
		{"stdin from file", "cat < in.txt", "from file\n", 0},
		{"stderr to stdout", "cat missing 2>&1 | grep -c 'No such'", "1\n", 0},
		{"order of 2>&1 and >", "cat missing > out.txt 2>&1; grep -c 'No such' out.txt", "1\n", 0},
		{"both to file", "{ echo out; cat missing; } &> all.txt; wc -l < all.txt | tr -d ' '", "2\n", 0},
		{"stdout to stderr", "echo warn >&2", "", 0},
		{"append", "echo a > f; echo b >> f; cat f", "a\nb\n", 0},
		{"empty redirect creates file", ": > empty.txt; [ -f empty.txt ] && echo made", "made\n", 0},
		{"missing directory fails", "echo x > nodir/f.txt", "", 1},
		{"devnull", "echo gone > /dev/null; echo kept", "kept\n", 0},
	})
}

func TestShellGlobs(t *testing.T) {
	files := map[string]string{"a.go": "", "b.go": "", "c.txt": "", ".hidden.go": "", "sub/d.go": ""}
	runShellCases(t, files, []shellCase{
		{"relative glob", "echo *.go", "a.go b.go\n", 0},
		{"quoted glob is literal", `echo "*.go"`, "*.go\n", 0},
		{"no match stays literal", "echo *.rs", "*.rs\n", 0},
		{"glob in for", "for f in *.txt; do echo $f; done", "c.txt\n", 0},
		{"dotfiles need a dot", "echo .*.go", ".hidden.go\n", 0},
		{"dir glob", "echo sub/*", filepath.FromSlash("sub/d.go") + "\n", 0},
	})
}

func TestShellScriptFiles(t *testing.T) {
	files := map[string]string{
		"build.sh": "#!/usr/bin/env bash\nset -e\necho \"building $1\"\n",
		"tool.py":  "#!/usr/bin/env python3\nprint('x')\n",
		"lib.sh":   "helper() { echo helped; }\nLIBVAR=loaded\n",
	}
	runShellCases(t, files, []shellCase{
		{"run shell script by path", "./build.sh app", "building app\n", 0},
		{"bash script", "bash build.sh lib", "building lib\n", 0},
		{"other interpreters escalate", "./tool.py", "", 127},
		{"source shares state", ". ./lib.sh; helper; echo $LIBVAR", "helped\nloaded\n", 0},
		{"missing command", "definitely-not-a-command", "", 127},
		{"command -v", "command -v echo; command -v nope; echo $?", "echo\n1\n", 0},
		{"type of function", "f() { :; }; type -t f; type -t echo; type -t if", "function\nbuiltin\nkeyword\n", 0},
	})
}

func TestShellLoopLimit(t *testing.T) {
	inScratchDir(t, nil)
	r := ParseAndExecute("while true; do :; done")
	if r.ExitCode != 1 || !strings.Contains(r.Stderr, "loop stopped") {
		t.Errorf("exit = %d stderr = %q, want the loop limit", r.ExitCode, r.Stderr)
	}
}
