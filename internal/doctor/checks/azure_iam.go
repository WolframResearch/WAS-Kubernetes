package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

// azureRoleAssignmentFn checks what role assignments exist for the current user.
// Overridable for tests.
var azureRoleAssignmentFn = func(ctx context.Context, subscriptionID string) ([]byte, error) {
	return runOutput(ctx, "az", "role", "assignment", "list",
		"--assignee", "[current]",
		"--scope", "/subscriptions/"+subscriptionID,
		"--output", "json")
}

// azureResourceGroupCreateCheckFn dry-runs resource group creation.
// Overridable for tests.
var azureResourceGroupCreateCheckFn = func(ctx context.Context, subscriptionID, location string) ([]byte, error) {
	return runOutput(ctx, "az", "deployment", "sub", "what-if",
		"--location", location,
		"--subscription", subscriptionID,
		"--template-uri", "https://schema.management.azure.com/schemas/2018-05-01/subscriptionDeploymentTemplate.json#",
		"--parameters", `{"$schema":"https://schema.management.azure.com/schemas/2019-04-01/deploymentParameters.json#","contentVersion":"1.0.0.0","parameters":{}}`,
	)
}

type roleAssignment struct {
	RoleDefinitionName string `json:"roleDefinitionName"`
	Scope              string `json:"scope"`
}

// hasContributorOrHigher returns true if any assignment grants Contributor or Owner.
func hasContributorOrHigher(assignments []roleAssignment) bool {
	for _, a := range assignments {
		name := strings.ToLower(a.RoleDefinitionName)
		if name == "contributor" || name == "owner" {
			return true
		}
	}
	return false
}

// hasRoleAssignmentPermission returns true if any assignment grants
// User Access Administrator or Owner (the two roles that can assign roles).
func hasRoleAssignmentPermission(assignments []roleAssignment) bool {
	for _, a := range assignments {
		name := strings.ToLower(a.RoleDefinitionName)
		if name == "user access administrator" || name == "owner" {
			return true
		}
	}
	return false
}

// AzureCanCreateResourceGroup checks the current principal can create resource groups.
type AzureCanCreateResourceGroup struct{ BaseCheck }

func (AzureCanCreateResourceGroup) ID() string                { return "azure.iam.create_resource_group" }
func (AzureCanCreateResourceGroup) Name() string              { return "Can create Azure resource groups" }
func (AzureCanCreateResourceGroup) Category() doctor.Category { return doctor.CategoryEnvironment }
func (AzureCanCreateResourceGroup) Description() string {
	return "Verifies the principal has at least Contributor role at subscription scope (required to create resource groups)."
}
func (c AzureCanCreateResourceGroup) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	if skip, reason := azureCredentialsPrecondition(rc); skip {
		return skip, reason
	}
	if rc.AzureSubscriptionID == "" {
		return true, "Azure credentials unavailable; check azure.credentials first"
	}
	return false, ""
}
func (c AzureCanCreateResourceGroup) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	out, err := azureRoleAssignmentFn(ctx, rc.AzureSubscriptionID)
	if err != nil {
		if strings.Contains(err.Error(), "AuthorizationFailed") || strings.Contains(err.Error(), "403") {
			return skip(c.ID(), c.Name(), "insufficient permissions to list role assignments; cannot verify")
		}
		return checkError(c.ID(), c.Name(), fmt.Sprintf("az role assignment list: %v", err))
	}
	var assignments []roleAssignment
	if err := json.Unmarshal(out, &assignments); err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("parse role assignments: %v", err))
	}
	if !hasContributorOrHigher(assignments) {
		return fail(c.ID(), c.Name(), doctor.SeverityProblem,
			"principal lacks Contributor or Owner role at subscription scope",
			"Assign the Contributor role:\n"+
				"  az role assignment create --assignee <principal-id> --role Contributor --scope /subscriptions/"+rc.AzureSubscriptionID+"\n"+
				"See: README.md (Prerequisites) and docs/Install.md")
	}
	return pass(c.ID(), c.Name(), "Contributor or higher role found at subscription scope")
}

// AzureCanAssignRoles checks whether the principal can assign Azure RBAC roles.
// Default wasctl Azure installs use static storage-account-key auth and do not
// need this permission. The check is advisory (warning) for operators who want
// to opt into Workload Identity later.
type AzureCanAssignRoles struct{ BaseCheck }

func (AzureCanAssignRoles) ID() string                { return "azure.iam.assign_roles" }
func (AzureCanAssignRoles) Name() string              { return "Can assign Azure roles" }
func (AzureCanAssignRoles) Category() doctor.Category { return doctor.CategoryEnvironment }
func (AzureCanAssignRoles) Description() string {
	return "Advisory: User Access Administrator or Owner is only needed to opt into Workload Identity (default installs use static storage keys)."
}
func (c AzureCanAssignRoles) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	if skip, reason := azureCredentialsPrecondition(rc); skip {
		return skip, reason
	}
	if rc.AzureSubscriptionID == "" {
		return true, "Azure credentials unavailable; check azure.credentials first"
	}
	return false, ""
}
func (c AzureCanAssignRoles) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	out, err := azureRoleAssignmentFn(ctx, rc.AzureSubscriptionID)
	if err != nil {
		if strings.Contains(err.Error(), "AuthorizationFailed") || strings.Contains(err.Error(), "403") {
			return skip(c.ID(), c.Name(), "insufficient permissions to list role assignments; cannot verify")
		}
		return checkError(c.ID(), c.Name(), fmt.Sprintf("az role assignment list: %v", err))
	}
	var assignments []roleAssignment
	if err := json.Unmarshal(out, &assignments); err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("parse role assignments: %v", err))
	}
	if !hasRoleAssignmentPermission(assignments) {
		return fail(c.ID(), c.Name(), doctor.SeverityWarning,
			"principal lacks User Access Administrator or Owner (optional for default static-key installs)",
			"Default wasctl Azure installs use objectStorage.auth.mode=static and only need Contributor.\n"+
				"UAA/Owner is required only if you switch to Workload Identity and create\n"+
				"Storage Blob Data Contributor role assignments on the resource-manager UAMI.\n"+
				"See: README.md (Prerequisites) and docs/Install.md")
	}
	return pass(c.ID(), c.Name(), "User Access Administrator or Owner role found (Workload Identity optional path available)")
}
