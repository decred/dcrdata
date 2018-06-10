// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package insight

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	apitypes "github.com/decred/dcrdata/api/types"
	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/decred/dcrdata/db/dcrpg"
	m "github.com/decred/dcrdata/middleware"
	"github.com/decred/dcrdata/semver"
	"github.com/decred/dcrdata/txhelpers"
)

// DataSourceLite specifies an interface for collecting data from the built-in
// databases (i.e. SQLite, storm, ffldb)
type DataSourceLite interface {
	UnconfirmedTxnsForAddress(address string) (*txhelpers.AddressOutpoints, int64, error)
}

type insightApiContext struct {
	nodeClient *rpcclient.Client
	BlockData  *dcrpg.ChainDBRPC
	params     *chaincfg.Params
	MemPool    DataSourceLite
	Status     apitypes.Status
	statusMtx  sync.RWMutex

	JSONIndent string
}

// NewInsightContext Constructor for insightApiContext
func NewInsightContext(client *rpcclient.Client, blockData *dcrpg.ChainDBRPC, params *chaincfg.Params, memPoolData DataSourceLite, JSONIndent string) *insightApiContext {
	conns, _ := client.GetConnectionCount()
	nodeHeight, _ := client.GetBlockCount()
	version := semver.NewSemver(1, 0, 0)

	newContext := insightApiContext{
		nodeClient: client,
		BlockData:  blockData,
		params:     params,
		MemPool:    memPoolData,
		Status: apitypes.Status{
			Height:          uint32(nodeHeight),
			NodeConnections: conns,
			APIVersion:      APIVersion,
			DcrdataVersion:  version.String(),
		},
	}
	return &newContext
}

func (c *insightApiContext) getIndentQuery(r *http.Request) (indent string) {
	useIndentation := r.URL.Query().Get("indent")
	if useIndentation == "1" || useIndentation == "true" {
		indent = c.JSONIndent
	}
	return
}

// Insight API successful response for JSON return items.
func writeJSON(w http.ResponseWriter, thing interface{}, indent string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", indent)
	if err := encoder.Encode(thing); err != nil {
		apiLog.Infof("JSON encode error: %v", err)
	}
}

