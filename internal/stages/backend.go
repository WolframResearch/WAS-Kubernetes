package stages

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
	"github.com/WolframResearch/WAS-Kubernetes/internal/assets"
	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metacontainer"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
)

const stackDir = "infra/aws/stack" // used by --local mode only

// Backend reads bootstrap outputs and writes the backend config to the meta store.
// AWS: backend.hcl    (S3 backend; content: bucket, key, region, use_lockfile)
// Azure: backend.tfvars (azurerm backend; content: resource_group_name,
//
//	storage_account_name, container_name, key, use_azuread_auth)
type Backend struct{}

func (Backend) Name() string         { return "backend" }
func (Backend) Description() string  { return "Backend config generated" }
func (Backend) EstimateText() string { return "~1 sec" }
func (Backend) Label() string {
	return "Generating backend config"
}
func (Backend) DestroyLabel() string { return "Removing backend configuration" }
func (Backend) Substeps() []string {
	return []string{"Read bootstrap outputs", "Write backend config"}
}

func (Backend) Check(ctx context.Context, cfg *config.Config) error {
	accountID, err := cloudAccountID(ctx, cfg)
	if err != nil {
		return fmt.Errorf("backend check: %w", err)
	}
	if cfg.Cloud == "azure" {
		c, err := metacontainer.Open(ctx, accountID, cfg.ClusterName.Value)
		if err != nil {
			return fmt.Errorf("backend check: %w", err)
		}
		ok, err := c.Exists(ctx, metacontainer.BackendTFVarsKey(cfg.ClusterName.Value))
		if err != nil {
			return fmt.Errorf("backend check: %w", err)
		}
		if !ok {
			return fmt.Errorf("backend.tfvars not in meta container: run 'wasctl install bootstrap' first")
		}
		return nil
	}
	b, err := metabucket.Open(ctx, cfg.MetaRegion.Value, accountID, cfg.ClusterName.Value)
	if err != nil {
		return fmt.Errorf("backend check: %w", err)
	}
	ok, err := b.Exists(ctx, metabucket.BackendHCLKey(cfg.ClusterName.Value))
	if err != nil {
		return fmt.Errorf("backend check: %w", err)
	}
	if !ok {
		return fmt.Errorf("backend.hcl not in meta bucket: run 'wasctl install bootstrap' first")
	}
	return nil
}

func (Backend) Apply(ctx context.Context, cfg *config.Config, _ runner.Runner, rep runner.Reporter) error {
	if cfg.Cloud == "azure" {
		return backendApplyAzure(ctx, cfg, rep)
	}
	return backendApplyAWS(ctx, cfg, rep)
}

func backendApplyAWS(ctx context.Context, cfg *config.Config, rep runner.Reporter) error {
	id, err := tools.GetCallerIdentity(ctx, cfg.Region.Value)
	if err != nil {
		return fmt.Errorf("derive account ID: %w", err)
	}

	b, err := metabucket.Open(ctx, cfg.MetaRegion.Value, id.Account, cfg.ClusterName.Value)
	if err != nil {
		return err
	}
	meta, err := metabucket.ReadMetadata(ctx, b, cfg.ClusterName.Value)
	if err != nil {
		return err
	}

	rep.SubstepStart("Read bootstrap outputs")
	if meta.StateBucket == "" {
		rep.SubstepFail(fmt.Errorf("stateBucket not recorded"))
		return fmt.Errorf("bootstrap outputs not available: run stage 'bootstrap' first")
	}
	rep.LogLine(fmt.Sprintf("[✓] State bucket: %s", meta.StateBucket))
	rep.SubstepDone()

	rep.SubstepStart("Write backend config")
	content := BackendHCLContent(meta.StateBucket, cfg.Region.Value)
	if cfg.DryRun {
		rep.LogLine(fmt.Sprintf("[dry-run] would write backend.hcl with bucket=%s", meta.StateBucket))
		rep.SubstepDone()
		return nil
	}
	if err := b.Put(ctx, metabucket.BackendHCLKey(cfg.ClusterName.Value), []byte(content)); err != nil {
		rep.SubstepFail(err)
		return fmt.Errorf("write backend.hcl to meta bucket: %w", err)
	}
	if cfg.Local {
		localPath := filepath.Join(cfg.RepoRoot, stackDir, "backend.hcl")
		if err := os.WriteFile(localPath, []byte(content), 0600); err != nil {
			rep.SubstepFail(err)
			return fmt.Errorf("write local backend.hcl: %w", err)
		}
	}
	rep.LogLine("[✓] Wrote backend.hcl")
	rep.SubstepDone()
	return nil
}

