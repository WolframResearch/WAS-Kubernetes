# wasctl Version Compatibility Matrix

This package defines which versions of each tool and component are supported
by wasctl. It runs version parsers for every installed tool and reports
incompatibilities as typed `Issue` values with `Warning` or `Critical` severity.

## Compatibility table

| Component              | Minimum | Maximum  | Detected from                                    | Notes                                                      |
|------------------------|---------|----------|--------------------------------------------------|------------------------------------------------------------|
| Helm                   | 3.10.0  | 3.17.99  | `helm version --short`                           | Required for chart install                                 |
| kubectl                | 1.28.0  | 1.36.99  | `kubectl version --client -o json`               | Client version; should match cluster ±1 minor              |
| Terraform              | 1.9.0   | 1.13.99  | `terraform version -json`                        | Used for infrastructure provisioning                       |
| AWS CLI                | 2.13.0  | 2.99.99  | `aws --version`                                  | Any 2.x OK; **bump max deliberately when 3.x ships**      |
| Azure CLI              | 2.50.0  | 2.99.99  | `az version --output json`                       | Any 2.x OK; **bump max deliberately when 3.x ships**      |
| Kubernetes (server)    | 1.30.0  | 1.36.99  | `kubectl version -o json` → `serverVersion`      | Cluster server version, not kubectl client                 |
| Strimzi operator       | 0.43.0  | 0.49.99  | Strimzi deployment container image tag           | Chart pins ~0.43.0; bump upper when chart pin advances     |
| WAS Helm chart         | 1.0.0   | 1.99.99  | `charts/wolfram-application-server/Chart.yaml`   | Critical if below (chart too old for this wasctl)          |
| hashicorp/aws provider | 5.50.0  | 6.99.99  | `terraform version -json` → `provider_selections`| Detected only after `terraform init`                       |
| hashicorp/azurerm      | 3.90.0  | 4.99.99  | `terraform version -json` → `provider_selections`| Detected only after `terraform init`                       |

### Deferred components

| Component             | Reason                                                                     |
|-----------------------|----------------------------------------------------------------------------|
| EKS module (aws/eks)  | Requires parsing `.terraform.lock.hcl` or `terraform providers lock` output, which is environment-dependent and not available without a full `terraform init` of the stack |

## Severity

- **Critical** — version is below the minimum. wasctl blocks the deployment in
  `preflight.Apply()` and reports a remediation hint for each failing component.
- **Warning** — version exceeds the tested maximum. Deployment proceeds with a
  logged warning. The upper bound is a "last-known-good" cap, not a hard limit.

Both severities are encoded on the `Issue.Severity` field. Render code reads
`Issue.Severity` directly — it does not re-inspect version bounds.

## How it works

`versions.Check()` calls each tool's version parser (see `parsers/`) and
compares results against `CurrentMatrix`. Tools that are not installed or
unreachable (e.g., Strimzi/Kubernetes when no cluster is configured) are
silently skipped — their absence is not an error.

## Updating the matrix

When support bounds change:

1. Update `CurrentMatrix` in `matrix.go`.
2. Update this README table.
3. Update the `kubeVersion` constraint in `charts/wolfram-application-server/Chart.yaml`
   to match the Kubernetes range.
4. Run `go test ./internal/versions/...` to confirm tests pass.
5. Commit all three changes in one PR so matrix, chart, and docs stay in sync.
