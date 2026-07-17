package handlers

import (
	"context"
	"fmt"

	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metacontainer"
)

// reportStore is satisfied by *metabucket.Bucket and *metacontainer.Container.
type reportStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, data []byte) error
}

// openReportStore opens the cloud meta store used for doctor reports.
func openReportStore(ctx context.Context, metaRegion, clusterName, preferCloud string) (reportStore, string, error) {
	ws, cloud, accountID, err := openWorkspacePreferring(ctx, metaRegion, clusterName, preferCloud)
	if err != nil {
		return nil, "", err
	}
	defer ws.Close()
	return openReportStoreFromAccount(ctx, metaRegion, cloud, accountID, clusterName)
}

func openReportStoreFromAccount(ctx context.Context, metaRegion, cloud, accountID, clusterName string) (reportStore, string, error) {
	if cloud == "azure" {
		c, err := metacontainer.Open(ctx, accountID, clusterName)
		if err != nil {
			return nil, cloud, fmt.Errorf("meta container unavailable: %w", err)
		}
		return c, cloud, nil
	}
	b, err := metabucket.Open(ctx, metaRegion, accountID, clusterName)
	if err != nil {
		return nil, cloud, fmt.Errorf("meta bucket unavailable: %w", err)
	}
	return b, cloud, nil
}