func writeText(w http.ResponseWriter, str string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Insight API error response for a BAD REQUEST.  This means the request was
// malformed in some way or the request HASH, ADDRESS, BLOCK was not valid.
func writeInsightError(w http.ResponseWriter, str string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	io.WriteString(w, str)
}

// Insight API response for an item NOT FOUND.  This means the request was valid
// but no records were found for the item in question.  For some endpoints
// responding with an empty array [] is expected such as a transaction query for
// addresses with no transactions.
func writeInsightNotFound(w http.ResponseWriter, str string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	io.WriteString(w, str)
}

func (c *insightApiContext) getTransaction(w http.ResponseWriter, r *http.Request) {
	txid := m.GetTxIDCtx(r)
	if txid == "" {
		apiLog.Errorf("Txid cannot be empty")
		writeInsightError(w, fmt.Sprintf("Txid cannot be empty"))
		return
	}

	// Return raw transaction
	txOld, err := c.BlockData.GetRawTransaction(txid)
	if err != nil {
		apiLog.Errorf("Unable to get transaction %s", txid)
		writeInsightNotFound(w, fmt.Sprintf("Unable to get transaction (%s)", txid))
		return
	}

	txsOld := []*dcrjson.TxRawResult{txOld}

	// convert to insight struct
	txsNew, err := c.TxConverter(txsOld)

	if err != nil {
		apiLog.Errorf("Error Processing Transactions")
		writeInsightError(w, fmt.Sprintf("Error Processing Transactions"))
		return
	}

	writeJSON(w, txsNew[0], c.getIndentQuery(r))
}

func (c *insightApiContext) getTransactionHex(w http.ResponseWriter, r *http.Request) {
	txid := m.GetTxIDCtx(r)
	if txid == "" {
		writeInsightError(w, "TxId must not be empty")
		return
	}

	txHex := c.BlockData.GetTransactionHex(txid)

	if txHex == "" {
		writeInsightNotFound(w, fmt.Sprintf("Unable to get transaction (%s)", txHex))
		return
	}

	hexOutput := new(apitypes.InsightRawTx)
	hexOutput.Rawtx = txHex

	writeJSON(w, hexOutput, c.getIndentQuery(r))
}

func (c *insightApiContext) getBlockSummary(w http.ResponseWriter, r *http.Request) {
	// attempt to get hash of block set by hash or (fallback) height set on path
	hash, ok := c.GetInsightBlockHashCtx(r)
	if !ok {
		idx, ok := c.GetInsightBlockIndexCtx(r)
		if !ok {
			writeInsightError(w, "Must provide an index or block hash")
			return
		}
		var err error
		hash, err = c.BlockData.ChainDB.GetBlockHash(int64(idx))
		if err != nil {
			writeInsightError(w, "Unable to get block hash from index")
			return
		}
	}
	blockDcrd := c.BlockData.GetBlockVerboseByHash(hash, false)
	if blockDcrd == nil {
		writeInsightNotFound(w, "Unable to get block")
		return
	}

	blockSummary := []*dcrjson.GetBlockVerboseResult{blockDcrd}
	blockInsight, err := c.BlockConverter(blockSummary)
	if err != nil {
		apiLog.Errorf("Unable to process block (%s)", hash)
		writeInsightError(w, "Unable to Process Block")
		return
	}

	writeJSON(w, blockInsight, c.getIndentQuery(r))
}

func (c *insightApiContext) getBlockHash(w http.ResponseWriter, r *http.Request) {
	idx, ok := c.GetInsightBlockIndexCtx(r)
	if !ok {
		writeInsightError(w, "No index found in query")
		return
	}
	if idx < 0 || idx > c.BlockData.ChainDB.GetHeight() {
		writeInsightError(w, "Block height out of range")
		return
	}
	hash, err := c.BlockData.ChainDB.GetBlockHash(int64(idx))
	if err != nil || hash == "" {
		writeInsightNotFound(w, "Not found")
		return
	}

	blockOutput := struct {
		BlockHash string `json:"blockHash"`
	}{
		hash,
	}
	writeJSON(w, blockOutput, c.getIndentQuery(r))
}

func (c *insightApiContext) getBlockChainHashCtx(r *http.Request) *chainhash.Hash {
	hash, err := chainhash.NewHashFromStr(c.getBlockHashCtx(r))
	if err != nil {
		apiLog.Errorf("Failed to parse block hash: %v", err)
		return nil
	}
	return hash
}

func (c *insightApiContext) getRawBlock(w http.ResponseWriter, r *http.Request) {

	hash, ok := c.GetInsightBlockHashCtx(r)
	if !ok {
		idx, ok := c.GetInsightBlockIndexCtx(r)
		if !ok {
			writeInsightError(w, "Must provide an index or block hash")
			return
		}
		var err error
		hash, err = c.BlockData.ChainDB.GetBlockHash(int64(idx))
		if err != nil {
			writeInsightError(w, "Unable to get block hash from index")
			return
		}
	}
	chainHash, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		writeInsightError(w, fmt.Sprintf("Failed to parse block hash: %v", err))
		return
	}

	blockMsg, err := c.nodeClient.GetBlock(chainHash)
	if err != nil {
		writeInsightNotFound(w, fmt.Sprintf("Failed to retrieve block %s: %v", chainHash.String(), err))
		return
	}
	var blockHex bytes.Buffer
	if err = blockMsg.Serialize(&blockHex); err != nil {
		apiLog.Errorf("Failed to serialize block: %v", err)
		writeInsightError(w, fmt.Sprintf("Failed to serialize block"))
		return
	}

	blockJSON := struct {
		BlockHash string `json:"rawblock"`
	}{
		hex.EncodeToString(blockHex.Bytes()),
	}
	writeJSON(w, blockJSON, c.getIndentQuery(r))
}

