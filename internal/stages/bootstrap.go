package stages

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/assets"
	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metacontainer"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

// Bootstrap creates the Terraform remote-state backend.
// AWS: S3 bucket + DynamoDB lock table.
// Azure: resource group + storage account + blob container for Terraform state.
type Bootstrap struct{}

func (Bootstrap) Name() string         { return "bootstrap" }
func (Bootstrap) Description() string  { return "State backend bootstrapped" }
func (Bootstrap) EstimateText() string { return "~1 min" }
func (Bootstrap) Label() string        { return "Bootstrapping Terraform state backend" }
func (Bootstrap) DestroyLabel() string { return "Removing bootstrap resources" }
func (Bootstrap) Substeps() []string {
	// "Wait for state backend" is Azure-only (DNS readiness); started dynamically.
	return []string{"Open workspace", "Initialize Terraform", "Create state backend", "Save state"}
}

// Check returns nil when the workspace has a stateBucket (AWS) or
// AzureStateStorageAccount (Azure) recorded.
func (Bootstrap) Check(ctx context.Context, cfg *config.Config) error {
	accountID, err := cloudAccountID(ctx, cfg)
	if err != nil {
		return fmt.Errorf("bootstrap check: %w", err)
	}
	if cfg.Cloud == "azure" {
		c, err := metacontainer.Open(ctx, accountID, cfg.ClusterName.Value)
		if err != nil {
			return fmt.Errorf("bootstrap not complete: %w", err)
		}
		meta, err := metacontainer.ReadMetadata(ctx, c, cfg.ClusterName.Value)
		if err != nil {
			return fmt.Errorf("bootstrap not complete: %w", err)
		}
		if meta.AzureStateStorageAccount == "" {
			return fmt.Errorf("bootstrap not complete: AzureStateStorageAccount not recorded in workspace")
		}
		return nil
	}
	b, err := metabucket.Open(ctx, cfg.MetaRegion.Value, accountID, cfg.ClusterName.Value)
	if err != nil {
		return fmt.Errorf("bootstrap not complete: %w", err)
	}
	meta, err := metabucket.ReadMetadata(ctx, b, cfg.ClusterName.Value)
	if err != nil {
		return fmt.Errorf("bootstrap not complete: %w", err)
	}
	if meta.StateBucket == "" {
		return fmt.Errorf("bootstrap not complete: stateBucket not recorded in workspace")
	}
	return nil
}

func (Bootstrap) Apply(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	if cfg.Cloud == "azure" {
		return bootstrapApplyAzure(ctx, cfg, r, rep)
	}
	return bootstrapApplyAWS(ctx, cfg, r, rep)
}

func bootstrapApplyAWS(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	id, err := tools.GetCallerIdentity(ctx, cfg.Region.Value)
	if err != nil {
		return fmt.Errorf("derive account ID: %w", err)
	}
	accountID := id.Account

	rep.SubstepStart("Open workspace")
	w, err := workspace.OpenOrCreate(ctx, cfg.MetaRegion.Value, accountID, cfg.ClusterName.Value, cfg.Region.Value)
	if err != nil {
		rep.SubstepFail(err)
		return err
	}
	if err := w.Lock(ctx); err != nil {
		rep.SubstepFail(err)
		return err
	}
	defer w.Unlock(ctx)

	mat, err := w.Materialize(ctx, bootstrapAssets(cfg), workspace.Bootstrap)
	if err != nil {
		rep.SubstepFail(err)
		return err
	}
	defer func() {
		if persistErr := w.Persist(ctx, mat); persistErr != nil {
			rep.LogLine("[!] persist failed: " + persistErr.Error())
		}
		w.Close()
	}()
	rep.SubstepDone()

	cfg.DeriveBucketNames(accountID)
	rep.LogLine(fmt.Sprintf("State bucket: %s", cfg.StateBucket.Value))
	rep.LogLine(fmt.Sprintf("Lock table:   %s", cfg.LockTable.Value))

	rep.SubstepStart("Initialize Terraform")
	if err := runCmd(ctx, cfg, r, rep, tools.TerraformInit(mat.BootstrapDir, "")); err != nil {
		rep.SubstepFail(err)
		return err
	}
	rep.SubstepDone()

	rep.SubstepStart("Create state backend")
	if err := runCmd(ctx, cfg, r, rep, tools.TerraformApply(mat.BootstrapDir, map[string]string{
		"aws_region":        cfg.Region.Value,
		"state_bucket_name": cfg.StateBucket.Value,
		"lock_table_name":   cfg.LockTable.Value,
	})); err != nil {
		rep.SubstepFail(err)
		return err
	}
	rep.SubstepDone()

	rep.SubstepStart("Save state")
	stateBucket, err := tools.TerraformOutput(ctx, mat.BootstrapDir, "state_bucket_name")
	if err != nil || stateBucket == "" {
		rep.SubstepFail(fmt.Errorf("state_bucket_name output missing"))
		return fmt.Errorf("terraform output state_bucket_name: %w", err)
	}
	lockTable, err := tools.TerraformOutput(ctx, mat.BootstrapDir, "lock_table_name")
	if err != nil || lockTable == "" {
		rep.SubstepFail(fmt.Errorf("lock_table_name output missing"))
		return fmt.Errorf("terraform output lock_table_name: %w", err)
	}
	w.Meta.StateBucket = stateBucket
	w.Meta.LockTable = lockTable
	rep.LogLine(fmt.Sprintf("[✓] Recorded state bucket: %s", stateBucket))
	rep.SubstepDone()
	return nil
}

