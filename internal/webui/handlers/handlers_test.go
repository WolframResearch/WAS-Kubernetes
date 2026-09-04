package handlers_test

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/handlers"
)

// minimalTemplates builds a Templates set from inline template strings so the
// tests don't depend on the embedded FS.
func minimalTemplates(t *testing.T) *handlers.Templates {
	t.Helper()

	layoutSrc := `{{define "layout"}}<!DOCTYPE html><html><body>{{block "content" .}}{{end}}</body></html>{{end}}`
	// homeData now uses .Tab.Clusters instead of .Clusters
	homeSrc := `{{define "content"}}HOME:{{if eq .Tab.State "ok"}}CLUSTERS:{{range .Tab.Clusters}}{{.Name}},{{end}}{{else if eq .Tab.State "not_configured"}}NOT_CONFIGURED{{else}}EMPTY{{end}}{{end}}{{define "_cluster_tab_body"}}TAB:{{.Cloud}}:{{.State}}{{end}}`
	aboutSrc := `{{define "content"}}ABOUT:{{.Version}}{{end}}`

	newT := func(src ...string) *template.Template {
		base := template.Must(template.New("base").Parse(layoutSrc))
		for _, s := range src {
			template.Must(base.New("page").Parse(s))
		}
		return base
	}

	return &handlers.Templates{
		Home:  newT(homeSrc),
		About: newT(aboutSrc),
	}
}

func TestHealthz(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	handlers.Healthz()(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ok") {
		t.Fatalf("expected 'ok' in body, got %q", w.Body.String())
	}
}

func TestAbout_200(t *testing.T) {
	tmpls := minimalTemplates(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/about", nil)
	handlers.About(tmpls)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("want text/html, got %q", ct)
	}
}
