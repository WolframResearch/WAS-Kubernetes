# Configuration

After deploying Wolfram Application Server with **wasctl**, you can customize passwords, kernel initialization, scaling, pools, and service settings.

Declare changes in Helm values (or `--set` / a custom values file) and re-run the **app** stage. Prefer that over hand-editing live manifests in the cluster.

Full value comments: [charts/wolfram-application-server/values.yaml](charts/wolfram-application-server/values.yaml). Operator guides: [docs/](docs/).

---

## How to apply changes

```bash
# Custom values file
./wasctl install app --set-file custom-values.yaml

# Individual overrides
./wasctl install app --set awes.replicaCount=2
./wasctl install app --set awes.kernelPools[0].count=4
```

With Helm directly (cluster already prepared by wasctl):

```bash
helm upgrade --install was charts/wolfram-application-server \
  -f charts/wolfram-application-server/values-aws.yaml \
  -f custom-values.yaml \
  --namespace was \
  # … plus required objectStorage / ingress.host flags
```

After configuration that affects running kernels, use the [restart API](docs/API/Utilities.md) so engines reload node files and pool settings.

---

## Passwords (Restart API)

It is **highly recommended** to change the Restart API password before production use.

The restart endpoint (force restart of Wolfram Engine pools) is protected with HTTP basic authentication. The default user is **applicationserver**. The chart stores an htpasswd file in a Kubernetes Secret (`basic-auth` by default).

### Recommended: Helm values

Generate an htpasswd line, then set `basicAuth.credentials`:

```bash
htpasswd -nb applicationserver 'YourSecurePasswordHere'
# → applicationserver:$apr1$...
```

```yaml
basicAuth:
  secretName: basic-auth
  credentials: |
    applicationserver:$apr1$REPLACE_WITH_YOUR_HASH
```

Apply with `./wasctl install app` (or `helm upgrade`). The chart creates/updates the Secret; do not rely on a local `Source/ingress/auth` file.

### Optional: update the Secret by hand

```bash
htpasswd -nb applicationserver 'YourSecurePasswordHere' > /tmp/auth
kubectl create secret generic basic-auth \
  --from-file=auth=/tmp/auth \
  -n was --dry-run=client -o yaml | kubectl apply -f -
```

You can add more users with additional htpasswd lines in the same `auth` file / `credentials` block.

---

## Kernel initialization

Developers may run Wolfram Language code when Wolfram Engine kernels initialize, before handling user requests. By convention, deploy that code as a node file named `init.wl` (the container also recognizes `init.m` via `applicationserver.kernelinitializationfile.name`) at:

| Path | Scope |
|------|--------|
| `.Wolfram/Kernel/init.wl` | All kernels in all pools |
| `.Wolfram/[pool name]/Kernel/init.wl` | Only that pool (for example `.Wolfram/Public/Kernel/init.wl`) |

### Deploy with Wolfram Language

```wolfram
Needs["WolframApplicationServer`"]
was = ServiceConnect["WolframApplicationServer", "https://was.example.com"];
ServiceExecute[was, "DeployNodeFile",
  {"Contents" -> File["path/to/init.wl"],
   "NodeFile"  -> ".Wolfram/Kernel/init.wl"}]
```

### Deploy with the REST API

See [NodeFilesManager.md](docs/API/NodeFilesManager.md). Then restart:

```bash
curl -X POST https://was.example.com/.applicationserver/kernel/restart \
  -u applicationserver:YourSecurePasswordHere
```

Use `http://` if TLS is not enabled.

---

## Pod scaling

### Services that scale

| Component | Notes |
|-----------|--------|
| Active Web Elements Server (AWES) | CPU/memory **and** pool custom metrics |
| Endpoint Manager | CPU / memory HPA |
| Resource Manager | CPU / memory HPA |
| Kafka | Broker replicas via chart `kafka.replicas` (Strimzi), not the WAS app HPAs |

Node-file APIs are served through AWES / Resource Manager paths (there is no separate MinIO service; object storage is S3 or Azure Blob from the **infra** stage).

### Default algorithm (Resource Manager, Endpoint Manager, and AWES resource metrics)

Autoscaling raises and lowers replica counts from load:

- **Scale up** when average pod CPU exceeds **85%** or memory exceeds **90%**
- **Scale down** when load stays below those targets (downscaling is less aggressive than upscaling to limit churn)

Defaults: HPA `minReplicas` comes from each service’s `replicaCount` (chart default **1**); `maxReplicas` is **10**. Prefer at least **2** replicas in production.

### Active Web Elements Server (extra metrics)

