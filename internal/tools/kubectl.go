package tools

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// GetIngressNginxHostname queries the ingress-nginx-controller LoadBalancer
// and returns a DNS hostname suitable for Ingress spec.rules[].host.
//
//   - Prefer .status.loadBalancer.ingress[0].hostname when it is a DNS name
//     (AWS ELB/NLB, or Azure after azure-dns-label-name is set).
//   - On Azure, if status only has an IP, derive
//     {azure-dns-label}.{location}.cloudapp.azure.com from the service
//     annotation (never return a bare IP — Kubernetes rejects IP hosts).
//
// azureLocation is used only for the Azure FQDN fallback (e.g. "eastus").
func GetIngressNginxHostname(ctx context.Context, kubeconfigPath, contextName, azureLocation string) (string, error) {
	base := []string{
		"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
		"get", "svc", "ingress-nginx-controller",
		"-n", "ingress-nginx",
	}

	hostname, err := kubectlJSONPath(ctx, base, "{.status.loadBalancer.ingress[0].hostname}")
	if err != nil {
		return "", fmt.Errorf("get ingress-nginx LoadBalancer hostname: %w", err)
	}
	hostname = strings.TrimSpace(hostname)
	if hostname != "" && !isIPv4orIPv6(hostname) {
		return hostname, nil
	}

	ip, err := kubectlJSONPath(ctx, base, "{.status.loadBalancer.ingress[0].ip}")
	if err != nil {
		return "", fmt.Errorf("get ingress-nginx LoadBalancer IP: %w", err)
	}
	ip = strings.TrimSpace(ip)
	if ip == "" && hostname == "" {
		return "", fmt.Errorf("LoadBalancer address not yet assigned — still provisioning (retry in a minute)")
	}

	// Azure (or any LB with only an IP): require DNS label → cloudapp FQDN.
	label, lErr := kubectlGoTemplate(ctx, base,
		`{{index .metadata.annotations "service.beta.kubernetes.io/azure-dns-label-name"}}`)
	if lErr == nil {
		label = strings.TrimSpace(label)
	}
	if label != "" {
		loc := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(azureLocation), " ", ""))
		if loc == "" {
			loc = "eastus"
		}
		return label + "." + loc + ".cloudapp.azure.com", nil
	}

	if ip != "" {
		return "", fmt.Errorf(
			"ingress-nginx has public IP %s but no DNS hostname\n"+
				"Kubernetes Ingress host must be a DNS name, not an IP.\n"+
				"  • Re-run addons so wasctl sets azure-dns-label-name on the LB, or\n"+
				"  • Pass --ingress-host <dns-name> (e.g. was.example.com)",
			ip,
		)
	}
	return "", fmt.Errorf("LoadBalancer hostname not yet assigned — still provisioning (retry in a minute)")
}

func kubectlJSONPath(ctx context.Context, base []string, jsonPath string) (string, error) {
	args := append(append([]string{}, base[1:]...), "-o", "jsonpath="+jsonPath)
	out, err := exec.CommandContext(ctx, base[0], args...).Output() //nolint:gosec
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func kubectlGoTemplate(ctx context.Context, base []string, tmpl string) (string, error) {
	args := append(append([]string{}, base[1:]...), "-o", "go-template="+tmpl)
	out, err := exec.CommandContext(ctx, base[0], args...).Output() //nolint:gosec
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func isIPv4orIPv6(s string) bool {
	return net.ParseIP(s) != nil
}

// IsValidIngressHost reports whether s is acceptable as Ingress spec.rules[].host
// (DNS name, not an IP address). Kubernetes rejects bare IPs in spec.rules[].host.
func IsValidIngressHost(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && !isIPv4orIPv6(s)
}

// IsACMEIssuableHost reports whether a public ACME CA (e.g. Let's Encrypt) will
// accept the name. Cloud load-balancer hostnames are DNS-valid for Ingress but
// often forbidden by ACME policy (rejectedIdentifier).
//
// Azure *.cloudapp.azure.com is allowed here — wasctl uses it as the default
// public hostname with Let's Encrypt HTTP-01.
func IsACMEIssuableHost(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if !IsValidIngressHost(s) {
		return false
	}
	forbiddenSuffixes := []string{
		".amazonaws.com",
		".elasticbeanstalk.com",
		".cloudfront.net",
	}
	for _, suf := range forbiddenSuffixes {
		if strings.HasSuffix(s, suf) {
			return false
		}
	}
	return true
}

// KubectlDeletePVC returns kubectl delete pvc for the named PVCs in namespace.
func KubectlDeletePVC(namespace, kubeconfigPath, contextName string, pvcs ...string) []string {
	cmd := []string{
		"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
		"delete", "pvc", "-n", namespace,
	}
	return append(cmd, pvcs...)
}

// KubectlDeletePVCByLabel returns kubectl delete pvc -l <label> -n <namespace>.
func KubectlDeletePVCByLabel(namespace, kubeconfigPath, contextName, label string) []string {
	return []string{
		"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
		"delete", "pvc", "-l", label, "-n", namespace,
	}
}

// KubectlGetStorageClass returns kubectl get storageclass <name>.
func KubectlGetStorageClass(name, kubeconfigPath, contextName string) []string {
	return []string{
		"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
		"get", "storageclass", name,
	}
}

// KubectlGet returns kubectl get <resource> -n <namespace>.
func KubectlGet(resource, namespace, kubeconfigPath, contextName string) []string {
	return []string{
		"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
		"get", resource, "-n", namespace,
	}
}
