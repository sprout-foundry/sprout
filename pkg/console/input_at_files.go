package console

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// maxAtFileCandidates caps one directory listing in the @file dropdown.
const maxAtFileCandidates = 50

// atFileToken returns the "@path" word the line ends with, if any. Only an
// @ that starts a word counts, so an email address is left alone.
func atFileToken(line string) (string, bool) {
	start := strings.LastIndexFunc(line, unicode.IsSpace) + 1
	token := line[start:]
	return token, strings.HasPrefix(token, "@")
}

// atFileCandidates lists the entries of the directory the trailing @path
// points into whose names start with what has been typed. Each candidate's
// Text is the whole line with the token completed; directories end in "/"
// so accepting one lists its contents next. A token that already names a
// file exactly has nothing left to complete.
func atFileCandidates(line string) []CompletionCandidate {
	token, ok := atFileToken(line)
	if !ok {
		return nil
	}
	partial := token[1:]
	dir, base := filepath.Split(partial)
	if base != "" && !strings.HasSuffix(partial, "/") {
		if info, err := os.Stat(partial); err == nil && !info.IsDir() {
			return nil
		}
	}
	listDir := dir
	if listDir == "" {
		listDir = "."
	}
	entries, err := os.ReadDir(listDir)
	if err != nil {
		return nil
	}
	head := line[:len(line)-len(token)]
	lowerBase := strings.ToLower(base)
	var out []CompletionCandidate
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), lowerBase) {
			continue
		}
		path := dir + name
		if e.IsDir() {
			path += "/"
		}
		out = append(out, CompletionCandidate{Text: head + "@" + path, Display: "@" + path})
	}
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := strings.HasSuffix(out[i].Display, "/"), strings.HasSuffix(out[j].Display, "/")
		if di != dj {
			return di
		}
		return out[i].Display < out[j].Display
	})
	if len(out) > maxAtFileCandidates {
		out = out[:maxAtFileCandidates]
	}
	return out
}
