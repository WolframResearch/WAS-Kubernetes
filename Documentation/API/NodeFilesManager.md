# Node Files Manager API
Node Files Manager component can be used to deploy shared source libraries that may be accessed by multiple resources or other system files used for configuration of components. The Node Files manager coordinates with other components to deliver local instances of node files.

## Node Files [/nodefiles]

### GET

Retrieves all the available node files. The API returns list of node file paths along with the nodeFileName and location in the JSON format. Node file paths are relative to a specific directory in Active web element server.

* Request

		GET /nodefiles
	Example:

		GET "http://nodefilesmanager.wolfram.com/nodefiles"

* Response 200 (application/json):

	Example:

		{
			"WebPackages/init.m":{
			  "size":"1048576",
			  "hashMD5":"e65a396cca1a0e502d676c20f5f29b21",
			  "uploaded":"2019-04-30 12:40:05.0"
			},
			"WebPackages/Kernel/init.m":{
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

Posts a new node file to the shared location. The Content-Type of this request is `multipart/form-data`. We should upload a node file using `nodeFile` parameter and provide node file location in the `path` parameter. Once the node file is successfully uploaded the API returns location of the newly created node file.

* Request

 		POST /nodefiles
 	Example:

		POST "http://nodefilesmanager.wolfram.com/nodefiles"
* Parameters

	* nodeFile(required, file): This parameter use to upload a node file which will get stored in the shared storage

	 	Example:

	 		name="nodeFile"; filename="file.m"

	* path(required, string): This parameter use to specify the relative node file path.
 		Example:

	 		name="path"; value="WebPackages/Kernel/init.m"

* Response 201 Created (application/json)

	 Example:

 		{
  			"location": "WebPackages/Kernel/init.m"
  		}

## Node Files [/nodefiles/{location}]

### GET
Gets the actual node file. If we wanted to download the actual node file we can send location as the input parameter. The API will return the source of the node file.

* Parameter
	* location(String) : This location define the node files directory where node files get stored
* Request

		GET /nodefiles/{location}

	Example:

		GET "http://nodefilesmanager.wolfram.com/nodefiles/WebPackages/Kernel/init.m"

The response Content-Type corresponds to the file MIME type, and the response body is the raw file content.

* Response 200

* If the location not exist, Response 404 Not Found (application/json)

		[{
    		"timestamp": "2019-08-28T17:11:30.427+0000",
    		"status": 404,
    		"error": "Not Found",
    		"message": "Unknown node file",
    		"path": "WebPackages/Kernel/init.m"
		}]

### PUT
Updates an existing node file. Provide the node file location in the request parameter. The Content-Type of this request is `multipart/form-data`. Upload a new node file to update, using `nodeFile` parameter. The API returns response status Accepted. To view the updated file call, GET `/nodefiles/{location}`.

* Parameter
	* location (String) : This location define the file directory where node files get stored
	* nodeFile (required, file): This parameter use to upload new node file which will get stored in the shared storage

	 	Example:

	 		name="nodeFile"; filename="init.m"
* Request

		PUT /nodefiles/{location}

	Example:

		PUT "http://nodefilesmanager.wolfram.com/nodefiles/WebPackages/Kernel/init.m"


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

Deletes an existing node file. Provide the node file location in the request parameter. After deletion the API returns response status Accepted. To verify, call, GET `/nodefiles/{location}` .

* Parameter
	* location (String) : This path define the node file directory where files get stored
* Request

		DELETE /nodefiles/{location}
	Example:

		DELETE "http://nodefilesmanager.wolfram.com/nodefiles/WebPackages/Kernel/init.m"
* Response 202 Accepted

## NodeFile Health Check [/nodefiles/.applicationserver/info]

### GET

Retrieves information for the node file manager and provides a way to confirm that the node file manager is running.

* Request

		GET /nodefiles/.applicationserver/info
	Example:

		GET "http://nodefilesmanager.wolfram.com/nodefiles/.applicationserver/info"
* Response 200 (application/json):

	Example:

		{
  			"name": "nodefile-manager",
			"version": "1.0.0"
  		}
