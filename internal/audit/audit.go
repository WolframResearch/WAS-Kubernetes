// Package audit provides append-only audit logging to the per-cluster workspace.
// Each audit entry is one JSON line in clusters/<name>/audit.log (S3 or Azure blob).
package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metacontainer"
)

// AuditLog is the interface for recording cluster lifecycle events.
type AuditLog interface {
	Log(ctx context.Context, cluster, action string, params map[string]string, result string) error
}

// Entry is one JSON line in the audit log.
type Entry struct {
	Time    time.Time         `json:"time"`
	Cluster string            `json:"cluster"`
	Action  string            `json:"action"`
	Params  map[string]string `json:"params,omitempty"`
	Result  string            `json:"result"`
}

// AuditLogKey returns the S3 key for a cluster's audit log.
func AuditLogKey(clusterName string) string {
	return "clusters/" + clusterName + "/audit.log"
}

// Noop implements AuditLog but does nothing. Used when no meta bucket is available
// (e.g. chart-only mode against an external cluster).
type Noop struct{}

func (Noop) Log(_ context.Context, _, _ string, _ map[string]string, _ string) error { return nil }

// MetaBucketLog appends JSON lines to clusters/<name>/audit.log in the meta S3 bucket.
// S3 does not support append, so each write is read-modify-write. This is acceptable
// for an operator tool that runs at most a few times per day.
type MetaBucketLog struct {
	bucket      *metabucket.Bucket
	clusterName string
}

// NewMetaBucketLog returns an AuditLog backed by the meta S3 bucket.
func NewMetaBucketLog(b *metabucket.Bucket, clusterName string) *MetaBucketLog {
	return &MetaBucketLog{bucket: b, clusterName: clusterName}
}

func (m *MetaBucketLog) Log(ctx context.Context, cluster, action string, params map[string]string, result string) error {
	entry := Entry{
		Time:    time.Now().UTC(),
		Cluster: cluster,
		Action:  action,
		Params:  params,
		Result:  result,
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("audit marshal: %w", err)
	}

	key := AuditLogKey(m.clusterName)
	existing, err := m.bucket.Get(ctx, key)
	if err != nil {
		var nf *metabucket.ErrNotFound
		if !errors.As(err, &nf) {
			return fmt.Errorf("audit read: %w", err)
		}
		existing = nil
	}

	var buf bytes.Buffer
	if len(existing) > 0 {
		buf.Write(existing)
		if existing[len(existing)-1] != '\n' {
			buf.WriteByte('\n')
		}
	}
	buf.Write(line)
	buf.WriteByte('\n')

	if err := m.bucket.Put(ctx, key, buf.Bytes()); err != nil {
		return fmt.Errorf("audit write: %w", err)
	}
	return nil
}

// ReadEntries fetches and parses the audit log for clusterName from the meta bucket.
// Entries are returned newest-first. At most maxEntries are returned (0 = all).
func ReadEntries(ctx context.Context, b *metabucket.Bucket, clusterName string, maxEntries int) ([]Entry, error) {
	data, err := b.Get(ctx, AuditLogKey(clusterName))
	if err != nil {
		var nf *metabucket.ErrNotFound
		if errors.As(err, &nf) {
			return nil, nil
		}
		return nil, fmt.Errorf("audit read: %w", err)
	}

	return parseAuditEntries(data, maxEntries), nil
}

// MetaContainerLog appends JSON lines to clusters/<name>/audit.log in the Azure
// meta storage container. Same read-modify-write pattern as MetaBucketLog.
type MetaContainerLog struct {
	container   *metacontainer.Container
	clusterName string
}

// NewMetaContainerLog returns an AuditLog backed by the Azure meta container.
func NewMetaContainerLog(c *metacontainer.Container, clusterName string) *MetaContainerLog {
	return &MetaContainerLog{container: c, clusterName: clusterName}
}

func (m *MetaContainerLog) Log(ctx context.Context, cluster, action string, params map[string]string, result string) error {
	entry := Entry{
		Time:    time.Now().UTC(),
		Cluster: cluster,
		Action:  action,
		Params:  params,
		Result:  result,
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("audit marshal: %w", err)
	}

	key := AuditLogKey(m.clusterName)
	existing, err := m.container.Get(ctx, key)
	if err != nil {
		var nf *metacontainer.ErrNotFound
		if !errors.As(err, &nf) {
			return fmt.Errorf("audit read: %w", err)
		}
		existing = nil
	}

	var buf bytes.Buffer
	if len(existing) > 0 {
		buf.Write(existing)
		if existing[len(existing)-1] != '\n' {
			buf.WriteByte('\n')
		}
	}
	buf.Write(line)
	buf.WriteByte('\n')

	if err := m.container.Put(ctx, key, buf.Bytes()); err != nil {
		return fmt.Errorf("audit write: %w", err)
	}
	return nil
}

// ReadEntriesFromContainer fetches and parses the audit log from Azure blob storage.
// Entries are returned newest-first. At most maxEntries are returned (0 = all).
func ReadEntriesFromContainer(ctx context.Context, c *metacontainer.Container, clusterName string, maxEntries int) ([]Entry, error) {
	data, err := c.Get(ctx, AuditLogKey(clusterName))
	if err != nil {
		var nf *metacontainer.ErrNotFound
		if errors.As(err, &nf) {
			return nil, nil
		}
		return nil, fmt.Errorf("audit read: %w", err)
	}
	return parseAuditEntries(data, maxEntries), nil
}

func parseAuditEntries(data []byte, maxEntries int) []Entry {
	var entries []Entry
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	if maxEntries > 0 && len(entries) > maxEntries {
		entries = entries[:maxEntries]
	}
	return entries
}

// FormatAge returns a human-readable age string for an audit entry (e.g. "3h ago").
func FormatAge(e Entry) string {
	diff := time.Since(e.Time)
	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(diff.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(diff.Hours()/24))
	}
}
