// Copyright (c) 2018-2021, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package insight

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/rpcclient/v6"

	m "github.com/decred/dcrdata/cmd/dcrdata/middleware"
	apitypes "github.com/decred/dcrdata/v6/api/types"
	"github.com/decred/dcrdata/v6/db/dbtypes"
	"github.com/decred/dcrdata/v6/rpcutils"
)

type BlockDataSource interface {
	AddressBalance(address string) (bal *dbtypes.AddressBalance, cacheUpdated bool, err error)
	AddressIDsByOutpoint(txHash string, voutIndex uint32) ([]uint64, []string, int64, error)
	AddressUTXO(address string) ([]*dbtypes.AddressTxnOutput, bool, error)
	BlockSummaryTimeRange(min, max int64, limit int) ([]dbtypes.BlockDataBasic, error)
	GetBlockHash(idx int64) (string, error)
	GetBlockHeight(hash string) (int64, error)
	GetBlockVerboseByHash(hash string, verboseTx bool) *chainjson.GetBlockVerboseResult
	GetHeight() (int64, error)
	GetRawTransaction(txid *chainhash.Hash) (*chainjson.TxRawResult, error)
	GetTransactionHex(txid *chainhash.Hash) string
	Height() int64
	InsightAddressTransactions(addr []string, recentBlockHeight int64) (txs, recentTxs []chainhash.Hash, err error)
	SendRawTransaction(txhex string) (string, error)
	SpendDetailsForFundingTx(fundHash string) ([]*apitypes.SpendByFundingHash, error)
}

const (
	defaultReqPerSecLimit = 20.0

	// maxInsightAddrsUTXOs limits the number of UTXOs returned by the
	// addrs[/{addresses}]/utxo endpoints when the {addresses} list has more
	// than one address. The project fund address has 263383 UTXOs at block
	// height 342553, and this is a ~75MB JSON payload.
	maxInsightAddrsUTXOs = 500000

	// maxInsightAddrsTxns limits the number of transactions that may be
	// returned by the addrs[/{addresses}]/txs endpoints when the {addresses}
	// list has more than one address. This limit is applied to the "to" and
	// "from" URL query parameters. Note that each transaction requires a
	// getrawtransaction RPC call to dcrd.
	maxInsightAddrsTxns = 250

	// inflightUTXOLimit is a soft limit on the number of in-flight UTXOs that
	// may be handled by the /addr/{address}/utxo endpoint. It is set slightly
	// higher than maxInsightAddrsUTXOs to allow smaller requests to be
	// processed concurrently with a very large request.
	inflightUTXOLimit = maxInsightAddrsUTXOs * 5 / 4
)

// InsightApi contains the resources for the Insight HTTP API. InsightApi's
// methods include the http.Handlers for the URL path routes.
type InsightApi struct {
	nodeClient      *rpcclient.Client
	BlockData       BlockDataSource
	params          *chaincfg.Params
	mp              rpcutils.MempoolAddressChecker
	status          *apitypes.Status
	JSONIndent      string
	ReqPerSecLimit  float64
	inflightUTXOs   int64
	inflightLimiter sync.Mutex
}

// NewInsightAPI is the constructor for InsightApi.
func NewInsightAPI(client *rpcclient.Client, blockData BlockDataSource, params *chaincfg.Params,
	memPoolData rpcutils.MempoolAddressChecker, JSONIndent string, status *apitypes.Status) *InsightApi {

	return &InsightApi{
		nodeClient:     client,
		BlockData:      blockData,
		params:         params,
		mp:             memPoolData,
		status:         status,
		JSONIndent:     JSONIndent,
		ReqPerSecLimit: defaultReqPerSecLimit,
	}
}

// SetReqRateLimit is used to set the requests/second/IP for the Insight API's
// rate limiter.
func (iapi *InsightApi) SetReqRateLimit(reqPerSecLimit float64) {
	iapi.ReqPerSecLimit = reqPerSecLimit
}

// Insight API successful response for JSON return items.
func writeJSON(w http.ResponseWriter, thing interface{}, indent string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", indent)
	if err := encoder.Encode(thing); err != nil {
		apiLog.Warnf("JSON encode error: %v", err)
	}
}

// Insight API error response for a BAD REQUEST.  This means the request was
// malformed in some way or the request HASH, ADDRESS, BLOCK was not valid. The
// string must be escaped html.
func writeInsightError(w http.ResponseWriter, str string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	io.WriteString(w, str)
}

// Insight API response for an item NOT FOUND.  This means the request was valid
// but no records were found for the item in question.  For some endpoints
// responding with an empty array [] is expected such as a transaction query for
// addresses with no transactions. The string must be escaped html.
func writeInsightNotFound(w http.ResponseWriter, str string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	io.WriteString(w, str)
}

func (iapi *InsightApi) getTransaction(w http.ResponseWriter, r *http.Request) {
	txid, err := m.GetTxIDCtx(r)
	if err != nil {
		errStr := html.EscapeString(err.Error())
		writeInsightError(w, errStr)
		return
	}

	// Return raw transaction
	txOld, err := iapi.BlockData.GetRawTransaction(txid)
	if err != nil {
		apiLog.Errorf("Unable to get transaction %s", txid)
		writeInsightNotFound(w, fmt.Sprintf("Unable to get transaction (%s)", txid))
		return
	}

	txsOld := []*chainjson.TxRawResult{txOld}

	// convert to insight struct
	txsNew, err := iapi.TxConverter(txsOld)

	if err != nil {
		apiLog.Errorf("Error Processing Transactions")
		writeInsightError(w, "Error Processing Transactions")
		return
	}

	writeJSON(w, txsNew[0], m.GetIndentCtx(r))
}

func (iapi *InsightApi) getTransactionHex(w http.ResponseWriter, r *http.Request) {
	txid, err := m.GetTxIDCtx(r)
	if err != nil {
		errStr := html.EscapeString(err.Error())
		writeInsightError(w, errStr)
		return
	}

	txHex := iapi.BlockData.GetTransactionHex(txid)
	if txHex == "" {
		writeInsightNotFound(w, fmt.Sprintf("Unable to get transaction (%s)", txHex))
		return
	}

	hexOutput := &apitypes.InsightRawTx{
		Rawtx: txHex,
	}

	writeJSON(w, hexOutput, m.GetIndentCtx(r))
}

