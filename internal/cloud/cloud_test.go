package cloud_test

import (
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/cloud"
	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
)

func TestForNameReturnsAWS(t *testing.T) {
	c := cloud.ForName("aws")
	if c.Name() != "aws" {
		t.Errorf("expected 'aws', got %q", c.Name())
	}
}

func TestForNameFallbackToAWS(t *testing.T) {
	c := cloud.ForName("unknown")
	if c.Name() != "aws" {
		t.Errorf("unknown name should default to aws, got %q", c.Name())
	}
}

func TestForNameAzure(t *testing.T) {
	c := cloud.ForName("azure")
	if c.Name() != "azure" {
		t.Errorf("expected 'azure', got %q", c.Name())
	}
}

func TestAWSCloudStorageClass(t *testing.T) {
	c := cloud.AWSCloud{}
	if c.StorageClassName() != "was-efs" {
		t.Errorf("AWS storage class: got %q", c.StorageClassName())
	}
}

func TestAWSCloudHelmValuesFile(t *testing.T) {
	c := cloud.AWSCloud{}
	if c.HelmValuesFile() == "" {
		t.Error("HelmValuesFile should not be empty")
	}
}

func TestAWSCloudHelmExtraSets(t *testing.T) {
	cfg, _ := config.Load("/repo", "", nil)
	cfg.IngressHost = config.Field[string]{Value: "was.example.com", Source: "test"}
	c := cloud.AWSCloud{}
	sets := c.HelmExtraSets(cfg)
	if sets["ingress.host"] != "was.example.com" {
		t.Errorf("ingress.host set: got %q", sets["ingress.host"])
	}
}

func TestAzureCloudStorageClass(t *testing.T) {
	c := cloud.AzureCloud{}
	if c.StorageClassName() != "azurefile" {
		t.Errorf("Azure storage class: got %q", c.StorageClassName())
	}
}

func TestAzureCloudHelmExtraSetsNil(t *testing.T) {
	c := cloud.AzureCloud{}
	if sets := c.HelmExtraSets(nil); sets != nil {
		t.Errorf("Azure HelmExtraSets should return nil: %v", sets)
	}
}
