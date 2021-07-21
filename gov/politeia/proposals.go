// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

// Package politeia manages Politeia proposals and the voting that is
// coordinated by the Politeia server and anchored on the blockchain.
package politeia

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/asdine/storm/v3"
	"github.com/asdine/storm/v3/q"
	"github.com/decred/dcrdata/gov/v4/politeia/piclient"
	pitypes "github.com/decred/dcrdata/gov/v4/politeia/types"
	"github.com/decred/dcrdata/v6/semver"
	piapi "github.com/decred/politeia/politeiawww/api/www/v1"
)

var (
	// errDef defines the default error returned if the proposals db was not
	// initialized correctly.
	errDef = fmt.Errorf("ProposalDB was not initialized correctly")

	// dbVersion is the current required version of the proposals.db.
	dbVersion = semver.NewSemver(2, 0, 0)
)

// dbinfo defines the property that holds the db version.
const dbinfo = "_proposals.db_"

// ProposalDB defines the common data needed to query the proposals db.
type ProposalDB struct {
	lastSync   int64 // atomic
	dbP        *storm.DB
	client     *http.Client
	APIURLpath string
}

// NewProposalsDB opens an exiting database or creates a new DB instance with
// the provided file name. Returns an initialized instance of proposals DB, http
// client and the formatted politeia API URL path to be used. It also checks the
// db version, Reindexes the db if need be and sets the required db version.
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

	// Checks if the correct db version has been set.
	var version string
	err = db.Get(dbinfo, "version", &version)
	if err != nil && err != storm.ErrNotFound {
		return nil, err
	}

	if version != dbVersion.String() {
		// Attempt to delete the ProposalInfo bucket.
		if err = db.Drop(&pitypes.ProposalInfo{}); err != nil {
			// If error due bucket not found was returned, ignore it.
			if !strings.Contains(err.Error(), "not found") {
				return nil, fmt.Errorf("delete bucket struct failed: %v", err)
			}
		}

		// Set the required db version.
		err = db.Set(dbinfo, "version", dbVersion.String())
		if err != nil {
			return nil, err
		}
		log.Infof("proposals.db version %v was set", dbVersion)
	}

	// Create the http client used to query the API endpoints.
	c := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:       10,
			IdleConnTimeout:    5 * time.Second,
			DisableCompression: false,
		},
		Timeout: 30 * time.Second,
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

// generateCustomID generates a custom ID that is used to reference the proposals
// from the frontend. The ID generated from the title by having all its
// punctuation marks replaced with a hyphen and the string converted to lowercase.
// According to Politeia, a proposal title has a max length of 80 characters thus
// the new ID should have a max length of 80 characters.
func generateCustomID(title string) (string, error) {
	if title == "" {
		return "", fmt.Errorf("ID not generated: invalid title found")
	}
	// regex selects only the alphanumeric characters.
	reg, err := regexp.Compile("[^a-zA-Z0-9]+")
	if err != nil {
		return "", err
	}

	// Replace all punctuation marks with a hyphen and make it lower case.
	return reg.ReplaceAllString(strings.ToLower(title), "-"), nil
}

// fetchAPIData returns the API data fetched from the Politeia API endpoints.
// NB: "/api/v1/proposals/vetted" path returns the latest snapshot of the API
// data that currently exits. This implies that parameter "after=" should be used
// when syncing the data from scratch and "before=" should be used to fetch newer
// updates.
func (db *ProposalDB) fetchAPIData(URLParams string) (pitypes.Proposals, error) {
	copyURLParams := URLParams
	pageSize := int(piapi.ProposalListPageSize)
	var publicProposals pitypes.Proposals

	// It helps determine when fresh sync is to run when no previous data
	// existed. copyURLParams is an empty string when fresh sync is to run.
	var param = "before"
	if copyURLParams == "" {
		param = "after"
	}

	// Since Politeia sets page the limit as piapi.ProposalListPageSize, keep
	// fetching the proposals till the count of fetched proposals is less than
	// piapi.ProposalListPageSize.
	for {
		data, err := piclient.RetrieveAllProposals(db.client, db.APIURLpath, copyURLParams)
		if err != nil {
			return publicProposals, err
		}

		// Break if no valid data was found.
		if data == nil || data.Data == nil {
			// Should help detect when API changes are effected on Politeia's end.
			log.Warn("invalid or empty data entries were returned")
			break
		}

		if len(data.Data) == 0 {
			// No updates found.
			break
		}

		publicProposals.Data = append(publicProposals.Data, data.Data...)

		// Break the loop when number the proposals returned are not equal to
		// piapi.ProposalListPageSize in count.
		if len(data.Data) != pageSize {
			break
		}

		copyURLParams = fmt.Sprintf("?%v=%v", param, data.Data[pageSize-1].TokenVal)
	}
	return publicProposals, nil
}

