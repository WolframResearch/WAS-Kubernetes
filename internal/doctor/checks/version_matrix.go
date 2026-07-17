package checks

import (
	"context"
	"fmt"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/versions"
)

// VersionMatrix checks all tools against the supported compatibility matrix.
type VersionMatrix struct{ BaseCheck }

func (VersionMatrix) ID() string                { return "version.matrix" }
func (VersionMatrix) Name() string              { return "All tools within supported matrix" }
func (VersionMatrix) Category() doctor.Category { return doctor.CategoryEnvironment }
func (VersionMatrix) Description() string {
	return "Validates installed tool versions against the wasctl compatibility matrix."
}
func (VersionMatrix) Precondition(_ context.Context, _ *doctor.RunContext) (bool, string) {
	return false, ""
}

func (c VersionMatrix) Run(_ context.Context, _ *doctor.RunContext) doctor.Finding {
	issues, err := versions.Check()
	if err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("version matrix check failed: %v", err))
	}
	if len(issues) == 0 {
		return pass(c.ID(), c.Name(), "all detected tool versions are within supported ranges")
	}

	var critical, warnings []versions.Issue
	for _, iss := range issues {
		if iss.Severity == versions.IssueSeverityCritical {
			critical = append(critical, iss)
		} else {
			warnings = append(warnings, iss)
		}
	}

	var lines []string
	for _, iss := range append(critical, warnings...) {
		label := "WARN"
		if iss.Severity == versions.IssueSeverityCritical {
			label = "CRIT"
		}
		lines = append(lines, fmt.Sprintf("  [%s] %s: found %s, supported %s\n        Fix: %s",
			label, iss.Component, iss.CurrentVer.String(), iss.SupportedRange.String(), iss.Remediation))
	}
	detail := strings.Join(lines, "\n")

	if len(critical) > 0 {
		return fail(c.ID(), c.Name(), doctor.SeverityCritical,
			fmt.Sprintf("%d critical, %d warning version issue(s)", len(critical), len(warnings)),
			detail)
	}
	return fail(c.ID(), c.Name(), doctor.SeverityWarning,
		fmt.Sprintf("%d version warning(s) — versions above tested maximum", len(warnings)),
		detail)
}
