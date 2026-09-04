package stages

import (
	"context"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/report"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
)

// RunOrchestrated runs stagesToRun in order, signalling stage lifecycle
// transitions through cond. This is the canonical orchestration path shared
// by the CLI and the web UI. The only thing that differs between callers is
// the Conductor implementation.
func RunOrchestrated(ctx context.Context, cfg *config.Config, stagesToRun []Stage, cond report.Conductor) error {
	r := runner.ExecRunner{}
	for _, s := range stagesToRun {
		cond.StageStart(s.Label())
		if err := s.Apply(ctx, cfg, r, cond); err != nil {
			cond.StageFail(err)
			cond.InstallComplete(err)
			return err
		}
		cond.StageDone()
	}
	cond.InstallComplete(nil)
	return nil
}

// RunDestroyOrchestrated runs stagesToRun in reverse order, calling Destroy()
// on each stage. It uses the same conductor protocol as RunOrchestrated so the
// web UI SSE stream works identically for both install and destroy.
func RunDestroyOrchestrated(ctx context.Context, cfg *config.Config, stagesToRun []Stage, cond report.Conductor) error {
	r := runner.ExecRunner{}
	for i := len(stagesToRun) - 1; i >= 0; i-- {
		s := stagesToRun[i]
		cond.StageStart(s.DestroyLabel())
		if err := s.Destroy(ctx, cfg, r, cond); err != nil {
			cond.StageFail(err)
			cond.InstallComplete(err)
			return err
		}
		cond.StageDone()
	}
	cond.InstallComplete(nil)
	return nil
}
