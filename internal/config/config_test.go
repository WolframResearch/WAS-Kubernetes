package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/versions"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func loadWith(t *testing.T, file string, env map[string]string, flags map[string]string) *config.Config {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	cfg, err := config.Load("/repo", file, flags)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

// ── defaults ──────────────────────────────────────────────────────────────────

func TestDefaults(t *testing.T) {
	cfg := loadWith(t, "", nil, nil)

	check := func(label, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s: got %q, want %q", label, got, want)
		}
	}
	check("Region.Value", cfg.Region.Value, "us-east-1")
	check("Region.Source", cfg.Region.Source, "default")
	check("ClusterName.Value", cfg.ClusterName.Value, "was-prod")
	check("K8sVersion.Value", cfg.K8sVersion.Value, versions.AWSClusterK8sDefault)
	check("NodeType.Value", cfg.NodeType.Value, "c5.2xlarge")
	check("NodeMin.Value", cfg.NodeMin.Value, "2")
	check("NodeDesired.Value", cfg.NodeDesired.Value, "2")
	check("NodeDesired.Source", cfg.NodeDesired.Source, "default") // Source must track defaults
	check("NodeMax.Value", cfg.NodeMax.Value, "10")
	check("IngressHost.Value", cfg.IngressHost.Value, "")
	check("IngressHost.Source", cfg.IngressHost.Source, "unset")
	check("StateBucket.Value", cfg.StateBucket.Value, "")
	check("StateBucket.Source", cfg.StateBucket.Source, "derived")
}

func TestAzureCloudUsesAzureK8sDefault(t *testing.T) {
	cfg := loadWith(t, "", nil, map[string]string{"cloud": "azure"})
	if cfg.K8sVersion.Value != versions.AzureClusterK8sDefault {
		t.Errorf("K8sVersion: got %q, want %q", cfg.K8sVersion.Value, versions.AzureClusterK8sDefault)
	}
	if cfg.K8sVersion.Source != "default" {
		t.Errorf("Source: got %q, want default", cfg.K8sVersion.Source)
	}
}

func TestAzureCloudKeepsExplicitK8sVersion(t *testing.T) {
	cfg := loadWith(t, "", nil, map[string]string{
		"cloud":       "azure",
		"k8s-version": "1.34",
	})
	if cfg.K8sVersion.Value != "1.34" {
		t.Errorf("K8sVersion: got %q, want 1.34", cfg.K8sVersion.Value)
	}
}

// ── config file ───────────────────────────────────────────────────────────────

func TestConfigFile(t *testing.T) {
	cfg := loadWith(t, filepath.Join("testdata", "valid.conf"), nil, nil)

	if cfg.Region.Value != "eu-west-1" {
		t.Errorf("Region: got %q, want eu-west-1", cfg.Region.Value)
	}
	if cfg.Region.Source != "file:testdata/valid.conf" {
		t.Errorf("Region.Source: got %q", cfg.Region.Source)
	}
	if cfg.ClusterName.Value != "was-staging" {
		t.Errorf("ClusterName: got %q", cfg.ClusterName.Value)
	}
	if cfg.IngressHost.Value != "was.example.com" {
		t.Errorf("IngressHost: got %q", cfg.IngressHost.Value)
	}
	if cfg.NodeMin.Value != "1" {
		t.Errorf("NodeMin: got %q, want 1", cfg.NodeMin.Value)
	}
	if cfg.NodeDesired.Value != "2" {
		t.Errorf("NodeDesired: got %q", cfg.NodeDesired.Value)
	}
	if cfg.NodeMax.Value != "5" {
		t.Errorf("NodeMax: got %q", cfg.NodeMax.Value)
	}
}

func TestConfigFileMissing(t *testing.T) {
	// A missing config file is silently ignored (it is optional).
	cfg := loadWith(t, "/nonexistent/wasctl.conf", nil, nil)
	if cfg.Region.Value != "us-east-1" {
		t.Errorf("expected default region when file missing, got %q", cfg.Region.Value)
	}
}

func TestConfigFileInvalid(t *testing.T) {
	tmp := t.TempDir()
	badFile := filepath.Join(tmp, "bad.conf")
	// Write a file that is not readable.
	if err := os.WriteFile(badFile, []byte("WAS_REGION=eu-west-1\n"), 0000); err != nil {
		t.Fatal(err)
	}
	if os.Getuid() == 0 {
		t.Skip("running as root; permission test skipped")
	}
	_, err := config.Load("/repo", badFile, nil)
	if err == nil {
		t.Error("expected error for unreadable file, got nil")
	}
}

