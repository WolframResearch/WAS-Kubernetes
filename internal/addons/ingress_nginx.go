package addons

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
)

// NewIngressNginx returns the ingress-nginx add-on.
//
// Both clouds set externalTrafficPolicy=Local.
// Azure additionally sets service.beta.kubernetes.io/azure-dns-label-name so
// Azure publishes a free *.cloudapp.azure.com FQDN. Ingress host rules require
// a DNS name — a bare public IP is rejected by the Kubernetes API.
func NewIngressNginx() *ingressNginx {
	return &ingressNginx{}
}

type ingressNginx struct{}

func (i *ingressNginx) Name() string         { return "ingress-nginx" }
func (i *ingressNginx) Clouds() []string     { return []string{"aws", "azure"} }
func (i *ingressNginx) Namespaces() []string { return []string{"ingress-nginx"} }

func (i *ingressNginx) Check(ctx context.Context, rc *RunContext) (State, error) {
	return i.component(rc).Check(ctx, rc)
}

func (i *ingressNginx) Verify(ctx context.Context, rc *RunContext) error {
	return i.component(rc).Verify(ctx, rc)
}

func (i *ingressNginx) Uninstall(ctx context.Context, rc *RunContext) error {
	return i.component(rc).Uninstall(ctx, rc)
}

func (i *ingressNginx) Install(ctx context.Context, rc *RunContext) error {
	h := i.component(rc)

	// Azure: if already healthy but missing the DNS-label annotation, force a
	// helm upgrade so the free cloudapp.azure.com FQDN is provisioned.
	if rc.Cfg != nil && rc.Cfg.Cloud == "azure" {
		state, err := h.Check(ctx, rc)
		if err != nil {
			return err
		}
		if state == StateHealthy {
			label := AzureDNSLabel(rc.Cfg.ClusterName.Value)
			has, annErr := azureIngressHasDNSLabel(ctx, rc, label)
			if annErr != nil {
				rc.Reporter.LogLine(fmt.Sprintf("[addons] ingress-nginx: warn checking DNS label: %v", annErr))
			}
			if has {
				rc.Reporter.LogLine("[addons] ingress-nginx: already healthy with Azure DNS label — skipping")
				return nil
			}
			rc.Reporter.LogLine("[addons] ingress-nginx: healthy but missing azure-dns-label-name — upgrading")
			// Fall through: HelmComponent.Install will see Healthy and skip
			// unless we uninstall-or force. Call helm upgrade by temporarily
			// treating as not installed via direct upgrade path.
			return i.forceUpgrade(ctx, rc, h)
		}
	}

	if err := h.Install(ctx, rc); err != nil {
		return err
	}

	if rc.Cfg != nil && rc.Cfg.Cloud == "azure" {
		return ensureAzureIngressFQDN(ctx, rc)
	}
	return nil
}

func (i *ingressNginx) forceUpgrade(ctx context.Context, rc *RunContext, h *HelmComponent) error {
	// Run helm upgrade --install with Azure values even though release is healthy.
	timeout := "5m"
	if h.Timeout != "" {
		timeout = h.Timeout
	}
	if h.RepoURL != "" {
		if err := rc.Runner.Run(ctx, []string{
			"helm", "repo", "add", h.RepoName, h.RepoURL, "--force-update",
		}, nil, rc.Reporter); err != nil {
			return fmt.Errorf("%s: helm repo add: %w", h.ReleaseName, err)
		}
		_ = rc.Runner.Run(ctx, []string{"helm", "repo", "update", h.RepoName}, nil, rc.Reporter)
	}
	args := []string{
		"helm", "upgrade", "--install", h.ReleaseName, h.ChartRef,
		"--namespace", h.Namespace,
		"--create-namespace",
		"--version", h.Version,
		"--wait", "--timeout", timeout,
	}
	if !h.DisableAtomic {
		args = append(args, "--atomic")
	}
	args = append(args, rc.helmFlags()...)
	for k, v := range h.Values {
		args = append(args, "--set", fmt.Sprintf("%s=%s", k, v))
	}
	if err := rc.Runner.Run(ctx, args, nil, rc.Reporter); err != nil {
		return err
	}
	return ensureAzureIngressFQDN(ctx, rc)
}

