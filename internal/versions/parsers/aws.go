package parsers

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

var awsVersionFn = func(ctx context.Context) ([]byte, error) {
	// aws --version writes to stderr on some platforms; combine both.
	cmd := exec.CommandContext(ctx, "aws", "--version")
	out, _ := cmd.CombinedOutput()
	if len(out) > 0 {
		return out, nil
	}
	return nil, fmt.Errorf("aws --version produced no output")
}

// AWS returns the installed aws-cli version.
// Runs `aws --version`. Sample output: "aws-cli/2.15.0 Python/3.11.6 ..."
func AWS(ctx context.Context) (Version, error) {
	out, err := awsVersionFn(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("aws --version: %w", err)
	}
	// "aws-cli/2.15.0 Python/..." → extract the version after "aws-cli/"
	s := strings.TrimSpace(string(out))
	const prefix = "aws-cli/"
	idx := strings.Index(s, prefix)
	if idx < 0 {
		return Version{}, fmt.Errorf("aws --version unexpected output: %q", s)
	}
	rest := s[idx+len(prefix):]
	// Take up to next space.
	if sp := strings.IndexByte(rest, ' '); sp > 0 {
		rest = rest[:sp]
	}
	v, err := Parse(rest)
	if err != nil {
		return Version{}, fmt.Errorf("aws version parse %q: %w", rest, err)
	}
	return v, nil
}
