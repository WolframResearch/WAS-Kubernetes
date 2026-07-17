package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/stages"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/sse"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

type destroyPageData struct {
	Version     string
	ClusterName string
	Region      string
	Cloud       string
	Error       string
}

// openWorkspaceForDestroy locates a cluster workspace on AWS or Azure.
// Prefer openWorkspacePreferring when the UI knows the active cloud cookie.
func openWorkspaceForDestroy(ctx context.Context, metaRegion, name string) (*workspace.Workspace, string, string, error) {
	return openWorkspace(ctx, metaRegion, name, "", false)
}

// openWorkspacePreferring tries the preferred cloud first so Azure cluster
// pages do not wait on a failing AWS STS/S3 probe (and vice versa).
// Uses a read-only Azure open (no lock blob) suitable for the web UI.
func openWorkspacePreferring(ctx context.Context, metaRegion, name, preferCloud string) (*workspace.Workspace, string, string, error) {
	return openWorkspace(ctx, metaRegion, name, preferCloud, true)
}

// openWorkspace locates a cluster workspace. When preferCloud is "aws" or
// "azure", only that cloud is tried (no cross-cloud fallback — avoids multi-
// second STS/ARM probes on the wrong cloud). readOnly uses OpenAzureReadOnly
// so UI/inspect paths do not acquire or renew the install lock.
func openWorkspace(ctx context.Context, metaRegion, name, preferCloud string, readOnly bool) (*workspace.Workspace, string, string, error) {
	tryAWS := func() (*workspace.Workspace, string, string, error) {
		accountID, awsErr := getAWSAccountID(ctx, "us-east-1")
		if awsErr != nil {
			return nil, "", "", awsErr
		}
		w, err := workspace.Open(ctx, metaRegion, accountID, name)
		if err != nil {
			return nil, "", "", err
		}
		return w, "aws", accountID, nil
	}

	tryAzure := func() (*workspace.Workspace, string, string, error) {
		openFn := workspace.OpenAzure
		if readOnly {
			openFn = workspace.OpenAzureReadOnly
		}
		if serverCredCache != nil {
			if avail, subs, ok := serverCredCache.getAzure(); ok && avail.Authenticated {
				for _, sub := range subs {
					if w, err := openFn(ctx, sub.ID, name); err == nil {
						return w, "azure", sub.ID, nil
					}
				}
			}
		}
		info, err := tools.GetAccountInfo(ctx)
		if err != nil {
			return nil, "", "", err
		}
		w, err := openFn(ctx, info.ID, name)
		if err != nil {
			return nil, "", "", err
		}
		return w, "azure", info.ID, nil
	}

	switch preferCloud {
	case "azure":
		if w, cloud, id, err := tryAzure(); err == nil {
			return w, cloud, id, nil
		}
		return nil, "", "", fmt.Errorf("workspace %q not found or Azure credentials unavailable", name)
	case "aws":
		if w, cloud, id, err := tryAWS(); err == nil {
			return w, cloud, id, nil
		}
		return nil, "", "", fmt.Errorf("workspace %q not found or AWS credentials unavailable", name)
	}

	// No preference: try AWS then Azure (CLI / destroy without cookie).
	if w, cloud, id, err := tryAWS(); err == nil {
		return w, cloud, id, nil
	}
	if w, cloud, id, err := tryAzure(); err == nil {
		return w, cloud, id, nil
	}
	return nil, "", "", fmt.Errorf("workspace %q not found or credentials unavailable on AWS/Azure", name)
}

// DestroyConfirm handles GET /clusters/{name}/destroy — type-to-confirm page.
func DestroyConfirm(t *Templates, metaRegion, region string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		data := destroyPageData{
			Version:     version.Version,
			ClusterName: name,
			Region:      region,
			Cloud:       "aws",
		}

		// Load workspace metadata to display the cluster's actual region.
		ws, cloud, _, err := openWorkspacePreferring(ctx, metaRegion, name, preferredCloud(r))
		if err == nil {
			defer ws.Close()
			data.Cloud = cloud
			if ws.Meta != nil && (ws.Meta.Cloud == "azure" || ws.Meta.AzureLocation != "") {
				data.Cloud = "azure"
			}
			if data.Cloud == "azure" {
				data.Region = ws.Meta.AzureLocation
			} else {
				data.Region = ws.Meta.AWSRegion
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.DestroyConfirm.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

// DestroyApply handles POST /clusters/{name}/destroy — validates the type-to-confirm
// input, starts the destroy goroutine, and redirects to the streaming page.
func DestroyApply(t *Templates, metaRegion, region string, broker *sse.Broker, localMode bool, repoRoot string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		_ = r.ParseForm()

		if r.FormValue("confirm_name") != name {
			http.Redirect(w, r, "/clusters/"+name+"/destroy", http.StatusSeeOther)
			return
		}

		ctx := r.Context()
		ws, cloud, cloudAccountID, err := openWorkspacePreferring(ctx, metaRegion, name, preferredCloud(r))
		if err != nil {
			http.Error(w, "workspace unavailable: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer ws.Close()

		destroyStateBackend := r.FormValue("destroy_state_backend") == "on"

		cfg := &config.Config{
			Cloud:               cloud,
			MetaRegion:          config.Field[string]{Value: metaRegion, Source: "webui"},
			ClusterName:         config.Field[string]{Value: name, Source: "path"},
			DestroyStateBackend: destroyStateBackend,
			Local:               localMode,
			RepoRoot:            repoRoot,
		}

		if cloud == "azure" {
			cfg.AzureLocation = config.Field[string]{Value: ws.Meta.AzureLocation, Source: "workspace"}
		} else {
			cfg.Region = config.Field[string]{Value: ws.Meta.AWSRegion, Source: "workspace"}
			cfg.StateBucket = config.Field[string]{Value: ws.Meta.StateBucket, Source: "workspace"}
			cfg.LockTable = config.Field[string]{Value: ws.Meta.LockTable, Source: "workspace"}
		}

		run := broker.NewRun(name, cloud)
		cond := sse.NewDestroyingConductor(run)

		go func() {
			bgCtx := context.Background()
			err := stages.RunDestroyOrchestrated(bgCtx, cfg, stages.All(), cond)
			result := "success"
			if err != nil {
				result = "failed: " + err.Error()
			}
			invalidateClusterSession(name)
			writeClusterAudit(bgCtx, cloud, metaRegion, cloudAccountID, name, "destroy", result)
		}()

		http.Redirect(w, r, "/clusters/"+name+"/destroy/stream/"+run.SessionID, http.StatusSeeOther)
	}
}

// DestroyStream handles GET /clusters/{name}/destroy/stream/{sessionID} — renders
// the live destroy progress page.
func DestroyStream(t *Templates, broker *sse.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		sessionID := r.PathValue("sessionID")
		run, ok := broker.Get(sessionID)
		if !ok {
			http.NotFound(w, r)
			return
		}

		data := struct {
			Version     string
			ClusterName string
			Cloud       string
			SessionID   string
			StageList   []stageRow
		}{
			Version:     version.Version,
			ClusterName: name,
			Cloud:       run.Cloud,
			SessionID:   sessionID,
			StageList:   destroyStageRows(),
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.DestroyStream.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

// DestroySSE handles GET /clusters/{name}/destroy/sse/{sessionID} — SSE stream.
// Reuses the same SSE protocol as the install wizard.
func DestroySSE(broker *sse.Broker) http.HandlerFunc {
	return InstallSSE(broker)
}
