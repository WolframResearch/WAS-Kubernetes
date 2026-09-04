package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

// azureVMUsageFn shells out to az vm list-usage.
// Overridable for tests.
var azureVMUsageFn = func(ctx context.Context, location string) ([]byte, error) {
	return runOutput(ctx, "az", "vm", "list-usage",
		"--location", location,
		"--output", "json")
}

// azureNetworkUsageFn shells out to az network list-usages.
// Overridable for tests.
var azureNetworkUsageFn = func(ctx context.Context, location string) ([]byte, error) {
	return runOutput(ctx, "az", "network", "list-usages",
		"--location", location,
		"--output", "json")
}

// azureAKSListFn shells out to az aks list.
// Overridable for tests.
var azureAKSListFn = func(ctx context.Context, subscriptionID string) ([]byte, error) {
	return runOutput(ctx, "az", "aks", "list",
		"--subscription", subscriptionID,
		"--output", "json")
}

type azureUsageItem struct {
	CurrentValue int64 `json:"currentValue"`
	Limit        int64 `json:"limit"`
	Name         struct {
		Value          string `json:"value"`
		LocalizedValue string `json:"localizedValue"`
	} `json:"name"`
}

func fetchAzureQuota(ctx context.Context, rc *doctor.RunContext, cacheKey string, fetchFn func() ([]byte, error), matchName string) (current, limit int64, ok bool, err error) {
	if v, hit := rc.QuotaCached(cacheKey); hit {
		parts := strings.SplitN(v, "/", 2)
		if len(parts) == 2 {
			c, _ := strconv.ParseInt(parts[0], 10, 64)
			l, _ := strconv.ParseInt(parts[1], 10, 64)
			return c, l, true, nil
		}
	}
	raw, err := fetchFn()
	if err != nil {
		if strings.Contains(err.Error(), "AuthorizationFailed") || strings.Contains(err.Error(), "403") {
			return 0, 0, false, nil
		}
		return 0, 0, false, err
	}
	var items []azureUsageItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return 0, 0, false, fmt.Errorf("parse usages: %w", err)
	}
	for _, item := range items {
		if item.Name.Value == matchName || item.Name.LocalizedValue == matchName {
			rc.QuotaStore(cacheKey, fmt.Sprintf("%d/%d", item.CurrentValue, item.Limit))
			return item.CurrentValue, item.Limit, true, nil
		}
	}
	return 0, 0, false, nil
}

// azureQuotaPrecondition skips when not Azure or no subscription.
func azureQuotaPrecondition(rc *doctor.RunContext) (bool, string) {
	if skip, reason := azureCredentialsPrecondition(rc); skip {
		return skip, reason
	}
	if rc.AzureSubscriptionID == "" {
		return true, "Azure credentials unavailable; check azure.credentials first"
	}
	return false, ""
}

// AzureQuotaCores checks the available vCPU quota in the configured location.
type AzureQuotaCores struct{ BaseCheck }

func (AzureQuotaCores) ID() string                { return "azure.quota.cores" }
func (AzureQuotaCores) Name() string              { return "Azure vCPU quota available" }
func (AzureQuotaCores) Category() doctor.Category { return doctor.CategoryEnvironment }
func (AzureQuotaCores) Description() string {
	return "Checks that the subscription has sufficient vCPU quota in the configured Azure location for a WAS deployment."
}
func (c AzureQuotaCores) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	return azureQuotaPrecondition(rc)
}
func (c AzureQuotaCores) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	location := rc.Cfg.AzureLocation.Value
	cacheKey := "az/vm/cores/" + location
	current, limit, ok, err := fetchAzureQuota(ctx, rc, cacheKey,
		func() ([]byte, error) { return azureVMUsageFn(ctx, location) },
		"Total Regional vCPUs",
	)
	if err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("az vm list-usage: %v", err))
	}
	if !ok {
		return skip(c.ID(), c.Name(), "IAM identity lacks quota read permission; cannot verify vCPU quota")
	}
	available := limit - current
	const minCores int64 = 16
	if available < minCores {
		return fail(c.ID(), c.Name(), doctor.SeverityProblem,
			fmt.Sprintf("only %d vCPUs available in %s (used %d of %d; need ≥%d)", available, location, current, limit, minCores),
			fmt.Sprintf("Request a quota increase in Azure Portal for location %s.\n"+
				"See: README.md (Prerequisites) and docs/Troubleshooting.md", location))
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("%d vCPUs available in %s (%d used of %d limit)", available, location, current, limit))
}

