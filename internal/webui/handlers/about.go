package handlers

import (
	"net/http"
	"runtime"

	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
)

type aboutData struct {
	Version    string
	GoVersion  string
	Commit     string
	BuildDate  string
	ListenAddr string
}

// About returns a handler for GET /about. ListenAddr is injected at serve time
// so the about page can display the server's bind address.
func About(t *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := aboutData{
			Version:    version.Version,
			GoVersion:  runtime.Version(),
			Commit:     buildCommit,
			BuildDate:  buildDate,
			ListenAddr: r.Host,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.About.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

// buildCommit and buildDate are injected at build time via -ldflags:
//
//	-X github.com/WolframResearch/WAS-Kubernetes/internal/webui/handlers.buildCommit=<sha>
//	-X github.com/WolframResearch/WAS-Kubernetes/internal/webui/handlers.buildDate=<date>
var (
	buildCommit = "unknown"
	buildDate   = "unknown"
)