func bootstrapApplyAzure(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	info, err := tools.GetAccountInfo(ctx)
	if err != nil {
		return fmt.Errorf("derive subscription ID: %w", err)
	}
	subscriptionID := info.ID
	tenantID := info.TenantID
	if cfg.AzureTenantID.Value != "" {
		tenantID = cfg.AzureTenantID.Value
	}

	rep.SubstepStart("Open workspace")
	w, err := workspace.OpenOrCreateAzure(ctx, subscriptionID,
		azureMetaResourceGroup(cfg.ClusterName.Value), cfg.AzureLocation.Value, cfg.ClusterName.Value)
	if err != nil {
		rep.SubstepFail(err)
		return err
	}
	if err := w.Lock(ctx); err != nil {
		rep.SubstepFail(err)
		return err
	}
	defer w.Unlock(ctx)

	mat, err := w.Materialize(ctx, bootstrapAzureAssets(cfg), workspace.Bootstrap)
	if err != nil {
		rep.SubstepFail(err)
		return err
	}
	defer func() {
		if persistErr := w.Persist(ctx, mat); persistErr != nil {
			rep.LogLine("[!] persist failed: " + persistErr.Error())
		}
		w.Close()
	}()
	rep.SubstepDone()

	rep.SubstepStart("Initialize Terraform")
	if err := runCmd(ctx, cfg, r, rep, tools.TerraformInit(mat.BootstrapDir, "")); err != nil {
		rep.SubstepFail(err)
		return err
	}
	rep.SubstepDone()

	rep.SubstepStart("Create state backend")
	if err := runCmd(ctx, cfg, r, rep, tools.TerraformApply(mat.BootstrapDir, map[string]string{
		"subscription_id": subscriptionID,
		"tenant_id":       tenantID,
		"location":        cfg.AzureLocation.Value,
	})); err != nil {
		rep.SubstepFail(err)
		return err
	}
	rep.SubstepDone()

	rep.SubstepStart("Save state")
	rgName, err := tools.TerraformOutput(ctx, mat.BootstrapDir, "resource_group_name")
	if err != nil || rgName == "" {
		rep.SubstepFail(fmt.Errorf("resource_group_name output missing"))
		return fmt.Errorf("terraform output resource_group_name: %w", err)
	}
	saName, err := tools.TerraformOutput(ctx, mat.BootstrapDir, "storage_account_name")
	if err != nil || saName == "" {
		rep.SubstepFail(fmt.Errorf("storage_account_name output missing"))
		return fmt.Errorf("terraform output storage_account_name: %w", err)
	}
	w.Meta.AzureStateResourceGroup = rgName
	w.Meta.AzureStateStorageAccount = saName
	rep.LogLine(fmt.Sprintf("[✓] Recorded state resource group: %s", rgName))
	rep.LogLine(fmt.Sprintf("[✓] Recorded state storage account: %s", saName))
	rep.SubstepDone()

	rep.SubstepStart("Wait for state backend")
	if !cfg.DryRun {
		if err := waitForAzureStateBlobDNS(ctx, saName, rep); err != nil {
			rep.SubstepFail(err)
			return err
		}
	}
	rep.SubstepDone()
	return nil
}

func (Bootstrap) Destroy(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	if !cfg.DestroyStateBackend {
		rep.LogLine("Skipping state backend destruction (pass --destroy-state-backend to include).")
		return nil
	}
	if cfg.Cloud == "azure" {
		return bootstrapDestroyAzure(ctx, cfg, r, rep)
	}
	return bootstrapDestroyAWS(ctx, cfg, r, rep)
}

