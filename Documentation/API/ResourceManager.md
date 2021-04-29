# Resource Manager API

This API deals with all the resource creation required for Wolfram Application Server. The resource encapsulates metadata that specifies how it is to be evaluated by the active web element server application and a reference to the stored source data that may be used to access it.

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

This API uses a `ResourceInfo` JSON model for creation of resources. This model contains information like resourcePath, poolName, timeout and content type etc. Use of each parameter is described below:

* **resourcePath** (required, string) : This path uniquely identify particular resource. This resourcePath is the name of the file name we are going to upload in the shared storage system. For example, if we are uploading a file named `add.wl` then resourcePath is `add.wl` which is the unique key in the shared storage system.

* **resourceType** (required, string): This field accepts values, `Active `, `MSP` and `Static `. The value `Active ` is used to evaluate active web elements which include `APIFunction`, `FormFunction` and `AskFunction` etc. The value `MSP ` is used for evaluating all MSPs. The value `Static` is used for raw asset files that are not evaluated, e.g. HTML files, CSS files, images, etc. This field cannot be null.
* **mimeType** (optional, string): This field used to specify the mime type of the contents send as part of the request body for the POST API call. The value will be `Automatic` by default.
* **poolName** (required, string): This field is required for MSP requests and the value should be set to `MSP`. For all other resource, the poolName is `Public`, and if we don't specify any value, the 'default' pool will be used.
* **timeout** (optional): The number of seconds to wait for a resource to respond. The default timeout is 30 seconds, which can be specified with the value `null`.

## Resources [/resources]

### GET

Retrieves all the resource created. The API returns list of ResourceInfo objects along with resourcePath in a JSON format.

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

Posts a new resource to the shared location. The Content-Type of this request is `multipart/form-data`. We should upload a file using `resource` parameter and provide file meta information in the `resourceInfo` parameter, which should an JSON format object converted into string. The API returns the resource path of the newly uploaded resource.

* Request

 		POST /resources
 	Example:

		POST "http://wasresourcemanager.wolfram.com/resources"
* Parameters

	* resource(required, file): This parameter use to upload input file which will get stored in the shared storage

	 	Example:

	 		name="resource"; filename="add.wl"

	* resourceInfo(required, string): This parameter has the meta data about the resource. The value should be provided in JSON format.

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
* If we provide invalid JSON format in the requestInfo parameter, Response 400 Bad Request (application/json)

		[{
    		"timestamp": "2019-02-08T18:29:49.597+0000",
   			 "status": 400,
    		"error": "Bad Request",
    		"message": "Expected the request body in JSON format."
		}]

* If we provide invalid `resourceType`, Response 400 Bad Request (application/json):

	 	[{
    		"timestamp": "2019-02-06T17:53:50.227+0000",
    		"status": 400,
    		"error": "Bad Request",
    		"message": "The resourceType is not one of Active, MSP, or Static."
		}]

* If the `resourceType` is null, Response 400 (application/json)

		[ {
    		"timestamp": "2019-02-06T18:20:52.052+0000",
   			"status": 400,
    		"error": "Bad Request",
    		"message": "The resourceType is not one of Active, MSP, or Static.",
    		"path": "/resources"
		}]

## Resources [/resources/contents/{path}]
### GET
Gets the actual resource. If we wanted to download the actual resource we can send resource path as input parameter, the API will return the source file.
* Parameter
	* resourcePath(String) : This path uniquely identify a resource in the shared storage
* Request

		GET /resource/contents/{path}
	Example:

		GET "http://wasresourcemanager.wolfram.com/resources/contents/add.wl"
The response Content-Type corresponds to the file MIME type, and the response body is the raw file content.
* Response 200
* If the file not exist. Response 404 Not Found (application/json)

		[{
    		"timestamp": "2019-02-06T18:32:17.516+0000",
    		"status": 404,
    		"error": "Not Found",
    		"message": "Unknown resource",
    		"path": "/resources/contents/add.wl"
		}]
