package commands

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PathCompleter returns file/directory path completions matching the given
// prefix. Directories are returned with a trailing "/" so the user can
// continue pressing Tab to drill deeper. Hidden files (dotfiles) are
// excluded unless the prefix starts with ".".
//
// Uses os.ReadDir for fast directory listing without stat'ing every entry.
// Returns absolute paths for absolute prefixes, relative paths otherwise.
// Sorted alphabetically.
func PathCompleter(prefix string) []string {
	if prefix == "" {
		prefix = "."
	}

	dir := filepath.Dir(prefix)
	base := filepath.Base(prefix)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var matches []string
	for _, e := range entries {
		name := e.Name()
		// Skip hidden files unless prefix starts with "."
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), strings.ToLower(base)) {
			continue
		}
		match := filepath.Join(dir, name)
		if e.IsDir() {
			match += "/"
		}
		matches = append(matches, match)
	}

	sort.Strings(matches)
	return matches
}
