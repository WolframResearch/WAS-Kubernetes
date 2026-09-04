package addons

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// HelmComponent implements Addon for every chart-based add-on. Subtype files
// (strimzi.go, ingress_nginx.go, …) return *HelmComponent from constructors
// and only set fields — they never override methods.
type HelmComponent struct {
	// Required
	ReleaseName string
	ChartRef    string // "repo/chart" or OCI ref
	Version     string // semver range or exact, passed as --version
	Namespace   string // helm release namespace

	// WatchedNS lists namespaces that must exist before Install, beyond the
	// release namespace. Strimzi populates this; most addons leave it nil.
	WatchedNS []string

	// RepoName + RepoURL: when non-empty, helm repo add is issued before install.
	RepoName string
	RepoURL  string

	// Values are passed as --set k=v.
	Values map[string]string

	// clouds is set by constructors; returned from Clouds().
	clouds []string

	// verifyFn is the Verify implementation. When nil, Verify re-checks helm
	// status (sufficient for most addons since --wait already gates on pods).
	verifyFn func(ctx context.Context, rc *RunContext) error

	// OrphanedResourceCheck lists Kubernetes objects this chart creates that
	// are known to survive helm uninstall/rollback (no ownership GC). Each is
	// checked before every install attempt and deleted when it has no valid
	// helm release-name annotation — i.e. it is an orphan from a prior failed
	// install. Leave nil for addons that create no cluster-scoped or
	// cross-namespace resources, or whose resources are always cleaned up by
	// helm uninstall.
	OrphanedResourceCheck []OrphanedResource

	// Timeout controls the --timeout flag passed to helm install/upgrade.
	// Defaults to "5m" if empty.
	Timeout string

	// DisableAtomic suppresses the --atomic flag, leaving failed resources
	// in the cluster for easier debugging.
	DisableAtomic bool
}

// OrphanedResource identifies a single Kubernetes object that may survive a
// failed install. Only objects with known, constant names from the chart's
// templates should be listed — not label selectors or name prefixes.
type OrphanedResource struct {
	Kind string
	Name string
	// Namespace: "" = cluster-scoped; "@watched" = release namespace + every
	// WatchedNS entry; any other value = literal namespace name.
	Namespace string
}

// clusterScopedKinds lists Kubernetes resource kinds that are not namespaced.
// kubectl must NOT receive a --namespace flag for these.
var clusterScopedKinds = map[string]bool{
	"ClusterRole":              true,
	"ClusterRoleBinding":       true,
	"CSIDriver":                true,
	"IngressClass":             true,
	"CustomResourceDefinition": true,
	"StorageClass":             true,
	"Namespace":                true,
	"PersistentVolume":         true,
	"APIService":               true,
}

func isClusterScoped(kind string) bool {
	return clusterScopedKinds[kind]
}

// resourceMeta captures the minimum JSON needed to check helm ownership
// annotations and managed-by labels on a resource.
type resourceMeta struct {
	Metadata struct {
		Annotations map[string]string `json:"annotations"`
		Labels      map[string]string `json:"labels"`
	} `json:"metadata"`
}

func (h *HelmComponent) Name() string    { return h.ReleaseName }
func (h *HelmComponent) Clouds() []string { return h.clouds }

// Namespaces returns the release namespace plus any WatchedNS entries,
// deduplicated. The list defines which namespaces must exist before Install.
func (h *HelmComponent) Namespaces() []string {
	seen := map[string]bool{h.Namespace: true}
	out := []string{h.Namespace}
	for _, ns := range h.WatchedNS {
		if !seen[ns] {
			seen[ns] = true
			out = append(out, ns)
		}
	}
	return out
}

