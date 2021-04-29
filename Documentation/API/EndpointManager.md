# Endpoint Manager API

This API deals with all the resource endpoint creation required for the Wolfram Application Server. Using this API we can create multiple endpoints for a particular resource.

## General notes and definitions
### API Model
* Model (application/json)

		[ EndpointInfo{
				endpointPath : string
				resourcePath : string
			} ]

This API uses the `EndpointInfo` JSON model for creation of the endpoint and to communicate with the user. The model contains endpointPath and the resourcePath information.

Use of each parameter is described below:

* endpointPath (required, string): This path uniquely identifies an endpoint which is publicly accessible from the active web elements server. If it is an endpoint path that lives in a directory, it should be treated without a leading slash. For example, if under the directory `test` we have endpoint `add`, the path can be written `test/add`.
* resourcePath (required, string): This path uniquely identifies a resource that should be accessible though the resource manager.

## Endpoints [/endpoints]

### GET

Use to retrieve all the endpoints created. The API returns list of EndpointInfo objects in a JSON format.

* Request

		GET /endpoints
	Example:

		GET "http://wasendpointmanager.wolfram.com/endpoints"
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

This API creates a new endpoint for a particular resource file. The EndpointInfo object should be provided in the request body as JSON. The API returns endpointPath of the newly created endpoint.

* Request

 		POST /endpoints
 	Example:

		POST "http://wasendpointmanager.wolfram.com/endpoints"
 	Request body example (application/json):

 		{
 			"endpointPath":"add"
 			"resourcePath":"api/add.wl"
		}


* Response 201 Created (application/json)

		{
			endpointPath : "add"
		}
* If the resourcePath not exist, Response 201 Created (application/json)

		{
			endpointPath : "add",
			warning: "no resource found at api/add.wl; users accessing this endpoint may receive a 404 Not Found error."
		}
* If the endpointPath is null or empty, Response 400 Bad Request (application/json)

  		[{
  			"timestamp": "2019-07-16T18:32:17.516+0000",
  			"status": 400,
  			"error": "Bad Request",
  			"message": "endpointPath cannot be null or empty",
  			"path": "/endpoints"
  		}]

* If the resourcePath is null or empty, Response 400 Bad Request (application/json)

  		[{
  			"timestamp": "2019-07-16T18:32:17.516+0000",
  			"status": 400,
  			"error": "Bad Request",
  			"message": "resourcePath cannot be null or empty",
  			"path": "/endpoints"
  		}]

**Note:** This API also has some reserved endpointPath for internal purposes. Users trying to access those endpointPath will receive a 403 Forbidden error.

* Reserved endpointPath:
	
		.applicationserver/kernel/restart

* If the endpointPath is the reserved path, Response 403 Forbidden (application/json)

  		[{
  			"timestamp": "2020-04-30T19:16:50.965+0000",
  			"status": 403,
  			"error": "Forbidden",
  			"message": "This endpointPath is reserved for internal purpose"
  		}]

## Endpoints [/endpoints/{path}]

### GET

Use this to get a specific endpoint information. The API will take endpointPath as an input parameter and return an EndpointInfo object as JSON format.
* Parameter
	* endpointPath (String) : This path uniquely identify an endpoint

* Request

 		GET /endpoints/{path}

 	Example:

 		GET "http://wasendpointmanager.wolfram.com/endpoints/add"
* Response 200 (application/json)

		{
    		"endpointPath": "add",
    		"resourcePath": "api/add.wl"

		}
* If the endpointPath not exist, Response 404 Not Found (application/json)

  		[{
  			"timestamp": "2019-05-16T18:32:17.516+0000",
  			"status": 404,
  			"error": "Not Found",
  			"message": "Unknown endpoint",
  			"path": "/endpoints/add"
  		}]

### DELETE

Use this to delete an existing endpoint. Provide the path of the endpoint in the request parameter. After deletion the API returns response status Accepted. To verify, call, GET `/endpoints/{path}` .

* Parameter
	* endpointPath (String) : This path uniquely identify an endpoint
* Request

		DELETE /endpoints/{path}
	Example:

		DELETE "http://wasendpointmanager.wolfram.com/endpoints/add"
* Response 202 Accepted

## Endpoint Health Check [/endpoints/.applicationserver/info]

### GET

Retrieves information for the endpoint manager and provides a way to confirm that the endpoint manager is running.

* Request

		GET /endpoints/.applicationserver/info
	Example:

		GET "http://wasendpointmanager.wolfram.com/endpoints/.applicationserver/info"
* Response 200 (application/json):

	Example:

		{
  			"name": "endpoint-manager",
			"version": "1.0.0"
  		}
