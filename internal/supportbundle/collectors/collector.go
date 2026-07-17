// Package collectors implements individual data-collection units for the support bundle.
package collectors

import (
	"context"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

// File is a single file entry in the support bundle.
type File struct {
	Path       string   // relative to bundle root, e.g. "system/os.txt"
	Content    []byte
	Redactions []string // dot-paths of redacted keys (for manifest.json)
}

// CollectContext carries shared context for all collectors in one bundle run.
type CollectContext struct {
	Cfg          *config.Config
	Workspace    *workspace.Workspace // nil if unavailable
	Kubeconfig   string               // abs path to temp kubeconfig; "" if unavailable
	ContextName  string               // kubectl context; "" if unavailable
	MaxLogLines  int                  // max log lines per pod (default 1000)
	NoCloudTrail bool                 // skip CloudTrail collection
}

// Collector collects one section of the support bundle.
type Collector interface {
	// Name is a short identifier used for progress display and --include/--exclude flags.
	Name() string
	// Collect gathers files for this section. Returning an error causes the
	// section to be marked skipped in the bundle manifest.
	Collect(ctx context.Context, cc *CollectContext) ([]File, error)
}

// text returns a File with the given path and text content.
func text(path, content string) File {
	return File{Path: path, Content: []byte(content)}
}
