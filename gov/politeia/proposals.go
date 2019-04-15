// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

// Package politeia manages Politeia proposals and the voting that is
// coordinated by the Politeia server and anchored on the blockchain.
package politeia

import (
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/asdine/storm"
	"github.com/asdine/storm/q"
	"github.com/decred/dcrdata/gov/politeia/piclient"
	pitypes "github.com/decred/dcrdata/gov/politeia/types"
	piapi "github.com/decred/politeia/politeiawww/api/v1"
)

// errDef defines the default error returned if the proposals db was not
// initialized correctly.
var errDef = fmt.Errorf("ProposalDB was not initialized correctly")

// ProposalDB defines the common data needed to query the proposals db.
type ProposalDB struct {
	mtx        sync.RWMutex
	dbP        *storm.DB
	client     *http.Client
	APIURLpath string
}

// NewProposalsDB opens an exiting database or creates a new DB instance with
// the provided file name. Returns an initialized instance of proposals DB, http
// client and the formatted politeia API URL path to be used.
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

	// Create the http client used to query the API endpoints.
	c := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:       10,
			IdleConnTimeout:    5 * time.Second,
			DisableCompression: false,
		},
		Timeout: 10 * time.Second,
	}

	// politeiaURL should just be the domain part of the url without the API versioning.
	versionedPath := fmt.Sprintf("%s/api/v%d", politeiaURL, piapi.PoliteiaWWWAPIVersion)

	proposalDB := &ProposalDB{
		dbP:        db,
		client:     c,
		APIURLpath: versionedPath,
	}

	return proposalDB, nil
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
	copyURLParams := URLParams
	pageSize := int(piapi.ProposalListPageSize)
	var publicProposals pitypes.Proposals

	// Since Politeia sets page the limit as piapi.ProposalListPageSize, keep
	// fetching the proposals till the count of fetched proposals is less than
	// piapi.ProposalListPageSize.
	for {
		data, err := piclient.RetrieveAllProposals(db.client, db.APIURLpath, copyURLParams)
		if err != nil {
			return 0, err
		}

		// Break if no valid data was found.
		if data == nil || data.Data == nil {
			break
		}

		publicProposals.Data = append(publicProposals.Data, data.Data...)

		// Break the loop when number the proposals returned are not equal to
		// piapi.ProposalListPageSize in count.
		if len(data.Data) != pageSize {
			break
		}

		copyURLParams = fmt.Sprintf("%s?after=%v", URLParams,
			data.Data[pageSize-1].Censorship.Token)
	}

	// Save all the proposals
	for i, val := range publicProposals.Data {
		if err := db.dbP.Save(val); err != nil {
			return i, fmt.Errorf("save operation failed: %v", err)
		}
	}

	return len(publicProposals.Data), nil
}

// AllProposals fetches all the proposals data saved to the db.
func (db *ProposalDB) AllProposals(offset, rowsCount int,
	filterByVoteStatus ...int) (proposals []*pitypes.ProposalInfo,
	totalCount int, err error) {
	if db == nil || db.dbP == nil {
		return nil, 0, errDef
	}

	db.mtx.RLock()
	defer db.mtx.RUnlock()

	query := db.dbP.Select()
	if len(filterByVoteStatus) > 0 {
		// Filter by the votes status
		query = db.dbP.Select(q.Eq("VoteStatus",
			pitypes.VoteStatusType(filterByVoteStatus[0])))
	}

	// Count the proposals based on the query created above.
	totalCount, err = query.Count(&pitypes.ProposalInfo{})
	if err != nil {
		return
	}

	// Return the proposals listing starting with the newest.
	err = query.Skip(offset).Limit(rowsCount).Reverse().OrderBy("Timestamp").
		Find(&proposals)

	if err != nil && err != storm.ErrNotFound {
		log.Errorf("Failed to fetch data from Proposals DB: %v", err)
	} else {
		err = nil
	}

	return
}

