package supportbundle

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/supportbundle/collectors"
	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

// Options configures a bundle build.
type Options struct {
	Cluster      string   // default: cfg.ClusterName.Value
	MaxLogLines  int      // default: 1000
	NoCloudTrail bool
	Include      []string // collector names to include; nil = all
	Exclude      []string // collector names to exclude
}

// Build collects diagnostic data from all collectors, assembles a tar.gz, and
// writes it to dst. Output is always plain tar.gz; sanitization handles
// secret redaction. Callers are responsible for secure transport.
func Build(
	ctx context.Context,
	cfg *config.Config,
	ws *workspace.Workspace,
	kubeconfig, contextName string,
	opts Options,
	progress *ProgressWriter,
	dst io.Writer,
) error {
	maxLines := opts.MaxLogLines
	if maxLines <= 0 {
		maxLines = 1000
	}

	cluster := opts.Cluster
	if cluster == "" && cfg != nil {
		cluster = cfg.ClusterName.Value
	}

	cc := &collectors.CollectContext{
		Cfg:          cfg,
		Workspace:    ws,
		Kubeconfig:   kubeconfig,
		ContextName:  contextName,
		MaxLogLines:  maxLines,
		NoCloudTrail: opts.NoCloudTrail,
	}

	colls := collectors.All()
	colls = filterCollectors(colls, opts.Include, opts.Exclude)

	progress.BundleStart(cluster)

	type result struct {
		name    string
		files   []collectors.File
		err     error
		elapsed time.Duration
	}

	results := make([]result, len(colls))
	var wg sync.WaitGroup
	wg.Add(len(colls))

	for i, c := range colls {
		i, c := i, c
		go func() {
			defer wg.Done()
			progress.CollectorStart(c.Name())
			t0 := time.Now()
			files, err := c.Collect(ctx, cc)
			elapsed := time.Since(t0)
			if err != nil {
				progress.CollectorFail(c.Name(), err)
			} else {
				progress.CollectorDone(c.Name(), elapsed, len(files))
			}
			results[i] = result{name: c.Name(), files: files, err: err, elapsed: elapsed}
		}()
	}
	wg.Wait()

	// Gather all files and record sections + redactions for the manifest.
	var allFiles []collectors.File
	var sections []string
	var allRedactions []string

	for _, r := range results {
		if r.err != nil {
			continue
		}
		sections = append(sections, r.name)
		for _, f := range r.files {
			if len(f.Redactions) > 0 {
				allRedactions = append(allRedactions, f.Redactions...)
			}
			allFiles = append(allFiles, f)
		}
	}

	now := time.Now().UTC()
	manifest := &Manifest{
		Version:             ManifestVersion,
		GeneratedAt:         now,
		WasctlVersion:       version.Version,
		SanitizationVersion: SanitizationVersion,
		Cluster:             cluster,
		Sections:            sections,
		Redactions:          allRedactions,
	}
	manifestJSON, err := manifest.JSON()
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	// manifest.json is always first.
	allFiles = append([]collectors.File{{Path: "manifest.json", Content: manifestJSON}}, allFiles...)

	return writeTarGz(dst, allFiles, now)
}

func writeTarGz(w io.Writer, files []collectors.File, modTime time.Time) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	for _, f := range files {
		hdr := &tar.Header{
			Name:    f.Path,
			Mode:    0644,
			Size:    int64(len(f.Content)),
			ModTime: modTime,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("tar header %s: %w", f.Path, err)
		}
		if _, err := tw.Write(f.Content); err != nil {
			return fmt.Errorf("tar write %s: %w", f.Path, err)
		}
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("tar close: %w", err)
	}
	return gz.Close()
}

func filterCollectors(colls []collectors.Collector, include, exclude []string) []collectors.Collector {
	if len(include) > 0 {
		set := make(map[string]bool, len(include))
		for _, n := range include {
			set[n] = true
		}
		var out []collectors.Collector
		for _, c := range colls {
			if set[c.Name()] {
				out = append(out, c)
			}
		}
		return out
	}
	if len(exclude) > 0 {
		set := make(map[string]bool, len(exclude))
		for _, n := range exclude {
			set[n] = true
		}
		var out []collectors.Collector
		for _, c := range colls {
			if !set[c.Name()] {
				out = append(out, c)
			}
		}
		return out
	}
	return colls
}
