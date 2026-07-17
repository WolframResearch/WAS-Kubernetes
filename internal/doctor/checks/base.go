// Package checks implements all wasctl doctor diagnostic checks.
// Each check lives in its own file and embeds BaseCheck for shared defaults.
package checks

import (
	"context"
	"errors"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

// BaseCheck provides default implementations of SafeToFix and Fix.
// Embed this in every check struct.
type BaseCheck struct{}

// SafeToFix returns false; only a small number of checks opt in to auto-fix.
func (BaseCheck) SafeToFix() bool { return false }

// Fix returns an error because this check is not safe to auto-fix.
func (BaseCheck) Fix(_ context.Context, _ *doctor.RunContext) error {
	return errors.New("this check does not support auto-fix")
}

// pass returns a passing Finding for check id/name with the given message.
func pass(id, name, msg string) doctor.Finding {
	return doctor.Finding{
		CheckID:   id,
		CheckName: name,
		Severity:  doctor.SeverityInfo,
		Status:    doctor.StatusPass,
		Message:   msg,
	}
}

// fail returns a failing Finding.
func fail(id, name string, sev doctor.Severity, msg, remediation string) doctor.Finding {
	return doctor.Finding{
		CheckID:     id,
		CheckName:   name,
		Severity:    sev,
		Status:      doctor.StatusFail,
		Message:     msg,
		Remediation: remediation,
	}
}

// skip returns a skipped Finding (precondition not met).
func skip(id, name, reason string) doctor.Finding {
	return doctor.Finding{
		CheckID:   id,
		CheckName: name,
		Severity:  doctor.SeverityInfo,
		Status:    doctor.StatusSkip,
		Message:   reason,
	}
}

// checkError returns a Finding with StatusError (the check itself broke).
func checkError(id, name, msg string) doctor.Finding {
	return doctor.Finding{
		CheckID:   id,
		CheckName: name,
		Severity:  doctor.SeverityProblem,
		Status:    doctor.StatusError,
		Message:   msg,
	}
}

// noCluster is the standard skip reason when no kubeconfig is available.
const noCluster = "no cluster reachable; run `wasctl install kubeconfig` first"

// noChart is the standard skip reason when the chart is not deployed.
const noChart = "WAS chart not deployed; run `wasctl install app` first"
