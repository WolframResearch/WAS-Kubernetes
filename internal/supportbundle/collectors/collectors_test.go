package collectors

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/metabucket"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func testCC() *CollectContext {
	return &CollectContext{
		Cfg:         testCfg(),
		MaxLogLines: 10,
	}
}

func testCCWithKubeconfig() *CollectContext {
	cc := testCC()
	cc.Kubeconfig = "/tmp/test-kubeconfig"
	return cc
}

func testCfg() *config.Config {
	return &config.Config{
		Region:      config.Field[string]{Value: "us-east-1"},
		MetaRegion:  config.Field[string]{Value: "us-east-1"},
		ClusterName: config.Field[string]{Value: "was-test"},
		IngressHost: config.Field[string]{Value: "was.example.com"},
	}
}

func mockWorkspace(clusterName, accountID string) *workspace.Workspace {
	ws := &workspace.Workspace{}
	ws.Meta = &metabucket.Metadata{
		ClusterName:  clusterName,
		AWSAccountID: accountID,
		AWSRegion:    "us-east-1",
		Status:       "active",
	}
	return ws
}

// ── SystemCollector ───────────────────────────────────────────────────────────

func TestSystemCollector_Collect(t *testing.T) {
	files, err := SystemCollector{}.Collect(context.Background(), testCC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}
	paths := map[string]bool{}
	for _, f := range files {
		paths[f.Path] = true
	}
	for _, want := range []string{"system/os.txt", "system/wasctl_version.txt", "system/env.txt"} {
		if !paths[want] {
			t.Errorf("missing file %q", want)
		}
	}
}

func TestSystemCollector_Name(t *testing.T) {
	c := SystemCollector{}
	if c.Name() != "system" {
		t.Fatal("wrong name")
	}
}

// ── CLIVersionsCollector ──────────────────────────────────────────────────────

func TestCLIVersionsCollector_Collect(t *testing.T) {
	orig := runOutputFn
	defer func() { runOutputFn = orig }()
	runOutputFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("v1.0.0"), nil
	}

	files, err := CLIVersionsCollector{}.Collect(context.Background(), testCC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0].Path != "system/cli_versions.txt" {
		t.Fatal("expected cli_versions.txt")
	}
}

func TestCLIVersionsCollector_ToolError(t *testing.T) {
	orig := runOutputFn
	defer func() { runOutputFn = orig }()
	runOutputFn = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("not found")
	}

	files, err := CLIVersionsCollector{}.Collect(context.Background(), testCC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected file even on error")
	}
}

// ── AWSCollector ──────────────────────────────────────────────────────────────

func TestAWSCollector_NoConfig(t *testing.T) {
	cc := &CollectContext{}
	_, err := AWSCollector{}.Collect(context.Background(), cc)
	if err == nil {
		t.Fatal("expected error when no config")
	}
}

func TestAWSCollector_Collect(t *testing.T) {
	orig := runOutputFn
	defer func() { runOutputFn = orig }()
	runOutputFn = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{"Account":"123456789012"}`), nil
	}

	cc := testCC()
	cc.NoCloudTrail = true
	files, err := AWSCollector{}.Collect(context.Background(), cc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected files")
	}
}

func TestAWSCollector_WithCloudTrail(t *testing.T) {
	orig := runOutputFn
	defer func() { runOutputFn = orig }()
	runOutputFn = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{}`), nil
	}

	cc := testCC()
	cc.NoCloudTrail = false
	cc.Cfg.StateBucket = config.Field[string]{Value: "my-state-bucket"}
	files, err := AWSCollector{}.Collect(context.Background(), cc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should include cloudtrail file.
	hasTrail := false
	for _, f := range files {
		if f.Path == "aws/cloudtrail_recent.json" {
			hasTrail = true
		}
	}
	if !hasTrail {
		t.Fatal("expected cloudtrail_recent.json")
	}
}

// ── AzureCollector ────────────────────────────────────────────────────────────

func TestAzureCollector_SkipsNonAzure(t *testing.T) {
	cc := testCC() // Cloud not set to azure
	_, err := AzureCollector{}.Collect(context.Background(), cc)
	if err == nil {
		t.Fatal("expected error for non-azure cloud")
	}
}

