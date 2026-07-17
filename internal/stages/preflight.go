package stages

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
	"github.com/WolframResearch/WAS-Kubernetes/internal/versions"
)

// Preflight checks that all required CLIs are installed and that cloud
// credentials are valid. AWS and Azure modes have different tool/credential
// requirements but identical substep structure.
type Preflight struct{}

func (Preflight) Name() string         { return "preflight" }
func (Preflight) Description() string  { return "Prerequisites checked" }
func (Preflight) EstimateText() string { return "~10 sec" }
func (Preflight) Label() string        { return "Checking prerequisites" }
func (Preflight) DestroyLabel() string { return "Verifying teardown conditions" }
func (Preflight) Substeps() []string {
	return []string{
		"Check version compatibility",
		"Check terraform",
		"Check helm",
		"Check kubectl",
		"Check cloud CLI",
		"Verify cloud credentials",
	}
}

// Check returns nil only if all required CLIs are on PATH and cloud credentials
// are valid (both presence and authentication — not tools alone).
func (p Preflight) Check(ctx context.Context, cfg *config.Config) error {
	cloudCLI := p.cloudCLI(cfg)
	for _, tool := range []string{"terraform", "helm", "kubectl", cloudCLI} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s not found on PATH", tool)
		}
	}
	if _, err := cloudAccountID(ctx, cfg); err != nil {
		return fmt.Errorf("cloud credentials not valid: %w", err)
	}
	return nil
}

// Apply verifies each CLI and prints its version, then confirms cloud credentials.
func (p Preflight) Apply(ctx context.Context, cfg *config.Config, _ runner.Runner, r runner.Reporter) error {
	var failures []string

	r.SubstepStart("Check version compatibility")
	versionIssues, _ := versions.Check()
	for _, iss := range versionIssues {
		if iss.Severity == versions.IssueSeverityCritical {
			msg := fmt.Sprintf("%s version %s is below minimum %s — %s",
				iss.Component, iss.CurrentVer.String(), iss.SupportedRange.Min.String(), iss.Remediation)
			r.LogLine("[!] " + msg)
			failures = append(failures, msg)
		} else {
			r.LogLine(fmt.Sprintf("[~] %s version %s is above tested maximum %s (may work but unsupported)",
				iss.Component, iss.CurrentVer.String(), iss.SupportedRange.Max.String()))
		}
	}
	if len(versionIssues) == 0 {
		r.LogLine("[✓] all tool versions within supported ranges")
		r.SubstepDone()
	} else if len(failures) > 0 {
		r.SubstepFail(fmt.Errorf("critical version incompatibilities detected"))
	} else {
		r.SubstepDone() // warnings only — proceed
	}

	for _, tool := range []string{"terraform", "helm", "kubectl"} {
		r.SubstepStart("Check " + tool)
		path, err := exec.LookPath(tool)
		if err != nil {
			msg := tool + " not found on PATH"
			r.LogLine("[!] " + msg)
			r.SubstepFail(fmt.Errorf("%s not found", tool))
			failures = append(failures, msg)
			continue
		}
		ver := toolVersion(tool, path)
		r.LogLine(fmt.Sprintf("[✓] %-12s %s", tool, ver))
		r.SubstepDone()
	}

	cloudCLI := p.cloudCLI(cfg)
	r.SubstepStart("Check cloud CLI")
	path, err := exec.LookPath(cloudCLI)
	if err != nil {
		msg := cloudCLI + " not found on PATH"
		r.LogLine("[!] " + msg)
		r.SubstepFail(fmt.Errorf("%s not found", cloudCLI))
		failures = append(failures, msg)
	} else {
		ver := toolVersion(cloudCLI, path)
		r.LogLine(fmt.Sprintf("[✓] %-12s %s", cloudCLI, ver))
		r.SubstepDone()
	}

	r.SubstepStart("Verify cloud credentials")
	if cfg.Cloud == "azure" {
		info, err := tools.GetAccountInfo(ctx)
		if err != nil {
			msg := "Azure credentials invalid: " + err.Error()
			r.LogLine("[!] " + msg)
			r.LogLine("    Run: az login")
			r.SubstepFail(err)
			failures = append(failures, msg)
		} else {
			r.LogLine(fmt.Sprintf("[✓] Subscription: %s  Location: %s", info.ID, cfg.AzureLocation.Value))
			r.SubstepDone()
		}
	} else {
		id, err := tools.GetCallerIdentity(ctx, cfg.Region.Value)
		if err != nil {
			msg := "AWS credentials invalid: " + err.Error()
			r.LogLine("[!] " + msg)
			r.LogLine("    Run: aws configure  (or set AWS_PROFILE / AWS_ACCESS_KEY_ID)")
			r.SubstepFail(err)
			failures = append(failures, msg)
		} else {
			r.LogLine(fmt.Sprintf("[✓] Account: %s  Region: %s", id.Account, cfg.Region.Value))
			r.SubstepDone()
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("preflight failed:\n  • %s", strings.Join(failures, "\n  • "))
	}
	return nil
}

// Destroy is a no-op for preflight.
func (Preflight) Destroy(_ context.Context, _ *config.Config, _ runner.Runner, _ runner.Reporter) error {
	return nil
}

// cloudCLI returns the required cloud CLI name for this config.
func (Preflight) cloudCLI(cfg *config.Config) string {
	if cfg.Cloud == "azure" {
		return "az"
	}
	return "aws"
}

// toolVersion returns the version string for a CLI tool (best-effort).
func toolVersion(tool, path string) string {
	var args []string
	switch tool {
	case "helm":
		args = []string{path, "version", "--short"}
	case "kubectl":
		args = []string{path, "version", "--client"}
	case "aws":
		args = []string{path, "--version"}
	case "az":
		args = []string{path, "--version"}
	case "kubelogin":
		args = []string{path, "--version"}
	default:
		args = []string{path, "version"}
	}
	out, err := exec.Command(args[0], args[1:]...).CombinedOutput() //nolint:gosec
	if err != nil {
		return "?"
	}
	line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	return strings.TrimSpace(line)
}
