package addons

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
)

// orphanJSON builds the minimal kubectl-get JSON payload with the given
// annotations and labels. Nil maps are accepted and produce empty objects.
func orphanJSON(annotations, labels map[string]string) []byte {
	type meta struct {
		Annotations map[string]string `json:"annotations"`
		Labels      map[string]string `json:"labels"`
	}
	if annotations == nil {
		annotations = map[string]string{}
	}
	if labels == nil {
		labels = map[string]string{}
	}
	b, _ := json.Marshal(struct {
		Metadata meta `json:"metadata"`
	}{Metadata: meta{Annotations: annotations, Labels: labels}})
	return b
}

// ── Safety: stale self-annotation is treated as orphan ────────────────────────
// Install only calls cleanupOrphanedResources when the release is NOT deployed.
// A resource annotated for this release therefore has a stale annotation from a
// prior failed/rolled-back install and must be deleted, not skipped.

func TestCleanupDeletesResourceWithStaleOwnReleaseAnnotation(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get ClusterRoleBinding strimzi-cluster-operator",
		orphanJSON(
			map[string]string{
				"meta.helm.sh/release-name":      "strimzi-kafka-operator",
				"meta.helm.sh/release-namespace": "strimzi-system",
			}, nil),
		nil,
	)
	m.Register("kubectl delete", nil)

	h := NewStrimzi()
	rc := testRC(m)
	res := OrphanedResource{Kind: "ClusterRoleBinding", Name: "strimzi-cluster-operator"}
	if err := h.cleanupResourceIfOrphaned(context.Background(), rc, res, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.CalledWith("kubectl delete ClusterRoleBinding strimzi-cluster-operator") {
		t.Errorf("stale self-annotation must be treated as orphan and deleted; calls: %v", m.Calls)
	}
}

// ── Safety: owned by a different release ─────────────────────────────────────

func TestCleanupSafetyViolationDifferentRelease(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get ClusterRoleBinding strimzi-cluster-operator",
		orphanJSON(
			map[string]string{
				"meta.helm.sh/release-name":      "some-other-release",
				"meta.helm.sh/release-namespace": "default",
			}, nil),
		nil,
	)

	h := NewStrimzi()
	rc := testRC(m)
	res := OrphanedResource{Kind: "ClusterRoleBinding", Name: "strimzi-cluster-operator"}
	err := h.cleanupResourceIfOrphaned(context.Background(), rc, res, "")
	if err == nil {
		t.Fatal("expected SAFETY VIOLATION error, got nil")
	}
	if !strings.Contains(err.Error(), "SAFETY VIOLATION") {
		t.Errorf("expected 'SAFETY VIOLATION' in error; got: %v", err)
	}
	if m.CalledWith("kubectl delete") {
		t.Error("must not delete a resource owned by a different helm release")
	}
}

// ── Safety: managed by a non-Helm tool ───────────────────────────────────────

func TestCleanupSafetyViolationManagedByKustomize(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get ClusterRole strimzi-cluster-operator-namespaced",
		orphanJSON(nil, map[string]string{
			"app.kubernetes.io/managed-by": "kustomize",
		}),
		nil,
	)

	h := NewStrimzi()
	rc := testRC(m)
	res := OrphanedResource{Kind: "ClusterRole", Name: "strimzi-cluster-operator-namespaced"}
	err := h.cleanupResourceIfOrphaned(context.Background(), rc, res, "")
	if err == nil {
		t.Fatal("expected SAFETY VIOLATION error, got nil")
	}
	if !strings.Contains(err.Error(), "SAFETY VIOLATION") {
		t.Errorf("expected 'SAFETY VIOLATION' in error; got: %v", err)
	}
	if m.CalledWith("kubectl delete") {
		t.Error("must not delete a resource managed by kustomize")
	}
}

// ── Happy path: orphan deleted ────────────────────────────────────────────────

func TestCleanupDeletesOrphanedResource(t *testing.T) {
	m := runner.NewMock()
	// Resource exists with no annotations or managed-by label.
	m.RegisterOutput("kubectl get RoleBinding strimzi-cluster-operator",
		orphanJSON(nil, nil),
		nil,
	)
	m.Register("kubectl delete", nil)

	h := NewStrimzi()
	rc := testRC(m)
	res := OrphanedResource{Kind: "RoleBinding", Name: "strimzi-cluster-operator", Namespace: "was"}
	if err := h.cleanupResourceIfOrphaned(context.Background(), rc, res, "was"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.CalledWith("kubectl delete RoleBinding strimzi-cluster-operator") {
		t.Errorf("expected kubectl delete; calls: %v", m.Calls)
	}
	if !m.CalledWith("--namespace was") {
		t.Errorf("expected --namespace was in delete call; calls: %v", m.Calls)
	}
}

// ── Resource absent: empty body, exit 0 (some kubectl versions) ──────────────

func TestCleanupHandlesNotFound(t *testing.T) {
	m := runner.NewMock()
	// kubectl exits 0 with no output on some versions (e.g. with --ignore-not-found).
	m.RegisterOutput("kubectl get ClusterRole strimzi-entity-operator",
		[]byte{},
		nil,
	)

	h := NewStrimzi()
	rc := testRC(m)
	res := OrphanedResource{Kind: "ClusterRole", Name: "strimzi-entity-operator"}
	if err := h.cleanupResourceIfOrphaned(context.Background(), rc, res, ""); err != nil {
		t.Fatalf("unexpected error for absent resource: %v", err)
	}
	if m.CalledWith("kubectl delete") {
		t.Error("must not call kubectl delete when resource is absent")
	}
}

// ── Resource absent: "not found" in stderr, exit 1 ───────────────────────────
// Verifies scenario 1 and 4: fresh cluster / wrong namespace → NotFound exit.

func TestCleanupHandlesNotFoundViaExitCode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output string
	}{
		{"NotFound word", `Error from server (NotFound): clusterroles.rbac.authorization.k8s.io "x" not found`},
		{"not found phrase", `error: the server doesn't have a resource type "x" not found`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := runner.NewMock()
			m.RegisterOutput("kubectl get ClusterRole strimzi-entity-operator",
				[]byte(tc.output),
				errors.New("exit status 1"),
			)

			h := NewStrimzi()
			rc := testRC(m)
			res := OrphanedResource{Kind: "ClusterRole", Name: "strimzi-entity-operator"}
			if err := h.cleanupResourceIfOrphaned(context.Background(), rc, res, ""); err != nil {
				t.Fatalf("NotFound exit should be treated as absent; got error: %v", err)
			}
			if m.CalledWith("kubectl delete") {
				t.Error("must not call kubectl delete when resource is absent")
			}
		})
	}
}

