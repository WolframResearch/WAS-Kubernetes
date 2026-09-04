package parsers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

var kubernetesVersionFn = func(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "kubectl", "version", "-o", "json").Output()
}

// Kubernetes returns the Kubernetes server version.
// Runs `kubectl version -o json` and extracts serverVersion.gitVersion.
// Returns an error if the cluster is not reachable.
func Kubernetes(ctx context.Context) (Version, error) {
	out, err := kubernetesVersionFn(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("kubectl version (server): %w", err)
	}
	var payload struct {
		ServerVersion struct {
			GitVersion string `json:"gitVersion"`
		} `json:"serverVersion"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return Version{}, fmt.Errorf("kubernetes version parse: %w", err)
	}
	if payload.ServerVersion.GitVersion == "" {
		return Version{}, fmt.Errorf("kubernetes version: serverVersion not available (cluster unreachable?)")
	}
	v, err := Parse(payload.ServerVersion.GitVersion)
	if err != nil {
		return Version{}, fmt.Errorf("kubernetes version parse %q: %w", payload.ServerVersion.GitVersion, err)
	}
	return v, nil
}
