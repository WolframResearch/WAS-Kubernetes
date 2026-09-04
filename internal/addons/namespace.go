package addons

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// ensureNamespace creates ns if it does not exist; no-op if it already does.
// Uses kubectl apply with a rendered manifest so the operation is idempotent
// regardless of prior state.
func ensureNamespace(ctx context.Context, rc *RunContext, ns string) error {
	// Base manifest
	manifest := fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, ns)

	// Set Helm ownership metadata based on the namespace name.
	// This ensures that when the corresponding Helm charts (or WAS chart)
	// try to manage or adopt these namespaces, they won't fail with
	// ownership/validation errors.
	var releaseName string
	var releaseNamespace string

	switch ns {
	case "was", "kafka":
		releaseName = "was"
		releaseNamespace = "was"
	case "strimzi-system":
		releaseName = "strimzi-kafka-operator"
		releaseNamespace = "strimzi-system"
	case "cert-manager":
		releaseName = "cert-manager"
		releaseNamespace = "cert-manager"
	case "prometheus":
		releaseName = "prometheus"
		releaseNamespace = "prometheus"
	}

	if releaseName != "" && releaseNamespace != "" {
		manifest = fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    app.kubernetes.io/managed-by: Helm
  annotations:
    meta.helm.sh/release-name: %s
    meta.helm.sh/release-namespace: %s
`, ns, releaseName, releaseNamespace)
	}

	return applyManifest(ctx, rc, manifest)
}

// applyManifest writes manifest to a temp file and runs kubectl apply -f.
// The temp file is removed immediately after the kubectl call returns.
func applyManifest(ctx context.Context, rc *RunContext, manifest string) error {
	tmp, err := os.CreateTemp("", "wasctl-manifest-*.yaml")
	if err != nil {
		return fmt.Errorf("applyManifest: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(manifest); err != nil {
		tmp.Close()
		return fmt.Errorf("applyManifest: write temp file: %w", err)
	}
	tmp.Close()

	return rc.Runner.Run(ctx, append([]string{
		"kubectl", "apply", "-f", filepath.Clean(tmpPath),
	}, rc.kubectlFlags()...), nil, rc.Reporter)
}
