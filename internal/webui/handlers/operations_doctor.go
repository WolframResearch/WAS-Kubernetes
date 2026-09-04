package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

// findingGroup groups findings by category for template rendering.
type findingGroup struct {
	Category string
	Findings []doctor.Finding
}

type opsDoctorData struct {
	Version     string
	ClusterName string
	ActiveTab   string
	Cloud       string
	Report      *doctor.Report
	Groups      []findingGroup
	ReportError string
	ReportID    string
	IsHistory   bool
}

// OpsDoctor handles GET /clusters/{name}/operations — last doctor report.
func OpsDoctor(t *Templates, metaRegion, region string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		reportID := r.URL.Query().Get("report")
		prefer := preferredCloud(r)
		data := opsDoctorData{
			Version:     version.Version,
			ClusterName: name,
			ActiveTab:   "doctor",
			Cloud:       prefer,
			IsHistory:   reportID != "",
		}

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		rep, id, cloud, errMsg := loadDoctorReport(ctx, metaRegion, name, reportID, prefer)
		data.Report = rep
		data.ReportError = errMsg
		data.ReportID = id
		if cloud != "" {
			data.Cloud = cloud
		}
		if rep != nil {
			data.Groups = groupFindings(rep.Findings, data.Cloud)
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.OpsDoctor.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

func loadDoctorReport(ctx context.Context, metaRegion, clusterName, id, preferCloud string) (*doctor.Report, string, string, string) {
	store, cloud, err := openReportStore(ctx, metaRegion, clusterName, preferCloud)
	if err != nil {
		return nil, "", "", err.Error()
	}

	if id == "" {
		entries, err := workspace.ListReportEntries(ctx, store, clusterName)
		if err != nil || len(entries) == 0 {
			return nil, "", cloud, ""
		}
		id = entries[0].ID
	}

	data, err := workspace.LoadReport(ctx, store, clusterName, id)
	if err != nil {
		return nil, "", cloud, "could not load report: " + err.Error()
	}

	var rep doctor.Report
	if err := json.Unmarshal(data, &rep); err != nil {
		return nil, "", cloud, "corrupt report JSON: " + err.Error()
	}
	return &rep, id, cloud, ""
}

func groupFindings(findings []doctor.Finding, cloud string) []findingGroup {
	seen := map[string]int{}
	var groups []findingGroup

	for _, f := range findings {
		if cloud != "" && !findingAppliesToCloud(f.CheckID, cloud) {
			continue
		}
		cat := categoryFromID(f.CheckID)
		idx, ok := seen[cat]
		if !ok {
			idx = len(groups)
			seen[cat] = idx
			groups = append(groups, findingGroup{Category: cat})
		}
		groups[idx].Findings = append(groups[idx].Findings, f)
	}
	return groups
}

func findingAppliesToCloud(id, cloud string) bool {
	awsOnly := strings.HasPrefix(id, "aws.") ||
		strings.HasPrefix(id, "meta_bucket.") ||
		id == "cli.aws" ||
		id == "cluster.irsa"
	azureOnly := strings.HasPrefix(id, "azure.") ||
		strings.HasPrefix(id, "meta_container.") ||
		id == "cli.az" ||
		id == "cli.kubelogin" ||
		id == "cluster.workload_identity"

	switch cloud {
	case "azure":
		return !awsOnly
	default:
		return !azureOnly
	}
}

func categoryFromID(id string) string {
	switch {
	case strings.HasPrefix(id, "aws."), strings.HasPrefix(id, "meta_bucket."):
		return "AWS"
	case strings.HasPrefix(id, "azure."), strings.HasPrefix(id, "meta_container."):
		return "Azure"
	case strings.HasPrefix(id, "cluster."), strings.HasPrefix(id, "k8s."):
		return "Cluster"
	case strings.HasPrefix(id, "app."), strings.HasPrefix(id, "chart."),
		strings.HasPrefix(id, "kafka."), strings.HasPrefix(id, "was."):
		return "Application"
	default:
		return "Environment"
	}
}
