package repo_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/repo"
)

func TestLocateFindsGoMod(t *testing.T) {
	// Create a temp tree: root/sub/leaf — go.mod in root
	root := t.TempDir()
	leaf := filepath.Join(root, "sub", "leaf")
	if err := os.MkdirAll(leaf, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n"), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := repo.Locate(leaf)
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if got != root {
		t.Errorf("got %q, want %q", got, root)
	}
}

func TestLocateFindsGitDir(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	got, err := repo.Locate(nested)
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if got != root {
		t.Errorf("got %q, want %q", got, root)
	}
}

func TestLocateErrorsWhenNoMarker(t *testing.T) {
	tmp := t.TempDir()
	// No .git or go.mod anywhere in the tree.
	_, err := repo.Locate(tmp)
	if err == nil {
		t.Error("expected error when no marker found")
	}
}

func TestLocateUsesCurrentDirWhenStartEmpty(t *testing.T) {
	// Locate("") should use the cwd, which in tests is inside the module.
	got, err := repo.Locate("")
	if err != nil {
		t.Fatalf("Locate empty: %v", err)
	}
	if got == "" {
		t.Error("expected non-empty result")
	}
}
