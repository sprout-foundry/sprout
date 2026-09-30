package semantic

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
)

// writeJSONRPC writes a JSON-RPC message to the writer.
func writeJSONRPC(w *bufio.Writer, msg map[string]interface{}) {
	data, _ := json.Marshal(msg)
	// Content-Length header format used by gopls
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	_, _ = w.WriteString(header)
	_, _ = w.Write(data)
	_ = w.Flush()
}

// readJSONRPC reads a JSON-RPC message from the reader.
func readJSONRPC(r *bufio.Reader) (map[string]interface{}, error) {
	// Read Content-Length header
	header, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	var contentLen int
	fmt.Sscanf(strings.TrimSpace(header), "Content-Length: %d", &contentLen)

	// Skip the empty line after header
	_, err = r.ReadString('\n')
	if err != nil {
		return nil, err
	}

	// Read content
	content := make([]byte, contentLen)
	_, err = io.ReadFull(r, content)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(content, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// fileURI converts a filesystem path to an LSP file:// URI. Windows paths
// need the slash-separated, drive-rooted form (file:///C:/src/main.go);
// url.URL also percent-encodes spaces and other reserved characters.
func fileURI(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}