In addition to CPU/memory, AWES scaling uses per-pool metrics (via prometheus-adapter):

- Recent peak percentage of kernels in use
- Queue length of users waiting for a kernel

If **any** pool averages above **90%** kernel use or queue length above **2**, additional AWES pods are started (until the maximum). If **all** pools average under **80%** kernel use and queues under **1**, pods may scale down.

“Average” means average across active pods of that type.

### How to change bounds

Override Helm values (do **not** edit `Source/hpa/*.yaml`):

```yaml
resourceManager:
  replicaCount: 2   # HPA minReplicas

endpointManager:
  replicaCount: 2

awes:
  replicaCount: 2   # HPA minReplicas for AWES
```

Then:

```bash
./wasctl install app
```

AWES custom-metric thresholds are defined in the chart’s `awes-hpa` template (90% kernel use, queue size 2, CPU 85%, memory 90%). Changing those thresholds currently requires a chart template change or a follow-up values parameterization.

Ensure prometheus-adapter is installed (addons stage) or custom-metric HPAs will not work; CPU/memory HPAs still apply.

---

## Kernel pools

Wolfram Engine kernels run in pools managed by AWES. Configure them under `awes.kernelPools` (mapped to `poolconfiguration_kernelpool_<n>__*` environment variables).

- Configure at least one pool.
- Keep a pool named **Public** — it is the default when a deployed resource does not declare a pool.

```yaml
awes:
  kernelPools:
    - name: Public
      count: 2
      jlinkEnabled: false   # active web elements
    - name: MSP
      count: 2
      jlinkEnabled: true    # MSP pages (JLink required)
```

Add further pools with additional list entries (indices increase automatically).

Equivalent legacy env names (for reference):

| Env var | Meaning |
|---------|---------|
| `poolconfiguration_kernelpool_<n>__KernelNumber` | Engines in the pool (`count`) |
| `poolconfiguration_kernelpool_<n>__KernelPoolName` | Pool name |
| `poolconfiguration_kernelpool_<n>__JLinkEnabled` | `"true"` / `"false"` |

---

## Environment variables and URLs

Many cluster settings are environment variables inside the service pods. With wasctl they come from the Helm chart, not from editing `Source/deployments` and `kubectl apply`.

### Public service URLs

Base URLs for AWES are derived from `ingress.host` and TLS:

`{http\|https}://<ingress.host>/…`

| Logical setting | Chart source | Example path |
|-----------------|--------------|--------------|
| Base URL | `applicationserver.servername` | `/` |
| Resource Manager | `…/resources/` | |
| Node files | `…/nodefiles/` | |
| Endpoint Manager | `…/endpoints/` | |
| Restart API | `…/.applicationserver/kernel/restart` | |

Set the hostname (and TLS) once:

```yaml
ingress:
  host: was.example.com
  tls:
    enabled: true   # wasctl enables this when cert-manager is not skipped
```

### Kafka

| Env var | Chart source |
|---------|----------------|
| `KAFKA.BOOTSTRAP-SERVERS` | Built-in: `<kafka.clusterName>-kafka-bootstrap.<kafka.namespace>.svc.cluster.local:9092`, or `kafka.bootstrapServers` when `kafka.mode=external` |

```yaml
kafka:
  mode: builtin          # or external
  clusterName: kafka-persistent
  namespace: kafka
  # bootstrapServers: "my-kafka:9092"   # required when mode=external
```

### AWES caching

| Env var | Default in chart |
|---------|------------------|
| `APPLICATIONSERVER_CACHEDIRECTORY` | `/tmp/.wolframcache` |
| `applicationserver.nodefiles.cachedirectory` | `/opt/.wolframcache/nodefiles/` |

These are set in the AWES Deployment template today. To change them, adjust the chart templates or request values wiring if you need them as first-class Helm knobs.

---

## Related documentation

| Doc | Topic |
|-----|--------|
| [charts/wolfram-application-server/README.md](charts/wolfram-application-server/README.md) | Install flags and value reference |
| [docs/API/Utilities.md](docs/API/Utilities.md) | Restart API |
| [docs/API/NodeFilesManager.md](docs/API/NodeFilesManager.md) | Node files / license / init scripts |
| [docs/API/ResourceManager.md](docs/API/ResourceManager.md) | Resources |
| [docs/API/EndpointManager.md](docs/API/EndpointManager.md) | Endpoints |
| [docs/Operations.md](docs/Operations.md) | Upgrade, doctor, destroy |
| [docs/Troubleshooting.md](docs/Troubleshooting.md) | Common failures |
