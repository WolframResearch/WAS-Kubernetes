package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor/checks"
)

var (
	flagDoctorOutput   string
	flagDoctorCategory string
	flagDoctorCheck    string
	flagDoctorSkip     string
	flagDoctorStrict   bool
	flagDoctorVerbose  bool
	flagDoctorFix      bool
	flagDoctorFixYes   bool
)

var doctorCmd = &cobra.Command{
	Use:          "doctor",
	Short:        "Run diagnostic checks on environment, cluster, and application",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return runDoctor(cmd.Context(), cfg)
	},
}

func init() {
	f := doctorCmd.Flags()
	f.StringVar(&flagDoctorOutput, "output", "text", "output format: text, json, markdown")
	f.StringVar(&flagDoctorCategory, "category", "",
		"run only one category: environment, cluster, application")
	f.StringVar(&flagDoctorCheck, "check", "", "run a single check by ID (e.g. aws.credentials)")
	f.StringVar(&flagDoctorSkip, "skip", "", "comma-separated check IDs to skip")
	f.BoolVar(&flagDoctorStrict, "strict", false,
		"exit non-zero if any finding is worse than Warning")
	f.BoolVar(&flagDoctorVerbose, "verbose", false, "show remediation steps for all failures")
	f.BoolVar(&flagDoctorFix, "fix", false, "attempt auto-fix for safe checks only (dry-run by default)")
	f.BoolVar(&flagDoctorFixYes, "yes", false, "apply fixes without prompting (requires --fix)")
}

func runDoctor(ctx context.Context, cfg *config.Config) error {
	// Build the check list
	selected := selectChecks(cfg.Cloud)
	if len(selected) == 0 {
		return fmt.Errorf("no checks match the specified filter")
	}

	// Build run context (best-effort; errors are surfaced by individual checks)
	rc := doctor.NewRunContext(ctx, cfg)
	defer rc.Cleanup()

	// Run
	report := doctor.Run(ctx, rc, selected)

	// Render
	switch strings.ToLower(flagDoctorOutput) {
	case "json":
		r := doctor.NewJSONRenderer(os.Stdout)
		if err := r.Render(report); err != nil {
			return fmt.Errorf("render json: %w", err)
		}

	case "markdown", "md":
		r := doctor.NewMarkdownRenderer(os.Stdout)
		r.Render(report)

	default: // "text"
		w := os.Stdout
		// Force plain output when piped
		if !term.IsTerminal(int(os.Stdout.Fd())) {
			flagDoctorVerbose = true // expand remediations for pipe/CI
		}
		r := doctor.NewTextRenderer(w)
		r.Render(report, flagDoctorVerbose)
	}

	// Exit code
	if flagDoctorStrict && report.Summary.Fail > 0 {
		// Count findings worse than Warning
		for _, f := range report.Findings {
			if f.Status == doctor.StatusFail && f.Severity >= doctor.SeverityProblem {
				return fmt.Errorf("doctor found %d problem(s); exit non-zero (--strict)", report.Summary.Fail)
			}
		}
	}
	if report.Summary.Fail > 0 {
		// Exit 2 for failures (1 is reserved for usage errors)
		os.Exit(2) //nolint:gocritic
	}
	return nil
}

func selectChecks(cloud string) []doctor.Check {
	all := checks.ForCloud(cloud)

	// Single check by ID
	if flagDoctorCheck != "" {
		c := checks.ByID(flagDoctorCheck)
		if c == nil {
			fmt.Fprintf(os.Stderr, "wasctl doctor: unknown check ID %q\n", flagDoctorCheck)
			fmt.Fprintf(os.Stderr, "Available checks:\n")
			for _, ch := range checks.All() {
				fmt.Fprintf(os.Stderr, "  %s\n", ch.ID())
			}
			return nil
		}
		return []doctor.Check{c}
	}

	// Skip set
	skipSet := make(map[string]bool)
	for _, id := range strings.Split(flagDoctorSkip, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			skipSet[id] = true
		}
	}

	// Category filter
	var catFilter *doctor.Category
	if flagDoctorCategory != "" {
		var cat doctor.Category
		switch strings.ToLower(flagDoctorCategory) {
		case "environment", "env":
			cat = doctor.CategoryEnvironment
		case "cluster":
			cat = doctor.CategoryCluster
		case "application", "app":
			cat = doctor.CategoryApplication
		default:
			fmt.Fprintf(os.Stderr, "wasctl doctor: unknown category %q (use: environment, cluster, application)\n",
				flagDoctorCategory)
			return nil
		}
		catFilter = &cat
	}

	var out []doctor.Check
	for _, c := range all {
		if skipSet[c.ID()] {
			continue
		}
		if catFilter != nil && c.Category() != *catFilter {
			continue
		}
		out = append(out, c)
	}
	return out
}
