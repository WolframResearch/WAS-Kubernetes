package collectors

import (
	"context"
	"fmt"
	"strings"
)

// NetworkingCollector collects ingress controllers, load balancer info, and DNS resolution.
type NetworkingCollector struct{}

func (NetworkingCollector) Name() string { return "networking" }

func (NetworkingCollector) Collect(ctx context.Context, cc *CollectContext) ([]File, error) {
	if cc.Kubeconfig == "" {
		return nil, fmt.Errorf("no cluster access (kubeconfig unavailable)")
	}

	kargs := kubectlArgs(cc)
	var files []File

	// Ingress controllers (ingress-nginx namespace).
	controllers, err := runOutput(ctx, "kubectl",
		append(kargs, "get", "all", "-n", "ingress-nginx", "-o", "json")...)
	if err != nil {
		controllers = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "networking/ingress_controllers.json", Content: controllers})

	// Load balancer service.
	lb, err := runOutput(ctx, "kubectl",
		append(kargs, "get", "svc", "-n", "ingress-nginx", "-o", "json")...)
	if err != nil {
		lb = []byte(fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	files = append(files, File{Path: "networking/load_balancer.json", Content: lb})

	// DNS resolution for the ingress host.
	var dnsResult strings.Builder
	host := ""
	if cc.Cfg != nil {
		host = cc.Cfg.IngressHost.Value
	}
	if host != "" {
		addrs, err := lookupHostFn(host)
		if err != nil {
			fmt.Fprintf(&dnsResult, "lookup %s: %s\n", host, err)
		} else {
			fmt.Fprintf(&dnsResult, "lookup %s:\n", host)
			for _, a := range addrs {
				fmt.Fprintf(&dnsResult, "  %s\n", a)
			}
		}
	} else {
		dnsResult.WriteString("no ingress host configured\n")
	}
	files = append(files, File{Path: "networking/dns_resolution.txt", Content: []byte(dnsResult.String())})

	return files, nil
}
