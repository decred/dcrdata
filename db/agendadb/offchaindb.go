// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package agendadb

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"time"
)

const (
	vettedProposalsRoute = "/proposals/vetted"
	voteStatusesRoute    = "/proposals/votestatus"
)

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

type CensorshipRecord struct {
	Token      string `json:"token"`
	MerkleRoot string `json:"merkle"`
	Signature  string `json:"signature"`
}

type AttachmentFile struct {
	Name      string `json:"name"`
	MimeType  string `json:"mime"`
	DigestKey string `json:"digest"`
	Payload   string `json:"payload"`
}

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

type Results struct {
	Option struct {
		ID          string `json:"id"`
		Description string `json:"description"`
		Bits        int32  `json:"bits"`
	} `json:"option"`
	VotesReceived int64 `json:"votesreceived"`
}

func GetClient() *http.Client {
	tr := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    5 * time.Second,
		DisableCompression: false,
	}

	return &http.Client{Transport: tr}
}

func (db *AgendaDB) handleGetRequests(root, path, params string) ([]byte, error) {
	response, err := db.client.Get(root + path + params)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	return ioutil.ReadAll(response.Body)
}

func (db *AgendaDB) saveProposals(URLParams string) error {
	data, err := db.handleGetRequests(db.politeiaURL, vettedProposalsRoute, URLParams)
	if err != nil {
		return err
	}

	publicProposals := make([]ProposalInfo, 0)
	err = json.Unmarshal(data, publicProposals)
	if err != nil {
		return err
	}

	data, err = db.handleGetRequests(db.politeiaURL, voteStatusesRoute, URLParams)
	if err != nil {
		return err
	}

	votesInfo := make([]ProposalVotes, 0)
	err = json.Unmarshal(data, votesInfo)
	if err != nil {
		return err
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

	return db.sdb.Save(publicProposals)
}

func (db *AgendaDB) retrieveProposals() ([]ProposalInfo, error) {
	proposals, := db.sdb.Select().OrderBy("Timestamp")
}

func checkForUpdates() {

}
