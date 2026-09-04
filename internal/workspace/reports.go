package workspace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metacontainer"
)

// ReportSummary holds per-status counts for a doctor run. It mirrors
// doctor.Summary without importing the doctor package to avoid circular imports.
type ReportSummary struct {
	Pass  int `json:"pass"`
	Fail  int `json:"fail"`
	Skip  int `json:"skip"`
	Error int `json:"error"`
	Info  int `json:"info"`
}

// ReportEntry is one row in the report index — enough to render the history list.
type ReportEntry struct {
	ID          string        `json:"id"`
	GeneratedAt time.Time     `json:"generated_at"`
	Cluster     string        `json:"cluster"`
	Summary     ReportSummary `json:"summary"`
}

type reportIndex struct {
	Entries []ReportEntry `json:"entries"`
}

// reportStore is the minimal interface for storing doctor reports.
// Both *metabucket.Bucket and *metacontainer.Container satisfy it.
type reportStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, data []byte) error
}

func reportDataKey(clusterName, id string) string {
	return "clusters/" + clusterName + "/reports/" + id + ".json"
}

func reportIndexKey(clusterName string) string {
	return "clusters/" + clusterName + "/reports/_index.json"
}

func newReportID(t time.Time) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return t.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b)
}

// SaveReport stores report JSON to the meta store and prepends a new entry to
// the index (newest first, capped at 50). Returns the assigned report ID.
func SaveReport(ctx context.Context, s reportStore, clusterName string, data []byte, summary ReportSummary) (string, error) {
	now := time.Now().UTC()
	id := newReportID(now)

	if err := s.Put(ctx, reportDataKey(clusterName, id), data); err != nil {
		return "", fmt.Errorf("save report data: %w", err)
	}

	entry := ReportEntry{
		ID:          id,
		GeneratedAt: now,
		Cluster:     clusterName,
		Summary:     summary,
	}

	idx, err := loadReportIndex(ctx, s, clusterName)
	if err != nil {
		idx = &reportIndex{}
	}

	idx.Entries = append([]ReportEntry{entry}, idx.Entries...)
	if len(idx.Entries) > 50 {
		idx.Entries = idx.Entries[:50]
	}

	idxJSON, err := json.Marshal(idx)
	if err != nil {
		return id, fmt.Errorf("marshal report index: %w", err)
	}
	if err := s.Put(ctx, reportIndexKey(clusterName), idxJSON); err != nil {
		return id, fmt.Errorf("save report index: %w", err)
	}
	return id, nil
}

// LoadReport retrieves a report's raw JSON bytes by ID.
func LoadReport(ctx context.Context, s reportStore, clusterName, id string) ([]byte, error) {
	data, err := s.Get(ctx, reportDataKey(clusterName, id))
	if err != nil {
		return nil, fmt.Errorf("load report %s: %w", id, err)
	}
	return data, nil
}

// ListReportEntries returns all index entries for a cluster, newest first.
// Returns an empty slice (not an error) if no reports exist yet.
func ListReportEntries(ctx context.Context, s reportStore, clusterName string) ([]ReportEntry, error) {
	idx, err := loadReportIndex(ctx, s, clusterName)
	if err != nil {
		return nil, err
	}
	return idx.Entries, nil
}

func loadReportIndex(ctx context.Context, s reportStore, clusterName string) (*reportIndex, error) {
	data, err := s.Get(ctx, reportIndexKey(clusterName))
	if err != nil {
		var mb *metabucket.ErrNotFound
		var mc *metacontainer.ErrNotFound
		if errors.As(err, &mb) || errors.As(err, &mc) {
			return &reportIndex{}, nil
		}
		return nil, fmt.Errorf("load report index: %w", err)
	}
	var idx reportIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("unmarshal report index: %w", err)
	}
	return &idx, nil
}
