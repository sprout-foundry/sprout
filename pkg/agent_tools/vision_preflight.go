package tools

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// remoteSizeExceededError is returned when a remote resource exceeds the
// configured size cap. It is identified by the IsRemoteSizeExceededError
// helper so callers can produce a tailored ErrCodeRemoteFetchFailed message.
type remoteSizeExceededError struct {
	URL         string
	SizeBytes   int64
	CapBytes    int64
	IsHEADGuess bool // true when no Content-Length was provided
}

// Error implements the error interface.
func (e *remoteSizeExceededError) Error() string {
	if e.IsHEADGuess {
		return fmt.Sprintf("remote resource at %s likely exceeds %d MB cap (no Content-Length header; size unknown)",
			e.URL, e.CapBytes/1024/1024)
	}
	return fmt.Sprintf("remote resource at %s is %d MB, exceeds %d MB cap",
		e.URL, e.SizeBytes/1024/1024, e.CapBytes/1024/1024)
}

// IsRemoteSizeExceededError reports whether err (or any wrapped error in
// its chain) is a *remoteSizeExceededError.
func IsRemoteSizeExceededError(err error) bool {
	if err == nil {
		return false
	}
	for cur := err; cur != nil; {
		if _, ok := cur.(*remoteSizeExceededError); ok {
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := cur.(unwrapper)
		if !ok {
			return false
		}
		cur = u.Unwrap()
	}
	return false
}

// preflightRemoteSize issues an HTTP HEAD against url to determine its
// Content-Length. Returns nil when the size is missing (caller should
// fall back to streaming + per-byte cap) or when the size is below capBytes.
//
// Errors:
//   - When Content-Length is present and exceeds capBytes, returns a
//     *remoteSizeExceededError WITHOUT issuing a GET.
//   - When the HEAD itself fails (e.g., S3 signed URLs that reject HEAD),
//     returns nil and a "size unknown" sentinel so callers can fall back
//     to streaming + size checking on the actual GET.
//
// The ctx is used to cancel an in-flight HEAD that runs too long (10s
// default; safe upper bound). A pre-cancelled ctx fails the HEAD fast.
func preflightRemoteSize(ctx context.Context, url string, capBytes int64) error {
	headCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(headCtx, http.MethodHead, url, nil)
	if err != nil {
		// Treat request construction failure as size-unknown — let the GET
		// fall back to streaming.
		return nil
	}
	// Use a separate client for the HEAD so the GET client's Timeout
	// (30s) doesn't apply.
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// HEAD failed (network error, server rejected HEAD, ctx canceled).
		// Don't bail — the GET might still work. Return nil so the caller
		// falls back to streaming + size cap on the body.
		return nil
	}
	defer resp.Body.Close()

	// Non-2xx responses on HEAD: fall back to GET. Some servers return 405
	// for HEAD against authenticated endpoints; we don't want to fail.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}

	// Some servers return 204 No Content for HEAD — fall back to GET.
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	clHeader := strings.TrimSpace(resp.Header.Get("Content-Length"))
	if clHeader == "" {
		// No Content-Length: chunked transfer or hidden by intermediate.
		// Fall back to streaming + size cap.
		return nil
	}
	size, err := strconv.ParseInt(clHeader, 10, 64)
	if err != nil {
		// Malformed header value: treat as size-unknown.
		return nil
	}
	if size > capBytes {
		return &remoteSizeExceededError{URL: url, SizeBytes: size, CapBytes: capBytes}
	}
	return nil
}