// ── environment variables ─────────────────────────────────────────────────────

func TestEnvVarsOverrideDefaults(t *testing.T) {
	cfg := loadWith(t, "", map[string]string{
		"WAS_REGION":       "ap-southeast-1",
		"WAS_CLUSTER_NAME": "was-test",
		"WAS_NODE_DESIRED": "4",
	}, nil)

	if cfg.Region.Value != "ap-southeast-1" {
		t.Errorf("Region: got %q", cfg.Region.Value)
	}
	if cfg.Region.Source != "env:WAS_REGION" {
		t.Errorf("Region.Source: got %q", cfg.Region.Source)
	}
	if cfg.ClusterName.Value != "was-test" {
		t.Errorf("ClusterName: got %q", cfg.ClusterName.Value)
	}
	// NodeDesired.Source must be set when the env var is present.
	if cfg.NodeDesired.Value != "4" {
		t.Errorf("NodeDesired: got %q", cfg.NodeDesired.Value)
	}
	if cfg.NodeDesired.Source != "env:WAS_NODE_DESIRED" {
		t.Errorf("NodeDesired.Source: got %q, want env:WAS_NODE_DESIRED", cfg.NodeDesired.Source)
	}
}

func TestEnvVarsOverrideConfigFile(t *testing.T) {
	cfg := loadWith(t, filepath.Join("testdata", "valid.conf"),
		map[string]string{"WAS_REGION": "us-west-2"},
		nil)

	// Env var must beat config file.
	if cfg.Region.Value != "us-west-2" {
		t.Errorf("Region: got %q, want us-west-2", cfg.Region.Value)
	}
	if cfg.Region.Source != "env:WAS_REGION" {
		t.Errorf("Region.Source: got %q", cfg.Region.Source)
	}
	// Config file value still wins for other fields.
	if cfg.ClusterName.Value != "was-staging" {
		t.Errorf("ClusterName: got %q, want was-staging", cfg.ClusterName.Value)
	}
}

// ── CLI flags ─────────────────────────────────────────────────────────────────

func TestFlagsOverrideEverything(t *testing.T) {
	cfg := loadWith(t,
		filepath.Join("testdata", "valid.conf"),
		map[string]string{"WAS_REGION": "us-west-2"},
		map[string]string{"region": "sa-east-1"},
	)

	// Flag must beat env var AND config file.
	if cfg.Region.Value != "sa-east-1" {
		t.Errorf("Region: got %q, want sa-east-1", cfg.Region.Value)
	}
	if cfg.Region.Source != "flag:--region" {
		t.Errorf("Region.Source: got %q", cfg.Region.Source)
	}
}

func TestFlagsNodeDesiredSource(t *testing.T) {
	cfg := loadWith(t, "", nil, map[string]string{"node-desired": "6"})
	if cfg.NodeDesired.Value != "6" {
		t.Errorf("NodeDesired: got %q", cfg.NodeDesired.Value)
	}
	if cfg.NodeDesired.Source != "flag:--node-desired" {
		t.Errorf("NodeDesired.Source: got %q", cfg.NodeDesired.Source)
	}
}

// ── full precedence chain ─────────────────────────────────────────────────────

func TestFullPrecedenceChain(t *testing.T) {
	// default < file < env < flag  — each layer independently verified
	cfg := loadWith(t,
		filepath.Join("testdata", "valid.conf"),    // file: eu-west-1
		map[string]string{"WAS_REGION": "us-west-2"}, // env wins over file
		map[string]string{"region": "ca-central-1"},  // flag wins over env
	)
	if cfg.Region.Value != "ca-central-1" {
		t.Errorf("precedence: got %q, want ca-central-1", cfg.Region.Value)
	}
	if cfg.Region.Source != "flag:--region" {
		t.Errorf("precedence source: got %q", cfg.Region.Source)
	}
}

// ── DeriveBucketNames ─────────────────────────────────────────────────────────

func TestDeriveBucketNames(t *testing.T) {
	cfg := loadWith(t, "", nil, nil) // defaults: cluster=was-prod
	cfg.DeriveBucketNames("123456789012")

	wantBucket := "wolfram-was-tfstate-was-prod-123456789012"
	if cfg.StateBucket.Value != wantBucket {
		t.Errorf("StateBucket: got %q, want %q", cfg.StateBucket.Value, wantBucket)
	}
	wantTable := "wolfram-was-tfstate-lock-was-prod"
	if cfg.LockTable.Value != wantTable {
		t.Errorf("LockTable: got %q, want %q", cfg.LockTable.Value, wantTable)
	}
	if cfg.StateBucket.Source != "derived" {
		t.Errorf("StateBucket.Source: got %q", cfg.StateBucket.Source)
	}
}

