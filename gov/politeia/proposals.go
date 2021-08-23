// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.

package politeia

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/asdine/storm/v3"
	"github.com/asdine/storm/v3/q"
	pitypes "github.com/decred/dcrdata/gov/v4/politeia/types"
	"github.com/decred/dcrdata/v6/semver"
	commentsv1 "github.com/decred/politeia/politeiawww/api/comments/v1"
	recordsv1 "github.com/decred/politeia/politeiawww/api/records/v1"
	ticketvotev1 "github.com/decred/politeia/politeiawww/api/ticketvote/v1"
	piclient "github.com/decred/politeia/politeiawww/client"
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

// ProposalsDB defines the object that interacts with the local proposals
// db, and with decred's politeia server.
type ProposalsDB struct {
	lastSync int64 // atomic
	dbP      *storm.DB
	client   *piclient.Client
	APIPath  string
}

// NewProposalsDB opens an existing database or creates a new a storm DB
// instance with the provided path. It also sets up a new politeia http
// client and returns them on a proposals DB instance.
func NewProposalsDB(politeiaURL, dbPath string) (*ProposalsDB, error) {
	// Validate arguments
	if politeiaURL == "" {
		return nil, errors.New("missing Politeia URL")
	}
	if dbPath == "" {
		return nil, errors.New("missing db path")
	}

	// Check path and open storm DB
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
	if err != nil && !errors.Is(err, storm.ErrNotFound) {
		return nil, err
	}

	if version != dbVersion.String() {
		// Attempt to delete the ProposalRecord bucket.
		if err = db.Drop(&pitypes.ProposalRecord{}); err != nil {
			// If error due bucket not found was returned, ignore it.
			if !strings.Contains(err.Error(), "not found") {
				return nil, fmt.Errorf("delete bucket struct failed: %w", err)
			}
		}

		// Set the required db version.
		err = db.Set(dbinfo, "version", dbVersion.String())
		if err != nil {
			return nil, err
		}
		log.Infof("proposals.db version %v was set", dbVersion)
	}

	pc, err := piclient.New(politeiaURL+"/api", piclient.Opts{})
	if err != nil {
		return nil, err
	}

	proposalDB := &ProposalsDB{
		dbP:     db,
		client:  pc,
		APIPath: politeiaURL,
	}

	return proposalDB, nil
}

// Close closes the proposal DB instance.
func (db *ProposalsDB) Close() error {
	if db == nil || db.dbP == nil {
		return nil
	}

	return db.dbP.Close()
}

// ProposalsLastSync reads the last sync timestamp from the atomic db.
//
// Satisfies the PoliteiaBackend interface.
func (db *ProposalsDB) ProposalsLastSync() int64 {
	return atomic.LoadInt64(&db.lastSync)
}

// ProposalsSync is responsible for keeping an up-to-date database synced
// with politeia's latest updates.
//
// Satisfies the PoliteiaBackend interface.
func (db *ProposalsDB) ProposalsSync() error {
	// Sanity check
	if db == nil || db.dbP == nil {
		return errDef
	}

	// Save the timestamp of the last update check.
	defer atomic.StoreInt64(&db.lastSync, time.Now().UTC().Unix())

	// Update db with any new proposals on politeia server.
	err := db.proposalsNewUpdate()
	if err != nil {
		return err
	}

	// Update all current proposals who might still be suffering changes
	// with edits, and that has undergone some data change.
	err = db.proposalsInProgressUpdate()
	if err != nil {
		return err
	}

	// Update vote results data on finished proposals that are not yet
	// fully synced with politeia.
	err = db.proposalsVoteResultsUpdate()
	if err != nil {
		return err
	}

	log.Info("Politeia records were synced.")

	return nil
}

// ProposalsAll fetches the proposals data from the local db.
// The argument filterByVoteStatus is optional.
//
// Satisfies the PoliteiaBackend interface.
func (db *ProposalsDB) ProposalsAll(offset, rowsCount int,
	filterByVoteStatus ...int) ([]*pitypes.ProposalRecord, int, error) {
	// Sanity check
	if db == nil || db.dbP == nil {
		return nil, 0, errDef
	}

	var query storm.Query

	if len(filterByVoteStatus) > 0 {
		query = db.dbP.Select(q.Eq("VoteStatus",
			ticketvotev1.VoteStatusT(filterByVoteStatus[0])))
	} else {
		query = db.dbP.Select()
	}

	// Count the proposals based on the query created above.
	totalCount, err := query.Count(&pitypes.ProposalRecord{})
	if err != nil {
		return nil, 0, err
	}

	// Return the proposals listing starting with the newest.
	var proposals []*pitypes.ProposalRecord
	err = query.Skip(offset).Limit(rowsCount).Reverse().OrderBy("Timestamp").
		Find(&proposals)
	if err != nil && !errors.Is(err, storm.ErrNotFound) {
		return nil, 0, err
	}

	return proposals, totalCount, nil
}