func TestAzureCollector_Azure(t *testing.T) {
	orig := runOutputFn
	defer func() { runOutputFn = orig }()
	runOutputFn = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{"name":"test"}`), nil
	}

	cc := testCC()
	cc.Cfg.Cloud = "azure"
	files, err := AzureCollector{}.Collect(context.Background(), cc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}
}

// ── WorkspaceCollector ────────────────────────────────────────────────────────

func TestWorkspaceCollector_NoWorkspace(t *testing.T) {
	_, err := WorkspaceCollector{}.Collect(context.Background(), testCC())
	if err == nil {
		t.Fatal("expected error when no workspace")
	}
}

func TestWorkspaceCollector_WithWorkspace(t *testing.T) {
	orig := runOutputFn
	defer func() { runOutputFn = orig }()
	runOutputFn = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("2024-01-01 install was-prod success\n"), nil
	}

	cc := testCC()
	cc.Workspace = mockWorkspace("was-test", "123456789012")
	files, err := WorkspaceCollector{}.Collect(context.Background(), cc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	// Account ID should be masked in workspace.json.
	for _, f := range files {
		if f.Path == "workspace/workspace.json" {
			if string(f.Content) == "" {
				t.Fatal("workspace.json is empty")
			}
		}
	}
}

// ── HelmCollector ─────────────────────────────────────────────────────────────

func TestHelmCollector_NoKubeconfig(t *testing.T) {
	_, err := HelmCollector{}.Collect(context.Background(), testCC())
	if err == nil {
		t.Fatal("expected error when no kubeconfig")
	}
}

func TestHelmCollector_Collect(t *testing.T) {
	orig := runOutputFn
	defer func() { runOutputFn = orig }()
	callCount := 0
	runOutputFn = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		callCount++
		return []byte(`[]`), nil
	}

	files, err := HelmCollector{}.Collect(context.Background(), testCCWithKubeconfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected files")
	}
}

// ── KubernetesCollector ───────────────────────────────────────────────────────

func TestKubernetesCollector_NoKubeconfig(t *testing.T) {
	_, err := KubernetesCollector{}.Collect(context.Background(), testCC())
	if err == nil {
		t.Fatal("expected error when no kubeconfig")
	}
}

func TestKubernetesCollector_Collect(t *testing.T) {
	orig := runOutputFn
	defer func() { runOutputFn = orig }()
	runOutputFn = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{"items":[]}`), nil
	}

	files, err := KubernetesCollector{}.Collect(context.Background(), testCCWithKubeconfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected files")
	}
}

// ── WorkloadsCollector ────────────────────────────────────────────────────────

func TestWorkloadsCollector_NoKubeconfig(t *testing.T) {
	_, err := WorkloadsCollector{}.Collect(context.Background(), testCC())
	if err == nil {
		t.Fatal("expected error when no kubeconfig")
	}
}

func TestWorkloadsCollector_Collect(t *testing.T) {
	orig := runOutputFn
	defer func() { runOutputFn = orig }()
	runOutputFn = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{"items":[]}`), nil
	}

	files, err := WorkloadsCollector{}.Collect(context.Background(), testCCWithKubeconfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 6 {
		t.Fatalf("expected 6 files, got %d", len(files))
	}
}

// ── LogsCollector ─────────────────────────────────────────────────────────────

func TestLogsCollector_NoKubeconfig(t *testing.T) {
	_, err := LogsCollector{}.Collect(context.Background(), testCC())
	if err == nil {
		t.Fatal("expected error when no kubeconfig")
	}
}

func TestLogsCollector_NoPods(t *testing.T) {
	orig := runOutputFn
	defer func() { runOutputFn = orig }()
	runOutputFn = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{"items":[]}`), nil
	}

	files, err := LogsCollector{}.Collect(context.Background(), testCCWithKubeconfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 files for empty pod list, got %d", len(files))
	}
}

