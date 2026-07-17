package metabucket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// currentSchemaVersion is the workspace.json schema version this binary
// understands. Increment when adding new required fields.
const currentSchemaVersion = 1

// Metadata is the workspace.json schema. It records everything needed to
// locate and verify a cluster's resources from any machine.
//
// Cloud-routing: the Cloud field determines which provider-specific fields are
// populated. Empty Cloud means "aws" for backwards compatibility with workspace
// files created before Azure support was added.
type Metadata struct {
	SchemaVersion  int       `json:"schemaVersion"`
	ClusterName    string    `json:"clusterName"`
	AWSAccountID   string    `json:"awsAccountID"`
	AWSRegion      string    `json:"awsRegion"`
	CreatedAt      time.Time `json:"createdAt"`
	CreatedBy      string    `json:"createdBy"`
	LastModifiedAt time.Time `json:"lastModifiedAt"`
	LastModifiedBy string    `json:"lastModifiedBy"`
	// Populated by kubeconfig stage after first successful cluster connection.
	ClusterUID string `json:"clusterUID"`
	// Populated by infra stage. AWS: arn:aws:eks:…; Azure: /subscriptions/…/managedClusters/…
	ClusterARN string `json:"clusterARN"`
	// Populated by bootstrap stage from terraform outputs.
	StateBucket string `json:"stateBucket"`
	LockTable   string `json:"lockTable"`
	// Populated by app stage after helm install.
	IngressHost string `json:"ingressHost"`
	// "installing" | "active" | "destroyed"
	Status      string     `json:"status"`
	DestroyedAt *time.Time `json:"destroyedAt,omitempty"`

	// Cloud identifies the cloud provider. "aws" (default) or "azure".
	// Empty is treated as "aws" for backwards compatibility.
	Cloud string `json:"cloud,omitempty"`

	// Azure-specific fields — empty for AWS workspaces.
	AzureSubscriptionID      string `json:"azureSubscriptionID,omitempty"`
	AzureResourceGroup       string `json:"azureResourceGroup,omitempty"`
	AzureLocation            string `json:"azureLocation,omitempty"`
	AzureStateResourceGroup  string `json:"azureStateResourceGroup,omitempty"`
	AzureStateStorageAccount string `json:"azureStateStorageAccount,omitempty"`
}

// ReadMetadata fetches and parses workspace.json for clusterName.
// Returns ErrWorkspaceNotFound if the key doesn't exist.
func ReadMetadata(ctx context.Context, b *Bucket, clusterName string) (*Metadata, error) {
	data, err := b.Get(ctx, WorkspaceMetaKey(clusterName))
	if err != nil {
		var nf *ErrNotFound
		if errors.As(err, &nf) {
			return nil, &ErrWorkspaceNotFound{ClusterName: clusterName, AccountID: accountIDFromBucket(b.name)}
		}
		return nil, fmt.Errorf("read workspace metadata: %w", err)
	}

	var m Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse workspace.json: %w", err)
	}

	if m.SchemaVersion > currentSchemaVersion {
		return nil, fmt.Errorf(
			"workspace %q was created by a newer version of wasctl (schema %d, this binary understands %d).\n"+
				"Please upgrade wasctl.",
			clusterName, m.SchemaVersion, currentSchemaVersion,
		)
	}
	return &m, nil
}

// WriteMetadata serialises m to workspace.json in the meta bucket.
func WriteMetadata(ctx context.Context, b *Bucket, m *Metadata, version string) error {
	m.LastModifiedAt = time.Now().UTC()
	m.LastModifiedBy = "wasctl " + version
	m.SchemaVersion = currentSchemaVersion

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workspace.json: %w", err)
	}
	if err := b.Put(ctx, WorkspaceMetaKey(m.ClusterName), data); err != nil {
		return fmt.Errorf("write workspace.json: %w", err)
	}
	return nil
}

// NewMetadata returns a Metadata initialised for a new cluster.
// Status starts as "installing" until the app stage completes successfully.
func NewMetadata(clusterName, accountID, region, wasctlVersion string) *Metadata {
	now := time.Now().UTC()
	return &Metadata{
		SchemaVersion:  currentSchemaVersion,
		ClusterName:    clusterName,
		AWSAccountID:   accountID,
		AWSRegion:      region,
		CreatedAt:      now,
		CreatedBy:      "wasctl " + wasctlVersion,
		LastModifiedAt: now,
		LastModifiedBy: "wasctl " + wasctlVersion,
		Status:         "installing",
	}
}

// accountIDFromBucket extracts the account ID from a bucket name of the form
// "wolfram-wasctl-meta-<accountID>".
func accountIDFromBucket(name string) string {
	prefix := "wolfram-wasctl-meta-"
	if len(name) > len(prefix) {
		return name[len(prefix):]
	}
	return ""
}

// ErrWorkspaceNotFound is returned when a cluster's workspace.json doesn't
// exist in the meta bucket.
type ErrWorkspaceNotFound struct {
	ClusterName string
	AccountID   string
}

func (e *ErrWorkspaceNotFound) Error() string {
	return fmt.Sprintf(
		"no workspace found for cluster %q in account %s.\n\n"+
			"Possible causes:\n"+
			"  1. The cluster was destroyed previously.\n"+
			"  2. The cluster was created from a different AWS account.\n"+
			"  3. The cluster name is misspelled.\n\n"+
			"To see clusters known in this account:\n"+
			"  wasctl workspace list\n\n"+
			"If you know the cluster exists but the workspace is gone, see:\n"+
			"  docs/Operations.md (Recovery tips)",
		e.ClusterName, e.AccountID,
	)
}
