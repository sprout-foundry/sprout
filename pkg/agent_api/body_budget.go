package api

// TrimMessagesForBodyBudget shrinks a conversation when a provider keeps
// rejecting a request with HTTP 413 (body too large) after every inline
// image has already been shed — i.e. the overflow is in the text body
// (system prompt + accumulated history), not the images.
//
// It keeps every leading system message (the system prompt carries the
// persona, project context and memory facts and must never be dropped) and
// the most recent keepTail messages, discarding the oldest non-system
// messages in between. The cut is rewound forward to a user-message
// boundary so an assistant tool_calls message and its tool results are
// never split across the boundary (a tool message whose assistant parent
// was dropped is invalid on the wire).
//
// The input slice is not modified. When there is nothing to drop (the
// non-system history already fits in keepTail) the input is returned
// unchanged, keeping the wire prefix — and the provider's prompt cache —
// stable. keepTail <= 0 keeps only the system messages.
func TrimMessagesForBodyBudget(messages []Message, keepTail int) []Message {
	start := 0
	for start < len(messages) && messages[start].Role == "system" {
		start++
	}
	rest := messages[start:]
	if keepTail < 0 {
		keepTail = 0
	}
	if len(rest) <= keepTail {
		return messages
	}

	cut := len(rest) - keepTail
	// Rewind forward to the next user message so the kept tail starts a
	// complete turn rather than mid tool-call sequence.
	for cut < len(rest) && rest[cut].Role != "user" {
		cut++
	}
	if cut >= len(rest) {
		// No user boundary in the tail window — fall back to the raw cut
		// rather than dropping everything.
		cut = len(rest) - keepTail
	}

	out := make([]Message, 0, start+(len(rest)-cut))
	out = append(out, messages[:start]...)
	out = append(out, rest[cut:]...)
	return out
}
