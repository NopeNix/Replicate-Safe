// Package server implements the HTTP API + static file serving for the
// replicate-safe frontend.
package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/disintegration/imaging"

	"github.com/phil/Replicate-Safe/frontend/internal/listing"
)

const thumbSize = 128

// Server holds the config and serves from a single data directory.
type Server struct {
	DataDir string
	WebFS   fs.FS
	Addr    string
	Log     *slog.Logger
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/predictions", s.handleList)
	mux.HandleFunc("/api/metadata", s.handleMetadata)
	mux.HandleFunc("/file", s.handleFile)
	mux.HandleFunc("/thumb", s.handleThumb)

	staticFS, err := fs.Sub(s.WebFS, "web")
	if err == nil {
		mux.Handle("/", http.FileServer(http.FS(staticFS)))
	} else {
		s.Log.Warn("static fs sub failed; serving 404 for /", "err", err)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
	}

	return logMiddleware(s.Log, mux)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	entries, err := listing.Load(s.DataDir, q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if entries == nil {
		entries = []listing.Entry{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(entries); err != nil {
		s.Log.Error("encode list", "err", err)
	}
}

func (s *Server) handleMetadata(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if !validID(id) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid id"))
		return
	}
	data, err := listing.ReadMetadata(s.DataDir, id)
	if err != nil {
		if errors.Is(err, listing.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	id, name, err := s.resolveFile(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	clean := filepath.Clean(filepath.Join(s.DataDir, name))
	if !strings.HasPrefix(clean, s.DataDir+string(os.PathSeparator)) && clean != s.DataDir {
		writeErr(w, http.StatusBadRequest, errors.New("path escapes data dir"))
		return
	}
	if !strings.HasPrefix(name, id+"__") {
		writeErr(w, http.StatusBadRequest, errors.New("file does not belong to id"))
		return
	}
	f, err := os.Open(clean)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if stat.IsDir() {
		writeErr(w, http.StatusBadRequest, errors.New("is a directory"))
		return
	}
	mime := mimeByName(name)
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Content-Disposition", `inline; filename="`+url.PathEscape(name)+`"`)
	http.ServeContent(w, r, name, stat.ModTime(), f)
}

func (s *Server) handleThumb(w http.ResponseWriter, r *http.Request) {
	id, name, err := s.resolveFile(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	clean := filepath.Clean(filepath.Join(s.DataDir, name))
	if !strings.HasPrefix(clean, s.DataDir+string(os.PathSeparator)) && clean != s.DataDir {
		writeErr(w, http.StatusBadRequest, errors.New("path escapes data dir"))
		return
	}
	if !strings.HasPrefix(name, id+"__") {
		writeErr(w, http.StatusBadRequest, errors.New("file does not belong to id"))
		return
	}

	// Only resize images. Other types get a generic SVG icon (so the
	// browser doesn't try to render a video as a thumbnail).
	kind := listingKind(name)
	if kind != "image" {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "private, max-age=86400")
		_, _ = w.Write([]byte(iconForKind(kind)))
		return
	}

	// Avoid wasted work on huge files for a tiny thumbnail.
	src, err := imaging.Open(clean, imaging.AutoOrientation(true))
	if err != nil {
		// Fall back to the original file (let the browser scale). This
		// handles SVG, WebP, AVIF, etc. that imaging can't decode.
		f, ferr := os.Open(clean)
		if ferr != nil {
			writeErr(w, http.StatusNotFound, ferr)
			return
		}
		defer f.Close()
		stat, _ := f.Stat()
		w.Header().Set("Content-Type", mimeByName(name))
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
		http.ServeContent(w, r, name, stat.ModTime(), f)
		return
	}

	thumb := imaging.Fit(src, thumbSize, thumbSize, imaging.Lanczos)
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, thumb, imaging.JPEG, imaging.JPEGQuality(80)); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", buf.Len()))
	_, _ = io.Copy(w, &buf)
}

// resolveFile pulls id and optional file out of the query and resolves the
// first output file for that prediction if no file is given.
func (s *Server) resolveFile(r *http.Request) (id, name string, err error) {
	id = r.URL.Query().Get("id")
	if !validID(id) {
		return "", "", errors.New("invalid id")
	}
	name = r.URL.Query().Get("file")
	if name != "" {
		return id, name, nil
	}
	base, err := listing.FirstOutputFor(s.DataDir, id)
	if err != nil {
		return "", "", fmt.Errorf("no output for prediction: %w", err)
	}
	return id, base, nil
}

// listingKind is a duplicate of the helper in listing so the server doesn't
// have to read the entry off disk again. Kept simple: classify by extension.
func listingKind(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".avif", ".ico", ".tif", ".tiff":
		return "image"
	case ".mp4", ".webm", ".mov", ".m4v", ".mkv", ".avi", ".ogv":
		return "video"
	case ".mp3", ".wav", ".ogg", ".flac", ".m4a", ".aac", ".opus":
		return "audio"
	case ".txt", ".md", ".csv", ".log", ".srt":
		return "text"
	}
	return "other"
}

func iconForKind(kind string) string {
	const base = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" width="64" height="64"><rect width="64" height="64" fill="#1f1f1f" rx="8"/>`
	switch kind {
	case "video":
		return base + `<polygon points="24,18 24,46 48,32" fill="#f3f3f3"/></svg>`
	case "audio":
		return base + `<g fill="#f3f3f3"><rect x="30" y="14" width="4" height="20"/><rect x="22" y="22" width="4" height="14"/><rect x="38" y="22" width="4" height="14"/><rect x="14" y="28" width="4" height="10"/><rect x="46" y="28" width="4" height="10"/></g></svg>`
	case "text":
		return base + `<g fill="#f3f3f3"><rect x="16" y="16" width="32" height="3"/><rect x="16" y="24" width="32" height="3"/><rect x="16" y="32" width="32" height="3"/><rect x="16" y="40" width="22" height="3"/></g></svg>`
	default:
		return base + `<text x="32" y="42" text-anchor="middle" font-family="monospace" font-size="14" fill="#f3f3f3">FILE</text></svg>`
	}
}

func mimeByName(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".avif":
		return "image/avif"
	case ".bmp":
		return "image/bmp"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".ogv":
		return "video/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	case ".txt", ".log":
		return "text/plain; charset=utf-8"
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".csv":
		return "text/csv; charset=utf-8"
	case ".json":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}

// validID guards against path traversal in id query params.
func validID(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func logMiddleware(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"elapsed", time.Since(start),
			"remote", r.RemoteAddr,
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