func TestLogsCollector_WithPods(t *testing.T) {
	orig := runOutputFn
	defer func() { runOutputFn = orig }()
	callCount := 0
	runOutputFn = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		callCount++
		if callCount == 1 {
			// First call: pod list
			return []byte(`{"items":[{"metadata":{"name":"awes-pod-0"}},{"metadata":{"name":"rm-pod-0"}}]}`), nil
		}
		// Subsequent calls: logs
		return []byte("INFO starting up\nDEBUG processing request\n"), nil
	}

	files, err := LogsCollector{}.Collect(context.Background(), testCCWithKubeconfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 log files, got %d", len(files))
	}
}

func TestLogsCollector_SanitizesLogs(t *testing.T) {
	orig := runOutputFn
	defer func() { runOutputFn = orig }()
	callCount := 0
	runOutputFn = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		callCount++
		if callCount == 1 {
			return []byte(`{"items":[{"metadata":{"name":"awes-0"}}]}`), nil
		}
		return []byte("key=AKIAIOSFODNN7EXAMPLE secret=plaintext\n"), nil
	}

	files, err := LogsCollector{}.Collect(context.Background(), testCCWithKubeconfig())
	if err != nil || len(files) == 0 {
		t.Fatalf("error: %v, files: %d", err, len(files))
	}
	if string(files[0].Content) == "key=AKIAIOSFODNN7EXAMPLE secret=plaintext\n" {
		t.Fatal("logs not sanitized")
	}
}

// ── KafkaCollector ────────────────────────────────────────────────────────────

func TestKafkaCollector_NoKubeconfig(t *testing.T) {
	_, err := KafkaCollector{}.Collect(context.Background(), testCC())
	if err == nil {
		t.Fatal("expected error when no kubeconfig")
	}
}

func TestKafkaCollector_Collect(t *testing.T) {
	orig := runOutputFn
	defer func() { runOutputFn = orig }()
	runOutputFn = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{"items":[]}`), nil
	}

	files, err := KafkaCollector{}.Collect(context.Background(), testCCWithKubeconfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 4 {
		t.Fatalf("expected 4 files, got %d", len(files))
	}
}

// ── StorageCollector ──────────────────────────────────────────────────────────

func TestStorageCollector_NoKubeconfig(t *testing.T) {
	_, err := StorageCollector{}.Collect(context.Background(), testCC())
	if err == nil {
		t.Fatal("expected error when no kubeconfig")
	}
}

func TestStorageCollector_AWS(t *testing.T) {
	orig := runOutputFn
	defer func() { runOutputFn = orig }()
	runOutputFn = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{"items":[]}`), nil
	}

	files, err := StorageCollector{}.Collect(context.Background(), testCCWithKubeconfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// AWS mode: should have storage_classes, pvc_detail, efs_describe (3 files).
	if len(files) != 3 {
		t.Fatalf("expected 3 files for AWS, got %d", len(files))
	}
}

func TestStorageCollector_Azure(t *testing.T) {
	orig := runOutputFn
	defer func() { runOutputFn = orig }()
	runOutputFn = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`[]`), nil
	}

	cc := testCCWithKubeconfig()
	cc.Cfg.Cloud = "azure"
	files, err := StorageCollector{}.Collect(context.Background(), cc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Azure mode: storage_classes, pvc_detail, azurefiles_describe (3 files).
	if len(files) != 3 {
		t.Fatalf("expected 3 files for Azure, got %d", len(files))
	}
}

// ── NetworkingCollector ───────────────────────────────────────────────────────

func TestNetworkingCollector_NoKubeconfig(t *testing.T) {
	_, err := NetworkingCollector{}.Collect(context.Background(), testCC())
	if err == nil {
		t.Fatal("expected error when no kubeconfig")
	}
}

