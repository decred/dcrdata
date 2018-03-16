// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import (
	"database/sql"
	"io"
	"net/http"
	"strconv"

	"github.com/decred/dcrd/chaincfg/chainhash"
)

// Home is the page handler for the "/" path
func (exp *explorerUI) Home(w http.ResponseWriter, r *http.Request) {
	height := exp.blockData.GetHeight()

	blocks := exp.blockData.GetExplorerBlocks(height, height-5)

	exp.NewBlockDataMtx.Lock()
	exp.MempoolData.RLock()
	str, err := exp.templates.execTemplateToString("home", struct {
		Info    *HomeInfo
		Mempool *MempoolInfo
		Blocks  []*BlockBasic
		Version string
	}{
		exp.ExtraInfo,
		exp.MempoolData,
		blocks,
		exp.Version,
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

	str, err := exp.templates.execTemplateToString("explorer", struct {
		Data      []*BlockBasic
		BestBlock int
		Version   string
	}{
		summaries,
		idx,
		exp.Version,
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
	// Checking if there exists any regular non-Coinbase transactions in the block.
	var count int
	data.TxAvailable = true
	for _, i := range data.Tx {
		if i.Coinbase {
			count++
		}
	}
	if count == len(data.Tx) {
		data.TxAvailable = false
	}

	if !exp.liteMode {
		var err error
		data.Misses, err = exp.explorerSource.BlockMissedVotes(hash)
		if err != nil && err != sql.ErrNoRows {
			log.Warnf("Unable to retrieve missed votes for block %s: %v", hash, err)
		}
	}

	pageData := struct {
		Data          *BlockInfo
		ConfirmHeight int64
		Version       string
	}{
		data,
		exp.NewBlockData.Height - data.Confirmations,
		exp.Version,
	}
	str, err := exp.templates.execTemplateToString("block", pageData)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.ErrorPage(w, "Something went wrong...", "and it's not your fault, try refreshing... that usually fixes things", false)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Turbolinks-Location", "/block/"+hash)
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Mempool is the page handler for the "/mempool" path
func (exp *explorerUI) Mempool(w http.ResponseWriter, r *http.Request) {
	exp.MempoolData.RLock()
	str, err := exp.templates.execTemplateToString("mempool", struct {
		Mempool *MempoolInfo
		Version string
	}{
		exp.MempoolData,
		exp.Version,
	})
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
		if tx.Type == "Ticket" {
			spendStatus, poolStatus, err := exp.explorerSource.PoolStatusForTicket(hash)
			if err != nil {
				log.Errorf("Unable to retrieve ticket spend and pool status for %s: %v", hash, err)
			} else {
				if tx.Mature == "False" {
					tx.TicketInfo.PoolStatus = "immature"
				} else {
					tx.TicketInfo.PoolStatus = poolStatus.String()
				}
				tx.TicketInfo.SpendStatus = spendStatus.String()
				tx.TicketInfo.TicketPoolSize = int64(exp.ChainParams.TicketPoolSize) * int64(exp.ChainParams.TicketsPerBlock)
				tx.TicketInfo.TicketExpiry = int64(exp.ChainParams.TicketExpiry)
				expirationInDays := (exp.ChainParams.TargetTimePerBlock.Hours() * float64(exp.ChainParams.TicketExpiry)) / 24
				maturityInDay := (exp.ChainParams.TargetTimePerBlock.Hours() * float64(tx.TicketInfo.TicketMaturity)) / 24
				tx.TicketInfo.TimeTillMaturity = ((float64(exp.ChainParams.TicketMaturity) - float64(tx.Confirmations)) / float64(exp.ChainParams.TicketMaturity)) * maturityInDay
				ticketExpiryBlocksLeft := int64(exp.ChainParams.TicketExpiry) - tx.Confirmations
				tx.TicketInfo.TicketExpiryDaysLeft = (float64(ticketExpiryBlocksLeft) / float64(exp.ChainParams.TicketExpiry)) * expirationInDays
				if tx.TicketInfo.SpendStatus == "Voted" {
					tx.TicketInfo.ShortConfirms = exp.blockData.TxHeight(tx.SpendingTxns[0].Hash) - tx.BlockHeight
				} else if tx.Confirmations >= int64(exp.ChainParams.TicketExpiry) {
					tx.TicketInfo.ShortConfirms = int64(exp.ChainParams.TicketExpiry)
				} else {
					tx.TicketInfo.ShortConfirms = tx.Confirmations
				}
				voteRounds := (tx.TicketInfo.ShortConfirms - tx.TicketMaturity)
				tx.TicketInfo.BestLuck = tx.TicketInfo.TicketExpiry / int64(exp.ChainParams.TicketPoolSize)
				tx.TicketInfo.AvgLuck = tx.TicketInfo.BestLuck - 1
				tx.TicketInfo.VoteLuck = float64(tx.TicketInfo.BestLuck) - (float64(voteRounds) / float64(exp.ChainParams.TicketPoolSize))
				if tx.TicketInfo.VoteLuck >= float64(tx.TicketInfo.BestLuck-(1/int64(exp.ChainParams.TicketPoolSize))) {
					tx.TicketInfo.LuckStatus = "Perfection"
				} else if tx.TicketInfo.VoteLuck > (float64(tx.TicketInfo.BestLuck) - 0.25) {
					tx.TicketInfo.LuckStatus = "Very Lucky!"
				} else if tx.TicketInfo.VoteLuck > (float64(tx.TicketInfo.BestLuck) - 0.75) {
					tx.TicketInfo.LuckStatus = "Good Luck"
				} else if tx.TicketInfo.VoteLuck > (float64(tx.TicketInfo.BestLuck) - 1.25) {
					tx.TicketInfo.LuckStatus = "Normal"
				} else if tx.TicketInfo.VoteLuck > (float64(tx.TicketInfo.BestLuck) * 0.50) {
					tx.TicketInfo.LuckStatus = "Bad Luck"
				} else if tx.TicketInfo.VoteLuck > 0 {
					tx.TicketInfo.LuckStatus = "Horrible Luck!"
				} else if tx.TicketInfo.VoteLuck == 0 {
					tx.TicketInfo.LuckStatus = "No Luck"
				}
			}
		}
	}

	pageData := struct {
		Data          *TxInfo
		ConfirmHeight int64
		Version       string
	}{
		tx,
		exp.NewBlockData.Height - tx.Confirmations,
		exp.Version,
	}

	str, err := exp.templates.execTemplateToString("tx", pageData)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.ErrorPage(w, "Something went wrong...", "and it's not your fault, try refreshing... that usually fixes things", false)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Turbolinks-Location", "/tx/"+hash)
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
	} else if limitN > MaxAddressRows {
		log.Warnf("addressPage: requested up to %d address rows, "+
			"limiting to %d", limitN, MaxAddressRows)
		limitN = MaxAddressRows
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
			addrData = exp.blockData.GetExplorerAddress(address, limitN, offsetAddrOuts)
			if addrData == nil {
				log.Errorf("Unable to get address %s", address)
				exp.ErrorPage(w, "Something went wrong...", "could not find that address", true)
				return
			}
			confirmHeights := make([]int64, len(addrData.Transactions))
			if addrData == nil {
				exp.ErrorPage(w, "Something went wrong...", "could not find that address", false)
			}
			addrData.Fullmode = true
			pageData := struct {
				Data          *AddressInfo
				ConfirmHeight []int64
				Version       string
			}{
				addrData,
				confirmHeights,
				exp.Version,
			}
			str, err := exp.templates.execTemplateToString("address", pageData)
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
		addrData.KnownTransactions = (balance.NumSpent * 2) + balance.NumUnspent
		addrData.NumTransactions = int64(len(addrData.Transactions))
		if addrData.NumTransactions > addrData.Limit {
			addrData.NumTransactions = addrData.Limit
		}
		addrData.Fullmode = true
		// still need []*AddressTx filled out and NumUnconfirmed

		// Query database for transaction details
		err = exp.explorerSource.FillAddressTransactions(addrData)
		if err != nil {
			log.Errorf("Unable to fill address %s transactions: %v", address, err)
			exp.ErrorPage(w, "Something went wrong...", "could not find transactions for that address", false)
			return
		}
		addrData.NumUnconfirmed, err = exp.blockData.CountUnconfirmedTransactions(address, MaxUnconfirmedPossible)
		if err != nil {
			log.Warnf("SearchRawTransactionsForUnconfirmedTransactions failed for address %s: %v", address, err)
		}
	}

	confirmHeights := make([]int64, len(addrData.Transactions))
	for i, v := range addrData.Transactions {
		confirmHeights[i] = exp.NewBlockData.Height - int64(v.Confirmations)
	}
	pageData := struct {
		Data          *AddressInfo
		ConfirmHeight []int64
		Version       string
	}{
		addrData,
		confirmHeights,
		exp.Version,
	}

	str, err := exp.templates.execTemplateToString("address", pageData)
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.ErrorPage(w, "Something went wrong...", "and it's not your fault, try refreshing... that usually fixes things", false)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Turbolinks-Location", "/address/"+address)
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

func (exp *explorerUI) DecodeTxPage(w http.ResponseWriter, r *http.Request) {
	str, err := exp.templates.execTemplateToString("rawtx", struct {
		Version string
	}{
		exp.Version,
	})
	if err != nil {
		log.Errorf("Template execute failure: %v", err)
		exp.ErrorPage(w, "Something went wrong...", "and it's not your fault, try refreshing, that usually fixes things", false)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}

// Search implements a primitive search algorithm by checking if the value in
// question is a block index, block hash, address hash or transaction hash and
// redirects to the appropriate page or displays an error
func (exp *explorerUI) Search(w http.ResponseWriter, r *http.Request) {
	searchStr := r.URL.Query().Get("search")
	if searchStr == "" {
		exp.ErrorPage(w, "search failed", "Nothing was searched for", true)
		return
	}

	// Attempt to get a block hash by calling GetBlockHash to see if the value
	// is a block index and then redirect to the block page if it is
	idx, err := strconv.ParseInt(searchStr, 10, 0)
	if err == nil {
		_, err = exp.blockData.GetBlockHash(idx)
		if err == nil {
			http.Redirect(w, r, "/block/"+searchStr, http.StatusPermanentRedirect)
			return
		}
		exp.ErrorPage(w, "search failed", "Block "+searchStr+" has not yet been mined", true)
		return
	}

	// Call GetExplorerAddress to see if the value is an address hash and
	// then redirect to the address page if it is
	address := exp.blockData.GetExplorerAddress(searchStr, 1, 0)
	if address != nil {
		http.Redirect(w, r, "/address/"+searchStr, http.StatusPermanentRedirect)
		return
	}

	// Check if the value is a valid hash
	if _, err = chainhash.NewHashFromStr(searchStr); err != nil {
		exp.ErrorPage(w, "search failed", "Couldn't find any address "+searchStr, true)
		return
	}

	// Attempt to get a block index by calling GetBlockHeight to see if the
	// value is a block hash and then redirect to the block page if it is
	_, err = exp.blockData.GetBlockHeight(searchStr)
	if err == nil {
		http.Redirect(w, r, "/block/"+searchStr, http.StatusPermanentRedirect)
		return
	}

	// Call GetExplorerTx to see if the value is a transaction hash and then
	// redirect to the tx page if it is
	tx := exp.blockData.GetExplorerTx(searchStr)
	if tx != nil {
		http.Redirect(w, r, "/tx/"+searchStr, http.StatusPermanentRedirect)
		return
	}
	exp.ErrorPage(w, "search failed", "Could not find any transaction or block "+searchStr, true)
}

// ErrorPage provides a way to show error on the pages without redirecting
func (exp *explorerUI) ErrorPage(w http.ResponseWriter, code string, message string, notFound bool) {
	str, err := exp.templates.execTemplateToString("error", struct {
		ErrorCode   string
		ErrorString string
		Version     string
	}{
		code,
		message,
		exp.Version,
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
