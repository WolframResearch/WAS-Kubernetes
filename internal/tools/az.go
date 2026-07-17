package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// AZGetKubeconfig returns the command to generate an isolated admin kubeconfig
// for an AKS cluster. --admin uses the local cluster-admin client certificate
// (bypasses Azure AD Kubernetes RBAC). The resulting context is named
// "<clusterName>-admin". kubeconfigPath must be a temp file (never ~/.kube/config).
func AZGetKubeconfig(resourceGroup, clusterName, kubeconfigPath string) []string {
	return []string{
		"az", "aks", "get-credentials",
		"--admin",
		"--resource-group", resourceGroup,
		"--name", clusterName,
		"--file", kubeconfigPath,
		"--overwrite-existing",
	}
}

// AccountInfo holds the fields returned by az account show.
type AccountInfo struct {
	ID             string `json:"id"`             // subscription ID
	Name           string `json:"name"`           // subscription display name
	TenantID       string `json:"tenantId"`
	IsDefault      bool   `json:"isDefault"`
	HomeTenantID   string `json:"homeTenantId"`
}

// GetAccountInfo calls az account show and returns the parsed result.
// It is used for preflight identity checks (Azure analog of aws sts get-caller-identity).
func GetAccountInfo(ctx context.Context) (AccountInfo, error) {
	args := []string{"az", "account", "show", "--output", "json"}
	out, err := exec.CommandContext(ctx, args[0], args[1:]...).Output() //nolint:gosec
	if err != nil {
		return AccountInfo{}, fmt.Errorf("az account show: %w\nHint: run 'az login' to authenticate", err)
	}
	var info AccountInfo
	if err := json.Unmarshal(bytes.TrimSpace(out), &info); err != nil {
		return AccountInfo{}, fmt.Errorf("parse az account show: %w", err)
	}
	return info, nil
}
