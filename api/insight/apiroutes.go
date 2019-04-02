// Copyright (c) 2018-2019, The Decred developers
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
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson/v2"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient/v2"
	apitypes "github.com/decred/dcrdata/api/types"
	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/decred/dcrdata/db/dcrpg"
	m "github.com/decred/dcrdata/middleware"
	"github.com/decred/dcrdata/rpcutils"
)

const defaultReqPerSecLimit = 20.0

// InsightApi contains the resources for the Insight HTTP API. InsightApi's
// methods include the http.Handlers for the URL path routes.
type InsightApi struct {
	nodeClient     *rpcclient.Client
	BlockData      *dcrpg.ChainDBRPC
	params         *chaincfg.Params
	mp             rpcutils.MempoolAddressChecker
	status         *apitypes.Status
	JSONIndent     string
	ReqPerSecLimit float64
	maxCSVAddrs    int
}

// NewInsightApi is the constructor for InsightApi.
func NewInsightApi(client *rpcclient.Client, blockData *dcrpg.ChainDBRPC, params *chaincfg.Params,
	memPoolData rpcutils.MempoolAddressChecker, JSONIndent string, maxAddrs int, status *apitypes.Status) *InsightApi {

	newContext := InsightApi{
		nodeClient:     client,
		BlockData:      blockData,
		params:         params,
		mp:             memPoolData,
		status:         status,
		ReqPerSecLimit: defaultReqPerSecLimit,
		maxCSVAddrs:    maxAddrs,
	}
	return &newContext
}

// SetReqRateLimit is used to set the requests/second/IP for the Insight API's
// rate limiter.
func (iapi *InsightApi) SetReqRateLimit(reqPerSecLimit float64) {
	iapi.ReqPerSecLimit = reqPerSecLimit
}