// Check queries helm status and returns the current State.
// A release that does not exist returns StateNotInstalled (not an error).
func (h *HelmComponent) Check(ctx context.Context, rc *RunContext) (State, error) {
	cmd := append([]string{
		"helm", "status", h.ReleaseName,
		"--namespace", h.Namespace,
		"-o", "json",
	}, rc.helmFlags()...)

	out, err := rc.Runner.Output(ctx, cmd, nil)
	if err != nil {
		// helm status exits non-zero when the release doesn't exist — treat
		// that as StateNotInstalled, not an error.
		return StateNotInstalled, nil
	}

	var payload struct {
		Info struct {
			Status string `json:"status"`
		} `json:"info"`
	}
	if jsonErr := json.Unmarshal(out, &payload); jsonErr != nil {
		return StateFailed, fmt.Errorf("parse helm status for %s: %w", h.ReleaseName, jsonErr)
	}

	switch payload.Info.Status {
	case "deployed":
		return StateHealthy, nil
	case "":
		return StateNotInstalled, nil
	default:
		// "failed", "pending-install", "pending-upgrade", "uninstalling", …
		return StateFailed, nil
	}
}

// Install is the idempotent entry point.
//
//  1. Ensure all required namespaces exist.
//  2. Add/update the helm repo when configured.
//  3. If Failed, uninstall orphans before reinstalling.
//  4. If Healthy, skip.
//  5. Run helm upgrade --install --wait.
func (h *HelmComponent) Install(ctx context.Context, rc *RunContext) error {
	for _, ns := range h.Namespaces() {
		if err := ensureNamespace(ctx, rc, ns); err != nil {
			return err
		}
	}

	if h.RepoURL != "" {
		if err := rc.Runner.Run(ctx, []string{
			"helm", "repo", "add", h.RepoName, h.RepoURL, "--force-update",
		}, nil, rc.Reporter); err != nil {
			return fmt.Errorf("%s: helm repo add: %w", h.ReleaseName, err)
		}
		if err := rc.Runner.Run(ctx, []string{
			"helm", "repo", "update", h.RepoName,
		}, nil, rc.Reporter); err != nil {
			return fmt.Errorf("%s: helm repo update: %w", h.ReleaseName, err)
		}
	}

	state, err := h.Check(ctx, rc)
	if err != nil {
		return err
	}

	if state == StateFailed {
		rc.Reporter.LogLine(fmt.Sprintf(
			"[addons] %s: previous install in failed state — cleaning up before reinstall",
			h.ReleaseName))
		if unErr := h.uninstall(ctx, rc); unErr != nil {
			return fmt.Errorf("%s: cleanup failed release: %w", h.ReleaseName, unErr)
		}
		state = StateNotInstalled
	}

	if state == StateHealthy {
		rc.Reporter.LogLine(fmt.Sprintf("[addons] %s: already healthy — skipping", h.ReleaseName))
		return nil
	}

	if err := h.cleanupOrphanedResources(ctx, rc); err != nil {
		return fmt.Errorf("%s: cleanup orphaned resources: %w", h.ReleaseName, err)
	}

	timeout := "5m"
	if h.Timeout != "" {
		timeout = h.Timeout
	}

	args := []string{
		"helm", "upgrade", "--install", h.ReleaseName, h.ChartRef,
		"--namespace", h.Namespace,
		"--create-namespace",
		"--version", h.Version,
	}
	if !h.DisableAtomic {
		args = append(args, "--atomic")
	}
	args = append(args, "--wait", "--timeout", timeout)
	args = append(args, rc.helmFlags()...)
	for k, v := range h.Values {
		args = append(args, "--set", fmt.Sprintf("%s=%s", k, v))
	}
	return rc.Runner.Run(ctx, args, nil, rc.Reporter)
}

// Verify re-checks helm status to confirm the release is deployed. When a
// custom verifyFn is set it is called instead.
func (h *HelmComponent) Verify(ctx context.Context, rc *RunContext) error {
	if h.verifyFn != nil {
		return h.verifyFn(ctx, rc)
	}
	state, err := h.Check(ctx, rc)
	if err != nil {
		return err
	}
	if state != StateHealthy {
		return fmt.Errorf("%s: not in deployed state after install", h.ReleaseName)
	}
	return nil
}

