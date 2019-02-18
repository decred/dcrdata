// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

// piclient handles the http requests made to API URL paths.

package piclient

import (
	"fmt"
	"io/ioutil"
	"net/http"
)

// HandleGetRequests accepts a http client and API URL path as arguments. If the
// parameters are valid, a GET request is made to the API URL path passed. The
// body returned is then decoded into []byte and returned.
func HandleGetRequests(client *http.Client, _URLPath string) ([]byte, error) {
	if client == nil {
		return nil, fmt.Errorf("invalid http client was passed")
	}

	if _URLPath == "" {
		return nil, fmt.Errorf("empty API URL is not supported")
	}

	response, err := client.Get(_URLPath)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	return ioutil.ReadAll(response.Body)
}
