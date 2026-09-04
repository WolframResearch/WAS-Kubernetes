package handlers

import (
	"net/http"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/schema"
)

// chartPreviewData is passed to chart_preview.html.
type chartPreviewData struct {
	Version    string
	Kubeconfig string // base64-encoded, carried as hidden field
	Release    string
	Namespace  string
	ChartDir   string
	Groups     []formGroup
	FormValues map[string]string // path → value for summary display
}

// ChartOnlyPreview handles POST /chart-only/preview — shows the preflight
// summary before committing the helm apply.
func ChartOnlyPreview(t *Templates, s *schema.Schema) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()

		release := strings.TrimSpace(r.FormValue("release"))
		if release == "" {
			release = "was"
		}
		namespace := strings.TrimSpace(r.FormValue("namespace"))
		if namespace == "" {
			namespace = "was"
		}

		// Collect visible form values for the summary table.
		fv := make(map[string]string, len(s.Fields))
		for _, f := range s.Fields {
			if v := r.FormValue(f.Path); v != "" {
				fv[f.Path] = v
			}
		}

		data := chartPreviewData{
			Version:    version.Version,
			Kubeconfig: r.FormValue("kubeconfig_b64"),
			Release:    release,
			Namespace:  namespace,
			ChartDir:   r.FormValue("chart_dir"),
			Groups:     groupFields(s),
			FormValues: fv,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.ChartPreview.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}
