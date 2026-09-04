package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/WolframResearch/WAS-Kubernetes/internal/assets"
	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/report"
	"github.com/WolframResearch/WAS-Kubernetes/internal/repo"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
	"github.com/WolframResearch/WAS-Kubernetes/internal/stages"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tui"
	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
	"github.com/WolframResearch/WAS-Kubernetes/internal/versions"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

// ── Persistent flag variables ─────────────────────────────────────────────────

var (
	flagConfigFile  string
	flagMetaRegion  string
	flagRegion      string
	flagCluster     string
	flagK8sVersion  string
	flagIngressHost string
	flagNodeType    string
	flagNodeMin     string
	flagNodeDesired string
	flagNodeMax     string
	flagStateBucket string
	flagLockTable   string
	flagYes         bool
	flagDryRun      bool
	flagNoTUI       bool
	flagLocal       bool
	// install-only
	flagAll  bool
	flagSkip string
	// destroy-only
	flagDestroyStateBackend bool
	// cloud provider
	flagCloud string
	// Azure-specific
	flagAzureLocation  string
	flagAzureTenantID  string
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// ── Root command ──────────────────────────────────────────────────────────────

var rootCmd = &cobra.Command{
	Use:           "wasctl",
	Short:         "Wolfram Application Server deployment tool",
	Long:          helpText,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		// version and completion commands don't need embedded assets.
		if cmd.Name() == "version" || cmd.Name() == "completion" {
			return nil
		}

		// Startup GC: remove orphan /tmp/wasctl-* dirs older than 24h.
		if count, bytes := workspace.CleanOrphanTempDirs(24 * time.Hour); count > 0 {
			fmt.Fprintln(os.Stderr, workspace.FormatCleanupLog(count, bytes))
		}

		// Verify embedded assets unless --local bypasses them.
		if !flagLocal {
			if err := assets.VerifyEmbedded(); err != nil {
				fmt.Fprintf(os.Stderr,
					"\nERROR: wasctl binary was built without embedded assets.\n"+
						"This is a build issue. Rebuild with:\n"+
						"    go generate ./...\n"+
						"    go build ./cmd/wasctl\n"+
						"Or run with --local to use local .tf files from the repo checkout.\n\n"+
						"Details: %v\n\n", err)
				os.Exit(1)
			}
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, _ []string) error {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return cmd.Help()
		}
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return interactiveMenu(cmd.Context(), cfg)
	},
}

// ── install ───────────────────────────────────────────────────────────────────

var installCmd = &cobra.Command{
	Use:          "install [stage]",
	Short:        "Install WAS stack stages",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		var toRun []stages.Stage

		switch {
		case len(args) == 1:
			s, err := stages.ByName(args[0])
			if err != nil {
				return err
			}
			toRun = []stages.Stage{s}

		case flagAll:
			toRun = pendingStages(cmd.Context(), cfg, false)
			if len(toRun) == 0 {
				fmt.Fprintln(os.Stderr, "✓ All stages already complete.")
				return nil
			}

		default:
			for _, s := range stages.All() {
				if err := s.Check(cmd.Context(), cfg); err != nil {
					toRun = []stages.Stage{s}
					break
				}
			}
			if len(toRun) == 0 {
				fmt.Fprintln(os.Stderr, "✓ All stages already complete.")
				return nil
			}
		}

		if useTUI(cfg) {
			return runWithTUI(cmd.Context(), cfg, toRun)
		}
		return runPlain(cmd.Context(), cfg, toRun, report.NewPlain(os.Stdout))
	},
}

// ── status ────────────────────────────────────────────────────────────────────

var statusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show completion status of all stages",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return showStatus(cmd.Context(), cfg)
	},
}

// ── config show ───────────────────────────────────────────────────────────────

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration management",
}

var configShowCmd = &cobra.Command{
	Use:          "show",
	Short:        "Show resolved configuration and value sources",
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		cfg.Show()
		return nil
	},
}

