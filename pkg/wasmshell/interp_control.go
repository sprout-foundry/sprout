package wasmshell

import (
	"fmt"
	"strings"
)

func (sh *interp) execIf(c *ifCmd, in *ioIn) CmdResult {
	var out CmdResult
	for i, cond := range c.conds {
		sh.condDepth++
		r := sh.execList(cond, in)
		sh.condDepth--
		out.add(r)
		if sh.flow != flowNone {
			return out
		}
		if r.ExitCode == 0 {
			out.add(sh.execList(c.bodies[i], in))
			return out
		}
	}
	out.ExitCode = 0
	if c.elseBody != nil {
		out.add(sh.execList(c.elseBody, in))
	}
	return out
}

// loopControl settles break/continue after a loop body; it reports
// whether the loop should stop.
func (sh *interp) loopControl() bool {
	switch sh.flow {
	case flowBreak:
		sh.flowN--
		if sh.flowN <= 0 {
			sh.flow = flowNone
		}
		return true
	case flowContinue:
		sh.flowN--
		if sh.flowN <= 0 {
			sh.flow = flowNone
			return false
		}
		return true
	case flowNone:
		return false
	}
	return true
}

func loopLimit(out CmdResult) CmdResult {
	out.writeErr(fmt.Sprintf("sh: loop stopped after %d iterations\n", maxLoopIterations))
	out.ExitCode = 1
	return out
}

func (sh *interp) execWhile(c *loopCmd, in *ioIn) CmdResult {
	var out CmdResult
	code := 0
	for n := 0; ; n++ {
		if n >= maxLoopIterations {
			return loopLimit(out)
		}
		sh.condDepth++
		r := sh.execList(c.cond, in)
		sh.condDepth--
		out.appendOutput(r)
		if sh.flow != flowNone {
			if sh.loopControl() {
				break
			}
			continue
		}
		if (r.ExitCode == 0) == c.until {
			break
		}
		b := sh.execList(c.body, in)
		out.appendOutput(b)
		code = b.ExitCode
		if sh.loopControl() {
			break
		}
	}
	out.ExitCode = code
	return out
}

func (sh *interp) execFor(c *forCmd, in *ioIn) CmdResult {
	if c.isArith {
		return sh.execForArith(c, in)
	}
	var items []string
	if c.hasIn {
		for _, w := range c.words {
			fs, err := sh.expandFields(w)
			if err != nil {
				return errResult(err)
			}
			items = append(items, fs...)
		}
	} else {
		items = append(items, sh.positional...)
	}
	var out CmdResult
	for _, it := range items {
		sh.setVar(c.varName, it)
		b := sh.execList(c.body, in)
		out.add(b)
		if sh.loopControl() {
			break
		}
	}
	return out
}

func (sh *interp) execForArith(c *forCmd, in *ioIn) CmdResult {
	var out CmdResult
	if strings.TrimSpace(c.arith[0]) != "" {
		if _, err := sh.arith(c.arith[0]); err != nil {
			return errResult(err)
		}
	}
	for n := 0; ; n++ {
		if n >= maxLoopIterations {
			return loopLimit(out)
		}
		if strings.TrimSpace(c.arith[1]) != "" {
			v, err := sh.arith(c.arith[1])
			if err != nil {
				out.add(errResult(err))
				return out
			}
			if v == 0 {
				break
			}
		}
		out.add(sh.execList(c.body, in))
		if sh.loopControl() {
			break
		}
		if strings.TrimSpace(c.arith[2]) != "" {
			if _, err := sh.arith(c.arith[2]); err != nil {
				out.add(errResult(err))
				return out
			}
		}
	}
	return out
}

func (sh *interp) execCase(c *caseCmd, in *ioIn) CmdResult {
	word, err := sh.expandString(c.word)
	if err != nil {
		return errResult(err)
	}
	for _, item := range c.items {
		for _, p := range item.patterns {
			re, err := sh.expandPattern(p)
			if err != nil {
				return errResult(err)
			}
			if re.MatchString(word) {
				return sh.execList(item.body, in)
			}
		}
	}
	return CmdResult{}
}
