package wasmshell

import (
	"encoding/json"
	"strings"
	"testing"
)

// transcript renders the ordered output with stderr runs in brackets.
func transcript(r CmdResult) string {
	var b strings.Builder
	for _, s := range r.ordered().Output {
		if s.Err {
			b.WriteString("[" + s.Text + "]")
		} else {
			b.WriteString(s.Text)
		}
	}
	return b.String()
}

// assertOrderedMatchesStreams checks the ordered output holds exactly what
// Stdout and Stderr hold.
func assertOrderedMatchesStreams(t *testing.T, r CmdResult) {
	t.Helper()
	var out, errs strings.Builder
	for _, s := range r.segs() {
		if s.Err {
			errs.WriteString(s.Text)
		} else {
			out.WriteString(s.Text)
		}
	}
	if out.String() != r.Stdout || errs.String() != r.Stderr {
		t.Errorf("ordered output drifted from the streams:\nstdout %q vs %q\nstderr %q vs %q",
			out.String(), r.Stdout, errs.String(), r.Stderr)
	}
}

func TestOutputOrder(t *testing.T) {
	cases := []struct {
		name, script, want string
	}{
		{"error then output", "nosuch; echo x", "[command not found: nosuch\n]x\n"},
		{"output then error then output", "echo a; nosuch; echo b", "a\n[command not found: nosuch\n]b\n"},
		{"missing file then done", "cat nope; echo done", "[cat: nope: No such file or directory\n]done\n"},
		{"or branch after failure", "cat nope || echo fallback", "[cat: nope: No such file or directory\n]fallback\n"},
		{"and chain stops", "echo a && nosuch && echo never", "a\n[command not found: nosuch\n]"},
		{"pipeline: earlier stage stderr, then result", "echo z; cat nope | wc -l", "z\n[cat: nope: No such file or directory\n]0\n"},
		{"2>&1 keeps order on stdout", "{ echo a; nosuch; echo b; } 2>&1 | cat", "a\ncommand not found: nosuch\nb\n"},
		{"2>/dev/null drops errors only", "{ echo a; nosuch; echo b; } 2>/dev/null", "a\nb\n"},
		{">&2 moves output to stderr", "echo a; echo warn >&2; echo b", "a\n[warn\n]b\n"},
		{"stdout to file leaves errors", "{ echo a; nosuch; } > out.txt; cat out.txt", "[command not found: nosuch\n]a\n"},
		{"&> file keeps order in the file", "{ echo a; nosuch; echo b; } &> all.txt; cat all.txt", "a\ncommand not found: nosuch\nb\n"},
		{"substitution stderr precedes its command", "echo before; echo \"got $(cat nope)\"", "before\n[cat: nope: No such file or directory\n]got \n"},
		{"function interleaves", "f() { echo in; nosuch; echo out; }; f", "in\n[command not found: nosuch\n]out\n"},
		{"loop interleaves", "for i in 1 2; do echo $i; nosuch; done", "1\n[command not found: nosuch\n]2\n[command not found: nosuch\n]"},
		{"xtrace precedes each command", "set -x; echo a; echo b", "[+ echo a\n]a\n[+ echo b\n]b\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inScratchDir(t, nil)
			r := ParseAndExecute(tc.script)
			if got := transcript(r); got != tc.want {
				t.Errorf("script %q\ntranscript = %q\nwant         %q", tc.script, got, tc.want)
			}
			assertOrderedMatchesStreams(t, r)
		})
	}
}

func TestJSONResult_OrderedOutput(t *testing.T) {
	inScratchDir(t, nil)
	var got struct {
		Stdout string   `json:"stdout"`
		Stderr string   `json:"stderr"`
		Output []OutSeg `json:"output"`
	}
	if err := json.Unmarshal([]byte(JSONResult(ParseAndExecute("nosuch; echo x; echo y"))), &got); err != nil {
		t.Fatal(err)
	}
	want := []OutSeg{{Err: true, Text: "command not found: nosuch\n"}, {Text: "x\ny\n"}}
	if len(got.Output) != len(want) || got.Output[0] != want[0] || got.Output[1] != want[1] {
		t.Errorf("output = %+v, want %+v", got.Output, want)
	}
	if got.Stdout != "x\ny\n" || got.Stderr != "command not found: nosuch\n" {
		t.Errorf("streams changed: stdout %q stderr %q", got.Stdout, got.Stderr)
	}

	var empty struct {
		Output []OutSeg `json:"output"`
	}
	_ = json.Unmarshal([]byte(JSONResult(ParseAndExecute("true"))), &empty)
	if empty.Output == nil {
		t.Error("a command with no output should still carry an empty output list")
	}
}
