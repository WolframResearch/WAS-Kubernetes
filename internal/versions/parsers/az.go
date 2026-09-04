package parsers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

var azVersionFn = func(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "az", "version", "--output", "json").Output()
}

// AzureCLI returns the installed Azure CLI version.
// Runs `az version --output json`.
func AzureCLI(ctx context.Context) (Version, error) {
	out, err := azVersionFn(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("az version: %w", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(out, &payload); err != nil {
		return Version{}, fmt.Errorf("az version parse: %w", err)
	}
	raw, ok := payload["azure-cli"].(string)
	if !ok {
		return Version{}, fmt.Errorf("az version: missing azure-cli field")
	}
	v, err := Parse(raw)
	if err != nil {
		return Version{}, fmt.Errorf("az version parse %q: %w", raw, err)
	}
	return v, nil
}