func (iapi *InsightApi) getBlockSummary(w http.ResponseWriter, r *http.Request) {
	// Attempt to get hash or height of block from URL path.
	hash, err := m.GetBlockHashCtx(r)
	if err != nil {
		idx := m.GetBlockIndexCtx(r)
		if idx < 0 {
			writeInsightError(w, "Must provide a block index or hash.")
			return
		}

		hash, err = iapi.BlockData.GetBlockHash(int64(idx))
		if dbtypes.IsTimeoutErr(err) {
			apiLog.Errorf("GetBlockHash: %v", err)
			http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
			return
		}
		if err != nil {
			writeInsightError(w, "Unable to get block hash from index")
			return
		}
	}

	block := iapi.BlockData.GetBlockVerboseByHash(hash, false)
	if block == nil {
		writeInsightNotFound(w, "Unable to get block")
		return
	}

	blockSummary := []*chainjson.GetBlockVerboseResult{block}
	blockInsight, err := iapi.DcrToInsightBlock(blockSummary)
	if err != nil {
		apiLog.Errorf("Unable to process block (%s)", hash)
		writeInsightError(w, "Unable to process block")
		return
	}

	writeJSON(w, blockInsight, m.GetIndentCtx(r))
}

func (iapi *InsightApi) getBlockHash(w http.ResponseWriter, r *http.Request) {
	idx := m.GetBlockIndexCtx(r)
	if idx < 0 {
		writeInsightError(w, "No index found in query")
		return
	}

	height := iapi.BlockData.Height()
	if idx < 0 || idx > int(height) {
		writeInsightError(w, "Block height out of range")
		return
	}
	hash, err := iapi.BlockData.GetBlockHash(int64(idx))
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("GetBlockHash: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}
	if err != nil || hash == "" {
		writeInsightNotFound(w, "Not found")
		return
	}

	blockOutput := struct {
		BlockHash string `json:"blockHash"`
	}{
		hash,
	}
	writeJSON(w, blockOutput, m.GetIndentCtx(r))
}

func (iapi *InsightApi) getRawBlock(w http.ResponseWriter, r *http.Request) {
	hash, err := m.GetBlockHashCtx(r)
	if err != nil {
		idx := m.GetBlockIndexCtx(r)
		if idx < 0 {
			writeInsightError(w, "Must provide an index or block hash")
			return
		}

		hash, err = iapi.BlockData.GetBlockHash(int64(idx))
		if dbtypes.IsTimeoutErr(err) {
			apiLog.Errorf("GetBlockHash: %v", err)
			http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
			return
		}
		if err != nil {
			writeInsightError(w, "Unable to get block hash from index")
			return
		}
	}

	chainHash, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		errStr := html.EscapeString(err.Error())
		writeInsightError(w, fmt.Sprintf("Failed to parse block hash: %q", errStr))
		return
	}

	blockMsg, err := iapi.nodeClient.GetBlock(r.Context(), chainHash)
	if err != nil {
		errStr := html.EscapeString(err.Error())
		writeInsightNotFound(w, fmt.Sprintf("Failed to retrieve block %s: %q",
			chainHash.String(), errStr))
		return
	}

	var blockHex bytes.Buffer
	if err = blockMsg.Serialize(&blockHex); err != nil {
		apiLog.Errorf("Failed to serialize block: %v", err)
		writeInsightError(w, "Failed to serialize block")
		return
	}

	blockJSON := struct {
		BlockHash string `json:"rawblock"`
	}{
		hex.EncodeToString(blockHex.Bytes()),
	}
	writeJSON(w, blockJSON, m.GetIndentCtx(r))
}

func (iapi *InsightApi) broadcastTransactionRaw(w http.ResponseWriter, r *http.Request) {
	// Check for rawtx.
	rawHexTx, err := m.GetRawHexTx(r)
	if err != nil {
		// JSON extraction failed or rawtx blank.
		errStr := html.EscapeString(err.Error())
		writeInsightError(w, errStr)
		return
	}

	// Check maximum transaction size.
	if len(rawHexTx)/2 > iapi.params.MaxTxSize {
		writeInsightError(w, fmt.Sprintf("Rawtx length exceeds maximum allowable characters"+
			"(%d bytes received)", len(rawHexTx)/2))
		return
	}

	// Broadcast the transaction.
	txid, err := iapi.BlockData.SendRawTransaction(rawHexTx)
	if err != nil {
		apiLog.Errorf("Unable to send transaction %s", rawHexTx)
		errStr := html.EscapeString(err.Error())
		writeInsightError(w, fmt.Sprintf("SendRawTransaction failed: %q", errStr))
		return
	}

	// Respond with hash of broadcasted transaction.
	txidJSON := struct {
		TxidHash string `json:"txid"`
	}{
		txid,
	}
	writeJSON(w, txidJSON, m.GetIndentCtx(r))
}

