package api

// streaming_sse.go — the SSE (Server-Sent Events) reader, split out of
// streaming.go. SSEReader reads an SSE byte stream line-by-line with
// per-line timeout handling and dispatches each complete event to the
// onEvent callback; sseLine is the reader's goroutine result type.
import (
	"bufio"
	"io"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// SSEReader reads Server-Sent Events from a reader
type SSEReader struct {
	reader  *bufio.Reader
	onEvent func(event, data string) error
}

// NewSSEReader creates a new SSE reader
func NewSSEReader(r io.Reader, onEvent func(event, data string) error) *SSEReader {
	return &SSEReader{
		reader:  bufio.NewReader(r),
		onEvent: onEvent,
	}
}

// Read processes the SSE stream
func (r *SSEReader) Read() error {
	return r.ReadWithTimeout(0) // Default: no timeout
}

// sseLine is a single line read result from the background reader goroutine.
type sseLine struct {
	line string
	err  error
}

// ReadWithTimeout processes the SSE stream with a timeout.
// Each line read is performed in a short-lived goroutine so that the
// select can respond to the timeout without blocking the main loop.
// The background goroutine will exit when the underlying HTTP response
// body is closed (which triggers ReadString to return an error).
func (r *SSEReader) ReadWithTimeout(timeout time.Duration) error {
	var event string
	var dataBuilder strings.Builder

	// Set up timeout handling if specified
	var timer *time.Timer
	var timerChan <-chan time.Time
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		timerChan = timer.C
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()
	}

	for {
		// Use a goroutine to read with timeout.
		// Note: if a timeout fires, the goroutine may block on ReadString
		// until the underlying connection is closed. This is acceptable
		// because SSE readers are always backed by HTTP response bodies
		// that will be closed by the HTTP client or server.
		readChan := make(chan sseLine, 1)

		go func() {
			line, err := r.reader.ReadString('\n')
			readChan <- sseLine{line: line, err: err}
		}()

		select {
		case result := <-readChan:
			if result.err != nil {
				if result.err == io.EOF {
					// Process any remaining data
					if dataBuilder.Len() > 0 && r.onEvent != nil {
						if err := r.onEvent(event, dataBuilder.String()); err != nil {
							return agenterrors.Wrap(err, "failed to process remaining data")
						}
					}
					return nil
				}
				return agenterrors.NewNetwork("failed to read stream", result.err)
			}

			line := strings.TrimSpace(result.line)

			// Empty line signals end of event
			if line == "" {
				if dataBuilder.Len() > 0 && r.onEvent != nil {
					if err := r.onEvent(event, dataBuilder.String()); err != nil {
						return agenterrors.Wrap(err, "processing SSE event")
					}
				}
				// Reset for next event
				event = ""
				dataBuilder.Reset()
				continue
			}

			// Parse field
			if strings.HasPrefix(line, "event:") {
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				data := strings.TrimPrefix(line, "data:")
				if dataBuilder.Len() > 0 {
					dataBuilder.WriteString("\n")
				}
				dataBuilder.WriteString(strings.TrimSpace(data))
			}
			// Ignore other fields like id:, retry:

		case <-timerChan:
			return agenterrors.NewTimeout("SSE stream", timeout)
		}
	}
}
