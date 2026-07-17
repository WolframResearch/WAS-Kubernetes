package versions

import (
	"fmt"

	"github.com/WolframResearch/WAS-Kubernetes/internal/versions/parsers"
)

// Version is a type alias so callers don't need to import parsers directly.
type Version = parsers.Version

// VersionRange defines the minimum and maximum (inclusive) supported versions.
type VersionRange struct {
	Min Version
	Max Version
}

// InRange returns true if v is within [Min, Max] inclusive.
func (r VersionRange) InRange(v Version) bool {
	return !v.LessThan(r.Min) && !v.GreaterThan(r.Max)
}

// String returns a human-readable description of the range.
func (r VersionRange) String() string {
	return fmt.Sprintf("%s – %s", r.Min.String(), r.Max.String())
}

// Matrix describes the supported version ranges for all wasctl components.
// Add new fields here when a component is added to the compatibility matrix;
// update CurrentMatrix and the README table in the same commit.
type Matrix struct {
	// WasctlVersion is informational — wasctl does not check its own version
	// against the matrix.
	WasctlVersion string

	Helm      VersionRange
	Kubectl   VersionRange
	Terraform VersionRange

	// AWSCLI: upper bound is 2.99.99 ("any 2.x"). AWS has stayed in 2.x since
	// 2020 and is not signalling a 3.x release. When 3.x ships, do NOT
	// auto-extend this range — review breaking changes and bump deliberately.
	AWSCLI VersionRange

	// AzureCLI: upper bound is 2.99.99 ("any 2.x"). Microsoft has been in 2.x
	// since 2018 and is not signalling a 3.x release. When 3.x ships, do NOT
	// auto-extend this range — review breaking changes and bump deliberately.
	AzureCLI VersionRange

	// Kubernetes is the cluster server version (not the kubectl client).
	Kubernetes VersionRange

	// Strimzi is the cluster operator version. The chart pins ~0.44.0 (0.44.x);
	// customers on a different major patch are already outside the chart's tested
	// config. Bump the upper bound when the chart pin advances.
	// Note: 0.43.x crashes on Kubernetes 1.33+ — fabric8 6.13.3 rejects the new
	// emulationMajor field. 0.44.0+ bundles fabric8 6.13.4+ which tolerates it.
	// See docs/Troubleshooting.md#14-strimzi-cluster-operator-crashloopbackoff-on-k8s-133
	Strimzi VersionRange

	// Chart is the WAS Helm chart version. Critical if below min (chart too old
	// for this wasctl binary), Warning if above max (chart newer than tested).
	Chart VersionRange

	// AWSProvider is the Terraform hashicorp/aws provider version.
	// Detected only after `terraform init` has been run in the stack directory.
	AWSProvider VersionRange

	// AzureProvider is the Terraform hashicorp/azurerm provider version.
	// Detected only after `terraform init` has been run in the stack directory.
	AzureProvider VersionRange

	// EKSModule (terraform-aws-modules/eks) is not version-checked here.
	// Detecting module versions requires parsing the lock file or running
	// `terraform providers lock`, which is environment-dependent.
}

// CurrentMatrix is the canonical compatibility matrix for this wasctl release.
// Update this table, the README, and Chart.yaml's kubeVersion in the same PR
// whenever bounds change.
var CurrentMatrix = Matrix{
	WasctlVersion: "1.2.0",
	Helm:          mustRange("3.10.0", "3.17.99"),
	Kubectl:       mustRange("1.28.0", "1.36.99"),
	Terraform:     mustRange("1.9.0", "1.15.99"),
	AWSCLI:        mustRange("2.13.0", "2.99.99"),
	AzureCLI:      mustRange("2.50.0", "2.99.99"),
	Kubernetes:    mustRange("1.30.0", "1.36.99"),
	Strimzi:       mustRange("0.44.0", "0.49.99"),
	Chart:         mustRange("0.1.0", "1.99.99"),
	AWSProvider:   mustRange("5.50.0", "6.99.99"),
	AzureProvider: mustRange("3.90.0", "4.99.99"),
}

// mustRange parses min and max; panics on error so misconfiguration is caught at
// program startup rather than silently being ignored.
func mustRange(min, max string) VersionRange {
	mn, err := parsers.Parse(min)
	if err != nil {
		panic("versions: bad min range " + min + ": " + err.Error())
	}
	mx, err := parsers.Parse(max)
	if err != nil {
		panic("versions: bad max range " + max + ": " + err.Error())
	}
	return VersionRange{Min: mn, Max: mx}
}
