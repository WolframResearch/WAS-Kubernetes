package collectors

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
)

// SystemCollector collects basic OS and runtime information.
type SystemCollector struct{}

func (SystemCollector) Name() string { return "system" }

func (SystemCollector) Collect(ctx context.Context, cc *CollectContext) ([]File, error) {
	hostname, _ := os.Hostname()

	osInfo := fmt.Sprintf("OS:           %s/%s\nHostname:     %s\nGo runtime:   %s\n",
		runtime.GOOS, runtime.GOARCH, hostname, runtime.Version())

	wasctlVer := fmt.Sprintf("wasctl %s\n", version.Version)

	rawEnv := os.Environ()
	safeEnv := redactEnvVars(rawEnv)
	envLines := strings.Join(safeEnv, "\n") + "\n"

	return []File{
		text("system/os.txt", osInfo),
		text("system/wasctl_version.txt", wasctlVer),
		text("system/env.txt", envLines),
	}, nil
}
