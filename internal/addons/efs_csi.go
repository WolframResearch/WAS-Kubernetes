package addons

import (
	"context"
	"fmt"
)

// NewEFSCSIDriver returns the AWS EFS CSI driver add-on.
// The controller service account is annotated with the IRSA role ARN from
// RunContext.EFSCSIRoleARN; Install validates it is non-empty.
func NewEFSCSIDriver() *efsCSIDriver {
	return &efsCSIDriver{}
}

type efsCSIDriver struct{}

func (e *efsCSIDriver) Name() string       { return "aws-efs-csi-driver" }
func (e *efsCSIDriver) Clouds() []string   { return []string{"aws"} }
func (e *efsCSIDriver) Namespaces() []string { return nil }

func (e *efsCSIDriver) Check(ctx context.Context, rc *RunContext) (State, error) {
	h := e.component(rc)
	return h.Check(ctx, rc)
}

func (e *efsCSIDriver) Install(ctx context.Context, rc *RunContext) error {
	if rc.EFSCSIRoleARN == "" {
		return fmt.Errorf("aws-efs-csi-driver: EFSCSIRoleARN not set (run infra stage first)")
	}
	h := e.component(rc)
	return h.Install(ctx, rc)
}

func (e *efsCSIDriver) Verify(ctx context.Context, rc *RunContext) error {
	h := e.component(rc)
	return h.Verify(ctx, rc)
}

func (e *efsCSIDriver) Uninstall(ctx context.Context, rc *RunContext) error {
	h := e.component(rc)
	return h.Uninstall(ctx, rc)
}

// component builds a HelmComponent using the role ARN from rc, called at
// Install time when rc.EFSCSIRoleARN is guaranteed to be populated.
func (e *efsCSIDriver) component(rc *RunContext) *HelmComponent {
	return &HelmComponent{
		ReleaseName: "aws-efs-csi-driver",
		ChartRef:    "aws-efs-csi-driver/aws-efs-csi-driver",
		Version:     "~3.0.0",
		Namespace:   "kube-system",
		RepoName:    "aws-efs-csi-driver",
		RepoURL:     "https://kubernetes-sigs.github.io/aws-efs-csi-driver",
		Values: map[string]string{
			// annotation key contains a literal dot: must be double-quoted for helm --set
			`controller.serviceAccount.annotations.eks\.amazonaws\.com/role-arn`: rc.EFSCSIRoleARN,
		},
		clouds: []string{"aws"},
		// Derived from: helm template aws-efs-csi-driver/aws-efs-csi-driver --version ~3.0.0
		OrphanedResourceCheck: []OrphanedResource{
			{Kind: "ClusterRole", Name: "aws-efs-csi-external-provisioner-role"},
			{Kind: "ClusterRole", Name: "aws-efs-csi-external-provisioner-role-leaderelection"},
			{Kind: "ClusterRoleBinding", Name: "aws-efs-csi-external-provisioner-role-binding"},
			{Kind: "ClusterRoleBinding", Name: "aws-efs-csi-external-provisioner-role-leaderelection-binding"},
			{Kind: "CSIDriver", Name: "efs.csi.aws.com"},
		},
	}
}