// ProposalByToken retrieves the proposal for the given token argument.
//
// Satisfies the PoliteiaBackend interface.
func (db *ProposalsDB) ProposalByToken(token string) (*pitypes.ProposalRecord, error) {
	if db == nil || db.dbP == nil {
		return nil, errDef
	}

	return db.proposal("Token", token)
}

// fetchProposalsData returns the parsed vetted proposals from politeia
// API's. It cooks up the data needed to save the proposals in stormdb. It
// first fetches the proposal details, then comments and then vote summary.
// This data is needed for the information provided in the dcrdata UI. The
// data returned does not include ticket vote data.
func (db *ProposalsDB) fetchProposalsData(tokens []string) ([]*pitypes.ProposalRecord, error) {
	// Fetch record details for each token from the inventory.
	recordDetails, err := db.fetchRecordDetails(tokens)
	if err != nil {
		return nil, err
	}

	// Fetch comments count for each token from the inventory.
	commentsCounts, err := db.fetchCommentsCounts(tokens)
	if err != nil {
		return nil, err
	}

	// Fetch vote summary for each token from the inventory.
	voteSummaries, err := db.fetchTicketVoteSummaries(tokens)
	if err != nil {
		return nil, err
	}

	// Iterate through every record and feed data used by dcrdata
	proposals := make([]*pitypes.ProposalRecord, 0, len(recordDetails))
	for _, record := range recordDetails {
		// Record data
		proposal := &pitypes.ProposalRecord{
			State:     record.State,
			Status:    record.Status,
			Version:   record.Version,
			Timestamp: uint64(record.Timestamp),
			Username:  record.Username,
			Token:     record.CensorshipRecord.Token,
		}

		// Proposal metadata
		pm, err := proposalMetadataDecode(record.Files)
		if err != nil {
			return nil, fmt.Errorf("proposalMetadataDecode err: %w", err)
		}
		proposal.Name = pm.Name

		// User metadata
		um, err := userMetadataDecode(record.Metadata)
		if err != nil {
			return nil, fmt.Errorf("userMetadataDecode err: %w", err)
		}
		proposal.UserID = um.UserID

		// Comments count
		commentsCount, ok := commentsCounts[proposal.Token]
		if !ok {
			log.Errorf("Comments count for proposal %v not returned by API", proposal.Token)
			continue
		}
		proposal.CommentsCount = int32(commentsCount)

		// Vote summary data
		summary, ok := voteSummaries[proposal.Token]
		if !ok {
			log.Errorf("Vote summary for proposal %v not returned by API", proposal.Token)
			continue
		}
		proposal.VoteStatus = summary.Status
		proposal.VoteResults = summary.Results
		proposal.EligibleTickets = summary.EligibleTickets
		proposal.StartBlockHeight = summary.StartBlockHeight
		proposal.EndBlockHeight = summary.EndBlockHeight
		proposal.QuorumPercentage = summary.QuorumPercentage
		proposal.PassPercentage = summary.PassPercentage

		var totalVotes uint64
		for _, v := range summary.Results {
			totalVotes += v.Votes
		}
		proposal.TotalVotes = totalVotes

		// Status change metadata
		statusTimestamps, changeMsg, err := statusChangeMetadataDecode(record.Metadata)
		if err != nil {
			return nil, fmt.Errorf("statusChangeMetadataDecode err: %w", err)
		}
		proposal.PublishedAt = uint64(statusTimestamps.published)
		proposal.CensoredAt = uint64(statusTimestamps.censored)
		proposal.AbandonedAt = uint64(statusTimestamps.abandoned)
		proposal.StatusChangeMsg = changeMsg

		// Append proposal after inserting the relevant data
		proposals = append(proposals, proposal)
	}

	return proposals, nil
}

