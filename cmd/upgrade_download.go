//go:build !js

package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// downloadTo fetches a URL into dst with a 60s connect timeout and a
// total deadline of 5 minutes. Caller's responsibility to size that for
// their needs — release tarballs are ~30MB so 5m is enormous headroom.
func downloadTo(url, dst string) error {
	resp, err := httpClient().Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}

	src := io.Reader(resp.Body)
	if resp.ContentLength > 0 {
		bar := newProgressBar(resp.ContentLength)
		src = bar.reader(resp.Body)
		defer bar.done()
	} else {
		// No Content-Length (e.g. chunked encoding): just count bytes
		// so the user sees movement instead of a silent hang.
		counter := &countingReader{}
		src = io.TeeReader(resp.Body, counter)
		defer fmt.Fprintf(os.Stderr, "\rDownloaded %s      \n", humanBytes(counter.n))
	}

	if _, err := io.Copy(f, src); err != nil {
		f.Close()
		return err
	}
	// Close explicitly so flush/write errors surfaced at close time (e.g.
	// disk full, NFS commit failure) are not silently dropped — a partially
	// written archive would otherwise be treated as a complete download.
	if err := f.Close(); err != nil {
		return fmt.Errorf("flush %s: %w", dst, err)
	}
	return nil
}

// countingReader is a minimal Writer that just tallies bytes, for the
// unknown-content-length path of downloadTo.
type countingReader struct{ n int64 }

func (c *countingReader) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// humanBytes renders a byte count as a short human-readable string
// (e.g. "12.3 MB"). Used by downloadTo's fallback progress line.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// progressBar prints a single-line ASCII progress bar to stderr. It's
// deliberately dependency-free: the upgrade command runs in a terminal,
// not a TUI, so a plain \r-overwriting line is the right UX. The bar
// throttles redraws to ~10fps to avoid spamming slow terminals.
type progressBar struct {
	total    int64
	written  int64
	lastDraw time.Time
}

const progressRedrawInterval = 100 * time.Millisecond

func newProgressBar(total int64) *progressBar {
	return &progressBar{total: total}
}

// reader wraps r so every copy chunk updates the bar.
func (b *progressBar) reader(r io.Reader) io.Reader {
	return &progressReader{r: r, bar: b}
}

func (b *progressBar) maybeDraw(force bool) {
	now := time.Now()
	if !force && now.Sub(b.lastDraw) < progressRedrawInterval {
		return
	}
	b.lastDraw = now

	const barWidth = 30
	frac := float64(b.written) / float64(b.total)
	if frac > 1 {
		frac = 1
	}
	filled := int(frac * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
	fmt.Fprintf(os.Stderr, "\r  [%s] %5.1f%%  %s / %s",
		bar, frac*100,
		humanBytes(b.written), humanBytes(b.total))
}

// done prints a final newline so subsequent output doesn't overwrite the bar.
func (b *progressBar) done() {
	b.maybeDraw(true)
	fmt.Fprintln(os.Stderr)
}

type progressReader struct {
	r   io.Reader
	bar *progressBar
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	pr.bar.written += int64(n)
	pr.bar.maybeDraw(false)
	return n, err
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Minute}
}
