package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/addons"
	"github.com/WolframResearch/WAS-Kubernetes/internal/assets"
	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
	"github.com/WolframResearch/WAS-Kubernetes/internal/tools"
	"github.com/WolframResearch/WAS-Kubernetes/internal/workspace"
)

const (
	wasNamespace = "was"
	wasRelease   = "was"
)

// App deploys the WAS Helm chart. All helm and kubectl calls use an isolated
// kubeconfig with an explicit --context so ~/.kube/config is never read.
type App struct{}

func (App) Name() string         { return "app" }
func (App) Description() string  { return "WAS application deployed" }
func (App) EstimateText() string { return "5-10 min" }
func (App) Label() string        { return "Deploying WAS application" }
func (App) DestroyLabel() string { return "Uninstalling WAS application" }
func (App) Substeps() []string {
	return []string{"Open workspace", "Safety check", "Deploy WAS chart"}
}

// Check returns nil when `helm status was -n was` reports a deployed release,
// using an isolated kubeconfig.
func (App) Check(ctx context.Context, cfg *config.Config) error {
	accountID, err := cloudAccountID(ctx, cfg)
	if err != nil {
		return fmt.Errorf("app check: %w", err)
	}
	w, err := workspace.OpenForCloud(ctx, cfg, accountID, cfg.ClusterName.Value)
	if err != nil {
		return fmt.Errorf("app check: %w", err)
	}
	if err := w.MaterializeTempDir(); err != nil {
		return fmt.Errorf("app check: %w", err)
	}
	defer w.Close()

	kubeconfigPath, err := w.Kubeconfig(ctx)
	if err != nil {
		return fmt.Errorf("app check: %w", err)
	}
	contextName := w.ContextName()

	out, err := exec.CommandContext(ctx,
		"helm", "--kubeconfig", kubeconfigPath, "--kube-context", contextName,
		"status", wasRelease, "-n", wasNamespace,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("helm release 'was' not found: %w", err)
	}
	if !strings.Contains(strings.ToLower(string(out)), "deployed") {
		return fmt.Errorf("helm release 'was' exists but is not in 'deployed' status")
	}
	return nil
}

func (App) Apply(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	if cfg.Cloud == "azure" {
		return appApplyAzure(ctx, cfg, r, rep)
	}
	return appApplyAWS(ctx, cfg, r, rep)
}

