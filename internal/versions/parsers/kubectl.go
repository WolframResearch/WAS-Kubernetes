package parsers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

var kubectlVersionFn = func(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "kubectl", "version", "--client", "-o", "json").Output()
}

// Kubectl returns the installed kubectl client version.
// Runs `kubectl version --client -o json`.
func Kubectl(ctx context.Context) (Version, error) {
	out, err := kubectlVersionFn(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("kubectl version: %w", err)
	}
	var payload struct {
		ClientVersion struct {
			GitVersion string `json:"gitVersion"`
		} `json:"clientVersion"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return Version{}, fmt.Errorf("kubectl version parse: %w", err)
	}
	v, err := Parse(payload.ClientVersion.GitVersion)
	if err != nil {
		return Version{}, fmt.Errorf("kubectl version parse %q: %w", payload.ClientVersion.GitVersion, err)
	}
	return v, nil
}