func (iapi *InsightApi) getAddressesTxnOutput(w http.ResponseWriter, r *http.Request) {
	addresses, err := m.GetAddressCtx(r, iapi.params) // Required, also validates the addresses
	if err != nil {
		errStr := html.EscapeString(err.Error())
		writeInsightError(w, errStr)
		return
	}

	t0 := time.Now()

	// When complete, subtract inflightUTXOs from this goroutine. If this call
	// got completion priority (hit the UTXO limit), and thus held the lock,
	// unlock prior to decrementing the in-flight UTXO count.
	var inflightUTXOs int64
	var priority bool
	defer func() {
		if priority {
			iapi.inflightLimiter.Unlock()
			apiLog.Info("Relinquishing prioritized goroutine status.")
		}
		final := atomic.AddInt64(&iapi.inflightUTXOs, -inflightUTXOs)
		apiLog.Tracef("Removing %d inflight UTXOs. New total = %d.", inflightUTXOs, final)

		apiLog.Debugf("getAddressesTxnOutput completed for %d addresses with %d UTXOs in %v.",
			len(addresses), inflightUTXOs, time.Since(t0))
	}()

	// Initialize output slice.
	txnOutputs := make([]*apitypes.AddressTxnOutput, 0)
	currentHeight := int32(iapi.BlockData.Height())

	for i, address := range addresses {
		// Unless this goroutine got completion priority on a previous address
		// and locked out other goroutines, we must wait for the lock to prevent
		// simultaneous requests from exceeding the inflight UTXO limit.
		if !priority {
			iapi.inflightLimiter.Lock()
		}

		tStart := time.Now()

		// Query for UTXOs for the current address.
		confirmedTxnOutputs, _, err := iapi.BlockData.AddressUTXO(address)

		apiLog.Debugf("AddressUTXO completed for %s with %d UTXOs in %v.",
			address, len(confirmedTxnOutputs), time.Since(tStart))

		// Account for in-flight UTXOs before error checking and unlocking.
		newInflightUTXOs := int64(len(confirmedTxnOutputs))
		totalInflight := atomic.AddInt64(&iapi.inflightUTXOs, newInflightUTXOs)
		apiLog.Tracef("Adding %d inflight (confirmed) UTXOs to the total.", newInflightUTXOs)
		inflightUTXOs += newInflightUTXOs

		// While locked, check in-flight UTXO count. If over the limit, become
		// the prioritized goroutine and hold the lock until this http handler
		// completes (and decrements the inflight UTXO count).
		if !priority {
			if totalInflight >= inflightUTXOLimit {
				// Over the limit, but it's our turn to wrap it up.
				priority = true
				apiLog.Infof("Becoming prioritized getAddressesTxnOutput "+
					"goroutine with %d total in-flight UTXOs.", totalInflight)
				// Unblock occurs only when we finish this entire http request.
			} else {
				// Otherwise, unlock now that the query and iapi.inflightUTXOs
				// has been updated.
				iapi.inflightLimiter.Unlock()
			}
		}

		// Check the returned error value from the query now that the limiter is
		// unlocked or unlocking is deferred.
		if dbtypes.IsTimeoutErr(err) {
			apiLog.Errorf("AddressUTXO: %v", err)
			http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
			return
		}
		if err != nil {
			apiLog.Errorf("Error getting UTXOs: %v", err)
			continue
		}

		// Unconfirmed UTXOs
		newInflightUTXOs = 0 // count the unconfirmed UTXOs now
		unconfirmedTxnOutputs, _, err := iapi.mp.UnconfirmedTxnsForAddress(address)
		if err != nil {
			apiLog.Errorf("Error getting unconfirmed transactions: %v", err)
			continue
		}

		if unconfirmedTxnOutputs != nil {
			// Add any relevant mempool transaction outputs to the UTXO set.
		FUNDING_TX_DUPLICATE_CHECK:
			for _, f := range unconfirmedTxnOutputs.Outpoints {
				fundingTx, ok := unconfirmedTxnOutputs.TxnsStore[f.Hash]
				if !ok {
					apiLog.Errorf("An outpoint's transaction is not available in TxnStore.")
					continue
				}
				if fundingTx.Confirmed() {
					apiLog.Errorf("An outpoint's transaction is unexpectedly confirmed.")
					continue
				}
				// TODO: Confirmed() not always return true for txs that have
				// already been confirmed in a block.  The mempool cache update
				// process should correctly update these.  Until we sort out why we
				// need to do one more search on utxo and do not add if this is
				// already in the list as a confirmed tx.
				for _, utxo := range confirmedTxnOutputs {
					if utxo.Vout == f.Index && utxo.TxHash == f.Hash {
						continue FUNDING_TX_DUPLICATE_CHECK
					}
				}

				newInflightUTXOs++

				txOut := fundingTx.Tx.TxOut[f.Index]

				txnOutputs = append(txnOutputs, &apitypes.AddressTxnOutput{
					Address:       address,
					TxnID:         fundingTx.Hash().String(),
					Vout:          f.Index,
					BlockTime:     fundingTx.MemPoolTime,
					ScriptPubKey:  hex.EncodeToString(txOut.PkScript),
					Amount:        dcrutil.Amount(txOut.Value).ToCoin(),
					Satoshis:      txOut.Value,
					Confirmations: 0,
				})
			}
		}

		// If multiple addresses were in the request, enforce a limit on the
		// number of UTXOs we will return. Do this before appending the latest
		// confirmed transactions slice.
		numOuts := len(confirmedTxnOutputs) + len(txnOutputs)
		if len(addresses) > 1 && numOuts > maxInsightAddrsUTXOs {
			writeInsightError(w, "Too many UTXOs in that result. "+
				"Please request the UTXOs for each address individually.")
			return
		}

		totalInflight = atomic.AddInt64(&iapi.inflightUTXOs, newInflightUTXOs)
		apiLog.Tracef("Adding %d inflight (unconfirmed) UTXOs for a total of %d.",
			newInflightUTXOs, totalInflight)
		inflightUTXOs += newInflightUTXOs

		// On the first address, start with a slice of the correct size.
		if i == 0 {
			txnOutputs = append(make([]*apitypes.AddressTxnOutput, 0, numOuts),
				txnOutputs...)
		}

		//txnOutputs = append(txnOutputs, confirmedTxnOutputs...)
		for _, out := range confirmedTxnOutputs {
			txnOutputs = append(txnOutputs, apitypes.TxOutFromDB(out, currentHeight))
		}
		//nolint:ineffassign
		confirmedTxnOutputs = nil // Go GC, go!

		// Search for items in mempool that spend one of these UTXOs and remove
		// them from the set.
		for _, f := range unconfirmedTxnOutputs.PrevOuts {
			spendingTx, ok := unconfirmedTxnOutputs.TxnsStore[f.TxSpending]
			if !ok {
				apiLog.Errorf("An outpoint's transaction is not available in TxnStore.")
				continue
			}
			if spendingTx.Confirmed() {
				apiLog.Errorf("A transaction spending the outpoint of an unconfirmed transaction is unexpectedly confirmed.")
				continue
			}

			var garbage []int
			for g, utxo := range txnOutputs {
				if utxo.Vout == f.PreviousOutpoint.Index && utxo.TxnID == f.PreviousOutpoint.Hash.String() {
					apiLog.Trace("Removing an unconfirmed spent UTXO.")
					garbage = append(garbage, g)
				}
			}

			// Remove entries from the end to the beginning of the slice.
			txnOutputs = removeSliceElements(txnOutputs, garbage)
		}
	}

	// Sort the UTXOs by timestamp (descending) if unconfirmed and by
	// confirmations (ascending) if confirmed.
	sort.Slice(txnOutputs, func(i, j int) bool {
		if txnOutputs[i].Confirmations == 0 && txnOutputs[j].Confirmations == 0 {
			return txnOutputs[i].BlockTime > txnOutputs[j].BlockTime
		}
		return txnOutputs[i].Confirmations < txnOutputs[j].Confirmations
	})

	writeJSON(w, txnOutputs, m.GetIndentCtx(r))
}

