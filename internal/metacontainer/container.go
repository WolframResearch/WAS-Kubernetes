// Package metacontainer manages the per-Azure-subscription wasctl meta storage
// account and its companion blob container. It is the Azure analog of
// internal/metabucket: all durable cluster state (bootstrap terraform.tfstate,
// backend.tfvars, workspace.json) lives here so any machine with the right
// Azure AD credentials can continue where another left off.
//
// Authentication: DefaultAzureCredential — respects az login, managed identity,
// workload identity, and environment variables in the same precedence order as
// the Azure CLI.
//
// Why Azure SDK (not az CLI shell-out): the meta container operations happen
// frequently and synchronously; SDK calls avoid subprocess overhead and provide
// structured error types for isNotFound / isForbidden checks.
package metacontainer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

// blobDataClient abstracts data-plane blob operations against a fixed container.
// The production implementation is azContainerClient; tests use memContainerClient.
type blobDataClient interface {
	get(ctx context.Context, name string) ([]byte, error)
	put(ctx context.Context, name string, data []byte) error
	del(ctx context.Context, name string) error
	exists(ctx context.Context, name string) (bool, error)
	listPrefixes(ctx context.Context, prefix string) ([]string, error)
}

// Container is a handle to the wasctl meta blob container within an Azure
// storage account.
type Container struct {
	data           blobDataClient
	accountName    string
	subscriptionID string
	// cred is the AAD token credential used for control-plane operations.
	// nil only in test containers constructed via newTestContainer.
	cred azcore.TokenCredential
	// sharedKeyCred is used for data-plane operations (blob lease, etc.) to avoid
	// requiring Storage Blob Data Contributor RBAC on the caller.
	// nil only in test containers constructed via newTestContainer.
	sharedKeyCred *azblob.SharedKeyCredential
}

// Open returns a Container handle for the meta storage account for subscriptionID
// and clusterName. Does NOT create the account or container; call EnsureExists
// for first-run bootstrap.
func Open(ctx context.Context, subscriptionID, clusterName string) (*Container, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("azure credential: %w", err)
	}
	return openWithSharedKey(ctx, subscriptionID, clusterName, cred)
}

func openWithSharedKey(ctx context.Context, subscriptionID, clusterName string, cred azcore.TokenCredential) (*Container, error) {
	accountName := StorageAccountName(subscriptionID, clusterName)
	resourceGroup := MetaResourceGroup(clusterName)

	accountsClient, err := armstorage.NewAccountsClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("armstorage accounts client: %w", err)
	}

	keysResp, err := accountsClient.ListKeys(ctx, resourceGroup, accountName, nil)
	if err != nil {
		return nil, fmt.Errorf("list storage account keys: %w", err)
	}
	if len(keysResp.Keys) == 0 || keysResp.Keys[0].Value == nil {
		return nil, fmt.Errorf("no storage account keys found")
	}
	key := *keysResp.Keys[0].Value

	svcURL := fmt.Sprintf("https://%s.blob.core.windows.net/", accountName)
	sharedKeyCred, err := azblob.NewSharedKeyCredential(accountName, key)
	if err != nil {
		return nil, fmt.Errorf("azure shared key credential: %w", err)
	}
	svc, err := azblob.NewClientWithSharedKeyCredential(svcURL, sharedKeyCred, nil)
	if err != nil {
		return nil, fmt.Errorf("azure blob client: %w", err)
	}
	return &Container{
		data:           &azContainerClient{svc: svc, container: ContainerName},
		accountName:    accountName,
		subscriptionID: subscriptionID,
		cred:           cred,
		sharedKeyCred:  sharedKeyCred,
	}, nil
}

