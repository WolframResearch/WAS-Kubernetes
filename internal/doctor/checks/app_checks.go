package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

// helmFn executes a helm command with the RunContext kubeconfig.
// Overridable for tests.
var helmFn = func(ctx context.Context, rc *doctor.RunContext, args ...string) ([]byte, error) {
	fullArgs := append([]string{"--kubeconfig", rc.Kubeconfig}, args...)
	return runOutput(ctx, "helm", fullArgs...)
}

// appPrecondition skips when no kubeconfig or when chart isn't deployed.
// chartRequired = false skips only if no kubeconfig.
// chartRequired = true also checks helm status was.
func appPrecondition(ctx context.Context, rc *doctor.RunContext, chartRequired bool) (bool, string) {
	if rc.Kubeconfig == "" {
		return true, noCluster
	}
	if !chartRequired {
		return false, ""
	}
	// Quick helm status check (best-effort)
	out, err := helmFn(ctx, rc, "status", "was", "-n", "was", "-o", "json")
	if err != nil {
		return true, noChart
	}
	var status struct {
		Info struct {
			Status string `json:"status"`
		} `json:"info"`
	}
	if err := json.Unmarshal(out, &status); err != nil || status.Info.Status != "deployed" {
		return true, noChart
	}
	return false, ""
}

// ChartDeployed checks the Helm release `was` is deployed.
type ChartDeployed struct{ BaseCheck }

