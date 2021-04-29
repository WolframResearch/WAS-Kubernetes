# Azure AKS WAS Setup

## Introduction

This document describes the setup of Azure Kubernetes Service (AKS) and Wolfram Application Server (WAS).


## Pre-Requisite Tools

The following CLI tools are required to be installed on your local machine to complete the setup and installation:

* **Azure CLI** - https://docs.microsoft.com/en-us/cli/azure/install-azure-cli?view=azure-cli-latest

* **Kubectl** - https://kubernetes.io/docs/tasks/tools/install-kubectl/

* **Docker v20.10 or newer** - https://docs.docker.com/get-docker/

* **Docker Compose  v1.28.6 or newer** - https://docs.docker.com/compose/install/

### Default Configuration

* Cluster Name: WAS
* Region: eastus
* AMI Instance Type: Standard_D8s_v3
* Disk Size: 30GB
* Node Group scaling configuration: [Minimum size: 2, Maximum size: 10, Desired size: 2]
* Kubernetes Version: 1.19.7

To change any of the above defaults open `Source/terraform/variables.tf` and modify accordingly and save file.



## First Time Setup

**Prerequisite:** Make sure your Azure subscription has a minimum `total region CPU` value of 40 or higher to support the default configuration. See the following for more information on managing your Azure quotas: [https://docs.microsoft.com/en-us/azure/azure-portal/supportability/regional-quota-requests](https://docs.microsoft.com/en-us/azure/azure-portal/supportability/regional-quota-requests).

Once your total region CPU of 40 or higher is confirmed available, authenticate with Azure:

	az login --tenant <your-tenant-id>


**Note:** Your Tenant ID can be fetched from [https://portal.azure.com/#blade/Microsoft_AAD_IAM/ActiveDirectoryMenuBlade/Overview](https://portal.azure.com/#blade/Microsoft_AAD_IAM/ActiveDirectoryMenuBlade/Overview)

## Setup

**Step 1.** Checkout the repository:

	git clone https://github.com/WolframResearch/WAS-Kubernetes.git

**Step 2.** Change directory to Azure:

	cd WAS-Kubernetes/EnvironmentSetup/Azure/

**Step 3.** Replace the tag with your tenant ID and run the following command to set up AKS and deploy WAS:

	YOURTENANTID=<your-tenant-id> && rm -rf Source/terraform/tenant-id.config && echo $YOURTENANTID >> Source/terraform/tenant-id.config && mkdir -p ~/.kube && docker-compose up --build -d && clear && docker exec -it azure-setup-manager bash setup --create && sudo chown -R $USER ~/.kube

Example:

	YOURTENANTID=QQQQQQQ-9810-46e1-bef2-hswqw56sf && rm -rf Source/terraform/tenant-id.config && echo $YOURTENANTID >> Source/terraform/tenant-id.config && mkdir -p ~/.kube && docker-compose up --build -d && clear && docker exec -it azure-setup-manager bash setup --create && sudo chown -R $USER ~/.kube


**Note:** This can take approximately 25 minutes to complete.


**Step 4.** Run the following command to retrieve your base URL and application URLs:

	docker-compose up --build -d && clear && docker exec -it azure-setup-manager bash setup --endpoint-info


The output of this command will follow this pattern:
	
	Base URL - Active Web Elements Server: http://<your-base-url>/
	
	Resource Manager: http://<your-base-url>/resources/
	
	Endpoints Manager: http://<your-base-url>/endpoints/
	
	Nodefiles: http://<your-base-url>/nodefiles/
	
	Endpoints Info: http://<your-base-url>/.applicationserver/info
	
	Restart AWES: http://<your-base-url>/.applicationserver/kernel/restart



**Step 5.** After completion, run this command to shutdown the azure-setup-manager:

	docker-compose down


**Step 6.** Get a license file from your Wolfram Research sales representative.


**Step 7.** This file needs to be deployed to WAS as a node file in the conventional location `.Wolfram/Licensing/mathpass`. From a Wolfram Language client, this may be achieved using the following code: 

    was = ServiceConnect["WolframApplicationServer", "http://<your-base-url>"];
	ServiceExecute[was, "DeployNodeFile",
	{"Contents"-> File["/path/to/mathpass"], "NodeFile" -> ".Wolfram/Licensing/mathpass"}]

Alternatively you may use the [node files REST API](../../Documentation/API/NodeFilesManager.md) to install the license file.

**Note:** In order to use the Wolfram Language functions, the WolframApplicationServer paclet must be installed and loaded. Run the following code:

    PacletInstall["WolframApplicationServer"];
    Needs["WolframApplicationServer`"]


**Step 8.** Restart the application using the [restart API](../../Documentation/API/Utilities.md) to enable your Wolfram Engines.

URL: `http://<your-base-url>/.applicationserver/kernel/restart`
	
The default credentials for this API are: 
	
	Username: applicationserver
	
	Password: P7g[/Y8v?KR}#YvN


To change these, see the [configuration documentation](../../Configuration.md).

**Note:** Active Web Elements Server will restart and activate using the mathpass. Upon successful activation, the application shall start. 

Your setup is now complete.

------------------------------------------------------

## Remove the cluster

The following completely deletes everything including the kubernetes cluster, Wolfram Application Server and all resources:

**Step 1.** Change your directory to the directory containing `docker-compose.yml` directory and run the following command to destroy your AKS cluster and WAS:

	docker-compose up --build -d && clear && docker exec -it azure-setup-manager bash setup --delete

**Warning:** All data will be destroyed.

**Step 2.**  After completion, shutdown the azure-setup-manager by running the following command:

	docker-compose down -v
