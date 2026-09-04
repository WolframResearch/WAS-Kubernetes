package doctor_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

func sampleReport() *doctor.Report {
	return &doctor.Report{
		GeneratedAt:   time.Now(),
		Cluster:       "test-cluster",
		WasctlVersion: "0.1.0",
		Elapsed:       2*time.Second + 300*time.Millisecond,
		Summary:       doctor.Summary{Pass: 2, Fail: 1, Skip: 1},
		Findings: []doctor.Finding{
			{
				CheckID:     "aws.credentials",
				CheckName:   "AWS credentials valid and unexpired",
				Severity:    doctor.SeverityCritical,
				Status:      doctor.StatusFail,
				Message:     "credentials expired",
				Remediation: "aws sso login",
				Elapsed:     45 * time.Millisecond,
			},
			{
				CheckID:   "aws.region",
				CheckName: "AWS region exists",
				Severity:  doctor.SeverityInfo,
				Status:    doctor.StatusPass,
				Message:   "us-east-1 confirmed",
				Elapsed:   12 * time.Millisecond,
			},
			{
				CheckID:   "cluster.reachable",
				CheckName: "kubectl cluster-info succeeds",
				Severity:  doctor.SeverityInfo,
				Status:    doctor.StatusSkip,
				Message:   "no cluster reachable",
				Elapsed:   1 * time.Millisecond,
			},
			{
				CheckID:   "app.awes",
				CheckName: "AWES pods running and ready",
				Severity:  doctor.SeverityInfo,
				Status:    doctor.StatusPass,
				Message:   "awes: 2/2 ready",
				Elapsed:   200 * time.Millisecond,
			},
		},
	}
}

func TestTextRenderer_Render(t *testing.T) {
	var buf bytes.Buffer
	r := doctor.NewTextRenderer(&buf)
	r.Render(sampleReport(), false)

	out := buf.String()
	if !strings.Contains(out, "Environment") {
		t.Error("expected 'Environment' header")
	}
	if !strings.Contains(out, "Cluster") {
		t.Error("expected 'Cluster' header")
	}
	if !strings.Contains(out, "Application") {
		t.Error("expected 'Application' header")
	}
	if !strings.Contains(out, "Summary") {
		t.Error("expected 'Summary' line")
	}
	if !strings.Contains(out, "credentials expired") {
		t.Error("expected failure message in output")
	}
}

func TestTextRenderer_Verbose(t *testing.T) {
	var buf bytes.Buffer
	r := doctor.NewTextRenderer(&buf)
	r.Render(sampleReport(), true)

	out := buf.String()
	if !strings.Contains(out, "aws sso login") {
		t.Error("expected remediation in verbose output")
	}
}

func TestJSONRenderer_Render(t *testing.T) {
	var buf bytes.Buffer
	r := doctor.NewJSONRenderer(&buf)
	if err := r.Render(sampleReport()); err != nil {
		t.Fatalf("render failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if out["schemaVersion"] != "1" {
		t.Errorf("expected schemaVersion '1', got %v", out["schemaVersion"])
	}
	if out["cluster"] != "test-cluster" {
		t.Errorf("expected cluster 'test-cluster', got %v", out["cluster"])
	}
	findings, ok := out["findings"].([]interface{})
	if !ok || len(findings) == 0 {
		t.Error("expected non-empty findings array")
	}
}

func TestMarkdownRenderer_Render(t *testing.T) {
	var buf bytes.Buffer
	r := doctor.NewMarkdownRenderer(&buf)
	r.Render(sampleReport())

	out := buf.String()
	if !strings.HasPrefix(out, "## wasctl doctor") {
		t.Error("expected markdown heading")
	}
	if !strings.Contains(out, "| Status |") {
		t.Error("expected markdown table header")
	}
	if !strings.Contains(out, "Summary") {
		t.Error("expected Summary section")
	}
}

func TestSanitize_MasksAccountID(t *testing.T) {
	var buf bytes.Buffer
	r := doctor.NewMarkdownRenderer(&buf)
	rep := sampleReport()
	rep.Findings[0].Message = "account 123456789012 is denied"
	r.Render(rep)

	out := buf.String()
	if strings.Contains(out, "123456789012") {
		t.Error("expected account ID to be masked in markdown output")
	}
	if !strings.Contains(out, "1234****9012") {
		t.Error("expected masked account ID pattern '1234****9012'")
	}
}

func TestFmtElapsed_SubSecond(t *testing.T) {
	// Test via text render: check elapsed format in output
	var buf bytes.Buffer
	r := doctor.NewTextRenderer(&buf)
	rep := &doctor.Report{
		Cluster:       "x",
		WasctlVersion: "0",
		Elapsed:       250 * time.Millisecond,
		Findings: []doctor.Finding{
			{CheckID: "aws.region", CheckName: "n", Status: doctor.StatusPass, Elapsed: 45 * time.Millisecond},
		},
	}
	r.Render(rep, false)
	out := buf.String()
	if !strings.Contains(out, "ms") {
		t.Errorf("expected 'ms' elapsed format for sub-second timing, got:\n%s", out)
	}
}