func TestDeriveBucketNamesDoesNotOverrideExplicit(t *testing.T) {
	cfg := loadWith(t, "", nil, map[string]string{
		"state-bucket": "my-custom-bucket",
		"lock-table":   "my-custom-table",
	})
	cfg.DeriveBucketNames("123456789012")

	if cfg.StateBucket.Value != "my-custom-bucket" {
		t.Errorf("explicit bucket overridden: got %q", cfg.StateBucket.Value)
	}
	if cfg.LockTable.Value != "my-custom-table" {
		t.Errorf("explicit table overridden: got %q", cfg.LockTable.Value)
	}
	if cfg.StateBucket.Source != "flag:--state-bucket" {
		t.Errorf("source corrupted: got %q", cfg.StateBucket.Source)
	}
}

// ── Show / accessor coverage ─────────────────────────────────────────────────

func TestShowDoesNotPanic(t *testing.T) {
	cfg := loadWith(t, "", nil, nil)
	// Show() writes to stdout; we just verify it doesn't panic.
	cfg.Show()
}

func TestStateBucketNameAndLockTableName(t *testing.T) {
	cfg := loadWith(t, "", nil, map[string]string{
		"state-bucket": "test-bucket",
		"lock-table":   "test-table",
	})
	if cfg.StateBucketName() != "test-bucket" {
		t.Errorf("StateBucketName: %q", cfg.StateBucketName())
	}
	if cfg.LockTableName() != "test-table" {
		t.Errorf("LockTableName: %q", cfg.LockTableName())
	}
}

func TestFieldStringMethod(t *testing.T) {
	cfg := loadWith(t, "", nil, nil)
	// Verify Field.String() returns a non-empty string for a string field.
	s := cfg.Region.String()
	if s != "us-east-1" {
		t.Errorf("Field.String(): got %q, want us-east-1", s)
	}
}

func TestAllKVMappings(t *testing.T) {
	cfg := loadWith(t, "", nil, map[string]string{
		"cluster-name":  "test-cluster",
		"k8s-version":   "1.30",
		"ingress-host":  "was.test.com",
		"node-type":     "t3.medium",
		"node-min":      "1",
		"node-max":      "5",
		"node-desired":  "3",
		"state-bucket":  "bucket1",
		"lock-table":    "table1",
		"skip":          "cert-manager",
	})
	if cfg.ClusterName.Value != "test-cluster" {
		t.Errorf("ClusterName: %q", cfg.ClusterName.Value)
	}
	if cfg.K8sVersion.Value != "1.30" {
		t.Errorf("K8sVersion: %q", cfg.K8sVersion.Value)
	}
	if cfg.IngressHost.Value != "was.test.com" {
		t.Errorf("IngressHost: %q", cfg.IngressHost.Value)
	}
	if cfg.NodeType.Value != "t3.medium" {
		t.Errorf("NodeType: %q", cfg.NodeType.Value)
	}
	if cfg.NodeMin.Value != "1" {
		t.Errorf("NodeMin: %q", cfg.NodeMin.Value)
	}
	if cfg.NodeMax.Value != "5" {
		t.Errorf("NodeMax: %q", cfg.NodeMax.Value)
	}
	if cfg.AddonsSkip.Value != "cert-manager" {
		t.Errorf("AddonsSkip: %q", cfg.AddonsSkip.Value)
	}
}

func TestShowWithIngressHostAndSkip(t *testing.T) {
	cfg := loadWith(t, "", nil, map[string]string{
		"ingress-host": "was.example.com",
		"skip":         "cert-manager",
	})
	// Should not panic with AddonsSkip set (triggers the conditional row in Show).
	cfg.Show()
}

// ── config file comment/blank handling ───────────────────────────────────────

func TestConfigFileCommentsAndBlanks(t *testing.T) {
	tmp := t.TempDir()
	content := `# This is a comment
WAS_REGION=ap-northeast-1

# Another comment
WAS_CLUSTER_NAME=was-tokyo
`
	path := filepath.Join(tmp, "wasctl.conf")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := loadWith(t, path, nil, nil)
	if cfg.Region.Value != "ap-northeast-1" {
		t.Errorf("Region: got %q", cfg.Region.Value)
	}
	if cfg.ClusterName.Value != "was-tokyo" {
		t.Errorf("ClusterName: got %q", cfg.ClusterName.Value)
	}
}
