package handlers

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/stages"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
	"github.com/WolframResearch/WAS-Kubernetes/internal/versions"
	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/sse"
)

// ── Region / location lists ───────────────────────────────────────────────────

var eksRegions = []string{
	"us-east-1", "us-east-2", "us-west-1", "us-west-2",
	"ca-central-1", "eu-west-1", "eu-west-2", "eu-west-3",
	"eu-central-1", "eu-north-1", "eu-south-1",
	"ap-southeast-1", "ap-southeast-2", "ap-northeast-1",
	"ap-northeast-2", "ap-northeast-3", "ap-south-1",
	"sa-east-1", "me-south-1", "af-south-1",
}

var azureLocations = []string{
	"eastus", "eastus2", "westus2", "westus3",
	"centralus", "northcentralus", "southcentralus",
	"westeurope", "northeurope", "uksouth", "ukwest",
	"germanywestcentral", "swedencentral", "switzerlandnorth",
	"japaneast", "japanwest", "australiaeast",
	"southeastasia", "eastasia",
	"brazilsouth",
	"canadacentral", "canadaeast",
}

var nodeTypes = []string{
	"c5.2xlarge", "c5.4xlarge",
	"m5.2xlarge", "m5.4xlarge",
	"r5.2xlarge",
}

var azureVMSizes = []string{
	"Standard_D4s_v5", "Standard_D8s_v5",
	"Standard_D4s_v3", "Standard_D8s_v3",
	"Standard_E4s_v5", "Standard_E8s_v5",
	"Standard_F8s_v2",
}

var clusterNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9\-]{0,98}[a-zA-Z0-9]$|^[a-zA-Z]$`)
var hostnameRe = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`)

// ── Cost estimation ───────────────────────────────────────────────────────────

// hourlyEC2 contains approximate on-demand USD/hour prices in us-east-1.
var hourlyEC2 = map[string]float64{
	"c5.2xlarge": 0.34,
	"c5.4xlarge": 0.68,
	"m5.2xlarge": 0.384,
	"m5.4xlarge": 0.768,
	"r5.2xlarge": 0.504,
}

// hourlyAzureVM contains approximate pay-as-you-go USD/hour prices (eastus).
var hourlyAzureVM = map[string]float64{
	"Standard_D4s_v5": 0.192,
	"Standard_D8s_v5": 0.384,
	"Standard_D4s_v3": 0.192,
	"Standard_D8s_v3": 0.384,
	"Standard_E4s_v5": 0.252,
	"Standard_E8s_v5": 0.504,
	"Standard_F8s_v2": 0.338,
}

const (
	hoursPerMonth = 730.0
	fixedMonthly  = 146.0 // EKS control plane + NAT + ALB + EFS ~10 GB
	// AKS control plane is free; fixed covers load balancer + Premium Files + bandwidth.
	fixedMonthlyAzure = 80.0
)

func estimateCost(nodeType string, desired int) (low, high string) {
	rate := hourlyEC2[nodeType]
	if rate == 0 {
		rate = 0.40
	}
	base := rate * float64(desired) * hoursPerMonth
	lo := int(base + fixedMonthly)
	hi := int(base*1.15 + fixedMonthly)
	return fmt.Sprintf("$%d", lo), fmt.Sprintf("$%d", hi)
}

func estimateAzureCost(vmSize string, desired int) (low, high string) {
	rate := hourlyAzureVM[vmSize]
	if rate == 0 {
		rate = 0.25
	}
	base := rate * float64(desired) * hoursPerMonth
	lo := int(base + fixedMonthlyAzure)
	hi := int(base*1.15 + fixedMonthlyAzure)
	return fmt.Sprintf("$%d", lo), fmt.Sprintf("$%d", hi)
}

// ── installFormData carries all wizard values ────────────────────────────────

