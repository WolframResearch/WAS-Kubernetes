package collectors

import (
	"context"
	"net"
	"os/exec"
)

// runOutputFn is the injectable exec helper for all collectors.
// Tests override this to return mock output without executing real commands.
var runOutputFn = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// lookupHostFn is the injectable DNS resolver for the networking collector.
var lookupHostFn = net.LookupHost

// runOutput executes a command and returns combined stdout.
func runOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return runOutputFn(ctx, name, args...)
}

// kubectlArgs builds a kubectl argument list with optional kubeconfig + context.
func kubectlArgs(cc *CollectContext, args ...string) []string {
	var out []string
	if cc.Kubeconfig != "" {
		out = append(out, "--kubeconfig", cc.Kubeconfig)
	}
	if cc.ContextName != "" {
		out = append(out, "--context", cc.ContextName)
	}
	return append(out, args...)
}
