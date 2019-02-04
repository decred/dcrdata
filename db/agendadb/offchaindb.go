// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package agendadb

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/asdine/storm"
)

const (
	// vettedProposalsRoute is the URL path that returns the public/vetted
	// proposals.
	vettedProposalsRoute = "/proposals/vetted"

	// voteStatusesRoute is the URL path that returns the vote details of the
	// public proposals.
	voteStatusesRoute = "/proposals/votestatus"
)

// ProposalInfo holds the proposal details as document here
// https://github.com/decred/politeia/blob/master/politeiawww/api/v1/api.md#proposal.
// It also holds the votes status details.
type ProposalInfo struct {
	ID              int              `storm:"id"`
	Name            string           `json:"name"`
	State           int32            `json:"state"`
	Status          int32            `json:"status"`
	Timestamp       int64            `json:"timestamp" storm:"index"`
	UserID          string           `json:"userid"`
	PublicKey       string           `json:"publickey"`
	Signature       string           `json:"signature"`
	Version         string           `json:"version"`
	Censorship      CensorshipRecord `json:"censorshiprecord"`
	Files           []AttachmentFile `json:"files"`
	Numcomments     int32            `json:"numcomments"`
	StatusChangeMsg string           `json:"statuschangemessage"`
	PubishedDate    int64            `json:"publishedat"`
	CensoredDate    int64            `json:"censoredat"`
	AbandonedDate   int64            `json:"abandonedat"`
	VotesStatus     *ProposalVotes   `json:"votesstatus"`
}

// CensorshipRecord is an entry that was created when the proposal was submitted.
// https://github.com/decred/politeia/blob/master/politeiawww/api/v1/api.md#censorship-record
type CensorshipRecord struct {
	Token      string `json:"token"`
	MerkleRoot string `json:"merkle"`
	Signature  string `json:"signature"`
}

// AttachmentFile are files and documents submitted as proposal details.
// https://github.com/decred/politeia/blob/master/politeiawww/api/v1/api.md#file
type AttachmentFile struct {
	Name      string `json:"name"`
	MimeType  string `json:"mime"`
	DigestKey string `json:"digest"`
	Payload   string `json:"payload"`
}

// ProposalVotes defines the proposal status(Votes infor for the public proposals).
// https://github.com/decred/politeia/blob/master/politeiawww/api/v1/api.md#proposal-vote-status
type ProposalVotes struct {
	Token              string  `json:"token"`
	Status             int32   `json:"status"`
	VoteResults        Results `json:"optionsresult"`
	TotalVotes         int64   `json:"totalvotes"`
	Endheight          string  `json:"endheight"`
	NumOfEligibleVotes int64   `json:"numofeligiblevotes"`
	QuorumPercentage   uint32  `json:"quorumpercentage"`
	PassPercentage     uint32  `json:"passpercentage"`
}

// Results defines the actual vote count info per the votes choices available.
type Results struct {
	Option struct {
		ID          string `json:"id"`
		Description string `json:"description"`
		Bits        int32  `json:"bits"`
	} `json:"option"`
	VotesReceived int64 `json:"votesreceived"`
}

// fetchHTTPClient returns a http client used to query the API endpoints.
func fetchHTTPClient() *http.Client {
	tr := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    5 * time.Second,
		DisableCompression: false,
	}

	return &http.Client{Transport: tr}
}

// handleGetRequests constructs the full URL path, querys the API endpoints and
// returns the queried data in form of byte array and an error if it exists.
func (db *AgendaDB) handleGetRequests(root, path, params string) ([]byte, error) {
	response, err := db.client.Get(root + path + params)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	return ioutil.ReadAll(response.Body)
}

// saveProposals adds the proposals data to the db.
func (db *AgendaDB) saveProposals(URLParams string) (int, error) {
	data, err := db.handleGetRequests(db.politeiaURL, vettedProposalsRoute, URLParams)
	if err != nil {
		return 0, err
	}

	publicProposals := make([]ProposalInfo, 0)
	err = json.Unmarshal(data, publicProposals)
	if err != nil || len(publicProposals) == 0 {
		return 0, err
	}

	data, err = db.handleGetRequests(db.politeiaURL, voteStatusesRoute, URLParams)
	if err != nil {
		return 0, err
	}

	votesInfo := make([]ProposalVotes, 0)
	err = json.Unmarshal(data, votesInfo)
	if err != nil {
		return 0, err
	}

	// Append the votes information to the respective proposals.
	for i := range publicProposals {
		for k := range votesInfo {
			if publicProposals[i].Censorship.Token == votesInfo[k].Token {
				publicProposals[i].VotesStatus = &votesInfo[k]
				// exits the second loop after finding a match.
				break
			}
		}
	}

	err = db.offNode.Save(publicProposals)

	return len(publicProposals), err
}

// AllProposals fetches all the proposals data saved to the db.
func (db *AgendaDB) AllProposals() (proposals []ProposalInfo, err error) {
	var adb *AgendaDB
	adb, err = Open()
	if err != nil {
		log.Errorf("Failed to open new Agendas DB: %v", err)
		return
	}

	defer func() {
		err = adb.Close()
		if err != nil {
			log.Errorf("Failed to close the Agendas DB: %v", err)
		}
	}()

	err = adb.onNode.All(&proposals)
	if err != nil {
		log.Errorf("Failed to fetch data from Agendas DB: %v", err)
	}

	return
}

// lastSavedProposal
func (db *AgendaDB) lastSavedProposal() (lastP *ProposalInfo, err error) {
	err = db.offNode.All(lastP, storm.Limit(1), storm.Reverse())
	return
}

func (db *AgendaDB) checkOffchainUpdates() (int, error) {
	lastProposal, err := db.getLastSavedProposal()
	if err != nil {
		return 0, err
	}

	var queryParam = ""

	if lastProposal != nil {
		queryParam = fmt.Sprintf("?after=%v", lastProposal.Censorship.Token)
	}

	return db.saveProposals(queryParam)
}
