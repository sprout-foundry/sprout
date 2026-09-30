package wasmshell

import (
	"crypto/md5" //nolint:gosec // G501: md5sum computes the digest the user asked for, not a security check
	crand "crypto/rand"
	"crypto/sha1" //nolint:gosec // G505: sha1sum computes the digest the user asked for, not a security check
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

func init() {
	for name, h := range map[string]func() hash.Hash{
		"md5sum": md5.New, "sha1sum": sha1.New, "sha256sum": sha256.New, "sha512sum": sha512.New, "sha224sum": sha256.New224, "sha384sum": sha512.New384,
	} {
		name, h := name, h
		CmdRegistry[name] = func(args []string, stdin string) CmdResult { return cmdHashSum(name, h, args, stdin) }
	}
	CmdRegistry["base64"] = cmdBase64
	CmdRegistry["stat"] = cmdStat
	CmdRegistry["du"] = cmdDu
	CmdRegistry["readlink"] = cmdReadlink
	CmdRegistry["mktemp"] = cmdMktemp
	CmdRegistry["file"] = cmdFile
}

func cmdHashSum(name string, newHash func() hash.Hash, args []string, stdin string) CmdResult {
	check := false
	var files []string
	for _, a := range args {
		switch {
		case a == "-c" || a == "--check":
			check = true
		case strings.HasPrefix(a, "-") && a != "-":
		default:
			files = append(files, a)
		}
	}
	sum := func(data string) string {
		h := newHash()
		h.Write([]byte(data))
		return hex.EncodeToString(h.Sum(nil))
	}
	if check {
		return hashCheck(name, sum, files, stdin)
	}
	if len(files) == 0 {
		files = []string{"-"}
	}
	var out, errs strings.Builder
	for _, f := range files {
		data := stdin
		if f != "-" {
			var err error
			if data, err = readFileArg(f); err != nil {
				fmt.Fprintf(&errs, "%s: %s: %s\n", name, f, describeErrText(err))
				continue
			}
		}
		fmt.Fprintf(&out, "%s  %s\n", sum(data), f)
	}
	if errs.Len() > 0 {
		return CmdResult{out.String(), errs.String(), 1}
	}
	return CmdResult{out.String(), "", 0}
}

func hashCheck(name string, sum func(string) string, files []string, stdin string) CmdResult {
	list := stdin
	if len(files) > 0 {
		data, err := readFileArg(files[0])
		if err != nil {
			return CmdResult{"", fmt.Sprintf("%s: %s: %s\n", name, files[0], describeErrText(err)), 1}
		}
		list = data
	}
	var out strings.Builder
	failed := 0
	for _, l := range splitLines(list) {
		want, file, ok := strings.Cut(l, "  ")
		if !ok {
			continue
		}
		data, err := readFileArg(file)
		if err != nil || sum(data) != want {
			fmt.Fprintf(&out, "%s: FAILED\n", file)
			failed++
			continue
		}
		fmt.Fprintf(&out, "%s: OK\n", file)
	}
	if failed > 0 {
		return CmdResult{out.String(), fmt.Sprintf("%s: WARNING: %d computed checksum did NOT match\n", name, failed), 1}
	}
	return CmdResult{out.String(), "", 0}
}

func cmdBase64(args []string, stdin string) CmdResult {
	decode := false
	wrap := 76
	var files []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-d" || a == "--decode" || a == "-D":
			decode = true
		case a == "-w" && i+1 < len(args):
			i++
			wrap, _ = strconv.Atoi(args[i])
		case strings.HasPrefix(a, "-w"):
			wrap, _ = strconv.Atoi(a[2:])
		case strings.HasPrefix(a, "--wrap="):
			wrap, _ = strconv.Atoi(strings.TrimPrefix(a, "--wrap="))
		case a == "-i" || a == "--ignore-garbage":
		default:
			files = append(files, a)
		}
	}
	input, errRes := readInputs("base64", files, stdin)
	if errRes != nil {
		errRes.ExitCode = 1
		return *errRes
	}
	if decode {
		clean := strings.Join(strings.Fields(input), "")
		data, err := base64.StdEncoding.DecodeString(clean)
		if err != nil {
			if data, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(clean, "=")); err != nil {
				return CmdResult{"", "base64: invalid input\n", 1}
			}
		}
		return CmdResult{string(data), "", 0}
	}
	enc := base64.StdEncoding.EncodeToString([]byte(input))
	if wrap > 0 {
		var b strings.Builder
		for len(enc) > wrap {
			b.WriteString(enc[:wrap] + "\n")
			enc = enc[wrap:]
		}
		b.WriteString(enc)
		enc = b.String()
	}
	if enc == "" {
		return CmdResult{}
	}
	return CmdResult{enc + "\n", "", 0}
}

