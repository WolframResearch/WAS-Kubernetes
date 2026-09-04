package metacontainer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
)

// Empty deletes all current blobs (and their snapshots) from the wasctl-meta
// container. Idempotent when the container or account is gone. Soft-deleted /
// versioned leftovers are removed when Destroy deletes the storage account.
func (c *Container) Empty(ctx context.Context) error {
	svc := c.getSvc()
	if svc == nil {
		return nil // test/mem client — nothing to empty on the wire
	}
	pager := svc.NewListBlobsFlatPager(ContainerName, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isAzureNotFound(err) || isBlobNotFound(err) {
				return nil
			}
			return fmt.Errorf("list meta blobs for empty: %w", err)
		}
		include := blob.DeleteSnapshotsOptionTypeInclude
		for _, item := range page.Segment.BlobItems {
			if item.Name == nil {
				continue
			}
			_, err := svc.DeleteBlob(ctx, ContainerName, *item.Name, &azblob.DeleteBlobOptions{
				DeleteSnapshots: &include,
			})
			if err != nil && !isBlobNotFound(err) {
				return fmt.Errorf("delete meta blob %s: %w", *item.Name, err)
			}
		}
	}
	return nil
}

// Destroy deletes the per-cluster meta storage account and its resource group
// (wolfram-<cluster>-meta). It best-effort empties the wasctl-meta container
// first. Missing account/RG is treated as success (idempotent).
func Destroy(ctx context.Context, subscriptionID, clusterName string) error {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("azure credential: %w", err)
	}
	rg := MetaResourceGroup(clusterName)
	account := StorageAccountName(subscriptionID, clusterName)

	// Best-effort empty so soft-delete / versioning does not block account delete.
	if c, openErr := Open(ctx, subscriptionID, clusterName); openErr == nil {
		_ = c.Empty(ctx)
	}

	accountsClient, err := armstorage.NewAccountsClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("armstorage accounts client: %w", err)
	}
	if _, delErr := accountsClient.Delete(ctx, rg, account, nil); delErr != nil && !isAzureNotFound(delErr) {
		return fmt.Errorf("delete meta storage account %s: %w", account, delErr)
	}

	rgClient, err := armresources.NewResourceGroupsClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("armresources groups client: %w", err)
	}
	poller, delErr := rgClient.BeginDelete(ctx, rg, nil)
	if delErr != nil {
		if isAzureNotFound(delErr) {
			return nil
		}
		return fmt.Errorf("delete meta resource group %s: %w", rg, delErr)
	}
	delCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if _, waitErr := poller.PollUntilDone(delCtx, nil); waitErr != nil && !isAzureNotFound(waitErr) {
		if strings.Contains(strings.ToLower(waitErr.Error()), "not found") {
			return nil
		}
		return fmt.Errorf("wait delete meta resource group %s: %w", rg, waitErr)
	}
	return nil
}

// DestroyStorageAccount deletes a storage account by name (used as a fallback
// when terraform destroy fails on a shared state resource group).
func DestroyStorageAccount(ctx context.Context, subscriptionID, resourceGroup, accountName string) error {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("azure credential: %w", err)
	}
	accountsClient, err := armstorage.NewAccountsClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("armstorage accounts client: %w", err)
	}
	if _, err := accountsClient.Delete(ctx, resourceGroup, accountName, nil); err != nil && !isAzureNotFound(err) {
		return fmt.Errorf("delete storage account %s: %w", accountName, err)
	}
	return nil
}

// DeleteResourceGroupBestEffort deletes an Azure resource group if it still
// exists. Used for orphaned AKS node RGs (MC_*). Non-fatal callers should
// log errors from this function.
func DeleteResourceGroupBestEffort(ctx context.Context, subscriptionID, resourceGroup string) error {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("azure credential: %w", err)
	}
	rgClient, err := armresources.NewResourceGroupsClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("armresources groups client: %w", err)
	}
	_, err = rgClient.Get(ctx, resourceGroup, nil)
	if err != nil {
		if isAzureNotFound(err) {
			return nil
		}
		return fmt.Errorf("get resource group %s: %w", resourceGroup, err)
	}
	poller, err := rgClient.BeginDelete(ctx, resourceGroup, nil)
	if err != nil {
		if isAzureNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete resource group %s: %w", resourceGroup, err)
	}
	delCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	if _, err := poller.PollUntilDone(delCtx, nil); err != nil && !isAzureNotFound(err) {
		return fmt.Errorf("wait delete resource group %s: %w", resourceGroup, err)
	}
	return nil
}

// ListAccountKey returns the first storage account key for accountName in rg.
func ListAccountKey(ctx context.Context, subscriptionID, resourceGroup, accountName string) (string, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return "", fmt.Errorf("azure credential: %w", err)
	}
	accountsClient, err := armstorage.NewAccountsClient(subscriptionID, cred, nil)
	if err != nil {
		return "", fmt.Errorf("armstorage accounts client: %w", err)
	}
	keysResp, err := accountsClient.ListKeys(ctx, resourceGroup, accountName, nil)
	if err != nil {
		return "", fmt.Errorf("list keys for %s: %w", accountName, err)
	}
	if keysResp.Keys == nil || len(keysResp.Keys) == 0 || keysResp.Keys[0].Value == nil {
		return "", fmt.Errorf("no keys returned for storage account %s", accountName)
	}
	return *keysResp.Keys[0].Value, nil
}
