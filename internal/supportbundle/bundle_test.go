package supportbundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/supportbundle/collectors"
)

// mockCollector is a test double for Collector.
type mockCollector struct {
	name  string
	files []collectors.File
	err   error
}

func (m *mockCollector) Name() string { return m.name }
func (m *mockCollector) Collect(_ context.Context, _ *collectors.CollectContext) ([]collectors.File, error) {
	return m.files, m.err
}

func testProgress() *ProgressWriter {
	return NewProgressWriter(io.Discard)
}

func testBuildCfg() *config.Config {
	return &config.Config{
		ClusterName: config.Field[string]{Value: "was-test"},
		Region:      config.Field[string]{Value: "us-east-1"},
		MetaRegion:  config.Field[string]{Value: "us-east-1"},
	}
}

func TestBuild(t *testing.T) {
	var dst bytes.Buffer
	err := Build(context.Background(), testBuildCfg(), nil, "", "",
		Options{Cluster: "was-test"},
		testProgress(), &dst)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Verify it's a valid tar.gz.
	gr, err := gzip.NewReader(&dst)
	if err != nil {
		t.Fatalf("not a gzip: %v", err)
	}
	tr := tar.NewReader(gr)
	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar error: %v", err)
		}
		if hdr.Name == "manifest.json" {
			found = true
		}
	}
	if !found {
		t.Fatal("manifest.json not found in bundle")
	}
}

func TestBuild_FilterInclude(t *testing.T) {
	colls := filterCollectors([]collectors.Collector{
		&mockCollector{name: "system"},
		&mockCollector{name: "aws"},
		&mockCollector{name: "kubernetes"},
	}, []string{"system", "aws"}, nil)

	if len(colls) != 2 {
		t.Fatalf("expected 2, got %d", len(colls))
	}
}

func TestBuild_FilterExclude(t *testing.T) {
	colls := filterCollectors([]collectors.Collector{
		&mockCollector{name: "system"},
		&mockCollector{name: "logs"},
		&mockCollector{name: "kubernetes"},
	}, nil, []string{"logs"})

	if len(colls) != 2 {
		t.Fatalf("expected 2, got %d", len(colls))
	}
}

func TestBuild_FilterNone(t *testing.T) {
	all := []collectors.Collector{
		&mockCollector{name: "system"},
		&mockCollector{name: "aws"},
	}
	colls := filterCollectors(all, nil, nil)
	if len(colls) != len(all) {
		t.Fatalf("expected %d, got %d", len(all), len(colls))
	}
}

func TestBuild_ManifestContents(t *testing.T) {
	var dst bytes.Buffer
	err := Build(context.Background(), testBuildCfg(), nil, "", "",
		Options{Cluster: "was-test"},
		testProgress(), &dst)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Extract and read manifest.json.
	gr, _ := gzip.NewReader(&dst)
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		if hdr.Name == "manifest.json" {
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read manifest: %v", err)
			}
			s := string(data)
			if !strings.Contains(s, `"version"`) {
				t.Fatalf("manifest missing version: %s", s)
			}
			if !strings.Contains(s, `"was-test"`) {
				t.Fatalf("manifest missing cluster name: %s", s)
			}
			return
		}
	}
	t.Fatal("manifest.json not found")
}

func TestWriteTarGz(t *testing.T) {
	files := []collectors.File{
		{Path: "test/hello.txt", Content: []byte("hello world")},
	}
	var dst bytes.Buffer
	if err := writeTarGz(&dst, files, time.Now()); err != nil {
		t.Fatalf("writeTarGz: %v", err)
	}
	gr, err := gzip.NewReader(&dst)
	if err != nil {
		t.Fatalf("not gzip: %v", err)
	}
	tr := tar.NewReader(gr)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("tar next: %v", err)
	}
	if hdr.Name != "test/hello.txt" {
		t.Fatalf("unexpected path: %q", hdr.Name)
	}
}

func TestProgress_BundleDone(t *testing.T) {
	var buf bytes.Buffer
	p := NewProgressWriter(&buf)
	p.BundleDone("bundle.tar.gz", 1024*50)
	if !strings.Contains(buf.String(), "bundle.tar.gz") {
		t.Fatal("expected bundle path in output")
	}
	if !strings.Contains(buf.String(), "support@wolfram.com") {
		t.Fatal("expected support email in output")
	}
}

func TestProgress_CollectorDone(t *testing.T) {
	var buf bytes.Buffer
	p := NewProgressWriter(&buf)
	p.CollectorDone("system", 500_000_000, 3) // 500ms
	if buf.Len() == 0 {
		t.Fatal("expected output")
	}
}

func TestManifest_JSON(t *testing.T) {
	m := &Manifest{
		Version:  ManifestVersion,
		Cluster:  "was-test",
		Sections: []string{"system"},
	}
	b, err := m.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if !strings.Contains(string(b), `"was-test"`) {
		t.Fatal("cluster name missing from manifest JSON")
	}
}

func TestFmtSize(t *testing.T) {
	cases := []struct {
		bytes int64
		want  string
	}{
		{512, "512 B"},
		{1024 * 2, "2.0 KB"},
		{1024 * 1024 * 3, "3.0 MB"},
	}
	for _, tc := range cases {
		got := fmtSize(tc.bytes)
		if got != tc.want {
			t.Errorf("fmtSize(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}

func TestFmtElapsed(t *testing.T) {
	if fmtElapsed(500_000_000) == "" {
		t.Fatal("fmtElapsed returned empty")
	}
	if fmtElapsed(100_000_000) == "" {
		t.Fatal("fmtElapsed sub-second returned empty")
	}
}