func fileTypeName(info os.FileInfo) string {
	switch {
	case info.IsDir():
		return "directory"
	case info.Mode()&os.ModeSymlink != 0:
		return "symbolic link"
	case info.Size() == 0:
		return "regular empty file"
	}
	return "regular file"
}

func cmdStat(args []string, _ string) CmdResult {
	format := ""
	var files []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-c" || a == "--format" || a == "-f") && i+1 < len(args):
			i++
			format = args[i]
		case strings.HasPrefix(a, "--format="), strings.HasPrefix(a, "--printf="):
			_, format, _ = strings.Cut(a, "=")
		case a == "-L" || a == "--dereference" || a == "-t":
		default:
			files = append(files, a)
		}
	}
	if len(files) == 0 {
		return CmdResult{"", "stat: missing operand\n", 1}
	}
	var out, errs strings.Builder
	for _, f := range files {
		info, err := os.Lstat(ResolvePath(f)) //nolint:gosec // G703: shell commands act on the paths the user names
		if err != nil {
			fmt.Fprintf(&errs, "stat: cannot statx '%s': %s\n", f, describeErr(err))
			continue
		}
		if format != "" {
			out.WriteString(statFormat(format, f, info) + "\n")
			continue
		}
		fmt.Fprintf(&out, "  File: %s\n  Size: %d\t\t%s\nAccess: (%04o/%s)\nModify: %s\n",
			f, info.Size(), fileTypeName(info), info.Mode().Perm(), info.Mode().String(),
			info.ModTime().Format("2006-01-02 15:04:05.000000000 -0700"))
	}
	if errs.Len() > 0 {
		return CmdResult{out.String(), errs.String(), 1}
	}
	return CmdResult{out.String(), "", 0}
}

func statFormat(format, name string, info os.FileInfo) string {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		if format[i] != '%' || i+1 >= len(format) {
			b.WriteByte(format[i])
			continue
		}
		i++
		switch format[i] {
		case 'n':
			b.WriteString(name)
		case 's':
			b.WriteString(strconv.FormatInt(info.Size(), 10))
		case 'F':
			b.WriteString(fileTypeName(info))
		case 'a':
			fmt.Fprintf(&b, "%o", info.Mode().Perm())
		case 'A':
			b.WriteString(info.Mode().String())
		case 'y':
			b.WriteString(info.ModTime().Format("2006-01-02 15:04:05.000000000 -0700"))
		case 'Y':
			b.WriteString(strconv.FormatInt(info.ModTime().Unix(), 10))
		case 'U', 'G':
			b.WriteString(ShellEnv.Get("USER"))
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteByte(format[i])
		}
	}
	return b.String()
}

func cmdDu(args []string, _ string) CmdResult {
	summarize, human, all, total := false, false, false, false
	maxDepth := -1
	var files []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-d" && i+1 < len(args):
			i++
			maxDepth, _ = strconv.Atoi(args[i])
		case strings.HasPrefix(a, "--max-depth="):
			maxDepth, _ = strconv.Atoi(strings.TrimPrefix(a, "--max-depth="))
		case len(a) > 1 && a[0] == '-' && a[1] != '-':
			for _, c := range a[1:] {
				switch c {
				case 's':
					summarize = true
				case 'h':
					human = true
				case 'a':
					all = true
				case 'c':
					total = true
				}
			}
		case strings.HasPrefix(a, "--"):
		default:
			files = append(files, a)
		}
	}
	if len(files) == 0 {
		files = []string{"."}
	}
	if summarize {
		maxDepth = 0
	}
	size := func(n int64) string {
		if human {
			return humanizeSize(n)
		}
		return strconv.FormatInt((n+1023)/1024, 10)
	}
	var out strings.Builder
	var grand int64
	for _, f := range files {
		root := ResolvePath(f)
		rootInfo, err := os.Stat(root) //nolint:gosec // G703: shell commands act on the paths the user names
		if err != nil {
			return CmdResult{out.String(), fmt.Sprintf("du: cannot access '%s': %s\n", f, describeErr(err)), 1}
		}
		if !rootInfo.IsDir() {
			fmt.Fprintf(&out, "%s\t%s\n", size(rootInfo.Size()), f)
			grand += rootInfo.Size()
			continue
		}
		sizes := map[string]int64{}
		var order []string
		_ = WalkCompat(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				order = append(order, p)
				return nil
			}
			for dir := filepath.Dir(p); ; dir = filepath.Dir(dir) {
				sizes[dir] += info.Size()
				if dir == root || len(dir) <= len(root) {
					break
				}
			}
			if all && (maxDepth < 0 || findDepth(root, p) <= maxDepth) {
				fmt.Fprintf(&out, "%s\t%s\n", size(info.Size()), joinFindPath(f, relTo(root, p)))
			}
			return nil
		})
		for k := len(order) - 1; k >= 0; k-- {
			d := order[k]
			if maxDepth >= 0 && findDepth(root, d) > maxDepth {
				continue
			}
			fmt.Fprintf(&out, "%s\t%s\n", size(sizes[d]), joinFindPath(f, relTo(root, d)))
		}
		grand += sizes[root]
	}
	if total {
		fmt.Fprintf(&out, "%s\ttotal\n", size(grand))
	}
	return CmdResult{out.String(), "", 0}
}

