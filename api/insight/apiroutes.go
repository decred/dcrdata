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
	"io/ioutil"
	"net/http"
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
)

type insightApiContext struct {
	nodeClient *rpcclient.Client
	BlockData  *dcrpg.ChainDBRPC
	Status     apitypes.Status
	statusMtx  sync.RWMutex
	JSONIndent string
}

// NewInsightContext Constructor for insightApiContext
func NewInsightContext(client *rpcclient.Client, blockData *dcrpg.ChainDBRPC, JSONIndent string) *insightApiContext {
	conns, _ := client.GetConnectionCount()
	nodeHeight, _ := client.GetBlockCount()
	version := semver.NewSemver(1, 0, 0)

	newContext := insightApiContext{
		nodeClient: client,
		BlockData:  blockData,
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

func (c *insightApiContext) getTransaction(w http.ResponseWriter, r *http.Request) {
	txid := m.GetTxIDCtx(r)
	if txid == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	// Return raw transaction
	txOld, err := c.BlockData.GetRawTransaction(txid)
	if err != nil {
		apiLog.Errorf("Unable to get transaction %s", txid)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	// convert stuct type to new sturct
	txNew := dbtypes.TxConverter(txOld)

	// This block set addr value in tx vin
	for _, vin := range txNew.Vins {
		if vin.Txid != "" {
			vin.UnconfirmedInput = false
			vin.IsConfirmed = true
			vinsTx, err := c.BlockData.GetRawTransaction(vin.Txid)
			if err != nil {
				apiLog.Errorf("Tried to get transaction by vin tx %s", vin.Txid)
				http.Error(w, http.StatusText(422), 422)
				return
			}
			vin.Confirmations = vinsTx.Confirmations
			for _, vinVout := range vinsTx.Vout {
				if vinVout.Value == vin.Amountin {
					if vinVout.ScriptPubKey.Addresses != nil {
						if vinVout.ScriptPubKey.Addresses[0] != "" {
							vin.Addr = vinVout.ScriptPubKey.Addresses[0]
						}
					}
				}
			}
		} else {
			vin.Confirmations = 0
			vin.UnconfirmedInput = true
			vin.IsConfirmed = false
			txNew.IncompleteInputs++ // add 1 to incomplete inputs
		}
	}

	// set of unique addresses for db query
	uniqAddrs := make(map[string]string)

	for _, vout := range txNew.Vout {
		for _, addr := range vout.ScriptPubKeyValue.Addresses {
			uniqAddrs[addr] = txNew.Txid
		}
	}

	addresses := []string{}
	for addr := range uniqAddrs {
		addresses = append(addresses, addr)
	}

	addrFull := c.BlockData.ChainDB.GetAddressSpendByFunHash(addresses, txNew.Txid)
	for _, dbaddr := range addrFull {
		txNew.Vout[dbaddr.FundingTxVoutIndex].SpentIndex = dbaddr.SpendingTxVinIndex
		txNew.Vout[dbaddr.FundingTxVoutIndex].SpentTxID = dbaddr.SpendingTxHash
		txNew.Vout[dbaddr.FundingTxVoutIndex].SpentTs = 0 // todo
	}

	// create block hash
	bHash, err := chainhash.NewHashFromStr(txNew.Blockhash)
	if err != nil {
		apiLog.Errorf("Faild to gen block hash for Tx %s", txid)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	// get block
	block, err := c.BlockData.Client.GetBlock(bHash)
	if err != nil {
		apiLog.Errorf("Unable to get block %s", bHash)
		http.Error(w, http.StatusText(422), 422)
		return
	}

	// stakeTree 0: Tx, 1: stakeTx
	dbTransactions, _, _ := dbtypes.ExtractBlockTransactions(block, 0, &chaincfg.MainNetParams)

	sdbTransactions, _, _ := dbtypes.ExtractBlockTransactions(block, 1, &chaincfg.MainNetParams)

	// its cumbersome but easier than differentiate tx and stx at that point
	dbTransactions = append(dbTransactions, sdbTransactions...)

	for _, dbtx := range dbTransactions {
		if dbtx.TxID == txid {
			txNew.Size = dbtx.Size
			txNew.Fees = dcrutil.Amount(dbtx.Fees).ToCoin()
			if txNew.IsStakeGen || txNew.IsCoinBase {
				txNew.Fees = 0
			}

			txNew.ValueOut, _ = strconv.ParseFloat(fmt.Sprintf("%.8f", txNew.ValueOut), 64)

			break
		}
	}

	writeJSON(w, txNew, c.getIndentQuery(r))
}

func (c *insightApiContext) getTransactionHex(w http.ResponseWriter, r *http.Request) {
	txid := m.GetTxIDCtx(r)
	if txid == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	txHex := c.BlockData.GetTransactionHex(txid)

	hexOutput := new(apitypes.InsightRawTx)
	hexOutput.Rawtx = txHex

	writeJSON(w, hexOutput, c.getIndentQuery(r))
}

func (c *insightApiContext) getBlockSummary(w http.ResponseWriter, r *http.Request) {
	// attempt to get hash of block set by hash or (fallback) height set on path
	hash := c.getBlockHashCtx(r)
	if hash == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	blockSummary := c.BlockData.GetBlockVerboseByHash(hash, false)

	writeJSON(w, blockSummary, c.getIndentQuery(r))
}

func (c *insightApiContext) getBlockHash(w http.ResponseWriter, r *http.Request) {
	hash := c.getBlockHashCtx(r)

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
	hash := c.getBlockChainHashCtx(r)
	blockMsg, err := c.nodeClient.GetBlock(hash)
	if err != nil {
		apiLog.Errorf("Failed to retrieve block %s: %v", hash.String(), err)
		http.Error(w, http.StatusText(422), 422)
		return
	}
	var blockHex bytes.Buffer
	if err = blockMsg.Serialize(&blockHex); err != nil {
		apiLog.Errorf("Failed to serialize block: %v", err)
		http.Error(w, http.StatusText(422), 422)
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
	// Allow parameters to be posted either as query or in the body as a JSON
	rawHexTx := m.GetRawHexTx(r) // Check for a query post

	if rawHexTx == "" {
		// No query post.  Check for a Body JSON
		// Read and close the JSON-RPC request body from the caller.
		body, err := ioutil.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			apiLog.Errorf("error reading JSON message: %v", err)
			http.Error(w, http.StatusText(422), 422)
			return
		}
		var req apitypes.InsightRawTx
		err = json.Unmarshal(body, &req)
		if err != nil {
			apiLog.Errorf("Failed to parse request: %v", err)
			http.Error(w, http.StatusText(422), 422)
			return
		}
		// Successful extraction of Body JSON
		rawHexTx = req.Rawtx
	}

	txid, err := c.BlockData.SendRawTransaction(rawHexTx)
	if err != nil {
		apiLog.Errorf("Unable to send transaction %s", rawHexTx)
		// Write the raw error out for display
		writeText(w, txid)
		return
	}

	txidJSON := struct {
		TxidHash string `json:"rawtx"`
	}{
		txid,
	}
	writeJSON(w, txidJSON, c.getIndentQuery(r))
}

func (c *insightApiContext) getAddressTxnOutput(w http.ResponseWriter, r *http.Request) {
	address := m.GetAddressCtx(r)
	if address == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}
	txnOutputs := c.BlockData.ChainDB.GetAddressUTXO(address)
	writeJSON(w, txnOutputs, c.getIndentQuery(r))
}

func (c *insightApiContext) getAddressesTxnOutput(w http.ResponseWriter, r *http.Request) {
	addresses := strings.Split(m.GetAddressCtx(r), ",")

	var txnOutputs []apitypes.AddressTxnOutput

	for _, address := range addresses {
		if address == "" {
			http.Error(w, http.StatusText(422), 422)
			return
		}
		utxo := c.BlockData.ChainDB.GetAddressUTXO(address)
		txnOutputs = append(txnOutputs, utxo...)
	}

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
	address := m.GetAddressCtx(r)
	count := m.GetCountCtx(r)
	offset := m.GetOffsetCtx(r)

	if address == "" {
		http.Error(w, http.StatusText(422), 422)
		return
	}

	addresses := strings.Split(address, ",")

	addressOutput := new(apitypes.InsightAddress)

	addressOutput.From = offset
	addressOutput.To = count

	for _, addr := range addresses {
		addressTxn := c.BlockData.InsightGetAddressTransactions(addr, count, offset)
		addressOutput.Transactions = append(addressOutput.Transactions, addressTxn...)
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
	limit := m.GetLimitCtx(r)

	layout := "2006-01-02 15:04:05"

	minDate, err := time.Parse(layout, blockDate+" 00:00:00")
	if err != nil {
		apiLog.Errorf("Unable to retrieve block summary using time %s: %v", blockDate, err)
		http.Error(w, "invalid date ", 422)
		return
	}

	maxDate, err := time.Parse(layout, blockDate+" 23:59:59")
	if err != nil {
		apiLog.Errorf("Unable to retrieve block summary using time %s: %v", blockDate, err)
		http.Error(w, "invalid date", 422)
		return
	}

	minTime, maxTime := minDate.Unix(), maxDate.Unix()

	blockSummary := c.BlockData.ChainDB.GetBlockSummaryTimeRange(minTime, maxTime, limit)

	if blockSummary == nil {
		http.Error(w, "error occurred", 422)
		return
	}

	summaryOutput := struct {
		Blocks []dbtypes.BlockDataBasic `json:"blocks"`
		Length int                      `json:"length"`
	}{
		blockSummary, limit,
	}

	writeJSON(w, summaryOutput, c.getIndentQuery(r))

}

func (c *insightApiContext) getAddressInfo(w http.ResponseWriter, r *http.Request) {
	address := m.GetAddressCtx(r)
	offset := m.GetOffsetCtx(r)
	count := m.GetCountCtx(r)
	count -= offset

	if count < 0 {
		count = 20
	}

	addressInfo := c.BlockData.ChainDB.GetAddressInfo(address, int64(count), int64(offset))

	if addressInfo == nil {
		http.Error(w, "an error occurred", 422)
		return
	}

	writeJSON(w, addressInfo, c.getIndentQuery(r))
}