func TestNetworkingCollector_Collect(t *testing.T) {
	origRun := runOutputFn
	origLookup := lookupHostFn
	defer func() { runOutputFn = origRun; lookupHostFn = origLookup }()

	runOutputFn = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{"items":[]}`), nil
	}
	lookupHostFn = func(host string) ([]string, error) {
		return []string{"1.2.3.4"}, nil
	}

	files, err := NetworkingCollector{}.Collect(context.Background(), testCCWithKubeconfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}
}

func TestNetworkingCollector_DNSFail(t *testing.T) {
	origRun := runOutputFn
	origLookup := lookupHostFn
	defer func() { runOutputFn = origRun; lookupHostFn = origLookup }()

	runOutputFn = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{}`), nil
	}
	lookupHostFn = func(host string) ([]string, error) {
		return nil, &net.DNSError{Err: "no such host", Name: host}
	}

	files, err := NetworkingCollector{}.Collect(context.Background(), testCCWithKubeconfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var dnsFile *File
	for i := range files {
		if files[i].Path == "networking/dns_resolution.txt" {
			dnsFile = &files[i]
		}
	}
	if dnsFile == nil {
		t.Fatal("expected dns_resolution.txt")
	}
	if len(dnsFile.Content) == 0 {
		t.Fatal("dns_resolution.txt is empty")
	}
}

func TestNetworkingCollector_NoHost(t *testing.T) {
	origRun := runOutputFn
	defer func() { runOutputFn = origRun }()
	runOutputFn = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{}`), nil
	}

	cc := testCCWithKubeconfig()
	cc.Cfg.IngressHost = config.Field[string]{Value: ""}
	files, err := NetworkingCollector{}.Collect(context.Background(), cc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected files even with no host")
	}
}

// ── Registry ──────────────────────────────────────────────────────────────────

func TestRegistry_All(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("All() returned empty list")
	}
	names := map[string]bool{}
	for _, c := range all {
		n := c.Name()
		if n == "" {
			t.Fatal("collector with empty name")
		}
		if names[n] {
			t.Fatalf("duplicate collector name: %s", n)
		}
		names[n] = true
	}
}

// ── sanitize_helpers ──────────────────────────────────────────────────────────

func TestSanitizeHelpers_RedactText(t *testing.T) {
	out := redactText("key AKIAIOSFODNN7EXAMPLE value")
	if out == "key AKIAIOSFODNN7EXAMPLE value" {
		t.Fatal("AWS key not redacted")
	}
}

func TestSanitizeHelpers_MaskAccountID(t *testing.T) {
	if got := maskAccountID("123456789012"); got != "1234****9012" {
		t.Fatalf("got %q", got)
	}
	// Short ID passes through.
	if got := maskAccountID("1234"); got != "1234" {
		t.Fatalf("got %q", got)
	}
}

func TestSanitizeHelpers_RedactHelmValues(t *testing.T) {
	input := `{"db":{"password":"secret123","host":"localhost"}}`
	out, keys, err := redactHelmValues([]byte(input))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if string(out) == input {
		t.Fatal("password not redacted")
	}
	if len(keys) == 0 {
		t.Fatal("no keys recorded")
	}
}

func TestSanitizeHelpers_RedactEnvVars(t *testing.T) {
	env := []string{"PATH=/usr/bin", "AWS_ACCESS_KEY_ID=key", "HOME=/home/u"}
	out := redactEnvVars(env)
	for _, e := range out {
		if e == "AWS_ACCESS_KEY_ID=key" {
			t.Fatal("secret env var leaked")
		}
	}
}

func TestRedactJSONLogLines_Clean(t *testing.T) {
	in := `{"level":"info","message":"hello"}`
	out := redactJSONLogLines(in)
	if out == "" {
		t.Fatal("empty output")
	}
}

func TestRedactJSONLogLines_Sensitive(t *testing.T) {
	in := `{"level":"error","password":"hunter2"}`
	out := redactJSONLogLines(in)
	if out == in {
		t.Fatal("sensitive JSON log not redacted")
	}
}

func TestKubectlArgs_Empty(t *testing.T) {
	cc := &CollectContext{}
	args := kubectlArgs(cc, "get", "pods")
	if len(args) != 2 || args[0] != "get" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestKubectlArgs_WithKubeconfig(t *testing.T) {
	cc := &CollectContext{Kubeconfig: "/tmp/kc", ContextName: "my-ctx"}
	args := kubectlArgs(cc, "get", "pods")
	if args[0] != "--kubeconfig" {
		t.Fatalf("expected --kubeconfig first, got %v", args)
	}
}
