package stages

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
)

// emptyAzureBlobContainer deletes all current blobs in a container via az CLI.
// Soft-deleted / versioned leftovers are removed when the storage account is
// destroyed. Missing container/account is treated as success.
func emptyAzureBlobContainer(ctx context.Context, accountName, containerName, accountKey string, rep runner.Reporter) error {
	if accountName == "" || containerName == "" || accountKey == "" {
		return fmt.Errorf("emptyAzureBlobContainer: account, container, and key are required")
	}
	rep.LogLine(fmt.Sprintf("[cleanup] emptying Azure blob container %s/%s...", accountName, containerName))

	cmd := exec.CommandContext(ctx, "az", "storage", "blob", "delete-batch",
		"--account-name", accountName,
		"--source", containerName,
		"--account-key", accountKey,
		"--delete-snapshots", "include",
		"--output", "none",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.ToLower(string(out) + err.Error())
		if strings.Contains(msg, "containernotfound") ||
			strings.Contains(msg, "accountnotfound") ||
			strings.Contains(msg, "resourcenotfound") ||
			strings.Contains(msg, "the specified container does not exist") ||
			strings.Contains(msg, "cannot find") {
			rep.LogLine(fmt.Sprintf("[cleanup] container %s/%s already gone", accountName, containerName))
			return nil
		}
		return fmt.Errorf("az storage blob delete-batch %s/%s: %w (%s)",
			accountName, containerName, err, strings.TrimSpace(string(out)))
	}
	rep.LogLine(fmt.Sprintf("[cleanup] emptied Azure blob container %s/%s", accountName, containerName))
	return nil
}
