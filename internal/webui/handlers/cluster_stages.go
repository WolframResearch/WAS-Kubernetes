package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/addons"
	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/stages"
	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/sse"
)

// stagePickerRow is one row in the Stages tab picker.
type stagePickerRow struct {
	Num         int
	Name        string
	Label       string
	ETA         string
	Description string
	// Risk is a short customer-facing note for infrastructure-heavy stages.
	Risk string
	// DefaultChecked is true for the common repair path (addons + app).
	DefaultChecked bool
}

// addonPickerRow is one checkbox in the Stages tab add-on sub-picker.
type addonPickerRow struct {
	Name           string
	Label          string
	Hint           string
	Optional       bool
	DefaultChecked bool
}

func stagePickerRows() []stagePickerRow {
	all := stages.All()
	rows := make([]stagePickerRow, len(all))
	for i, s := range all {
		row := stagePickerRow{
			Num:         i + 1,
			Name:        s.Name(),
			Label:       s.Label(),
			ETA:         s.EstimateText(),
			Description: s.Description(),
		}
		switch s.Name() {
		case "bootstrap", "backend", "infra":
			row.Risk = "Touches cloud infrastructure — usually not needed for app/addon fixes."
		case "addons", "app":
			row.DefaultChecked = true
		}
		rows[i] = row
	}
	return rows
}

func addonPickerRows(cloud string) []addonPickerRow {
	rows := make([]addonPickerRow, 0)
	for _, a := range addons.AddonsFor(cloud) {
		row := addonPickerRow{
			Name:           a.Name(),
			Label:          a.Name(),
			DefaultChecked: true,
		}
		switch a.Name() {
		case "ingress-nginx":
			row.Label = "ingress-nginx"
			row.Hint = "Required for WAS Ingress routes."
		case "aws-efs-csi-driver":
			row.Label = "EFS CSI driver"
			row.Hint = "Required for was-efs StorageClass (AWS)."
		case "was-efs-storageclass":
			row.Label = "was-efs StorageClass"
			row.Hint = "RWX log volumes on EFS."
		case "aws-ebs-csi-driver":
			row.Label = "EBS CSI driver"
			row.Hint = "Required for Kafka broker volumes (AWS)."
		case "was-aws-kafka-storageclass":
			row.Label = "was-kafka-gp3 StorageClass"
			row.Hint = "gp3 disks for Kafka brokers (EBS CSI)."
		case "was-azurefile-storageclass":
			row.Label = "was-azurefile StorageClass"
			row.Hint = "RWX log volumes on Azure Files (wires Terraform account)."
		case "was-azure-kafka-storageclass":
			row.Label = "kafka-standardssd-xfs StorageClass"
			row.Hint = "Disks for Kafka brokers."
		case "strimzi-kafka-operator":
			row.Label = "Strimzi Kafka operator"
			row.Hint = "Required unless you use external Kafka."
		case "metrics-server":
			row.Label = "metrics-server"
			row.Hint = "Optional — AKS often already has it; wasctl skips if present."
			row.Optional = true
		case "kube-prometheus-stack":
			row.Label = "Prometheus stack"
			row.Hint = "Optional — monitoring / custom metrics."
			row.Optional = true
		case "prometheus-adapter":
			row.Label = "prometheus-adapter"
			row.Hint = "Optional — needed for custom-metrics HPAs."
			row.Optional = true
		case "cert-manager":
			row.Label = "cert-manager"
			row.Hint = "TLS via Let's Encrypt. Azure: on by default. AWS: only with a custom DNS name (not the ELB hostname)."
			row.Optional = true
		}
		rows = append(rows, row)
	}
	return rows
}

func stageRowsFromNames(names []string) []stageRow {
	list, err := stages.Select(names)
	if err != nil {
		return nil
	}
	rows := make([]stageRow, len(list))
	for i, s := range list {
		rows[i] = stageRow{
			Num:   i + 1,
			Name:  s.Name(),
			Label: s.Label(),
			ETA:   s.EstimateText(),
		}
	}
	return rows
}

// addonsSkipCSV returns a comma-separated skip list: every cloud addon not in selected.
func addonsSkipCSV(cloud string, selected []string) string {
	want := make(map[string]bool, len(selected))
	for _, s := range selected {
		s = strings.TrimSpace(s)
		if s != "" {
			want[s] = true
		}
	}
	var skip []string
	for _, name := range addons.NamesFor(cloud) {
		if !want[name] {
			skip = append(skip, name)
		}
	}
	return strings.Join(skip, ",")
}

func stageListIncludes(list []stages.Stage, name string) bool {
	for _, s := range list {
		if s.Name() == name {
			return true
		}
	}
	return false
}

