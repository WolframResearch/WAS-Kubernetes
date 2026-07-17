package collectors

import (
	"context"
	"encoding/json"
	"fmt"
)

// WorkspaceCollector collects sanitized workspace.json and the last 100 audit log entries.
type WorkspaceCollector struct{}

func (WorkspaceCollector) Name() string { return "workspace" }

func (WorkspaceCollector) Collect(ctx context.Context, cc *CollectContext) ([]File, error) {
	if cc.Workspace == nil {
		return nil, fmt.Errorf("workspace not available")
	}

	var files []File
	meta := cc.Workspace.Meta

	// Sanitize workspace.json (mask account ID).
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		raw = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	sanitized, err := redactWorkspaceJSON(raw)
	if err != nil {
		sanitized = raw
	}
	files = append(files, File{Path: "workspace/workspace.json", Content: sanitized})

	// Audit log: read from meta bucket via AWS CLI.
	region := cc.Cfg.MetaRegion.Value
	if region == "" && cc.Cfg != nil {
		region = cc.Cfg.Region.Value
	}
	bucketName := fmt.Sprintf("wolfram-%s-meta-%s", meta.ClusterName, meta.AWSAccountID)
	s3Key := "clusters/" + meta.ClusterName + "/audit.log"
	s3URI := fmt.Sprintf("s3://%s/%s", bucketName, s3Key)

	auditData, err := runOutput(ctx, "aws", "s3", "cp", s3URI, "-", "--region", region)
	if err != nil {
		auditData = []byte(fmt.Sprintf("audit log unavailable: %s\n", err))
	}
	files = append(files, File{Path: "workspace/audit.log", Content: auditData})

	return files, nil
}
