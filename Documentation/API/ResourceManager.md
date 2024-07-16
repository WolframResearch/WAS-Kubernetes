# Resource Manager API

This API covers resource lifecycle required for deploying public content. A resource encapsulates code to evaluate or content to display along with metadata which specifies how it is to be interpreted by the Active Web Elements server application. Using this API we can create, configure, modify, and delete user facing content.

## General notes and definitions
### API Model
* Model (application/json)

		ResourceInfo: {
				resourcePath : string
				resourceType  : string Enum: Array [ 2 ]
				mimeType : string
				poolName : string
				timeout : integer
			}

This API uses a `ResourceInfo` JSON model for the specification of resources. This model contains information like resourcePath, poolName, timeout and content type etc. Use of each parameter is described below:

* **resourcePath** (required, string) : This path uniquely identifies a deployed resource.
* **resourceType** (required, string): This field specifies how the resource will be interpreted by the Wolfram Web Engine. It accepts values from an enumeration: `Active `, `MSP` and `Static `. The value `Active ` is used when deploying Active Web Elements such as `APIFunction`, `FormFunction` and `AskFunction`, etc. The value `MSP ` is used when deploying legacy MSP content. The value `Static` is used for raw asset files which are not processed before serving, e.g. HTML files, CSS files, images, etc. This field cannot be null.
* **mimeType** (optional, string): This field specifies the mime type of the contents to be uploaded. The value will be `Automatic` by default.
* **poolName** (required, string): This field names the kernel pool to be used to process a request for the resource. The 'default' pool is `Public` which is configured to handle Active Web Element content. Legacy MSP resources must use the pool `MSP` or a custom pool configured specifically for this type of content (and not any pool configured for handling Active Web Elements). This field is ignored for static resources.
* **timeout** (optional): This field specifies the maximum number of seconds to wait to process a resource. The default timeout is 30 seconds, which can be specified with the value `null`.

## Resources [/resources]

### GET

Use this to retrieve information on all deployed resources. The API returns list of ResourceInfo objects along with the resource path in a JSON format.

* Request

		GET /resources
	Example:

		GET "http://wasresourcemanager.wolfram.com/resources"
* Response 200 (application/json):

	Example:

		{
  			"add.wl": {
  				"resourcePath": "add.wl",
   	 			"resourceType": "Active",
    			"mimeType": "Automatic"
    			"poolName": "Public",
    			"timeout": null
  			}
  			....
  		}


### POST

Use this to deploy a new resource. The Content-Type of this request is `multipart/form-data`. Name the file to upload using `resource` parameter and provide file meta information in the `resourceInfo` parameter as a JSON object converted into string. The API returns the resource path of the newly uploaded resource.

* Request

 		POST /resources
 	Example:

		POST "http://wasresourcemanager.wolfram.com/resources"
* Parameters

	* resource(required, file): This parameter specifies a local file to upload that will be deployed as the resource source.

	 	Example:

	 		name="resource"; filename="add.wl"

	* resourceInfo(required, string): This parameter provides the meta data describing the resource. The value should be provided in JSON format.

		Example:

	 		resourceInfo: {
  				"resourcePath": "add.wl",
   	 			"resourceType": "Active",
    			"mimeType": "Automatic",
    			"poolName": "Public",
    			"timeout": null
  			}

 		Request body example (multipart/form-data):


 			POST /resources HTTP/1.1
			Host: http://wasresourcemanager.wolfram.com
			Cache-Control: no-cache
			Content-Type: multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW

			------WebKitFormBoundary7MA4YWxkTrZu0gW
			Content-Disposition: form-data; name="resource"; filename="add.wl"
			fileContent:Plus[5,x]
			Content-Type:


			------WebKitFormBoundary7MA4YWxkTrZu0gW
			Content-Disposition: form-data; name="resourceInfo"

			{
 				"resourcePath": "add.wl",
   	 			"resourceType": "Active",
    			"mimeType": "Automatic",
    			"poolName": "Public",
    			"timeout": null"
			}
			------WebKitFormBoundary7MA4YWxkTrZu0gW--


* Response 201 Created (application/json)

		{
			"resourcePath": "add.wl"
		}
