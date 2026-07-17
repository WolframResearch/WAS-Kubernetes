package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metacontainer"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/report"
	"github.com/WolframResearch/WAS-Kubernetes/internal/stages"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
)

// clusterSummary is one row in the interactive cluster table.
type clusterSummary struct {
	Name         string
	Region       string
	Status       string
	LastModified time.Time
}

// ── Interactive menu ──────────────────────────────────────────────────────────

func interactiveMenu(ctx context.Context, cfg *config.Config) error {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println()
		fmt.Printf("Cloud: %s", cfg.Cloud)
		if cfg.Cloud == "azure" && cfg.AzureLocation.Value != "" {
			fmt.Printf("  (default location %s)", cfg.AzureLocation.Value)
		} else if cfg.Cloud == "aws" && cfg.Region.Value != "" {
			fmt.Printf("  (default region %s)", cfg.Region.Value)
		}
		fmt.Println()
		fmt.Println("  Tip: ./wasctl --cloud azure   or   ./wasctl --cloud aws")
		fmt.Println()

		clusters, listErr := listClusterSummaries(ctx, cfg)
		if listErr != nil {
			fmt.Printf("  Could not list clusters: %v\n", listErr)
			fmt.Println("  (Credentials missing or meta store unreachable — you can still install a new cluster.)")
			fmt.Println()
			clusters = nil
		} else {
			printClusterTable(cfg.Cloud, clusters)
		}

		fmt.Println("What would you like to do?")
		fmt.Println()
		n := 1
		selectN, installN, switchN, quitN := 0, 0, 0, 0
		if len(clusters) > 0 {
			fmt.Printf("  %d) Work with an existing cluster\n", n)
			selectN = n
			n++
		}
		fmt.Printf("  %d) Install a new cluster  (recommended)\n", n)
		installN = n
		n++
		fmt.Printf("  %d) Switch cloud provider\n", n)
		switchN = n
		n++
		fmt.Printf("  %d) Quit\n", n)
		quitN = n
		fmt.Println()
		def := installN
		if selectN > 0 {
			def = selectN
		}
		fmt.Printf("Choice [%d]: ", def)

		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)
		if choice == "" {
			choice = strconv.Itoa(def)
		}
		num, err := strconv.Atoi(choice)
		if err != nil {
			fmt.Println("Invalid choice.")
			continue
		}

		switch num {
		case selectN:
			if selectN == 0 {
				fmt.Println("Invalid choice.")
				continue
			}
			if err := selectAndManageCluster(ctx, cfg, reader, clusters); err != nil {
				return err
			}
		case installN:
			if err := interactiveNewInstall(ctx, cfg, reader); err != nil {
				return err
			}
		case switchN:
			interactiveSwitchCloud(reader, cfg)
		case quitN:
			fmt.Println("Quit.")
			return nil
		default:
			fmt.Println("Invalid choice.")
		}
	}
}

func interactiveSwitchCloud(reader *bufio.Reader, cfg *config.Config) {
	other := "azure"
	if cfg.Cloud == "azure" {
		other = "aws"
	}
	fmt.Printf("Switch to %s? [Y/n]: ", other)
	ans, _ := reader.ReadString('\n')
	ans = strings.TrimSpace(strings.ToLower(ans))
	if ans == "" || ans == "y" || ans == "yes" {
		cfg.Cloud = other
		fmt.Printf("Cloud set to %s.\n", cfg.Cloud)
	}
}

func selectAndManageCluster(ctx context.Context, cfg *config.Config, reader *bufio.Reader, clusters []clusterSummary) error {
	fmt.Println()
	fmt.Print("Cluster number (or name): ")
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" || strings.EqualFold(line, "q") {
		return nil
	}

	var chosen *clusterSummary
	if n, err := strconv.Atoi(line); err == nil {
		if n < 1 || n > len(clusters) {
			fmt.Println("No such cluster number.")
			return nil
		}
		chosen = &clusters[n-1]
	} else {
		for i := range clusters {
			if clusters[i].Name == line {
				chosen = &clusters[i]
				break
			}
		}
		if chosen == nil {
			fmt.Printf("No cluster named %q in this cloud account.\n", line)
			return nil
		}
	}

	applyClusterToConfig(cfg, *chosen)
	fmt.Printf("\nSelected cluster: %s (%s)\n", chosen.Name, chosen.Status)
	return clusterSubmenu(ctx, cfg, reader)
}

