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
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
)

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

func BlockConsumesOutpointWithAddresses(block *dcrutil.Block, addrs map[string]TxAction,
	c *dcrrpcclient.Client, params *chaincfg.Params) map[string][]*dcrutil.Tx {
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

func FeeInfoBlock(block *dcrutil.Block, c *dcrrpcclient.Client) *dcrjson.FeeInfoBlock {
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
