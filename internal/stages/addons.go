package stages

import (
	"context"
	"fmt"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/addons"
	"github.com/WolframResearch/WAS-Kubernetes/internal/cloudsweep"
	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

// Addons installs cluster-level prerequisites idempotently via the addons
// package (same Go path for AWS and Azure).
//
// AWS: ingress-nginx, EFS CSI, was-efs, EBS CSI, was-kafka-gp3, Strimzi,
//
//	metrics-server, kube-prometheus-stack, prometheus-adapter, cert-manager.
//
// Azure: ingress-nginx, was-azurefile StorageClass, Strimzi,
//
//	metrics-server, kube-prometheus-stack, prometheus-adapter, cert-manager.
//	(Azure Files CSI is built into AKS; no chart install needed.)
type Addons struct{}

func (Addons) Name() string         { return "addons" }
func (Addons) Description() string  { return "Cluster add-ons installed" }
func (Addons) EstimateText() string { return "~5 min" }
func (Addons) Label() string        { return "Installing cluster add-ons" }
func (Addons) DestroyLabel() string { return "Removing cluster add-ons" }
func (Addons) Substeps() []string {
	return []string{
		"Open workspace",
		"ingress-nginx",
		"aws-efs-csi-driver",
		"was-efs-storageclass",
		"aws-ebs-csi-driver",
		"was-aws-kafka-storageclass",
		"was-azurefile-storageclass",
		"was-azure-kafka-storageclass",
		"strimzi-kafka-operator",
		"metrics-server",
		"kube-prometheus-stack",
		"prometheus-adapter",
		"cert-manager",
	}
}

// Check validates that required cluster add-ons are healthy. Criteria must
// match InstallAll: metrics-server may be the AKS/EKS built-in addon (no helm
// release), and the cloud-specific StorageClass must exist.
func (Addons) Check(ctx context.Context, cfg *config.Config) error {
	accountID, err := cloudAccountID(ctx, cfg)
	if err != nil {
		return fmt.Errorf("addons check: %w", err)
	}
	w, err := workspace.OpenForCloud(ctx, cfg, accountID, cfg.ClusterName.Value)
	if err != nil {
		return fmt.Errorf("addons check: %w", err)
	}
	if err := w.MaterializeTempDir(); err != nil {
		return fmt.Errorf("addons check: %w", err)
	}
	defer w.Close()

	kubeconfigPath, err := w.Kubeconfig(ctx)
	if err != nil {
		return fmt.Errorf("addons check: generate kubeconfig: %w", err)
	}
	contextName := w.ContextName()

	rc := &addons.RunContext{
		Cfg:         cfg,
		Kubeconfig:  kubeconfigPath,
		KubeContext: contextName,
		Runner:      runner.ExecRunner{},
		Reporter:    silentReporter{},
	}

	required := []addons.Addon{
		addons.NewIngressNginx(),
		addons.NewStrimzi(),
		addons.NewMetricsServer(),
	}
	if cfg.Cloud == "azure" {
		required = append(required, &addons.AzureFileStorageClass{})
	} else {
		required = append(required,
			&addons.EFSStorageClass{},
			&addons.AWSKafkaStorageClass{},
		)
	}

	for _, a := range required {
		state, err := a.Check(ctx, rc)
		if err != nil {
			return fmt.Errorf("addons check %s: %w", a.Name(), err)
		}
		if state != addons.StateHealthy {
			return fmt.Errorf("addon %s not healthy (state=%d)", a.Name(), state)
		}
	}
	return nil
}

// silentReporter discards progress output (used by Check-only paths).
type silentReporter struct{}

func (silentReporter) SubstepStart(string) {}
func (silentReporter) SubstepDone()         {}
func (silentReporter) SubstepFail(error)    {}
func (silentReporter) LogLine(string)       {}

func (Addons) Apply(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	accountID, err := cloudAccountID(ctx, cfg)
	if err != nil {
		return fmt.Errorf("derive account ID: %w", err)
	}

	rep.SubstepStart("Open workspace")
	w, err := workspace.OpenForCloud(ctx, cfg, accountID, cfg.ClusterName.Value)
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
	defer w.Close()

	stackTFDir, err := prepareStackTF(ctx, cfg, w, r, rep)
	if err != nil {
		rep.SubstepFail(err)
		return err
	}

	kubeconfigPath, err := w.Kubeconfig(ctx)
	if err != nil {
		rep.SubstepFail(err)
		return err
	}
	contextName := w.ContextName()
	rep.LogLine(fmt.Sprintf("[✓] Using isolated kubeconfig for cluster %s", cfg.ClusterName.Value))
	rep.SubstepDone()

	rc := &addons.RunContext{
		Cfg:         cfg,
		Kubeconfig:  kubeconfigPath,
		KubeContext: contextName,
		Runner:      r,
		Reporter:    rep,
	}

	if cfg.Cloud == "aws" {
		efsID, err := requireTFOutput(ctx, stackTFDir, "efs_filesystem_id")
		if err != nil {
			return err
		}
		efsCsiRole, err := requireTFOutput(ctx, stackTFDir, "efs_csi_driver_role_arn")
		if err != nil {
			return err
		}
		ebsCsiRole, err := requireTFOutput(ctx, stackTFDir, "ebs_csi_driver_role_arn")
		if err != nil {
			return err
		}
		rc.EFSFilesystemID = efsID
		rc.EFSCSIRoleARN = efsCsiRole
		rc.EBSCSIRoleARN = ebsCsiRole
	}

	if cfg.Cloud == "azure" {
		fsAccount, err := requireTFOutput(ctx, stackTFDir, "filesystem_storage_account_name")
		if err != nil {
			return err
		}
		fsKey, err := requireTFOutput(ctx, stackTFDir, "filesystem_storage_account_key")
		if err != nil {
			return err
		}
		rc.AzureFilesystemAccount = fsAccount
		rc.AzureFilesystemKey = fsKey
		rc.AzureFilesystemRG = workspace.AzureStackResourceGroup(cfg.ClusterName.Value)
		// Match infra/azure/stack default filesystem_tier=Premium (Premium FileStorage → Premium_LRS).
		rc.AzureFilesystemSKU = "Premium_LRS"
		if sku, skuErr := tools.TerraformOutput(ctx, stackTFDir, "filesystem_sku_name"); skuErr == nil && sku != "" {
			rc.AzureFilesystemSKU = sku
		}
	}

	installer := addons.NewInstaller(rc)
	list := addons.AddonsFor(cfg.Cloud)
	if len(list) == 0 {
		return fmt.Errorf("no add-ons registered for cloud %q", cfg.Cloud)
	}
	ensureAWSCertManagerSkip(cfg, rep)
	if err := installer.InstallAll(ctx, list); err != nil {
		return err
	}

	// Azure: ingress-nginx publishes a free *.cloudapp.azure.com FQDN via
	// azure-dns-label-name. Surface it clearly — Ingress host must be DNS, not IP.
	if cfg.Cloud == "azure" && rc.AzureIngressFQDN != "" {
		rep.LogLine(fmt.Sprintf("[✓] Azure ingress FQDN: %s", rc.AzureIngressFQDN))
		rep.LogLine("    Use this as --ingress-host (or leave unset for auto-detect). Do not use the LB IP.")
		if cfg.IngressHost.Value == "" {
			cfg.IngressHost = config.Field[string]{Value: rc.AzureIngressFQDN, Source: "auto:azure-dns-label"}
		}
	}

	// Re-check with the same criteria as Check so we never report success while
	// status still shows addons pending (e.g. metrics-server helm vs AKS addon).
	rep.SubstepStart("Verify required add-ons")
	if err := (Addons{}).Check(ctx, cfg); err != nil {
		rep.SubstepFail(err)
		return fmt.Errorf("addons installed but verification failed: %w", err)
	}
	rep.SubstepDone()
	return nil
}

func (Addons) Destroy(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	accountID, err := cloudAccountID(ctx, cfg)
	if err != nil {
		rep.LogLine(fmt.Sprintf("[addons] cannot derive account ID: %v — skipping addon uninstall", err))
		return nil
	}

	w, err := workspace.OpenForCloud(ctx, cfg, accountID, cfg.ClusterName.Value)
	if err != nil {
		rep.LogLine(fmt.Sprintf("[addons] cannot open workspace: %v — skipping addon uninstall", err))
		return nil
	}
	if err := w.MaterializeTempDir(); err != nil {
		rep.LogLine(fmt.Sprintf("[addons] cannot create temp dir: %v — skipping addon uninstall", err))
		return nil
	}
	defer w.Close()

	kubeconfigPath, err := w.Kubeconfig(ctx)
	if err != nil {
		rep.LogLine(fmt.Sprintf("[addons] cannot get kubeconfig: %v — skipping addon uninstall", err))
		return nil
	}
	contextName := w.ContextName()

	rc := &addons.RunContext{
		Cfg:         cfg,
		Kubeconfig:  kubeconfigPath,
		KubeContext: contextName,
		Runner:      r,
		Reporter:    rep,
	}

	// Uninstall addons in reverse install order. ingress-nginx is last, which
	// means its CCM-owned NLB starts deleting after all other addons are gone.
	rep.SubstepStart("Uninstall cluster add-ons")
	addons.NewInstaller(rc).UninstallAll(ctx, addons.AddonsFor(cfg.Cloud))
	rep.SubstepDone()

	// AWS-only: wait for CCM to finish deleting the NLB and associated resources,
	// then sweep any orphans so terraform destroy can proceed without errors.
	if cfg.Cloud == "aws" {
		rep.SubstepStart("Wait for AWS resource cleanup")
		vpcID, discoverErr := cloudsweep.DiscoverVPCID(ctx, cfg.Region.Value, cfg.ClusterName.Value)
		if discoverErr != nil {
			// The kubernetes.io/cluster/ tag may have been removed by the CCM
			// during partial teardown. The infra stage will attempt its own
			// sweep using the terraform vpc_id output as a second chance.
			rep.LogLine(fmt.Sprintf("[sweep] VPC discovery failed for cluster %q in %s: %v",
				cfg.ClusterName.Value, cfg.Region.Value, discoverErr))
			rep.LogLine("[sweep] proceeding without addons-stage sweep; the infra stage will retry")
			rep.LogLine("[sweep] if destroy later hangs on DependencyViolation, the VPC sweep was skipped on both paths")
			rep.SubstepDone()
			return nil
		}

		sweeper, sweepErr := cloudsweep.NewAWSSweeper(ctx, cloudsweep.SweepOptions{
			ClusterName: cfg.ClusterName.Value,
			VPCID:       vpcID,
			Region:      cfg.Region.Value,
			DryRun:      cfg.DryRun,
		}, rep)
		if sweepErr != nil {
			rep.LogLine(fmt.Sprintf("[sweep] cannot initialise sweeper: %v — proceeding without sweep", sweepErr))
			rep.SubstepDone()
			return nil
		}

		// Poll until the NLB is gone (CCM cleanup) or 5 min timeout.
		// The infra stage's terraform destroy will handle any remaining cleanup;
		// if CCM-created SGs block the VPC, the post-destroy sweep handles them.
		if waitErr := sweeper.WaitForCleanup(ctx, 5*time.Minute); waitErr != nil {
			rep.LogLine(fmt.Sprintf("[sweep] %v — proceeding to terraform destroy", waitErr))
		}
		rep.SubstepDone()
	}
	return nil
}

func logSweepReport(rep runner.Reporter, report *cloudsweep.SweepReport) {
	if report == nil {
		return
	}
	rep.LogLine(fmt.Sprintf("[sweep] deleted: %d, skipped: %d",
		len(report.Deleted), len(report.Skipped)))
	for _, e := range report.Deleted {
		rep.LogLine(fmt.Sprintf("[sweep]   %-22s %s — %s", e.Type, e.ID, e.Reason))
	}
	for _, e := range report.Skipped {
		rep.LogLine(fmt.Sprintf("[sweep]   SKIPPED %-14s %s — %s", e.Type, e.ID, e.Reason))
	}
}
