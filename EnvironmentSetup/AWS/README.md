# Amazon EKS WAS Setup

## Introduction

This document describes the setup of Amazon Kubernetes (EKS) and Wolfram Application Server (WAS).


## Prerequisite Tools

The following CLI tools are required to be installed on your local machine to complete the setup and installation:

* **AWS CLIv2** - https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html#getting-started-install-instructions

* **Kubectl >= 1.24** - https://kubernetes.io/docs/tasks/tools/install-kubectl/

* **Docker v20.10 or newer** - https://docs.docker.com/get-docker/

* **Docker Compose  v1.28.6 or newer** - https://docs.docker.com/compose/install/


### Default Configuration
The automated configuration tool will use the following default values when building EKS and configuring WAS.

* Cluster Name: WAS
* Region: us-east-1
* AMI Instance Type: c5.2xlarge
* Disk Size: 30GB
* Node Group scaling configuration: [Minimum size: 2, Maximum size: 10, Desired size: 2]
* Kubernetes Version: 1.22

To change any of the above defaults open `Source/terraform/variables.tf`, modify accordingly and save file.


## First Time Setup

**Prerequisite:** Obtain an AWS IAM User with administrator priviledges, access key and secret key.

To configure the AWS CLI run the following command:

	aws configure

This will interactively prompt for your AWS IAM user access key, secret key and preferred region. 

**Note:** Your region needs to match the above default configuration else the setup will fail.

## Setup

**Step 1.** Checkout the repository:

	git clone https://github.com/WolframResearch/WAS-Kubernetes.git

**Step 2.** Change directory to AWS:

	cd WAS-Kubernetes/EnvironmentSetup/AWS/

**Step 3.** Run the following command to set up EKS and deploy WAS:

	mkdir -p ~/.kube && docker-compose up --build -d && clear && docker exec -it aws-setup-manager bash setup --create && sudo chown -R $USER ~/.kube

**Note:** This can take approximately 45 minutes to complete.


**Step 4.** Run the following command to retrieve your base URL and application URLs:

	docker-compose up --build -d && clear && docker exec -it aws-setup-manager bash setup --endpoint-info


The output of this command will follow this pattern:
	
	Base URL - Active Web Elements Server: http://<your-base-url>/
	
	Resource Manager: http://<your-base-url>/resources/
	
	Endpoints Manager: http://<your-base-url>/endpoints/
	
	Nodefiles: http://<your-base-url>/nodefiles/
	
	Endpoints Info: http://<your-base-url>/.applicationserver/info
	
	Restart AWES: http://<your-base-url>/.applicationserver/kernel/restart



**Step 5.** After completion, run this command to shutdown the aws-setup-manager:

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


## Remove the cluster

The following completely deletes everything including the kubernetes cluster, Wolfram Application Server and all resources:

**Step 1.** Update the `terraform/variables.tf` file with your WAS cluster info(aws_region, cluster name etc.)

**Step 2.** Change your directory to the directory containing `docker-compose.yml` directory and run the following command to destroy your EKS cluster and WAS:

	docker-compose up --build -d && clear && docker exec -it aws-setup-manager bash setup --delete

**Warning:** All data will be destroyed.

**Step 2.** After completion, shutdown the aws-setup-manager by running the following command:

	docker-compose down	-v

---

## Troubleshooting

**1.** Backend configuration is changed error.
```
Building EKS - (can take upto 30 minutes) [ERROR] Failed with error: 1
[ ✘ ]

[ERROR] Something went wrong. Exiting.
[ERROR] The last few log entries were:
╷
│ Error: Backend configuration changed
│
│ A change in the backend configuration has been detected, which may require
│ migrating existing state.
│
│ If you wish to attempt automatic migration of the state, use “terraform
│ init -migrate-state”.
│ If you wish to store the current configuration with no changes to the
│ state, use “terraform init -reconfigure”.
```

You need to check these bullet-points.

* Check S3/DynamoDb for previous WAS setup states. If any of them exists, remove it manually on AWS Console.

  

* Remove docker container cache.

  * Stop the running `aws-setup-manager` container with `docker kill <CONTAINER_ID>`
  * `docker container prune -f`
  * `docker volume prune -a -f` or `docker volume prune -f` , depends on the docker version.
