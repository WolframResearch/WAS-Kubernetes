package addons

import (
	"context"
	"fmt"
)

// NewEBSCSIDriver returns the AWS EBS CSI driver add-on.
// Required for Kafka (and any other block PVCs): modern EKS no longer includes
// the in-tree kubernetes.io/aws-ebs provisioner that StorageClass "gp2" referenced.
func NewEBSCSIDriver() *ebsCSIDriver {
	return &ebsCSIDriver{}
}

type ebsCSIDriver struct{}

func (e *ebsCSIDriver) Name() string         { return "aws-ebs-csi-driver" }
func (e *ebsCSIDriver) Clouds() []string     { return []string{"aws"} }
func (e *ebsCSIDriver) Namespaces() []string { return nil }

func (e *ebsCSIDriver) Check(ctx context.Context, rc *RunContext) (State, error) {
	return e.component(rc).Check(ctx, rc)
}

func (e *ebsCSIDriver) Install(ctx context.Context, rc *RunContext) error {
	if rc.EBSCSIRoleARN == "" {
		return fmt.Errorf("aws-ebs-csi-driver: EBSCSIRoleARN not set (run infra stage first)")
	}
	return e.component(rc).Install(ctx, rc)
}

func (e *ebsCSIDriver) Verify(ctx context.Context, rc *RunContext) error {
	return e.component(rc).Verify(ctx, rc)
}

func (e *ebsCSIDriver) Uninstall(ctx context.Context, rc *RunContext) error {
	return e.component(rc).Uninstall(ctx, rc)
}

func (e *ebsCSIDriver) component(rc *RunContext) *HelmComponent {
	return &HelmComponent{
		ReleaseName: "aws-ebs-csi-driver",
		ChartRef:    "aws-ebs-csi-driver/aws-ebs-csi-driver",
		// Pin to a current 2.50.x band (in-repo ~2.39.0 was too stale vs chart index).
		Version:   "~2.50.0",
		Namespace: "kube-system",
		RepoName:  "aws-ebs-csi-driver",
		RepoURL:   "https://kubernetes-sigs.github.io/aws-ebs-csi-driver",
		Values: map[string]string{
			`controller.serviceAccount.annotations.eks\.amazonaws\.com/role-arn`: rc.EBSCSIRoleARN,
		},
		clouds: []string{"aws"},
		// Names from aws-ebs-csi-driver chart 2.50.x templates.
		OrphanedResourceCheck: []OrphanedResource{
			{Kind: "CSIDriver", Name: "ebs.csi.aws.com"},
			{Kind: "ClusterRole", Name: "ebs-external-provisioner-role"},
			{Kind: "ClusterRole", Name: "ebs-external-attacher-role"},
			{Kind: "ClusterRoleBinding", Name: "ebs-csi-provisioner-binding"},
			{Kind: "ClusterRoleBinding", Name: "ebs-csi-attacher-binding"},
		},
	}
}