* If the JSON provided in the requestInfo parameter is invalid: Response 400 Bad Request (application/json)

		[{
    		"timestamp": "2019-02-08T18:29:49.597+0000",
   			 "status": 400,
    		"error": "Bad Request",
    		"message": "Expected the request body in JSON format."
		}]

* If the `resourceType` provided is invalid: Response 400 Bad Request (application/json):

	 	[{
    		"timestamp": "2019-02-06T17:53:50.227+0000",
    		"status": 400,
    		"error": "Bad Request",
    		"message": "The resourceType is not one of Active, MSP, or Static."
		}]

* If the `resourceType` is null: Response 400 (application/json)

		[ {
    		"timestamp": "2019-02-06T18:20:52.052+0000",
   			"status": 400,
    		"error": "Bad Request",
    		"message": "The resourceType is not one of Active, MSP, or Static.",
    		"path": "/resources"
		}]

## Resources [/resources/contents/{path}]
### GET
Use this to get the resource source. The API takes resource path as a path parameter and returns the source.
* Parameter
	* resourcePath(String) : This path uniquely identifies a resource.
* Request

		GET /resource/contents/{path}
	Example:

		GET "http://wasresourcemanager.wolfram.com/resources/contents/add.wl"
The response Content-Type corresponds to the source MIME type, and the response body is the raw source content.
* Response 200
* If the resource does not exist: Response 404 Not Found (application/json)

		[{
    		"timestamp": "2019-02-06T18:32:17.516+0000",
    		"status": 404,
    		"error": "Not Found",
    		"message": "Unknown resource",
    		"path": "/resources/contents/add.wl"
		}]
## Resources [/resources/metadata/{path}]
### GET

Use this to get meta information concerning a specific resource. The API takes resource path as path variable and returns a ResourceInfo object in JSON format.
* Parameter
	* resourcePath(String) : This path uniquely identifies a resource.

* Request

 		GET /resources/metadata/{path}

 	Example:

 		GET "http://wasresourcemanager.wolfram.com/resources/metadata/add.wl"

* Response 200 (application/json)

		{
    		"resourcePath": "add.wl",
    		"poolName": "Public",
    		"resourceType": "Active",
    		"timeout": null,
    		"mimeType": "Automatic"
		}
* If the resource does not exist: Response 404 Not Found (application/json)

		[{
    		"timestamp": "2019-02-06T18:32:17.516+0000",
    		"status": 404,
    		"error": "Not Found",
    		"message": "Unknown resource",
    		"path": "/resources/metadata/add.wl"
		}]

## Resources [/resources/{path}]
### PUT
Use this to replace an existing resource. The API takes the resource path as input parameter. The Content-Type of this request is `multipart/form-data`. Name the file to upload as the new resource source using the `resource` parameter and provide the updated resource meta information using the `resourceInfo` parameter as a JSON object converted into string. The`resourcePath` field inside the `resourceInfo` is required and must match the resource path given in the path variable. The API returns nothing.

* Parameter
	* resourcePath (String) : This path uniquely identifies a resource in the shared storage.
	* resource (required, file): This parameter specifies a local file to upload that will be deployed as the resource source.

	 	Example:

	 		name="resource"; filename="add.wl"

	* resourceInfo(required, string): This parameter provides the meta data about the resource. The value should be given in JSON format.

		Example:

	 		resourceInfo: {
  				"resourcePath": "add.wl",
   	 			"resourceType": "Active",
    			"mimeType": "Automatic",
    			"poolName": "Public",
    			"timeout": null
  			}
* Request

		PUT /resources/{path}
	Example:

		PUT "http://wasresourcemanager.wolfram.com/resources/add.wl"

	Request body example (multipart/form-data):


 			POST /resources HTTP/1.1
			Host: http://wasresourcemanager.wolfram.com
			Cache-Control: no-cache
			Content-Type: multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW

			------WebKitFormBoundary7MA4YWxkTrZu0gW
			Content-Disposition: form-data; name="resource"; filename="addOne.wl" fileContent : Plus[3,x]
			Content-Type:


			------WebKitFormBoundary7MA4YWxkTrZu0gW
			Content-Disposition: form-data; name="resourceInfo"

			{
 				"resourcePath": "add.wl",
   	 			"resourceType": "Active",
    			"mimeType": "Automatic",
    			"poolName": "Public",
    			"timeout": null"
			}
			------WebKitFormBoundary7MA4YWxkTrZu0gW--


	In the above example `filename` field is updated.

