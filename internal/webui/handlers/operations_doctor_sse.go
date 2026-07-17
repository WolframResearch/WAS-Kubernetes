package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor/checks"
	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/sse"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

type opsDoctorRunningData struct {
	Version     string
	ClusterName string
	SessionID   string
}

// OpsDoctorRun handles POST /clusters/{name}/operations/doctor/run — starts a
// new doctor run and redirects to the SSE stream page.
func OpsDoctorRun(t *Templates, metaRegion, region string, broker *sse.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		prefer := preferredCloud(r)

		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()

		ws, cloud, accountID, err := openWorkspacePreferring(ctx, metaRegion, name, prefer)
		if err != nil {
			http.Error(w, "workspace unavailable: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer ws.Close()

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

		run := broker.NewRun(name, cloud)
		go runDoctorAsync(cfg, run, metaRegion, cloud, accountID)

		http.Redirect(w, r,
			"/clusters/"+name+"/operations/doctor/stream/"+run.SessionID,
			http.StatusSeeOther)
	}
}

// OpsDoctorStream handles GET /clusters/{name}/operations/doctor/stream/{sessionID}.
func OpsDoctorStream(t *Templates, broker *sse.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("sessionID")
		run, ok := broker.Get(sessionID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		data := opsDoctorRunningData{
			Version:     version.Version,
			ClusterName: run.ClusterName,
			SessionID:   sessionID,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.OpsDoctorRunning.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

// OpsDoctorSSE handles GET /clusters/{name}/operations/doctor/sse/{sessionID}.
func OpsDoctorSSE(broker *sse.Broker) http.HandlerFunc {
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

// runDoctorAsync performs the full doctor run in a background goroutine,
// streaming SSE events and persisting the final report to the cloud meta store.
func runDoctorAsync(cfg *config.Config, run *sse.InstallRun, metaRegion, cloud, accountID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pub := func(evType string, payload map[string]any) {
		data, _ := json.Marshal(payload)
		run.Publish(sse.Event{Type: evType, Data: string(data)})
	}

	rc := doctor.NewRunContext(ctx, cfg)
	defer rc.Cleanup()

	allChecks := checks.ForCloud(cloud)

	onStart := func(c doctor.Check) {
		pub("check-start", map[string]any{
			"id":       c.ID(),
			"name":     c.Name(),
			"category": c.Category().String(),
		})
	}

	onFinding := func(c doctor.Check, f doctor.Finding) {
		pub("check-done", map[string]any{
			"id": f.CheckID,
			"finding": map[string]any{
				"checkName":   f.CheckName,
				"status":      f.Status,
				"severity":    f.Severity,
				"message":     f.Message,
				"remediation": f.Remediation,
				"elapsed":     f.Elapsed.Milliseconds(),
			},
		})
	}

	rep := doctor.RunWithCallback(ctx, rc, allChecks, onStart, onFinding)

	repJSON, err := json.Marshal(rep)
	if err == nil {
		repID, saveErr := persistReport(ctx, cfg, rep, repJSON, metaRegion, cloud, accountID)
		if saveErr == nil {
			pub("run-complete", map[string]any{
				"reportID":   repID,
				"clusterURL": "/clusters/" + run.ClusterName + "/operations",
				"summary": map[string]any{
					"pass":  rep.Summary.Pass,
					"fail":  rep.Summary.Fail,
					"skip":  rep.Summary.Skip,
					"error": rep.Summary.Error,
					"info":  rep.Summary.Info,
				},
			})
			run.MarkDone(nil)
			return
		}
	}

	pub("run-complete", map[string]any{
		"clusterURL": "/clusters/" + run.ClusterName + "/operations",
		"summary": map[string]any{
			"pass":  rep.Summary.Pass,
			"fail":  rep.Summary.Fail,
			"skip":  rep.Summary.Skip,
			"error": rep.Summary.Error,
			"info":  rep.Summary.Info,
		},
	})
	run.MarkDone(nil)
}

func persistReport(ctx context.Context, cfg *config.Config, rep *doctor.Report, repJSON []byte, metaRegion, cloud, accountID string) (string, error) {
	store, _, err := openReportStoreFromAccount(ctx, metaRegion, cloud, accountID, cfg.ClusterName.Value)
	if err != nil {
		return "", err
	}
	summary := workspace.ReportSummary{
		Pass:  rep.Summary.Pass,
		Fail:  rep.Summary.Fail,
		Skip:  rep.Summary.Skip,
		Error: rep.Summary.Error,
		Info:  rep.Summary.Info,
	}
	return workspace.SaveReport(ctx, store, rep.Cluster, repJSON, summary)
}
