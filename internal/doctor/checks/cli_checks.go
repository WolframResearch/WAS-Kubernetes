package checks

import (
	"context"
	"fmt"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

// cliCheck is a generic check that verifies a CLI binary is present and returns
// a version string matching a minimum version prefix.
type cliCheck struct {
	BaseCheck
	id       string
	name     string
	sev      doctor.Severity
	binary   string
	versionArgs []string
	minVer   string // prefix expected in version output, e.g. "v1." or "helm v3."
	desc     string
	azure    bool // if true, skip when cloud != "azure"
}

func (c cliCheck) ID() string               { return c.id }
func (c cliCheck) Name() string             { return c.name }
func (c cliCheck) Category() doctor.Category { return doctor.CategoryEnvironment }
func (c cliCheck) Description() string      { return c.desc }

func (c cliCheck) Precondition(_ context.Context, rc *doctor.RunContext) (bool, string) {
	if c.azure && rc.Cfg.Cloud != "azure" {
		return true, c.binary + " only checked for Azure deployments (cloud != azure)"
	}
	return false, ""
}

var cliVersionOutputFn = func(ctx context.Context, bin string, args []string) (string, error) {
	out, err := runOutput(ctx, bin, args...)
	return string(out), err
}

func (c cliCheck) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	_, err := lookPath(c.binary)
	if err != nil {
		return fail(c.id, c.name, c.sev,
			fmt.Sprintf("%s binary not found in PATH", c.binary),
			kubeloginRemediation(c.binary))
	}

	if len(c.versionArgs) == 0 {
		return pass(c.id, c.name, fmt.Sprintf("%s found in PATH", c.binary))
	}

	out, verErr := cliVersionOutputFn(ctx, c.binary, c.versionArgs)
	if verErr != nil {
		// Binary exists but version query failed — treat as warn, not fail
		return pass(c.id, c.name, fmt.Sprintf("%s found (version query failed: %v)", c.binary, verErr))
	}
	out = strings.TrimSpace(out)

	if c.minVer != "" && !strings.Contains(out, c.minVer) {
		// Extract first line for cleaner message
		first := strings.SplitN(out, "\n", 2)[0]
		return fail(c.id, c.name, c.sev,
			fmt.Sprintf("%s version %q does not meet minimum requirement %q", c.binary, first, c.minVer),
			fmt.Sprintf("Upgrade %s. Minimum required: %s", c.binary, c.minVer))
	}

	first := strings.SplitN(out, "\n", 2)[0]
	return pass(c.id, c.name, first)
}

func kubeloginRemediation(binary string) string {
	if binary == "kubelogin" {
		return "Optional for wasctl (uses az aks get-credentials --admin).\n" +
			"If you want AAD user kubeconfigs outside wasctl:\n" +
			"  az aks install-cli\n" +
			"  # or see https://aka.ms/aks/kubelogin"
	}
	return fmt.Sprintf("Install %s. See: README.md (Prerequisites)", binary)
}

// CLI checks registered in the registry.

// CLITerraform checks that terraform is present.
var CLITerraform = cliCheck{
	id:          "cli.terraform",
	name:        "Terraform binary present",
	sev:         doctor.SeverityProblem,
	binary:      "terraform",
	versionArgs: []string{"version", "-json"},
	minVer:      "", // any version accepted; check at least it runs
	desc:        "Verifies terraform is installed and in PATH.",
}

// CLIHelm checks that helm ≥ v3 is present.
var CLIHelm = cliCheck{
	id:          "cli.helm",
	name:        "Helm binary present (≥ v3)",
	sev:         doctor.SeverityProblem,
	binary:      "helm",
	versionArgs: []string{"version", "--short"},
	minVer:      "v3.",
	desc:        "Verifies helm v3 or later is installed and in PATH.",
}

// CLIKubectl checks that kubectl is present.
var CLIKubectl = cliCheck{
	id:          "cli.kubectl",
	name:        "kubectl binary present",
	sev:         doctor.SeverityProblem,
	binary:      "kubectl",
	versionArgs: []string{"version", "--client", "--short"},
	minVer:      "",
	desc:        "Verifies kubectl is installed and in PATH.",
}

// CLIAWS checks that the AWS CLI is present.
var CLIAWS = cliCheck{
	id:          "cli.aws",
	name:        "AWS CLI present",
	sev:         doctor.SeverityProblem,
	binary:      "aws",
	versionArgs: []string{"--version"},
	minVer:      "aws-cli/2",
	desc:        "Verifies aws CLI v2 is installed and in PATH.",
}

// CLIAz checks that the Azure CLI is present (Azure deployments only).
var CLIAz = cliCheck{
	id:          "cli.az",
	name:        "Azure CLI (az) present",
	sev:         doctor.SeverityWarning,
	binary:      "az",
	versionArgs: []string{"version", "--output", "tsv", "--query", "\"azure-cli\""},
	minVer:      "",
	desc:        "Verifies az CLI is installed (only required for Azure deployments).",
	azure:       true,
}

// CLIKubelogin checks that kubelogin is present (Azure only; optional for wasctl).
// wasctl uses az aks get-credentials --admin (cert auth), so kubelogin is not
// required for install/destroy. Kept as a Warning for operators who prefer
// AAD user kubeconfigs outside wasctl.
var CLIKubelogin = cliCheck{
	id:          "cli.kubelogin",
	name:        "kubelogin binary present (Azure, optional)",
	sev:         doctor.SeverityWarning,
	binary:      "kubelogin",
	versionArgs: []string{"--version"},
	minVer:      "",
	desc:        "Optionally verifies kubelogin is installed. wasctl itself uses --admin credentials and does not require it.",
	azure:       true,
}
