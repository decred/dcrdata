// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package offchaindb

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
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
	ID              int                `json:"id" storm:"id,increment"`
	Name            string             `json:"name"`
	State           ProposalStateType  `json:"state"`
	Status          ProposalStatusType `json:"status"`
	Timestamp       uint64             `json:"timestamp"`
	UserID          string             `json:"userid"`
	Username        string             `json:"username"`
	PublicKey       string             `json:"publickey"`
	Signature       string             `json:"signature"`
	Version         string             `json:"version"`
	Censorship      CensorshipRecord   `json:"censorshiprecord"`
	Files           []AttachmentFile   `json:"files"`
	NumComments     int32              `json:"numcomments"`
	StatusChangeMsg string             `json:"statuschangemessage"`
	PublishedDate   uint64             `json:"publishedat" storm:"index"`
	CensoredDate    uint64             `json:"censoredat"`
	AbandonedDate   uint64             `json:"abandonedat"`
	VotesStatus     *ProposalVotes     `json:"votes"`
}

// Proposals defines an array of proposals as returned by vettedProposalsRoute.
type Proposals struct {
	Data []*ProposalInfo `json:"proposals"`
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
	Token              string         `json:"token"`
	Status             VoteStatusType `json:"status"`
	VoteResults        []Results      `json:"optionsresult"`
	TotalVotes         int64          `json:"totalvotes"`
	Endheight          string         `json:"endheight"`
	NumOfEligibleVotes int64          `json:"numofeligiblevotes"`
	QuorumPercentage   uint32         `json:"quorumpercentage"`
	PassPercentage     uint32         `json:"passpercentage"`
}

// Votes defines a slice of VotesStatuses as returned by voteStatusesRoute.
type Votes struct {
	Data []*ProposalVotes `json:"votesstatus"`
}

// Results defines the actual vote count info per the votes choices available.
type Results struct {
	Option        VoteResults `json:"option"`
	VotesReceived int64       `json:"votesreceived"`
}

// VoteResults defines the actual high level vote results for the specific agenda.
type VoteResults struct {
	OptionID    string `json:"id"`
	Description string `json:"description"`
	Bits        int32  `json:"bits"`
}

// VoteStatusType defines the various vote statuses available.
type VoteStatusType int8

const (
	// InvalidVoteStatus defines the invalid votes status.
	InvalidVoteStatus VoteStatusType = iota

	// NotAuthorizedVoteStatus defines status where the vote has not been
	// authorized by author
	NotAuthorizedVoteStatus

	// AuthorizedVoteStatus defines status where the vote has been authorized by author.
	AuthorizedVoteStatus

	// StartedVoteStatus defines started votes status.
	StartedVoteStatus

	// FinishedVoteStatus defines the finished votes status.
	FinishedVoteStatus

	// NotFoundVoteStatus defines the not found votes status(doesn't exist).
	NotFoundVoteStatus
	UnknownVoteStatus
)

func (s VoteStatusType) String() string {
	switch s {
	case InvalidVoteStatus:
		return "invalid"
	case NotAuthorizedVoteStatus:
		return "not authorized"
	case AuthorizedVoteStatus:
		return "authorized"
	case StartedVoteStatus:
		return "started"
	case FinishedVoteStatus:
		return "finished"
	case NotFoundVoteStatus:
		return "doesn't exit"
	default:
		return "unknown"
	}
}

// VoteStatusTypeFromStr string version of the status to VoteStatusType.
func VoteStatusTypeFromStr(val string) VoteStatusType {
	switch val {
	case "invalid":
		return InvalidVoteStatus
	case "not authorized":
		return NotAuthorizedVoteStatus
	case "authorized":
		return AuthorizedVoteStatus
	case "started":
		return StartedVoteStatus
	case "finished":
		return FinishedVoteStatus
	case "doesn't exit":
		return NotFoundVoteStatus
	default:
		return UnknownVoteStatus
	}
}

// ProposalStatusType defines the various
type ProposalStatusType int8

const (
	// InvalidProposalStatus defines the Invalid proposal status
	InvalidProposalStatus ProposalStatusType = iota

	// NotFoundProposalStatus defines not found proposal status
	NotFoundProposalStatus

	// NotReviewedProposalStatus defines a proposal that has not been reviewed.
	NotReviewedProposalStatus

	// CensoredProposalStatus defines a proposal that has been censored.
	CensoredProposalStatus

	// PublicProposalStatus defines a  proposal that is publicly visible.
	PublicProposalStatus

	// UnreviewedChangesProposalStatus defines a proposal that is not public and
	// has unreviewed changes.
	UnreviewedChangesProposalStatus

	// AbandonedProposalStatus defines a proposal that has been declared
	// abandoned by an admin.
	AbandonedProposalStatus
	UnknownProposalStatus
)

func (p ProposalStatusType) String() string {
	switch p {
	case InvalidProposalStatus:
		return "invalid"
	case NotFoundProposalStatus:
		return "not found"
	case NotReviewedProposalStatus:
		return "not reviewed"
	case CensoredProposalStatus:
		return "censored"
	case PublicProposalStatus:
		return "public"
	case UnreviewedChangesProposalStatus:
		return "unreviewed changes"
	case AbandonedProposalStatus:
		return "abandoned"
	default:
		return "unknown"
	}
}

