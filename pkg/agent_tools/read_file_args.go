package tools

import (
	"fmt"
	"sort"
	"strings"
)

// read_file accepts exactly two canonical arguments: `path` and
// `view_range`. Models routinely invent other spellings for the line range
// (`start_line`/`end_line`, `offset`/`limit`, `line_start`/`line_end`) and
// then see the whole file come back, conclude the range was honored, and
// repeat the identical call. Rather than silently ignoring those keys, the
// documented aliases are translated into `view_range`; anything left over
// is a hard error that names the valid arguments so the model can correct
// itself in one retry.

// readFilePathAliases are accepted spellings of the `path` argument.
var readFilePathAliases = []string{"file_path"}

// readRangeStartAliases map a start-line argument spelling to the effective
// 1-based start line. Each entry is paired with an end alias below.
var readRangeStartAliases = []string{"start_line", "line_start", "offset"}

// readRangeEndAliases map an end-line argument spelling to the effective
// (inclusive) 1-based end line.
var readRangeEndAliases = []string{"end_line", "line_end", "limit"}

// readFileCanonicalArgs is the set of keys the handler understands, after
// alias translation. Used only to build the unknown-argument error.
var readFileCanonicalArgs = []string{"path", "view_range"}

// isKnownReadFileArg reports whether key (case-insensitively) is one of the
// canonical argument names or an accepted alias.
func isKnownReadFileArg(key string) bool {
	lower := strings.ToLower(key)
	for _, k := range readFileCanonicalArgs {
		if lower == k {
			return true
		}
	}
	for _, k := range readFilePathAliases {
		if lower == k {
			return true
		}
	}
	for _, k := range readRangeStartAliases {
		if lower == k {
			return true
		}
	}
	for _, k := range readRangeEndAliases {
		if lower == k {
			return true
		}
	}
	return false
}

// rejectUnknownReadFileArgs returns an error naming the valid arguments and
// the accepted aliases when args carries any key the handler cannot resolve.
// It is called before validation so the model learns the mistake immediately
// instead of receiving the file again.
func rejectUnknownReadFileArgs(args map[string]any) error {
	var unknown []string
	for k := range args {
		if !isKnownReadFileArg(k) {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf(
		"unknown argument(s) %s: read_file accepts 'path' and 'view_range' (aliases 'file_path'; 'start_line'/'end_line', 'line_start'/'line_end', 'offset'/'limit')",
		strings.Join(unknown, ", "),
	)
}

// effectiveReadRange resolves the line range for a read_file call, honouring
// the accepted aliases. Precedence: an explicit `view_range` always wins over
// the aliases; among aliases, a start alias pairs with an end alias, and
// `offset` (1-based start line) pairs with `limit` (line count, converted to
// an inclusive end line). Returns (0, 0) when no range was supplied.
func effectiveReadRange(args map[string]any) (int, int) {
	if vr, exists := lookupKey(args, "view_range"); exists && vr != nil {
		if arr, ok := vr.([]any); ok && len(arr) == 2 {
			return toIntArg(arr[0]), toIntArg(arr[1])
		}
	}

	start, haveStart := readAliasInt(args, readRangeStartAliases, true)
	end, haveEnd := readAliasInt(args, readRangeEndAliases, false)
	if !haveStart && !haveEnd {
		return 0, 0
	}
	// `offset`/`limit` is the only pair where the end alias is a count, not
	// an absolute line. Detect it so `offset=100, limit=50` means lines
	// 100-149 rather than 100-50.
	if off, ok := lookupKey(args, "offset"); ok && off != nil {
		if offStart, ok2 := toAliasInt(off); ok2 {
			if lim, ok3 := lookupKey(args, "limit"); ok3 && lim != nil {
				if limN, ok4 := toAliasInt(lim); ok4 {
					if limN > 0 {
						return offStart, offStart + limN - 1
					}
					return offStart, 0
				}
			}
			return offStart, 0
		}
	}
	if !haveStart {
		// A lone end alias still means a bounded read; start from line 1
		// rather than silently returning the whole file.
		return 1, end
	}
	if !haveEnd {
		return start, 0
	}
	return start, end
}

// readAliasInt returns the first alias in keys that resolves to an integer,
// applying a 1-based minimum of 1 for start aliases.
func readAliasInt(args map[string]any, keys []string, isStart bool) (int, bool) {
	for _, k := range keys {
		v, ok := lookupKey(args, k)
		if !ok || v == nil {
			continue
		}
		n, ok := toAliasInt(v)
		if !ok {
			continue
		}
		if isStart && n < 1 {
			n = 1
		}
		return n, true
	}
	return 0, false
}

// toAliasInt converts a JSON number or numeric string to an int.
func toAliasInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		trimmed := strings.TrimSpace(n)
		if trimmed == "" {
			return 0, false
		}
		parsed := 0
		if _, err := fmt.Sscanf(trimmed, "%d", &parsed); err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

// readRepeatNote is the short response returned in place of content when the
// same read_file call is repeated within one turn. It names the repeat and
// points at view_range so the model reads a different section instead of
// re-fetching what it already has.
func readRepeatNote(path string, startLine, endLine, count int) string {
	if startLine > 0 || endLine > 0 {
		a, b := startLine, endLine
		if b < a {
			b = a
		}
		return fmt.Sprintf(
			"read_file %s: identical to your last read (repeated %d times this turn) — use view_range [%d, %d] for lines %d-%d, or a different range to read a new section.",
			path, count, a, b, a, b,
		)
	}
	return fmt.Sprintf(
		"read_file %s: identical to your last read (repeated %d times this turn) — the whole file was already returned; use view_range [start, end] to read a specific section.",
		path, count,
	)
}
