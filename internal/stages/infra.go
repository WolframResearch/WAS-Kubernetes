package stages

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/cloudsweep"
	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metacontainer"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

// Infra provisions the full cloud infrastructure stack via Terraform.
// AWS: EKS cluster, VPC, EFS, S3 buckets, IAM roles.
// Azure: AKS cluster, VNet, Azure Files, blob storage, managed identities.
type Infra struct{}

func (Infra) Name() string         { return "infra" }
func (Infra) Description() string  { return "Infrastructure provisioned" }
func (Infra) EstimateText() string { return "25-35 min" }
func (Infra) Label() string {
	return "Provisioning infrastructure"
}
func (Infra) DestroyLabel() string { return "Destroying infrastructure" }
func (Infra) Substeps() []string {
	// "Wait for state backend" is Azure-only and started dynamically in
	// infraApplyAzure / infraDestroyAzure — omit from the shared list so AWS
	// progress UIs do not show a phantom substep.
	return []string{"Open workspace", "Initialize Terraform", "Apply infrastructure"}
}

func (Infra) Check(ctx context.Context, cfg *config.Config) error {
	if cfg.Local {
		dir := localStackDir(cfg)
		val, err := tools.TerraformOutput(ctx, dir, "cluster_name")
		if err != nil || val == "" {
			return fmt.Errorf("infra not provisioned: cluster_name output missing")
		}
		return nil
	}
	accountID, err := cloudAccountID(ctx, cfg)
	if err != nil {
		return fmt.Errorf("infra check: %w", err)
	}
	w, wErr := workspace.OpenForCloud(ctx, cfg, accountID, cfg.ClusterName.Value)
	if wErr != nil {
		return fmt.Errorf("infra not provisioned: %w", wErr)
	}
	// AWS: require a live ACTIVE cluster with node groups. If ClusterARN was
	// never persisted (e.g. --local infra, persist failure), backfill it when
	// the live cluster is healthy so the UI stops showing INSTALLING.
	if cfg.Cloud != "azure" {
		status, err := tools.EKSClusterStatus(ctx, cfg.Region.Value, cfg.ClusterName.Value)
		if err != nil {
			return fmt.Errorf("infra not provisioned: %w", err)
		}
		if status != "ACTIVE" {
			return fmt.Errorf("infra not provisioned: cluster status %q (want ACTIVE)", status)
		}
		n, err := tools.EKSNodegroupCount(ctx, cfg.Region.Value, cfg.ClusterName.Value)
		if err != nil {
			return fmt.Errorf("infra not provisioned: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("infra not provisioned: cluster has no node groups (partial apply; re-run infra)")
		}
		if w.Meta.ClusterARN == "" {
			w.Meta.ClusterARN = workspace.ClusterARN(cfg.Region.Value, accountID, cfg.ClusterName.Value)
			if err := w.PersistMeta(ctx); err != nil {
				return fmt.Errorf("infra check: persist healed clusterARN: %w", err)
			}
		}
		return nil
	}
	if w.Meta.ClusterARN == "" {
		return fmt.Errorf("infra not provisioned: clusterARN not recorded in workspace")
	}
	return nil
}

func (Infra) Apply(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	if cfg.Cloud == "azure" {
		return infraApplyAzure(ctx, cfg, r, rep)
	}
	return infraApplyAWS(ctx, cfg, r, rep)
}

func infraApplyAWS(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	rep.LogLine("This stage takes 25–35 minutes. Terraform output streams below.")
	rep.LogLine("")

	var stackTFDir string
	var w *workspace.Workspace
	var accountID string

	if cfg.Local {
		stackTFDir = filepath.Join(cfg.RepoRoot, stackDir)
		rep.SubstepStart("Open workspace")
		rep.LogLine("[local] using local stack directory: " + stackTFDir)
		// Still open the meta workspace so we can record ClusterARN after apply
		// (UI used to show INSTALLING forever when ARN was never persisted).
		id, idErr := tools.GetCallerIdentity(ctx, cfg.Region.Value)
		if idErr != nil {
			rep.LogLine(fmt.Sprintf("[!] derive account ID: %v (will continue apply)", idErr))
		} else {
			accountID = id.Account
			ws, openErr := workspace.Open(ctx, cfg.MetaRegion.Value, id.Account, cfg.ClusterName.Value)
			if openErr != nil {
				rep.LogLine(fmt.Sprintf("[!] open workspace for meta update: %v (will continue apply)", openErr))
			} else {
				w = ws
				if err := w.Lock(ctx); err != nil {
					rep.LogLine(fmt.Sprintf("[!] workspace lock: %v", err))
					w = nil
				} else {
					defer w.Unlock(ctx)
					defer func() {
						if w == nil {
							return
						}
						if persistErr := w.PersistMeta(ctx); persistErr != nil {
							rep.LogLine("[!] persist meta failed: " + persistErr.Error())
						}
						w.Close()
					}()
				}
			}
		}
		rep.SubstepDone()

		backendPath := filepath.Join(cfg.RepoRoot, stackDir, "backend.hcl")
		rep.SubstepStart("Initialize Terraform")
		if err := runCmd(ctx, cfg, r, rep, tools.TerraformInit(stackTFDir, backendPath)); err != nil {
			rep.SubstepFail(err)
			return err
		}
		rep.SubstepDone()
	} else {
		id, err := tools.GetCallerIdentity(ctx, cfg.Region.Value)
		if err != nil {
			return fmt.Errorf("derive account ID: %w", err)
		}
		accountID = id.Account
		rep.SubstepStart("Open workspace")
		ws, err := workspace.Open(ctx, cfg.MetaRegion.Value, id.Account, cfg.ClusterName.Value)
		if err != nil {
			rep.SubstepFail(err)
			return err
		}
		w = ws
		if err := w.CheckCloudMatch(cfg.Cloud); err != nil {
			rep.SubstepFail(err)
			return err
		}
		if err := w.Lock(ctx); err != nil {
			rep.SubstepFail(err)
			return err
		}
		defer w.Unlock(ctx)

		mat, err := w.Materialize(ctx, stackAssets(cfg), workspace.Stack)
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
		stackTFDir = mat.StackDir
		backendPath := filepath.Join(stackTFDir, "backend.hcl")
		rep.SubstepDone()

		rep.SubstepStart("Initialize Terraform")
		if err := runCmd(ctx, cfg, r, rep, tools.TerraformInit(stackTFDir, backendPath)); err != nil {
			rep.SubstepFail(err)
			return err
		}
		rep.SubstepDone()
	}

	// Clear exact-name LT orphans from older stacks (fixed name, no prefix).
	// Only when the cluster is missing or has no node groups — never delete an
	// in-service template on a healthy cluster.
	if !cfg.DryRun {
		cleanLT := true
		if status, err := tools.EKSClusterStatus(ctx, cfg.Region.Value, cfg.ClusterName.Value); err == nil && status == "ACTIVE" {
			if n, err := tools.EKSNodegroupCount(ctx, cfg.Region.Value, cfg.ClusterName.Value); err == nil && n > 0 {
				cleanLT = false
			}
		}
		if cleanLT {
			ltName := cfg.ClusterName.Value + "-nodes-lt"
			if err := tools.DeleteLaunchTemplateIfExists(ctx, cfg.Region.Value, ltName); err != nil {
				rep.LogLine(fmt.Sprintf("[infra] warning: could not clear orphan launch template %q: %v", ltName, err))
			}
		}
	}

	rep.SubstepStart("Apply infrastructure")
	if err := runCmd(ctx, cfg, r, rep, tools.TerraformApply(stackTFDir, infraAWSVars(cfg))); err != nil {
		rep.SubstepFail(err)
		return err
	}
	rep.SubstepDone()

	// Record identity only after a successful apply (matches Azure). A prior
	// defer set ClusterARN even when apply failed, which made Check skip infra.
	if w != nil && accountID != "" {
		w.Meta.ClusterARN = workspace.ClusterARN(cfg.Region.Value, accountID, cfg.ClusterName.Value)
		rep.LogLine("[✓] Recorded EKS cluster ARN in workspace")
	}
	return nil
}

func infraApplyAzure(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	info, err := tools.GetAccountInfo(ctx)
	if err != nil {
		return fmt.Errorf("derive subscription ID: %w", err)
	}
	tenantID := info.TenantID
	if cfg.AzureTenantID.Value != "" {
		tenantID = cfg.AzureTenantID.Value
	}

	rep.LogLine("This stage takes 25–35 minutes. Terraform output streams below.")
	rep.LogLine("")

	var stackTFDir string
	var w *workspace.Workspace

	if cfg.Local {
		stackTFDir = filepath.Join(cfg.RepoRoot, azureLocalStackDir)
		rep.SubstepStart("Open workspace")
		rep.LogLine("[local] using local Azure stack directory: " + stackTFDir)
		// Still open the meta workspace so we can record the stack RG / AKS identity
		// after apply (kubeconfig must not use the meta RG).
		var openErr error
		w, openErr = workspace.OpenAzure(ctx, info.ID, cfg.ClusterName.Value)
		if openErr != nil {
			rep.LogLine(fmt.Sprintf("[!] open workspace for meta update: %v (will continue apply)", openErr))
		} else {
			if err := w.Lock(ctx); err != nil {
				rep.LogLine(fmt.Sprintf("[!] workspace lock: %v", err))
				w = nil
			} else {
				defer w.Unlock(ctx)
				defer func() {
					if w == nil {
						return
					}
					if persistErr := w.PersistMeta(ctx); persistErr != nil {
						rep.LogLine("[!] persist meta failed: " + persistErr.Error())
					}
					w.Close()
				}()
			}
		}
		rep.SubstepDone()

		backendPath := filepath.Join(stackTFDir, "backend.tfvars")
		if err := waitAzureBackendThenInit(ctx, cfg, r, rep, stackTFDir, backendPath); err != nil {
			return err
		}
	} else {
		rep.SubstepStart("Open workspace")
		var err error
		w, err = workspace.OpenAzure(ctx, info.ID, cfg.ClusterName.Value)
		if err != nil {
			rep.SubstepFail(err)
			return err
		}
		if err := w.CheckCloudMatch(cfg.Cloud); err != nil {
			rep.SubstepFail(err)
			return err
		}
		if err := w.Lock(ctx); err != nil {
			rep.SubstepFail(err)
			return err
		}
		defer w.Unlock(ctx)

		mat, err := w.Materialize(ctx, azureStackAssets(cfg), workspace.Stack)
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
		stackTFDir = mat.StackDir
		backendPath := filepath.Join(stackTFDir, "backend.tfvars")
		rep.SubstepDone()

		if err := waitAzureBackendThenInit(ctx, cfg, r, rep, stackTFDir, backendPath); err != nil {
			return err
		}
	}

	rep.SubstepStart("Apply infrastructure")
	if err := runCmd(ctx, cfg, r, rep,
		tools.TerraformApply(stackTFDir, infraAzureVars(cfg, info.ID, tenantID))); err != nil {
		rep.SubstepFail(err)
		return err
	}
	rep.SubstepDone()

	if w != nil {
		rg := workspace.AzureStackResourceGroup(cfg.ClusterName.Value)
		aks := workspace.AzureAKSName(cfg.ClusterName.Value)
		w.Meta.AzureResourceGroup = rg
		w.Meta.ClusterARN = workspace.ClusterResourceID(info.ID, rg, aks)
		rep.LogLine(fmt.Sprintf("[✓] Recorded AKS identity: rg=%s name=%s", rg, aks))
	}
	return nil
}

func (Infra) Destroy(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	if cfg.Cloud == "azure" {
		return infraDestroyAzure(ctx, cfg, r, rep)
	}
	return infraDestroyAWS(ctx, cfg, r, rep)
}

func infraDestroyAWS(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	id, err := tools.GetCallerIdentity(ctx, cfg.Region.Value)
	if err != nil {
		return fmt.Errorf("derive account ID: %w", err)
	}

	if cfg.Local {
		backendPath := filepath.Join(cfg.RepoRoot, stackDir, "backend.hcl")
		dir := filepath.Join(cfg.RepoRoot, stackDir)
		if err := runCmd(ctx, cfg, r, rep, tools.TerraformInit(dir, backendPath)); err != nil {
			return err
		}
		return terraformDestroyWithVPCRetry(ctx, cfg, r, rep, dir)
	}

	w, err := workspace.Open(ctx, cfg.MetaRegion.Value, id.Account, cfg.ClusterName.Value)
	if err != nil {
		return err
	}
	if err := w.Lock(ctx); err != nil {
		return err
	}
	defer w.Unlock(ctx)

	mat, err := w.Materialize(ctx, stackAssets(cfg), workspace.Stack)
	if err != nil {
		return err
	}
	defer w.Close()

	backendPath := filepath.Join(mat.StackDir, "backend.hcl")
	if err := runCmd(ctx, cfg, r, rep, tools.TerraformInit(mat.StackDir, backendPath)); err != nil {
		return err
	}
	if err := terraformDestroyWithVPCRetry(ctx, cfg, r, rep, mat.StackDir); err != nil {
		return err
	}

	if !cfg.DryRun {
		now := time.Now().UTC()
		w.Meta.Status = "destroyed"
		w.Meta.DestroyedAt = &now
		w.Meta.ClusterARN = ""
		if err := w.PersistMeta(ctx); err != nil {
			rep.LogLine(fmt.Sprintf("[destroy] warning: could not mark workspace destroyed: %v", err))
		} else {
			rep.LogLine("[destroy] workspace status set to destroyed")
		}
	}
	return nil
}

// terraformDestroyWithVPCRetry runs terraform destroy in a robust sequence:
// 1. Destroys the EKS cluster module first to shut down compute/nodes and stop new resources.
// 2. Sweeps Kubernetes CCM-created orphans (SGs, load balancers, ENIs) from the VPC.
// 3. Runs the full untargeted terraform destroy to cleanly remove VPC and subnets.
//
// Retries the sweep and destroy exactly once if the final run still hits a VPC DependencyViolation.
func terraformDestroyWithVPCRetry(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter, stackDir string) error {
	// First, destroy the EKS cluster module to terminate compute nodes and stop controller loops.
	// EKS deletion does not depend on load balancers, so it always succeeds or fails cleanly.
	rep.LogLine("[destroy] destroying EKS cluster first to allow safe resource sweep...")
	destroyEKSCmd := tools.TerraformDestroyTarget(stackDir, "module.eks", infraDestroyAWSVars(cfg))
	if err := runCmd(ctx, cfg, r, rep, destroyEKSCmd); err != nil {
		rep.LogLine(fmt.Sprintf("[destroy] warning: targeted EKS destroy: %v (proceeding to sweep and full destroy)", err))
	} else {
		rep.LogLine("[destroy] EKS cluster destroyed successfully")
	}

	// Scan and sweep CCM-created load balancers, ENIs, and security groups that block VPC deletion.
	// Since EKS is gone, there are no running controllers to re-create them.
	rep.LogLine("[destroy] sweeping CCM-created orphans (SGs, load balancers, ENIs) from VPC...")
	postDestroyVPCSweep(ctx, cfg, stackDir, rep)

	// Run the full stack destroy to clean up VPC, subnets, and all remaining resources.
	destroyCmd := tools.TerraformDestroy(stackDir, infraDestroyAWSVars(cfg))
	if err := runCmd(ctx, cfg, r, rep, destroyCmd); err != nil {
		if !isVPCDependencyViolation(err) {
			return err
		}
		rep.LogLine("[destroy] terraform failed with VPC DependencyViolation — running failsafe sweep")
		postDestroyVPCSweep(ctx, cfg, stackDir, rep)
		rep.LogLine("[destroy] retrying terraform destroy after failsafe sweep")
		return runCmd(ctx, cfg, r, rep, destroyCmd)
	}
	return nil
}

// isVPCDependencyViolation reports whether err came from terraform failing to
// delete the VPC with a DependencyViolation. terraform's runner error for this
// case includes both marker strings in its tail-line summary.
func isVPCDependencyViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "DependencyViolation") &&
		(strings.Contains(msg, "EC2 VPC") || strings.Contains(msg, "aws_vpc"))
}

