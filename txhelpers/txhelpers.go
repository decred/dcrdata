// txhelpers.go contains helper functions for working with transactions and
// blocks (e.g. checking for a transaction in a block).

package txhelpers

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
)

// RawTransactionGetter is an interface satisfied by rpcclient.Client, and
// required by functions that would otherwise require a rpcclient.Client just
// for GetRawTransaction.
type RawTransactionGetter interface {
	GetRawTransaction(txHash *chainhash.Hash) (*dcrutil.Tx, error)
}

// BlockWatchedTx contains, for a certain block, the transactions for certain
// watched addresses
type BlockWatchedTx struct {
	BlockHeight   int64
	TxsForAddress map[string][]*dcrutil.Tx
}

// TxAction is what is happening to the transaction (mined or inserted into
// mempool).
type TxAction int32

// Valid values for TxAction
const (
	TxMined TxAction = 1 << iota
	TxInserted
	// removed? invalidated?
)

// HashInSlice determines if a hash exists in a slice of hashes.
func HashInSlice(h chainhash.Hash, list []chainhash.Hash) bool {
	for _, hash := range list {
		if h == hash {
			return true
		}
	}
	return false
}

// TxhashInSlice searches a slice of *dcrutil.Tx for a transaction with the hash
// txHash. If found, it returns the corresponding *Tx, otherwise nil.
func TxhashInSlice(txs []*dcrutil.Tx, txHash *chainhash.Hash) *dcrutil.Tx {
	if len(txs) < 1 {
		return nil
	}

	for _, minedTx := range txs {
		txSha := minedTx.Hash()
		if txHash.IsEqual(txSha) {
			return minedTx
		}
	}
	return nil
}

// IncludesStakeTx checks if a block contains a stake transaction hash
func IncludesStakeTx(txHash *chainhash.Hash, block *dcrutil.Block) (int, int8) {
	blockTxs := block.STransactions()

	if tx := TxhashInSlice(blockTxs, txHash); tx != nil {
		return tx.Index(), tx.Tree()
	}
	return -1, -1
}

// IncludesTx checks if a block contains a transaction hash
func IncludesTx(txHash *chainhash.Hash, block *dcrutil.Block) (int, int8) {
	blockTxs := block.Transactions()

	if tx := TxhashInSlice(blockTxs, txHash); tx != nil {
		return tx.Index(), tx.Tree()
	}
	return -1, -1
}

// BlockConsumesOutpointWithAddresses checks the specified block to see if it
// includes transactions that spend from outputs created using any of the
// addresses in addrs. The TxAction for each address is not important, but it
// would logically be TxMined. Both regular and stake transactions are checked.
// The RPC client is used to get the PreviousOutPoint for each TxIn of each
// transaction in the block, from which the address is obtained from the
// PkScript of that output. chaincfg Params is required to decode the script.
func BlockConsumesOutpointWithAddresses(block *dcrutil.Block, addrs map[string]TxAction,
	c RawTransactionGetter, params *chaincfg.Params) map[string][]*dcrutil.Tx {
	addrMap := make(map[string][]*dcrutil.Tx)

	checkForOutpointAddr := func(blockTxs []*dcrutil.Tx) {
		for _, tx := range blockTxs {
			for _, txIn := range tx.MsgTx().TxIn {
				prevOut := &txIn.PreviousOutPoint
				// For each TxIn, check the indicated vout index in the txid of the
				// previous outpoint.
				// txrr, err := c.GetRawTransactionVerbose(&prevOut.Hash)
				prevTx, err := c.GetRawTransaction(&prevOut.Hash)
				if err != nil {
					fmt.Printf("Unable to get raw transaction for %s\n", prevOut.Hash.String())
					continue
				}

				// prevOut.Index should tell us which one, but check all anyway
				for _, txOut := range prevTx.MsgTx().TxOut {
					_, txAddrs, _, err := txscript.ExtractPkScriptAddrs(
						txOut.Version, txOut.PkScript, params)
					if err != nil {
						fmt.Printf("ExtractPkScriptAddrs: %v\n", err.Error())
						continue
					}

					for _, txAddr := range txAddrs {
						addrstr := txAddr.EncodeAddress()
						if _, ok := addrs[addrstr]; ok {
							if addrMap[addrstr] == nil {
								addrMap[addrstr] = make([]*dcrutil.Tx, 0)
							}
							addrMap[addrstr] = append(addrMap[addrstr], prevTx)
						}
					}
				}
			}
		}
	}

	checkForOutpointAddr(block.Transactions())
	checkForOutpointAddr(block.STransactions())

	return addrMap
}

