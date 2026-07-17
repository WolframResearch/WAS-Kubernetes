package handlers_test

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/metacontainer"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
	"github.com/WolframResearch/WAS-Kubernetes/internal/webui/handlers"
)

// homeTabTemplates builds a minimal Templates set that covers the home page
// tab templates without depending on the embedded FS.
func homeTabTemplates(t *testing.T) *handlers.Templates {
	t.Helper()

	layout := `{{define "layout"}}{{block "content" .}}{{end}}{{end}}`
	homeSrc := `
{{define "content"}}
HOME:{{.ActiveCloud}}:AWS={{.AWSCount}}:AZ={{.AzureCount}}
{{template "_cloud_tabs" .}}
{{template "_clusters_content_wrapper" .Tab}}
{{end}}
{{define "_cloud_tabs"}}<div id="cloud-tabs"{{if .OOB}} hx-swap-oob="true"{{end}}>AWS{{if eq .ActiveCloud "aws"}} class="cloud-tab__link--active"{{end}}{{if not .AWSAvail.CLIInstalled}} aws-disabled{{end}}|Azure{{if eq .ActiveCloud "azure"}} class="cloud-tab__link--active"{{end}}{{if not .AzureAvail.CLIInstalled}} azure-disabled{{end}}</div>{{end}}
{{define "_clusters_content_wrapper"}}<div id="clusters-content" hx-trigger="load, every 10s" hx-get="/clusters/data?cloud={{.Cloud}}" hx-target="#clusters-content">{{template "_cluster_tab_body" .}}</div>{{end}}
{{define "_cluster_tab_body"}}TAB:{{.Cloud}}:{{.State}}{{if and (eq .State "not_configured") .Avail.CLIInstalled}}:CLI-WARNING{{end}}{{if and (eq .State "not_configured") (not .Avail.CLIInstalled)}}:CLI-MISSING{{end}}{{if eq .State "ok"}}:{{range .Clusters}}{{.Name}},{{end}}{{end}}{{if eq .State "not_configured"}}:{{.ConfigCmd}}{{end}}{{if .Subscriptions}}<select id="azure-sub-selector">{{range .Subscriptions}}<option value="{{.ID}}">{{.Name}}</option>{{end}}</select>{{end}}{{end}}
`
	opsOverviewSrc := `
{{define "content"}}OPS-OVERVIEW:{{.ActiveCloud}}{{end}}
{{define "_ops_cloud_tabs"}}<div id="ops-cloud-tabs"{{if .OOB}} hx-swap-oob="true"{{end}}>AWS{{if eq .ActiveCloud "aws"}} class="cloud-tab__link--active"{{end}}|Azure{{if eq .ActiveCloud "azure"}} class="cloud-tab__link--active"{{end}}</div>{{end}}
{{define "_ops_content_wrapper"}}<div id="ops-content" hx-trigger="load, every 10s" hx-get="/operations/data?cloud={{.Cloud}}" hx-target="#ops-content">{{template "_ops_tab_body" .}}</div>{{end}}
{{define "_ops_tab_body"}}OPS-TAB:{{.Cloud}}:{{.State}}{{end}}
`

	newT := func(srcs ...string) *template.Template {
		base := template.Must(template.New("base").Parse(layout))
		for _, s := range srcs {
			template.Must(base.New("p").Parse(s))
		}
		return base
	}

	return &handlers.Templates{
		Home:        newT(homeSrc),
		OpsOverview: newT(opsOverviewSrc),
	}
}

// TestHomePageTabsSwitching verifies that GET /clusters?cloud=azure
// sets the wasctl_last_cloud cookie and returns the Azure tab body.
func TestHomePageTabsSwitching(t *testing.T) {
	tmpls := homeTabTemplates(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters?cloud=azure", nil)

	handlers.ClustersByCloud(tmpls, "us-east-1", "us-east-1")(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	// Cookie must be set to "azure".
	var found bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "wasctl_last_cloud" {
			if c.Value != "azure" {
				t.Errorf("cookie: want azure, got %q", c.Value)
			}
			if !c.HttpOnly {
				t.Error("cookie must be HttpOnly")
			}
			found = true
		}
	}
	if !found {
		t.Error("wasctl_last_cloud cookie not set")
	}

	// Content-Type must be HTML.
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("want text/html, got %q", ct)
	}

	// Body must reference azure (the _cluster_tab_body partial).
	body := w.Body.String()
	if !strings.Contains(body, "azure") {
		t.Errorf("expected 'azure' in tab body, got: %s", body)
	}
}

