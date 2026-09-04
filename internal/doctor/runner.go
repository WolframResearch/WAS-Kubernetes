package doctor

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

const (
	maxConcurrency = 8
	checkTimeout   = 25 * time.Second
	totalTimeout   = 90 * time.Second
)

// NewRunContext builds a RunContext for cfg. Workspace, Kubeconfig, and
// ContextName are populated on a best-effort basis; failures leave them empty
// and are surfaced by the relevant checks.
func NewRunContext(ctx context.Context, cfg *config.Config) *RunContext {
	rc := &RunContext{Cfg: cfg}

	if cfg.Cloud == "azure" {
		return newRunContextAzure(ctx, cfg, rc)
	}
	return newRunContextAWS(ctx, cfg, rc)
}

func newRunContextAWS(ctx context.Context, cfg *config.Config, rc *RunContext) *RunContext {
	// Caller identity (best-effort; aws.credentials check surfaces errors)
	id, err := tools.GetCallerIdentity(ctx, cfg.MetaRegion.Value)
	if err == nil {
		rc.CallerID = &id
	}

	if rc.CallerID == nil || cfg.ClusterName.Value == "" || cfg.ChartOnly {
		return rc
	}

	ws, wsErr := workspace.Open(ctx, cfg.MetaRegion.Value, rc.CallerID.Account, cfg.ClusterName.Value)
	if wsErr != nil {
		return rc
	}
	rc.Workspace = ws

	if matErr := ws.MaterializeTempDir(); matErr != nil {
		return rc
	}
	kc, kcErr := ws.Kubeconfig(ctx)
	if kcErr != nil {
		return rc
	}
	rc.Kubeconfig = kc
	rc.ContextName = ws.ContextName()
	rc.cleanup = ws.Close
	return rc
}

func newRunContextAzure(ctx context.Context, cfg *config.Config, rc *RunContext) *RunContext {
	// Azure account info (best-effort; azure.credentials check surfaces errors)
	info, err := tools.GetAccountInfo(ctx)
	if err == nil {
		rc.AzureSubscriptionID = info.ID
	}

	if rc.AzureSubscriptionID == "" || cfg.ClusterName.Value == "" || cfg.ChartOnly {
		return rc
	}

	ws, wsErr := workspace.OpenAzure(ctx, rc.AzureSubscriptionID, cfg.ClusterName.Value)
	if wsErr != nil {
		return rc
	}
	rc.Workspace = ws

	if matErr := ws.MaterializeTempDir(); matErr != nil {
		return rc
	}
	kc, kcErr := ws.Kubeconfig(ctx)
	if kcErr != nil {
		return rc
	}
	rc.Kubeconfig = kc
	rc.ContextName = ws.ContextName()
	rc.cleanup = ws.Close
	return rc
}

// Run executes checks in parallel (≤maxConcurrency at a time) with per-check
// and total timeouts. Results are ordered: critical fails first, then problems,
// warnings, passes, skips, errors.
func Run(ctx context.Context, rc *RunContext, checks []Check) *Report {
	return RunWithCallback(ctx, rc, checks, nil, nil)
}

// RunWithCallback is the same as Run but calls onStart when each check
// begins execution (semaphore acquired) and onFinding when each check
// completes, before the final sort. Both callbacks run from separate goroutines;
// callers must synchronise if needed. Pass nil for either to skip.
// This is the SSE streaming path — see webui operations handlers.
func RunWithCallback(ctx context.Context, rc *RunContext, checks []Check, onStart func(Check), onFinding func(Check, Finding)) *Report {
	totalCtx, totalCancel := context.WithTimeout(ctx, totalTimeout)
	defer totalCancel()

	start := time.Now()
	sem := make(chan struct{}, maxConcurrency)

	findings := make([]Finding, len(checks))
	var wg sync.WaitGroup

	for i, ch := range checks {
		wg.Add(1)
		go func(idx int, check Check) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
			case <-totalCtx.Done():
				findings[idx] = timeoutFinding(check)
				return
			}
			defer func() { <-sem }()

			if onStart != nil {
				onStart(check)
			}

			findStart := time.Now()

			skip, reason := check.Precondition(totalCtx, rc)
			if skip {
				f := Finding{
					CheckID:   check.ID(),
					CheckName: check.Name(),
					Severity:  SeverityInfo,
					Status:    StatusSkip,
					Message:   reason,
					Elapsed:   time.Since(findStart),
				}
				findings[idx] = f
				if onFinding != nil {
					onFinding(check, f)
				}
				return
			}

			checkCtx, checkCancel := context.WithTimeout(totalCtx, checkTimeout)
			defer checkCancel()

			resultCh := make(chan Finding, 1)
			go func() { resultCh <- check.Run(checkCtx, rc) }()

			var f Finding
			select {
			case f = <-resultCh:
			case <-checkCtx.Done():
				f = timeoutFinding(check)
			}
			f.Elapsed = time.Since(findStart)
			findings[idx] = f
			if onFinding != nil {
				onFinding(check, f)
			}
		}(i, ch)
	}
	wg.Wait()

	sortFindings(findings)

	var summary Summary
	for _, f := range findings {
		switch f.Status {
		case StatusPass:
			summary.Pass++
		case StatusFail:
			summary.Fail++
		case StatusSkip:
			summary.Skip++
		case StatusError:
			summary.Error++
		case StatusInfo:
			summary.Info++
		}
	}

	clusterName := rc.Cfg.ClusterName.Value
	if rc.Workspace != nil {
		clusterName = rc.Workspace.Meta.ClusterName
	}

	return &Report{
		GeneratedAt:   time.Now(),
		Cluster:       clusterName,
		WasctlVersion: version.Version,
		Findings:      findings,
		Summary:       summary,
		Elapsed:       time.Since(start),
	}
}

func timeoutFinding(check Check) Finding {
	return Finding{
		CheckID:     check.ID(),
		CheckName:   check.Name(),
		Severity:    SeverityProblem,
		Status:      StatusError,
		Message:     fmt.Sprintf("check exceeded %s timeout", checkTimeout),
		Remediation: "Run `wasctl doctor --check " + check.ID() + "` in isolation to investigate.",
	}
}

// sortFindings orders findings: critical fail → problem fail → warning fail →
// other fail → pass → info → skip → error (check-broken).
func sortFindings(findings []Finding) {
	priority := func(f Finding) int {
		if f.Status == StatusFail {
			switch f.Severity {
			case SeverityCritical:
				return 0
			case SeverityProblem:
				return 1
			case SeverityWarning:
				return 2
			default:
				return 3
			}
		}
		switch f.Status {
		case StatusPass:
			return 4
		case StatusInfo:
			return 5
		case StatusSkip:
			return 6
		case StatusError:
			return 7
		}
		return 8
	}
	sort.SliceStable(findings, func(i, j int) bool {
		return priority(findings[i]) < priority(findings[j])
	})
}