// BlockReceivesToAddresses checks a block for transactions paying to the
// specified addresses, and creates a map of addresses to a slice of dcrutil.Tx
// involving the address.
func BlockReceivesToAddresses(block *dcrutil.Block, addrs map[string]TxAction,
	params *chaincfg.Params) map[string][]*dcrutil.Tx {
	addrMap := make(map[string][]*dcrutil.Tx)

	checkForAddrOut := func(blockTxs []*dcrutil.Tx) {
		for _, tx := range blockTxs {
			// Check the addresses associated with the PkScript of each TxOut
			for _, txOut := range tx.MsgTx().TxOut {
				_, txOutAddrs, _, err := txscript.ExtractPkScriptAddrs(txOut.Version,
					txOut.PkScript, params)
				if err != nil {
					fmt.Printf("ExtractPkScriptAddrs: %v", err.Error())
					continue
				}

				// Check if we are watching any address for this TxOut
				for _, txAddr := range txOutAddrs {
					addrstr := txAddr.EncodeAddress()
					if _, ok := addrs[addrstr]; ok {
						if _, gotSlice := addrMap[addrstr]; !gotSlice {
							addrMap[addrstr] = make([]*dcrutil.Tx, 0) // nil
						}
						addrMap[addrstr] = append(addrMap[addrstr], tx)
					}
				}
			}
		}
	}

	checkForAddrOut(block.Transactions())
	checkForAddrOut(block.STransactions())

	return addrMap
}

// OutPointAddresses gets the addresses paid to by a transaction output.
func OutPointAddresses(outPoint *wire.OutPoint, c RawTransactionGetter,
	params *chaincfg.Params) ([]string, error) {
	// The addresses are encoded in the pkScript, so we need to get the
	// raw transaction, and the TxOut that contains the pkScript.
	prevTx, err := c.GetRawTransaction(&outPoint.Hash)
	if err != nil {
		return nil, fmt.Errorf("unable to get raw transaction for %s", outPoint.Hash.String())
	}

	txOuts := prevTx.MsgTx().TxOut
	if len(txOuts) <= int(outPoint.Index) {
		return nil, fmt.Errorf("PrevOut index (%d) is beyond the TxOuts slice (length %d)",
			outPoint.Index, len(txOuts))
	}

	// For the TxOut of interest, extract the list of addresses
	txOut := txOuts[outPoint.Index]
	_, txAddrs, _, err := txscript.ExtractPkScriptAddrs(
		txOut.Version, txOut.PkScript, params)
	if err != nil {
		return nil, fmt.Errorf("ExtractPkScriptAddrs: %v", err.Error())
	}

	addresses := make([]string, 0, len(txAddrs))
	for _, txAddr := range txAddrs {
		addr := txAddr.EncodeAddress()
		addresses = append(addresses, addr)
	}
	return addresses, nil
}

// OutPointAddressesFromString is the same as OutPointAddresses, but it takes
// the outpoint as the tx string, vout index, and tree.
func OutPointAddressesFromString(txid string, index uint32, tree int8,
	c RawTransactionGetter, params *chaincfg.Params) ([]string, error) {
	hash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return nil, fmt.Errorf("Invalid hash %s", txid)
	}

	outPoint := wire.NewOutPoint(hash, index, tree)

	return OutPointAddresses(outPoint, c, params)
}

// MedianAmount gets the median Amount from a slice of Amounts
func MedianAmount(s []dcrutil.Amount) dcrutil.Amount {
	if len(s) == 0 {
		return 0
	}

	sort.Sort(dcrutil.AmountSorter(s))

	middle := len(s) / 2

	if len(s) == 0 {
		return 0
	} else if (len(s) % 2) != 0 {
		return s[middle]
	}
	return (s[middle] + s[middle-1]) / 2
}

// MedianCoin gets the median DCR from a slice of float64s
func MedianCoin(s []float64) float64 {
	if len(s) == 0 {
		return 0
	}

	sort.Float64s(s)

	middle := len(s) / 2

	if len(s) == 0 {
		return 0
	} else if (len(s) % 2) != 0 {
		return s[middle]
	}
	return (s[middle] + s[middle-1]) / 2
}

