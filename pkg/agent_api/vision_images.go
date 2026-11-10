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
// The input slice and its messages are not modified (kept image parts are
// copied, never aliased). When the budget is not binding the input is
// returned unchanged — a stable wire prefix keeps the provider's prompt
// cache and sprout's token anchor valid. maxImages <= 0 strips every image.
func TrimImagesBeyondLatest(messages []Message, maxImages int) []Message {
	budget := maxImages
	if budget < 0 {
		budget = 0
	}
	if CountImages(messages) <= budget {
		return messages
	}

	out := make([]Message, len(messages))
	copy(out, messages)

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
		// Copy the kept tail so the result never aliases the input's
		// backing array (a later append must not write into it).
		keep := make([]ImageData, budget)
		copy(keep, out[i].Images[len(out[i].Images)-budget:])
		out[i].Images = keep
		if !strings.Contains(out[i].Content, imageWithholdNote) {
			out[i].Content += imageWithholdNote
		}
		budget = 0
	}
	return out
}

// MessageBodyImageBytes is the request-body cost of one message's image
// parts, in base64 payload bytes — what actually crosses the wire (the
// per-image inline cap keeps each part's base64 within a known size;
// URL-carrying parts cost nothing on the body budget, the URL is short
// and the fetch is client-side). A bytes estimate, not a part count: the
// same three-image turn can be 600KB (screenshots) or 8MB (full-res
// attachments), and a count cap can only guess at the mass.
func MessageBodyImageBytes(m Message) int {
	n := 0
	for i := range m.Images {
		n += imagePartBodyBytes(m.Images[i])
	}
	return n
}

// imagePartBodyBytes is the body cost of a single image part: its base64
// payload (a URL part costs nothing here — the fetch is client-side).
func imagePartBodyBytes(part ImageData) int {
	return len(part.Base64)
}

// MessagesBodyImageBytes is the request-body cost of every image part in
// the message slice (base64 payload bytes).
func MessagesBodyImageBytes(messages []Message) int {
	total := 0
	for i := range messages {
		total += MessageBodyImageBytes(messages[i])
	}
	return total
}

// TrimImageBudget keeps the newest image parts whose combined base64
// payload fits maxImageBytes, most-recent first, and withholds the older
// ones with a bracketed note so the model knows pixels are withheld rather
// than absent. The budget is in wire BODY bytes, not a part count: a
// vision model with a large but sub-limit image set keeps every image
// (the count cap of one generation shed the most recent attachment to
// make room for long history — the failure this budget replaces), while a
// bulk-screenshot turn still sheds old images before it can blow the
// provider's body limit (413).
//
// The input slice and its messages are not modified (kept image parts are
// copied, never aliased). When the budget is not binding the input is
// returned unchanged — a stable wire prefix keeps the provider's prompt
// cache and sprout's token anchor valid. maxImageBytes <= 0 strips every
// image.
func TrimImageBudget(messages []Message, maxImageBytes int) []Message {
	budget := maxImageBytes
	if budget < 0 {
		budget = 0
	}
	if MessagesBodyImageBytes(messages) <= budget {
		return messages
	}

	out := make([]Message, len(messages))
	copy(out, messages)

	for i := len(out) - 1; i >= 0; i-- {
		msgBytes := MessageBodyImageBytes(out[i])
		switch {
		case budget <= 0:
			// Budget spent by newer messages: shed this one too.
			if msgBytes > 0 {
				out[i].Images = nil
				if !strings.Contains(out[i].Content, imageWithholdNote) {
					out[i].Content += imageWithholdNote
				}
			}
		case msgBytes <= budget:
			budget -= msgBytes
		default:
			// The message overflows the remaining budget: keep the newest
			// parts that still fit. Parts are shed oldest-first within the
			// message, so the latest pixels (usually the current turn's
			// attached image) survive.
			kept := make([]ImageData, 0, len(out[i].Images))
			for j := len(out[i].Images) - 1; j >= 0; j-- {
				part := imagePartBodyBytes(out[i].Images[j])
				if part > budget {
					continue
				}
				budget -= part
				kept = append(kept, out[i].Images[j])
			}
			if len(kept) < len(out[i].Images) {
				reversed := make([]ImageData, len(kept))
				for k := range kept {
					reversed[k] = kept[len(kept)-1-k]
				}
				out[i].Images = reversed
				if !strings.Contains(out[i].Content, imageWithholdNote) {
					out[i].Content += imageWithholdNote
				}
			}
		}
	}
	return out
}
