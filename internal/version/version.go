package version

// Version is injected at build time via -ldflags:
//
//	go build -ldflags="-X github.com/WolframResearch/WAS-Kubernetes/internal/version.Version=1.0.0"
var Version = "dev"