// configFromWorkspace builds an install Config from workspace metadata so
// stage re-runs reuse the cluster's existing region / ingress / state settings.
func configFromWorkspace(wsMeta *metabucket.Metadata, cloud, metaRegion string, localMode bool, repoRoot string) *config.Config {
	src := "workspace"
	cfg := &config.Config{
		Cloud:       cloud,
		MetaRegion:  config.Field[string]{Value: metaRegion, Source: "webui"},
		ClusterName: config.Field[string]{Value: wsMeta.ClusterName, Source: src},
		IngressHost: config.Field[string]{Value: wsMeta.IngressHost, Source: src},
		Local:       localMode,
		RepoRoot:    repoRoot,
	}
	if cloud == "azure" {
		cfg.AzureLocation = config.Field[string]{Value: wsMeta.AzureLocation, Source: src}
	} else {
		cfg.Region = config.Field[string]{Value: wsMeta.AWSRegion, Source: src}
		cfg.StateBucket = config.Field[string]{Value: wsMeta.StateBucket, Source: src}
		cfg.LockTable = config.Field[string]{Value: wsMeta.LockTable, Source: src}
	}
	return cfg
}

// ClusterStagesRun handles POST /clusters/{name}/stages/run — starts a
// selective stage re-install and redirects to the live stream page.
func ClusterStagesRun(t *Templates, metaRegion, region string, broker *sse.Broker, localMode bool, repoRoot string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}

		mode := r.FormValue("mode")
		if mode != "from" {
			mode = "selected"
		}

		var toRun []stages.Stage
		var err error
		switch mode {
		case "from":
			start := strings.TrimSpace(r.FormValue("from_stage"))
			if start == "" {
				http.Error(w, "from_stage is required when mode=from", http.StatusBadRequest)
				return
			}
			toRun, err = stages.From(start)
		default:
			selected := r.Form["stage"]
			toRun, err = stages.Select(selected)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		ws, cloud, cloudAccountID, openErr := openWorkspacePreferring(ctx, metaRegion, name, preferredCloud(r))
		if openErr != nil {
			http.Error(w, "workspace unavailable: "+openErr.Error(), http.StatusBadGateway)
			return
		}
		defer ws.Close()

		if ws.Meta == nil {
			http.Error(w, "workspace metadata unavailable", http.StatusBadGateway)
			return
		}

		cfg := configFromWorkspace(ws.Meta, cloud, metaRegion, localMode, repoRoot)
		if cfg.ClusterName.Value == "" {
			cfg.ClusterName = config.Field[string]{Value: name, Source: "path"}
		}

		// When the addons stage is in the run, honor the per-addon checkboxes.
		if stageListIncludes(toRun, "addons") {
			if selectedAddons := r.Form["addon"]; len(selectedAddons) > 0 {
				if skip := addonsSkipCSV(cloud, selectedAddons); skip != "" {
					cfg.AddonsSkip = config.Field[string]{Value: skip, Source: "webui"}
				}
			}
		}

		names := make([]string, len(toRun))
		for i, s := range toRun {
			names[i] = s.Name()
		}

		run := broker.NewRun(name, cloud)
		run.StageNames = names
		run.Heading = "Re-running stages on"
		cond := sse.NewConductorForStages(run, toRun)

		go func() {
			bgCtx := context.Background()
			runErr := stages.RunOrchestrated(bgCtx, cfg, toRun, cond)
			result := "success"
			if runErr != nil {
				result = "failed: " + runErr.Error()
			}
			writeClusterAudit(bgCtx, cloud, metaRegion, cloudAccountID, name, "stages-rerun:"+strings.Join(names, ","), result)
		}()

		http.Redirect(w, r, "/install/stream/"+run.SessionID, http.StatusSeeOther)
	}
}

// stagesSectionData is rendered into _cluster_stages.html.
type stagesSectionData struct {
	ClusterName string
	Cloud       string
	Stages      []stagePickerRow
	Addons      []addonPickerRow
	Error       string
}

func renderStagesSection(w http.ResponseWriter, t *Templates, name string, meta *metabucket.Metadata, sectionErr string) {
	cloud := "aws"
	if meta != nil {
		if meta.Cloud == "azure" || (meta.Cloud == "" && meta.AzureLocation != "") {
			cloud = "azure"
		}
	}
	data := stagesSectionData{
		ClusterName: name,
		Cloud:       cloud,
		Stages:      stagePickerRows(),
		Addons:      addonPickerRows(cloud),
		Error:       sectionErr,
	}
	// Stages form is usable without live inspect; only block submit messaging
	// when we truly have no cloud/metadata. Soften credential flakes.
	if sectionErr != "" && meta != nil {
		data.Error = "" // meta loaded — ignore inspect-adjacent noise
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Cluster.ExecuteTemplate(w, "_cluster_stages", data); err != nil {
		renderErr(w, fmt.Errorf("stages section: %w", err))
	}
}