// fetchVettedTokens fetches all vetted tokens ordered by the timestamp of
// their last status change.
func (db *ProposalsDB) fetchVettedTokensInventory() ([]string, error) {
	page := uint32(1)
	var vettedTokens []string
	for {
		inventoryReq := recordsv1.InventoryOrdered{
			State: recordsv1.RecordStateVetted,
			Page:  page,
		}
		reply, err := db.client.RecordInventoryOrdered(inventoryReq)
		if err != nil {
			return nil, fmt.Errorf("pi client RecordInventoryOrdered err: %w", err)
		}

		vettedTokens = append(vettedTokens, reply.Tokens...)

		if len(reply.Tokens) < int(recordsv1.InventoryPageSize) {
			// Break loop if we fetch last page. An empty token slice is
			// returned if we request an non-existent/empty page, so in the
			// case of the last page size being equal to the limit page size,
			// we'll fetch an empty page afterwords and know the last page was
			// fetched.
			break
		}

		page++
	}

	return vettedTokens, nil
}

// fetchRecordDetails fetches the record details of the given proposal tokens.
func (db *ProposalsDB) fetchRecordDetails(tokens []string) (map[string]*recordsv1.Record, error) {
	records := make(map[string]*recordsv1.Record, len(tokens))
	for _, token := range tokens {
		detailsReq := recordsv1.Details{
			Token: token,
		}
		dr, err := db.client.RecordDetails(detailsReq)
		if err != nil {
			return nil, fmt.Errorf("pi client RecordDetails err: %w", err)
		}
		records[token] = dr
	}

	return records, nil
}

// fetchCommentsCounts fetches the comments counts for the given proposal tokens.
func (db *ProposalsDB) fetchCommentsCounts(tokens []string) (map[string]uint32, error) {
	commentsCounts := make(map[string]uint32, len(tokens))
	paginatedTokens := paginateTokens(tokens, commentsv1.CountPageSize)
	for i := range paginatedTokens {
		cr, err := db.client.CommentCount(commentsv1.Count{
			Tokens: paginatedTokens[i],
		})
		if err != nil {
			return nil, fmt.Errorf("pi client CommentCount err: %w", err)
		}
		for token, count := range cr.Counts {
			commentsCounts[token] = count
		}
	}
	return commentsCounts, nil
}

// fetchTicketVoteSummaries fetches the vote summaries for the given proposal tokens.
func (db *ProposalsDB) fetchTicketVoteSummaries(tokens []string) (map[string]ticketvotev1.Summary, error) {
	voteSummaries := make(map[string]ticketvotev1.Summary, len(tokens))
	paginatedTokens := paginateTokens(tokens, ticketvotev1.SummariesPageSize)
	for i := range paginatedTokens {
		sr, err := db.client.TicketVoteSummaries(ticketvotev1.Summaries{
			Tokens: paginatedTokens[i],
		})
		if err != nil {
			return nil, fmt.Errorf("pi client TicketVoteSummaries err: %w", err)
		}
		for token := range sr.Summaries {
			voteSummaries[token] = sr.Summaries[token]
		}
	}
	return voteSummaries, nil
}

// paginateTokens paginates tokens in a matrix according to the provided
// page size.
func paginateTokens(tokens []string, pageSize uint32) [][]string {
	n := len(tokens) / int(pageSize) // number of pages needed
	if len(tokens)%int(pageSize) != 0 {
		n++
	}
	ts := make([][]string, n)
	page := 0
	for i := 0; i < len(tokens); i++ {
		if len(ts[page]) >= int(pageSize) {
			page++
		}
		ts[page] = append(ts[page], tokens[i])
	}
	return ts
}

