package addons

import (
	"context"
	"fmt"
	"strings"
)

// ── EFS (AWS) ─────────────────────────────────────────────────────────────────

// EFSStorageClass creates the was-efs StorageClass after the EFS CSI driver
// is healthy. AWS-only; depends on EFSFilesystemID being set in RunContext.
type EFSStorageClass struct{}

func (e *EFSStorageClass) Name() string       { return "was-efs-storageclass" }
func (e *EFSStorageClass) Clouds() []string   { return []string{"aws"} }
func (e *EFSStorageClass) Namespaces() []string { return nil }

func (e *EFSStorageClass) Check(ctx context.Context, rc *RunContext) (State, error) {
	_, err := rc.Runner.Output(ctx, append([]string{
		"kubectl", "get", "storageclass", "was-efs",
	}, rc.kubectlFlags()...), nil)
	if err != nil {
		return StateNotInstalled, nil
	}
	return StateHealthy, nil
}

func (e *EFSStorageClass) Install(ctx context.Context, rc *RunContext) error {
	state, err := e.Check(ctx, rc)
	if err != nil {
		return err
	}
	if state == StateHealthy {
		rc.Reporter.LogLine("[addons] was-efs StorageClass: already present — skipping")
		return nil
	}
	if rc.EFSFilesystemID == "" {
		return fmt.Errorf("was-efs StorageClass: EFSFilesystemID not set (run infra stage first)")
	}
	manifest := fmt.Sprintf(`apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: was-efs
provisioner: efs.csi.aws.com
parameters:
  provisioningMode: efs-ap
  fileSystemId: %s
  directoryPerms: "700"
  basePath: /was
reclaimPolicy: Delete
volumeBindingMode: Immediate
`, rc.EFSFilesystemID)
	return applyManifest(ctx, rc, manifest)
}

func (e *EFSStorageClass) Verify(ctx context.Context, rc *RunContext) error {
	state, err := e.Check(ctx, rc)
	if err != nil {
		return err
	}
	if state != StateHealthy {
		return fmt.Errorf("was-efs StorageClass not found after apply")
	}
	return nil
}

func (e *EFSStorageClass) Uninstall(ctx context.Context, rc *RunContext) error {
	return rc.Runner.Run(ctx, append([]string{
		"kubectl", "delete", "storageclass", "was-efs", "--ignore-not-found",
	}, rc.kubectlFlags()...), nil, rc.Reporter)
}

// ── Azure Files ───────────────────────────────────────────────────────────────

const (
	azureFileSecretName      = "was-azurefile-account"
	azureFileSecretNamespace = "kube-system"
)

// AzureFileStorageClass creates the was-azurefile StorageClass wired to the
// Terraform-provisioned Azure Files storage account (filesystem.tf).
//
// A bare StorageClass with only skuName makes the CSI driver try to create a
// storage account in the AKS node RG (MC_*), which fails with AuthorizationFailed
// unless the kubelet identity has Storage Account Contributor there. Instead we
// point at the explicit account and pass its key via a Secret.
type AzureFileStorageClass struct{}

func (a *AzureFileStorageClass) Name() string         { return "was-azurefile-storageclass" }
func (a *AzureFileStorageClass) Clouds() []string     { return []string{"azure"} }
func (a *AzureFileStorageClass) Namespaces() []string { return []string{azureFileSecretNamespace} }

func (a *AzureFileStorageClass) Check(ctx context.Context, rc *RunContext) (State, error) {
	out, err := rc.Runner.Output(ctx, append([]string{
		"kubectl", "get", "storageclass", "was-azurefile",
		"-o", "jsonpath={.parameters.storageAccount}",
	}, rc.kubectlFlags()...), nil)
	if err != nil {
		return StateNotInstalled, nil
	}
	got := strings.TrimSpace(string(out))
	if got == "" || (rc.AzureFilesystemAccount != "" && got != rc.AzureFilesystemAccount) {
		// Present but not wired to the Terraform account — treat as failed so
		// Install recreates it.
		return StateFailed, nil
	}
	return StateHealthy, nil
}

