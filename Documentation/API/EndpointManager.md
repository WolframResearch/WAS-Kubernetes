# Endpoint Manager API

This API covers the resource endpoint lifecycle required for making resources publicly accessible. Using this API we can create, modify and remove one or multiple endpoints for a particular resource.

## General notes and definitions
### API Model
* Model (application/json)

		[ EndpointInfo{
				endpointPath : string
				resourcePath : string
			} ]

This API uses the `EndpointInfo` JSON model for creation of the endpoint and to communicate information about the endpoint. The model contains the endpointPath and the resourcePath parameters.

Use of each parameter is described below:

* endpointPath (required, string): This path uniquely identifies an endpoint which is publicly accessible from the active web elements server. If it is a hierarchical endpoint path (with a directory structure), it should be specified without a leading slash. For example, if under the directory `test` we have endpoint `add`, the path should be written `test/add`.
* resourcePath (required, string): This path uniquely identifies a resource that should be accessible though the resource manager.

## Endpoints [/endpoints]

### GET

Use this to retrieve the complete set of endpoints created. The API returns list of EndpointInfo objects in JSON format.

* Request

		GET /endpoints
	Example:

		GET "http://applicationserver.wolfram.com/endpoints"
* Response 200 (application/json):

	Example:

		{
  			"add" : {
				endpointPath : "add"
				resourcePath : "api/add.wl"
			}
			"country" : {
				endpointPath : "country"
				resourcePath : "api/country.wl"
			}
  		}


### POST

Use this to create or modify an endpoint for a specified resource file. The EndpointInfo object should be provided in the request body as JSON. The API returns the endpointPath of the newly created endpoint.

* Request

 		POST /endpoints
 	Example:

		POST "http://applicationserver.wolfram.com/endpoints"
 	Request body example (application/json):

 		{
 			"endpointPath":"add"
 			"resourcePath":"api/add.wl"
		}


* Response 201 Created (application/json)

		{
			endpointPath : "add"
		}
* If the resource referenced in resourcePath does not exist: Response 201 Created (application/json)

		{
			endpointPath : "add",
			warning: "no resource found at api/add.wl; users accessing this endpoint may receive a 404 Not Found error."
		}
* If the endpointPath is null or empty: Response 400 Bad Request (application/json)

  		[{
  			"timestamp": "2019-07-16T18:32:17.516+0000",
  			"status": 400,
  			"error": "Bad Request",
  			"message": "endpointPath cannot be null or empty",
  			"path": "/endpoints"
  		}]

* If the resourcePath is null or empty: Response 400 Bad Request (application/json)

  		[{
  			"timestamp": "2019-07-16T18:32:17.516+0000",
  			"status": 400,
  			"error": "Bad Request",
  			"message": "resourcePath cannot be null or empty",
  			"path": "/endpoints"
  		}]

**Note:** Wolfram Application Server reserves a small number of endpointPath paths for internal usage. Attempting to assign a resource to one those paths will generate a 403 Forbidden error response.

* Reserved endpointPath:
	
		.applicationserver/kernel/restart

* If the endpointPath is a reserved path: Response 403 Forbidden (application/json)

  		[{
  			"timestamp": "2020-04-30T19:16:50.965+0000",
  			"status": 403,
  			"error": "Forbidden",
  			"message": "This endpointPath is reserved for internal purpose"
  		}]

## Endpoints [/endpoints/{path}]

### GET

Use this to get information about a specific endpoint. The API takes an endpointPath as a path variable and returns an EndpointInfo object in JSON format.
* Parameter
	* endpointPath (String) : This path uniquely identify an endpoint.

* Request

		GET /endpoints/{path}

 	Example:

 		GET "http://applicationserver.wolfram.com/endpoints/add"
* Response 200 (application/json)

		{
    		"endpointPath": "add",
    		"resourcePath": "api/add.wl"

		}
* If the endpointPath not exist: Response 404 Not Found (application/json)

  		[{
  			"timestamp": "2019-05-16T18:32:17.516+0000",
  			"status": 404,
  			"error": "Not Found",
  			"message": "Unknown endpoint",
  			"path": "/endpoints/add"
  		}]

### DELETE

Use this to delete an existing endpoint. The API takes the path of the endpoint as a path variable and returns nothing.

* Parameter
	* endpointPath (String) : This path uniquely identifies an endpoint.
* Request

		DELETE /endpoints/{path}
	Example:

		DELETE "http://applicationserver.wolfram.com/endpoints/add"
* Response 202 Accepted

## Endpoint Health Check [/endpoints/.applicationserver/info]

### GET

Use this to retrieve information about the endpoint manager. The API may be used to confirm that the endpoint manager is running.

* Request

		GET /endpoints/.applicationserver/info
	Example:

		GET "http://applicationserver.wolfram.com/endpoints/.applicationserver/info"
* Response 200 (application/json):

	Example:

		{
  			"name": "endpoint-manager",
			"version": "1.0.0"
  		}
