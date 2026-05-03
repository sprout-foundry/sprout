package agent

import "testing"

func newTestResponseValidator() *ResponseValidator {
	return NewResponseValidator(&Agent{
		debug: false,
		state: NewAgentStateManager(false),
	})
}

func TestHasIncompletePatterns(t *testing.T) {
	rv := newTestResponseValidator()
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", false},
		{"ellipsis end", "This is incomplete...", true},
		{"ellipsis end with spaces", "This is incomplete...   ", true},
		{"no ellipsis", "This is a complete sentence.", false},
		{"ellipsis in middle", "Wait... let me think. Done.", false},
		{"just ellipsis", "...", true},
		{"multiple trailing dots", "text....", true}, // HasSuffix("text....", "...") is true (last 3 chars)
		{"newline then ellipsis", "text\n...", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rv.hasIncompletePatterns(tt.content)
			if got != tt.want {
				t.Errorf("hasIncompletePatterns(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestHasAbruptEnding(t *testing.T) {
	rv := newTestResponseValidator()
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"url ending", "Visit https://example.com", false},
		{"file path ending", "The file is at /usr/local/bin/foo", false},
		{"word ending no punctuation", "hello", false},
		{"letter ending with code block", "code here\n```\nprint(x)\n```", false},
		{"letter ending with http content", "see http://example.com for details", false},
		{"digit ending no code", "result is 42", false},
		{"digit ending with code block", "```\n42\n```", false},
		{"comma ending", "The result is,", true},
		{"hyphen ending", "Continued-", true},
		{"comma with spaces after", "Hello, world,   ", true},
		{"period ending", "This is complete.", false},
		{"question mark ending", "Is this done?", false},
		{"exclamation ending", "Wow!", false},
		{"colon ending", "Note:", false},
		{"semicolon ending", "First; second", false},
		{"paragraph ending period", "This is a paragraph.\nIt has two sentences.", false},
		{"paragraph ending comma", "This is a paragraph.\nIt has two sentences,", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rv.hasAbruptEnding(tt.content)
			if got != tt.want {
				t.Errorf("hasAbruptEnding(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestIsUnusuallyShort(t *testing.T) {
	rv := newTestResponseValidator()
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", true},
		{"one word", "hello", true},
		{"five words", "This is a short response here", true},
		{"exactly nine words", "one two three four five six seven eight nine", true},
		{"exactly ten words", "one two three four five six seven eight nine ten", false},
		{"done", "done", false},
		{"DONE uppercase", "DONE", false},
		{"completed task", "Task completed", false},
		{"finished already", "I'm finished", false},
		{"yes answer", "yes", false},
		{"no answer", "no", false},
		{"error message", "Error: not found", false},
		{"success message", "Success", false},
		{"failed message", "It failed", false},
		{"short phrase no completion", "the file", true},
		{"two words", "just words", true},
		{"nine words no completion", "This is a response that is really short here", true},
		{"long response", "This is a long enough response with more than ten words total", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rv.isUnusuallyShort(tt.content)
			if got != tt.want {
				t.Errorf("isUnusuallyShort(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestIsCompleteShortAnswer(t *testing.T) {
	rv := newTestResponseValidator()
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", false},
		{"done", "done", true},
		{"DONE", "DONE", true},
		{"Done!", "Done!", true},
		{"completed", "completed", true},
		{"Completed", "Completed", true},
		{"finished", "finished", true},
		{"Finished", "Finished", true},
		{"yes", "yes", true},
		{"Yes", "Yes", true},
		{"no", "no", true},
		{"No", "No", true},
		{"error:", "error:", true},
		{"Error:", "Error:", true},
		{"Error: something broke", "Error: something broke", true},
		{"success", "success", true},
		{"Success", "Success", true},
		{"failed", "failed", true},
		{"Failed", "Failed", true},
		{"normal sentence", "This is a normal sentence", true}, // "normal" contains "no"
		{"contains done but is question", "are you done?", true},
		{"does not contain any pattern", "the file was modified", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rv.isCompleteShortAnswer(tt.content)
			if got != tt.want {
				t.Errorf("isCompleteShortAnswer(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestLooksLikeTentativePostToolResponse(t *testing.T) {
	rv := newTestResponseValidator()
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"let me check", "Let me check", true},
		{"i'll investigate", "I'll investigate", true},
		{"i will look", "I will look at the file", true},
		{"i'm going to search", "I'm going to search for it", true},
		{"i need to verify", "I need to verify this", true},
		{"now i need to do X", "Now I need to do something", true},
		{"next i need to", "Next I need to run the tests", true},
		{"now i can proceed", "Now I can proceed", true},
		{"next i can do Y", "Next I can do something else", true},
		{"now i'll try", "Now I'll try again", true},
		{"next i'll check Z", "Next I'll check the result", true},
		{"next, i'll review", "Next, I'll review the code", true},
		{"first, i'll set up", "First, I'll set up the test", true},
		{"first i'll prepare", "First I'll prepare the data", true},
		{"let me investigate", "Let me investigate the issue", true},
		{"let me look into", "Let me look into this", true},
		{"let me verify", "Let me verify the results", true},
		{"let me inspect", "Let me inspect the file", true},
		{"i need to check", "I need to check the status", true},
		{"i'll check", "I'll check that for you", true},
		{"i'll inspect", "I'll inspect the logs", true},
		{"i'll verify", "I'll verify the output", true},
		{"i'll look into", "I'll look into this problem", true},
		{"the next step is", "The next step is to build", true},
		{"the next thing i need to do is", "The next thing I need to do is compile", true},
		{"good + now i need to", "Good, now I need to run the tests", true},
		{"okay + next i need to", "Okay, next I need to check the build", true},
		{"ok + now i'll", "Ok, now I'll look at the results", true},
		{"great + next i'll", "Great, next I'll verify the output", true},
		{"alright + the next step is", "Alright, the next step is to deploy", true},
		{"good alone", "Good, that's fine", false},
		{"okay alone", "Okay, I see", false},
		{"great alone", "Great job!", false},
		{"complete answer", "The file has been created successfully at the specified location.", false},
		{"summary", "Here is the summary of changes made to the codebase.", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rv.LooksLikeTentativePostToolResponse(tt.content)
			if got != tt.want {
				t.Errorf("LooksLikeTentativePostToolResponse(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestHasIncompleteCodeBlock(t *testing.T) {
	rv := newTestResponseValidator()
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", false},
		{"no code blocks", "Just text, no code", false},
		{"one code block (even)", "```code```", false},
		{"open code block (odd)", "```\ncode", true},
		{"closed code block", "```\ncode\n```", false},
		{"three markers (odd)", "```first```\n```second", true},
		{"three markers plain", "```\n```\n```", true},
		{"four markers (even)", "```\n```\n```\n```", false},
		{"one open one closed", "```\ncode\n```", false},
		{"backticks in text", "Use `code` inline", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rv.hasIncompleteCodeBlock(tt.content)
			if got != tt.want {
				t.Errorf("hasIncompleteCodeBlock(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestContainsAttemptedToolCalls(t *testing.T) {
	rv := newTestResponseValidator()
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", false},
		{"normal text", "This is a normal response", false},
		{"tool_calls key", `"tool_calls": [{"name": "read"}]`, true},
		{"function key", `"function": "execute"`, true},
		{"arguments key", `"arguments": {"file": "test"}`, true},
		{"tool_use key", `"tool_use": "read_file"`, true},
		{"function_calls key", `"function_calls": []`, true},
		{"xml tool_calls", "<tool_calls>read_file</tool_calls>", true},
		{"xml function_calls", "<function_calls>execute</function_calls>", true},
		{"TOOL_CALL bracket", "[TOOL_CALL] read_file", true},
		{"FUNCTION bracket", "[FUNCTION] execute", true},
		{"name json", `{"name": "read_file"}`, true},
		{"tool json", `{"tool": "execute"}`, true},
		{"function json", `{"function": "run"}`, true},
		{"function open tag", "<function=read>", true},
		{"function close tag", "</function>", true},
		{"I'll use the", "I'll use the read_file tool", true},
		{"I'll call the", "I'll call the execute function", true},
		{"Using the", "Using the shell_command tool", true},
		{"Calling the", "Calling the API endpoint", true},
		{"Let me use the", "Let me use the read_file tool", true},
		{"I need to use", "I need to use the shell_command", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rv.containsAttemptedToolCalls(tt.content)
			if got != tt.want {
				t.Errorf("containsAttemptedToolCalls(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestValidateToolCalls(t *testing.T) {
	rv := newTestResponseValidator()
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", true},
		{"normal text", "This is a normal response", true},
		{"contains tool_calls", `"tool_calls": []`, false},
		{"contains function_calls", "<function_calls>test</function_calls>", false},
		{"contains name json", `{"name": "test"}`, false},
		{"I'll use the", "I'll use the read_file tool", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rv.ValidateToolCalls(tt.content)
			if got != tt.want {
				t.Errorf("ValidateToolCalls(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestIsIncomplete(t *testing.T) {
	rv := newTestResponseValidator()
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", false},
		{"ellipsis end", "This is incomplete...", true},
		{"no punctuation ending", "hello", true},                            // triggers isUnusuallyShort
		{"comma ending", "The result is,", true},
		{"short phrase no completion patterns", "the file was", true},       // triggers isUnusuallyShort (3 words, no short-answer match)
		{"short but complete", "done", false},
		{"incomplete code block", "```\ncode", true},
		{"long complete sentence", "This is a fully formed complete response that explains everything in detail.", false},
		{"multiple checks fail", "hi", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rv.IsIncomplete(tt.content)
			if got != tt.want {
				t.Errorf("IsIncomplete(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}
