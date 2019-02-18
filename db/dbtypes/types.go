// Copyright (c) 2018-2019, The Decred developers
// See LICENSE for details.

package dbtypes

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrdata/v4/db/dbtypes/internal"
	"github.com/decred/dcrdata/v4/txhelpers"
)

var (
	// PGCancelError is the error string PostgreSQL returns when a query fails
	// to complete due to user requested cancellation.
	PGCancelError       = "pq: canceling statement due to user request"
	CtxDeadlineExceeded = context.DeadlineExceeded.Error()
	TimeoutPrefix       = "TIMEOUT of PostgreSQL query"
)

// IsTimeout checks if the message is prefixed with the expected DB timeout
// message prefix.
func IsTimeout(msg string) bool {
	// Contains is used instead of HasPrefix since error messages are often
	// supplemented with additional information.
	return strings.Contains(msg, TimeoutPrefix) ||
		strings.Contains(msg, CtxDeadlineExceeded)
}

// IsTimeout checks if error's message is prefixed with the expected DB timeout
// message prefix.
func IsTimeoutErr(err error) bool {
	return err != nil && IsTimeout(err.Error())
}

// TimeDef is time.Time wrapper that formats time by default as a string without
// a timezone. The time Stringer interface formats the time into a string
// with a timezone.
type TimeDef struct {
	T time.Time
}

const (
	timeDefFmtHuman = "2006-01-02 15:04:05 (MST)"
	timeDefFmtJS    = time.RFC3339
)

// String formats the time in a human-friendly layout. This may be used when
// TimeDef values end up on the explorer pages.
func (t TimeDef) String() string {
	return t.T.Format(timeDefFmtHuman)
}

// RFC3339 formats the time in a machine-friendly layout.
func (t TimeDef) RFC3339() string {
	return t.T.Format(timeDefFmtJS)
}

// UNIX returns the UNIX epoch time stamp.
func (t TimeDef) UNIX() int64 {
	return t.T.Unix()
}

func (t TimeDef) Format(layout string) string {
	return t.T.Format(layout)
}

// MarshalJSON is set as the default marshalling function for TimeDef struct.
func (t *TimeDef) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.RFC3339())
}

// NewTimeDef constructs a TimeDef from the given time.Time. It presets the
// timezone for formatting to UTC.
func NewTimeDef(t time.Time) TimeDef {
	return TimeDef{
		T: t.UTC(),
	}
}

// NewTimeDefFromUNIX constructs a TimeDef from the given UNIX epoch time stamp
// in seconds. It presets the timezone for formatting to UTC.
func NewTimeDefFromUNIX(t int64) TimeDef {
	return NewTimeDef(time.Unix(t, 0))
}

// Scan implements the sql.Scanner interface for TimeDef. This will not
// reinterpret the stored time string for a particular time zone. That is, if
// the stored time stamp shows no time zone (as with TIMESTAMP), the default
// time.Time scanner will load it as a local time, and this Scan converts to
// UTC. If the timestamp has a timezone (as with TIMESTAMPTZ), including UTC
// explicitly set, it will be accounted for when converting to UTC. All this
// Scan implementation does beyond the default time.Time scanner is to set the
// time.Time's location to UTC, which keeps the instant in time the same,
// adjusting the numbers in the time string to the equivalent time in UTC. For
// example, if the time read from the DB is "2016-02-08 12:00:00" (with no time
// zone) and the server time zone is CST (UTC-6), this ensures the default
// displayed time string is in UTC: "2016-02-08 18:00:00Z". On the other hand,
// if the time read from the DB is "2016-02-08 12:00:00+6", it does not matter
// what the server time zone is set to, and the time will still be converted to
// UTC as "2016-02-08 18:00:00Z".
func (t *TimeDef) Scan(src interface{}) error {
	srcTime, ok := src.(time.Time)
	if !ok {
		return fmt.Errorf("scanned value not a time.Time")
	}
	// Debug:
	// fmt.Printf("srcTime: %v, location: %p\n", srcTime, srcTime.Location()) // valid location not set!

	// Set location to UTC. This does not shift the UNIX epoch time.
	t.T = srcTime.UTC()

	// Debug:
	// fmt.Printf("t: %v, t.T: %v, location: %s\n", t, t.T, t.T.Location().String())
	return nil
}

// Value implements the sql.Valuer interface. It ensures that the Time Values
// are for the UTC time zone. Times will only survive a round trip to and from
// the DB tables if they are stored from a time.Time with Location set to UTC.
func (t TimeDef) Value() (driver.Value, error) {
	return t.T.UTC(), nil
}

// Ensure TimeDef satisfies sql.Valuer.
var _ driver.Valuer = (*TimeDef)(nil)