// fetchTicketVoteResults fetches the vote data for the given proposal token,
// then builds and returns its parsed chart data.
func (db *ProposalsDB) fetchTicketVoteResults(token string) (*pitypes.ProposalChartData, error) {
	// Fetch ticket votes details to acquire vote bits options info.
	details, err := db.client.TicketVoteDetails(ticketvotev1.Details{
		Token: token,
	})
	if err != nil {
		return nil, fmt.Errorf("pi client TicketVoteDetails err: %w", err)
	}

	// Maps the vote bits option to their respective string ID.
	voteOptsMap := make(map[uint64]string)
	for _, opt := range details.Vote.Params.Options {
		voteOptsMap[opt.Bit] = opt.ID
	}

	tvr, err := db.client.TicketVoteResults(ticketvotev1.Results{
		Token: token,
	})
	if err != nil {
		return nil, fmt.Errorf("pi client TicketVoteResults err: %w", err)
	}

	// Parse proposal chart data from the ticket vote results reply and
	// sort it afterwords.
	type voteData struct {
		yes, no   uint64
		timestamp int64
	}
	votes := make([]*voteData, 0, len(tvr.Votes))
	for iv := range tvr.Votes {
		// Vote bit comes as a hexadecimal number in the format of a string.
		// Convert it to uint64.
		bit, err := strconv.ParseUint(tvr.Votes[iv].VoteBit, 16, 64)
		if err != nil {
			return nil, err
		}

		// Verify vote bit is valid.
		err = voteBitVerify(details.Vote.Params.Options,
			details.Vote.Params.Mask, bit)
		if err != nil {
			return nil, err
		}

		// Parse relevant data.
		var vd voteData
		switch voteOptsMap[bit] {
		case ticketvotev1.VoteOptionIDApprove:
			vd.yes = 1
			vd.no = 0
		case ticketvotev1.VoteOptionIDReject:
			vd.no = 1
			vd.yes = 0
		default:
			log.Warnf("Unknown vote option ID %v", voteOptsMap[bit])
			continue
		}
		vd.timestamp = tvr.Votes[iv].Timestamp
		votes = append(votes, &vd)
	}
	sort.Slice(votes, func(i, j int) bool {
		return votes[i].timestamp < votes[j].timestamp
	})

	// Build data for the returned proposal chart data object.
	var (
		yes   = make([]uint64, 0, len(votes))
		no    = make([]uint64, 0, len(votes))
		times = make([]int64, 0, len(votes))
	)
	for _, vote := range votes {
		yes = append(yes, vote.yes)
		no = append(no, vote.no)
		times = append(times, vote.timestamp)
	}

	return &pitypes.ProposalChartData{
		Yes:  yes,
		No:   no,
		Time: times,
	}, nil
}

// proposalsSave saves the batch proposals data to the db. This is ran when the
// proposals sync function finds new proposals that don't exist on our db yet.
// Before saving a proposal to the db, set the synced property to false to
// indicate that the proposal is not fully synced with politeia yet.
func (db *ProposalsDB) proposalsSave(proposals []*pitypes.ProposalRecord) error {
	for _, proposal := range proposals {
		proposal.Synced = false
		err := db.dbP.Save(proposal)
		if errors.Is(err, storm.ErrAlreadyExists) {
			// Proposal exists, update instead of inserting new.
			data, err := db.ProposalByToken(proposal.Token)
			if err != nil {
				return fmt.Errorf("ProposalsDB ProposalByToken err: %w", err)
			}
			updateData := *proposal
			updateData.ID = data.ID
			err = db.dbP.Update(&updateData)
			if err != nil {
				return fmt.Errorf("stormdb update err: %w", err)
			}
		}
		if err != nil {
			return fmt.Errorf("stormdb save err: %w", err)
		}
	}

	return nil
}

// proposal is used to retrieve proposals from stormdb given the search
// arguments passed in.
func (db *ProposalsDB) proposal(searchBy, searchTerm string) (*pitypes.ProposalRecord, error) {
	var proposal pitypes.ProposalRecord
	err := db.dbP.Select(q.Eq(searchBy, searchTerm)).Limit(1).First(&proposal)
	if err != nil {
		log.Errorf("Failed to fetch data from Proposals DB: %v", err)
		return nil, err
	}

	return &proposal, nil
}

// proposalsNewUpdate verifies if there is any new proposals on the politeia
// server that are not yet synced with our stormdb.
func (db *ProposalsDB) proposalsNewUpdate() error {
	log.Infof("Loading all proposal records from DB...")
	var proposals []*pitypes.ProposalRecord
	err := db.dbP.All(&proposals)
	if err != nil {
		return fmt.Errorf("stormdb All err: %w", err)
	}
	log.Infof("Loaded %d proposal records from DB...", len(proposals))

	// Create proposals map from local stormdb proposals.
	proposalsMap := make(map[string]struct{}, len(proposals))
	for _, prop := range proposals {
		proposalsMap[prop.Token] = struct{}{}
	}

	// Empty db so first time fetching proposals, fetch all vetted tokens.
	log.Infof("Fetching all proposal tokens...")
	tokens, err := db.fetchVettedTokensInventory()
	if err != nil {
		return err
	}

	// Filter new proposals to be fetched.
	var newTokens []string
	for _, token := range tokens {
		if _, ok := proposalsMap[token]; ok {
			continue
		}
		// New proposal found.
		newTokens = append(newTokens, token)
	}

	if len(newTokens) == 0 {
		log.Infof("No new proposals found.")
		return nil
	}

	// Fetch data for found tokens.
	log.Infof("Fetching data for %d new proposals...", len(newTokens))
	prs, err := db.fetchProposalsData(newTokens)
	if err != nil {
		return err
	}
	log.Infof("Obtained data for %d new proposals.", len(prs)) // always equal length?

	// Save proposals data in the db.
	return db.proposalsSave(prs)
}

