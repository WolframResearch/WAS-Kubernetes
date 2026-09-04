# Node Files Manager API

This API covers the node file lifecycle required for deploying content that resides on the node local file system. Using this API we can install, modify, and remove files which may be directly accessed by the Wolfram Engine.

## Node Files [/nodefiles]

### GET

Use this to retrieve a listing of all the resident node files. The API returns list of node file paths along with the nodeFileName and location in JSON format. Node file paths are relative to the configured node files root directory in Active web element server.

* Request

		GET /nodefiles
	Example:

		GET "http://applicationserver.wolfram.com/nodefiles"

* Response 200 (application/json):

	Example:

		{
			"WebPackages/init.m":{
			  "size":"1048576",
			  "hashMD5":"e65a396cca1a0e502d676c20f5f29b21",
			  "uploaded":"2019-04-30 12:40:05.0"
			},
			".Wolfram/Kernel/init.m":{
			  "size":"20971520",
			  "hashMD5":"9bb5c73e11f0731e2de05874b26532d8",
			  "uploaded":"2019-01-15 09:30:15.0"
			},
			"WebPackages/Kernel/kernel.m":{
			  "size":"104857600",
			  "hashMD5":"0cb53ff034bedf7b87bd07c08e8151c7",
			  "uploaded":"2019-10-01 02:45:13.0"
			}
		}

### POST

Use this to create a new node file to the local node files directory. The Content-Type of this request is `multipart/form-data`. The `nodeFile` parameter with the file name should be provided along with the directory path in the `path` parameter (use the value of '/' for files to be placed in the root directory). Once the node file is successfully uploaded the API returns location of the newly created node file. The path parameter combined with node file name uniquely identifies a node file location.

* Request

 		POST /nodefiles
 	Example:

		POST "http://applicationserver.wolfram.com/nodefiles"
* Parameters

	* nodeFile(required, file): This is the file name for the uploaded file.

	 	Example:

	 		name="nodeFile"; filename="init.m"

	* path(required, string): This is the directory in which the uploaded file should be placed.
 		Example:

	 		name="path"; value=".Wolfram/Kernel"

* Response 201 Created (application/json)

	 Example:

 		{
  			"location": "WebPackages/Kernel/init.m"
  		}
* If the file already exists: Response 400 Bad Request (application/json)

		[{
 			"timestamp": "2019-08-28T17:11:30.427+0000",
 			"status": 400,
 			"error": "Bad Request",
			"message": "NodeFile already exist at path : .Wolfram/Kernel/init.m",
 			"path": "/nodefiles/"
		}]
		
## Node Files [/nodefiles/{location}]

### GET
Use this to get the contents of a node file. The API takes the location of the node file as a path variable and returns the contents of the node file.

* Parameter
	* location(String) : This location specifies the full path to the node file.
* Request

		GET /nodefiles/{location}

	Example:

		GET "http://applicationserver.wolfram.com/nodefiles/.Wolfram/Kernel/init.m"

The response Content-Type corresponds to the file MIME type, and the response body is the raw file content.

* Response 200

* If the location does not exist: Response 404 Not Found (application/json)

		[{
    		"timestamp": "2019-08-28T17:11:30.427+0000",
    		"status": 404,
    		"error": "Not Found",
    		"message": "Unknown node file",
    		"path": "WebPackages/Kernel/init.m"
		}]

### PUT
Use this to update an existing node file. The API takes the node file location as a path variable and a local file with the source contents to be uploaded specified with the `nodeFile` parameter. The Content-Type of this request is `multipart/form-data`. The API returns nothing.

* Parameter
	* location (String) : This location specifies the full path to the node file.
	* nodeFile (required, file): This is the location of a local file to upload as the replacement contents of the existing node file.

	 	Example:

	 		name="nodeFile"; filename="init.m"
* Request

		PUT /nodefiles/{location}

	Example:

		PUT "http://applicationserver.wolfram.com/nodefiles/.Wolfram/Kernel/init.m"


* Response 202 Accepted

* If the `location` not exist, Response 404 Not Found (application/json)

		[{
    		"timestamp": "2019-08-28T17:11:30.427+0000",
    		"status": 404,
    		"error": "Not Found",
    		"message": "Unknown node file",
    		"path": "WebPackages/Kernel/init.m"
		}]


### DELETE

Use this to delete an existing node file. This API takes the node file location as a path variable and returns nothing.

* Parameter
	* location (String) : This location specifies the full path to the node file.
* Request

		DELETE /nodefiles/{location}
	Example:

		DELETE "http://applicationserver.wolfram.com/nodefiles/.Wolfram/Kernel/init.m"
* Response 202 Accepted

## NodeFile Health Check [/nodefiles/.applicationserver/info]

### GET

Use this to retrieve information about the node files manager. The API may be used to confirm that the endpoint manager is running.

* Request

		GET /nodefiles/.applicationserver/info
	Example:

		GET "http://applicationserver.wolfram.com/nodefiles/.applicationserver/info"
* Response 200 (application/json):

	Example:

		{
  			"name": "nodefile-manager",
			"version": "1.0.0"
  		}
