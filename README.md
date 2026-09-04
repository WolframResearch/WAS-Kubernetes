# Wolfram Application Server on Kubernetes

The [Wolfram Application Server (WAS)](https://www.wolfram.com/application-server/) combines the computational power of the Wolfram Engine with modern containerization. It provides a scalable deployment model for Wolfram-powered web applications.

This repository deploys WAS on **Amazon EKS** or **Azure AKS** with **wasctl**. The documentation here, together with an appropriate license from Wolfram Research, will get you started.

![Wolfram Application Server on Kubernetes](docs/images/architecture-app-stack.png)

| Tool | Description |
|------|-------------|
| `wasctl` | Command-line installer and operator (install, destroy, doctor, status, …) |
| `wasctl serve` | Local web UI for the same workflows |

During `wasctl install`, an interactive terminal can show stage progress when stdout is a TTY. For plain text output, use `--no-tui` or set `NO_COLOR=1`.

---

## Getting started

### License

To deploy and use Wolfram Language content, you need a license file from Wolfram Research. Contact your sales representative at [1-800-WOLFRAM](tel:18009653726) to discuss licensing options.

### Choose your Kubernetes environment

WAS runs in Kubernetes. Supported cloud paths in this repository:

| Environment | How to deploy |
|-------------|----------------|
| **Amazon EKS** | Use wasctl (`--cloud aws`). See [Quick start](#quick-start) and [docs/Install.md](docs/Install.md). |
| **Azure AKS** | Use wasctl (`--cloud azure`). See [Quick start](#quick-start) and [docs/Install.md](docs/Install.md). |
| **On-premises** | Contact [Wolfram Technical Support](https://www.wolfram.com/support/) for options and documentation. |

---

## Prerequisites

Install these on the machine that runs `wasctl` or `wasctl serve`.

### Always required

| Tool | Notes | Install |
|------|--------|---------|
| **wasctl** | Download a release binary, or `go build -o wasctl ./cmd/wasctl` ([Go](https://go.dev/dl/) 1.26+) | Build from this repo, or use a released binary when available |
| **Terraform** ≥ 1.9 | Cloud infrastructure | [Install Terraform](https://developer.hashicorp.com/terraform/install) |
| **Helm** ≥ 3.12 | Chart install and upgrades | [Install Helm](https://helm.sh/docs/intro/install/) |
| **kubectl** | Cluster operations | [Install kubectl](https://kubernetes.io/docs/tasks/tools/) |
| **curl** | Used by doctor and some checks | Usually preinstalled; see [curl download](https://curl.se/download.html) |

### AWS

| Requirement | Notes | Install |
|-------------|--------|---------|
| **AWS CLI v2** | Configured (`aws configure` or environment variables) | [Install the AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html) |
| Permissions | Create VPC, EKS, EFS, S3, IAM roles, DynamoDB | — |
| Optional DNS | Route53 if wasctl should create a CNAME | — |

### Azure

| Requirement | Notes | Install |
|-------------|--------|---------|
| **Azure CLI** | `az login` and the correct subscription (`az account set`) | [Install the Azure CLI](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli) |
| Permissions | Create resource groups, AKS, storage, managed identities, role assignments | — |
| Ingress host | A DNS name (for example a `*.cloudapp.azure.com` label), not a raw public IP | — |

Cluster add-ons (ingress-nginx, Strimzi, CSI, metrics-server, prometheus-adapter, optional cert-manager) are installed in the **addons** stage unless you `--skip` them. Object storage, identity, and shared filesystems come from the **infra** stage.

---

## Quick start

### CLI

```bash
# AWS
./wasctl install --all \
  --cloud aws \
  --region us-east-1 \
  --cluster-name was-prod \
  --ingress-host was.example.com

# Azure
./wasctl install --all \
  --cloud azure \
  --azure-location eastus \
  --cluster-name was-prod \
  --ingress-host was-prod.eastus.cloudapp.azure.com

# Plain text progress (no interactive terminal UI)
./wasctl install --all --no-tui --cloud aws ...

# Interactive guided menu (lists existing clusters per cloud)
./wasctl --cloud aws

./wasctl status
./wasctl doctor
./wasctl info
./wasctl destroy
```

A full install usually takes about 35–50 minutes (Terraform infra is the longest step).

### Web UI

```bash
./wasctl serve
# http://127.0.0.1:8765
# ./wasctl serve --port 9000 --no-browser
```

The UI has no authentication. Keep it on localhost unless you are on a trusted network.

---

## What `wasctl` does

Seven stages. Stage state is stored in your cloud account so you can resume from another machine with the same credentials.

| # | Stage | Description |
|---|-------|-------------|
| 1 | preflight | Local tools and cloud credentials |
| 2 | bootstrap | Terraform remote state storage |
| 3 | backend | Terraform backend configuration |
| 4 | infra | VPC/VNet, EKS/AKS, storage, identity |
| 5 | kubeconfig | Isolated kubeconfig (does not write `~/.kube/config`) |
| 6 | addons | ingress-nginx, Strimzi, CSI, metrics, optional cert-manager |
| 7 | app | Helm install of the WAS chart |

---

## Activation

Obtain a license file from your sales representative. Deploy it to WAS as a node file at the conventional location `.Wolfram/Licensing/mathpass`.

From a Wolfram Language client, load the Wolfram Application Server package (once per session):

```wolfram
Needs["WolframApplicationServer`"]
```

Then evaluate:

```wolfram
was = ServiceConnect["WolframApplicationServer", "http://<your-base-url>"];
ServiceExecute[was, "DeployNodeFile",
  {"Contents" -> File["path/to/mathpass"],
   "NodeFile" -> ".Wolfram/Licensing/mathpass"}]
```

Use `https://` in the base URL when TLS is enabled (cert-manager / Let's Encrypt via wasctl).

Alternatively, use the [node files REST API](docs/API/NodeFilesManager.md) to install the license file.

Restart the application using the [restart API](docs/API/Utilities.md) so your Wolfram Engines pick up the license.

---

## Development

In your Wolfram Language environment, evaluate:

```wolfram
PacletInstall["WolframApplicationServer"]
```

- Guide page: **WolframApplicationServer/guide/WolframApplicationServer** (links to WAS functions)
- Service page: **WolframApplicationServer/ref/service/WolframApplicationServer** (details of a `ServiceConnection` to WAS)

For Helm value customization after deploy, see [Configuration.md](Configuration.md).

---

## Documentation

### wasctl (deploy and operate)

| Doc | Description |
|-----|-------------|
| [docs/Install.md](docs/Install.md) | Installation (CLI and web UI) |
| [docs/Operations.md](docs/Operations.md) | Status, doctor, upgrade, destroy, support bundles |
| [docs/WebUI.md](docs/WebUI.md) | Web UI |
| [docs/CLI.md](docs/CLI.md) | Commands and flags |
| [docs/Troubleshooting.md](docs/Troubleshooting.md) | Common failures |
| [docs/Wasctl-Architecture.md](docs/Wasctl-Architecture.md) | Stages, workspaces, stack overview, and diagrams |
| [charts/wolfram-application-server/README.md](charts/wolfram-application-server/README.md) | Helm chart values |
| [Configuration.md](Configuration.md) | Application configuration via Helm values |
| [README-go.md](README-go.md) | Building wasctl from source |

### API specifications

| Doc | Description |
|-----|-------------|
| [docs/API/Utilities.md](docs/API/Utilities.md) | Restart and utility endpoints |
| [docs/API/ResourceManager.md](docs/API/ResourceManager.md) | Resource Manager API |
| [docs/API/NodeFilesManager.md](docs/API/NodeFilesManager.md) | Node files (including license) API |
| [docs/API/EndpointManager.md](docs/API/EndpointManager.md) | Endpoint Manager API |

### Product architecture

| Doc | Description |
|-----|-------------|
| [docs/Architecture/WolframApplicationServerArchitecture.md](docs/Architecture/WolframApplicationServerArchitecture.md) | WAS architecture |

---

## Repository layout

```
wasctl / cmd/wasctl/                 CLI and serve
internal/                            Stages, web UI, doctor, workspace, assets
charts/wolfram-application-server/   Helm chart
infra/aws/  infra/azure/             Terraform bootstrap and stack
docs/                                Product API and wasctl operation guide
```

---

## Uninstall

```bash
./wasctl destroy
./wasctl destroy --destroy-state-backend   # also remove Terraform state storage
```
