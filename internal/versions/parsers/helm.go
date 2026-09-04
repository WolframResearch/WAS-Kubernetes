package parsers

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

var helmVersionFn = func(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "helm", "version", "--short").Output()
}

// Helm returns the installed Helm version by running `helm version --short`.
// Sample output: "v3.17.2+g9f9b2d2"
func Helm(ctx context.Context) (Version, error) {
	out, err := helmVersionFn(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("helm version: %w", err)
	}
	s := strings.TrimSpace(string(out))
	// Strip build metadata (e.g. "+g9f9b2d2").
	if i := strings.IndexByte(s, '+'); i > 0 {
		s = s[:i]
	}
	v, err := Parse(s)
	if err != nil {
		return Version{}, fmt.Errorf("helm version parse %q: %w", s, err)
	}
	return v, nil
}
