package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/metacontainer"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

type opsOverviewRow struct {
	Name        string
	StatusClass string // "status-healthy", "status-problem", "status-critical", "status-unknown"
	StatusLabel string // "Healthy", "1 problem", "N problems", "Unknown"
	Issues      int
	Total       int
	LastChecked string // "3 hours ago", "Never"
}

// opsCloudTabData holds the display data for one cloud's operations tab panel.
type opsCloudTabData struct {
	Cloud     string            // "aws" or "azure"
	State     tabState          // same tabState constants as home.go
	Avail     CloudAvailability // CLI and credential state
	Clusters  []opsOverviewRow
	ConfigCmd string
	Error     string
	Healthy   int
	Degraded  int
	Total     int
}

type opsOverviewData struct {
	Version     string
	ActiveCloud string // "aws" or "azure"
	AWSCount    int    // -1 = auth missing
	AzureCount  int
	Tab         opsCloudTabData
	OOB         bool // true when rendering _ops_cloud_tabs as an HTMX OOB fragment
	AWSAvail    CloudAvailability
	AzureAvail  CloudAvailability
}

// Operations handles GET /operations — top-level operations overview.
func Operations(t *Templates, metaRegion, region string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		activeCloud := preferredCloud(r)
		subscriptionID := activeAzureSubscription(r)

		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()

		tab := fetchOpsCloudTab(ctx, activeCloud, metaRegion, region, subscriptionID)
		awsAvail, azureAvail := buildTabBarAvail(ctx, activeCloud, tab.Avail)

		var awsCount, azureCount int
		if activeCloud == "azure" {
			awsCount = fetchCloudCount(ctx, "aws", metaRegion, region)
			azureCount = countFromOpsTab(tab)
		} else {
			awsCount = countFromOpsTab(tab)
			azureCount = fetchCloudCount(ctx, "azure", metaRegion, region)
		}

		data := opsOverviewData{
			Version:     version.Version,
			ActiveCloud: activeCloud,
			AWSCount:    awsCount,
			AzureCount:  azureCount,
			Tab:         tab,
			AWSAvail:    awsAvail,
			AzureAvail:  azureAvail,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.OpsOverview.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

// OperationsByCloud handles GET /operations/clusters?cloud=aws|azure[&subscription=<id>].
// Returns TWO fragments: an OOB _ops_cloud_tabs update followed by
// the primary _ops_content_wrapper (into #ops-tab-body).
// Sets the wasctl_last_cloud cookie; subscription= also sets wasctl_azure_subscription.
func OperationsByCloud(t *Templates, metaRegion, region string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cloud := r.URL.Query().Get("cloud")
		if cloud != "azure" {
			cloud = "aws"
		}

		subscriptionID := r.URL.Query().Get("subscription")
		if subscriptionID != "" {
			http.SetCookie(w, &http.Cookie{
				Name:     azureSubCookie,
				Value:    subscriptionID,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
		} else {
			subscriptionID = activeAzureSubscription(r)
		}

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		http.SetCookie(w, &http.Cookie{
			Name:     cloudCookie,
			Value:    cloud,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})

		tab := fetchOpsCloudTab(ctx, cloud, metaRegion, region, subscriptionID)
		awsAvail, azureAvail := buildTabBarAvail(ctx, cloud, tab.Avail)

		var awsCount, azureCount int
		if cloud == "azure" {
			awsCount = fetchCloudCount(ctx, "aws", metaRegion, region)
			azureCount = countFromOpsTab(tab)
		} else {
			awsCount = countFromOpsTab(tab)
			azureCount = fetchCloudCount(ctx, "azure", metaRegion, region)
		}

		tabBarData := opsOverviewData{
			ActiveCloud: cloud,
			AWSCount:    awsCount,
			AzureCount:  azureCount,
			OOB:         true,
			AWSAvail:    awsAvail,
			AzureAvail:  azureAvail,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.OpsOverview.ExecuteTemplate(w, "_ops_cloud_tabs", tabBarData); err != nil {
			renderErr(w, err)
			return
		}
		// Primary content: _ops_content_wrapper carries the 10s refresh trigger.
		if err := t.OpsOverview.ExecuteTemplate(w, "_ops_content_wrapper", tab); err != nil {
			renderErr(w, err)
		}
	}
}

// OpsDataRefresh handles GET /operations/data?cloud=aws|azure.
// Background-refresh counterpart of OperationsByCloud: returns only _ops_tab_body,
// no OOB tab bar, no cookie update.
func OpsDataRefresh(t *Templates, metaRegion, region string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cloud := r.URL.Query().Get("cloud")
		if cloud != "azure" {
			cloud = "aws"
		}
		subscriptionID := activeAzureSubscription(r)

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		tab := fetchOpsCloudTab(ctx, cloud, metaRegion, region, subscriptionID)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.OpsOverview.ExecuteTemplate(w, "_ops_tab_body", tab); err != nil {
			renderErr(w, err)
		}
	}
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func fetchOpsCloudTab(ctx context.Context, cloud, metaRegion, region, subscriptionID string) opsCloudTabData {
	if cloud == "azure" {
		return fetchAzureOpsTab(ctx, subscriptionID)
	}
	return fetchAWSOpsTab(ctx, metaRegion, region)
}

func fetchAWSOpsTab(ctx context.Context, metaRegion, region string) opsCloudTabData {
	tab := opsCloudTabData{Cloud: "aws"}

	avail := DetectAWSFnForTest(ctx, region)
	tab.Avail = avail

	if !avail.CLIInstalled {
		tab.State = tabStateNotConfigured
		tab.ConfigCmd = "install aws-cli  # https://aws.amazon.com/cli/"
		return tab
	}
	if !avail.Authenticated {
		tab.State = tabStateNotConfigured
		tab.ConfigCmd = "aws configure  # or: aws sso login"
		return tab
	}

	b, err := OpenMetaBucketFnForTest(ctx, metaRegion, avail.ActiveAccount)
	if err != nil {
		tab.State = tabStateNoClusters
		return tab
	}

	names, err := b.ListClusters(ctx)
	if err != nil || len(names) == 0 {
		tab.State = tabStateNoClusters
		return tab
	}

	rows := make([]opsOverviewRow, 0, len(names))
	for _, name := range names {
		row := buildOpsRow(ctx, b, name)
		rows = append(rows, row)
	}
	tab.State = tabStateOK
	tab.Clusters = rows
	tab.Total = len(rows)
	for _, row := range rows {
		if row.StatusClass == "status-healthy" {
			tab.Healthy++
		} else if row.StatusClass != "status-unknown" {
			tab.Degraded++
		}
	}
	return tab
}

func fetchAzureOpsTab(ctx context.Context, subscriptionID string) opsCloudTabData {
	tab := opsCloudTabData{Cloud: "azure"}

	avail, _ := DetectAzureFnForTest(ctx)
	tab.Avail = avail

	if !avail.CLIInstalled {
		tab.State = tabStateNotConfigured
		tab.ConfigCmd = "install azure-cli  # https://aka.ms/installazurecli"
		return tab
	}
	if !avail.Authenticated {
		tab.State = tabStateNotConfigured
		tab.ConfigCmd = "az login  # or: az account set --subscription <id>"
		return tab
	}

	activeSub := avail.ActiveAccount
	if subscriptionID != "" {
		activeSub = subscriptionID
	}

	names, err := ListAzureClustersFnForTest(ctx, activeSub)
	if err != nil || len(names) == 0 {
		tab.State = tabStateNoClusters
		return tab
	}

	rows := make([]opsOverviewRow, 0, len(names))
	for _, name := range names {
		c, err := OpenMetaContainerFnForTest(ctx, activeSub, name)
		if err != nil {
			continue
		}
		row := buildOpsRowAzure(ctx, c, name)
		rows = append(rows, row)
	}
	tab.State = tabStateOK
	tab.Clusters = rows
	tab.Total = len(rows)
	for _, row := range rows {
		if row.StatusClass == "status-healthy" {
			tab.Healthy++
		} else if row.StatusClass != "status-unknown" {
			tab.Degraded++
		}
	}
	return tab
}

// buildOpsRow loads the last doctor report entry for an AWS cluster.
func buildOpsRow(ctx context.Context, b *metabucket.Bucket, clusterName string) opsOverviewRow {
	row := opsOverviewRow{Name: clusterName, StatusClass: "status-unknown", StatusLabel: "Unknown", LastChecked: "Never"}

	entries, err := workspace.ListReportEntries(ctx, b, clusterName)
	if err != nil || len(entries) == 0 {
		return row
	}

	last := entries[0]
	row.LastChecked = formatAge(last.GeneratedAt)
	row.Total = last.Summary.Pass + last.Summary.Fail + last.Summary.Skip + last.Summary.Error + last.Summary.Info
	row.Issues = last.Summary.Fail
	applyOpsStatus(&row, last.Summary.Fail)
	return row
}

// buildOpsRowAzure loads the last doctor report entry for an Azure cluster.
func buildOpsRowAzure(ctx context.Context, c *metacontainer.Container, clusterName string) opsOverviewRow {
	row := opsOverviewRow{Name: clusterName, StatusClass: "status-unknown", StatusLabel: "Unknown", LastChecked: "Never"}

	entries, err := workspace.ListReportEntries(ctx, c, clusterName)
	if err != nil || len(entries) == 0 {
		return row
	}

	last := entries[0]
	row.LastChecked = formatAge(last.GeneratedAt)
	row.Total = last.Summary.Pass + last.Summary.Fail + last.Summary.Skip + last.Summary.Error + last.Summary.Info
	row.Issues = last.Summary.Fail
	applyOpsStatus(&row, last.Summary.Fail)
	return row
}

func applyOpsStatus(row *opsOverviewRow, failCount int) {
	row.StatusClass, row.StatusLabel = opsStatusClass(failCount)
}

// opsStatusClass returns the canonical badge--status-* CSS suffix and human label
// for a doctor report fail count. Call this from every place cluster health is rendered
// so the mapping is defined once.
func opsStatusClass(failCount int) (cssClass, label string) {
	if failCount == 0 {
		return "status-healthy", "Healthy"
	}
	if failCount == 1 {
		return "status-problem", "1 problem"
	}
	return "status-problem", formatIssueCount(failCount)
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func formatIssueCount(n int) string {
	return fmt.Sprintf("%d problems", n)
}

// countFromOpsTab derives the cluster badge count from an already-fetched
// opsCloudTabData, avoiding a second API round-trip into the same request context.
func countFromOpsTab(tab opsCloudTabData) int {
	switch tab.State {
	case tabStateOK:
		return len(tab.Clusters)
	case tabStateNoClusters:
		return 0
	default:
		return -1
	}
}

// ── AWS-only helpers preserved for per-cluster operations pages ───────────────

// buildOpsOverviewRows is kept for callers that only need the AWS list.
// Internal callers should use fetchAWSOpsTab instead.
func buildOpsOverviewRows(ctx context.Context, metaRegion, region string) ([]opsOverviewRow, error) {
	accountID, err := getAWSAccountID(ctx, region)
	if err != nil {
		return nil, err
	}
	b, err := metabucket.Open(ctx, metaRegion, accountID, "")
	if err != nil {
		return nil, err
	}
	names, err := b.ListClusters(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]opsOverviewRow, 0, len(names))
	for _, name := range names {
		rows = append(rows, buildOpsRow(ctx, b, name))
	}
	return rows, nil
}