// postDestroyVPCSweep runs after terraform destroy exits with a VPC
// DependencyViolation. At this point EKS has already been destroyed by
// terraform (the dependency graph guarantees it precedes VPC deletion), so
// only CCM-created orphans that terraform never managed can be blocking:
//   - k8s-elb-* security groups (in-tree ELB controller)
//   - k8s-traffic-* security groups (AWS LBC NLB)
//
// Terraform-managed SGs (eks-cluster-sg-*, wasctl-cluster-sg, etc.) are
// already gone. All errors are non-fatal.
func postDestroyVPCSweep(ctx context.Context, cfg *config.Config, stackTFDir string, rep runner.Reporter) {
	vpcID, err := tools.TerraformOutput(ctx, stackTFDir, "vpc_id")
	if err != nil || vpcID == "" {
		var tagErr error
		vpcID, tagErr = cloudsweep.DiscoverVPCID(ctx, cfg.Region.Value, cfg.ClusterName.Value)
		if tagErr != nil {
			rep.LogLine(fmt.Sprintf("[destroy] cannot identify VPC for post-fail sweep (terraform: %v, tag: %v)", err, tagErr))
			rep.LogLine("[destroy] investigate the VPC blocker manually:")
			rep.LogLine(fmt.Sprintf("[destroy]   aws ec2 describe-security-groups --filters Name=vpc-id,Values=<vpc-id> --region %s", cfg.Region.Value))
			return
		}
	}

	rep.LogLine(fmt.Sprintf("[destroy] post-fail sweep: scanning VPC %s for CCM-created orphan SGs", vpcID))

	sweeper, err := cloudsweep.NewAWSSweeper(ctx, cloudsweep.SweepOptions{
		ClusterName: cfg.ClusterName.Value,
		VPCID:       vpcID,
		Region:      cfg.Region.Value,
		DryRun:      cfg.DryRun,
	}, rep)
	if err != nil {
		rep.LogLine(fmt.Sprintf("[destroy] cannot init sweeper for post-fail sweep: %v — retry will likely fail", err))
		return
	}

	report, sweepErr := sweeper.Sweep(ctx)
	if sweepErr != nil {
		rep.LogLine(fmt.Sprintf("[destroy] post-fail sweep error (non-fatal): %v", sweepErr))
	}
	if report != nil && len(report.Deleted) == 0 && len(report.Skipped) == 0 {
		rep.LogLine("[destroy] post-fail sweep: no CCM orphans found in VPC")
		rep.LogLine("[destroy] VPC blocker is not a k8s-elb-*/k8s-traffic-* SG — investigate manually:")
		rep.LogLine(fmt.Sprintf("[destroy]   aws ec2 describe-security-groups --filters Name=vpc-id,Values=%s --region %s", vpcID, cfg.Region.Value))
		rep.LogLine(fmt.Sprintf("[destroy]   aws ec2 describe-network-interfaces --filters Name=vpc-id,Values=%s Name=status,Values=available --region %s", vpcID, cfg.Region.Value))
	} else {
		logSweepReport(rep, report)
	}
}