// ── destroy ───────────────────────────────────────────────────────────────────

var destroyCmd = &cobra.Command{
	Use:          "destroy",
	Short:        "Tear down all deployed resources (destructive)",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return runDestroy(cmd.Context(), cfg)
	},
}

// ── version ───────────────────────────────────────────────────────────────────

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version",
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Printf("wasctl %s\n", version.Version)
	},
}

// ── init ──────────────────────────────────────────────────────────────────────

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&flagConfigFile, "config", "", "path to wasctl.conf (default: ./wasctl.conf)")
	pf.StringVar(&flagMetaRegion, "meta-region", "",
		"AWS region for wasctl meta bucket (default: us-east-1). Override for data-residency requirements.\nEnv: WAS_META_REGION")
	pf.StringVar(&flagRegion, "region", "", "AWS region (default: us-east-1)")
	pf.StringVar(&flagCluster, "cluster-name", "", "EKS cluster name (default: was-prod)")
	pf.StringVar(&flagK8sVersion, "k8s-version", "",
		fmt.Sprintf("Kubernetes version (default: %s AWS / %s Azure; last 3 minors from infra cluster_version)",
			versions.AWSClusterK8sDefault, versions.AzureClusterK8sDefault))
	pf.StringVar(&flagIngressHost, "ingress-host", "", "public DNS hostname for WAS ingress (AWS HTTPS: your domain, not *.elb.amazonaws.com; Azure: *.cloudapp.azure.com OK)")
	pf.StringVar(&flagNodeType, "node-type", "", "EC2 instance type (default: c5.2xlarge)")
	pf.StringVar(&flagNodeMin, "node-min", "", "min worker nodes (default: 2)")
	pf.StringVar(&flagNodeDesired, "node-desired", "", "desired worker nodes (default: 2)")
	pf.StringVar(&flagNodeMax, "node-max", "", "max worker nodes (default: 10)")
	pf.StringVar(&flagStateBucket, "state-bucket", "", "Terraform state S3 bucket name")
	pf.StringVar(&flagLockTable, "lock-table", "", "DynamoDB lock table name")
	pf.BoolVar(&flagYes, "yes", false, "skip non-destructive confirmation prompts")
	pf.BoolVar(&flagDryRun, "dry-run", false, "print commands without executing them")
	pf.BoolVar(&flagNoTUI, "no-tui", false,
		"disable live terminal UI (also auto-disabled in CI/pipe/dumb-term)")
	// --local is a developer-only flag: reads assets from local filesystem
	// instead of embedded FS. Hidden from default --help.
	pf.BoolVar(&flagLocal, "local", false,
		"read assets from local filesystem (developer iteration; requires repo checkout)")
	_ = pf.MarkHidden("local")
	pf.StringVar(&flagCloud, "cloud", "", "cloud provider: aws (default) or azure. Env: WAS_CLOUD")
	pf.StringVar(&flagAzureLocation, "location", "", "Azure region (default: eastus). Env: WAS_AZURE_LOCATION")
	pf.StringVar(&flagAzureTenantID, "tenant-id", "", "Azure AD tenant ID (required for Terraform provider). Env: WAS_AZURE_TENANT_ID")

	infoCmd.Flags().BoolVar(&flagInfoWatch, "watch", false, "refresh every 5 seconds (disables in non-TTY/CI)")
	infoCmd.Flags().StringVar(&flagInfoOutput, "output", "text", "output format: text, json, yaml")
	infoCmd.Flags().StringVar(&flagInfoSections, "sections", "", "comma-separated sections to show (default: all)")

	installCmd.Flags().BoolVar(&flagAll, "all", false,
		"run all stages in sequence (skip already-complete)")
	installCmd.Flags().StringVar(&flagSkip, "skip", "",
		"comma-separated add-ons to skip (addons stage)")

	destroyCmd.Flags().BoolVar(&flagDestroyStateBackend, "destroy-state-backend", true,
		"destroy Terraform state backend and wasctl meta store (S3/DynamoDB on AWS; state SA + meta SA/RG on Azure)")

	configCmd.AddCommand(configShowCmd)
	rootCmd.AddCommand(installCmd, statusCmd, configCmd, destroyCmd, versionCmd,
		infoCmd, workspaceCmd, unlockCmd, serveCmd, doctorCmd, supportBundleCmd)
	workspaceCmd.AddCommand(workspaceListCmd, workspaceInfoCmd, workspaceDeleteCmd)
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	// --version flag (Cobra sets this up automatically when Version is set).
	rootCmd.Version = version.Version
}