// ── Real error (non-NotFound) is propagated ───────────────────────────────────

func TestCleanupPropagatesRealGetError(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get ClusterRole strimzi-entity-operator",
		[]byte("Error from server (Forbidden): clusterroles is forbidden"),
		errors.New("exit status 1"),
	)

	h := NewStrimzi()
	rc := testRC(m)
	res := OrphanedResource{Kind: "ClusterRole", Name: "strimzi-entity-operator"}
	err := h.cleanupResourceIfOrphaned(context.Background(), rc, res, "")
	if err == nil {
		t.Fatal("expected error for Forbidden response, got nil")
	}
	if strings.Contains(err.Error(), "SAFETY") {
		t.Errorf("error should not be a safety violation; got: %v", err)
	}
}

// ── Namespace flag: namespaced resource in a specific namespace (scenario 2) ─
// kubectl get and kubectl delete must both carry --namespace.

func TestCleanupNamespacedResourceGetAndDeleteHaveNamespaceFlag(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get RoleBinding strimzi-cluster-operator",
		orphanJSON(nil, nil),
		nil,
	)
	m.Register("kubectl delete", nil)

	h := NewStrimzi()
	rc := testRC(m)
	res := OrphanedResource{Kind: "RoleBinding", Name: "strimzi-cluster-operator", Namespace: "strimzi-system"}
	if err := h.cleanupResourceIfOrphaned(context.Background(), rc, res, "strimzi-system"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.CalledWith("--namespace strimzi-system") {
		t.Errorf("get and delete must carry --namespace strimzi-system; calls: %v", m.Calls)
	}
}

// ── Namespace flag: cluster-scoped resource must never carry --namespace (scenario 3) ─

func TestCleanupClusterScopedResourceHasNoNamespaceFlag(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get ClusterRoleBinding strimzi-cluster-operator",
		orphanJSON(nil, nil),
		nil,
	)
	m.Register("kubectl delete", nil)

	h := NewStrimzi()
	rc := testRC(m)
	// Even if a non-empty namespace is passed by the caller, it must be ignored
	// for cluster-scoped kinds.
	res := OrphanedResource{Kind: "ClusterRoleBinding", Name: "strimzi-cluster-operator"}
	if err := h.cleanupResourceIfOrphaned(context.Background(), rc, res, "strimzi-system"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.CalledWith("--namespace") {
		t.Errorf("cluster-scoped resource must not carry --namespace; calls: %v", m.Calls)
	}
}

// ── isClusterScoped covers expected kinds ─────────────────────────────────────

func TestIsClusterScoped(t *testing.T) {
	clusterScoped := []string{
		"ClusterRole", "ClusterRoleBinding", "CSIDriver", "IngressClass",
		"CustomResourceDefinition", "StorageClass", "Namespace", "PersistentVolume",
	}
	for _, k := range clusterScoped {
		if !isClusterScoped(k) {
			t.Errorf("isClusterScoped(%q) = false, want true", k)
		}
	}
	namespaced := []string{"RoleBinding", "Role", "ServiceAccount", "Deployment", "ConfigMap", "Secret"}
	for _, k := range namespaced {
		if isClusterScoped(k) {
			t.Errorf("isClusterScoped(%q) = true, want false", k)
		}
	}
}

// ── @watched expands to all three namespaces ─────────────────────────────────

func TestCleanupChecksAllWatchedNamespaces(t *testing.T) {
	m := runner.NewMock()
	// Return empty for every kubectl get (all resources absent).
	m.RegisterOutput("kubectl get", []byte{}, nil)

	h := &HelmComponent{
		ReleaseName: "strimzi-kafka-operator",
		Namespace:   "strimzi-system",
		WatchedNS:   []string{"was", "kafka"},
		OrphanedResourceCheck: []OrphanedResource{
			{Kind: "RoleBinding", Name: "strimzi-cluster-operator", Namespace: "@watched"},
		},
	}

	rc := testRC(m)
	if err := h.cleanupOrphanedResources(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect exactly 3 kubectl get calls: strimzi-system, was, kafka.
	getCount := 0
	for _, c := range m.Calls {
		if strings.Contains(strings.Join(c, " "), "kubectl get RoleBinding strimzi-cluster-operator") {
			getCount++
		}
	}
	if getCount != 3 {
		t.Errorf("expected 3 kubectl get calls (strimzi-system, was, kafka), got %d; calls: %v",
			getCount, m.Calls)
	}
}
