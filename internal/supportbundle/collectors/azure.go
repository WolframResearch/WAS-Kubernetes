package collectors

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/metacontainer"
)

// AzureCollector collects Azure account and AKS cluster metadata.
// Only active when cfg.Cloud == "azure".
type AzureCollector struct{}

func (AzureCollector) Name() string { return "azure" }

func (AzureCollector) Collect(ctx context.Context, cc *CollectContext) ([]File, error) {
	if cc.Cfg == nil || cc.Cfg.Cloud != "azure" {
		return nil, fmt.Errorf("azure collector skipped: cloud is not azure")
	}

	var files []File

	// az account show
	account, err := runOutput(ctx, "az", "account", "show", "--output", "json")
	if err != nil {
		account = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "azure/account.json", Content: []byte(redactText(string(account)))})

	// az aks show
	clusterName := cc.Cfg.ClusterName.Value
	resourceGroup := cc.Cfg.AzureLocation.Value // fallback; overridden by workspace below
	subscriptionID := ""

	if cc.Workspace != nil && cc.Workspace.Meta != nil {
		if cc.Workspace.Meta.AzureResourceGroup != "" {
			resourceGroup = cc.Workspace.Meta.AzureResourceGroup
		}
		subscriptionID = cc.Workspace.Meta.AzureSubscriptionID
	}

	// Derive subscription ID if not in workspace
	if subscriptionID == "" {
		raw, _ := runOutput(ctx, "az", "account", "show", "--query", "id", "--output", "tsv")
		subscriptionID = strings.TrimSpace(string(raw))
	}

	aks, err := runOutput(ctx, "az", "aks", "show",
		"--name", clusterName,
		"--resource-group", resourceGroup,
		"--output", "json")
	if err != nil {
		aks = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "azure/aks_cluster.json", Content: []byte(redactText(string(aks)))})

	// AKS node pools
	if clusterName != "" && resourceGroup != "" {
		nodePools, err := runOutput(ctx, "az", "aks", "nodepool", "list",
			"--resource-group", resourceGroup,
			"--cluster-name", clusterName,
			"--output", "json")
		if err != nil {
			nodePools = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
		}
		files = append(files, File{
			Path:    "azure/aks_nodepools.json",
			Content: []byte(redactText(string(nodePools))),
		})
	}

	// Meta storage account
	if subscriptionID != "" {
		metaAccountName := metacontainer.StorageAccountName(subscriptionID, clusterName)
		storage, err := runOutput(ctx, "az", "storage", "account", "show",
			"--name", metaAccountName, "--output", "json")
		if err != nil {
			storage = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
		}
		files = append(files, File{
			Path:    "azure/meta_storage_status.json",
			Content: []byte(redactText(string(storage))),
		})
	}

	// Activity log — errors in the resource group, last 4 hours
	if resourceGroup != "" {
		since := time.Now().Add(-4 * time.Hour).UTC().Format(time.RFC3339)
		activityLog, err := runOutput(ctx, "az", "monitor", "activity-log", "list",
			"--resource-group", resourceGroup,
			"--start-time", since,
			"--status", "Failed",
			"--output", "json")
		if err != nil {
			activityLog = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
		}
		files = append(files, File{
			Path:    "azure/activity_log_errors.json",
			Content: []byte(redactText(string(activityLog))),
		})
	}

	return files, nil
}
