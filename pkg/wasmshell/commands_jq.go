package wasmshell

import (
	"fmt"
	"github.com/sprout-foundry/sprout/pkg/utils"
	"strconv"
	"strings"
)

// cmdJq runs a jq filter over the JSON values in the input.
func cmdJq(args []string, stdin string) CmdResult {
	raw, compact, join, sortKeys, slurp, nullIn, exitStatus, rawIn, tab := false, false, false, false, false, false, false, false, false
	indent := 2
	env := &jqEnv{vars: map[string]any{}}
	var filter *string
	var files []string
	args = expandClusters(args, "rcjSsneRCMa", "")
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-r", "--raw-output":
			raw = true
		case "-j", "--join-output":
			raw, join = true, true
		case "-c", "--compact-output":
			compact = true
		case "-S", "--sort-keys":
			sortKeys = true
		case "-s", "--slurp":
			slurp = true
		case "-n", "--null-input":
			nullIn = true
		case "-e", "--exit-status":
			exitStatus = true
		case "-R", "--raw-input":
			rawIn = true
		case "--tab":
			tab = true
		case "--indent":
			if i+1 < len(args) {
				i++
				indent, _ = strconv.Atoi(args[i])
			}
		case "-C", "-M", "-a", "--color-output", "--monochrome-output", "--ascii-output", "--seq", "--stream-errors":
		case "--arg", "--argjson":
			if i+2 >= len(args) {
				return CmdResult{Stdout: "", Stderr: "jq: " + a + " takes two parameters (e.g. " + a + " varname value)\n", ExitCode: 2}
			}
			name, val := args[i+1], args[i+2]
			i += 2
			if a == "--arg" {
				env.vars[name] = val
				continue
			}
			vals, err := decodeJSONStream(val)
			if err != nil || len(vals) != 1 {
				return CmdResult{Stdout: "", Stderr: "jq: invalid JSON text passed to --argjson\n", ExitCode: 2}
			}
			env.vars[name] = vals[0]
		default:
			if strings.HasPrefix(a, "-") && len(a) > 1 {
				return CmdResult{Stdout: "", Stderr: "jq: unsupported option " + a + "\n", ExitCode: ExitCommandNotFound}
			}
			if filter == nil {
				f := a
				filter = &f
			} else {
				files = append(files, a)
			}
		}
	}
	if filter == nil {
		f := "."
		filter = &f
	}
	node, err := parseJq(*filter)
	if err != nil {
		code := 3
		if strings.Contains(err.Error(), "not supported") {
			code = ExitCommandNotFound
		}
		return CmdResult{Stdout: "", Stderr: "jq: error: " + err.Error() + "\njq: 1 compile error\n", ExitCode: code}
	}

	input, errRes := readInputs("jq", files, stdin)
	if errRes != nil {
		return *errRes
	}
	var inputs []any
	switch {
	case nullIn:
		inputs = []any{nil}
	case rawIn:
		for _, l := range utils.SplitLines(input) {
			inputs = append(inputs, l)
		}
		if slurp {
			inputs = []any{input}
		}
	default:
		vals, err := decodeJSONStream(input)
		if err != nil {
			return CmdResult{Stdout: "", Stderr: fmt.Sprintf("jq: error (at <stdin>:0): Cannot parse input: %s\n", err), ExitCode: 2}
		}
		inputs = vals
		if slurp {
			if inputs == nil {
				inputs = []any{}
			}
			inputs = []any{inputs}
		}
	}

	ind := strings.Repeat(" ", indent)
	if tab {
		ind = "\t"
	}
	if compact || indent == 0 {
		ind = ""
	}
	var out strings.Builder
	var last any
	produced := false
	for _, in := range inputs {
		results, err := env.eval(node, in)
		for _, r := range results {
			produced = true
			last = r
			if s, ok := r.(string); ok && raw {
				out.WriteString(s)
			} else {
				out.WriteString(encodeJSON(r, ind, sortKeys))
			}
			if !join {
				out.WriteByte('\n')
			}
		}
		if err != nil {
			return CmdResult{Stdout: out.String(), Stderr: "jq: error (at <stdin>:0): " + err.Error() + "\n", ExitCode: 5}
		}
	}
	code := 0
	if exitStatus {
		switch {
		case !produced:
			code = 4
		case !jqTruthy(last):
			code = 1
		}
	}
	return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: code}
}

func init() {
	CmdRegistry["jq"] = cmdJq
}