// removeSliceElements removes elements of the input slice at the specified
// indexes. NOTE: The input slice's buffer is modified but the output length
// will be different if any elements are removed, so this function should be
// called like `s = removeSliceElements(s, inds)`. Also note that the original
// order of elements in the input slice may not be maintained.
func removeSliceElements(txOuts []*apitypes.AddressTxnOutput, inds []int) []*apitypes.AddressTxnOutput {
	// Remove entries from the end to the beginning of the slice.
	sort.Slice(inds, func(i, j int) bool { return inds[i] > inds[j] }) // descending indexes
	for _, g := range inds {
		if g > len(txOuts)-1 {
			continue
		}
		txOuts[g] = txOuts[len(txOuts)-1] // overwrite element g with last element
		txOuts[len(txOuts)-1] = nil       // nil out last element
		txOuts = txOuts[:len(txOuts)-1]
	}
	return txOuts
}

func (iapi *InsightApi) getTransactions(w http.ResponseWriter, r *http.Request) {
	pageNum := m.GetPageNumCtx(r)
	hash, blockerr := m.GetBlockHashCtx(r)
	addresses, addrerr := m.GetAddressCtx(r, iapi.params) // validates the addresses

	if blockerr != nil && addrerr != nil {
		msg := fmt.Sprintf(`Required query parameters (address or block) not present. `+
			`address error: "%v" / block error: "%v"`, addrerr, blockerr)
		writeInsightError(w, html.EscapeString(msg))
		return
	}
	if addrerr == nil && len(addresses) > 1 {
		writeInsightError(w, "Only one address is allowed.")
		return
	}

	txPageSize := 10

	if blockerr == nil {
		blkTrans := iapi.BlockData.GetBlockVerboseByHash(hash, true)
		if blkTrans == nil {
			apiLog.Errorf("Unable to get block %s transactions", hash)
			writeInsightError(w, fmt.Sprintf("Unable to get block %q transactions", hash))
			return
		}

		// Sorting is not necessary since all of these transactions are in the
		// same block, but put the stake transactions first.
		mergedTxns := append(blkTrans.RawSTx, blkTrans.RawTx...)
		txCount := len(mergedTxns)
		if txCount == 0 {
			addrTransactions := apitypes.InsightBlockAddrTxSummary{
				Txs: []apitypes.InsightTx{},
			}
			writeJSON(w, addrTransactions, m.GetIndentCtx(r))
			return
		}

		pagesTotal := txCount / txPageSize
		if txCount%txPageSize != 0 {
			pagesTotal++
		}

		// Go to the last page if pageNum is greater than the number of pages.
		if pageNum > pagesTotal {
			pageNum = pagesTotal
		}

		// Grab the transactions for the given page (1-based index).
		skipTxns := (pageNum - 1) * txPageSize // middleware guarantees pageNum>0
		txsOld := []*chainjson.TxRawResult{}
		for i := skipTxns; i < txCount && i < txPageSize+skipTxns; i++ {
			txsOld = append(txsOld, &mergedTxns[i])
		}

		// Convert to chainjson transaction to Insight tx type.
		txsNew, err := iapi.TxConverter(txsOld)
		if err != nil {
			apiLog.Error("getTransactions: Error processing transactions: %v", err)
			writeInsightError(w, "Error Processing Transactions")
			return
		}

		blockTransactions := apitypes.InsightBlockAddrTxSummary{
			PagesTotal: int64(txCount),
			Txs:        txsNew,
		}
		writeJSON(w, blockTransactions, m.GetIndentCtx(r))
		return
	}

	if addrerr == nil {
		address := addresses[0]
		hashes, recentTxs, err :=
			iapi.BlockData.InsightAddressTransactions([]string{address},
				int64(iapi.status.Height()-2))
		if dbtypes.IsTimeoutErr(err) {
			apiLog.Errorf("InsightAddressTransactions: %v", err)
			http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
			return
		}
		if err != nil {
			errStr := html.EscapeString(err.Error())
			writeInsightError(w,
				fmt.Sprintf("Error retrieving transactions for address %s (%q)",
					address, errStr))
			return
		}

		addressOuts, _, err := iapi.mp.UnconfirmedTxnsForAddress(address)
		if err != nil {
			errStr := html.EscapeString(err.Error())
			writeInsightError(w, fmt.Sprintf("Error gathering mempool transactions (%q)", errStr))
			return
		}

		var UnconfirmedTxs []chainhash.Hash
		var UnconfirmedTxTimes []int64

	FUNDING_TX_DUPLICATE_CHECK:
		for _, f := range addressOuts.Outpoints {
			// Confirm its not already in our recent transactions
			for _, v := range recentTxs {
				if v.IsEqual(&f.Hash) {
					continue FUNDING_TX_DUPLICATE_CHECK
				}
			}
			UnconfirmedTxs = append(UnconfirmedTxs, f.Hash) // Funding tx
			recentTxs = append(recentTxs, f.Hash)

			// Access the mempool transaction store for MempoolTime.
			fundingTx, ok := addressOuts.TxnsStore[f.Hash]
			if !ok {
				apiLog.Errorf("An outpoint's transaction is not available in TxnStore.")
				continue
			}
			if fundingTx.Confirmed() {
				apiLog.Errorf("An outpoint's transaction is unexpectedly confirmed.")
				continue
			}

			UnconfirmedTxTimes = append(UnconfirmedTxTimes, fundingTx.MemPoolTime)
		}
	SPENDING_TX_DUPLICATE_CHECK:
		for _, f := range addressOuts.PrevOuts {
			for _, v := range recentTxs {
				if v.IsEqual(&f.TxSpending) {
					continue SPENDING_TX_DUPLICATE_CHECK
				}
			}
			UnconfirmedTxs = append(UnconfirmedTxs, f.TxSpending) // Spending tx
			recentTxs = append(recentTxs, f.TxSpending)

			// Access the mempool transaction store for MempoolTime.
			spendingTx, ok := addressOuts.TxnsStore[f.TxSpending]
			if !ok {
				apiLog.Errorf("An outpoint's transaction is not available in TxnStore.")
				continue
			}
			if spendingTx.Confirmed() {
				apiLog.Errorf("An outpoint's transaction is unexpectedly confirmed.")
				continue
			}

			UnconfirmedTxTimes = append(UnconfirmedTxTimes, spendingTx.MemPoolTime)
		}

		// Merge unconfirmed with confirmed transactions. Unconfirmed must be
		// first since these first transactions have their time set differently
		// from confirmed transactions.
		hashes = append(UnconfirmedTxs, hashes...)

		txCount := len(hashes)
		if txCount == 0 {
			addrTransactions := apitypes.InsightBlockAddrTxSummary{
				Txs: []apitypes.InsightTx{},
			}
			writeJSON(w, addrTransactions, m.GetIndentCtx(r))
			return
		}

		pagesTotal := txCount / txPageSize
		if txCount%txPageSize != 0 {
			pagesTotal++
		}

		// Go to the last page if pageNum is greater than the number of pages.
		if pageNum > pagesTotal {
			pageNum = pagesTotal
		}

		// Grab the transactions for the given page.
		skipTxns := (pageNum - 1) * txPageSize
		txsOld := []*chainjson.TxRawResult{}
		for i := skipTxns; i < txCount && i < txPageSize+skipTxns; i++ {
			txOld, err := iapi.BlockData.GetRawTransaction(&hashes[i])
			if err != nil {
				apiLog.Errorf("Unable to get transaction %s", hashes[i])
				errStr := html.EscapeString(err.Error())
				writeInsightError(w, fmt.Sprintf("Error gathering transaction details (%q)", errStr))
				return
			}

			if i < len(UnconfirmedTxTimes) {
				txOld.Time = UnconfirmedTxTimes[i]
			}
			txsOld = append(txsOld, txOld)
		}

		// Convert to chainjson transaction to Insight tx type. TxConverter also
		// retrieves previous outpoint addresses.
		txsNew, err := iapi.TxConverter(txsOld)
		if err != nil {
			apiLog.Error("getTransactions: Error processing transactions: %v", err)
			writeInsightError(w, "Error Processing Transactions")
			return
		}

		addrTransactions := apitypes.InsightBlockAddrTxSummary{
			PagesTotal: int64(pagesTotal),
			Txs:        txsNew,
		}
		writeJSON(w, addrTransactions, m.GetIndentCtx(r))
	}
}

