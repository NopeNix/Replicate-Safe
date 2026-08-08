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

const idSep = "__" // separator written by the backend between prediction id and index

// Entry is one row in the listing. One per output file on disk.
type Entry struct {
	ID          string  `json:"id"`
	Filename    string  `json:"filename"`     // basename as written on disk
	Path        string  `json:"-"`            // full path, never sent to client
	Size        int64   `json:"size"`         // bytes
	Model       string  `json:"model"`
	Version     string  `json:"version"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"created_at"`
	CompletedAt string  `json:"completed_at"`
	TimeToMake  float64 `json:"time_to_make"`
	Mime        string  `json:"mime"`
	PreviewKind string  `json:"preview_kind"` // image | video | audio | text | other
	MetaPath    string  `json:"-"`            // path to <id>.metadata.json
}

// Load walks dir and returns one Entry per output file. Metadata sidecars
// are loaded and joined; missing metadata is non-fatal.
//
// If query is non-empty, only predictions whose metadata.json contains the
// query (case-insensitive substring) are returned. The metadata file is the
// raw JSON of the Replicate prediction response, so this matches against
// inputs (e.g. the user's prompt), the model name, the version, output URLs,
// etc. Output files without a metadata sidecar are excluded when query is
// set; otherwise all output files are included.
func Load(dir, query string) ([]Entry, error) {
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	query = strings.ToLower(strings.TrimSpace(query))

	// First pass: load every metadata sidecar keyed by id.
	metaByID := make(map[string]*metadataFile)
	type metaWithPath struct {
		mf   *metadataFile
		path string
		data []byte
	}
	metaRaw := make(map[string]*metaWithPath)
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".metadata.json") {
			return nil
		}
		id := strings.TrimSuffix(name, ".metadata.json")
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		mf, err := loadMetadata(data, id)
		if err != nil {
			return nil
		}
		metaByID[id] = mf
		metaRaw[id] = &metaWithPath{mf: mf, path: path, data: data}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Pre-compute matching id set when a query is set.
	var matchIDs map[string]bool
	if query != "" {
		matchIDs = make(map[string]bool)
		for id, mr := range metaRaw {
			if strings.Contains(strings.ToLower(string(mr.data)), query) {
				matchIDs[id] = true
			}
		}
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
		if query != "" && !matchIDs[id] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		e := Entry{
			ID:          id,
			Filename:    name,
			Path:        path,
			Size:        info.Size(),
			Model:       "unknown",
			Status:      "unknown",
			PreviewKind: classify(name),
			Mime:        mimeFromExt(name),
		}
		if mr, ok := metaRaw[id]; ok {
			e.MetaPath = mr.path
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

	// Default sort: newest first by created_at; missing dates sink to bottom.
	sortBy(entries, "created_at", "desc")
	return entries, nil
}

// SortField identifies a sortable column. Empty = default (created_at desc).
type SortField string

const (
	SortByCreated SortField = "created_at"
	SortByName    SortField = "filename"
	SortByModel   SortField = "model"
	SortByStatus  SortField = "status"
	SortByTime    SortField = "time_to_make"
	SortBySize    SortField = "size"
)

// SortDir is "asc" or "desc".
type SortDir string

const (
	Asc  SortDir = "asc"
	Desc SortDir = "desc"
)

// sortBy sorts entries in place by the given field and direction.
func sortBy(entries []Entry, field SortField, dir SortDir) {
	asc := dir == Asc
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		var less bool
		switch field {
		case SortByName:
			less = strings.ToLower(a.Filename) < strings.ToLower(b.Filename)
		case SortByModel:
			less = strings.ToLower(a.Model) < strings.ToLower(b.Model)
		case SortByStatus:
			less = strings.ToLower(a.Status) < strings.ToLower(b.Status)
		case SortByTime:
			less = a.TimeToMake < b.TimeToMake
		case SortBySize:
			less = a.Size < b.Size
		case SortByCreated, "":
			ai, oki := parseTime(a.CreatedAt)
			bi, okj := parseTime(b.CreatedAt)
			switch {
			case oki && okj:
				less = ai.Before(bi)
			case oki:
				less = false
			case okj:
				less = true
			default:
				less = strings.ToLower(a.Filename) < strings.ToLower(b.Filename)
			}
		default:
			less = false
		}
		if !asc {
			less = !less
		}
		return less
	})
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

func loadMetadata(data []byte, id string) (*metadataFile, error) {
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

// FirstOutputFor returns the first output file path (basename) for the given
// prediction id. Returns "" if no output file exists.
func FirstOutputFor(dir, id string) (string, error) {
	if strings.ContainsAny(id, "/\\.") || id == "" {
		return "", ErrNotFound
	}
	matches, err := filepath.Glob(filepath.Join(dir, id+"__*"))
	if err != nil {
		return "", err
	}
	for _, m := range matches {
		base := filepath.Base(m)
		if strings.Contains(base, "__") && !strings.HasSuffix(base, ".metadata.json") {
			return base, nil
		}
	}
	return "", ErrNotFound
}
