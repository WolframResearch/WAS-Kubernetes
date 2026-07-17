// Package doctor implements the wasctl doctor diagnostic subsystem.
// Checks are registered in checks/registry.go and run in parallel by runner.go.
package doctor

import (
	"context"
	"sync"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

// Severity ranks how bad a failing check is.
type Severity int

const (
	SeverityInfo     Severity = iota // informational only
	SeverityWarning                  // degraded but not broken
	SeverityProblem                  // something is wrong
	SeverityCritical                 // nothing will work without fixing this
)

func (s Severity) String() string {
	switch s {
	case SeverityWarning:
		return "Warning"
	case SeverityProblem:
		return "Problem"
	case SeverityCritical:
		return "Critical"
	default:
		return "Info"
	}
}

// Status is the outcome of a check.
type Status int

const (
	StatusPass  Status = iota // check passed
	StatusFail                // check failed
	StatusSkip                // precondition not met
	StatusError               // check itself failed to run (timeout, panic, etc.)
	StatusInfo                // informational; no pass/fail judgement
)

// Category groups checks for display and filtering.
type Category int

const (
	CategoryEnvironment Category = iota // no cluster required
	CategoryCluster                     // requires a reachable cluster
	CategoryApplication                 // requires the WAS chart to be deployed
)

func (c Category) String() string {
	switch c {
	case CategoryCluster:
		return "Cluster"
	case CategoryApplication:
		return "Application"
	default:
		return "Environment"
	}
}

// RunContext is constructed once per doctor run and passed read-only to all
// check goroutines. Fields are immutable after NewRunContext returns;
// quotaCache is concurrency-safe by design (sync.Map).
type RunContext struct {
	Cfg                 *config.Config
	Workspace           *workspace.Workspace  // nil if unavailable
	Kubeconfig          string                // abs path to temp file; "" if unavailable
	ContextName         string                // kubectl context; "" if unavailable
	CallerID            *tools.CallerIdentity // nil if AWS creds unavailable (AWS only)
	AzureSubscriptionID string                // "" if Azure creds unavailable (Azure only)
	quotaCache          sync.Map              // keyed "svc/quota" → string value
	cleanup             func()
}

// Cleanup removes any temporary files created during RunContext construction
// (e.g., isolated kubeconfig). It is idempotent.
func (rc *RunContext) Cleanup() {
	if rc.cleanup != nil {
		rc.cleanup()
	}
}

// QuotaCached returns a previously cached service quota value.
func (rc *RunContext) QuotaCached(key string) (string, bool) {
	v, ok := rc.quotaCache.Load(key)
	if !ok {
		return "", false
	}
	return v.(string), true
}

// QuotaStore caches a service quota value for reuse within this run.
func (rc *RunContext) QuotaStore(key, value string) {
	rc.quotaCache.Store(key, value)
}

// Check is the interface every diagnostic check must satisfy.
type Check interface {
	// Name is a short human-readable label, e.g. "AWS credentials valid".
	Name() string
	// ID is a stable dot-separated identifier, e.g. "aws.credentials".
	ID() string
	Category() Category
	Description() string
	// Precondition returns (true, reason) if the check should be skipped.
	Precondition(ctx context.Context, rc *RunContext) (skip bool, reason string)
	// Run executes the check and returns a Finding.
	Run(ctx context.Context, rc *RunContext) Finding
	// SafeToFix returns true only for checks that can automatically repair.
	SafeToFix() bool
	// Fix attempts automatic repair. Only called when SafeToFix() == true.
	Fix(ctx context.Context, rc *RunContext) error
}

// Finding is the result of one check execution.
type Finding struct {
	CheckID     string
	CheckName   string
	Severity    Severity
	Status      Status
	Message     string
	Details     string // multi-line supplementary data
	Remediation string // markdown with fix steps
	DocsLink    string // fragment into docs/Troubleshooting.md
	Elapsed     time.Duration
}

// Summary holds per-status counts for the report header.
type Summary struct {
	Pass  int
	Fail  int
	Skip  int
	Error int
	Info  int
}

// Report is the full result of a doctor run.
type Report struct {
	GeneratedAt   time.Time
	Cluster       string
	WasctlVersion string
	Findings      []Finding
	Summary       Summary
	Elapsed       time.Duration
}