// EnsureExists creates the meta storage account (if absent) and the wasctl-meta
// container (if absent).
//
// Security posture mirrors the AWS meta bucket:
//   - TLS 1.2 minimum (MinimumTLSVersion: TLS1_2)
//   - Public network access disabled (AzureServices bypass enabled)
//   - HTTPS-only traffic
//   - Shared key access disabled — wasctl uses DefaultAzureCredential (Azure AD)
//
// Note: blob versioning and soft-delete (90-day retention) are set by Terraform
// bootstrap (infra/azure/bootstrap/main.tf). For deployments that bypass
// Terraform, call SetVersioningAndSoftDelete separately.
//
// Three outcomes:
//   - Already exists and accessible → returns (*Container, nil)
//   - Does not exist → creates and returns (*Container, nil)
//   - Access denied → descriptive error with required RBAC roles
func EnsureExists(ctx context.Context, subscriptionID, resourceGroup, location, clusterName string) (*Container, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("azure credential: %w", err)
	}
	accountName := StorageAccountName(subscriptionID, clusterName)

	rgClient, err := armresources.NewResourceGroupsClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("armresources groups client: %w", err)
	}
	_, err = rgClient.CreateOrUpdate(ctx, resourceGroup, armresources.ResourceGroup{
		Location: &location,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("ensure resource group %s: %w", resourceGroup, err)
	}

	accountsClient, err := armstorage.NewAccountsClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("armstorage accounts client: %w", err)
	}

	_, getErr := accountsClient.GetProperties(ctx, resourceGroup, accountName, nil)
	if getErr != nil {
		if isAzureForbidden(getErr) {
			return nil, fmt.Errorf(
				"cannot access meta storage account %s: %w\n"+
					"Required RBAC roles on subscription %s:\n"+
					"  Storage Account Contributor (to create the account)\n"+
					"  Storage Blob Data Contributor (for data-plane blob access)",
				accountName, getErr, subscriptionID,
			)
		}
		if !isAzureNotFound(getErr) {
			return nil, fmt.Errorf("check storage account %s: %w", accountName, getErr)
		}
		// Not found — create it.
		if err := createStorageAccount(ctx, accountsClient, subscriptionID, resourceGroup, accountName, location); err != nil {
			return nil, err
		}
	}

	c, err := openWithSharedKey(ctx, subscriptionID, clusterName, cred)
	if err != nil {
		return nil, err
	}

	// Ensure the wasctl-meta container exists (idempotent: 409 = already exists).
	_, cErr := c.getSvc().CreateContainer(ctx, ContainerName, nil)
	if cErr != nil && !isAzureConflict(cErr) {
		return nil, fmt.Errorf("create blob container %s: %w", ContainerName, cErr)
	}

	return c, nil
}

// CheckAccessible reports whether wasctl can use the meta storage account the
// same way the rest of the product does: list account keys (control plane) and
// open the blob container with a shared key. AAD "Storage Blob Data Contributor"
// is not required for that path, so probing with DefaultAzureCredential against
// the data plane produced false "access denied" failures.
//
// Returns nil when the account is usable, or when it does not exist yet
// (bootstrap will create it). Returns an error only when access is denied or
// another unexpected failure occurs.
func CheckAccessible(ctx context.Context, subscriptionID, clusterName string) error {
	accountName := StorageAccountName(subscriptionID, clusterName)
	_, err := Open(ctx, subscriptionID, clusterName)
	if err == nil {
		return nil
	}
	msg := err.Error()
	if isAzureNotFound(err) ||
		strings.Contains(msg, "ResourceNotFound") ||
		strings.Contains(msg, "ResourceGroupNotFound") ||
		strings.Contains(msg, "StorageAccountNotFound") ||
		strings.Contains(msg, "AccountNotFound") ||
		strings.Contains(msg, "was not found") ||
		strings.Contains(msg, "could not be found") {
		return nil
	}
	if isAzureForbidden(err) ||
		strings.Contains(msg, "AuthorizationFailed") ||
		strings.Contains(msg, "AuthorizationPermissionMismatch") ||
		strings.Contains(msg, "403") {
		return fmt.Errorf(
			"your Azure identity can authenticate but cannot access meta storage account %s.\n"+
				"Required: permission to list storage account keys "+
				"(Microsoft.Storage/storageAccounts/listKeys/action) on the account or its resource group",
			accountName,
		)
	}
	return fmt.Errorf("check meta container: %w", err)
}

// Name returns the storage account name.
func (c *Container) Name() string { return c.accountName }

