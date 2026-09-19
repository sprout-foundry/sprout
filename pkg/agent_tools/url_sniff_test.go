//go:build !js

package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 1x1 transparent PNG.
var sniffTestPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89,
}

func TestSniffURLContent(t *testing.T) {
	pngServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No Content-Type at all — forces the sniff.
		_, _ = w.Write(sniffTestPNG)
	}))
	defer pngServer.Close()

	octetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(sniffTestPNG)
	}))
	defer octetServer.Close()

	pdfServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("%PDF-1.4 fake body for signature detection"))
	}))
	defer pdfServer.Close()

	textServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>hello</body></html>"))
	}))
	defer textServer.Close()

	deadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer deadServer.Close()

	cases := []struct {
		name string
		url  string
		want ResponseKind
		ok   bool
	}{
		{"png without content-type", pngServer.URL, ResponseKindImage, true},
		{"png behind octet-stream", octetServer.URL, ResponseKindImage, true},
		{"pdf by signature", pdfServer.URL, ResponseKindPDF, true},
		{"html stays text", textServer.URL, ResponseKindUnknown, false},
		{"404 is not sniffable", deadServer.URL, ResponseKindUnknown, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, _, ok := SniffURLContent(context.Background(), tc.url)
			if kind != tc.want || ok != tc.ok {
				t.Errorf("SniffURLContent(%s) = (%v, %v), want (%v, %v)", tc.url, kind, ok, tc.want, tc.ok)
			}
		})
	}
}