// GetDifficultyRatio returns the proof-of-work difficulty as a multiple of the
// minimum difficulty using the passed bits field from the header of a block.
func GetDifficultyRatio(bits uint32, params *chaincfg.Params) float64 {
	// The minimum difficulty is the max possible proof-of-work limit bits
	// converted back to a number.  Note this is not the same as the proof of
	// work limit directly because the block difficulty is encoded in a block
	// with the compact form which loses precision.
	max := blockchain.CompactToBig(params.PowLimitBits)
	target := blockchain.CompactToBig(bits)

	difficulty := new(big.Rat).SetFrac(max, target)
	outString := difficulty.FloatString(8)
	diff, err := strconv.ParseFloat(outString, 64)
	if err != nil {
		fmt.Printf("Cannot get difficulty: %v", err)
		return 0
	}
	return diff
}

// SSTXInBlock gets a slice containing all of the SSTX mined in a block
func SSTXInBlock(block *dcrutil.Block) []*dcrutil.Tx {
	_, txns := TicketTxnsInBlock(block)
	return txns
}

// SSGenVoteBlockValid determines if a vote transaction is voting yes or no to a
// block, and returns the votebits in case the caller wants to check agenda
// votes. The error return may be ignored if the input transaction is known to
// be a valid ssgen (vote), otherwise it should be checked.
func SSGenVoteBlockValid(msgTx *wire.MsgTx) (BlockValidation, uint16, error) {
	if !stake.IsSSGen(msgTx) {
		return BlockValidation{}, 0, fmt.Errorf("not a vote transaction")
	}

	ssGenVoteBits := stake.SSGenVoteBits(msgTx)
	blockHash, blockHeight := stake.SSGenBlockVotedOn(msgTx)
	blockValid := BlockValidation{
		Hash:     blockHash,
		Height:   int64(blockHeight),
		Validity: dcrutil.IsFlagSet16(ssGenVoteBits, dcrutil.BlockValid),
	}
	return blockValid, ssGenVoteBits, nil
}

// VoteBitsInBlock returns a list of vote bits for the votes in a block
func VoteBitsInBlock(block *dcrutil.Block) []stake.VoteVersionTuple {
	var voteBits []stake.VoteVersionTuple
	for _, stx := range block.MsgBlock().STransactions {
		if !stake.IsSSGen(stx) {
			continue
		}

		voteBits = append(voteBits, stake.VoteVersionTuple{
			Version: stake.SSGenVersion(stx),
			Bits:    stake.SSGenVoteBits(stx),
		})
	}

	return voteBits
}

// SSGenVoteBits returns the VoteBits of txOut[1] of a ssgen tx
func SSGenVoteBits(tx *wire.MsgTx) (uint16, error) {
	if len(tx.TxOut) < 2 {
		return 0, fmt.Errorf("not a ssgen")
	}

	pkScript := tx.TxOut[1].PkScript
	if len(pkScript) < 8 {
		return 0, fmt.Errorf("vote consensus version abent")
	}

	return binary.LittleEndian.Uint16(pkScript[2:4]), nil
}

// BlockValidation models the block validation indicated by an ssgen (vote)
// transaction.
type BlockValidation struct {
	// Hash is the hash of the block being targeted (in)validated
	Hash chainhash.Hash

	// Height is the height of the block
	Height int64

	// Validity indicates the vote is to validate (true) or invalidate (false)
	// the block.
	Validity bool
}

// VoteChoice represents the choice made by a vote transaction on a single vote
// item in an agenda. The ID, Description, and Mask fields describe the vote
// item for which the choice is being made. Those are the initial fields in
// chaincfg.Params.Deployments[VoteVersion][VoteIndex].
type VoteChoice struct {
	// Single unique word identifying the vote.
	ID string `json:"id"`

	// Longer description of what the vote is about.
	Description string `json:"description"`

	// Usable bits for this vote.
	Mask uint16 `json:"mask"`

	// VoteVersion and VoteIndex specify which vote item is referenced by this
	// VoteChoice (i.e. chaincfg.Params.Deployments[VoteVersion][VoteIndex]).
	VoteVersion uint32 `json:"vote_version"`
	VoteIndex   int    `json:"vote_index"`

	// ChoiceIdx indicates the corresponding element in the vote item's []Choice
	ChoiceIdx int `json:"choice_index"`

	// Choice is the selected choice for the specified vote item
	Choice *chaincfg.Choice `json:"choice"`
}

