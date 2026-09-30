package wasmshell

import (
	"fmt"
	"math"
	"math/rand"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func init() {
	CmdRegistry["awk"] = cmdAwk
	CmdRegistry["gawk"] = cmdAwk
	CmdRegistry["mawk"] = cmdAwk
}

const (
	kNum uint8 = iota
	kStr
	kStrNum
)

type aval struct {
	s    string
	n    float64
	kind uint8
}

func numVal(n float64) aval { return aval{n: n, kind: kNum} }
func strVal(s string) aval  { return aval{s: s, kind: kStr} }
func strnum(s string) aval {
	return aval{s: s, n: awkStrToNum(s), kind: kStrNum}
}

func awkStrToNum(s string) float64 {
	s = strings.TrimLeft(s, " \t\n")
	end := 0
	seenDigit, seenDot, seenExp := false, false, false
	for end < len(s) {
		c := s[end]
		switch {
		case isDigit(c):
			seenDigit = true
		case c == '.' && !seenDot && !seenExp:
			seenDot = true
		case (c == 'e' || c == 'E') && seenDigit && !seenExp:
			if end+1 < len(s) && (isDigit(s[end+1]) || ((s[end+1] == '+' || s[end+1] == '-') && end+2 < len(s) && isDigit(s[end+2]))) {
				seenExp = true
				end++
			} else {
				goto done
			}
		case (c == '+' || c == '-') && end == 0:
		default:
			goto done
		}
		end++
	}
done:
	n, _ := strconv.ParseFloat(s[:end], 64)
	return n
}

// looksNumeric reports whether a field value is a number (strnum rules).
func looksNumeric(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	_, err := strconv.ParseFloat(t, 64)
	return err == nil && !strings.ContainsAny(t, "xXnN")
}

type awkRun struct {
	prog     *awkProg
	globals  map[string]aval
	arrays   map[string]map[string]aval
	frames   []*awkFrame
	fields   []string
	fieldsOK bool
	record   string
	out      strings.Builder
	files    map[string]*strings.Builder
	fileOrd  []string
	regexes  map[string]*regexp.Regexp
	exiting  bool
	exitCode int
	rng      *rand.Rand
}

type awkFrame struct {
	vars   map[string]aval
	arrays map[string]map[string]aval
}

type awkSignal int

const (
	sigNone awkSignal = iota
	sigNext
	sigBreak
	sigContinue
	sigReturn
	sigExit
)

type awkRuntimeError struct{ msg string }

func (e awkRuntimeError) Error() string { return e.msg }

func cmdAwk(args []string, stdin string) CmdResult {
	var progSrc string
	haveProg := false
	fs := ""
	var assigns []string
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			i++
			goto operands
		case a == "-F" && i+1 < len(args):
			i++
			fs = args[i]
		case strings.HasPrefix(a, "-F"):
			fs = a[2:]
		case a == "-v" && i+1 < len(args):
			i++
			assigns = append(assigns, args[i])
		case strings.HasPrefix(a, "-v") && len(a) > 2:
			assigns = append(assigns, a[2:])
		case a == "-f" && i+1 < len(args):
			i++
			data, err := readFileArg(args[i])
			if err != nil {
				return CmdResult{Stdout: "", Stderr: fmt.Sprintf("awk: can't open file %s\n", args[i]), ExitCode: 2}
			}
			progSrc += data + "\n"
			haveProg = true
		case strings.HasPrefix(a, "-") && len(a) > 1:
			return CmdResult{Stdout: "", Stderr: "awk: unsupported option " + a + "\n", ExitCode: ExitCommandNotFound}
		default:
			goto operands
		}
	}
