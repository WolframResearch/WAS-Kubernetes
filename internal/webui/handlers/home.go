package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/metacontainer"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
)

// tabState describes the rendering state of a cloud tab panel.
type tabState = string

const (
	tabStateOK            tabState = "ok"
	tabStateNotConfigured tabState = "not_configured"
	tabStateNoClusters    tabState = "no_clusters"
	tabStateError         tabState = "error"
)

// CloudTabData holds the display data for one cloud's cluster tab panel.
// It is exported so templates and tests can reference it directly.
type CloudTabData struct {
	Cloud     string           // "aws" or "azure"
	State     tabState         // tabStateOK | tabStateNotConfigured | tabStateNoClusters | tabStateError
	Avail     CloudAvailability // CLI and credential state, used by tab body for contextual messaging
	Clusters  []ClusterRow
	ConfigCmd string // credentials command to show in not_configured state
	Error     string // non-empty only in tabStateError

	// Azure-specific: populated when Authenticated and az account list returns entries.
	Subscriptions      []AzureSubscription
	ActiveSubscription string // subscription ID currently selected (from query param or cookie)
}

// ClusterRow holds display data for one cluster in a tab.
type ClusterRow struct {
	Name         string
	Region       string
	Status       string
	StatusClass  string // badge modifier: active, deployed, installing, destroyed, error
	LastModified string
}

type homeData struct {
	Version     string
	ActiveCloud string // "aws" or "azure"
	AWSCount    int    // -1 = auth missing / not queried
	AzureCount  int    // -1 = auth missing / not queried
	Tab         CloudTabData
	OOB         bool // true when rendering _cloud_tabs as an HTMX OOB fragment

	// AWSAvail / AzureAvail carry CLIInstalled for the tab bar so it can render
	// disabled buttons when the CLI is absent.  For the active cloud, Avail is
	// fully populated (from detectAWS/detectAzure).  For the inactive cloud,
	// only CLIInstalled is set (from exec.LookPath — no API call).
	AWSAvail   CloudAvailability
	AzureAvail CloudAvailability
}

const (
	cloudCookie    = "wasctl_last_cloud"
	azureSubCookie = "wasctl_azure_subscription"
)

// ── Overridable functions (replaced in tests) ─────────────────────────────────
// Exported so tests in the handlers_test package can inject fakes without
// a real AWS account or Azure subscription.

var (
	GetCallerIdentityFnForTest   = tools.GetCallerIdentity
	GetAzureAccountInfoFnForTest = tools.GetAccountInfo
	OpenMetaBucketFnForTest      = func(ctx context.Context, region, accountID string) (*metabucket.Bucket, error) {
		return metabucket.Open(ctx, region, accountID, "")
	}
	OpenMetaContainerFnForTest   = metacontainer.Open

	// ListAWSClustersFnForTest / ListAzureClustersFnForTest wrap the ListClusters
	// call so tests can return empty lists without a real S3 bucket.
	ListAWSClustersFnForTest = func(ctx context.Context, b *metabucket.Bucket) ([]string, error) {
		return b.ListClusters(ctx)
	}
	ListAzureClustersFnForTest = func(ctx context.Context, subscriptionID string) ([]string, error) {
		return metacontainer.ListClustersInSubscription(ctx, subscriptionID)
	}
)

// ── Handlers ──────────────────────────────────────────────────────────────────

