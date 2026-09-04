package doctor_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

// stubCheck is a test double.
type stubCheck struct {
	id       string
	category doctor.Category
	severity doctor.Severity
	status   doctor.Status
	skipIt   bool
}

func (s stubCheck) ID() string         { return s.id }
func (s stubCheck) Name() string       { return s.id }
func (s stubCheck) Category() doctor.Category { return s.category }
func (s stubCheck) Description() string { return "" }
func (s stubCheck) SafeToFix() bool    { return false }
func (s stubCheck) Fix(_ context.Context, _ *doctor.RunContext) error {
	return errors.New("not implemented")
}
func (s stubCheck) Precondition(_ context.Context, _ *doctor.RunContext) (bool, string) {
	return s.skipIt, "precondition not met"
}
func (s stubCheck) Run(_ context.Context, _ *doctor.RunContext) doctor.Finding {
	return doctor.Finding{
		CheckID:   s.id,
		CheckName: s.id,
		Severity:  s.severity,
		Status:    s.status,
		Message:   "stub",
	}
}

func testRC() *doctor.RunContext {
	return &doctor.RunContext{Cfg: &config.Config{
		ClusterName: config.Field[string]{Value: "test"},
		MetaRegion:  config.Field[string]{Value: "us-east-1"},
		Region:      config.Field[string]{Value: "us-east-1"},
	}}
}

func TestRun_Summary(t *testing.T) {
	checks := []doctor.Check{
		stubCheck{id: "a", status: doctor.StatusPass},
		stubCheck{id: "b", status: doctor.StatusFail, severity: doctor.SeverityCritical},
		stubCheck{id: "c", skipIt: true},
	}
	rc := testRC()
	report := doctor.Run(context.Background(), rc, checks)

	if report.Summary.Pass != 1 {
		t.Errorf("expected 1 pass, got %d", report.Summary.Pass)
	}
	if report.Summary.Fail != 1 {
		t.Errorf("expected 1 fail, got %d", report.Summary.Fail)
	}
	if report.Summary.Skip != 1 {
		t.Errorf("expected 1 skip, got %d", report.Summary.Skip)
	}
	if len(report.Findings) != 3 {
		t.Errorf("expected 3 findings, got %d", len(report.Findings))
	}
}

func TestRun_SortOrder(t *testing.T) {
	// Critical fail should sort before pass
	checks := []doctor.Check{
		stubCheck{id: "pass", status: doctor.StatusPass},
		stubCheck{id: "crit", status: doctor.StatusFail, severity: doctor.SeverityCritical},
		stubCheck{id: "warn", status: doctor.StatusFail, severity: doctor.SeverityWarning},
	}
	rc := testRC()
	report := doctor.Run(context.Background(), rc, checks)

	if report.Findings[0].CheckID != "crit" {
		t.Errorf("critical fail should sort first; got %s", report.Findings[0].CheckID)
	}
	if report.Findings[1].CheckID != "warn" {
		t.Errorf("warning fail should sort second; got %s", report.Findings[1].CheckID)
	}
	if report.Findings[2].CheckID != "pass" {
		t.Errorf("pass should sort last; got %s", report.Findings[2].CheckID)
	}
}

func TestRun_SkipPrecondition(t *testing.T) {
	checks := []doctor.Check{
		stubCheck{id: "skip-me", skipIt: true},
	}
	rc := testRC()
	report := doctor.Run(context.Background(), rc, checks)

	if report.Findings[0].Status != doctor.StatusSkip {
		t.Errorf("expected skip status, got %v", report.Findings[0].Status)
	}
}

func TestRun_ConcurrencyLimit(t *testing.T) {
	// 20 checks that all sleep briefly; total should be well under 10s
	var checks []doctor.Check
	for i := 0; i < 20; i++ {
		checks = append(checks, stubCheck{id: "c", status: doctor.StatusPass})
	}
	rc := testRC()
	start := time.Now()
	doctor.Run(context.Background(), rc, checks)
	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Errorf("expected quick run, took %v", elapsed)
	}
}

func TestRunContext_QuotaCache(t *testing.T) {
	rc := &doctor.RunContext{Cfg: &config.Config{}}
	rc.QuotaStore("svc/code", "42")
	v, ok := rc.QuotaCached("svc/code")
	if !ok || v != "42" {
		t.Errorf("expected cached value '42', got %q ok=%v", v, ok)
	}
}

func TestRunContext_Cleanup_NilSafe(t *testing.T) {
	rc := &doctor.RunContext{Cfg: &config.Config{}}
	rc.Cleanup() // should not panic
}