func infraDestroyAzure(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	info, err := tools.GetAccountInfo(ctx)
	if err != nil {
		return fmt.Errorf("derive subscription ID: %w", err)
	}
	tenantID := info.TenantID
	if cfg.AzureTenantID.Value != "" {
		tenantID = cfg.AzureTenantID.Value
	}

	stackRG := workspace.AzureStackResourceGroup(cfg.ClusterName.Value)
	location := cfg.AzureLocation.Value
	nodeRG := workspace.AzureNodeResourceGroup(cfg.ClusterName.Value, location)

	var destroyErr error
	var stackDir string
	var ws *workspace.Workspace
	if cfg.Local {
		dir := filepath.Join(cfg.RepoRoot, azureLocalStackDir)
		backendPath := filepath.Join(dir, "backend.tfvars")
		if err := waitAzureBackendThenInit(ctx, cfg, r, rep, dir, backendPath); err != nil {
			return err
		}
		stackDir = dir
	} else {
		w, err := workspace.OpenAzure(ctx, info.ID, cfg.ClusterName.Value)
		if err != nil {
			return err
		}
		ws = w
		if err := w.Lock(ctx); err != nil {
			return err
		}
		defer w.Unlock(ctx)

		if w.Meta.AzureLocation != "" {
			location = w.Meta.AzureLocation
			nodeRG = workspace.AzureNodeResourceGroup(cfg.ClusterName.Value, location)
		}
		if w.Meta.AzureResourceGroup != "" && w.Meta.AzureResourceGroup != stackRG {
			// Prefer recorded stack RG when present (older workspaces may have wrong values).
			rep.LogLine(fmt.Sprintf("[destroy] workspace AzureResourceGroup=%s; using stack RG %s for AKS teardown",
				w.Meta.AzureResourceGroup, stackRG))
		}

		mat, err := w.Materialize(ctx, azureStackAssets(cfg), workspace.Stack)
		if err != nil {
			return err
		}
		defer w.Close()

		backendPath := filepath.Join(mat.StackDir, "backend.tfvars")
		if err := waitAzureBackendThenInit(ctx, cfg, r, rep, mat.StackDir, backendPath); err != nil {
			return err
		}
		stackDir = mat.StackDir
	}

	// Empty app blob containers before terraform destroy (mirrors AWS S3 empty).
	if !cfg.DryRun {
		emptyAzureStackBlobs(ctx, stackDir, rep)
	}

	destroyErr = runCmd(ctx, cfg, r, rep, tools.TerraformDestroy(stackDir, infraDestroyAzureVars(cfg, info.ID, tenantID)))

	// AKS usually deletes MC_* with the cluster; clean up orphans if terraform left them.
	if !cfg.DryRun {
		rep.LogLine(fmt.Sprintf("[destroy] checking for orphaned AKS node resource group %s...", nodeRG))
		if err := metacontainer.DeleteResourceGroupBestEffort(ctx, info.ID, nodeRG); err != nil {
			rep.LogLine(fmt.Sprintf("[destroy] warning: could not delete node RG %s: %v", nodeRG, err))
		} else {
			rep.LogLine(fmt.Sprintf("[destroy] node resource group %s absent or deleted", nodeRG))
		}
	}

	// Mark destroyed when stack teardown succeeds (bootstrap may later delete meta).
	if destroyErr == nil && ws != nil && !cfg.DryRun {
		now := time.Now().UTC()
		ws.Meta.Status = "destroyed"
		ws.Meta.DestroyedAt = &now
		ws.Meta.ClusterARN = ""
		if err := ws.PersistMeta(ctx); err != nil {
			rep.LogLine(fmt.Sprintf("[destroy] warning: could not mark workspace destroyed: %v", err))
		} else {
			rep.LogLine("[destroy] workspace status set to destroyed")
		}
	}

	return destroyErr
}