// TimeDefLocal satisfies the sql.Valuer interface, but it provides Local Time.
type TimeDefLocal TimeDef

// Value implements the sql.Valuer interface. It ensures that the Time Values
// are for the Local time zone. It is unlikely to be desirable to store values
// this way. Only storing a time.Time in UTC allows round trip fidelity.
func (t TimeDefLocal) Value() (driver.Value, error) {
	return t.T.Local(), nil
}

// Ensure TimeDefLocal satisfies sql.Valuer.
var _ driver.Valuer = (*TimeDefLocal)(nil)

// Tickets have 6 states, 5 possible fates:
// Live -...---> Voted
//           \-> Missed (unspent) [--> Revoked]
//            \--...--> Expired (unspent) [--> Revoked]

type TicketSpendType int16

const (
	TicketUnspent TicketSpendType = iota
	TicketRevoked
	TicketVoted
)

func (p TicketSpendType) String() string {
	switch p {
	case TicketUnspent:
		return "unspent"
	case TicketRevoked:
		return "revoked"
	case TicketVoted:
		return "Voted"
	default:
		return "unknown"
	}
}

// AgendaStatusType defines the various agenda statuses.
type AgendaStatusType int8

const (
	// InitialAgendaStatus is the agenda status when the agenda is not yet up for
	// voting and the votes tally is not also available.
	InitialAgendaStatus AgendaStatusType = iota

	// StartedAgendaStatus is the agenda status when the agenda is up for voting.
	StartedAgendaStatus

	// FailedAgendaStatus is the agenda status set when the votes tally does not
	// attain the minimum threshold set. Activation height is not set for such an
	// agenda.
	FailedAgendaStatus

	// LockedInAgendaStatus is the agenda status when the agenda is considered to
	// have passed after attaining the minimum set threshold. This agenda will
	// have its activation height set.
	LockedInAgendaStatus

	// ActivatedAgendaStatus is the agenda status chaincfg.RuleChangeActivationInterval
	// blocks (e.g. 8064 blocks = 2016 * 4 for 4 weeks on mainnet) after
	// LockedInAgendaStatus ("lockedin") that indicates when the rule change is to
	// be effected. https://docs.decred.org/glossary/#rule-change-interval-rci.
	ActivatedAgendaStatus

	UnknownStatus
)

func (a AgendaStatusType) String() string {
	switch a {
	case InitialAgendaStatus:
		return "defined"
	case StartedAgendaStatus:
		return "started"
	case FailedAgendaStatus:
		return "failed"
	case LockedInAgendaStatus:
		return "lockedin"
	case ActivatedAgendaStatus:
		return "active"
	default:
		return "unknown"
	}
}

// Ensure at compile time that AgendaStatusType satisfies interface json.Marshaller.
var _ json.Marshaler = (*AgendaStatusType)(nil)

// AgendaStatusType default marshaller.
func (a AgendaStatusType) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.String())
}

// AgendaStatusFromStr creates an agenda status from a string.
func AgendaStatusFromStr(status string) AgendaStatusType {
	switch strings.ToLower(status) {
	case "defined":
		return InitialAgendaStatus
	case "started":
		return StartedAgendaStatus
	case "failed":
		return FailedAgendaStatus
	case "lockedin":
		return LockedInAgendaStatus
	case "active":
		return ActivatedAgendaStatus
	default:
		return UnknownStatus
	}
}

// AddrTxnType enumerates the different transaction types as displayed by the
// address page.
type AddrTxnType int

const (
	AddrTxnAll AddrTxnType = iota
	AddrTxnCredit
	AddrTxnDebit
	AddrMergedTxnDebit
	AddrMergedTxnCredit
	AddrMergedTxn
	AddrTxnUnknown
)

// AddrTxnTypes is the canonical mapping from AddrTxnType to string.
var AddrTxnTypes = map[AddrTxnType]string{
	AddrTxnAll:          "all",
	AddrTxnCredit:       "credit",
	AddrTxnDebit:        "debit",
	AddrMergedTxnDebit:  "merged_debit",
	AddrMergedTxnCredit: "merged_credit",
	AddrMergedTxn:       "merged",
	AddrTxnUnknown:      "unknown",
}

func (a AddrTxnType) String() string {
	return AddrTxnTypes[a]
}

// AddrTxnTypeFromStr attempts to decode a string into an AddrTxnType.
func AddrTxnTypeFromStr(txnType string) AddrTxnType {
	txnType = strings.ToLower(txnType)
	switch txnType {
	case "all":
		return AddrTxnAll
	case "credit", "credits":
		return AddrTxnCredit
	case "debit", "debits":
		return AddrTxnDebit
	case "merged_debit", "merged debit":
		return AddrMergedTxnDebit
	case "merged_credit", "merged credit":
		return AddrMergedTxnCredit
	case "merged":
		return AddrMergedTxn
	default:
		return AddrTxnUnknown
	}
}