func (c *insightApiContext) broadcastTransactionRaw(w http.ResponseWriter, r *http.Request) {
	// Check for rawtx
	rawHexTx, ok := c.GetRawHexTx(r)
	if !ok {
		// JSON extraction failed or rawtx blank.  Error message already returned.
		return
	}

	// Check maximum transaction size
	if len(rawHexTx)/2 > c.params.MaxTxSize {
		writeInsightError(w, fmt.Sprintf("Rawtx length exceeds maximum allowable characters (%d bytes received)", len(rawHexTx)/2))
		return
	}

	// Broadcast
	txid, err := c.BlockData.SendRawTransaction(rawHexTx)
	if err != nil {
		apiLog.Errorf("Unable to send transaction %s", rawHexTx)
		writeInsightError(w, fmt.Sprintf("SendRawTransaction failed: %v", err))
		return
	}

	// Respond with hash of broadcasted transaction
	txidJSON := struct {
		TxidHash string `json:"rawtx"`
	}{
		txid,
	}
	writeJSON(w, txidJSON, c.getIndentQuery(r))
}

func (c *insightApiContext) getAddressesTxnOutput(w http.ResponseWriter, r *http.Request) {
	address := m.GetAddressCtx(r) // Required
	if address == "" {
		writeInsightError(w, "Address cannot be empty")
		return
	}

	// Allow Addresses to be single or multiple separated by a comma.
	addresses := strings.Split(address, ",")

	// Initialize Output Structure
	txnOutputs := make([]apitypes.AddressTxnOutput, 0)

	for _, address := range addresses {

		confirmedTxnOutputs := c.BlockData.ChainDB.GetAddressUTXO(address)

		addressOuts, _, err := c.MemPool.UnconfirmedTxnsForAddress(address)
		if err != nil {
			apiLog.Errorf("Error in getting unconfirmed transactions")
		}

		if addressOuts != nil {
			// If there is any mempool add to the utxo set
		FUNDING_TX_DUPLICATE_CHECK:
			for _, f := range addressOuts.Outpoints {
				fundingTx, ok := addressOuts.TxnsStore[f.Hash]
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
					if utxo.Vout == f.Index && utxo.TxnID == f.Hash.String() {
						continue FUNDING_TX_DUPLICATE_CHECK
					}
				}

				txnOutput := apitypes.AddressTxnOutput{
					Address:       address,
					TxnID:         fundingTx.Hash().String(),
					Vout:          f.Index,
					ScriptPubKey:  hex.EncodeToString(fundingTx.Tx.TxOut[f.Index].PkScript),
					Amount:        dcrutil.Amount(fundingTx.Tx.TxOut[f.Index].Value).ToCoin(),
					Satoshis:      fundingTx.Tx.TxOut[f.Index].Value,
					Confirmations: 0,
					BlockTime:     fundingTx.MemPoolTime,
				}
				txnOutputs = append(txnOutputs, txnOutput)
			}
		}
		txnOutputs = append(txnOutputs, confirmedTxnOutputs...)

		// Search for items in mempool that spend utxo (matching hash and index)
		// and remove those from the set
		for _, f := range addressOuts.PrevOuts {
			spendingTx, ok := addressOuts.TxnsStore[f.TxSpending]
			if !ok {
				apiLog.Errorf("An outpoint's transaction is not available in TxnStore.")
				continue
			}
			if spendingTx.Confirmed() {
				apiLog.Errorf("A transaction spending the outpoint of an unconfirmed transaction is unexpectedly confirmed.")
				continue
			}
			for g, utxo := range txnOutputs {
				if utxo.Vout == f.PreviousOutpoint.Index && utxo.TxnID == f.PreviousOutpoint.Hash.String() {
					// Found a utxo that is unconfirmed spent.  Remove from slice
					txnOutputs = append(txnOutputs[:g], txnOutputs[g+1:]...)
				}
			}
		}
	}
	// Final sort by timestamp desc if unconfirmed and by confirmations
	// ascending if confirmed
	sort.Slice(txnOutputs, func(i, j int) bool {
		if txnOutputs[i].Confirmations == 0 && txnOutputs[j].Confirmations == 0 {
			return txnOutputs[i].BlockTime > txnOutputs[j].BlockTime
		}
		return txnOutputs[i].Confirmations < txnOutputs[j].Confirmations
	})

	writeJSON(w, txnOutputs, c.getIndentQuery(r))
}

