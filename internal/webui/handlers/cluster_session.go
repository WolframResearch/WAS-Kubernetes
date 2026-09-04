package handlers

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/audit"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metacontainer"
)

// clusterSession caches the expensive bits of opening a cluster for the web UI:
// workspace.json, and (once) az aks get-credentials / aws eks update-kubeconfig.
// Without this, every HTMX tab request re-ran get-credentials (multi-second).
type clusterSession struct {
	Meta           *metabucket.Metadata
	Cloud          string
	AccountID      string
	KubeconfigPath string
	ContextName    string
	tempDir        string
	AuditEntries   []audit.Entry
	metaAt         time.Time
	kubeAt         time.Time
	auditAt        time.Time
}

const (
	sessionMetaTTL  = 2 * time.Minute
	sessionKubeTTL  = 10 * time.Minute
	sessionAuditTTL = 2 * time.Minute
)

var (
	sessionMu      sync.Mutex
	sessions       = map[string]*clusterSession{}
	sessionWaiters = map[string]chan struct{}{}
)

func (s *clusterSession) metaFresh() bool {
	return s != nil && s.Meta != nil && time.Since(s.metaAt) < sessionMetaTTL
}

func (s *clusterSession) kubeFresh() bool {
	if s == nil || s.KubeconfigPath == "" || time.Since(s.kubeAt) >= sessionKubeTTL {
		return false
	}
	info, err := os.Stat(s.KubeconfigPath)
	return err == nil && info.Size() > 0
}

func (s *clusterSession) auditFresh() bool {
	return s != nil && time.Since(s.auditAt) < sessionAuditTTL
}

// getClusterSession returns a cached (or freshly built) session for the cluster.
// needKube triggers az/aws kubeconfig generation once per sessionKubeTTL.
func getClusterSession(ctx context.Context, metaRegion, clusterName, preferCloud string, needKube bool) (*clusterSession, error) {
	for {
		sessionMu.Lock()
		if s := sessions[clusterName]; s != nil {
			if s.metaFresh() && (!needKube || s.kubeFresh()) {
				// Refresh audit in the background path below if stale.
				sessionMu.Unlock()
				if needKube && !s.auditFresh() {
					refreshSessionAudit(ctx, metaRegion, s)
				}
				return s, nil
			}
		}
		if wait, ok := sessionWaiters[clusterName]; ok {
			sessionMu.Unlock()
			select {
			case <-wait:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		}
		wait := make(chan struct{})
		sessionWaiters[clusterName] = wait
		sessionMu.Unlock()

		s, err := buildClusterSession(ctx, metaRegion, clusterName, preferCloud, needKube)

		sessionMu.Lock()
		delete(sessionWaiters, clusterName)
		if err == nil {
			// Merge with existing so a meta-only session can gain kube later.
			if prev := sessions[clusterName]; prev != nil && prev.tempDir != "" && (s.tempDir == "" || s.tempDir != prev.tempDir) {
				if s.tempDir == "" {
					s.tempDir = prev.tempDir
					s.KubeconfigPath = prev.KubeconfigPath
					s.ContextName = prev.ContextName
					s.kubeAt = prev.kubeAt
				} else if prev.tempDir != s.tempDir {
					_ = os.RemoveAll(prev.tempDir)
				}
			}
			sessions[clusterName] = s
		}
		close(wait)
		sessionMu.Unlock()
		return s, err
	}
}

func buildClusterSession(ctx context.Context, metaRegion, clusterName, preferCloud string, needKube bool) (*clusterSession, error) {
	sessionMu.Lock()
	prev := sessions[clusterName]
	sessionMu.Unlock()

	// Reuse meta if still fresh; only open workspace when needed.
	if prev != nil && prev.metaFresh() && (!needKube || prev.kubeFresh()) {
		s := cloneSession(prev)
		if needKube && !s.auditFresh() {
			loadSessionAudit(ctx, metaRegion, s)
		}
		return s, nil
	}

	ws, cloud, accountID, err := openWorkspacePreferring(ctx, metaRegion, clusterName, preferCloud)
	if err != nil {
		return nil, err
	}

	s := &clusterSession{
		Meta:      ws.Meta,
		Cloud:     cloud,
		AccountID: accountID,
		metaAt:    time.Now(),
	}

	if prev != nil && prev.kubeFresh() {
		s.tempDir = prev.tempDir
		s.KubeconfigPath = prev.KubeconfigPath
		s.ContextName = prev.ContextName
		s.kubeAt = prev.kubeAt
		s.AuditEntries = prev.AuditEntries
		s.auditAt = prev.auditAt
		ws.Close()
	} else if needKube {
		if err := ws.MaterializeTempDir(); err != nil {
			ws.Close()
			return nil, err
		}
		kubePath, kerr := ws.Kubeconfig(ctx)
		if kerr != nil {
			ws.Close()
			return nil, kerr
		}
		s.KubeconfigPath = kubePath
		s.ContextName = ws.ContextName()
		s.tempDir = ws.DetachTempDir()
		s.kubeAt = time.Now()
		ws.Close()
		loadSessionAudit(ctx, metaRegion, s)
	} else {
		ws.Close()
	}

	return s, nil
}

func cloneSession(s *clusterSession) *clusterSession {
	cp := *s
	return &cp
}

func loadSessionAudit(ctx context.Context, metaRegion string, s *clusterSession) {
	if s.AccountID == "" {
		s.auditAt = time.Now()
		return
	}
	var entries []audit.Entry
	if s.Cloud == "azure" {
		if c, err := metacontainer.Open(ctx, s.AccountID, s.Meta.ClusterName); err == nil {
			entries, _ = audit.ReadEntriesFromContainer(ctx, c, s.Meta.ClusterName, 50)
		}
	} else {
		if b, err := metabucket.Open(ctx, metaRegion, s.AccountID, s.Meta.ClusterName); err == nil {
			entries, _ = audit.ReadEntries(ctx, b, s.Meta.ClusterName, 50)
		}
	}
	s.AuditEntries = entries
	s.auditAt = time.Now()
}

func refreshSessionAudit(ctx context.Context, metaRegion string, s *clusterSession) {
	sessionMu.Lock()
	if s.auditFresh() {
		sessionMu.Unlock()
		return
	}
	sessionMu.Unlock()
	loadSessionAudit(ctx, metaRegion, s)
	sessionMu.Lock()
	if cur := sessions[s.Meta.ClusterName]; cur != nil {
		cur.AuditEntries = s.AuditEntries
		cur.auditAt = s.auditAt
	}
	sessionMu.Unlock()
}

// invalidateClusterSession drops cached kubeconfig/meta for a cluster
// (call after destroy starts, etc.).
func invalidateClusterSession(clusterName string) {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if s := sessions[clusterName]; s != nil {
		if s.tempDir != "" {
			_ = os.RemoveAll(s.tempDir)
		}
		delete(sessions, clusterName)
	}
}
