package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

func azureQuotaRC() *doctor.RunContext {
	rc := azureRC()
	rc.Cfg.AzureLocation = config.Field[string]{Value: "eastus"}
	return rc
}

func TestAzureQuotaCores_Pass(t *testing.T) {
	orig := azureVMUsageFn
	defer func() { azureVMUsageFn = orig }()
	azureVMUsageFn = func(_ context.Context, _ string) ([]byte, error) {
		return []byte(`[{"name":{"value":"Total Regional vCPUs","localizedValue":"Total Regional vCPUs"},"currentValue":10,"limit":96}]`), nil
	}

	f := AzureQuotaCores{}.Run(context.Background(), azureQuotaRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAzureQuotaCores_Fail_Insufficient(t *testing.T) {
	orig := azureVMUsageFn
	defer func() { azureVMUsageFn = orig }()
	azureVMUsageFn = func(_ context.Context, _ string) ([]byte, error) {
		return []byte(`[{"name":{"value":"Total Regional vCPUs","localizedValue":"Total Regional vCPUs"},"currentValue":90,"limit":96}]`), nil
	}

	f := AzureQuotaCores{}.Run(context.Background(), azureQuotaRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail with only 6 available, got %v: %s", f.Status, f.Message)
	}
}

func TestAzureQuotaCores_Skip_AuthFailed(t *testing.T) {
	orig := azureVMUsageFn
	defer func() { azureVMUsageFn = orig }()
	azureVMUsageFn = func(_ context.Context, _ string) ([]byte, error) {
		return nil, errors.New("AuthorizationFailed: insufficient permissions")
	}

	f := AzureQuotaCores{}.Run(context.Background(), azureQuotaRC())
	if f.Status != doctor.StatusSkip {
		t.Fatalf("expected skip on auth failure, got %v", f.Status)
	}
}

func TestAzureQuotaCores_Error_ParseFail(t *testing.T) {
	orig := azureVMUsageFn
	defer func() { azureVMUsageFn = orig }()
	azureVMUsageFn = func(_ context.Context, _ string) ([]byte, error) {
		return []byte(`not json`), nil
	}

	f := AzureQuotaCores{}.Run(context.Background(), azureQuotaRC())
	if f.Status != doctor.StatusError {
		t.Fatalf("expected error on parse fail, got %v", f.Status)
	}
}

func TestAzureQuotaPublicIPs_Pass(t *testing.T) {
	orig := azureNetworkUsageFn
	defer func() { azureNetworkUsageFn = orig }()
	azureNetworkUsageFn = func(_ context.Context, _ string) ([]byte, error) {
		return []byte(`[{"name":{"value":"PublicIPAddresses","localizedValue":"Public IP Addresses"},"currentValue":2,"limit":20}]`), nil
	}

	f := AzureQuotaPublicIPs{}.Run(context.Background(), azureQuotaRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAzureQuotaPublicIPs_Fail_Insufficient(t *testing.T) {
	orig := azureNetworkUsageFn
	defer func() { azureNetworkUsageFn = orig }()
	azureNetworkUsageFn = func(_ context.Context, _ string) ([]byte, error) {
		return []byte(`[{"name":{"value":"PublicIPAddresses","localizedValue":"Public IP Addresses"},"currentValue":19,"limit":20}]`), nil
	}

	f := AzureQuotaPublicIPs{}.Run(context.Background(), azureQuotaRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail with only 1 available, got %v", f.Status)
	}
}

func TestAzureQuotaAKSClusters_Pass(t *testing.T) {
	orig := azureAKSListFn
	defer func() { azureAKSListFn = orig }()
	azureAKSListFn = func(_ context.Context, _ string) ([]byte, error) {
		return []byte(`[{"name":"cluster1"},{"name":"cluster2"}]`), nil
	}

	f := AzureQuotaAKSClusters{}.Run(context.Background(), azureQuotaRC())
	if f.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %v: %s", f.Status, f.Message)
	}
}

func TestAzureQuotaAKSClusters_Fail_NearLimit(t *testing.T) {
	orig := azureAKSListFn
	defer func() { azureAKSListFn = orig }()
	// Build 49 clusters (limit-1 = at risk)
	clusters := make([]byte, 0, 512)
	clusters = append(clusters, '[')
	for i := 0; i < 49; i++ {
		if i > 0 {
			clusters = append(clusters, ',')
		}
		clusters = append(clusters, []byte(`{"name":"c"}`)...)
	}
	clusters = append(clusters, ']')
	azureAKSListFn = func(_ context.Context, _ string) ([]byte, error) {
		return clusters, nil
	}

	f := AzureQuotaAKSClusters{}.Run(context.Background(), azureQuotaRC())
	if f.Status != doctor.StatusFail {
		t.Fatalf("expected fail near limit, got %v", f.Status)
	}
}

func TestAzureQuotaAKSClusters_Skip_AuthFailed(t *testing.T) {
	orig := azureAKSListFn
	defer func() { azureAKSListFn = orig }()
	azureAKSListFn = func(_ context.Context, _ string) ([]byte, error) {
		return nil, errors.New("AuthorizationFailed: cannot list AKS")
	}

	f := AzureQuotaAKSClusters{}.Run(context.Background(), azureQuotaRC())
	if f.Status != doctor.StatusSkip {
		t.Fatalf("expected skip on auth failure, got %v", f.Status)
	}
}

func TestAzureQuotaChecks_Skip_NotAzure(t *testing.T) {
	rc := &doctor.RunContext{Cfg: testCfg()}
	for _, check := range []doctor.Check{AzureQuotaCores{}, AzureQuotaPublicIPs{}, AzureQuotaAKSClusters{}} {
		skip, _ := check.Precondition(context.Background(), rc)
		if !skip {
			t.Errorf("%s: expected skip for non-Azure cloud", check.ID())
		}
	}
}