func bootstrapDestroyAWS(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	id, err := tools.GetCallerIdentity(ctx, cfg.Region.Value)
	if err != nil {
		return fmt.Errorf("derive account ID: %w", err)
	}
	w, err := workspace.Open(ctx, cfg.MetaRegion.Value, id.Account, cfg.ClusterName.Value)
	if err != nil {
		return err
	}
	if err := w.Lock(ctx); err != nil {
		return err
	}
	defer w.Unlock(ctx)

	mat, err := w.Materialize(ctx, bootstrapAssets(cfg), workspace.Bootstrap)
	if err != nil {
		return err
	}
	defer w.Close()

	cfg.DeriveBucketNames(id.Account)

	// 1. Empty the state S3 bucket first so Terraform delete does not fail on non-empty bucket
	if err := emptyS3Bucket(ctx, cfg.Region.Value, cfg.StateBucket.Value, rep); err != nil {
		rep.LogLine(fmt.Sprintf("[cleanup] warning: could not empty state bucket %s: %v", cfg.StateBucket.Value, err))
	}

	// 2. Initialize Terraform in the freshly materialized directory
	rep.LogLine("[cleanup] initializing Terraform in bootstrap directory...")
	if err := runCmd(ctx, cfg, r, rep, tools.TerraformInit(mat.BootstrapDir, "")); err != nil {
		return fmt.Errorf("terraform init: %w", err)
	}

	// 3. Destroy the state bucket and lock table via Terraform
	destroyErr := runCmd(ctx, cfg, r, rep, tools.TerraformDestroy(mat.BootstrapDir, map[string]string{
		"aws_region":        cfg.Region.Value,
		"state_bucket_name": cfg.StateBucket.Value,
		"lock_table_name":   cfg.LockTable.Value,
	}))
	if destroyErr != nil {
		return destroyErr
	}

	// 3. Clean up workspace metadata in the meta bucket
	rep.LogLine(fmt.Sprintf("[cleanup] removing workspace metadata for %s from meta bucket...", cfg.ClusterName.Value))
	keys := []string{
		metabucket.WorkspaceMetaKey(cfg.ClusterName.Value),
		metabucket.BackendHCLKey(cfg.ClusterName.Value),
		metabucket.BootstrapStateKey(cfg.ClusterName.Value),
		metabucket.BootstrapStateBackupKey(cfg.ClusterName.Value),
	}
	var deleteErr error
	for _, key := range keys {
		if err := w.Delete(ctx, key); err != nil {
			deleteErr = err
		}
	}
	if deleteErr != nil {
		rep.LogLine(fmt.Sprintf("[cleanup] warning: metadata cleanup incomplete: %v", deleteErr))
	} else {
		rep.LogLine("[cleanup] workspace metadata cleaned up successfully")
	}

	// 4. Delete the cluster-specific meta lock table and meta S3 bucket
	rep.LogLine("[cleanup] destroying cluster-specific meta lock table and S3 meta bucket...")
	if err := metabucket.DestroyLockTable(ctx, cfg.MetaRegion.Value, id.Account, cfg.ClusterName.Value); err != nil {
		rep.LogLine(fmt.Sprintf("[cleanup] warning: could not destroy meta lock table: %v", err))
	} else {
		rep.LogLine("[cleanup] DynamoDB lock table deleted successfully")
	}

	b, err := metabucket.Open(ctx, cfg.MetaRegion.Value, id.Account, cfg.ClusterName.Value)
	if err == nil {
		if err := b.Destroy(ctx); err != nil {
			rep.LogLine(fmt.Sprintf("[cleanup] warning: could not destroy S3 meta bucket: %v", err))
		} else {
			rep.LogLine("[cleanup] S3 meta bucket deleted successfully")
		}
	} else {
		rep.LogLine(fmt.Sprintf("[cleanup] warning: could not open S3 meta bucket: %v", err))
	}

	return nil
}