func appApplyAWS(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	accountID, err := cloudAccountID(ctx, cfg)
	if err != nil {
		return fmt.Errorf("derive account ID: %w", err)
	}

	rep.SubstepStart("Open workspace")
	w, err := workspace.Open(ctx, cfg.MetaRegion.Value, accountID, cfg.ClusterName.Value)
	if err != nil {
		rep.SubstepFail(err)
		return err
	}
	if err := w.CheckCloudMatch(cfg.Cloud); err != nil {
		rep.SubstepFail(err)
		return err
	}
	if err := w.Lock(ctx); err != nil {
		rep.SubstepFail(err)
		return err
	}
	defer w.Unlock(ctx)
	defer w.Close()

	if err := w.MaterializeTempDir(); err != nil {
		rep.SubstepFail(err)
		return err
	}
	kubeconfigPath, err := w.Kubeconfig(ctx)
	if err != nil {
		rep.SubstepFail(err)
		return err
	}
	contextName := w.ContextName()
	rep.SubstepDone()

	rep.SubstepStart("Safety check")
	if !cfg.DryRun {
		if err := workspace.VerifyCluster(ctx, w, kubeconfigPath); err != nil {
			rep.SubstepFail(err)
			return err
		}
		rep.LogLine("[✓] Cluster identity verified")
	}
	rep.SubstepDone()

	if err := resolveIngressHost(ctx, cfg, kubeconfigPath, contextName, rep); err != nil {
		return err
	}

	stackTFDir, err := prepareStackTF(ctx, cfg, w, r, rep)
	if err != nil {
		return err
	}
	resourceBucket, err := requireTFOutput(ctx, stackTFDir, "resource_bucket_name")
	if err != nil {
		return err
	}
	nodefileBucket, err := requireTFOutput(ctx, stackTFDir, "nodefile_bucket_name")
	if err != nil {
		return err
	}
	rmRoleARN, err := requireTFOutput(ctx, stackTFDir, "resource_manager_role_arn")
	if err != nil {
		return err
	}

	mat, err := w.Materialize(ctx, chartAssets(cfg), workspace.Chart)
	if err != nil {
		return fmt.Errorf("materialize chart: %w", err)
	}
	defer func() {
		if persistErr := w.Persist(ctx, mat); persistErr != nil {
			rep.LogLine("[!] persist failed: " + persistErr.Error())
		}
	}()
	kubeconfigPath, err = w.Kubeconfig(ctx)
	if err != nil {
		return err
	}
	contextName = w.ContextName()

	chartTFDir := mat.ChartDir
	valFile := filepath.Join(chartTFDir, "values-aws.yaml")

	if err := ensureKafkaNamespaceReady(ctx, cfg, r, rep, kubeconfigPath, contextName); err != nil {
		return err
	}

	deletePendingLogPVCs(ctx, r, rep, kubeconfigPath, contextName)

	rep.SubstepStart("Deploy WAS chart")
	if err := assertKafkaNamespaceNotTerminating(ctx, r, kubeconfigPath, contextName); err != nil {
		rep.SubstepFail(err)
		return err
	}
	installCmd := tools.HelmUpgradeInstall(wasRelease, chartTFDir, valFile, wasNamespace,
		kubeconfigPath, contextName,
		map[string]string{
			"ingress.host":                          cfg.IngressHost.Value,
			"objectStorage.region":                  cfg.Region.Value,
			"objectStorage.resourceBucket":           resourceBucket,
			"objectStorage.nodefileBucket":           nodefileBucket,
			"resourceManager.serviceAccount.roleArn": rmRoleARN,
		},
	)
	if wantIngressTLS(cfg) {
		installCmd = append(installCmd,
			"--set", "ingress.tls.enabled=true",
			"--set", "ingress.tls.clusterIssuer="+addons.ClusterIssuerName,
		)
	}
	if cfg.KafkaMode.Value != "" {
		installCmd = append(installCmd, "--set", "kafka.mode="+cfg.KafkaMode.Value)
	}
	if cfg.KafkaBootstrapServers.Value != "" {
		installCmd = append(installCmd, "--set", "kafka.bootstrapServers="+cfg.KafkaBootstrapServers.Value)
	}
	if err := runCmd(ctx, cfg, r, rep, installCmd); err != nil {
		rep.SubstepFail(err)
		return err
	}
	rep.SubstepDone()

	ensureClusterARN(cfg, w, accountID)
	w.Meta.IngressHost = cfg.IngressHost.Value
	w.Meta.Status = "active"
	return nil
}

