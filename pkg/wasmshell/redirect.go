package wasmshell

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

type sinkKind int

const (
	sinkOut sinkKind = iota
	sinkErr
	sinkFile
	sinkNull
)

type sink struct {
	kind sinkKind
	path string
}

// withRedirs applies a command's redirections around fn: input is
// replaced before it runs, and its stdout/stderr are routed afterwards.
// Output files are created (or truncated) when opened, so a command
// writing nothing still leaves an empty file, as in a real shell.
func (sh *interp) withRedirs(rs []*redir, in *ioIn, fn func(*ioIn) CmdResult) CmdResult {
	if len(rs) == 0 {
		return fn(in)
	}
	routes := map[int]sink{1: {kind: sinkOut}, 2: {kind: sinkErr}}
	for _, r := range rs {
		newIn, err := sh.openRedir(r, routes)
		if err != nil {
			return CmdResult{"", "sh: " + err.Error() + "\n", 1}
		}
		if newIn != nil {
			in = newIn
		}
	}
	res := fn(in)
	out := CmdResult{ExitCode: res.ExitCode}
	write := func(s sink, text string) {
		switch s.kind {
		case sinkOut:
			out.Stdout += text
		case sinkErr:
			out.Stderr += text
		case sinkFile:
			if err := appendToFile(s.path, text); err != nil {
				out.Stderr += fmt.Sprintf("sh: %s: %s\n", s.path, err)
			}
		}
	}
	write(routes[1], res.Stdout)
	write(routes[2], res.Stderr)
	return out
}

func appendToFile(path, text string) error {
	if text == "" {
		return nil
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return SyncWriteFile(path, string(existing)+text)
}

func (sh *interp) openRedir(r *redir, routes map[int]sink) (*ioIn, error) {
	fd := r.fd
	switch r.op {
	case "<<", "<<-":
		body := r.body
		if r.expand {
			var err error
			if body, err = sh.expandMode(body, modeHeredoc); err != nil {
				return nil, err
			}
		}
		return &ioIn{data: body}, nil
	}

	target, err := sh.expandString(r.target)
	if err != nil {
		return nil, err
	}
	switch r.op {
	case "<<<":
		return &ioIn{data: target + "\n"}, nil
	case "<", "<>":
		if fd > 0 {
			return nil, nil
		}
		switch target {
		case "/dev/null":
			return &ioIn{}, nil
		case "/dev/stdin":
			return nil, nil
		}
		data, err := os.ReadFile(ResolvePath(target)) //nolint:gosec // G703: shell commands act on the paths the user names
		if err != nil {
			return nil, fmt.Errorf("%s: %s", target, describeErr(err))
		}
		return &ioIn{data: string(data)}, nil
	case "<&":
		return nil, nil
	case ">", ">|", ">>":
		if fd < 0 {
			fd = 1
		}
		s, err := openSink(target, r.op == ">>", routes)
		if err != nil {
			return nil, err
		}
		routes[fd] = s
	case "&>", "&>>":
		s, err := openSink(target, r.op == "&>>", routes)
		if err != nil {
			return nil, err
		}
		routes[1], routes[2] = s, s
	case ">&":
		if fd < 0 {
			fd = 1
		}
		if target == "-" {
			routes[fd] = sink{kind: sinkNull}
			return nil, nil
		}
		if n, convErr := strconv.Atoi(target); convErr == nil {
			s, ok := routes[n]
			if !ok {
				return nil, fmt.Errorf("%d: bad file descriptor", n)
			}
			routes[fd] = s
			return nil, nil
		}
		s, err := openSink(target, false, routes)
		if err != nil {
			return nil, err
		}
		routes[1], routes[2] = s, s
	}
	return nil, nil
}

func openSink(target string, appendMode bool, routes map[int]sink) (sink, error) {
	switch target {
	case "/dev/null":
		return sink{kind: sinkNull}, nil
	case "/dev/stdout":
		return routes[1], nil
	case "/dev/stderr":
		return routes[2], nil
	}
	path := ResolvePath(target)
	if info, err := os.Stat(path); err == nil && info.IsDir() { //nolint:gosec // G703: shell commands act on the paths the user names
		return sink{}, fmt.Errorf("%s: Is a directory", target)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil { //nolint:gosec // G703: shell commands act on the paths the user names
		return sink{}, fmt.Errorf("%s: No such file or directory", target)
	}
	if !appendMode {
		if err := SyncWriteFile(path, ""); err != nil {
			return sink{}, fmt.Errorf("%s: %s", target, describeErr(err))
		}
	} else if _, err := os.Stat(path); os.IsNotExist(err) { //nolint:gosec // G703: shell commands act on the paths the user names
		if err := SyncWriteFile(path, ""); err != nil {
			return sink{}, fmt.Errorf("%s: %s", target, describeErr(err))
		}
	}
	return sink{kind: sinkFile, path: path}, nil
}

// describeErr renders a filesystem error the way coreutils phrase it.
func describeErr(err error) string {
	switch {
	case os.IsNotExist(err):
		return "No such file or directory"
	case os.IsPermission(err):
		return "Permission denied"
	case os.IsExist(err):
		return "File exists"
	}
	if pe, ok := err.(*os.PathError); ok {
		return pe.Err.Error()
	}
	return err.Error()
}
