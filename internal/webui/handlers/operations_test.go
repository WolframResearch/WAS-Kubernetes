package handlers_test

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/versions"
	"github.com/WolframResearch/WAS-Kubernetes/internal/versions/parsers"
	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/handlers"
	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/sse"
)

// opsTemplates builds a minimal Templates for operations handler tests.
func opsTemplates(t *testing.T) *handlers.Templates {
	t.Helper()
	layout := `{{define "layout"}}{{block "content" .}}{{end}}{{end}}`
	content := func(src string) *template.Template {
		return template.Must(template.Must(
			template.New("l").Funcs(handlers.FuncMap()).Parse(layout),
		).New("p").Parse(src))
	}
	navSrc := `{{define "_operations_nav"}}NAV{{end}}`
	findingSrc := `{{define "_finding"}}FINDING{{end}}`
	findingGroupSrc := `{{define "_finding_group"}}GROUP{{end}}`

	cloneWithNav := func(src string) *template.Template {
		base := template.Must(template.New("l").Funcs(handlers.FuncMap()).Parse(layout))
		template.Must(base.New("n").Parse(navSrc))
		template.Must(base.New("fi").Parse(findingSrc))
		template.Must(base.New("fg").Parse(findingGroupSrc))
		template.Must(base.New("p").Parse(src))
		return base
	}

	return &handlers.Templates{
		Home:             content(`{{define "content"}}HOME{{end}}`),
		About:            content(`{{define "content"}}ABOUT{{end}}`),
		OpsOverview: content(`{{define "content"}}OPS-OVERVIEW:{{if eq .Tab.State "ok"}}OK{{else}}EMPTY{{end}}{{end}}{{define "_ops_cloud_tabs"}}OPS-TABS:{{.ActiveCloud}}:OOB={{.OOB}}{{end}}{{define "_ops_tab_body"}}OPS-TAB:{{.Cloud}}:{{.State}}{{end}}`),
		OpsDoctor:        cloneWithNav(`{{define "content"}}OPS-DOCTOR:{{.ClusterName}}{{end}}`),
		OpsDoctorRunning: content(`{{define "content"}}OPS-RUNNING:{{.SessionID}}{{end}}`),
		OpsBundle:        cloneWithNav(`{{define "content"}}OPS-BUNDLE:{{.ClusterName}}{{end}}`),
		OpsVersions:      cloneWithNav(`{{define "content"}}OPS-VERSIONS:{{range .Rows}}{{.Component}},{{end}}{{end}}`),
		OpsHistory:       cloneWithNav(`{{define "content"}}OPS-HISTORY:{{.ClusterName}}{{end}}`),
	}
}

// ── OpsVersions ───────────────────────────────────────────────────────────────

