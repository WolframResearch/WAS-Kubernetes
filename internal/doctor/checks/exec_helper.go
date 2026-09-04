package checks

import (
	"context"
	"os/exec"
)

// runOutput is the package-level exec helper. It is overridden in tests by
// replacing runOutputFn, or per-check via per-check function variables.
var runOutputFn = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output() //nolint:gosec
}

// runOutput runs name args and returns combined output.
func runOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return runOutputFn(ctx, name, args...)
}

// lookPath is the package-level exec.LookPath replacement, overridable in tests.
var lookPath = exec.LookPath
