package addons

import (
	"context"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
)

func TestAzureDNSLabel(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"wasctl", "wasctl"},
		{"WAS-Cluster", "was-cluster"},
		{"123bad", "was-123bad"},
		{"", "was"},
		{"my_cluster.name", "myclustername"},
	}
	for _, tc := range cases {
		if got := AzureDNSLabel(tc.in); got != tc.want {
			t.Errorf("AzureDNSLabel(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestAzureCloudAppFQDN(t *testing.T) {
	got := AzureCloudAppFQDN("wasctl", "East US")
	want := "wasctl.eastus.cloudapp.azure.com"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestInstall_AzureHealthy_MissingDNSLabel_Upgrades(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("helm status ingress-nginx", helmStatusJSON("deployed"), nil)
	m.RegisterOutput("kubectl get svc ingress-nginx-controller", []byte(""), nil)
	m.Register("helm repo", nil)
	m.Register("helm upgrade", nil)
	m.Register("kubectl wait", nil)

	rc := testRC(m)
	rc.Cfg.Cloud = "azure"
	rc.Cfg.ClusterName = config.Field[string]{Value: "wasctl"}
	rc.Cfg.AzureLocation = config.Field[string]{Value: "eastus"}

	if err := NewIngressNginx().Install(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.CalledWith("helm upgrade --install") {
		t.Error("expected helm upgrade when DNS label missing")
	}
	if rc.AzureIngressFQDN != "wasctl.eastus.cloudapp.azure.com" {
		t.Errorf("AzureIngressFQDN=%q", rc.AzureIngressFQDN)
	}
	if rc.Cfg.IngressHost.Value != "wasctl.eastus.cloudapp.azure.com" {
		t.Errorf("IngressHost=%q", rc.Cfg.IngressHost.Value)
	}
}

func TestInstall_AzureHealthy_WithDNSLabel_Skips(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("helm status ingress-nginx", helmStatusJSON("deployed"), nil)
	m.RegisterOutput("kubectl get svc ingress-nginx-controller", []byte("wasctl"), nil)

	rc := testRC(m)
	rc.Cfg.Cloud = "azure"
	rc.Cfg.ClusterName = config.Field[string]{Value: "wasctl"}

	if err := NewIngressNginx().Install(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.CalledWith("helm upgrade") {
		t.Error("should not upgrade when DNS label already set")
	}
}
