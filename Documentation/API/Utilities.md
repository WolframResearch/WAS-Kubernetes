# WAS Utility APIs

These APIs provide general information about the Wolfram Application Server instance and offer administrative services.

## Information [.applicationserver/info]

### Retrieve Server Information [GET]

Use this to retrieve general information about the Wolfram Application Server cluster.

* Request

		GET /.applicationserver/info
	Example:

		GET "http://applicationserver.wolfram.com/.applicationserver/info"

* Response 200 (application/json)

    * Example:
 
			{
				"resourceManager": "http://resources.applicationserver.wolfram.com",
				"endpointManager": "http://endpoints.applicationserver.wolfram.com",
				"nodefileManager": "http://nodefiles.applicationserver.wolfram.com",
				"canonicalBaseURL": "http://applicationserver.wolfram.com",
				"restartURL": "http://applicationserver.wolfram.com/.applicationserver/kernel/restart",
				"wasVersion": "3.0",
				"wolframEngineVersion": "13."
			}

## Restart Container [GET]

### Restart [.applicationserver/kernel/restart]

Use this to initiate a rolling restart of the Active Web Element Server instances (other services will not be affected). The API uses basic authentication and requires a username and password (set during cluster initiation). It returns a success message string.

* Request

		GET /.applicationserver/kernel/restart
	Example:

		GET "http://applicationserver.wolfram.com/.applicationserver/kernel/restart"

* Response 200 OK

    * Example:
    		
    		Container restarted successfully!