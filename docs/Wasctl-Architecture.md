# Architecture

How wasctl deploys Wolfram Application Server on AWS EKS or Azure AKS.

![wasctl control plane](images/architecture-control-plane.png)

```mermaid
flowchart TB
  op["Operator laptop<br/>wasctl CLI · wasctl serve"]
  meta["Cloud meta store<br/>workspace · locks · kubeconfig · stage marks"]
  tf["Terraform stack<br/>network · EKS/AKS · storage · identity"]
  add["Cluster add-ons<br/>ingress-nginx · Strimzi · CSI · metrics · …"]
  app["WAS Helm chart<br/>AWES · Resource Manager · Endpoint Manager · Kafka CRs"]

  op --> meta --> tf --> add --> app

  subgraph metaDetail [Meta store by cloud]
    awsMeta["AWS: S3 + DynamoDB lock"]
    azMeta["Azure: Blob + lease"]
  end
  meta -.-> metaDetail
```

Installation state lives in your cloud account. Any machine with the same credentials can continue an install.

---

## Operator surfaces

wasctl exposes the same workflows through the CLI and a local web UI.

```mermaid
flowchart LR
  cli["CLI<br/>install · status · doctor · destroy · …"]
  ui["Web UI<br/>wasctl serve"]
  orch["Shared orchestration<br/>stages + meta store"]
  cloud["AWS or Azure resources"]

  cli --> orch
  ui --> orch
  orch --> cloud
```

See [cli.md](cli.md) and [web-ui.md](web-ui.md).

---

## Seven stages

![Install stage pipeline](images/architecture-install-stages.png)

```mermaid
flowchart LR
  s1[1 preflight] --> s2[2 bootstrap] --> s3[3 backend] --> s4[4 infra]
  s4 --> s5[5 kubeconfig] --> s6[6 addons] --> s7[7 app]
```

| Stage | Output |
|-------|--------|
| preflight | Verified tools and login |
| bootstrap | Remote Terraform state backend |
| backend | Backend configuration in the workspace |
| infra | Cluster, network, buckets/containers, identity |
| kubeconfig | Isolated kubeconfig stored with the workspace |
| addons | Cluster operators and controllers |
| app | WAS Helm release |

Destroy runs these stages in reverse order. Details: [install.md](install.md).

---

## Application stack

![WAS application stack on Kubernetes](images/architecture-app-stack.png)

```mermaid
flowchart TB
  dns["Internet / DNS<br/>ingress.host"]
  ing["ingress-nginx<br/>LoadBalancer"]
  awes["AWES<br/>kernels · HTTP APIs"]
  rm["Resource Manager<br/>S3 / Blob objects"]
  em["Endpoint Manager<br/>endpoint registry"]
  kafka["Kafka + KafkaBridge<br/>Strimzi"]
  logs["Log PVCs RWX<br/>EFS or Azure Files"]
  obj["Object storage<br/>S3 buckets or Blob containers"]

  dns --> ing
  ing --> awes
  ing --> rm
  ing --> em
  awes --> kafka
  rm --> kafka
  em --> kafka
  awes --> logs
  rm --> logs
  em --> logs
  rm --> obj
```

| Service | Role |
|---------|------|
| AWES | Wolfram HTTP APIs and kernels |
| Resource Manager | Resource objects in S3 or Blob |
| Endpoint Manager | Endpoint registry |
| Kafka + Bridge (Strimzi) | Messaging between components |

ingress-nginx routes public traffic by path. Shared ReadWriteMany volumes hold logs.

---

## Workspace metadata storage

```mermaid
flowchart LR
  subgraph aws [AWS]
    s3["S3 meta bucket<br/>wolfram-wasctl-meta-*"]
    ddb["DynamoDB lock table"]
    s3 --- ddb
  end
  subgraph azure [Azure]
    blob["Storage account<br/>+ containers"]
    lease["Blob lease lock"]
    blob --- lease
  end
  ws["Per-cluster workspace<br/>workspace.json · kubeconfig · stage marks"]
  aws --> ws
  azure --> ws
```

| Cloud | Storage | Lock |
|-------|---------|------|
| AWS | S3 bucket `wolfram-wasctl-meta-*` | DynamoDB |
| Azure | Storage account and containers | Blob lease |

---

## Notes

- wasctl does not write kubeconfig to `~/.kube/config`.
- Install locks prevent two concurrent installs for the same cluster name.
- Destroy and workspace delete ask for confirmation.
- The web UI has no authentication; bind it to localhost.
