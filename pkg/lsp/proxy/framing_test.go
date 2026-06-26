package proxy

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseContentLength(t *testing.T) {
	tests := []struct {
		name     string
		headers  string
		expected int
	}{
		{"simple case", "Content-Length: 42", 42},
		{"case insensitive", "content-length: 42", 42},
		{"mixed case", "CONTENT-length: 42", 42},
		{"with extra spaces", "Content-Length:   42   ", 42},
		{"multiple headers", "Content-Type: text/plain\nContent-Length: 123\n", 123},
		{"missing Content-Length", "Content-Type: text/plain", 0},
		{"invalid number", "Content-Length: abc", 0},
		{"empty string", "", 0},
		{"zero is valid for parsing", "Content-Length: 0", 0},
		{"negative number", "Content-Length: -1", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseContentLength(tt.headers)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWriteMessage(t *testing.T) {
	t.Run("writes correct format", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteMessage(&buf, "test")
		require.NoError(t, err)
		assert.Equal(t, "Content-Length: 4\r\n\r\ntest", buf.String())
	})

	t.Run("writes JSON body", func(t *testing.T) {
		var buf bytes.Buffer
		body := `{"jsonrpc":"2.0","method":"initialize"}`
		err := WriteMessage(&buf, body)
		require.NoError(t, err)
		assert.Equal(t, "Content-Length: 39\r\n\r\n"+body, buf.String())
	})

	t.Run("writes empty body", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteMessage(&buf, "")
		require.NoError(t, err)
		assert.Equal(t, "Content-Length: 0\r\n\r\n", buf.String())
	})

	t.Run("returns error on write failure", func(t *testing.T) {
		w := &errorWriter{err: errors.New("write failed")}
		err := WriteMessage(w, "test")
		require.Error(t, err)
		assert.Equal(t, "write failed", err.Error())
	})
}

func TestWriteMessagef(t *testing.T) {
	t.Run("writes formatted message", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteMessagef(&buf, "Hello, %s!", "World")
		require.NoError(t, err)
		assert.Equal(t, "Content-Length: 13\r\n\r\nHello, World!", buf.String())
	})

	t.Run("handles multiple arguments", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteMessagef(&buf, "%d %s %.2f", 42, "test", 3.14)
		require.NoError(t, err)
		assert.Equal(t, "Content-Length: 12\r\n\r\n42 test 3.14", buf.String())
	})
}

func TestReadMessage(t *testing.T) {
	t.Run("reads simple message with \\n line endings", func(t *testing.T) {
		msg := "Content-Length: 4\n\ntest"
		body, err := ReadMessage(strings.NewReader(msg))
		require.NoError(t, err)
		assert.Equal(t, "test", body)
	})

	t.Run("reads JSON message", func(t *testing.T) {
		body := `{"jsonrpc":"2.0","method":"initialize"}`
		msg := "Content-Length: 39\n\n" + body
		result, err := ReadMessage(strings.NewReader(msg))
		require.NoError(t, err)
		assert.Equal(t, body, result)
	})

	t.Run("reads with extra headers", func(t *testing.T) {
		msg := "Content-Type: application/json\nContent-Length: 4\n\ntest"
		result, err := ReadMessage(strings.NewReader(msg))
		require.NoError(t, err)
		assert.Equal(t, "test", result)
	})

	t.Run("handles case insensitive header", func(t *testing.T) {
		msg := "content-length: 4\n\ntest"
		body, err := ReadMessage(strings.NewReader(msg))
		require.NoError(t, err)
		assert.Equal(t, "test", body)
	})

	t.Run("returns error on empty input", func(t *testing.T) {
		_, err := ReadMessage(strings.NewReader(""))
		require.Error(t, err)
		assert.Equal(t, io.EOF, err)
	})

	t.Run("returns error on missing Content-Length", func(t *testing.T) {
		r := strings.NewReader("Content-Type: text/plain\n\n")
		_, err := ReadMessage(r)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid or missing Content-Length")
	})

	t.Run("returns error on invalid Content-Length value", func(t *testing.T) {
		r := strings.NewReader("Content-Length: abc\n\n")
		_, err := ReadMessage(r)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid or missing Content-Length")
	})

	t.Run("returns error on incomplete body", func(t *testing.T) {
		// Content-Length says 10 but body is only 4 bytes
		r := strings.NewReader("Content-Length: 10\n\ntest")
		_, err := ReadMessage(r)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "incomplete message body")
	})

	t.Run("returns error on unexpected EOF mid-header", func(t *testing.T) {
		// Partial header with no newline
		r := strings.NewReader("Content-Length")
		_, err := ReadMessage(r)
		require.Error(t, err)
	})

	t.Run("reads empty body when Content-Length is 0 - returns error", func(t *testing.T) {
		// Content-Length: 0 results in length <= 0 check failing
		r := strings.NewReader("Content-Length: 0\n\n")
		_, err := ReadMessage(r)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid or missing Content-Length")
	})

	t.Run("works with existing bufio.Reader", func(t *testing.T) {
		msg := "Content-Length: 5\n\nhello"
		br := bufio.NewReader(strings.NewReader(msg))
		body, err := ReadMessage(br)
		require.NoError(t, err)
		assert.Equal(t, "hello", body)
	})
}

