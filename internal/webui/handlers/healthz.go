package handlers

import "net/http"

// Healthz returns a handler for GET /healthz. It always responds 200 OK with
// a plain-text body so load balancers and readiness probes have a stable target.
func Healthz() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	}
}