// TimeBasedGrouping defines the possible ways that a time can be grouped
// according to all, year, month, week or day grouping. This time grouping is
// used in time-based grouping like charts and blocks list view.
type TimeBasedGrouping int8

const (
	AllGrouping TimeBasedGrouping = iota
	YearGrouping
	MonthGrouping
	WeekGrouping
	DayGrouping
	UnknownGrouping
)

// TimeIntervals is a slice of distinct time intervals used for grouping data.
var TimeIntervals = []TimeBasedGrouping{
	YearGrouping,
	MonthGrouping,
	WeekGrouping,
	DayGrouping,
}

const (
	// InitialDBLoad is a sync where data is first loaded from the chain db into
	// the respective dbs currently supported. Runs on both liteMode and fullMode.
	// InitialDBLoad value references the first progress bar id on the status page.
	InitialDBLoad = "initial-load"
	// AddressesTableSync is a sync that runs immediately after initialDBLoad. Data
	// previously loaded into vins table is sync'd with the addresses table.
	// Runs only in fullMode. AddressesTableSync value references the second
	// progress bar id on the status page.
	AddressesTableSync = "addresses-sync"
)

// ProgressBarLoad contains the raw data needed to populate the status sync updates.
// It is used to update the status sync through a channel.
type ProgressBarLoad struct {
	From      int64
	To        int64
	Msg       string
	Subtitle  string
	BarID     string
	Timestamp int64
}

// BlocksGroupedInfo contains the data about a stake difficulty (ticket price) window,
// including intrinsic properties (e.g. window index, ticket price, start block, etc.),
// and aggregate transaction counts (e.g. number of votes, regular transactions,
// new tickets, etc.)
type BlocksGroupedInfo struct {
	// intrinsic properties
	IndexVal           int64
	EndBlock           int64
	Difficulty         float64
	TicketPrice        int64
	StartTime          TimeDef
	FormattedStartTime string
	EndTime            TimeDef
	FormattedEndTime   string
	Size               int64
	FormattedSize      string
	// Aggregate properties
	Voters       uint64
	Transactions uint64
	FreshStake   uint64
	Revocations  uint64
	BlocksCount  int64
}

// TimeBasedGroupings maps a given time grouping to its standard string value.
var TimeBasedGroupings = map[TimeBasedGrouping]string{
	AllGrouping:   "all",
	YearGrouping:  "year",
	MonthGrouping: "month",
	WeekGrouping:  "week",
	DayGrouping:   "day",
}

func (g TimeBasedGrouping) String() string {
	return TimeBasedGroupings[g]
}

// TimeGroupingFromStr converts groupings string to its respective TimeBasedGrouping value.
func TimeGroupingFromStr(groupings string) TimeBasedGrouping {
	switch strings.ToLower(groupings) {
	case "all":
		return AllGrouping
	case "yr", "year", "years":
		return YearGrouping
	case "mo", "month", "months":
		return MonthGrouping
	case "wk", "week", "weeks":
		return WeekGrouping
	case "day", "days":
		return DayGrouping
	default:
		return UnknownGrouping
	}
}

// HistoryChart is used to differentiate the distinct graphs that appear on the
// address history page.
type HistoryChart int8

const (
	TxsType HistoryChart = iota
	AmountFlow
	TotalUnspent
	ChartUnknown
)

type TicketPoolStatus int16

// NB:PoolStatusLive also defines immature tickets in addition to defining live tickets.
const (
	PoolStatusLive TicketPoolStatus = iota
	PoolStatusVoted
	PoolStatusExpired
	PoolStatusMissed
)

// VoteChoice defines the type of vote choice, and the undelying integer value
// is stored in the database (do not change these without upgrading the DB!).
type VoteChoice uint8

const (
	Yes VoteChoice = iota
	Abstain
	No
	VoteChoiceUnknown
)

func (p TicketPoolStatus) String() string {
	switch p {
	case PoolStatusLive:
		return "live"
	case PoolStatusVoted:
		return "Voted"
	case PoolStatusExpired:
		return "expired"
	case PoolStatusMissed:
		return "missed"
	default:
		return "unknown"
	}
}

func (v VoteChoice) String() string {
	switch v {
	case Abstain:
		return "abstain"
	case Yes:
		return "yes"
	case No:
		return "no"
	default:
		return "unknown"
	}
}

