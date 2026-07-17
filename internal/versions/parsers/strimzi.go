package parsers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

var strimziVersionFn = func(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "kubectl", "get", "deployment",
		"strimzi-cluster-operator", "-n", "kafka", "-o", "json").Output()
}

// Strimzi returns the Strimzi operator version by inspecting the container
// image tag of the strimzi-cluster-operator Deployment.
// Sample image: "quay.io/strimzi/operator:0.43.0"
func Strimzi(ctx context.Context) (Version, error) {
	out, err := strimziVersionFn(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("strimzi operator: %w", err)
	}
	var deployment struct {
		Spec struct {
			Template struct {
				Spec struct {
					Containers []struct {
						Image string `json:"image"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(out, &deployment); err != nil {
		return Version{}, fmt.Errorf("strimzi deployment parse: %w", err)
	}
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return Version{}, fmt.Errorf("strimzi deployment has no containers")
	}
	image := deployment.Spec.Template.Spec.Containers[0].Image
	// "quay.io/strimzi/operator:0.43.0" → "0.43.0"
	idx := strings.LastIndexByte(image, ':')
	if idx < 0 {
		return Version{}, fmt.Errorf("strimzi image has no tag: %q", image)
	}
	tag := image[idx+1:]
	v, err := Parse(tag)
	if err != nil {
		return Version{}, fmt.Errorf("strimzi version parse %q: %w", tag, err)
	}
	return v, nil
}
