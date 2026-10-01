package wasmshell

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func init() {
	CmdRegistry["diff"] = cmdDiff
	CmdRegistry["cmp"] = cmdCmp
}

type diffOp struct {
	kind byte // ' ', '-', '+'
	a, b int  // line indexes in a and b
}

// myersDiff returns the edit script turning a into b (shortest edit
// sequence, Myers' algorithm).
func myersDiff(a, b []string) []diffOp {
	n, m := len(a), len(b)
	maxD := n + m
	v := make([]int, 2*maxD+2)
	var trace [][]int
	for d := 0; d <= maxD; d++ {
		if d*len(v) > 50_000_000 {
			return replaceAll(n, m)
		}
		snapshot := make([]int, len(v))
		copy(snapshot, v)
		trace = append(trace, snapshot)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[maxD+k-1] < v[maxD+k+1]) {
				x = v[maxD+k+1]
			} else {
				x = v[maxD+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[maxD+k] = x
			if x >= n && y >= m {
				return backtrack(trace, a, b, maxD, d)
			}
		}
	}
	return nil
}

// replaceAll is the fallback edit script for inputs too different to diff
// within the memory budget: every line removed, then every line added.
func replaceAll(n, m int) []diffOp {
	ops := make([]diffOp, 0, n+m)
	for i := 0; i < n; i++ {
		ops = append(ops, diffOp{'-', i, 0})
	}
	for j := 0; j < m; j++ {
		ops = append(ops, diffOp{'+', n, j})
	}
	return ops
}

func backtrack(trace [][]int, a, b []string, offset, d int) []diffOp {
	x, y := len(a), len(b)
	var ops []diffOp
	for ; d > 0; d-- {
		v := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := v[offset+prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			x--
			y--
			ops = append(ops, diffOp{' ', x, y})
		}
		if x == prevX {
			y--
			ops = append(ops, diffOp{'+', x, y})
		} else {
			x--
			ops = append(ops, diffOp{'-', x, y})
		}
	}
	for x > 0 && y > 0 {
		x--
		y--
		ops = append(ops, diffOp{' ', x, y})
	}
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}
	return ops
}

type diffOpts struct {
	unified    bool
	context    int
	brief      bool
	recursive  bool
	ignoreWS   bool
	ignoreAll  bool
	ignoreCase bool
	labels     []string
}

func cmdDiff(args []string, stdin string) CmdResult {
	o := diffOpts{context: 3}
	var files []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-u" || a == "--unified":
			o.unified = true
		case strings.HasPrefix(a, "-U") || strings.HasPrefix(a, "--unified="):
			o.unified = true
			v := strings.TrimPrefix(strings.TrimPrefix(a, "-U"), "--unified=")
			if v == "" && i+1 < len(args) {
				i++
				v = args[i]
			}
			if n, err := strconv.Atoi(v); err == nil {
				o.context = n
			}
		case a == "-q" || a == "--brief":
			o.brief = true
		case a == "-r" || a == "-N" || a == "--recursive" || a == "-Nr" || a == "-rN" || a == "-ruN" || a == "-Nru" || a == "-ur" || a == "-ru":
			o.recursive = true
			if strings.Contains(a, "u") {
				o.unified = true
			}
		case a == "-b" || a == "--ignore-space-change":
			o.ignoreWS = true
		case a == "-w" || a == "--ignore-all-space":
			o.ignoreAll = true
		case a == "-i" || a == "--ignore-case":
			o.ignoreCase = true
		case a == "--label" && i+1 < len(args):
			i++
			o.labels = append(o.labels, args[i])
		case a == "--color" || strings.HasPrefix(a, "--color=") || a == "-a" || a == "--text" || a == "-B":
		case strings.HasPrefix(a, "-") && a != "-":
			return CmdResult{Stdout: "", Stderr: "diff: unsupported option " + a + "\n", ExitCode: ExitCommandNotFound}
		default:
			files = append(files, a)
		}
	}
	if len(files) != 2 {
		return CmdResult{Stdout: "", Stderr: "diff: missing operand\n", ExitCode: 2}
	}
	var out strings.Builder
	code, err := o.diffPaths(&out, files[0], files[1], stdin)
	if err != nil {
		return CmdResult{Stdout: out.String(), Stderr: "diff: " + err.Error() + "\n", ExitCode: 2}
	}
	return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: code}
}

