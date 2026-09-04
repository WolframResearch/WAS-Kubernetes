package parsers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

var terraformVersionFn = func(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "terraform", "version", "-json").Output()
}

// Terraform returns the installed Terraform version.
// Runs `terraform version -json`.
func Terraform(ctx context.Context) (Version, error) {
	out, err := terraformVersionFn(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("terraform version: %w", err)
	}
	var payload struct {
		TerraformVersion string `json:"terraform_version"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return Version{}, fmt.Errorf("terraform version parse: %w", err)
	}
	v, err := Parse(payload.TerraformVersion)
	if err != nil {
		return Version{}, fmt.Errorf("terraform version parse %q: %w", payload.TerraformVersion, err)
	}
	return v, nil
}
