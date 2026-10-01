package wasmshell

import (
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"unicode/utf8"
)

func (r *awkRun) call(c *aCall) aval {
	if f, ok := r.prog.funcs[c.name]; ok {
		return r.callUser(f, c.args)
	}
	arg := func(k int) aval {
		if k < len(c.args) {
			return r.eval(c.args[k])
		}
		return aval{kind: kStrNum}
	}
	s := func(k int) string { return r.toStr(arg(k)) }
	n := func(k int) float64 { return r.toNum(arg(k)) }
	switch c.name {
	case "length":
		if len(c.args) == 0 {
			return numVal(float64(utf8.RuneCountInString(r.record)))
		}
		if v, ok := c.args[0].(*aVar); ok {
			if a, isArr := r.arrays[v.name]; isArr {
				return numVal(float64(len(a)))
			}
			if f := r.frame(); f != nil {
				if a, isArr := f.arrays[v.name]; isArr {
					return numVal(float64(len(a)))
				}
			}
		}
		return numVal(float64(utf8.RuneCountInString(s(0))))
	case "substr":
		rs := []rune(s(0))
		start := int(math.Round(n(1)))
		end := len(rs) + 1
		if len(c.args) > 2 {
			end = start + int(math.Round(n(2)))
		}
		start = max(start, 1)
		end = min(end, len(rs)+1)
		if end <= start {
			return strVal("")
		}
		return strVal(string(rs[start-1 : end-1]))
	case "index":
		i := strings.Index(s(0), s(1))
		if i < 0 {
			return numVal(0)
		}
		return numVal(float64(utf8.RuneCountInString(s(0)[:i]) + 1))
	case "split":
		v, ok := c.args[1].(*aVar)
		if !ok {
			panic(awkRuntimeError{"split: second argument must be an array"})
		}
		fs := r.getStr("FS")
		if len(c.args) > 2 {
			if re, isRe := c.args[2].(*aRegex); isRe {
				fs = re.re.String()
			} else {
				fs = s(2)
			}
		}
		parts := r.splitWith(s(0), fs)
		arr := r.array(v.name)
		for k := range arr {
			delete(arr, k)
		}
		for k, p := range parts {
			arr[strconv.Itoa(k+1)] = strnum(p)
		}
		return numVal(float64(len(parts)))
	case "sub", "gsub":
		return r.substitute(c, c.name == "gsub")
	case "match":
		var loc []int
		if re, ok := c.args[1].(*aRegex); ok {
			loc = re.re.FindStringIndex(s(0))
		} else {
			loc = r.regex(s(1)).FindStringIndex(s(0))
		}
		if loc == nil {
			r.globals["RSTART"], r.globals["RLENGTH"] = numVal(0), numVal(-1)
			return numVal(0)
		}
		str := s(0)
		start := utf8.RuneCountInString(str[:loc[0]]) + 1
		r.globals["RSTART"] = numVal(float64(start))
		r.globals["RLENGTH"] = numVal(float64(utf8.RuneCountInString(str[loc[0]:loc[1]])))
		return numVal(float64(start))
	case "sprintf":
		vals := make([]aval, 0, len(c.args))
		for k := 1; k < len(c.args); k++ {
			vals = append(vals, arg(k))
		}
		return strVal(r.sprintf(s(0), vals))
	case "tolower":
		return strVal(strings.ToLower(s(0)))
	case "toupper":
		return strVal(strings.ToUpper(s(0)))
	case "int":
		return numVal(math.Trunc(n(0)))
	case "sqrt":
		return numVal(math.Sqrt(n(0)))
	case "exp":
		return numVal(math.Exp(n(0)))
	case "log":
		return numVal(math.Log(n(0)))
	case "sin":
		return numVal(math.Sin(n(0)))
	case "cos":
		return numVal(math.Cos(n(0)))
	case "atan2":
		return numVal(math.Atan2(n(0), n(1)))
	case "rand":
		return numVal(r.rng.Float64())
	case "srand":
		r.rng = rand.New(rand.NewSource(int64(n(0)))) //nolint:gosec // G404: awk's rand() is a seedable, reproducible generator by definition
		return numVal(0)
	case "close", "fflush":
		return numVal(0)
	case "system":
		panic(awkRuntimeError{"system() is not supported by the in-browser shell"})
	}
	panic(awkRuntimeError{"calling undefined function " + c.name})
}

