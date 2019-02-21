// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package types

import piapi "github.com/decred/politeia/politeiawww/api/v1"

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

// Proposals defines an array of proposals as returned by RouteAllVetted.
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

// Votes defines a slice of VotesStatuses as returned by RouteAllVoteStatus.
type Votes struct {
	Data []*ProposalVotes `json:"votesstatus"`
}

// Results defines the actual vote count info per the votes choices available.
type Results struct {
	Option        VoteOption `json:"option"`
	VotesReceived int64      `json:"votesreceived"`
}

// VoteOption defines the actual high level vote results for the specific agenda.
type VoteOption struct {
	OptionID    string `json:"id"`
	Description string `json:"description"`
	Bits        int32  `json:"bits"`
}

// ProposalStatusType defines the various proposal statuses available as referenced
// in https://github.com/decred/politeia/blob/master/politeiawww/api/v1/v1.go
type ProposalStatusType piapi.PropStatusT

func (p ProposalStatusType) String() string {
	return piapi.PropStatus[piapi.PropStatusT(p)]
}

// ProposalStatusFrmStr converts the string into ProposalStatusType value.
func ProposalStatusFrmStr(val string) ProposalStatusType {
	for key, status := range piapi.PropStatus {
		if status == val {
			return ProposalStatusType(key)
		}
	}

	// returns invalid proposal status as the default.
	return ProposalStatusType(piapi.PropStatusInvalid)
}

// VoteStatusType defines the various vote statuses available as referenced in
// https://github.com/decred/politeia/blob/master/politeiawww/api/v1/v1.go
type VoteStatusType piapi.PropVoteStatusT

// shorterDesc maps the short description to there respective proposal description.
var shorterDesc = map[piapi.PropVoteStatusT]string{
	piapi.PropVoteStatusInvalid:       "Invalid",
	piapi.PropVoteStatusNotAuthorized: "Not Authorized",
	piapi.PropVoteStatusAuthorized:    "Authorized",
	piapi.PropVoteStatusStarted:       "Started",
	piapi.PropVoteStatusFinished:      "Finished",
	piapi.PropVoteStatusDoesntExist:   "Doesn't Exist",
}

// VoteStatusType stringer returns the shorter vote status description.
func (s VoteStatusType) String() string {
	return shorterDesc[piapi.PropVoteStatusT(s)]
}

// Description returns the long vote status description.
func (s VoteStatusType) Description() string {
	return piapi.PropVoteStatus[piapi.PropVoteStatusT(s)]
}

// VoteStatusTypeFromStr string version of the status to VoteStatusType.
func VoteStatusTypeFromStr(val string) VoteStatusType {
	for key, status := range piapi.PropVoteStatus {
		if status == val {
			return VoteStatusType(key)
		}
	}
	// Invalid votes status is returned as the default..
	return VoteStatusType(piapi.PropVoteStatusInvalid)
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