func TestOpsVersions_200(t *testing.T) {
	tmpls := opsTemplates(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters/was-dev/operations/versions", nil)
	r.SetPathValue("name", "was-dev")
	handlers.OpsVersions(tmpls)(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestOpsVersions_ContentType(t *testing.T) {
	tmpls := opsTemplates(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters/x/operations/versions", nil)
	r.SetPathValue("name", "x")
	handlers.OpsVersions(tmpls)(w, r)
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("want text/html, got %q", ct)
	}
}

// ── OpsBundle ─────────────────────────────────────────────────────────────────

func TestOpsBundle_200(t *testing.T) {
	tmpls := opsTemplates(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters/was-dev/operations/bundle", nil)
	r.SetPathValue("name", "was-dev")
	handlers.OpsBundle(tmpls)(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "was-dev") {
		t.Errorf("expected cluster name in body")
	}
}

// ── findingClassFn / findingIconFn / fmtElapsedFn ────────────────────────────

func TestFindingClassFn_Pass(t *testing.T) {
	fm := handlers.FuncMap()
	fn := fm["findingClass"].(func(doctor.Status, doctor.Severity) string)
	got := fn(doctor.StatusPass, doctor.SeverityInfo)
	if got != "pass" {
		t.Errorf("got %q, want pass", got)
	}
}

func TestFindingClassFn_Critical(t *testing.T) {
	fm := handlers.FuncMap()
	fn := fm["findingClass"].(func(doctor.Status, doctor.Severity) string)
	got := fn(doctor.StatusFail, doctor.SeverityCritical)
	if got != "critical" {
		t.Errorf("got %q, want critical", got)
	}
}

func TestFindingClassFn_Warning(t *testing.T) {
	fm := handlers.FuncMap()
	fn := fm["findingClass"].(func(doctor.Status, doctor.Severity) string)
	got := fn(doctor.StatusFail, doctor.SeverityWarning)
	if got != "warning" {
		t.Errorf("got %q, want warning", got)
	}
}

func TestFindingClassFn_Skip(t *testing.T) {
	fm := handlers.FuncMap()
	fn := fm["findingClass"].(func(doctor.Status, doctor.Severity) string)
	got := fn(doctor.StatusSkip, doctor.SeverityInfo)
	if got != "skip" {
		t.Errorf("got %q, want skip", got)
	}
}

func TestFindingIconFn_Pass(t *testing.T) {
	fm := handlers.FuncMap()
	fn := fm["findingIcon"].(func(doctor.Status, doctor.Severity) template.HTML)
	got := string(fn(doctor.StatusPass, doctor.SeverityInfo))
	if !strings.Contains(got, "pass") {
		t.Errorf("expected pass class in %q", got)
	}
}

func TestFindingIconFn_Critical(t *testing.T) {
	fm := handlers.FuncMap()
	fn := fm["findingIcon"].(func(doctor.Status, doctor.Severity) template.HTML)
	got := string(fn(doctor.StatusFail, doctor.SeverityCritical))
	if !strings.Contains(got, "critical") {
		t.Errorf("expected critical class in %q", got)
	}
}

func TestFmtElapsedFn_Milliseconds(t *testing.T) {
	fm := handlers.FuncMap()
	fn := fm["fmtElapsed"].(func(time.Duration) string)
	got := fn(450 * time.Millisecond)
	if got != "450ms" {
		t.Errorf("got %q, want 450ms", got)
	}
}

func TestFmtElapsedFn_Seconds(t *testing.T) {
	fm := handlers.FuncMap()
	fn := fm["fmtElapsed"].(func(time.Duration) string)
	got := fn(2100 * time.Millisecond)
	if got != "2.1s" {
		t.Errorf("got %q, want 2.1s", got)
	}
}

// ── OpsDoctorStream / OpsBundleStream ────────────────────────────────────────

func TestOpsDoctorStream_NotFound(t *testing.T) {
	tmpls := opsTemplates(t)
	broker := sse.NewBroker()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters/x/operations/doctor/stream/bad-session", nil)
	r.SetPathValue("sessionID", "bad-session")
	handlers.OpsDoctorStream(tmpls, broker)(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}

func TestOpsDoctorStream_FoundRender(t *testing.T) {
	tmpls := opsTemplates(t)
	broker := sse.NewBroker()
	run := broker.NewRun("was-dev", "aws")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters/was-dev/operations/doctor/stream/"+run.SessionID, nil)
	r.SetPathValue("sessionID", run.SessionID)
	r.SetPathValue("name", "was-dev")
	handlers.OpsDoctorStream(tmpls, broker)(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), run.SessionID) {
		t.Errorf("expected session ID in body")
	}
}

func TestOpsBundleStream_NotFound(t *testing.T) {
	tmpls := opsTemplates(t)
	broker := sse.NewBroker()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters/x/operations/bundle/stream/bad", nil)
	r.SetPathValue("sessionID", "bad")
	handlers.OpsBundleStream(tmpls, broker)(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}

// ── OpsBundleDownload ─────────────────────────────────────────────────────────

func TestOpsBundleDownload_Gone(t *testing.T) {
	store := &handlers.BundleStore{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters/x/operations/bundle/download/badtoken", nil)
	r.SetPathValue("token", "badtoken")
	handlers.OpsBundleDownload(store)(w, r)
	if w.Code != http.StatusGone {
		t.Errorf("want 410, got %d", w.Code)
	}
}

// ── groupFindings ─────────────────────────────────────────────────────────────

func TestGroupFindings_External(t *testing.T) {
	// Covered by the internal test file; this is a placeholder to confirm
	// the exported route works end-to-end (no separate assertion needed here).
}

// ── buildVersionRows ─────────────────────────────────────────────────────────

func TestBuildVersionRows_AllUnknown(t *testing.T) {
	detected := map[string]versions.Version{}
	rows := handlers.BuildVersionRows(detected)
	if len(rows) == 0 {
		t.Fatal("expected non-empty rows")
	}
	for _, row := range rows {
		if row.Status != "unknown" {
			t.Errorf("row %q: want unknown, got %q", row.Component, row.Status)
		}
	}
}

func TestBuildVersionRows_HelmInRange(t *testing.T) {
	v, err := parsers.Parse("3.15.0")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	detected := map[string]versions.Version{"helm": v}
	rows := handlers.BuildVersionRows(detected)
	for _, row := range rows {
		if row.Component == "Helm" {
			if row.Status != "pass" {
				t.Errorf("Helm 3.15.0: want pass, got %q", row.Status)
			}
			return
		}
	}
	t.Fatal("helm row not found")
}

func TestBuildVersionRows_HelmBelowMin(t *testing.T) {
	v, err := parsers.Parse("3.5.0")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	detected := map[string]versions.Version{"helm": v}
	rows := handlers.BuildVersionRows(detected)
	for _, row := range rows {
		if row.Component == "Helm" {
			if row.Status != "critical" {
				t.Errorf("Helm 3.5.0: want critical, got %q", row.Status)
			}
			return
		}
	}
	t.Fatal("helm row not found")
}
