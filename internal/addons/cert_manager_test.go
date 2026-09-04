package addons

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
)

func TestCertManagerInstallAppliesClusterIssuer(t *testing.T) {
	m := runner.NewMock()
	m.Register("helm repo", nil)
	m.Register("kubectl apply", nil) // namespaces + ClusterIssuer
	// Orphan cleanup: resources absent (NotFound text must be in output bytes)
	notFound := []byte("Error from server (NotFound): not found")
	m.RegisterOutput("kubectl get ClusterRole", notFound, errors.New("exit status 1"))
	m.RegisterOutput("kubectl get ClusterRoleBinding", notFound, errors.New("exit status 1"))
	m.RegisterOutput("kubectl get ValidatingWebhookConfiguration", notFound, errors.New("exit status 1"))
	m.RegisterOutput("kubectl get MutatingWebhookConfiguration", notFound, errors.New("exit status 1"))
	m.RegisterOutputSequence("helm status cert-manager", []runner.OutputResponse{
		{Err: errors.New("release: not found")},
		{Data: []byte(`{"info":{"status":"deployed"}}`)},
	})
	m.Register("helm upgrade", nil)
	// Verify waits for Ready=True on the ClusterIssuer (prefix must beat any broader get)
	m.RegisterOutput("kubectl get clusterissuer", []byte("True"), nil)

	rc := testRC(m)
	rc.Cfg = &config.Config{
		ClusterName: config.Field[string]{Value: "was-prod"},
		IngressHost: config.Field[string]{Value: "was.example.com"},
	}

	cm := NewCertManager()
	if err := cm.Install(context.Background(), rc); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := cm.Verify(context.Background(), rc); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	foundIssuerApply := false
	for _, c := range m.Calls {
		joined := strings.Join(c, " ")
		if strings.Contains(joined, "kubectl apply -f") {
			foundIssuerApply = true
		}
	}
	if !foundIssuerApply {
		t.Fatalf("expected kubectl apply for ClusterIssuer; calls=%v", m.Calls)
	}
}

func TestCertManagerCheckRequiresClusterIssuer(t *testing.T) {
	m := runner.NewMock()
	// Helm healthy…
	m.RegisterOutput("helm status cert-manager", []byte(`{"info":{"status":"deployed"}}`), nil)
	// …but ClusterIssuer missing
	m.RegisterOutput("kubectl get clusterissuer "+ClusterIssuerName, nil, errors.New("not found"))

	state, err := NewCertManager().Check(context.Background(), testRC(m))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if state != StateNotInstalled {
		t.Fatalf("state=%d want StateNotInstalled when issuer missing", state)
	}
}

func TestClusterIssuerManifest(t *testing.T) {
	manifest := clusterIssuerManifest()
	for _, want := range []string{
		"kind: ClusterIssuer",
		"name: " + ClusterIssuerName,
		"ingressClassName: nginx",
		"acme-v02.api.letsencrypt.org",
	} {
		if !strings.Contains(manifest, want) {
			t.Errorf("manifest missing %q", want)
		}
	}
	if strings.Contains(manifest, "email:") {
		t.Errorf("ACME email must be omitted; got:\n%s", manifest)
	}
}