// ── Config loading ────────────────────────────────────────────────────────────

func loadConfig() (*config.Config, error) {
	var root string
	var err error
	if flagLocal {
		root, err = filepath.Abs(".")
		if err != nil {
			return nil, fmt.Errorf("resolve CWD: %w", err)
		}
		fmt.Fprintf(os.Stderr, "wasctl: --local mode — reading assets from %s\n        Do not use this in production installs.\n", root)
	} else {
		root, err = repo.Locate(".")
		if err != nil {
			// Without --local, locate from any parent dir. If not found, guide the user.
			root = "." // fall back gracefully; stages will still use embedded assets
		}
	}
	overrides := map[string]string{
		"meta-region":  flagMetaRegion,
		"region":       flagRegion,
		"cluster-name": flagCluster,
		"k8s-version":  flagK8sVersion,
		"ingress-host": flagIngressHost,
		"node-type":    flagNodeType,
		"node-min":     flagNodeMin,
		"node-desired": flagNodeDesired,
		"node-max":     flagNodeMax,
		"state-bucket": flagStateBucket,
		"lock-table":   flagLockTable,
		"skip":         flagSkip,
		"cloud":        flagCloud,
		"location":     flagAzureLocation,
		"tenant-id":    flagAzureTenantID,
	}
	cfg, err := config.Load(root, flagConfigFile, overrides)
	if err != nil {
		return nil, err
	}
	cfg.Yes = flagYes
	cfg.DryRun = flagDryRun
	cfg.NoTUI = flagNoTUI
	cfg.Local = flagLocal
	cfg.DestroyStateBackend = flagDestroyStateBackend
	return cfg, nil
}

// ── TUI detection ─────────────────────────────────────────────────────────────

// useTUI returns true when the interactive Bubble Tea install UI should run:
// a real TTY, sufficient terminal size, --no-tui unset, and no CI/NO_COLOR env.
func useTUI(cfg *config.Config) bool {
	if cfg.NoTUI {
		return false
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	for _, v := range []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI"} {
		if os.Getenv(v) != "" {
			return false
		}
	}
	t := os.Getenv("TERM")
	if t == "dumb" || t == "" {
		return false
	}
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w < 80 || h < 24 {
		return false
	}
	return true
}

// ── Orchestration ─────────────────────────────────────────────────────────────

// runOrchestrated runs stagesToRun in order and signals stage-level transitions
// through cond. stages call the runner.Reporter subset; the orchestrator calls
// the stage-level subset.
func runOrchestrated(ctx context.Context, cfg *config.Config, stagesToRun []stages.Stage, cond report.Conductor) error {
	return stages.RunOrchestrated(ctx, cfg, stagesToRun, cond)
}

