package localmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// hfBaseURL and hfHTTPClient are package vars so tests can point the
// downloader at an httptest server.
var (
	hfBaseURL    = "https://huggingface.co"
	hfHTTPClient = &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 60 * time.Second,
		TLSHandshakeTimeout:   30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	}}
	hfProgressInterval = 200 * time.Millisecond
	hfRetryBackoff     = time.Second
)

const (
	hfRevision       = "main"
	partSuffix       = ".part"
	hfMaxFileRetries = 3
)

type hfFile struct {
	Path string
	Size int64
}

type hfTreeEntry struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Size int64  `json:"size"`
	LFS  *struct {
		Size int64 `json:"size"`
	} `json:"lfs"`
}

var linkNextRe = regexp.MustCompile(`<([^>]+)>\s*;\s*rel="?next"?`)

func hfNewRequest(ctx context.Context, method, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "sprout-localmodel")
	if tok := strings.TrimSpace(os.Getenv("HF_TOKEN")); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return req, nil
}

func hfStatusError(repo string, resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("access to %s denied (HTTP %d) — gated or private repos need HF_TOKEN set", repo, resp.StatusCode)
	case http.StatusNotFound:
		return fmt.Errorf("%s not found on Hugging Face (HTTP 404)", repo)
	}
	return fmt.Errorf("hugging face request for %s failed: HTTP %d", repo, resp.StatusCode)
}

func escapeRepoPath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// listHFFiles returns every file in the repo at hfRevision, following the
// tree API's Link-header pagination.
func listHFFiles(ctx context.Context, repo string) ([]hfFile, error) {
	next := fmt.Sprintf("%s/api/models/%s/tree/%s?recursive=true", hfBaseURL, escapeRepoPath(repo), hfRevision)
	var files []hfFile
	for next != "" {
		req, err := hfNewRequest(ctx, http.MethodGet, next)
		if err != nil {
			return nil, err
		}
		resp, err := hfHTTPClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", repo, err)
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return nil, hfStatusError(repo, resp)
		}
		var entries []hfTreeEntry
		err = json.NewDecoder(resp.Body).Decode(&entries)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("list %s: decode: %w", repo, err)
		}
		for _, e := range entries {
			if e.Type != "file" {
				continue
			}
			size := e.Size
			if e.LFS != nil && e.LFS.Size > 0 {
				size = e.LFS.Size
			}
			files = append(files, hfFile{Path: e.Path, Size: size})
		}
		next = ""
		if m := linkNextRe.FindStringSubmatch(resp.Header.Get("Link")); m != nil {
			u, err := resp.Request.URL.Parse(m[1])
			if err != nil {
				return nil, fmt.Errorf("list %s: bad Link header: %w", repo, err)
			}
			next = u.String()
		}
	}
	return files, nil
}