## Resources [/resources/metadata/{path}]
### GET

Gets a specific resource meta information. The API will take resource path as input parameter and return ResourceInfo object as JSON format.
* Parameter
	* resourcePath(String) : This path uniquely identify a resource in the shared storage

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
* If the `resourcePath` not exist, Response 404 Not Found (application/json)

		[{
    		"timestamp": "2019-02-06T18:32:17.516+0000",
    		"status": 404,
    		"error": "Not Found",
    		"message": "Unknown resource",
    		"path": "/resources/metadata/add.wl"
		}]

## Resources [/resources/{path}]
### PUT
Updates an existing resource. In order to update we need to provide resource path as input parameter. The Content-Type of this request is `multipart/form-data`. Upload a new file to update the file, using `resource` parameter and provide the updated file meta information in the `resourceInfo` parameter, which should be a JSON format object converted into string. We cannot modify `resourcePath` inside the `resourceInfo`. The API returns response status Accepted. To view the updated changes call, GET `/resources/{path}`.

* Parameter
	* resourcePath (String) : This path uniquely identify a resource in the shared storage
	* resource (required, file): This parameter use to upload input file which will get stored in the shared storage

	 	Example:

	 		name="resource"; filename="add.wl"

	* resourceInfo(required, string): This parameter has the meta data about the resource. The value should be provided in JSON format.

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

* If the `resourcePath` not exist, Response 404 Not Found (application/json)

		[{
    		"timestamp": "2019-02-06T18:32:17.516+0000",
    		"status": 404,
    		"error": "Not Found",
    		"message": "Unknown resource",
    		"path": "/resources/addOne.wl"
		}]
* If we try to modify  `resourcePath`, Response 404 Bad Request (application/json)

		[{
    		"timestamp": "2019-06-17T13:42:52.846+000",
    		"status": 400,
    		"error": "Bad Request",
    		"message": "resourcePath cannot be modified",
    		"path": "/resources/addOne.wl"
		}]

### DELETE

Deletes an existing resource. Provide the resource path in the request parameter. After deletion the API returns response status Accepted. To verify, call, GET `/resources/{path}` .

* Parameter
	* resourcePath(String) : This path uniquely identify a resource in the shared storage
* Request

		DELETE /resources/{path}
	Example:

		DELETE "http://wasresourcemanager.wolfram.com/resources/add.wl"
* Response 202 Accepted

### PATCH

Patches an existing resource. Provide resource path in the input parameter.The Content-Type of this request is `multipart/form-data`. We should upload a new file if we are updating the file, using `resource` parameter . If we are updating file meta information update the particular field in the `resourceInfo` parameter, which should be a JSON format object converted into string. We cannot modify `resourcePath` inside the `resourceInfo`. The API returns response status Accepted. To view the updated changes call, GET `/resources/{path}`.

* Parameter
	* resourcePath(String) : This path uniquely identify a resource in the shared storage
	* resource(optional, file): This parameter use to upload input file which will get stored in the shared storage

	 	Example:

	 		name="resource"; filename="add.wl"

	* resourceInfo(optional, string): This parameter has the meta data about the resource. The value should be provided in JSON format.

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

* If the `resourcePath` not exist, Response 404 Not Found (application/json)

		[{
    		"timestamp": "2018-12-11T18:32:17.516+0000",
    		"status": 404,
    		"error": "Not Found",
    		"message": "Unknown resource",
    		"path": "/resources/add.wl"
		}]

* If we try to modify  `resourcePath`, Response 404 Bad Request (application/json)

		[{
    		"timestamp": "2019-06-17T13:42:52.846+000",
    		"status": 400,
    		"error": "Bad Request",
    		"message": "resourcePath cannot be modified",
    		"path": "/resources/add.wl"
		}]

## Resource Health Check [/resources/.applicationserver/info]

### GET

Retrieves information for the resource manager and provides a way to confirm that the resource manager is running.

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