// Home renders the full home page (GET /).
// Reads the wasctl_last_cloud cookie for the initial active tab.
// Fetches counts for both clouds (names only), then loads full cluster
// metadata for the active tab only.
func Home(t *Templates, metaRegion, region string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		activeCloud := preferredCloud(r)
		subscriptionID := activeAzureSubscription(r)

		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()

		tab := fetchCloudTab(ctx, activeCloud, metaRegion, region, subscriptionID)
		awsAvail, azureAvail := buildTabBarAvail(ctx, activeCloud, tab.Avail)

		var awsCount, azureCount int
		if activeCloud == "azure" {
			awsCount = fetchCloudCount(ctx, "aws", metaRegion, region)
			azureCount = countFromTab(tab)
		} else {
			awsCount = countFromTab(tab)
			azureCount = fetchCloudCount(ctx, "azure", metaRegion, region)
		}

		data := homeData{
			Version:     version.Version,
			ActiveCloud: activeCloud,
			AWSCount:    awsCount,
			AzureCount:  azureCount,
			Tab:         tab,
			AWSAvail:    awsAvail,
			AzureAvail:  azureAvail,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.Home.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

// ClustersByCloud handles GET /clusters?cloud=aws|azure[&subscription=<id>].
// Returns TWO fragments:
//   - an OOB _cloud_tabs update (fixes active-tab indicator, updates counts)
//   - the primary _clusters_content_wrapper (goes into #cluster-tab-body)
//
// Sets the wasctl_last_cloud cookie. When a subscription= param is present for
// Azure, sets wasctl_azure_subscription as well.
func ClustersByCloud(t *Templates, metaRegion, region string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cloud := r.URL.Query().Get("cloud")
		if cloud != "azure" {
			cloud = "aws"
		}

		// Resolve Azure subscription: URL param takes priority over cookie.
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

		tab := fetchCloudTab(ctx, cloud, metaRegion, region, subscriptionID)
		awsAvail, azureAvail := buildTabBarAvail(ctx, cloud, tab.Avail)

		// Active cloud count comes from the already-fetched tab to avoid a second
		// API round-trip within the same context. Inactive cloud still needs its own call.
		var awsCount, azureCount int
		if cloud == "azure" {
			awsCount = fetchCloudCount(ctx, "aws", metaRegion, region)
			azureCount = countFromTab(tab)
		} else {
			awsCount = countFromTab(tab)
			azureCount = fetchCloudCount(ctx, "azure", metaRegion, region)
		}

		tabBarData := homeData{
			ActiveCloud: cloud,
			AWSCount:    awsCount,
			AzureCount:  azureCount,
			OOB:         true,
			AWSAvail:    awsAvail,
			AzureAvail:  azureAvail,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// OOB fragment: HTMX replaces #cloud-tabs in place (active indicator fix).
		if err := t.Home.ExecuteTemplate(w, "_cloud_tabs", tabBarData); err != nil {
			renderErr(w, err)
			return
		}
		// Primary content: #clusters-content wrapper with 10s polling trigger.
		// Goes into #cluster-tab-body via the caller's hx-target.
		if err := t.Home.ExecuteTemplate(w, "_clusters_content_wrapper", tab); err != nil {
			renderErr(w, err)
		}
	}
}

// ClusterDataRefresh handles GET /clusters/data?cloud=aws|azure.
// Called by the hx-trigger="every 10s" on #clusters-content for background polling.
// Returns ONLY the _cluster_tab_body fragment — no OOB tab bar, no cookie update —
// so the tab bar does not flicker on background refreshes.
func ClusterDataRefresh(t *Templates, metaRegion, region string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cloud := r.URL.Query().Get("cloud")
		if cloud != "azure" {
			cloud = "aws"
		}
		subscriptionID := activeAzureSubscription(r)

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		tab := fetchCloudTab(ctx, cloud, metaRegion, region, subscriptionID)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.Home.ExecuteTemplate(w, "_cluster_tab_body", tab); err != nil {
			renderErr(w, err)
		}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func preferredCloud(r *http.Request) string {
	c, err := r.Cookie(cloudCookie)
	if err == nil && c.Value == "azure" {
		return "azure"
	}
	return "aws"
}

// activeAzureSubscription returns the Azure subscription ID persisted in the
// wasctl_azure_subscription cookie, or "" if none is set.
func activeAzureSubscription(r *http.Request) string {
	if c, err := r.Cookie(azureSubCookie); err == nil {
		return c.Value
	}
	return ""
}

// fetchCloudCount returns the cluster count for cloud, or -1 when auth fails.
// It fetches names only — no metadata — to minimise latency.
func fetchCloudCount(ctx context.Context, cloud, metaRegion, region string) int {
	if cloud == "azure" {
		info, err := GetAzureAccountInfoFnForTest(ctx)
		if err != nil {
			return -1
		}
		names, err := ListAzureClustersFnForTest(ctx, info.ID)
		if err != nil {
			return 0
		}
		return len(names)
	}
	id, err := GetCallerIdentityFnForTest(ctx, region)
	if err != nil {
		return -1
	}
	b, err := OpenMetaBucketFnForTest(ctx, metaRegion, id.Account)
	if err != nil {
		return 0
	}
	names, err := ListAWSClustersFnForTest(ctx, b)
	if err != nil {
		return 0
	}
	return len(names)
}

// fetchCloudTab builds the full CloudTabData for the given cloud.
// subscriptionID selects a specific Azure subscription; pass "" to use the default.
func fetchCloudTab(ctx context.Context, cloud, metaRegion, region, subscriptionID string) CloudTabData {
	if cloud == "azure" {
		return fetchAzureTab(ctx, subscriptionID)
	}
	return fetchAWSTab(ctx, metaRegion, region)
}

func fetchAWSTab(ctx context.Context, metaRegion, region string) CloudTabData {
	tab := CloudTabData{Cloud: "aws"}

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

	names, err := ListAWSClustersFnForTest(ctx, b)
	if err != nil || len(names) == 0 {
		tab.State = tabStateNoClusters
		return tab
	}

	rows := make([]ClusterRow, 0, len(names))
	for _, name := range names {
		// Open the specific meta bucket for this cluster
		cb, err := metabucket.Open(ctx, metaRegion, avail.ActiveAccount, name)
		if err != nil {
			rows = append(rows, ClusterRow{Name: name, Status: "error", StatusClass: "error"})
			continue
		}
		m, err := metabucket.ReadMetadata(ctx, cb, name)
		if err != nil {
			rows = append(rows, ClusterRow{Name: name, Status: "error", StatusClass: "error"})
			continue
		}
		status := clusterDisplayStatus(m)
		rows = append(rows, ClusterRow{
			Name:         m.ClusterName,
			Region:       m.AWSRegion,
			Status:       formatStatus(status),
			StatusClass:  statusClass(status),
			LastModified: m.LastModifiedAt.UTC().Format("2006-01-02 15:04 UTC"),
		})
	}
	tab.State = tabStateOK
	tab.Clusters = rows
	return tab
}

func fetchAzureTab(ctx context.Context, subscriptionID string) CloudTabData {
	tab := CloudTabData{Cloud: "azure"}

	avail, subs := DetectAzureFnForTest(ctx)
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

	// Only expose multi-subscription selector when there is more than one enabled subscription.
	if len(subs) > 1 {
		tab.Subscriptions = subs
	}

	// Use the explicitly-requested subscription when provided; fall back to the
	// default subscription from az account show.
	activeSub := avail.ActiveAccount
	if subscriptionID != "" {
		activeSub = subscriptionID
	}
	tab.ActiveSubscription = activeSub

	names, err := ListAzureClustersFnForTest(ctx, activeSub)
	if err != nil || len(names) == 0 {
		tab.State = tabStateNoClusters
		return tab
	}

	rows := make([]ClusterRow, 0, len(names))
	for _, name := range names {
		c, err := OpenMetaContainerFnForTest(ctx, activeSub, name)
		if err != nil {
			rows = append(rows, ClusterRow{Name: name, Status: "error", StatusClass: "error"})
			continue
		}
		m, err := metacontainer.ReadMetadata(ctx, c, name)
		if err != nil {
			rows = append(rows, ClusterRow{Name: name, Status: "error", StatusClass: "error"})
			continue
		}
		status := clusterDisplayStatus(m)
		rows = append(rows, ClusterRow{
			Name:         m.ClusterName,
			Region:       m.AzureLocation,
			Status:       formatStatus(status),
			StatusClass:  statusClass(status),
			LastModified: m.LastModifiedAt.UTC().Format("2006-01-02 15:04 UTC"),
		})
	}
	tab.State = tabStateOK
	tab.Clusters = rows
	return tab
}

// buildTabBarAvail returns the (awsAvail, azureAvail) pair used by the tab bar
// partial to decide which tab buttons to disable.
//
// For the active cloud, the caller already holds a fully-populated Avail from
// the tab fetch — reuse it.  For the inactive cloud, only CLIInstalled matters
// for the tab bar (the count badge already covers the auth-missing case), so we
// do a cheap exec.LookPath probe rather than a full API round-trip.
func buildTabBarAvail(ctx context.Context, activeCloud string, activeAvail CloudAvailability) (aws, azure CloudAvailability) {
	if activeCloud == "aws" {
		return activeAvail, CloudAvailability{CLIInstalled: ProbeAzureCLIFnForTest(ctx)}
	}
	return CloudAvailability{CLIInstalled: ProbeAWSCLIFnForTest(ctx)}, activeAvail
}

func formatStatus(s string) string {
	if s == "" {
		return "active"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// countFromTab derives the cluster badge count from an already-fetched
// CloudTabData, avoiding a second API round-trip into the same request context.
func countFromTab(tab CloudTabData) int {
	switch tab.State {
	case tabStateOK:
		return len(tab.Clusters)
	case tabStateNoClusters:
		return 0
	default: // not_configured, error
		return -1
	}
}

func statusClass(s string) string {
	switch s {
	case "active":
		return "active"
	case "deployed":
		return "deployed"
	case "installing":
		return "installing"
	case "destroyed":
		return "destroyed"
	default:
		return "active"
	}
}