func (r *awkRun) callUser(f *awkFunc, args []awkExpr) aval {
	if len(r.frames) > 200 {
		panic(awkRuntimeError{"function call nesting too deep"})
	}
	fr := &awkFrame{vars: map[string]aval{}, arrays: map[string]map[string]aval{}}
	for k, p := range f.params {
		if k < len(args) {
			if v, ok := args[k].(*aVar); ok {
				if a, isArr := r.arrays[v.name]; isArr {
					fr.arrays[p] = a
					continue
				}
				if cur := r.frame(); cur != nil {
					if a, isArr := cur.arrays[v.name]; isArr {
						fr.arrays[p] = a
						continue
					}
				}
			}
			fr.vars[p] = r.eval(args[k])
		} else {
			fr.vars[p] = aval{kind: kStrNum}
		}
	}
	r.frames = append(r.frames, fr)
	sig := r.runBlock(f.body)
	r.frames = r.frames[:len(r.frames)-1]
	if sig == sigExit {
		r.exiting = true
	}
	if v, ok := fr.vars["\x00ret"]; ok {
		return v
	}
	return aval{kind: kStrNum}
}

func (r *awkRun) substitute(c *aCall, global bool) aval {
	if len(c.args) < 2 {
		panic(awkRuntimeError{c.name + ": not enough arguments"})
	}
	var target awkExpr = &aField{idx: &aNum{v: 0}}
	if len(c.args) > 2 {
		target = c.args[2]
	}
	var re = r.regexFor(c.args[0])
	repl := r.toStr(r.eval(c.args[1]))
	src := r.toStr(r.eval(target))
	count := 0
	var b strings.Builder
	last := 0
	for _, loc := range re.FindAllStringIndex(src, -1) {
		if !global && count == 1 {
			break
		}
		count++
		b.WriteString(src[last:loc[0]])
		m := src[loc[0]:loc[1]]
		for i := 0; i < len(repl); i++ {
			switch {
			case repl[i] == '\\' && i+1 < len(repl) && (repl[i+1] == '&' || repl[i+1] == '\\'):
				i++
				b.WriteByte(repl[i])
			case repl[i] == '&':
				b.WriteString(m)
			default:
				b.WriteByte(repl[i])
			}
		}
		last = loc[1]
	}
	if count > 0 {
		b.WriteString(src[last:])
		r.assign(target, strVal(b.String()))
	}
	return numVal(float64(count))
}

func (r *awkRun) regexFor(x awkExpr) interface {
	FindAllStringIndex(string, int) [][]int
} {
	if re, ok := x.(*aRegex); ok {
		return re.re
	}
	return r.regex(r.toStr(r.eval(x)))
}

// sprintf formats like awk's printf: %c takes a number or a string, and
// integer verbs truncate floating values.
func (r *awkRun) sprintf(format string, vals []aval) string {
	var b strings.Builder
	next := 0
	take := func() aval {
		if next < len(vals) {
			next++
			return vals[next-1]
		}
		return aval{kind: kStrNum}
	}
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			b.WriteByte(format[i])
			continue
		}
		if i+1 < len(format) && format[i+1] == '%' {
			b.WriteByte('%')
			i++
			continue
		}
		j := i + 1
		for j < len(format) && strings.IndexByte("-+ #0123456789.*", format[j]) >= 0 {
			j++
		}
		if j >= len(format) {
			b.WriteString(format[i:])
			break
		}
		spec := format[i+1 : j]
		for strings.Contains(spec, "*") {
			spec = strings.Replace(spec, "*", strconv.Itoa(int(r.toNum(take()))), 1)
		}
		verb := format[j]
		switch verb {
		case 'd', 'i':
			fmt.Fprintf(&b, "%"+spec+"d", int64(r.toNum(take())))
		case 'o', 'x', 'X':
			fmt.Fprintf(&b, "%"+spec+string(verb), int64(r.toNum(take())))
		case 'u':
			fmt.Fprintf(&b, "%"+spec+"d", uint64(r.toNum(take())))
		case 'e', 'E', 'f', 'F', 'g', 'G':
			fmt.Fprintf(&b, "%"+spec+string(verb), r.toNum(take()))
		case 'c':
			v := take()
			if v.kind == kNum {
				fmt.Fprintf(&b, "%"+spec+"c", rune(v.n))
			} else if s := r.toStr(v); s != "" {
				ch, _ := utf8.DecodeRuneInString(s)
				fmt.Fprintf(&b, "%"+spec+"c", ch)
			}
		case 's':
			fmt.Fprintf(&b, "%"+spec+"s", r.toStr(take()))
		default:
			b.WriteString(format[i : j+1])
		}
		i = j
	}
	return b.String()
}