operands:
	operands := args[i:]
	if !haveProg {
		if len(operands) == 0 {
			return CmdResult{Stdout: "", Stderr: "usage: awk [-F fs][-v var=value][prog | -f progfile][file ...]\n", ExitCode: 2}
		}
		progSrc, operands = operands[0], operands[1:]
	}
	prog, err := parseAwk(progSrc)
	if err != nil {
		code := 2
		if strings.Contains(err.Error(), "not supported") {
			code = ExitCommandNotFound
		}
		return CmdResult{Stdout: "", Stderr: "awk: " + err.Error() + "\n", ExitCode: code}
	}
	r := &awkRun{
		prog:    prog,
		globals: map[string]aval{},
		arrays:  map[string]map[string]aval{},
		files:   map[string]*strings.Builder{},
		regexes: map[string]*regexp.Regexp{},
		rng:     rand.New(rand.NewSource(0)), //nolint:gosec // G404: awk's rand() is a seedable, reproducible generator by definition
	}
	r.globals["FS"] = strVal(" ")
	if fs != "" {
		if fs == "t" {
			fs = "\t"
		}
		r.globals["FS"] = strVal(unescapeC(fs, false))
	}
	for k, v := range map[string]string{"OFS": " ", "ORS": "\n", "RS": "\n", "SUBSEP": "\x1c", "CONVFMT": "%.6g", "OFMT": "%.6g"} {
		r.globals[k] = strVal(v)
	}
	r.globals["NR"], r.globals["FNR"], r.globals["NF"] = numVal(0), numVal(0), numVal(0)
	env := map[string]aval{}
	for k, v := range ShellEnv.All() {
		env[k] = strnum(v)
	}
	r.arrays["ENVIRON"] = env
	argv := map[string]aval{"0": strVal("awk")}
	for k, o := range operands {
		argv[strconv.Itoa(k+1)] = strnum(o)
	}
	r.arrays["ARGV"] = argv
	r.globals["ARGC"] = numVal(float64(len(operands) + 1))
	for _, a := range assigns {
		r.assignArg(a)
	}
	errMsg := r.execute(operands, stdin)
	code := r.exitCode
	if errMsg != "" {
		code = 2
		if strings.Contains(errMsg, "not supported") {
			code = ExitCommandNotFound
		}
	}
	for _, name := range r.fileOrd {
		if err := SyncWriteFile(ResolvePath(name), r.files[name].String()); err != nil {
			errMsg += fmt.Sprintf("awk: can't write %s: %s\n", name, err)
		}
	}
	return CmdResult{Stdout: r.out.String(), Stderr: errMsg, ExitCode: code}
}

func (r *awkRun) assignArg(a string) bool {
	name, val, ok := strings.Cut(a, "=")
	if !ok || !nameRe.MatchString(name) {
		return false
	}
	r.globals[name] = strnum(unescapeC(val, false))
	return true
}

func (r *awkRun) execute(operands []string, stdin string) (errMsg string) {
	defer func() {
		if rec := recover(); rec != nil {
			if e, ok := rec.(awkRuntimeError); ok {
				errMsg = "awk: " + e.msg + "\n"
				return
			}
			panic(rec)
		}
	}()
	for _, it := range r.prog.items {
		if it.kind == "BEGIN" && !r.exiting {
			r.runBlock(it.body)
		}
	}
	hasMain := false
	for _, it := range r.prog.items {
		if it.kind != "BEGIN" {
			hasMain = true
		}
	}
	if hasMain && !r.exiting {
		inputs := 0
		for _, o := range operands {
			if strings.Contains(o, "=") && r.assignArg(o) {
				continue
			}
			inputs++
			var data string
			if o == "-" {
				data = stdin
			} else {
				d, err := readFileArg(o)
				if err != nil {
					return fmt.Sprintf("awk: can't open file %s: %s\n", o, describeErrText(err))
				}
				data = d
			}
			r.globals["FILENAME"] = strVal(o)
			r.processInput(data)
			if r.exiting {
				break
			}
		}
		if inputs == 0 && !r.exiting {
			r.processInput(stdin)
		}
	}
	r.exiting = false
	for _, it := range r.prog.items {
		if it.kind == "END" && !r.exiting {
			r.runBlock(it.body)
		}
	}
	return ""
}

func (r *awkRun) records(data string) []string {
	rs := r.getStr("RS")
	if rs == "" {
		var recs []string
		for _, para := range regexp.MustCompile(`\n\n+`).Split(strings.Trim(data, "\n"), -1) {
			if para != "" {
				recs = append(recs, para)
			}
		}
		return recs
	}
	sep := rs
	if len(rs) == 1 {
		recs := strings.Split(data, rs)
		if len(recs) > 0 && recs[len(recs)-1] == "" {
			recs = recs[:len(recs)-1]
		}
		return recs
	}
	recs := r.regex(sep).Split(data, -1)
	if len(recs) > 0 && recs[len(recs)-1] == "" {
		recs = recs[:len(recs)-1]
	}
	return recs
}

func (r *awkRun) processInput(data string) {
	r.globals["FNR"] = numVal(0)
	for _, rec := range r.records(data) {
		r.globals["NR"] = numVal(r.getNum("NR") + 1)
		r.globals["FNR"] = numVal(r.getNum("FNR") + 1)
		r.setRecord(rec)
		for _, it := range r.prog.items {
			if it.kind != "main" {
				continue
			}
			if !r.patternMatches(it) {
				continue
			}
			var sig awkSignal
			if it.body == nil {
				r.emit(r.record+r.getStr("ORS"), nil, false)
			} else {
				sig = r.runBlock(it.body)
			}
			if sig == sigNext || r.exiting {
				break
			}
		}
		if r.exiting {
			return
		}
	}
}