* Response 202 Accepted

* If the `resourcePath` does not exist: Response 404 Not Found (application/json)

		[{
    		"timestamp": "2019-02-06T18:32:17.516+0000",
    		"status": 404,
    		"error": "Not Found",
    		"message": "Unknown resource",
    		"path": "/resources/addOne.wl"
		}]
* If the `resourcePath` does not match the path variable: Response 400 Bad Request (application/json)

		[{
    		"timestamp": "2019-06-17T13:42:52.846+000",
    		"status": 400,
    		"error": "Bad Request",
    		"message": "resourcePath cannot be modified",
    		"path": "/resources/addOne.wl"
		}]

### DELETE

Use this to delete an existing resource. The api takes the resource path as a path variable and returns nothing.

* Parameter
	* resourcePath(String) : This path uniquely identifies a resource.
* Request

		DELETE /resources/{path}
	Example:

		DELETE "http://wasresourcemanager.wolfram.com/resources/add.wl"
* Response 202 Accepted

### PATCH

Use this to modify an existing resource. The API takes the resource path as input parameter. The Content-Type of this request is `multipart/form-data`. If the resource source is to be modified, name the file to upload as the new resource source using the `resource` parameter. If the resource meta information is to be updated modify the desired fields using the `resourceInfo` parameter as a JSON object converted into string. Unmodified fields of the ResourceInformation structure may be omitted with the exception of the`resourcePath` field which is required and must match the resource path given in the path variable. The API returns nothing.

* Parameter
	* resourcePath(String) : This path uniquely identifies a resource.
	* resource(optional, file): This parameter specifies a local file to upload that will be deployed as modified the resource source.

	 	Example:

	 		name="resource"; filename="add.wl"

	* resourceInfo(optional, string): This parameter provides the meta data about the resource. The value should be given in JSON format.

		Example:

	 		resourceInfo: {
  				"resourcePath": "add.wl",
   	 			"resourceType": "Active",
    			"mimeType": "Automatic",
    			"poolName": "Public",
    			"timeout": null
  			}
* Request

		PATCH resources/{path}
	Example:

		PATCH "http://wasresourcemanager.wolfram.com/resources/add.wl"

	Request body example (multipart/form-data):


 			POST /resources HTTP/1.1
			Host: http://wasresourcemanager.wolfram.com
			Cache-Control: no-cache
			Content-Type: multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW

			------WebKitFormBoundary7MA4YWxkTrZu0gW
			Content-Disposition: form-data; name="resource"; filename="addNew.wl" fileContent : Plus[3,x]
			------WebKitFormBoundary7MA4YWxkTrZu0gW--

	In the above example source `fileName` field is modified for the resourcePath `add.wl`.

* Response 202 Accepted

* If the resource does not exist: Response 404 Not Found (application/json)

		[{
    		"timestamp": "2018-12-11T18:32:17.516+0000",
    		"status": 404,
    		"error": "Not Found",
    		"message": "Unknown resource",
    		"path": "/resources/add.wl"
		}]

* If the `resourcePath` does not match the path variable: Response 400 Bad Request (application/json)

		[{
    		"timestamp": "2019-06-17T13:42:52.846+000",
    		"status": 400,
    		"error": "Bad Request",
    		"message": "resourcePath cannot be modified",
    		"path": "/resources/add.wl"
		}]

## Resource Health Check [/resources/.applicationserver/info]

### GET

Use this to retrieve information about the resource manager. The API may be used to confirm that the resource manager is running.

* Request

		GET /resources/.applicationserver/info
	Example:

		GET "http://wasresourcemanager.wolfram.com/resources/.applicationserver/info"
* Response 200 (application/json):

	Example:

		{
  			"name": "resource-manager",
			"version": "1.0.0"
  		}
