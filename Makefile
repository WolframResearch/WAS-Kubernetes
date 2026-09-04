##
## Wolfram Application Server — top-level Makefile
##
## Usage:
##   make tf-bootstrap  ENV=prod              # run bootstrap stack once per account
##   make tf-init       ENV=prod              # init main stack with backend.hcl
##   make tf-plan       ENV=prod              # plan main stack
##   make tf-apply      ENV=prod              # apply main stack
##   make tf-destroy    ENV=prod              # destroy main stack (chart must be uninstalled first)
##   make helm-install                        # install the chart using Terraform outputs
##   make print-helm-command                  # print the helm install command without running it
##
## Prefer wasctl for cluster add-ons and day-2 operations:
##   ./wasctl install addons
##   ./wasctl install app
##
## ENV controls which backend.hcl is used:
##   infra/aws/stack/examples/backend-<ENV>.hcl
##   (copy examples/backend.hcl.example → examples/backend-<ENV>.hcl and fill in values)
##

BOOTSTRAP_DIR := infra/aws/bootstrap
STACK_DIR     := infra/aws/stack
CHART_DIR     := charts/wolfram-application-server
ENV           ?= prod
BACKEND_FILE  := $(STACK_DIR)/examples/backend-$(ENV).hcl

# ─── Terraform ───────────────────────────────────────────────────────────────

.PHONY: tf-bootstrap
tf-bootstrap:
	@echo "==> Initialising bootstrap stack (local state)..."
	terraform -chdir=$(BOOTSTRAP_DIR) init
	@echo "==> Applying bootstrap stack..."
	terraform -chdir=$(BOOTSTRAP_DIR) apply
	@echo
	@echo "Copy the backend_hcl_hint output into $(STACK_DIR)/examples/backend-$(ENV).hcl"
	@echo "then run: make tf-init ENV=$(ENV)"

.PHONY: tf-init
tf-init: _check-backend
	@echo "==> Initialising main stack (backend: $(BACKEND_FILE))..."
	terraform -chdir=$(STACK_DIR) init -backend-config=$(abspath $(BACKEND_FILE))

.PHONY: tf-plan
tf-plan: _check-backend
	@echo "==> Planning main stack..."
	terraform -chdir=$(STACK_DIR) plan -var-file=$(abspath $(STACK_DIR)/examples/terraform.tfvars.example)

.PHONY: tf-apply
tf-apply: _check-backend
	@echo "==> Applying main stack..."
	terraform -chdir=$(STACK_DIR) apply -var-file=$(abspath $(STACK_DIR)/examples/terraform.tfvars.example)

.PHONY: tf-destroy
tf-destroy: _check-backend
	@echo "==> WARNING: this destroys the EKS cluster, EFS, and S3 buckets."
	@echo "==> Uninstall the Helm chart first: helm uninstall was --namespace was"
	@read -p "Continue? [y/N] " ans && [ "$$ans" = "y" ]
	terraform -chdir=$(STACK_DIR) destroy -var-file=$(abspath $(STACK_DIR)/examples/terraform.tfvars.example)

.PHONY: _check-backend
_check-backend:
	@if [ ! -f "$(BACKEND_FILE)" ]; then \
	  echo "ERROR: $(BACKEND_FILE) not found."; \
	  echo "Copy $(STACK_DIR)/examples/backend.hcl.example to $(BACKEND_FILE) and fill in values."; \
	  exit 1; \
	fi

# ─── Helm ────────────────────────────────────────────────────────────────────

.PHONY: helm-install
helm-install:
	@echo "==> Reading Terraform outputs..."
	$(eval RESOURCE_BUCKET  := $(shell terraform -chdir=$(STACK_DIR) output -raw resource_bucket_name))
	$(eval NODEFILE_BUCKET  := $(shell terraform -chdir=$(STACK_DIR) output -raw nodefile_bucket_name))
	$(eval RM_ROLE_ARN      := $(shell terraform -chdir=$(STACK_DIR) output -raw resource_manager_role_arn))
	$(eval AWS_REGION       := $(shell terraform -chdir=$(STACK_DIR) output -raw aws_region 2>/dev/null || echo us-east-1))
	@if [ -z "$(INGRESS_HOST)" ]; then \
	  echo "ERROR: set INGRESS_HOST=<your-dns-name>"; \
	  echo "  make helm-install INGRESS_HOST=was.example.com"; \
	  exit 1; \
	fi
	@echo "==> Running helm dependency update..."
	helm dependency update $(CHART_DIR)
	@echo "==> Installing WAS chart..."
	helm install was $(CHART_DIR) \
	  -f $(CHART_DIR)/values-aws.yaml \
	  --namespace was --create-namespace \
	  --set ingress.host=$(INGRESS_HOST) \
	  --set objectStorage.region=$(AWS_REGION) \
	  --set objectStorage.resourceBucket=$(RESOURCE_BUCKET) \
	  --set objectStorage.nodefileBucket=$(NODEFILE_BUCKET) \
	  --set resourceManager.serviceAccount.roleArn=$(RM_ROLE_ARN)

.PHONY: print-helm-command
print-helm-command:
	@echo "==> Helm install command (from Terraform outputs):"
	@terraform -chdir=$(STACK_DIR) output -raw helm_install_command_hint
	@echo

# ─── Go binary ───────────────────────────────────────────────────────────────

GO_VERSION  ?= $(shell go env GOVERSION 2>/dev/null || echo go1.26)
GO_MODULE   := github.com/WolframResearch/WAS-Kubernetes
BIN_VERSION ?= dev
LDFLAGS     := -s -w -X $(GO_MODULE)/internal/version.Version=$(BIN_VERSION)

.PHONY: generate
generate:
	@echo "==> Syncing embedded chart + Terraform assets..."
	go generate ./internal/assets/
	@echo "==> Done (internal/assets/ + internal/versions/k8s_cluster_gen.go)"

.PHONY: build
build: generate
	@echo "==> Building wasctl (local)..."
	go build -trimpath -ldflags="$(LDFLAGS)" -o wasctl ./cmd/wasctl
	@echo "Binary: ./wasctl"

.PHONY: build-all
build-all: generate
	@echo "==> Cross-compiling all platforms..."
	@mkdir -p dist
	GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/wasctl-darwin-amd64      ./cmd/wasctl
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/wasctl-darwin-arm64      ./cmd/wasctl
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/wasctl-linux-amd64       ./cmd/wasctl
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/wasctl-linux-arm64       ./cmd/wasctl
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/wasctl-windows-amd64.exe ./cmd/wasctl
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/wasctl-windows-arm64.exe ./cmd/wasctl
	cd dist && sha256sum wasctl-* > SHA256SUMS
	@echo "Binaries and SHA256SUMS written to dist/"

.PHONY: test
test:
	go test ./... -race -cover

.PHONY: vet
vet:
	go vet ./...

.PHONY: lint
lint:
	golangci-lint run

.PHONY: tidy
tidy:
	go mod tidy
