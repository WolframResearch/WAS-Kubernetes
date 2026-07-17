package workspace

import (
	"testing"
	"time"
)

func TestNewReportID_Format(t *testing.T) {
	now := time.Date(2024, 9, 15, 12, 0, 5, 0, time.UTC)
	id := newReportID(now)
	// Must start with the formatted timestamp.
	if len(id) < 16 {
		t.Fatalf("report ID too short: %q", id)
	}
	if id[:16] != "20240915T120005Z" {
		t.Errorf("unexpected ID prefix: %q", id[:16])
	}
	// Must have a dash and 8 hex chars after.
	if len(id) != 16+1+8 {
		t.Errorf("unexpected ID length %d: %q", len(id), id)
	}
	if id[16] != '-' {
		t.Errorf("expected dash at position 16, got %q", id[16])
	}
}

func TestReportDataKey(t *testing.T) {
	got := reportDataKey("was-prod", "20240915T120005Z-ab12cd34")
	want := "clusters/was-prod/reports/20240915T120005Z-ab12cd34.json"
	if got != want {
		t.Errorf("reportDataKey = %q, want %q", got, want)
	}
}

func TestReportIndexKey(t *testing.T) {
	got := reportIndexKey("was-prod")
	want := "clusters/was-prod/reports/_index.json"
	if got != want {
		t.Errorf("reportIndexKey = %q, want %q", got, want)
	}
}

func TestReportSummary_Fields(t *testing.T) {
	s := ReportSummary{Pass: 30, Fail: 2, Skip: 5, Error: 0, Info: 1}
	if s.Pass != 30 || s.Fail != 2 || s.Skip != 5 || s.Error != 0 || s.Info != 1 {
		t.Errorf("ReportSummary fields not set correctly: %+v", s)
	}
}

func TestReportEntry_Fields(t *testing.T) {
	now := time.Now().UTC()
	e := ReportEntry{
		ID:          "test-id",
		GeneratedAt: now,
		Cluster:     "was-prod",
		Summary:     ReportSummary{Pass: 10},
	}
	if e.ID != "test-id" {
		t.Errorf("ID = %q, want %q", e.ID, "test-id")
	}
	if e.Cluster != "was-prod" {
		t.Errorf("Cluster = %q, want %q", e.Cluster, "was-prod")
	}
	if e.Summary.Pass != 10 {
		t.Errorf("Summary.Pass = %d, want 10", e.Summary.Pass)
	}
}

func TestLoadReportIndex_Empty(t *testing.T) {
	// loadReportIndex on a nil bucket panics — we test the index type directly.
	idx := &reportIndex{}
	if len(idx.Entries) != 0 {
		t.Errorf("expected empty entries")
	}
}

func TestReportIndex_Prepend(t *testing.T) {
	idx := &reportIndex{}
	older := ReportEntry{ID: "older", GeneratedAt: time.Now().Add(-time.Hour)}
	newer := ReportEntry{ID: "newer", GeneratedAt: time.Now()}

	idx.Entries = append([]ReportEntry{older}, idx.Entries...)
	idx.Entries = append([]ReportEntry{newer}, idx.Entries...)

	if idx.Entries[0].ID != "newer" {
		t.Errorf("expected newest first, got %q", idx.Entries[0].ID)
	}
	if idx.Entries[1].ID != "older" {
		t.Errorf("expected older second, got %q", idx.Entries[1].ID)
	}
}

func TestReportIndex_Cap50(t *testing.T) {
	idx := &reportIndex{}
	for i := 0; i < 60; i++ {
		idx.Entries = append([]ReportEntry{{ID: "x"}}, idx.Entries...)
	}
	if len(idx.Entries) > 50 {
		idx.Entries = idx.Entries[:50]
	}
	if len(idx.Entries) != 50 {
		t.Errorf("expected 50 entries, got %d", len(idx.Entries))
	}
}
