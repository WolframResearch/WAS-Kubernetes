# Wolfram Application Server — Helm Chart

Deploys AWES, Resource Manager, Endpoint Manager, PVCs, Ingress, HPAs, and Strimzi Kafka CRs on EKS or AKS.

**Recommended:** use `wasctl install` / `wasctl serve`. They provision infrastructure and cluster add-ons, then install this chart. Use Helm directly only when the cluster already meets the prerequisites below (for example chart-only installs on an existing cluster).

See also [docs/Install.md](../../docs/Install.md) and the [root README](../../README.md).

---

## Prerequisites

**Helm ≥ 3.12** (`helm version --short`).

| # | Prerequisite | AWS | Azure | Provided by |
|---|--------------|-----|-------|-------------|
| 1 | Shared filesystem (EFS / Azure Files) | ✓ | ✓ | Terraform / wasctl infra |
| 2 | CSI driver + StorageClass (logs) | ✓ EFS CSI + `was-efs` | ✓ `was-azurefile` (CSI often built-in) | wasctl addons or manual install |
| 2b | Block CSI + StorageClass (Kafka) | ✓ EBS CSI + `was-kafka-gp3` | ✓ `kafka-standardssd-xfs` (disk CSI) | wasctl addons or manual install |
| 3 | ingress-nginx | ✓ | ✓ | wasctl addons or manual install |
| 4 | metrics-server | ✓ | ✓ | wasctl addons (AKS may already have it) |
| 5 | Strimzi operator ≥ 0.40 | ✓ | ✓ | wasctl addons or manual install |
| 6 | Prometheus + prometheus-adapter | ✓ | ✓ | wasctl addons or manual install |
| 7 | cert-manager | optional | optional | wasctl addons or manual install |
| 8 | Object storage (S3 / Blob containers) | ✓ | ✓ | Terraform / wasctl infra |
| 9 | Object-storage auth for Resource Manager | ✓ IRSA (default) | ✓ static account key (default); Workload Identity optional | Terraform / wasctl infra |

After ingress-nginx is ready, set `ingress.host` to a DNS hostname (not a raw IP on Azure):

```bash
# AWS — LoadBalancer hostname
kubectl get svc -n ingress-nginx ingress-nginx-controller \
  -o jsonpath='{.status.loadBalancer.ingress[0].hostname}{"\n"}'

# Azure — map a DNS name to the LoadBalancer IP (for example *.cloudapp.azure.com)
kubectl get svc -n ingress-nginx ingress-nginx-controller \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}{"\n"}'
```

Install add-ons and the app with wasctl when the cluster was created by wasctl:

```bash
./wasctl install addons
./wasctl install app
```

Optional chart subchart dependencies (ingress-nginx, Strimzi, and so on) can be enabled with `helm dependency update` and the toggles below — only when those components are **not** already installed in the cluster.

---

## Install with Helm

### AWS (IRSA)

```bash
helm upgrade --install was charts/wolfram-application-server \
  -f charts/wolfram-application-server/values-aws.yaml \
  --namespace was --create-namespace \
  --set ingress.host=was.example.com \
  --set objectStorage.region=us-east-1 \
  --set objectStorage.resourceBucket=my-resourceinfo-bucket \
  --set objectStorage.nodefileBucket=my-nodefiles-bucket \
  --set resourceManager.serviceAccount.roleArn=arn:aws:iam::123456789:role/was-resource-manager
```

### Azure (default — static storage account key)

`values-azure.yaml` defaults to `objectStorage.auth.mode=static`. Contributor can `listKeys`; no Owner / User Access Administrator is required.

```bash
helm upgrade --install was charts/wolfram-application-server \
  -f charts/wolfram-application-server/values-azure.yaml \
  --namespace was --create-namespace \
  --set ingress.host=was.example.com \
  --set objectStorage.azure.accountName=mystorageaccount \
  --set objectStorage.resourceBucket=was-resources \
  --set objectStorage.nodefileBucket=was-nodefiles \
  --set objectStorage.auth.mode=static \
  --set objectStorage.auth.secretKey=<storage-account-key>
```