// Uninstall removes the release if it exists. No-op when not installed.
func (h *HelmComponent) Uninstall(ctx context.Context, rc *RunContext) error {
	state, err := h.Check(ctx, rc)
	if err != nil {
		return err
	}
	if state == StateNotInstalled {
		rc.Reporter.LogLine(fmt.Sprintf("[addons] %s: not installed — skipping", h.ReleaseName))
		return nil
	}
	return h.uninstall(ctx, rc)
}

func (h *HelmComponent) uninstall(ctx context.Context, rc *RunContext) error {
	return rc.Runner.Run(ctx, append([]string{
		"helm", "uninstall", h.ReleaseName,
		"--namespace", h.Namespace,
		"--wait",
	}, rc.helmFlags()...), nil, rc.Reporter)
}

// cleanupOrphanedResources removes orphaned resources listed in
// OrphanedResourceCheck. It is called before every install attempt so that
// AlreadyExists errors caused by a prior failed install do not block retry.
func (h *HelmComponent) cleanupOrphanedResources(ctx context.Context, rc *RunContext) error {
	for _, res := range h.OrphanedResourceCheck {
		for _, ns := range resolveOrphanNamespaces(res.Namespace, h.Namespace, h.WatchedNS) {
			if err := h.cleanupResourceIfOrphaned(ctx, rc, res, ns); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveOrphanNamespaces expands the Namespace field of an OrphanedResource
// to the concrete namespace list that should be checked:
//   - ""         → ["""]          (cluster-scoped resource; namespace arg omitted)
//   - "@watched" → release NS + every WatchedNS entry, deduplicated
//   - anything else → [ns] literal
func resolveOrphanNamespaces(ns, releaseNS string, watchedNS []string) []string {
	if ns != "@watched" {
		return []string{ns}
	}
	seen := map[string]bool{releaseNS: true}
	out := []string{releaseNS}
	for _, w := range watchedNS {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

// orphanKubectlAttempts retries kubectl get/delete on transient API/DNS blips
// (common against AKS public API hostnames). NotFound is not retried.
const orphanKubectlAttempts = 3

// orphanRetryBaseDelay is the wait before the first retry; doubles each attempt.
var orphanRetryBaseDelay = 2 * time.Second

var kubectlTransientPatterns = []string{
	"i/o timeout",
	"io timeout",
	"dial tcp",
	"read udp",
	"temporary failure in name resolution",
	"no such host",
	"connection reset",
	"connection refused",
	"tls handshake timeout",
	"timeout awaiting response headers",
	"server misbehaving",
	"client.timeout",
	"context deadline exceeded",
	"http2: client connection lost",
	"connection reset by peer",
}

func isTransientKubectlError(err error, out []byte) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error() + " " + string(out))
	// Missing resources are expected during orphan checks — never retry.
	if strings.Contains(msg, "notfound") || strings.Contains(msg, "not found") {
		return false
	}
	for _, p := range kubectlTransientPatterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func kubectlOutputRetry(ctx context.Context, rc *RunContext, cmd []string) ([]byte, error) {
	var out []byte
	var err error
	for attempt := 1; attempt <= orphanKubectlAttempts; attempt++ {
		out, err = rc.Runner.Output(ctx, cmd, nil)
		if err == nil || !isTransientKubectlError(err, out) {
			return out, err
		}
		if attempt == orphanKubectlAttempts {
			break
		}
		delay := orphanRetryBaseDelay * time.Duration(1<<(attempt-1))
		rc.Reporter.LogLine(fmt.Sprintf(
			"[addons] kubectl transient error (attempt %d/%d): %v — retrying in %s…",
			attempt, orphanKubectlAttempts, err, delay))
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-time.After(delay):
		}
	}
	return out, err
}

func kubectlRunRetry(ctx context.Context, rc *RunContext, cmd []string) error {
	var err error
	for attempt := 1; attempt <= orphanKubectlAttempts; attempt++ {
		err = rc.Runner.Run(ctx, cmd, nil, rc.Reporter)
		if err == nil || !isTransientKubectlError(err, nil) {
			return err
		}
		if attempt == orphanKubectlAttempts {
			break
		}
		delay := orphanRetryBaseDelay * time.Duration(1<<(attempt-1))
		rc.Reporter.LogLine(fmt.Sprintf(
			"[addons] kubectl transient error (attempt %d/%d): %v — retrying in %s…",
			attempt, orphanKubectlAttempts, err, delay))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return err
}

// cleanupResourceIfOrphaned checks a single resource and deletes it only when
// all safety conditions are met (no helm annotation for this or another release,
// no foreign managed-by label).
func (h *HelmComponent) cleanupResourceIfOrphaned(
	ctx context.Context, rc *RunContext, res OrphanedResource, namespace string,
) error {
	cmd := []string{"kubectl", "get", res.Kind, res.Name, "-o", "json"}
	if !isClusterScoped(res.Kind) && namespace != "" {
		cmd = append(cmd, "--namespace", namespace)
	}
	cmd = append(cmd, rc.kubectlFlags()...)

	out, err := kubectlOutputRetry(ctx, rc, cmd)
	if err != nil {
		// kubectl exits 1 for both "not found" and genuine errors. Distinguish
		// by inspecting output: a missing resource is not an error here.
		if strings.Contains(string(out), "NotFound") ||
			strings.Contains(string(out), "not found") {
			return nil
		}
		return fmt.Errorf("get %s/%s: %w (output: %s)", res.Kind, res.Name, err, string(out))
	}
	if len(out) == 0 {
		return nil // resource absent (--ignore-not-found style empty response)
	}

	var meta resourceMeta
	if err := json.Unmarshal(out, &meta); err != nil {
		return fmt.Errorf("parse %s/%s: %w", res.Kind, res.Name, err)
	}
	ann := meta.Metadata.Annotations
	lbl := meta.Metadata.Labels

	// SAFETY: owned by a DIFFERENT helm release — hard stop.
	// Resources carrying THIS release's own annotation are treated as stale orphans:
	// cleanupOrphanedResources is only reached when the release is not deployed
	// (Install returns early at StateHealthy), so any self-annotation is from a
	// prior failed or rolled-back install and the resource must be cleaned up.
	if rel := ann["meta.helm.sh/release-name"]; rel != "" && rel != h.ReleaseName {
		return fmt.Errorf(
			"SAFETY VIOLATION: %s %q is owned by helm release %q — will not delete; "+
				"inspect with: kubectl get %s %s -o yaml",
			res.Kind, res.Name, rel, res.Kind, res.Name)
	}

	// SAFETY: managed by something other than Helm — hard stop.
	if mgr := lbl["app.kubernetes.io/managed-by"]; mgr != "" && mgr != "Helm" {
		return fmt.Errorf(
			"SAFETY VIOLATION: %s %q is managed by %q — will not delete; "+
				"inspect with: kubectl get %s %s -o yaml",
			res.Kind, res.Name, mgr, res.Kind, res.Name)
	}

	// SAFETY: AKS / cloud addon-manager owned — hard stop (no managed-by label).
	if mode := lbl["addonmanager.kubernetes.io/mode"]; mode != "" {
		return fmt.Errorf(
			"SAFETY VIOLATION: %s %q is owned by cluster addon-manager (mode=%s) — will not delete; "+
				"inspect with: kubectl get %s %s -o yaml",
			res.Kind, res.Name, mode, res.Kind, res.Name)
	}

	// No helm ownership and no foreign management: orphan from prior failed install.
	loc := namespace
	if loc == "" {
		loc = "(cluster-scoped)"
	}
	rc.Reporter.LogLine(fmt.Sprintf(
		"[addons] %s: cleaning up orphaned %s %q in %s (no helm ownership)",
		h.ReleaseName, res.Kind, res.Name, loc))

	delCmd := []string{"kubectl", "delete", res.Kind, res.Name, "--ignore-not-found"}
	if !isClusterScoped(res.Kind) && namespace != "" {
		delCmd = append(delCmd, "--namespace", namespace)
	}
	delCmd = append(delCmd, rc.kubectlFlags()...)
	return kubectlRunRetry(ctx, rc, delCmd)
}