func (iapi *InsightApi) getAddressesTxn(w http.ResponseWriter, r *http.Request) {
	addresses, err := m.GetAddressCtx(r, iapi.params) // Required, also validates the addresses
	if err != nil {
		errStr := html.EscapeString(err.Error())
		writeInsightError(w, errStr)
		return
	}

	noAsm := GetNoAsmCtx(r)             // Optional
	noScriptSig := GetNoScriptSigCtx(r) // Optional
	noSpent := GetNoSpentCtx(r)         // Optional
	from := GetFromCtx(r)               // Optional
	if from < 0 {
		from = 0
	}
	to, ok := GetToCtx(r) // Optional
	if !ok {
		to = from + 10
	}
	if to < 0 {
		to = 0
	}
	if from > to {
		to = from
	}

	if to-from > maxInsightAddrsTxns {
		writeInsightError(w, fmt.Sprintf(
			`"from" (%d) and "to" (%d) range should be less than or equal to %d`,
			from, to, maxInsightAddrsTxns))
		return
	}

	// Initialize output structure.
	addressOutput := new(apitypes.InsightMultiAddrsTxOutput)
	var UnconfirmedTxs []chainhash.Hash
	var UnconfirmedTxTimes []int64

	rawTxs, recentTxs, err :=
		iapi.BlockData.InsightAddressTransactions(addresses, int64(iapi.status.Height()-2))
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("InsightAddressTransactions: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		errStr := html.EscapeString(err.Error())
		writeInsightError(w,
			fmt.Sprintf("Error retrieving transactions for addresses %s (%q)",
				addresses, errStr))
		return
	}

	// Pull unconfirmed transactions for all addresses.
	for _, addr := range addresses {
		addressOuts, _, err := iapi.mp.UnconfirmedTxnsForAddress(addr)
		if err != nil {
			errStr := html.EscapeString(err.Error())
			writeInsightError(w, fmt.Sprintf("Error gathering mempool transactions (%q)", errStr))
			return
		}

	FUNDING_TX_DUPLICATE_CHECK:
		for _, f := range addressOuts.Outpoints {
			// Confirm it's not already in our recent transactions.
			for _, v := range recentTxs {
				if v.IsEqual(&f.Hash) {
					continue FUNDING_TX_DUPLICATE_CHECK
				}
			}
			UnconfirmedTxs = append(UnconfirmedTxs, f.Hash) // funding
			recentTxs = append(recentTxs, f.Hash)

			// Access the mempool transaction store for MempoolTime.
			fundingTx, ok := addressOuts.TxnsStore[f.Hash]
			if !ok {
				apiLog.Errorf("An outpoint's transaction is not available in TxnStore.")
				continue
			}
			if fundingTx.Confirmed() {
				apiLog.Errorf("An outpoint's transaction is unexpectedly confirmed.")
				continue
			}

			UnconfirmedTxTimes = append(UnconfirmedTxTimes, fundingTx.MemPoolTime)
		}
	SPENDING_TX_DUPLICATE_CHECK:
		for _, f := range addressOuts.PrevOuts {
			for _, v := range recentTxs {
				if v.IsEqual(&f.TxSpending) {
					continue SPENDING_TX_DUPLICATE_CHECK
				}
			}
			UnconfirmedTxs = append(UnconfirmedTxs, f.TxSpending) // spending
			recentTxs = append(recentTxs, f.TxSpending)

			// Access the mempool transaction store for MempoolTime.
			spendingTx, ok := addressOuts.TxnsStore[f.TxSpending]
			if !ok {
				apiLog.Errorf("An outpoint's transaction is not available in TxnStore.")
				continue
			}
			if spendingTx.Confirmed() {
				apiLog.Errorf("An outpoint's transaction is unexpectedly confirmed.")
				continue
			}

			UnconfirmedTxTimes = append(UnconfirmedTxTimes, spendingTx.MemPoolTime)
		}
	}

	// Merge unconfirmed with confirmed transactions. Unconfirmed must be first.
	rawTxs = append(UnconfirmedTxs, rawTxs...)

	txCount := len(rawTxs)
	addressOutput.TotalItems = int64(txCount)

	// Set the actual to and from values given the total transactions.
	if txCount > 0 {
		if int(from) > txCount {
			from = int64(txCount)
		}
		if int(to) > txCount {
			to = int64(txCount)
		}
		if from > to {
			to = from
		}
		// Should already be checked at start, but do it again to be safe.
		if to-from > maxInsightAddrsTxns {
			writeInsightError(w, fmt.Sprintf(
				`"from" (%d) and "to" (%d) range should be less than or equal to %d`,
				from, to, maxInsightAddrsTxns))
			return
		}
		// Final slice extraction
		rawTxs = rawTxs[from:to]
	}
	addressOutput.From = int(from)
	addressOutput.To = int(to)

	// Make getrawtransaction RPCs for each selected transaction.
	txsOld := []*chainjson.TxRawResult{}
	for i, rawTx := range rawTxs {
		txOld, err := iapi.BlockData.GetRawTransaction(&rawTx)
		if err != nil {
			apiLog.Errorf("Unable to get transaction %s", rawTx)
			errStr := html.EscapeString(err.Error())
			writeInsightError(w, fmt.Sprintf("Error gathering transaction details (%q)", errStr))
			return
		}

		if i < len(UnconfirmedTxTimes) {
			txOld.Time = UnconfirmedTxTimes[i]
		}
		txsOld = append(txsOld, txOld)
	}

	// Convert to Insight API struct.
	txsNew, err := iapi.DcrToInsightTxns(txsOld, noAsm, noScriptSig, noSpent)
	if err != nil {
		apiLog.Error("Unable to process transactions")
		errStr := html.EscapeString(err.Error())
		writeInsightError(w, fmt.Sprintf("Unable to convert transactions (%q)", errStr))
		return
	}
	addressOutput.Items = append(addressOutput.Items, txsNew...)
	if addressOutput.Items == nil {
		// Pass a non-nil empty array for JSON if there are no txns.
		addressOutput.Items = make([]apitypes.InsightTx, 0)
	}

	writeJSON(w, addressOutput, m.GetIndentCtx(r))
}