func (i *ingressNginx) component(rc *RunContext) *HelmComponent {
	values := map[string]string{
		"controller.service.externalTrafficPolicy": "Local",
	}
	if rc.Cfg != nil && rc.Cfg.Cloud == "azure" {
		label := AzureDNSLabel(rc.Cfg.ClusterName.Value)
		// Escaped dots match helm --set annotation key syntax (see chart README (Prerequisites)).
		values[`controller.service.annotations.service\.beta\.kubernetes\.io/azure-dns-label-name`] = label
	}
	return &HelmComponent{
		ReleaseName: "ingress-nginx",
		ChartRef:    "ingress-nginx/ingress-nginx",
		Version:     "~4.11.0",
		Namespace:   "ingress-nginx",
		RepoName:    "ingress-nginx",
		RepoURL:     "https://kubernetes.github.io/ingress-nginx",
		Values:      values,
		clouds:      []string{"aws", "azure"},
		OrphanedResourceCheck: []OrphanedResource{
			{Kind: "ClusterRole", Name: "ingress-nginx"},
			{Kind: "ClusterRoleBinding", Name: "ingress-nginx"},
			{Kind: "IngressClass", Name: "nginx"},
			{Kind: "ValidatingWebhookConfiguration", Name: "ingress-nginx-admission"},
			{Kind: "ServiceAccount", Name: "ingress-nginx", Namespace: "ingress-nginx"},
		},
	}
}

// AzureDNSLabel returns a valid azure-dns-label-name for the cluster.
// Must be lowercase alphanumeric + hyphens, start with a letter, ≤63 chars.
func AzureDNSLabel(clusterName string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(clusterName) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
			b.WriteRune(r)
		}
	}
	label := b.String()
	label = strings.Trim(label, "-")
	if label == "" {
		label = "was"
	}
	if label[0] >= '0' && label[0] <= '9' {
		label = "was-" + label
	}
	if len(label) > 63 {
		label = label[:63]
		label = strings.TrimRight(label, "-")
	}
	return label
}

// AzureCloudAppFQDN builds the free Azure DNS name for a public IP with a DNS label.
// Format: {label}.{location}.cloudapp.azure.com
func AzureCloudAppFQDN(dnsLabel, location string) string {
	loc := strings.ToLower(strings.ReplaceAll(location, " ", ""))
	return dnsLabel + "." + loc + ".cloudapp.azure.com"
}

func azureIngressHasDNSLabel(ctx context.Context, rc *RunContext, want string) (bool, error) {
	out, err := rc.Runner.Output(ctx, append([]string{
		"kubectl", "get", "svc", "ingress-nginx-controller", "-n", "ingress-nginx",
		"-o", `go-template={{index .metadata.annotations "service.beta.kubernetes.io/azure-dns-label-name"}}`,
	}, rc.kubectlFlags()...), nil)
	if err != nil {
		return false, err
	}
	got := strings.TrimSpace(string(out))
	return got == want && got != "", nil
}

func ensureAzureIngressFQDN(ctx context.Context, rc *RunContext) error {
	label := AzureDNSLabel(rc.Cfg.ClusterName.Value)
	loc := rc.Cfg.AzureLocation.Value
	if loc == "" {
		loc = "eastus"
	}
	fqdn := AzureCloudAppFQDN(label, loc)
	rc.AzureIngressFQDN = fqdn
	rc.Reporter.LogLine(fmt.Sprintf("[addons] ingress-nginx: Azure DNS label %q → FQDN %s", label, fqdn))
	rc.Reporter.LogLine("[addons] ingress-nginx: this FQDN is used as ingress.host (never the public IP)")

	// Seed config so a same-process app stage (or later auto-detect) has a DNS name.
	if rc.Cfg.IngressHost.Value == "" {
		rc.Cfg.IngressHost = config.Field[string]{Value: fqdn, Source: "auto:azure-dns-label"}
	}

	// Best-effort wait so the LB shows an address before we move on.
	_ = rc.Runner.Run(ctx, append([]string{
		"kubectl", "wait", "--for=jsonpath={.status.loadBalancer.ingress}",
		"svc/ingress-nginx-controller", "-n", "ingress-nginx",
		"--timeout=5m",
	}, rc.kubectlFlags()...), nil, rc.Reporter)

	has, err := azureIngressHasDNSLabel(ctx, rc, label)
	if err != nil {
		rc.Reporter.LogLine(fmt.Sprintf("[addons] ingress-nginx: warn verifying DNS label: %v", err))
	} else if !has {
		rc.Reporter.LogLine(fmt.Sprintf(
			"[addons] ingress-nginx: warn: azure-dns-label-name=%q not visible yet — FQDN %s should still resolve once Azure assigns it",
			label, fqdn,
		))
	}
	return nil
}
