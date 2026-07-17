package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
)

// azureRC returns a RunContext configured for Azure.
func azureRC() *doctor.RunContext {
	rc := &doctor.RunContext{Cfg: testCfg()}
	rc.Cfg.Cloud = "azure"
	rc.AzureSubscriptionID = "sub-abc-123"
	return rc
}

// azureAccountInfo returns a stub AccountInfo for tests.
func stubAccountInfo() tools.AccountInfo {
	return tools.AccountInfo{
		ID:        "sub-abc-123",
		Name:      "My Subscription",
		TenantID:  "tenant-xyz",
		IsDefault: true,
	}
}

func TestAzureCredentials_Pass(t *testing.T) {
	orig := getAccountInfoFn
	defer func() { getAccountInfoFn = orig }()
	getAccountInfoFn = func(_ context.Context) (tools.AccountInfo, error) {
		return stubAccountInfo(), nil
	}

	f := AzureCredentials{}.Run(context.Background(), azureRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAzureCredentials_Fail_NotLoggedIn(t *testing.T) {
	orig := getAccountInfoFn
	defer func() { getAccountInfoFn = orig }()
	getAccountInfoFn = func(_ context.Context) (tools.AccountInfo, error) {
		return tools.AccountInfo{}, errors.New("Please run 'az login' to setup account")
	}

	f := AzureCredentials{}.Run(context.Background(), azureRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail, got %v", f.Status)
	}
	if f.Severity != doctor.SeverityCritical {
		t.Fatalf("expected critical, got %v", f.Severity)
	}
}

func TestAzureCredentials_Fail_TokenExpired(t *testing.T) {
	orig := getAccountInfoFn
	defer func() { getAccountInfoFn = orig }()
	getAccountInfoFn = func(_ context.Context) (tools.AccountInfo, error) {
		return tools.AccountInfo{}, errors.New("AADSTS70043: The refresh token has expired")
	}

	f := AzureCredentials{}.Run(context.Background(), azureRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail, got %v", f.Status)
	}
}

func TestAzureCredentials_Skip_NotAzure(t *testing.T) {
	rc := &doctor.RunContext{Cfg: testCfg()} // cloud = "" (defaults to aws)
	skip, _ := AzureCredentials{}.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected skip for non-Azure cloud")
	}
}

func TestAzureSubscriptionAccess_Pass(t *testing.T) {
	orig := getAccountInfoFn
	defer func() { getAccountInfoFn = orig }()
	getAccountInfoFn = func(_ context.Context) (tools.AccountInfo, error) {
		return stubAccountInfo(), nil
	}

	f := AzureSubscriptionAccess{}.Run(context.Background(), azureRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAzureSubscriptionAccess_Warning_NotDefault(t *testing.T) {
	orig := getAccountInfoFn
	defer func() { getAccountInfoFn = orig }()
	getAccountInfoFn = func(_ context.Context) (tools.AccountInfo, error) {
		info := stubAccountInfo()
		info.IsDefault = false
		return info, nil
	}

	f := AzureSubscriptionAccess{}.Run(context.Background(), azureRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail for non-default subscription, got %v", f.Status)
	}
	if f.Severity != doctor.SeverityWarning {
		t.Fatalf("expected warning severity, got %v", f.Severity)
	}
}

func TestAzureSubscriptionAccess_Skip_NoCreds(t *testing.T) {
	rc := azureRC()
	rc.AzureSubscriptionID = "" // no credentials
	skip, _ := AzureSubscriptionAccess{}.Precondition(context.Background(), rc)
	if !skip {
		t.Fatal("expected skip when no subscription ID")
	}
}