// AzureQuotaPublicIPs checks the public IP quota.
type AzureQuotaPublicIPs struct{ BaseCheck }

func (AzureQuotaPublicIPs) ID() string                { return "azure.quota.public_ips" }
func (AzureQuotaPublicIPs) Name() string              { return "Azure public IP quota available" }
func (AzureQuotaPublicIPs) Category() doctor.Category { return doctor.CategoryEnvironment }
func (AzureQuotaPublicIPs) Description() string {
	return "Checks that the subscription has at least 3 public IPs available (needed for LoadBalancer)."
}
func (c AzureQuotaPublicIPs) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	return azureQuotaPrecondition(rc)
}
func (c AzureQuotaPublicIPs) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	location := rc.Cfg.AzureLocation.Value
	cacheKey := "az/network/public_ips/" + location
	current, limit, ok, err := fetchAzureQuota(ctx, rc, cacheKey,
		func() ([]byte, error) { return azureNetworkUsageFn(ctx, location) },
		"PublicIPAddresses",
	)
	if err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("az network list-usages: %v", err))
	}
	if !ok {
		return skip(c.ID(), c.Name(), "cannot read public IP quota; check network permissions")
	}
	available := limit - current
	const minIPs int64 = 3
	if available < minIPs {
		return fail(c.ID(), c.Name(), doctor.SeverityWarning,
			fmt.Sprintf("only %d public IPs available in %s (need ≥%d)", available, location, minIPs),
			"Request a quota increase in Azure Portal for Public IP Addresses.")
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("%d public IPs available in %s", available, location))
}

// AzureQuotaAKSClusters checks AKS cluster count vs a safe threshold.
type AzureQuotaAKSClusters struct{ BaseCheck }

func (AzureQuotaAKSClusters) ID() string                { return "azure.quota.aks_clusters" }
func (AzureQuotaAKSClusters) Name() string              { return "AKS cluster quota available" }
func (AzureQuotaAKSClusters) Category() doctor.Category { return doctor.CategoryEnvironment }
func (AzureQuotaAKSClusters) Description() string {
	return "Counts existing AKS clusters vs the default 50-cluster limit."
}
func (c AzureQuotaAKSClusters) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	return azureQuotaPrecondition(rc)
}
func (c AzureQuotaAKSClusters) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	cacheKey := "az/aks/cluster_count"
	var count int
	if v, ok := rc.QuotaCached(cacheKey); ok {
		_, _ = fmt.Sscanf(v, "%d", &count)
	} else {
		out, err := azureAKSListFn(ctx, rc.AzureSubscriptionID)
		if err != nil {
			if strings.Contains(err.Error(), "AuthorizationFailed") || strings.Contains(err.Error(), "403") {
				return skip(c.ID(), c.Name(), "insufficient permissions to list AKS clusters; cannot verify quota")
			}
			return checkError(c.ID(), c.Name(), fmt.Sprintf("az aks list: %v", err))
		}
		var clusters []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(out, &clusters); err != nil {
			return checkError(c.ID(), c.Name(), fmt.Sprintf("parse aks list: %v", err))
		}
		count = len(clusters)
		rc.QuotaStore(cacheKey, strconv.Itoa(count))
	}
	const limit = 50
	if count >= limit-2 {
		return fail(c.ID(), c.Name(), doctor.SeverityProblem,
			fmt.Sprintf("%d of %d AKS clusters in use — near limit", count, limit),
			"Request a quota increase or delete unused clusters.")
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("%d of %d AKS clusters in use", count, limit))
}
