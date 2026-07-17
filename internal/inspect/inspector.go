// Package inspect provides the wasctl info runtime health view. All inspectors
// run in parallel with per-section 3-second timeouts; individual failures are
// recorded in Report.Errors rather than aborting the whole report.
package inspect

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

// Inspector orchestrates all section queries for wasctl info.
type Inspector struct {
	ws             *workspace.Workspace
	kubeconfigPath string
	contextName    string
}

// New returns an Inspector for the given workspace and isolated kubeconfig.
func New(ws *workspace.Workspace, kubeconfigPath, contextName string) *Inspector {
	return &Inspector{ws: ws, kubeconfigPath: kubeconfigPath, contextName: contextName}
}

// Report holds the output of all section queries.
type Report struct {
	Cluster   ClusterInfo     `json:"cluster"`
	Nodes     NodesInfo       `json:"nodes"`
	Ingress   IngressInfo     `json:"ingress"`
	Workloads []WorkloadInfo  `json:"workloads"`
	Kafka     KafkaInfo       `json:"kafka"`
	Storage   StorageInfo     `json:"storage"`
	AddOns    []AddOnInfo     `json:"addons"`
	Activity  []ActivityEntry `json:"activity"`
	Errors    []SectionError  `json:"errors,omitempty"`
	Generated time.Time       `json:"generated"`
}

type ClusterInfo struct {
	Name       string `json:"name"`
	Region     string `json:"region"`
	ARN        string `json:"arn"`
	Version    string `json:"version"`
	Status     string `json:"status"`
	CreatedAt  string `json:"createdAt"`
}

type NodesInfo struct {
	Ready int    `json:"ready"`
	Total int    `json:"total"`
	Type  string `json:"instanceType"`
}

type IngressInfo struct {
	URL     string `json:"url"`
	ELB     string `json:"elb"`
	DNSok   bool   `json:"dnsOk"`
	TLSDays int    `json:"tlsDaysUntilExpiry"`
}

type WorkloadInfo struct {
	Name      string `json:"name"`
	Ready     int    `json:"ready"`
	Desired   int    `json:"desired"`
	UptimeStr string `json:"uptime"`
}

type KafkaInfo struct {
	ClusterName    string `json:"clusterName"`
	BrokersReady   int    `json:"brokersReady"`
	BrokersTotal   int    `json:"brokersTotal"`
	TopicsReady    int    `json:"topicsReady"`
	TopicsTotal    int    `json:"topicsTotal"`
	ZKReady        int    `json:"zkReady"`
	ZKTotal        int    `json:"zkTotal"`
}

type StorageInfo struct {
	// Cloud is "aws" or "azure" — templates branch on this for labels.
	Cloud string `json:"cloud"`
	// FilesStatus is the shared/logs filesystem StorageClass status
	// (was-efs on AWS, was-azurefile on Azure).
	FilesStatus string `json:"filesStatus"`
	EFSUsedBytes int64 `json:"efsUsedBytes"` // AWS-only; often unavailable
	PVCBound     int   `json:"pvcBound"`
	PVCTotal     int   `json:"pvcTotal"`
	// ResourceBucket / NodefileBucket hold S3 bucket or Azure container stats.
	ResourceBucket BucketInfo `json:"resourceBucket"`
	NodefileBucket BucketInfo `json:"nodefileBucket"`
}

type BucketInfo struct {
	Name    string `json:"name"`
	Objects int    `json:"objects"`
	Bytes   int64  `json:"bytes"`
}

type AddOnInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Status  string `json:"status"`
}

type ActivityEntry struct {
	Age     string `json:"age"`
	Summary string `json:"summary"`
}

type SectionError struct {
	Section string `json:"section"`
	Error   string `json:"error"`
}

const sectionTimeout = 10 * time.Second
const totalTimeout = 25 * time.Second