func applyClusterToConfig(cfg *config.Config, c clusterSummary) {
	cfg.ClusterName = config.Field[string]{Value: c.Name, Source: "interactive"}
	if c.Region == "" {
		return
	}
	if cfg.Cloud == "azure" {
		cfg.AzureLocation = config.Field[string]{Value: c.Region, Source: "workspace"}
	} else {
		cfg.Region = config.Field[string]{Value: c.Region, Source: "workspace"}
	}
}

func clusterSubmenu(ctx context.Context, cfg *config.Config, reader *bufio.Reader) error {
	for {
		fmt.Println()
		_ = showStatus(ctx, cfg)
		started := installInProgress(ctx, cfg)

		fmt.Printf("Cluster %q — what next?\n\n", cfg.ClusterName.Value)
		if started {
			fmt.Println("  1) Continue — run next pending stage")
			fmt.Println("  2) Run all remaining pending stages")
		} else {
			fmt.Println("  1) Start / resume installation for this cluster")
			fmt.Println("  2) Run next stage only")
		}
		fmt.Println("  3) Install a specific stage")
		fmt.Println("  4) Show configuration")
		fmt.Println("  5) Show cluster details (live)")
		fmt.Println("  6) Tear down this cluster  (destructive)")
		fmt.Println("  7) Back to cluster list")
		fmt.Println()
		fmt.Print("Choice [7]: ")

		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)
		if choice == "" {
			choice = "7"
		}

		runStages := func(toRun []stages.Stage) error {
			if len(toRun) == 0 {
				fmt.Println("✓ All stages already complete for this cluster.")
				return nil
			}
			if !confirmInstall(reader, cfg, toRun) {
				fmt.Println("Cancelled.")
				return nil
			}
			if useTUI(cfg) {
				return runWithTUI(ctx, cfg, toRun)
			}
			return runPlain(ctx, cfg, toRun, report.NewPlain(os.Stdout))
		}

		switch choice {
		case "1":
			toRun := collectPending(ctx, cfg, false, false)
			if started && len(toRun) > 1 {
				toRun = toRun[:1]
			}
			if err := runStages(toRun); err != nil {
				return err
			}
		case "2":
			toRun := collectPending(ctx, cfg, false, false)
			if !started && len(toRun) > 1 {
				toRun = toRun[:1]
			}
			if err := runStages(toRun); err != nil {
				return err
			}
		case "3":
			fmt.Println()
			fmt.Println("Stages: " + strings.Join(stages.Names(), "  "))
			fmt.Print("Stage name: ")
			name, _ := reader.ReadString('\n')
			name = strings.TrimSpace(name)
			s, err := stages.ByName(name)
			if err != nil {
				fmt.Println(err.Error())
				continue
			}
			if err := runStages([]stages.Stage{s}); err != nil {
				return err
			}
		case "4":
			cfg.Show()
		case "5":
			if err := showClusterDetails(ctx, cfg); err != nil {
				fmt.Printf("%v\n", err)
			}
		case "6":
			if err := runDestroy(ctx, cfg); err != nil {
				return err
			}
		case "7":
			return nil
		default:
			fmt.Println("Invalid choice.")
		}
	}
}

