package addons

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
)

// ── test helpers ─────────────────────────────────────────────────────────────

type noopReporter struct{ lines []string }

func (r *noopReporter) SubstepStart(name string) {}
func (r *noopReporter) SubstepDone()             {}
func (r *noopReporter) SubstepFail(error)        {}
func (r *noopReporter) LogLine(l string)         { r.lines = append(r.lines, l) }

func testRC(m *runner.MockRunner) *RunContext {
	rep := &noopReporter{}
	return &RunContext{
		Cfg:         &config.Config{},
		Kubeconfig:  "/tmp/test.kubeconfig",
		KubeContext: "test-context",
		Runner:      m,
		Reporter:    rep,
	}
}

func helmStatusJSON(status string) []byte {
	payload := map[string]any{"info": map[string]any{"status": status}}
	b, _ := json.Marshal(payload)
	return b
}

// ── HelmComponent.Check ───────────────────────────────────────────────────────

func TestCheck_Deployed(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("helm status ingress-nginx", helmStatusJSON("deployed"), nil)

	h := NewIngressNginx()
	state, err := h.Check(context.Background(), testRC(m))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != StateHealthy {
		t.Errorf("expected StateHealthy, got %d", state)
	}
}

func TestCheck_Failed(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("helm status ingress-nginx", helmStatusJSON("failed"), nil)

	h := NewIngressNginx()
	state, err := h.Check(context.Background(), testRC(m))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != StateFailed {
		t.Errorf("expected StateFailed, got %d", state)
	}
}

func TestCheck_PendingInstall(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("helm status ingress-nginx", helmStatusJSON("pending-install"), nil)

	h := NewIngressNginx()
	state, _ := h.Check(context.Background(), testRC(m))
	if state != StateFailed {
		t.Errorf("pending-install should map to StateFailed, got %d", state)
	}
}

func TestCheck_NotInstalled(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("helm status ingress-nginx", nil, errors.New("release not found"))

	h := NewIngressNginx()
	state, err := h.Check(context.Background(), testRC(m))
	if err != nil {
		t.Fatalf("missing release should not be an error: %v", err)
	}
	if state != StateNotInstalled {
		t.Errorf("expected StateNotInstalled, got %d", state)
	}
}

// ── Install state machine ─────────────────────────────────────────────────────