func (o *diffOpts) diffPaths(out *strings.Builder, left, right, stdin string) (int, error) {
	li, lerr := os.Stat(ResolvePath(left))  //nolint:gosec // G703: shell commands act on the paths the user names
	ri, rerr := os.Stat(ResolvePath(right)) //nolint:gosec // G703: shell commands act on the paths the user names
	if left != "-" && lerr != nil {
		return 2, fmt.Errorf("%s: %s", left, describeErr(lerr))
	}
	if right != "-" && rerr != nil {
		return 2, fmt.Errorf("%s: %s", right, describeErr(rerr))
	}
	lDir, rDir := left != "-" && li.IsDir(), right != "-" && ri.IsDir()
	switch {
	case lDir && rDir:
		return o.diffDirs(out, left, right)
	case lDir:
		left = filepath.Join(left, filepath.Base(right))
	case rDir:
		right = filepath.Join(right, filepath.Base(left))
	}
	read := func(p string) (string, error) {
		if p == "-" {
			return stdin, nil
		}
		return readFileArg(p)
	}
	a, err := read(left)
	if err != nil {
		return 2, fmt.Errorf("%s: %s", left, describeErrText(err))
	}
	b, err := read(right)
	if err != nil {
		return 2, fmt.Errorf("%s: %s", right, describeErrText(err))
	}
	return o.diffText(out, left, right, a, b), nil
}

func (o *diffOpts) diffDirs(out *strings.Builder, left, right string) (int, error) {
	names := map[string]int{}
	for side, dir := range []string{left, right} {
		entries, err := ReadDirCompat(ResolvePath(dir))
		if err != nil {
			return 2, err
		}
		for _, e := range entries {
			names[e.Name()] |= 1 << side
		}
	}
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	code := 0
	for _, k := range keys {
		lp, rp := filepath.Join(left, k), filepath.Join(right, k)
		switch names[k] {
		case 1:
			fmt.Fprintf(out, "Only in %s: %s\n", left, k)
			code = 1
		case 2:
			fmt.Fprintf(out, "Only in %s: %s\n", right, k)
			code = 1
		default:
			li, _ := os.Stat(ResolvePath(lp)) //nolint:gosec // G703: shell commands act on the paths the user names
			ri, _ := os.Stat(ResolvePath(rp)) //nolint:gosec // G703: shell commands act on the paths the user names
			if li.IsDir() && ri.IsDir() {
				if !o.recursive {
					fmt.Fprintf(out, "Common subdirectories: %s and %s\n", lp, rp)
					continue
				}
				c, err := o.diffDirs(out, lp, rp)
				if err != nil {
					return 2, err
				}
				code = max(code, c)
				continue
			}
			c, err := o.diffPaths(out, lp, rp, "")
			if err != nil {
				return 2, err
			}
			code = max(code, c)
		}
	}
	return code, nil
}

func (o *diffOpts) normalize(l string) string {
	switch {
	case o.ignoreAll:
		l = strings.Join(strings.Fields(l), "")
	case o.ignoreWS:
		l = strings.Join(strings.Fields(l), " ")
	}
	if o.ignoreCase {
		l = strings.ToLower(l)
	}
	return l
}

func (o *diffOpts) diffText(out *strings.Builder, leftName, rightName, a, b string) int {
	al, bl := splitLines(a), splitLines(b)
	an, bn := al, bl
	if o.ignoreWS || o.ignoreAll || o.ignoreCase {
		an, bn = make([]string, len(al)), make([]string, len(bl))
		for i, l := range al {
			an[i] = o.normalize(l)
		}
		for i, l := range bl {
			bn[i] = o.normalize(l)
		}
	}
	ops := myersDiff(an, bn)
	changed := false
	for _, op := range ops {
		if op.kind != ' ' {
			changed = true
			break
		}
	}
	if !changed {
		return 0
	}
	if o.brief {
		fmt.Fprintf(out, "Files %s and %s differ\n", leftName, rightName)
		return 1
	}
	if o.unified {
		l1, l2 := "--- "+leftName, "+++ "+rightName
		if len(o.labels) > 0 {
			l1 = "--- " + o.labels[0]
		}
		if len(o.labels) > 1 {
			l2 = "+++ " + o.labels[1]
		}
		out.WriteString(l1 + "\n" + l2 + "\n")
		writeUnifiedHunks(out, ops, al, bl, o.context)
		return 1
	}
	writeNormalDiff(out, ops, al, bl)
	return 1
}

