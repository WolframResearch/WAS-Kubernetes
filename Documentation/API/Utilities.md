# WAS Utility APIs

The purpose of this document is to detail all APIs provided by the Wolfram Application Server which are used in a general informative or administrative purpose.

## Information [.applicationserver/info]

### Retrieve Server Information [GET]

Used to retrieve general information about the WAS instance. This API should be served without any subdomain or extraneous path elements. For the example below, the request URL would be `http://devel.applicationserver.wolfram.com/.applicationserver/info`.

+ Response 200 (application/json)

    + Response
        ```
        {
            "resourceManager": "http://resources.devel.applicationserver.wolfram.com",
            "endpointManager": "http://endpoints.devel.applicationserver.wolfram.com",
            "nodefileManager": "http://nodefiles.devel.applicationserver.wolfram.com",
            "canonicalBaseURL": "http://devel.applicationserver.wolfram.com",
            "restartURL": "http://devel.applicationserver.wolfram.com/.applicationserver/kernel/restart",
            "wasVersion": "1.0",
            "wolframEngineVersion": "12.1"
        }
        ```

## Restart Container [GET]

### Restart [.applicationserver/kernel/restart]

This api used for administrative purpose. Admin can restart the containers using this api url. In order to restart the container first we need to provide username and password. Once authenticated container will get restarted and user will receive message as "Container restarted successfully!". This API should be served without any subdomain or extraneous path elements. For example below, the request URL would be `https://test.applicationserver.wolfram.com/.applicationserver/kernel/restart`.

Restart - Basic Auth Details:

	Username: applicationserver

	Password: P7g[/Y8v?KR}#YvN

+ Response 200 (application/json)

    + Response String
    		
    		Container restarted successfully!