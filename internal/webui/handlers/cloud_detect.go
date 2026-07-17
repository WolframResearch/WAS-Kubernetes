package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// CloudAvailability captures CLI presence and credential state for one cloud.
// Populated on every tab request; never cached between requests.
type CloudAvailability struct {
	CLIInstalled  bool   // true if the cloud CLI binary is present and executes
	CLIVersion    string // e.g. "aws-cli/2.17.3 …" or "azure-cli/2.65.0"
	Authenticated bool   // true if current credentials are accepted
	AuthError     string // human-readable hint when !Authenticated
	ActiveAccount string // AWS account ID or Azure subscription display name
}

// AzureSubscription is one entry from az account list --output json.
// Used by the Azure tab to render a subscription selector when >1 enabled
// subscription exists under the authenticated identity.
type AzureSubscription struct {
	ID        string
	Name      string
	TenantID  string
	IsDefault bool
	State     string // "Enabled", "Disabled", etc.
}

// ── Injectable detection functions ───────────────────────────────────────────
//
// Replace these in tests to avoid real CLI/API calls.
// detectAWSAvailability and detectAzureAvailability both delegate their auth
// check to GetCallerIdentityFnForTest / GetAzureAccountInfoFnForTest, so tests
// that already override those functions work without change.

var (
	// DetectAWSFnForTest / DetectAzureFnForTest are the entry points for all
	// credential detection. Tests replace them with fakes. In production they
	// call through the serverCredCache (when enabled) to avoid subprocess forks
	// on every HTTP request.
	DetectAWSFnForTest = func(ctx context.Context, region string) CloudAvailability {
		if serverCredCache != nil {
			if avail, ok := serverCredCache.getAWS(region); ok {
				return avail
			}
		}
		avail := detectAWSAvailability(ctx, region)
		if serverCredCache != nil {
			serverCredCache.setAWS(region, avail)
		}
		return avail
	}
	DetectAzureFnForTest = func(ctx context.Context) (CloudAvailability, []AzureSubscription) {
		if serverCredCache != nil {
			if avail, subs, ok := serverCredCache.getAzure(); ok {
				return avail, subs
			}
		}
		avail, subs := detectAzureAvailability(ctx)
		if serverCredCache != nil {
			serverCredCache.setAzure(avail, subs)
		}
		return avail, subs
	}
	ListAzureSubsFnForTest = listAzureSubscriptions

	// ProbeAWSCLIFnForTest / ProbeAzureCLIFnForTest are lightweight probes used
	// when building the tab bar for the INACTIVE cloud (no auth API call needed).
	ProbeAWSCLIFnForTest   = func(_ context.Context) bool { _, err := exec.LookPath("aws"); return err == nil }
	ProbeAzureCLIFnForTest = func(_ context.Context) bool { _, err := exec.LookPath("az"); return err == nil }
)

// detectAWSAvailability checks the AWS CLI binary, then tests credentials via
// GetCallerIdentityFnForTest (injectable in tests).
func detectAWSAvailability(ctx context.Context, region string) CloudAvailability {
	avail := CloudAvailability{}

	out, err := exec.CommandContext(ctx, "aws", "--version").CombinedOutput() //nolint:gosec
	if err != nil {
		return avail // CLIInstalled stays false
	}
	avail.CLIInstalled = true
	avail.CLIVersion = strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]

	id, err := GetCallerIdentityFnForTest(ctx, region)
	if err != nil {
		avail.AuthError = "run 'aws configure' or set AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY"
		return avail
	}
	avail.Authenticated = true
	avail.ActiveAccount = id.Account
	return avail
}

// detectAzureAvailability checks the Azure CLI binary, tests credentials via
// GetAzureAccountInfoFnForTest, then enumerates subscriptions.
func detectAzureAvailability(ctx context.Context) (CloudAvailability, []AzureSubscription) {
	avail := CloudAvailability{}

	out, err := exec.CommandContext(ctx, "az", "version", "--output", "json").CombinedOutput() //nolint:gosec
	if err != nil {
		return avail, nil
	}
	avail.CLIInstalled = true
	avail.CLIVersion = parseAzureCLIVersion(out)

	info, err := GetAzureAccountInfoFnForTest(ctx)
	if err != nil {
		avail.AuthError = "run 'az login'"
		return avail, nil
	}
	avail.Authenticated = true
	avail.ActiveAccount = info.ID // subscription ID; use Name for display if needed

	subs, _ := ListAzureSubsFnForTest(ctx)
	return avail, subs
}

// listAzureSubscriptions calls az account list and returns the parsed slice.
// Returns nil without error on empty output (not a fatal condition).
func listAzureSubscriptions(ctx context.Context) ([]AzureSubscription, error) {
	out, err := exec.CommandContext(ctx, "az", "account", "list", "--output", "json").Output() //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("az account list: %w", err)
	}
	type rawSub struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		TenantID  string `json:"tenantId"`
		IsDefault bool   `json:"isDefault"`
		State     string `json:"state"`
	}
	var raw []rawSub
	if err := json.Unmarshal(bytes.TrimSpace(out), &raw); err != nil {
		return nil, fmt.Errorf("parse az account list: %w", err)
	}
	subs := make([]AzureSubscription, len(raw))
	for i, r := range raw {
		subs[i] = AzureSubscription{
			ID:        r.ID,
			Name:      r.Name,
			TenantID:  r.TenantID,
			IsDefault: r.IsDefault,
			State:     r.State,
		}
	}
	return subs, nil
}

// parseAzureCLIVersion extracts "azure-cli/<version>" from the JSON output of
// az version --output json.
func parseAzureCLIVersion(jsonOut []byte) string {
	var obj map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(jsonOut), &obj); err != nil {
		return ""
	}
	if v, ok := obj["azure-cli"].(string); ok {
		return "azure-cli/" + v
	}
	return ""
}