func (a *AzureFileStorageClass) Install(ctx context.Context, rc *RunContext) error {
	if rc.AzureFilesystemAccount == "" || rc.AzureFilesystemKey == "" || rc.AzureFilesystemRG == "" {
		return fmt.Errorf("was-azurefile StorageClass: filesystem account/key/RG not set (run infra stage first)")
	}
	sku := rc.AzureFilesystemSKU
	if sku == "" {
		sku = "Premium_LRS"
	}

	state, err := a.Check(ctx, rc)
	if err != nil {
		return err
	}
	if state == StateHealthy {
		rc.Reporter.LogLine("[addons] was-azurefile StorageClass: already wired to " + rc.AzureFilesystemAccount + " — skipping")
		return nil
	}
	if state == StateFailed {
		rc.Reporter.LogLine("[addons] was-azurefile StorageClass: present but not wired to Terraform account — recreating")
		_ = a.Uninstall(ctx, rc)
	}

	if err := ensureNamespace(ctx, rc, azureFileSecretNamespace); err != nil {
		return err
	}

	secret := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
type: Opaque
stringData:
  azurestorageaccountname: %q
  azurestorageaccountkey: %q
`, azureFileSecretName, azureFileSecretNamespace, rc.AzureFilesystemAccount, rc.AzureFilesystemKey)
	if err := applyManifest(ctx, rc, secret); err != nil {
		return fmt.Errorf("was-azurefile account secret: %w", err)
	}

	manifest := fmt.Sprintf(`apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: was-azurefile
provisioner: file.csi.azure.com
parameters:
  skuName: %s
  storageAccount: %s
  resourceGroup: %s
  useDataPlaneAPI: "true"
  csi.storage.k8s.io/provisioner-secret-name: %s
  csi.storage.k8s.io/provisioner-secret-namespace: %s
  csi.storage.k8s.io/node-stage-secret-name: %s
  csi.storage.k8s.io/node-stage-secret-namespace: %s
mountOptions:
  - dir_mode=0777
  - file_mode=0777
  - uid=0
  - gid=0
  - mfsymlinks
  - cache=strict
  - actimeo=30
allowVolumeExpansion: true
reclaimPolicy: Delete
volumeBindingMode: Immediate
`, sku, rc.AzureFilesystemAccount, rc.AzureFilesystemRG,
		azureFileSecretName, azureFileSecretNamespace,
		azureFileSecretName, azureFileSecretNamespace)

	rc.Reporter.LogLine(fmt.Sprintf(
		"[addons] was-azurefile: using account %s in %s (sku %s) — not the AKS node RG",
		rc.AzureFilesystemAccount, rc.AzureFilesystemRG, sku,
	))
	return applyManifest(ctx, rc, manifest)
}

func (a *AzureFileStorageClass) Uninstall(ctx context.Context, rc *RunContext) error {
	_ = rc.Runner.Run(ctx, append([]string{
		"kubectl", "delete", "storageclass", "was-azurefile", "--ignore-not-found",
	}, rc.kubectlFlags()...), nil, rc.Reporter)
	_ = rc.Runner.Run(ctx, append([]string{
		"kubectl", "delete", "secret", azureFileSecretName,
		"-n", azureFileSecretNamespace, "--ignore-not-found",
	}, rc.kubectlFlags()...), nil, rc.Reporter)
	return nil
}

func (a *AzureFileStorageClass) Verify(ctx context.Context, rc *RunContext) error {
	state, err := a.Check(ctx, rc)
	if err != nil {
		return err
	}
	if state != StateHealthy {
		return fmt.Errorf("was-azurefile StorageClass not wired to filesystem account after apply")
	}
	return nil
}

// AzureKafkaStorageClass creates the kafka-standardssd-xfs StorageClass.
type AzureKafkaStorageClass struct{}

func (a *AzureKafkaStorageClass) Name() string       { return "was-azure-kafka-storageclass" }
func (a *AzureKafkaStorageClass) Clouds() []string   { return []string{"azure"} }
func (a *AzureKafkaStorageClass) Namespaces() []string { return nil }

func (a *AzureKafkaStorageClass) Check(ctx context.Context, rc *RunContext) (State, error) {
	_, err := rc.Runner.Output(ctx, append([]string{
		"kubectl", "get", "storageclass", "kafka-standardssd-xfs",
	}, rc.kubectlFlags()...), nil)
	if err != nil {
		return StateNotInstalled, nil
	}
	return StateHealthy, nil
}

func (a *AzureKafkaStorageClass) Install(ctx context.Context, rc *RunContext) error {
	state, err := a.Check(ctx, rc)
	if err != nil {
		return err
	}
	if state == StateHealthy {
		rc.Reporter.LogLine("[addons] kafka-standardssd-xfs StorageClass: already present — skipping")
		return nil
	}
	const manifest = `apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: kafka-standardssd-xfs
provisioner: disk.csi.azure.com
allowVolumeExpansion: true
parameters:
  skuname: StandardSSD_LRS
  kind: Managed
  cachingMode: None
  fsType: xfs
reclaimPolicy: Delete
volumeBindingMode: WaitForFirstConsumer
`
	return applyManifest(ctx, rc, manifest)
}

func (a *AzureKafkaStorageClass) Uninstall(ctx context.Context, rc *RunContext) error {
	return rc.Runner.Run(ctx, append([]string{
		"kubectl", "delete", "storageclass", "kafka-standardssd-xfs", "--ignore-not-found",
	}, rc.kubectlFlags()...), nil, rc.Reporter)
}

func (a *AzureKafkaStorageClass) Verify(ctx context.Context, rc *RunContext) error {
	state, err := a.Check(ctx, rc)
	if err != nil {
		return err
	}
	if state != StateHealthy {
		return fmt.Errorf("kafka-standardssd-xfs StorageClass not found after apply")
	}
	return nil
}

// ── AWS Kafka (EBS gp3) ───────────────────────────────────────────────────────

const awsKafkaStorageClassName = "was-kafka-gp3"

// AWSKafkaStorageClass creates was-kafka-gp3 for Strimzi broker volumes.
// Uses ebs.csi.aws.com (not the removed in-tree kubernetes.io/aws-ebs / "gp2").
type AWSKafkaStorageClass struct{}

func (a *AWSKafkaStorageClass) Name() string         { return "was-aws-kafka-storageclass" }
func (a *AWSKafkaStorageClass) Clouds() []string     { return []string{"aws"} }
func (a *AWSKafkaStorageClass) Namespaces() []string { return nil }

func (a *AWSKafkaStorageClass) Check(ctx context.Context, rc *RunContext) (State, error) {
	out, err := rc.Runner.Output(ctx, append([]string{
		"kubectl", "get", "storageclass", awsKafkaStorageClassName,
		"-o", "jsonpath={.provisioner}",
	}, rc.kubectlFlags()...), nil)
	if err != nil {
		return StateNotInstalled, nil
	}
	if strings.TrimSpace(string(out)) != "ebs.csi.aws.com" {
		return StateFailed, nil
	}
	return StateHealthy, nil
}

func (a *AWSKafkaStorageClass) Install(ctx context.Context, rc *RunContext) error {
	state, err := a.Check(ctx, rc)
	if err != nil {
		return err
	}
	if state == StateHealthy {
		rc.Reporter.LogLine("[addons] was-kafka-gp3 StorageClass: already present — skipping")
		return nil
	}
	if state == StateFailed {
		rc.Reporter.LogLine("[addons] was-kafka-gp3 StorageClass: wrong provisioner — recreating")
		_ = a.Uninstall(ctx, rc)
	}
	const manifest = `apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: was-kafka-gp3
provisioner: ebs.csi.aws.com
allowVolumeExpansion: true
parameters:
  type: gp3
  fsType: xfs
reclaimPolicy: Delete
volumeBindingMode: WaitForFirstConsumer
`
	return applyManifest(ctx, rc, manifest)
}

func (a *AWSKafkaStorageClass) Uninstall(ctx context.Context, rc *RunContext) error {
	return rc.Runner.Run(ctx, append([]string{
		"kubectl", "delete", "storageclass", awsKafkaStorageClassName, "--ignore-not-found",
	}, rc.kubectlFlags()...), nil, rc.Reporter)
}

func (a *AWSKafkaStorageClass) Verify(ctx context.Context, rc *RunContext) error {
	state, err := a.Check(ctx, rc)
	if err != nil {
		return err
	}
	if state != StateHealthy {
		return fmt.Errorf("was-kafka-gp3 StorageClass not found after apply")
	}
	return nil
}

