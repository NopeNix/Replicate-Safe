// Package listing scans the replicate-safe output directory and joins
// output files with their metadata sidecars to produce a single sortable
// list of "predictions" for the frontend.
package listing

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	idSep = "__" // separator written by the backend between prediction id and index
)

// Entry is one row in the listing. One per output file on disk.
type Entry struct {
	ID          string  `json:"id"`
	Filename    string  `json:"filename"`     // basename as written on disk
	Path        string  `json:"-"`            // full path, never sent to client
	Size        int64   `json:"size"`         // bytes
	Model       string  `json:"model"`        // e.g. "black-forest-labs/flux-schnell"
	Version     string  `json:"version"`      // short version id (first 12 chars)
	Status      string  `json:"status"`       // succeeded/failed/canceled/...
	CreatedAt   string  `json:"created_at"`   // ISO 8601, "" if unknown
	CompletedAt string  `json:"completed_at"` // ISO 8601, "" if unknown
	TimeToMake  float64 `json:"time_to_make"` // seconds, computed from metrics.total_time or created/completed delta
	Mime        string  `json:"mime"`
	PreviewKind string  `json:"preview_kind"` // image | video | audio | text | other
}

// Load walks dir and returns one Entry per output file (files matching the
// `id__idx_name` pattern written by the backend). Metadata sidecars are
// loaded and joined; missing metadata is non-fatal.
func Load(dir string) ([]Entry, error) {
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	// First pass: load every metadata sidecar keyed by id.
	metaByID := make(map[string]*metadataFile)
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".metadata.json") {
			return nil
		}
		id := strings.TrimSuffix(name, ".metadata.json")
		mf, err := loadMetadata(path, id)
		if err != nil {
			return nil // skip unreadable metadata
		}
		metaByID[id] = mf
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Second pass: one Entry per output file.
	var entries []Entry
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if !isOutputFile(name) {
			return nil
		}
		id, ok := extractID(name)
		if !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		e := Entry{
			ID:         id,
			Filename:   name,
			Path:       path,
			Size:       info.Size(),
			Model:      "unknown",
			Status:     "unknown",
			PreviewKind: classify(name),
			Mime:       mimeFromExt(name),
		}
		if mf, ok := metaByID[id]; ok {
			e.Model = orDefault(mf.Model, "unknown")
			e.Version = shortVersion(mf.Version)
			e.Status = orDefault(mf.Status, "unknown")
			e.CreatedAt = mf.CreatedAt
			e.CompletedAt = mf.CompletedAt
			e.TimeToMake = computeTime(mf)
		}
		entries = append(entries, e)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort newest first by created_at; unparseable / missing dates sink to bottom.
	sort.SliceStable(entries, func(i, j int) bool {
		ti, oki := parseTime(entries[i].CreatedAt)
		tj, okj := parseTime(entries[j].CreatedAt)
		switch {
		case oki && okj:
			return ti.After(tj)
		case oki:
			return true
		case okj:
			return false
		default:
			return entries[i].Filename < entries[j].Filename
		}
	})

	return entries, nil
}

// isOutputFile returns true for files written by replicate-safe (the
// `<id>__<idx>_<name>` pattern), and false for metadata sidecars, the
// state file, dotfiles, or anything else.
func isOutputFile(name string) bool {
	if strings.HasPrefix(name, ".") {
		return false
	}
	if strings.HasSuffix(name, ".metadata.json") {
		return false
	}
	return strings.Contains(name, idSep)
}

func extractID(name string) (string, bool) {
	idx := strings.Index(name, idSep)
	if idx <= 0 {
		return "", false
	}
	return name[:idx], true
}

type metadataFile struct {
	Model       string  `json:"model"`
	Version     string  `json:"version"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"created_at"`
	CompletedAt string  `json:"completed_at"`
	Metrics     struct {
		PredictTime *float64 `json:"predict_time"`
		TotalTime   *float64 `json:"total_time"`
	} `json:"metrics"`
}

func loadMetadata(path, id string) (*metadataFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := &metadataFile{}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, err
	}
	if m.Model == "" {
		m.Model = id
	}
	return m, nil
}

func computeTime(m *metadataFile) float64 {
	if m.Metrics.TotalTime != nil {
		return *m.Metrics.TotalTime
	}
	if m.Metrics.PredictTime != nil {
		return *m.Metrics.PredictTime
	}
	start, ok1 := parseTime(m.CreatedAt)
	end, ok2 := parseTime(m.CompletedAt)
	if ok1 && ok2 {
		return end.Sub(start).Seconds()
	}
	return 0
}

func parseTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	// Try the common RFC3339 variants Replicate uses.
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func shortVersion(v string) string {
	if len(v) <= 12 {
		return v
	}
	return v[:12]
}

func classify(name string) string {
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
	default:
		return "other"
	}
}

func mimeFromExt(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if mt := mimeByExt[ext]; mt != "" {
		return mt
	}
	return "application/octet-stream"
}

var mimeByExt = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".svg":  "image/svg+xml",
	".avif": "image/avif",
	".ico":  "image/x-icon",
	".tif":  "image/tiff",
	".tiff": "image/tiff",
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mov":  "video/quicktime",
	".m4v":  "video/mp4",
	".mkv":  "video/x-matroska",
	".avi":  "video/x-msvideo",
	".ogv":  "video/ogg",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".flac": "audio/flac",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".opus": "audio/opus",
	".txt":  "text/plain",
	".md":   "text/markdown",
	".csv":  "text/csv",
	".log":  "text/plain",
	".srt":  "application/x-subrip",
	".json": "application/json",
	".pdf":  "application/pdf",
}

// ErrNotFound is returned by ReadMetadata when no sidecar exists for an id.
var ErrNotFound = errors.New("metadata not found")

// ReadMetadata returns the raw sidecar bytes for a given prediction id.
func ReadMetadata(dir, id string) ([]byte, error) {
	if strings.ContainsAny(id, "/\\.") || id == "" {
		return nil, ErrNotFound
	}
	path := filepath.Join(dir, id+".metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return data, nil
}