func (c *insightApiContext) getTransactions(w http.ResponseWriter, r *http.Request) {
	hash := m.GetBlockHashCtx(r)
	address := m.GetAddressCtx(r)
	if hash == "" && address == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	if hash != "" {
		blockTransactions := c.BlockData.GetTransactionsForBlockByHash(hash)
		if blockTransactions == nil {
			apiLog.Errorf("Unable to get block %s transactions", hash)
			http.Error(w, http.StatusText(422), 422)
			return
		}

		writeJSON(w, blockTransactions, c.getIndentQuery(r))
	}

	if address != "" {
		address := m.GetAddressCtx(r)
		if address == "" {
			http.Error(w, http.StatusText(422), 422)
			return
		}
		txs := c.BlockData.InsightGetAddressTransactions(address, 20, 0)
		if txs == nil {
			http.Error(w, http.StatusText(422), 422)
			return
		}

		txsOutput := struct {
			Txs []*dcrjson.SearchRawTransactionsResult `json:"txs"`
		}{
			txs,
		}
		writeJSON(w, txsOutput, c.getIndentQuery(r))
	}
}

func (c *insightApiContext) getAddressesTxn(w http.ResponseWriter, r *http.Request) {
	address := m.GetAddressCtx(r) // Required
	if address == "" {
		writeInsightError(w, "Address cannot be empty")
		return
	}

	noAsm := c.GetNoAsmCtx(r)             // Optional
	noScriptSig := c.GetNoScriptSigCtx(r) // Optional
	noSpent := c.GetNoSpentCtx(r)         // Optional
	from := c.GetFromCtx(r)               // Optional
	to, ok := c.GetToCtx(r)               // Optional
	if !ok {
		to = from + 10
	}

	// Allow Addresses to be single or multiple separated by a comma.
	addresses := strings.Split(address, ",")

	// Initialize Output Structure
	addressOutput := new(apitypes.InsightMultiAddrsTxOutput)
	UnconfirmedTxs := []string{}

	rawTxs, recentTxs := c.BlockData.ChainDB.InsightPgGetAddressTransactions(addresses, int64(c.Status.Height-2))

	// Confirm all addresses are valid and pull unconfirmed transactions for all addresses
	for _, addr := range addresses {
		address, err := dcrutil.DecodeAddress(addr)
		if err != nil {
			writeInsightError(w, fmt.Sprintf("Address is invalid (%s)", addr))
			return
		}
		addressOuts, _, err := c.MemPool.UnconfirmedTxnsForAddress(address.String())
		if err != nil {
			writeInsightError(w, fmt.Sprintf("Error gathering mempool transactions (%s)", err))
			return
		}

	FUNDING_TX_DUPLICATE_CHECK:
		for _, f := range addressOuts.Outpoints {
			// Confirm its not already in our recent transactions
			for _, v := range recentTxs {
				if v == f.Hash.String() {
					continue FUNDING_TX_DUPLICATE_CHECK
				}
			}
			UnconfirmedTxs = append(UnconfirmedTxs, f.Hash.String()) // Funding tx
			recentTxs = append(recentTxs, f.Hash.String())
		}
	SPENDING_TX_DUPLICATE_CHECK:
		for _, f := range addressOuts.PrevOuts {
			for _, v := range recentTxs {
				if v == f.TxSpending.String() {
					continue SPENDING_TX_DUPLICATE_CHECK
				}
			}
			UnconfirmedTxs = append(UnconfirmedTxs, f.TxSpending.String()) // Spending tx
			recentTxs = append(recentTxs, f.TxSpending.String())
		}
	}

	// Merge unconfirmed with confirmed transactions
	rawTxs = append(UnconfirmedTxs, rawTxs...)

	txcount := len(rawTxs)
	addressOutput.TotalItems = int64(txcount)

	if txcount > 0 {
		if int(from) > txcount {
			from = int64(txcount)
		}
		if int(from) < 0 {
			from = 0
		}
		if int(to) > txcount {
			to = int64(txcount)
		}
		if int(to) < 0 {
			to = 0
		}
		if from > to {
			to = from
		}
		if (to - from) > 50 {
			writeInsightError(w, fmt.Sprintf("\"from\" (%d) and \"to\" (%d) range should be less than or equal to 50", from, to))
			return
		}
		// Final Slice Extraction
		rawTxs = rawTxs[from:to]
	}
	addressOutput.From = int(from)
	addressOutput.To = int(to)

	txsOld := []*dcrjson.TxRawResult{}
	for _, rawTx := range rawTxs {
		txOld, err := c.BlockData.GetRawTransaction(rawTx)
		if err != nil {
			apiLog.Errorf("Unable to get transaction %s", rawTx)
			writeInsightError(w, fmt.Sprintf("Error gathering transaction details (%s)", err))
			return
		}
		txsOld = append(txsOld, txOld)
	}

	// Convert to Insight API struct
	txsNew, err := c.TxConverterWithParams(txsOld, noAsm, noScriptSig, noSpent)
	if err != nil {
		apiLog.Error("Unable to process transactions")
		writeInsightError(w, fmt.Sprintf("Unable to convert transactions (%s)", err))
		return
	}
	addressOutput.Items = append(addressOutput.Items, txsNew...)
	if addressOutput.Items == nil {
		// Make sure we pass an empty array not null to json response if no Tx
		addressOutput.Items = make([]apitypes.InsightTx, 0)
	}
	writeJSON(w, addressOutput, c.getIndentQuery(r))
}