// ProposalStatusFrmStr converts the string into ProposalStatusType value.
func ProposalStatusFrmStr(val string) ProposalStatusType {
	switch val {
	case "invalid":
		return InvalidProposalStatus
	case "not found":
		return NotFoundProposalStatus
	case "not reviewed":
		return NotReviewedProposalStatus
	case "censored":
		return CensoredProposalStatus
	case "public":
		return PublicProposalStatus
	case "unreviewed changes":
		return UnreviewedChangesProposalStatus
	case "abandoned":
		return AbandonedProposalStatus
	default:
		return UnknownProposalStatus
	}
}

// ProposalStateType defines the proposal state entry.
type ProposalStateType int8

const (
	// InvalidState defines the invalid state proposals.
	InvalidState ProposalStateType = iota

	// UnvettedState defines the unvetted state proposals and includes proposals
	// with a status of:
	//   * PropStatusNotReviewed
	//   * PropStatusUnreviewedChanges
	//   * PropStatusCensored
	UnvettedState

	// VettedState defines the vetted state proposals and includes proposals
	// with a status of:
	//   * PropStatusPublic
	//   * PropStatusAbandoned
	VettedState
	UnknownState
)

func (f ProposalStateType) String() string {
	switch f {
	case InvalidState:
		return "invalid"
	case UnvettedState:
		return "unvetted"
	case VettedState:
		return "vetted"
	default:
		return "unknown"
	}
}

// ProposalStateFromStr converts the string into ProposalStateType value.
func ProposalStateFromStr(val string) ProposalStateType {
	switch val {
	case "invalid":
		return InvalidState
	case "unvetted":
		return UnvettedState
	case "vetted":
		return VettedState
	default:
		return UnknownState
	}
}

// ProposalDB defines the common data needed to query the proposals db.
type ProposalDB struct {
	dbP          *storm.DB
	client       *http.Client
	NumProposals int
	_APIURLpath  string
}

// errDef defines the default error returned if the proposals db was initialized
// correctly.
var errDef = fmt.Errorf("ProposalDB was not initialized correctly")

// NewProposalsDB opens an exiting database or creates a new db instance with the
// provided file name. returns an initialized instance of Proposal DB, http client
// and the politeia API URL path to be used.
func NewProposalsDB(politeiaURL, dbPath string) (*ProposalDB, error) {
	_, err := os.Stat(dbPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	if politeiaURL == "" {
		return nil, fmt.Errorf("missing politeia API URL")
	}

	if dbPath == "" {
		return nil, fmt.Errorf("missing db path")
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

// countProperties fetches the proposal count and appends it the ProposalDB instance
// provided.
func (db *ProposalDB) countProperties() error {
	count, err := db.dbP.Count(&ProposalInfo{})
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

// handleGetRequests constructs the full URL path, querys the API endpoints and
// returns the queried data in form of byte array and an error if it exists.
func (db *ProposalDB) handleGetRequests(path, params string) ([]byte, error) {
	response, err := db.client.Get(db._APIURLpath + path + params)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	return ioutil.ReadAll(response.Body)
}

// saveProposals adds the proposals data to the db.
func (db *ProposalDB) saveProposals(URLParams string) (int, error) {
	data, err := db.handleGetRequests(vettedProposalsRoute, URLParams)
	if err != nil {
		return 0, err
	}

	var publicProposals Proposals
	err = json.Unmarshal(data, &publicProposals)
	if err != nil || len(publicProposals.Data) == 0 {
		return 0, err
	}

	data, err = db.handleGetRequests(voteStatusesRoute, URLParams)
	if err != nil {
		return 0, err
	}

	var votesInfo Votes
	err = json.Unmarshal(data, &votesInfo)
	if err != nil {
		return 0, err
	}

	// Append the votes status information to the respective proposals if it exits.
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
		// val.ID = val.Censorship.Token
		if err = db.dbP.Save(val); err != nil {
			return i, fmt.Errorf("save operation failed: %v", err)
		}
	}

	return len(publicProposals.Data), err
}

// AllProposals fetches all the proposals data saved to the db.
func (db *ProposalDB) AllProposals() (proposals []*ProposalInfo, err error) {
	if db == nil || db.dbP == nil {
		return nil, errDef
	}

	err = db.dbP.All(&proposals)
	if err != nil {
		log.Errorf("Failed to fetch data from Agendas DB: %v", err)
	}

	return
}

// ProposalByID returns the single proposal identified by the provided id.
func (db *ProposalDB) ProposalByID(proposalID int) (proposal *ProposalInfo, err error) {
	if db == nil || db.dbP == nil {
		return nil, errDef
	}

	var proposals []*ProposalInfo

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

// CheckOffChainUpdates updates the on chain changes if they exist.
func (db *ProposalDB) CheckOffChainUpdates() error {
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

	log.Infof("%d off-chain records (politeia proposals) were updated", numRecords)

	return db.countProperties()
}

func (db *ProposalDB) lastSavedProposal() (lastP []*ProposalInfo, err error) {
	err = db.dbP.All(&lastP, storm.Limit(1), storm.Reverse())
	return
}