func appApplyAzure(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	info, err := tools.GetAccountInfo(ctx)
	if err != nil {
		return fmt.Errorf("derive subscription ID: %w", err)
	}

	rep.SubstepStart("Open workspace")
	w, err := workspace.OpenAzure(ctx, info.ID, cfg.ClusterName.Value)
	if err != nil {
		rep.SubstepFail(err)
		return err
	}
	if err := w.CheckCloudMatch(cfg.Cloud); err != nil {
		rep.SubstepFail(err)
		return err
	}
	if err := w.Lock(ctx); err != nil {
		rep.SubstepFail(err)
		return err
	}
	defer w.Unlock(ctx)
	defer w.Close()

	if err := w.MaterializeTempDir(); err != nil {
		rep.SubstepFail(err)
		return err
	}
	kubeconfigPath, err := w.Kubeconfig(ctx)
	if err != nil {
		rep.SubstepFail(err)
		return err
	}
	contextName := w.ContextName()
	rep.SubstepDone()

	rep.SubstepStart("Safety check")
	if !cfg.DryRun {
		if err := workspace.VerifyCluster(ctx, w, kubeconfigPath); err != nil {
			rep.SubstepFail(err)
			return err
		}
		rep.LogLine("[✓] Cluster identity verified")
	}
	rep.SubstepDone()

	if err := resolveIngressHost(ctx, cfg, kubeconfigPath, contextName, rep); err != nil {
		return err
	}

	stackTFDir, err := prepareStackTF(ctx, cfg, w, r, rep)
	if err != nil {
		return err
	}
	storageAccountName, err := requireTFOutput(ctx, stackTFDir, "resource_storage_account_name")
	if err != nil {
		return err
	}
	storageAccountKey, err := requireTFOutput(ctx, stackTFDir, "resource_storage_account_key")
	if err != nil {
		return err
	}
	resourceContainer, err := requireTFOutput(ctx, stackTFDir, "resource_container_name")
	if err != nil {
		return err
	}
	nodefileContainer, err := requireTFOutput(ctx, stackTFDir, "nodefile_container_name")
	if err != nil {
		return err
	}

	mat, err := w.Materialize(ctx, chartAssets(cfg), workspace.Chart)
	if err != nil {
		return fmt.Errorf("materialize chart: %w", err)
	}
	defer func() {
		if persistErr := w.Persist(ctx, mat); persistErr != nil {
			rep.LogLine("[!] persist failed: " + persistErr.Error())
		}
	}()
	kubeconfigPath, err = w.Kubeconfig(ctx)
	if err != nil {
		return err
	}
	contextName = w.ContextName()

	chartDir := mat.ChartDir
	valFile := filepath.Join(chartDir, "values-azure.yaml")

	if err := ensureKafkaNamespaceReady(ctx, cfg, r, rep, kubeconfigPath, contextName); err != nil {
		return err
	}

	deletePendingLogPVCs(ctx, r, rep, kubeconfigPath, contextName)

	rep.SubstepStart("Deploy WAS chart")
	if err := assertKafkaNamespaceNotTerminating(ctx, r, kubeconfigPath, contextName); err != nil {
		rep.SubstepFail(err)
		return err
	}
	installCmd := tools.HelmUpgradeInstall(wasRelease, chartDir, valFile, wasNamespace,
		kubeconfigPath, contextName,
		map[string]string{
			"ingress.host":                    cfg.IngressHost.Value,
			"objectStorage.azure.accountName": storageAccountName,
			"objectStorage.resourceBucket":    resourceContainer,
			"objectStorage.nodefileBucket":    nodefileContainer,
			// Static account-key auth: Contributor can listKeys; no Azure
			// roleAssignments/write (Owner/UAA) required for the default path.
			"objectStorage.auth.mode":      "static",
			"objectStorage.auth.secretKey": storageAccountKey,
		},
	)
	if wantIngressTLS(cfg) {
		installCmd = append(installCmd,
			"--set", "ingress.tls.enabled=true",
			"--set", "ingress.tls.clusterIssuer="+addons.ClusterIssuerName,
		)
	}
	if cfg.KafkaMode.Value != "" {
		installCmd = append(installCmd, "--set", "kafka.mode="+cfg.KafkaMode.Value)
	}
	if cfg.KafkaBootstrapServers.Value != "" {
		installCmd = append(installCmd, "--set", "kafka.bootstrapServers="+cfg.KafkaBootstrapServers.Value)
	}
	if err := runCmd(ctx, cfg, r, rep, installCmd); err != nil {
		rep.SubstepFail(err)
		return err
	}
	rep.SubstepDone()

	ensureClusterARN(cfg, w, info.ID)
	w.Meta.IngressHost = cfg.IngressHost.Value
	w.Meta.Status = "active"
	return nil
}

