// txhelpers.go contains helper functions for working with transactions and
// blocks (e.g. checking for a transaction in a block).

package txhelpers

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrutil"
)

// RawTransactionGetter is an interface satisfied by dcrrpcclient.Client, and
// required by functions that would otherwise require a dcrrpcclient.Client just
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
// PkScript of that output. chaincfg Params is requried to decode the script.
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
					fmt.Printf("Unable to get raw transaction for %s", prevOut.Hash.String())
					continue
				}

				// prevOut.Index should tell us which one, but check all anyway
				for _, txOut := range prevTx.MsgTx().TxOut {
					_, txAddrs, _, err := txscript.ExtractPkScriptAddrs(
						txOut.Version, txOut.PkScript, params)
					if err != nil {
						fmt.Printf("ExtractPkScriptAddrs: %v", err.Error())
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

// OutPointAddresses gets the addresses payed to by a transaction output.
func OutPointAddresses(outPoint *wire.OutPoint, c RawTransactionGetter,
	params *chaincfg.Params) []string {
	// The addresses are encoded in the pkScript, so we need to get the
	// raw transaction, and the TxOut that contains the pkScript.
	prevTx, err := c.GetRawTransaction(&outPoint.Hash)
	if err != nil {
		fmt.Printf("Unable to get raw transaction for %s", outPoint.Hash.String())
		return nil
	}

	txOuts := prevTx.MsgTx().TxOut
	if len(txOuts) <= int(outPoint.Index) {
		fmt.Printf("PrevOut index (%d) is beyond the TxOuts slice (length %d)",
			outPoint.Index, len(txOuts))
		return nil
	}

	// For the TxOut of interest, extract the list of addresses
	txOut := txOuts[outPoint.Index]
	_, txAddrs, _, err := txscript.ExtractPkScriptAddrs(
		txOut.Version, txOut.PkScript, params)
	if err != nil {
		fmt.Printf("ExtractPkScriptAddrs: %v", err.Error())
		return nil
	}

	addresses := make([]string, 0, len(txAddrs))
	for _, txAddr := range txAddrs {
		addr := txAddr.EncodeAddress()
		addresses = append(addresses, addr)
	}
	return addresses
}

// OutPointAddressesFromString is the same as OutPointAddresses, but it takes
// the outpoint as the tx string, vout index, and tree.
func OutPointAddressesFromString(txid string, index uint32, tree int8,
	c RawTransactionGetter, params *chaincfg.Params) []string {
	hash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		fmt.Printf("Invalid hash %s", txid)
		return nil
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
func SSTXInBlock(block *dcrutil.Block, c RawTransactionGetter) []*dcrutil.Tx {
	newSStx := TicketsInBlock(block)
	allSSTx := make([]*dcrutil.Tx, len(newSStx))
	for it := range newSStx {
		var err error
		allSSTx[it], err = c.GetRawTransaction(&newSStx[it])
		if err != nil {
			fmt.Printf("Unable to get sstx details: %v", err)
		}
	}
	return allSSTx
}

// FeeInfoBlock computes ticket fee statistics for the tickets included in the
// specified block.  The RPC client is used to fetch raw transaction details
// need to compute the fee for each sstx.
func FeeInfoBlock(block *dcrutil.Block, c RawTransactionGetter) *dcrjson.FeeInfoBlock {
	feeInfo := new(dcrjson.FeeInfoBlock)
	newSStx := TicketsInBlock(block)

	feeInfo.Height = uint32(block.Height())
	feeInfo.Number = uint32(len(newSStx))

	var minFee, maxFee, meanFee float64
	minFee = math.MaxFloat64
	fees := make([]float64, feeInfo.Number)
	for it := range newSStx {
		//var rawTx *dcrutil.Tx
		// rawTx, err := c.GetRawTransactionVerbose(&newSStx[it])
		// if err != nil {
		// 	log.Errorf("Unable to get sstx details: %v", err)
		// }
		// rawTx.Vin[iv].AmountIn
		rawTx, err := c.GetRawTransaction(&newSStx[it])
		if err != nil {
			fmt.Printf("Unable to get sstx details: %v", err)
		}
		msgTx := rawTx.MsgTx()
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
// in the specified block.  The RPC client is used to fetch raw transaction
// details need to compute the fee rate for each sstx.
func FeeRateInfoBlock(block *dcrutil.Block, c RawTransactionGetter) *dcrjson.FeeInfoBlock {
	feeInfo := new(dcrjson.FeeInfoBlock)
	newSStx := TicketsInBlock(block)

	feeInfo.Height = uint32(block.Height())
	feeInfo.Number = uint32(len(newSStx))

	//var minFee, maxFee dcrutil.Amount
	//minFee = dcrutil.MaxAmount
	var minFee, maxFee, meanFee float64
	minFee = math.MaxFloat64
	feesRates := make([]float64, feeInfo.Number)
	for it := range newSStx {
		rawTx, err := c.GetRawTransaction(&newSStx[it])
		if err != nil {
			fmt.Printf("Unable to get sstx details: %v", err)
		}
		msgTx := rawTx.MsgTx()
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
