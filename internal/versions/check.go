package versions

import (
	"context"

	"github.com/WolframResearch/WAS-Kubernetes/internal/versions/parsers"
)

// IssueSeverity classifies how serious a version incompatibility is.
type IssueSeverity int

const (
	// IssueSeverityWarning means the version exceeds the known-good maximum
	// — it may work but has not been validated.
	IssueSeverityWarning IssueSeverity = iota
	// IssueSeverityCritical means the version is below the minimum — it is
	// likely incompatible and deployment should be blocked.
	IssueSeverityCritical
)

// Issue describes a single version incompatibility.
type Issue struct {
	Component      string
	CurrentVer     Version
	SupportedRange VersionRange
	Severity       IssueSeverity
	Remediation    string
}

// componentDef maps a component key to its matrix range and remediation hint.
type componentDef struct {
	key         string
	rang        func(m Matrix) VersionRange
	remediation string
}

var componentDefs = []componentDef{
	{
		key:         "helm",
		rang:        func(m Matrix) VersionRange { return m.Helm },
		remediation: "Install the supported version: https://helm.sh/docs/intro/install/",
	},
	{
		key:         "kubectl",
		rang:        func(m Matrix) VersionRange { return m.Kubectl },
		remediation: "Install the supported version: https://kubernetes.io/docs/tasks/tools/",
	},
	{
		key:         "terraform",
		rang:        func(m Matrix) VersionRange { return m.Terraform },
		remediation: "Use tfenv or the official installer: https://developer.hashicorp.com/terraform/downloads",
	},
	{
		key:         "aws-cli",
		rang:        func(m Matrix) VersionRange { return m.AWSCLI },
		remediation: "Upgrade with: pip install --upgrade awscli  or  https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html",
	},
	{
		key:         "azure-cli",
		rang:        func(m Matrix) VersionRange { return m.AzureCLI },
		remediation: "Upgrade with: az upgrade  or  https://docs.microsoft.com/en-us/cli/azure/install-azure-cli",
	},
	{
		key:         "kubernetes",
		rang:        func(m Matrix) VersionRange { return m.Kubernetes },
		remediation: "Upgrade the cluster to a supported EKS/AKS version.",
	},
	{
		key:         "strimzi",
		rang:        func(m Matrix) VersionRange { return m.Strimzi },
		remediation: "Upgrade the Strimzi operator: helm upgrade strimzi-kafka-operator strimzi/strimzi-kafka-operator",
	},
	{
		key:         "chart",
		rang:        func(m Matrix) VersionRange { return m.Chart },
		remediation: "Update the WAS Helm chart to match this wasctl version: git pull && helm dependency update charts/wolfram-application-server",
	},
	{
		key:         "terraform-aws-provider",
		rang:        func(m Matrix) VersionRange { return m.AWSProvider },
		remediation: "Update the hashicorp/aws provider constraint in terraform/versions.tf and run terraform init -upgrade",
	},
	{
		key:         "terraform-azure-provider",
		rang:        func(m Matrix) VersionRange { return m.AzureProvider },
		remediation: "Update the hashicorp/azurerm provider constraint in terraform/versions.tf and run terraform init -upgrade",
	},
}

// CheckCompatibility compares detected versions against m and returns Issues.
// Components absent from current are silently skipped (tool not installed).
func CheckCompatibility(m Matrix, current map[string]Version) []Issue {
	var issues []Issue
	for _, def := range componentDefs {
		v, ok := current[def.key]
		if !ok {
			continue
		}
		rang := def.rang(m)
		if rang.InRange(v) {
			continue
		}
		sev := IssueSeverityWarning
		if v.LessThan(rang.Min) {
			sev = IssueSeverityCritical
		}
		issues = append(issues, Issue{
			Component:      def.key,
			CurrentVer:     v,
			SupportedRange: rang,
			Severity:       sev,
			Remediation:    def.remediation,
		})
	}
	return issues
}

// Detect runs all installed tool version parsers and returns a map of
// component-key → detected version. Tools that are not installed or
// unreachable are silently absent from the result.
// The returned error is always nil; the signature preserves future extensibility.
func Detect() (map[string]Version, error) {
	ctx := context.Background()
	current := make(map[string]Version)

	if v, err := parsers.Helm(ctx); err == nil {
		current["helm"] = v
	}
	if v, err := parsers.Kubectl(ctx); err == nil {
		current["kubectl"] = v
	}
	if v, err := parsers.Terraform(ctx); err == nil {
		current["terraform"] = v
	}
	if v, err := parsers.AWS(ctx); err == nil {
		current["aws-cli"] = v
	}
	if v, err := parsers.AzureCLI(ctx); err == nil {
		current["azure-cli"] = v
	}
	if v, err := parsers.Kubernetes(ctx); err == nil {
		current["kubernetes"] = v
	}
	if v, err := parsers.Strimzi(ctx); err == nil {
		current["strimzi"] = v
	}
	if v, err := parsers.Chart(ctx); err == nil {
		current["chart"] = v
	}
	if v, err := parsers.TerraformAWSProvider(ctx); err == nil {
		current["terraform-aws-provider"] = v
	}
	if v, err := parsers.TerraformAzureProvider(ctx); err == nil {
		current["terraform-azure-provider"] = v
	}

	return current, nil
}

// Check runs all installed tool version parsers and returns incompatibilities
// against CurrentMatrix. Parser failures (tool not installed or cluster
// unreachable) are silently ignored — only detected versions are checked.
func Check() ([]Issue, error) {
	current, err := Detect()
	if err != nil {
		return nil, err
	}
	return CheckCompatibility(CurrentMatrix, current), nil
}