func (App) Destroy(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter) error {
	accountID, err := cloudAccountID(ctx, cfg)
	if err != nil {
		return fmt.Errorf("derive account ID: %w", err)
	}

	w, err := workspace.OpenForCloud(ctx, cfg, accountID, cfg.ClusterName.Value)
	if err != nil {
		return err
	}
	if err := w.MaterializeTempDir(); err != nil {
		return err
	}
	defer w.Close()

	kubeconfigPath, err := w.Kubeconfig(ctx)
	if err != nil {
		return err
	}
	contextName := w.ContextName()

	if cfg.DryRun {
		for _, cmd := range [][]string{
			tools.HelmUninstall(wasRelease, wasNamespace, kubeconfigPath, contextName),
			tools.KubectlDeletePVC(wasNamespace, kubeconfigPath, contextName, "awes-logs", "resources-logs", "endpoint-logs"),
			tools.KubectlDeletePVCByLabel("kafka", kubeconfigPath, contextName, "strimzi.io/cluster=kafka-persistent"),
		} {
			rep.LogLine("-> " + strings.Join(cmd, " "))
		}
		return nil
	}

	_ = r.Run(ctx, tools.HelmUninstall(wasRelease, wasNamespace, kubeconfigPath, contextName), nil, rep)
	_ = r.Run(ctx, tools.KubectlDeletePVC(wasNamespace, kubeconfigPath, contextName, "awes-logs", "resources-logs", "endpoint-logs"), nil, rep)
	_ = r.Run(ctx, tools.KubectlDeletePVCByLabel("kafka", kubeconfigPath, contextName, "strimzi.io/cluster=kafka-persistent"), nil, rep)

	rep.LogLine("[✓] WAS chart uninstalled")

	if cfg.Cloud == "aws" {
		stackTFDir, prepErr := prepareStackTF(ctx, cfg, w, r, rep)
		if prepErr != nil {
			rep.LogLine(fmt.Sprintf("[cleanup] warning: could not init stack for bucket cleanup: %v", prepErr))
			return nil
		}
		resourceBucket, resErr := tools.TerraformOutput(ctx, stackTFDir, "resource_bucket_name")
		nodefileBucket, nodeErr := tools.TerraformOutput(ctx, stackTFDir, "nodefile_bucket_name")

		if resErr == nil && resourceBucket != "" {
			if err := emptyS3Bucket(ctx, cfg.Region.Value, resourceBucket, rep); err != nil {
				rep.LogLine(fmt.Sprintf("[cleanup] warning: could not empty resource bucket %s: %v", resourceBucket, err))
			}
		} else {
			rep.LogLine(fmt.Sprintf("[cleanup] warning: could not retrieve resource bucket name: %v", resErr))
		}

		if nodeErr == nil && nodefileBucket != "" {
			if err := emptyS3Bucket(ctx, cfg.Region.Value, nodefileBucket, rep); err != nil {
				rep.LogLine(fmt.Sprintf("[cleanup] warning: could not empty nodefile bucket %s: %v", nodefileBucket, err))
			}
		} else {
			rep.LogLine(fmt.Sprintf("[cleanup] warning: could not retrieve nodefile bucket name: %v", nodeErr))
		}
	}

	return nil
}

// chartAssets returns the chart asset FS.
func chartAssets(cfg *config.Config) fs.FS {
	if cfg.Local {
		return assets.LocalChartFS(cfg.RepoRoot)
	}
	return assets.ChartFS
}

// wantIngressTLS reports whether the WAS chart should enable ingress TLS
// (Certificate + HTTPS URLs).
//
// Rules:
//   - false if cert-manager is in AddonsSkip
//   - AWS: true only when --ingress-host is a Let's Encrypt–issuable name
//     (custom DNS). Raw *.elb.amazonaws.com names cannot get public certs.
//   - Azure: true whenever cert-manager is installed (*.cloudapp.azure.com works)
func wantIngressTLS(cfg *config.Config) bool {
	for _, s := range strings.Split(cfg.AddonsSkip.Value, ",") {
		if strings.TrimSpace(s) == "cert-manager" {
			return false
		}
	}
	if cfg.Cloud == "aws" {
		return tools.IsACMEIssuableHost(cfg.IngressHost.Value)
	}
	return true
}

// ensureAWSCertManagerSkip appends cert-manager to AddonsSkip on AWS when
// there is no custom ACME-capable ingress host. Let's Encrypt cannot issue for
// ELB hostnames; installing cert-manager alone does not enable HTTPS.
func ensureAWSCertManagerSkip(cfg *config.Config, rep runner.Reporter) {
	if cfg.Cloud != "aws" {
		return
	}
	if tools.IsACMEIssuableHost(cfg.IngressHost.Value) {
		return
	}
	for _, s := range strings.Split(cfg.AddonsSkip.Value, ",") {
		if strings.TrimSpace(s) == "cert-manager" {
			return
		}
	}
	skip := strings.TrimSpace(cfg.AddonsSkip.Value)
	if skip == "" {
		skip = "cert-manager"
	} else {
		skip = skip + ",cert-manager"
	}
	cfg.AddonsSkip = config.Field[string]{Value: skip, Source: "auto:aws-no-custom-domain"}
	rep.LogLine("[addons] AWS: skipping cert-manager — Let's Encrypt needs a custom DNS name (--ingress-host was.example.com).")
	rep.LogLine("[addons]     Default ELB hostnames (*.elb.amazonaws.com) cannot get a public TLS certificate.")
}

// WantIngressTLSForTest and EnsureAWSCertManagerSkipForTest removed — tests use package stages.

