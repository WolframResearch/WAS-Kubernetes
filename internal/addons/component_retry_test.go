package addons

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
)

func TestIsTransientKubectlError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"Unable to connect to the server: dial tcp: lookup wasctl-k8s... read udp ... i/o timeout", true},
		{"Get \"https://...\": dial tcp 1.2.3.4:443: i/o timeout", true},
		{"Error from server (NotFound): clusterroles.rbac.authorization.k8s.io \"x\" not found", false},
		{"clusterroles.rbac.authorization.k8s.io \"x\" not found", false},
		{"forbidden: User cannot get", false},
	}
	for _, tc := range cases {
		got := isTransientKubectlError(errors.New(tc.msg), nil)
		if got != tc.want {
			t.Errorf("isTransientKubectlError(%q)=%v want %v", tc.msg, got, tc.want)
		}
	}
}

func TestCleanupRetriesTransientKubectlGet(t *testing.T) {
	old := orphanRetryBaseDelay
	orphanRetryBaseDelay = time.Millisecond
	defer func() { orphanRetryBaseDelay = old }()

	m := runner.NewMock()
	m.RegisterOutputSequence("kubectl get ClusterRole cert-manager-view", []runner.OutputResponse{
		{Data: []byte("Unable to connect: dial tcp: i/o timeout"), Err: errors.New("exit status 1")},
		{Data: []byte("Unable to connect: dial tcp: i/o timeout"), Err: errors.New("exit status 1")},
		{Data: []byte("Error from server (NotFound): not found"), Err: errors.New("exit status 1")},
	})
	m.Register("helm repo", nil)
	m.Register("kubectl apply", nil)
	m.RegisterOutput("helm status cert-manager", nil, errors.New("release: not found"))
	m.Register("helm upgrade", nil)

	h := &HelmComponent{
		ReleaseName: "cert-manager",
		Namespace:   "cert-manager",
		OrphanedResourceCheck: []OrphanedResource{
			{Kind: "ClusterRole", Name: "cert-manager-view"},
		},
	}

	if err := h.Install(context.Background(), testRC(m)); err != nil {
		t.Fatalf("Install: %v", err)
	}
	gets := 0
	for _, c := range m.Calls {
		if strings.Contains(strings.Join(c, " "), "kubectl get ClusterRole cert-manager-view") {
			gets++
		}
	}
	if gets < 3 {
		t.Fatalf("expected ≥3 kubectl get attempts for transient errors, got %d; calls=%v", gets, m.Calls)
	}
}