// VoteVersion extracts the vote version from the input pubkey script.
func VoteVersion(pkScript []byte) uint32 {
	if len(pkScript) < 8 {
		return stake.VoteConsensusVersionAbsent
	}

	return binary.LittleEndian.Uint32(pkScript[4:8])
}

// SSGenVoteChoices gets a ssgen's vote choices (block validity and any
// agendas). The vote's stake version, to which the vote choices correspond, and
// vote bits are also returned. Note that []*VoteChoice may be an empty slice if
// there are no consensus deployments for the transaction's vote version. The
// error value may be non-nil if the tx is not a valid ssgen.
func SSGenVoteChoices(tx *wire.MsgTx, params *chaincfg.Params) (BlockValidation, uint32, uint16, []*VoteChoice, error) {
	validBlock, voteBits, err := SSGenVoteBlockValid(tx)
	if err != nil {
		return validBlock, 0, 0, nil, err
	}

	// Determine the ssgen's vote version and get the relevant consensus
	// deployments containing the vote items targeted.
	voteVersion := stake.SSGenVersion(tx)
	deployments := params.Deployments[voteVersion]

	// Allocate space for each choice
	choices := make([]*VoteChoice, 0, len(deployments))

	// For each vote item (consensus deployment), extract the choice from the
	// vote bits and store the vote item's Id, Description and vote bits Mask.
	for d := range deployments {
		voteAgenda := &deployments[d].Vote
		choiceIndex := voteAgenda.VoteIndex(voteBits)
		voteChoice := VoteChoice{
			ID:          voteAgenda.Id,
			Description: voteAgenda.Description,
			Mask:        voteAgenda.Mask,
			VoteVersion: voteVersion,
			VoteIndex:   d,
			ChoiceIdx:   choiceIndex,
			Choice:      &voteAgenda.Choices[choiceIndex],
		}
		choices = append(choices, &voteChoice)
	}

	return validBlock, voteVersion, voteBits, choices, nil
}

// FeeInfoBlock computes ticket fee statistics for the tickets included in the
// specified block.
func FeeInfoBlock(block *dcrutil.Block) *dcrjson.FeeInfoBlock {
	feeInfo := new(dcrjson.FeeInfoBlock)
	_, sstxMsgTxns := TicketsInBlock(block)

	feeInfo.Height = uint32(block.Height())
	feeInfo.Number = uint32(len(sstxMsgTxns))

	var minFee, maxFee, meanFee float64
	minFee = math.MaxFloat64
	fees := make([]float64, feeInfo.Number)
	for it, msgTx := range sstxMsgTxns {
		var amtIn int64
		for iv := range msgTx.TxIn {
			amtIn += msgTx.TxIn[iv].ValueIn
		}
		var amtOut int64
		for iv := range msgTx.TxOut {
			amtOut += msgTx.TxOut[iv].Value
		}
		fee := dcrutil.Amount(amtIn - amtOut).ToCoin()
		if fee < minFee {
			minFee = fee
		}
		if fee > maxFee {
			maxFee = fee
		}
		meanFee += fee
		fees[it] = fee
	}

	if feeInfo.Number > 0 {
		N := float64(feeInfo.Number)
		meanFee /= N
		feeInfo.Mean = meanFee
		feeInfo.Median = MedianCoin(fees)
		feeInfo.Min = minFee
		feeInfo.Max = maxFee

		if N > 1 {
			var variance float64
			for _, f := range fees {
				variance += (f - meanFee) * (f - meanFee)
			}
			variance /= (N - 1)
			feeInfo.StdDev = math.Sqrt(variance)
		}
	}

	return feeInfo
}