// Inspect runs all inspectors in parallel and returns a Report.
// sectionFilter limits which sections are populated; nil means all.
func (i *Inspector) Inspect(ctx context.Context, sectionFilter []string) (*Report, error) {
	ctx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()

	report := &Report{
		Generated: time.Now().UTC(),
		Cluster: ClusterInfo{
			Name:   i.ws.Meta.ClusterName,
			Region: clusterRegion(i.ws),
			ARN:    i.ws.Meta.ClusterARN,
			Status: i.ws.Meta.Status,
		},
	}

	want := func(s string) bool {
		if len(sectionFilter) == 0 {
			return true
		}
		for _, f := range sectionFilter {
			if strings.EqualFold(f, s) {
				return true
			}
			if strings.EqualFold(f, "overview") && strings.EqualFold(s, "nodes") {
				return true
			}
		}
		return false
	}

	var mu sync.Mutex
	addErr := func(section string, err error) {
		if err != nil {
			mu.Lock()
			report.Errors = append(report.Errors, SectionError{Section: section, Error: err.Error()})
			mu.Unlock()
		}
	}

	var wg sync.WaitGroup
	run := func(section string, fn func() error) {
		if !want(section) {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sctx, scancel := context.WithTimeout(ctx, sectionTimeout)
			defer scancel()
			_ = sctx
			addErr(section, fn())
		}()
	}

	run("nodes", func() error {
		info, err := i.collectNodes(ctx)
		if err == nil {
			mu.Lock()
			report.Nodes = info
			mu.Unlock()
		}
		return err
	})

	run("workloads", func() error {
		wls, err := i.collectWorkloads(ctx)
		if err == nil {
			mu.Lock()
			report.Workloads = wls
			mu.Unlock()
		}
		return err
	})

	run("kafka", func() error {
		info, err := i.collectKafka(ctx)
		if err == nil {
			mu.Lock()
			report.Kafka = info
			mu.Unlock()
		}
		return err
	})

	run("addons", func() error {
		addons, err := i.collectAddons(ctx)
		if err == nil {
			mu.Lock()
			report.AddOns = addons
			mu.Unlock()
		}
		return err
	})

	run("activity", func() error {
		acts, err := i.collectActivity(ctx)
		if err == nil {
			mu.Lock()
			report.Activity = acts
			mu.Unlock()
		}
		return err
	})

	run("storage", func() error {
		stor := i.collectStorage(ctx)
		mu.Lock()
		report.Storage = stor
		mu.Unlock()
		return nil
	})

	wg.Wait()
	return report, nil
}

// kubectlJSON runs a kubectl command and parses JSON output into v.
func (i *Inspector) kubectlJSON(ctx context.Context, args []string, v interface{}) error {
	fullArgs := append([]string{"--kubeconfig", i.kubeconfigPath, "--context", i.contextName}, args...)
	out, err := exec.CommandContext(ctx, "kubectl", fullArgs...).Output()
	if err != nil {
		return fmt.Errorf("kubectl %v: %w", args, err)
	}
	return json.Unmarshal(out, v)
}

