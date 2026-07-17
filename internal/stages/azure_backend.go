package stages

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
)

var (
	backendTFVarsStorageAccountRe = regexp.MustCompile(`(?m)^\s*storage_account_name\s*=\s*"([^"]+)"`)
	backendTFVarsContainerRe      = regexp.MustCompile(`(?m)^\s*container_name\s*=\s*"([^"]+)"`)
	backendTFVarsKeyRe            = regexp.MustCompile(`(?m)^\s*key\s*=\s*"([^"]+)"`)
	backendTFVarsAccessKeyRe      = regexp.MustCompile(`(?m)^\s*access_key\s*=\s*"([^"]+)"`)
	backendConfigFlagRe           = regexp.MustCompile(`^-backend-config=(.+)$`)
)

// parseBackendTFVarsStorageAccount reads storage_account_name from a backend.tfvars file.
func parseBackendTFVarsStorageAccount(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read backend.tfvars: %w", err)
	}
	m := backendTFVarsStorageAccountRe.FindSubmatch(data)
	if len(m) < 2 {
		return "", fmt.Errorf("storage_account_name not found in %s", path)
	}
	return strings.TrimSpace(string(m[1])), nil
}

type azureBackendTFVars struct {
	StorageAccount string
	Container      string
	Key            string
	AccessKey      string
}

func parseAzureBackendTFVars(path string) (azureBackendTFVars, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return azureBackendTFVars{}, fmt.Errorf("read backend.tfvars: %w", err)
	}
	var out azureBackendTFVars
	if m := backendTFVarsStorageAccountRe.FindSubmatch(data); len(m) >= 2 {
		out.StorageAccount = strings.TrimSpace(string(m[1]))
	}
	if m := backendTFVarsContainerRe.FindSubmatch(data); len(m) >= 2 {
		out.Container = strings.TrimSpace(string(m[1]))
	}
	if m := backendTFVarsKeyRe.FindSubmatch(data); len(m) >= 2 {
		out.Key = strings.TrimSpace(string(m[1]))
	}
	if m := backendTFVarsAccessKeyRe.FindSubmatch(data); len(m) >= 2 {
		out.AccessKey = strings.TrimSpace(string(m[1]))
	}
	if out.StorageAccount == "" || out.Container == "" || out.Key == "" || out.AccessKey == "" {
		return azureBackendTFVars{}, fmt.Errorf("incomplete backend.tfvars at %s (need storage_account_name, container_name, key, access_key)", path)
	}
	return out, nil
}

func backendConfigPathFromCmd(cmd []string) string {
	for _, a := range cmd {
		if m := backendConfigFlagRe.FindStringSubmatch(a); len(m) == 2 {
			return m[1]
		}
	}
	return ""
}

func isTerraformInitCmd(cmd []string) bool {
	for _, a := range cmd {
		if a == "init" {
			return true
		}
	}
	return false
}

// isAzureStateBlobLocked reports a Terraform Azure backend lease conflict.
// These often appear after a DNS/network timeout mid-lock ("terraformlockid was empty").
func isAzureStateBlobLocked(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already locked") ||
		strings.Contains(msg, `blob metadata "terraformlockid" was empty`) ||
		strings.Contains(msg, "blob metadata \"terraformlockid\" was empty")
}

// breakAzureTFStateLease breaks a stuck azurerm backend blob lease so terraform
// init can proceed. Uses az CLI + access_key from backend.tfvars (Contributor-capable).
var breakAzureTFStateLease = func(ctx context.Context, backendPath string) error {
	cfg, err := parseAzureBackendTFVars(backendPath)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "az", "storage", "blob", "lease", "break",
		"--account-name", cfg.StorageAccount,
		"--container-name", cfg.Container,
		"--blob-name", cfg.Key,
		"--account-key", cfg.AccessKey,
		"--output", "none",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.ToLower(string(out) + err.Error())
		// No blob / no lease yet — nothing to break.
		if strings.Contains(msg, "blobnotfound") ||
			strings.Contains(msg, "blobnotfounderror") ||
			strings.Contains(msg, "there is currently no lease") ||
			strings.Contains(msg, "leasenotpresent") {
			return nil
		}
		return fmt.Errorf("az storage blob lease break: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func waitForAzureStateBlobDNS(ctx context.Context, storageAccount string, rep runner.Reporter) error {
	return tools.WaitForAzureBlobDNS(ctx, storageAccount, rep.LogLine)
}
