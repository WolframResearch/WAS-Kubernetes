FROM ubuntu:focal
ENV KUBECTL_VERSION='v1.27.0'
ENV DEBIAN_FRONTEND=noninteractive 
ENV TERM=xterm-256color
ENV COLORTERM=truecolor
ENV LANG='en_US.UTF-8' LANGUAGE='en_US:en' LC_ALL='en_US.UTF-8'
ENV TF_DATA_DIR=/terraform-state
RUN apt-get update && apt-get install software-properties-common apt-transport-https locales curl tar git procps libncurses-dev --reinstall ca-certificates -y && \
	locale-gen en_US.utf8 && locale -a  && \
	update-locale LANG=en_US.UTF-8 LANG=en_US.UTF8 LC_CTYPE=en_US.UTF8 LC_COLLATE=en_US.UTF8 && \
	curl -sL https://aka.ms/InstallAzureCLIDeb | bash && \
	curl -LO "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" && \
    chmod +x ./kubectl && \
    mv ./kubectl /usr/local/bin/kubectl  && \
    kubectl version --client  && \
	cd /tmp  && curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 && \
	chmod 700 get_helm.sh && \
	bash get_helm.sh && \
	curl -fsSL https://apt.releases.hashicorp.com/gpg | apt-key add -  && \
	echo "deb [arch=amd64] https://apt.releases.hashicorp.com focal main" | tee /etc/apt/sources.list && \
	apt-get update && apt-get install terraform=1.2.4 -y
WORKDIR /
ENTRYPOINT ["bash"]