// proposalsInProgressUpdate retrieves proposals with the vote status equal to
// unauthorized, authorized and started. Afterwords, it proceeds to check with
// newly fetched data if any of them need to be updated on stormdb.
func (db *ProposalsDB) proposalsInProgressUpdate() error {
	var propsInProgress []*pitypes.ProposalRecord
	err := db.dbP.Select(
		q.Or(
			q.Eq("VoteStatus", ticketvotev1.VoteStatusUnauthorized),
			q.Eq("VoteStatus", ticketvotev1.VoteStatusAuthorized),
			q.Eq("VoteStatus", ticketvotev1.VoteStatusStarted),
		),
	).Find(&propsInProgress)
	if err != nil && !errors.Is(err, storm.ErrNotFound) {
		return err
	}

	log.Infof("Fetching data for %d in-progress proposals...", len(propsInProgress))
	for _, prop := range propsInProgress {
		// Fetch fresh data for the proposal.
		proposals, err := db.fetchProposalsData([]string{prop.Token})
		if err != nil {
			return fmt.Errorf("fetchProposalsData failed with err: %w", err)
		}
		proposal := proposals[0]

		// Ticket vote results is an expensive API call, so we check
		// appropriate conditions to call it. Vote status needs to be started
		// for in progress proposals. Then we check the total votes to see
		// if any new votes has been cast. Then we check if chart data is nil,
		// which means first time fetching ticket vote data.
		if prop.VoteStatus == ticketvotev1.VoteStatusStarted &&
			(prop.TotalVotes != proposal.TotalVotes || prop.ChartData == nil) {
			t0 := time.Now()
			log.Infof("Fetching vote results for proposal %v (status %v)...", prop.Token, recordsv1.RecordStatuses[prop.Status])
			voteResults, err := db.fetchTicketVoteResults(prop.Token)
			if err != nil {
				return fmt.Errorf("fetchTicketVoteResults failed with err: %w", err)
			}
			proposal.ChartData = voteResults
			log.Infof("Retrieved vote results for proposal %v in %v.", prop.Token, time.Since(t0))
		}

		if prop.IsEqual(*proposal) {
			// No changes made to proposal, skip db call.
			continue
		}

		// Insert ID from storm DB to update proposal.
		proposal.ID = prop.ID

		err = db.dbP.Update(proposal)
		if err != nil {
			return fmt.Errorf("storm db Update failed with err: %w", err)
		}
	}

	return nil
}

// proposalsVoteResultsUpdate verifies if there is still a need to update vote
// results data for proposals with the vote status equal to finished, approved
// and rejected. This is the final sync between dcrdata and politeia servers
// for proposals with the final finished/approved/rejected vote status.
func (db *ProposalsDB) proposalsVoteResultsUpdate() error {
	// Get proposals that need to be synced
	var propsVotingComplete []*pitypes.ProposalRecord
	err := db.dbP.Select(
		q.Or(
			q.And(
				q.Eq("VoteStatus", ticketvotev1.VoteStatusFinished),
				q.Eq("Synced", false),
			),
			q.And(
				q.Eq("VoteStatus", ticketvotev1.VoteStatusApproved),
				q.Eq("Synced", false),
			),
			q.And(
				q.Eq("VoteStatus", ticketvotev1.VoteStatusRejected),
				q.Eq("Synced", false),
			),
		),
	).Find(&propsVotingComplete)
	if err != nil && !errors.Is(err, storm.ErrNotFound) {
		return err
	}

	// Update finished proposals that are not yet synced with the
	// latest vote results.
	for _, prop := range propsVotingComplete {
		t0 := time.Now()
		log.Infof("Fetching vote results for proposal %v (status %v)...", prop.Token, recordsv1.RecordStatuses[prop.Status])
		voteResults, err := db.fetchTicketVoteResults(prop.Token)
		if err != nil {
			return fmt.Errorf("fetchTicketVoteResults failed with err: %w", err)
		}
		prop.ChartData = voteResults
		prop.Synced = true

		err = db.dbP.Update(prop)
		if err != nil {
			return fmt.Errorf("storm db Update failed with err: %w", err)
		}
		log.Infof("Retrieved vote results for proposal %v in %v.", prop.Token, time.Since(t0))
	}

	return nil
}
