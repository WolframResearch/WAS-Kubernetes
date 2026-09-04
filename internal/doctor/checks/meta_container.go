package checks

import (
	"context"
	"fmt"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metacontainer"
)

// checkBlobContainerFn probes the meta blob container.
// Returns ("exists", nil), ("notfound", nil), ("denied", err), or ("", err).
// Overridable for tests.
var checkBlobContainerFn = func(ctx context.Context, subscriptionID, clusterName string) (string, error) {
	err := metacontainer.CheckAccessible(ctx, subscriptionID, clusterName)
	if err == nil {
		return "exists", nil
	}
	msg := err.Error()
	if strings.Contains(msg, "BlobServiceProperties") || strings.Contains(msg, "AuthorizationFailed") ||
		strings.Contains(msg, "403") {
		return "denied", fmt.Errorf("access denied to meta storage account")
	}
	if strings.Contains(msg, "The specified account is not found") ||
		strings.Contains(msg, "AccountNotFound") ||
		strings.Contains(msg, "no such host") {
		return "notfound", nil
	}
	return "", err
}

// putBlobFn writes a probe blob to verify write access. Overridable for tests.
var putBlobFn = func(ctx context.Context, subscriptionID, clusterName, key string, data []byte) error {
	c, err := metacontainer.Open(ctx, subscriptionID, clusterName)
	if err != nil {
		return err
	}
	if err := c.Put(ctx, key, data); err != nil {
		return err
	}
	_ = c.Delete(ctx, key) // best-effort cleanup
	return nil
}

// checkLockBlobFn verifies the lock blob placeholder exists or can be created.
// Overridable for tests.
var checkLockBlobFn = func(ctx context.Context, subscriptionID, clusterName string) error {
	c, err := metacontainer.Open(ctx, subscriptionID, clusterName)
	if err != nil {
		return err
	}
	key := metacontainer.LockBlobKey(clusterName)
	exists, err := c.Exists(ctx, key)
	if err != nil {
		return err
	}
	if exists {
		return nil // lock blob is present
	}
	// Probe write access
	return c.Put(ctx, key, []byte("wasctl-lock-probe"))
}

// metaContainerPrecondition skips when cloud is not azure, or no subscription.
func metaContainerPrecondition(rc *doctor.RunContext) (bool, string) {
	if rc.Cfg.Cloud != "azure" {
		return true, "not an Azure cluster"
	}
	if rc.Cfg.ChartOnly {
		return true, "chart-only mode; no wasctl-managed infrastructure"
	}
	if rc.AzureSubscriptionID == "" {
		return true, "Azure credentials unavailable; check azure.credentials first"
	}
	return false, ""
}

// MetaContainerReachable checks that the Azure meta storage account is accessible.
// NoSuchAccount on first install is a Pass (will be created on bootstrap).
type MetaContainerReachable struct{ BaseCheck }

func (MetaContainerReachable) ID() string               { return "meta_container.reachable" }
func (MetaContainerReachable) Name() string             { return "Meta storage container accessible" }
func (MetaContainerReachable) Category() doctor.Category { return doctor.CategoryEnvironment }
func (MetaContainerReachable) Description() string {
	return "Checks that the wasctl meta Azure Blob container is reachable. AccountNotFound on first install is a Pass."
}
func (c MetaContainerReachable) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	return metaContainerPrecondition(rc)
}
func (c MetaContainerReachable) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	clusterName := rc.Cfg.ClusterName.Value
	accountName := metacontainer.StorageAccountName(rc.AzureSubscriptionID, clusterName)
	rgName := metacontainer.MetaResourceGroup(clusterName)
	status, err := checkBlobContainerFn(ctx, rc.AzureSubscriptionID, clusterName)
	if err != nil {
		return fail(c.ID(), c.Name(), doctor.SeverityCritical,
			fmt.Sprintf("access denied to meta storage account %s", accountName),
			"Grant permission to list storage account keys on the meta account "+
				"(Contributor on the resource group, or Microsoft.Storage/storageAccounts/listKeys/action).\n"+
				"  Scope example:\n"+
				"  /subscriptions/"+rc.AzureSubscriptionID+"/resourceGroups/"+rgName+"/providers/Microsoft.Storage/storageAccounts/"+accountName+"\n"+
				"wasctl uses account keys for meta blob access; Storage Blob Data Contributor alone is not what this check requires.")
	}
	switch status {
	case "notfound":
		return pass(c.ID(), c.Name(), fmt.Sprintf("meta storage account %s not yet created; will be created on first install", accountName))
	default:
		return pass(c.ID(), c.Name(), fmt.Sprintf("meta storage account %s is accessible", accountName))
	}
}