// ChoiceIndexFromStr converts the vote choice string to a vote choice index.
func ChoiceIndexFromStr(choice string) (VoteChoice, error) {
	switch choice {
	case "abstain":
		return Abstain, nil
	case "yes":
		return Yes, nil
	case "no":
		return No, nil
	default:
		return VoteChoiceUnknown, fmt.Errorf(`Vote Choice "%s" is unknown`, choice)
	}
}

// MileStone defines the various stages passed by vote on a given agenda.
// Activated is the height at which the delay time begins before a vote activates.
// HardForked is the height at which the consensus rule changes.
// VotingDone is the height at which voting is considered complete or when the
// status changes from "started" to either "failed" or "lockedin".
type MileStone struct {
	ID         int64            `json:"-"`
	Status     AgendaStatusType `json:"status"`
	VotingDone int64            `json:"votingdone"`
	Activated  int64            `json:"activated"`
	HardForked int64            `json:"hardforked"`
	StartTime  time.Time        `json:"starttime"`
	ExpireTime time.Time        `json:"expiretime"`
}

// BlockChainData defines data holding the latest block chain state from the
// getblockchaininfo rpc endpoint.
type BlockChainData struct {
	Chain                  string
	SyncHeight             int64
	BestHeight             int64
	BestBlockHash          string
	Difficulty             uint32
	VerificationProgress   float64
	ChainWork              string
	IsInitialBlockDownload bool
	MaxBlockSize           int64
	AgendaMileStones       map[string]MileStone
}

// SyncResult is the result of a database sync operation, containing the height
// of the last block and an arror value.
type SyncResult struct {
	Height int64
	Error  error
}

// JSONB is used to implement the sql.Scanner and driver.Valuer interfaces
// required for the type to make a postgresql compatible JSONB type.
type JSONB map[string]interface{}

// Value satisfies driver.Valuer
func (p VinTxPropertyARRAY) Value() (driver.Value, error) {
	j, err := json.Marshal(p)
	return j, err
}

// Scan satisfies sql.Scanner
func (p *VinTxPropertyARRAY) Scan(src interface{}) error {
	source, ok := src.([]byte)
	if !ok {
		return fmt.Errorf("scan type assertion .([]byte) failed")
	}

	var i interface{}
	err := json.Unmarshal(source, &i)
	if err != nil {
		return err
	}

	// Set this JSONB
	is, ok := i.([]interface{})
	if !ok {
		return fmt.Errorf("type assertion .([]interface{}) failed")
	}
	numVin := len(is)
	ba := make(VinTxPropertyARRAY, numVin)
	for ii := range is {
		VinTxPropertyMapIface, ok := is[ii].(map[string]interface{})
		if !ok {
			return fmt.Errorf("type assertion .(map[string]interface) failed")
		}
		b, _ := json.Marshal(VinTxPropertyMapIface)
		err := json.Unmarshal(b, &ba[ii])
		if err != nil {
			return err
		}
	}
	*p = ba

	return nil
}

// DeletionSummary provides the number of rows removed from the tables when a
// block is removed.
type DeletionSummary struct {
	Blocks, Vins, Vouts, Addresses, Transactions, Tickets, Votes, Misses int64
	Timings                                                              *DeletionSummary
}

// String makes a pretty summary of the totals.
func (s DeletionSummary) String() string {
	summary := fmt.Sprintf("%9d Blocks purged\n", s.Blocks)
	summary = summary + fmt.Sprintf("%9d Vins purged\n", s.Vins)
	summary = summary + fmt.Sprintf("%9d Vouts purged\n", s.Vouts)
	summary = summary + fmt.Sprintf("%9d Addresses purged\n", s.Addresses)
	summary = summary + fmt.Sprintf("%9d Transactions purged\n", s.Transactions)
	summary = summary + fmt.Sprintf("%9d Tickets purged\n", s.Tickets)
	summary = summary + fmt.Sprintf("%9d Votes purged\n", s.Votes)
	summary = summary + fmt.Sprintf("%9d Misses purged", s.Misses)
	return summary
}

// DeletionSummarySlice is used to define methods on DeletionSummary slices.
type DeletionSummarySlice []DeletionSummary

// Reduce returns a single DeletionSummary with the corresponding fields summed.
func (ds DeletionSummarySlice) Reduce() DeletionSummary {
	var s DeletionSummary
	for i := range ds {
		s.Blocks += ds[i].Blocks
		s.Vins += ds[i].Vins
		s.Vouts += ds[i].Vouts
		s.Addresses += ds[i].Addresses
		s.Transactions += ds[i].Transactions
		s.Tickets += ds[i].Tickets
		s.Votes += ds[i].Votes
		s.Misses += ds[i].Misses
	}
	return s
}