func bootstrapDestroyAzure(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	info, err := tools.GetAccountInfo(ctx)
	if err != nil {
		return fmt.Errorf("derive subscription ID: %w", err)
	}
	tenantID := info.TenantID
	if cfg.AzureTenantID.Value != "" {
		tenantID = cfg.AzureTenantID.Value
	}

	w, err := workspace.OpenAzure(ctx, info.ID, cfg.ClusterName.Value)
	if err != nil {
		return err
	}
	if err := w.Lock(ctx); err != nil {
		return err
	}
	unlocked := false
	defer func() {
		if !unlocked {
			w.Unlock(ctx)
		}
	}()

	mat, err := w.Materialize(ctx, bootstrapAzureAssets(cfg), workspace.Bootstrap)
	if err != nil {
		return err
	}
	defer w.Close()

	stateRG := w.Meta.AzureStateResourceGroup
	if stateRG == "" {
		stateRG = "wolfram-was-tfstate-rg"
	}
	stateSA := w.Meta.AzureStateStorageAccount
	stateContainer := "tfstate"

	// Empty the state container so terraform can delete it (mirrors AWS emptyS3Bucket).
	if stateSA != "" && !cfg.DryRun {
		if key, keyErr := metacontainer.ListAccountKey(ctx, info.ID, stateRG, stateSA); keyErr != nil {
			rep.LogLine(fmt.Sprintf("[cleanup] warning: could not list state SA keys: %v", keyErr))
		} else if err := emptyAzureBlobContainer(ctx, stateSA, stateContainer, key, rep); err != nil {
			rep.LogLine(fmt.Sprintf("[cleanup] warning: could not empty state container: %v", err))
		}
	}

	rep.LogLine("[cleanup] initializing Terraform in bootstrap directory...")
	if err := runCmd(ctx, cfg, r, rep, tools.TerraformInit(mat.BootstrapDir, "")); err != nil {
		return fmt.Errorf("terraform init: %w", err)
	}

	destroyVars := map[string]string{
		"subscription_id":     info.ID,
		"tenant_id":           tenantID,
		"location":            cfg.AzureLocation.Value,
		"resource_group_name": stateRG,
	}
	destroyErr := runCmd(ctx, cfg, r, rep, tools.TerraformDestroy(mat.BootstrapDir, destroyVars))
	if destroyErr != nil {
		// Shared RG (wolfram-was-tfstate-rg) may still hold other clusters' SAs.
		// Fall back to deleting this cluster's state storage account only.
		rep.LogLine(fmt.Sprintf("[cleanup] terraform bootstrap destroy failed: %v", destroyErr))
		if stateSA != "" && !cfg.DryRun {
			rep.LogLine(fmt.Sprintf("[cleanup] falling back to delete state storage account %s in %s", stateSA, stateRG))
			if err := metacontainer.DestroyStorageAccount(ctx, info.ID, stateRG, stateSA); err != nil {
				return fmt.Errorf("bootstrap destroy failed (%v); storage account fallback: %w", destroyErr, err)
			}
			rep.LogLine("[cleanup] state storage account deleted; shared resource group left in place if still in use")
		} else {
			return destroyErr
		}
	}

	// Mark destroyed before deleting keys / meta RG (workspace delete / UI status).
	now := time.Now().UTC()
	w.Meta.Status = "destroyed"
	w.Meta.DestroyedAt = &now
	w.Meta.ClusterARN = ""
	if err := w.PersistMeta(ctx); err != nil {
		rep.LogLine(fmt.Sprintf("[cleanup] warning: could not mark workspace destroyed: %v", err))
	}

	rep.LogLine(fmt.Sprintf("[cleanup] removing Azure workspace metadata for %s...", cfg.ClusterName.Value))
	keys := []string{
		metacontainer.WorkspaceMetaKey(cfg.ClusterName.Value),
		metacontainer.BackendTFVarsKey(cfg.ClusterName.Value),
		metacontainer.BootstrapStateKey(cfg.ClusterName.Value),
		metacontainer.BootstrapStateBackupKey(cfg.ClusterName.Value),
		metacontainer.LockBlobKey(cfg.ClusterName.Value),
	}
	var deleteErr error
	for _, key := range keys {
		if err := w.Delete(ctx, key); err != nil {
			deleteErr = err
		}
	}
	if deleteErr != nil {
		rep.LogLine(fmt.Sprintf("[cleanup] warning: metadata cleanup incomplete: %v", deleteErr))
	} else {
		rep.LogLine("[cleanup] workspace metadata cleaned up successfully")
	}

	// Release the lock before deleting the meta SA (lease target goes away).
	w.Unlock(ctx)
	unlocked = true

	rep.LogLine(fmt.Sprintf("[cleanup] destroying meta storage account and resource group %s...",
		metacontainer.MetaResourceGroup(cfg.ClusterName.Value)))
	if cfg.DryRun {
		rep.LogLine("[cleanup] dry-run: skip meta SA/RG destroy")
		return nil
	}
	if err := metacontainer.Destroy(ctx, info.ID, cfg.ClusterName.Value); err != nil {
		rep.LogLine(fmt.Sprintf("[cleanup] warning: could not destroy meta SA/RG: %v", err))
	} else {
		rep.LogLine("[cleanup] meta storage account and resource group deleted successfully")
	}

	return nil
}

// bootstrapAssets returns the AWS bootstrap asset FS.
func bootstrapAssets(cfg *config.Config) fs.FS {
	if cfg.Local {
		return assets.LocalBootstrapFS(cfg.RepoRoot)
	}
	return assets.BootstrapFS
}

// bootstrapAzureAssets returns the Azure bootstrap asset FS.
func bootstrapAzureAssets(cfg *config.Config) fs.FS {
	if cfg.Local {
		return assets.LocalAzureBootstrapFS(cfg.RepoRoot)
	}
	return assets.AzureBootstrapFS
}