func (r *awkRun) patternMatches(it *awkItem) bool {
	if it.pattern == nil {
		return true
	}
	if it.end == nil {
		return r.truthy(r.eval(it.pattern))
	}
	if it.inRange {
		if r.truthy(r.eval(it.end)) {
			it.inRange = false
		}
		return true
	}
	if r.truthy(r.eval(it.pattern)) {
		it.inRange = !r.truthy(r.eval(it.end))
		return true
	}
	return false
}

func (r *awkRun) setRecord(rec string) {
	r.record = rec
	r.fieldsOK = false
	r.splitFields()
}

func (r *awkRun) splitFields() {
	r.fields = r.splitWith(r.record, r.getStr("FS"))
	r.fieldsOK = true
	r.globals["NF"] = numVal(float64(len(r.fields)))
}

func (r *awkRun) splitWith(s, fs string) []string {
	switch {
	case fs == " ":
		return strings.Fields(s)
	case s == "":
		return nil
	case fs == "":
		var out []string
		for _, ch := range s {
			out = append(out, string(ch))
		}
		return out
	case len(fs) == 1 && fs != "\\":
		return strings.Split(s, fs)
	}
	return r.regex(fs).Split(s, -1)
}

func (r *awkRun) rebuildRecord() {
	r.record = strings.Join(r.fields, r.getStr("OFS"))
}

func (r *awkRun) regex(src string) *regexp.Regexp {
	if re, ok := r.regexes[src]; ok {
		return re
	}
	re, err := regexp.Compile(src)
	if err != nil {
		panic(awkRuntimeError{fmt.Sprintf("bad regex %q: %s", src, err)})
	}
	r.regexes[src] = re
	return re
}

func (r *awkRun) getStr(name string) string  { return r.toStr(r.globals[name]) }
func (r *awkRun) getNum(name string) float64 { return r.toNum(r.globals[name]) }

func (r *awkRun) toNum(v aval) float64 {
	if v.kind == kStr {
		return awkStrToNum(v.s)
	}
	return v.n
}

func (r *awkRun) toStr(v aval) string {
	if v.kind != kNum {
		return v.s
	}
	return r.numToStr(v.n, "CONVFMT")
}

func (r *awkRun) numToStr(n float64, fmtVar string) string {
	if n == math.Trunc(n) && math.Abs(n) < 1e16 {
		return strconv.FormatInt(int64(n), 10)
	}
	if math.IsNaN(n) {
		return "nan"
	}
	if math.IsInf(n, 0) {
		if n > 0 {
			return "inf"
		}
		return "-inf"
	}
	return fmt.Sprintf(r.toStrRaw(r.globals[fmtVar]), n)
}

func (r *awkRun) toStrRaw(v aval) string {
	if v.kind == kNum {
		return strconv.FormatFloat(v.n, 'g', -1, 64)
	}
	return v.s
}

func (r *awkRun) truthy(v aval) bool {
	switch v.kind {
	case kNum:
		return v.n != 0
	case kStr:
		return v.s != ""
	}
	if looksNumeric(v.s) {
		return v.n != 0
	}
	return v.s != ""
}

func (r *awkRun) emit(text string, dest awkExpr, appendMode bool) {
	if dest == nil {
		r.out.WriteString(text)
		return
	}
	name := r.toStr(r.eval(dest))
	switch name {
	case "/dev/stdout", "-":
		r.out.WriteString(text)
		return
	case "/dev/stderr":
		r.out.WriteString(text)
		return
	}
	b, ok := r.files[name]
	if !ok {
		b = &strings.Builder{}
		if appendMode {
			if existing, err := readFileArg(name); err == nil {
				b.WriteString(existing)
			}
		}
		r.files[name] = b
		r.fileOrd = append(r.fileOrd, name)
	}
	b.WriteString(text)
}

func (r *awkRun) sortedKeys(arr map[string]aval) []string {
	keys := make([]string, 0, len(arr))
	for k := range arr {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		x, errX := strconv.ParseFloat(keys[a], 64)
		y, errY := strconv.ParseFloat(keys[b], 64)
		if errX == nil && errY == nil {
			return x < y
		}
		return keys[a] < keys[b]
	})
	return keys
}
