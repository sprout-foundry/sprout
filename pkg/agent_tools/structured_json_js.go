//go:build js && wasm

// Package tools provides platform-specific JSON encoding for WASM builds.
package tools

import (
	"bytes"
	"encoding/json"
)

// platformJSONEncode marshals v to JSON and writes it to buf with indentation.
func platformJSONEncode(buf *bytes.Buffer, v interface{}) error {
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// platformJSONUnmarshal unmarshals JSON data into v.
func platformJSONUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