func (iapi *InsightApi) getSyncInfo(w http.ResponseWriter, r *http.Request) {
	errorResponse := func(err error) {
		// To insure JSON encodes an error properly as a string, and no error as
		// null, use a pointer to a string.
		var errorString *string
		if err != nil {
			s := err.Error()
			errorString = &s
		}
		syncInfo := apitypes.SyncResponse{
			Status: "error",
			Error:  errorString,
		}
		writeJSON(w, syncInfo, m.GetIndentCtx(r))
	}

	blockChainHeight, err := iapi.nodeClient.GetBlockCount(r.Context())
	if err != nil {
		errorResponse(err)
		return
	}

	height := iapi.BlockData.Height()
	syncPercentage := int64((float64(height) / float64(blockChainHeight)) * 100)

	st := "syncing"
	if syncPercentage == 100 {
		st = "finished"
	}

	syncInfo := apitypes.SyncResponse{
		Status:           st,
		BlockChainHeight: blockChainHeight,
		SyncPercentage:   syncPercentage,
		Height:           height,
		Type:             "from RPC calls",
	}
	writeJSON(w, syncInfo, m.GetIndentCtx(r))
}

func (iapi *InsightApi) getStatusInfo(w http.ResponseWriter, r *http.Request) {
	statusInfo := m.GetStatusInfoCtx(r)
	ctx := r.Context()

	// best block idx is also embedded through the middleware.  We could use
	// this value or the other best blocks as done below.  Which one is best?
	// idx := m.GetBlockIndexCtx(r)

	infoResult, err := iapi.nodeClient.GetInfo(ctx)
	if err != nil {
		apiLog.Error("Error getting status")
		errStr := html.EscapeString(err.Error())
		writeInsightError(w, fmt.Sprintf("Error getting status (%q)", errStr))
		return
	}

	switch statusInfo {
	case "getDifficulty":
		info := struct {
			Difficulty float64 `json:"difficulty"`
		}{
			infoResult.Difficulty,
		}
		writeJSON(w, info, m.GetIndentCtx(r))
	case "getBestBlockHash":
		blockhash, err := iapi.nodeClient.GetBlockHash(ctx, infoResult.Blocks)
		if err != nil {
			apiLog.Errorf("Error getting block hash %d (%s)", infoResult.Blocks, err)
			errStr := html.EscapeString(err.Error())
			writeInsightError(w, fmt.Sprintf("Error getting block hash %d (%q)", infoResult.Blocks, errStr))
			return
		}

		info := struct {
			BestBlockHash string `json:"bestblockhash"`
		}{
			blockhash.String(),
		}
		writeJSON(w, info, m.GetIndentCtx(r))
	case "getLastBlockHash":
		blockhashtip, err := iapi.nodeClient.GetBlockHash(ctx, infoResult.Blocks)
		if err != nil {
			apiLog.Errorf("Error getting block hash %d (%s)", infoResult.Blocks, err)
			errStr := html.EscapeString(err.Error())
			writeInsightError(w, fmt.Sprintf("Error getting block hash %d (%q)", infoResult.Blocks, errStr))
			return
		}
		lastblockhash, err := iapi.nodeClient.GetBlockHash(ctx, int64(iapi.status.Height()))
		if err != nil {
			apiLog.Errorf("Error getting block hash %d (%s)", iapi.status.Height(), err)
			errStr := html.EscapeString(err.Error())
			writeInsightError(w, fmt.Sprintf("Error getting block hash %d (%q)", iapi.status.Height(), errStr))
			return
		}

		info := struct {
			SyncTipHash   string `json:"syncTipHash"`
			LastBlockHash string `json:"lastblockhash"`
		}{
			blockhashtip.String(),
			lastblockhash.String(),
		}
		writeJSON(w, info, m.GetIndentCtx(r))
	default:
		info := struct {
			Version         int32   `json:"version"`
			Protocolversion int32   `json:"protocolversion"`
			Blocks          int64   `json:"blocks"`
			NodeTimeoffset  int64   `json:"timeoffset"`
			NodeConnections int32   `json:"connections"`
			Proxy           string  `json:"proxy"`
			Difficulty      float64 `json:"difficulty"`
			Testnet         bool    `json:"testnet"`
			Relayfee        float64 `json:"relayfee"`
			Errors          string  `json:"errors"`
		}{
			infoResult.Version,
			infoResult.ProtocolVersion,
			infoResult.Blocks,
			infoResult.TimeOffset,
			infoResult.Connections,
			infoResult.Proxy,
			infoResult.Difficulty,
			infoResult.TestNet,
			infoResult.RelayFee,
			infoResult.Errors,
		}

		writeJSON(w, info, m.GetIndentCtx(r))
	}
}

