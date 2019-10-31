// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

// Package piclient handles the http requests made to Politeia APIs.
package piclient

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"

	pitypes "github.com/decred/dcrdata/gov/v3/politeia/types"
	piapi "github.com/decred/politeia/politeiawww/api/www/v1"
)

// HandleGetRequests accepts a http client and API URL path as arguments. If the
// parameters are valid, a GET request is made to the API URL passed. The body
// returned is decoded into []byte and returned.
func HandleGetRequests(client *http.Client, URLPath string) ([]byte, error) {
	if client == nil {
		return nil, fmt.Errorf("invalid http client was passed")
	}

	if URLPath == "" {
		return nil, fmt.Errorf("empty API URL is not supported")
	}

	response, err := client.Get(URLPath)
	if err != nil || response == nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}

	// Check if valid status code (200 Ok) was returned.
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request (%s) failed with status code: %s",
			URLPath, response.Status)
	}

	defer response.Body.Close()

	return ioutil.ReadAll(response.Body)
}

// DropURLRegex replaces "{token:[A-z0-9]{64}}" in a URL with provided the parameter.
func DropURLRegex(URLPath, param string) string {
	r := regexp.MustCompile(`\{token:\[A-z0-9]\{64\}}`)
	return r.ReplaceAllLiteralString(URLPath, param)
}

// RetrieveAllProposals returns a list of Proposals whose maximum count is defined
// by piapi.ProposalListPageSize. Data returned is queried from Politeia API.
func RetrieveAllProposals(client *http.Client, APIRootPath, URLParams string) (
	*pitypes.Proposals, error) {
	// Constructs the full vetted proposals API URL
	URLpath := APIRootPath + piapi.RouteAllVetted + URLParams
	data, err := HandleGetRequests(client, URLpath)
	if err != nil {
		return nil, err
	}

	var publicProposals pitypes.Proposals
	err = json.Unmarshal(data, &publicProposals)
	if err != nil || len(publicProposals.Data) == 0 {
		return &publicProposals, err
	}

	// Constructs the full vote status API URL
	URLpath = APIRootPath + piapi.RouteAllVoteStatus + URLParams
	data, err = HandleGetRequests(client, URLpath)
	if err != nil {
		return nil, err
	}

	var votesInfo pitypes.Votes
	err = json.Unmarshal(data, &votesInfo)
	if err != nil {
		return nil, err
	}

	// Append the votes status information to the respective proposals if it exists.
	for _, val := range publicProposals.Data {
		for k := range votesInfo.Data {
			if val.TokenVal == votesInfo.Data[k].Token {
				val.ProposalVotes = votesInfo.Data[k]
				// exits the second loop after finding a match.
				break
			}
		}
	}

	return &publicProposals, nil
}

// RetrieveProposalByToken returns a single proposal identified by the token
// hash provided if it exists. Data returned is queried from Politeia API.
func RetrieveProposalByToken(client *http.Client, APIRootPath, token string) (*pitypes.Proposal, error) {
	// Constructs the full proposal's URl and fetch is data.
	proposalRoute := APIRootPath + DropURLRegex(piapi.RouteProposalDetails, token)
	data, err := HandleGetRequests(client, proposalRoute)
	if err != nil {
		return nil, fmt.Errorf("retrieving %s proposal details failed: %v", token, err)
	}

	var proposal pitypes.Proposal
	err = json.Unmarshal(data, &proposal)
	if err != nil {
		return nil, err
	}

	// Check if null proposal data was returned as part of the proposal details.
	if proposal.Data == nil {
		return nil, fmt.Errorf("invalid proposal with null data found for %s", token)
	}

	// Constructs the full votes status URL and fetch its data.
	votesStatusRoute := APIRootPath + DropURLRegex(piapi.RouteVoteStatus, token)
	data, err = HandleGetRequests(client, votesStatusRoute)
	if err != nil {
		return nil, fmt.Errorf("retrieving %s proposal vote status failed: %v", token, err)
	}

	err = json.Unmarshal(data, &proposal.Data.ProposalVotes)
	if err != nil {
		return nil, err
	}

	return &proposal, nil
}