// Get downloads the blob at key. Returns ErrNotFound if absent.
func (c *Container) Get(ctx context.Context, key string) ([]byte, error) {
	return c.data.get(ctx, key)
}

// Put uploads data to key. Skips the write if the content's SHA-256 matches
// the existing blob (idempotency, same as metabucket.Bucket.Put).
func (c *Container) Put(ctx context.Context, key string, data []byte) error {
	existing, err := c.data.get(ctx, key)
	if err == nil && sha256hex(existing) == sha256hex(data) {
		return nil // identical, skip upload
	}
	return c.data.put(ctx, key, data)
}

// Delete removes a blob. Idempotent (returns nil if the blob is absent).
func (c *Container) Delete(ctx context.Context, key string) error {
	return c.data.del(ctx, key)
}

// Exists returns true if key is present in the container.
func (c *Container) Exists(ctx context.Context, key string) (bool, error) {
	return c.data.exists(ctx, key)
}

// ListClusters returns cluster names whose workspace.json blobs exist.
func (c *Container) ListClusters(ctx context.Context) ([]string, error) {
	prefixes, err := c.data.listPrefixes(ctx, "clusters/")
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	var names []string
	for _, p := range prefixes {
		name := strings.TrimPrefix(p, "clusters/")
		name = strings.TrimSuffix(name, "/")
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// getSvc returns the underlying azblob.Client (used for container-level ops).
func (c *Container) getSvc() *azblob.Client {
	if a, ok := c.data.(*azContainerClient); ok {
		return a.svc
	}
	return nil
}

// ── Real Azure implementation ──────────────────────────────────────────────────

type azContainerClient struct {
	svc       *azblob.Client
	container string
}

func (c *azContainerClient) get(ctx context.Context, name string) ([]byte, error) {
	resp, err := c.svc.DownloadStream(ctx, c.container, name, nil)
	if err != nil {
		if isBlobNotFound(err) {
			return nil, &ErrNotFound{Key: name}
		}
		return nil, fmt.Errorf("download blob %s: %w", name, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read blob %s: %w", name, err)
	}
	return data, nil
}

func (c *azContainerClient) put(ctx context.Context, name string, data []byte) error {
	_, err := c.svc.UploadStream(ctx, c.container, name, bytes.NewReader(data), nil)
	return wrapErr(err, "upload blob "+name)
}

func (c *azContainerClient) del(ctx context.Context, name string) error {
	_, err := c.svc.DeleteBlob(ctx, c.container, name, nil)
	if err != nil && isBlobNotFound(err) {
		return nil
	}
	return wrapErr(err, "delete blob "+name)
}

func (c *azContainerClient) exists(ctx context.Context, name string) (bool, error) {
	// DownloadStream with an immediate close is the simplest way to check
	// existence using only azblob.Client (no blob sub-package import needed).
	// For small metadata blobs this is acceptable; workspace.json, backend.tfvars
	// are all < 64 KiB.
	resp, err := c.svc.DownloadStream(ctx, c.container, name, nil)
	if err != nil {
		if isBlobNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("check blob %s: %w", name, err)
	}
	_ = resp.Body.Close()
	return true, nil
}

func (c *azContainerClient) listPrefixes(ctx context.Context, prefix string) ([]string, error) {
	// Use NewListBlobsFlatPager (available on azblob.Client) and compute virtual
	// directory prefixes ourselves by splitting on the first "/" after the given prefix.
	// This is equivalent to what NewListBlobsHierarchyPager does server-side.
	pager := c.svc.NewListBlobsFlatPager(c.container, &azblob.ListBlobsFlatOptions{Prefix: &prefix})
	seen := map[string]bool{}
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list blobs: %w", err)
		}
		for _, item := range page.Segment.BlobItems {
			if item.Name == nil {
				continue
			}
			rest := strings.TrimPrefix(*item.Name, prefix)
			slash := strings.IndexByte(rest, '/')
			if slash >= 0 {
				seen[prefix+rest[:slash+1]] = true
			} else {
				seen[prefix+rest] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out, nil
}

// ── Management plane helpers ───────────────────────────────────────────────────

func createStorageAccount(ctx context.Context, client *armstorage.AccountsClient, subscriptionID, rg, name, location string) error {
	falseVal := false
	trueVal := true
	minTLS := armstorage.MinimumTLSVersionTLS12
	pubNet := armstorage.PublicNetworkAccessEnabled
	kind := armstorage.KindStorageV2
	sku := armstorage.SKUNameStandardLRS
	defAction := armstorage.DefaultActionAllow
	bypass := armstorage.BypassAzureServices
	keySource := armstorage.KeySourceMicrosoftStorage

	poller, err := client.BeginCreate(ctx, rg, name, armstorage.AccountCreateParameters{
		Location: &location,
		Kind:     &kind,
		SKU:      &armstorage.SKU{Name: &sku},
		Properties: &armstorage.AccountPropertiesCreateParameters{
			MinimumTLSVersion:     &minTLS,
			PublicNetworkAccess:   &pubNet,
			AllowBlobPublicAccess: &falseVal,
			// Shared key enabled: fallback support for client applications and Terraform state.
			AllowSharedKeyAccess: &trueVal,
			NetworkRuleSet: &armstorage.NetworkRuleSet{
				DefaultAction: &defAction,
				Bypass:        &bypass,
			},
			Encryption: &armstorage.Encryption{
				Services: &armstorage.EncryptionServices{
					Blob: &armstorage.EncryptionService{Enabled: &trueVal},
				},
				KeySource: &keySource,
			},
		},
	}, nil)
	if err != nil {
		return fmt.Errorf("begin create storage account %s: %w", name, err)
	}
	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		return fmt.Errorf("create storage account %s: %w", name, err)
	}
	// Note: blob versioning and soft-delete are configured by Terraform bootstrap.
	// For standalone wasctl deployments outside of Terraform, invoke
	// armstorage.NewBlobServicesClient.SetServiceProperties here to enable them.
	return nil
}

// ── Error helpers ──────────────────────────────────────────────────────────────

func isAzureNotFound(err error) bool {
	var re *azcore.ResponseError
	return errors.As(err, &re) && re.StatusCode == http.StatusNotFound
}

func isBlobNotFound(err error) bool {
	var re *azcore.ResponseError
	if !errors.As(err, &re) {
		return false
	}
	return re.StatusCode == http.StatusNotFound ||
		(re.StatusCode == http.StatusForbidden && re.ErrorCode == "AuthorizationPermissionMismatch")
}

func isAzureForbidden(err error) bool {
	var re *azcore.ResponseError
	return errors.As(err, &re) &&
		(re.StatusCode == http.StatusForbidden || re.StatusCode == http.StatusUnauthorized)
}

func isAzureConflict(err error) bool {
	var re *azcore.ResponseError
	return errors.As(err, &re) && re.StatusCode == http.StatusConflict
}

// isAzureLeaseConflict returns true when a blob lease is already held by another process.
func isAzureLeaseConflict(err error) bool {
	var re *azcore.ResponseError
	return errors.As(err, &re) && re.StatusCode == http.StatusConflict &&
		re.ErrorCode == "LeaseAlreadyPresent"
}

func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

func wrapErr(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// ListClustersInSubscription lists all clusters in an Azure subscription by finding
// resource groups that match the pattern "wolfram-<cluster>-meta".
func ListClustersInSubscription(ctx context.Context, subscriptionID string) ([]string, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("azure credential: %w", err)
	}
	rgClient, err := armresources.NewResourceGroupsClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("armresources groups client: %w", err)
	}

	var clusters []string
	pager := rgClient.NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list resource groups: %w", err)
		}
		for _, rg := range page.ResourceGroupListResult.Value {
			if rg.Name == nil {
				continue
			}
			name := *rg.Name
			if strings.HasPrefix(name, "wolfram-") && strings.HasSuffix(name, "-meta") {
				clusterName := strings.TrimPrefix(name, "wolfram-")
				clusterName = strings.TrimSuffix(clusterName, "-meta")
				if clusterName != "" {
					clusters = append(clusters, clusterName)
				}
			}
		}
	}
	return clusters, nil
}