func dateFromStr(format, dateStr string) (date time.Time, isToday bool, err error) {
	// Start of today (UTC)
	todayAM := time.Now().UTC().Truncate(24 * time.Hour)

	// If "dateStr" is empty, use today, otherwise try to parse the input date
	// string.
	if dateStr == "" {
		date = todayAM
	} else {
		date, err = time.Parse(format, dateStr)
		if err != nil {
			return
		}
		date = date.UTC()
	}

	isToday = date.Truncate(24*time.Hour) == todayAM

	return
}

func (iapi *InsightApi) getBlockSummaryByTime(w http.ResponseWriter, r *http.Request) {
	// Format of the blockDate URL param, and of the pagination parameters
	blockDateStr := m.GetBlockDateCtx(r)
	ymdFormat := "2006-01-02"
	blockDate, isToday, err := dateFromStr(ymdFormat, blockDateStr)
	if err != nil {
		writeInsightError(w,
			html.EscapeString(fmt.Sprintf("Unable to retrieve block summary using time %s: %v",
				blockDateStr, err)))
		return
	}

	minDate := blockDate
	maxDate := blockDate.Add(24*time.Hour - time.Second)

	var summaryOutput apitypes.InsightBlocksSummaryResult
	summaryOutput.Pagination.Next = minDate.AddDate(0, 0, 1).Format(ymdFormat)
	summaryOutput.Pagination.Prev = minDate.AddDate(0, 0, -1).Format(ymdFormat)
	summaryOutput.Pagination.Current = blockDate.Format(ymdFormat)
	summaryOutput.Pagination.IsToday = isToday

	// TODO: limit the query rather than returning all and limiting in go.
	minTime, maxTime := minDate.Unix(), maxDate.Unix()
	blockSummary, err := iapi.BlockData.BlockSummaryTimeRange(minTime, maxTime, 0)
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("BlockSummaryTimeRange: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		errStr := html.EscapeString(err.Error())
		writeInsightError(w, fmt.Sprintf("Unable to retrieve block summaries: %q", errStr))
		return
	}

	// Generate the pagination parameters More and MoreTs, and limit the result.
	limit := GetLimitCtx(r)
	if limit > 0 {
		var outputBlockSummary []dbtypes.BlockDataBasic
		for i, block := range blockSummary {
			if i >= limit {
				summaryOutput.Pagination.More = true
				break
			}
			outputBlockSummary = append(outputBlockSummary, block)
			blockTime := block.Time.UNIX()
			if blockTime < summaryOutput.Pagination.MoreTs {
				summaryOutput.Pagination.MoreTs = blockTime
			}
		}
		summaryOutput.Blocks = outputBlockSummary
	} else {
		summaryOutput.Blocks = blockSummary
		summaryOutput.Pagination.More = false
		summaryOutput.Pagination.MoreTs = minTime
	}

	summaryOutput.Pagination.CurrentTs = maxTime
	summaryOutput.Length = len(summaryOutput.Blocks)

	writeJSON(w, summaryOutput, m.GetIndentCtx(r))
}

