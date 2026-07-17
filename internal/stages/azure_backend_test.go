package stages

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseBackendTFVarsStorageAccount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.tfvars")
	content := `resource_group_name  = "wolfram-was-tfstate-rg"
storage_account_name = "wolframwastfstat5ad81563"
container_name       = "tfstate"
key                  = "stack/terraform.tfstate"
access_key           = "secret"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := parseBackendTFVarsStorageAccount(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "wolframwastfstat5ad81563" {
		t.Fatalf("got %q", got)
	}
}
