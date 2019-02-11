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
	ID              int                `storm:"id,increment"`
	Name            string             `json:"name"`
	State           ProposalStateType  `json:"state"`
	Status          ProposalStatusType `json:"status"`
	Timestamp       uint64             `json:"timestamp" storm:"index"`
	UserID          string             `json:"userid"`
	Username        string             `json:"username"`
	PublicKey       string             `json:"publickey"`
	Signature       string             `json:"signature"`
	Version         string             `json:"version"`
	Censorship      CensorshipRecord   `json:"censorshiprecord"`
	Files           []AttachmentFile   `json:"files"`
	Numcomments     int32              `json:"numcomments"`
	StatusChangeMsg string             `json:"statuschangemessage"`
	PubishedDate    uint64             `json:"publishedat"`
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

	var publicProposals Proposals
	err = json.Unmarshal(data, &publicProposals)
	if err != nil || len(publicProposals.Data) == 0 {
		return 0, err
	}

	data, err = db.handleGetRequests(db.politeiaURL, voteStatusesRoute, URLParams)
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
		if err = db.offNode.Save(val); err != nil {
			return i, fmt.Errorf("node save operation failed: %v", err)
		}
	}

	return len(publicProposals.Data), err
}

// AllProposals fetches all the proposals data saved to the db.
func AllProposals() (proposals []*ProposalInfo, err error) {
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

	err = adb.offNode.All(&proposals)
	if err != nil {
		log.Errorf("Failed to fetch data from Agendas DB: %v", err)
	}

	return
}

// ProposalByToken returns the proposal identified by the provided token.
func ProposalByToken(token string) (proposal *ProposalInfo, err error) {
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

	var proposals []*ProposalInfo

	err = adb.offNode.Find("Censorship.Token", token, &proposals, storm.Limit(1))
	if err != nil {
		log.Errorf("Failed to fetch data from Agendas DB: %v", err)
	}

	if len(proposals) > 1 && proposals[0] != nil {
		proposal = proposals[0]
	}

	return
}

func (db *AgendaDB) lastSavedProposal() (lastP []*ProposalInfo, err error) {
	err = db.offNode.All(&lastP, storm.Limit(1), storm.Reverse())
	return
}

// checkOffChainUpdates updates the on chain changes if they exist.
func (db *AgendaDB) checkOffChainUpdates() (int, error) {
	lastProposal, err := db.lastSavedProposal()
	if err != nil {
		return 0, fmt.Errorf("lastSavedProposal failed: %v", err)
	}

	var queryParam string

	if len(lastProposal) > 0 {
		queryParam = fmt.Sprintf("?after=%v", lastProposal[0].Censorship.Token)
	}

	return db.saveProposals(queryParam)
}
