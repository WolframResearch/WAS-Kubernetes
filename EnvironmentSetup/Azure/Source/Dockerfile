FROM ubuntu:22.04

ARG KUBECTL_VERSION="v1.34.0"
ARG TERRAFORM_VERSION="1.13.1"
ARG HELM_VERSION="v3.15.2"
ARG TARGETARCH=amd64

ENV DEBIAN_FRONTEND=noninteractive \
    LANG='en_US.UTF-8' \
    LANGUAGE='en_US:en' \
    LC_ALL='en_US.UTF-8'

RUN \
    apt-get update && \
    apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    unzip \
    git \
    procps \
    vim-tiny \
    locales \
    && \
	update-ca-certificates && \
	\
    locale-gen en_US.utf8 && \
    \
    # Install Azure CLI
	curl -sL https://aka.ms/InstallAzureCLIDeb | bash && \
    \
    # Install kubectl
    curl -fLO "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl" && \
    install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl && \
    \
    # Install Helm
    curl -fL "https://get.helm.sh/helm-${HELM_VERSION}-linux-${TARGETARCH}.tar.gz" | tar -xz && \
    install -o root -g root -m 0755 linux-${TARGETARCH}/helm /usr/local/bin/helm && \
    \
    # Install Terraform
    curl -fL "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_${TARGETARCH}.zip" -o "terraform.zip" && \
    unzip -o terraform.zip -d /usr/local/bin && \
    \
    apt-get purge -y --auto-remove unzip && \
    rm -rf \
        /var/lib/apt/lists/* \
        kubectl \
        helm \
        linux-${TARGETARCH}/ \
        terraform.zip && \
    \
    echo "--- Verifying Installations ---" && \
    kubectl version --client && \
    helm version && \
    terraform --version && \
    az --version && \
    echo "--- Verification Complete ---"

WORKDIR /
ENTRYPOINT ["bash"]