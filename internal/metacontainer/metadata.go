package metacontainer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
)

// ReadMetadata fetches and parses workspace.json for clusterName from the container.
// Returns ErrWorkspaceNotFound if the key doesn't exist.
// Returns the same *metabucket.Metadata type used by the AWS path so callers
// (workspace package, stages, inspect) need no branching on the return type.
func ReadMetadata(ctx context.Context, c *Container, clusterName string) (*metabucket.Metadata, error) {
	data, err := c.Get(ctx, WorkspaceMetaKey(clusterName))
	if err != nil {
		var nf *ErrNotFound
		if errors.As(err, &nf) {
			return nil, &ErrWorkspaceNotFound{
				ClusterName:    clusterName,
				SubscriptionID: c.subscriptionID,
			}
		}
		return nil, fmt.Errorf("read workspace metadata: %w", err)
	}

	var m metabucket.Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse workspace.json: %w", err)
	}
	if m.SchemaVersion > 1 {
		return nil, fmt.Errorf(
			"workspace %q was created by a newer version of wasctl (schema %d, this binary understands %d).\n"+
				"Please upgrade wasctl.",
			clusterName, m.SchemaVersion, 1,
		)
	}
	return &m, nil
}

// WriteMetadata serialises m to workspace.json in the meta container.
func WriteMetadata(ctx context.Context, c *Container, m *metabucket.Metadata, wasctlVersion string) error {
	m.LastModifiedAt = time.Now().UTC()
	m.LastModifiedBy = "wasctl " + wasctlVersion
	m.SchemaVersion = 1

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workspace.json: %w", err)
	}
	if err := c.Put(ctx, WorkspaceMetaKey(m.ClusterName), data); err != nil {
		return fmt.Errorf("write workspace.json: %w", err)
	}
	return nil
}

// NewAzureMetadata returns a Metadata initialised for a new Azure cluster.
// ClusterARN is left empty and populated by the infra stage once the AKS
// cluster is provisioned.
func NewAzureMetadata(clusterName, subscriptionID, resourceGroup, location, stateRG, stateAccount, wasctlVersion string) *metabucket.Metadata {
	now := time.Now().UTC()
	return &metabucket.Metadata{
		SchemaVersion:            1,
		Cloud:                    "azure",
		ClusterName:              clusterName,
		AzureSubscriptionID:      subscriptionID,
		AzureResourceGroup:       resourceGroup,
		AzureLocation:            location,
		AzureStateResourceGroup:  stateRG,
		AzureStateStorageAccount: stateAccount,
		CreatedAt:                now,
		CreatedBy:                "wasctl " + wasctlVersion,
		LastModifiedAt:           now,
		LastModifiedBy:           "wasctl " + wasctlVersion,
		Status:                   "installing",
	}
}
