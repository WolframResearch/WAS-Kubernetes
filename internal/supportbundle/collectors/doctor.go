package collectors

import (
	"bytes"
	"context"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor/checks"
)

// DoctorCollector runs wasctl doctor and embeds the JSON + markdown reports.
type DoctorCollector struct{}

func (DoctorCollector) Name() string { return "doctor" }

func (DoctorCollector) Collect(ctx context.Context, cc *CollectContext) ([]File, error) {
	rc := doctor.NewRunContext(ctx, cc.Cfg)
	if cc.Kubeconfig != "" {
		rc.Kubeconfig = cc.Kubeconfig
	}
	if cc.ContextName != "" {
		rc.ContextName = cc.ContextName
	}
	if cc.Workspace != nil {
		rc.Workspace = cc.Workspace
	}
	defer rc.Cleanup()

	rep := doctor.Run(ctx, rc, checks.All())

	var jsonBuf bytes.Buffer
	jr := doctor.NewJSONRenderer(&jsonBuf)
	_ = jr.Render(rep)

	var mdBuf bytes.Buffer
	mr := doctor.NewMarkdownRenderer(&mdBuf)
	mr.Render(rep)

	return []File{
		{Path: "doctor/report.json", Content: jsonBuf.Bytes()},
		{Path: "doctor/report.md", Content: mdBuf.Bytes()},
	}, nil
}
