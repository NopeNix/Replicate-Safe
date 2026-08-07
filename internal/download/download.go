package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Downloader struct {
	Client *http.Client
	Token  string
	Log    *slog.Logger
}

func New(timeout time.Duration, token string, log *slog.Logger) *Downloader {
	return &Downloader{
		Client: &http.Client{Timeout: timeout},
		Token:  token,
		Log:    log,
	}
}

// Download fetches rawURL into outPath with auth. If outPath already exists with
// the same size as the remote content, it is skipped. Returns true if a new
// download happened, false if skipped or already present.
func (d *Downloader) Download(ctx context.Context, rawURL, outPath string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return false, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+d.Token)
	req.Header.Set("User-Agent", "replicate-safe/1.0")

	resp, err := d.doWithRetry(req, 3)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false, fmt.Errorf("not found (output likely expired on Replicate)")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	expected := resp.ContentLength
	if expected > 0 {
		if info, err := os.Stat(outPath); err == nil && info.Size() == expected {
			d.Log.Debug("skip, already complete", "path", outPath)
			return false, nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return false, fmt.Errorf("mkdir: %w", err)
	}
	tmp := outPath + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return false, fmt.Errorf("create tmp: %w", err)
	}
	cleanup := func() { _ = os.Remove(tmp) }

	written, err := io.Copy(f, resp.Body)
	if err != nil {
		_ = f.Close()
		cleanup()
		return false, fmt.Errorf("copy: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return false, fmt.Errorf("close tmp: %w", err)
	}
	if expected > 0 && written != expected {
		cleanup()
		return false, fmt.Errorf("size mismatch: wrote %d, expected %d", written, expected)
	}
	if err := os.Rename(tmp, outPath); err != nil {
		cleanup()
		return false, fmt.Errorf("rename: %w", err)
	}
	return true, nil
}

func (d *Downloader) doWithRetry(req *http.Request, attempts int) (*http.Response, error) {
	var lastErr error
	delay := 2 * time.Second
	for i := 0; i < attempts; i++ {
		resp, err := d.Client.Do(req.Clone(req.Context()))
		if err == nil {
			if resp.StatusCode == 429 || (resp.StatusCode >= 500 && resp.StatusCode < 600) {
				lastErr = fmt.Errorf("transient status %d", resp.StatusCode)
				_ = resp.Body.Close()
				if i < attempts-1 {
					d.Log.Warn("retrying after backoff", "attempt", i+1, "delay", delay, "err", lastErr)
					time.Sleep(delay)
					delay *= 2
					continue
				}
				return nil, lastErr
			}
			return resp, nil
		}
		lastErr = err
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if i < attempts-1 {
			d.Log.Warn("retrying after backoff", "attempt", i+1, "delay", delay, "err", err)
			time.Sleep(delay)
			delay *= 2
		}
	}
	return nil, fmt.Errorf("after %d attempts: %w", attempts, lastErr)
}

// FilenameFromURL derives a sane local filename for a remote URL.
// Order: Content-Disposition filename, URL path basename, "output.bin".
func FilenameFromURL(rawURL, contentDisposition string) string {
	if contentDisposition != "" {
		if _, params, err := mime.ParseMediaType(contentDisposition); err == nil {
			if fn := params["filename"]; fn != "" {
				return sanitize(fn)
			}
		}
	}
	if u, err := url.Parse(rawURL); err == nil && u.Path != "" {
		base := filepath.Base(u.Path)
		if base != "" && base != "/" && base != "." {
			return sanitize(base)
		}
	}
	return "output.bin"
}

// sanitize strips path separators from a filename.
func sanitize(name string) string {
	name = filepath.Base(name)
	repl := strings.NewReplacer("/", "_", "\\", "_", "\x00", "")
	return repl.Replace(name)
}

// SizeFromHeaders returns the content length if parseable, else -1.
func SizeFromHeaders(h http.Header) int64 {
	if v := h.Get("Content-Length"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			return n
		}
	}
	return -1
}
