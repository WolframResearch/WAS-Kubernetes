// Package cloudsweep removes orphaned AWS/Azure resources that Kubernetes
// created (NLBs, EIPs, ENIs, security groups) but that Terraform does not
// manage. These resources block terraform destroy with DependencyViolation
// when Helm releases are not cleanly uninstalled before the infra teardown.
//
// Safety model — a resource is only deleted when ALL of the following hold:
//
//  1. It lives in the target VPC (scope by VpcId / ARN lookup).
//  2. It carries the cluster ownership tag:
//     kubernetes.io/cluster/<clusterName> = owned | shared
//     ENIs are the SOLE exception: four tighter conditions apply instead (see
//     eniIsSweepable).
//  3. It is not an active attachment (ENI rule).
//  4. DryRun=true → print the decision but do NOT call the delete API.
//
// The package never touches resources that do not meet every applicable
// criterion, and it logs its decision (including skips) for every resource it
// inspects.
package cloudsweep

// SweepOptions configures a resource sweep.
type SweepOptions struct {
	// ClusterName is the EKS/AKS cluster name; used to match the cluster tag.
	ClusterName string

	// VPCID restricts sweep scope. Every non-ENI resource must be in this VPC.
	VPCID string

	// Region is the AWS region (e.g. "us-east-1").
	Region string

	// DryRun prints what would be deleted without issuing any delete API calls.
	DryRun bool
}

// ResourceEntry records the sweep decision for one inspected resource.
type ResourceEntry struct {
	// Type is one of: "load-balancer-v2", "classic-load-balancer",
	// "target-group", "elastic-ip", "eni", "security-group".
	Type string

	// ID is the ARN, allocation ID, ENI ID, or SG ID.
	ID string

	// Desc is an optional human-readable detail (LB name, ENI description…).
	Desc string

	// Action is one of: "deleted", "skipped", "dry-run-would-delete".
	Action string

	// Reason explains the ownership decision.
	Reason string
}

// SweepReport holds the full audit of what was touched and what was skipped.
type SweepReport struct {
	Deleted []ResourceEntry
	Skipped []ResourceEntry
}

func (r *SweepReport) record(deleted bool, e ResourceEntry) {
	if deleted {
		r.Deleted = append(r.Deleted, e)
	} else {
		r.Skipped = append(r.Skipped, e)
	}
}
