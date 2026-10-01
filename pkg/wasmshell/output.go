package wasmshell

// OutSeg is a run of output written to one stream.
type OutSeg struct {
	Err  bool   `json:"err,omitempty"`
	Text string `json:"text"`
}

// segs returns the result's output in the order it was written.
func (r CmdResult) segs() []OutSeg {
	if r.Output != nil {
		return r.Output
	}
	var s []OutSeg
	if r.Stdout != "" {
		s = append(s, OutSeg{Text: r.Stdout})
	}
	if r.Stderr != "" {
		s = append(s, OutSeg{Err: true, Text: r.Stderr})
	}
	return s
}

func (r *CmdResult) write(isErr bool, text string) {
	if text == "" {
		return
	}
	if r.Output == nil {
		r.Output = r.segs()
	}
	r.Output = append(r.Output, OutSeg{Err: isErr, Text: text})
	if isErr {
		r.Stderr += text
	} else {
		r.Stdout += text
	}
}

func (r *CmdResult) writeOut(text string) { r.write(false, text) }

func (r *CmdResult) writeErr(text string) { r.write(true, text) }

// appendOutput follows r's output with o's, in o's order.
func (r *CmdResult) appendOutput(o CmdResult) {
	for _, s := range o.segs() {
		r.write(s.Err, s.Text)
	}
}

// prependErr puts text on stderr ahead of everything r wrote.
func (r *CmdResult) prependErr(text string) {
	if text == "" {
		return
	}
	r.Output = append([]OutSeg{{Err: true, Text: text}}, r.segs()...)
	r.Stderr = text + r.Stderr
}

// ordered returns r with Output always set and adjacent runs on the same
// stream joined, for callers that print the transcript.
func (r CmdResult) ordered() CmdResult {
	var out []OutSeg
	for _, s := range r.segs() {
		if n := len(out); n > 0 && out[n-1].Err == s.Err {
			out[n-1].Text += s.Text
			continue
		}
		out = append(out, s)
	}
	if out == nil {
		out = []OutSeg{}
	}
	r.Output = out
	return r
}