// TestHomePageTabsSwitchingAWS verifies GET /clusters with no cloud param
// defaults to aws and sets the cookie accordingly.
func TestHomePageTabsSwitchingAWS(t *testing.T) {
	tmpls := homeTabTemplates(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters", nil)

	handlers.ClustersByCloud(tmpls, "us-east-1", "us-east-1")(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "wasctl_last_cloud" && c.Value != "aws" {
			t.Errorf("default cloud: want aws, got %q", c.Value)
		}
	}
}

// TestUnauthenticatedTabRendersEmptyState verifies that when credentials are
// unavailable the tab body renders the not_configured empty state (not a 500).
func TestUnauthenticatedTabRendersEmptyState(t *testing.T) {
	tmpls := homeTabTemplates(t)

	// Override cloud API functions to simulate missing credentials.
	origAWS := handlers.GetCallerIdentityFnForTest
	origAzure := handlers.GetAzureAccountInfoFnForTest
	handlers.GetCallerIdentityFnForTest = func(_ context.Context, _ string) (tools.CallerIdentity, error) {
		return tools.CallerIdentity{}, errNoCredentials
	}
	handlers.GetAzureAccountInfoFnForTest = func(_ context.Context) (tools.AccountInfo, error) {
		return tools.AccountInfo{}, errNoCredentials
	}
	t.Cleanup(func() {
		handlers.GetCallerIdentityFnForTest = origAWS
		handlers.GetAzureAccountInfoFnForTest = origAzure
	})

	for _, cloud := range []string{"aws", "azure"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/clusters?cloud="+cloud, nil)
		handlers.ClustersByCloud(tmpls, "us-east-1", "us-east-1")(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("cloud=%s: want 200, got %d", cloud, w.Code)
		}
		if strings.Contains(w.Body.String(), "internal error:") {
			t.Errorf("cloud=%s: unexpected internal error in body", cloud)
		}
		// The tab body must show the not_configured state.
		if !strings.Contains(w.Body.String(), "not_configured") {
			t.Errorf("cloud=%s: expected not_configured state in body; got: %s", cloud, w.Body.String())
		}
	}
}

// TestTabQueriesCorrectMetaStore verifies that on a tab switch each cloud's
// auth is called exactly once: the active cloud from fetchCloudTab (full data),
// the inactive cloud from fetchCloudCount. The active cloud count is derived
// from the already-fetched tab data — no second API call is made for it.
func TestTabQueriesCorrectMetaStore(t *testing.T) {
	tmpls := homeTabTemplates(t)

	var awsCalled, azureCalled int

	origCallerID := handlers.GetCallerIdentityFnForTest
	origAccountInfo := handlers.GetAzureAccountInfoFnForTest
	origOpenBucket := handlers.OpenMetaBucketFnForTest
	origOpenContainer := handlers.OpenMetaContainerFnForTest
	origDetectAWS := handlers.DetectAWSFnForTest
	origDetectAzure := handlers.DetectAzureFnForTest

	handlers.GetCallerIdentityFnForTest = func(_ context.Context, _ string) (tools.CallerIdentity, error) {
		awsCalled++
		return tools.CallerIdentity{Account: "123456789012"}, nil
	}
	handlers.GetAzureAccountInfoFnForTest = func(_ context.Context) (tools.AccountInfo, error) {
		azureCalled++
		return tools.AccountInfo{ID: "sub-abc"}, nil
	}
	// Detect functions delegate to the injectable auth functions so counts are accurate
	// regardless of whether the real AWS/Azure CLIs are installed on this machine.
	handlers.DetectAWSFnForTest = func(ctx context.Context, region string) handlers.CloudAvailability {
		id, err := handlers.GetCallerIdentityFnForTest(ctx, region)
		if err != nil {
			return handlers.CloudAvailability{CLIInstalled: true}
		}
		return handlers.CloudAvailability{CLIInstalled: true, Authenticated: true, ActiveAccount: id.Account}
	}
	handlers.DetectAzureFnForTest = func(ctx context.Context) (handlers.CloudAvailability, []handlers.AzureSubscription) {
		info, err := handlers.GetAzureAccountInfoFnForTest(ctx)
		if err != nil {
			return handlers.CloudAvailability{CLIInstalled: true}, nil
		}
		return handlers.CloudAvailability{CLIInstalled: true, Authenticated: true, ActiveAccount: info.ID}, nil
	}
	// Return errors so we don't need real cloud infra. Auth calls still happen.
	handlers.OpenMetaBucketFnForTest = func(_ context.Context, _, _ string) (*metabucket.Bucket, error) {
		return nil, errNoCredentials
	}
	handlers.OpenMetaContainerFnForTest = func(_ context.Context, _, _ string) (*metacontainer.Container, error) {
		return nil, errNoCredentials
	}

	t.Cleanup(func() {
		handlers.GetCallerIdentityFnForTest = origCallerID
		handlers.GetAzureAccountInfoFnForTest = origAccountInfo
		handlers.OpenMetaBucketFnForTest = origOpenBucket
		handlers.OpenMetaContainerFnForTest = origOpenContainer
		handlers.DetectAWSFnForTest = origDetectAWS
		handlers.DetectAzureFnForTest = origDetectAzure
	})

	// AWS tab: AWS auth called once (full-data via fetchAWSTab); count from tab, no second call.
	// Azure auth called once (inactive-cloud count via fetchCloudCount).
	awsCalled, azureCalled = 0, 0
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters?cloud=aws", nil)
	handlers.ClustersByCloud(tmpls, "us-east-1", "us-east-1")(w, r)
	if awsCalled != 1 {
		t.Errorf("aws tab: AWS GetCallerIdentity should be called exactly once (data only, count from tab), got %d", awsCalled)
	}
	if azureCalled != 1 {
		t.Errorf("aws tab: Azure GetAccountInfo should be called exactly once (count only), got %d", azureCalled)
	}

	// Azure tab: Azure auth called once (full-data); count from tab, no second call.
	// AWS auth called once (inactive-cloud count via fetchCloudCount).
	awsCalled, azureCalled = 0, 0
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/clusters?cloud=azure", nil)
	handlers.ClustersByCloud(tmpls, "us-east-1", "us-east-1")(w, r)
	if azureCalled != 1 {
		t.Errorf("azure tab: Azure GetAccountInfo should be called exactly once (data only, count from tab), got %d", azureCalled)
	}
	if awsCalled != 1 {
		t.Errorf("azure tab: AWS GetCallerIdentity should be called exactly once (count only), got %d", awsCalled)
	}
}

// TestOperationsPageTabbedStructure verifies that GET /operations/clusters
// with a cloud query param sets the cookie and returns an ops tab body.
func TestOperationsPageTabbedStructure(t *testing.T) {
	tmpls := homeTabTemplates(t)

	for _, cloud := range []string{"aws", "azure"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/operations/clusters?cloud="+cloud, nil)
		handlers.OperationsByCloud(tmpls, "us-east-1", "us-east-1")(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("cloud=%s: want 200, got %d", cloud, w.Code)
		}

		var cookieFound bool
		for _, c := range w.Result().Cookies() {
			if c.Name == "wasctl_last_cloud" {
				if c.Value != cloud {
					t.Errorf("cloud=%s: cookie value %q != %q", cloud, c.Value, cloud)
				}
				cookieFound = true
			}
		}
		if !cookieFound {
			t.Errorf("cloud=%s: wasctl_last_cloud cookie not set", cloud)
		}

		body := w.Body.String()
		if !strings.Contains(body, cloud) {
			t.Errorf("cloud=%s: expected cloud name in body; got: %s", cloud, body)
		}
	}
}

// TestHomePageEmptyWhenNoClusters verifies that when AWS auth succeeds but the
// meta bucket contains no cluster entries the tab renders [NoClustersYet].
func TestHomePageEmptyWhenNoClusters(t *testing.T) {
	tmpls := homeTabTemplates(t)

	origID := handlers.GetCallerIdentityFnForTest
	origOpen := handlers.OpenMetaBucketFnForTest
	origList := handlers.ListAWSClustersFnForTest
	origDetect := handlers.DetectAWSFnForTest
	handlers.GetCallerIdentityFnForTest = func(_ context.Context, _ string) (tools.CallerIdentity, error) {
		return tools.CallerIdentity{Account: "123456789012"}, nil
	}
	handlers.DetectAWSFnForTest = func(ctx context.Context, region string) handlers.CloudAvailability {
		id, _ := handlers.GetCallerIdentityFnForTest(ctx, region)
		return handlers.CloudAvailability{CLIInstalled: true, Authenticated: true, ActiveAccount: id.Account}
	}
	// Open returns nil bucket with no error; ListAWSClustersFnForTest never dereferences it.
	handlers.OpenMetaBucketFnForTest = func(_ context.Context, _, _ string) (*metabucket.Bucket, error) {
		return nil, nil
	}
	handlers.ListAWSClustersFnForTest = func(_ context.Context, _ *metabucket.Bucket) ([]string, error) {
		return nil, nil // empty list
	}
	t.Cleanup(func() {
		handlers.GetCallerIdentityFnForTest = origID
		handlers.OpenMetaBucketFnForTest = origOpen
		handlers.ListAWSClustersFnForTest = origList
		handlers.DetectAWSFnForTest = origDetect
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters?cloud=aws", nil)
	handlers.ClustersByCloud(tmpls, "us-east-1", "us-east-1")(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "no_clusters") {
		t.Errorf("want no_clusters state; got: %s", body)
	}
	// Must not render any cluster row content.
	if strings.Contains(body, "was-prod") || strings.Contains(body, "was-dev") {
		t.Errorf("should not contain cluster name rows; got: %s", body)
	}
}

// TestHomePageEmptyWhenMetaBucketMissing verifies that when the meta bucket
// does not exist (Open returns an error) the tab still renders no_clusters,
// not an error banner.
func TestHomePageEmptyWhenMetaBucketMissing(t *testing.T) {
	tmpls := homeTabTemplates(t)

	origID := handlers.GetCallerIdentityFnForTest
	origOpen := handlers.OpenMetaBucketFnForTest
	origDetect := handlers.DetectAWSFnForTest
	handlers.GetCallerIdentityFnForTest = func(_ context.Context, _ string) (tools.CallerIdentity, error) {
		return tools.CallerIdentity{Account: "123456789012"}, nil
	}
	handlers.DetectAWSFnForTest = func(ctx context.Context, region string) handlers.CloudAvailability {
		id, _ := handlers.GetCallerIdentityFnForTest(ctx, region)
		return handlers.CloudAvailability{CLIInstalled: true, Authenticated: true, ActiveAccount: id.Account}
	}
	handlers.OpenMetaBucketFnForTest = func(_ context.Context, _, _ string) (*metabucket.Bucket, error) {
		return nil, &stubError{"NoSuchBucket"}
	}
	t.Cleanup(func() {
		handlers.GetCallerIdentityFnForTest = origID
		handlers.OpenMetaBucketFnForTest = origOpen
		handlers.DetectAWSFnForTest = origDetect
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters?cloud=aws", nil)
	handlers.ClustersByCloud(tmpls, "us-east-1", "us-east-1")(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "no_clusters") {
		t.Errorf("want no_clusters state; got: %s", body)
	}
	if strings.Contains(body, "internal error") {
		t.Errorf("must not show error state; got: %s", body)
	}
}

// TestTabSwitchUpdatesActiveIndicator verifies that a tab-switch HTMX request
// (HX-Request: true) returns an OOB _cloud_tabs fragment with hx-swap-oob and
// the newly-selected cloud marked as active.
func TestTabSwitchUpdatesActiveIndicator(t *testing.T) {
	tmpls := homeTabTemplates(t)

	// Override so no real cloud calls are attempted.
	origID := handlers.GetCallerIdentityFnForTest
	origAZ := handlers.GetAzureAccountInfoFnForTest
	handlers.GetCallerIdentityFnForTest = func(_ context.Context, _ string) (tools.CallerIdentity, error) {
		return tools.CallerIdentity{}, errNoCredentials
	}
	handlers.GetAzureAccountInfoFnForTest = func(_ context.Context) (tools.AccountInfo, error) {
		return tools.AccountInfo{}, errNoCredentials
	}
	t.Cleanup(func() {
		handlers.GetCallerIdentityFnForTest = origID
		handlers.GetAzureAccountInfoFnForTest = origAZ
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters?cloud=azure", nil)
	r.Header.Set("HX-Request", "true")
	handlers.ClustersByCloud(tmpls, "us-east-1", "us-east-1")(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()

	// OOB tab bar must be present with the swap attribute.
	if !strings.Contains(body, "hx-swap-oob") {
		t.Error("HTMX response missing OOB tab bar fragment (hx-swap-oob)")
	}
	// The tab bar fragment must carry the active class on Azure.
	if !strings.Contains(body, "cloud-tab__link--active") {
		t.Error("active tab class missing from HTMX response")
	}
	// Sanity: the OOB div targets the right element.
	if !strings.Contains(body, `id="cloud-tabs"`) {
		t.Error("OOB fragment must have id=\"cloud-tabs\"")
	}
}

// ── Part 2 — CLI detection, disabled tabs, warning banners ────────────────────

// TestCloudTabRendersDisabledWhenCLINotInstalled verifies that when the AWS CLI
// is absent, the tab bar marks the AWS button disabled and the tab body shows a
// CLI-MISSING hint rather than a generic not-configured message.
func TestCloudTabRendersDisabledWhenCLINotInstalled(t *testing.T) {
	tmpls := homeTabTemplates(t)

	origDetect := handlers.DetectAWSFnForTest
	origProbe := handlers.ProbeAWSCLIFnForTest
	handlers.DetectAWSFnForTest = func(_ context.Context, _ string) handlers.CloudAvailability {
		return handlers.CloudAvailability{CLIInstalled: false}
	}
	handlers.ProbeAWSCLIFnForTest = func(_ context.Context) bool { return false }
	t.Cleanup(func() {
		handlers.DetectAWSFnForTest = origDetect
		handlers.ProbeAWSCLIFnForTest = origProbe
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters?cloud=aws", nil)
	handlers.ClustersByCloud(tmpls, "us-east-1", "us-east-1")(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()

	if !strings.Contains(body, "aws-disabled") {
		t.Errorf("tab bar: expected aws-disabled when AWS CLI absent; got: %s", body)
	}
	if !strings.Contains(body, "CLI-MISSING") {
		t.Errorf("tab body: expected CLI-MISSING hint when AWS CLI absent; got: %s", body)
	}
	if strings.Contains(body, "CLI-WARNING") {
		t.Errorf("tab body: unexpected CLI-WARNING when CLI not installed; got: %s", body)
	}
}

// TestCloudTabRendersWarningWhenNotAuthenticated verifies that when the CLI is
// present but credentials are missing, the tab body shows CLI-WARNING (not
// CLI-MISSING) and the tab button is not disabled.
func TestCloudTabRendersWarningWhenNotAuthenticated(t *testing.T) {
	tmpls := homeTabTemplates(t)

	origDetect := handlers.DetectAWSFnForTest
	origProbe := handlers.ProbeAWSCLIFnForTest
	handlers.DetectAWSFnForTest = func(_ context.Context, _ string) handlers.CloudAvailability {
		return handlers.CloudAvailability{
			CLIInstalled:  true,
			CLIVersion:    "aws-cli/2.17.0",
			Authenticated: false,
			AuthError:     "run 'aws configure'",
		}
	}
	handlers.ProbeAWSCLIFnForTest = func(_ context.Context) bool { return true }
	t.Cleanup(func() {
		handlers.DetectAWSFnForTest = origDetect
		handlers.ProbeAWSCLIFnForTest = origProbe
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters?cloud=aws", nil)
	handlers.ClustersByCloud(tmpls, "us-east-1", "us-east-1")(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()

	if strings.Contains(body, "aws-disabled") {
		t.Errorf("tab bar: unexpected aws-disabled when CLI installed; got: %s", body)
	}
	if !strings.Contains(body, "CLI-WARNING") {
		t.Errorf("tab body: expected CLI-WARNING when authenticated=false; got: %s", body)
	}
	if strings.Contains(body, "CLI-MISSING") {
		t.Errorf("tab body: unexpected CLI-MISSING when CLI is installed; got: %s", body)
	}
}

// TestCloudTabRendersNormalWhenAuthenticated verifies that when AWS is fully
// authenticated the tab renders in the normal ok/no_clusters state without any
// warning markers.
func TestCloudTabRendersNormalWhenAuthenticated(t *testing.T) {
	tmpls := homeTabTemplates(t)

	origDetect := handlers.DetectAWSFnForTest
	origOpen := handlers.OpenMetaBucketFnForTest
	origProbe := handlers.ProbeAWSCLIFnForTest
	handlers.DetectAWSFnForTest = func(_ context.Context, _ string) handlers.CloudAvailability {
		return handlers.CloudAvailability{
			CLIInstalled:  true,
			CLIVersion:    "aws-cli/2.17.0",
			Authenticated: true,
			ActiveAccount: "123456789012",
		}
	}
	handlers.OpenMetaBucketFnForTest = func(_ context.Context, _, _ string) (*metabucket.Bucket, error) {
		return nil, errNoCredentials // triggers no_clusters — sufficient to prove normal flow
	}
	handlers.ProbeAWSCLIFnForTest = func(_ context.Context) bool { return true }
	t.Cleanup(func() {
		handlers.DetectAWSFnForTest = origDetect
		handlers.OpenMetaBucketFnForTest = origOpen
		handlers.ProbeAWSCLIFnForTest = origProbe
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters?cloud=aws", nil)
	handlers.ClustersByCloud(tmpls, "us-east-1", "us-east-1")(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()

	if strings.Contains(body, "CLI-WARNING") || strings.Contains(body, "CLI-MISSING") {
		t.Errorf("no warning expected when authenticated; got: %s", body)
	}
	if strings.Contains(body, "aws-disabled") {
		t.Errorf("tab should not be disabled when CLI installed; got: %s", body)
	}
}

// TestAzureSubscriptionSelectorHiddenWithOneSub verifies that when there is
// exactly one Azure subscription the <select> is not rendered.
func TestAzureSubscriptionSelectorHiddenWithOneSub(t *testing.T) {
	tmpls := homeTabTemplates(t)

	origDetect := handlers.DetectAzureFnForTest
	origProbe := handlers.ProbeAzureCLIFnForTest
	handlers.DetectAzureFnForTest = func(_ context.Context) (handlers.CloudAvailability, []handlers.AzureSubscription) {
		avail := handlers.CloudAvailability{
			CLIInstalled:  true,
			Authenticated: true,
			ActiveAccount: "sub-single",
		}
		return avail, []handlers.AzureSubscription{
			{ID: "sub-single", Name: "Production", IsDefault: true, State: "Enabled"},
		}
	}
	handlers.ProbeAzureCLIFnForTest = func(_ context.Context) bool { return true }
	t.Cleanup(func() {
		handlers.DetectAzureFnForTest = origDetect
		handlers.ProbeAzureCLIFnForTest = origProbe
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters?cloud=azure", nil)
	handlers.ClustersByCloud(tmpls, "us-east-1", "us-east-1")(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()

	if strings.Contains(body, "azure-sub-selector") {
		t.Errorf("subscription selector should be hidden with single sub; got: %s", body)
	}
}

// TestAzureSubscriptionSelectorVisibleWithMultipleSubs verifies that with two or
// more enabled subscriptions a <select> is rendered with all subscription options.
func TestAzureSubscriptionSelectorVisibleWithMultipleSubs(t *testing.T) {
	tmpls := homeTabTemplates(t)

	origDetect := handlers.DetectAzureFnForTest
	origProbe := handlers.ProbeAzureCLIFnForTest
	handlers.DetectAzureFnForTest = func(_ context.Context) (handlers.CloudAvailability, []handlers.AzureSubscription) {
		avail := handlers.CloudAvailability{
			CLIInstalled:  true,
			Authenticated: true,
			ActiveAccount: "sub-aaa",
		}
		subs := []handlers.AzureSubscription{
			{ID: "sub-aaa", Name: "Production", IsDefault: true, State: "Enabled"},
			{ID: "sub-bbb", Name: "Development", State: "Enabled"},
		}
		return avail, subs
	}
	handlers.ProbeAzureCLIFnForTest = func(_ context.Context) bool { return true }
	t.Cleanup(func() {
		handlers.DetectAzureFnForTest = origDetect
		handlers.ProbeAzureCLIFnForTest = origProbe
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters?cloud=azure", nil)
	handlers.ClustersByCloud(tmpls, "us-east-1", "us-east-1")(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()

	if !strings.Contains(body, "azure-sub-selector") {
		t.Errorf("subscription selector should appear with multiple subs; got: %s", body)
	}
	if !strings.Contains(body, "sub-aaa") || !strings.Contains(body, "sub-bbb") {
		t.Errorf("both subscription IDs should appear in selector; got: %s", body)
	}
	if !strings.Contains(body, "Production") || !strings.Contains(body, "Development") {
		t.Errorf("both subscription names should appear; got: %s", body)
	}
}

// TestBackgroundRefreshAttributesPresentOnContentRegion verifies that the
// cluster content region carries hx-trigger="load, every 10s" so the list
// auto-refreshes without full-page navigation.
func TestBackgroundRefreshAttributesPresentOnContentRegion(t *testing.T) {
	tmpls := homeTabTemplates(t)

	origID := handlers.GetCallerIdentityFnForTest
	origAZ := handlers.GetAzureAccountInfoFnForTest
	handlers.GetCallerIdentityFnForTest = func(_ context.Context, _ string) (tools.CallerIdentity, error) {
		return tools.CallerIdentity{}, errNoCredentials
	}
	handlers.GetAzureAccountInfoFnForTest = func(_ context.Context) (tools.AccountInfo, error) {
		return tools.AccountInfo{}, errNoCredentials
	}
	t.Cleanup(func() {
		handlers.GetCallerIdentityFnForTest = origID
		handlers.GetAzureAccountInfoFnForTest = origAZ
	})

	for _, cloud := range []string{"aws", "azure"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/clusters?cloud="+cloud, nil)
		handlers.ClustersByCloud(tmpls, "us-east-1", "us-east-1")(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("cloud=%s: want 200, got %d", cloud, w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, `hx-trigger="load, every 10s"`) {
			t.Errorf("cloud=%s: hx-trigger for background refresh missing; got: %s", cloud, body)
		}
		if !strings.Contains(body, `hx-get="/clusters/data?cloud=`+cloud+`"`) {
			t.Errorf("cloud=%s: hx-get should target /clusters/data endpoint for cloud; got: %s", cloud, body)
		}
	}
}

// TestTabSwitchStopsOldRefresh verifies that the content region's hx-get
// attribute targets the newly-selected cloud's data endpoint after a tab switch,
// ensuring htmx replaces the old polling element and restarts polling for the
// new cloud.
func TestTabSwitchStopsOldRefresh(t *testing.T) {
	tmpls := homeTabTemplates(t)

	origID := handlers.GetCallerIdentityFnForTest
	origAZ := handlers.GetAzureAccountInfoFnForTest
	handlers.GetCallerIdentityFnForTest = func(_ context.Context, _ string) (tools.CallerIdentity, error) {
		return tools.CallerIdentity{}, errNoCredentials
	}
	handlers.GetAzureAccountInfoFnForTest = func(_ context.Context) (tools.AccountInfo, error) {
		return tools.AccountInfo{}, errNoCredentials
	}
	t.Cleanup(func() {
		handlers.GetCallerIdentityFnForTest = origID
		handlers.GetAzureAccountInfoFnForTest = origAZ
	})

	// Switch from AWS to Azure.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/clusters?cloud=azure", nil)
	r.Header.Set("HX-Request", "true")
	handlers.ClustersByCloud(tmpls, "us-east-1", "us-east-1")(w, r)

	body := w.Body.String()

	// The content region wrapper must use the background-refresh data endpoint for azure.
	if !strings.Contains(body, `hx-get="/clusters/data?cloud=azure"`) {
		t.Errorf("after switch to azure, content wrapper hx-get must target /clusters/data?cloud=azure; got: %s", body)
	}
	if strings.Contains(body, `hx-get="/clusters/data?cloud=aws"`) {
		t.Errorf("after switch to azure, no aws data endpoint should be present in tab body; got: %s", body)
	}
}

// errNoCredentials is a sentinel error for test doubles.
var errNoCredentials = &stubError{"no credentials"}

type stubError struct{ msg string }

func (e *stubError) Error() string { return e.msg }