func backendApplyAzure(ctx context.Context, cfg *config.Config, rep runner.Reporter) error {
	info, err := tools.GetAccountInfo(ctx)
	if err != nil {
		return fmt.Errorf("derive subscription ID: %w", err)
	}

	c, err := metacontainer.Open(ctx, info.ID, cfg.ClusterName.Value)
	if err != nil {
		return err
	}
	meta, err := metacontainer.ReadMetadata(ctx, c, cfg.ClusterName.Value)
	if err != nil {
		return err
	}

	rep.SubstepStart("Read bootstrap outputs")
	if meta.AzureStateStorageAccount == "" {
		rep.SubstepFail(fmt.Errorf("AzureStateStorageAccount not recorded"))
		return fmt.Errorf("bootstrap outputs not available: run stage 'bootstrap' first")
	}
	rep.LogLine(fmt.Sprintf("[✓] State storage account: %s", meta.AzureStateStorageAccount))
	rep.SubstepDone()

	rep.SubstepStart("Retrieve state storage account keys")
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		rep.SubstepFail(err)
		return fmt.Errorf("azure credential: %w", err)
	}
	accountsClient, err := armstorage.NewAccountsClient(info.ID, cred, nil)
	if err != nil {
		rep.SubstepFail(err)
		return fmt.Errorf("armstorage accounts client: %w", err)
	}
	keysResp, err := accountsClient.ListKeys(ctx, meta.AzureStateResourceGroup, meta.AzureStateStorageAccount, nil)
	if err != nil {
		rep.SubstepFail(err)
		return fmt.Errorf("list state storage account keys: %w", err)
	}
	if len(keysResp.Keys) == 0 || keysResp.Keys[0].Value == nil {
		errNoKeys := fmt.Errorf("no state storage account keys found")
		rep.SubstepFail(errNoKeys)
		return errNoKeys
	}
	accessKey := *keysResp.Keys[0].Value
	rep.SubstepDone()

	rep.SubstepStart("Write backend config")
	content := BackendTFVarsContent(meta.AzureStateResourceGroup, meta.AzureStateStorageAccount, accessKey)
	if cfg.DryRun {
		rep.LogLine(fmt.Sprintf("[dry-run] would write backend.tfvars with storage_account=%s", meta.AzureStateStorageAccount))
		rep.SubstepDone()
		return nil
	}
	if err := c.Put(ctx, metacontainer.BackendTFVarsKey(cfg.ClusterName.Value), []byte(content)); err != nil {
		rep.SubstepFail(err)
		return fmt.Errorf("write backend.tfvars to meta container: %w", err)
	}
	if cfg.Local {
		localPath := filepath.Join(cfg.RepoRoot, azureLocalStackDir, "backend.tfvars")
		if err := os.WriteFile(localPath, []byte(content), 0600); err != nil {
			rep.SubstepFail(err)
			return fmt.Errorf("write local backend.tfvars: %w", err)
		}
	}
	rep.LogLine("[✓] Wrote backend.tfvars")
	rep.SubstepDone()
	return nil
}

func (Backend) Destroy(_ context.Context, cfg *config.Config, _ runner.Runner, rep runner.Reporter) error {
	if cfg.DryRun {
		rep.LogLine("[dry-run] would remove backend config from meta store")
		return nil
	}
	rep.LogLine("[✓] backend config removed with workspace cleanup")
	return nil
}

// BackendHCLContent returns the canonical backend.hcl content (AWS S3 backend).
// Exported so the drift test can compare against the TF source of truth.
func BackendHCLContent(stateBucket, region string) string {
	return fmt.Sprintf(`bucket       = %q
key          = "stack/terraform.tfstate"
region       = %q
use_lockfile = true
encrypt      = true
`, stateBucket, region)
}

// BackendTFVarsContent returns the canonical backend.tfvars content (Azure azurerm backend).
// containerName defaults to "tfstate" (matching the bootstrap Terraform default).
func BackendTFVarsContent(resourceGroupName, storageAccountName, accessKey string) string {
	return fmt.Sprintf(`resource_group_name  = %q
storage_account_name = %q
container_name       = "tfstate"
key                  = "stack/terraform.tfstate"
access_key           = %q
`, resourceGroupName, storageAccountName, accessKey)
}

// stackAssets returns the AWS stack asset FS.
func stackAssets(cfg *config.Config) fs.FS {
	if cfg.Local {
		return assets.LocalStackFS(cfg.RepoRoot)
	}
	return assets.StackFS
}

// azureStackAssets returns the Azure stack asset FS.
func azureStackAssets(cfg *config.Config) fs.FS {
	if cfg.Local {
		return assets.LocalAzureStackFS(cfg.RepoRoot)
	}
	return assets.AzureStackFS
}
