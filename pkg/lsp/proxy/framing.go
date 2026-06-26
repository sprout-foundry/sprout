package proxy

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	headerContentLength = "Content-Length:"
	headerDelimiter     = "\r\n\r\n"
)

// ErrInvalidMessage is returned when a message cannot be parsed.
var ErrInvalidMessage = errors.New("invalid LSP message")

// ReadMessage reads a single LSP Content-Length framed message from the reader.
// Format: "Content-Length: <n>\r\n\r\n<body>"
// Returns the body as a string.
func ReadMessage(r io.Reader) (string, error) {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}

	// Read headers until we find the blank line delimiter
	var headerBuf bytes.Buffer
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && headerBuf.Len() > 0 {
				return "", fmt.Errorf("unexpected EOF while reading headers: %w", err)
			}
			return "", err
		}

		// Trim the trailing \n
		line = line[:len(line)-len("\n")]

		// Check for end of headers
		if line == "" {
			break
		}

		// Also handle if we get just \r\n (blank line without \r)
		if line == "\r" {
			// Need to check if next char is \n or if we've already consumed it
			// This is a bit tricky with ReadString - let's handle explicitly
			continue
		}

		// Remove \r if present (for Windows line endings)
		line = strings.TrimSuffix(line, "\r")

		_, err = headerBuf.WriteString(line)
		_, err = headerBuf.WriteString("\n")
		if err != nil {
			return "", err
		}
	}

	headerStr := headerBuf.String()
	if headerStr == "" {
		return "", ErrInvalidMessage
	}

	// Parse Content-Length
	length := parseContentLength(headerStr)
	if length <= 0 {
		return "", fmt.Errorf("invalid or missing Content-Length header")
	}

	// Read the body
	body := make([]byte, length)
	n, err := io.ReadFull(br, body)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return "", fmt.Errorf("incomplete message body: got %d, want %d: %w", n, length, err)
		}
		return "", err
	}

	return string(body), nil
}

// parseContentLength parses the Content-Length value from headers.
func parseContentLength(headers string) int {
	for _, line := range strings.Split(headers, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), strings.ToLower(headerContentLength)) {
			value := strings.TrimSpace(line[len(headerContentLength):])
			length, err := strconv.Atoi(value)
			if err != nil {
				return 0
			}
			return length
		}
	}
	return 0
}

// WriteMessage writes a single LSP Content-Length framed message to the writer.
func WriteMessage(w io.Writer, body string) error {
	length := len(body)
	header := fmt.Sprintf("%s %d%s%s", headerContentLength, length, headerDelimiter, body)
	_, err := w.Write([]byte(header))
	return err
}

// WriteMessagef writes a formatted LSP message.
func WriteMessagef(w io.Writer, format string, args ...interface{}) error {
	body := fmt.Sprintf(format, args...)
	return WriteMessage(w, body)
}

// MessageReader provides a convenient interface for reading messages.
type MessageReader struct {
	br *bufio.Reader
}

// NewMessageReader creates a new message reader.
func NewMessageReader(r io.Reader) *MessageReader {
	return &MessageReader{
		br: bufio.NewReader(r),
	}
}

// Read reads the next message.
func (mr *MessageReader) Read() (string, error) {
	return ReadMessage(mr.br)
}

// MessageWriter provides a convenient interface for writing messages.
type MessageWriter struct {
	w io.Writer
}

// NewMessageWriter creates a new message writer.
func NewMessageWriter(w io.Writer) *MessageWriter {
	return &MessageWriter{
		w: w,
	}
}

// Write writes a message.
func (mw *MessageWriter) Write(body string) error {
	return WriteMessage(mw.w, body)
}

// Writef writes a formatted message.
func (mw *MessageWriter) Writef(format string, args ...interface{}) error {
	return WriteMessagef(mw.w, format, args...)
}