// collectNodes returns node readiness info.
func (i *Inspector) collectNodes(ctx context.Context) (NodesInfo, error) {
	var nodeList struct {
		Items []struct {
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
			Metadata struct {
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := i.kubectlJSON(ctx, []string{"get", "nodes", "-o", "json"}, &nodeList); err != nil {
		return NodesInfo{}, err
	}
	var info NodesInfo
	info.Total = len(nodeList.Items)
	for _, n := range nodeList.Items {
		for _, c := range n.Status.Conditions {
			if c.Type == "Ready" && c.Status == "True" {
				info.Ready++
			}
		}
		if t, ok := n.Metadata.Labels["node.kubernetes.io/instance-type"]; ok && info.Type == "" {
			info.Type = t
		}
	}
	return info, nil
}

// collectWorkloads returns deployment status for the was namespace.
func (i *Inspector) collectWorkloads(ctx context.Context) ([]WorkloadInfo, error) {
	var deployList struct {
		Items []struct {
			Metadata struct{ Name string `json:"name"` } `json:"metadata"`
			Status   struct {
				ReadyReplicas   int `json:"readyReplicas"`
				Replicas        int `json:"replicas"`
			} `json:"status"`
			Metadata2 struct {
				CreationTimestamp string `json:"creationTimestamp"`
			}
		} `json:"items"`
	}
	if err := i.kubectlJSON(ctx, []string{"get", "deploy", "-n", "was", "-o", "json"}, &deployList); err != nil {
		return nil, err
	}
	var wls []WorkloadInfo
	for _, d := range deployList.Items {
		wls = append(wls, WorkloadInfo{
			Name:    d.Metadata.Name,
			Ready:   d.Status.ReadyReplicas,
			Desired: d.Status.Replicas,
		})
	}
	return wls, nil
}

// collectKafka returns Strimzi Kafka CR status.
func (i *Inspector) collectKafka(ctx context.Context) (KafkaInfo, error) {
	var kafkaList struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := i.kubectlJSON(ctx, []string{"get", "kafka", "-A", "-o", "json"}, &kafkaList); err != nil {
		return KafkaInfo{}, err
	}
	info := KafkaInfo{}
	if len(kafkaList.Items) > 0 {
		item := kafkaList.Items[0]
		info.ClusterName = item.Metadata.Name

		// Query Kafka broker pods in the same namespace to get ready/total counts.
		var podList struct {
			Items []struct {
				Status struct {
					ContainerStatuses []struct {
						Ready bool `json:"ready"`
					} `json:"containerStatuses"`
				} `json:"status"`
			} `json:"items"`
		}
		args := []string{"get", "pods", "-n", item.Metadata.Namespace, "-l", "strimzi.io/kind=Kafka,strimzi.io/cluster=" + item.Metadata.Name, "-o", "json"}
		if err := i.kubectlJSON(ctx, args, &podList); err == nil {
			info.BrokersTotal = len(podList.Items)
			for _, p := range podList.Items {
				if len(p.Status.ContainerStatuses) > 0 {
					allReady := true
					for _, cs := range p.Status.ContainerStatuses {
						if !cs.Ready {
							allReady = false
							break
						}
					}
					if allReady {
						info.BrokersReady++
					}
				}
			}
		}
	}

	var topicList struct {
		Items []struct {
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := i.kubectlJSON(ctx, []string{"get", "kafkatopic", "-A", "-o", "json"}, &topicList); err == nil {
		info.TopicsTotal = len(topicList.Items)
		for _, t := range topicList.Items {
			isReady := false
			for _, cond := range t.Status.Conditions {
				if cond.Type == "Ready" && cond.Status == "True" {
					isReady = true
					break
				}
			}
			if isReady {
				info.TopicsReady++
			}
		}
	}
	return info, nil
}

// collectAddons returns helm release info for known add-ons.
func (i *Inspector) collectAddons(ctx context.Context) ([]AddOnInfo, error) {
	out, err := exec.CommandContext(ctx,
		"helm", "--kubeconfig", i.kubeconfigPath, "--kube-context", i.contextName,
		"list", "-A", "-o", "json",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("helm list: %w", err)
	}
	var releases []struct {
		Name       string `json:"name"`
		AppVersion string `json:"app_version"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(out, &releases); err != nil {
		return nil, err
	}

	known := map[string]bool{
		"ingress-nginx": true, "strimzi-kafka-operator": true,
		"metrics-server": true, "kube-prometheus-stack": true,
		"prometheus-adapter": true, "aws-efs-csi-driver": true,
		"cert-manager": true,
	}
	var addons []AddOnInfo
	for _, r := range releases {
		if known[r.Name] {
			addons = append(addons, AddOnInfo{
				Name:    r.Name,
				Version: r.AppVersion,
				Status:  r.Status,
			})
		}
	}
	return addons, nil
}

// collectActivity returns recent HPA scaling and helm upgrade events.
func (i *Inspector) collectActivity(ctx context.Context) ([]ActivityEntry, error) {
	// Get recent events filtered to HPA scaling.
	out, err := exec.CommandContext(ctx,
		"kubectl", "--kubeconfig", i.kubeconfigPath, "--context", i.contextName,
		"get", "events", "-A", "--sort-by=.lastTimestamp",
		"-o", "json",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("kubectl get events: %w", err)
	}
	var eventList struct {
		Items []struct {
			Reason  string    `json:"reason"`
			Message string    `json:"message"`
			LastTimestamp time.Time `json:"lastTimestamp"`
		} `json:"items"`
	}
	_ = json.Unmarshal(out, &eventList)

	var acts []ActivityEntry
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, e := range eventList.Items {
		if e.LastTimestamp.Before(cutoff) {
			continue
		}
		if e.Reason == "SuccessfulRescale" || strings.Contains(e.Message, "scaled") {
			acts = append(acts, ActivityEntry{
				Age:     humanAge(e.LastTimestamp),
				Summary: e.Message,
			})
		}
	}
	return acts, nil
}

func humanAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func clusterRegion(meta *workspace.Workspace) string {
	if meta == nil || meta.Meta == nil {
		return ""
	}
	m := meta.Meta
	if m.Cloud == "azure" || (m.Cloud == "" && m.AzureLocation != "") {
		return m.AzureLocation
	}
	return m.AWSRegion
}

// collectStorage returns PVC counts, filesystem StorageClass status, and
// object-storage (S3 / Azure blob) stats. All sub-calls are best-effort.
func (i *Inspector) collectStorage(ctx context.Context) StorageInfo {
	var info StorageInfo
	cloud := "aws"
	if i.ws.Meta != nil && (i.ws.Meta.Cloud == "azure" || (i.ws.Meta.Cloud == "" && i.ws.Meta.AzureLocation != "")) {
		cloud = "azure"
	}
	info.Cloud = cloud

	scName := "was-efs"
	if cloud == "azure" {
		scName = "was-azurefile"
	}
	if err := i.kubectlJSON(ctx, []string{"get", "storageclass", scName, "-o", "json"}, &struct{}{}); err == nil {
		info.FilesStatus = "Available"
	} else {
		info.FilesStatus = "Not found"
	}

	var pvcList struct {
		Items []struct {
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := i.kubectlJSON(ctx, []string{"get", "pvc", "-n", "was", "-o", "json"}, &pvcList); err == nil {
		info.PVCTotal = len(pvcList.Items)
		for _, p := range pvcList.Items {
			if strings.EqualFold(p.Status.Phase, "bound") {
				info.PVCBound++
			}
		}
	}

	if cloud == "azure" {
		info.ResourceBucket, info.NodefileBucket = i.collectAzureContainers(ctx)
	} else {
		info.ResourceBucket, info.NodefileBucket = i.collectBuckets(ctx)
	}

	return info
}

// collectAzureContainers retrieves blob container names from helm release values.
// Object counts/sizes are omitted here — az storage blob list is multi-second
// and dominated Storage tab latency; names alone are enough for the UI.
func (i *Inspector) collectAzureContainers(ctx context.Context) (BucketInfo, BucketInfo) {
	out, err := exec.CommandContext(ctx,
		"helm", "--kubeconfig", i.kubeconfigPath, "--kube-context", i.contextName,
		"get", "values", "was", "-n", "was", "-o", "json",
	).Output()
	if err != nil {
		return BucketInfo{}, BucketInfo{}
	}
	var vals struct {
		ObjectStorage struct {
			ResourceBucket string `json:"resourceBucket"`
			NodefileBucket string `json:"nodefileBucket"`
		} `json:"objectStorage"`
	}
	if err := json.Unmarshal(out, &vals); err != nil {
		return BucketInfo{}, BucketInfo{}
	}
	return BucketInfo{Name: vals.ObjectStorage.ResourceBucket},
		BucketInfo{Name: vals.ObjectStorage.NodefileBucket}
}

// collectBuckets retrieves bucket names from the deployed helm release values
// and fetches object counts and sizes from S3.
func (i *Inspector) collectBuckets(ctx context.Context) (BucketInfo, BucketInfo) {
	out, err := exec.CommandContext(ctx,
		"helm", "--kubeconfig", i.kubeconfigPath, "--kube-context", i.contextName,
		"get", "values", "was", "-n", "was", "-o", "json",
	).Output()
	if err != nil {
		return BucketInfo{}, BucketInfo{}
	}
	var vals struct {
		ObjectStorage struct {
			ResourceBucket string `json:"resourceBucket"`
			NodefileBucket string `json:"nodefileBucket"`
		} `json:"objectStorage"`
	}
	if err := json.Unmarshal(out, &vals); err != nil {
		return BucketInfo{}, BucketInfo{}
	}
	region := i.ws.Meta.AWSRegion
	return i.fetchBucketStats(ctx, vals.ObjectStorage.ResourceBucket, region),
		i.fetchBucketStats(ctx, vals.ObjectStorage.NodefileBucket, region)
}

// fetchBucketStats queries S3 for object count and total bytes (first page only,
// up to 1000 objects). Returns BucketInfo with only Name set if the query fails.
func (i *Inspector) fetchBucketStats(ctx context.Context, name, region string) BucketInfo {
	if name == "" {
		return BucketInfo{}
	}
	info := BucketInfo{Name: name}
	out, err := exec.CommandContext(ctx,
		"aws", "s3api", "list-objects-v2",
		"--bucket", name, "--region", region,
		"--output", "json",
	).Output()
	if err != nil {
		return info
	}
	var result struct {
		Contents []struct {
			Size int64 `json:"Size"`
		} `json:"Contents"`
		KeyCount int `json:"KeyCount"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return info
	}
	info.Objects = result.KeyCount
	for _, obj := range result.Contents {
		info.Bytes += obj.Size
	}
	return info
}

// RenderText writes a human-readable report to w.
func RenderText(r *Report, w io.Writer) error {
	p := func(format string, args ...interface{}) {
		fmt.Fprintf(w, format+"\n", args...)
	}

	p("Cluster:       %s", r.Cluster.Name)
	p("Region:        %s", r.Cluster.Region)
	if r.Cluster.ARN != "" {
		p("ARN:           %s", r.Cluster.ARN)
	}
	p("")

	p("Nodes:         %d / %d ready", r.Nodes.Ready, r.Nodes.Total)
	if r.Nodes.Type != "" {
		p("               %s", r.Nodes.Type)
	}
	p("")

	if len(r.Workloads) > 0 {
		p("Workloads (namespace: was):")
		for _, wl := range r.Workloads {
			p("  %-28s %d / %d ready", wl.Name, wl.Ready, wl.Desired)
		}
		p("")
	}

	if r.Kafka.ClusterName != "" {
		p("Kafka (Strimzi):")
		p("  Cluster (%s)   %d / %d brokers ready",
			r.Kafka.ClusterName, r.Kafka.BrokersReady, r.Kafka.BrokersTotal)
		p("  Topics                    %d / %d created",
			r.Kafka.TopicsReady, r.Kafka.TopicsTotal)
		p("")
	}

	if len(r.AddOns) > 0 {
		p("Add-ons:")
		for _, a := range r.AddOns {
			p("  %-30s %-10s %s", a.Name, a.Version, a.Status)
		}
		p("")
	}

	if len(r.Activity) > 0 {
		p("Recent activity:")
		for _, a := range r.Activity {
			p("  %-10s %s", a.Age, a.Summary)
		}
		p("")
	}

	if len(r.Errors) > 0 {
		p("Section errors:")
		for _, e := range r.Errors {
			p("  %-12s (unavailable: %s)", e.Section, e.Error)
		}
		p("")
	}

	p("Displayed at %s. Some values are point-in-time.", r.Generated.Format("2006-01-02 15:04:05 UTC"))
	return nil
}
