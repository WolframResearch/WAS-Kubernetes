package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

func TestAzureCanCreateResourceGroup_Pass(t *testing.T) {
	orig := azureRoleAssignmentFn
	defer func() { azureRoleAssignmentFn = orig }()
	azureRoleAssignmentFn = func(_ context.Context, _ string) ([]byte, error) {
		return []byte(`[{"roleDefinitionName":"Contributor","scope":"/subscriptions/sub-abc-123"}]`), nil
	}

	f := AzureCanCreateResourceGroup{}.Run(context.Background(), azureRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAzureCanCreateResourceGroup_Pass_Owner(t *testing.T) {
	orig := azureRoleAssignmentFn
	defer func() { azureRoleAssignmentFn = orig }()
	azureRoleAssignmentFn = func(_ context.Context, _ string) ([]byte, error) {
		return []byte(`[{"roleDefinitionName":"Owner","scope":"/subscriptions/sub-abc-123"}]`), nil
	}

	f := AzureCanCreateResourceGroup{}.Run(context.Background(), azureRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass for Owner role, got %v: %s", f.Status, f.Message)
	}
}

func TestAzureCanCreateResourceGroup_Fail_NoRole(t *testing.T) {
	orig := azureRoleAssignmentFn
	defer func() { azureRoleAssignmentFn = orig }()
	azureRoleAssignmentFn = func(_ context.Context, _ string) ([]byte, error) {
		return []byte(`[{"roleDefinitionName":"Reader","scope":"/subscriptions/sub-abc-123"}]`), nil
	}

	f := AzureCanCreateResourceGroup{}.Run(context.Background(), azureRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail without Contributor+, got %v: %s", f.Status, f.Message)
	}
}

func TestAzureCanCreateResourceGroup_Skip_AuthorizationFailed(t *testing.T) {
	orig := azureRoleAssignmentFn
	defer func() { azureRoleAssignmentFn = orig }()
	azureRoleAssignmentFn = func(_ context.Context, _ string) ([]byte, error) {
		return nil, errors.New("AuthorizationFailed listing roles")
	}

	f := AzureCanCreateResourceGroup{}.Run(context.Background(), azureRC())
	if f.Status != doctor.StatusSkip {
		t.Fatalf("expected skip on AuthorizationFailed, got %v", f.Status)
	}
}

func TestAzureCanCreateResourceGroup_Error_ParseFail(t *testing.T) {
	orig := azureRoleAssignmentFn
	defer func() { azureRoleAssignmentFn = orig }()
	azureRoleAssignmentFn = func(_ context.Context, _ string) ([]byte, error) {
		return []byte(`not json`), nil
	}

	f := AzureCanCreateResourceGroup{}.Run(context.Background(), azureRC())
	if f.Status != doctor.StatusError {
		t.Fatalf("expected error on bad JSON, got %v", f.Status)
	}
}

func TestAzureCanAssignRoles_Pass(t *testing.T) {
	orig := azureRoleAssignmentFn
	defer func() { azureRoleAssignmentFn = orig }()
	azureRoleAssignmentFn = func(_ context.Context, _ string) ([]byte, error) {
		return []byte(`[{"roleDefinitionName":"User Access Administrator","scope":"/subscriptions/sub-abc-123"}]`), nil
	}

	f := AzureCanAssignRoles{}.Run(context.Background(), azureRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAzureCanAssignRoles_Fail_NoRole(t *testing.T) {
	orig := azureRoleAssignmentFn
	defer func() { azureRoleAssignmentFn = orig }()
	azureRoleAssignmentFn = func(_ context.Context, _ string) ([]byte, error) {
		return []byte(`[{"roleDefinitionName":"Contributor","scope":"/subscriptions/sub-abc-123"}]`), nil
	}

	f := AzureCanAssignRoles{}.Run(context.Background(), azureRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail without UAA/Owner, got %v", f.Status)
	}
	if f.Severity != doctor.SeverityWarning {
		t.Fatalf("expected warning severity for advisory UAA check, got %v", f.Severity)
	}
}

func TestAzureCanAssignRoles_Skip_NotAzure(t *testing.T) {
	rc := &doctor.RunContext{Cfg: testCfg()}
	skip, _ := AzureCanAssignRoles{}.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected skip for non-Azure cloud")
	}
}
