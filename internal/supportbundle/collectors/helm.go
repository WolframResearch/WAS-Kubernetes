package collectors

import (
	"context"
	"fmt"
)

// HelmCollector collects Helm release info, sanitized values, manifest, and history.
type HelmCollector struct{}

func (HelmCollector) Name() string { return "helm" }

func (HelmCollector) Collect(ctx context.Context, cc *CollectContext) ([]File, error) {
	if cc.Kubeconfig == "" {
		return nil, fmt.Errorf("no cluster access (kubeconfig unavailable)")
	}

	var files []File
	kc := cc.Kubeconfig

	// All helm releases.
	releases, err := runOutput(ctx, "helm", "list", "--all-namespaces", "--output", "json",
		"--kubeconfig", kc)
	if err != nil {
		releases = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "helm/releases.json", Content: releases})

	// WAS release details.
	wasRelease, err := runOutput(ctx, "helm", "status", "was", "--namespace", "was", "--output", "json",
		"--kubeconfig", kc)
	if err != nil {
		wasRelease = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "helm/was_release.json", Content: wasRelease})

	// Values (JSON for deep-walk redaction).
	valuesJSON, err := runOutput(ctx, "helm", "get", "values", "was", "--namespace", "was",
		"--output", "json", "--kubeconfig", kc)
	if err != nil {
		files = append(files, File{Path: "helm/was_values.yaml",
			Content: []byte(fmt.Sprintf("# error: %s\n", err))})
	} else {
		redacted, redactedKeys, _ := redactHelmValues(valuesJSON)
		files = append(files, File{
			Path:       "helm/was_values.yaml",
			Content:    redacted,
			Redactions: redactedKeys,
		})
	}

	// Manifest.
	manifest, err := runOutput(ctx, "helm", "get", "manifest", "was", "--namespace", "was",
		"--kubeconfig", kc)
	if err != nil {
		manifest = []byte(fmt.Sprintf("# error: %s\n", err))
	}
	files = append(files, File{Path: "helm/was_manifest.yaml", Content: manifest})

	// History.
	history, err := runOutput(ctx, "helm", "history", "was", "--namespace", "was",
		"--output", "json", "--kubeconfig", kc)
	if err != nil {
		history = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "helm/was_history.json", Content: history})

	return files, nil
}
