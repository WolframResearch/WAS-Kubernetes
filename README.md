# Getting Started

The [Wolfram Application Server (WAS)](https://www.wolfram.com/application-server/) combines the computational power of the Wolfram Engine with the robust containerization technologies available today. It provides a scalable deployment model for your Wolfram-powered web applications. The documentation provided here along with the appropriate license will get you started in no time.

In order to deploy and use Wolfram Language content, you will need a license file provided by Wolfram Research. Contact your sales representative at [1-800-WOLFRAM](tel:18009653726) to discuss licensing options. 

The Wolfram Application Server runs within Kubernetes. You need to choose your Kubernetes environment. We support deploying the Wolfram Application Server onto Amazon, Azure and your on-premises cluster. 

## Amazon
Instantiate a cluster in Amazon EKS, check out the following repository (EnvironmentSetup/AWS) and follow the instructions in [README.md](./EnvironmentSetup/AWS/README.md). 

## Azure
Instantiate a cluster in Azure, check out the following repository (EnvironmentSetup/Azure) and follow the instructions in [README.md](./EnvironmentSetup/Azure/README.md). 

## On-premises
Contact Wolfram Technical Support for options and documentation.

# Activation
Obtain a license file from your sales representative. This file needs to be deployed to the WAS as a node file in the conventional location `.Wolfram/Licensing/mathpass`. From a Wolfram Language client, this may be achieved using the following code: 

    was = ServiceConnect["WolframApplicationServer", "http://<your-base-url>"];
	ServiceExecute[was, "DeployNodeFile",
	{"Contents"-> File["path/to/mathpass"], "NodeFile" -> ".Wolfram/Licensing/mathpass"}]


Alternatively you may use the [node files REST API](Documentation/API/NodeFilesManager.md) to install the license file.

Restart the application using the [restart API](Documentation/API/Utilities.md) to enable your Wolfram Engines.

# Development
In your Wolfram Language environment, evaluate `PacletInstall["WolframApplicationServer"]`. The guide page contains documentation links to Wolfram Application Server functions (WolframApplicationServer/guide/WolframApplicationServer). The service page describes the details of a `ServiceConnection` to a Wolfram Application Server (WolframApplicationServer/ref/service/WolframApplicationServer).

# Additional Documentation
## API Specifications
* [Utilities.md](Documentation/API/Utilities.md)
* [ResourceManager.md](Documentation/API/ResourceManager.md)
* [NodeFilesManager.md](Documentation/API/NodeFilesManager.md)
* [EndpointManager.md](Documentation/API/EndpointManager.md)

## Other
* [WolframApplicationServerArchitecture.md](Documentation/Architecture/WolframApplicationServerArchitecture.md)
* [Configuration.md](./Configuration.md)
