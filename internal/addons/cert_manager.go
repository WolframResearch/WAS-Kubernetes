package addons

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ClusterIssuerName is the Let's Encrypt HTTP-01 ClusterIssuer wasctl creates
// with the cert-manager addon. The app stage sets ingress.tls.clusterIssuer to
// this name so the was-certificate Certificate references it.
const ClusterIssuerName = "letsencrypt-cluster-issuer"

// NewCertManager returns the cert-manager add-on for AWS and Azure.
// After the Helm release is healthy it applies a Let's Encrypt ClusterIssuer
// (HTTP-01 via ingress-nginx) so operators do not create one by hand.
func NewCertManager() *certManager {
	return &certManager{}
}

type certManager struct{}

func (c *certManager) Name() string         { return "cert-manager" }
func (c *certManager) Clouds() []string     { return []string{"aws", "azure"} }
func (c *certManager) Namespaces() []string { return []string{"cert-manager"} }

func (c *certManager) Check(ctx context.Context, rc *RunContext) (State, error) {
	state, err := c.component().Check(ctx, rc)
	if err != nil || state != StateHealthy {
		return state, err
	}
	if !clusterIssuerPresent(ctx, rc) {
		// Helm is up but issuer missing — treat as incomplete so Install re-runs.
		return StateNotInstalled, nil
	}
	return StateHealthy, nil
}

func (c *certManager) Install(ctx context.Context, rc *RunContext) error {
	if err := c.component().Install(ctx, rc); err != nil {
		return err
	}
	return ensureClusterIssuer(ctx, rc)
}

func (c *certManager) Verify(ctx context.Context, rc *RunContext) error {
	if err := c.component().Verify(ctx, rc); err != nil {
		return err
	}
	return waitClusterIssuerReady(ctx, rc)
}

func (c *certManager) Uninstall(ctx context.Context, rc *RunContext) error {
	if err := deleteClusterIssuer(ctx, rc); err != nil {
		rc.Reporter.LogLine(fmt.Sprintf("[addons] cert-manager: ClusterIssuer cleanup warning: %v", err))
	}
	return c.component().Uninstall(ctx, rc)
}

func (c *certManager) component() *HelmComponent {
	return &HelmComponent{
		ReleaseName: "cert-manager",
		ChartRef:    "jetstack/cert-manager",
		Version:     "~1.15.0",
		Namespace:   "cert-manager",
		RepoName:    "jetstack",
		RepoURL:     "https://charts.jetstack.io",
		Values: map[string]string{
			"crds.enabled": "true",
		},
		clouds: []string{"aws", "azure"},
		// Derived from: helm template jetstack/cert-manager --version ~1.15.0
		// CRDs are intentionally excluded — they persist across reinstalls by
		// design and do not cause AlreadyExists install conflicts.
		OrphanedResourceCheck: []OrphanedResource{
			{Kind: "ClusterRole", Name: "cert-manager-controller-issuers"},
			{Kind: "ClusterRole", Name: "cert-manager-controller-clusterissuers"},
			{Kind: "ClusterRole", Name: "cert-manager-controller-certificates"},
			{Kind: "ClusterRole", Name: "cert-manager-controller-orders"},
			{Kind: "ClusterRole", Name: "cert-manager-controller-challenges"},
			{Kind: "ClusterRole", Name: "cert-manager-controller-ingress-shim"},
			{Kind: "ClusterRole", Name: "cert-manager-view"},
			{Kind: "ClusterRole", Name: "cert-manager-edit"},
			{Kind: "ClusterRole", Name: "cert-manager-controller-approve:cert-manager-io"},
			{Kind: "ClusterRole", Name: "cert-manager-controller-certificatesigningrequests"},
			{Kind: "ClusterRole", Name: "cert-manager-webhook:subjectaccessreviews"},
			{Kind: "ClusterRoleBinding", Name: "cert-manager-controller-issuers"},
			{Kind: "ClusterRoleBinding", Name: "cert-manager-controller-clusterissuers"},
			{Kind: "ClusterRoleBinding", Name: "cert-manager-controller-certificates"},
			{Kind: "ClusterRoleBinding", Name: "cert-manager-controller-orders"},
			{Kind: "ClusterRoleBinding", Name: "cert-manager-controller-challenges"},
			{Kind: "ClusterRoleBinding", Name: "cert-manager-controller-ingress-shim"},
			{Kind: "ClusterRoleBinding", Name: "cert-manager-controller-approve:cert-manager-io"},
			{Kind: "ClusterRoleBinding", Name: "cert-manager-controller-certificatesigningrequests"},
			{Kind: "ClusterRoleBinding", Name: "cert-manager-webhook:auth-delegator"},
			{Kind: "ClusterRoleBinding", Name: "cert-manager-webhook:subjectaccessreviews"},
			{Kind: "ClusterRoleBinding", Name: "cert-manager-cainjector"},
			{Kind: "ValidatingWebhookConfiguration", Name: "cert-manager-webhook"},
			{Kind: "MutatingWebhookConfiguration", Name: "cert-manager-webhook"},
		},
	}
}

func clusterIssuerManifest() string {
	// ACME email is omitted on purpose: cert-manager and Let's Encrypt treat it
	// as optional. Avoid inventing @wolfram.com (or any) addresses.
	return fmt.Sprintf(`apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: %s
  labels:
    app.kubernetes.io/managed-by: wasctl
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    privateKeySecretRef:
      name: %s-key
    solvers:
    - http01:
        ingress:
          ingressClassName: nginx
`, ClusterIssuerName, ClusterIssuerName)
}

func clusterIssuerPresent(ctx context.Context, rc *RunContext) bool {
	_, err := rc.Runner.Output(ctx, append([]string{
		"kubectl", "get", "clusterissuer", ClusterIssuerName,
	}, rc.kubectlFlags()...), nil)
	return err == nil
}

func ensureClusterIssuer(ctx context.Context, rc *RunContext) error {
	rc.Reporter.LogLine(fmt.Sprintf("[addons] cert-manager: applying ClusterIssuer %s (no ACME email)", ClusterIssuerName))
	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		if err := applyManifest(ctx, rc, clusterIssuerManifest()); err != nil {
			lastErr = err
			rc.Reporter.LogLine(fmt.Sprintf("[addons] cert-manager: ClusterIssuer apply attempt %d/5 failed: %v", attempt, err))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("apply ClusterIssuer %s: %w", ClusterIssuerName, lastErr)
}

func waitClusterIssuerReady(ctx context.Context, rc *RunContext) error {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		out, err := rc.Runner.Output(ctx, append([]string{
			"kubectl", "get", "clusterissuer", ClusterIssuerName,
			"-o", "jsonpath={.status.conditions[?(@.type==\"Ready\")].status}",
		}, rc.kubectlFlags()...), nil)
		if err == nil && strings.EqualFold(strings.TrimSpace(string(out)), "True") {
			rc.Reporter.LogLine(fmt.Sprintf("[addons] cert-manager: ClusterIssuer %s is Ready", ClusterIssuerName))
			return nil
		}
		if time.Now().After(deadline) {
			detail := strings.TrimSpace(string(out))
			if err != nil {
				detail = err.Error()
			}
			return fmt.Errorf("ClusterIssuer %s not Ready within 2m (%s)", ClusterIssuerName, detail)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func deleteClusterIssuer(ctx context.Context, rc *RunContext) error {
	return rc.Runner.Run(ctx, append([]string{
		"kubectl", "delete", "clusterissuer", ClusterIssuerName, "--ignore-not-found",
	}, rc.kubectlFlags()...), nil, rc.Reporter)
}
