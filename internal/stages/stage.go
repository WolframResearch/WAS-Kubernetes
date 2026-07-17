// Package stages contains the seven deployment stages and the Stage interface
// they all implement.
package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metacontainer"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
)

// Stage is the interface every deployment stage implements.
type Stage interface {
	// Name returns the machine-readable stage name ("preflight", "bootstrap", …).
	Name() string
	// Description returns the human-readable past-tense completion description.
	Description() string
	// EstimateText returns the rough time estimate displayed to the user.
	EstimateText() string
	// Label returns the present-tense action label shown during an install run.
	Label() string
	// DestroyLabel returns the present-tense action label shown during a destroy run.
	DestroyLabel() string
	// Substeps returns the ordered substep names that Apply will walk through.
	Substeps() []string

	// Check returns nil when the stage is already complete (idempotent check).
	// A non-nil error is informational; it means the stage needs to run.
	Check(ctx context.Context, cfg *config.Config) error

	// Apply runs the stage. Substep transitions are signalled via r.
	Apply(ctx context.Context, cfg *config.Config, run runner.Runner, r runner.Reporter) error

	// Destroy reverses the stage; may be a no-op for some stages.
	Destroy(ctx context.Context, cfg *config.Config, run runner.Runner, r runner.Reporter) error
}

// All returns the seven stages in deployment order.
func All() []Stage {
	return []Stage{
		&Preflight{},
		&Bootstrap{},
		&Backend{},
		&Infra{},
		&Kubeconfig{},
		&Addons{},
		&App{},
	}
}

// ByName returns a stage by its name, or an error if not found.
func ByName(name string) (Stage, error) {
	for _, s := range All() {
		if s.Name() == name {
			return s, nil
		}
	}
	return nil, fmt.Errorf("unknown stage %q: valid stages are %s",
		name, strings.Join(Names(), ", "))
}

// Select returns the named stages in install order. Unknown names are an error.
// Duplicate names are ignored after the first occurrence.
func Select(names []string) ([]Stage, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("no stages selected")
	}
	want := make(map[string]bool, len(names))
	order := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if !want[n] {
			want[n] = true
			order = append(order, n)
		}
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("no stages selected")
	}
	known := make(map[string]Stage, len(All()))
	for _, s := range All() {
		known[s.Name()] = s
	}
	for _, n := range order {
		if _, ok := known[n]; !ok {
			return nil, fmt.Errorf("unknown stage %q: valid stages are %s",
				n, strings.Join(Names(), ", "))
		}
	}
	// Preserve global install order, not checkbox submission order.
	var out []Stage
	for _, s := range All() {
		if want[s.Name()] {
			out = append(out, s)
		}
	}
	return out, nil
}

// From returns the named stage and every stage after it in install order.
func From(name string) ([]Stage, error) {
	all := All()
	for i, s := range all {
		if s.Name() == name {
			return all[i:], nil
		}
	}
	return nil, fmt.Errorf("unknown stage %q: valid stages are %s",
		name, strings.Join(Names(), ", "))
}

// Names returns the stage names in deployment order.
func Names() []string {
	all := All()
	names := make([]string, len(all))
	for i, s := range all {
		names[i] = s.Name()
	}
	return names
}

const cmdMaxRetries = 5

var cmdRetryBaseDelay = 15 * time.Second

// terraformTransientPatterns are substrings that indicate a retryable network
// blip when talking to a cloud state backend (Azure blob / S3). Kept narrow so
// helm/kubectl permanent errors are not retried for minutes.
//
// Do NOT include broad phrases like "failed to lock azure state" / "error loading
// state" — those also match permanent "already locked" conflicts after a
// mid-lock DNS timeout left a stale blob lease.
var terraformTransientPatterns = []string{
	"connection may have been reset",
	"connection reset by peer",
	"i/o timeout",
	"io timeout",
	"http response was nil",
	"tls handshake timeout",
	"timeout awaiting response headers",
	"read: connection timed out",
	"read udp",
	"dial tcp",
	"no such host",
	"temporary failure in name resolution",
}

// isTerraformCmd reports whether cmd is a terraform invocation.
func isTerraformCmd(cmd []string) bool {
	if len(cmd) == 0 {
		return false
	}
	return cmd[0] == "terraform" || strings.HasSuffix(cmd[0], "/terraform")
}

