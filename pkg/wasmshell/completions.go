package wasmshell

import (
	"encoding/json"
	"sort"
	"strings"
)

// AutoCompleteResult holds the JSON response for tab completion.
type AutoCompleteResult struct {
	Completions []string `json:"completions"`
}

// AutoComplete performs tab completion for the given input.
// It handles:
// - Command name completion (first token)
// - File/directory path completion (subsequent tokens)
// - History search (for empty input or ! commands)
func AutoComplete(input string) AutoCompleteResult {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return AutoCompleteResult{Completions: []string{}}
	}

	tokens := Tokenize(trimmed, true)
	if len(tokens) == 0 {
		return AutoCompleteResult{Completions: []string{}}
	}

	var completions []string

	// If we have exactly one token and it's the command name (not a path),
	// complete against known commands.
	lastToken := tokens[len(tokens)-1]
	isFirstToken := len(tokens) == 1

	if isFirstToken && !strings.Contains(lastToken, "/") && !strings.Contains(lastToken, ".") {
		// Complete command names.
		for name := range CmdRegistry {
			if strings.HasPrefix(name, lastToken) {
				// For commands that don't take arguments (pwd, clear, history), no trailing space.
				switch name {
				case "pwd", "clear", "history":
					completions = append(completions, name)
				default:
					completions = append(completions, name+" ")
				}
			}
		}
	} else {
		// Complete file/directory paths.
		pathCompletions := GlobCompletion(lastToken)
		for _, p := range pathCompletions {
			// For directories, add trailing space so user can continue typing.
			if strings.HasSuffix(p, "/") {
				completions = append(completions, p)
			} else {
				completions = append(completions, p+" ")
			}
		}

		// If no path completions and input starts with !, do history search.
		if len(pathCompletions) == 0 && strings.HasPrefix(trimmed, "!") {
			prefix := strings.TrimPrefix(trimmed, "!")
			historyMatches := HistorySearch(prefix)
			for _, h := range historyMatches {
				completions = append(completions, h)
			}
		}
	}

	sort.Strings(completions)
	return AutoCompleteResult{Completions: completions}
}

// AutoCompleteJSON returns completions as a JSON string.
func AutoCompleteJSON(input string) string {
	result := AutoComplete(input)
	data, _ := json.Marshal(result)
	return string(data)
}
