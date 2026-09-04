//go:generate go run gen.go

// Package assets provides the embedded Terraform modules and Helm chart that
// wasctl extracts at runtime into a temporary directory.
//
// The embedded directories are populated by `go generate` (see gen.go), which
// copies from the source locations:
//
//	infra/aws/bootstrap/       →  internal/assets/terraform/bootstrap/
//	infra/aws/stack/           →  internal/assets/terraform/stack/
//	infra/azure/bootstrap/     →  internal/assets/terraform/azure-bootstrap/
//	infra/azure/stack/         →  internal/assets/terraform/azure-stack/
//	charts/wolfram-application-server/ → internal/assets/chart/
//
// and writes internal/versions/k8s_cluster_gen.go from each stack’s
// cluster_version default (install UI offers last 3 minors ending at that pin).
//
// Production builds must run `make generate` (or `go generate ./internal/assets/`)
// before `go build` to populate
// these directories. The --local flag bypasses embedded assets and reads
// from the source directories on disk instead (for developer iteration).
package assets

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed all:terraform/bootstrap
var bootstrapEmbed embed.FS

//go:embed all:terraform/stack
var stackEmbed embed.FS

//go:embed all:terraform/azure-bootstrap
var azureBootstrapEmbed embed.FS

//go:embed all:terraform/azure-stack
var azureStackEmbed embed.FS

//go:embed all:chart
var chartEmbed embed.FS

// BootstrapFS is the embedded infra/aws/bootstrap/ Terraform module.
var BootstrapFS fs.FS

// StackFS is the embedded infra/aws/stack/ Terraform module.
var StackFS fs.FS

// AzureBootstrapFS is the embedded infra/azure/bootstrap/ Terraform module.
var AzureBootstrapFS fs.FS

// AzureStackFS is the embedded infra/azure/stack/ Terraform module.
var AzureStackFS fs.FS

// ChartFS is the embedded Helm chart.
var ChartFS fs.FS

func init() {
	var err error
	BootstrapFS, err = fs.Sub(bootstrapEmbed, "terraform/bootstrap")
	if err != nil {
		panic(fmt.Sprintf("assets: sub bootstrap FS: %v", err))
	}
	StackFS, err = fs.Sub(stackEmbed, "terraform/stack")
	if err != nil {
		panic(fmt.Sprintf("assets: sub stack FS: %v", err))
	}
	AzureBootstrapFS, err = fs.Sub(azureBootstrapEmbed, "terraform/azure-bootstrap")
	if err != nil {
		panic(fmt.Sprintf("assets: sub azure-bootstrap FS: %v", err))
	}
	AzureStackFS, err = fs.Sub(azureStackEmbed, "terraform/azure-stack")
	if err != nil {
		panic(fmt.Sprintf("assets: sub azure-stack FS: %v", err))
	}
	ChartFS, err = fs.Sub(chartEmbed, "chart")
	if err != nil {
		panic(fmt.Sprintf("assets: sub chart FS: %v", err))
	}
}

// LocalBootstrapFS returns an os.DirFS for the AWS bootstrap module.
func LocalBootstrapFS(repoRoot string) fs.FS {
	return localFS(repoRoot + "/infra/aws/bootstrap")
}

// LocalStackFS returns an os.DirFS for the AWS stack module.
func LocalStackFS(repoRoot string) fs.FS {
	return localFS(repoRoot + "/infra/aws/stack")
}

// LocalAzureBootstrapFS returns an os.DirFS for the Azure bootstrap module.
func LocalAzureBootstrapFS(repoRoot string) fs.FS {
	return localFS(repoRoot + "/infra/azure/bootstrap")
}

// LocalAzureStackFS returns an os.DirFS for the Azure stack module.
func LocalAzureStackFS(repoRoot string) fs.FS {
	return localFS(repoRoot + "/infra/azure/stack")
}

// LocalChartFS returns an os.DirFS for the Helm chart at the given repo root.
func LocalChartFS(repoRoot string) fs.FS {
	return localFS(repoRoot + "/charts/wolfram-application-server")
}

// VerifyEmbedded returns an error if any required asset is absent from the
// embedded FS. Call at startup when --local is not active. A missing file
// means the binary was built without running `go generate ./...` first.
func VerifyEmbedded() error {
	// Sentinel files that always exist in each module. Stacks are split across
	// multiple .tf files (no top-level main.tf); bootstraps still use main.tf.
	checks := []struct {
		label string
		fsys  fs.FS
		file  string
	}{
		{"aws bootstrap", BootstrapFS, "main.tf"},
		{"aws stack", StackFS, "eks.tf"},
		{"azure bootstrap", AzureBootstrapFS, "main.tf"},
		{"azure stack", AzureStackFS, "aks.tf"},
		{"helm chart", ChartFS, "Chart.yaml"},
	}
	for _, c := range checks {
		f, err := c.fsys.Open(c.file)
		if err != nil {
			return fmt.Errorf("required embedded asset missing (%s/%s): was `go generate ./...` run before build?", c.label, c.file)
		}
		f.Close()
	}
	return nil
}

func localFS(path string) fs.FS {
	return &localDirFS{path: path}
}

// localDirFS implements fs.FS using os.DirFS. We wrap it to surface a useful
// error message if the --local directory doesn't exist.
type localDirFS struct{ path string }

func (l *localDirFS) Open(name string) (fs.File, error) {
	dir := fs.FS(nativeFS(l.path))
	f, err := dir.Open(name)
	if err != nil {
		return nil, fmt.Errorf(
			"--local flag is set but %q does not contain the expected assets: %w\n"+
				"The --local flag requires running wasctl from the source repo root.\n"+
				"Current path: %s",
			name, err, l.path,
		)
	}
	return f, nil
}
