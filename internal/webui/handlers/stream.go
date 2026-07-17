package handlers

import (
	"fmt"
	"net/http"

	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/sse"
)

// InstallStream handles GET /install/stream/{sessionID} — renders the live
// progress page.
func InstallStream(t *Templates, broker *sse.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("sessionID")
		run, ok := broker.Get(sessionID)
		if !ok {
			http.NotFound(w, r)
			return
		}

		data := struct {
			Version   string
			SessionID string
			Cluster   string
			Cloud     string
			Heading   string
			StageList []stageRow
		}{
			Version:   version.Version,
			SessionID: sessionID,
			Cluster:   run.ClusterName,
			Cloud:     run.Cloud,
			Heading:   "Installing",
			StageList: allStageRows(),
		}
		if run.Heading != "" {
			data.Heading = run.Heading
		}
		if len(run.StageNames) > 0 {
			if rows := stageRowsFromNames(run.StageNames); len(rows) > 0 {
				data.StageList = rows
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.InstallStream.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

// InstallSSE handles GET /install/sse/{sessionID} — the actual SSE endpoint.
// A browser connects here; events stream until install completes or the
// connection drops. The install goroutine is NOT cancelled on disconnect.
func InstallSSE(broker *sse.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("sessionID")
		run, ok := broker.Get(sessionID)
		if !ok {
			http.NotFound(w, r)
			return
		}

		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

		ctx := r.Context()
		snapshot, ch := run.Subscribe(ctx)

		// Replay ring buffer to this client.
		for _, ev := range snapshot {
			writeSSEEvent(w, ev)
		}
		fl.Flush()

		if ch == nil {
			// Run already done — send a final ping and close.
			writeSSEEvent(w, sse.Event{Type: "ping", Data: "{}"})
			fl.Flush()
			return
		}

		defer run.Unsubscribe(ch)

		for {
			select {
			case <-ctx.Done():
				return
			case ev, open := <-ch:
				if !open {
					// Channel closed: install done.
					writeSSEEvent(w, sse.Event{Type: "ping", Data: "{}"})
					fl.Flush()
					return
				}
				writeSSEEvent(w, ev)
				fl.Flush()
			}
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, ev sse.Event) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, ev.Data)
}
