// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package types

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/blockchain/stake/v5"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdata/v8/db/dbtypes"
	"github.com/decred/dcrdata/v8/txhelpers"
)

// Types of votes
const (
	VoteReject  = -1
	VoteAffirm  = 1
	VoteMissing = 0
)

// TimeDef is time.Time wrapper that formats time by default as a string without
// a timezone. The time Stringer interface formats the time into a string with a
// timezone.
type TimeDef struct {
	T time.Time
}

const (
	timeDefFmtHuman        = "2006-01-02 15:04:05 (MST)"
	timeDefFmtDateTimeNoTZ = "2006-01-02 15:04:05"
	timeDefFmtJS           = time.RFC3339
)

// String formats the time in a human-friendly layout. This ends up on the
// explorer web pages.
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

// MarshalJSON implements json.Marshaler.
func (t *TimeDef) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.RFC3339())
}

// UnmarshalJSON implements json.Unmarshaler.
func (t *TimeDef) UnmarshalJSON(data []byte) error {
	if t == nil {
		return fmt.Errorf("TimeDef: UnmarshalJSON on nil pointer")
	}
	tStr := string(data)
	tStr = strings.Trim(tStr, `"`)
	T, err := time.Parse(timeDefFmtJS, tStr)
	if err != nil {
		return err
	}
	t.T = T
	return nil
}

// PrettyMDY formats the time down to day only, using 3 day month, unpadded day,
// comma, and 4 digit year.
func (t *TimeDef) PrettyMDY() string {
	return t.T.Format("Jan 2, 2006")
}

// HMSTZ is the hour:minute:second with 3-digit timezone code.
func (t *TimeDef) HMSTZ() string {
	return t.T.Format("15:04:05 MST")
}