(`wasctl install app` sets the static mode and key from Terraform outputs automatically.)

### Azure (optional — Workload Identity)

Requires a Storage Blob Data Contributor assignment on the resource-manager UAMI (Owner/UAA, or an out-of-band role assignment). Infra still creates the UAMI + federated credential for this path.

```bash
helm upgrade --install was charts/wolfram-application-server \
  -f charts/wolfram-application-server/values-azure.yaml \
  --namespace was --create-namespace \
  --set ingress.host=was.example.com \
  --set objectStorage.azure.accountName=mystorageaccount \
  --set objectStorage.resourceBucket=was-resources \
  --set objectStorage.nodefileBucket=was-nodefiles \
  --set objectStorage.auth.mode=workloadIdentity \
  --set resourceManager.serviceAccount.azureClientId=<managed-identity-client-id>
```

### Chart-managed add-ons

Do not enable a dependency that is already installed in the cluster.

```bash
helm dependency update charts/wolfram-application-server
helm upgrade --install was charts/wolfram-application-server \
  -f charts/wolfram-application-server/values-aws.yaml \
  --set ingress-nginx.enabled=true \
  --set strimzi-kafka-operator.enabled=true \
  --set metrics-server.enabled=true \
  --set kube-prometheus-stack.enabled=true \
  --set prometheus-adapter.enabled=true \
  --set aws-efs-csi-driver.enabled=true \
  --set ingress.host=was.example.com \
  # … plus objectStorage / roleArn as above
```

### Static object-storage credentials on AWS (deprecated)

Prefer IRSA on AWS. On Azure, static account-key auth is the **supported default** (see above), not a deprecated path.

```bash
--set objectStorage.auth.mode=static \
--set objectStorage.auth.accessKey=... \
--set objectStorage.auth.secretKey=...
```

---

## Value reference (common keys)

| Key | Description |
|-----|-------------|
| `cloud` | `aws` or `azure` |
| `ingress.host` | Required public DNS hostname |
| `ingress.tls.enabled` | TLS on Ingress (cert-manager) |
| `ingress.tls.clusterIssuer` | Issuer for `was-certificate` (wasctl default: `letsencrypt-cluster-issuer`) |
| `objectStorage.*` | Buckets/containers, region, endpoint, auth mode |
| `storage.className` | Log PVC StorageClass (`was-efs` / `was-azurefile`) |
| `kafka.storage.class` | Broker volume StorageClass (`was-kafka-gp3` on AWS; `kafka-standardssd-xfs` on Azure). Do **not** use legacy AWS `gp2`. |
| `kafka.clusterName` / `kafka.namespace` | Bootstrap address is derived from these |
| `resourceManager.serviceAccount.roleArn` | AWS IRSA role ARN |
| `resourceManager.serviceAccount.azureClientId` | Azure UAMI client ID (only when `auth.mode=workloadIdentity`) |
| `objectStorage.auth.mode` | `irsa` (AWS default) / `static` (Azure default) / `workloadIdentity` (Azure opt-in) |
| `*.image.tag` | Service image tags |

Defaults and comments: `values.yaml`, `values-aws.yaml`, `values-azure.yaml`.

---

## Upgrade / uninstall

```bash
helm upgrade was charts/wolfram-application-server -f values-aws.yaml --reuse-values \
  --set awes.image.tag=4.4.0

helm uninstall was --namespace was
# PVCs and the kafka namespace are not removed automatically
```

---

## Troubleshooting

- Init waiting on Kafka: `kubectl get kafka,kafkabridge,kafkatopic -n kafka`
- AWES HPA / custom metrics: prometheus-adapter must serve WAS metrics
- Path 404: check `ingress.pathType` for your cloud
- See also [docs/Troubleshooting.md](../../docs/Troubleshooting.md)