// VinTxPropertyARRAY is a slice of VinTxProperty sturcts that implements
// sql.Scanner and driver.Valuer.
type VinTxPropertyARRAY []VinTxProperty

// func VinTxPropertyToJSONB(vin *VinTxProperty) (JSONB, error) {
// 	var vinJSONB map[string]interface{}
// 	vinJSON, err := json.Marshal(vin)
// 	if err != nil {
// 		return vinJSONB, err
// 	}
// 	var vinInterface interface{}
// 	err = json.Unmarshal(vinJSON, &vinInterface)
// 	if err != nil {
// 		return vinJSONB, err
// 	}
// 	vinJSONB = vinInterface.(map[string]interface{})
// 	return vinJSONB, nil
// }

// UInt64Array represents a one-dimensional array of PostgreSQL integer types
type UInt64Array []uint64

// Scan implements the sql.Scanner interface.
func (a *UInt64Array) Scan(src interface{}) error {
	switch src := src.(type) {
	case []byte:
		return a.scanBytes(src)
	case string:
		return a.scanBytes([]byte(src))
	case nil:
		*a = nil
		return nil
	}

	return fmt.Errorf("pq: cannot convert %T to UInt64Array", src)
}

func (a *UInt64Array) scanBytes(src []byte) error {
	elems, err := internal.ScanLinearArray(src, []byte{','}, "UInt64Array")
	if err != nil {
		return err
	}
	if *a != nil && len(elems) == 0 {
		*a = (*a)[:0]
	} else {
		b := make(UInt64Array, len(elems))
		for i, v := range elems {
			if b[i], err = strconv.ParseUint(string(v), 10, 64); err != nil {
				return fmt.Errorf("pq: parsing array element index %d: %v", i, err)
			}
		}
		*a = b
	}
	return nil
}

// Value implements the driver.Valuer interface.
func (a UInt64Array) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}

	if n := len(a); n > 0 {
		// There will be at least two curly brackets, N bytes of values,
		// and N-1 bytes of delimiters.
		b := make([]byte, 1, 1+2*n)
		b[0] = '{'

		b = strconv.AppendUint(b, a[0], 10)
		for i := 1; i < n; i++ {
			b = append(b, ',')
			b = strconv.AppendUint(b, a[i], 10)
		}

		return string(append(b, '}')), nil
	}

	return "{}", nil
}

// Vout defines a transaction output
type Vout struct {
	// txDbID           int64
	TxHash           string           `json:"tx_hash"`
	TxIndex          uint32           `json:"tx_index"`
	TxTree           int8             `json:"tx_tree"`
	TxType           int16            `json:"tx_type"`
	Value            uint64           `json:"value"`
	Version          uint16           `json:"version"`
	ScriptPubKey     []byte           `json:"pkScriptHex"`
	ScriptPubKeyData ScriptPubKeyData `json:"pkScript"`
}

// UTXOData stores an address and value associated with a transaction output.
type UTXOData struct {
	Addresses []string
	Value     int64
}

// UTXO represents a transaction output, but it is intended to help track
// unspent outputs.
type UTXO struct {
	TxHash  string
	TxIndex uint32
	UTXOData
}

// AddressRow represents a row in the addresses table
type AddressRow struct {
	// id int64
	Address        string
	ValidMainChain bool
	// MatchingTxHash provides the relationship between spending tx inputs and
	// funding tx outputs.
	MatchingTxHash string
	IsFunding      bool
	TxBlockTime    TimeDef
	TxHash         string
	TxVinVoutIndex uint32
	Value          uint64
	VinVoutDbID    uint64
	MergedCount    uint64
	TxType         int16
	// In merged view, both Atoms members might be non-zero.
	// In that case, Value is abs(AtomsCredit - AtomsDebit) and
	// IsFunding should true if AtomsCredit > AtomsDebit
	AtomsCredit uint64
	AtomsDebit  uint64
}

// AddressMetrics defines address metrics needed to make decisions by which
// grouping buttons on the address history page charts should be disabled or
// enabled by default.
type AddressMetrics struct {
	OldestBlockTime TimeDef
	YearTxsCount    int64 // number of year intervals with transactions
	MonthTxsCount   int64 // number of year month with transactions
	WeekTxsCount    int64 // number of year week with transactions
	DayTxsCount     int64 // number of year day with transactions
}