func relTo(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return rel
}

func cmdReadlink(args []string, _ string) CmdResult {
	canonical := false
	var files []string
	for _, a := range args {
		switch {
		case a == "-f" || a == "-e" || a == "-m" || a == "--canonicalize":
			canonical = true
		case strings.HasPrefix(a, "-"):
		default:
			files = append(files, a)
		}
	}
	var out strings.Builder
	code := 0
	for _, f := range files {
		path := ResolvePath(f)
		if canonical {
			if resolved, err := filepath.EvalSymlinks(path); err == nil {
				path = resolved
			}
			out.WriteString(path + "\n")
			continue
		}
		target, err := os.Readlink(path)
		if err != nil {
			code = 1
			continue
		}
		out.WriteString(target + "\n")
	}
	return CmdResult{out.String(), "", code}
}

func cmdMktemp(args []string, _ string) CmdResult {
	dir := false
	parent := ""
	template := "tmp.XXXXXXXXXX"
	useTmp := true
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-d" || a == "--directory":
			dir = true
		case (a == "-p" || a == "--tmpdir") && i+1 < len(args):
			i++
			parent = args[i]
		case a == "-t" || a == "-q" || a == "-u":
		case strings.HasPrefix(a, "-"):
		default:
			template = a
			useTmp = !strings.Contains(a, "/") && parent == ""
		}
	}
	if parent == "" && useTmp {
		parent = os.Getenv("TMPDIR")
		if parent == "" {
			parent = "/tmp"
		}
	}
	if err := os.MkdirAll(ResolvePath(parent), 0o755); err != nil { //nolint:gosec // G703: shell commands act on the paths the user names
		return CmdResult{"", fmt.Sprintf("mktemp: %s: %s\n", parent, describeErr(err)), 1}
	}
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for attempt := 0; attempt < 100; attempt++ {
		name := []byte(template)
		random := make([]byte, len(name))
		_, _ = crand.Read(random)
		for k := len(name) - 1; k >= 0 && name[k] == 'X'; k-- {
			name[k] = letters[int(random[k])%len(letters)]
		}
		path := string(name)
		if parent != "" {
			path = filepath.Join(parent, path)
		}
		abs := ResolvePath(path)
		if _, err := os.Stat(abs); err == nil { //nolint:gosec // G703: shell commands act on the paths the user names
			continue
		}
		var err error
		if dir {
			err = os.MkdirAll(abs, 0o700) //nolint:gosec // G703: shell commands act on the paths the user names
		} else {
			err = SyncWriteFile(abs, "")
		}
		if err != nil {
			return CmdResult{"", fmt.Sprintf("mktemp: failed to create %s: %s\n", path, describeErr(err)), 1}
		}
		return CmdResult{path + "\n", "", 0}
	}
	return CmdResult{"", "mktemp: too many templates\n", 1}
}

func cmdFile(args []string, _ string) CmdResult {
	var out strings.Builder
	for _, f := range args {
		if strings.HasPrefix(f, "-") {
			continue
		}
		info, err := os.Stat(ResolvePath(f)) //nolint:gosec // G703: shell commands act on the paths the user names
		if err != nil {
			fmt.Fprintf(&out, "%s: cannot open `%s' (No such file or directory)\n", f, f)
			continue
		}
		kind := "directory"
		if !info.IsDir() {
			data, _ := readFileArg(f)
			switch {
			case len(data) == 0:
				kind = "empty"
			case hasBinary(data) || !utf8.ValidString(data):
				kind = "data"
			case strings.HasPrefix(data, "#!"):
				line, _, _ := strings.Cut(data, "\n")
				kind = strings.TrimSpace(line[2:]) + " script, ASCII text executable"
			case strings.HasPrefix(strings.TrimSpace(data), "{") || strings.HasPrefix(strings.TrimSpace(data), "["):
				kind = "JSON text data"
			default:
				kind = "ASCII text"
				if !isASCII(data) {
					kind = "Unicode text, UTF-8 text"
				}
			}
		}
		fmt.Fprintf(&out, "%s: %s\n", f, kind)
	}
	return CmdResult{out.String(), "", 0}
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
