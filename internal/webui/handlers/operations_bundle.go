package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/supportbundle"
	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/sse"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

// BundleEntry tracks a generated bundle awaiting download.
type BundleEntry struct {
	Path      string
	CreatedAt time.Time
}

// BundleStore is a sync.Map[string]BundleEntry for one-time download tokens.
// The Server embeds this. GC is done in NewServer.
type BundleStore struct {
	m sync.Map
}

func (s *BundleStore) store(token string, e BundleEntry) {
	s.m.Store(token, e)
}

func (s *BundleStore) take(token string) (BundleEntry, bool) {
	v, ok := s.m.LoadAndDelete(token)
	if !ok {
		return BundleEntry{}, false
	}
	return v.(BundleEntry), true
}

// StartGC starts a background goroutine that removes expired bundle entries
// and their temp files. Call once from NewServer.
func (s *BundleStore) StartGC() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		s.m.Range(func(k, v any) bool {
			e := v.(BundleEntry)
			if time.Since(e.CreatedAt) > 10*time.Minute {
				s.m.Delete(k)
				_ = os.Remove(e.Path)
			}
			return true
		})
	}
}

type opsBundleData struct {
	Version     string
	ClusterName string
	ActiveTab   string
	SessionID   string // set when viewing stream page
}

// OpsBundle handles GET /clusters/{name}/operations/bundle — bundle form page.
func OpsBundle(t *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		data := opsBundleData{
			Version:     version.Version,
			ClusterName: name,
			ActiveTab:   "bundle",
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.OpsBundle.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

// OpsBundleRun handles POST /clusters/{name}/operations/bundle — starts bundle
// generation and redirects to the SSE stream page.
func OpsBundleRun(t *Templates, metaRegion, region string, broker *sse.Broker, store *BundleStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		_ = r.ParseForm()
		prefer := preferredCloud(r)

		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()

		ws, cloud, _, err := openWorkspacePreferring(ctx, metaRegion, name, prefer)
		if err != nil {
			http.Error(w, "workspace unavailable: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer ws.Close()

		run := broker.NewRun(name, cloud)

		cfg := &config.Config{
			Cloud:       cloud,
			MetaRegion:  config.Field[string]{Value: metaRegion},
			ClusterName: config.Field[string]{Value: name},
		}
		if cloud == "azure" {
			cfg.AzureLocation = config.Field[string]{Value: ws.Meta.AzureLocation}
		} else {
			cfg.Region = config.Field[string]{Value: ws.Meta.AWSRegion}
			if cfg.Region.Value == "" {
				cfg.Region = config.Field[string]{Value: region}
			}
		}

		go runBundleAsync(cfg, run, store, metaRegion, region)

		http.Redirect(w, r,
			"/clusters/"+name+"/operations/bundle/stream/"+run.SessionID,
			http.StatusSeeOther)
	}
}

// OpsBundleStream handles GET /clusters/{name}/operations/bundle/stream/{sessionID}.
func OpsBundleStream(t *Templates, broker *sse.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("sessionID")
		run, ok := broker.Get(sessionID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		data := opsBundleData{
			Version:     version.Version,
			ClusterName: run.ClusterName,
			ActiveTab:   "bundle",
			SessionID:   sessionID,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.OpsBundle.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

// OpsBundleSSE handles GET /clusters/{name}/operations/bundle/sse/{sessionID}.
func OpsBundleSSE(broker *sse.Broker) http.HandlerFunc {
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
		w.Header().Set("X-Accel-Buffering", "no")

		ctx := r.Context()
		snapshot, ch := run.Subscribe(ctx)
		for _, ev := range snapshot {
			writeSSEEvent(w, ev)
		}
		fl.Flush()

		if ch == nil {
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

// OpsBundleDownload handles GET /clusters/{name}/operations/bundle/download/{token}.
// Serves the file once and deletes it.
func OpsBundleDownload(store *BundleStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.PathValue("token")
		entry, ok := store.take(token)
		if !ok {
			http.Error(w, "download token expired or not found", http.StatusGone)
			return
		}
		defer os.Remove(entry.Path)

		f, err := os.Open(entry.Path)
		if err != nil {
			http.Error(w, "bundle file unavailable", http.StatusInternalServerError)
			return
		}
		defer f.Close()

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="bundle.tar.gz"`)
		_, _ = io.Copy(w, f)
	}
}

// runBundleAsync runs bundle generation in the background, streaming SSE events.
func runBundleAsync(cfg *config.Config, run *sse.InstallRun, store *BundleStore, metaRegion, region string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pub := func(evType string, payload map[string]any) {
		data, _ := json.Marshal(payload)
		run.Publish(sse.Event{Type: evType, Data: string(data)})
	}

	pub("bundle-start", map[string]any{"cluster": run.ClusterName})

	// Build a RunContext for workspace access.
	rc := doctor.NewRunContext(ctx, cfg)
	defer rc.Cleanup()

	var kubeconfigPath, contextName string
	if rc.Workspace != nil {
		kubeconfigPath = rc.Kubeconfig
		contextName = rc.ContextName
	}

	// A ProgressWriter that publishes SSE events.
	pw := newSSEProgressWriter(run)

	// Write to a temp file.
	tmpFile, err := os.CreateTemp("", "wasctl-bundle-*.tar.gz")
	if err != nil {
		pub("bundle-error", map[string]any{"error": "failed to create temp file: " + err.Error()})
		run.MarkDone(err)
		return
	}
	tmpPath := tmpFile.Name()

	var ws *workspace.Workspace
	if rc.Workspace != nil {
		ws = rc.Workspace
	}

	bundleErr := supportbundle.Build(ctx, cfg, ws, kubeconfigPath, contextName,
		supportbundle.Options{MaxLogLines: 1000},
		pw, tmpFile)
	_ = tmpFile.Close()

	if bundleErr != nil {
		_ = os.Remove(tmpPath)
		pub("bundle-error", map[string]any{"error": bundleErr.Error()})
		run.MarkDone(bundleErr)
		return
	}

	// Generate download token.
	tok := newDownloadToken()
	store.store(tok, BundleEntry{Path: tmpPath, CreatedAt: time.Now()})

	downloadURL := fmt.Sprintf("/clusters/%s/operations/bundle/download/%s", run.ClusterName, tok)
	pub("bundle-complete", map[string]any{"downloadURL": downloadURL})
	run.MarkDone(nil)
}

// sseLineWriter writes progress lines from supportbundle.ProgressWriter to
// the SSE run as "log" events. Each '\n'-terminated line is one event.
type sseLineWriter struct {
	run *sse.InstallRun
	buf []byte
}

func (w *sseLineWriter) Write(p []byte) (int, error) {
	for _, b := range p {
		if b == '\n' {
			if len(w.buf) > 0 {
				data, _ := json.Marshal(map[string]any{"line": string(w.buf)})
				w.run.Publish(sse.Event{Type: "log", Data: string(data)})
				w.buf = w.buf[:0]
			}
		} else {
			w.buf = append(w.buf, b)
		}
	}
	return len(p), nil
}

func newSSEProgressWriter(run *sse.InstallRun) *supportbundle.ProgressWriter {
	return supportbundle.NewProgressWriter(&sseLineWriter{run: run})
}

func newDownloadToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