// MetaContainerWritable checks write access to the meta blob container.
type MetaContainerWritable struct{ BaseCheck }

func (MetaContainerWritable) ID() string               { return "meta_container.writable" }
func (MetaContainerWritable) Name() string             { return "Can write to meta storage container" }
func (MetaContainerWritable) Category() doctor.Category { return doctor.CategoryEnvironment }
func (MetaContainerWritable) Description() string {
	return "Puts a probe blob to verify write access to the meta Azure Blob container."
}
func (c MetaContainerWritable) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	return metaContainerPrecondition(rc)
}
func (c MetaContainerWritable) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	clusterName := rc.Cfg.ClusterName.Value
	accountName := metacontainer.StorageAccountName(rc.AzureSubscriptionID, clusterName)
	status, err := checkBlobContainerFn(ctx, rc.AzureSubscriptionID, clusterName)
	if err != nil {
		return skip(c.ID(), c.Name(), "meta container not accessible; check meta_container.reachable first")
	}
	if status == "notfound" {
		return pass(c.ID(), c.Name(), "meta storage account not yet created; write check deferred to first install")
	}
	if writeErr := putBlobFn(ctx, rc.AzureSubscriptionID, clusterName, "doctor-probe/writable-check.txt", []byte("probe")); writeErr != nil {
		return fail(c.ID(), c.Name(), doctor.SeverityCritical,
			fmt.Sprintf("cannot write to meta blob container in %s: %v", accountName, writeErr),
			"Grant permission to list storage account keys on the meta account "+
				"(Contributor or Microsoft.Storage/storageAccounts/listKeys/action), then re-run doctor.")
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("write to %s succeeded", accountName))
}

// MetaContainerLockBlob checks that a blob lease lock can be established.
type MetaContainerLockBlob struct{ BaseCheck }

func (MetaContainerLockBlob) ID() string               { return "meta_container.lock_blob" }
func (MetaContainerLockBlob) Name() string             { return "Meta container lock blob writable" }
func (MetaContainerLockBlob) Category() doctor.Category { return doctor.CategoryEnvironment }
func (MetaContainerLockBlob) Description() string {
	return "Checks that the blob lease lock placeholder for this cluster can be written."
}
func (c MetaContainerLockBlob) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	if skip, reason := metaContainerPrecondition(rc); skip {
		return skip, reason
	}
	if rc.Cfg.ClusterName.Value == "" {
		return true, "no cluster name configured; skipping lock blob check"
	}
	return false, ""
}
func (c MetaContainerLockBlob) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	clusterName := rc.Cfg.ClusterName.Value
	accountName := metacontainer.StorageAccountName(rc.AzureSubscriptionID, clusterName)
	status, _ := checkBlobContainerFn(ctx, rc.AzureSubscriptionID, clusterName)
	if status == "notfound" {
		return pass(c.ID(), c.Name(), "meta storage account not yet created; lock blob check deferred to first install")
	}
	if err := checkLockBlobFn(ctx, rc.AzureSubscriptionID, clusterName); err != nil {
		return fail(c.ID(), c.Name(), doctor.SeverityCritical,
			fmt.Sprintf("cannot write lock blob in %s: %v", accountName, err),
			"Grant permission to list storage account keys on the meta account "+
				"(Contributor or Microsoft.Storage/storageAccounts/listKeys/action), then re-run doctor.")
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("lock blob writable in %s", accountName))
}