// emptyAzureStackBlobs empties resource/nodefile containers using terraform outputs
// from an initialized stack directory.
func emptyAzureStackBlobs(ctx context.Context, stackDir string, rep runner.Reporter) {
	account, accErr := tools.TerraformOutput(ctx, stackDir, "resource_storage_account_name")
	key, keyErr := tools.TerraformOutput(ctx, stackDir, "resource_storage_account_key")
	resourceContainer, resErr := tools.TerraformOutput(ctx, stackDir, "resource_container_name")
	nodefileContainer, nodeErr := tools.TerraformOutput(ctx, stackDir, "nodefile_container_name")

	if accErr != nil || account == "" || keyErr != nil || key == "" {
		rep.LogLine(fmt.Sprintf("[cleanup] warning: could not retrieve Azure storage account/key before destroy (account: %v, key: %v)", accErr, keyErr))
		return
	}
	if resErr == nil && resourceContainer != "" {
		if err := emptyAzureBlobContainer(ctx, account, resourceContainer, key, rep); err != nil {
			rep.LogLine(fmt.Sprintf("[cleanup] warning: could not empty resource container %s: %v", resourceContainer, err))
		}
	}
	if nodeErr == nil && nodefileContainer != "" {
		if err := emptyAzureBlobContainer(ctx, account, nodefileContainer, key, rep); err != nil {
			rep.LogLine(fmt.Sprintf("[cleanup] warning: could not empty nodefile container %s: %v", nodefileContainer, err))
		}
	}
}