func (c *insightApiContext) getAddressBalance(w http.ResponseWriter, r *http.Request) {
	address := m.GetAddressCtx(r)
	if address == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	addressInfo := c.BlockData.ChainDB.GetAddressBalance(address, 20, 0)
	if addressInfo == nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	writeJSON(w, addressInfo.TotalUnspent, c.getIndentQuery(r))
}

func (c *insightApiContext) getAddressTotalReceived(w http.ResponseWriter, r *http.Request) {
	address := m.GetAddressCtx(r)
	if address == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	addressInfo := c.BlockData.ChainDB.GetAddressBalance(address, 20, 0)
	if addressInfo == nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	totalReceived := addressInfo.TotalSpent + addressInfo.TotalUnspent

	writeText(w, strconv.Itoa(int(totalReceived)))
}

func (c *insightApiContext) getAddressUnconfirmedBalance(w http.ResponseWriter, r *http.Request) {
	address := m.GetAddressCtx(r)
	if address == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	addressInfo := c.BlockData.ChainDB.GetAddressBalance(address, 20, 0)
	if addressInfo == nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	writeText(w, string(addressInfo.TotalUnspent))
}

func (c *insightApiContext) getAddressTotalSent(w http.ResponseWriter, r *http.Request) {
	address := m.GetAddressCtx(r)
	if address == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	addressInfo := c.BlockData.ChainDB.GetAddressBalance(address, 20, 0)
	if addressInfo == nil {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	writeText(w, strconv.Itoa(int(addressInfo.TotalSpent)))
}

// TODO getDifficulty and getInfo
func (c *insightApiContext) getStatusInfo(w http.ResponseWriter, r *http.Request) {
	statusInfo := m.GetStatusInfoCtx(r)

	if statusInfo == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	if statusInfo == "getLastBlockHash" {
		hash := c.getBlockHashCtx(r)
		hashOutput := struct {
			LastBlockHash string `json:"lastblockhash"`
		}{
			hash,
		}
		writeJSON(w, hashOutput, c.getIndentQuery(r))
	}

	if statusInfo == "getBestBlockHash" {
		hash := c.getBlockHashCtx(r)
		hashOutput := struct {
			BestBlockHash string `json:"bestblockhash"`
		}{
			hash,
		}
		writeJSON(w, hashOutput, c.getIndentQuery(r))
	}

}

func (c *insightApiContext) getBlockSummaryByTime(w http.ResponseWriter, r *http.Request) {
	blockDate := m.GetBlockDateCtx(r)
	limit := c.GetLimitCtx(r)

	summaryOutput := apitypes.InsightBlocksSummaryResult{}
	layout := "2006-01-02 15:04:05"
	blockDateToday := time.Now().UTC().Format("2006-01-02")

	if blockDate == "" {
		blockDate = blockDateToday
	}

	if blockDateToday == blockDate {
		summaryOutput.Pagination.IsToday = true
	}
	minDate, err := time.Parse(layout, blockDate+" 00:00:00")
	if err != nil {
		writeInsightError(w, fmt.Sprintf("Unable to retrieve block summary using time %s: %v", blockDate, err))
		return
	}

	maxDate, err := time.Parse(layout, blockDate+" 23:59:59")
	if err != nil {
		writeInsightError(w, fmt.Sprintf("Unable to retrieve block summary using time %s: %v", blockDate, err))
		return
	}
	summaryOutput.Pagination.Next = minDate.AddDate(0, 0, 1).Format("2006-01-02")
	summaryOutput.Pagination.Prev = minDate.AddDate(0, 0, -1).Format("2006-01-02")

	summaryOutput.Pagination.Current = blockDate

	minTime, maxTime := minDate.Unix(), maxDate.Unix()
	summaryOutput.Pagination.CurrentTs = maxTime
	summaryOutput.Pagination.MoreTs = maxTime

	blockSummary := c.BlockData.ChainDB.GetBlockSummaryTimeRange(minTime, maxTime, 0)

	outputBlockSummary := []dbtypes.BlockDataBasic{}

	// Generate the pagenation parameters more and moreTs and limit the result
	if limit > 0 {
		for i, block := range blockSummary {
			if i >= limit {
				summaryOutput.Pagination.More = true
				break
			}
			outputBlockSummary = append(outputBlockSummary, block)
			if block.Time < summaryOutput.Pagination.MoreTs {
				summaryOutput.Pagination.MoreTs = block.Time
			}
		}
		summaryOutput.Blocks = outputBlockSummary
	} else {
		summaryOutput.Blocks = blockSummary
		summaryOutput.Pagination.More = false
		summaryOutput.Pagination.MoreTs = minTime
	}

	summaryOutput.Length = len(summaryOutput.Blocks)

	writeJSON(w, summaryOutput, c.getIndentQuery(r))

}

func (c *insightApiContext) getAddressInfo(w http.ResponseWriter, r *http.Request) {
	address := m.GetAddressCtx(r)
	command, isCmd := c.GetAddressCommandCtx(r)

	_, err := dcrutil.DecodeAddress(address)
	if err != nil {
		writeInsightError(w, "Invalid Address")
		return
	}

	noTxList := c.GetNoTxListCtx(r)

	from := c.GetFromCtx(r)
	to, ok := c.GetToCtx(r)
	if !ok || to <= from {
		to = from + 1000
	}

	// Get Confirmed Balances
	var unconfirmedBalanceSat int64
	_, _, totalSpent, totalUnspent, err := c.BlockData.ChainDB.RetrieveAddressSpentUnspent(address)
	if err != nil {
		return
	}

	if isCmd {
		switch command {
		case "balance":
			writeJSON(w, totalUnspent, c.getIndentQuery(r))
			return
		case "totalReceived":
			writeJSON(w, totalSpent+totalUnspent, c.getIndentQuery(r))
			return
		case "totalSent":
			writeJSON(w, totalSpent, c.getIndentQuery(r))
			return
		}
	}

	addresses := []string{address}

	// Get Confirmed Transactions
	rawTxs, recentTxs := c.BlockData.ChainDB.InsightPgGetAddressTransactions(addresses, int64(c.Status.Height-2))
	confirmedTxCount := len(rawTxs)

	// Get Unconfirmed Transactions
	unconfirmedTxs := []string{}
	addressOuts, _, err := c.MemPool.UnconfirmedTxnsForAddress(address)
	if err != nil {
		apiLog.Errorf("Error in getting unconfirmed transactions")
	}
	if addressOuts != nil {
	FUNDING_TX_DUPLICATE_CHECK:
		for _, f := range addressOuts.Outpoints {
			// Confirm its not already in our recent transactions
			for _, v := range recentTxs {
				if v == f.Hash.String() {
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
			unconfirmedTxs = append(unconfirmedTxs, f.Hash.String()) // Funding tx
			recentTxs = append(recentTxs, f.Hash.String())
		}
	SPENDING_TX_DUPLICATE_CHECK:
		for _, f := range addressOuts.PrevOuts {
			for _, v := range recentTxs {
				if v == f.TxSpending.String() {
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

			// Sent total sats has to be a lookup of the vout:i prevout value
			// because vin:i valuein is not reliable from dcrd at present
			prevhash := spendingTx.Tx.TxIn[f.InputIndex].PreviousOutPoint.Hash
			previndex := spendingTx.Tx.TxIn[f.InputIndex].PreviousOutPoint.Index
			valuein := addressOuts.TxnsStore[prevhash].Tx.TxOut[previndex].Value
			unconfirmedBalanceSat -= valuein
			unconfirmedTxs = append(unconfirmedTxs, f.TxSpending.String()) // Spending tx
			recentTxs = append(recentTxs, f.TxSpending.String())
		}
	}

	if isCmd {
		switch command {
		case "unconfirmedBalance":
			writeJSON(w, unconfirmedBalanceSat, c.getIndentQuery(r))
			return
		}
	}

	// Merge Unconfirmed with Confirmed transactions
	rawTxs = append(unconfirmedTxs, rawTxs...)

	// Final Slice Extraction
	txcount := len(rawTxs)
	if txcount > 0 {
		if int(from) > txcount {
			from = int64(txcount)
		}
		if int(from) < 0 {
			from = 0
		}
		if int(to) > txcount {
			to = int64(txcount)
		}
		if int(to) < 0 {
			to = 0
		}
		if from > to {
			to = from
		}
		if (to - from) > 1000 {
			writeInsightError(w, fmt.Sprintf("\"from\" (%d) and \"to\" (%d) range should be less than or equal to 1000", from, to))
			return
		}

		rawTxs = rawTxs[from:to]
	}

	addressInfo := apitypes.InsightAddressInfo{
		Address:                  address,
		TotalReceivedSat:         (totalSpent + totalUnspent),
		TotalSentSat:             totalSpent,
		BalanceSat:               totalUnspent,
		TotalReceived:            dcrutil.Amount(totalSpent + totalUnspent).ToCoin(),
		TotalSent:                dcrutil.Amount(totalSpent).ToCoin(),
		Balance:                  dcrutil.Amount(totalUnspent).ToCoin(),
		TxAppearances:            int64(confirmedTxCount),
		UnconfirmedBalance:       dcrutil.Amount(unconfirmedBalanceSat).ToCoin(),
		UnconfirmedBalanceSat:    unconfirmedBalanceSat,
		UnconfirmedTxAppearances: int64(len(unconfirmedTxs)),
	}

	if noTxList == 0 {
		addressInfo.TransactionsID = rawTxs
	}

	writeJSON(w, addressInfo, c.getIndentQuery(r))
}