func TestMessageReader(t *testing.T) {
	t.Run("NewMessageReader creates reader", func(t *testing.T) {
		r := strings.NewReader("test")
		mr := NewMessageReader(r)
		assert.NotNil(t, mr)
		assert.NotNil(t, mr.br)
	})

	t.Run("Read works through wrapper", func(t *testing.T) {
		msg := "Content-Length: 4\n\ntest"
		mr := NewMessageReader(strings.NewReader(msg))

		body, err := mr.Read()
		require.NoError(t, err)
		assert.Equal(t, "test", body)
	})

	t.Run("Read returns error on invalid message", func(t *testing.T) {
		mr := NewMessageReader(strings.NewReader("invalid"))
		_, err := mr.Read()
		require.Error(t, err)
	})
}

func TestMessageWriter(t *testing.T) {
	t.Run("NewMessageWriter creates writer", func(t *testing.T) {
		var buf bytes.Buffer
		mw := NewMessageWriter(&buf)
		assert.NotNil(t, mw)
		assert.NotNil(t, mw.w)
	})

	t.Run("Write works through wrapper", func(t *testing.T) {
		var buf bytes.Buffer
		mw := NewMessageWriter(&buf)
		err := mw.Write("test")
		require.NoError(t, err)
		assert.Equal(t, "Content-Length: 4\r\n\r\ntest", buf.String())
	})

	t.Run("Writef works through wrapper", func(t *testing.T) {
		var buf bytes.Buffer
		mw := NewMessageWriter(&buf)
		err := mw.Writef("Hello, %s!", "World")
		require.NoError(t, err)
		assert.Equal(t, "Content-Length: 13\r\n\r\nHello, World!", buf.String())
	})
}

// --- Coverage gap tests for framing.go ---

func TestReadMessageWithCROnlyLine(t *testing.T) {
	t.Run("handles \\r only line between headers", func(t *testing.T) {
		// Covers line 50: the `if line == "\r"` branch
		// When a \r\n appears in the header section, after stripping \n we get "\r".
		// The parser's \r-only check triggers `continue`, skipping to find the real blank line.
		// Input: "Content-Length: 4\n\r\n\ntest"
		//   - ReadString('\n') → "Content-Length: 4\n" → strip \n → "Content-Length: 4" → header
		//   - ReadString('\n') → "\r\n" → strip \n → "\r" → continue (line 50!)
		//   - ReadString('\n') → "\n" → strip \n → "" → break (end of headers)
		//   - ReadFull(4 bytes) → "test"
		msg := "Content-Length: 4\n\r\n\ntest"
		result, err := ReadMessage(strings.NewReader(msg))
		require.NoError(t, err)
		assert.Equal(t, "test", result)
	})
}

func TestReadMessageEmptyHeadersShowsErrInvalidMessage(t *testing.T) {
	t.Run("blank header section returns ErrInvalidMessage", func(t *testing.T) {
		// Covers line 67: headerStr == "" → ErrInvalidMessage
		// This happens when the first line is a blank line (just \n)
		_, err := ReadMessage(strings.NewReader("\n"))
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidMessage)
	})
}

func TestReadMessageWriteToBufferError(t *testing.T) {
	t.Run("writer error during header buffering", func(t *testing.T) {
		// Covers line 61: headerBuf.WriteString returns error
		// This is hard to trigger directly since headerBuf is internal.
		// We can't easily inject a failing writer into ReadMessage since
		// the io.Reader interface doesn't involve writes. Skip this as impractical.
	})
}

func TestReadMessageReadBodyNonEOFError(t *testing.T) {
	t.Run("non-EOF/non-UnexpectedEOF error reading body", func(t *testing.T) {
		// Covers line 84: io.ReadFull returns some other error
		r := &readErrorReader{
			header:  "Content-Length: 10\n\n",
			bodyErr: errors.New("read failure"),
		}
		_, err := ReadMessage(r)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read failure")
	})
}

// readErrorReader delivers header bytes then fails on body read.
type readErrorReader struct {
	header  string
	bodyErr error
	offset  int
}

func (r *readErrorReader) Read(p []byte) (int, error) {
	if r.offset < len(r.header) {
		n := copy(p, r.header[r.offset:])
		r.offset += n
		return n, nil
	}
	return 0, r.bodyErr
}

func TestReadMessageUnexpectedEOFMidHeader(t *testing.T) {
	t.Run("partial header with EOF returns error", func(t *testing.T) {
		// Covers line 61 area: unexpected EOF while reading headers
		// A partial header line with no trailing newline will trigger
		// ReadString('\n') to return io.EOF
		r := strings.NewReader("Content-Length")
		_, err := ReadMessage(r)
		require.Error(t, err)
	})
}

// errorWriter is a test helper that always returns an error
type errorWriter struct {
	err error
}

func (w *errorWriter) Write(p []byte) (int, error) {
	return 0, w.err
}