func runWithTUI(ctx context.Context, cfg *config.Config, stagesToRun []stages.Stage) error {
	if len(stagesToRun) == 0 {
		return nil
	}
	names := make([]string, len(stagesToRun))
	for i, s := range stagesToRun {
		names[i] = s.Label()
	}

	orchCtx, cancelOrch := context.WithCancel(ctx)
	defer cancelOrch()

	startOrch := make(chan struct{})
	var startOnce sync.Once
	m := tui.NewModel(names, func() {
		startOnce.Do(func() { close(startOrch) })
	})
	prog := tea.NewProgram(m, tea.WithAltScreen())
	rep := tui.NewTUIReporter(prog)

	// Buffer of 1: the goroutine writes exactly once (when runOrchestrated returns),
	// which is guaranteed to happen before InstallDoneMsg triggers tea.Quit, so
	// the receive below never blocks.
	orchErrCh := make(chan error, 1)
	go func() {
		select {
		case <-startOrch:
		case <-orchCtx.Done():
			orchErrCh <- fmt.Errorf("install UI closed before work started")
			return
		}
		orchErrCh <- runOrchestrated(orchCtx, cfg, stagesToRun, rep)
	}()

	_, tuiErr := prog.Run()
	// Unblock the orchestrator waiter if the UI exited before Init fired onReady
	// (e.g. immediate quit). Harmless if work already finished.
	cancelOrch()
	if tuiErr != nil {
		<-orchErrCh // drain
		return tuiErr
	}

	// Alt-screen has been restored — the TUI frame is gone from the terminal.
	// Print a plain-text banner into the normal scrollback so there is a clear
	// visual break between whatever was on screen before (menu, prompt, etc.)
	// and the outcome. On error main() prints the detail; we print the header.
	orchErr := <-orchErrCh
	sep := strings.Repeat("─", 60)
	if orchErr == nil {
		fmt.Fprintf(os.Stderr, "\n\x1b[32m%s\n✓  wasctl — installation complete\n%s\x1b[0m\n", sep, sep)
	} else {
		fmt.Fprintf(os.Stderr, "\n\x1b[31m%s\n✗  wasctl — installation failed\n%s\x1b[0m\n", sep, sep)
	}
	return orchErr
}

func runPlain(ctx context.Context, cfg *config.Config, stagesToRun []stages.Stage, cond report.Conductor) error {
	return runOrchestrated(ctx, cfg, stagesToRun, cond)
}

// ── Status ────────────────────────────────────────────────────────────────────

func showStatus(ctx context.Context, cfg *config.Config) error {
	fmt.Printf("\n  %-20s  %s\n", "Stage", "Status")
	fmt.Printf("  %-20s  %s\n", strings.Repeat("─", 20), strings.Repeat("─", 14))
	// Stages are sequential: once one is incomplete, later Checks (helm/kubectl)
	// are expensive and usually meaningless — mark them pending without probing.
	blocked := false
	for _, s := range stages.All() {
		if blocked {
			fmt.Printf("  %-20s  ○ pending\n", s.Name())
			continue
		}
		if err := s.Check(ctx, cfg); err == nil {
			// Preflight Check means tools + credentials are OK, not that an
			// install was previously run — label it differently.
			if s.Name() == "preflight" {
				fmt.Printf("  %-20s  ✓ ready\n", s.Name())
			} else {
				fmt.Printf("  %-20s  ✓ complete\n", s.Name())
			}
		} else {
			fmt.Printf("  %-20s  ○ pending\n", s.Name())
			blocked = true
		}
	}
	fmt.Println()
	return nil
}

// installInProgress reports whether any stage after preflight is already done
// (workspace / infra exist). Used to pick "Start" vs "Continue" menu wording.
func installInProgress(ctx context.Context, cfg *config.Config) bool {
	for _, s := range stages.All() {
		if s.Name() == "preflight" {
			continue
		}
		if err := s.Check(ctx, cfg); err == nil {
			return true
		}
		// First incomplete post-preflight stage — later stages cannot be done yet.
		return false
	}
	return false
}

// pendingStages returns stages whose Check reports "not yet done". When force
// is true, all stages are returned unconditionally.
func pendingStages(ctx context.Context, cfg *config.Config, force bool) []stages.Stage {
	return collectPending(ctx, cfg, force, true)
}