// ChartsData defines the fields that store the values needed to plot the charts
// on the frontend.
type ChartsData struct {
	Difficulty  []float64 `json:"difficulty,omitempty"`
	Time        []TimeDef `json:"time,omitempty"`
	Value       []uint64  `json:"value,omitempty"`
	Size        []uint64  `json:"size,omitempty"`
	ChainSize   []uint64  `json:"chainsize,omitempty"`
	Count       []uint64  `json:"count,omitempty"`
	SizeF       []float64 `json:"sizef,omitempty"`
	ValueF      []float64 `json:"valuef,omitempty"`
	Unspent     []uint64  `json:"unspent,omitempty"`
	Revoked     []uint64  `json:"revoked,omitempty"`
	Height      []uint64  `json:"height,omitempty"`
	Pooled      []uint64  `json:"pooled,omitempty"`
	Solo        []uint64  `json:"solo,omitempty"`
	SentRtx     []uint64  `json:"sentRtx,omitempty"`
	ReceivedRtx []uint64  `json:"receivedRtx,omitempty"`
	Tickets     []uint64  `json:"tickets,omitempty"`
	Votes       []uint64  `json:"votes,omitempty"`
	RevokeTx    []uint64  `json:"revokeTx,omitempty"`
	Amount      []float64 `json:"amount,omitempty"`
	Received    []float64 `json:"received,omitempty"`
	Sent        []float64 `json:"sent,omitempty"`
	Net         []float64 `json:"net,omitempty"`
	ChainWork   []uint64  `json:"chainwork,omitempty"`
	NetHash     []uint64  `json:"nethash,omitempty"`
}

// ScriptPubKeyData is part of the result of decodescript(ScriptPubKeyHex)
type ScriptPubKeyData struct {
	ReqSigs   uint32   `json:"reqSigs"`
	Type      string   `json:"type"`
	Addresses []string `json:"addresses"`
}

// VinTxProperty models a transaction input with previous outpoint information.
type VinTxProperty struct {
	PrevOut     string  `json:"prevout"`
	PrevTxHash  string  `json:"prevtxhash"`
	PrevTxIndex uint32  `json:"prevvoutidx"`
	PrevTxTree  uint16  `json:"tree"`
	Sequence    uint32  `json:"sequence"`
	ValueIn     int64   `json:"amountin"`
	TxID        string  `json:"tx_hash"`
	TxIndex     uint32  `json:"tx_index"`
	TxTree      uint16  `json:"tx_tree"`
	TxType      int16   `json:"tx_type"`
	BlockHeight uint32  `json:"blockheight"`
	BlockIndex  uint32  `json:"blockindex"`
	ScriptHex   []byte  `json:"scripthex"`
	IsValid     bool    `json:"is_valid"`
	IsMainchain bool    `json:"is_mainchain"`
	Time        TimeDef `json:"time"`
}

// PoolTicketsData defines the real time data
// needed for ticket pool visualization charts.
type PoolTicketsData struct {
	Time     []TimeDef `json:"time,omitempty"`
	Price    []float64 `json:"price,omitempty"`
	Mempool  []uint64  `json:"mempool,omitempty"`
	Immature []uint64  `json:"immature,omitempty"`
	Live     []uint64  `json:"live,omitempty"`
	Solo     uint64    `json:"solo,omitempty"`
	Pooled   uint64    `json:"pooled,omitempty"`
	TxSplit  uint64    `json:"txsplit,omitempty"`
}

// Vin models a transaction input.
type Vin struct {
	//txDbID      int64
	Coinbase    string  `json:"coinbase"`
	TxHash      string  `json:"txhash"`
	VoutIdx     uint32  `json:"voutidx"`
	Tree        int8    `json:"tree"`
	Sequence    uint32  `json:"sequence"`
	AmountIn    float64 `json:"amountin"`
	BlockHeight uint32  `json:"blockheight"`
	BlockIndex  uint32  `json:"blockindex"`
	ScriptHex   string  `json:"scripthex"`
}

// ScriptSig models the signature script used to redeem the origin transaction
// as a JSON object (non-coinbase txns only)
type ScriptSig struct {
	Asm string `json:"asm"`
	Hex string `json:"hex"`
}

// AgendaVoteChoices contains the vote counts on multiple intervals of time. The
// interval length may be either a single block, in which case Height contains
// the block heights, or a day, in which case Time contains the time stamps of
// each interval. Total is always the sum of Yes, No, and Abstain.
type AgendaVoteChoices struct {
	Abstain []uint64  `json:"abstain"`
	Yes     []uint64  `json:"yes"`
	No      []uint64  `json:"no"`
	Total   []uint64  `json:"total"`
	Height  []uint64  `json:"height,omitempty"`
	Time    []TimeDef `json:"time,omitempty"`
}

