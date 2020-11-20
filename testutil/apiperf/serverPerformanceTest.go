package main

// This trys to create a standard way we can measure the server performance
// by measuring how long it takes query a given endpoint in dcrdata for a certain
// number of defined iterations. This testing method focuses only on GET requests
// whose status code is always expected to be 200 if successful.

import (
	"fmt"
	"net/http"
	"time"
)

const (
	ticketPrice      = "/chart/ticket-price"
	tpMonthGrouping  = "/ticketpool/bydate/mo"
	tpDayGrouping    = "/ticketpool/bydate/day"
	tpAllGrouping    = "/ticketpool/bydate/all"
	allProjectFundTx = "/address/Dcur2mcGjmENx4DhNqDctW5wJCVyT3Qeqkx/types/all"
	serverAPIUrl     = "http://127.0.0.1:7777/api"

	statusOK      = http.StatusOK
	maxIterations = 10
)

// httpClient set the time out value, maximum connections and Disable compressions.
func httpClient() *http.Client {
	tr := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    10 * time.Second,
		DisableCompression: true,
	}
	return &http.Client{Transport: tr}
}

// queryEndpoint querys the endpoint provided for the number of iterations defined
// by maxIterations. An error is return when the client returns an error or when
// an incorrect status code is returned by the url.
func queryEndpoint(urlStr string, statusCode int) error {
	client := httpClient()

	for i := 0; i < maxIterations; i++ {
		res, err := client.Get(urlStr)
		if err != nil {
			return fmt.Errorf("failed to fetch %s url data at iteration %d: error: %v", urlStr, i, err)
		}
		res.Body.Close()

		if res.StatusCode != statusCode {
			return fmt.Errorf("on iteration %d expected status code %d but got %d",
				i, statusCode, res.StatusCode)
		}
	}
	return nil
}

// main initiates the server performance testing primarily measuring how long it
// takes to query each of the given testingUrls if not error is returned by the
// queryEndpoint function.
func main() {
	testingUrls := map[string]string{
		"ticketPrice":               serverAPIUrl + ticketPrice,
		"month ticket pool":         serverAPIUrl + tpMonthGrouping,
		"day ticket pool":           serverAPIUrl + tpDayGrouping,
		"all time ticket pool":      serverAPIUrl + tpAllGrouping,
		"all project fund txtypes ": serverAPIUrl + allProjectFundTx,
	}

	fmt.Printf(" >>>>>> Intitiating the Server Performance Test by Response Time for %d Iterations per URL<<<<<<< \n", maxIterations)
	for urlType, val := range testingUrls {
		startTime := time.Now()
		err := queryEndpoint(val, statusOK)
		if err != nil {
			fmt.Println(err)
			break
		}

		fmt.Printf("Server Performance testing on '%s' url took %v \n\n", urlType, time.Since(startTime))
	}
}