// ensureKafkaNamespaceReady waits out a Terminating kafka namespace (common after
// helm uninstall) and adopts an Active one for Helm. Creating Strimzi CRs while
// the namespace is Terminating fails with "unable to create new content".
func ensureKafkaNamespaceReady(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter, kubeconfigPath, contextName string) error {
	if cfg.KafkaMode.Value == "external" {
		return nil
	}

	terminating, err := kafkaNamespaceTerminating(ctx, r, kubeconfigPath, contextName)
	if err != nil {
		// Namespace missing — Helm/chart will create it.
		return nil
	}
	if !terminating {
		labelKafkaNamespace(ctx, cfg, r, rep, kubeconfigPath, contextName)
		return assertKafkaNamespaceNotTerminating(ctx, r, kubeconfigPath, contextName)
	}

	rep.LogLine("[info] namespace 'kafka' is Terminating (leftover from a previous uninstall)")
	rep.LogLine("[info] clearing Strimzi finalizers (KafkaTopics often stick with strimzi.io/topic-operator)...")
	_ = clearKafkaNamespaceBlockers(ctx, r, rep, kubeconfigPath, contextName)

	if waitKafkaNamespaceGone(ctx, r, rep, kubeconfigPath, contextName, 2*time.Minute) {
		rep.LogLine("[✓] namespace 'kafka' deleted — Helm can recreate it")
		return nil
	}

	// Second pass — topics sometimes reappear briefly.
	rep.LogLine("[info] still Terminating — clearing finalizers again...")
	_ = clearKafkaNamespaceBlockers(ctx, r, rep, kubeconfigPath, contextName)
	if waitKafkaNamespaceGone(ctx, r, rep, kubeconfigPath, contextName, 90*time.Second) {
		rep.LogLine("[✓] namespace 'kafka' deleted after finalizer cleanup")
		return nil
	}

	return fmt.Errorf(
		"namespace \"kafka\" is stuck Terminating — Helm cannot install Strimzi resources\n"+
			"  Remaining content is usually KafkaTopics with finalizer strimzi.io/topic-operator:\n"+
			"    kubectl -n kafka get kafkatopic\n"+
			"    kubectl -n kafka get kafkatopic -o name | while read r; do\n"+
			"      kubectl -n kafka patch \"$r\" --type merge -p '{\"metadata\":{\"finalizers\":[]}}'\n"+
			"    done\n"+
			"    kubectl -n kafka delete kafkatopic --all --force --grace-period=0 --wait=false\n"+
			"    kubectl get ns kafka -o json | jq '.spec.finalizers=[]' | kubectl replace --raw /api/v1/namespaces/kafka/finalize -f -\n"+
			"  Re-run: wasctl install app",
	)
}

// assertKafkaNamespaceNotTerminating is a last gate immediately before Helm.
func assertKafkaNamespaceNotTerminating(ctx context.Context, r runner.Runner, kubeconfigPath, contextName string) error {
	terminating, err := kafkaNamespaceTerminating(ctx, r, kubeconfigPath, contextName)
	if err != nil {
		return nil // gone
	}
	if terminating {
		return fmt.Errorf(
			"namespace \"kafka\" is still Terminating — refusing to run Helm\n"+
				"  Clear KafkaTopic finalizers (see wasctl docs / previous error), then retry",
		)
	}
	return nil
}

// kafkaNamespaceTerminating reports whether the kafka namespace exists and is
// deleting. Uses deletionTimestamp as well as phase — phase alone can be empty.
func kafkaNamespaceTerminating(ctx context.Context, r runner.Runner, kubeconfigPath, contextName string) (bool, error) {
	out, err := r.Output(ctx, []string{
		"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
		"get", "namespace", "kafka",
		"-o", "jsonpath={.status.phase}{'|'}{.metadata.deletionTimestamp}",
	}, nil)
	if err != nil {
		return false, err
	}
	s := strings.TrimSpace(string(out))
	parts := strings.SplitN(s, "|", 2)
	phase := parts[0]
	deleted := len(parts) > 1 && parts[1] != "" && parts[1] != "<nil>"
	return phase == "Terminating" || deleted, nil
}

func waitKafkaNamespaceGone(ctx context.Context, r runner.Runner, rep runner.Reporter, kubeconfigPath, contextName string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		_, err := r.Output(ctx, []string{
			"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
			"get", "namespace", "kafka", "-o", "name",
		}, nil)
		if err != nil {
			return true // get failed → gone
		}
		rep.LogLine("  … still Terminating")
		time.Sleep(5 * time.Second)
	}
	return false
}