// DatetimeWithoutTZ formats the time in a human-friendly layout, without
// time zone.
func (t *TimeDef) DatetimeWithoutTZ() string {
	return t.T.Format(timeDefFmtDateTimeNoTZ)
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

// BlockBasic models data for the explorer's explorer page
type BlockBasic struct {
	Height         int64   `json:"height"`
	Hash           string  `json:"hash"`
	Version        int32   `json:"version"`
	Size           int32   `json:"size"`
	Valid          bool    `json:"valid"`
	MainChain      bool    `json:"mainchain"`
	Voters         uint16  `json:"votes"`
	Transactions   int     `json:"tx"`
	IndexVal       int64   `json:"windowIndex"`
	FreshStake     uint8   `json:"tickets"`
	Revocations    uint32  `json:"revocations"`
	TxCount        uint32  `json:"tx_count"`
	BlockTime      TimeDef `json:"time"`
	FormattedBytes string  `json:"formatted_bytes"`
	Total          float64 `json:"total"`
}

// WebBasicBlock is used for quick DB data without rpc calls
type WebBasicBlock struct {
	Height      uint32   `json:"height"`
	Size        uint32   `json:"size"`
	Hash        string   `json:"hash"`
	Difficulty  float64  `json:"diff"`
	StakeDiff   float64  `json:"sdiff"`
	Time        int64    `json:"time"`
	NumTx       uint32   `json:"txlength"`
	PoolSize    uint32   `json:"poolsize"`
	PoolValue   float64  `json:"poolvalue"`
	PoolValAvg  float64  `json:"poolvalavg"`
	PoolWinners []string `json:"winners"`
}

// TxBasic models data for transactions on the block page
type TxBasic struct {
	TxID          string
	Type          string
	Version       int32
	FormattedSize string
	Total         float64
	Fee           dcrutil.Amount
	FeeRate       dcrutil.Amount
	VoteInfo      *VoteInfo
	Coinbase      bool
	Treasurybase  bool
	MixCount      uint32
	MixDenom      int64
}

// TrimmedTxInfo for use with /visualblocks
type TrimmedTxInfo struct {
	*TxBasic
	Fees      float64
	VinCount  int
	VoutCount int
	VoteValid bool
}

// TxInfo models data needed for display on the tx page
type TxInfo struct {
	*TxBasic
	SpendingTxns     []TxInID
	Vin              []Vin
	Vout             []Vout
	BlockHeight      int64
	BlockIndex       uint32
	BlockHash        string
	BlockMiningFee   int64
	Confirmations    int64
	Time             TimeDef
	Mature           string
	VoteFundsLocked  string
	Maturity         int64   // Total number of blocks before mature
	MaturityTimeTill float64 // Time in hours until mature
	TSpendMeta       *dbtypes.TreasurySpendMetaData
	TicketInfo
}

// These are the text representations of the various special transaction types.
// These strings should match the strings returned by txhelpers.TxTypeToString.
const (
	TicketTypeStr   = "Ticket"
	VoteTypeStr     = "Vote"
	RevTypeStr      = "Revocation"
	CoinbaseTypeStr = "Coinbase"
	// What actually happens is treasuryadd burns coins and credits the treasury
	// account. treasuryspend creates coins and debits the treasury account.
	// treasurybase is analogous to a coinbase in that it credits the treasury
	// without spending/burning any coins (aka creates them out of thin air,
	// just like coinbases do).
	TreasurybaseTypeStr  = "Treasurybase"
	TreasuryAddTypeStr   = "Treasury Add"
	TreasurySpendTypeStr = "Treasury Spend"
)

// IsTicket checks whether this transaction is a ticket.
func (t *TxInfo) IsTicket() bool {
	return t.Type == TicketTypeStr
}

// IsVote checks whether this transaction is a vote.
func (t *TxInfo) IsVote() bool {
	return t.Type == VoteTypeStr
}

// IsTreasurySpend checks whether this transaction is a tspend.
func (t *TxInfo) IsTreasurySpend() bool {
	return t.Type == TreasurySpendTypeStr
}

// IsTreasurybase checks whether this transaction is a treasurybase.
func (t *TxInfo) IsTreasurybase() bool {
	return t.Type == TreasurybaseTypeStr
}

// IsTreasuryAdd checks whether this transaction is a tadd.
func (t *TxInfo) IsTreasuryAdd() bool {
	return t.Type == TreasuryAddTypeStr
}

// IsRevocation checks whether this transaction is a revocation.
func (t *TxInfo) IsRevocation() bool {
	return t.Type == RevTypeStr
}

// IsLiveTicket verifies the conditions: 1. is a ticket, 2. is mature,
// 3. hasn't voted, 4. isn't  expired.
func (t *TxInfo) IsLiveTicket() bool {
	return t.Type == TicketTypeStr && t.Mature == "True" && t.SpendStatus != "Voted" &&
		t.PoolStatus == "live" && t.TicketLiveBlocks < t.TicketExpiry
}

// IsExpiredTicket verifies the conditions: 1. is a ticket, 2. is mature,
// 3. hasn't voted, 4. is past expiration.
func (t *TxInfo) IsExpiredTicket() bool {
	return t.Type == TicketTypeStr && t.Mature == "True" && t.SpendStatus != "Voted" &&
		t.PoolStatus == "live" && t.TicketLiveBlocks >= t.TicketExpiry
}

// IsImmatureTicket verifies the conditions: 1. is a ticket, 2. is not mature.
func (t *TxInfo) IsImmatureTicket() bool {
	return t.Type == TicketTypeStr && t.Mature == "False"
}

// IsImmatureVote verifies the conditions: 1. is a vote, 2. is not mature.
func (t *TxInfo) IsImmatureVote() bool {
	return t.Type == VoteTypeStr && t.Mature == "False"
}

// IsImmatureCoinbase verifies the conditions: 1. is coinbase, 2. is not mature.
func (t *TxInfo) IsImmatureCoinbase() bool {
	return t.Type == CoinbaseTypeStr && t.Mature == "False"
}

// IsImmatureRevocation verifies the conditions: 1. is a revocation, 2. is not
// mature.
func (t *TxInfo) IsImmatureRevocation() bool {
	return t.Type == RevTypeStr && t.Mature == "False"
}

// IsImmature indicates if the transaction is immature
func (t *TxInfo) IsImmature() bool {
	return t.Mature == "False"
}

// BlocksToTicketMaturity will return 0 if this isn't an immature ticket.
func (t *TxInfo) BlocksToTicketMaturity() (blocks int64) {
	if t.Type != TicketTypeStr {
		return
	}
	if t.Mature == "True" {
		return
	}
	return t.TicketInfo.TicketMaturity + 1 - t.Confirmations
}

// TicketInfo is used to represent data shown for a sstx transaction.
type TicketInfo struct {
	TicketMaturity       int64
	TimeTillMaturity     float64 // Time before a particular ticket reaches maturity, in hours
	PoolStatus           string
	SpendStatus          string
	LotteryBlock         string  // If the ticket was chosen to vote, it was chosen to vote in this block.
	TicketPoolSize       int64   // Total number of ticket in the pool
	TicketExpiry         int64   // Total number of blocks before a ticket expires
	TicketExpiryDaysLeft float64 // Approximate days left before the given ticket expires
	TicketLiveBlocks     int64   // Total number of confirms after maturity and up until the point the ticket votes or expires
	BestLuck             int64   // Best possible Luck for voting
	AvgLuck              int64   // Average Luck for voting
	VoteLuck             float64 // Actual Luck for voting on a ticket
	LuckStatus           string  // Short description based on the VoteLuck
	Probability          float64 // Probability of success before ticket expires
}

// TxInID models the identity of a spending transaction input
type TxInID struct {
	Hash  string
	Index uint32
}

// TSpendVote describes how a SSGen transaction decided on a tspend.
type TSpendVote struct {
	TSpend string `json:"tspend"`
	Choice string `json:"choice"`
}

// VoteInfo models data about a SSGen transaction (vote)
type VoteInfo struct {
	Validation         BlockValidation         `json:"block_validation"`
	Version            uint32                  `json:"vote_version"`
	Bits               uint16                  `json:"vote_bits"`
	Choices            []*txhelpers.VoteChoice `json:"vote_choices"`
	TicketSpent        string                  `json:"ticket_spent"`
	MempoolTicketIndex int                     `json:"mempool_ticket_index"`
	ForLastBlock       bool                    `json:"last_block"`
	TSpends            []*TSpendVote           `json:"tspend_votes,omitempty"`
}

func (vi *VoteInfo) DeepCopy() *VoteInfo {
	if vi == nil {
		return nil
	}
	out := *vi
	out.Choices = make([]*txhelpers.VoteChoice, len(vi.Choices))
	copy(out.Choices, vi.Choices)
	return &out
}

// ConvertTSpendVotes converts into the api's TSpendVote format.
func ConvertTSpendVotes(tspendChoices []*txhelpers.TSpendVote) []*TSpendVote {
	choiceStr := func(choice uint8) string {
		switch stake.TreasuryVoteT(choice) {
		case stake.TreasuryVoteYes:
			return "yes"
		case stake.TreasuryVoteNo:
			return "no"
		default:
			return "invalid"
		}
	}
	tspendVotes := make([]*TSpendVote, len(tspendChoices))
	for i := range tspendChoices {
		tspendVotes[i] = &TSpendVote{
			TSpend: tspendChoices[i].TSpend.String(),
			Choice: choiceStr(tspendChoices[i].Choice),
		}
	}
	return tspendVotes
}

// BlockValidation models data about a vote's decision on a block
type BlockValidation struct {
	Hash     string `json:"hash"`
	Height   int64  `json:"height"`
	Validity bool   `json:"validity"`
}

// SetTicketIndex assigns the VoteInfo an index based on the block that the vote
// is (in)validating and the spent ticket hash. The ticketSpendInds tracks
// known combinations of target block and spent ticket hash. This index is used
// for sorting in views and counting total unique votes for a block.
func (vi *VoteInfo) SetTicketIndex(ticketSpendInds BlockValidatorIndex) {
	// One-based indexing
	startInd := 1
	// Reference the sub-index for the block being (in)validated by this vote.
	if idxs, ok := ticketSpendInds[vi.Validation.Hash]; ok {
		// If this ticket has been seen before voting on this block, set the
		// known index. Otherwise, assign the next index in the series.
		if idx, ok := idxs[vi.TicketSpent]; ok {
			vi.MempoolTicketIndex = idx
		} else {
			idx := len(idxs) + startInd
			idxs[vi.TicketSpent] = idx
			vi.MempoolTicketIndex = idx
		}
	} else {
		// First vote encountered for this block. Create new ticket sub-index.
		ticketSpendInds[vi.Validation.Hash] = TicketIndex{
			vi.TicketSpent: startInd,
		}
		vi.MempoolTicketIndex = startInd
	}
}

// VotesOnBlock indicates if the vote is voting on the validity of block
// specified by the given hash.
func (vi *VoteInfo) VotesOnBlock(blockHash string) bool {
	return vi.Validation.ForBlock(blockHash)
}

// ForBlock indicates if the validation choice is for the specified block.
func (v *BlockValidation) ForBlock(blockHash string) bool {
	return blockHash != "" && blockHash == v.Hash
}

// Vin models basic data about a tx input for display
type Vin struct {
	*chainjson.Vin
	Addresses       []string
	FormattedAmount string
	Index           uint32
	DisplayText     string
	TextIsHash      bool
	Link            string
}

// Vout models basic data about a tx output for display
type Vout struct {
	Addresses       []string
	Amount          float64
	FormattedAmount string
	Type            string
	Spent           bool
	OP_RETURN       string
	OP_TADD         bool
	Index           uint32
	Version         uint16
}

// TrimmedBlockInfo models data needed to display block info on the new home page
type TrimmedBlockInfo struct {
	Time         TimeDef
	Height       int64
	Total        float64
	Fees         float64
	Subsidy      *chainjson.GetBlockSubsidyResult
	Votes        []*TrimmedTxInfo
	Tickets      []*TrimmedTxInfo
	Revocations  []*TrimmedTxInfo
	Transactions []*TrimmedTxInfo
}

// BlockInfo models data for display on the block page
type BlockInfo struct {
	*BlockBasic
	Confirmations         int64
	PoWHash               string
	StakeRoot             string
	MerkleRoot            string
	TxAvailable           bool
	Tx                    []*TrimmedTxInfo
	Treasury              []*TrimmedTxInfo
	Tickets               []*TrimmedTxInfo
	Revs                  []*TrimmedTxInfo
	Votes                 []*TrimmedTxInfo
	Misses                []string
	Nonce                 uint32
	VoteBits              uint16
	FinalState            string
	PoolSize              uint32
	Bits                  string
	SBits                 float64
	Difficulty            float64
	ExtraData             string
	StakeVersion          uint32
	PreviousHash          string
	NextHash              string
	TotalSent             float64
	MiningFee             float64
	TotalMixed            int64
	StakeValidationHeight int64
	Subsidy               *chainjson.GetBlockSubsidyResult
}

// Conversion is a representation of some amount of DCR in another index.
type Conversion struct {
	Value float64 `json:"value"`
	Index string  `json:"index"`
}

// HomeInfo represents data used for the home page
type HomeInfo struct {
	CoinSupply            int64                    `json:"coin_supply"`
	MixedPercent          float64                  `json:"mixed_percent"`
	StakeDiff             float64                  `json:"sdiff"`
	NextExpectedStakeDiff float64                  `json:"next_expected_sdiff"`
	NextExpectedBoundsMin float64                  `json:"next_expected_min"`
	NextExpectedBoundsMax float64                  `json:"next_expected_max"`
	IdxBlockInWindow      int                      `json:"window_idx"`
	IdxInRewardWindow     int                      `json:"reward_idx"`
	Difficulty            float64                  `json:"difficulty"`
	DevFund               int64                    `json:"dev_fund"` // legacy treasury address
	DevAddress            string                   `json:"dev_address"`
	TreasuryBalance       *dbtypes.TreasuryBalance `json:"treasury_bal"` // new decentralized treasury account
	TicketReward          float64                  `json:"reward"`
	RewardPeriod          string                   `json:"reward_period"`
	ASR                   float64                  `json:"ASR"`
	NBlockSubsidy         BlockSubsidy             `json:"subsidy"`
	Params                ChainParams              `json:"params"`
	PoolInfo              TicketPoolInfo           `json:"pool_info"`
	TotalLockedDCR        float64                  `json:"total_locked_dcr"`
	HashRate              float64                  `json:"hash_rate"`
	HashRateChangeDay     float64                  `json:"hash_rate_change_day"`
	HashRateChangeMonth   float64                  `json:"hash_rate_change_month"`
	ExchangeRate          *Conversion              `json:"exchange_rate,omitempty"`
}

// BlockSubsidy is an implementation of chainjson.GetBlockSubsidyResult
type BlockSubsidy struct {
	Total int64 `json:"total"`
	PoW   int64 `json:"pow"`
	PoS   int64 `json:"pos"`
	Dev   int64 `json:"dev"`
}

// TrimmedMempoolInfo is mempool data for the home page.
type TrimmedMempoolInfo struct {
	Transactions []*TrimmedTxInfo
	Tickets      []*TrimmedTxInfo
	Votes        []*TrimmedTxInfo
	Revocations  []*TrimmedTxInfo
	TSpends      []*TrimmedTxInfo
	TAdds        []*TrimmedTxInfo
	Subsidy      BlockSubsidy
	Total        float64
	Time         int64
	Fees         float64
}

// MempoolInfo models data to update mempool info on the home page.
type MempoolInfo struct {
	sync.RWMutex
	MempoolShort
	Transactions []MempoolTx `json:"tx"`
	Tickets      []MempoolTx `json:"tickets"`
	Votes        []MempoolTx `json:"votes"`
	Revocations  []MempoolTx `json:"revs"`
	TSpends      []MempoolTx `json:"tspends"`
	TAdds        []MempoolTx `json:"tadds"`
	Ident        uint64      `json:"id"`
}

// DeepCopy makes a deep copy of MempoolInfo, where all the slice and map data
// are copied over.
func (mpi *MempoolInfo) DeepCopy() *MempoolInfo {
	if mpi == nil {
		return nil
	}

	mpi.RLock()
	defer mpi.RUnlock()

	out := new(MempoolInfo)
	out.Transactions = CopyMempoolTxSlice(mpi.Transactions)
	out.Tickets = CopyMempoolTxSlice(mpi.Tickets)
	out.Votes = CopyMempoolTxSlice(mpi.Votes)
	out.Revocations = CopyMempoolTxSlice(mpi.Revocations)
	out.TSpends = CopyMempoolTxSlice(mpi.TSpends)
	out.TAdds = CopyMempoolTxSlice(mpi.TAdds)

	mps := mpi.MempoolShort.DeepCopy()
	out.MempoolShort = *mps

	return out
}

// Trim converts the MempoolInfo to TrimmedMempoolInfo.
func (mpi *MempoolInfo) Trim() *TrimmedMempoolInfo {
	mpi.RLock()

	mempoolRegularTxs := TrimMempoolTxs(mpi.Transactions)
	mempoolVotes := TrimMempoolTxs(mpi.Votes)

	data := &TrimmedMempoolInfo{
		Transactions: FilterRegularTx(mempoolRegularTxs),
		Tickets:      TrimMempoolTxs(mpi.Tickets),
		Votes:        FilterUniqueLastBlockVotes(mempoolVotes),
		Revocations:  TrimMempoolTxs(mpi.Revocations),
		TSpends:      TrimMempoolTxs(mpi.TSpends),
		TAdds:        TrimMempoolTxs(mpi.TAdds),
		Total:        mpi.TotalOut,
		Time:         mpi.LastBlockTime,
	}

	mpi.RUnlock()

	// Calculate total fees for all mempool transactions.
	getTotalFee := func(txs []*TrimmedTxInfo) dcrutil.Amount {
		var sum dcrutil.Amount
		for _, tx := range txs {
			sum += tx.TxBasic.Fee
		}
		return sum
	}

	allFees := getTotalFee(data.Transactions) + getTotalFee(data.Revocations) +
		getTotalFee(data.Tickets) + getTotalFee(data.Votes)
	data.Fees = allFees.ToCoin()

	return data
}

// getTxFromList is a helper function for searching the MempoolInfo tx lists.
func getTxFromList(txid string, txns []MempoolTx) (MempoolTx, bool) {
	for idx := range txns {
		if txns[idx].TxID == txid {
			return txns[idx], true
		}
	}
	return MempoolTx{}, false
}

// Tx checks the inventory and searches the appropriate lists for a
// transaction matching the provided transaction ID.
func (mpi *MempoolInfo) Tx(txid string) (MempoolTx, bool) {
	mpi.RLock()
	defer mpi.RUnlock()
	_, found := mpi.InvRegular[txid]
	if found {
		return getTxFromList(txid, mpi.Transactions)
	}
	_, found = mpi.InvStake[txid]
	if found {
		tx, found := getTxFromList(txid, mpi.Tickets)
		if found {
			return tx, true
		}
		tx, found = getTxFromList(txid, mpi.Votes)
		if found {
			return tx, true
		}
		tx, found = getTxFromList(txid, mpi.TAdds)
		if found {
			return tx, true
		}
		tx, found = getTxFromList(txid, mpi.TSpends)
		if found {
			return tx, true
		}
		return getTxFromList(txid, mpi.Revocations)
	}
	return MempoolTx{}, false
}

// ID can be used to track state changes.
func (mpi *MempoolInfo) ID() uint64 {
	mpi.RLock()
	defer mpi.RUnlock()
	return mpi.Ident
}

// FilterRegularTx returns a slice of all the regular (non-stake) transactions
// in the input slice, excluding coinbase (reward) transactions.
func FilterRegularTx(txs []*TrimmedTxInfo) (transactions []*TrimmedTxInfo) {
	for _, tx := range txs {
		if !tx.Coinbase {
			transactions = append(transactions, tx)
		}
	}
	return transactions
}

func BytesString(s uint64) string {
	if s < 1000 {
		return fmt.Sprintf("%d B", s)
	}
	e := math.Min(3, math.Floor(math.Log(float64(s))/math.Log(1000)))
	suffix := []string{"B", "kB", "MB", "GB"}[int(e)]
	val := math.Round(float64(s)/math.Pow(1000, e)*10) / 10
	f := "%.0f %s"
	if val < 10 {
		f = "%.1f %s"
	}

	return fmt.Sprintf(f, val, suffix)
}

// TrimMempoolTxs converts the input []MempoolTx to a []*TrimmedTxInfo.
func TrimMempoolTxs(txs []MempoolTx) []*TrimmedTxInfo {
	trimmedTxs := make([]*TrimmedTxInfo, 0, len(txs))
	for _, tx := range txs {
		trimmedTxs = append(trimmedTxs, TrimMempoolTx(&tx))
	}
	return trimmedTxs
}

// TrimMempoolTx converts the input []MempoolTx to a []*TrimmedTxInfo.
func TrimMempoolTx(tx *MempoolTx) (trimmedTx *TrimmedTxInfo) {
	fee, _ := dcrutil.NewAmount(tx.Fees) // non-nil error returns 0 fee
	var feeRate dcrutil.Amount
	if tx.Size > 0 {
		feeRate = fee / dcrutil.Amount(int64(tx.Size))
	}
	txBasic := &TxBasic{
		TxID:          tx.TxID,
		Type:          tx.Type,
		Version:       tx.Version,
		FormattedSize: BytesString(uint64(tx.Size)),
		Total:         tx.TotalOut,
		Fee:           fee,
		FeeRate:       feeRate,
		VoteInfo:      tx.VoteInfo,
		// TreasuryBase and Coinbase are not in mempool
	}

	var voteValid bool
	if tx.VoteInfo != nil {
		voteValid = tx.VoteInfo.Validation.Validity
	}

	return &TrimmedTxInfo{
		TxBasic:   txBasic,
		Fees:      tx.Fees,
		VoteValid: voteValid,
		VinCount:  tx.VinCount,
		VoutCount: tx.VoutCount,
	}
}

// FilterUniqueLastBlockVotes returns a slice of all the vote transactions from
// the input slice that are flagged as voting on the previous block.
func FilterUniqueLastBlockVotes(txs []*TrimmedTxInfo) (votes []*TrimmedTxInfo) {
	seenVotes := make(map[string]struct{})
	for _, tx := range txs {
		if tx.VoteInfo != nil && tx.VoteInfo.ForLastBlock {
			// Do not append duplicates.
			if _, seen := seenVotes[tx.TxID]; seen {
				continue
			}
			votes = append(votes, tx)
			seenVotes[tx.TxID] = struct{}{}
		}
	}
	return votes
}

// TicketIndex is used to assign an index to a ticket hash.
type TicketIndex map[string]int

// BlockValidatorIndex keeps a list of arbitrary indexes for unique combinations
// of block hash and the ticket being spent to validate the block, i.e.
// map[validatedBlockHash]map[ticketHash]index.
type BlockValidatorIndex map[string]TicketIndex

// MempoolShort represents the mempool data sent as the mempool update
type MempoolShort struct {
	LastBlockHeight    int64               `json:"block_height"`
	LastBlockHash      string              `json:"block_hash"`
	LastBlockTime      int64               `json:"block_time"`
	FormattedBlockTime string              `json:"formatted_block_time"`
	Time               int64               `json:"time"`
	TotalOut           float64             `json:"total"`
	TotalSize          int32               `json:"size"`
	NumTickets         int                 `json:"num_tickets"`
	NumVotes           int                 `json:"num_votes"`
	NumRegular         int                 `json:"num_regular"`
	NumRevokes         int                 `json:"num_revokes"`
	NumTSpends         int                 `json:"num_tspends"`
	NumTAdds           int                 `json:"num_tadds"`
	NumAll             int                 `json:"num_all"`
	LikelyMineable     LikelyMineable      `json:"likely_mineable"`
	LatestTransactions []MempoolTx         `json:"latest"`
	FormattedTotalSize string              `json:"formatted_size"`
	TicketIndexes      BlockValidatorIndex `json:"-"`
	VotingInfo         VotingInfo          `json:"voting_info"`
	InvRegular         map[string]struct{} `json:"-"`
	InvStake           map[string]struct{} `json:"-"`
}

// LikelyMineable holds the totals for all mempool transactions except for votes
// on non-tip blocks and multiple votes that spend the same ticket.
type LikelyMineable struct {
	Total         float64 `json:"total"`
	Size          int32   `json:"size"`
	FormattedSize string  `json:"formatted_size"`
	RegularTotal  float64 `json:"regular_total"`
	TicketTotal   float64 `json:"ticket_total"`
	VoteTotal     float64 `json:"vote_total"`
	RevokeTotal   float64 `json:"revoke_total"`
	TSpendTotal   float64 `json:"tspend_total"`
	TAddTotal     float64 `json:"tadd_total"`
	Count         int     `json:"count"`
}

func (mps *MempoolShort) DeepCopy() *MempoolShort {
	if mps == nil {
		return nil
	}

	out := &MempoolShort{
		LastBlockHash:      mps.LastBlockHash,
		LastBlockHeight:    mps.LastBlockHeight,
		LastBlockTime:      mps.LastBlockTime,
		FormattedBlockTime: mps.FormattedBlockTime,
		Time:               mps.Time,
		TotalOut:           mps.TotalOut,
		TotalSize:          mps.TotalSize,
		NumTickets:         mps.NumTickets,
		NumVotes:           mps.NumVotes,
		NumRegular:         mps.NumRegular,
		NumRevokes:         mps.NumRevokes,
		NumTSpends:         mps.NumTSpends,
		NumTAdds:           mps.NumTAdds,
		NumAll:             mps.NumAll,
		LikelyMineable:     mps.LikelyMineable,
		FormattedTotalSize: mps.FormattedTotalSize,
		VotingInfo: VotingInfo{
			TicketsVoted:     mps.VotingInfo.TicketsVoted,
			MaxVotesPerBlock: mps.VotingInfo.MaxVotesPerBlock,
		},
	}

	out.LatestTransactions = CopyMempoolTxSlice(mps.LatestTransactions)

	out.TicketIndexes = make(BlockValidatorIndex, len(mps.TicketIndexes))
	for bs, ti := range mps.TicketIndexes {
		m := make(TicketIndex, len(ti))
		out.TicketIndexes[bs] = m
		for bt, i := range ti {
			m[bt] = i
		}
	}

	out.VotingInfo.VotedTickets = make(map[string]bool, len(mps.VotingInfo.VotedTickets))
	for s, b := range mps.VotingInfo.VotedTickets {
		out.VotingInfo.VotedTickets[s] = b
	}

	out.VotingInfo.VoteTallys = make(map[string]*VoteTally, len(mps.VotingInfo.VoteTallys))
	for hash, tally := range mps.VotingInfo.VoteTallys {
		out.VotingInfo.VoteTallys[hash] = &VoteTally{
			TicketsPerBlock: tally.TicketsPerBlock,
			Marks:           tally.Marks,
		}
	}

	out.InvRegular = make(map[string]struct{}, len(mps.InvRegular))
	for s := range mps.InvRegular {
		out.InvRegular[s] = struct{}{}
	}

	out.InvStake = make(map[string]struct{}, len(mps.InvStake))
	for s := range mps.InvStake {
		out.InvStake[s] = struct{}{}
	}

	return out
}

// VotingInfo models data about the validity of the next block from mempool.
type VotingInfo struct {
	TicketsVoted     uint16          `json:"tickets_voted"`
	MaxVotesPerBlock uint16          `json:"max_votes_per_block"`
	VotedTickets     map[string]bool `json:"-"`
	// VoteTallys maps block hash to vote counts.
	VoteTallys map[string]*VoteTally `json:"vote_tally"`
}

// NewVotingInfo initializes a VotingInfo.
func NewVotingInfo(votesPerBlock uint16) VotingInfo {
	return VotingInfo{
		MaxVotesPerBlock: votesPerBlock,
		VotedTickets:     make(map[string]bool),
		VoteTallys:       make(map[string]*VoteTally),
	}
}

// Tally adds the VoteInfo to the VotingInfo.VoteTally
func (vi *VotingInfo) Tally(vinfo *VoteInfo) {
	_, ok := vi.VoteTallys[vinfo.Validation.Hash]
	if ok {
		vi.VoteTallys[vinfo.Validation.Hash].Mark(vinfo.Validation.Validity)
		return
	}
	marks := make([]bool, 1, vi.MaxVotesPerBlock)
	marks[0] = vinfo.Validation.Validity
	vi.VoteTallys[vinfo.Validation.Hash] = &VoteTally{
		TicketsPerBlock: int(vi.MaxVotesPerBlock),
		Marks:           marks,
	}
}

// BlockStatus fetches a list of votes in mempool, for the provided block hash.
// If not found, a list of VoteMissing is returned.
func (vi *VotingInfo) BlockStatus(hash string) ([]int, int) {
	tally, ok := vi.VoteTallys[hash]
	if ok {
		return tally.Status()
	}
	marks := make([]int, int(vi.MaxVotesPerBlock))
	for i := range marks {
		marks[i] = VoteMissing
	}
	return marks, VoteMissing
}

// VoteTally manages a list of bools representing the votes for a block.
type VoteTally struct {
	TicketsPerBlock int    `json:"-"`
	Marks           []bool `json:"marks"`
}

// Mark adds the vote to the VoteTally.
func (tally *VoteTally) Mark(vote bool) {
	tally.Marks = append(tally.Marks, vote)
}

// Status is a list of ints representing votes both received and not yet
// received for a block, and a single int representing consensus.
// 0: rejected, 1: affirmed, 2: vote not yet received
func (tally *VoteTally) Status() ([]int, int) {
	votes := []int{}
	var up, down, consensus int
	for _, affirmed := range tally.Marks {
		if affirmed {
			up++
			votes = append(votes, VoteAffirm)
		} else {
			down++
			votes = append(votes, VoteReject)
		}
	}
	for i := len(votes); i < tally.TicketsPerBlock; i++ {
		votes = append(votes, VoteMissing)
	}
	threshold := tally.TicketsPerBlock / 2
	if up > threshold {
		consensus = VoteAffirm
	} else if down > threshold {
		consensus = VoteReject
	}
	return votes, consensus
}

// Affirmations counts the number of selected ticket holders who have voted
// in favor of the block for the given hash.
func (tally *VoteTally) Affirmations() (c int) {
	for _, affirmed := range tally.Marks {
		if affirmed {
			c++
		}
	}
	return c
}

// VoteCount is the number of votes received.
func (tally *VoteTally) VoteCount() int {
	return len(tally.Marks)
}

// ChainParams models simple data about the chain server's parameters used for
// some info on the front page.
type ChainParams struct {
	WindowSize       int64 `json:"window_size"`
	RewardWindowSize int64 `json:"reward_window_size"`
	TargetPoolSize   int64 `json:"target_pool_size"`
	BlockTime        int64 `json:"target_block_time"`
	MeanVotingBlocks int64
}

// WebsocketBlock wraps the new block info for use in the websocket
type WebsocketBlock struct {
	Block *BlockInfo `json:"block"`
	Extra *HomeInfo  `json:"extra"`
}

// BlockID provides basic identifying information about a block.
type BlockID struct {
	Hash   string
	Height int64
	Time   int64
}

// TicketPoolInfo describes the live ticket pool
type TicketPoolInfo struct {
	Size          uint32  `json:"size"`
	Value         float64 `json:"value"`
	ValAvg        float64 `json:"valavg"`
	Percentage    float64 `json:"percent"`
	Target        uint32  `json:"target"`
	PercentTarget float64 `json:"percent_target"`
}

// MempoolTx models the tx basic data for the mempool page
type MempoolTx struct {
	TxID    string  `json:"txid"`
	Version int32   `json:"version"`
	Fees    float64 `json:"fees"`
	FeeRate float64 `json:"fee_rate"`
	// Consider atom representation:
	//FeeAmount   int64        `json:"fee_amount"`
	VinCount  int            `json:"vin_count"`
	VoutCount int            `json:"vout_count"`
	Vin       []MempoolInput `json:"vin,omitempty"`
	Coinbase  bool           `json:"coinbase"` // to signal the coinbase tx on new block despite not being in mempool
	Hash      string         `json:"hash"`     // dup of TxID?
	Time      int64          `json:"time"`
	Size      int32          `json:"size"`
	TotalOut  float64        `json:"total"`
	// Consider atom representation:
	//TotalOutAmt int64        `json:"total_amount"`
	Type     string    `json:"Type"`
	TypeID   int       `json:"typeID"` // stake package types
	VoteInfo *VoteInfo `json:"vote_info,omitempty"`
}

func (mpt *MempoolTx) DeepCopy() *MempoolTx {
	if mpt == nil {
		return nil
	}
	out := *mpt
	out.Vin = make([]MempoolInput, len(mpt.Vin))
	copy(out.Vin, mpt.Vin)
	out.VoteInfo = mpt.VoteInfo.DeepCopy()
	return &out
}

func CopyMempoolTxSlice(s []MempoolTx) []MempoolTx {
	if s == nil { // []types.MempoolTx(nil) != []types.MempoolTx{}
		return nil
	}
	out := make([]MempoolTx, 0, len(s))
	for i := range s {
		out = append(out, *s[i].DeepCopy())
	}
	return out
}

// NewMempoolTx models data sent from the notification handler
type NewMempoolTx struct {
	Time int64
	Hex  string
}

// MempoolVin is minimal information about the inputs of a mempool transaction.
type MempoolVin struct {
	TxId   string
	Inputs []MempoolInput
}

// MempoolInput is basic information about a transaction input.
type MempoolInput struct {
	TxId   string `json:"txid"`
	Index  uint32 `json:"index"`
	Outdex uint32 `json:"vout"`
}

type MPTxsByTime []MempoolTx

func (txs MPTxsByTime) Less(i, j int) bool {
	return txs[i].Time > txs[j].Time
}

func (txs MPTxsByTime) Len() int {
	return len(txs)
}

func (txs MPTxsByTime) Swap(i, j int) {
	txs[i], txs[j] = txs[j], txs[i]
}

type MPTxsByHeight []MempoolTx

func (votes MPTxsByHeight) Less(i, j int) bool {
	if votes[i].VoteInfo.Validation.Height == votes[j].VoteInfo.Validation.Height {
		return votes[i].VoteInfo.MempoolTicketIndex <
			votes[j].VoteInfo.MempoolTicketIndex
	}
	return votes[i].VoteInfo.Validation.Height >
		votes[j].VoteInfo.Validation.Height
}

func (votes MPTxsByHeight) Len() int {
	return len(votes)
}

func (votes MPTxsByHeight) Swap(i, j int) {
	votes[i], votes[j] = votes[j], votes[i]
}

// AddrPrefix represent the address name it's prefix and description
type AddrPrefix struct {
	Name        string
	Prefix      string
	Description string
}

// AddressPrefixes generates an array AddrPrefix by using chaincfg.Params
func AddressPrefixes(params *chaincfg.Params) []AddrPrefix {
	Descriptions := []string{"P2PK address",
		"P2PKH address prefix. Standard wallet address. 1 public key -> 1 private key",
		"Ed25519 P2PKH address prefix",
		"secp256k1 Schnorr P2PKH address prefix",
		"P2SH address prefix",
		"WIF private key prefix",
		"HD extended private key prefix",
		"HD extended public key prefix",
	}
	Name := []string{"PubKeyAddrID",
		"PubKeyHashAddrID",
		"PKHEdwardsAddrID",
		"PKHSchnorrAddrID",
		"ScriptHashAddrID",
		"PrivateKeyID",
		"HDPrivateKeyID",
		"HDPublicKeyID",
	}

	MainnetPrefixes := []string{"Dk", "Ds", "De", "DS", "Dc", "Pm", "dprv", "dpub"}
	TestnetPrefixes := []string{"Tk", "Ts", "Te", "TS", "Tc", "Pt", "tprv", "tpub"}
	SimnetPrefixes := []string{"Sk", "Ss", "Se", "SS", "Sc", "Ps", "sprv", "spub"}

	name := params.Name
	var netPrefixes []string
	if name == "mainnet" {
		netPrefixes = MainnetPrefixes
	} else if strings.HasPrefix(name, "testnet") {
		netPrefixes = TestnetPrefixes
	} else if name == "simnet" {
		netPrefixes = SimnetPrefixes
	} else {
		return nil
	}

	addrPrefix := make([]AddrPrefix, 0, len(Descriptions))
	for i, desc := range Descriptions {
		addrPrefix = append(addrPrefix, AddrPrefix{
			Name:        Name[i],
			Description: desc,
			Prefix:      netPrefixes[i],
		})
	}
	return addrPrefix
}

// StatsInfo represents all of the data for the stats page.
type StatsInfo struct {
	UltimateSupply             int64
	TotalSupply                int64
	TotalSupplyPercentage      float64
	ProjectFunds               int64
	ProjectAddress             string
	PoWDiff                    float64
	HashRate                   float64
	BlockReward                int64
	NextBlockReward            int64
	PoWReward                  int64
	PoSReward                  int64
	ProjectFundReward          int64
	VotesInMempool             int
	TicketsInMempool           int
	TicketPrice                float64
	NextEstimatedTicketPrice   float64
	TicketPoolSize             uint32
	TicketPoolSizePerToTarget  float64
	TicketPoolValue            float64
	TPVOfTotalSupplyPeecentage float64
	TicketsROI                 float64
	RewardPeriod               string
	ASR                        float64
	APR                        float64
	IdxBlockInWindow           int
	WindowSize                 int64
	BlockTime                  int64
	IdxInRewardWindow          int
	RewardWindowSize           int64
}

// UnspentOutputIndices finds the indices of the transaction outputs that appear
// unspent. The indices returned are the index within the passed slice, not
// within the transaction.
func UnspentOutputIndices(vouts []Vout) (unspents []int) {
	for idx := range vouts {
		vout := vouts[idx]
		if vout.Amount == 0.0 || vout.Spent {
			continue
		}
		unspents = append(unspents, idx)
	}
	return
}

// MsgTxMempoolInputs parses a MsgTx and creates a list of MempoolInput.
func MsgTxMempoolInputs(msgTx *wire.MsgTx) (inputs []MempoolInput) {
	for vindex := range msgTx.TxIn {
		outpoint := msgTx.TxIn[vindex].PreviousOutPoint
		outId := outpoint.Hash.String()
		inputs = append(inputs, MempoolInput{
			TxId:   outId,
			Index:  uint32(vindex),
			Outdex: outpoint.Index,
		})
	}
	return
}
