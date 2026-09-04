// Package fixers provides the Fixer interface and safe auto-fix implementations
// for doctor checks that opt in to automatic remediation.
package fixers

import (
	"context"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

// Fixer can automatically remediate a check failure.
type Fixer interface {
	// DryRun describes what Fix would do without making changes.
	DryRun(ctx context.Context, rc *doctor.RunContext) (description string, err error)
	// Fix applies the remediation.
	Fix(ctx context.Context, rc *doctor.RunContext) error
}