func (iapi *InsightApi) getAddressInfo(w http.ResponseWriter, r *http.Request) {
	addresses, err := m.GetAddressCtx(r, iapi.params)
	if err != nil {
		errStr := html.EscapeString(err.Error())
		writeInsightError(w, errStr)
		return
	}
	if len(addresses) != 1 {
		writeInsightError(w, fmt.Sprintln("only one address allowed"))
		return
	}
	address := addresses[0]

	// Get confirmed balance.
	balance, _, err := iapi.BlockData.AddressBalance(address)
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("AddressSpentUnspent: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		apiLog.Errorf("AddressSpentUnspent: %v", err)
		http.Error(w, "Unexpected error retrieving address info.", http.StatusInternalServerError)
		return
	}

	command, isCmd := GetAddressCommandCtx(r)
	if isCmd {
		switch command {
		case "balance":
			writeJSON(w, balance.TotalUnspent, m.GetIndentCtx(r))
			return
		case "totalReceived":
			writeJSON(w, balance.TotalSpent+balance.TotalUnspent, m.GetIndentCtx(r))
			return
		case "totalSent":
			writeJSON(w, balance.TotalSpent, m.GetIndentCtx(r))
			return
		}
	}

	// Get confirmed transactions.
	rawTxs, recentTxs, err :=
		iapi.BlockData.InsightAddressTransactions(addresses, int64(iapi.status.Height()-2))
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("InsightAddressTransactions: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		apiLog.Errorf("Error retrieving transactions for addresses %s: %v",
			addresses, err)
		http.Error(w, "Error retrieving transactions for that addresses.",
			http.StatusInternalServerError)
		return
	}
	confirmedTxCount := len(rawTxs)

	// Get unconfirmed transactions.
	var unconfirmedBalanceSat int64
	var unconfirmedTxs []chainhash.Hash
	addressOuts, _, err := iapi.mp.UnconfirmedTxnsForAddress(address)
	if err != nil {
		apiLog.Errorf("Error in getting unconfirmed transactions")
	}
	if addressOuts != nil {
	FUNDING_TX_DUPLICATE_CHECK:
		for _, f := range addressOuts.Outpoints {
			// Confirm it's not already in our recent transactions.
			for _, v := range recentTxs {
				if v.IsEqual(&f.Hash) {
					continue FUNDING_TX_DUPLICATE_CHECK
				}
			}
			fundingTx, ok := addressOuts.TxnsStore[f.Hash]
			if !ok {
				apiLog.Errorf("An outpoint's transaction is not available in TxnStore.")
				continue
			}
			if fundingTx.Confirmed() {
				apiLog.Errorf("An outpoint's transaction is unexpectedly confirmed.")
				continue
			}
			unconfirmedBalanceSat += fundingTx.Tx.TxOut[f.Index].Value
			unconfirmedTxs = append(unconfirmedTxs, f.Hash) // Funding tx
			recentTxs = append(recentTxs, f.Hash)
		}
	SPENDING_TX_DUPLICATE_CHECK:
		for _, f := range addressOuts.PrevOuts {
			for _, v := range recentTxs {
				if v.IsEqual(&f.TxSpending) {
					continue SPENDING_TX_DUPLICATE_CHECK
				}
			}
			spendingTx, ok := addressOuts.TxnsStore[f.TxSpending]
			if !ok {
				apiLog.Errorf("An outpoint's transaction is not available in TxnStore.")
				continue
			}
			if spendingTx.Confirmed() {
				apiLog.Errorf("A transaction spending the outpoint of an unconfirmed transaction is unexpectedly confirmed.")
				continue
			}

			// Sent total atoms must be a lookup of the vout:i prevout value
			// because vin:i valuein is not reliable from dcrd at present.
			// TODO(chappjc): dcrd should be OK now. Recheck this.
			prevhash := spendingTx.Tx.TxIn[f.InputIndex].PreviousOutPoint.Hash
			previndex := spendingTx.Tx.TxIn[f.InputIndex].PreviousOutPoint.Index
			valuein := addressOuts.TxnsStore[prevhash].Tx.TxOut[previndex].Value
			unconfirmedBalanceSat -= valuein
			unconfirmedTxs = append(unconfirmedTxs, f.TxSpending) // Spending tx
			recentTxs = append(recentTxs, f.TxSpending)
		}
	}

	if isCmd && command == "unconfirmedBalance" {
		writeJSON(w, unconfirmedBalanceSat, m.GetIndentCtx(r))
		return
	}

	// Merge unconfirmed with confirmed transactions.
	rawTxs = append(unconfirmedTxs, rawTxs...)

	// Final raw tx slice extraction
	if txCount := int64(len(rawTxs)); txCount > 0 {
		txLimit := int64(1000)
		// "from" and "to" are zero-based indexes for inclusive range bounds.
		from := GetFromCtx(r)
		to, ok := GetToCtx(r)
		if !ok || to < from {
			to = from + txLimit - 1 // to is inclusive
		}

		// [from, to] --(limits)--> [start,end)
		start, end, err := fromToForSlice(from, to, txCount, txLimit)
		if err != nil {
			errStr := html.EscapeString(err.Error())
			writeInsightError(w, errStr)
			return
		}

		rawTxs = rawTxs[start:end]
	}

	addressInfo := apitypes.InsightAddressInfo{
		Address:                  address,
		TotalReceivedSat:         (balance.TotalSpent + balance.TotalUnspent),
		TotalSentSat:             balance.TotalSpent,
		BalanceSat:               balance.TotalUnspent,
		TotalReceived:            dcrutil.Amount(balance.TotalSpent + balance.TotalUnspent).ToCoin(),
		TotalSent:                dcrutil.Amount(balance.TotalSpent).ToCoin(),
		Balance:                  dcrutil.Amount(balance.TotalUnspent).ToCoin(),
		TxAppearances:            int64(confirmedTxCount),
		UnconfirmedBalance:       dcrutil.Amount(unconfirmedBalanceSat).ToCoin(),
		UnconfirmedBalanceSat:    unconfirmedBalanceSat,
		UnconfirmedTxAppearances: int64(len(unconfirmedTxs)),
	}

	noTxList := GetNoTxListCtx(r)
	if noTxList == 0 && len(rawTxs) > 0 {
		addressInfo.TransactionsID = make([]string, 0, len(rawTxs))
		for _, tx := range rawTxs {
			addressInfo.TransactionsID = append(addressInfo.TransactionsID,
				tx.String())
		}
	}

	writeJSON(w, addressInfo, m.GetIndentCtx(r))
}

// fromToForSlice takes ?from=A&to=B from the URL queries where A is the "from"
// index and B is the "to" index, which together define a range [A, B] for
// indexing a slice as slice[start,end]. This function computes valid start and
// end value given the provided from, to, the length of the slice, and an upper
// limit on the number of elements.
func fromToForSlice(from, to, sliceLength, txLimit int64) (int64, int64, error) {
	if sliceLength < 1 {
		return 0, 0, fmt.Errorf("no valid to/from for empty slice")
	}

	// Convert to exclusive range end semantics for slice end indexing (end
	// index + 1).
	start, end := from, to+1

	if start >= sliceLength {
		start = sliceLength - 1
	}
	if start < 0 {
		start = 0
	}
	if end <= start {
		end = start + 1
	}
	if end > sliceLength {
		end = sliceLength
	}

	if end-start > txLimit {
		return start, end, fmt.Errorf(`"from" (%d) and "to" (%d) range "+
			"must include at most %d transactions`, start, end-1, txLimit)
	}
	return start, end, nil
}

func (iapi *InsightApi) getEstimateFee(w http.ResponseWriter, r *http.Request) {
	nbBlocks := GetNbBlocksCtx(r)
	if nbBlocks == 0 {
		nbBlocks = 2
	}

	// A better solution would be a call to the DCRD RPC "estimatefee" endpoint
	// but that does not appear to be exposed currently.
	infoResult, err := iapi.nodeClient.GetInfo(r.Context())
	if err != nil {
		apiLog.Error("Error getting status")
		errStr := html.EscapeString(err.Error())
		writeInsightError(w, fmt.Sprintf("Error getting status (%s)", errStr))
		return
	}

	estimateFee := map[string]float64{
		strconv.Itoa(nbBlocks): infoResult.RelayFee,
	}

	writeJSON(w, estimateFee, m.GetIndentCtx(r))
}

// GetPeerStatus handles requests for node peer info (i.e. getpeerinfo RPC).
func (iapi *InsightApi) GetPeerStatus(w http.ResponseWriter, r *http.Request) {
	// Use a RPC call to tell if we are connected or not
	_, err := iapi.nodeClient.GetPeerInfo(r.Context())
	connected := err == nil

	var port *string
	peerInfo := struct {
		Connected bool    `json:"connected"`
		Host      string  `json:"host"`
		Port      *string `json:"port"`
	}{
		connected, "127.0.0.1", port,
	}

	writeJSON(w, peerInfo, m.GetIndentCtx(r))
}