func clearKafkaNamespaceBlockers(ctx context.Context, r runner.Runner, rep runner.Reporter, kubeconfigPath, contextName string) error {
	// Strip finalizers from Strimzi CRs that keep the namespace alive.
	// KafkaTopics often stick with finalizer strimzi.io/topic-operator after
	// helm uninstall (topic operator already gone).
	for _, res := range []string{
		"kafkatopics.kafka.strimzi.io",
		"kafkas.kafka.strimzi.io",
		"kafkanodepools.kafka.strimzi.io",
		"kafkausers.kafka.strimzi.io",
		"kafkabridges.kafka.strimzi.io",
		"kafkatopic", "kafka", "kafkanodepool", "kafkauser", "kafkabridge",
	} {
		cmd := []string{
			"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
			"get", res, "-n", "kafka", "-o", "name",
		}
		out, err := r.Output(ctx, cmd, nil)
		if err != nil || strings.TrimSpace(string(out)) == "" {
			continue
		}
		for _, name := range strings.Fields(string(out)) {
			// Empty array clears finalizers more reliably than null for Strimzi.
			patch := []string{
				"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
				"patch", name, "-n", "kafka",
				"--type", "merge", "-p", `{"metadata":{"finalizers":[]}}`,
			}
			if err := r.Run(ctx, patch, nil, rep); err != nil {
				rep.LogLine(fmt.Sprintf("[!] warning: could not clear finalizers on %s: %v", name, err))
			}
			_ = r.Run(ctx, []string{
				"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
				"delete", name, "-n", "kafka", "--wait=false", "--ignore-not-found",
				"--force", "--grace-period=0",
			}, nil, rep)
		}
	}

	// Force-finalize the namespace itself (no jq dependency — Go JSON edit).
	raw, err := r.Output(ctx, []string{
		"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
		"get", "namespace", "kafka", "-o", "json",
	}, nil)
	if err != nil {
		return err
	}
	var ns map[string]any
	if err := json.Unmarshal(raw, &ns); err != nil {
		return fmt.Errorf("parse namespace json: %w", err)
	}
	if spec, ok := ns["spec"].(map[string]any); ok {
		spec["finalizers"] = []any{}
	}
	cleared, err := json.Marshal(ns)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "wasctl-kafka-ns-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(cleared); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	return r.Run(ctx, []string{
		"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
		"replace", "--raw", "/api/v1/namespaces/kafka/finalize", "-f", tmpPath,
	}, nil, rep)
}

func labelKafkaNamespace(ctx context.Context, cfg *config.Config, r runner.Runner, rep runner.Reporter, kubeconfigPath, contextName string) {
	if cfg.KafkaMode.Value == "external" {
		return
	}

	checkCmd := []string{
		"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
		"get", "namespace", "kafka",
	}
	if _, err := r.Output(ctx, checkCmd, nil); err != nil {
		return
	}

	rep.LogLine("[info] preparing 'kafka' namespace for Helm adoption...")

	cmd1 := []string{
		"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
		"label", "namespace", "kafka", "app.kubernetes.io/managed-by=Helm", "--overwrite",
	}
	if err := r.Run(ctx, cmd1, nil, rep); err != nil {
		rep.LogLine(fmt.Sprintf("[!] warning: failed to label namespace 'kafka': %v", err))
	}

	cmd2 := []string{
		"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
		"annotate", "namespace", "kafka",
		"meta.helm.sh/release-name=" + wasRelease,
		"meta.helm.sh/release-namespace=" + wasNamespace,
		"--overwrite",
	}
	if err := r.Run(ctx, cmd2, nil, rep); err != nil {
		rep.LogLine(fmt.Sprintf("[!] warning: failed to annotate namespace 'kafka': %v", err))
	}
}

// wasLogPVCs are the chart's three RWX log claims. Helm cannot change
// storage size on an existing PVC (even Pending ones on many clusters), so
// upgrades that bump storage.logsSize (e.g. Azure Premium ≥100Gi) fail unless
// Pending claims are removed first and recreated by the chart.
var wasLogPVCs = []string{"awes-logs", "resources-logs", "endpoint-logs"}