type installFormData struct {
	// Wizard: cloud and cluster basics
	Cloud       string // "aws" (default) or "azure"
	ClusterName string
	Region      string // AWS region or Azure location (label depends on cloud)
	K8sVersion  string
	// Wizard: ingress
	IngressHost string
	// Wizard: node pool sizing
	NodeType    string // EC2 instance type or Azure VM SKU
	NodeMin     string
	NodeDesired string
	NodeMax     string
	// Wizard: service replica hints
	AWESReplicas string
	RMReplicas   string
	EMReplicas   string
	// Optional add-ons ("1" enabled, "0" skipped)
	CertManager   string
	Prometheus    string
	MetricsServer string
	Kafka         string
	KafkaHost     string
	KafkaPort     string
	// FromTab is true when Install was started from a cloud home button;
	// the cloud selector is then hidden because it is already chosen.
	FromTab bool
	// Validation errors for the active wizard page
	Errors map[string]string
}

func formDataFromRequest(r *http.Request) installFormData {
	_ = r.ParseForm()
	cloud := r.FormValue("cloud")
	if cloud != "azure" {
		cloud = "aws"
	}
	fromTab := r.FormValue("from_tab") == "1"

	// Checkbox handling: default to "0" if step 4 has been submitted but key is missing from form
	certManager := r.FormValue("cert_manager")
	prometheus := r.FormValue("prometheus")
	metricsServer := r.FormValue("metrics_server")
	kafka := r.FormValue("kafka")

	if r.Method == "POST" && r.FormValue("step4_submitted") == "1" {
		if certManager != "1" {
			certManager = "0"
		}
		if prometheus != "1" {
			prometheus = "0"
		}
		if metricsServer != "1" {
			metricsServer = "0"
		}
		if kafka != "1" {
			kafka = "0"
		}
	}

	return installFormData{
		Cloud:         cloud,
		ClusterName:   strings.TrimSpace(r.FormValue("cluster_name")),
		Region:        r.FormValue("region"),
		K8sVersion:    r.FormValue("k8s_version"),
		IngressHost:   strings.TrimSpace(r.FormValue("ingress_host")),
		NodeType:      r.FormValue("node_type"),
		NodeMin:       r.FormValue("node_min"),
		NodeDesired:   r.FormValue("node_desired"),
		NodeMax:       r.FormValue("node_max"),
		AWESReplicas:  r.FormValue("awes_replicas"),
		RMReplicas:    r.FormValue("rm_replicas"),
		EMReplicas:    r.FormValue("em_replicas"),
		CertManager:   certManager,
		Prometheus:    prometheus,
		MetricsServer: metricsServer,
		Kafka:         kafka,
		KafkaHost:     strings.TrimSpace(r.FormValue("kafka_host")),
		KafkaPort:     strings.TrimSpace(r.FormValue("kafka_port")),
		FromTab:       fromTab,
		Errors:        make(map[string]string),
	}
}

func (d *installFormData) applyDefaults() {
	if d.Cloud == "" {
		d.Cloud = "aws"
	}
	if d.Region == "" {
		if d.Cloud == "azure" {
			d.Region = "eastus"
		} else {
			d.Region = "us-east-1"
		}
	}
	if d.K8sVersion == "" {
		d.K8sVersion = versions.DefaultClusterK8s(d.Cloud)
	}
	if d.NodeType == "" {
		if d.Cloud == "azure" {
			d.NodeType = "Standard_D4s_v5"
		} else {
			d.NodeType = "c5.2xlarge"
		}
	}
	if d.NodeMin == "" {
		d.NodeMin = "2"
	}
	if d.NodeDesired == "" {
		d.NodeDesired = "2"
	}
	if d.NodeMax == "" {
		d.NodeMax = "10"
	}
	if d.AWESReplicas == "" {
		d.AWESReplicas = "2"
	}
	if d.RMReplicas == "" {
		d.RMReplicas = "2"
	}
	if d.EMReplicas == "" {
		d.EMReplicas = "2"
	}
	if d.CertManager == "" {
		// AWS: off by default — LE cannot issue for *.elb.amazonaws.com.
		// Azure: on — *.cloudapp.azure.com works with Let's Encrypt.
		if d.Cloud == "aws" {
			d.CertManager = "0"
		} else {
			d.CertManager = "1"
		}
	}
	if d.Prometheus == "" {
		d.Prometheus = "1"
	}
	if d.MetricsServer == "" {
		d.MetricsServer = "1"
	}
	if d.Kafka == "" {
		d.Kafka = "1"
	}
}