// waitAzureBackendThenInit waits until the Azure state blob hostname resolves
// via the local system resolver, then runs terraform init.
func waitAzureBackendThenInit(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter, stackDir, backendPath string) error {
	storageAccount, err := parseBackendTFVarsStorageAccount(backendPath)
	if err != nil {
		return err
	}
	rep.SubstepStart("Wait for state backend")
	if !cfg.DryRun {
		if err := waitForAzureStateBlobDNS(ctx, storageAccount, rep); err != nil {
			rep.SubstepFail(err)
			return err
		}
	}
	rep.SubstepDone()
	rep.SubstepStart("Initialize Terraform")
	if err := runCmd(ctx, cfg, r, rep, tools.TerraformInit(stackDir, backendPath)); err != nil {
		rep.SubstepFail(err)
		return err
	}
	rep.SubstepDone()
	return nil
}

// infraAWSVars returns Terraform -var flags for the AWS stack apply.
func infraAWSVars(cfg *config.Config) map[string]string {
	return map[string]string{
		"aws_region":      cfg.Region.Value,
		"cluster_name":    cfg.ClusterName.Value,
		"cluster_version": cfg.K8sVersion.Value,
		"node_instance_types": `["` + cfg.NodeType.Value + `"]`,
		"node_min_size":       cfg.NodeMin.Value,
		"node_desired_size":   cfg.NodeDesired.Value,
		"node_max_size":       cfg.NodeMax.Value,
		"resource_manager_service_account_namespace": "was",
	}
}