func writeUnifiedHunks(out *strings.Builder, ops []diffOp, a, b []string, ctx int) {
	var changes []int
	for i, op := range ops {
		if op.kind != ' ' {
			changes = append(changes, i)
		}
	}
	for g := 0; g < len(changes); {
		h := g
		for h+1 < len(changes) && changes[h+1]-changes[h]-1 <= 2*ctx {
			h++
		}
		start, end := max(changes[g]-ctx, 0), min(changes[h]+ctx+1, len(ops))
		aLen, bLen := 0, 0
		var body strings.Builder
		for _, op := range ops[start:end] {
			switch op.kind {
			case ' ':
				body.WriteString(" " + a[op.a] + "\n")
				aLen++
				bLen++
			case '-':
				body.WriteString("-" + a[op.a] + "\n")
				aLen++
			case '+':
				body.WriteString("+" + b[op.b] + "\n")
				bLen++
			}
		}
		fmt.Fprintf(out, "@@ -%s +%s @@\n", hunkRange(ops[start].a, aLen), hunkRange(ops[start].b, bLen))
		out.WriteString(body.String())
		g = h + 1
	}
}

func hunkRange(start, n int) string {
	if n == 0 {
		return fmt.Sprintf("%d,0", start)
	}
	if n == 1 {
		return fmt.Sprintf("%d", start+1)
	}
	return fmt.Sprintf("%d,%d", start+1, n)
}

func writeNormalDiff(out *strings.Builder, ops []diffOp, a, b []string) {
	rangeStr := func(from, to int) string {
		if to-from <= 1 {
			return fmt.Sprintf("%d", from+1)
		}
		return fmt.Sprintf("%d,%d", from+1, to)
	}
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		j := i
		var dels, adds []diffOp
		for j < len(ops) && ops[j].kind != ' ' {
			if ops[j].kind == '-' {
				dels = append(dels, ops[j])
			} else {
				adds = append(adds, ops[j])
			}
			j++
		}
		aPos, bPos := ops[i].a, ops[i].b
		switch {
		case len(dels) > 0 && len(adds) > 0:
			fmt.Fprintf(out, "%sc%s\n", rangeStr(dels[0].a, dels[len(dels)-1].a+1), rangeStr(adds[0].b, adds[len(adds)-1].b+1))
		case len(dels) > 0:
			fmt.Fprintf(out, "%sd%d\n", rangeStr(dels[0].a, dels[len(dels)-1].a+1), bPos)
		default:
			fmt.Fprintf(out, "%da%s\n", aPos, rangeStr(adds[0].b, adds[len(adds)-1].b+1))
		}
		for _, d := range dels {
			out.WriteString("< " + a[d.a] + "\n")
		}
		if len(dels) > 0 && len(adds) > 0 {
			out.WriteString("---\n")
		}
		for _, ad := range adds {
			out.WriteString("> " + b[ad.b] + "\n")
		}
		i = j
	}
}

func cmdCmp(args []string, _ string) CmdResult {
	silent := false
	var files []string
	for _, a := range args {
		switch a {
		case "-s", "--silent", "--quiet":
			silent = true
		default:
			files = append(files, a)
		}
	}
	if len(files) != 2 {
		return CmdResult{Stdout: "", Stderr: "cmp: missing operand\n", ExitCode: 2}
	}
	a, err := readFileArg(files[0])
	if err != nil {
		return CmdResult{Stdout: "", Stderr: fmt.Sprintf("cmp: %s: %s\n", files[0], describeErrText(err)), ExitCode: 2}
	}
	b, err := readFileArg(files[1])
	if err != nil {
		return CmdResult{Stdout: "", Stderr: fmt.Sprintf("cmp: %s: %s\n", files[1], describeErrText(err)), ExitCode: 2}
	}
	if a == b {
		return CmdResult{}
	}
	if silent {
		return CmdResult{ExitCode: 1}
	}
	line := 1
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return CmdResult{Stdout: fmt.Sprintf("%s %s differ: byte %d, line %d\n", files[0], files[1], i+1, line), Stderr: "", ExitCode: 1}
		}
		if a[i] == '\n' {
			line++
		}
	}
	shorter := files[0]
	if len(b) < len(a) {
		shorter = files[1]
	}
	return CmdResult{Stdout: "", Stderr: fmt.Sprintf("cmp: EOF on %s\n", shorter), ExitCode: 1}
}