func collectPending(ctx context.Context, cfg *config.Config, force, verbose bool) []stages.Stage {
	var out []stages.Stage
	blocked := false
	for _, s := range stages.All() {
		if !force {
			if blocked {
				out = append(out, s)
				continue
			}
			if err := s.Check(ctx, cfg); err == nil {
				if verbose {
					if s.Name() == "preflight" {
						fmt.Fprintf(os.Stderr, "  ✓ %s — prerequisites OK, skipping\n", s.Label())
					} else {
						fmt.Fprintf(os.Stderr, "  ✓ %s — already complete, skipping\n", s.Label())
					}
				}
				continue
			}
			blocked = true
		}
		out = append(out, s)
	}
	return out
}

// ── Destroy ───────────────────────────────────────────────────────────────────

func runDestroy(ctx context.Context, cfg *config.Config) error {
	if cfg.Cloud == "aws" {
		if id, err := tools.GetCallerIdentity(ctx, cfg.Region.Value); err == nil {
			cfg.DeriveBucketNames(id.Account)
		}
	}

	fmt.Println()
	fmt.Printf("\x1b[31mWARNING: This will permanently destroy:\x1b[0m\n")
	if cfg.Cloud == "azure" {
		fmt.Printf("  - The AKS cluster '%s-aks' in resource group '%s-rg'\n", cfg.ClusterName.Value, cfg.ClusterName.Value)
		fmt.Printf("  - Azure Files share and blob storage (resource + nodefile)\n")
		fmt.Printf("  - VNet / networking and the managed node resource group (MC_*)\n")
		fmt.Printf("  - All Wolfram services deployed in the cluster\n")
		fmt.Printf("  - All data stored by your Wolfram services\n")
		if cfg.DestroyStateBackend {
			fmt.Printf("  - The Terraform state storage account (and shared state RG if unused)\n")
			fmt.Printf("  - The wasctl meta storage account and resource group wolfram-%s-meta\n", cfg.ClusterName.Value)
		} else {
			fmt.Printf("  (pass --destroy-state-backend to also delete state + meta storage)\n")
		}
	} else {
		fmt.Printf("  - The EKS cluster '%s'\n", cfg.ClusterName.Value)
		fmt.Printf("  - The EFS filesystem (all data on it)\n")
		fmt.Printf("  - All Wolfram services deployed in the cluster\n")
		fmt.Printf("  - All data stored by your Wolfram services\n")
		if cfg.DestroyStateBackend {
			fmt.Printf("  - The Terraform state bucket '%s'\n", cfg.StateBucket.Value)
			fmt.Printf("  - The DynamoDB lock table '%s'\n", cfg.LockTable.Value)
		} else {
			fmt.Printf("  (pass --destroy-state-backend to also delete the state bucket)\n")
		}
	}
	fmt.Println()
	fmt.Println("This cannot be undone.")
	fmt.Println()
	fmt.Printf("To confirm, type the cluster name (%s): ", cfg.ClusterName.Value)

	reader := bufio.NewReader(os.Stdin)
	typed, _ := reader.ReadString('\n')
	typed = strings.TrimSpace(typed)
	if typed != cfg.ClusterName.Value {
		fmt.Println("Cluster name did not match. Aborting.")
		return nil
	}

	// Destroy in reverse installation order.
	all := stages.All()
	n := len(all)
	rev := make([]stages.Stage, n)
	for i, s := range all {
		rev[n-1-i] = s
	}
	// Unless --destroy-state-backend, omit the bootstrap stage.
	if !cfg.DestroyStateBackend {
		var filtered []stages.Stage
		for _, s := range rev {
			if s.Name() != "bootstrap" {
				filtered = append(filtered, s)
			}
		}
		rev = filtered
	}

	cond := report.NewPlain(os.Stdout)
	r := runner.ExecRunner{}
	var firstErr error
	for _, s := range rev {
		cond.StageStart("Destroying: " + s.Label())
		if err := s.Destroy(ctx, cfg, r, cond); err != nil {
			cond.StageFail(err)
			if firstErr == nil {
				firstErr = err
			}
		} else {
			cond.StageDone()
		}
	}
	return firstErr
}