// infraDestroyAWSVars returns the minimal Terraform -var flags for destroy.
// Node sizing variables (number type) are omitted: they are irrelevant to
// destruction and would fail if empty, because the destroy path doesn't
// collect them from the user.
func infraDestroyAWSVars(cfg *config.Config) map[string]string {
	return map[string]string{
		"aws_region":   cfg.Region.Value,
		"cluster_name": cfg.ClusterName.Value,
	}
}

// infraAzureVars returns Terraform -var flags for the Azure stack apply.
func infraAzureVars(cfg *config.Config, subscriptionID, tenantID string) map[string]string {
	return map[string]string{
		"subscription_id":     subscriptionID,
		"tenant_id":           tenantID,
		"location":            cfg.AzureLocation.Value,
		"cluster_name":        cfg.ClusterName.Value,
		"cluster_version":     cfg.K8sVersion.Value,
		"node_vm_size":        cfg.NodeType.Value,
		"node_min_count":      cfg.NodeMin.Value,
		"node_desired_count":  cfg.NodeDesired.Value,
		"node_max_count":      cfg.NodeMax.Value,
		"resource_manager_service_account_namespace": "was",
	}
}

// infraDestroyAzureVars returns the minimal Terraform -var flags for Azure destroy.
// Node count variables (number type) are omitted for the same reason as AWS.
func infraDestroyAzureVars(cfg *config.Config, subscriptionID, tenantID string) map[string]string {
	return map[string]string{
		"subscription_id": subscriptionID,
		"tenant_id":       tenantID,
		"location":        cfg.AzureLocation.Value,
		"cluster_name":    cfg.ClusterName.Value,
	}
}

// localStackDir returns the Terraform stack directory for local mode.
func localStackDir(cfg *config.Config) string {
	if cfg.Cloud == "azure" {
		return filepath.Join(cfg.RepoRoot, azureLocalStackDir)
	}
	return filepath.Join(cfg.RepoRoot, stackDir)
}