// FeeRateInfoBlock computes ticket fee rate statistics for the tickets included
// in the specified block.
func FeeRateInfoBlock(block *dcrutil.Block) *dcrjson.FeeInfoBlock {
	feeInfo := new(dcrjson.FeeInfoBlock)
	_, sstxMsgTxns := TicketsInBlock(block)

	feeInfo.Height = uint32(block.Height())
	feeInfo.Number = uint32(len(sstxMsgTxns))

	var minFee, maxFee, meanFee float64
	minFee = math.MaxFloat64
	feesRates := make([]float64, feeInfo.Number)
	for it, msgTx := range sstxMsgTxns {
		var amtIn, amtOut int64
		for iv := range msgTx.TxIn {
			amtIn += msgTx.TxIn[iv].ValueIn
		}
		for iv := range msgTx.TxOut {
			amtOut += msgTx.TxOut[iv].Value
		}
		fee := dcrutil.Amount(1000*(amtIn-amtOut)).ToCoin() / float64(msgTx.SerializeSize())
		if fee < minFee {
			minFee = fee
		}
		if fee > maxFee {
			maxFee = fee
		}
		meanFee += fee
		feesRates[it] = fee
	}

	if feeInfo.Number > 0 {
		N := float64(feeInfo.Number)
		feeInfo.Mean = meanFee / N
		feeInfo.Median = MedianCoin(feesRates)
		feeInfo.Min = minFee
		feeInfo.Max = maxFee

		if feeInfo.Number > 1 {
			var variance float64
			for _, f := range feesRates {
				fDev := f - feeInfo.Mean
				variance += fDev * fDev
			}
			variance /= (N - 1)
			feeInfo.StdDev = math.Sqrt(variance)
		}
	}

	return feeInfo
}

// MsgTxFromHex returns a wire.MsgTx struct built from the transaction hex string
func MsgTxFromHex(txhex string) (*wire.MsgTx, error) {
	txBytes, err := hex.DecodeString(txhex)
	if err != nil {
		return nil, err
	}
	msgTx := wire.NewMsgTx()
	if err = msgTx.FromBytes(txBytes); err != nil {
		return nil, err
	}
	return msgTx, nil
}

// DetermineTxTypeString returns a string representing the transaction type given
// a wire.MsgTx struct
func DetermineTxTypeString(msgTx *wire.MsgTx) string {
	switch stake.DetermineTxType(msgTx) {
	case stake.TxTypeSSGen:
		return "Vote"
	case stake.TxTypeSStx:
		return "Ticket"
	case stake.TxTypeSSRtx:
		return "Revocation"
	default:
		return "Regular"
	}
}

// IsStakeTx indicates if the input MsgTx is a stake transaction.
func IsStakeTx(msgTx *wire.MsgTx) bool {
	switch stake.DetermineTxType(msgTx) {
	case stake.TxTypeSSGen:
		fallthrough
	case stake.TxTypeSStx:
		fallthrough
	case stake.TxTypeSSRtx:
		return true
	default:
		return false
	}
}

// TxFee computes and returns the fee for a given tx
func TxFee(msgTx *wire.MsgTx) dcrutil.Amount {
	var amtIn int64
	for iv := range msgTx.TxIn {
		amtIn += msgTx.TxIn[iv].ValueIn
	}
	var amtOut int64
	for iv := range msgTx.TxOut {
		amtOut += msgTx.TxOut[iv].Value
	}
	return dcrutil.Amount(amtIn - amtOut)
}

// TxFeeRate computes and returns the fee rate in DCR/KB for a given tx
func TxFeeRate(msgTx *wire.MsgTx) (dcrutil.Amount, dcrutil.Amount) {
	var amtIn int64
	for iv := range msgTx.TxIn {
		amtIn += msgTx.TxIn[iv].ValueIn
	}
	var amtOut int64
	for iv := range msgTx.TxOut {
		amtOut += msgTx.TxOut[iv].Value
	}
	return dcrutil.Amount(amtIn - amtOut), dcrutil.Amount(1000 * (amtIn - amtOut) / int64(msgTx.SerializeSize()))
}

// TotalOutFromMsgTx computes the total value out of a MsgTx
func TotalOutFromMsgTx(msgTx *wire.MsgTx) dcrutil.Amount {
	var amtOut int64
	for _, v := range msgTx.TxOut {
		amtOut += v.Value
	}
	return dcrutil.Amount(amtOut)
}

// TotalVout computes the total value of a slice of dcrjson.Vout
func TotalVout(vouts []dcrjson.Vout) dcrutil.Amount {
	var total dcrutil.Amount
	for _, v := range vouts {
		a, err := dcrutil.NewAmount(v.Value)
		if err != nil {
			continue
		}
		total += a
	}
	return total
}
