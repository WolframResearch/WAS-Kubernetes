package handlers

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/schema"
)

// chartLandingData is passed to chart_only_landing.html.
type chartLandingData struct {
	Version string
}

// chartEditorData is passed to chart_values_editor.html.
type chartEditorData struct {
	Version    string
	Kubeconfig string // base64-encoded; carried through the whole flow
	Release    string // helm release name
	Groups     []formGroup
}

type formGroup struct {
	Name   string
	Fields []schema.Field
}

// ChartOnlyLanding handles GET /chart-only — shows prereq checklist + kubeconfig input.
func ChartOnlyLanding(t *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := chartLandingData{Version: version.Version}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.ChartOnlyLanding.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

// ChartOnlyValidate handles POST /chart-only/validate — runs kubectl cluster-info
// and returns an HTMX fragment with the cluster identity for user confirmation.
func ChartOnlyValidate(t *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		kubeconfig := r.FormValue("kubeconfig")
		if strings.TrimSpace(kubeconfig) == "" {
			renderValidateFragment(w, t, "", "", "Paste or upload a kubeconfig file first.")
			return
		}

		// Write to temp file — never persisted beyond this request.
		tmp, err := os.CreateTemp("", "wasctl-kube-*.yaml")
		if err != nil {
			renderValidateFragment(w, t, "", "", fmt.Sprintf("Failed to create temp file: %v", err))
			return
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)
		if _, err := tmp.WriteString(kubeconfig); err != nil {
			tmp.Close()
			renderValidateFragment(w, t, "", "", fmt.Sprintf("Failed to write kubeconfig: %v", err))
			return
		}
		tmp.Close()

		// kubectl cluster-info
		infoOut, err := runKubectl(r.Context(), tmpPath, "cluster-info")
		if err != nil {
			renderValidateFragment(w, t, "", "", "kubectl cluster-info failed: "+cleanCmdErr(infoOut, err))
			return
		}

		// kubectl version --short
		verOut, _ := runKubectl(r.Context(), tmpPath, "version", "--short")

		renderValidateFragment(w, t,
			base64.StdEncoding.EncodeToString([]byte(kubeconfig)),
			strings.TrimSpace(infoOut)+"\n"+strings.TrimSpace(verOut),
			"")
	}
}

// ChartOnlyConfigure handles POST /chart-only/configure — full values-editor page.
func ChartOnlyConfigure(t *Templates, s *schema.Schema) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		kbB64 := r.FormValue("kubeconfig_b64")
		release := strings.TrimSpace(r.FormValue("release"))
		if release == "" {
			release = "was"
		}

		data := chartEditorData{
			Version:    version.Version,
			Kubeconfig: kbB64,
			Release:    release,
			Groups:     groupFields(s),
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.ChartValuesEditor.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

// ── Helpers ────────────────────────────────────────────────────────────────────

type validateFragmentData struct {
	Version       string
	KubeconfigB64 string
	ClusterInfo   string
	Error         string
}

func renderValidateFragment(w http.ResponseWriter, t *Templates, kbB64, info, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := validateFragmentData{
		Version:       version.Version,
		KubeconfigB64: kbB64,
		ClusterInfo:   info,
		Error:         errMsg,
	}
	if err := t.ChartOnlyLanding.ExecuteTemplate(w, "_validate_result", data); err != nil {
		renderErr(w, err)
	}
}

func groupFields(s *schema.Schema) []formGroup {
	buckets := make(map[string][]schema.Field, len(s.Groups))
	for _, f := range s.Fields {
		if f.Hidden {
			continue
		}
		buckets[f.Group] = append(buckets[f.Group], f)
	}
	var groups []formGroup
	for _, name := range s.Groups {
		if fields, ok := buckets[name]; ok {
			groups = append(groups, formGroup{Name: name, Fields: fields})
		}
	}
	return groups
}

func cleanCmdErr(out string, err error) string {
	if out = strings.TrimSpace(out); out != "" {
		lines := strings.Split(out, "\n")
		if len(lines) > 0 {
			return strings.TrimSpace(lines[0])
		}
	}
	return err.Error()
}

func runKubectl(ctx interface{ Done() <-chan struct{} }, kubeconfig string, args ...string) (string, error) {
	cmdArgs := append([]string{"--kubeconfig", kubeconfig}, args...)
	cmd := exec.Command("kubectl", cmdArgs...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
