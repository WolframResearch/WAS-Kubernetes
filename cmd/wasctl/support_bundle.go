package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/supportbundle"
)

var (
	flagBundleOutput       string
	flagBundleInclude      string
	flagBundleExclude      string
	flagBundleMaxLogLines  int
	flagBundleNoCloudTrail bool
)

var supportBundleCmd = &cobra.Command{
	Use:          "support-bundle",
	Short:        "Collect a diagnostic support bundle for Wolfram support",
	SilenceUsage: true,
	Long: `Collects cluster state, logs, and diagnostic information into a
tar.gz bundle for submission to Wolfram support.

Sensitive values (AWS keys, JWTs, passwords) are automatically redacted by
the sanitizer. Review the bundle before sending if you have concerns about
residual sensitive data. Use encrypted email or your ticket system's secure
upload for transit.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return runSupportBundle(cmd.Context(), cfg)
	},
}

func runSupportBundle(ctx context.Context, cfg *config.Config) error {
	// Build run context to obtain kubeconfig (best-effort).
	rc := doctor.NewRunContext(ctx, cfg)
	defer rc.Cleanup()

	clusterName := cfg.ClusterName.Value
	if clusterName == "" {
		clusterName = "unknown"
	}

	// Determine output path.
	outPath := flagBundleOutput
	if outPath == "" {
		ts := time.Now().UTC().Format("20060102-150405")
		outPath = fmt.Sprintf("support-bundle-%s-%s.tar.gz", clusterName, ts)
	}

	// Parse include/exclude lists.
	var include, exclude []string
	if flagBundleInclude != "" {
		include = splitCSV(flagBundleInclude)
	}
	if flagBundleExclude != "" {
		exclude = splitCSV(flagBundleExclude)
	}

	opts := supportbundle.Options{
		Cluster:      clusterName,
		MaxLogLines:  flagBundleMaxLogLines,
		NoCloudTrail: flagBundleNoCloudTrail,
		Include:      include,
		Exclude:      exclude,
	}

	// Create output file.
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer f.Close()

	progress := supportbundle.NewProgressWriter(os.Stderr)

	err = supportbundle.Build(ctx, cfg, rc.Workspace,
		rc.Kubeconfig, rc.ContextName,
		opts, progress, f)
	if err != nil {
		_ = os.Remove(outPath)
		return fmt.Errorf("build bundle: %w", err)
	}

	stat, _ := f.Stat()
	var size int64
	if stat != nil {
		size = stat.Size()
	}
	progress.BundleDone(outPath, size)
	return nil
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func init() {
	f := supportBundleCmd.Flags()
	f.StringVar(&flagBundleOutput, "output", "",
		"output path (default: ./support-bundle-<cluster>-<timestamp>.tar.gz)")
	f.StringVar(&flagBundleInclude, "include", "",
		"comma-separated collectors to include (default: all)")
	f.StringVar(&flagBundleExclude, "exclude", "",
		"comma-separated collectors to exclude (e.g. logs,cloudtrail)")
	f.IntVar(&flagBundleMaxLogLines, "max-log-lines", 1000,
		"maximum log lines per pod")
	f.BoolVar(&flagBundleNoCloudTrail, "no-cloudtrail", false,
		"skip CloudTrail collection (requires extra IAM permissions)")
}
