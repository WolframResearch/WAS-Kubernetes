package handlers

import (
	"context"

	"github.com/WolframResearch/WAS-Kubernetes/internal/audit"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metacontainer"
)

// writeClusterAudit appends a best-effort audit entry for a cluster mutation.
// Failures are ignored — audit must never block install/destroy/rerun.
func writeClusterAudit(ctx context.Context, cloud, metaRegion, accountID, clusterName, action, result string) {
	if accountID == "" {
		return
	}
	if cloud == "azure" {
		if c, err := metacontainer.Open(ctx, accountID, clusterName); err == nil {
			alog := audit.NewMetaContainerLog(c, clusterName)
			_ = alog.Log(ctx, clusterName, action, nil, result)
		}
		return
	}
	if b, err := metabucket.Open(ctx, metaRegion, accountID, clusterName); err == nil {
		alog := audit.NewMetaBucketLog(b, clusterName)
		_ = alog.Log(ctx, clusterName, action, nil, result)
	}
}
