package parsers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// terraformProvidersOutputFn is shared by both provider parsers since they
// invoke the same command. Override in tests to inject mock output.
var terraformProvidersOutputFn = func(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "terraform", "version", "-json").Output()
}

type terraformVersionOutput struct {
	ProviderSelections map[string]string `json:"provider_selections"`
}

func parseTerraformProvider(ctx context.Context, key string) (Version, error) {
	out, err := terraformProvidersOutputFn(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("terraform version (providers): %w", err)
	}
	var payload terraformVersionOutput
	if err := json.Unmarshal(out, &payload); err != nil {
		return Version{}, fmt.Errorf("terraform version parse (providers): %w", err)
	}
	raw, ok := payload.ProviderSelections[key]
	if !ok || raw == "" {
		return Version{}, fmt.Errorf("terraform provider %q not found in selections (run terraform init?)", key)
	}
	v, err := Parse(raw)
	if err != nil {
		return Version{}, fmt.Errorf("terraform provider %q version parse %q: %w", key, raw, err)
	}
	return v, nil
}

// TerraformAWSProvider returns the installed version of hashicorp/aws.
// Requires `terraform init` to have been run in the working directory.
func TerraformAWSProvider(ctx context.Context) (Version, error) {
	return parseTerraformProvider(ctx, "registry.terraform.io/hashicorp/aws")
}

// TerraformAzureProvider returns the installed version of hashicorp/azurerm.
// Requires `terraform init` to have been run in the working directory.
func TerraformAzureProvider(ctx context.Context) (Version, error) {
	return parseTerraformProvider(ctx, "registry.terraform.io/hashicorp/azurerm")
}