func interactiveNewInstall(ctx context.Context, cfg *config.Config, reader *bufio.Reader) error {
	toRun := collectPending(ctx, cfg, false, false)
	// Force a full pipeline view for a brand-new name: pendingStages may still
	// show preflight as ready. That's fine — wizard sets the real cluster name.
	if !runInstallWizard(reader, cfg) {
		fmt.Println("Cancelled.")
		return nil
	}
	// Recompute pending after wizard (name/region/cloud may have changed).
	toRun = collectPending(ctx, cfg, false, false)
	if len(toRun) == 0 {
		fmt.Println("✓ All stages already complete for this cluster name.")
		return nil
	}
	if !confirmInstall(reader, cfg, toRun) {
		fmt.Println("Cancelled.")
		return nil
	}
	if useTUI(cfg) {
		return runWithTUI(ctx, cfg, toRun)
	}
	return runPlain(ctx, cfg, toRun, report.NewPlain(os.Stdout))
}

func showClusterDetails(ctx context.Context, cfg *config.Config) error {
	if len(collectPending(ctx, cfg, false, false)) == 0 {
		return runInfo(ctx, cfg, false, "text", "")
	}
	// Fall back to workspace metadata + honest stage status.
	fmt.Println()
	fmt.Printf("Cluster %q is not fully deployed yet (app stage incomplete).\n", cfg.ClusterName.Value)
	fmt.Println("Install progress:")
	_ = showStatus(ctx, cfg)

	meta, err := readWorkspaceMeta(ctx, cfg)
	if err != nil {
		fmt.Printf("No workspace metadata yet: %v\n", err)
		fmt.Println("Use “Start / resume installation” to create infrastructure.")
		return nil
	}
	fmt.Println("Workspace metadata:")
	fmt.Printf("  Status:         %s\n", meta.Status)
	fmt.Printf("  Cloud:          %s\n", orDefault(meta.Cloud, "aws"))
	if meta.Cloud == "azure" {
		fmt.Printf("  Location:       %s\n", meta.AzureLocation)
		fmt.Printf("  Resource group: %s\n", meta.AzureResourceGroup)
	} else {
		fmt.Printf("  Region:         %s\n", meta.AWSRegion)
	}
	if meta.ClusterARN != "" {
		fmt.Printf("  Cluster ID:     %s\n", meta.ClusterARN)
	}
	if !meta.CreatedAt.IsZero() {
		fmt.Printf("  Created:        %s\n", meta.CreatedAt.UTC().Format("2006-01-02 15:04 UTC"))
	}
	if !meta.LastModifiedAt.IsZero() {
		fmt.Printf("  Last modified:  %s\n", meta.LastModifiedAt.UTC().Format("2006-01-02 15:04 UTC"))
	}
	return nil
}

type workspaceMetaView struct {
	Status            string
	Cloud             string
	AWSRegion         string
	AzureLocation     string
	AzureResourceGroup string
	ClusterARN        string
	CreatedAt         time.Time
	LastModifiedAt    time.Time
}

func readWorkspaceMeta(ctx context.Context, cfg *config.Config) (*workspaceMetaView, error) {
	if cfg.Cloud == "azure" {
		info, err := tools.GetAccountInfo(ctx)
		if err != nil {
			return nil, err
		}
		c, err := metacontainer.Open(ctx, info.ID, cfg.ClusterName.Value)
		if err != nil {
			return nil, err
		}
		meta, err := metacontainer.ReadMetadata(ctx, c, cfg.ClusterName.Value)
		if err != nil {
			return nil, err
		}
		return &workspaceMetaView{
			Status:             meta.Status,
			Cloud:              meta.Cloud,
			AzureLocation:      meta.AzureLocation,
			AzureResourceGroup: meta.AzureResourceGroup,
			ClusterARN:         meta.ClusterARN,
			CreatedAt:          meta.CreatedAt,
			LastModifiedAt:     meta.LastModifiedAt,
		}, nil
	}
	id, err := tools.GetCallerIdentity(ctx, cfg.Region.Value)
	if err != nil {
		return nil, err
	}
	b, err := metabucket.Open(ctx, cfg.MetaRegion.Value, id.Account, cfg.ClusterName.Value)
	if err != nil {
		return nil, err
	}
	meta, err := metabucket.ReadMetadata(ctx, b, cfg.ClusterName.Value)
	if err != nil {
		return nil, err
	}
	return &workspaceMetaView{
		Status:         meta.Status,
		Cloud:          meta.Cloud,
		AWSRegion:      meta.AWSRegion,
		ClusterARN:     meta.ClusterARN,
		CreatedAt:      meta.CreatedAt,
		LastModifiedAt: meta.LastModifiedAt,
	}, nil
}