// isTransientNetworkError reports whether err looks like a retryable network failure.
func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}
	// Stale Azure state leases are recovered via lease-break, not blind retries.
	if isAzureStateBlobLocked(err) {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, p := range terraformTransientPatterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func agentDebugLog(hypothesisID, location, message string, data map[string]any) {
	f, err := os.OpenFile("/home/cenka/.gemini/antigravity/scratch/WAS-Kubernetes/.cursor/debug-a42788.log",
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	payload := map[string]any{
		"sessionId":    "a42788",
		"hypothesisId": hypothesisID,
		"location":     location,
		"message":      message,
		"data":         data,
		"timestamp":    time.Now().UnixMilli(),
	}
	b, _ := json.Marshal(payload)
	_, _ = f.Write(append(b, '\n'))
}

// runCmd wraps runner.Runner.Run with dry-run support and prefixes the command
// to the log tail. It injects TF_PLUGIN_CACHE_DIR for terraform commands so
// providers are cached between wasctl runs (avoiding ~30s re-download).
// Transient network failures on terraform commands are retried automatically.
// Azure state blob lease conflicts after a failed init are recovered by breaking
// the lease (empty terraformlockid cannot use terraform force-unlock).
func runCmd(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter, cmd []string) error {
	rep.LogLine("-> " + strings.Join(cmd, " "))
	if cfg.DryRun {
		rep.LogLine("[dry-run — not executed]")
		return nil
	}
	var env []string
	retry := isTerraformCmd(cmd)
	if retry {
		env = []string{"TF_PLUGIN_CACHE_DIR=" + tfPluginCacheDir()}
	}

	var lastErr error
	maxAttempts := 1
	if retry {
		maxAttempts = cmdMaxRetries
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			delay := cmdRetryDelay(attempt)
			rep.LogLine(fmt.Sprintf("[retry] transient network error (attempt %d/%d); waiting %s before retry…",
				attempt, maxAttempts, delay))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			rep.LogLine("-> " + strings.Join(cmd, " "))
		}
		lastErr = r.Run(ctx, cmd, env, rep)
		if lastErr == nil {
			agentDebugLog("H-A", "stage.go:runCmd", "terraform cmd succeeded", map[string]any{
				"attempt": attempt, "cmd0": cmd[0], "isInit": isTerraformInitCmd(cmd),
			})
			return nil
		}

		locked := isAzureStateBlobLocked(lastErr)
		transient := isTransientNetworkError(lastErr)
		agentDebugLog("H-B", "stage.go:runCmd", "terraform cmd failed", map[string]any{
			"attempt": attempt, "locked": locked, "transient": transient,
			"isInit": isTerraformInitCmd(cmd), "errSnippet": truncateErr(lastErr, 180),
		})

		if locked && isTerraformInitCmd(cmd) {
			if backendPath := backendConfigPathFromCmd(cmd); backendPath != "" {
				rep.LogLine("[retry] azure state blob lease held (likely stale after network timeout); breaking lease…")
				if err := breakAzureTFStateLease(ctx, backendPath); err != nil {
					agentDebugLog("H-C", "stage.go:runCmd", "lease break failed", map[string]any{
						"attempt": attempt, "err": err.Error(),
					})
					rep.LogLine(fmt.Sprintf("[retry] lease break failed: %v", err))
				} else {
					agentDebugLog("H-C", "stage.go:runCmd", "lease break ok", map[string]any{
						"attempt": attempt, "backendPath": backendPath,
					})
					rep.LogLine("[retry] lease break succeeded; will retry terraform init")
					if attempt < maxAttempts {
						continue
					}
					// Last scheduled attempt already failed; do one immediate re-run after break.
					rep.LogLine("-> " + strings.Join(cmd, " "))
					lastErr = r.Run(ctx, cmd, env, rep)
					return lastErr
				}
			}
		}

		if !retry || !transient || attempt == maxAttempts {
			return lastErr
		}
	}
	return lastErr
}

func truncateErr(err error, n int) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func cmdRetryDelay(attempt int) time.Duration {
	// attempt is 1-based; first retry waits 15s, then 30s, 45s, …
	return time.Duration(attempt-1) * cmdRetryBaseDelay
}

// ── Cloud-routing helpers ─────────────────────────────────────────────────────

const azureLocalBootstrapDir = "infra/azure/bootstrap"
const azureLocalStackDir = "infra/azure/stack"

// cloudAccountID returns the cloud account identifier for the current CLI session:
//   - AWS → AWS account ID (from sts:GetCallerIdentity)
//   - Azure → Azure subscription ID (from az account show)
func cloudAccountID(ctx context.Context, cfg *config.Config) (string, error) {
	if cfg.Cloud == "azure" {
		info, err := tools.GetAccountInfo(ctx)
		if err != nil {
			return "", fmt.Errorf("azure account: %w", err)
		}
		return info.ID, nil
	}
	id, err := tools.GetCallerIdentity(ctx, cfg.Region.Value)
	if err != nil {
		return "", fmt.Errorf("aws account: %w", err)
	}
	return id.Account, nil
}

// azureTenantID returns the Azure tenant ID: from config if set, otherwise
// from az account show at runtime. The tenant ID is required by the Azure
// Terraform provider but not needed for SDK operations.
func azureTenantID(ctx context.Context, cfg *config.Config) (string, error) {
	if cfg.AzureTenantID.Value != "" {
		return cfg.AzureTenantID.Value, nil
	}
	info, err := tools.GetAccountInfo(ctx)
	if err != nil {
		return "", fmt.Errorf("derive tenant ID: %w", err)
	}
	return info.TenantID, nil
}

// azureMetaResourceGroup returns the resource group name for the wasctl meta
// storage account for the given cluster. Format: "wolfram-<clusterName>-meta".
func azureMetaResourceGroup(clusterName string) string {
	return metacontainer.MetaResourceGroup(clusterName)
}

// tfPluginCacheDir returns a stable directory for terraform provider caching.
// Providers downloaded here are shared across all wasctl runs, avoiding the
// ~30-second provider download on every infra stage invocation.
func tfPluginCacheDir() string {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	dir := filepath.Join(cacheDir, "wasctl", "terraform-plugins")
	_ = os.MkdirAll(dir, 0755)
	return dir
}