// Tx models a Decred transaction. It is stored in a Block.
type Tx struct {
	//blockDbID  int64
	BlockHash   string  `json:"block_hash"`
	BlockHeight int64   `json:"block_height"`
	BlockTime   TimeDef `json:"block_time"`
	Time        TimeDef `json:"time"`
	TxType      int16   `json:"tx_type"`
	Version     uint16  `json:"version"`
	Tree        int8    `json:"tree"`
	TxID        string  `json:"txid"`
	BlockIndex  uint32  `json:"block_index"`
	Locktime    uint32  `json:"locktime"`
	Expiry      uint32  `json:"expiry"`
	Size        uint32  `json:"size"`
	Spent       int64   `json:"spent"`
	Sent        int64   `json:"sent"`
	Fees        int64   `json:"fees"`
	NumVin      uint32  `json:"numvin"`
	//Vins        VinTxPropertyARRAY `json:"vins"`
	VinDbIds  []uint64 `json:"vindbids"`
	NumVout   uint32   `json:"numvout"`
	Vouts     []*Vout  `json:"vouts"`
	VoutDbIds []uint64 `json:"voutdbids"`
	// NOTE: VoutDbIds may not be needed if there is a vout table since each
	// vout will have a tx_dbid
	IsValidBlock     bool `json:"valid_block"`
	IsMainchainBlock bool `json:"mainchain"`
}

// Block models a Decred block.
type Block struct {
	Hash         string `json:"hash"`
	Size         uint32 `json:"size"`
	Height       uint32 `json:"height"`
	Version      uint32 `json:"version"`
	NumTx        uint32
	NumRegTx     uint32
	Tx           []string `json:"tx"`
	TxDbIDs      []uint64
	NumStakeTx   uint32
	STx          []string `json:"stx"`
	STxDbIDs     []uint64
	Time         TimeDef `json:"time"`
	Nonce        uint64  `json:"nonce"`
	VoteBits     uint16  `json:"votebits"`
	Voters       uint16  `json:"voters"`
	FreshStake   uint8   `json:"freshstake"`
	Revocations  uint8   `json:"revocations"`
	PoolSize     uint32  `json:"poolsize"`
	Bits         uint32  `json:"bits"`
	SBits        uint64  `json:"sbits"`
	Difficulty   float64 `json:"difficulty"`
	StakeVersion uint32  `json:"stakeversion"`
	PreviousHash string  `json:"previousblockhash"`
	ChainWork    string  `json:"chainwork"`
}

type BlockDataBasic struct {
	Height     uint32  `json:"height,omitemtpy"`
	Size       uint32  `json:"size,omitemtpy"`
	Hash       string  `json:"hash,omitemtpy"`
	Difficulty float64 `json:"diff,omitemtpy"`
	StakeDiff  float64 `json:"sdiff,omitemtpy"`
	Time       TimeDef `json:"time,omitemtpy"`
	NumTx      uint32  `json:"txlength,omitempty"`
}

// BlockStatus describes a block's status in the block chain.
type BlockStatus struct {
	IsValid     bool   `json:"is_valid"`
	IsMainchain bool   `json:"is_mainchain"`
	Height      uint32 `json:"height"`
	PrevHash    string `json:"previous_hash"`
	Hash        string `json:"hash"`
	NextHash    string `json:"next_hash"`
}

// SideChain represents blocks of a side chain, in ascending height order.
type SideChain struct {
	Hashes  []string
	Heights []int64
}

// AddressTx models data for transactions on the address page.
type AddressTx struct {
	TxID           string
	TxType         string
	InOutID        uint32
	Size           uint32
	FormattedSize  string
	Total          float64
	Confirmations  uint64
	Time           TimeDef
	ReceivedTotal  float64
	SentTotal      float64
	IsFunding      bool
	MatchedTx      string
	MatchedTxIndex uint32
	MergedTxnCount uint64 `json:",omitempty"`
	BlockHeight    uint32
}

// IOID formats an identification string for the transaction input (or output)
// represented by the AddressTx.
func (a *AddressTx) IOID(txType ...string) string {
	// If transaction is of type merged_debit, return unformatted transaction ID
	if len(txType) > 0 && AddrTxnTypeFromStr(txType[0]) == AddrMergedTxnDebit {
		return a.TxID
	}
	// When AddressTx is used properly, at least one of ReceivedTotal or
	// SentTotal should be zero.
	if a.IsFunding {
		// An outpoint receiving funds
		return fmt.Sprintf("%s:out[%d]", a.TxID, a.InOutID)
	}
	// A transaction input referencing an outpoint being spent
	return fmt.Sprintf("%s:in[%d]", a.TxID, a.InOutID)
}

// AddressTransactions collects the transactions for an address as AddressTx
// slices.
type AddressTransactions struct {
	Transactions []*AddressTx
	TxnsFunding  []*AddressTx
	TxnsSpending []*AddressTx
}