// ── Cluster listing ───────────────────────────────────────────────────────────

func listClusterSummaries(ctx context.Context, cfg *config.Config) ([]clusterSummary, error) {
	if cfg.Cloud == "azure" {
		return listAzureClusterSummaries(ctx)
	}
	return listAWSClusterSummaries(ctx, cfg)
}

func listAWSClusterSummaries(ctx context.Context, cfg *config.Config) ([]clusterSummary, error) {
	id, err := tools.GetCallerIdentity(ctx, cfg.Region.Value)
	if err != nil {
		return nil, fmt.Errorf("aws credentials: %w", err)
	}
	b, err := metabucket.Open(ctx, cfg.MetaRegion.Value, id.Account, "")
	if err != nil {
		return nil, err
	}
	names, err := b.ListClusters(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]clusterSummary, 0, len(names))
	for _, name := range names {
		cb, err := metabucket.Open(ctx, cfg.MetaRegion.Value, id.Account, name)
		if err != nil {
			out = append(out, clusterSummary{Name: name, Status: "(error)"})
			continue
		}
		meta, err := metabucket.ReadMetadata(ctx, cb, name)
		if err != nil {
			out = append(out, clusterSummary{Name: name, Status: "(error)"})
			continue
		}
		out = append(out, clusterSummary{
			Name:         meta.ClusterName,
			Region:       meta.AWSRegion,
			Status:       orDefault(meta.Status, "unknown"),
			LastModified: meta.LastModifiedAt,
		})
	}
	return out, nil
}

func listAzureClusterSummaries(ctx context.Context) ([]clusterSummary, error) {
	info, err := tools.GetAccountInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("azure credentials: %w", err)
	}
	names, err := metacontainer.ListClustersInSubscription(ctx, info.ID)
	if err != nil {
		return nil, err
	}
	out := make([]clusterSummary, 0, len(names))
	for _, name := range names {
		c, err := metacontainer.Open(ctx, info.ID, name)
		if err != nil {
			out = append(out, clusterSummary{Name: name, Status: "(error)"})
			continue
		}
		meta, err := metacontainer.ReadMetadata(ctx, c, name)
		if err != nil {
			out = append(out, clusterSummary{Name: name, Status: "(error)"})
			continue
		}
		out = append(out, clusterSummary{
			Name:         meta.ClusterName,
			Region:       meta.AzureLocation,
			Status:       orDefault(meta.Status, "unknown"),
			LastModified: meta.LastModifiedAt,
		})
	}
	return out, nil
}

func printClusterTable(cloud string, clusters []clusterSummary) {
	regionLabel := "Region"
	if cloud == "azure" {
		regionLabel = "Location"
	}
	if len(clusters) == 0 {
		fmt.Println("No wasctl clusters found for this cloud account.")
		fmt.Println()
		return
	}
	fmt.Printf("Clusters (%s):\n\n", cloud)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  #\tName\t%s\tStatus\tLast modified\n", regionLabel)
	fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n",
		"─", strings.Repeat("─", 12), strings.Repeat("─", 12),
		strings.Repeat("─", 10), strings.Repeat("─", 16))
	for i, c := range clusters {
		mod := "—"
		if !c.LastModified.IsZero() {
			mod = c.LastModified.UTC().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\t%s\n", i+1, c.Name, orDefault(c.Region, "—"), c.Status, mod)
	}
	_ = tw.Flush()
	fmt.Println()
}