// deletePendingLogPVCs removes unbound WAS log PVCs so helm upgrade can
// recreate them at the size in values. Bound PVCs are left alone (data).
func deletePendingLogPVCs(ctx context.Context, r runner.Runner, rep runner.Reporter, kubeconfigPath, contextName string) {
	var pending []string
	for _, name := range wasLogPVCs {
		out, err := r.Output(ctx, []string{
			"kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName,
			"get", "pvc", name, "-n", wasNamespace,
			"-o", "jsonpath={.status.phase}",
		}, nil)
		if err != nil {
			continue // missing is fine — helm will create
		}
		phase := strings.TrimSpace(string(out))
		if phase == "Pending" || phase == "" {
			pending = append(pending, name)
		}
	}
	if len(pending) == 0 {
		return
	}
	rep.LogLine(fmt.Sprintf(
		"[info] deleting Pending log PVC(s) %v so helm can recreate them (storage size is immutable on existing claims)",
		pending,
	))
	cmd := tools.KubectlDeletePVC(wasNamespace, kubeconfigPath, contextName, pending...)
	if err := r.Run(ctx, cmd, nil, rep); err != nil {
		rep.LogLine(fmt.Sprintf("[!] warning: failed to delete Pending log PVCs: %v", err))
	}
}

// resolveIngressHost ensures cfg.IngressHost is a DNS name (never an IP).
// Kubernetes Ingress spec.rules[].host rejects IP addresses.
//
// When TLS/Let's Encrypt is enabled (cert-manager not skipped), the host must
// also be ACME-issuable — raw AWS ELB / Azure cloudapp names are rejected by
// Let's Encrypt policy (urn:ietf:params:acme:error:rejectedIdentifier).
func resolveIngressHost(ctx context.Context, cfg *config.Config, kubeconfigPath, contextName string, rep runner.Reporter) error {
	tls := wantIngressTLS(cfg)

	if cfg.IngressHost.Value != "" {
		if !tools.IsValidIngressHost(cfg.IngressHost.Value) {
			return fmt.Errorf(
				"--ingress-host %q is not a valid DNS name\n"+
					"Kubernetes Ingress host must be a DNS name, not an IP address.\n"+
					"  • Azure: re-run addons (sets azure-dns-label-name → *.cloudapp.azure.com), or\n"+
					"  • Pass --ingress-host <dns-name> (e.g. was.example.com)",
				cfg.IngressHost.Value,
			)
		}
		if tls && !tools.IsACMEIssuableHost(cfg.IngressHost.Value) {
			return acmeHostError(cfg.IngressHost.Value)
		}
		return nil
	}

	rep.LogLine("ingress-host not set — auto-detecting DNS hostname from ingress-nginx...")
	loc := ""
	if cfg.Cloud == "azure" {
		loc = cfg.AzureLocation.Value
	}
	host, err := tools.GetIngressNginxHostname(ctx, kubeconfigPath, contextName, loc)
	if err != nil {
		return fmt.Errorf(
			"cannot auto-detect ingress host: %w\n  Wait for the LoadBalancer, re-run addons on Azure, or pass --ingress-host <dns-name>",
			err,
		)
	}
	if !tools.IsValidIngressHost(host) {
		return fmt.Errorf("auto-detected ingress address %q is not a DNS name — pass --ingress-host <dns-name>", host)
	}
		if tls && !tools.IsACMEIssuableHost(host) {
		return fmt.Errorf(
			"auto-detected ingress host %q cannot get a Let's Encrypt certificate\n"+
				"ACME CAs refuse AWS load-balancer names (*.elb.amazonaws.com / *.amazonaws.com).\n"+
				"  1. Create a DNS record you control (e.g. was.example.com) → CNAME to the ELB hostname\n"+
				"  2. Re-run app with: --ingress-host was.example.com\n"+
				"Or skip TLS: --skip cert-manager (HTTP only)",
			host,
		)
	}
	rep.LogLine(fmt.Sprintf("[✓] Detected ingress host: %s", host))
	cfg.IngressHost = config.Field[string]{Value: host, Source: "auto-detected:kubectl"}
	return nil
}

func acmeHostError(host string) error {
	return fmt.Errorf(
		"--ingress-host %q cannot get a Let's Encrypt certificate\n"+
			"ACME CAs refuse AWS load-balancer names (*.elb.amazonaws.com / *.amazonaws.com).\n"+
			"  1. Point a DNS name you control at the ingress LoadBalancer (CNAME to the ELB)\n"+
			"  2. Pass --ingress-host <that-dns-name>\n"+
			"Or skip TLS: --skip cert-manager (HTTP only)",
		host,
	)
}
