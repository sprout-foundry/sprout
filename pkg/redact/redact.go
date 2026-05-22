// Package redact provides byte-oriented secret redaction for log files and
// other persisted artifacts. It is a thin façade over pkg/secretdetect that
// emits [REDACTED:<rule-id>] tokens so operators can tell what *kind* of
// secret was present in a log line without exposing its value.
package redact

import "github.com/sprout-foundry/sprout/pkg/secretdetect"

// Apply returns a copy of data with detected secrets replaced by
// [REDACTED:<rule-id>] tokens. The original slice is not modified.
func Apply(data []byte) []byte {
	if len(data) == 0 {
		out := make([]byte, len(data))
		copy(out, data)
		return out
	}
	return []byte(secretdetect.RedactTagged(string(data)))
}

// String is a convenience wrapper around Apply for string input.
func String(s string) string {
	return secretdetect.RedactTagged(s)
}
