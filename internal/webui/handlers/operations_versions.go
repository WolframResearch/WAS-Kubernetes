package handlers

import (
	"net/http"

	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
	"github.com/WolframResearch/WAS-Kubernetes/internal/versions"
)

type versionRow struct {
	Component string
	Current   string // "" if not detected
	Range     string // e.g. "3.10.0 – 3.17.99"
	Status    string // "pass", "warning", "critical", "unknown"
	StatusMsg string // "✓", "⚠ above supported", "[!] below minimum", "not detected"
}

type opsVersionsData struct {
	Version     string
	ClusterName string
	ActiveTab   string
	Rows        []versionRow
	Error       string
}

// OpsVersions handles GET /clusters/{name}/operations/versions.
func OpsVersions(t *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		data := opsVersionsData{
			Version:     version.Version,
			ClusterName: name,
			ActiveTab:   "versions",
		}

		detected, err := versions.Detect()
		if err != nil {
			data.Error = "could not detect versions: " + err.Error()
		} else {
			data.Rows = BuildVersionRows(detected)
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.OpsVersions.ExecuteTemplate(w, "layout", data); err != nil {
			renderErr(w, err)
		}
	}
}

var versionComponents = []struct {
	key   string
	label string
	rang  func() versions.VersionRange
}{
	{"helm", "Helm", func() versions.VersionRange { return versions.CurrentMatrix.Helm }},
	{"kubectl", "kubectl", func() versions.VersionRange { return versions.CurrentMatrix.Kubectl }},
	{"terraform", "Terraform", func() versions.VersionRange { return versions.CurrentMatrix.Terraform }},
	{"aws-cli", "AWS CLI", func() versions.VersionRange { return versions.CurrentMatrix.AWSCLI }},
	{"azure-cli", "Azure CLI", func() versions.VersionRange { return versions.CurrentMatrix.AzureCLI }},
	{"kubernetes", "Kubernetes (server)", func() versions.VersionRange { return versions.CurrentMatrix.Kubernetes }},
	{"strimzi", "Strimzi operator", func() versions.VersionRange { return versions.CurrentMatrix.Strimzi }},
	{"chart", "WAS Helm chart", func() versions.VersionRange { return versions.CurrentMatrix.Chart }},
	{"terraform-aws-provider", "hashicorp/aws provider", func() versions.VersionRange { return versions.CurrentMatrix.AWSProvider }},
	{"terraform-azure-provider", "hashicorp/azurerm provider", func() versions.VersionRange { return versions.CurrentMatrix.AzureProvider }},
}

// BuildVersionRows is exported for tests.
func BuildVersionRows(detected map[string]versions.Version) []versionRow {
	rows := make([]versionRow, 0, len(versionComponents))
	for _, comp := range versionComponents {
		rang := comp.rang()
		row := versionRow{
			Component: comp.label,
			Range:     rang.String(),
		}

		v, ok := detected[comp.key]
		if !ok {
			row.Status = "unknown"
			row.StatusMsg = "not detected"
			rows = append(rows, row)
			continue
		}

		row.Current = v.String()
		switch {
		case v.LessThan(rang.Min):
			row.Status = "critical"
			row.StatusMsg = "[!] below minimum"
		case v.GreaterThan(rang.Max):
			row.Status = "warning"
			row.StatusMsg = "[~] above supported"
		default:
			row.Status = "pass"
			row.StatusMsg = "✓"
		}
		rows = append(rows, row)
	}
	return rows
}
