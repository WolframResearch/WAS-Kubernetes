package versions

import (
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/versions/parsers"
)

func makeVersion(t *testing.T, s string) Version {
	t.Helper()
	v, err := parsers.Parse(s)
	if err != nil {
		t.Fatalf("parsers.Parse(%q): %v", s, err)
	}
	return v
}

// ── CheckCompatibility ────────────────────────────────────────────────────────

func TestCheckCompatibility_AllInRange(t *testing.T) {
	current := map[string]Version{
		"helm":                     makeVersion(t, "3.15.0"),
		"kubectl":                  makeVersion(t, "1.30.0"),
		"terraform":                makeVersion(t, "1.9.5"),
		"aws-cli":                  makeVersion(t, "2.15.0"),
		"kubernetes":               makeVersion(t, "1.31.0"),
		"strimzi":                  makeVersion(t, "0.44.0"),
		"chart":                    makeVersion(t, "1.0.0"),
		"terraform-aws-provider":   makeVersion(t, "5.60.0"),
		"terraform-azure-provider": makeVersion(t, "3.95.0"),
	}
	issues := CheckCompatibility(CurrentMatrix, current)
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %d: %+v", len(issues), issues)
	}
}

func TestCheckCompatibility_BelowMin_Critical(t *testing.T) {
	current := map[string]Version{
		"helm": makeVersion(t, "3.9.0"), // below min 3.10.0
	}
	issues := CheckCompatibility(CurrentMatrix, current)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Severity != IssueSeverityCritical {
		t.Errorf("expected Critical, got %v", issues[0].Severity)
	}
	if issues[0].Component != "helm" {
		t.Errorf("expected component=helm, got %q", issues[0].Component)
	}
}

func TestCheckCompatibility_AboveMax_Warning(t *testing.T) {
	current := map[string]Version{
		"helm": makeVersion(t, "4.0.0"), // above max 3.17.99
	}
	issues := CheckCompatibility(CurrentMatrix, current)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Severity != IssueSeverityWarning {
		t.Errorf("expected Warning, got %v", issues[0].Severity)
	}
}

func TestCheckCompatibility_PreRelease_Critical(t *testing.T) {
	// v1.30.0-rc.1 < v1.30.0 (the min) → Critical
	current := map[string]Version{
		"kubernetes": makeVersion(t, "v1.30.0-rc.1"),
	}
	issues := CheckCompatibility(CurrentMatrix, current)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Severity != IssueSeverityCritical {
		t.Errorf("expected Critical for pre-release, got %v", issues[0].Severity)
	}
}

func TestCheckCompatibility_SkipsAbsentComponents(t *testing.T) {
	// Only provide azure-cli; everything else absent — no issues for absent tools
	current := map[string]Version{
		"azure-cli": makeVersion(t, "2.60.0"),
	}
	issues := CheckCompatibility(CurrentMatrix, current)
	if len(issues) != 0 {
		t.Fatalf("expected no issues for absent tools, got %d", len(issues))
	}
}

func TestCheckCompatibility_MultipleIssues(t *testing.T) {
	current := map[string]Version{
		"helm":      makeVersion(t, "3.5.0"),  // below min → Critical
		"terraform": makeVersion(t, "2.0.0"),  // above max → Warning
	}
	issues := CheckCompatibility(CurrentMatrix, current)
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
}

func TestCheckCompatibility_RemedationNotEmpty(t *testing.T) {
	current := map[string]Version{
		"strimzi": makeVersion(t, "0.30.0"), // below min 0.40.0
	}
	issues := CheckCompatibility(CurrentMatrix, current)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue")
	}
	if issues[0].Remediation == "" {
		t.Error("Remediation should not be empty")
	}
}

func TestCheckCompatibility_EmptyCurrent(t *testing.T) {
	issues := CheckCompatibility(CurrentMatrix, nil)
	if len(issues) != 0 {
		t.Fatalf("expected no issues for empty current map, got %d", len(issues))
	}
}

func TestCheckCompatibility_ChartBelowMin_Critical(t *testing.T) {
	current := map[string]Version{
		"chart": makeVersion(t, "0.0.9"), // below min 0.1.0
	}
	issues := CheckCompatibility(CurrentMatrix, current)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Severity != IssueSeverityCritical {
		t.Errorf("expected Critical for old chart, got %v", issues[0].Severity)
	}
}

func TestCheckCompatibility_AWSProviderBelowMin_Critical(t *testing.T) {
	current := map[string]Version{
		"terraform-aws-provider": makeVersion(t, "5.40.0"), // below min 5.50.0
	}
	issues := CheckCompatibility(CurrentMatrix, current)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Severity != IssueSeverityCritical {
		t.Errorf("expected Critical for old AWS provider, got %v", issues[0].Severity)
	}
}

func TestCheckCompatibility_StrimziTightRange(t *testing.T) {
	// Strimzi is pinned to 0.44.x–0.49.99; 0.50.0 is outside the range.
	current := map[string]Version{
		"strimzi": makeVersion(t, "0.50.0"),
	}
	issues := CheckCompatibility(CurrentMatrix, current)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue for strimzi 0.50.0, got %d", len(issues))
	}
	if issues[0].Severity != IssueSeverityWarning {
		t.Errorf("expected Warning for strimzi above max, got %v", issues[0].Severity)
	}
}

func TestCheckCompatibility_StrimziExactMin(t *testing.T) {
	// 0.44.0 is the minimum supported version (0.43.x crashes on K8s 1.33+).
	current := map[string]Version{
		"strimzi": makeVersion(t, "0.44.0"),
	}
	issues := CheckCompatibility(CurrentMatrix, current)
	if len(issues) != 0 {
		t.Fatalf("expected no issues for strimzi 0.44.0, got %d: %+v", len(issues), issues)
	}
}

// ── Check() smoke test ────────────────────────────────────────────────────────

func TestCheck_NoError(t *testing.T) {
	// On machines without the tools, parsers will fail and Check returns 0 issues.
	// The important thing is it never returns an error.
	_, err := Check()
	if err != nil {
		t.Fatalf("Check() returned unexpected error: %v", err)
	}
}

// ── IssueSeverity values ──────────────────────────────────────────────────────

func TestIssueSeverity_Values(t *testing.T) {
	if IssueSeverityWarning >= IssueSeverityCritical {
		t.Error("IssueSeverityWarning should be < IssueSeverityCritical")
	}
}
