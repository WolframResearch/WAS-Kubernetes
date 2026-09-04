package checks

import (
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

// allChecks is the ordered list of all registered doctor checks.
// Environment checks run without a cluster; cluster and application checks
// require rc.Kubeconfig to be set (enforced by each check's Precondition).
var allChecks = []doctor.Check{
	// Environment — AWS
	AWSCredentials{},
	AWSRegion{},
	AWSIAMCreateRole{},
	AWSIAMCreateOIDCProvider{},
	AWSIAMCreatePolicy{},
	AWSQuotaVPC,
	AWSQuotaEIP,
	AWSQuotaEKSClusters,
	AWSQuotaEC2Instances,
	MetaBucketReachable{},
	MetaBucketWritable{},
	MetaBucketLockTable{},
	// Environment — Azure
	AzureCredentials{},
	AzureSubscriptionAccess{},
	AzureCanCreateResourceGroup{},
	AzureCanAssignRoles{},
	AzureQuotaCores{},
	AzureQuotaPublicIPs{},
	AzureQuotaAKSClusters{},
	MetaContainerReachable{},
	MetaContainerWritable{},
	MetaContainerLockBlob{},
	// Environment — CLI + version matrix
	CLITerraform,
	CLIHelm,
	CLIKubectl,
	CLIAWS,
	CLIAz,
	CLIKubelogin,
	VersionMatrix{},

	// Cluster
	ClusterReachable{},
	ClusterUID{},
	ClusterK8sVersion{},
	ClusterNodesReady{},
	ClusterSystemPods{},
	ClusterMetricsServer{},
	ClusterIRSA{},
	ClusterWorkloadIdentity{},

	// Application
	ChartDeployed{},
	ChartValuesValid{},
	AppAWES,
	AppResourceManager,
	AppEndpointManager,
	AppKafka{},
	AppKafkaTopics{},
	AppStorage{},
	AppIngressAddress{},
	AppIngressDNS{},
	AppTLS{},
	AppObjectStorage{},
}

// All returns all registered checks in their canonical order.
func All() []doctor.Check {
	out := make([]doctor.Check, len(allChecks))
	copy(out, allChecks)
	return out
}

// ForCloud returns checks applicable to the given cloud ("aws" or "azure").
// Other-cloud checks are omitted so they do not appear as skips/failures.
func ForCloud(cloud string) []doctor.Check {
	if cloud == "" {
		cloud = "aws"
	}
	var out []doctor.Check
	for _, c := range allChecks {
		if checkAppliesToCloud(c.ID(), cloud) {
			out = append(out, c)
		}
	}
	return out
}

func checkAppliesToCloud(id, cloud string) bool {
	awsOnly := strings.HasPrefix(id, "aws.") ||
		strings.HasPrefix(id, "meta_bucket.") ||
		id == "cli.aws" ||
		id == "cluster.irsa"
	azureOnly := strings.HasPrefix(id, "azure.") ||
		strings.HasPrefix(id, "meta_container.") ||
		id == "cli.az" ||
		id == "cli.kubelogin" ||
		id == "cluster.workload_identity"

	switch cloud {
	case "azure":
		return !awsOnly
	default:
		return !azureOnly
	}
}

// ByCategory returns checks matching cat.
func ByCategory(cat doctor.Category) []doctor.Check {
	var out []doctor.Check
	for _, c := range allChecks {
		if c.Category() == cat {
			out = append(out, c)
		}
	}
	return out
}

// ByID returns the check with the given ID, or nil if not found.
func ByID(id string) doctor.Check {
	for _, c := range allChecks {
		if c.ID() == id {
			return c
		}
	}
	return nil
}