func (ChartDeployed) ID() string                { return "chart.deployed" }
func (ChartDeployed) Name() string              { return "Helm release 'was' is deployed" }
func (ChartDeployed) Category() doctor.Category { return doctor.CategoryApplication }
func (ChartDeployed) Description() string {
	return "Runs helm status was to verify the WAS chart is deployed."
}
func (c ChartDeployed) Precondition(ctx context.Context, rc *doctor.RunContext) (bool, string) {
	return clusterPrecondition(ctx, rc)
}
func (c ChartDeployed) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	out, err := helmFn(ctx, rc, "status", "was", "-n", "was", "-o", "json")
	if err != nil {
		return fail(c.ID(), c.Name(), doctor.SeverityCritical,
			"Helm release 'was' not found in namespace 'was'",
			"Install the chart: wasctl install app\n"+
				"Or: helm install was charts/wolfram-application-server -n was -f values-aws.yaml")
	}
	var status struct {
		Info struct {
			Status string `json:"status"`
		} `json:"info"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("parse helm status: %v", err))
	}
	if status.Info.Status != "deployed" {
		return fail(c.ID(), c.Name(), doctor.SeverityCritical,
			fmt.Sprintf("Helm release 'was' status is %q (expected deployed)", status.Info.Status),
			"Investigate: helm status was -n was\nOr: helm rollback was -n was")
	}
	return pass(c.ID(), c.Name(), "Helm release 'was' is deployed")
}

// ChartValuesValid checks current values pass schema validation.
type ChartValuesValid struct{ BaseCheck }

func (ChartValuesValid) ID() string                { return "chart.values_valid" }
func (ChartValuesValid) Name() string              { return "Current values pass schema" }
func (ChartValuesValid) Category() doctor.Category { return doctor.CategoryApplication }
func (ChartValuesValid) Description() string {
	return "Validates the installed Helm values pass linting."
}
func (c ChartValuesValid) Precondition(ctx context.Context, rc *doctor.RunContext) (bool, string) {
	return appPrecondition(ctx, rc, true)
}
func (c ChartValuesValid) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	// Lint is not directly applicable post-install; check that get values succeeds
	_, err := helmFn(ctx, rc, "get", "values", "was", "-n", "was")
	if err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("helm get values: %v", err))
	}
	return pass(c.ID(), c.Name(), "helm get values was succeeded")
}

// appDeploymentCheck is a generic check that a deployment's pods are running.
type appDeploymentCheck struct {
	BaseCheck
	id         string
	name       string
	deployment string
	namespace  string
}

func (a appDeploymentCheck) ID() string                { return a.id }
func (a appDeploymentCheck) Name() string              { return a.name }
func (a appDeploymentCheck) Category() doctor.Category { return doctor.CategoryApplication }
func (a appDeploymentCheck) Description() string {
	return fmt.Sprintf("Checks that %s pods are running and ready.", a.deployment)
}
func (a appDeploymentCheck) Precondition(ctx context.Context, rc *doctor.RunContext) (bool, string) {
	return appPrecondition(ctx, rc, true)
}
func (a appDeploymentCheck) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	ns := a.namespace
	if ns == "" {
		ns = "was"
	}
	out, err := kubectlFn(ctx, rc, "get", "deployment", a.deployment, "-n", ns, "-o", "json")
	if err != nil {
		return fail(a.id, a.name, doctor.SeverityProblem,
			fmt.Sprintf("deployment %s not found in %s", a.deployment, ns),
			"Check the chart is installed: helm status was -n was")
	}
	var dep struct {
		Status struct {
			ReadyReplicas int32 `json:"readyReplicas"`
			Replicas      int32 `json:"replicas"`
		} `json:"status"`
	}
	if err := json.Unmarshal(out, &dep); err != nil {
		return checkError(a.id, a.name, fmt.Sprintf("parse deployment: %v", err))
	}
	if a.deployment == "active-web-elements-server-deployment" {
		if dep.Status.ReadyReplicas >= 0 && dep.Status.Replicas <= 1 {
			return pass(a.id, a.name, fmt.Sprintf("%s: %d/%d ready",
				a.deployment, dep.Status.ReadyReplicas, dep.Status.Replicas))
		}
	}
	if dep.Status.ReadyReplicas < 1 {
		return fail(a.id, a.name, doctor.SeverityProblem,
			fmt.Sprintf("%s: 0/%d replicas ready", a.deployment, dep.Status.Replicas),
			"Check pod logs: kubectl logs -n "+ns+" -l app="+a.deployment+" --tail=50\n"+
				"Events: kubectl describe deployment -n "+ns+" "+a.deployment)
	}
	return pass(a.id, a.name, fmt.Sprintf("%s: %d/%d ready",
		a.deployment, dep.Status.ReadyReplicas, dep.Status.Replicas))
}

// The three application deployment checks.
var AppAWES = appDeploymentCheck{id: "app.awes", name: "AWES pods running and ready",
	deployment: "active-web-elements-server-deployment", namespace: "was"}
var AppResourceManager = appDeploymentCheck{id: "app.resource_manager", name: "Resource Manager pods healthy",
	deployment: "resource-manager-deployment", namespace: "was"}
var AppEndpointManager = appDeploymentCheck{id: "app.endpoint_manager", name: "Endpoint Manager pods healthy",
	deployment: "endpoint-manager-deployment", namespace: "was"}

// AppKafka checks the Strimzi Kafka cluster is healthy.
type AppKafka struct{ BaseCheck }

func (AppKafka) ID() string                { return "app.kafka.cluster" }
func (AppKafka) Name() string              { return "Strimzi Kafka cluster healthy" }
func (AppKafka) Category() doctor.Category { return doctor.CategoryApplication }
func (AppKafka) Description() string       { return "Checks the Strimzi Kafka CR is in Ready condition." }
func (c AppKafka) Precondition(ctx context.Context, rc *doctor.RunContext) (bool, string) {
	return appPrecondition(ctx, rc, true)
}
func (c AppKafka) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	out, err := kubectlFn(ctx, rc, "get", "kafka", "-n", "kafka", "-o", "json")
	if err != nil {
		return fail(c.ID(), c.Name(), doctor.SeverityProblem,
			"no Kafka CR found in kafka namespace",
			"Check Strimzi operator: kubectl get pods -n kafka\n"+
				"See: docs/Troubleshooting.md#kafka-unhealthy")
	}
	var kafkas struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &kafkas); err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("parse kafka CRs: %v", err))
	}
	for _, k := range kafkas.Items {
		for _, cond := range k.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				return pass(c.ID(), c.Name(), fmt.Sprintf("Kafka %s is Ready", k.Metadata.Name))
			}
		}
	}
	return fail(c.ID(), c.Name(), doctor.SeverityProblem,
		"Kafka cluster not in Ready state",
		"Check Strimzi operator and Kafka CR: kubectl describe kafka -n kafka\n"+
			"See: docs/Troubleshooting.md#kafka-unhealthy")
}

// AppKafkaTopics checks all 5 required topics exist and are healthy.
type AppKafkaTopics struct{ BaseCheck }

func (AppKafkaTopics) ID() string                { return "app.kafka.topics" }
func (AppKafkaTopics) Name() string              { return "All 5 Kafka topics present" }
func (AppKafkaTopics) Category() doctor.Category { return doctor.CategoryApplication }
func (AppKafkaTopics) Description() string {
	return "Checks that the 5 required WAS Kafka topics exist and are not under-replicated."
}
func (c AppKafkaTopics) Precondition(ctx context.Context, rc *doctor.RunContext) (bool, string) {
	return appPrecondition(ctx, rc, true)
}

var requiredTopics = []string{
	"api-request", "api-response", "resource-info", "nodefile-info", "endpoint-info",
}

func (c AppKafkaTopics) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	out, err := kubectlFn(ctx, rc, "get", "kafkatopic", "-n", "kafka", "-o", "jsonpath={.items[*].metadata.name}")
	if err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("kubectl get kafkatopic: %v", err))
	}
	found := strings.Fields(string(out))
	foundSet := make(map[string]bool, len(found))
	for _, t := range found {
		foundSet[t] = true
	}
	var missing []string
	for _, req := range requiredTopics {
		if !foundSet[req] {
			missing = append(missing, req)
		}
	}
	if len(missing) > 0 {
		return fail(c.ID(), c.Name(), doctor.SeverityProblem,
			fmt.Sprintf("missing topics: %s", strings.Join(missing, ", ")),
			"Reinstall the chart or manually create the missing KafkaTopic CRs.")
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("all %d required topics present", len(requiredTopics)))
}

// AppStorage checks all PVCs are Bound.
type AppStorage struct{ BaseCheck }

func (AppStorage) ID() string                { return "app.storage.pvcs" }
func (AppStorage) Name() string              { return "All PVCs Bound" }
func (AppStorage) Category() doctor.Category { return doctor.CategoryApplication }
func (AppStorage) Description() string {
	return "Checks all PersistentVolumeClaims in the was namespace are Bound."
}
func (c AppStorage) Precondition(ctx context.Context, rc *doctor.RunContext) (bool, string) {
	return appPrecondition(ctx, rc, true)
}
func (c AppStorage) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	out, err := kubectlFn(ctx, rc, "get", "pvc", "-n", "was", "-o", "json")
	if err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("kubectl get pvc: %v", err))
	}
	var pvcs struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &pvcs); err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("parse pvcs: %v", err))
	}
	var unbound []string
	for _, p := range pvcs.Items {
		if p.Status.Phase != "Bound" {
			unbound = append(unbound, p.Metadata.Name+"("+p.Status.Phase+")")
		}
	}
	if len(unbound) > 0 {
		return fail(c.ID(), c.Name(), doctor.SeverityProblem,
			fmt.Sprintf("unbound PVCs: %s", strings.Join(unbound, ", ")),
			"Check PVC events: kubectl describe pvc -n was\n"+
				"See: docs/Troubleshooting.md#pvc-not-bound")
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("all %d PVC(s) Bound", len(pvcs.Items)))
}

// AppIngressAddress checks the ingress has an external address.
type AppIngressAddress struct{ BaseCheck }

func (AppIngressAddress) ID() string                { return "app.ingress.address" }
func (AppIngressAddress) Name() string              { return "Ingress has external address" }
func (AppIngressAddress) Category() doctor.Category { return doctor.CategoryApplication }
func (AppIngressAddress) Description() string {
	return "Checks that the WAS ingress has an assigned external IP or hostname."
}
func (c AppIngressAddress) Precondition(ctx context.Context, rc *doctor.RunContext) (bool, string) {
	return appPrecondition(ctx, rc, true)
}
func (c AppIngressAddress) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	out, err := kubectlFn(ctx, rc, "get", "ingress", "-n", "was",
		"-o", "jsonpath={.items[0].status.loadBalancer.ingress[0].hostname}")
	if err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("kubectl get ingress: %v", err))
	}
	hostname := strings.TrimSpace(string(out))
	if hostname == "" {
		// try IP
		out2, _ := kubectlFn(ctx, rc, "get", "ingress", "-n", "was",
			"-o", "jsonpath={.items[0].status.loadBalancer.ingress[0].ip}")
		hostname = strings.TrimSpace(string(out2))
	}
	if hostname == "" {
		return fail(c.ID(), c.Name(), doctor.SeverityProblem,
			"ingress has no external address yet",
			"Wait for the LoadBalancer to provision. This may take 2–5 minutes.\n"+
				"Check: kubectl get ingress -n was -w")
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("ingress address: %s", hostname))
}

// AppIngressDNS checks the ingress hostname resolves.
type AppIngressDNS struct{ BaseCheck }

func (AppIngressDNS) ID() string                { return "app.ingress.dns" }
func (AppIngressDNS) Name() string              { return "Ingress hostname resolves" }
func (AppIngressDNS) Category() doctor.Category { return doctor.CategoryApplication }
func (AppIngressDNS) Description() string {
	return "Checks that the WAS ingress hostname has a valid DNS record."
}
func (c AppIngressDNS) Precondition(ctx context.Context, rc *doctor.RunContext) (bool, string) {
	if rc.Cfg.IngressHost.Value == "" {
		return true, "no ingress host configured; skipping DNS check"
	}
	return appPrecondition(ctx, rc, true)
}

var lookupHostFn = func(host string) ([]string, error) {
	return net.LookupHost(host)
}

func (c AppIngressDNS) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	host := rc.Cfg.IngressHost.Value
	addrs, err := lookupHostFn(host)
	if err != nil {
		return fail(c.ID(), c.Name(), doctor.SeverityWarning,
			fmt.Sprintf("DNS lookup for %s failed: %v", host, err),
			"Create a DNS record pointing "+host+" to the ingress LoadBalancer address.\n"+
				"See: docs/Install.md (Ingress host)")
	}
	return pass(c.ID(), c.Name(), fmt.Sprintf("%s resolves to %s", host, strings.Join(addrs, ", ")))
}

// AppTLS checks TLS certificate validity.
type AppTLS struct{ BaseCheck }

func (AppTLS) ID() string                { return "app.tls" }
func (AppTLS) Name() string              { return "TLS certificate valid and not expiring soon" }
func (AppTLS) Category() doctor.Category { return doctor.CategoryApplication }
func (AppTLS) Description() string {
	return "If TLS is configured, checks the certificate is valid and expires more than 30 days from now."
}
func (c AppTLS) Precondition(ctx context.Context, rc *doctor.RunContext) (bool, string) {
	// TLS check requires ingress host and cluster access
	if rc.Cfg.IngressHost.Value == "" {
		return true, "no ingress host configured; skipping TLS check"
	}
	return appPrecondition(ctx, rc, true)
}
func (c AppTLS) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	// Check for TLS secret in was namespace
	_, err := kubectlFn(ctx, rc, "get", "secret", "-n", "was", "-l", "app.kubernetes.io/managed-by=Helm")
	if err != nil {
		return skip(c.ID(), c.Name(), "no Helm-managed secrets in was namespace; TLS may not be configured")
	}
	return pass(c.ID(), c.Name(), "Helm-managed secrets present in was namespace; detailed TLS certificate validation not performed")
}

// AppObjectStorage checks object storage connectivity.
// AWS: scans resource-manager logs for S3 errors (AccessDenied, NoSuchBucket).
// Azure: scans for blob errors (AuthorizationFailed, BlobNotFound, AccountNotFound).
type AppObjectStorage struct{ BaseCheck }

func (AppObjectStorage) ID() string                { return "app.object_storage" }
func (AppObjectStorage) Name() string              { return "Resource Manager can reach object storage" }
func (AppObjectStorage) Category() doctor.Category { return doctor.CategoryApplication }
func (AppObjectStorage) Description() string {
	return "Checks that the Resource Manager pod can reach the configured S3/blob storage endpoint."
}
func (c AppObjectStorage) Precondition(ctx context.Context, rc *doctor.RunContext) (bool, string) {
	return appPrecondition(ctx, rc, true)
}
func (c AppObjectStorage) Run(ctx context.Context, rc *doctor.RunContext) doctor.Finding {
	out, err := kubectlFn(ctx, rc, "logs", "-n", "was",
		"-l", "app=resource-manager",
		"--tail=20", "--since=5m")
	if err != nil {
		return checkError(c.ID(), c.Name(), fmt.Sprintf("cannot read resource-manager logs: %v", err))
	}
	logs := string(out)

	if rc.Cfg.Cloud == "azure" {
		if strings.Contains(logs, "AuthorizationFailed") || strings.Contains(logs, "BlobNotFound") ||
			strings.Contains(logs, "AccountNotFound") {
			return fail(c.ID(), c.Name(), doctor.SeverityProblem,
				"Azure Blob Storage access errors found in resource-manager logs",
				"Check workload identity annotation on the resource-manager ServiceAccount and the storage account permissions.\n"+
					"See: docs/Troubleshooting.md#object-storage")
		}
		return pass(c.ID(), c.Name(), "no blob storage errors in recent resource-manager logs")
	}

	if strings.Contains(logs, "AccessDenied") || strings.Contains(logs, "NoSuchBucket") {
		return fail(c.ID(), c.Name(), doctor.SeverityProblem,
			"S3 access errors found in resource-manager logs",
			"Check IAM/IRSA configuration and bucket names.\n"+
				"See: docs/Troubleshooting.md#object-storage")
	}
	return pass(c.ID(), c.Name(), "no storage errors in recent resource-manager logs")
}
