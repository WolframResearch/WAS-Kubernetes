// Package addons installs and manages cluster-side prerequisites idempotently.
// Each add-on implements the Addon interface. The Installer walks an ordered
// slice of addons, calling Check → Install → Verify for each.
package addons

import (
	"context"
	"fmt"
	"strings"
)

// State describes the current install state of an Addon.
type State int

const (
	StateNotInstalled State = iota
	StateHealthy
	StateFailed
)

// Addon is the unit of cluster-side infrastructure managed by the addons stage.
type Addon interface {
	// Name is the human/machine label used in progress output and skip lists.
	Name() string

	// Namespaces returns every namespace that must exist before Install is
	// called — the release namespace AND any RBAC/watch targets.
	Namespaces() []string

	// Check returns the current install state. StateNotInstalled is returned
	// (not an error) when the addon has never been installed.
	Check(ctx context.Context, rc *RunContext) (State, error)

	// Install is idempotent:
	//   StateNotInstalled → install
	//   StateHealthy      → skip (no-op)
	//   StateFailed       → uninstall orphans, then reinstall
	Install(ctx context.Context, rc *RunContext) error

	// Verify confirms the addon is functional after Install returns. A nil
	// error means the addon is ready. Called only after a successful Install.
	Verify(ctx context.Context, rc *RunContext) error

	// Uninstall removes the addon if it is installed; no-op if already absent.
	// Errors are non-fatal in the destroy sequence — the caller logs the error
	// and continues so subsequent addons and the orphan sweep can still run.
	Uninstall(ctx context.Context, rc *RunContext) error

	// Clouds returns which cloud providers this addon applies to.
	// Values: "aws", "azure".
	Clouds() []string
}

// Installer walks an ordered list of addons, skipping those in the skip list
// and reporting per-addon progress through the RunContext reporter.
type Installer struct {
	rc *RunContext
}

// NewInstaller returns an Installer bound to rc.
func NewInstaller(rc *RunContext) *Installer {
	return &Installer{rc: rc}
}

// InstallAll iterates addons in order. On any addon failure it stops and
// returns a wrapped error naming the addon.
func (i *Installer) InstallAll(ctx context.Context, list []Addon) error {
	skip := parseSkipList(i.rc.Cfg.AddonsSkip.Value)

	for _, a := range list {
		if skip[a.Name()] {
			i.rc.Reporter.LogLine(fmt.Sprintf("[addons] %s: skipped", a.Name()))
			continue
		}

		i.rc.Reporter.SubstepStart(a.Name())

		if err := a.Install(ctx, i.rc); err != nil {
			i.rc.Reporter.SubstepFail(err)
			return fmt.Errorf("addon %s: %w", a.Name(), err)
		}
		if err := a.Verify(ctx, i.rc); err != nil {
			i.rc.Reporter.SubstepFail(err)
			return fmt.Errorf("addon %s verify: %w", a.Name(), err)
		}

		i.rc.Reporter.SubstepDone()
	}
	return nil
}

// UninstallAll uninstalls addons in REVERSE install order. Errors are logged
// but not fatal — the orphan sweep handles stragglers.
func (i *Installer) UninstallAll(ctx context.Context, list []Addon) {
	skip := parseSkipList(i.rc.Cfg.AddonsSkip.Value)
	for j := len(list) - 1; j >= 0; j-- {
		a := list[j]
		if skip[a.Name()] {
			i.rc.Reporter.LogLine(fmt.Sprintf("[addons] %s: skipped", a.Name()))
			continue
		}
		i.rc.Reporter.SubstepStart(a.Name())
		if err := a.Uninstall(ctx, i.rc); err != nil {
			i.rc.Reporter.LogLine(fmt.Sprintf("[addons] %s: uninstall warning: %v", a.Name(), err))
		}
		i.rc.Reporter.SubstepDone()
	}
}

func parseSkipList(csv string) map[string]bool {
	out := make(map[string]bool)
	for _, s := range strings.Split(csv, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			out[s] = true
		}
	}
	return out
}
