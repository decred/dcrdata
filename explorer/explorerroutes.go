// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import (
	"io"
	"net/http"
	"strconv"

	"github.com/decred/dcrd/chaincfg/chainhash"
)

// Home is the page handler for the "/" path
func (exp *explorerUI) Home(w http.ResponseWriter, r *http.Request) {
	height := exp.blockData.GetHeight()

	blocks := exp.blockData.GetExplorerBlocks(height, height-6)

	exp.NewBlockDataMtx.Lock()
	exp.MempoolData.RLock()
	str, err := templateExecToString(exp.templates[homeTemplateIndex], "home", struct {
		Info    *HomeInfo
		Mempool *MempoolInfo
		Blocks  []*BlockBasic
	}{
		exp.ExtraInfo,
		exp.MempoolData,
		blocks,
	})
	exp.NewBlockDataMtx.Unlock()
	exp.MempoolData.RUnlock()

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.ErrorPage(w, "Something went wrong...", "and it's not your fault, try refreshing... that usually fixes things", false)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Blocks is the page handler for the "/blocks" path
func (exp *explorerUI) Blocks(w http.ResponseWriter, r *http.Request) {
	idx := exp.blockData.GetHeight()

	height, err := strconv.Atoi(r.URL.Query().Get("height"))
	if err != nil || height > idx {
		height = idx
	}

	rows, err := strconv.Atoi(r.URL.Query().Get("rows"))
	if err != nil || rows > maxExplorerRows || rows < minExplorerRows || height-rows < 0 {
		rows = minExplorerRows
	}
	summaries := exp.blockData.GetExplorerBlocks(height, height-rows)
	if summaries == nil {
		log.Errorf("Unable to get blocks: height=%d&rows=%d", height, rows)
		exp.ErrorPage(w, "Something went wrong...", "could not find those blocks", true)
		return
	}

	str, err := templateExecToString(exp.templates[rootTemplateIndex], "explorer", struct {
		Data      []*BlockBasic
		BestBlock int
	}{
		summaries,
		idx,
	})

	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.ErrorPage(w, "Something went wrong...", "and it's not your fault, try refreshing... that usually fixes things", false)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Block is the page handler for the "/block" path
func (exp *explorerUI) Block(w http.ResponseWriter, r *http.Request) {
	hash := getBlockHashCtx(r)

	data := exp.blockData.GetExplorerBlock(hash)
	if data == nil {
		log.Errorf("Unable to get block %s", hash)
		exp.ErrorPage(w, "Something went wrong...", "could not find that block", true)
		return
	}

	pageData := struct {
		Data          *BlockInfo
		ConfirmHeight int64
	}{
		data,
		exp.NewBlockData.Height - data.Confirmations,
	}
	str, err := templateExecToString(exp.templates[blockTemplateIndex], "block", pageData)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.ErrorPage(w, "Something went wrong...", "and it's not your fault, try refreshing... that usually fixes things", false)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// TxPage is the page handler for the "/tx" path
func (exp *explorerUI) TxPage(w http.ResponseWriter, r *http.Request) {
	// attempt to get tx hash string from URL path
	hash, ok := r.Context().Value(ctxTxHash).(string)
	if !ok {
		log.Trace("txid not set")
		exp.ErrorPage(w, "Something went wrong...", "there was no transaction requested", true)
		return
	}
	tx := exp.blockData.GetExplorerTx(hash)
	if tx == nil {
		log.Errorf("Unable to get transaction %s", hash)
		exp.ErrorPage(w, "Something went wrong...", "could not find that transaction", true)
		return
	}
	if !exp.liteMode {
		// For each output of this transaction, look up any spending transactions,
		// and the index of the spending transaction input.
		spendingTxHashes, spendingTxVinInds, voutInds, err := exp.explorerSource.SpendingTransactions(hash)
		if err != nil {
			log.Errorf("Unable to retrieve spending transactions for %s: %v", hash, err)
			exp.ErrorPage(w, "Something went wrong...", "and it's not your fault, try refreshing... that usually fixes things", false)
			return
		}
		for i, vout := range voutInds {
			if int(vout) >= len(tx.SpendingTxns) {
				log.Errorf("Invalid spending transaction data (%s:%d)", hash, vout)
				continue
			}
			tx.SpendingTxns[vout] = TxInID{
				Hash:  spendingTxHashes[i],
				Index: spendingTxVinInds[i],
			}
		}
	}

	pageData := struct {
		Data          *TxInfo
		ConfirmHeight int64
	}{
		tx,
		exp.NewBlockData.Height - tx.Confirmations,
	}

	str, err := templateExecToString(exp.templates[txTemplateIndex], "tx", pageData)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.ErrorPage(w, "Something went wrong...", "and it's not your fault, try refreshing... that usually fixes things", false)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// AddressPage is the page handler for the "/address" path
func (exp *explorerUI) AddressPage(w http.ResponseWriter, r *http.Request) {
	// Get the address URL parameter, which should be set in the request context
	// by the addressPathCtx middleware.
	address, ok := r.Context().Value(ctxAddress).(string)
	if !ok {
		log.Trace("address not set")
		exp.ErrorPage(w, "Something went wrong...", "there seems to not be an address in this request", true)
		return
	}

	// Number of outputs for the address to query the database for. The URL
	// query parameter "n" is used to specify the limit (e.g. "?n=20").
	limitN, err := strconv.ParseInt(r.URL.Query().Get("n"), 10, 64)
	if err != nil || limitN < 0 {
		limitN = defaultAddressRows
	} else if limitN > maxAddressRows {
		log.Warnf("addressPage: requested up to %d address rows, "+
			"limiting to %d", limitN, maxAddressRows)
		limitN = maxAddressRows
	}

	// Number of outputs to skip (OFFSET in database query). For UX reasons, the
	// "start" URL query parameter is used.
	offsetAddrOuts, err := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
	if err != nil || offsetAddrOuts < 0 {
		offsetAddrOuts = 0
	}

	var addrData *AddressInfo
	if exp.liteMode {
		addrData = exp.blockData.GetExplorerAddress(address, limitN, offsetAddrOuts)
		if addrData == nil {
			log.Errorf("Unable to get address %s", address)
			exp.ErrorPage(w, "Something went wrong...", "could not find that address", true)
			return
		}
	} else {
		// Get addresses table rows for the address
		addrHist, balance, errH := exp.explorerSource.AddressHistory(
			address, limitN, offsetAddrOuts)
		if errH != nil {
			log.Errorf("Unable to get address %s history: %v", address, errH)
			addrData := exp.blockData.GetExplorerAddress(address, limitN, offsetAddrOuts)

			if addrData == nil {
				exp.ErrorPage(w, "Something went wrong...", "could not find that address", false)
			}

			pageData := struct {
				Data          *AddressInfo
				ConfirmHeight []int64
			}{
				addrData,
				nil,
			}

			str, err := templateExecToString(exp.templates[addressTemplateIndex], "address", pageData)
			if err != nil {
				log.Errorf("Template execute failure: %v", err)
				exp.ErrorPage(w, "Something went wrong...", "and it's not your fault, try refreshing... that usually fixes things", false)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, str)
			return
		}

		// Generate AddressInfo skeleton from the address table rows
		addrData = ReduceAddressHistory(addrHist)
		if addrData == nil {
			log.Debugf("empty address history (%s): n=%d&start=%d", address, limitN, offsetAddrOuts)
			exp.ErrorPage(w, "Something went wrong...", "that address has no history", true)
			return
		}
		addrData.Limit, addrData.Offset = limitN, offsetAddrOuts
		addrData.KnownFundingTxns = balance.NumSpent + balance.NumUnspent
		addrData.Balance = balance
		addrData.Path = r.URL.Path
		// still need []*AddressTx filled out and NumUnconfirmed

		// Query database for transaction details
		err = exp.explorerSource.FillAddressTransactions(addrData)
		if err != nil {
			log.Errorf("Unable to fill address %s transactions: %v", address, err)
			exp.ErrorPage(w, "Something went wrong...", "could not find transactions for that address", false)
			return
		}
	}

	confirmHeights := make([]int64, len(addrData.Transactions))
	for i, v := range addrData.Transactions {
		confirmHeights[i] = exp.NewBlockData.Height - int64(v.Confirmations)
	}
	pageData := struct {
		Data          *AddressInfo
		ConfirmHeight []int64
	}{
		addrData,
		confirmHeights,
	}

	str, err := templateExecToString(exp.templates[addressTemplateIndex], "address", pageData)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.ErrorPage(w, "Something went wrong...", "and it's not your fault, try refreshing... that usually fixes things", false)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

func (exp *explorerUI) DecodeTxPage(w http.ResponseWriter, r *http.Request) {
	str, err := templateExecToString(exp.templates[decodeTxTemplateIndex], "rawtx", nil)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.ErrorPage(w, "Something went wrong...", "and it's not your fault, try refreshing, that usually fixes things", false)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// search implements a primitive search algorithm by checking if the value in
// question is a block index, block hash, address hash or transaction hash and
// redirects to the appropriate page or displays an error
func (exp *explorerUI) search(searchStr string) string {
	if searchStr == "" {
		return ""
	}

	// Attempt to get a block hash by calling GetBlockHash to see if the value
	// is a block index and then redirect to the block page if it is
	idx, err := strconv.ParseInt(searchStr, 10, 0)
	if err == nil {
		_, err = exp.blockData.GetBlockHash(idx)
		if err == nil {
			return "/block/" + searchStr
		}
	}

	// Call GetExplorerAddress to see if the value is an address hash and
	// then redirect to the address page if it is
	address := exp.blockData.GetExplorerAddress(searchStr, 1, 0)
	if address != nil {
		return "/address/" + searchStr
	}

	// Check if the value is a valid hash
	if _, err = chainhash.NewHashFromStr(searchStr); err != nil {
		return ""
	}

	// Attempt to get a block index by calling GetBlockHeight to see if the
	// value is a block hash and then redirect to the block page if it is
	_, err = exp.blockData.GetBlockHeight(searchStr)
	if err == nil {
		return "/block/" + searchStr
	}

	// Call GetExplorerTx to see if the value is a transaction hash and then
	// redirect to the tx page if it is
	tx := exp.blockData.GetExplorerTx(searchStr)
	if tx != nil {
		return "/tx/" + searchStr
	}
	return ""
}

// ErrorPage provides a way to show error on the pages without redirecting
func (exp *explorerUI) ErrorPage(w http.ResponseWriter, code string, message string, notFound bool) {
	str, err := templateExecToString(exp.templates[errorTemplateIndex], "error", struct {
		ErrorCode   string
		ErrorString string
	}{
		code,
		message,
	})
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		str = "Something went very wrong if you can see this, try refreshing"
	}
	w.Header().Set("Content-Type", "text/html")
	if notFound {
		w.WriteHeader(http.StatusNotFound)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
	io.WriteString(w, str)
}

// NotFound wraps ErrorPage to display a 404 page
func (exp *explorerUI) NotFound(w http.ResponseWriter, r *http.Request) {
	exp.ErrorPage(w, "Not found", "Cannot find page: "+r.URL.Path, true)
}