// globToRegexp converts an fnmatch-style pattern to a regexp. The hf CLI's
// --include uses Python fnmatch, where '*' also matches '/', so "*.json"
// selects JSON files in subdirectories too; path.Match would not.
func globToRegexp(pattern string) (*regexp.Regexp, error) {
	if strings.HasSuffix(pattern, "/") {
		pattern += "*"
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '[':
			end := strings.IndexByte(pattern[i+1:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := pattern[i+1 : i+1+end]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += end + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// filterHFFiles keeps files matching include (all files when empty) and
// drops any path that could escape the destination directory.
func filterHFFiles(files []hfFile, include string) ([]hfFile, error) {
	var re *regexp.Regexp
	if include != "" {
		var err error
		if re, err = globToRegexp(include); err != nil {
			return nil, fmt.Errorf("invalid include pattern %q: %w", include, err)
		}
	}
	var out []hfFile
	for _, f := range files {
		if !filepath.IsLocal(filepath.FromSlash(f.Path)) {
			continue
		}
		if re != nil && !re.MatchString(f.Path) {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

// progressTracker aggregates bytes across files and rate-limits callbacks.
type progressTracker struct {
	fn       ProgressCallback
	total    int64
	done     int64
	last     time.Time
	interval time.Duration
}

func (p *progressTracker) add(n int64) {
	p.done += n
	if p.fn == nil {
		return
	}
	if now := time.Now(); now.Sub(p.last) >= p.interval {
		p.last = now
		p.fn(p.done, p.total)
	}
}

func (p *progressTracker) flush() {
	if p.fn != nil {
		p.last = time.Now()
		p.fn(p.done, p.total)
	}
}

func fileSize(path string) (int64, bool) {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return 0, false
	}
	return st.Size(), true
}

// downloadHFRepo downloads the selected files of repo into localDir.
// Each file is streamed to <name>.part; the .part files are only renamed
// to their final names once every file is complete, so the catalog's
// weight-file detection never sees a partially downloaded model and an
// interrupted run leaves resumable .part files behind.
func downloadHFRepo(ctx context.Context, repo, include, localDir string, progressFn ProgressCallback) error {
	all, err := listHFFiles(ctx, repo)
	if err != nil {
		return err
	}
	files, err := filterHFFiles(all, include)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		if include != "" {
			return fmt.Errorf("no files in %s match %q", repo, include)
		}
		return fmt.Errorf("%s has no files", repo)
	}

	tracker := &progressTracker{fn: progressFn, interval: hfProgressInterval}
	var pending []hfFile
	for _, f := range files {
		tracker.total += f.Size
		final := filepath.Join(localDir, filepath.FromSlash(f.Path))
		if sz, ok := fileSize(final); ok {
			if sz == f.Size {
				tracker.done += f.Size
				continue
			}
			// A wrong-size final file is stale or truncated; drop it so
			// it can't pass weight detection while the fresh copy downloads.
			if err := os.Remove(final); err != nil {
				return fmt.Errorf("remove stale %s: %w", f.Path, err)
			}
		}
		if sz, ok := fileSize(final + partSuffix); ok && sz <= f.Size {
			tracker.done += sz
		}
		pending = append(pending, f)
	}
	tracker.flush()

	for _, f := range pending {
		if err := downloadHFFileWithRetry(ctx, repo, f, localDir, tracker); err != nil {
			return err
		}
	}

	// Rename weights and index files last so the model only looks
	// installed once its config and tokenizer are in place.
	sort.SliceStable(pending, func(i, j int) bool {
		return renameRank(pending[i].Path) < renameRank(pending[j].Path)
	})
	for _, f := range pending {
		final := filepath.Join(localDir, filepath.FromSlash(f.Path))
		if err := os.Rename(final+partSuffix, final); err != nil {
			return fmt.Errorf("finalize %s: %w", f.Path, err)
		}
	}
	tracker.flush()
	return nil
}

func renameRank(p string) int {
	switch {
	case strings.HasSuffix(p, "model.safetensors.index.json"):
		return 2
	case strings.HasSuffix(p, ".safetensors"), strings.HasSuffix(p, ".gguf"):
		return 1
	}
	return 0
}

func downloadHFFileWithRetry(ctx context.Context, repo string, f hfFile, localDir string, tracker *progressTracker) error {
	var err error
	for attempt := 0; attempt < hfMaxFileRetries; attempt++ {
		if err = downloadHFFile(ctx, repo, f, localDir, tracker); err == nil {
			return nil
		}
		var perm *permanentError
		if ctx.Err() != nil || errors.As(err, &perm) {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Duration(attempt+1) * hfRetryBackoff):
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("download %s: %w", f.Path, err)
}

type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// downloadHFFile brings <final>.part to the full size of f, resuming from
// an existing partial file when the server honours Range requests.
func downloadHFFile(ctx context.Context, repo string, f hfFile, localDir string, tracker *progressTracker) error {
	part := filepath.Join(localDir, filepath.FromSlash(f.Path)) + partSuffix
	if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
		return &permanentError{err}
	}

	offset, _ := fileSize(part)
	if offset > f.Size {
		tracker.done -= offset
		offset = 0
		if err := os.Remove(part); err != nil {
			return &permanentError{err}
		}
	}
	if offset == f.Size {
		return nil
	}

	rawURL := fmt.Sprintf("%s/%s/resolve/%s/%s", hfBaseURL, escapeRepoPath(repo), hfRevision, escapeRepoPath(f.Path))
	req, err := hfNewRequest(ctx, http.MethodGet, rawURL)
	if err != nil {
		return &permanentError{err}
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := hfHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	flags := os.O_WRONLY | os.O_CREATE
	switch {
	case resp.StatusCode == http.StatusPartialContent && offset > 0:
		flags |= os.O_APPEND
	case resp.StatusCode == http.StatusOK:
		flags |= os.O_TRUNC
		tracker.done -= offset
		offset = 0
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		tracker.done -= offset
		_ = os.Remove(part)
		return fmt.Errorf("server rejected resume range for %s", f.Path)
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
		return hfStatusError(repo, resp)
	default:
		return &permanentError{hfStatusError(repo, resp)}
	}

	out, err := os.OpenFile(part, flags, 0o644) //nolint:gosec // G302: model files are shared read-only data
	if err != nil {
		return &permanentError{err}
	}
	written, copyErr := copyWithProgress(out, resp.Body, tracker)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return &permanentError{closeErr}
	}
	if got := offset + written; got != f.Size {
		tracker.done -= got
		_ = os.Remove(part)
		return fmt.Errorf("size mismatch for %s: got %d bytes, want %d", f.Path, got, f.Size)
	}
	return nil
}

func copyWithProgress(dst io.Writer, src io.Reader, tracker *progressTracker) (int64, error) {
	buf := make([]byte, 256*1024)
	var written int64
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			w, werr := dst.Write(buf[:n])
			written += int64(w)
			tracker.add(int64(w))
			if werr != nil {
				return written, &permanentError{werr}
			}
		}
		if rerr == io.EOF {
			return written, nil
		}
		if rerr != nil {
			return written, rerr
		}
	}
}