// ── Interactive menu ──────────────────────────────────────────────────────────
// See menu.go for the cluster-first interactive UI.

// confirmInstall prints the resolved config and the stages about to run, then
// asks for an explicit yes. Default is no so Enter alone does not start work.
func confirmInstall(reader *bufio.Reader, cfg *config.Config, toRun []stages.Stage) bool {
	cfg.Show()
	if cfg.AddonsSkip.Value != "" {
		fmt.Printf("  %-36s %s  (%s)\n", "Skip addons:", cfg.AddonsSkip.Value, cfg.AddonsSkip.Source)
	} else {
		fmt.Printf("  %-36s %s\n", "Skip addons:", "(none — install optional add-ons)")
	}
	if cfg.KafkaMode.Value != "" {
		fmt.Printf("  %-36s %s  (%s)\n", "Kafka mode:", cfg.KafkaMode.Value, cfg.KafkaMode.Source)
	}
	if cfg.KafkaBootstrapServers.Value != "" {
		fmt.Printf("  %-36s %s  (%s)\n", "Kafka bootstrap:", cfg.KafkaBootstrapServers.Value, cfg.KafkaBootstrapServers.Source)
	}
	fmt.Println()
	fmt.Println("Stages to run:")
	for i, s := range toRun {
		fmt.Printf("  %d. %s  (%s)\n", i+1, s.Name(), s.EstimateText())
	}
	fmt.Println()
	if cfg.IngressHost.Value == "" {
		if cfg.Cloud == "aws" {
			fmt.Println("Note: ingress host unset — AWS will use the ELB hostname over HTTP.")
			fmt.Println("      HTTPS needs a custom DNS name: --ingress-host was.example.com (Let's Encrypt cannot use *.elb.amazonaws.com).")
		} else {
			fmt.Println("Note: ingress host is unset — wasctl will use the load-balancer hostname after ingress is ready.")
		}
		fmt.Println()
	}
	fmt.Print("Proceed with this configuration? [y/N]: ")
	ans, _ := reader.ReadString('\n')
	ans = strings.TrimSpace(strings.ToLower(ans))
	return ans == "y" || ans == "yes"
}

// ── wasctl info ───────────────────────────────────────────────────────────────

var (
	flagInfoWatch    bool
	flagInfoOutput   string
	flagInfoSections string
)

var infoCmd = &cobra.Command{
	Use:          "info",
	Short:        "Show cluster health, workloads, Kafka, and storage status",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return runInfo(cmd.Context(), cfg, flagInfoWatch, flagInfoOutput, flagInfoSections)
	},
}

// ── wasctl workspace ──────────────────────────────────────────────────────────

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Manage cloud workspaces for deployed clusters",
}

var workspaceListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List known clusters in this AWS account",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return runWorkspaceList(cmd.Context(), cfg)
	},
}

var workspaceInfoCmd = &cobra.Command{
	Use:          "info <cluster-name>",
	Short:        "Show workspace.json for a cluster",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return runWorkspaceInfo(cmd.Context(), cfg, args[0])
	},
}

var workspaceDeleteCmd = &cobra.Command{
	Use:          "delete <cluster-name>",
	Short:        "Delete workspace for a destroyed cluster (requires typed confirmation)",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return runWorkspaceDelete(cmd.Context(), cfg, args[0])
	},
}

// ── wasctl unlock ─────────────────────────────────────────────────────────────

var unlockCmd = &cobra.Command{
	Use:          "unlock <cluster-name>",
	Short:        "Force-release a stuck cluster lock (requires typed confirmation)",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return runUnlock(cmd.Context(), cfg, args[0])
	},
}

// ── Help text ─────────────────────────────────────────────────────────────────