// ProposalByID returns the single proposal identified by the provided id.
func (db *ProposalDB) ProposalByID(proposalID int) (proposal *pitypes.ProposalInfo, err error) {
	if db == nil || db.dbP == nil {
		return nil, errDef
	}

	db.mtx.RLock()
	defer db.mtx.RUnlock()

	var proposals []*pitypes.ProposalInfo

	err = db.dbP.Find("ID", proposalID, &proposals, storm.Limit(1))
	if err != nil {
		log.Errorf("Failed to fetch data from Proposals DB: %v", err)
		return nil, err
	}

	if len(proposals) > 0 && proposals[0] != nil {
		proposal = proposals[0]
	}

	return
}

// CheckProposalsUpdates updates the proposal changes if they exist and updates
// them to the proposal db.
func (db *ProposalDB) CheckProposalsUpdates() error {
	if db == nil || db.dbP == nil {
		return errDef
	}

	db.mtx.Lock()
	defer db.mtx.Unlock()

	// Retrieve and update all current proposals whose vote statuses is either
	// NotAuthorized, Authorized and Started
	numRecords, err := db.updateInProgressProposals()
	if err != nil {
		return err
	}

	// Retrieve and update any new proposals created since the previous
	// proposals were stored in the db.
	lastProposal, err := db.lastSavedProposal()
	if err != nil {
		return fmt.Errorf("lastSavedProposal failed: %v", err)
	}

	var queryParam string
	if len(lastProposal) > 0 && lastProposal[0].Censorship.Token != "" {
		queryParam = fmt.Sprintf("?after=%s", lastProposal[0].Censorship.Token)
	}

	n, err := db.saveProposals(queryParam)
	if err != nil {
		return err
	}

	// Add the sum of the newly added proposals.
	numRecords += n

	log.Infof("%d proposal records (politeia proposals-storm) were updated", numRecords)

	return nil
}

func (db *ProposalDB) lastSavedProposal() (lastP []*pitypes.ProposalInfo, err error) {
	err = db.dbP.All(&lastP, storm.Limit(1), storm.Reverse())
	return
}

// Proposals whose vote statuses are either NotAuthorized, Authorized or Started
// are considered to be in progress. Data for the in progress proposals is
// fetched from Politeia API. From the newly fetched proposals data, db update
// is only made for the vote statuses without NotAuthorized status out of all
// the new votes statuses fetched.
func (db *ProposalDB) updateInProgressProposals() (int, error) {
	// statuses defines a list of vote statuses whose proposals may need an update.
	statuses := []pitypes.VoteStatusType{
		pitypes.VoteStatusType(piapi.PropVoteStatusNotAuthorized),
		pitypes.VoteStatusType(piapi.PropVoteStatusAuthorized),
		pitypes.VoteStatusType(piapi.PropVoteStatusStarted),
	}

	var inProgress []*pitypes.ProposalInfo
	err := db.dbP.Select(
		q.Or(
			q.Eq("VoteStatus", statuses[0]),
			q.Eq("VoteStatus", statuses[1]),
			q.Eq("VoteStatus", statuses[2]),
		),
	).Find(&inProgress)
	// Return an error only if the said error is not 'not found' error.
	if err != nil && err != storm.ErrNotFound {
		return 0, err
	}

	// count defines the number of total updated records.
	var count int

	for _, val := range inProgress {
		proposal, err := piclient.RetrieveProposalByToken(db.client, db.APIURLpath,
			val.Censorship.Token)
		if err != nil {
			return 0, fmt.Errorf("RetrieveProposalByToken failed: %v ", err)
		}

		// Do not update if the new proposals status is NotAuthorized or If the
		// last update has not changed.
		if proposal.VoteStatus == statuses[0] || proposal.Timestamp == val.Timestamp {
			continue
		}

		proposal.ID = val.ID

		err = db.dbP.Update(proposal)
		if err != nil {
			return 0, fmt.Errorf("Update for %s failed with error: %v ",
				val.Censorship.Token, err)
		}

		count++
	}
	return count, nil
}
