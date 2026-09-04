// Package sse provides the server-sent event infrastructure for live install
// progress. A Broker manages active InstallRuns; each run holds a ring buffer
// of past events so late-connecting browsers can catch up.
package sse

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const ringSize = 500

// Event is a single SSE event sent from server to browser.
type Event struct {
	Type string // e.g. "stage-start", "log", "install-complete"
	Data string // JSON payload
}

// InstallRun tracks one in-flight (or recently completed) install session.
type InstallRun struct {
	SessionID   string
	ClusterName string
	Cloud       string // "aws" or "azure"
	StartedAt   time.Time
	Form        any // holds installFormData (handlers package) to allow pre-filled retries

	// StageNames, when non-empty, limits the stream UI and conductor to these
	// stages (install order). Empty means the full install pipeline.
	StageNames []string
	// Heading overrides the stream page title verb (default: "Installing").
	Heading string

	mu      sync.Mutex
	ring    []Event     // ring buffer of published events
	subs    []chan Event // live subscriber channels
	done    bool
	doneErr error
}

// Publish appends ev to the ring buffer and fans it out to all live subscribers.
func (r *InstallRun) Publish(ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.ring) >= ringSize {
		r.ring = r.ring[1:]
	}
	r.ring = append(r.ring, ev)
	for _, ch := range r.subs {
		select {
		case ch <- ev:
		default:
			// Slow consumer — skip rather than block the install goroutine.
		}
	}
}

// Subscribe returns a channel that receives all events from now on. The caller
// must pass the returned snapshot as the initial replay, then read from ch.
func (r *InstallRun) Subscribe(ctx context.Context) (snapshot []Event, ch chan Event) {
	r.mu.Lock()
	snap := make([]Event, len(r.ring))
	copy(snap, r.ring)
	isDone := r.done
	r.mu.Unlock()

	if isDone {
		return snap, nil
	}

	ch = make(chan Event, 64)
	r.mu.Lock()
	r.subs = append(r.subs, ch)
	r.mu.Unlock()
	return snap, ch
}

// Unsubscribe removes ch from live subscribers.
func (r *InstallRun) Unsubscribe(ch chan Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, s := range r.subs {
		if s == ch {
			r.subs = append(r.subs[:i], r.subs[i+1:]...)
			close(ch)
			return
		}
	}
}

// MarkDone seals the run and closes all subscriber channels.
func (r *InstallRun) MarkDone(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done = true
	r.doneErr = err
	for _, ch := range r.subs {
		close(ch)
	}
	r.subs = nil
}

// Done returns whether the install has finished and, if so, any error.
func (r *InstallRun) Done() (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done, r.doneErr
}

// Broker manages all active InstallRuns.
type Broker struct {
	mu   sync.Mutex
	runs map[string]*InstallRun
}

// NewBroker returns an initialised Broker.
func NewBroker() *Broker {
	b := &Broker{runs: make(map[string]*InstallRun)}
	go b.gcLoop()
	return b
}

// NewRun creates a new InstallRun for clusterName and returns it.
func (b *Broker) NewRun(clusterName, cloud string) *InstallRun {
	id := newSessionID()
	r := &InstallRun{
		SessionID:   id,
		ClusterName: clusterName,
		Cloud:       cloud,
		StartedAt:   time.Now(),
	}
	b.mu.Lock()
	b.runs[id] = r
	b.mu.Unlock()
	return r
}

// Get returns the run for sessionID, if it exists.
func (b *Broker) Get(sessionID string) (*InstallRun, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.runs[sessionID]
	return r, ok
}

// gcLoop removes completed runs older than 1 hour.
func (b *Broker) gcLoop() {
	t := time.NewTicker(15 * time.Minute)
	defer t.Stop()
	for range t.C {
		b.mu.Lock()
		for id, r := range b.runs {
			done, _ := r.Done()
			if done && time.Since(r.StartedAt) > time.Hour {
				delete(b.runs, id)
			}
		}
		b.mu.Unlock()
	}
}

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
