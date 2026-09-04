package webui_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/webui"
)

func TestNewServer_parsesTemplates(t *testing.T) {
	// Verifies that embedded templates parse without error at startup.
	_, err := webui.NewServer("localhost:0", "us-east-1", "us-east-1", "", false, "")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
}

func TestHealthzRoute(t *testing.T) {
	srv, err := webui.NewServer("localhost:0", "us-east-1", "us-east-1", "", false, "")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	srv.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("healthz: want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ok") {
		t.Fatalf("healthz body: want 'ok', got %q", w.Body.String())
	}
}

func TestStaticRoute(t *testing.T) {
	srv, err := webui.NewServer("localhost:0", "us-east-1", "us-east-1", "", false, "")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/static/wasctl.css", nil)
	srv.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("static CSS: want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/css") {
		t.Fatalf("static CSS content-type: want text/css, got %q", ct)
	}
}