func (iapi *InsightApi) getIndentQuery(r *http.Request) (indent string) {
	useIndentation := r.URL.Query().Get("indent")
	if useIndentation == "1" || useIndentation == "true" {
		indent = iapi.JSONIndent
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

func (iapi *InsightApi) getTransaction(w http.ResponseWriter, r *http.Request) {
	txid, err := m.GetTxIDCtx(r)
	if err != nil {
		writeInsightError(w, err.Error())
		return
	}

	// Return raw transaction
	txOld, err := iapi.BlockData.GetRawTransaction(txid)
	if err != nil {
		apiLog.Errorf("Unable to get transaction %s", txid)
		writeInsightNotFound(w, fmt.Sprintf("Unable to get transaction (%s)", txid))
		return
	}

	txsOld := []*dcrjson.TxRawResult{txOld}

	// convert to insight struct
	txsNew, err := iapi.TxConverter(txsOld)

	if err != nil {
		apiLog.Errorf("Error Processing Transactions")
		writeInsightError(w, fmt.Sprintf("Error Processing Transactions"))
		return
	}

	writeJSON(w, txsNew[0], iapi.getIndentQuery(r))
}

func (iapi *InsightApi) getTransactionHex(w http.ResponseWriter, r *http.Request) {
	txid, err := m.GetTxIDCtx(r)
	if err != nil {
		writeInsightError(w, err.Error())
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

	writeJSON(w, hexOutput, iapi.getIndentQuery(r))
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

		hash, err = iapi.BlockData.ChainDB.GetBlockHash(int64(idx))
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

	blockSummary := []*dcrjson.GetBlockVerboseResult{block}
	blockInsight, err := iapi.DcrToInsightBlock(blockSummary)
	if err != nil {
		apiLog.Errorf("Unable to process block (%s)", hash)
		writeInsightError(w, "Unable to process block")
		return
	}

	writeJSON(w, blockInsight, iapi.getIndentQuery(r))
}

func (iapi *InsightApi) getBlockHash(w http.ResponseWriter, r *http.Request) {
	idx := m.GetBlockIndexCtx(r)
	if idx < 0 {
		writeInsightError(w, "No index found in query")
		return
	}

	height := iapi.BlockData.ChainDB.Height()
	if idx < 0 || idx > int(height) {
		writeInsightError(w, "Block height out of range")
		return
	}
	hash, err := iapi.BlockData.ChainDB.GetBlockHash(int64(idx))
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
	writeJSON(w, blockOutput, iapi.getIndentQuery(r))
}

func (iapi *InsightApi) getRawBlock(w http.ResponseWriter, r *http.Request) {

	hash, err := m.GetBlockHashCtx(r)
	if err != nil {
		idx := m.GetBlockIndexCtx(r)
		if idx < 0 {
			writeInsightError(w, "Must provide an index or block hash")
			return
		}

		hash, err = iapi.BlockData.ChainDB.GetBlockHash(int64(idx))
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
		writeInsightError(w, fmt.Sprintf("Failed to parse block hash: %v", err))
		return
	}

	blockMsg, err := iapi.nodeClient.GetBlock(chainHash)
	if err != nil {
		writeInsightNotFound(w, fmt.Sprintf("Failed to retrieve block %s: %v",
			chainHash.String(), err))
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
	writeJSON(w, blockJSON, iapi.getIndentQuery(r))
}

func (iapi *InsightApi) broadcastTransactionRaw(w http.ResponseWriter, r *http.Request) {
	// Check for rawtx.
	rawHexTx, err := m.GetRawHexTx(r)
	if err != nil {
		// JSON extraction failed or rawtx blank.
		writeInsightError(w, err.Error())
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
		writeInsightError(w, fmt.Sprintf("SendRawTransaction failed: %v", err))
		return
	}

	// Respond with hash of broadcasted transaction.
	txidJSON := struct {
		TxidHash string `json:"txid"`
	}{
		txid,
	}
	writeJSON(w, txidJSON, iapi.getIndentQuery(r))
}

func (iapi *InsightApi) getAddressesTxnOutput(w http.ResponseWriter, r *http.Request) {
	addresses, err := m.GetAddressCtx(r, iapi.params, iapi.maxCSVAddrs) // Required
	if err != nil {
		writeInsightError(w, err.Error())
		return
	}

	// Initialize Output Structure
	txnOutputs := make([]apitypes.AddressTxnOutput, 0)

	for _, address := range addresses {
		confirmedTxnOutputs, _, err := iapi.BlockData.ChainDB.AddressUTXO(address)
		if dbtypes.IsTimeoutErr(err) {
			apiLog.Errorf("AddressUTXO: %v", err)
			http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
			return
		}
		if err != nil {
			apiLog.Errorf("Error getting UTXOs: %v", err)
			continue
		}

		addressOuts, _, err := iapi.mp.UnconfirmedTxnsForAddress(address)
		if err != nil {
			apiLog.Errorf("Error getting unconfirmed transactions: %v", err)
			continue
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

	writeJSON(w, txnOutputs, iapi.getIndentQuery(r))
}

func (iapi *InsightApi) getTransactions(w http.ResponseWriter, r *http.Request) {
	hash, blockerr := m.GetBlockHashCtx(r)
	addresses, addrerr := m.GetAddressCtx(r, iapi.params, 1)

	if blockerr != nil && addrerr != nil {
		writeInsightError(w, "Required query parameters (address or block) not present.")
		return
	}
	if addrerr == nil && len(addresses) > 1 {
		writeInsightError(w, "Only one address is allowed.")
		return
	}

	if blockerr == nil {
		blkTrans := iapi.BlockData.GetBlockVerboseByHash(hash, true)
		if blkTrans == nil {
			apiLog.Errorf("Unable to get block %s transactions", hash)
			writeInsightError(w, fmt.Sprintf("Unable to get block %s transactions", hash))
			return
		}

		txsOld := []*dcrjson.TxRawResult{}
		txcount := len(blkTrans.RawTx) + len(blkTrans.RawSTx)
		// Merge tx and stx together and limit result to 10 max
		count := 0
		for i := range blkTrans.RawTx {
			txsOld = append(txsOld, &blkTrans.RawTx[i])
			count++
			if count > 10 {
				break
			}
		}
		if count < 10 {
			for i := range blkTrans.RawSTx {
				txsOld = append(txsOld, &blkTrans.RawSTx[i])
				count++
				if count > 10 {
					break
				}
			}
		}

		// Convert to Insight struct
		txsNew, err := iapi.TxConverter(txsOld)
		if err != nil {
			apiLog.Error("Error Processing Transactions")
			writeInsightError(w, "Error Processing Transactions")
			return
		}

		blockTransactions := apitypes.InsightBlockAddrTxSummary{
			PagesTotal: int64(txcount),
			Txs:        txsNew,
		}
		writeJSON(w, blockTransactions, iapi.getIndentQuery(r))
		return
	}

	if addrerr == nil {
		address := addresses[0]
		// Validate Address
		_, err := dcrutil.DecodeAddress(address)
		if err != nil {
			writeInsightError(w, fmt.Sprintf("Address is invalid (%s)", address))
			return
		}
		addresses := []string{address}
		rawTxs, recentTxs, err :=
			iapi.BlockData.ChainDB.InsightAddressTransactions(addresses, int64(iapi.status.Height()-2))
		if dbtypes.IsTimeoutErr(err) {
			apiLog.Errorf("InsightAddressTransactions: %v", err)
			http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
			return
		}
		if err != nil {
			writeInsightError(w,
				fmt.Sprintf("Error retrieving transactions for addresss %s (%v)",
					addresses, err))
			return
		}

		addressOuts, _, err := iapi.mp.UnconfirmedTxnsForAddress(address)
		var UnconfirmedTxs []chainhash.Hash

		if err != nil {
			writeInsightError(w, fmt.Sprintf("Error gathering mempool transactions (%v)", err))
			return
		}

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
		}

		// Merge unconfirmed with confirmed transactions
		rawTxs = append(UnconfirmedTxs, rawTxs...)

		txcount := len(rawTxs)

		if txcount > 10 {
			rawTxs = rawTxs[0:10]
		}

		txsOld := []*dcrjson.TxRawResult{}
		for _, rawTx := range rawTxs {
			txOld, err1 := iapi.BlockData.GetRawTransaction(&rawTx)
			if err1 != nil {
				apiLog.Errorf("Unable to get transaction %s", rawTx)
				writeInsightError(w, fmt.Sprintf("Error gathering transaction details (%s)", err1))
				return
			}
			txsOld = append(txsOld, txOld)
		}

		// Convert to Insight struct
		txsNew, err := iapi.TxConverter(txsOld)
		if err != nil {
			apiLog.Error("Error Processing Transactions")
			writeInsightError(w, "Error Processing Transactions")
			return
		}

		addrTransactions := apitypes.InsightBlockAddrTxSummary{
			PagesTotal: int64(txcount),
			Txs:        txsNew,
		}
		writeJSON(w, addrTransactions, iapi.getIndentQuery(r))
	}
}

func (iapi *InsightApi) getAddressesTxn(w http.ResponseWriter, r *http.Request) {
	addresses, err := m.GetAddressCtx(r, iapi.params, iapi.maxCSVAddrs) // Required
	if err != nil {
		writeInsightError(w, err.Error())
		return
	}

	noAsm := GetNoAsmCtx(r)             // Optional
	noScriptSig := GetNoScriptSigCtx(r) // Optional
	noSpent := GetNoSpentCtx(r)         // Optional
	from := GetFromCtx(r)               // Optional
	to, ok := GetToCtx(r)               // Optional
	if !ok {
		to = from + 10
	}

	// Initialize Output Structure
	addressOutput := new(apitypes.InsightMultiAddrsTxOutput)
	var UnconfirmedTxs []chainhash.Hash

	rawTxs, recentTxs, err :=
		iapi.BlockData.ChainDB.InsightAddressTransactions(addresses, int64(iapi.status.Height()-2))
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("InsightAddressTransactions: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		writeInsightError(w,
			fmt.Sprintf("Error retrieving transactions for addresss %s (%s)",
				addresses, err))
		return
	}

	// Confirm all addresses are valid and pull unconfirmed transactions for all addresses
	for _, addr := range addresses {
		address, err := dcrutil.DecodeAddress(addr)
		if err != nil {
			writeInsightError(w, fmt.Sprintf("Address is invalid (%s)", addr))
			return
		}
		addressOuts, _, err := iapi.mp.UnconfirmedTxnsForAddress(address.String())
		if err != nil {
			writeInsightError(w, fmt.Sprintf("Error gathering mempool transactions (%s)", err))
			return
		}

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
		txOld, err := iapi.BlockData.GetRawTransaction(&rawTx)
		if err != nil {
			apiLog.Errorf("Unable to get transaction %s", rawTx)
			writeInsightError(w, fmt.Sprintf("Error gathering transaction details (%s)", err))
			return
		}
		txsOld = append(txsOld, txOld)
	}

	// Convert to Insight API struct
	txsNew, err := iapi.DcrToInsightTxns(txsOld, noAsm, noScriptSig, noSpent)
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
	writeJSON(w, addressOutput, iapi.getIndentQuery(r))
}

func (iapi *InsightApi) getAddressBalance(w http.ResponseWriter, r *http.Request) {
	addresses, err := m.GetAddressCtx(r, iapi.params, 1)
	if err != nil || len(addresses) > 1 {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	addressInfo, _, err := iapi.BlockData.ChainDB.AddressBalance(addresses[0])
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("AddressBalance: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}
	if err != nil || addressInfo == nil {
		apiLog.Warnf("AddressBalance: %v", err)
		http.Error(w, http.StatusText(422), 422)
		return
	}
	writeJSON(w, addressInfo.TotalUnspent, iapi.getIndentQuery(r))
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
		writeJSON(w, syncInfo, iapi.getIndentQuery(r))
	}

	blockChainHeight, err := iapi.nodeClient.GetBlockCount()
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
	writeJSON(w, syncInfo, iapi.getIndentQuery(r))
}

func (iapi *InsightApi) getStatusInfo(w http.ResponseWriter, r *http.Request) {
	statusInfo := m.GetStatusInfoCtx(r)

	// best block idx is also embedded through the middleware.  We could use
	// this value or the other best blocks as done below.  Which one is best?
	// idx := m.GetBlockIndexCtx(r)

	infoResult, err := iapi.nodeClient.GetInfo()
	if err != nil {
		apiLog.Error("Error getting status")
		writeInsightError(w, fmt.Sprintf("Error getting status (%s)", err))
		return
	}

	switch statusInfo {
	case "getDifficulty":
		info := struct {
			Difficulty float64 `json:"difficulty"`
		}{
			infoResult.Difficulty,
		}
		writeJSON(w, info, iapi.getIndentQuery(r))
	case "getBestBlockHash":
		blockhash, err := iapi.nodeClient.GetBlockHash(int64(infoResult.Blocks))
		if err != nil {
			apiLog.Errorf("Error getting block hash %d (%s)", infoResult.Blocks, err)
			writeInsightError(w, fmt.Sprintf("Error getting block hash %d (%s)", infoResult.Blocks, err))
			return
		}

		info := struct {
			BestBlockHash string `json:"bestblockhash"`
		}{
			blockhash.String(),
		}
		writeJSON(w, info, iapi.getIndentQuery(r))
	case "getLastBlockHash":
		blockhashtip, err := iapi.nodeClient.GetBlockHash(int64(infoResult.Blocks))
		if err != nil {
			apiLog.Errorf("Error getting block hash %d (%s)", infoResult.Blocks, err)
			writeInsightError(w, fmt.Sprintf("Error getting block hash %d (%s)", infoResult.Blocks, err))
			return
		}
		lastblockhash, err := iapi.nodeClient.GetBlockHash(int64(iapi.status.Height()))
		if err != nil {
			apiLog.Errorf("Error getting block hash %d (%s)", iapi.status.Height(), err)
			writeInsightError(w, fmt.Sprintf("Error getting block hash %d (%s)", iapi.status.Height(), err))
			return
		}

		info := struct {
			SyncTipHash   string `json:"syncTipHash"`
			LastBlockHash string `json:"lastblockhash"`
		}{
			blockhashtip.String(),
			lastblockhash.String(),
		}
		writeJSON(w, info, iapi.getIndentQuery(r))
	default:
		info := struct {
			Version         int32   `json:"version"`
			Protocolversion int32   `json:"protocolversion"`
			Blocks          int32   `json:"blocks"`
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

		writeJSON(w, info, iapi.getIndentQuery(r))
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
			fmt.Sprintf("Unable to retrieve block summary using time %s: %v",
				blockDateStr, err))
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
	blockSummary, err := iapi.BlockData.ChainDB.BlockSummaryTimeRange(minTime, maxTime, 0)
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("BlockSummaryTimeRange: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		writeInsightError(w, fmt.Sprintf("Unable to retrieve block summaries: %v", err))
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

	writeJSON(w, summaryOutput, iapi.getIndentQuery(r))
}

func (iapi *InsightApi) getAddressInfo(w http.ResponseWriter, r *http.Request) {
	addresses, err := m.GetAddressCtx(r, iapi.params, 1)
	if err != nil {
		writeInsightError(w, err.Error())
		return
	}
	if len(addresses) != 1 {
		writeInsightError(w, fmt.Sprintln("only one address allowed"))
		return
	}
	address := addresses[0]
	_, err = dcrutil.DecodeAddress(address)
	if err != nil {
		writeInsightError(w, "Invalid Address")
		return
	}

	// Get confirmed balance.
	balance, _, err := iapi.BlockData.ChainDB.AddressBalance(address)
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
			writeJSON(w, balance.TotalUnspent, iapi.getIndentQuery(r))
			return
		case "totalReceived":
			writeJSON(w, balance.TotalSpent+balance.TotalUnspent, iapi.getIndentQuery(r))
			return
		case "totalSent":
			writeJSON(w, balance.TotalSpent, iapi.getIndentQuery(r))
			return
		}
	}

	// Get confirmed transactions.
	rawTxs, recentTxs, err :=
		iapi.BlockData.ChainDB.InsightAddressTransactions(addresses, int64(iapi.status.Height()-2))
	if dbtypes.IsTimeoutErr(err) {
		apiLog.Errorf("InsightAddressTransactions: %v", err)
		http.Error(w, "Database timeout.", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		apiLog.Errorf("Error retrieving transactions for addresss %s: %v",
			addresses, err)
		http.Error(w, "Error retrieving transactions for that addresss.",
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
		writeJSON(w, unconfirmedBalanceSat, iapi.getIndentQuery(r))
		return
	}

	// Merge unconfirmed with confirmed transactions.
	rawTxs = append(unconfirmedTxs, rawTxs...)

	// Final raw tx slice extraction
	if txcount := int64(len(rawTxs)); txcount > 0 {
		txLimit := int64(1000)
		// "from" and "to" are zero-based indexes for inclusive range bounds.
		from := GetFromCtx(r)
		to, ok := GetToCtx(r)
		if !ok || to < from {
			to = from + txLimit - 1 // to is inclusive
		}

		// [from, to] --(limits)--> [start,end)
		start, end, err := fromToForSlice(from, to, txcount, txLimit)
		if err != nil {
			writeInsightError(w, err.Error())
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

	writeJSON(w, addressInfo, iapi.getIndentQuery(r))
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
	infoResult, err := iapi.nodeClient.GetInfo()
	if err != nil {
		apiLog.Error("Error getting status")
		writeInsightError(w, fmt.Sprintf("Error getting status (%s)", err))
		return
	}

	estimateFee := map[string]float64{
		strconv.Itoa(nbBlocks): infoResult.RelayFee,
	}

	writeJSON(w, estimateFee, iapi.getIndentQuery(r))
}

// GetPeerStatus handles requests for node peer info (i.e. getpeerinfo RPC).
func (iapi *InsightApi) GetPeerStatus(w http.ResponseWriter, r *http.Request) {
	// Use a RPC call to tell if we are connected or not
	_, err := iapi.nodeClient.GetPeerInfo()
	connected := err == nil

	var port *string
	peerInfo := struct {
		Connected bool    `json:"connected"`
		Host      string  `json:"host"`
		Port      *string `json:"port"`
	}{
		connected, "127.0.0.1", port,
	}

	writeJSON(w, peerInfo, iapi.getIndentQuery(r))
}
