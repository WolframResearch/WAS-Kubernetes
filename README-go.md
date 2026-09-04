# wasctl — Go developer guide

Build, test, and release notes for the `wasctl` binary. For install and day-2 use, see the root [README.md](README.md) and [docs/](docs/).

---

## Prerequisites

| Tool | Purpose | Install |
|------|---------|---------|
| **Go 1.26+** | Build and test (`go.mod`) | [go.dev/dl](https://go.dev/dl/) |
| **Helm** ≥ 3.12 | Optional: `helm dependency update` / lint when changing the chart | [helm.sh](https://helm.sh/docs/intro/install/) |
| **golangci-lint** | Local lint (also runs in CI) | [golangci-lint.run](https://golangci-lint.run/usage/install/) |

Runtime CLIs customers need (terraform, kubectl, aws/az) are **not** required to compile wasctl; they are required to run most stages.

---

## Embedded assets (important)

Production builds embed Terraform stacks and the Helm chart under `internal/assets/`. Source of truth:

| Source | Embedded copy |
|--------|----------------|
| `infra/aws/bootstrap/` | `internal/assets/terraform/bootstrap/` |
| `infra/aws/stack/` | `internal/assets/terraform/stack/` |
| `infra/azure/bootstrap/` | `internal/assets/terraform/azure-bootstrap/` |
| `infra/azure/stack/` | `internal/assets/terraform/azure-stack/` |
| `charts/wolfram-application-server/` | `internal/assets/chart/` |

`go generate ./internal/assets/` also writes `internal/versions/k8s_cluster_gen.go` from each stack’s `cluster_version` default (install UI offers the last three minors ending at that pin).

```bash
make generate
# equivalent:
go generate ./internal/assets/
```

- After editing `infra/` or `charts/`, regenerate and commit the embeds (unless you only use `--local`).
- If you change chart Chart.yaml dependencies, run `helm dependency update charts/wolfram-application-server` **before** `make generate` so the unpacked `charts/` subcharts are current.
- `--local` reads `infra/` and `charts/` from the repo on disk and skips embeds — use for fast iteration:

```bash
./wasctl --local install app --cloud aws ...
```

Do **not** edit `EnvironmentSetup/`; wasctl uses `infra/` + `charts/` only.

---

## Building from source

```bash
# Sync embeds, then build for your OS/arch (output: ./wasctl)
make build

# Or manually:
go generate ./internal/assets/
go build -trimpath \
  -ldflags="-s -w -X github.com/WolframResearch/WAS-Kubernetes/internal/version.Version=dev" \
  -o wasctl \
  ./cmd/wasctl

./wasctl --version
```

Cross-compile (no CGO):

```bash
make build-all
# → dist/wasctl-{darwin,linux}-{amd64,arm64} + dist/SHA256SUMS

# Or set BIN_VERSION for the ldflags string:
make build-all BIN_VERSION=v1.2.3
```

Manual cross-compile example:

```bash
VERSION=v1.2.3
LDFLAGS="-s -w -X github.com/WolframResearch/WAS-Kubernetes/internal/version.Version=${VERSION}"

GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="${LDFLAGS}" -o dist/wasctl-darwin-amd64      ./cmd/wasctl
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="${LDFLAGS}" -o dist/wasctl-darwin-arm64      ./cmd/wasctl
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="${LDFLAGS}" -o dist/wasctl-linux-amd64       ./cmd/wasctl
GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="${LDFLAGS}" -o dist/wasctl-linux-arm64       ./cmd/wasctl
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="${LDFLAGS}" -o dist/wasctl-windows-amd64.exe ./cmd/wasctl
GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="${LDFLAGS}" -o dist/wasctl-windows-arm64.exe ./cmd/wasctl
```

---

## Tests and lint

```bash
make test      # go test ./... -race -cover
make vet       # go vet ./...
make lint      # golangci-lint run
make tidy      # go mod tidy

# Single package
go test ./internal/stages/... -v
go test ./internal/addons/... -count=1
```

CI (`.github/workflows/ci.yml`) runs vet, race tests, and golangci-lint on `main` / PRs.

---

## Useful Make targets

| Target | What it does |
|--------|----------------|
| `make generate` | Sync chart + Terraform into `internal/assets/` |
| `make build` | `generate` + local `./wasctl` |
| `make build-all` | `generate` + six platform binaries under `dist/` (darwin/linux/windows × amd64/arm64) |
| `make test` / `vet` / `lint` / `tidy` | Quality gates |

The Makefile also has AWS Terraform/helm helpers (`tf-*`, `helm-install`) for manual infra experiments. Prefer **wasctl** for real installs (`./wasctl install --all`).

---

## Dependencies (Go modules)

| Area | Libraries |
|------|-----------|
| CLI | `spf13/cobra` |
| Install TUI | `charmbracelet/bubbletea`, `lipgloss`, `bubbles/spinner` |
| TTY detection | `golang.org/x/term` |
| AWS meta / sweep | `aws-sdk-go-v2` (S3, DynamoDB, EC2, ELB, …) |
| Azure meta | `azure-sdk-for-go` (azidentity, armresources, armstorage, azblob) |

Config loading is plain Go (defaults / file / env / flags), not Viper. wasctl still shells out to `terraform`, `helm`, `kubectl`, and `aws`/`az` for most stage work. Deferred ideas (Helm SDK in-process, fewer CLI shells, self-update): [NOTES.md](NOTES.md).

---

## Releasing

Binaries are built by `.github/workflows/release.yml` on pushes to versioned
release branches (including merges from master/main):

```text
release/4.0.0          ✅
release/4.1.0-rc.1     ✅
release/v4.0.0         ❌  (no "v" prefix)
```

The workflow:

1. Derives version from the branch (`release/4.0.0` → `4.0.0`)
2. Runs `helm dependency update` + `make generate`
3. Cross-compiles six platform binaries + `SHA256SUMS`
4. Uploads them as workflow artifacts
5. Creates/updates a GitHub Release tagged **`4.0.0`** (no `v`)

| Artifact | Platform |
|----------|----------|
| `wasctl-darwin-amd64` | macOS Intel |
| `wasctl-darwin-arm64` | macOS Apple Silicon |
| `wasctl-linux-amd64` | Linux x86_64 |
| `wasctl-linux-arm64` | Linux ARM64 |
| `wasctl-windows-amd64.exe` | Windows x86_64 |
| `wasctl-windows-arm64.exe` | Windows ARM64 |
| `SHA256SUMS` | checksums |

```bash
sha256sum -c SHA256SUMS
chmod +x wasctl-linux-amd64 && mv wasctl-linux-amd64 wasctl
```

Local equivalent: `make build-all BIN_VERSION=4.0.0`.

---

## Project layout

```
cmd/wasctl/
  main.go                   Cobra root, flags, stage wiring
  menu.go                   Interactive cluster-first menu
  wizard.go                 Guided install prompts
  commands.go               Subcommand helpers
  serve.go / doctor.go / support_bundle.go
internal/
  addons/                   Cluster add-on installers (ingress, Strimzi, cert-manager, …)
  assets/                   go:embed Terraform + Helm chart (+ gen.go)
  audit/                    Append-only workspace audit.log (S3 / Azure blob)
  cloud/                    Shared cloud helpers
  cloudsweep/               AWS destroy orphan sweep
  config/                   Config fields, defaults, Show()
  doctor/                   Diagnostic checks
  inspect/                  Cluster inspection for `wasctl info`
  metabucket/               AWS S3 workspace meta + locks
  metacontainer/            Azure Blob workspace meta + locks
  report/                   Plain + Fake reporters (Conductor)
  repo/                     Repo root discovery
  runner/                   ExecRunner + MockRunner
  stages/                   Seven stages: preflight → app
  supportbundle/            Support archive collectors
  tools/                    terraform / helm / kubectl / aws / az wrappers
  tui/                      Bubble Tea install UI
  version/                  Version string (ldflags)
  versions/                 Tool + k8s compatibility matrix (incl. generated pin)
  webui/                    wasctl serve handlers + static UI
  workspace/                Open/materialize/lock workspace
charts/wolfram-application-server/   Helm chart (source of truth)
infra/aws/  infra/azure/             Terraform bootstrap + stack (source of truth)
docs/                       Operator guides
```

---

## Smoke-check a local binary

```bash
make build
./wasctl version
./wasctl --help
./wasctl --local --cloud aws   # interactive menu; needs AWS creds to list clusters
```
