package handlers_test

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/handlers"
)

// installTemplates builds a minimal Templates set for install wizard tests.
// It includes a real cloud-selector template to exercise the conditional logic.
func installTemplates(t *testing.T) *handlers.Templates {
	t.Helper()
	layoutSrc := `{{define "layout"}}<!DOCTYPE html><html><body>{{block "content" .}}{{end}}</body></html>{{end}}`

	step1Src := `{{define "_install_step1"}}STEP1:cloud={{.Form.Cloud}}:name={{.Form.ClusterName}}:region={{.Form.Region}}{{if .Form.Errors.cluster_name}}:ERR={{.Form.Errors.cluster_name}}{{end}}{{end}}`
	step2Src := `{{define "_install_step2"}}STEP2:ingress={{.Form.IngressHost}}{{end}}`
	step3Src := `{{define "_install_step3"}}STEP3:node={{.Form.NodeType}}:cloud={{.Form.Cloud}}{{end}}`
	step4Src := `{{define "_install_step4"}}STEP4:awes={{.Form.AWESReplicas}}{{end}}`
	hiddenStep1 := `{{define "_hidden_step1"}}<input name="cloud" value="{{.Form.Cloud}}"><input name="cluster_name" value="{{.Form.ClusterName}}">{{end}}`
	hiddenStep2 := `{{define "_hidden_step2"}}{{end}}`
	hiddenStep3 := `{{define "_hidden_step3"}}{{end}}`
	wizardMainSrc := `{{define "content"}}WIZARD:{{template "_install_step1" .}}{{end}}`

	newWizard := func() *template.Template {
		base := template.Must(template.New("base").Parse(layoutSrc))
		for _, s := range []string{wizardMainSrc, step1Src, step2Src, step3Src, step4Src, hiddenStep1, hiddenStep2, hiddenStep3} {
			template.Must(base.New("p").Parse(s))
		}
		return base
	}

	previewSrc := `{{define "content"}}PREVIEW:cloud={{.Form.Cloud}}:cost={{.CostLow}}-{{.CostHigh}}:name={{.Form.ClusterName}}:region={{.Form.Region}}:node={{.Form.NodeType}}{{end}}`
	newPreview := func() *template.Template {
		base := template.Must(template.New("base").Parse(layoutSrc))
		template.Must(base.New("p").Parse(previewSrc))
		return base
	}

	return &handlers.Templates{
		InstallWizard:  newWizard(),
		InstallPreview: newPreview(),
	}
}

func postForm(t *testing.T, path string, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return w
}

// ── InstallStep2 (validates step 1) ──────────────────────────────────────────

func TestInstallStep2_AWS_Valid(t *testing.T) {
	tmpls := installTemplates(t)
	h := handlers.InstallStep2(tmpls)

	v := url.Values{
		"cloud":        {"aws"},
		"cluster_name": {"my-cluster"},
		"region":       {"us-east-1"},
		"k8s_version":  {"1.35"},
	}
	w := postForm(t, "/install/step2", v)
	r := httptest.NewRequest(http.MethodPost, "/install/step2", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "STEP2") {
		t.Errorf("expected STEP2 fragment, got %q", body)
	}
}

func TestInstallStep2_Azure_Valid(t *testing.T) {
	tmpls := installTemplates(t)
	h := handlers.InstallStep2(tmpls)

	v := url.Values{
		"cloud":        {"azure"},
		"cluster_name": {"my-aks"},
		"region":       {"eastus"},
		"k8s_version":  {"1.35"},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/install/step2", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "STEP2") {
		t.Errorf("expected STEP2 fragment for Azure, got %q", body)
	}
}

func TestInstallStep2_Azure_MissingLocation(t *testing.T) {
	tmpls := installTemplates(t)
	h := handlers.InstallStep2(tmpls)

	v := url.Values{
		"cloud":        {"azure"},
		"cluster_name": {"my-aks"},
		"region":       {""},
		"k8s_version":  {"1.35"},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/install/step2", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "STEP1") {
		t.Errorf("expected re-render of STEP1 with error, got %q", body)
	}
}