// ── installPageData is passed to all install templates ───────────────────────

type installPageData struct {
	Version        string
	Form           installFormData
	EKSRegions     []string
	AzureLocations []string
	K8sVersions    []string
	NodeTypes      []string
	AzureVMSizes   []string
	// Preflight summary extras
	CostLow   string
	CostHigh  string
	StageList []stageRow
	// FromTab is true when the wizard was opened from a cloud tab ("Install new
	// cluster" button). The cloud selector is hidden; a "Change cloud" link is
	// shown instead.
	FromTab bool
}

type stageRow struct {
	Num   int
	Name  string
	Label string
	ETA   string
}

func allStageRows() []stageRow {
	all := stages.All()
	rows := make([]stageRow, len(all))
	for i, s := range all {
		rows[i] = stageRow{
			Num:   i + 1,
			Name:  s.Name(),
			Label: s.Label(),
			ETA:   s.EstimateText(),
		}
	}
	return rows
}

// destroyStageRows returns stages in the order they run during destroy (reverse
// of install order). Used by the destroy stream page to pre-render the stage list.
func destroyStageRows() []stageRow {
	all := stages.All()
	rows := make([]stageRow, len(all))
	for i := range all {
		s := all[len(all)-1-i]
		rows[i] = stageRow{
			Num:   i + 1,
			Name:  s.Name(),
			Label: s.DestroyLabel(),
			ETA:   s.EstimateText(),
		}
	}
	return rows
}

