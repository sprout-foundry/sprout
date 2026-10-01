package api

import (
	"strings"
)

// CountImages returns the total number of inline image parts across
// messages.
func CountImages(messages []Message) int {
	n := 0
	for i := range messages {
		n += len(messages[i].Images)
	}
	return n
}

// imageWithholdNote is appended to a message whose image parts were
// withheld from a request by TrimImagesBeyondLatest.
const imageWithholdNote = "\n\n[image withheld from this request to stay within the provider's body limit — re-read the image file if its pixels are needed]"

// TrimImagesBeyondLatest keeps at most maxImages image parts across the
// message slice (most recent first) and withholds the older ones, appending
// a bracketed note to each affected message so the model knows pixels are
// withheld rather than absent from the conversation. The budget bounds the
// wire body size for providers with HTTP body limits (413): N images at the
// per-image inline cap plus prompt text must stay under a conservative
// server limit.
//
// The input slice and its messages are not modified. When the budget is not
// binding the input is returned unchanged — a stable wire prefix keeps the
// provider's prompt cache and sprout's token anchor valid. maxImages <= 0
// strips every image (StripImagesWithNote semantics).
func TrimImagesBeyondLatest(messages []Message, maxImages int) []Message {
	if maxImages <= 0 {
		return StripImagesWithNote(messages)
	}
	if CountImages(messages) <= maxImages {
		return messages
	}

	out := make([]Message, len(messages))
	copy(out, messages)

	budget := maxImages
	for i := len(out) - 1; i >= 0; i-- {
		if budget == 0 {
			// Budget already spent by newer messages: shed this one too.
			if len(out[i].Images) > 0 {
				out[i].Images = nil
				if !strings.Contains(out[i].Content, imageWithholdNote) {
					out[i].Content += imageWithholdNote
				}
			}
			continue
		}
		if len(out[i].Images) <= budget {
			budget -= len(out[i].Images)
			continue
		}
		out[i].Images = out[i].Images[len(out[i].Images)-budget:]
		if !strings.Contains(out[i].Content, imageWithholdNote) {
			out[i].Content += imageWithholdNote
		}
		budget = 0
	}
	return out
}
