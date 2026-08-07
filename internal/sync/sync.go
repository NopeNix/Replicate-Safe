package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/replicate/replicate-go"

	"github.com/phil/Replicate-Safe/internal/download"
	"github.com/phil/Replicate-Safe/internal/state"
)

const (
	statusSucceeded = "succeeded"
)

type Runner struct {
	Client    *replicate.Client
	State     *state.State
	Download  *download.Downloader
	OutputDir string
	WriteMeta bool
	Log       *slog.Logger
}

func New(client *replicate.Client, st *state.State, dl *download.Downloader, outDir string, writeMeta bool, log *slog.Logger) *Runner {
	return &Runner{
		Client:    client,
		State:     st,
		Download:  dl,
		OutputDir: outDir,
		WriteMeta: writeMeta,
		Log:       log,
	}
}

// RunOnce performs a single sync pass. Returns the number of predictions
// processed (downloaded or skipped-as-seen).
func (r *Runner) RunOnce(ctx context.Context) (int, error) {
	page, err := r.Client.ListPredictions(ctx)
	if err != nil {
		return 0, fmt.Errorf("list predictions: %w", err)
	}

	processed := 0
	batches, errs := replicate.Paginate(ctx, r.Client, page)
	for batches != nil || errs != nil {
		select {
		case results, ok := <-batches:
			if !ok {
				batches = nil
				continue
			}
			for _, p := range results {
				if err := r.handlePrediction(ctx, p); err != nil {
					r.Log.Error("prediction failed", "id", p.ID, "err", err)
					continue
				}
				processed++
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				return processed, fmt.Errorf("pagination: %w", err)
			}
		}
	}
	return processed, nil
}

// handlePrediction processes one prediction: download outputs + write metadata.
func (r *Runner) handlePrediction(ctx context.Context, p replicate.Prediction) error {
	if r.State.HasSeen(p.ID) {
		r.Log.Debug("already seen", "id", p.ID)
		return nil
	}

	createdAt, _ := time.Parse(time.RFC3339Nano, p.CreatedAt)
	if string(p.Status) != statusSucceeded {
		r.Log.Info("skip non-succeeded",
			"id", p.ID, "status", string(p.Status), "model", p.Model)
		r.State.MarkSeen(p.ID, createdAt)
		return nil
	}

	urls := extractOutputURLs(p.Output)
	if len(urls) == 0 {
		r.Log.Info("no output urls", "id", p.ID, "model", p.Model)
		r.State.MarkSeen(p.ID, createdAt)
		return nil
	}

	for idx, u := range urls {
		filename := download.FilenameFromURL(u, "")
		outName := fmt.Sprintf("%s__%02d_%s", p.ID, idx, filename)
		outPath := filepath.Join(r.OutputDir, outName)
		ok, err := r.Download.Download(ctx, u, outPath)
		if err != nil {
			// Best-effort: log and keep going. State is still updated so we
			// don't loop forever on expired outputs.
			r.Log.Warn("download failed",
				"id", p.ID, "url", u, "err", err)
			continue
		}
		if ok {
			r.Log.Info("downloaded", "id", p.ID, "path", outName)
		}
	}

	if r.WriteMeta {
		if err := r.writeMetadata(p); err != nil {
			r.Log.Warn("metadata write failed", "id", p.ID, "err", err)
		}
	}

	r.State.MarkSeen(p.ID, createdAt)
	return nil
}

func (r *Runner) writeMetadata(p replicate.Prediction) error {
	meta := map[string]any{
		"id":           p.ID,
		"model":        p.Model,
		"version":      p.Version,
		"status":       string(p.Status),
		"source":       string(p.Source),
		"created_at":   p.CreatedAt,
		"started_at":   p.StartedAt,
		"completed_at": p.CompletedAt,
		"input":        p.Input,
		"output":       p.Output,
		"metrics":      p.Metrics,
		"urls":         p.URLs,
	}
	if raw := p.RawJSON(); len(raw) > 0 {
		meta["raw"] = json.RawMessage(raw)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(r.OutputDir, p.ID+".metadata.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// extractOutputURLs flattens a Prediction's output (which may be a string,
// []any of strings, or a map of string->any) into a slice of http(s) URLs.
func extractOutputURLs(out replicate.PredictionOutput) []string {
	if out == nil {
		return nil
	}
	var urls []string
	switch v := out.(type) {
	case string:
		urls = append(urls, v)
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				urls = append(urls, s)
			}
		}
	case map[string]any:
		for _, val := range v {
			switch vv := val.(type) {
			case string:
				urls = append(urls, vv)
			case []any:
				for _, item := range vv {
					if s, ok := item.(string); ok {
						urls = append(urls, s)
					}
				}
			}
		}
	}
	return urls
}