func newInstallData(form installFormData) installPageData {
	cloud := form.Cloud
	if cloud == "" {
		cloud = "aws"
	}
	return installPageData{
		Version:        version.Version,
		Form:           form,
		EKSRegions:     eksRegions,
		AzureLocations: azureLocations,
		K8sVersions:    versions.K8sInstallChoices(versions.DefaultClusterK8s(cloud)),
		NodeTypes:      nodeTypes,
		AzureVMSizes:   azureVMSizes,
		StageList:      allStageRows(),
		FromTab:        form.FromTab,
	}
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// InstallWizard handles GET /install — shows step 1 (initial form).
// If ?cloud=aws or ?cloud=azure is present (e.g. from a tab's "Install" button),
// the cloud is pre-selected and the cloud selector is hidden.
func InstallWizard(t *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		form := installFormData{Errors: make(map[string]string)}
		if c := r.URL.Query().Get("cloud"); c == "aws" || c == "azure" {
			form.Cloud = c
			form.FromTab = true
		}
		form.applyDefaults()
		data := newInstallData(form)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.InstallWizard.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

// InstallStep2 handles POST /install/step2 — validates step 1, renders step 2.
func InstallStep2(t *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		form := formDataFromRequest(r)
		if !clusterNameRe.MatchString(form.ClusterName) {
			form.Errors["cluster_name"] = "Must start with a letter, contain only letters, digits, hyphens, and be ≤ 100 characters."
		}
		if form.Region == "" {
			if form.Cloud == "azure" {
				form.Errors["region"] = "Location is required."
			} else {
				form.Errors["region"] = "Region is required."
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if len(form.Errors) > 0 {
			data := newInstallData(form)
			if err := t.InstallWizard.ExecuteTemplate(w, "_install_step1", data); err != nil {
				renderErr(w, err)
			}
			return
		}
		data := newInstallData(form)
		if err := t.InstallWizard.ExecuteTemplate(w, "_install_step2", data); err != nil {
			renderErr(w, err)
		}
	}
}

// InstallStep3 handles POST /install/step3 — validates step 2, renders step 3.
func InstallStep3(t *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		form := formDataFromRequest(r)
		form.applyDefaults()
		if form.IngressHost != "" && !hostnameRe.MatchString(form.IngressHost) {
			form.Errors["ingress_host"] = "Enter a valid DNS hostname (e.g. was.example.com)."
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if len(form.Errors) > 0 {
			data := newInstallData(form)
			if err := t.InstallWizard.ExecuteTemplate(w, "_install_step2", data); err != nil {
				renderErr(w, err)
			}
			return
		}
		data := newInstallData(form)
		if err := t.InstallWizard.ExecuteTemplate(w, "_install_step3", data); err != nil {
			renderErr(w, err)
		}
	}
}

// InstallStep4 handles POST /install/step4 — validates step 3, renders step 4.
func InstallStep4(t *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		form := formDataFromRequest(r)
		form.applyDefaults()
		validateNodeGroup(&form)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if len(form.Errors) > 0 {
			data := newInstallData(form)
			if err := t.InstallWizard.ExecuteTemplate(w, "_install_step3", data); err != nil {
				renderErr(w, err)
			}
			return
		}
		data := newInstallData(form)
		if err := t.InstallWizard.ExecuteTemplate(w, "_install_step4", data); err != nil {
			renderErr(w, err)
		}
	}
}

// InstallPreview handles POST /install/preview — validates all steps, renders
// the preflight summary as a full page (not an HTMX fragment).
func InstallPreview(t *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		form := formDataFromRequest(r)
		form.applyDefaults()
		validateAll(&form)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if len(form.Errors) > 0 {
			// Restart from step 1 on wholesale re-validation failure.
			data := newInstallData(form)
			if err := t.InstallWizard.ExecuteTemplate(w, "layout", data); err != nil {
				renderErr(w, err)
			}
			return
		}
		desired, _ := strconv.Atoi(form.NodeDesired)
		var lo, hi string
		if form.Cloud == "azure" {
			lo, hi = estimateAzureCost(form.NodeType, desired)
		} else {
			lo, hi = estimateCost(form.NodeType, desired)
		}
		data := newInstallData(form)
		data.CostLow = lo
		data.CostHigh = hi
		if err := t.InstallPreview.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

// InstallConfirm handles POST /install/confirm — starts the install goroutine
// and redirects to the SSE streaming page.
func InstallConfirm(t *Templates, metaRegion string, broker *sse.Broker, localMode bool, repoRoot string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		form := formDataFromRequest(r)
		form.applyDefaults()
		validateAll(&form)
		if len(form.Errors) > 0 {
			http.Redirect(w, r, "/install", http.StatusSeeOther)
			return
		}

		ctx := r.Context()

		cfg := buildConfig(form, metaRegion, localMode, repoRoot)
		run := broker.NewRun(form.ClusterName, form.Cloud)
		run.Form = form
		cond := sse.NewConductor(run)

		clusterName := form.ClusterName
		accountID := ""

		if form.Cloud == "azure" {
			// Quick Azure sanity check before committing.
			info, azErr := tools.GetAccountInfo(ctx)
			if azErr != nil {
				http.Error(w, "Azure credentials unavailable: "+azErr.Error(), http.StatusBadGateway)
				return
			}
			accountID = info.ID
		} else {
			// Quick AWS sanity check before committing.
			callerID, awsErr := tools.GetCallerIdentity(ctx, form.Region)
			if awsErr != nil {
				http.Error(w, "AWS credentials unavailable: "+awsErr.Error(), http.StatusBadGateway)
				return
			}
			accountID = callerID.Account
		}

		// Background context: install outlives the HTTP request.
		go func() {
			bgCtx := context.Background()
			err := stages.RunOrchestrated(bgCtx, cfg, stages.All(), cond)
			result := "success"
			if err != nil {
				result = "failed: " + err.Error()
			}
			writeClusterAudit(bgCtx, form.Cloud, metaRegion, accountID, clusterName, "install", result)
		}()

		http.Redirect(w, r, "/install/stream/"+run.SessionID, http.StatusSeeOther)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func buildConfig(form installFormData, metaRegion string, localMode bool, repoRoot string) *config.Config {
	src := "webui"
	cfg := &config.Config{
		Cloud:       form.Cloud,
		MetaRegion:  config.Field[string]{Value: metaRegion, Source: src},
		ClusterName: config.Field[string]{Value: form.ClusterName, Source: src},
		K8sVersion:  config.Field[string]{Value: form.K8sVersion, Source: src},
		IngressHost: config.Field[string]{Value: form.IngressHost, Source: src},
		NodeType:    config.Field[string]{Value: form.NodeType, Source: src},
		NodeMin:     config.Field[string]{Value: form.NodeMin, Source: src},
		NodeDesired: config.Field[string]{Value: form.NodeDesired, Source: src},
		NodeMax:     config.Field[string]{Value: form.NodeMax, Source: src},
		Local:       localMode,
		RepoRoot:    repoRoot,
	}
	if form.Cloud == "azure" {
		cfg.AzureLocation = config.Field[string]{Value: form.Region, Source: src}
	} else {
		cfg.Region = config.Field[string]{Value: form.Region, Source: src}
	}

	var skips []string
	if form.CertManager != "1" {
		skips = append(skips, "cert-manager")
	}
	if form.Prometheus != "1" {
		skips = append(skips, "kube-prometheus-stack", "prometheus-adapter")
	}
	if form.MetricsServer != "1" {
		skips = append(skips, "metrics-server")
	}

	kafkaMode := "builtin"
	kafkaBootstrap := ""
	if form.Kafka != "1" {
		skips = append(skips, "strimzi-kafka-operator")
		kafkaMode = "external"
		if form.KafkaHost != "" && form.KafkaPort != "" {
			kafkaBootstrap = form.KafkaHost + ":" + form.KafkaPort
		}
	}

	cfg.AddonsSkip = config.Field[string]{Value: strings.Join(skips, ","), Source: src}
	cfg.KafkaMode = config.Field[string]{Value: kafkaMode, Source: src}
	cfg.KafkaBootstrapServers = config.Field[string]{Value: kafkaBootstrap, Source: src}

	return cfg
}

func validateNodeGroup(form *installFormData) {
	min, minErr := strconv.Atoi(form.NodeMin)
	des, desErr := strconv.Atoi(form.NodeDesired)
	max, maxErr := strconv.Atoi(form.NodeMax)
	if minErr != nil || min < 1 {
		form.Errors["node_min"] = "Must be a number ≥ 1."
	}
	if desErr != nil || des < 1 {
		form.Errors["node_desired"] = "Must be a number ≥ 1."
	}
	if maxErr != nil || max < 1 {
		form.Errors["node_max"] = "Must be a number ≥ 1."
	}
	if minErr == nil && desErr == nil && des < min {
		form.Errors["node_desired"] = "Must be ≥ minimum nodes."
	}
	if desErr == nil && maxErr == nil && max < des {
		form.Errors["node_max"] = "Must be ≥ desired nodes."
	}
}

func validateAll(form *installFormData) {
	if !clusterNameRe.MatchString(form.ClusterName) {
		form.Errors["cluster_name"] = "Invalid cluster name."
	}
	if form.Region == "" {
		if form.Cloud == "azure" {
			form.Errors["region"] = "Location is required."
		} else {
			form.Errors["region"] = "Region is required."
		}
	}
	if form.IngressHost != "" && !hostnameRe.MatchString(form.IngressHost) {
		form.Errors["ingress_host"] = "Invalid hostname."
	}
	validateNodeGroup(form)
	validateKafkaExternal(form)
}

func validateKafkaExternal(form *installFormData) {
	if form.Kafka != "1" {
		if form.KafkaHost == "" {
			form.Errors["kafka_host"] = "Bootstrap host is required for external Kafka."
		}
		if form.KafkaPort == "" {
			form.Errors["kafka_port"] = "Bootstrap port is required."
		} else {
			port, err := strconv.Atoi(form.KafkaPort)
			if err != nil || port < 1 || port > 65535 {
				form.Errors["kafka_port"] = "Must be a valid port (1-65535)."
			}
		}
	}
}

// InstallRetry handles GET /install/retry/{sessionID} — pre-fills the install wizard
// with the form values from a prior failed install run.
func InstallRetry(t *Templates, broker *sse.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("sessionID")
		run, ok := broker.Get(sessionID)
		if !ok || run.Form == nil {
			// Fallback to blank wizard if session is gone/GC'ed
			http.Redirect(w, r, "/install", http.StatusSeeOther)
			return
		}

		form, ok := run.Form.(installFormData)
		if !ok {
			http.Redirect(w, r, "/install", http.StatusSeeOther)
			return
		}

		// Clear errors and page-specific flow triggers for retry
		form.Errors = make(map[string]string)

		data := newInstallData(form)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.InstallWizard.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}
