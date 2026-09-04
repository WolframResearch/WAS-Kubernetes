package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

// ── formatAge ─────────────────────────────────────────────────────────────────

func TestFormatAge_JustNow(t *testing.T) {
	got := formatAge(time.Now().Add(-5 * time.Second))
	if got != "just now" {
		t.Errorf("want %q, got %q", "just now", got)
	}
}

func TestFormatAge_Minutes(t *testing.T) {
	got := formatAge(time.Now().Add(-90 * time.Second))
	if got != "1m ago" {
		t.Errorf("want %q, got %q", "1m ago", got)
	}
}

func TestFormatAge_Hours(t *testing.T) {
	got := formatAge(time.Now().Add(-2 * time.Hour))
	if got != "2h ago" {
		t.Errorf("want %q, got %q", "2h ago", got)
	}
}

func TestFormatAge_Days(t *testing.T) {
	got := formatAge(time.Now().Add(-49 * time.Hour))
	if got != "2d ago" {
		t.Errorf("want %q, got %q", "2d ago", got)
	}
}

// ── formatIssueCount ──────────────────────────────────────────────────────────

func TestFormatIssueCount(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{2, "2 problems"},
		{10, "10 problems"},
	}
	for _, tc := range cases {
		got := formatIssueCount(tc.n)
		if got != tc.want {
			t.Errorf("formatIssueCount(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// ── categoryFromID ────────────────────────────────────────────────────────────

func TestCategoryFromID(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"aws.credentials", "AWS"},
		{"k8s.pods", "Cluster"},
		{"app.chart", "Application"},
		{"was.ingress", "Application"},
		{"kafka.cluster", "Application"},
		{"version.matrix", "Environment"},
		{"helm.version", "Environment"},
		{"noDot", "Environment"},
	}
	for _, tc := range cases {
		got := categoryFromID(tc.id)
		if got != tc.want {
			t.Errorf("categoryFromID(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

// ── groupFindings ─────────────────────────────────────────────────────────────

func TestGroupFindings_Empty(t *testing.T) {
	groups := groupFindings(nil, "aws")
	if len(groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(groups))
	}
}

func TestGroupFindings_SingleCategory(t *testing.T) {
	findings := []doctor.Finding{
		{CheckID: "aws.creds", CheckName: "AWS credentials"},
		{CheckID: "aws.region", CheckName: "AWS region"},
	}
	groups := groupFindings(findings, "aws")
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0].Findings) != 2 {
		t.Errorf("expected 2 findings in group, got %d", len(groups[0].Findings))
	}
	if groups[0].Category != "AWS" {
		t.Errorf("expected category AWS, got %q", groups[0].Category)
	}
}

func TestGroupFindings_MultipleCategories(t *testing.T) {
	findings := []doctor.Finding{
		{CheckID: "aws.creds", CheckName: "AWS credentials"},
		{CheckID: "k8s.api", CheckName: "K8s API"},
		{CheckID: "aws.region", CheckName: "AWS region"},
	}
	groups := groupFindings(findings, "aws")
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	// First group should be AWS (first encountered).
	if groups[0].Category != "AWS" {
		t.Errorf("first group should be AWS, got %q", groups[0].Category)
	}
	// Second group should be Cluster.
	if groups[1].Category != "Cluster" {
		t.Errorf("second group should be Cluster, got %q", groups[1].Category)
	}
}

func TestGroupFindings_FiltersWrongCloud(t *testing.T) {
	findings := []doctor.Finding{
		{CheckID: "aws.credentials", CheckName: "AWS credentials"},
		{CheckID: "azure.credentials", CheckName: "Azure credentials"},
		{CheckID: "cluster.reachable", CheckName: "Cluster reachable"},
	}
	groups := groupFindings(findings, "azure")
	var ids []string
	for _, g := range groups {
		for _, f := range g.Findings {
			ids = append(ids, f.CheckID)
		}
	}
	for _, id := range ids {
		if strings.HasPrefix(id, "aws.") {
			t.Errorf("azure view should not include %s", id)
		}
	}
	foundAzure, foundCluster := false, false
	for _, id := range ids {
		if id == "azure.credentials" {
			foundAzure = true
		}
		if id == "cluster.reachable" {
			foundCluster = true
		}
	}
	if !foundAzure || !foundCluster {
		t.Errorf("expected azure + cluster findings, got %v", ids)
	}
}

// ── findingClassFn ────────────────────────────────────────────────────────────

func TestFindingClassFnInternal_All(t *testing.T) {
	cases := []struct {
		status doctor.Status
		sev    doctor.Severity
		want   string
	}{
		{doctor.StatusPass, doctor.SeverityInfo, "pass"},
		{doctor.StatusSkip, doctor.SeverityInfo, "skip"},
		{doctor.StatusInfo, doctor.SeverityInfo, "info"},
		{doctor.StatusError, doctor.SeverityInfo, "error"},
		{doctor.StatusFail, doctor.SeverityCritical, "critical"},
		{doctor.StatusFail, doctor.SeverityProblem, "problem"},
		{doctor.StatusFail, doctor.SeverityWarning, "warning"},
		{doctor.StatusFail, doctor.SeverityInfo, "fail"},
	}
	for _, tc := range cases {
		got := findingClassFn(tc.status, tc.sev)
		if got != tc.want {
			t.Errorf("findingClassFn(%d,%d) = %q, want %q", tc.status, tc.sev, got, tc.want)
		}
	}
}

// ── opsStatusClass ────────────────────────────────────────────────────────────

func TestOpsStatusClass(t *testing.T) {
	cases := []struct {
		failCount int
		wantClass string
		wantLabel string
	}{
		{0, "status-healthy", "Healthy"},
		{1, "status-problem", "1 problem"},
		{3, "status-problem", "3 problems"},
	}
	for _, c := range cases {
		cls, lbl := opsStatusClass(c.failCount)
		if cls != c.wantClass || lbl != c.wantLabel {
			t.Errorf("opsStatusClass(%d) = (%q, %q), want (%q, %q)",
				c.failCount, cls, lbl, c.wantClass, c.wantLabel)
		}
	}
}

func TestFmtElapsedFnInternal(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{250 * time.Millisecond, "250ms"},
		{999 * time.Millisecond, "999ms"},
		{1000 * time.Millisecond, "1.0s"},
		{2100 * time.Millisecond, "2.1s"},
	}
	for _, tc := range cases {
		got := fmtElapsedFn(tc.d)
		if got != tc.want {
			t.Errorf("fmtElapsedFn(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
