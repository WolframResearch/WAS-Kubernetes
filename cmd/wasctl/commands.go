package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/inspect"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

// ── wasctl info ───────────────────────────────────────────────────────────────

func runInfo(ctx context.Context, cfg *config.Config, watch bool, output, sections string) error {
	id, err := tools.GetCallerIdentity(ctx, cfg.Region.Value)
	if err != nil {
		return fmt.Errorf("get caller identity: %w", err)
	}

	w, err := workspace.Open(ctx, cfg.MetaRegion.Value, id.Account, cfg.ClusterName.Value)
	if err != nil {
		return err
	}
	if err := w.MaterializeTempDir(); err != nil {
		return err
	}
	defer w.Close()

	kubeconfigPath, err := w.Kubeconfig(ctx)
	if err != nil {
		return err
	}
	contextName := w.ContextName()

	var sectionFilter []string
	if sections != "" {
		sectionFilter = strings.Split(sections, ",")
	}

	insp := inspect.New(w, kubeconfigPath, contextName)

	if watch && useTUI(cfg) {
		return runInfoWatch(ctx, insp, output, sectionFilter)
	}

	report, err := insp.Inspect(ctx, sectionFilter)
	if err != nil {
		return err
	}
	return renderReport(report, output, os.Stdout)
}

func runInfoWatch(ctx context.Context, insp *inspect.Inspector, output string, sections []string) error {
	for {
		report, _ := insp.Inspect(ctx, sections)
		// Clear screen and render.
		fmt.Print("\033[H\033[2J")
		_ = renderReport(report, output, os.Stdout)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
}

func renderReport(r *inspect.Report, format string, w *os.File) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	case "yaml":
		// Simple YAML: marshal to JSON then convert (avoids a yaml dep).
		data, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(w, string(data)) // JSON indented output; not YAML yet
		return nil
	default:
		return inspect.RenderText(r, w)
	}
}

// ── wasctl workspace list ─────────────────────────────────────────────────────

func runWorkspaceList(ctx context.Context, cfg *config.Config) error {
	clusters, err := listClusterSummaries(ctx, cfg)
	if err != nil {
		return err
	}
	if len(clusters) == 0 {
		if cfg.Cloud == "azure" {
			info, aerr := tools.GetAccountInfo(ctx)
			if aerr == nil {
				fmt.Printf("No clusters found in subscription %s.\n", info.ID)
				return nil
			}
		}
		fmt.Printf("No clusters found for cloud %s.\n", cfg.Cloud)
		return nil
	}
	printClusterTable(cfg.Cloud, clusters)
	return nil
}

// ── wasctl workspace info ─────────────────────────────────────────────────────

func runWorkspaceInfo(ctx context.Context, cfg *config.Config, clusterName string) error {
	id, err := tools.GetCallerIdentity(ctx, cfg.Region.Value)
	if err != nil {
		return fmt.Errorf("get caller identity: %w", err)
	}

	b, err := metabucket.Open(ctx, cfg.MetaRegion.Value, id.Account, clusterName)
	if err != nil {
		return err
	}

	meta, err := metabucket.ReadMetadata(ctx, b, clusterName)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(meta)
}

// ── wasctl workspace delete ───────────────────────────────────────────────────

func runWorkspaceDelete(ctx context.Context, cfg *config.Config, clusterName string) error {
	id, err := tools.GetCallerIdentity(ctx, cfg.Region.Value)
	if err != nil {
		return fmt.Errorf("get caller identity: %w", err)
	}

	b, err := metabucket.Open(ctx, cfg.MetaRegion.Value, id.Account, clusterName)
	if err != nil {
		return err
	}

	meta, err := metabucket.ReadMetadata(ctx, b, clusterName)
	if err != nil {
		return err
	}

	if meta.Status != "destroyed" {
		return fmt.Errorf(
			"cluster %q has status %q (expected 'destroyed').\n"+
				"Run 'wasctl destroy' before deleting the workspace.",
			clusterName, meta.Status,
		)
	}

	fmt.Printf("This will permanently delete the workspace for cluster %q\n", clusterName)
	fmt.Printf("(account: %s, region: %s)\n\n", id.Account, cfg.MetaRegion.Value)
	fmt.Printf("To confirm, type the cluster name: ")

	reader := bufio.NewReader(os.Stdin)
	typed, _ := reader.ReadString('\n')
	if strings.TrimSpace(typed) != clusterName {
		fmt.Println("Cluster name did not match. Aborting.")
		return nil
	}

	// Delete all known workspace keys. S3 has no delete-prefix API;
	// we enumerate the canonical keys. Not-found is silently ignored.
	keys := []string{
		metabucket.WorkspaceMetaKey(clusterName),
		metabucket.BackendHCLKey(clusterName),
		metabucket.BootstrapStateKey(clusterName),
		metabucket.BootstrapStateBackupKey(clusterName),
	}
	var deleteErr error
	for _, key := range keys {
		if err := b.Delete(ctx, key); err != nil {
			deleteErr = err
		}
	}
	if deleteErr != nil {
		return fmt.Errorf("workspace delete incomplete: %w", deleteErr)
	}
	fmt.Printf("[✓] Workspace for cluster %q deleted.\n", clusterName)
	return nil
}

// ── wasctl unlock ─────────────────────────────────────────────────────────────

func runUnlock(ctx context.Context, cfg *config.Config, clusterName string) error {
	id, err := tools.GetCallerIdentity(ctx, cfg.Region.Value)
	if err != nil {
		return fmt.Errorf("get caller identity: %w", err)
	}

	fmt.Printf("Force-releasing cluster lock for %q (account %s).\n\n", clusterName, id.Account)
	fmt.Println("WARNING: Only do this if the process that held the lock has died.")
	fmt.Println("If another wasctl is actively running, this will cause data corruption.")
	fmt.Println()
	fmt.Printf("To confirm, type the cluster name: ")

	reader := bufio.NewReader(os.Stdin)
	typed, _ := reader.ReadString('\n')
	if strings.TrimSpace(typed) != clusterName {
		fmt.Println("Cluster name did not match. Aborting.")
		return nil
	}

	lk, err := metabucket.NewLock(ctx, cfg.MetaRegion.Value, id.Account, clusterName)
	if err != nil {
		return err
	}
	if err := lk.ForceRelease(ctx); err != nil {
		return fmt.Errorf("force release lock: %w", err)
	}
	fmt.Printf("[✓] Lock released for cluster %q.\n", clusterName)
	return nil
}
