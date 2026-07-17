package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/schema"
)

// ChartOnlyRender handles POST /chart-only/render — runs `helm template` with
// the submitted form values and returns a rendered-YAML HTMX fragment.
func ChartOnlyRender(t *Templates, s *schema.Schema, chartDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()

		valuesJSON, err := buildValuesJSON(r, s)
		if err != nil {
			renderPreviewFragment(w, t, "", fmt.Sprintf("failed to build values: %v", err))
			return
		}

		if chartDir == "" {
			renderPreviewFragment(w, t, "", "chart-dir is not configured (run wasctl serve --chart-dir=<path>)")
			return
		}

		// Write values to temp file.
		vf, err := os.CreateTemp("", "wasctl-values-*.json")
		if err != nil {
			renderPreviewFragment(w, t, "", fmt.Sprintf("temp file error: %v", err))
			return
		}
		defer os.Remove(vf.Name())
		if _, err := vf.Write(valuesJSON); err != nil {
			vf.Close()
			renderPreviewFragment(w, t, "", fmt.Sprintf("write error: %v", err))
			return
		}
		vf.Close()

		release := strings.TrimSpace(r.FormValue("release"))
		if release == "" {
			release = "was"
		}

		cmd := exec.CommandContext(r.Context(), "helm", "template",
			release, chartDir,
			"-f", vf.Name(),
			"--namespace", "was",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			renderPreviewFragment(w, t, "", "helm template failed:\n"+strings.TrimSpace(string(out)))
			return
		}
		renderPreviewFragment(w, t, string(out), "")
	}
}

// ── Preview fragment data ─────────────────────────────────────────────────────

type previewFragmentData struct {
	Version string
	YAML    string
	Error   string
}

func renderPreviewFragment(w http.ResponseWriter, t *Templates, yaml, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := previewFragmentData{Version: version.Version, YAML: yaml, Error: errMsg}
	if err := t.ChartValuesEditor.ExecuteTemplate(w, "_preview_pane", data); err != nil {
		renderErr(w, err)
	}
}

// ── Form → values JSON ────────────────────────────────────────────────────────

func buildValuesJSON(r *http.Request, s *schema.Schema) ([]byte, error) {
	// Build a field-type lookup so we can coerce strings to the right type.
	typeLookup := make(map[string]string, len(s.Fields))
	for _, f := range s.Fields {
		typeLookup[f.Path] = f.Type
	}

	root := make(map[string]interface{})
	for _, f := range s.Fields {
		val := r.FormValue(f.Path)
		if val == "" {
			continue
		}
		parsed := parseFormVal(val, f.Type)
		setNestedVal(root, strings.Split(f.Path, "."), parsed)
	}
	return json.MarshalIndent(root, "", "  ")
}

func parseFormVal(s, typ string) interface{} {
	switch typ {
	case "bool":
		return s == "true" || s == "on" || s == "1"
	case "int":
		if i, err := strconv.Atoi(s); err == nil {
			return i
		}
	}
	return s
}

func setNestedVal(m map[string]interface{}, path []string, val interface{}) {
	if len(path) == 1 {
		m[path[0]] = val
		return
	}
	if _, ok := m[path[0]]; !ok {
		m[path[0]] = make(map[string]interface{})
	}
	sub, ok := m[path[0]].(map[string]interface{})
	if !ok {
		return
	}
	setNestedVal(sub, path[1:], val)
}