// AddressInfo models data for display on the address page.
type AddressInfo struct {
	// Address is the decred address on the current page
	Address string
	Net     string

	// IsDummyAddress is true when the address is the dummy address typically
	// used for unspendable ticket change outputs. See
	// https://github.com/decred/dcrdata/v4/issues/358 for details.
	IsDummyAddress bool

	// Page parameters
	MaxTxLimit    int64
	Fullmode      bool
	Path          string
	Limit, Offset int64  // ?n=Limit&start=Offset
	TxnType       string // ?txntype=TxnType
	TxnCount      int64
	IsMerged      bool

	// NumUnconfirmed is the number of unconfirmed txns for the address
	NumUnconfirmed  int64
	UnconfirmedTxns *AddressTransactions

	// Transactions on the current page
	Transactions    []*AddressTx
	TxnsFunding     []*AddressTx
	TxnsSpending    []*AddressTx
	NumTransactions int64 // The number of transactions in the address
	NumFundingTxns  int64 // number paying to the address
	NumSpendingTxns int64 // number spending outpoints associated with the address
	AmountReceived  dcrutil.Amount
	AmountSent      dcrutil.Amount
	AmountUnspent   dcrutil.Amount

	// Balance is used in full mode, describing all known transactions
	Balance *AddressBalance

	// KnownTransactions refers to the total transaction count in the DB when in
	// full mode, the sum of funding (crediting) and spending (debiting) txns.
	KnownTransactions int64
	KnownFundingTxns  int64
	KnownSpendingTxns int64
}

// AddressBalance represents the number and value of spent and unspent outputs
// for an address.
type AddressBalance struct {
	Address      string `json:"address"`
	NumSpent     int64  `json:"num_stxos"`
	NumUnspent   int64  `json:"num_utxos"`
	TotalSpent   int64  `json:"amount_spent"`
	TotalUnspent int64  `json:"amount_unspent"`
}

// ReduceAddressHistory generates a template AddressInfo from a slice of
// AddressRow. All fields except NumUnconfirmed and Transactions are set
// completely. Transactions is partially set, with each transaction having only
// the TxID and ReceivedTotal set. The rest of the data should be filled in by
// other means, such as RPC calls or database queries.
func ReduceAddressHistory(addrHist []*AddressRow) *AddressInfo {
	if len(addrHist) == 0 {
		return nil
	}

	var received, sent int64
	var transactions, creditTxns, debitTxns []*AddressTx
	for _, addrOut := range addrHist {
		if !addrOut.ValidMainChain {
			continue
		}
		coin := dcrutil.Amount(addrOut.Value).ToCoin()
		tx := AddressTx{
			Time:      addrOut.TxBlockTime,
			InOutID:   addrOut.TxVinVoutIndex,
			TxID:      addrOut.TxHash,
			TxType:    txhelpers.TxTypeToString(int(addrOut.TxType)),
			MatchedTx: addrOut.MatchingTxHash,
			IsFunding: addrOut.IsFunding,
		}

		tx.MergedTxnCount = addrOut.MergedCount

		if addrOut.IsFunding {
			// Funding transaction
			received += int64(addrOut.Value)
			tx.ReceivedTotal = coin
			creditTxns = append(creditTxns, &tx)
		} else {
			// Spending transaction
			sent += int64(addrOut.Value)
			tx.SentTotal = coin
			debitTxns = append(debitTxns, &tx)
		}

		transactions = append(transactions, &tx)
	}

	return &AddressInfo{
		Address:         addrHist[0].Address,
		Transactions:    transactions,
		TxnsFunding:     creditTxns,
		TxnsSpending:    debitTxns,
		NumFundingTxns:  int64(len(creditTxns)),
		NumSpendingTxns: int64(len(debitTxns)),
		AmountReceived:  dcrutil.Amount(received),
		AmountSent:      dcrutil.Amount(sent),
		AmountUnspent:   dcrutil.Amount(received - sent),
	}
}

// Post-process performs time/vin/vout sorting and block height calculations.
func (a *AddressInfo) PostProcess(tipHeight uint32) {
	// Sort the transactions by date and vin/vout index
	sort.Slice(a.Transactions, func(i, j int) bool {
		if a.Transactions[i].Time == a.Transactions[j].Time {
			return a.Transactions[i].InOutID > a.Transactions[j].InOutID
		}
		return a.Transactions[i].Time.UNIX() > a.Transactions[j].Time.UNIX()
	})

	// Compute block height for each transaction.
	for i := range a.Transactions {
		tx := a.Transactions[i]
		tx.BlockHeight = tipHeight - uint32(tx.Confirmations) + 1
	}
}
