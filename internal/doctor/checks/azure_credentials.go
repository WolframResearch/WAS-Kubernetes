package checks

import (
	"context"
	"fmt"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
)

// getAccountInfoFn is overridable for tests.
var getAccountInfoFn = func(ctx context.Context) (tools.AccountInfo, error) {
	return tools.GetAccountInfo(ctx)
}

// azureCredentialsPrecondition skips when cloud is not azure.
func azureCredentialsPrecondition(rc *doctor.RunContext) (bool, string) {
	if rc.Cfg.Cloud != "azure" {
		return true, "not an Azure cluster"
	}
	return false, ""
}

// AzureCredentials checks that az CLI is logged in and credentials are valid.
type AzureCredentials struct{ BaseCheck }

func (AzureCredentials) ID() string                { return "azure.credentials" }
func (AzureCredentials) Name() string              { return "Azure credentials valid" }
func (AzureCredentials) Category() doctor.Category { return doctor.CategoryEnvironment }
func (AzureCredentials) Description() string {
	return "Calls az account show to verify Azure credentials are valid and not expired."
}
func (c AzureCredentials) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	return azureCredentialsPrecondition(rc)
}
func (c AzureCredentials) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	info, err := getAccountInfoFn(ctx)
	if err != nil {
		msg := err.Error()
		remediation := "Log in to Azure:\n" +
			"  az login\n" +
			"  az account set --subscription <id>\n" +
			"See: docs/Troubleshooting.md#azure-credentials"
		if strings.Contains(msg, "Please run 'az login'") || strings.Contains(msg, "not logged in") {
			msg = "Not logged in to Azure CLI"
		} else if strings.Contains(msg, "token has expired") || strings.Contains(msg, "AADSTS70043") {
			msg = "Azure credentials have expired"
			remediation = "Refresh your Azure credentials:\n  az login"
		}
		return fail(c.ID(), c.Name(), doctor.SeverityCritical, msg, remediation)
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("authenticated to tenant %s (subscription %s)", info.TenantID, info.ID))
}

// AzureSubscriptionAccess checks the active subscription is the expected one.
type AzureSubscriptionAccess struct{ BaseCheck }

func (AzureSubscriptionAccess) ID() string                { return "azure.subscription_access" }
func (AzureSubscriptionAccess) Name() string              { return "Azure subscription accessible" }
func (AzureSubscriptionAccess) Category() doctor.Category { return doctor.CategoryEnvironment }
func (AzureSubscriptionAccess) Description() string {
	return "Verifies the Azure subscription is set and accessible in the current az context."
}
func (c AzureSubscriptionAccess) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	if skip, reason := azureCredentialsPrecondition(rc); skip {
		return skip, reason
	}
	if rc.AzureSubscriptionID == "" {
		return true, "Azure credentials unavailable; check azure.credentials first"
	}
	return false, ""
}
func (c AzureSubscriptionAccess) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	info, err := getAccountInfoFn(ctx)
	if err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("az account show: %v", err))
	}
	if !info.IsDefault {
		return fail(c.ID(), c.Name(), doctor.SeverityWarning,
			fmt.Sprintf("subscription %q is not the default subscription", info.Name),
			"Set the correct subscription:\n  az account set --subscription "+info.ID)
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("subscription %q (%s) is active", info.Name, info.ID))
}
