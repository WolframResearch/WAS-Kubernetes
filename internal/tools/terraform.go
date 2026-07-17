// Package tools wraps the external CLI tools (terraform, helm, kubectl, aws)
// that wasctl shells out to. Each function returns a command slice ready for
// runner.Runner.Run; none of them execute anything directly.
package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// TerraformInit returns the command to initialise a Terraform working directory.
// backendConfig is the absolute path to a backend.hcl file; pass "" to omit.
func TerraformInit(dir, backendConfig string) []string {
	cmd := []string{"terraform", "-chdir=" + dir, "init"}
	if backendConfig != "" {
		// -reconfigure accepts the current backend.hcl without prompting for
		// migration when the backend config has changed (e.g. dynamodb_table →
		// use_lockfile). The backend type and state path are unchanged, so no
		// migration is needed.
		cmd = append(cmd, "-reconfigure", "-backend-config="+backendConfig)
	}
	return cmd
}

// TerraformApply returns the command to run terraform apply -auto-approve with
// the provided -var flags. vars maps variable name to value.
func TerraformApply(dir string, vars map[string]string) []string {
	cmd := []string{"terraform", "-chdir=" + dir, "apply", "-auto-approve"}
	for k, v := range vars {
		cmd = append(cmd, fmt.Sprintf("-var=%s=%s", k, v))
	}
	return cmd
}

// TerraformDestroy returns the terraform destroy -auto-approve command.
func TerraformDestroy(dir string, vars map[string]string) []string {
	cmd := []string{"terraform", "-chdir=" + dir, "destroy", "-auto-approve"}
	for k, v := range vars {
		cmd = append(cmd, fmt.Sprintf("-var=%s=%s", k, v))
	}
	return cmd
}

// TerraformDestroyTarget returns the terraform destroy -auto-approve -target=<target> command.
func TerraformDestroyTarget(dir, target string, vars map[string]string) []string {
	cmd := []string{"terraform", "-chdir=" + dir, "destroy", "-auto-approve", "-target=" + target}
	for k, v := range vars {
		cmd = append(cmd, fmt.Sprintf("-var=%s=%s", k, v))
	}
	return cmd
}

// TerraformOutput reads a single output value from a Terraform working
// directory. Unlike the other functions this executes synchronously because
// it is a read-only query used by Check() functions, not a state-changing
// command that needs to stream to a Reporter.
//
// Returns ("", nil) when the output exists but is empty.
// Returns ("", ErrOutputMissing) when the output key does not exist or the
// state has not been initialised.
func TerraformOutput(ctx context.Context, dir, key string) (string, error) {
	out, err := exec.CommandContext(ctx, "terraform", "-chdir="+dir, "output", "-raw", key).Output()
	if err != nil {
		return "", fmt.Errorf("terraform output %s: %w", key, err)
	}
	return strings.TrimSpace(string(bytes.TrimRight(out, "\n"))), nil
}
