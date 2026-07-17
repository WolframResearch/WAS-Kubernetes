package sse

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/stages"
)

// Conductor implements report.Conductor and fans events into an InstallRun.
// The install goroutine calls its methods; the SSE handler reads from the run.
type Conductor struct {
	run       *InstallRun
	stageInfo []stageInfo

	stageIdx   int
	curStage   string
	curSubstep string
	startedAt  time.Time
	stageStart time.Time
	tickStop   chan struct{}
}

type stageInfo struct {
	Num   int
	Name  string
	Label string
	ETA   string
}

// NewConductor builds a Conductor pre-loaded with metadata from stages.All()
// in install order.
func NewConductor(run *InstallRun) *Conductor {
	return NewConductorForStages(run, stages.All())
}

// NewConductorForStages builds a Conductor for a subset of stages (still in
// install order). Used by the cluster Stages tab reinstall flow.
func NewConductorForStages(run *InstallRun, list []stages.Stage) *Conductor {
	info := make([]stageInfo, len(list))
	for i, s := range list {
		info[i] = stageInfo{
			Num:   i + 1,
			Name:  s.Name(),
			Label: s.Label(),
			ETA:   s.EstimateText(),
		}
	}
	c := &Conductor{
		run:       run,
		stageInfo: info,
		startedAt: time.Now(),
		tickStop:  make(chan struct{}),
	}
	go c.tickLoop()
	return c
}

// NewDestroyingConductor builds a Conductor pre-loaded with stages in destroy
// order (reverse of install). Stage numbers match the pre-rendered destroy
// stage list; labels use each stage's DestroyLabel so the UI reads correctly.
func NewDestroyingConductor(run *InstallRun) *Conductor {
	all := stages.All()
	info := make([]stageInfo, len(all))
	for i := range all {
		s := all[len(all)-1-i]
		info[i] = stageInfo{
			Num:   i + 1,
			Name:  s.Name(),
			Label: s.DestroyLabel(),
			ETA:   s.EstimateText(),
		}
	}
	c := &Conductor{
		run:       run,
		stageInfo: info,
		startedAt: time.Now(),
		tickStop:  make(chan struct{}),
	}
	go c.tickLoop()
	return c
}

// ── Stage-level (report.Conductor) ───────────────────────────────────────────

func (c *Conductor) StageStart(label string) {
	c.stageStart = time.Now()
	// Find stage info by label.
	si := stageInfo{Num: c.stageIdx + 1, Label: label, ETA: ""}
	for _, s := range c.stageInfo {
		if s.Label == label {
			si = s
			break
		}
	}
	c.stageIdx++
	c.curStage = si.Name
	if c.curStage == "" {
		c.curStage = label
	}
	c.publish("stage-start", map[string]any{
		"num":   si.Num,
		"name":  si.Name,
		"label": si.Label,
		"eta":   si.ETA,
	})
}

func (c *Conductor) StageDone() {
	c.publish("stage-done", map[string]any{
		"name":    c.curStage,
		"elapsed": elapsed(c.stageStart),
	})
}

func (c *Conductor) StageFail(err error) {
	c.publish("stage-fail", map[string]any{
		"name":    c.curStage,
		"error":   err.Error(),
		"elapsed": elapsed(c.stageStart),
	})
}

func (c *Conductor) InstallComplete(err error) {
	close(c.tickStop)
	payload := map[string]any{
		"totalElapsed": elapsed(c.startedAt),
	}
	if err != nil {
		payload["error"] = err.Error()
	} else {
		payload["clusterUrl"] = "/clusters/" + c.run.ClusterName
	}
	c.publish("install-complete", payload)
	c.run.MarkDone(err)
}

// ── Substep-level (runner.Reporter) ──────────────────────────────────────────

func (c *Conductor) SubstepStart(name string) {
	c.curSubstep = name
	c.publish("substep-start", map[string]any{
		"stage": c.curStage,
		"name":  name,
	})
}

func (c *Conductor) SubstepDone() {
	c.publish("substep-done", map[string]any{
		"stage": c.curStage,
		"name":  c.curSubstep,
	})
}

func (c *Conductor) SubstepFail(err error) {
	c.publish("substep-fail", map[string]any{
		"stage": c.curStage,
		"name":  c.curSubstep,
		"error": err.Error(),
	})
}

func (c *Conductor) LogLine(line string) {
	c.publish("log", map[string]any{"line": line})
}

// ── Internal ──────────────────────────────────────────────────────────────────

func (c *Conductor) publish(evType string, payload map[string]any) {
	data, _ := json.Marshal(payload)
	c.run.Publish(Event{Type: evType, Data: string(data)})
}

func (c *Conductor) tickLoop() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-c.tickStop:
			return
		case <-t.C:
			data, _ := json.Marshal(map[string]any{
				"totalElapsed": elapsed(c.startedAt),
			})
			c.run.Publish(Event{Type: "tick", Data: string(data)})
		}
	}
}

func elapsed(since time.Time) string {
	d := time.Since(since).Round(time.Second)
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%d:%02d", m, s)
}