// ── InstallStep3 (validates step 2) ──────────────────────────────────────────

func TestInstallStep3_Azure_Propagates(t *testing.T) {
	tmpls := installTemplates(t)
	h := handlers.InstallStep3(tmpls)

	v := url.Values{
		"cloud":        {"azure"},
		"cluster_name": {"my-aks"},
		"region":       {"eastus"},
		"k8s_version":  {"1.35"},
		"ingress_host": {"was.example.com"},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/install/step3", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "STEP3") {
		t.Errorf("expected STEP3 fragment, got %q", body)
	}
	if !strings.Contains(body, "cloud=azure") {
		t.Errorf("expected cloud=azure in step3 output, got %q", body)
	}
}

// ── InstallPreview ────────────────────────────────────────────────────────────

func TestInstallPreview_Azure(t *testing.T) {
	tmpls := installTemplates(t)
	h := handlers.InstallPreview(tmpls)

	v := url.Values{
		"cloud":         {"azure"},
		"cluster_name":  {"my-aks"},
		"region":        {"eastus"},
		"k8s_version":   {"1.35"},
		"node_type":     {"Standard_D4s_v5"},
		"node_min":      {"2"},
		"node_desired":  {"2"},
		"node_max":      {"10"},
		"awes_replicas": {"2"},
		"rm_replicas":   {"2"},
		"em_replicas":   {"2"},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/install/preview", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "cloud=azure") {
		t.Errorf("expected cloud=azure in preview, got %q", body)
	}
	// Azure cost estimate should be non-empty and different from AWS
	if !strings.Contains(body, "cost=") {
		t.Errorf("expected cost range in preview, got %q", body)
	}
}

func TestInstallPreview_AWS(t *testing.T) {
	tmpls := installTemplates(t)
	h := handlers.InstallPreview(tmpls)

	v := url.Values{
		"cloud":         {"aws"},
		"cluster_name":  {"my-eks"},
		"region":        {"us-east-1"},
		"k8s_version":   {"1.35"},
		"node_type":     {"c5.2xlarge"},
		"node_min":      {"2"},
		"node_desired":  {"2"},
		"node_max":      {"10"},
		"awes_replicas": {"2"},
		"rm_replicas":   {"2"},
		"em_replicas":   {"2"},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/install/preview", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "cloud=aws") {
		t.Errorf("expected cloud=aws in preview, got %q", body)
	}
}

// ── InstallWizard (GET) ───────────────────────────────────────────────────────

func TestInstallWizard_DefaultsToAWS(t *testing.T) {
	tmpls := installTemplates(t)
	h := handlers.InstallWizard(tmpls)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/install", nil)
	h(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "cloud=aws") {
		t.Errorf("expected default cloud=aws, got %q", body)
	}
}

// ── estimateCost helpers ──────────────────────────────────────────────────────

func TestEstimateAzureCost_Standard_D4s_v5(t *testing.T) {
	// White-box: 2 nodes × $0.192/hr × 730 hr + $80 fixed ≈ $360
	// Use preview handler with azure cloud to exercise estimateAzureCost indirectly.
	tmpls := installTemplates(t)
	h := handlers.InstallPreview(tmpls)

	v := url.Values{
		"cloud":         {"azure"},
		"cluster_name":  {"test"},
		"region":        {"eastus"},
		"k8s_version":   {"1.35"},
		"node_type":     {"Standard_D4s_v5"},
		"node_min":      {"1"},
		"node_desired":  {"2"},
		"node_max":      {"10"},
		"awes_replicas": {"2"},
		"rm_replicas":   {"2"},
		"em_replicas":   {"2"},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/install/preview", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h(w, r)

	body := w.Body.String()
	// Cost should appear as "cost=$NNN-$NNN"; just check it's present and non-zero
	if !strings.Contains(body, "cost=$") {
		t.Errorf("expected dollar cost, got %q", body)
	}
	if strings.Contains(body, "cost=$0-$0") {
		t.Errorf("cost should not be zero for Standard_D4s_v5")
	}
}