func TestInstall_Healthy_Skips(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("helm status ingress-nginx", helmStatusJSON("deployed"), nil)
	// ensure namespace + repo add/update are still permitted
	m.Register("kubectl apply", nil)
	m.Register("helm repo", nil)

	h := NewIngressNginx()
	rc := testRC(m)
	if err := h.Install(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// helm upgrade --install must NOT have been called
	if m.CalledWith("helm upgrade --install") {
		t.Error("helm upgrade --install should not be called when already healthy")
	}
	rep := rc.Reporter.(*noopReporter)
	found := false
	for _, l := range rep.lines {
		if strings.Contains(l, "already healthy") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'already healthy' in log; got: %v", rep.lines)
	}
}

func TestInstall_Failed_UninstallsThenReinstalls(t *testing.T) {
	m := runner.NewMock()
	// Check returns StateFailed. Install sets state=StateNotInstalled after
	// uninstall without a second Check call, so only one output rule needed.
	m.RegisterOutput("helm status ingress-nginx", helmStatusJSON("failed"), nil)
	// Orphan cleanup runs before helm upgrade; return empty (resource absent).
	m.RegisterOutput("kubectl get", []byte{}, nil)
	m.Register("kubectl apply", nil)
	m.Register("helm repo", nil)
	m.Register("helm uninstall", nil)
	m.Register("helm upgrade", nil)

	h := NewIngressNginx()
	if err := h.Install(context.Background(), testRC(m)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.CalledWith("helm uninstall") {
		t.Error("expected helm uninstall to be called for failed release")
	}
	if !m.CalledWith("helm upgrade --install") {
		t.Error("expected helm upgrade --install after cleanup")
	}
}

func TestInstall_NotInstalled_Installs(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("helm status metrics-server", nil, errors.New("not found"))
	// Orphan cleanup runs before helm upgrade; return empty (resource absent).
	m.RegisterOutput("kubectl get", []byte{}, nil)
	m.Register("kubectl apply", nil)
	m.Register("helm repo", nil)
	m.Register("helm upgrade", nil)

	h := NewMetricsServer()
	if err := h.Install(context.Background(), testRC(m)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.CalledWith("helm upgrade --install metrics-server") {
		t.Errorf("expected helm upgrade --install; calls: %v", m.Calls)
	}
}

func TestInstall_MetricsServer_SkipsClusterAddon(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get deploy metrics-server", []byte("Reconcile"), nil)
	m.Register("helm repo", nil) // should not be needed; Install returns early

	if err := NewMetricsServer().Install(context.Background(), testRC(m)); err != nil {
		t.Fatal(err)
	}
	if m.CalledWith("helm upgrade") {
		t.Error("must not helm-install over AKS cluster addon")
	}
}

// ── ensureNamespace ───────────────────────────────────────────────────────────

func TestEnsureNamespace_Idempotent(t *testing.T) {
	m := runner.NewMock()
	m.Register("kubectl apply", nil)

	rc := testRC(m)
	if err := ensureNamespace(context.Background(), rc, "was"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := ensureNamespace(context.Background(), rc, "was"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	// kubectl apply -f should have been called twice; both succeeded (idempotent).
	count := 0
	for _, c := range m.Calls {
		if strings.Join(c, " ") == strings.Join([]string{"kubectl", "apply", "-f",
			// path varies; just check the verb
			c[len(c)-1], "--kubeconfig", "/tmp/test.kubeconfig",
			"--context", "test-context"}, " ") {
			count++
		}
	}
	if len(m.Calls) != 2 {
		t.Errorf("expected 2 kubectl apply calls, got %d", len(m.Calls))
	}
}

func TestEnsureNamespace_CarriesKubeflags(t *testing.T) {
	m := runner.NewMock()
	m.Register("kubectl apply", nil)
	rc := testRC(m)

	if err := ensureNamespace(context.Background(), rc, "kafka"); err != nil {
		t.Fatal(err)
	}
	call := strings.Join(m.Calls[0], " ")
	if !strings.Contains(call, "--kubeconfig /tmp/test.kubeconfig") {
		t.Errorf("missing --kubeconfig: %s", call)
	}
	if !strings.Contains(call, "--context test-context") {
		t.Errorf("missing --context: %s", call)
	}
}

// ── Strimzi specifics ─────────────────────────────────────────────────────────

func TestStrimzi_WatchNamespacesMatchesWatchedNS(t *testing.T) {
	s := NewStrimzi()
	wv, ok := s.Values["watchNamespaces"]
	if !ok {
		t.Fatal("watchNamespaces not set")
	}
	// Every WatchedNS entry must appear in the watchNamespaces value.
	for _, ns := range s.WatchedNS {
		if !strings.Contains(wv, ns) {
			t.Errorf("WatchedNS %q not in watchNamespaces value %q", ns, wv)
		}
	}
	// Release namespace must NOT appear to avoid duplicate RoleBinding definitions.
	if strings.Contains(wv, s.Namespace) {
		t.Errorf("release namespace %q should not be in watchNamespaces value %q", s.Namespace, wv)
	}
}

func TestStrimzi_NamespacesIncludesWatched(t *testing.T) {
	s := NewStrimzi()
	ns := s.Namespaces()
	want := map[string]bool{"strimzi-system": true, "was": true, "kafka": true}
	for _, n := range ns {
		delete(want, n)
	}
	if len(want) > 0 {
		t.Errorf("missing namespaces: %v", want)
	}
}

// ── Registry ─────────────────────────────────────────────────────────────────

func TestRegistry_AWS(t *testing.T) {
	list := AddonsFor("aws")
	names := make([]string, len(list))
	for i, a := range list {
		names[i] = a.Name()
	}
	// must include AWS-specific entries
	mustHave := []string{
		"aws-efs-csi-driver", "was-efs-storageclass",
		"aws-ebs-csi-driver", "was-aws-kafka-storageclass",
		"strimzi-kafka-operator",
	}
	for _, want := range mustHave {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AWS registry missing %q; got %v", want, names)
		}
	}
	// must NOT include Azure entries
	for _, n := range names {
		if n == "was-azurefile-storageclass" {
			t.Errorf("AWS registry must not include was-azurefile-storageclass")
		}
	}
}

func TestRegistry_Azure(t *testing.T) {
	list := AddonsFor("azure")
	names := make([]string, len(list))
	for i, a := range list {
		names[i] = a.Name()
	}
	mustHave := []string{"was-azurefile-storageclass", "strimzi-kafka-operator"}
	for _, want := range mustHave {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Azure registry missing %q; got %v", want, names)
		}
	}
	for _, n := range names {
		if n == "aws-efs-csi-driver" || n == "was-efs-storageclass" ||
			n == "aws-ebs-csi-driver" || n == "was-aws-kafka-storageclass" {
			t.Errorf("Azure registry must not include %q", n)
		}
	}
}

func TestRegistry_OrderCSIBeforeStorageClass(t *testing.T) {
	list := AddonsFor("aws")
	efsCsi, efsSC, ebsCsi, kafkaSC := -1, -1, -1, -1
	for i, a := range list {
		switch a.Name() {
		case "aws-efs-csi-driver":
			efsCsi = i
		case "was-efs-storageclass":
			efsSC = i
		case "aws-ebs-csi-driver":
			ebsCsi = i
		case "was-aws-kafka-storageclass":
			kafkaSC = i
		}
	}
	if efsCsi < 0 || efsSC < 0 || ebsCsi < 0 || kafkaSC < 0 {
		t.Fatal("csi driver or storageclass missing from AWS registry")
	}
	if efsCsi >= efsSC {
		t.Errorf("aws-efs-csi-driver (idx %d) must precede was-efs-storageclass (idx %d)", efsCsi, efsSC)
	}
	if ebsCsi >= kafkaSC {
		t.Errorf("aws-ebs-csi-driver (idx %d) must precede was-aws-kafka-storageclass (idx %d)", ebsCsi, kafkaSC)
	}
}

func TestRegistry_OrderPrometheusBeforeAdapter(t *testing.T) {
	list := AddonsFor("aws")
	stackIdx, adapterIdx := -1, -1
	for i, a := range list {
		switch a.Name() {
		case "kube-prometheus-stack":
			stackIdx = i
		case "prometheus-adapter":
			adapterIdx = i
		}
	}
	if stackIdx < 0 || adapterIdx < 0 {
		t.Fatal("kube-prometheus-stack or prometheus-adapter missing")
	}
	if stackIdx >= adapterIdx {
		t.Errorf("kube-prometheus-stack must precede prometheus-adapter")
	}
}

// ── EFSStorageClass ───────────────────────────────────────────────────────────

func TestEFSStorageClass_RequiresFilesystemID(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get storageclass was-efs", nil, errors.New("not found"))

	rc := testRC(m)
	// EFSFilesystemID not set → error
	err := (&EFSStorageClass{}).Install(context.Background(), rc)
	if err == nil {
		t.Fatal("expected error when EFSFilesystemID is empty")
	}
	if !strings.Contains(err.Error(), "EFSFilesystemID") {
		t.Errorf("expected EFSFilesystemID in error: %v", err)
	}
}

func TestEFSStorageClass_SkipsWhenPresent(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get storageclass was-efs", []byte("was-efs"), nil)

	rc := testRC(m)
	rc.EFSFilesystemID = "fs-12345"
	if err := (&EFSStorageClass{}).Install(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.CalledWith("kubectl apply") {
		t.Error("kubectl apply must not be called when StorageClass already exists")
	}
}

func TestEFSStorageClass_AppliesManifestWithFilesystemID(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get storageclass was-efs", nil, errors.New("not found"))
	m.Register("kubectl apply", nil)

	rc := testRC(m)
	rc.EFSFilesystemID = "fs-abc123"
	if err := (&EFSStorageClass{}).Install(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.CalledWith("kubectl apply") {
		t.Error("kubectl apply not called")
	}
}

// ── HelmComponent.Verify ─────────────────────────────────────────────────────

func TestVerify_Healthy(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("helm status ingress-nginx", helmStatusJSON("deployed"), nil)
	if err := NewIngressNginx().Verify(context.Background(), testRC(m)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerify_NotDeployed_ReturnsError(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("helm status ingress-nginx", nil, errors.New("not found"))
	if err := NewIngressNginx().Verify(context.Background(), testRC(m)); err == nil {
		t.Fatal("expected error when release is not deployed")
	}
}

func TestVerify_CustomFn(t *testing.T) {
	called := false
	h := &HelmComponent{
		ReleaseName: "test",
		Namespace:   "default",
		clouds:      []string{"aws"},
		verifyFn: func(_ context.Context, _ *RunContext) error {
			called = true
			return nil
		},
	}
	m := runner.NewMock()
	if err := h.Verify(context.Background(), testRC(m)); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("custom verifyFn was not called")
	}
}

// ── EFS StorageClass Verify ───────────────────────────────────────────────────

func TestEFSStorageClass_Verify_Present(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get storageclass was-efs", []byte("was-efs"), nil)
	if err := (&EFSStorageClass{}).Verify(context.Background(), testRC(m)); err != nil {
		t.Fatal(err)
	}
}

func TestEFSStorageClass_Verify_Missing(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get storageclass was-efs", nil, errors.New("not found"))
	if err := (&EFSStorageClass{}).Verify(context.Background(), testRC(m)); err == nil {
		t.Fatal("expected error when StorageClass missing")
	}
}

func TestEFSStorageClass_Namespaces(t *testing.T) {
	if ns := (&EFSStorageClass{}).Namespaces(); ns != nil {
		t.Errorf("expected nil, got %v", ns)
	}
}

// ── Azure StorageClass ────────────────────────────────────────────────────────

func azureFileRC(m *runner.MockRunner) *RunContext {
	rc := testRC(m)
	rc.Cfg.Cloud = "azure"
	rc.Cfg.ClusterName = config.Field[string]{Value: "wasctl"}
	rc.AzureFilesystemAccount = "wasctlfsaccount"
	rc.AzureFilesystemKey = "fakekey=="
	rc.AzureFilesystemRG = "wasctl-rg"
	rc.AzureFilesystemSKU = "Premium_LRS"
	return rc
}

func TestAzureFileStorageClass_Check_Missing(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get storageclass was-azurefile", nil, errors.New("not found"))
	state, err := (&AzureFileStorageClass{}).Check(context.Background(), azureFileRC(m))
	if err != nil {
		t.Fatal(err)
	}
	if state != StateNotInstalled {
		t.Errorf("expected StateNotInstalled, got %d", state)
	}
}

func TestAzureFileStorageClass_Check_Unwired(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get storageclass was-azurefile", []byte(""), nil)
	state, err := (&AzureFileStorageClass{}).Check(context.Background(), azureFileRC(m))
	if err != nil {
		t.Fatal(err)
	}
	if state != StateFailed {
		t.Errorf("expected StateFailed for unwired SC, got %d", state)
	}
}

func TestAzureFileStorageClass_Check_Present(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get storageclass was-azurefile", []byte("wasctlfsaccount"), nil)
	state, err := (&AzureFileStorageClass{}).Check(context.Background(), azureFileRC(m))
	if err != nil {
		t.Fatal(err)
	}
	if state != StateHealthy {
		t.Errorf("expected StateHealthy, got %d", state)
	}
}

func TestAzureFileStorageClass_Install_SkipsWhenPresent(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get storageclass was-azurefile", []byte("wasctlfsaccount"), nil)
	if err := (&AzureFileStorageClass{}).Install(context.Background(), azureFileRC(m)); err != nil {
		t.Fatal(err)
	}
	if m.CalledWith("kubectl apply") {
		t.Error("kubectl apply must not be called when StorageClass exists")
	}
}

func TestAzureFileStorageClass_Install_Applies(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get storageclass was-azurefile", nil, errors.New("not found"))
	m.Register("kubectl apply", nil)
	if err := (&AzureFileStorageClass{}).Install(context.Background(), azureFileRC(m)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.CalledWith("kubectl apply") {
		t.Error("kubectl apply not called")
	}
}

func TestAzureFileStorageClass_Install_RecreatesUnwired(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get storageclass was-azurefile", []byte(""), nil)
	m.Register("kubectl delete", nil)
	m.Register("kubectl apply", nil)
	if err := (&AzureFileStorageClass{}).Install(context.Background(), azureFileRC(m)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.CalledWith("kubectl delete storageclass was-azurefile") {
		t.Error("expected delete of unwired StorageClass")
	}
	if !m.CalledWith("kubectl apply") {
		t.Error("kubectl apply not called")
	}
}

func TestAzureFileStorageClass_Verify_Pass(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get storageclass was-azurefile", []byte("wasctlfsaccount"), nil)
	if err := (&AzureFileStorageClass{}).Verify(context.Background(), azureFileRC(m)); err != nil {
		t.Fatal(err)
	}
}

func TestAzureFileStorageClass_Verify_Fail(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get storageclass was-azurefile", nil, errors.New("not found"))
	if err := (&AzureFileStorageClass{}).Verify(context.Background(), azureFileRC(m)); err == nil {
		t.Fatal("expected error")
	}
}

func TestAzureFileStorageClass_Namespaces(t *testing.T) {
	ns := (&AzureFileStorageClass{}).Namespaces()
	if len(ns) != 1 || ns[0] != "kube-system" {
		t.Errorf("expected [kube-system], got %v", ns)
	}
}

// ── efsCSIDriver ─────────────────────────────────────────────────────────────

func TestEFSCSIDriver_RequiresRoleARN(t *testing.T) {
	m := runner.NewMock()
	rc := testRC(m)
	// EFSCSIRoleARN is empty
	if err := NewEFSCSIDriver().Install(context.Background(), rc); err == nil {
		t.Fatal("expected error when EFSCSIRoleARN is empty")
	}
}

func TestEFSCSIDriver_Check_Delegates(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("helm status aws-efs-csi-driver", helmStatusJSON("deployed"), nil)
	rc := testRC(m)
	rc.EFSCSIRoleARN = "arn:aws:iam::123:role/efs"
	state, err := NewEFSCSIDriver().Check(context.Background(), rc)
	if err != nil {
		t.Fatal(err)
	}
	if state != StateHealthy {
		t.Errorf("expected StateHealthy, got %d", state)
	}
}

func TestEFSCSIDriver_Verify_Delegates(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("helm status aws-efs-csi-driver", helmStatusJSON("deployed"), nil)
	rc := testRC(m)
	rc.EFSCSIRoleARN = "arn:aws:iam::123:role/efs"
	if err := NewEFSCSIDriver().Verify(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
}

func TestEFSCSIDriver_Namespaces(t *testing.T) {
	if ns := NewEFSCSIDriver().Namespaces(); ns != nil {
		t.Errorf("expected nil namespaces, got %v", ns)
	}
}

// ── ebsCSIDriver ─────────────────────────────────────────────────────────────

func TestEBSCSIDriver_RequiresRoleARN(t *testing.T) {
	m := runner.NewMock()
	rc := testRC(m)
	if err := NewEBSCSIDriver().Install(context.Background(), rc); err == nil {
		t.Fatal("expected error when EBSCSIRoleARN is empty")
	}
}

func TestEBSCSIDriver_Install_WithRoleARN(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("helm status aws-ebs-csi-driver", nil, errors.New("not found"))
	m.RegisterOutput("kubectl get", []byte{}, nil)
	m.Register("kubectl apply", nil)
	m.Register("helm repo", nil)
	m.Register("helm upgrade", nil)

	rc := testRC(m)
	rc.EBSCSIRoleARN = "arn:aws:iam::123456789012:role/EBSCSIRole"
	if err := NewEBSCSIDriver().Install(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.CalledWith("helm upgrade --install aws-ebs-csi-driver") {
		t.Errorf("expected helm install; calls: %v", m.Calls)
	}
	if !m.CalledWith("EBSCSIRole") {
		t.Errorf("role ARN not passed to helm; calls: %v", m.Calls)
	}
}

// ── AWSKafkaStorageClass ─────────────────────────────────────────────────────

func TestAWSKafkaStorageClass_AppliesManifest(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get storageclass was-kafka-gp3", nil, errors.New("not found"))
	m.Register("kubectl apply", nil)
	if err := (&AWSKafkaStorageClass{}).Install(context.Background(), testRC(m)); err != nil {
		t.Fatal(err)
	}
	if !m.CalledWith("kubectl apply") {
		t.Error("expected kubectl apply")
	}
}

func TestAWSKafkaStorageClass_SkipsWhenHealthy(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("kubectl get storageclass was-kafka-gp3", []byte("ebs.csi.aws.com"), nil)
	if err := (&AWSKafkaStorageClass{}).Install(context.Background(), testRC(m)); err != nil {
		t.Fatal(err)
	}
	if m.CalledWith("kubectl apply") {
		t.Error("should not re-apply healthy StorageClass")
	}
}

func TestEFSCSIDriver_Install_WithRoleARN(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("helm status aws-efs-csi-driver", nil, errors.New("not found"))
	// Orphan cleanup runs before helm upgrade; return empty (resource absent).
	m.RegisterOutput("kubectl get", []byte{}, nil)
	m.Register("kubectl apply", nil)
	m.Register("helm repo", nil)
	m.Register("helm upgrade", nil)

	rc := testRC(m)
	rc.EFSCSIRoleARN = "arn:aws:iam::123456789012:role/EFSCSIRole"
	if err := NewEFSCSIDriver().Install(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.CalledWith("helm upgrade --install aws-efs-csi-driver") {
		t.Errorf("expected helm install; calls: %v", m.Calls)
	}
	// Role ARN must appear in the --set flag.
	if !m.CalledWith("EFSCSIRole") {
		t.Errorf("role ARN not passed to helm; calls: %v", m.Calls)
	}
}

// ── Installer ─────────────────────────────────────────────────────────────────

func TestInstaller_SkipsList(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("helm status cert-manager", nil, errors.New("not found"))
	m.Register("kubectl apply", nil)
	m.Register("helm repo", nil)
	m.Register("helm upgrade", nil)

	cfg := &config.Config{
		AddonsSkip: config.Field[string]{Value: "cert-manager"},
	}
	rc := testRC(m)
	rc.Cfg = cfg

	ins := NewInstaller(rc)
	if err := ins.InstallAll(context.Background(), []Addon{NewCertManager()}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.CalledWith("helm upgrade --install cert-manager") {
		t.Error("cert-manager should have been skipped")
	}
}

func TestInstaller_StopsOnFirstError(t *testing.T) {
	m := runner.NewMock()
	// ingress-nginx not installed, install fails
	m.RegisterOutput("helm status ingress-nginx", nil, errors.New("not found"))
	// Orphan cleanup runs before helm upgrade; return empty (resource absent).
	m.RegisterOutput("kubectl get", []byte{}, nil)
	m.Register("kubectl apply", nil)
	m.Register("helm repo", nil)
	m.RegisterError("helm upgrade --install ingress-nginx", nil, errors.New("install failed"))
	// strimzi should never be reached
	m.RegisterOutput("helm status strimzi-kafka-operator", nil, errors.New("not found"))

	rc := testRC(m)
	ins := NewInstaller(rc)
	err := ins.InstallAll(context.Background(), []Addon{NewIngressNginx(), NewStrimzi()})
	if err == nil {
		t.Fatal("expected error")
	}
	if m.CalledWith("helm upgrade --install strimzi") {
		t.Error("strimzi should not have been attempted after ingress-nginx failed")
	}
}

// ── Install-path integration: orphan handling with realistic kubectl output ──
//
// These three scenarios must ALL pass before any change to cleanup logic ships.
// They test through the full Install path (Check → cleanupOrphanedResources →
// helm upgrade) using kubectl outputs that match what a real cluster returns.
//
// Scenario 1: fresh cluster — no orphans, kubectl exits 1 with NotFound message.
// This validates the NotFound check at the Install level (not just the unit level).
// The bug was: Install tests used exit-0 empty output, so the exit-1 NotFound
// path could silently break without any test catching it at this level.
func TestInstall_FreshCluster_KubectlNotFoundDoesNotAbort(t *testing.T) {
	notFound := func(kind, name string) []byte {
		return []byte(`Error from server (NotFound): ` + strings.ToLower(kind) + `s.rbac.authorization.k8s.io "` + name + `" not found`)
	}
	_ = notFound

	m := runner.NewMock()
	// helm status: not installed.
	m.RegisterOutput("helm status strimzi-kafka-operator", nil, errors.New("not found"))
	// All orphan checks: kubectl exits 1 with NotFound message (realistic fresh-cluster response).
	m.RegisterOutput("kubectl get",
		[]byte(`Error from server (NotFound): the server could not find the requested resource`),
		errors.New("exit status 1"),
	)
	// Namespace create, repo ops, and the install itself all succeed.
	m.Register("kubectl apply", nil)
	m.Register("helm repo", nil)
	m.Register("helm upgrade --install strimzi-kafka-operator", nil)

	h := NewStrimzi()
	if err := h.Install(context.Background(), testRC(m)); err != nil {
		t.Fatalf("Install must succeed on fresh cluster (all NotFound): %v", err)
	}
	if !m.CalledWith("helm upgrade --install strimzi-kafka-operator") {
		t.Errorf("expected helm upgrade --install to run; calls: %v", m.Calls)
	}
}

// Scenario 2: orphan exists — prior failed install left resources behind.
// One resource exists with NO helm annotation → cleaned up; then install proceeds.
// kubectl get for the first ClusterRoleBinding returns the orphaned resource JSON.
// All other resources return NotFound (exit 1) as on a real partially-cleaned cluster.
func TestInstall_OrphanPresent_CleanedUpThenInstalls(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("helm status strimzi-kafka-operator", nil, errors.New("not found"))
	// Specific orphan: ClusterRoleBinding strimzi-cluster-operator with NO helm
	// annotation and NO managed-by label → orphan from a prior failed install.
	m.RegisterOutput("kubectl get ClusterRoleBinding strimzi-cluster-operator ",
		orphanJSON(nil, nil),
		nil,
	)
	// All other orphan checks: NotFound exit 1.
	m.RegisterOutput("kubectl get",
		[]byte(`Error from server (NotFound): the server could not find the requested resource`),
		errors.New("exit status 1"),
	)
	m.Register("kubectl delete ClusterRoleBinding strimzi-cluster-operator", nil)
	m.Register("kubectl apply", nil)
	m.Register("helm repo", nil)
	m.Register("helm upgrade --install strimzi-kafka-operator", nil)

	h := NewStrimzi()
	if err := h.Install(context.Background(), testRC(m)); err != nil {
		t.Fatalf("Install must clean up orphan and proceed: %v", err)
	}
	if !m.CalledWith("kubectl delete ClusterRoleBinding strimzi-cluster-operator") {
		t.Errorf("expected orphan to be deleted; calls: %v", m.Calls)
	}
	if !m.CalledWith("helm upgrade --install strimzi-kafka-operator") {
		t.Errorf("expected helm upgrade --install after cleanup; calls: %v", m.Calls)
	}
}

// Scenario 3: resource owned by a DIFFERENT release — hard stop, SAFETY VIOLATION.
// Install must return an error and must NOT call helm upgrade --install or kubectl delete.
func TestInstall_DifferentReleaseOwnsResource_SafetyViolation(t *testing.T) {
	m := runner.NewMock()
	m.RegisterOutput("helm status strimzi-kafka-operator", nil, errors.New("not found"))
	// First orphan check: ClusterRoleBinding owned by a different helm release.
	m.RegisterOutput("kubectl get ClusterRoleBinding strimzi-cluster-operator ",
		orphanJSON(map[string]string{
			"meta.helm.sh/release-name":      "some-other-release",
			"meta.helm.sh/release-namespace": "default",
		}, nil),
		nil,
	)
	// Remaining checks would be NotFound, but should never be reached.
	m.RegisterOutput("kubectl get",
		[]byte(`Error from server (NotFound): the server could not find the requested resource`),
		errors.New("exit status 1"),
	)
	m.Register("kubectl apply", nil)
	m.Register("helm repo", nil)

	h := NewStrimzi()
	err := h.Install(context.Background(), testRC(m))
	if err == nil {
		t.Fatal("Install must return SAFETY VIOLATION error; got nil")
	}
	if !strings.Contains(err.Error(), "SAFETY VIOLATION") {
		t.Errorf("expected SAFETY VIOLATION in error; got: %v", err)
	}
	if m.CalledWith("kubectl delete") {
		t.Error("must NOT call kubectl delete on a resource owned by another release")
	}
	if m.CalledWith("helm upgrade --install") {
		t.Error("must NOT proceed to helm upgrade after SAFETY VIOLATION")
	}
}