// saveProposals adds the proposals data to the db.
func (db *ProposalDB) saveProposals(publicProposals pitypes.Proposals) (int, error) {
	var proposalsSaved int
	// Attempt to save a given a given item for a max of 5 times.
	const maxLoop = 5

	// Save all the proposals
	for i, val := range publicProposals.Data {
		var err error
		if val.RefID, err = generateCustomID(val.Name); err != nil {
			return 0, err
		}

		err = db.dbP.Save(val)

		// When a duplicate CensorshipRecord struct is detected this "already exists"
		// error is thrown, this means that some edits were made to an older version
		// of the proposal and its fixed by updating the new changes. In another
		// case that is more rare, is that the current proposal could have a Name
		// that generates a RefID similar to one already in the db and appending
		// integers to it till it becomes unique is the solution.
		if err == storm.ErrAlreadyExists {
			var data *pitypes.ProposalInfo
			// Check if the proposal token already exists in the db.
			data, err = db.proposal("TokenVal", val.TokenVal)
			if err == nil && data != nil {
				// The proposal token already exists thus trigger an update with
				// the latest details.
				valCopy := *val
				valCopy.ID = data.ID
				suffixStr := ""

				for k := 1; k <= maxLoop; k++ {
					valCopy.RefID += suffixStr
					// Attempt to update the old entry.
					err = db.dbP.Update(&valCopy)
					if err == storm.ErrAlreadyExists {
						suffixStr = strconv.Itoa(k)
						continue
					}
					if err != nil {
						log.Error("storm DB update failed: %v", err)
					}
					break
				}
			}

			// First try wasn't successful if err != nil.
			if err != nil {
				for c := 1; c <= maxLoop; c++ {
					// Drop the previously assigned ID.
					val.ID = 0

					val.RefID += strconv.Itoa(c)
					// Attempt to save a new entry.
					err = db.dbP.Save(val)
					if err == storm.ErrAlreadyExists {
						continue
					}
					if err != nil {
						log.Error("storm DB save failed: %v", err)
					}
					break
				}
			}
		}

		if err != nil {
			return i, fmt.Errorf("save operation failed: %v", err)
		}

		// increment since the save is successful.
		proposalsSaved++
	}

	return proposalsSaved, nil
}

