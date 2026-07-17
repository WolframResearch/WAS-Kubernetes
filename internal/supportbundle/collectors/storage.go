package collectors

import (
	"context"
	"fmt"
)

// StorageCollector collects storage classes, PVCs, and cloud-specific storage info.
type StorageCollector struct{}

func (StorageCollector) Name() string { return "storage" }

func (StorageCollector) Collect(ctx context.Context, cc *CollectContext) ([]File, error) {
	if cc.Kubeconfig == "" {
		return nil, fmt.Errorf("no cluster access (kubeconfig unavailable)")
	}

	kargs := kubectlArgs(cc)
	var files []File

	// Storage classes.
	sc, err := runOutput(ctx, "kubectl", append(kargs, "get", "storageclass", "-o", "json")...)
	if err != nil {
		sc = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "storage/storage_classes.json", Content: sc})

	// PVCs across all namespaces.
	pvcs, err := runOutput(ctx, "kubectl",
		append(kargs, "get", "pvc", "--all-namespaces", "-o", "json")...)
	if err != nil {
		pvcs = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "storage/pvc_detail.json", Content: pvcs})

	// AWS: EFS describe.
	if cc.Cfg != nil && cc.Cfg.Cloud != "azure" {
		region := cc.Cfg.Region.Value
		efsInfo, err := runOutput(ctx, "aws", "efs", "describe-file-systems", "--region", region, "--output", "json")
		if err != nil {
			efsInfo = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
		}
		files = append(files, File{Path: "storage/efs_describe.json", Content: efsInfo})
	}

	// Azure: Azure Files describe.
	if cc.Cfg != nil && cc.Cfg.Cloud == "azure" {
		azFiles, err := runOutput(ctx, "az", "storage", "share", "list", "--output", "json")
		if err != nil {
			azFiles = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
		}
		files = append(files, File{Path: "storage/azurefiles_describe.json", Content: azFiles})
	}

	return files, nil
}
