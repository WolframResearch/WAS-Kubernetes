package handlers

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/audit"
	"github.com/WolframResearch/WAS-Kubernetes/internal/inspect"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

// clusterPageData is the top-level data for the cluster detail page.
type clusterPageData struct {
	Version      string
	ClusterName  string
	Report       *inspect.Report
	Meta         *metabucket.Metadata
	AuditEntries []audit.Entry
	Error        string
	// SectionError is populated by the HTMX section-refresh endpoint.
	SectionError string
}

// ClusterDetail handles GET /clusters/{name} — the full cluster info page.
func ClusterDetail(t *Templates, metaRegion, region string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		data := clusterPageData{
			Version:     version.Version,
			ClusterName: name,
		}

		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()

		// Fast shell: metadata only (no kubectl). Tabs load their own sections.
		meta, errMsg := loadClusterMeta(ctx, metaRegion, name, preferredCloud(r))
		data.Meta = meta
		data.Error = errMsg

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.Cluster.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

type cachedReport struct {
	Report       *inspect.Report
	Meta         *metabucket.Metadata
	AuditEntries []audit.Entry
	Error        string
	Timestamp    time.Time
}

var (
	reportCache   = make(map[string]cachedReport)
	reportCacheMu sync.Mutex
)

// metaOnlySections need workspace.json only — no kubeconfig / kubectl.
var metaOnlySections = map[string]bool{
	"stages": true,
	"config": true,
}

// ClusterSection handles GET /clusters/{name}/sections/{section} — HTMX
// partial refresh for a single inspector section. Returns an HTML fragment
// (no surrounding layout) for HTMX to swap in place.
func ClusterSection(t *Templates, metaRegion, region string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		section := r.PathValue("section")
		refresh := r.URL.Query().Get("refresh") == "true"

		prefer := preferredCloud(r)

		if metaOnlySections[section] {
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()
			meta, errMsg := loadClusterMeta(ctx, metaRegion, name, prefer)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			switch section {
			case "stages":
				renderStagesSection(w, t, name, meta, errMsg)
			default:
				data := clusterPageData{
					Version:      version.Version,
					ClusterName:  name,
					Meta:         meta,
					SectionError: errMsg,
				}
				if err := t.Cluster.ExecuteTemplate(w, "_cluster_"+section, data); err != nil {
					renderErr(w, err)
				}
			}
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()

		var report *inspect.Report
		var meta *metabucket.Metadata
		var auditEntries []audit.Entry
		var errMsg string

		cacheKey := name + ":" + section
		useCache := false
		if !refresh {
			reportCacheMu.Lock()
			cached, ok := reportCache[cacheKey]
			if ok && time.Since(cached.Timestamp) < 2*time.Minute {
				report = cached.Report
				meta = cached.Meta
				auditEntries = cached.AuditEntries
				errMsg = cached.Error
				useCache = true
			}
			reportCacheMu.Unlock()
		}

		if !useCache {
			filters := sectionInspectFilters(section)
			var err string
			report, meta, auditEntries, err = inspectCluster(ctx, metaRegion, region, name, prefer, filters...)
			errMsg = err

			if errMsg == "" {
				reportCacheMu.Lock()
				reportCache[cacheKey] = cachedReport{
					Report:       report,
					Meta:         meta,
					AuditEntries: auditEntries,
					Error:        errMsg,
					Timestamp:    time.Now(),
				}
				reportCacheMu.Unlock()
			}
		}

		data := clusterPageData{
			Version:      version.Version,
			ClusterName:  name,
			Report:       report,
			Meta:         meta,
			AuditEntries: auditEntries,
			SectionError: errMsg,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmplName := "_cluster_" + section
		if err := t.Cluster.ExecuteTemplate(w, tmplName, data); err != nil {
			renderErr(w, err)
		}
	}
}

// sectionInspectFilters maps a UI tab to inspector section names.
// Narrow filters keep tab clicks fast (no full-cluster kubectl sweep).
func sectionInspectFilters(section string) []string {
	switch section {
	case "overview":
		return []string{"overview", "nodes"}
	case "workloads":
		return []string{"workloads"}
	case "kafka":
		return []string{"kafka"}
	case "storage":
		return []string{"storage"}
	case "addons":
		return []string{"addons"}
	case "activity":
		return []string{"activity"}
	case "none":
		return []string{"none"}
	default:
		return nil // all sections
	}
}

// loadClusterMeta opens the workspace for metadata only (no kubeconfig).
func loadClusterMeta(ctx context.Context, metaRegion, clusterName, preferCloud string) (*metabucket.Metadata, string) {
	s, err := getClusterSession(ctx, metaRegion, clusterName, preferCloud, false)
	if err != nil {
		return nil, err.Error()
	}
	return s.Meta, ""
}

// inspectCluster opens the workspace and runs the inspector. sections is
// optional; empty means all sections. Returns audit entries as a best-effort
// read — a failure to load audit entries is silently ignored.
//
// Kubeconfig generation (az aks get-credentials / aws eks update-kubeconfig)
// is cached in getClusterSession so tab clicks do not re-run it every time.
func inspectCluster(ctx context.Context, metaRegion, region, clusterName, preferCloud string, sections ...string) (*inspect.Report, *metabucket.Metadata, []audit.Entry, string) {
	needKube := !(len(sections) == 1 && sections[0] == "none")
	s, err := getClusterSession(ctx, metaRegion, clusterName, preferCloud, needKube)
	if err != nil {
		return nil, nil, nil, err.Error()
	}

	meta := s.Meta
	auditEntries := s.AuditEntries

	if !needKube {
		report := &inspect.Report{
			Generated: time.Now().UTC(),
			Cluster: inspect.ClusterInfo{
				Name:   meta.ClusterName,
				Region: clusterRegion(meta),
				ARN:    meta.ClusterARN,
				Status: meta.Status,
			},
		}
		return report, meta, auditEntries, ""
	}

	ws := &workspace.Workspace{Meta: meta}
	insp := inspect.New(ws, s.KubeconfigPath, s.ContextName)
	report, err := insp.Inspect(ctx, sections)
	if err != nil {
		return nil, meta, auditEntries, "inspect failed: " + err.Error()
	}

	if meta != nil {
		status := clusterDisplayStatus(meta)
		meta.Status = status
		if report != nil {
			report.Cluster.Status = status
		}
	}

	return report, meta, auditEntries, ""
}

func clusterRegion(meta *metabucket.Metadata) string {
	if meta == nil {
		return ""
	}
	if meta.Cloud == "azure" || (meta.Cloud == "" && meta.AzureLocation != "") {
		return meta.AzureLocation
	}
	return meta.AWSRegion
}