var helpText = fmt.Sprintf(`NAME
    wasctl — Wolfram Application Server deployment tool

SYNOPSIS
    wasctl [subcommand] [flags]

SUBCOMMANDS
    (no args)           Launch interactive guided menu
    install --all       Run all stages in sequence (skip completed ones)
    install <stage>     Run a specific stage
    status              Show completion status of all stages
    config show         Show resolved configuration and sources
    destroy             Tear down all deployed resources (destructive)
    serve               Launch the wasctl web UI (default: localhost:8765)
    version             Print version

STAGES (in order)
    preflight           Verify required tools and AWS credentials
    bootstrap           Create Terraform state bucket + DynamoDB lock table
    backend             Write infra/aws/stack/backend.hcl from bootstrap outputs
    infra               Provision VPC, EKS, EFS, S3 buckets, IAM roles (~25–35 min)
    kubeconfig          Update local kubeconfig via aws eks update-kubeconfig
    addons              Install cluster add-ons (ingress-nginx, Strimzi, EFS CSI, etc.)
    app                 Install/upgrade the WAS Helm chart

FLAGS
    --all                   (install) Run all stages
    --yes                   Skip non-destructive confirmation prompts
    --dry-run               Print commands without executing them
    --ingress-host <host>   DNS hostname for WAS ingress. If omitted, the ELB
                            hostname is auto-detected from the ingress-nginx
                            LoadBalancer service when ingress.host is omitted.
    --local                 Use local repo files from CWD instead of walking up
                            to find the repo root. Use this when testing before
                            pushing to GitHub or when the binary is run from the
                            repo directory directly.
    --region <region>       AWS region (default: us-east-1)
    --cluster-name <name>   EKS cluster name (default: was-prod)
    --k8s-version <ver>     Kubernetes version (default from infra cluster_version:
                            AWS %s / Azure %s; install offers last 3 minors)
    --node-type <type>      EC2 instance type (default: c5.2xlarge)
    --node-min <n>          Min worker nodes (default: 2)
    --node-desired <n>      Desired worker nodes (default: 2)
    --node-max <n>          Max worker nodes (default: 10)
    --state-bucket <name>   Terraform state S3 bucket name (derived if not set)
    --lock-table <name>     DynamoDB lock table name (derived if not set)
    --skip <list>           (addons) Comma-separated add-ons to skip.
                            Valid: ingress,strimzi,metrics-server,
                                   prometheus,prometheus-adapter,csi,cert-manager
    --destroy-state-backend (destroy) Also destroy the Terraform state bucket
    --no-tui                Disable the live terminal UI; use plain text output
                            (also triggered automatically when stdout is piped,
                             CI=true, NO_COLOR is set, TERM=dumb, or terminal
                             is smaller than 80x24)

ENVIRONMENT VARIABLES
    WAS_REGION            AWS region
    WAS_CLUSTER_NAME      EKS cluster name
    WAS_K8S_VERSION       Kubernetes version
    WAS_INGRESS_HOST      Ingress hostname
    WAS_NODE_TYPE         EC2 instance type
    WAS_NODE_MIN          Min worker nodes
    WAS_NODE_DESIRED      Desired worker nodes
    WAS_NODE_MAX          Max worker nodes
    WAS_ADDONS_SKIP       Comma-separated add-ons to skip

CONFIGURATION FILE
    If ./wasctl.conf exists in the repo root, it is sourced as KEY=VALUE
    assignments. CLI flags and environment variables take precedence.

EXAMPLES
    # Quickstart (local repo, ingress host auto-detected from ELB)
    ./wasctl install --all --local

    # Quickstart with explicit hostname
    ./wasctl install --all --local --ingress-host was.example.com

    # Interactive guided install
    ./wasctl --local

    # Install only the infrastructure stage
    ./wasctl install infra --region eu-west-1

    # Dry-run all stages
    ./wasctl install --all --dry-run --ingress-host was.example.com

    # Check what's deployed
    ./wasctl status

    # Tear down everything
    ./wasctl destroy`, versions.AWSClusterK8sDefault, versions.AzureClusterK8sDefault)
