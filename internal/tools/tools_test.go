package tools_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
)

const (
	testKubeconfig = "/tmp/test-kubeconfig"
	testContext    = "arn:aws:eks:us-east-1:123456789012:cluster/test"
)

// ── TerraformInit ─────────────────────────────────────────────────────────────

func TestTerraformInitNoBackend(t *testing.T) {
	got := tools.TerraformInit("infra/aws/bootstrap", "")
	want := []string{"terraform", "-chdir=infra/aws/bootstrap", "init"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTerraformInitWithBackend(t *testing.T) {
	got := tools.TerraformInit("infra/aws/stack", "/abs/backend.hcl")
	if got[len(got)-1] != "-backend-config=/abs/backend.hcl" {
		t.Errorf("missing backend-config flag: %v", got)
	}
	hasReconfigure := false
	for _, a := range got {
		if a == "-reconfigure" {
			hasReconfigure = true
		}
	}
	if !hasReconfigure {
		t.Errorf("-reconfigure missing from %v", got)
	}
}

// ── TerraformApply ────────────────────────────────────────────────────────────

func TestTerraformApplyContainsAutoApprove(t *testing.T) {
	cmd := tools.TerraformApply("infra/aws/stack", map[string]string{
		"aws_region": "us-east-1",
	})
	found := false
	for _, a := range cmd {
		if a == "-auto-approve" {
			found = true
		}
	}
	if !found {
		t.Errorf("-auto-approve missing from %v", cmd)
	}
	foundVar := false
	for _, a := range cmd {
		if a == "-var=aws_region=us-east-1" {
			foundVar = true
		}
	}
	if !foundVar {
		t.Errorf("-var=aws_region=us-east-1 not in %v", cmd)
	}
}

// ── HelmUpgradeInstall ────────────────────────────────────────────────────────

func TestHelmUpgradeInstall(t *testing.T) {
	cmd := tools.HelmUpgradeInstall(
		"was",
		"charts/wolfram-application-server",
		"charts/wolfram-application-server/values-aws.yaml",
		"was",
		testKubeconfig,
		testContext,
		map[string]string{"ingress.host": "was.example.com"},
	)

	for _, s := range []string{"helm", "upgrade", "--install", "was", "--kubeconfig", testKubeconfig, "--kube-context", testContext} {
		found := false
		for _, a := range cmd {
			if a == s {
				found = true
			}
		}
		if !found {
			t.Errorf("token %q missing from %v", s, cmd)
		}
	}

	foundSet := false
	for i, a := range cmd {
		if a == "--set-string" && i+1 < len(cmd) && cmd[i+1] == "ingress.host=was.example.com" {
			foundSet = true
		}
	}
	if !foundSet {
		t.Errorf("--set-string ingress.host=was.example.com not found in %v", cmd)
	}
}

// ── HelmDepUpdate ─────────────────────────────────────────────────────────────

func TestHelmDepUpdate(t *testing.T) {
	cmd := tools.HelmDepUpdate("charts/wolfram-application-server", testKubeconfig, testContext)
	for _, s := range []string{"helm", "--kubeconfig", testKubeconfig, "--kube-context", testContext, "dependency", "update"} {
		found := false
		for _, a := range cmd {
			if a == s {
				found = true
			}
		}
		if !found {
			t.Errorf("token %q missing from %v", s, cmd)
		}
	}
}

func TestHelmStatus(t *testing.T) {
	cmd := tools.HelmStatus("was", "was", testKubeconfig, testContext)
	for _, s := range []string{"helm", "--kubeconfig", testKubeconfig, "--kube-context", testContext, "status", "was", "-n", "was"} {
		found := false
		for _, a := range cmd {
			if a == s {
				found = true
			}
		}
		if !found {
			t.Errorf("token %q missing from %v", s, cmd)
		}
	}
}

func TestHelmUninstall(t *testing.T) {
	cmd := tools.HelmUninstall("was", "was", testKubeconfig, testContext)
	for _, s := range []string{"helm", "--kubeconfig", testKubeconfig, "--kube-context", testContext, "uninstall", "was", "-n", "was"} {
		found := false
		for _, a := range cmd {
			if a == s {
				found = true
			}
		}
		if !found {
			t.Errorf("token %q missing from %v", s, cmd)
		}
	}
}

// ── KubectlDeletePVC ──────────────────────────────────────────────────────────

func TestKubectlDeletePVC(t *testing.T) {
	cmd := tools.KubectlDeletePVC("was", testKubeconfig, testContext, "awes-logs", "resources-logs", "endpoint-logs")
	for _, s := range []string{"kubectl", "--kubeconfig", testKubeconfig, "--context", testContext, "delete", "pvc", "-n", "was", "awes-logs"} {
		found := false
		for _, a := range cmd {
			if a == s {
				found = true
			}
		}
		if !found {
			t.Errorf("token %q missing from %v", s, cmd)
		}
	}
}

func TestKubectlDeletePVCByLabel(t *testing.T) {
	cmd := tools.KubectlDeletePVCByLabel("kafka", testKubeconfig, testContext, "strimzi.io/cluster=kafka-persistent")
	for _, s := range []string{"kubectl", "--kubeconfig", testKubeconfig, "--context", testContext, "delete", "pvc", "-l", "strimzi.io/cluster=kafka-persistent", "-n", "kafka"} {
		found := false
		for _, a := range cmd {
			if a == s {
				found = true
			}
		}
		if !found {
			t.Errorf("token %q missing from %v", s, cmd)
		}
	}
}

// ── KubectlGetStorageClass ────────────────────────────────────────────────────

func TestKubectlGetStorageClass(t *testing.T) {
	cmd := tools.KubectlGetStorageClass("was-efs", testKubeconfig, testContext)
	for _, s := range []string{"kubectl", "--kubeconfig", testKubeconfig, "--context", testContext, "get", "storageclass", "was-efs"} {
		found := false
		for _, a := range cmd {
			if a == s {
				found = true
			}
		}
		if !found {
			t.Errorf("token %q missing from %v", s, cmd)
		}
	}
}

// ── AWSUpdateKubeconfig ───────────────────────────────────────────────────────

func TestAWSUpdateKubeconfig(t *testing.T) {
	cmd := tools.AWSUpdateKubeconfig("us-east-1", "was-prod", testKubeconfig)
	for _, s := range []string{"aws", "eks", "update-kubeconfig", "--kubeconfig", testKubeconfig, "--region", "us-east-1", "--name", "was-prod"} {
		found := false
		for _, a := range cmd {
			if a == s {
				found = true
			}
		}
		if !found {
			t.Errorf("token %q missing from %v", s, cmd)
		}
	}
}

// ── TerraformDestroy ─────────────────────────────────────────────────────────

func TestTerraformDestroyContainsAutoApprove(t *testing.T) {
	cmd := tools.TerraformDestroy("infra/aws/stack", map[string]string{"aws_region": "us-east-1"})
	for _, kw := range []string{"-auto-approve", "destroy"} {
		found := false
		for _, a := range cmd {
			if a == kw {
				found = true
			}
		}
		if !found {
			t.Errorf("%q missing from %v", kw, cmd)
		}
	}
}

func TestTerraformDestroyTarget(t *testing.T) {
	cmd := tools.TerraformDestroyTarget("infra/aws/stack", "module.eks", map[string]string{"aws_region": "us-east-1"})
	for _, kw := range []string{"-auto-approve", "destroy", "-target=module.eks"} {
		found := false
		for _, a := range cmd {
			if a == kw {
				found = true
			}
		}
		if !found {
			t.Errorf("%q missing from %v", kw, cmd)
		}
	}
}

// ── TerraformOutput error path ────────────────────────────────────────────────

func TestTerraformOutputFailsForNonexistentDir(t *testing.T) {
	// Use a guaranteed-nonexistent path. terraform -chdir to a missing directory
	// exits non-zero regardless of terraform version or state.
	_, err := tools.TerraformOutput(t.Context(), "/tmp/__wasctl_test_nonexistent_dir_abc123", "some_key")
	if err == nil {
		t.Error("TerraformOutput should fail for a non-existent directory")
	}
}

// ── kubectl/helm commands must always carry --kubeconfig / --kube-context ─────

func TestKubectlCommandsCarryKubeconfig(t *testing.T) {
	cmds := [][]string{
		tools.KubectlDeletePVC("was", testKubeconfig, testContext, "pvc1"),
		tools.KubectlDeletePVCByLabel("was", testKubeconfig, testContext, "app=foo"),
		tools.KubectlGetStorageClass("gp2", testKubeconfig, testContext),
		tools.KubectlGet("pods", "was", testKubeconfig, testContext),
	}
	for _, cmd := range cmds {
		hasKC, hasCtx := false, false
		for i, a := range cmd {
			if a == "--kubeconfig" && i+1 < len(cmd) && cmd[i+1] == testKubeconfig {
				hasKC = true
			}
			if a == "--context" && i+1 < len(cmd) && cmd[i+1] == testContext {
				hasCtx = true
			}
		}
		if !hasKC || !hasCtx {
			t.Errorf("kubectl cmd missing --kubeconfig/--context: %v", cmd)
		}
	}
}

func TestHelmCommandsCarryKubeconfig(t *testing.T) {
	cmds := [][]string{
		tools.HelmDepUpdate("charts/was", testKubeconfig, testContext),
		tools.HelmUpgradeInstall("was", "charts/was", "values.yaml", "was", testKubeconfig, testContext, nil),
		tools.HelmStatus("was", "was", testKubeconfig, testContext),
		tools.HelmUninstall("was", "was", testKubeconfig, testContext),
		tools.HelmList(testKubeconfig, testContext),
		tools.HelmHistory("was", "was", testKubeconfig, testContext),
	}
	for _, cmd := range cmds {
		hasKC, hasCtx := false, false
		for i, a := range cmd {
			if a == "--kubeconfig" && i+1 < len(cmd) && cmd[i+1] == testKubeconfig {
				hasKC = true
			}
			if (a == "--kube-context" || a == "--context") && i+1 < len(cmd) && cmd[i+1] == testContext {
				hasCtx = true
			}
		}
		if !hasKC || !hasCtx {
			t.Errorf("helm cmd missing --kubeconfig/--kube-context: %v", strings.Join(cmd, " "))
		}
	}
}