// AllProposals fetches all the proposals data saved to the db.
func (db *ProposalDB) AllProposals(offset, rowsCount int,
	filterByVoteStatus ...int) (proposals []*pitypes.ProposalInfo,
	totalCount int, err error) {
	if db == nil || db.dbP == nil {
		return nil, 0, errDef
	}

	var query storm.Query
	if len(filterByVoteStatus) > 0 {
		// Filter by the votes status
		query = db.dbP.Select(q.Eq("VoteStatus",
			pitypes.VoteStatusType(filterByVoteStatus[0])))
	} else {
		query = db.dbP.Select()
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

// ProposalByToken returns the single proposal identified by the provided token.
func (db *ProposalDB) ProposalByToken(proposalToken string) (*pitypes.ProposalInfo, error) {
	if db == nil || db.dbP == nil {
		return nil, errDef
	}

	return db.proposal("TokenVal", proposalToken)
}

// ProposalByRefID returns the single proposal identified by the provided refID.
// RefID is generated from the proposal name and used as the descriptive part of
// the URL to proposal details page on the /proposal page.
func (db *ProposalDB) ProposalByRefID(RefID string) (*pitypes.ProposalInfo, error) {
	if db == nil || db.dbP == nil {
		return nil, errDef
	}

	return db.proposal("RefID", RefID)
}

// proposal runs the query with searchBy and searchTerm parameters provided and
// returns the result.
func (db *ProposalDB) proposal(searchBy, searchTerm string) (*pitypes.ProposalInfo, error) {
	var pInfo pitypes.ProposalInfo
	err := db.dbP.Select(q.Eq(searchBy, searchTerm)).Limit(1).First(&pInfo)
	if err != nil {
		if !errors.Is(err, storm.ErrNotFound) {
			log.Errorf("Failed to fetch data from Proposals DB: %v", err)
		}
		return nil, err
	}

	return &pInfo, nil
}

// LastProposalsSync returns the last time a sync to update the proposals was run
// but not necessarily the last time updates were synced in proposals.db.
func (db *ProposalDB) LastProposalsSync() int64 {
	return atomic.LoadInt64(&db.lastSync)
}

// CheckProposalsUpdates updates the proposal changes if they exist and updates
// them to the proposal db.
func (db *ProposalDB) CheckProposalsUpdates() error {
	if db == nil || db.dbP == nil {
		return errDef
	}

	defer atomic.StoreInt64(&db.lastSync, time.Now().UTC().Unix())

	// Retrieve and update all current proposals whose vote statuses is either
	// NotAuthorized, Authorized and Started
	numRecords, err := db.updateInProgressProposals()
	if err != nil {
		return err
	}

	// Retrieve and update any new proposals created since the previous
	// proposals were stored in the db.
	lastProposal, err := db.lastSavedProposal()
	if err != nil && err != storm.ErrNotFound {
		return fmt.Errorf("lastSavedProposal failed: %v", err)
	}

	var queryParam string
	if len(lastProposal) > 0 && lastProposal[0].TokenVal != "" {
		queryParam = fmt.Sprintf("?before=%s", lastProposal[0].TokenVal)
	}
	publicProposals, err := db.fetchAPIData(queryParam)
	if err != nil {
		return err
	}

	n, err := db.saveProposals(publicProposals)
	if err != nil {
		return err
	}

	// Add the sum of the newly added proposals.
	numRecords += n

	log.Infof("%d politeia proposal DB records were updated", numRecords)

	return nil
}

func (db *ProposalDB) lastSavedProposal() (lastP []*pitypes.ProposalInfo, err error) {
	err = db.dbP.Select().Limit(1).OrderBy("Timestamp").Reverse().Find(&lastP)
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
		proposal, err := piclient.RetrieveProposalByToken(db.client, db.APIURLpath, val.TokenVal)
		// Do not update if:
		// 1. piclient.RetrieveProposalByToken returned an error
		if err != nil {
			// Since the proposal tokens being updated here are already in the
			// proposals.db. Do not return errors found since they will still be
			// updated when the data is available.
			log.Errorf("RetrieveProposalByToken failed: %v ", err)
			continue
		}

		proposal.Data.ID = val.ID
		proposal.Data.RefID = val.RefID

		// 2. The new proposal data has not changed.
		if val.IsEqual(proposal.Data) {
			continue
		}

		// 4. Some or all data returned was empty or invalid.
		if proposal.Data.TokenVal == "" || proposal.Data.TotalVotes < val.TotalVotes {
			// Should help detect when API changes are effected on Politeia's end.
			log.Warnf("invalid or empty data entries were returned for %v", val.TokenVal)
			continue
		}

		err = db.dbP.Update(proposal.Data)
		if err != nil {
			return 0, fmt.Errorf("Update for %s failed with error: %v ", val.TokenVal, err)
		}

		count++
	}
	return count, nil
}
