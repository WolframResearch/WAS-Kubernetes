package handlers

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/schema"
	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/sse"
)

// ChartOnlyApply handles POST /chart-only/apply — starts `helm upgrade --install`
// in a background goroutine and redirects to the streaming page.
func ChartOnlyApply(t *Templates, s *schema.Schema, broker *sse.Broker, chartDir string) http.HandlerFunc {
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
		kbB64 := r.FormValue("kubeconfig_b64")

		// Decode kubeconfig.
		kbRaw, err := base64.StdEncoding.DecodeString(kbB64)
		if err != nil || len(kbRaw) == 0 {
			http.Error(w, "missing or invalid kubeconfig", http.StatusBadRequest)
			return
		}

		// Build values JSON.
		valuesJSON, err := buildValuesJSON(r, s)
		if err != nil {
			http.Error(w, "failed to build values: "+err.Error(), http.StatusBadRequest)
			return
		}

		run := broker.NewRun(release, "")

		go runHelmUpgrade(context.Background(), release, namespace, chartDir, kbRaw, valuesJSON, run)

		http.Redirect(w, r, "/chart-only/stream/"+run.SessionID, http.StatusSeeOther)
	}
}

// ChartOnlyStream handles GET /chart-only/stream/{sessionID} — renders the
// live helm output page.
func ChartOnlyStream(t *Templates, broker *sse.Broker) http.HandlerFunc {
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
			Release   string
		}{
			Version:   version.Version,
			SessionID: sessionID,
			Release:   run.ClusterName,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.ChartApply.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

// ChartOnlySSE handles GET /chart-only/sse/{sessionID} — SSE stream for helm output.
// Reuses the same SSE infrastructure as the install wizard.
func ChartOnlySSE(broker *sse.Broker) http.HandlerFunc {
	// The event format is identical to InstallSSE so we delegate.
	return InstallSSE(broker)
}

// ── Helm runner ───────────────────────────────────────────────────────────────

func runHelmUpgrade(ctx context.Context, release, namespace, chartDir string, kubeconfig, valuesJSON []byte, run *sse.InstallRun) {
	publish := func(typ string, payload interface{}) {
		data, _ := json.Marshal(payload)
		run.Publish(sse.Event{Type: typ, Data: string(data)})
	}
	logLine := func(line string) {
		publish("log", map[string]string{"line": line})
	}

	// Write kubeconfig to temp.
	kf, err := os.CreateTemp("", "wasctl-kube-*.yaml")
	if err != nil {
		publish("install-complete", map[string]string{"error": fmt.Sprintf("temp file: %v", err)})
		run.MarkDone(err)
		return
	}
	defer os.Remove(kf.Name())
	kf.Write(kubeconfig)
	kf.Close()

	// Write values to temp.
	vf, err := os.CreateTemp("", "wasctl-values-*.json")
	if err != nil {
		publish("install-complete", map[string]string{"error": fmt.Sprintf("temp file: %v", err)})
		run.MarkDone(err)
		return
	}
	defer os.Remove(vf.Name())
	vf.Write(valuesJSON)
	vf.Close()

	args := []string{
		"upgrade", "--install", release, chartDir,
		"--kubeconfig", kf.Name(),
		"-f", vf.Name(),
		"--namespace", namespace,
		"--create-namespace",
		"--wait",
		"--timeout", "10m",
	}
	cmd := exec.CommandContext(ctx, "helm", args...)

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	logLine("$ helm " + strings.Join(args, " "))

	if err := cmd.Start(); err != nil {
		pw.Close()
		errMsg := fmt.Sprintf("helm not found: %v", err)
		logLine(errMsg)
		publish("install-complete", map[string]string{"error": errMsg})
		run.MarkDone(err)
		return
	}

	go func() {
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			logLine(scanner.Text())
		}
	}()

	cmdErr := cmd.Wait()
	pw.Close()

	result := "success"
	if cmdErr != nil {
		result = "helm upgrade failed: " + cmdErr.Error()
		publish("install-complete", map[string]string{"error": result})
	} else {
		publish("install-complete", map[string]interface{}{
			"clusterUrl": "",
		})
	}
	// Log the helm apply result for the release. Chart-only mode has no meta
	// bucket, so we log to a no-op implementation. The log call is kept here
	// as a hook for future enhancement (e.g., workspace-aware chart-only mode).
	_ = result
	run.MarkDone(cmdErr)
}
