// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

// Package politeia manages Politeia proposals and the voting that is coordinated
// by the Politeia server and anchored on the blockchain.
package politeia

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/asdine/storm"
	"github.com/decred/dcrdata/v4/gov/politeia/piclient"
	pitypes "github.com/decred/dcrdata/v4/gov/politeia/types"
	piapi "github.com/decred/politeia/politeiawww/api/v1"
)

// errDef defines the default error returned if the proposals db was not initialized
// correctly.
var errDef = fmt.Errorf("ProposalDB was not initialized correctly")

// ProposalDB defines the common data needed to query the proposals db.
type ProposalDB struct {
	dbP          *storm.DB
	client       *http.Client
	NumProposals int
	_APIURLpath  string
}

// NewProposalsDB opens an exiting database or creates a new db instance with the
// provided file name. Returns an initialized instance of proposals DB, http client
// and the politeia API URL path to be used.
func NewProposalsDB(politeiaURL, dbPath string) (*ProposalDB, error) {
	if politeiaURL == "" {
		return nil, fmt.Errorf("missing politeia API URL")
	}

	if dbPath == "" {
		return nil, fmt.Errorf("missing db path")
	}

	_, err := os.Stat(dbPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	db, err := storm.Open(dbPath)
	if err != nil {
		return nil, err
	}

	// returns a http client used to query the API endpoints.
	c := &http.Client{Transport: &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    5 * time.Second,
		DisableCompression: false,
	}}

	proposalDB := &ProposalDB{
		dbP:         db,
		client:      c,
		_APIURLpath: politeiaURL,
	}

	return proposalDB, nil
}

// countProperties fetches the proposals count and appends it the ProposalDB instance
// provided.
func (db *ProposalDB) countProperties() error {
	count, err := db.dbP.Count(&pitypes.ProposalInfo{})
	if err != nil {
		return err
	}

	db.NumProposals = count
	return nil
}

// Close closes the proposal DB instance created passed if it not nil.
func (db *ProposalDB) Close() error {
	if db == nil || db.dbP == nil {
		return nil
	}

	return db.dbP.Close()
}

// saveProposals adds the proposals data to the db.
func (db *ProposalDB) saveProposals(URLParams string) (int, error) {
	// constructs the full vetted proposals API URL
	URLpath := db._APIURLpath + piapi.RouteAllVetted + URLParams
	data, err := piclient.HandleGetRequests(db.client, URLpath)
	if err != nil {
		return 0, err
	}

	var publicProposals pitypes.Proposals
	err = json.Unmarshal(data, &publicProposals)
	if err != nil || len(publicProposals.Data) == 0 {
		return 0, err
	}

	// constructs the full vote status API URL
	URLpath = db._APIURLpath + piapi.RouteAllVoteStatus + URLParams
	data, err = piclient.HandleGetRequests(db.client, URLpath)
	if err != nil {
		return 0, err
	}

	var votesInfo pitypes.Votes
	err = json.Unmarshal(data, &votesInfo)
	if err != nil {
		return 0, err
	}

	// Append the votes status information to the respective proposals if it exists.
	for _, val := range publicProposals.Data {
		for k := range votesInfo.Data {
			if val.Censorship.Token == votesInfo.Data[k].Token {
				val.VotesStatus = votesInfo.Data[k]
				// exits the second loop after finding a match.
				break
			}
		}
	}

	// Save all the proposals
	for i, val := range publicProposals.Data {
		if err = db.dbP.Save(val); err != nil {
			return i, fmt.Errorf("save operation failed: %v", err)
		}
	}

	return len(publicProposals.Data), err
}

// AllProposals fetches all the proposals data saved to the db.
func (db *ProposalDB) AllProposals(offset, rowsCount int) (proposals []*pitypes.ProposalInfo,
	totalCount int, err error) {
	if db == nil || db.dbP == nil {
		return nil, 0, errDef
	}

	// Return the agendas listing starting with the oldest.
	err = db.dbP.Select().Skip(offset).Limit(rowsCount).OrderBy("Timestamp").Find(&proposals)
	if err != nil {
		log.Errorf("Failed to fetch data from Agendas DB: %v", err)
	}

	totalCount = db.NumProposals

	return
}

// ProposalByID returns the single proposal identified by the provided id.
func (db *ProposalDB) ProposalByID(proposalID int) (proposal *pitypes.ProposalInfo, err error) {
	if db == nil || db.dbP == nil {
		return nil, errDef
	}

	var proposals []*pitypes.ProposalInfo

	err = db.dbP.Find("ID", proposalID, &proposals, storm.Limit(1))
	if err != nil {
		log.Errorf("Failed to fetch data from Agendas DB: %v", err)
		return nil, err
	}

	if len(proposals) > 0 && proposals[0] != nil {
		proposal = proposals[0]
	}

	return
}

// CheckProposalsUpdates updates the on chain changes if they exist.
func (db *ProposalDB) CheckProposalsUpdates() error {
	if db == nil || db.dbP == nil {
		return errDef
	}

	lastProposal, err := db.lastSavedProposal()
	if err != nil {
		return fmt.Errorf("lastSavedProposal failed: %v", err)
	}

	var queryParam string
	if len(lastProposal) > 0 && lastProposal[0].Censorship.Token != "" {
		queryParam = fmt.Sprintf("?after=%s", lastProposal[0].Censorship.Token)
	}

	numRecords, err := db.saveProposals(queryParam)
	if err != nil {
		return err
	}

	log.Infof("%d proposal records (politeia proposals) were updated", numRecords)

	return db.countProperties()
}

func (db *ProposalDB) lastSavedProposal() (lastP []*pitypes.ProposalInfo, err error) {
	err = db.dbP.All(&lastP, storm.Limit(1), storm.Reverse())
	return
}
