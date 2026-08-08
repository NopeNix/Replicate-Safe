// Package server implements the HTTP API + static file serving for the
// replicate-safe frontend.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/phil/Replicate-Safe/frontend/internal/listing"
)

// Server holds the config and serves from a single data directory.
type Server struct {
	DataDir   string
	WebFS     fs.FS // embedded static assets
	Addr      string
	CacheTTL  time.Duration
	Log       *slog.Logger
	cacheMu   cacheMu
	cache     []listing.Entry
	cacheTime time.Time
}

type cacheMu struct{ /* placeholder; mutex added below */ }

func (s *Server) cacheLoad() ([]listing.Entry, error) {
	now := time.Now()
	if s.cache != nil && now.Sub(s.cacheTime) < s.CacheTTL {
		return s.cache, nil
	}
	entries, err := listing.Load(s.DataDir)
	if err != nil {
		return nil, err
	}
	s.cache = entries
	s.cacheTime = now
	return entries, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// API
	mux.HandleFunc("/api/predictions", s.handleList)
	mux.HandleFunc("/api/metadata", s.handleMetadata)

	// File streaming
	mux.HandleFunc("/file", s.handleFile)

	// Static frontend
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
	entries, err := s.cacheLoad()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(entries); err != nil {
		s.Log.Error("encode list", "err", err)
	}
}

func (s *Server) handleMetadata(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, errors.New("missing id"))
		return
	}
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

// handleFile streams a file from the data directory. ?id=<prediction id>
// resolves to the first matching output file for that prediction. Optional
// &file= picks a specific output when a prediction produced
// several.
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	name := r.URL.Query().Get("file")
	if id == "" {
		writeErr(w, http.StatusBadRequest, errors.New("missing id"))
		return
	}
	if !validID(id) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid id"))
		return
	}

	target := name
	if target == "" {
		// Find first output file for this prediction.
		matches, err := filepath.Glob(filepath.Join(s.DataDir, id+"__*"))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		// Filter out metadata sidecars (already excluded by pattern, but
		// defensively keep only real output files).
		var outs []string
		for _, m := range matches {
			base := filepath.Base(m)
			if !strings.HasSuffix(base, ".metadata.json") && strings.Contains(base, "__") {
				outs = append(outs, m)
			}
		}
		if len(outs) == 0 {
			writeErr(w, http.StatusNotFound, errors.New("no output for prediction"))
			return
		}
		target = filepath.Base(outs[0])
	}

	// Defense in depth: ensure the resolved path is inside DataDir.
	clean := filepath.Clean(filepath.Join(s.DataDir, target))
	if !strings.HasPrefix(clean, s.DataDir+string(os.PathSeparator)) && clean != s.DataDir {
		writeErr(w, http.StatusBadRequest, errors.New("path escapes data dir"))
		return
	}
	if !strings.HasPrefix(target, id+"__") {
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

	mime := mimeByName(target)
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Content-Disposition", `inline; filename="`+url.PathEscape(target)+`"`)
	http.ServeContent(w, r, target, stat.ModTime(), f)
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
